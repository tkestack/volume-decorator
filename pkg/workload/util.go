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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"strings"
)

// extractVolumes extracts mounted volume info from pod spec.
func extractVolumes(spec *corev1.PodSpec) []*VolumeInfo {
	var result []*VolumeInfo
	for _, volume := range spec.Volumes {
		if volume.PersistentVolumeClaim != nil {
			result = append(result, volume.PersistentVolumeClaim)
		}
	}
	return result
}

// filterVolumes extracts mounted volume info from pod spec without filtered volumes.
func filterVolumes(filterVolumes []*VolumeInfo, specs ...*corev1.PodSpec) []*VolumeInfo {
	var result []*VolumeInfo
	filterSet := sets.NewString()
	for _, volume := range filterVolumes {
		filterSet.Insert(volume.ClaimName)
	}
	for _, spec := range specs {
		for _, volume := range spec.Volumes {
			if volume.PersistentVolumeClaim != nil &&
				!filterSet.Has(volume.PersistentVolumeClaim.ClaimName) {
				result = append(result, volume.PersistentVolumeClaim)
			}
		}
	}
	return result
}

// objRefToGVK transfers ObjectReference to GroupVersionKind.
func objRefToGVK(ref *corev1.ObjectReference) metav1.GroupVersionKind {
	group, version := "", ref.APIVersion
	if strings.Contains(version, "/") {
		tags := strings.SplitN(version, "/", 2)
		group, version = tags[0], tags[1]
	}
	return metav1.GroupVersionKind{Group: group, Version: version, Kind: ref.Kind}
}

// int32Ptr generates a point value of int32.
func int32Ptr(v int32) *int32 {
	ptr := new(int32)
	*ptr = v
	return ptr
}
