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

	"tkestack.io/volume-decorator/pkg/tapps"
	"tkestack.io/volume-decorator/pkg/util"

	admissionv1beta1 "k8s.io/api/admission/v1beta1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog"
	tappv1 "tkestack.io/tapp/pkg/apis/tappcontroller/v1"
)

var completedTappStatues = map[tappv1.AppStatus]bool{
	tappv1.AppSucc:   true,
	tappv1.AppFailed: true,
	tappv1.AppKilled: true,
}

// newTappManager creates a Manager for tke Tapp API.
func newTappManager(manager tapps.Manager) Manager {
	return &tappManager{manager: manager}
}

// tappManager is a Manager for tke Tapp API.
type tappManager struct {
	manager tapps.Manager
}

// Start starts the manager.
func (m *tappManager) Start(stopCh <-chan struct{}) error {
	return nil
}

// Handle handles a workload admission request.
func (m *tappManager) Handle(
	request *admissionv1beta1.AdmissionRequest) (w *Workload, used, released []*VolumeInfo, err error) {
	tapp := &tappv1.TApp{}
	if _, _, err := util.Codecs.UniversalDeserializer().Decode(request.Object.Raw, nil, tapp); err != nil {
		return nil, nil, nil, fmt.Errorf("decode tapp failed: %v", err)
	}

	var usedVolumes, releasedVolumes []*VolumeInfo

	for _, spec := range extractTappPodSpecs(tapp) {
		usedVolumes = append(usedVolumes, extractVolumes(spec)...)
	}

	if request.Operation == admissionv1beta1.Update {
		if tappCompleted(tapp) {
			// Tapp is already completed, just release the used volumes.
			usedVolumes = nil
			releasedVolumes = usedVolumes
		} else {
			oldTapp := &tappv1.TApp{}
			if _, _, err := util.Codecs.UniversalDeserializer().Decode(request.OldObject.Raw, nil, oldTapp); err != nil {
				return nil, nil, nil, fmt.Errorf("decode old tapp failed: %v", err)
			}
			releasedVolumes = filterVolumes(usedVolumes, extractTappPodSpecs(tapp)...)
		}
	}

	ref := corev1.ObjectReference{
		APIVersion: "tke.cloud.tencent.com/v1",
		Kind:       "TApp",
		Name:       tapp.Name,
		Namespace:  tapp.Namespace,
		UID:        tapp.UID,
	}
	klog.V(4).Infof("Processed Tapp: %+v", ref)

	return &Workload{ObjectReference: ref, Replicas: int32Ptr(1)}, usedVolumes, releasedVolumes, nil
}

// MountedVolumes returns mounted volumes by a workload.
func (m *tappManager) MountedVolumes(ref *corev1.ObjectReference) ([]*VolumeInfo, error) {
	tapp, err := m.manager.Get(ref.Namespace, ref.Name)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			klog.Infof("Tapp %s/%s not found", ref.Namespace, ref.Name)
			return nil, nil
		}
		return nil, fmt.Errorf("get tapp failed: %v", err)
	}
	var usedVolumes []*VolumeInfo
	for _, spec := range extractTappPodSpecs(tapp) {
		usedVolumes = append(usedVolumes, extractVolumes(spec)...)
	}
	return usedVolumes, nil
}

// Exist returns true is a workload exist.
func (m *tappManager) Exist(ref *corev1.ObjectReference) (bool, error) {
	_, err := m.manager.Get(ref.Namespace, ref.Name)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// extractTappPodSpecs extracts pod spec from a Tapp object.
func extractTappPodSpecs(tapp *tappv1.TApp) []*corev1.PodSpec {
	var specs []*corev1.PodSpec
	names := sets.NewString()
	for _, hash := range tapp.Spec.Templates {
		names.Insert(hash)
	}
	for _, name := range names.List() {
		template := tapp.Spec.TemplatePool[name]
		specs = append(specs, &template.Spec)
	}
	if int32(len(tapp.Spec.Templates)) < tapp.Spec.Replicas {
		/// this means there are some instances use the default template.
		specs = append(specs, &tapp.Spec.Template.Spec)
	}
	return specs
}

// tappCompleted returns true if a tapp was completed.
func tappCompleted(tapp *tappv1.TApp) bool {
	return completedTappStatues[tapp.Status.AppStatus]
}
