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

package volume

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	storagev1alpha1 "tkestack.io/volume-decorator/pkg/apis/storage/v1"
	"tkestack.io/volume-decorator/pkg/config"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog"
)

const (
	cephfsVolumesRoot = "/csi-volumes"
)

// newCephRBDVolume creates a volume for CephRBD storage.
func newCephRBDVolume(config *config.VolumeConfig) volume {
	return &cephRBDVolume{
		cephVolume: newCephVolume(config),
	}
}

//cephRBDVolume is a wrapper for CephRBD storage.
type cephRBDVolume struct {
	cephVolume
}

// Start starts the volume.
func (v *cephRBDVolume) Start(stopCh <-chan struct{}) error { return nil }

// Available returns true if the volume can be mounted by a workload.
func (v *cephRBDVolume) Available(
	workload *storagev1alpha1.Workload,
	pvcr *storagev1alpha1.PersistentVolumeClaimRuntime) error {
	return blockVolumeAvailable(workload, pvcr)
}

// MountedNodes returns the workloads mounted the volume.
func (v *cephRBDVolume) MountedNodes(pv *corev1.PersistentVolume) ([]string, error) {
	rbdInfo := getRBDInfo(pv)
	watchers, err := v.listRBDWatchers(rbdInfo)
	if err != nil {
		return nil, err
	}
	lockers, err := v.listRBDLockers(rbdInfo)
	if err != nil {
		return nil, err
	}
	return sets.NewString(append(watchers, lockers...)...).List(), nil
}

// Usage returns current usage of the volume.
func (v *cephRBDVolume) Usage(pv *corev1.PersistentVolume) (int64, error) {
	return v.getUsageByDu(pv)
}

// Get CephRBD image usage by `rbd du` command.
func (v *cephRBDVolume) getUsageByDu(pv *corev1.PersistentVolume) (int64, error) {
	rbdInfo := getRBDInfo(pv)
	output, err := v.ExecRBDCommandWithTimeout(rbdInfo, longCmdTimeout, "du", rbdInfo.Image)
	if err != nil {
		if isRBDImageNotFound(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("diff rbd volume %s failed: %v", pv.Name, err)
	}

	result := struct {
		Images []struct {
			UsedSize int64 `json:"used_size"`
		} `json:"images"`
	}{}
	if err := json.Unmarshal(output, &result); err != nil {
		return 0, fmt.Errorf("unmarshal layer info of rbd volume %s failed: %v", pv.Name, err)
	}

	if len(result.Images) != 1 {
		return 0, fmt.Errorf("unexpect result count of du %s: %+v", pv.Name, result)
	}

	return result.Images[0].UsedSize, nil
}

// Get CephRBD image usage by `rbd diff` command.
func (v *cephRBDVolume) getUsageByDiff(pv *corev1.PersistentVolume) (int64, error) {
	rbdInfo := getRBDInfo(pv)
	output, err := v.ExecRBDCommandWithTimeout(rbdInfo, longCmdTimeout, "diff", rbdInfo.Image)
	if err != nil {
		if isRBDImageNotFound(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("diff rbd volume %s failed: %v", pv.Name, err)
	}

	var layers []struct {
		Offset int   `json:"offset"`
		Length int64 `json:"length"`
	}
	if err := json.Unmarshal(output, &layers); err != nil {
		return 0, fmt.Errorf("unmarshal layer info of rbd volume %s failed: %v", pv.Name, err)
	}

	usage := int64(0)
	for _, layer := range layers {
		usage += layer.Length
	}

	return usage, nil
}

// Get the device name(such as `/dev/rbd0`) of a CephRBD image.
func (v *cephRBDVolume) getDeviceIfExist(info *rbdInfo) (string, error) {
	output, err := v.ExecRBDCommand(info, "showmapped")
	if err != nil {
		return "", fmt.Errorf("show mapped rbd images failed: %v", err)
	}
	// Example: [{"id":"0","pool":"replicapool","namespace":"",
	// "name":"pvc-12ec0024-c84b-4534-a82b-3e7b7202d050","snap":"-","device":"/dev/rbd0"}]
	var images []struct {
		Pool   string `json:"pool"`
		Name   string `json:"name"`
		Device string `json:"device"`
	}
	err = json.Unmarshal(output, &images)
	if err != nil {
		return "", fmt.Errorf("unmarshal mapped images failed: %v", err)
	}

	for _, image := range images {
		if image.Pool == info.Pool && image.Name == info.Image {
			return image.Device, nil
		}
	}

	return "", nil
}

// Get all watchers of a CephRBD image.
func (v *cephRBDVolume) listRBDWatchers(info *rbdInfo) ([]string, error) {
	output, err := v.ExecRBDCommand(info, "status", info.Image)
	if err != nil {
		return nil, fmt.Errorf("status rbd image failed: %v", err)
	}
	watchers := struct {
		Watchers []struct {
			Address string `json:"address,omitempty"`
		} `json:"watchers,omitempty"`
	}{}
	err = json.Unmarshal(output, &watchers)
	if err != nil {
		if isRBDImageNotFound(err) {
			klog.Warningf("Image %s/%s is deleted, ignore it", info.Pool, info.Image)
			return nil, nil
		}
		return nil, fmt.Errorf("unmarshal watchers failed: %v", err)
	}
	hosts := make([]string, 0, len(watchers.Watchers))
	for _, w := range watchers.Watchers {
		host := parseAddress(w.Address)
		if len(host) > 0 {
			hosts = append(hosts, host)
		}
	}
	return hosts, nil
}

// Get all lockers of a CephRBD image.
func (v *cephRBDVolume) listRBDLockers(info *rbdInfo) ([]string, error) {
	output, err := v.ExecRBDCommand(info, "lock", "list", info.Image)
	if err != nil {
		return nil, fmt.Errorf("status rbd image failed: %v", err)
	}
	var lockers []struct {
		Address string `json:"address"`
	}
	err = json.Unmarshal(output, &lockers)
	if err != nil {
		if isRBDImageNotFound(err) {
			klog.Warningf("Image %s/%s is deleted, ignore it", info.Pool, info.Image)
			return nil, nil
		}
		return nil, fmt.Errorf("unmarshal lockers failed: %v", err)
	}
	hosts := make([]string, 0, len(lockers))
	for _, locker := range lockers {
		host := parseAddress(locker.Address)
		if len(host) > 0 {
			hosts = append(hosts, host)
		}
	}
	return hosts, nil
}

// getRBDInfo extracts CephRBD information from volume.
func getRBDInfo(pv *corev1.PersistentVolume) *rbdInfo {
	attributes := pv.Spec.CSI.VolumeAttributes
	info := &rbdInfo{
		Image:    pv.Name,
		Pool:     attributes["pool"],
		Monitors: attributes["monitors"],
	}
	return info
}

// rbdInfo is a set of information of CephRBD image.
type rbdInfo struct {
	Pool     string
	Image    string
	Monitors string
}

// newCephFSVolume creates a volume for CephFS storage.
func newCephFSVolume(config *config.VolumeConfig) volume {
	return &cephFSVolume{
		cephVolume:           newCephVolume(config),
		mdsSessions:          newMDSSessions(),
		mdsSessionListPeriod: config.CephConfig.MdsSessionListPeriod,
		cephfsRootPath:       config.CephFSRootPath,
		cephfsRootMountPath:  config.CephFSRootMountPath,
	}
}

// cephFSVolume is a wrapper of CephFS volume.
type cephFSVolume struct {
	cephVolume
	mdsSessions          *mdsSessions
	mdsSessionListPeriod time.Duration
	cephfsRootPath       string
	cephfsRootMountPath  string
}

// Start starts the volume.
func (v *cephFSVolume) Start(stopCh <-chan struct{}) error {
	if err := wait.PollUntil(time.Second*10, v.mountCephRootPath, stopCh); err != nil {
		return err
	}
	go wait.Until(v.listMDSSessions, v.mdsSessionListPeriod, stopCh)
	return nil
}

// Available returns true if the volume can be mounted by a workload.
func (v *cephFSVolume) Available(
	workload *storagev1alpha1.Workload,
	pvcr *storagev1alpha1.PersistentVolumeClaimRuntime) error {
	return nil
}

// MountedNodes returns the workloads mounted the volume.
func (v *cephFSVolume) MountedNodes(pv *corev1.PersistentVolume) ([]string, error) {
	// Currently CephFS CSI driver doesn't store abs path in the VolumeAttributes for
	// provisioned volumes. So we need to Splicing the path manually. this is not a good
	// way as it depends on the internal implement of CephFS CSI driver.
	path := getCephfsPath(pv)
	hosts := v.mdsSessions.Get(path)
	if hosts == nil {
		klog.V(4).Infof("Cannot find cephfs session for %s", path)
		return nil, nil
	}
	return hosts.List(), nil
}

// Usage returns current usage of the volume.
func (v *cephFSVolume) Usage(pv *corev1.PersistentVolume) (int64, error) {
	path := filepath.Join(v.cephfsRootMountPath, getCephfsPath(pv))
	output, err := execCommand("getfattr", []string{"-d", "-m", "ceph.dir.rbytes", path})
	if err != nil {
		return 0, fmt.Errorf("exec getfattr for %s failed: %v", pv.Name, err)
	}

	scanner := bufio.NewScanner(bytes.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "ceph.dir.rbytes") {
			continue
		}
		usedBytes, err := strconv.ParseInt(strings.Trim(line[strings.Index(line, "=")+1:], "\""), 10, 64)
		if err != nil {
			return 0, fmt.Errorf("parse usage of %s failed: %v", pv.Name, err)
		}
		return usedBytes, nil
	}
	return 0, errors.New("cannot parse getfattr output")
}

// mountCephRootPath mounts the CephFS root path to the host so that we can access the CephFS dirs directly.
func (v *cephFSVolume) mountCephRootPath() (bool, error) {
	if _, err := os.Stat(v.cephfsRootMountPath); err != nil {
		if os.IsNotExist(err) {
			klog.Infof("Cephfs root mount point not exist, create it")
			if createdErr := os.MkdirAll(v.cephfsRootMountPath, 0700); createdErr != nil {
				klog.Errorf("Create cephfs root mount point %s failed: %v", v.cephfsRootMountPath, createdErr)
				return false, createdErr
			}
		} else {
			klog.Errorf("Stat cephfs root mount point %s failed: %v", v.cephfsRootMountPath, err)
			return false, err
		}
	}
	// Mount point maybe umounted incorrectly, umount manually to eliminate unexpected errors.
	if _, err := execCommand("umount", []string{v.cephfsRootMountPath}); err != nil {
		if !strings.Contains(err.Error(), "not mounted") &&
			!strings.Contains(err.Error(), "未挂载") &&
			!strings.Contains(err.Error(), "mountpoint not found") {
			klog.Errorf("Umount cephfs root mount dir %s failed: %v",
				v.cephfsRootMountPath, err)
			return false, nil
		}
	}

	klog.Info("Mount cephfs root dir")
	_, err := execCommand("ceph-fuse", v.WithCephConfigArgs(v.cephfsRootMountPath, "-r", v.cephfsRootPath))
	if err == nil {
		klog.Info("Mount cephfs root dir succeeded")
		return true, nil
	}
	if strings.Contains(err.Error(), "mountpoint is not empty") {
		klog.Info("Cephfs root dir is already mounted")
		return true, nil
	}
	klog.Errorf("Mount cephfs root dir failed: %v", err)
	return false, nil
}

// listMDSSessions list all active mds sessions so that we can know which CephFS dir is mounted on some host.
func (v *cephFSVolume) listMDSSessions() {
	for _, mds := range v.getAvailableMDS() {
		sessions, err := v.getMDSSessionList(mds)
		if err != nil {
			continue
		}
		v.mdsSessions.Update(generateSessionSet(sessions))
	}
}

// getAvailableMDS get all active mds servers.
func (v *cephFSVolume) getAvailableMDS() []string {
	output, err := execCommand("ceph", v.WithCephConfigArgs("mds", "stat"))
	if err != nil {
		klog.Errorf("Get mds stat failed: %v", err)
		return nil
	}

	var mdsList []string
	scanner := bufio.NewScanner(bytes.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, "up:active") {
			mdsList = append(mdsList, fetchMDS(line))
		}
	}
	if scanner.Err() != nil {
		klog.Errorf("Parse mds stat failed: %v", err)
	}

	klog.V(4).Infof("Find mds: %v", mdsList)

	return mdsList
}

// fetchMDS extracts mds address.
func fetchMDS(info string) string {
	return "mds." + info[strings.Index(info, "{")+1:strings.Index(info, "=")]
}

// getMDSSessionList executes ceph command to find active mds.
func (v *cephFSVolume) getMDSSessionList(mds string) ([]mdsSession, error) {
	output, err := execCommand("ceph", v.WithCephConfigArgs("tell", mds, "session", "ls"))
	if err != nil {
		klog.Errorf("Exec mds session list failed: %v", err)
		return nil, err
	}
	var sessionList []mdsSession
	err = json.Unmarshal(output, &sessionList)
	if err != nil {
		klog.Errorf("Unmarshal session list output failed: %v", err)
	}
	return sessionList, err
}

// generateSessionSet finds mounted nodes of this dir.
func generateSessionSet(sessions []mdsSession) map[string]sets.String {
	sessionSet := make(map[string]sets.String)
	for _, session := range sessions {
		if len(session.Metadata.Root) == 0 || len(session.Metadata.Hostname) == 0 {
			continue
		}
		hosts, exist := sessionSet[session.Metadata.Root]
		if !exist {
			hosts = sets.NewString()
			sessionSet[session.Metadata.Root] = hosts
		}
		hosts.Insert(session.Metadata.Hostname)
	}
	return sessionSet
}

// newMDSSessions creates a mdsSessions.
func newMDSSessions() *mdsSessions {
	return &mdsSessions{sessions: make(map[string]sets.String)}
}

// mdsSessions is a set of mds sessions.
type mdsSessions struct {
	sync.Mutex
	// Map cephfs path to mounted hosts.
	sessions map[string]sets.String
}

// Update updates all sessions.
func (s *mdsSessions) Update(sessions map[string]sets.String) {
	s.Lock()
	defer s.Unlock()
	s.sessions = sessions

	klog.V(5).Infof("Update sessions: %v", s.sessions)
}

// Get returns mounted nodes of a dir.
func (s *mdsSessions) Get(path string) sets.String {
	return s.sessions[path]
}

// mdsSession is a wrapper of Ceph mds session struct.
type mdsSession struct {
	Metadata struct {
		Root     string `json:"root"`
		Hostname string `json:"hostname"`
	} `json:"client_metadata"`
}

// newCephVolume creates a common volume object of Ceph.
func newCephVolume(config *config.VolumeConfig) cephVolume {
	return cephVolume{
		configFile:  config.CephConfig.ConfigFile,
		keyringFile: config.CephConfig.KeryingFile,
	}
}

// cephVolume is a common framework of Ceph volumes.
type cephVolume struct {
	configFile  string
	keyringFile string
}

// WithCephConfigArgs appends Ceph config related arguments to args.
func (v *cephVolume) WithCephConfigArgs(args ...string) []string {
	return append(args, "-c", v.configFile, "--keyring", v.keyringFile)
}

// ExecRBDCommand executes a `rbd xxx` command.
func (v *cephVolume) ExecRBDCommand(info *rbdInfo, args ...string) ([]byte, error) {
	return execCommand("rbd", v.WithCephConfigArgs(withCephPoolArgs(info, args...)...))
}

// ExecRBDCommandWithTimeout executes a `rbd xxx` command with a custom timeout.
func (v *cephVolume) ExecRBDCommandWithTimeout(info *rbdInfo, timeout time.Duration, args ...string) ([]byte, error) {
	return execCmd(timeout, "rbd", v.WithCephConfigArgs(withCephPoolArgs(info, args...)...)...)
}

// withCephPoolArgs appends Ceph poll related arguments to args.
func withCephPoolArgs(info *rbdInfo, args ...string) []string {
	return append(args, "--pool", info.Pool, "-m", info.Monitors, "--format", "json")
}
