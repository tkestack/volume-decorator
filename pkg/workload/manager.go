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

	admissionv1beta1 "k8s.io/api/admission/v1beta1"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"tkestack.io/tapp/pkg/apis/tappcontroller"
	tappv1 "tkestack.io/tapp/pkg/apis/tappcontroller/v1"
)

// Manager is used to manage workloads, such as Pod, Deployments, etc.
type Manager interface {
	// Start starts the manager.
	Start(stopCh <-chan struct{}) error
	// Handle handles a workload admission request.
	Handle(request *admissionv1beta1.AdmissionRequest) (w *Workload, used, released []*VolumeInfo, err error)
	// MountedVolumes returns mounted volumes by a workload.
	MountedVolumes(ref *corev1.ObjectReference) ([]*VolumeInfo, error)
	// Exist returns true is a workload exist.
	Exist(ref *corev1.ObjectReference) (bool, error)
}

// New creates a new Manager.
func New(
	k8sClient kubernetes.Interface,
	informerFactory informers.SharedInformerFactory,
	tappManager tapps.Manager) Manager {
	podGVK := metav1.GroupVersionKind{
		Group:   corev1.GroupName,
		Version: corev1.SchemeGroupVersion.Version,
		Kind:    "Pod",
	}
	deploymentGVK := metav1.GroupVersionKind{
		Group:   appsv1.GroupName,
		Version: appsv1.SchemeGroupVersion.Version,
		Kind:    "Deployment",
	}
	replicaSetGVK := metav1.GroupVersionKind{
		Group:   appsv1.GroupName,
		Version: appsv1.SchemeGroupVersion.Version,
		Kind:    "ReplicaSet",
	}
	statefulSetGVK := metav1.GroupVersionKind{
		Group:   appsv1.GroupName,
		Version: appsv1.SchemeGroupVersion.Version,
		Kind:    "StatefulSet",
	}
	daemonSetGVK := metav1.GroupVersionKind{
		Group:   appsv1.GroupName,
		Version: appsv1.SchemeGroupVersion.Version,
		Kind:    "DaemonSet",
	}
	jobGVK := metav1.GroupVersionKind{
		Group:   batchv1.GroupName,
		Version: batchv1.SchemeGroupVersion.Version,
		Kind:    "Job",
	}
	tappGVK := metav1.GroupVersionKind{
		Group:   tappcontroller.GroupName,
		Version: tappv1.SchemeGroupVersion.Version,
		Kind:    "TApp",
	}

	manager := &compositeManager{
		managers: map[metav1.GroupVersionKind]Manager{
			podGVK:         newPodManager(k8sClient),
			deploymentGVK:  newDeploymentManager(informerFactory),
			replicaSetGVK:  newReplicaSetManager(informerFactory),
			statefulSetGVK: newStatefulSetManager(informerFactory),
			daemonSetGVK:   newDaemonSetManager(informerFactory),
			jobGVK:         newJobManager(informerFactory),
		},
	}

	if tappManager.Support() {
		manager.managers[tappGVK] = newTappManager(tappManager)
	}

	return manager
}

// compositeManager is an implementation of Manager which consists of a set of Managers.
type compositeManager struct {
	managers map[metav1.GroupVersionKind]Manager
}

// Start starts the manager.
func (m *compositeManager) Start(stopCh <-chan struct{}) error {
	for _, manager := range m.managers {
		if err := manager.Start(stopCh); err != nil {
			return err
		}
	}
	return nil
}

// Handle handles a workload admission request.
func (m *compositeManager) Handle(
	request *admissionv1beta1.AdmissionRequest) (*Workload, []*VolumeInfo, []*VolumeInfo, error) {
	manager, err := m.getManager(request.Kind)
	if err != nil {
		return nil, nil, nil, err
	}
	return manager.Handle(request)
}

// MountedVolumes returns mounted volumes by a workload.
func (m *compositeManager) MountedVolumes(ref *corev1.ObjectReference) ([]*VolumeInfo, error) {
	manager, err := m.getManager(objRefToGVK(ref))
	if err != nil {
		return nil, err
	}
	return manager.MountedVolumes(ref)
}

// Exist returns true is a workload exist.
func (m *compositeManager) Exist(ref *corev1.ObjectReference) (bool, error) {
	manager, err := m.getManager(objRefToGVK(ref))
	if err != nil {
		return false, err
	}
	return manager.Exist(ref)
}

// getManager returns according Manager for a specific gvk.
func (m *compositeManager) getManager(gvk metav1.GroupVersionKind) (Manager, error) {
	manager, exist := m.managers[gvk]
	if !exist {
		return nil, fmt.Errorf("no available admitor for %s", gvk.String())
	}
	return manager, nil
}

// VolumeInfo is the information of a volume used by a workload.
type VolumeInfo = corev1.PersistentVolumeClaimVolumeSource

// Workload is the information of a workload used some volumes.
type Workload struct {
	corev1.ObjectReference
	Replicas *int32
}

// newIgnoreError returns an error which can be ignored by invokers.
func newIgnoreError() error {
	return ignoreError{}
}

// ignoreError is an error
type ignoreError struct{}

// Error returns the error massage.
func (ignoreError) Error() string {
	return "Irrelevant workload"
}

// IsIgnore returns true if err was an ignoreError.
func IsIgnore(err error) bool {
	_, ok := err.(ignoreError)
	return ok
}
