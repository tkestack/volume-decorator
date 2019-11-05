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

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog"
)

type updater func(
	pvcr *storagev1alpha1.PersistentVolumeClaimRuntime) (*storagev1alpha1.PersistentVolumeClaimRuntime, error)

// newController creates a controller.
func newController(
	name string,
	updater updater,
	syncInterval time.Duration,
	pvcrClient clientset.Interface,
	pvcLister corelisters.PersistentVolumeClaimLister,
	pvcrLister pvcrlisters.PersistentVolumeClaimRuntimeLister) *controller {
	queue := workqueue.NewNamedRateLimitingQueue(
		workqueue.DefaultControllerRateLimiter(), "workload_recycler")
	return &controller{
		name:         name,
		updater:      updater,
		syncInterval: syncInterval,

		pvcLister:  pvcLister,
		pvcrClient: pvcrClient,
		pvcrLister: pvcrLister,

		queue: queue,
	}
}

// controller is common framework.
type controller struct {
	name         string
	updater      updater
	syncInterval time.Duration

	pvcrClient clientset.Interface
	pvcLister  corelisters.PersistentVolumeClaimLister
	pvcrLister pvcrlisters.PersistentVolumeClaimRuntimeLister

	queue workqueue.RateLimitingInterface
}

// Run starts the controller.
func (c *controller) Run(workers int, stopCh <-chan struct{}) {
	go wait.Until(c.resync, c.syncInterval, stopCh)

	for i := 0; i < workers; i++ {
		go wait.Until(c.syncPVCs, 0, stopCh)
	}
	klog.Infof("%s started", c.name)
}

// resync list all PVCs and put into the queue.
func (c *controller) resync() {
	pvcs, err := c.pvcLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("List PVC runtime failed: %v", err)
		return
	}
	for _, pvc := range pvcs {
		key, err := cache.MetaNamespaceKeyFunc(pvc)
		if err != nil {
			klog.Errorf("Generate key of PVC %s/%s failed: %v", pvc.Namespace, pvc.Name, key)
			continue
		}
		c.queue.Add(key)
	}
}

// syncPVCs syncs all PVC objects.
func (c *controller) syncPVCs() {
	key, quit := c.queue.Get()
	if quit {
		return
	}
	defer c.queue.Done(key)

	if err := c.syncPVC(key.(string)); err != nil {
		// Put PVC back to the queue so that we can retry later.
		c.queue.AddRateLimited(key)
	} else {
		c.queue.Forget(key)
	}
}

// syncPVC syncs a PVC object.
func (c *controller) syncPVC(key string) error {
	klog.V(4).Infof("%s start to process PVC: %s", c.name, key)

	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		klog.Errorf("Split meta namespace key of pvc %s failed: %v", key, err)
		return err
	}

	pvcr, err := c.pvcrLister.PersistentVolumeClaimRuntimes(namespace).Get(name)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Errorf("Get PVC runtime error: %+v", err)
		return err
	}

	newPVCR, err := c.updater(pvcr)
	if err != nil {
		return err
	}
	if newPVCR == nil {
		return nil
	}

	_, err = c.pvcrClient.StorageV1().PersistentVolumeClaimRuntimes(pvcr.Namespace).Update(newPVCR)
	if err != nil {
		klog.Errorf("%s Update PVC runtime %s failed: %v", c.name, key, err)
	}
	return err
}

// updatePVCStatus updates a PVC's status.
func updatePVCStatus(pvcr *storagev1alpha1.PersistentVolumeClaimRuntime) {
	if len(pvcr.Spec.Workloads) == 0 && len(pvcr.Spec.MountedNodes) == 0 {
		pvcr.Spec.Statuses = replacePVCStatus(pvcr.Spec.Statuses,
			storagev1alpha1.ClaimStatusInUse, storagev1alpha1.ClaimStatusAvailable)
	}
}

// replacePVCStatus replace a PVC's status.
func replacePVCStatus(
	statuses []storagev1alpha1.PersistentVolumeClaimStatus,
	oldStatus, newStatus storagev1alpha1.PersistentVolumeClaimStatus) []storagev1alpha1.PersistentVolumeClaimStatus {
	newStatusExist := false
	result := make([]storagev1alpha1.PersistentVolumeClaimStatus, 0, len(statuses))
	for _, status := range statuses {
		if status == oldStatus {
			continue
		}
		result = append(result, status)
		if status == newStatus {
			newStatusExist = true
		}
	}
	if !newStatusExist {
		result = append(result, newStatus)
	}
	return result
}

// arrayEqual returns true if two arrays are equal.
func arrayEqual(a1, a2 []string) bool {
	if len(a1) != len(a2) {
		return false
	}
	set := sets.NewString(a1...)
	for _, e := range a2 {
		if !set.Has(e) {
			return false
		}
	}
	return true
}
