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

package tapps

import (
	"errors"
	"fmt"
	"time"

	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog"
	tappv1 "tkestack.io/tapp/pkg/apis/tappcontroller/v1"
	clientset "tkestack.io/tapp/pkg/client/clientset/versioned"
	informers "tkestack.io/tapp/pkg/client/informers/externalversions"
	listers "tkestack.io/tapp/pkg/client/listers/tappcontroller/v1"
)

// Manager manages the TApp objects in cluster.
type Manager interface {
	// Start starts the manager.
	Start(stopCh <-chan struct{}) error
	// Support returns true if the cluster installed TApp crd.
	Support() bool
	// Get returns the tapp object.
	Get(namespace, name string) (*tappv1.TApp, error)
}

// New creates a manager.
func New(config *rest.Config, resyncPeriod time.Duration) (Manager, error) {
	client, err := clientset.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("create tapp client failed: %+v", err)
	}

	err = discovery.ServerSupportsVersion(client.Discovery(), tappv1.SchemeGroupVersion)
	if err != nil {
		klog.Warningf("Not support Tapp: %v", err)
		return &manager{support: false}, nil
	}

	informerFactory := informers.NewSharedInformerFactory(client, resyncPeriod)
	informer := informerFactory.Tappcontroller().V1().TApps()

	return &manager{
		support:         true,
		lister:          informer.Lister(),
		synced:          informer.Informer().HasSynced,
		informerFactory: informerFactory,
	}, nil
}

// manager is the implementation of Manager.
type manager struct {
	support         bool
	lister          listers.TAppLister
	synced          cache.InformerSynced
	informerFactory informers.SharedInformerFactory
}

// Start starts the manager.
func (m *manager) Start(stopCh <-chan struct{}) error {
	if !m.Support() {
		return nil
	}
	m.informerFactory.Start(stopCh)
	if !cache.WaitForCacheSync(stopCh, m.synced) {
		return fmt.Errorf("wait for tapp cache syned timeout")
	}
	return nil
}

// Support returns true if the cluster installed TApp crd.
func (m *manager) Support() bool {
	return m.support
}

// Get returns the tapp object.
func (m *manager) Get(namespace, name string) (*tappv1.TApp, error) {
	if !m.Support() {
		return nil, errors.New("not supported")
	}
	return m.lister.TApps(namespace).Get(name)
}
