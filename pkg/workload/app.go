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
	"reflect"

	"tkestack.io/volume-decorator/pkg/util"

	admissionv1beta1 "k8s.io/api/admission/v1beta1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog"
)

// Return a Manager for k8s native Deployment object.
func newDeploymentManager(informerFactory informers.SharedInformerFactory) Manager {
	informer := informerFactory.Apps().V1().Deployments()
	lister := informer.Lister()

	getter := func(namespace, name string) (runtime.Object, error) {
		return lister.Deployments(namespace).Get(name)
	}
	objCreator := func() runtime.Object {
		return &appsv1.Deployment{}
	}

	return &appManager{
		kind:       "Deployment",
		appGetter:  getter,
		objCreator: objCreator,
		appSynced:  informer.Informer().HasSynced,
	}
}

// Return a Manager for k8s native ReplicaSet object.
func newReplicaSetManager(informerFactory informers.SharedInformerFactory) Manager {
	informer := informerFactory.Apps().V1().ReplicaSets()

	getter := func(namespace, name string) (runtime.Object, error) {
		return informer.Lister().ReplicaSets(namespace).Get(name)
	}
	objCreator := func() runtime.Object {
		return &appsv1.ReplicaSet{}
	}

	return &appManager{
		kind:       "ReplicaSet",
		appGetter:  getter,
		objCreator: objCreator,
		appSynced:  informer.Informer().HasSynced,
	}
}

// Return a Manager for k8s native StatefulSet object.
func newStatefulSetManager(informerFactory informers.SharedInformerFactory) Manager {
	informer := informerFactory.Apps().V1().StatefulSets()

	getter := func(namespace, name string) (runtime.Object, error) {
		return informer.Lister().StatefulSets(namespace).Get(name)
	}
	objCreator := func() runtime.Object {
		return &appsv1.StatefulSet{}
	}

	return &appManager{
		kind:       "StatefulSet",
		appGetter:  getter,
		objCreator: objCreator,
		appSynced:  informer.Informer().HasSynced,
	}
}

// Return a Manager for k8s native DaemonSet object.
func newDaemonSetManager(informerFactory informers.SharedInformerFactory) Manager {
	informer := informerFactory.Apps().V1().DaemonSets()

	getter := func(namespace, name string) (runtime.Object, error) {
		return informer.Lister().DaemonSets(namespace).Get(name)
	}
	objCreator := func() runtime.Object {
		return &appsv1.DaemonSet{}
	}

	return &appManager{
		kind:       "DaemonSet",
		appGetter:  getter,
		objCreator: objCreator,
		appSynced:  informer.Informer().HasSynced,
	}
}

type (
	objCreator func() runtime.Object
	appGetter  func(namespace, name string) (runtime.Object, error)
)

// appManager is an administrator framework to handle app workloads.
type appManager struct {
	kind       string
	appGetter  appGetter
	objCreator objCreator
	appSynced  cache.InformerSynced
}

// Start starts the manager.
func (m *appManager) Start(stopCh <-chan struct{}) error {
	if !cache.WaitForCacheSync(stopCh, m.appSynced) {
		return fmt.Errorf("wait for %s caches synced timeout", m.kind)
	}
	return nil
}

// Handle handles a workload admission request.
func (m *appManager) Handle(
	request *admissionv1beta1.AdmissionRequest) (*Workload, []*VolumeInfo, []*VolumeInfo, error) {
	obj, err := m.decodeObj(request.Object.Raw)
	if err != nil {
		return nil, nil, nil, err
	}

	// This is special for replicasets as it maybe created by deployment.
	if m.createdByController(obj) {
		return nil, nil, nil, newIgnoreError()
	}

	workload, podSpec, err := m.getWorkloadAndPodSpec(obj)
	if err != nil {
		return nil, nil, nil, err
	}

	var releasedVolumes []*VolumeInfo
	usedVolumes := extractVolumes(podSpec)

	if request.Operation == admissionv1beta1.Update {
		oldObj, err := m.decodeObj(request.OldObject.Raw)
		if err != nil {
			return nil, nil, nil, err
		}
		_, oldPodSpec := getReplicasAndPodSpec(oldObj)
		releasedVolumes = filterVolumes(usedVolumes, oldPodSpec)
	}

	klog.V(4).Infof("Processed app: %+v", workload.ObjectReference)

	return workload, usedVolumes, releasedVolumes, nil
}

// MountedVolumes returns mounted volumes by a workload.
func (m *appManager) MountedVolumes(ref *corev1.ObjectReference) ([]*VolumeInfo, error) {
	obj, err := m.appGetter(ref.Namespace, ref.Name)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			klog.V(4).Infof("App %s not exist", ref.String())
			return nil, nil
		}
		return nil, err
	}
	_, podSpec := getReplicasAndPodSpec(obj)
	return extractVolumes(podSpec), nil
}

// Exist returns true is a workload exist.
func (m *appManager) Exist(ref *corev1.ObjectReference) (bool, error) {
	_, err := m.appGetter(ref.Namespace, ref.Name)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			klog.V(4).Infof("App %s not exist", ref.String())
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// decodeObj decodes an obj.
func (m *appManager) decodeObj(raw []byte) (runtime.Object, error) {
	obj := m.objCreator()
	if _, _, err := util.Codecs.UniversalDeserializer().Decode(raw, nil, obj); err != nil {
		return nil, fmt.Errorf("decode %s failed: %v", m.kind, err)
	}
	return obj, nil
}

// createdByController returns true if obj is created by a Controller.
func (m *appManager) createdByController(obj runtime.Object) bool {
	owners := reflect.ValueOf(obj).Elem().FieldByName("OwnerReferences")
	if owners.IsNil() {
		return false
	}
	for _, ref := range owners.Interface().([]metav1.OwnerReference) {
		if ref.Controller != nil && *ref.Controller {
			return true
		}
	}
	return false
}

// getWorkloadAndPodSpec extracts workload and pod spec information form obj.
func (m *appManager) getWorkloadAndPodSpec(obj runtime.Object) (*Workload, *corev1.PodSpec, error) {
	accessor, err := meta.Accessor(obj)
	if err != nil {
		return nil, nil, fmt.Errorf("access the object failed: %v", err)
	}

	replicas, podSpec := getReplicasAndPodSpec(obj)

	return &Workload{
		ObjectReference: corev1.ObjectReference{
			APIVersion: "apps/v1",
			Kind:       m.kind,
			Name:       accessor.GetName(),
			Namespace:  accessor.GetNamespace(),
			UID:        accessor.GetUID(),
		},
		Replicas: replicas,
	}, podSpec, nil
}

//getReplicasAndPodSpec extracts workload replicas and pod spec from obj.
func getReplicasAndPodSpec(obj runtime.Object) (*int32, *corev1.PodSpec) {
	appSpec := reflect.ValueOf(obj).Elem().FieldByName("Spec")
	podSpec := appSpec.FieldByName("Template").FieldByName("Spec").Addr().Interface().(*corev1.PodSpec)

	replicas := appSpec.FieldByName("Replicas")
	if replicas.IsValid() {
		return int32Ptr(int32(replicas.Elem().Int())), podSpec
	}

	return nil, podSpec
}
