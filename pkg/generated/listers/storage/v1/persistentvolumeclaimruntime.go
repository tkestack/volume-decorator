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

// Code generated by lister-gen. DO NOT EDIT.

package v1

import (
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
	v1 "tkestack.io/volume-decorator/pkg/apis/storage/v1"
)

// PersistentVolumeClaimRuntimeLister helps list PersistentVolumeClaimRuntimes.
type PersistentVolumeClaimRuntimeLister interface {
	// List lists all PersistentVolumeClaimRuntimes in the indexer.
	List(selector labels.Selector) (ret []*v1.PersistentVolumeClaimRuntime, err error)
	// PersistentVolumeClaimRuntimes returns an object that can list and get PersistentVolumeClaimRuntimes.
	PersistentVolumeClaimRuntimes(namespace string) PersistentVolumeClaimRuntimeNamespaceLister
	PersistentVolumeClaimRuntimeListerExpansion
}

// persistentVolumeClaimRuntimeLister implements the PersistentVolumeClaimRuntimeLister interface.
type persistentVolumeClaimRuntimeLister struct {
	indexer cache.Indexer
}

// NewPersistentVolumeClaimRuntimeLister returns a new PersistentVolumeClaimRuntimeLister.
func NewPersistentVolumeClaimRuntimeLister(indexer cache.Indexer) PersistentVolumeClaimRuntimeLister {
	return &persistentVolumeClaimRuntimeLister{indexer: indexer}
}

// List lists all PersistentVolumeClaimRuntimes in the indexer.
func (s *persistentVolumeClaimRuntimeLister) List(selector labels.Selector) (ret []*v1.PersistentVolumeClaimRuntime, err error) {
	err = cache.ListAll(s.indexer, selector, func(m interface{}) {
		ret = append(ret, m.(*v1.PersistentVolumeClaimRuntime))
	})
	return ret, err
}

// PersistentVolumeClaimRuntimes returns an object that can list and get PersistentVolumeClaimRuntimes.
func (s *persistentVolumeClaimRuntimeLister) PersistentVolumeClaimRuntimes(namespace string) PersistentVolumeClaimRuntimeNamespaceLister {
	return persistentVolumeClaimRuntimeNamespaceLister{indexer: s.indexer, namespace: namespace}
}

// PersistentVolumeClaimRuntimeNamespaceLister helps list and get PersistentVolumeClaimRuntimes.
type PersistentVolumeClaimRuntimeNamespaceLister interface {
	// List lists all PersistentVolumeClaimRuntimes in the indexer for a given namespace.
	List(selector labels.Selector) (ret []*v1.PersistentVolumeClaimRuntime, err error)
	// Get retrieves the PersistentVolumeClaimRuntime from the indexer for a given namespace and name.
	Get(name string) (*v1.PersistentVolumeClaimRuntime, error)
	PersistentVolumeClaimRuntimeNamespaceListerExpansion
}

// persistentVolumeClaimRuntimeNamespaceLister implements the PersistentVolumeClaimRuntimeNamespaceLister
// interface.
type persistentVolumeClaimRuntimeNamespaceLister struct {
	indexer   cache.Indexer
	namespace string
}

// List lists all PersistentVolumeClaimRuntimes in the indexer for a given namespace.
func (s persistentVolumeClaimRuntimeNamespaceLister) List(selector labels.Selector) (ret []*v1.PersistentVolumeClaimRuntime, err error) {
	err = cache.ListAllByNamespace(s.indexer, s.namespace, selector, func(m interface{}) {
		ret = append(ret, m.(*v1.PersistentVolumeClaimRuntime))
	})
	return ret, err
}

// Get retrieves the PersistentVolumeClaimRuntime from the indexer for a given namespace and name.
func (s persistentVolumeClaimRuntimeNamespaceLister) Get(name string) (*v1.PersistentVolumeClaimRuntime, error) {
	obj, exists, err := s.indexer.GetByKey(s.namespace + "/" + name)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.NewNotFound(v1.Resource("persistentvolumeclaimruntime"), name)
	}
	return obj.(*v1.PersistentVolumeClaimRuntime), nil
}
