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
	storagev1alpha1 "tkestack.io/volume-decorator/pkg/apis/storage/v1"

	corev1 "k8s.io/api/core/v1"
)

// newCBSVolume creates a cbsVolume.
func newCBSVolume() volume {
	return &cbsVolume{}
}

// cbsVolume is a wrapper for TencentCloud CBS storage.
type cbsVolume struct {
}

// Start starts the manager.
func (v *cbsVolume) Start(stopCh <-chan struct{}) error {
	return nil
}

// Status returns the getPVCStatus of a PVC/PV.
func (v *cbsVolume) Available(w *storagev1alpha1.Workload, pvcr *storagev1alpha1.PersistentVolumeClaimRuntime) error {
	return blockVolumeAvailable(w, pvcr)
}

// MountedNodes returns the node list this volume mounted on.
func (v *cbsVolume) MountedNodes(pv *corev1.PersistentVolume) ([]string, error) {
	// TODO: Get information from Tencent Cloud API?
	return nil, nil
}

// Usage returns the real usage of volume in byte.
func (v *cbsVolume) Usage(pv *corev1.PersistentVolume) (int64, error) {
	// TODO
	return 0, nil
}
