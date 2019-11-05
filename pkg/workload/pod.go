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

package workload

import (
	"fmt"

	"tkestack.io/volume-decorator/pkg/util"

	admissionv1beta1 "k8s.io/api/admission/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog"
)

// newPodManager creates a Manager for k8s native Pod API.
func newPodManager(k8sClient kubernetes.Interface) Manager {
	return &podManager{k8sClient: k8sClient}
}

// podManager is a Manager for k8s native Pod API.
type podManager struct {
	k8sClient kubernetes.Interface
}

// Start starts the manager.
func (m *podManager) Start(stopCh <-chan struct{}) error {
	return nil
}

// Handle handles a workload admission request.
func (m *podManager) Handle(
	request *admissionv1beta1.AdmissionRequest) (*Workload, []*VolumeInfo, []*VolumeInfo, error) {
	pod := &corev1.Pod{}
	if _, _, err := util.Codecs.UniversalDeserializer().Decode(request.Object.Raw, nil, pod); err != nil {
		return nil, nil, nil, fmt.Errorf("decode pod failed: %v", err)
	}

	for _, ref := range pod.OwnerReferences {
		if ref.Controller != nil && *ref.Controller {
			// This pod is created/managed by a controller(deployment, sts, etc.),
			// we only concern the First level object.
			return nil, nil, nil, newIgnoreError()
		}
	}

	var releasedVolumes []*VolumeInfo
	usedVolumes := extractVolumes(&pod.Spec)

	if request.Operation == admissionv1beta1.Update {
		if podCompleted(pod) {
			// Pod is already completed, just release the used volumes.
			usedVolumes = nil
			releasedVolumes = usedVolumes
		} else {
			oldPod := &corev1.Pod{}
			if _, _, err := util.Codecs.UniversalDeserializer().Decode(request.OldObject.Raw, nil, oldPod); err != nil {
				return nil, nil, nil, fmt.Errorf("decode old pod failed: %v", err)
			}
			releasedVolumes = filterVolumes(usedVolumes, &oldPod.Spec)
		}
	}

	ref := corev1.ObjectReference{APIVersion: "v1", Kind: "Pod", Name: pod.Name, Namespace: pod.Namespace, UID: pod.UID}
	klog.V(4).Infof("Processed Pod: %+v", ref)

	return &Workload{ObjectReference: ref, Replicas: int32Ptr(1)}, usedVolumes, releasedVolumes, nil
}

// MountedVolumes returns mounted volumes by a workload.
func (m *podManager) MountedVolumes(ref *corev1.ObjectReference) ([]*VolumeInfo, error) {
	// NOTE: Get the pod from Apiserver directly. We only concern independent pods not created
	// by any controllers, and suppose there aren't many pods of this type. The benefit is that
	// we needn't to cache all pods in the informer, this may take up a lot of memory.
	// If this becomes a performance bottleneck maybe we still need to use informer instead.
	pod, err := m.k8sClient.CoreV1().Pods(ref.Namespace).Get(ref.Name, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			klog.V(4).Infof("Pod %s/%s not exist", ref.Namespace, ref.Name)
			return nil, nil
		}
		return nil, err
	}
	if podCompleted(pod) {
		klog.V(4).Infof("Pod %s/%s is already completed", ref.Namespace, ref.Name)
		return nil, nil
	}

	return extractVolumes(&pod.Spec), nil
}

// Exist returns true is a workload exist.
func (m *podManager) Exist(ref *corev1.ObjectReference) (bool, error) {
	_, err := m.k8sClient.CoreV1().Pods(ref.Namespace).Get(ref.Name, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			klog.V(4).Infof("Pod %s not exist", ref.String())
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// podCompleted returns true if pod completed.
func podCompleted(pod *corev1.Pod) bool {
	return pod.Status.Phase == corev1.PodFailed ||
		pod.Status.Phase == corev1.PodSucceeded ||
		(pod.DeletionTimestamp != nil && podNotRunning(pod.Status.ContainerStatuses))
}

// podNotRunning returns true if pod was still running.
func podNotRunning(statuses []corev1.ContainerStatus) bool {
	for _, status := range statuses {
		if status.State.Terminated == nil && status.State.Waiting == nil {
			return false
		}
	}
	return true
}
