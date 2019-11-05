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
	"tkestack.io/volume-decorator/pkg/workload"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog"
)

const (
	workloadCheckDelay    = time.Second * 10
	workloadCheckInterval = time.Second * 10
)

// newWorkloadRecycler creates a workloadRecycler.
func newWorkloadRecycler(
	workloadManager workload.Manager,
	pvcrClient clientset.Interface,
	pvcrLister pvcrlisters.PersistentVolumeClaimRuntimeLister) *workloadRecycler {
	queue := workqueue.NewNamedRateLimitingQueue(
		workqueue.DefaultControllerRateLimiter(), "workload_recycler")
	return &workloadRecycler{
		workloadManager: workloadManager,
		pvcrClient:      pvcrClient,
		pvcrLister:      pvcrLister,

		queue: queue,
	}
}

// workloadRecycler is a manager to release volumes from a terminated workload.
type workloadRecycler struct {
	workloadManager workload.Manager
	pvcrClient      clientset.Interface
	pvcrLister      pvcrlisters.PersistentVolumeClaimRuntimeLister

	queue workqueue.RateLimitingInterface
}

// Run starts the workloadRecycler.
func (r *workloadRecycler) Run(workers int, stopCh <-chan struct{}) {
	go wait.Until(r.resync, workloadCheckInterval, stopCh)

	for i := 0; i < workers; i++ {
		go wait.Until(r.syncPVCRs, 0, stopCh)
	}
	klog.Infof("Workload recycler started")
}

// resync list all PVCRs and put into the queue.
func (r *workloadRecycler) resync() {
	pvcrs, err := r.pvcrLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("")
	}
	for _, pvcr := range pvcrs {
		key, err := cache.MetaNamespaceKeyFunc(pvcr)
		if err != nil {
			klog.Errorf("Generate key of PVC runtime %s/%s failed: %v", pvcr.Namespace, pvcr.Name, key)
			continue
		}
		r.queue.Add(key)
	}
}

// syncPVCRs updates all PVCRs
func (r *workloadRecycler) syncPVCRs() {
	key, quit := r.queue.Get()
	if quit {
		return
	}
	defer r.queue.Done(key)

	if err := r.syncPVCR(key.(string)); err != nil {
		// Put PVC back to the queue so that we can retry later.
		r.queue.AddRateLimited(key)
	} else {
		r.queue.Forget(key)
	}
}

// syncPVCR updates PVCR's Workloads.
func (r *workloadRecycler) syncPVCR(key string) error {
	klog.V(4).Infof("Start to process PVC runtime: %s", key)

	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		klog.Errorf("Split meta namespace key of pvc runtime %s failed: %v", key, err)
		return err
	}

	pvcr, err := r.pvcrLister.PersistentVolumeClaimRuntimes(namespace).Get(name)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Errorf("Get pvc runtime error: %+v", err)
		return err
	}

	workloads := make([]storagev1alpha1.Workload, 0, len(pvcr.Spec.Workloads))
	for _, w := range pvcr.Spec.Workloads {
		exist, existErr := r.workloadManager.Exist(&w.ObjectReference)
		if existErr != nil {
			klog.Errorf("Can't determine workload %+v exist or not of PVC %s: %v",
				w.ObjectReference, key, existErr)
			// Assume this workload is still exist.
			exist = true
		}
		// If the workload iis just created,, it maybe not exist in the cache.
		// So we use a delay to make sure the workload is indeed deleted.
		if exist || w.Timestamp.Time.Add(workloadCheckDelay).After(time.Now()) {
			workloads = append(workloads, w)
		}
	}

	if len(workloads) == len(pvcr.Spec.Workloads) {
		return nil
	}
	klog.Infof("Mounted workloads of PVC %s changed: %v -> %v", key, pvcr.Spec.Workloads, workloads)

	newPVCR := pvcr.DeepCopy()
	newPVCR.Spec.Workloads = workloads
	updatePVCStatus(newPVCR)
	_, err = r.pvcrClient.StorageV1().PersistentVolumeClaimRuntimes(pvcr.Namespace).Update(newPVCR)
	if err != nil {
		klog.Errorf("Update workloads of PVC runtime %s failed: %v", key, err)
	}
	return err
}
