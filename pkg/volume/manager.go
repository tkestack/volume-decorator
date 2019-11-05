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

package volume

import (
	"errors"
	"fmt"
	"strings"

	storagev1alpha1 "tkestack.io/volume-decorator/pkg/apis/storage/v1"
	"tkestack.io/volume-decorator/pkg/config"
	clientset "tkestack.io/volume-decorator/pkg/generated/clientset/versioned"
	pvcrlisters "tkestack.io/volume-decorator/pkg/generated/listers/storage/v1"
	"tkestack.io/volume-decorator/pkg/types"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/klog"
)

var resizeConditions = map[corev1.PersistentVolumeClaimConditionType]bool{
	corev1.PersistentVolumeClaimResizing:                true,
	corev1.PersistentVolumeClaimFileSystemResizePending: true,
}

// Manager manages volumes.
type Manager interface {
	// Start starts the manager.
	Start(stopCh <-chan struct{}) error
	// Status returns the getPVCStatus of a PVC/PV.
	Status(namespace, name string) ([]storagev1alpha1.PersistentVolumeClaimStatus, error)
	// Attach attaches a volume to a workload.
	Attach(w *storagev1alpha1.Workload, namespace, name string) error
	// MountedNodes returns the node list this volume mounted on.
	MountedNodes(namespace, name string) ([]string, error)
	// Usage returns the real usage of volume in byte.
	Usage(namespace, name string) (int64, error)
}

// New creates a new manager.
func New(
	config *config.VolumeConfig,
	pvcrClient clientset.Interface,
	pvLister corelisters.PersistentVolumeLister,
	pvcLister corelisters.PersistentVolumeClaimLister,
	pvcrLister pvcrlisters.PersistentVolumeClaimRuntimeLister) Manager {
	volumes := make(map[types.VolumeType]volume)
	for _, typ := range strings.Split(config.Types, ",") {
		switch typ {
		case types.CephFS:
			volumes[types.CephFS] = newCephFSVolume(config)
		case types.CephRBD:
			volumes[types.CephRBD] = newCephRBDVolume(config)
		case types.TencentCBS:
			volumes[types.TencentCBS] = newCBSVolume()
		}
	}

	return &manager{
		pvcrClient: pvcrClient,
		pvLister:   pvLister,
		pvcLister:  pvcLister,
		pvcrLister: pvcrLister,

		volumes: volumes,
	}
}

// manager is a common framework implements Manager.
type manager struct {
	pvcrClient clientset.Interface
	pvLister   corelisters.PersistentVolumeLister
	pvcLister  corelisters.PersistentVolumeClaimLister
	pvcrLister pvcrlisters.PersistentVolumeClaimRuntimeLister

	volumes map[types.VolumeType]volume
}

// Start starts the manager.
func (m *manager) Start(stopCh <-chan struct{}) error {
	for _, volume := range m.volumes {
		if err := volume.Start(stopCh); err != nil {
			return err
		}
	}
	return nil
}

// Status returns the getPVCStatus of a PVC/PV.
func (m *manager) Status(namespace, name string) ([]storagev1alpha1.PersistentVolumeClaimStatus, error) {
	pvc, err := m.pvcLister.PersistentVolumeClaims(namespace).Get(name)
	if err != nil {
		return nil, err
	}
	pv, err := m.getPV(pvc.Spec.VolumeName)
	if err != nil {
		return nil, err
	}
	pvcr, err := m.pvcrLister.PersistentVolumeClaimRuntimes(namespace).Get(name)
	if err != nil && !k8serrors.IsNotFound(err) {
		return nil, err
	}
	return getPVCStatus(pvc, pv, pvcr)
}

// Attach attaches a volume to a workload.
func (m *manager) Attach(w *storagev1alpha1.Workload, namespace, name string) error {
	klog.V(4).Infof("Try to attach volume %s/%s to workload %+v",
		namespace, name, w)

	pvc, pv, vol, err := m.getVolume(namespace, name)
	if err != nil {
		return err
	}
	pvcr, err := m.pvcrLister.PersistentVolumeClaimRuntimes(namespace).Get(name)
	if err != nil {
		return err
	}

	for i := range pvcr.Spec.Workloads {
		if sameWorkload(w, &pvcr.Spec.Workloads[i]) {
			return nil
		}
	}

	if err = vol.Available(w, pvcr); err != nil {
		return err
	}

	newPVCR := pvcr.DeepCopy()
	newPVCR.Spec.Workloads = append(newPVCR.Spec.Workloads, *w)
	statuses, err := getPVCStatus(pvc, pv, newPVCR)
	if err != nil {
		return err
	}
	newPVCR.Spec.Statuses = statuses

	_, err = m.pvcrClient.StorageV1().PersistentVolumeClaimRuntimes(newPVCR.Namespace).Update(newPVCR)
	return err
}

// MountedNodes returns the node list this volume mounted on.
func (m *manager) MountedNodes(namespace, name string) ([]string, error) {
	_, pv, vol, err := m.getVolume(namespace, name)
	if err != nil {
		return nil, err
	}
	return vol.MountedNodes(pv)
}

// Usage returns the real usage of volume in byte.
func (m *manager) Usage(namespace, name string) (int64, error) {
	_, pv, vol, err := m.getVolume(namespace, name)
	if err != nil {
		return 0, err
	}
	return vol.Usage(pv)
}

// getVolume returns detail information of a volume.
func (m *manager) getVolume(
	namespace, name string) (*corev1.PersistentVolumeClaim, *corev1.PersistentVolume, volume, error) {
	pvc, err := m.pvcLister.PersistentVolumeClaims(namespace).Get(name)
	if err != nil {
		return nil, nil, nil, err
	}
	pv, err := m.getPV(pvc.Spec.VolumeName)
	if err != nil {
		return nil, nil, nil, err
	}
	if pv == nil {
		return nil, nil, nil, errors.New("volume is still creating")
	}
	if pv.Spec.CSI == nil {
		return nil, nil, nil, k8serrors.NewBadRequest("not a CSI volume")
	}
	vol, exist := m.volumes[pv.Spec.CSI.Driver]
	if !exist {
		return nil, nil, nil, k8serrors.NewBadRequest(fmt.Sprintf("unsupported volume type: %s", pv.Spec.CSI.Driver))
	}
	return pvc, pv, vol, nil
}

// getPVCStatus returns the status of a volume.
func getPVCStatus(
	pvc *corev1.PersistentVolumeClaim,
	pv *corev1.PersistentVolume,
	pvcr *storagev1alpha1.PersistentVolumeClaimRuntime) ([]storagev1alpha1.PersistentVolumeClaimStatus, error) {
	if pvc.DeletionTimestamp != nil {
		return []storagev1alpha1.PersistentVolumeClaimStatus{storagev1alpha1.ClaimStatusDeleting}, nil
	}

	var statuses []storagev1alpha1.PersistentVolumeClaimStatus

	switch pvc.Status.Phase {
	case corev1.ClaimPending:
		return []storagev1alpha1.PersistentVolumeClaimStatus{storagev1alpha1.ClaimStatusCreating}, nil
	case corev1.ClaimLost:
		return []storagev1alpha1.PersistentVolumeClaimStatus{storagev1alpha1.ClaimStatusLost}, nil
	case corev1.ClaimBound:
		if pv == nil {
			return []storagev1alpha1.PersistentVolumeClaimStatus{storagev1alpha1.ClaimStatusLost}, nil
		}
		if pvcr != nil && len(pvcr.Spec.Workloads) > 0 {
			statuses = append(statuses, storagev1alpha1.ClaimStatusInUse)
		} else {
			statuses = append(statuses, storagev1alpha1.ClaimStatusAvailable)
		}
	}

	for _, condition := range pvc.Status.Conditions {
		if resizeConditions[condition.Type] && condition.Status == corev1.ConditionTrue {
			statuses = append(statuses, storagev1alpha1.ClaimStatusExpanding)
		}
	}

	return statuses, nil
}

// getPV returns a PV object by name.
func (m *manager) getPV(volumeName string) (*corev1.PersistentVolume, error) {
	if len(volumeName) == 0 {
		return nil, nil
	}
	pv, err := m.pvLister.Get(volumeName)
	if k8serrors.IsNotFound(err) {
		return nil, nil
	}
	return pv, err
}
