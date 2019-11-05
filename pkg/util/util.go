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

package util

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math"
	"math/big"
	"time"

	"tkestack.io/volume-decorator/pkg/config"
	clientset "tkestack.io/volume-decorator/pkg/generated/clientset/versioned"
	"tkestack.io/volume-decorator/pkg/types"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/cert"
	certutil "k8s.io/client-go/util/cert"
	"k8s.io/client-go/util/keyutil"
)

const (
	certificateBlockType = "CERTIFICATE"
	rsaKeySize           = 2048
	duration365d         = time.Hour * 24 * 365 * 100
)

// NewK8sClient is an utility function used to create a kubernetes sdk client and a custom client for Runtime crd.
func NewK8sClient(cfg *rest.Config) (kubernetes.Interface, clientset.Interface, error) {
	kubeClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create k8s client: %v", err)
	}
	runtimeClient, err := clientset.NewForConfig(cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create runtime client: %v", err)
	}
	return kubeClient, runtimeClient, nil
}

// NewK8sConfig creates k8s rest config.
func NewK8sConfig(cfg *config.K8sConfig) (*rest.Config, error) {
	var (
		err     error
		restCfg *rest.Config
	)
	if cfg.Master != "" || cfg.KubeConfig != "" {
		restCfg, err = clientcmd.BuildConfigFromFlags(cfg.Master, cfg.KubeConfig)
	} else {
		restCfg, err = rest.InClusterConfig()
	}
	if err != nil {
		return nil, fmt.Errorf("failed to create k8s restCfg: %v", err)
	}
	return restCfg, nil
}

// SetupServerCert setups the server cert. For example, user apiservers and admission webhooks
// can use the cert to prove their identify to the kube-apiserver
func SetupServerCert(domain, commonName string) (*types.CertContext, error) {
	signingKey, err := newPrivateKey()
	if err != nil {
		return nil, fmt.Errorf("failed to create CA private key %v", err)
	}
	signingCert, err := cert.NewSelfSignedCACert(cert.Config{CommonName: commonName}, signingKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create CA cert for apiserver %v", err)
	}
	key, err := newPrivateKey()
	if err != nil {
		return nil, fmt.Errorf("failed to create private key for %v", err)
	}

	signedCert, err := newSignedCert(
		&cert.Config{
			CommonName: domain,
			Usages:     []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		},
		key, signingCert, signingKey,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create cert %v", err)
	}
	privateKeyPEM, err := keyutil.MarshalPrivateKeyToPEM(key)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal key %v", err)
	}
	return &types.CertContext{
		Cert:        encodeCertPEM(signedCert),
		Key:         privateKeyPEM,
		SigningCert: encodeCertPEM(signingCert),
	}, nil
}

// newPrivateKey generates a private key.
func newPrivateKey() (*rsa.PrivateKey, error) {
	return rsa.GenerateKey(rand.Reader, rsaKeySize)
}

// encodeCertPEM encodes a Cert.
func encodeCertPEM(cert *x509.Certificate) []byte {
	block := pem.Block{
		Type:  certificateBlockType,
		Bytes: cert.Raw,
	}
	return pem.EncodeToMemory(&block)
}

// newSignedCert generates s singed cert.
func newSignedCert(
	cfg *certutil.Config,
	key crypto.Signer,
	caCert *x509.Certificate,
	caKey crypto.Signer) (*x509.Certificate, error) {
	serial, err := rand.Int(rand.Reader, new(big.Int).SetInt64(math.MaxInt64))
	if err != nil {
		return nil, err
	}
	if len(cfg.CommonName) == 0 {
		return nil, errors.New("must specify a CommonName")
	}
	if len(cfg.Usages) == 0 {
		return nil, errors.New("must specify at least one ExtKeyUsage")
	}

	certTmpl := x509.Certificate{
		Subject: pkix.Name{
			CommonName:   cfg.CommonName,
			Organization: cfg.Organization,
		},
		DNSNames:     cfg.AltNames.DNSNames,
		IPAddresses:  cfg.AltNames.IPs,
		SerialNumber: serial,
		NotBefore:    caCert.NotBefore,
		NotAfter:     time.Now().Add(duration365d).UTC(),
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  cfg.Usages,
	}
	certDERBytes, err := x509.CreateCertificate(rand.Reader, &certTmpl, caCert, key.Public(), caKey)
	if err != nil {
		return nil, err
	}
	return x509.ParseCertificate(certDERBytes)
}
