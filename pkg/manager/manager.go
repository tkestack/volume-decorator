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
	"context"
	"fmt"
	"net/http"

	"tkestack.io/volume-decorator/pkg/config"
	pvcrinformers "tkestack.io/volume-decorator/pkg/generated/informers/externalversions"
	"tkestack.io/volume-decorator/pkg/tapps"
	"tkestack.io/volume-decorator/pkg/util"
	"tkestack.io/volume-decorator/pkg/volume"
	"tkestack.io/volume-decorator/pkg/workload"

	"github.com/kubernetes-csi/csi-lib-utils/leaderelection"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/runtime/signals"
)

// Manager manages volume runtime information.
type Manager interface {
	Run(cfg *config.Config) error
}

// manager is the implementation of Manager.
type manager struct {
	k8sClient           kubernetes.Interface
	informerFactory     informers.SharedInformerFactory
	pvSynced            cache.InformerSynced
	pvcSynced           cache.InformerSynced
	pvcrInformerFactory pvcrinformers.SharedInformerFactory
	pvcrSynced          cache.InformerSynced

	admitor          *admitor
	pvcrManager      *pvcrManager
	nodeCollector    *nodeCollector
	usageCollector   *usageCollector
	workloadRecycler *workloadRecycler
	volumeManager    volume.Manager
	workloadManager  workload.Manager

	tappManager tapps.Manager
}

// New creates a new manager.
func New(cfg *config.Config) (Manager, error) {
	k8sConfig := &cfg.K8sConfig
	volumeConfig := &cfg.VolumeConfig

	restCfg, err := util.NewK8sConfig(k8sConfig)
	if err != nil {
		return nil, err
	}

	if cfg.CreateCRD {
		if err := syncCRD(restCfg); err != nil {
			return nil, err
		}
	}

	k8sClient, pvcrClient, err := util.NewK8sClient(restCfg)
	if err != nil {
		return nil, err
	}

	tappManager, err := tapps.New(restCfg, k8sConfig.ResyncPeriod)
	if err != nil {
		return nil, fmt.Errorf("create tapp manager failed: %v", err)
	}

	informerFactory := informers.NewSharedInformerFactory(k8sClient, k8sConfig.ResyncPeriod)
	pvInformer := informerFactory.Core().V1().PersistentVolumes()
	pvcInformer := informerFactory.Core().V1().PersistentVolumeClaims()

	pvcrInformerFactory := pvcrinformers.NewSharedInformerFactory(pvcrClient, k8sConfig.ResyncPeriod)
	pvcrInformer := pvcrInformerFactory.Storage().V1().PersistentVolumeClaimRuntimes()

	pvLister := pvInformer.Lister()
	pvcLister := pvcInformer.Lister()
	pvcrLister := pvcrInformer.Lister()

	volumeManager := volume.New(volumeConfig, pvcrClient, pvLister, pvcLister, pvcrLister)
	workloadManager := workload.New(k8sClient, informerFactory, tappManager)

	return &manager{
		k8sClient:           k8sClient,
		informerFactory:     informerFactory,
		pvSynced:            pvInformer.Informer().HasSynced,
		pvcSynced:           pvcInformer.Informer().HasSynced,
		pvcrInformerFactory: pvcrInformerFactory,
		pvcrSynced:          pvcrInformer.Informer().HasSynced,

		admitor:          newAdmitor(volumeManager, workloadManager),
		volumeManager:    volumeManager,
		workloadManager:  workloadManager,
		pvcrManager:      newPVCRManager(volumeManager, pvcLister, pvcrClient, pvcrLister, pvcInformer),
		nodeCollector:    newNodeCollector(volumeManager, pvcrClient, pvcLister, pvcrLister),
		usageCollector:   newUsageCollector(volumeManager, pvcrClient, pvcLister, pvcrLister),
		workloadRecycler: newWorkloadRecycler(workloadManager, pvcrClient, pvcrLister),

		tappManager: tappManager,
	}, nil
}

// Run starts the manager.
func (m *manager) Run(cfg *config.Config) error {
	webhookConfig := &cfg.WebhookConfig
	if !cfg.LeaderElection {
		return m.run(webhookConfig, cfg.Worker, signals.SetupSignalHandler())
	}

	run := func(ctx context.Context) {
		stopCh := ctx.Done()
		err := m.run(webhookConfig, cfg.Worker, stopCh)
		if err != nil {
			{
				klog.Errorf("Start volume manager failed: %v", err)
			}
		}
	}

	le := leaderelection.NewLeaderElectionWithConfigMaps(m.k8sClient, "tke-volume-decorator", run)
	if len(cfg.LeaderElectionNamespace) != 0 {
		le.WithNamespace(cfg.LeaderElectionNamespace)
	}

	return le.Run()
}

// run starts the manager.
func (m *manager) run(webhookCfg *config.WebhookConfig, worker int, stopCh <-chan struct{}) error {
	m.informerFactory.Start(stopCh)
	m.pvcrInformerFactory.Start(stopCh)
	if !cache.WaitForCacheSync(stopCh, m.pvSynced, m.pvcSynced, m.pvcrSynced) {
		return fmt.Errorf("wait for pv/pvc caches synced timeout")
	}

	if err := m.tappManager.Start(stopCh); err != nil {
		return fmt.Errorf("start tapp manager failed: %v", err)
	}

	if err := m.workloadManager.Start(stopCh); err != nil {
		return fmt.Errorf("start workload manager failed: %v", err)
	}
	if err := m.volumeManager.Start(stopCh); err != nil {
		return fmt.Errorf("start volume manager failed: %v", err)
	}

	m.pvcrManager.Run(worker, stopCh)
	m.nodeCollector.Run(worker, stopCh)
	m.usageCollector.Run(worker, stopCh)
	m.workloadRecycler.Run(worker, stopCh)

	addr := ":443"
	if len(webhookCfg.URL) > 0 {
		addr = webhookCfg.URL
	}

	if !webhookCfg.WorkloadAdmission {
		klog.Infof("Workload admission disabled")
		<-stopCh
		return nil
	}

	klog.Info("Workload admission enabled, start webhook server")

	mux := http.NewServeMux()
	mux.HandleFunc(webhookCfg.ValidatingPath, m.admitor.handle)
	server := &http.Server{
		Addr:      addr,
		Handler:   mux,
		TLSConfig: webhookCfg.TLSConfig(),
	}

	if err := m.syncWebhook(webhookCfg); err != nil {
		return fmt.Errorf("sync webhook failed: %v", err)
	}

	return server.ListenAndServeTLS("", "")
}
