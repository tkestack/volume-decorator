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

package v1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PersistentVolumeClaimStatus is the status of a PVC/PV.
type PersistentVolumeClaimStatus string

const (
	// ClaimStatusUnknown indicates cannot determine volume's status.
	ClaimStatusUnknown PersistentVolumeClaimStatus = "Unknown"
	// ClaimStatusCreating indicates the PV object is still creating.
	ClaimStatusCreating PersistentVolumeClaimStatus = "Creating"
	// ClaimStatusExpanding indicates the PVC is expanding.
	ClaimStatusExpanding PersistentVolumeClaimStatus = "Expanding"
	// ClaimStatusAvailable indicates the PVC is created and can be used by any workloads.
	ClaimStatusAvailable PersistentVolumeClaimStatus = "Available"
	// ClaimStatusInUse indicates the PVC is used by some workloads.
	ClaimStatusInUse PersistentVolumeClaimStatus = "InUse"
	// ClaimStatusLost indicates the PV is missed.
	ClaimStatusLost PersistentVolumeClaimStatus = "Lost"
	// ClaimStatusDeleting indicates the PVC is deleting.
	ClaimStatusDeleting PersistentVolumeClaimStatus = "Deleting"
	// TODO: Add explorer related status.
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// PersistentVolumeClaimRuntime is the runtime information of a PVC/PV.
type PersistentVolumeClaimRuntime struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec PersistentVolumeClaimRuntimeSpec
}

// PersistentVolumeClaimRuntimeSpec is the spec for a PersistentVolumeClaimRuntime resource.
type PersistentVolumeClaimRuntimeSpec struct {
	// Current Statuses of PersistentVolumeClaim.
	// PersistentVolumeClaim may have more than one status at a moment.
	// For example, an InUse volume maybe also in Expanding status.
	Statuses []PersistentVolumeClaimStatus `json:"status"`
	// Workloads mounted by.
	// +optional
	Workloads []Workload `json:"workloads"`
	// Current usage in bytes.
	// +optional
	UsageBytes int64 `json:"usageBytes"`
	// Nodes which mount this volume.
	// +optional
	MountedNodes []string `json:"mountedNodes"`

	//TODO: Add user related information.
}

// Workload is the information of workloads used some volumes.
type Workload struct {
	corev1.ObjectReference `json:",inline"`

	// The volume is used by this workload as read only.
	ReadOnly bool `json:"readOnly"`
	// Replicas of this workload. Will be nil if we can't
	// determine the replicas, for example: DaemonSet.
	Replicas *int32 `json:"replicas"`
	// Timestamp when the workload added.
	Timestamp *metav1.Time `json:"timestamp"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// PersistentVolumeClaimRuntimeList is a list of PersistentVolumeClaimRuntime.
type PersistentVolumeClaimRuntimeList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []PersistentVolumeClaimRuntime `json:"items"`
}
