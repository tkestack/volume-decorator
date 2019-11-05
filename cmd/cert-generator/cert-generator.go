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
	"fmt"
	"io/ioutil"
	"os"

	"tkestack.io/volume-decorator/pkg/util"
)

var (
	certFile = flag.String("tls-cert-file", "tls.cert", ""+
		"File containing the default x509 Certificate for HTTPS. (CA cert, if any, concatenated "+
		"after server cert).")
	keyFile = flag.String("tls-private-key-file", "tls.key", ""+
		"File containing the default x509 private key matching --tls-cert-file.")
	caFile     = flag.String("client-ca-file", "ca.cert", "File containing the client certificate")
	domain     = flag.String("domain", "", "Webhook server domain")
	commonName = flag.String("common-name", "", "Webhook server common name")
)

// main func.
func main() {
	flag.Parse()

	context, err := util.SetupServerCert(*domain, *commonName)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	if err := writeFile(*certFile, context.Cert); err != nil {
		fmt.Printf("Create tls cert file failed: %v", err)
		os.Exit(1)
	}

	if err := writeFile(*keyFile, context.Key); err != nil {
		fmt.Printf("Create tls key file failed: %v", err)
		os.Exit(1)
	}

	if err := writeFile(*caFile, context.SigningCert); err != nil {
		fmt.Printf("Create client ca file failed: %v", err)
		os.Exit(1)
	}
}

// writeFile writes content into a file named fileName.
func writeFile(fileName string, content []byte) error {
	return ioutil.WriteFile(fileName, content, 0640)
}
