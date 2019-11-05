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
	"fmt"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	storagev1alpha1 "tkestack.io/volume-decorator/pkg/apis/storage/v1"
)

// volume provides a unified view for different volume types.
type volume interface {
	// Start starts the volume.
	Start(stopCh <-chan struct{}) error
	// Available returns true if the volume can be mounted by a workload.
	Available(w *storagev1alpha1.Workload, pvcr *storagev1alpha1.PersistentVolumeClaimRuntime) error
	// MountedNodes returns the workloads mounted the volume.
	MountedNodes(pv *corev1.PersistentVolume) ([]string, error)
	// Usage returns current usage of the volume.
	Usage(pv *corev1.PersistentVolume) (int64, error)
}

// blockVolumeAvailable returns true if a block storage is available.
func blockVolumeAvailable(
	workload *storagev1alpha1.Workload,
	pvcr *storagev1alpha1.PersistentVolumeClaimRuntime) error {
	if workload.ReadOnly {
		return nil
	}
	if workload.Replicas != nil && *workload.Replicas > 1 {
		return k8serrors.NewBadRequest(
			fmt.Sprintf("CephRBD volume cannot be mounted as ReadWrite mode by workloads with %d replicas",
				*workload.Replicas))
	}
	for _, w := range pvcr.Spec.Workloads {
		if !w.ReadOnly {
			return k8serrors.NewBadRequest(
				"CephRBD volume cannot be mounted as ReadWrite mode by more than one workload")
		}
	}
	return nil
}
