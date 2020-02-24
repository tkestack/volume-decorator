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
	"fmt"
	"io/ioutil"

	"tkestack.io/volume-decorator/pkg/config"

	"strings"

	"k8s.io/api/admissionregistration/v1beta1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog"
)

// newWebhook creates a ValidatingWebhookConfiguration.
func newWebhook(webhookCfg *config.WebhookConfig) (*v1beta1.ValidatingWebhookConfiguration, error) {
	caCert, err := ioutil.ReadFile(webhookCfg.CAFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read certificate authority from %s: %v", webhookCfg.CAFile, err)
	}

	failurePolicy := v1beta1.Fail
	webhook := v1beta1.ValidatingWebhook{
		Name: webhookCfg.Name + ".storage.tkestack.io",
		Rules: []v1beta1.RuleWithOperations{
			{
				Operations: []v1beta1.OperationType{v1beta1.Create, v1beta1.Update},
				Rule: v1beta1.Rule{
					APIGroups:   []string{""},
					APIVersions: []string{"v1"},
					Resources:   []string{"pods"},
				},
			},
			{
				Operations: []v1beta1.OperationType{v1beta1.Create, v1beta1.Update},
				Rule: v1beta1.Rule{
					APIGroups:   []string{"apps"},
					APIVersions: []string{"v1"},
					Resources:   []string{"deployments", "statefulsets", "replicasets", "daemonsets"},
				},
			},
			{
				Operations: []v1beta1.OperationType{v1beta1.Create, v1beta1.Update},
				Rule: v1beta1.Rule{
					APIGroups:   []string{"batch"},
					APIVersions: []string{"v1"},
					Resources:   []string{"jobs"},
				},
			},
			{
				Operations: []v1beta1.OperationType{v1beta1.Create, v1beta1.Update},
				Rule: v1beta1.Rule{
					APIGroups:   []string{"tkestack.io"},
					APIVersions: []string{"v1"},
					Resources:   []string{"tapps"},
				},
			},
		},
		FailurePolicy: &failurePolicy,
		ClientConfig: v1beta1.WebhookClientConfig{
			CABundle: caCert,
		},
	}
	if len(webhookCfg.URL) > 0 {
		url := "https://" + strings.Trim(webhookCfg.URL, "/") + webhookCfg.ValidatingPath
		webhook.ClientConfig.URL = &url
	} else {
		webhook.ClientConfig.Service = &v1beta1.ServiceReference{
			Name:      webhookCfg.ServiceName,
			Namespace: webhookCfg.ServiceNamespace,
			Path:      &webhookCfg.ValidatingPath,
		}
	}

	validatingWebhook := &v1beta1.ValidatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: webhookCfg.Name,
		},
		Webhooks: []v1beta1.ValidatingWebhook{webhook},
	}

	return validatingWebhook, nil
}

// syncWebhook creates or updates a webhook from WebhookConfig.
func (m *manager) syncWebhook(webhookCfg *config.WebhookConfig) error {
	validatingWebhook, err := newWebhook(webhookCfg)
	if err != nil {
		return err
	}
	return m.syncValidatingWebhook(validatingWebhook)
}

// syncValidatingWebhook creates or updates a ValidatingWebhookConfiguration.
func (m *manager) syncValidatingWebhook(webhook *v1beta1.ValidatingWebhookConfiguration) error {
	exist, err := m.k8sClient.AdmissionregistrationV1beta1().
		ValidatingWebhookConfigurations().Get(webhook.Name, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			_, createErr := m.k8sClient.AdmissionregistrationV1beta1().
				ValidatingWebhookConfigurations().Create(webhook)
			if createErr != nil {
				return fmt.Errorf("create validating webhook %s failed: %v", webhook.Name, createErr)
			}
			klog.Infof("Created validating webhook %s", webhook.Name)
			return nil
		}
		return fmt.Errorf("get validating webhook %s failed: %v", webhook.Name, err)
	}

	if equality.Semantic.DeepEqual(webhook.Webhooks, exist.Webhooks) {
		return nil
	}
	klog.Warningf("Webhook %s has been modified by someone, recovery it", webhook.Name)

	updated := exist.DeepCopy()
	updated.Webhooks = webhook.Webhooks
	_, err = m.k8sClient.AdmissionregistrationV1beta1().ValidatingWebhookConfigurations().Update(updated)
	if err != nil {
		return fmt.Errorf("recovery validating webhook %s failed: %v", webhook.Name, err)
	}

	return nil
}

// syncValidatingWebhook creates or updates a MutatingWebhookConfiguration.
func (m *manager) syncMutatingWebhook(webhook *v1beta1.MutatingWebhookConfiguration) error {
	exist, err := m.k8sClient.AdmissionregistrationV1beta1().
		MutatingWebhookConfigurations().Get(webhook.Name, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			_, createErr := m.k8sClient.AdmissionregistrationV1beta1().
				MutatingWebhookConfigurations().Create(webhook)
			if createErr != nil {
				return fmt.Errorf("create mutating webhook %s failed: %v", webhook.Name, err)
			}
			klog.Infof("Create mutating webhook %s", webhook.Name)
			return nil
		}
		return fmt.Errorf("get mutating webhook %s failed: %v", webhook.Name, err)
	}

	if equality.Semantic.DeepEqual(webhook.Webhooks, exist.Webhooks) {
		return nil
	}
	klog.Warningf("Webhook %s has been modified by someone, recovery it", webhook.Name)

	updated := exist.DeepCopy()
	updated.Webhooks = webhook.Webhooks
	_, err = m.k8sClient.AdmissionregistrationV1beta1().MutatingWebhookConfigurations().Update(updated)
	if err != nil {
		return fmt.Errorf("recovery mutating webhook %s failed: %v", webhook.Name, err)
	}

	return nil
}
