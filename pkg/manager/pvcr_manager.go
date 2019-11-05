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
	storagev1alpha1 "tkestack.io/volume-decorator/pkg/apis/storage/v1"
	clientset "tkestack.io/volume-decorator/pkg/generated/clientset/versioned"
	pvcrlisters "tkestack.io/volume-decorator/pkg/generated/listers/storage/v1"
	"tkestack.io/volume-decorator/pkg/volume"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	coreinformers "k8s.io/client-go/informers/core/v1"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog"
)

// pvcrManager is responsible for creating PVC runtime and update PVC/PV's status.
type pvcrManager struct {
	volumeManager volume.Manager
	pvcLister     corelisters.PersistentVolumeClaimLister
	pvcrClient    clientset.Interface
	pvcrLister    pvcrlisters.PersistentVolumeClaimRuntimeLister

	queue workqueue.RateLimitingInterface
}

// newPVCRManager creates a pvcrManager.
func newPVCRManager(
	volumeManager volume.Manager,
	pvcLister corelisters.PersistentVolumeClaimLister,
	pvcrClient clientset.Interface,
	pvcrLister pvcrlisters.PersistentVolumeClaimRuntimeLister,
	pvcInformer coreinformers.PersistentVolumeClaimInformer) *pvcrManager {
	queue := workqueue.NewNamedRateLimitingQueue(
		workqueue.DefaultControllerRateLimiter(), "status_updater")
	u := &pvcrManager{
		volumeManager: volumeManager,
		pvcLister:     pvcLister,
		pvcrClient:    pvcrClient,
		pvcrLister:    pvcrLister,

		queue: queue,
	}
	pvcInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    u.pvcAdd,
		UpdateFunc: u.pvcUpdate,
	})

	return u
}

// Run starts the pvcrManager.
func (u *pvcrManager) Run(workers int, stopCh <-chan struct{}) {
	for i := 0; i < workers; i++ {
		go wait.Until(u.syncPVCs, 0, stopCh)
	}
	klog.Infof("PVC runtime manager started")
}

// pvcAdd adds a PVCR.
func (u *pvcrManager) pvcAdd(obj interface{}) {
	objKey, err := getPVCKey(obj)
	if err != nil {
		return
	}
	u.queue.Add(objKey)
}

// pvcUpdate updates a PVCR.
func (u *pvcrManager) pvcUpdate(oldObj, newObj interface{}) {
	u.pvcAdd(newObj)
}

// syncPVCs sync the PVC and PVCR objects.
func (u *pvcrManager) syncPVCs() {
	key, quit := u.queue.Get()
	if quit {
		return
	}
	defer u.queue.Done(key)

	if err := u.syncPVC(key.(string)); err != nil {
		// Put PVC back to the queue so that we can retry later.
		u.queue.AddRateLimited(key)
	} else {
		u.queue.Forget(key)
	}
}

// syncPVCs sync a PVC and PVCR object.
func (u *pvcrManager) syncPVC(key string) error {
	klog.V(4).Infof("Started PVC processing %q", key)

	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		klog.Errorf("Split meta namespace key of pvc %s failed: %v", key, err)
		return err
	}

	pvc, err := u.pvcLister.PersistentVolumeClaims(namespace).Get(name)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			klog.Infof("PVC %s deleted, skip it", key)
			return nil
		}
		klog.Errorf("Get PVC %s failed: %v", key, err)
		return err
	}

	pvcr, err := u.pvcrLister.PersistentVolumeClaimRuntimes(namespace).Get(name)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return u.createPVCR(pvc)
		}
		klog.Errorf("Get PVC runtime %s failed: %v", key, err)
		return err
	}

	return u.updatePVCR(pvc, pvcr)
}

// createPVCR creates a PVCR.
func (u *pvcrManager) createPVCR(pvc *corev1.PersistentVolumeClaim) error {
	statuses, err := u.volumeManager.Status(pvc.Namespace, pvc.Name)
	if err != nil {
		klog.Errorf("Get status of PVC %s/%s failed: %v", pvc.Namespace, pvc.Name, err)
		return err
	}
	pvcr := &storagev1alpha1.PersistentVolumeClaimRuntime{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pvc.Name,
			Namespace: pvc.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "v1",
					Kind:       "PersistentVolumeClaim",
					Name:       pvc.Name,
					UID:        pvc.UID,
				},
			},
		},
		Spec: storagev1alpha1.PersistentVolumeClaimRuntimeSpec{
			Statuses: statuses,
		},
	}
	_, err = u.pvcrClient.StorageV1().PersistentVolumeClaimRuntimes(pvcr.Namespace).Create(pvcr)
	if err != nil {
		klog.Errorf("Create PVC runtime %s/%s failed: %v", pvcr.Namespace, pvcr.Name, err)
	}
	return err
}

// updatePVCR updates a PVCR.
func (u *pvcrManager) updatePVCR(
	pvc *corev1.PersistentVolumeClaim,
	pvcr *storagev1alpha1.PersistentVolumeClaimRuntime) error {
	statuses, err := u.volumeManager.Status(pvc.Namespace, pvc.Name)
	if err != nil {
		klog.Errorf("Get status of PVC %s/%s failed: %v", pvc.Namespace, pvc.Name, err)
		return err
	}

	newPVCR := pvcr.DeepCopy()
	newPVCR.Spec.Statuses = statuses
	_, err = u.pvcrClient.StorageV1().PersistentVolumeClaimRuntimes(pvcr.Namespace).Update(newPVCR)
	if err != nil {
		klog.Errorf("Update PVC runtime %s/%s failed: %v", pvcr.Namespace, pvcr.Name, err)
	}
	return err
}

// getPVCKey generates a unique key for a PVC object.
func getPVCKey(obj interface{}) (string, error) {
	if unknown, ok := obj.(cache.DeletedFinalStateUnknown); ok && unknown.Obj != nil {
		obj = unknown.Obj
	}
	objKey, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	if err != nil {
		klog.Errorf("Failed to get key from object: %v", err)
		return "", err
	}
	return objKey, nil
}
