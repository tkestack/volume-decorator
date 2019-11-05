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
	"time"

	storagev1alpha1 "tkestack.io/volume-decorator/pkg/apis/storage/v1"
	clientset "tkestack.io/volume-decorator/pkg/generated/clientset/versioned"
	pvcrlisters "tkestack.io/volume-decorator/pkg/generated/listers/storage/v1"
	"tkestack.io/volume-decorator/pkg/volume"

	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/klog"
)

const usageSyncInterval = time.Minute

// newUsageCollector creates a usageCollector.
func newUsageCollector(
	volumeManager volume.Manager,
	pvcrClient clientset.Interface,
	pvcLister corelisters.PersistentVolumeClaimLister,
	pvcrLister pvcrlisters.PersistentVolumeClaimRuntimeLister) *usageCollector {
	c := &usageCollector{volumeManager: volumeManager}
	c.controller = newController("usage-collector", c.update, usageSyncInterval, pvcrClient, pvcLister, pvcrLister)
	return c
}

// usageCollector is a collector to collect a volume's real usage.
type usageCollector struct {
	*controller
	volumeManager volume.Manager
}

// update collects and updates a volume's real usage.
func (c *usageCollector) update(
	pvcr *storagev1alpha1.PersistentVolumeClaimRuntime) (*storagev1alpha1.PersistentVolumeClaimRuntime, error) {
	usage, err := c.volumeManager.Usage(pvcr.Namespace, pvcr.Name)
	if err != nil {
		klog.Errorf("Check real usage for PVC %s/%s failed: %v", pvcr.Namespace, pvcr.Name, err)
		return nil, err
	}
	if usage == pvcr.Spec.UsageBytes {
		return nil, nil
	}
	klog.Infof("Usage bytes of PVC %s/%s changed: %v -> %v", pvcr.Namespace, pvcr.Name, pvcr.Spec.UsageBytes, usage)

	newPVCR := pvcr.DeepCopy()
	newPVCR.Spec.UsageBytes = usage

	return newPVCR, nil
}
