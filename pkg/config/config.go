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

package config

import (
	"crypto/tls"
	"flag"
	"strings"
	"time"

	"tkestack.io/volume-decorator/pkg/types"

	"k8s.io/klog"
)

// Config contains all configurations.
type Config struct {
	WebhookConfig
	K8sConfig
	VolumeConfig
	Worker                  int
	CreateCRD               bool
	LeaderElection          bool
	LeaderElectionNamespace string
}

// AddFlags adds all configurations to the global flags.
func (c *Config) AddFlags() {
	c.WebhookConfig.AddFlags()
	c.K8sConfig.AddFlags()
	c.VolumeConfig.AddFlags()
	flag.IntVar(&c.Worker, "worker", 10, "Worker count")
	flag.BoolVar(&c.CreateCRD, "create-crd", false, "Create the CRD when manager started")
	flag.BoolVar(&c.LeaderElection, "leader-election", false, "Enable leader election.")
	flag.StringVar(&c.LeaderElectionNamespace, "leader-election-namespace",
		"kube-system", "Namespace where the leader election resource lives.")
}

// WebhookConfig is a set of configurations of Webhook.
type WebhookConfig struct {
	Name              string
	CertFile          string
	KeyFile           string
	CAFile            string
	MutatingPath      string
	ValidatingPath    string
	URL               string
	ServiceName       string
	ServiceNamespace  string
	WorkloadAdmission bool
}

// AddFlags adds webhook related configurations to the global flags.
func (c *WebhookConfig) AddFlags() {
	flag.StringVar(&c.Name, "webhook-name", "volume-manager", "Name of the webhook")
	flag.StringVar(&c.ValidatingPath, "workload-webhook-path",
		"/tke/storage/workload", "Path of the workload webhook")
	flag.StringVar(&c.CertFile, "tls-cert-file", c.CertFile, ""+
		"File containing the default x509 Certificate for HTTPS. (CA cert, if any, concatenated "+
		"after server cert).")
	flag.StringVar(&c.KeyFile, "tls-private-key-file", c.KeyFile, ""+
		"File containing the default x509 private key matching --tls-cert-file.")
	flag.StringVar(&c.CAFile, "client-ca-file", c.CAFile, "File containing the client certificate")
	flag.StringVar(&c.URL, "webhook-url", "",
		"URL of the webhook service, will be used if the service running out of cluster")
	flag.StringVar(&c.ServiceName, "service-name",
		"volume-manager", "Name of the webhook service, will be used if the service running in the cluster")
	flag.StringVar(&c.ServiceNamespace, "service-namespace", "kube-system",
		"Namespace the webhook service running, will be used if the service running in the cluster")
	flag.BoolVar(&c.WorkloadAdmission, "workload-admission", false, "Enable workload admission")
}

// TLSConfig returns the TLS config.
func (c *WebhookConfig) TLSConfig() *tls.Config {
	sCert, err := tls.LoadX509KeyPair(c.CertFile, c.KeyFile)
	if err != nil {
		klog.Fatal(err)
	}
	return &tls.Config{
		Certificates: []tls.Certificate{sCert},
	}
}

// K8sConfig is a set of configurations used to create kubernetes clients and informers.
type K8sConfig struct {
	Master       string
	KubeConfig   string
	ResyncPeriod time.Duration
}

// AddFlags adds Kubernetes related configurations to the global flags.
func (c *K8sConfig) AddFlags() {
	flag.StringVar(&c.Master, "master", "",
		"Master URL to build a client config from. Either this or kubeconfig needs to be set if the "+
			"provisioner is being run out of cluster.")
	flag.StringVar(&c.KubeConfig, "kubeconfig", "", "Absolute path to the kubeconfig")
	flag.DurationVar(&c.ResyncPeriod, "resync-period", time.Minute*10, "Resync period for cache")
}

// VolumeConfig is a set of configurations about concrete volumes.
type VolumeConfig struct {
	Types string
	CephConfig
}

// AddFlags adds volume related configurations to the global flags.
func (c *VolumeConfig) AddFlags() {
	flag.StringVar(&c.Types, "volume-types", strings.Join([]string{types.CephRBD, types.CephFS}, ","),
		"Volume types the cluster supported")
	flag.StringVar(&c.CephConfig.ConfigFile, "ceph-config-file",
		"/etc/ceph/ceph.conf", "Path of ceph config file")
	flag.StringVar(&c.CephConfig.KeryingFile, "ceph-keyring-file",
		"/etc/ceph/ceph.client.admin.keyring", "Path of ceph admin keyring file")
	flag.DurationVar(&c.CephConfig.MdsSessionListPeriod, "ceph-mds-session-list-period",
		time.Second*30, "Period between two consecutive mds session list operations")
	flag.StringVar(&c.CephConfig.CephFSRootPath, "cephfs-root-path", "/", "Path of cephfs root dir")
	flag.StringVar(&c.CephConfig.CephFSRootMountPath, "cephfs-root-mount-path",
		"/tmp/cephfs-root", "Local path to mount the cephfs root dir")
}

// CephConfig is a set of configurations used to manage ceph related volumes: CephRBD and CephFS.
type CephConfig struct {
	ConfigFile           string
	KeryingFile          string
	MdsSessionListPeriod time.Duration
	CephFSRootPath       string
	CephFSRootMountPath  string
}
