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

package nodes

import (
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/klog"
)

const (
	kubeletReadonlyPort      = 10255
	kubeletVolumeUsageMetric = "kubelet_volume_stats_used_bytes"

	syncPeriod   = time.Minute
	usageTimeout = time.Minute * 5
)

// NewVolumeUsageCollector creates a VolumeUsageCollector.
func NewVolumeUsageCollector(nodeLister corelisters.NodeLister) *VolumeUsageCollector {
	return &VolumeUsageCollector{
		usages:     newUsages(),
		nodeLister: nodeLister,
	}
}

// VolumeUsageCollector collects volume real usage from kubelet's metrics periodically.
type VolumeUsageCollector struct {
	usages     *usages
	nodeLister corelisters.NodeLister
}

// Start starts the collector.
func (c *VolumeUsageCollector) Start(stopCh <-chan struct{}) {
	go wait.Until(c.syncVolumeUsages, syncPeriod, stopCh)
}

// GetUsage returns the real usage of a volume.
func (c *VolumeUsageCollector) GetUsage(namespace, name string, nodeNames []string) (int64, bool) {
	for _, nodeName := range nodeNames {
		value, exist := c.getVolumeUsageFromNode(namespace, name, nodeName)
		if exist {
			return value, true
		}
	}
	return 0, false
}

// getVolumeUsageFromNode collects a volume's real usage from kubelet's metric API.
func (c *VolumeUsageCollector) getVolumeUsageFromNode(namespace, name, nodeName string) (int64, bool) {
	key := namespacedVolumeKey(namespace, name)
	usage, exist := c.usages.Get(nodeName, key)
	if exist {
		return usage, true
	}

	values, err := c.syncVolumeUsageFromNode(nodeName, sets.NewString(key))
	if err != nil {
		klog.Errorf("Fetch volume usage from node %s failed: %v", nodeName, err)
		return 0, false
	}
	c.usages.Update(nodeName, values)
	usage, exist = values[key]

	return usage, exist
}

// syncVolumeUsages syncs volumes usage.
func (c *VolumeUsageCollector) syncVolumeUsages() {
	wg := &sync.WaitGroup{}
	for _, nodeName := range c.usages.Nodes() {
		go func(nodeName string) {
			wg.Add(1)
			defer wg.Done()
			values, err := c.syncVolumeUsageFromNode(nodeName, c.usages.Volumes(nodeName))
			if err != nil {
				klog.Errorf("Fetch volume usage from node %s failed: %v", nodeName, err)
			} else {
				c.usages.Update(nodeName, values)
			}
		}(nodeName)
	}
	wg.Wait()
}

// syncVolumeUsageFromNode syncs volumes' usage from kubelet's metric API.
func (c *VolumeUsageCollector) syncVolumeUsageFromNode(nodeName string, volumes sets.String) (map[string]int64, error) {
	address, err := c.getNodeAddress(nodeName)
	if err != nil {
		return nil, err
	}
	if len(address) == 0 {
		return nil, nil
	}

	samples, err := getVolumeMetricsFromNode(nodeName, address)
	if err != nil {
		return nil, err
	}

	result := make(map[string]int64, volumes.Len())
	for _, sample := range samples {
		name, namespace := "", ""
		for k, v := range sample.Metric {
			switch k {
			case "persistentvolumeclaim":
				name = string(v)
			case "namespace":
				namespace = string(v)
			}
		}
		if len(name) == 0 || len(namespace) == 0 {
			klog.Errorf("Can't get name or namespace from sample: %+v", sample)
			continue
		}

		key := namespacedVolumeKey(namespace, name)
		if volumes.Has(key) {
			result[key] = int64(sample.Value)
		}
	}

	return result, nil
}

// getNodeAddress gets node's IP through k8s API.
func (c *VolumeUsageCollector) getNodeAddress(nodeName string) (string, error) {
	node, err := c.nodeLister.Get(nodeName)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			klog.V(4).Infof("Node %s not exist", nodeName)
			return "", nil
		}
		return "", err
	}

	address := ""
	for _, a := range node.Status.Addresses {
		if a.Type == corev1.NodeInternalIP {
			address = a.Address
			break
		}
	}
	if len(address) == 0 {
		return "", fmt.Errorf("can't find address for node %s", nodeName)
	}

	return address, nil
}

// getVolumeMetricsFromNode get metrics from kubelet's API.
func getVolumeMetricsFromNode(nodeName, address string) (model.Samples, error) {
	response, err := http.Get(fmt.Sprintf("http://%s:%d/metrics", address, kubeletReadonlyPort))
	if err != nil {
		return nil, fmt.Errorf("request to node %s failed: %v", nodeName, err)
	}
	defer response.Body.Close()

	data, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("read response from node %s failed: %v", nodeName, err)
	}

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status from node %s: %d, %s", nodeName, response.StatusCode, string(data))
	}

	metrics, err := parseMetrics(string(data))
	if err != nil {
		return nil, fmt.Errorf("parse metrics from node %s failed: %v", nodeName, err)
	}

	var usageSamples model.Samples
	for name, samples := range metrics {
		if name == kubeletVolumeUsageMetric {
			usageSamples = samples
			break
		}
	}
	if len(usageSamples) == 0 {
		return nil, fmt.Errorf("can't find metric %s from node %s", kubeletVolumeUsageMetric, nodeName)
	}

	return usageSamples, nil
}

type metrics map[string]model.Samples

// parseMetrics parses kubelet metrics.
func parseMetrics(data string) (metrics, error) {
	ms := metrics{}
	dec := expfmt.NewDecoder(strings.NewReader(data), expfmt.FmtText)
	decoder := expfmt.SampleDecoder{
		Dec:  dec,
		Opts: &expfmt.DecodeOptions{},
	}

	for {
		var v model.Vector
		if err := decoder.Decode(&v); err != nil {
			if err == io.EOF {
				// Expected loop termination condition.
				return ms, nil
			}
			continue
		}
		for _, metric := range v {
			name := string(metric.Metric[model.MetricNameLabel])
			ms[name] = append(ms[name], metric)
		}
	}
}

// newUsages creates an empty usages object.
func newUsages() *usages {
	return &usages{usages: make(map[string]map[string]*usage)}
}

// usages is a set of usage.
type usages struct {
	lock   sync.RWMutex
	usages map[string]map[string]*usage
}

// usage is a wrapper of volume usage.
type usage struct {
	value     int64
	lastQuery time.Time
}

// Nodes returns all nodes.
func (u *usages) Nodes() []string {
	u.lock.RLock()
	defer u.lock.RUnlock()
	nodes := make([]string, 0, len(u.usages))
	for nodeName := range u.usages {
		nodes = append(nodes, nodeName)
	}
	return nodes
}

// Volumes returns all volumes mounted on specific node.
func (u *usages) Volumes(nodeName string) sets.String {
	u.lock.RLock()
	defer u.lock.RUnlock()
	volumes := sets.NewString()
	for key := range u.usages[nodeName] {
		volumes.Insert(key)
	}
	return volumes
}

// Get gets a volume's usage from a specific node.
func (u *usages) Get(nodeName string, key string) (int64, bool) {
	u.lock.RLock()
	defer u.lock.RUnlock()
	values, exist := u.usages[nodeName]
	if !exist {
		return 0, false
	}
	usage, exist := values[key]
	if !exist {
		return 0, false
	}
	usage.lastQuery = time.Now()
	return usage.value, true
}

// Update updates a node's metrics.
func (u *usages) Update(nodeName string, values map[string]int64) {
	u.lock.Lock()
	defer u.lock.Unlock()

	usages, exist := u.usages[nodeName]
	if !exist {
		usages = make(map[string]*usage)
		u.usages[nodeName] = usages
	}

	for key, value := range values {
		us, exist := usages[key]
		if !exist {
			us = &usage{lastQuery: time.Now()}
			usages[key] = us
		}
		us.value = value
	}

	// Clear unused usage.
	for key, usage := range usages {
		if usage.lastQuery.Add(usageTimeout).Before(time.Now()) {
			delete(usages, key)
			klog.V(5).Infof("Delete usage of volume %s from node %s", key, nodeName)
		}
	}
}

// namespacedVolumeKey generates a key from namespace and name.
func namespacedVolumeKey(namespace, name string) string {
	return namespace + "/" + name
}
