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

package main

import (
	"flag"

	"tkestack.io/volume-decorator/pkg/config"
	"tkestack.io/volume-decorator/pkg/manager"

	"k8s.io/klog"
)

// main func.
func main() {
	klog.InitFlags(nil)
	cfg := &config.Config{}
	cfg.AddFlags()

	flag.Parse()

	m, err := manager.New(cfg)
	if err != nil {
		klog.Fatalf("Create Manager failed: %v", err)
	}

	if err := m.Run(cfg); err != nil {
		klog.Fatalf("Start Manager failed: %v", err)
	}
}
