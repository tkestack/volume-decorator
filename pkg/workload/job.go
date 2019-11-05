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

package workload

import (
	"errors"
	"fmt"

	"tkestack.io/volume-decorator/pkg/util"

	admissionv1beta1 "k8s.io/api/admission/v1beta1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/informers"
	batchlisters "k8s.io/client-go/listers/batch/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog"
)

// newJobManager creates a Manager used for k8s native Job API.
func newJobManager(informerFactory informers.SharedInformerFactory) Manager {
	informer := informerFactory.Batch().V1().Jobs()
	return &jobManager{
		jobLister: informer.Lister(),
		jobSynced: informer.Informer().HasSynced,
	}
}

// jobManager is a manager for k8s native job API.
type jobManager struct {
	jobSynced cache.InformerSynced
	jobLister batchlisters.JobLister
}

// Start starts the manager.
func (m *jobManager) Start(stopCh <-chan struct{}) error {
	if !cache.WaitForCacheSync(stopCh, m.jobSynced) {
		return errors.New("wait for Job caches synced timeout")
	}
	return nil
}

// Handle handles a workload admission request.
func (m *jobManager) Handle(
	request *admissionv1beta1.AdmissionRequest) (*Workload, []*VolumeInfo, []*VolumeInfo, error) {
	job := &batchv1.Job{}
	if _, _, err := util.Codecs.UniversalDeserializer().Decode(request.Object.Raw, nil, job); err != nil {
		return nil, nil, nil, fmt.Errorf("decode Job failed: %v", err)
	}

	var releasedVolumes []*VolumeInfo
	usedVolumes := extractVolumes(&job.Spec.Template.Spec)

	if request.Operation == admissionv1beta1.Update {
		if jobFinished(job) {
			// Job is already completed, just release the used volumes.
			usedVolumes = nil
			releasedVolumes = usedVolumes
		} else {
			// Job is not yet completed, release volumes which used by old job but not the new one.
			oldJob := &batchv1.Job{}
			if _, _, err := util.Codecs.UniversalDeserializer().Decode(request.OldObject.Raw, nil, oldJob); err != nil {
				return nil, nil, nil, fmt.Errorf("decode old Job failed: %v", err)
			}
			releasedVolumes = filterVolumes(usedVolumes, &oldJob.Spec.Template.Spec)
		}
	}

	ref := corev1.ObjectReference{
		APIVersion: "batch/v1",
		Kind:       "Job",
		Name:       job.Name,
		Namespace:  job.Namespace,
		UID:        job.UID,
	}
	klog.V(4).Infof("Processed app: %+v", ref)

	return &Workload{ObjectReference: ref, Replicas: job.Spec.Parallelism}, usedVolumes, releasedVolumes, nil
}

// MountedVolumes returns mounted volumes by a workload.
func (m *jobManager) MountedVolumes(ref *corev1.ObjectReference) ([]*VolumeInfo, error) {
	job, err := m.jobLister.Jobs(ref.Namespace).Get(ref.Name)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			klog.V(4).Infof("Job %s/%s not exist", ref.Namespace, ref.Name)
			return nil, nil
		}
		return nil, err
	}

	// If job already finished, we assume all volumes used by it are released.
	if jobFinished(job) {
		klog.V(4).Infof("Job %s/%s is already completed", ref.Namespace, ref.Name)
		return nil, nil
	}

	return extractVolumes(&job.Spec.Template.Spec), nil
}

// Exist returns true is a workload exist.
func (m *jobManager) Exist(ref *corev1.ObjectReference) (bool, error) {
	_, err := m.jobLister.Jobs(ref.Namespace).Get(ref.Name)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			klog.V(4).Infof("Job %s not exist", ref.String())
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// A Job object can finished after running some times, so we need to check this.
func jobFinished(j *batchv1.Job) bool {
	for _, c := range j.Status.Conditions {
		if (c.Type == batchv1.JobComplete || c.Type == batchv1.JobFailed) && c.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}
