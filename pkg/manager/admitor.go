/*
 * Tencent is pleased to support the open source community by making TKEStack available.
 *
 * Copyright (C) 2012-2019 Tencent. All Rights Reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License"); you may not use
 * this file except in compliance with the License. You may obtain a copy of the
 * License at
 *
 * https://opensource.org/licenses/Apache-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS, WITHOUT
 * WARRANTIES OF ANY KIND, either express or implied.  See the License for the
 * specific language governing permissions and limitations under the License.
 */

package manager

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"

	storagev1alpha1 "tkestack.io/volume-decorator/pkg/apis/storage/v1"
	"tkestack.io/volume-decorator/pkg/util"
	"tkestack.io/volume-decorator/pkg/volume"
	"tkestack.io/volume-decorator/pkg/workload"

	admissionv1beta1 "k8s.io/api/admission/v1beta1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog"
)

// newAdmitor creates an admitor object.
func newAdmitor(volumeManager volume.Manager, workloadManager workload.Manager) *admitor {
	return &admitor{
		volumeManager:   volumeManager,
		workloadManager: workloadManager,
	}
}

// admitor is a administrator to check whether a workload can be created or updated.
type admitor struct {
	volumeManager   volume.Manager
	workloadManager workload.Manager
}

// handle handles an admission request.
func (a *admitor) handle(w http.ResponseWriter, req *http.Request) {
	klog.V(5).Info("Receive workload request")

	if req.Body == nil {
		klog.Error("Receive an invalid request, body is empty")
		response(w, http.StatusBadRequest, "request body required")
		return
	}

	data, err := ioutil.ReadAll(req.Body)
	if err != nil {
		klog.Errorf("Read request body failed: %v", err)
		response(w, http.StatusInternalServerError, fmt.Sprintf("read request body failed: %v", err))
		return
	}

	request := &admissionv1beta1.AdmissionReview{}
	deserializer := util.Codecs.UniversalDeserializer()
	if _, _, err := deserializer.Decode(data, nil, request); err != nil {
		klog.Errorf("Parse request body failed: %s, %v", string(data), err)
		response(w, http.StatusBadRequest, fmt.Sprintf("parse request failed: %v", err))
		return
	}

	klog.V(5).Infof("Receive workload %s request: %+v/%s/%s",
		request.Request.Operation, request.Request.Resource, request.Request.Namespace, request.Request.Name)

	respBytes, err := json.Marshal(a.handleWorkload(request))
	if err != nil {
		response(w, http.StatusInternalServerError, fmt.Sprintf("marshal response failed: %v", err))
		return
	}
	if _, err := w.Write(respBytes); err != nil {
		klog.Errorf("Send response failed: %v", err)
	}
}

// handleWorkload handles a AdmissionReview.
func (a *admitor) handleWorkload(request *admissionv1beta1.AdmissionReview) *admissionv1beta1.AdmissionReview {
	resp := &admissionv1beta1.AdmissionReview{
		Response: &admissionv1beta1.AdmissionResponse{UID: request.Request.UID},
	}

	w, usedVolumes, _, err := a.workloadManager.Handle(request.Request)
	if err != nil {
		if workload.IsIgnore(err) {
			markResponseAsSuccess(resp)
			return resp
		}
		resp.Response.Result = &metav1.Status{
			Status:  metav1.StatusFailure,
			Reason:  metav1.StatusReasonInternalError,
			Message: err.Error(),
			Code:    http.StatusInternalServerError,
		}
		return resp
	}

	now := metav1.Now()
	for _, vol := range usedVolumes {
		err := a.volumeManager.Attach(&storagev1alpha1.Workload{
			ObjectReference: w.ObjectReference,
			ReadOnly:        vol.ReadOnly,
			Replicas:        w.Replicas,
			Timestamp:       &now,
		}, request.Request.Namespace, vol.ClaimName)
		if err != nil {
			resp.Response.Result = statusFromError(err)
			return resp
		}
	}

	markResponseAsSuccess(resp)

	return resp
}

// response is an utility to write response to a http request.
func response(w http.ResponseWriter, status int, message string) {
	w.WriteHeader(status)
	_, _ = w.Write([]byte(message))
}

// markResponseAsSuccess set the resp to success.
func markResponseAsSuccess(resp *admissionv1beta1.AdmissionReview) {
	resp.Response.Allowed = true
	resp.Response.Result = &metav1.Status{
		Status: metav1.StatusSuccess,
	}
}

// statusFromError generates an Status from an error.
func statusFromError(err error) *metav1.Status {
	if statusErr, ok := err.(*k8serrors.StatusError); ok {
		status := statusErr.Status()
		return &status
	}
	return &metav1.Status{
		Status:  metav1.StatusFailure,
		Reason:  k8serrors.ReasonForError(err),
		Message: err.Error(),
		Code:    http.StatusInternalServerError,
	}
}
