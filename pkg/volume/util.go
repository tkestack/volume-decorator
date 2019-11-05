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
	"bytes"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog"
	"path/filepath"
	storagev1alpha1 "tkestack.io/volume-decorator/pkg/apis/storage/v1"
)

const (
	defaultCmdTimeout = time.Minute
	longCmdTimeout    = time.Minute * 5
)

// execCmd runs a cmd.
func execCmd(timeout time.Duration, cmd string, args ...string) ([]byte, error) {
	command := exec.Command(cmd, args...)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := command.Start(); err != nil {
		return nil, err
	}

	timer := time.AfterFunc(timeout, func() {
		if err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL); err != nil {
			klog.Errorf("Kill process failed: %s %v, %v", cmd, args, err)
		} else {
			klog.Errorf("Execute command %s %v timeout(%ds): %d", cmd, args, timeout, command.Process.Pid)
		}
	})
	defer timer.Stop()

	err := command.Wait()
	if err != nil {
		if !isKilledErr(err) {
			return nil, fmt.Errorf("execute cmd %s %v failed output: %s, error: %v", cmd, args, stderr.String(), err)
		}
		return nil, fmt.Errorf("execute command(%s %v) timeout with error(%v)", cmd, args, err)
	}
	return stdout.Bytes(), nil
}

// isKilledErr returns true if an error is a SIGKILL error.
func isKilledErr(err error) bool {
	if strings.Contains(err.Error(), "signal: killed") {
		return true
	}
	return false
}

// execCommand runs a command.
func execCommand(command string, args []string) ([]byte, error) {
	return execCmd(defaultCmdTimeout, command, args...)
}

// parseAddress extract IP from an IP:Port address.
func parseAddress(address string) string {
	return address[:strings.Index(address, ":")]
}

// isRBDImageNotFound returns true if an error is a RBDImageNotFound error.
func isRBDImageNotFound(err error) bool {
	return strings.Contains(err.Error(), "No such file or directory")
}

// getCephfsPath extracts cephfs path from a PV object.
func getCephfsPath(pv *corev1.PersistentVolume) string {
	return filepath.Join(cephfsVolumesRoot, pv.Spec.CSI.VolumeHandle)
}

// sameWorkload returns true if to workload is same.
func sameWorkload(w1, w2 *storagev1alpha1.Workload) bool {
	return w1.ObjectReference.String() == w2.ObjectReference.String()
}
