// Copyright 2019 ArgoCD Operator Developers
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// 	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package argocdexport

import (
	"context"

	argoprojv1a1 "github.com/argoproj-labs/argocd-operator/pkg/apis/argoproj/v1alpha1"
	argoprojv1alpha1 "github.com/argoproj-labs/argocd-operator/pkg/apis/argoproj/v1alpha1"
	"github.com/argoproj-labs/argocd-operator/pkg/common"
	"github.com/argoproj-labs/argocd-operator/pkg/controller/argoutil"
	"github.com/sethvargo/go-password/password"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// generateAWSBucketName will return the AWS bucket name for the the given ArgoCDExport.
func generateAWSBucketName(cr *argoprojv1a1.ArgoCDExport) []byte {
	return []byte(cr.Name)
}

// generateBackupKey will generate and return the backup key for the export process.
func generateBackupKey() ([]byte, error) {
	pass, err := password.Generate(
		common.ArgoCDDefaultBackupKeyLength,
		common.ArgoCDDefaultBackupKeyNumDigits,
		common.ArgoCDDefaultBackupKeyNumSymbols,
		false, false)

	return []byte(pass), err
}

// reconcileExport will ensure that the resources for the export process are present for the ArgoCDExport.
func (r *ReconcileArgoCDExport) reconcileExport(cr *argoprojv1a1.ArgoCDExport) error {
	log.Info("reconciling export secret")
	if err := r.reconcileExportSecret(cr); err != nil {
		return err
	}

	if cr.Spec.Schedule != nil && len(*cr.Spec.Schedule) > 0 {
		log.Info("reconciling export cronjob")
		if err := r.reconcileCronJob(cr); err != nil {
			return err
		}
	} else {
		log.Info("reconciling export job")
		if err := r.reconcileJob(cr); err != nil {
			return err
		}
	}

	return nil
}

// reconcileExportSecret will ensure that the Secret used for the export process is present.
func (r *ReconcileArgoCDExport) reconcileExportSecret(cr *argoprojv1a1.ArgoCDExport) error {
	name := argoutil.FetchStorageSecretName(cr)
	secret := argoutil.NewSecretWithName(cr.ObjectMeta, name)
	if argoutil.IsObjectFound(r.client, cr.Namespace, name, secret) {
		changed := false
		backupKey := secret.Data[common.ArgoCDKeyBackupKey]
		if len(backupKey) <= 0 {
			backupKey, err := generateBackupKey()
			if err != nil {
				return err
			}
			secret.Data[common.ArgoCDKeyBackupKey] = backupKey
			changed = true
		}

		if cr.Spec.Storage != nil && cr.Spec.Storage.Backend == common.ArgoCDExportStorageBackendAWS {
			bucketName := secret.Data[common.ArgoCDKeyAWSBucketName]
			if len(bucketName) <= 0 {
				bucketName = generateAWSBucketName(cr)
				secret.Data[common.ArgoCDKeyAWSBucketName] = bucketName
				changed = true
			}
		}

		if changed {
			return r.client.Update(context.TODO(), secret)
		}
		return nil // TODO: Handle case where backup key changes, should trigger a new export?
	}

	backupKey, err := generateBackupKey()
	if err != nil {
		return err
	}

	secret.Data = map[string][]byte{
		common.ArgoCDKeyBackupKey:     backupKey,
		common.ArgoCDKeyAWSBucketName: generateAWSBucketName(cr),
	}

	if err := controllerutil.SetControllerReference(cr, secret, r.scheme); err != nil {
		return err
	}
	return r.client.Create(context.TODO(), secret)
}

// validateExport will ensure that the given ArgoCDExport is valid.
func (r *ReconcileArgoCDExport) validateExport(cr *argoprojv1alpha1.ArgoCDExport) error {
	if len(cr.Status.Phase) <= 0 {
		cr.Status.Phase = "Pending"
		return r.client.Status().Update(context.TODO(), cr)
	}
	return nil
}
