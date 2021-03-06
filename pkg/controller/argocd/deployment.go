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

package argocd

import (
	"context"
	"fmt"

	argoprojv1a1 "github.com/argoproj-labs/argocd-operator/pkg/apis/argoproj/v1alpha1"
	"github.com/argoproj-labs/argocd-operator/pkg/common"
	"github.com/argoproj-labs/argocd-operator/pkg/controller/argoutil"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func (r *ReconcileArgoCD) getArgoCDExport(cr *argoprojv1a1.ArgoCD) *argoprojv1a1.ArgoCDExport {
	if cr.Spec.Import == nil {
		return nil
	}

	namespace := cr.ObjectMeta.Namespace
	if cr.Spec.Import.Namespace != nil && len(*cr.Spec.Import.Namespace) > 0 {
		namespace = *cr.Spec.Import.Namespace
	}

	export := &argoprojv1a1.ArgoCDExport{}
	if argoutil.IsObjectFound(r.client, namespace, cr.Spec.Import.Name, export) {
		return export
	}
	return nil
}

// getArgoApplicationControllerCommand will return the command for the ArgoCD Application Controller component.
func getArgoApplicationControllerCommand(cr *argoprojv1a1.ArgoCD) []string {
	cmd := make([]string, 0)
	cmd = append(cmd, "argocd-application-controller")

	cmd = append(cmd, "--operation-processors")
	cmd = append(cmd, fmt.Sprint(getArgoServerOperationProcessors(cr)))

	if cr.Spec.HA.Enabled {
		cmd = append(cmd, getRedisSentinelArgs(cr)...)
	} else {
		cmd = append(cmd, "--redis")
		cmd = append(cmd, getRedisServerAddress(cr))
	}

	cmd = append(cmd, "--repo-server")
	cmd = append(cmd, nameWithSuffix("repo-server:8081", cr))

	cmd = append(cmd, "--status-processors")
	cmd = append(cmd, fmt.Sprint(getArgoServerStatusProcessors(cr)))

	return cmd
}

func getArgoExportSecretName(export *argoprojv1a1.ArgoCDExport) string {
	name := argoutil.NameWithSuffix(export.ObjectMeta, "cluster")
	if export.Spec.Storage != nil && len(export.Spec.Storage.SecretName) > 0 {
		name = export.Spec.Storage.SecretName
	}
	return name
}

func getArgoImportBackend(client client.Client, cr *argoprojv1a1.ArgoCD) string {
	backend := common.ArgoCDExportStorageBackendLocal
	namespace := cr.ObjectMeta.Namespace
	if cr.Spec.Import != nil && cr.Spec.Import.Namespace != nil && len(*cr.Spec.Import.Namespace) > 0 {
		namespace = *cr.Spec.Import.Namespace
	}

	export := &argoprojv1a1.ArgoCDExport{}
	if argoutil.IsObjectFound(client, namespace, cr.Spec.Import.Name, export) {
		if export.Spec.Storage != nil && len(export.Spec.Storage.Backend) > 0 {
			backend = export.Spec.Storage.Backend
		}
	}
	return backend
}

// getArgoImportCommand will return the command for the ArgoCD import process.
func getArgoImportCommand(client client.Client, cr *argoprojv1a1.ArgoCD) []string {
	cmd := make([]string, 0)
	cmd = append(cmd, "uid_entrypoint.sh")
	cmd = append(cmd, "argocd-operator-util")
	cmd = append(cmd, "import")
	cmd = append(cmd, getArgoImportBackend(client, cr))
	return cmd
}

func getArgoImportContainerEnv(cr *argoprojv1a1.ArgoCDExport) []corev1.EnvVar {
	env := make([]corev1.EnvVar, 0)

	switch cr.Spec.Storage.Backend {
	case common.ArgoCDExportStorageBackendAWS:
		env = append(env, corev1.EnvVar{
			Name: "AWS_ACCESS_KEY_ID",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: argoutil.FetchStorageSecretName(cr),
					},
					Key: "aws.access.key.id",
				},
			},
		})

		env = append(env, corev1.EnvVar{
			Name: "AWS_SECRET_ACCESS_KEY",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: argoutil.FetchStorageSecretName(cr),
					},
					Key: "aws.secret.access.key",
				},
			},
		})
	}

	return env
}

// getArgoImportContainerImage will return the container image for the Argo CD import process.
func getArgoImportContainerImage() string {
	img := common.ArgoCDDefaultExportJobImage
	tag := common.ArgoCDDefaultExportJobVersion
	return argoutil.CombineImageTag(img, tag)
}

// getArgoImportVolumeMounts will return the VolumneMounts for the given ArgoCDExport.
func getArgoImportVolumeMounts(cr *argoprojv1a1.ArgoCDExport) []corev1.VolumeMount {
	mounts := make([]corev1.VolumeMount, 0)

	mounts = append(mounts, corev1.VolumeMount{
		Name:      "backup-storage",
		MountPath: "/backups",
	})

	mounts = append(mounts, corev1.VolumeMount{
		Name:      "secret-storage",
		MountPath: "/secrets",
	})

	return mounts
}

// getArgoImportVolumes will return the Volumes for the given ArgoCDExport.
func getArgoImportVolumes(cr *argoprojv1a1.ArgoCDExport) []corev1.Volume {
	volumes := make([]corev1.Volume, 0)

	if cr.Spec.Storage != nil && cr.Spec.Storage.Backend == common.ArgoCDExportStorageBackendLocal {
		volumes = append(volumes, corev1.Volume{
			Name: "backup-storage",
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: cr.Name,
				},
			},
		})
	} else {
		volumes = append(volumes, corev1.Volume{
			Name: "backup-storage",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		})
	}

	volumes = append(volumes, corev1.Volume{
		Name: "secret-storage",
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName: getArgoExportSecretName(cr),
			},
		},
	})

	return volumes
}

// getRedisSentinelArgs will return the command args for the Redis sentinels used in HA mode.
func getRedisSentinelArgs(cr *argoprojv1a1.ArgoCD) []string {
	args := make([]string, 0)

	for i := int32(0); i < common.ArgoCDDefaultRedisHAReplicas; i++ {
		args = append(args, "--sentinel")
		args = append(args, nameWithSuffix(fmt.Sprintf("redis-ha-announce-%d:%d", i, common.ArgoCDDefaultRedisSentinelPort), cr))
	}

	args = append(args, "--sentinelmaster")
	args = append(args, "argocd") // TODO: Should this be cr.Name?

	return args
}

// getArgoRepoCommand will return the command for the ArgoCD Repo component.
func getArgoRepoCommand(cr *argoprojv1a1.ArgoCD) []string {
	cmd := make([]string, 0)

	cmd = append(cmd, "uid_entrypoint.sh")
	cmd = append(cmd, "argocd-repo-server")

	if cr.Spec.HA.Enabled {
		cmd = append(cmd, getRedisSentinelArgs(cr)...)
	} else {
		cmd = append(cmd, "--redis")
		cmd = append(cmd, getRedisServerAddress(cr))
	}

	return cmd
}

// getArgoServerCommand will return the command for the ArgoCD server component.
func getArgoServerCommand(cr *argoprojv1a1.ArgoCD) []string {
	cmd := make([]string, 0)
	cmd = append(cmd, "argocd-server")

	cmd = append(cmd, "--staticassets")
	cmd = append(cmd, "/shared/app")

	cmd = append(cmd, "--dex-server")
	cmd = append(cmd, getDexServerAddress(cr))

	cmd = append(cmd, "--repo-server")
	cmd = append(cmd, geRepoServerAddress(cr))

	if getArgoServerInsecure(cr) {
		cmd = append(cmd, "--insecure")
	}

	if cr.Spec.HA.Enabled {
		cmd = append(cmd, getRedisSentinelArgs(cr)...)
	} else {
		cmd = append(cmd, "--redis")
		cmd = append(cmd, getRedisServerAddress(cr))
	}

	return cmd
}

// getDexServerAddress will return the Dex server address.
func getDexServerAddress(cr *argoprojv1a1.ArgoCD) string {
	return fmt.Sprintf("http://%s:%d", nameWithSuffix("dex-server", cr), common.ArgoCDDefaultDexHTTPPort)
}

// geRepoServerAddress will return the Argo CD repo server address.
func geRepoServerAddress(cr *argoprojv1a1.ArgoCD) string {
	return fmt.Sprintf("%s:%d", nameWithSuffix("repo-server", cr), common.ArgoCDDefaultRepoServerPort)
}

// newDeployment returns a new Deployment instance for the given ArgoCD.
func newDeployment(cr *argoprojv1a1.ArgoCD) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cr.Name,
			Namespace: cr.Namespace,
			Labels:    labelsForCluster(cr),
		},
	}
}

// newDeploymentWithName returns a new Deployment instance for the given ArgoCD using the given name.
func newDeploymentWithName(name string, component string, cr *argoprojv1a1.ArgoCD) *appsv1.Deployment {
	deploy := newDeployment(cr)
	deploy.ObjectMeta.Name = name

	lbls := deploy.ObjectMeta.Labels
	lbls[common.ArgoCDKeyName] = name
	lbls[common.ArgoCDKeyComponent] = component
	deploy.ObjectMeta.Labels = lbls

	deploy.Spec = appsv1.DeploymentSpec{
		Selector: &metav1.LabelSelector{
			MatchLabels: map[string]string{
				common.ArgoCDKeyName: name,
			},
		},
		Template: corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					common.ArgoCDKeyName: name,
				},
			},
		},
	}

	return deploy
}

// newDeploymentWithSuffix returns a new Deployment instance for the given ArgoCD using the given suffix.
func newDeploymentWithSuffix(suffix string, component string, cr *argoprojv1a1.ArgoCD) *appsv1.Deployment {
	return newDeploymentWithName(fmt.Sprintf("%s-%s", cr.Name, suffix), component, cr)
}

// reconcileApplicationControllerDeployment will ensure the Deployment resource is present for the ArgoCD Application Controller component.
func (r *ReconcileArgoCD) reconcileApplicationControllerDeployment(cr *argoprojv1a1.ArgoCD) error {
	deploy := newDeploymentWithSuffix("application-controller", "application-controller", cr)
	if argoutil.IsObjectFound(r.client, cr.Namespace, deploy.Name, deploy) {
		return nil // Deployment found, do nothing
	}

	podSpec := &deploy.Spec.Template.Spec
	podSpec.Containers = []corev1.Container{{
		Command:         getArgoApplicationControllerCommand(cr),
		Image:           getArgoContainerImage(cr),
		ImagePullPolicy: corev1.PullAlways,
		Name:            "argocd-application-controller",
		LivenessProbe: &corev1.Probe{
			Handler: corev1.Handler{
				HTTPGet: &corev1.HTTPGetAction{
					Path: "/healthz",
					Port: intstr.FromInt(8082),
				},
			},
			InitialDelaySeconds: 5,
			PeriodSeconds:       10,
		},
		Ports: []corev1.ContainerPort{
			{
				ContainerPort: 8082,
			},
		},
		ReadinessProbe: &corev1.Probe{
			Handler: corev1.Handler{
				HTTPGet: &corev1.HTTPGetAction{
					Path: "/healthz",
					Port: intstr.FromInt(8082),
				},
			},
			InitialDelaySeconds: 5,
			PeriodSeconds:       10,
		},
		Resources: getArgoApplicationControllerResources(cr),
	}}

	// Handle import/restore from ArgoCDExport
	export := r.getArgoCDExport(cr)
	if export == nil {
		log.Info("existing argocd export not found, skipping import")
	} else {
		podSpec.InitContainers = []corev1.Container{{
			Command:         getArgoImportCommand(r.client, cr),
			Env:             getArgoImportContainerEnv(export),
			Image:           getArgoImportContainerImage(),
			ImagePullPolicy: corev1.PullAlways,
			Name:            "argocd-import",
			VolumeMounts:    getArgoImportVolumeMounts(export),
		}}

		podSpec.Volumes = getArgoImportVolumes(export)
	}

	podSpec.ServiceAccountName = "argocd-application-controller"

	if err := controllerutil.SetControllerReference(cr, deploy, r.scheme); err != nil {
		return err
	}
	return r.client.Create(context.TODO(), deploy)
}

// reconcileDeployments will ensure that all Deployment resources are present for the given ArgoCD.
func (r *ReconcileArgoCD) reconcileDeployments(cr *argoprojv1a1.ArgoCD) error {
	err := r.reconcileApplicationControllerDeployment(cr)
	if err != nil {
		return err
	}

	err = r.reconcileDexDeployment(cr)
	if err != nil {
		return err
	}

	err = r.reconcileRedisDeployment(cr)
	if err != nil {
		return err
	}

	err = r.reconcileRepoDeployment(cr)
	if err != nil {
		return err
	}

	err = r.reconcileServerDeployment(cr)
	if err != nil {
		return err
	}

	err = r.reconcileGrafanaDeployment(cr)
	if err != nil {
		return err
	}

	return nil
}

// reconcileDexDeployment will ensure the Deployment resource is present for the ArgoCD Dex component.
func (r *ReconcileArgoCD) reconcileDexDeployment(cr *argoprojv1a1.ArgoCD) error {
	deploy := newDeploymentWithSuffix("dex-server", "dex-server", cr)
	if argoutil.IsObjectFound(r.client, cr.Namespace, deploy.Name, deploy) {
		return nil // Deployment found, do nothing
	}

	deploy.Spec.Template.Spec.Containers = []corev1.Container{{
		Command: []string{
			"/shared/argocd-util",
			"rundex",
		},
		Image:           getDexContainerImage(cr),
		ImagePullPolicy: corev1.PullAlways,
		Name:            "dex",
		Ports: []corev1.ContainerPort{
			{
				ContainerPort: common.ArgoCDDefaultDexHTTPPort,
				Name:          "http",
			}, {
				ContainerPort: common.ArgoCDDefaultDexGRPCPort,
				Name:          "grpc",
			},
		},
		Resources: getDexResources(cr),
		VolumeMounts: []corev1.VolumeMount{{
			Name:      "static-files",
			MountPath: "/shared",
		}},
	}}

	deploy.Spec.Template.Spec.InitContainers = []corev1.Container{{
		Command: []string{
			"cp",
			"/usr/local/bin/argocd-util",
			"/shared",
		},
		Image:           getArgoContainerImage(cr),
		ImagePullPolicy: corev1.PullAlways,
		Name:            "copyutil",
		VolumeMounts: []corev1.VolumeMount{{
			Name:      "static-files",
			MountPath: "/shared",
		}},
	}}

	deploy.Spec.Template.Spec.ServiceAccountName = common.ArgoCDDefaultDexServiceAccountName
	deploy.Spec.Template.Spec.Volumes = []corev1.Volume{{
		Name: "static-files",
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		},
	}}

	if err := controllerutil.SetControllerReference(cr, deploy, r.scheme); err != nil {
		return err
	}
	return r.client.Create(context.TODO(), deploy)
}

// reconcileGrafanaDeployment will ensure the Deployment resource is present for the ArgoCD Grafana component.
func (r *ReconcileArgoCD) reconcileGrafanaDeployment(cr *argoprojv1a1.ArgoCD) error {
	deploy := newDeploymentWithSuffix("grafana", "grafana", cr)
	if argoutil.IsObjectFound(r.client, cr.Namespace, deploy.Name, deploy) {
		if !cr.Spec.Grafana.Enabled {
			// Deployment exists but enabled flag has been set to false, delete the Deployment
			return r.client.Delete(context.TODO(), deploy)
		}
		if hasGrafanaSpecChanged(deploy, cr) {
			deploy.Spec.Replicas = cr.Spec.Grafana.Size
			return r.client.Update(context.TODO(), deploy)
		}
		return nil // Deployment found, do nothing
	}

	if !cr.Spec.Grafana.Enabled {
		return nil // Grafana not enabled, do nothing.
	}

	deploy.Spec.Replicas = getGrafanaReplicas(cr)

	deploy.Spec.Template.Spec.Containers = []corev1.Container{{
		Image:           getGrafanaContainerImage(cr),
		ImagePullPolicy: corev1.PullAlways,
		Name:            "grafana",
		Ports: []corev1.ContainerPort{
			{
				ContainerPort: 3000,
			},
		},
		Resources: getGrafanaResources(cr),
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "grafana-config",
				MountPath: "/etc/grafana",
			}, {
				Name:      "grafana-datasources-config",
				MountPath: "/etc/grafana/provisioning/datasources",
			}, {
				Name:      "grafana-dashboards-config",
				MountPath: "/etc/grafana/provisioning/dashboards",
			}, {
				Name:      "grafana-dashboard-templates",
				MountPath: "/var/lib/grafana/dashboards",
			},
		},
	}}

	deploy.Spec.Template.Spec.Volumes = []corev1.Volume{
		{
			Name: "grafana-config",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: nameWithSuffix("grafana-config", cr),
					},
					Items: []corev1.KeyToPath{{
						Key:  "grafana.ini",
						Path: "grafana.ini",
					}},
				},
			},
		}, {
			Name: "grafana-datasources-config",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: nameWithSuffix("grafana-config", cr),
					},
					Items: []corev1.KeyToPath{{
						Key:  "datasource.yaml",
						Path: "datasource.yaml",
					}},
				},
			},
		}, {
			Name: "grafana-dashboards-config",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: nameWithSuffix("grafana-config", cr),
					},
					Items: []corev1.KeyToPath{{
						Key:  "provider.yaml",
						Path: "provider.yaml",
					}},
				},
			},
		}, {
			Name: "grafana-dashboard-templates",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: nameWithSuffix("grafana-dashboards", cr),
					},
				},
			},
		},
	}

	if err := controllerutil.SetControllerReference(cr, deploy, r.scheme); err != nil {
		return err
	}
	return r.client.Create(context.TODO(), deploy)
}

// reconcileRedisDeployment will ensure the Deployment resource is present for the ArgoCD Redis component.
func (r *ReconcileArgoCD) reconcileRedisDeployment(cr *argoprojv1a1.ArgoCD) error {
	deploy := newDeploymentWithSuffix("redis", "redis", cr)
	if argoutil.IsObjectFound(r.client, cr.Namespace, deploy.Name, deploy) {
		if cr.Spec.HA.Enabled {
			// Deployment exists but HA enabled flag has been set to true, delete the Deployment
			return r.client.Delete(context.TODO(), deploy)
		}
		return nil // Deployment found, do nothing
	}

	if cr.Spec.HA.Enabled {
		return nil // HA enabled, do nothing.
	}

	deploy.Spec.Template.Spec.Containers = []corev1.Container{{
		Args: []string{
			"--save",
			"",
			"--appendonly",
			"no",
		},
		Image:           getRedisContainerImage(cr),
		ImagePullPolicy: corev1.PullAlways,
		Name:            "redis",
		Ports: []corev1.ContainerPort{
			{
				ContainerPort: common.ArgoCDDefaultRedisPort,
			},
		},
		Resources: getRedisResources(cr),
	}}

	if err := controllerutil.SetControllerReference(cr, deploy, r.scheme); err != nil {
		return err
	}
	return r.client.Create(context.TODO(), deploy)
}

// reconcileRepoDeployment will ensure the Deployment resource is present for the ArgoCD Repo component.
func (r *ReconcileArgoCD) reconcileRepoDeployment(cr *argoprojv1a1.ArgoCD) error {
	deploy := newDeploymentWithSuffix("repo-server", "repo-server", cr)
	if argoutil.IsObjectFound(r.client, cr.Namespace, deploy.Name, deploy) {
		return nil // Deployment found, do nothing
	}

	automountToken := false
	deploy.Spec.Template.Spec.AutomountServiceAccountToken = &automountToken

	deploy.Spec.Template.Spec.Containers = []corev1.Container{{
		Command:         getArgoRepoCommand(cr),
		Image:           getArgoContainerImage(cr),
		ImagePullPolicy: corev1.PullAlways,
		LivenessProbe: &corev1.Probe{
			Handler: corev1.Handler{
				TCPSocket: &corev1.TCPSocketAction{
					Port: intstr.FromInt(common.ArgoCDDefaultRepoServerPort),
				},
			},
			InitialDelaySeconds: 5,
			PeriodSeconds:       10,
		},
		Name: "argocd-repo-server",
		Ports: []corev1.ContainerPort{
			{
				ContainerPort: common.ArgoCDDefaultRepoServerPort,
				Name:          "server",
			}, {
				ContainerPort: common.ArgoCDDefaultRepoMetricsPort,
				Name:          "metrics",
			},
		},
		ReadinessProbe: &corev1.Probe{
			Handler: corev1.Handler{
				TCPSocket: &corev1.TCPSocketAction{
					Port: intstr.FromInt(common.ArgoCDDefaultRepoServerPort),
				},
			},
			InitialDelaySeconds: 5,
			PeriodSeconds:       10,
		},
		Resources: getArgoRepoResources(cr),
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "ssh-known-hosts",
				MountPath: "/app/config/ssh",
			}, {
				Name:      "tls-certs",
				MountPath: "/app/config/tls",
			},
		},
	}}

	deploy.Spec.Template.Spec.Volumes = []corev1.Volume{
		{
			Name: "ssh-known-hosts",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: common.ArgoCDKnownHostsConfigMapName,
					},
				},
			},
		}, {
			Name: "tls-certs",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: common.ArgoCDTLSCertsConfigMapName,
					},
				},
			},
		},
	}

	if err := controllerutil.SetControllerReference(cr, deploy, r.scheme); err != nil {
		return err
	}
	return r.client.Create(context.TODO(), deploy)
}

// reconcileServerDeployment will ensure the Deployment resource is present for the ArgoCD Server component.
func (r *ReconcileArgoCD) reconcileServerDeployment(cr *argoprojv1a1.ArgoCD) error {
	deploy := newDeploymentWithSuffix("server", "server", cr)
	if argoutil.IsObjectFound(r.client, cr.Namespace, deploy.Name, deploy) {
		return nil // Deployment found, do nothing
	}

	deploy.Spec.Template.Spec.Containers = []corev1.Container{{
		Command:         getArgoServerCommand(cr),
		Image:           getArgoContainerImage(cr),
		ImagePullPolicy: corev1.PullAlways,
		LivenessProbe: &corev1.Probe{
			Handler: corev1.Handler{
				HTTPGet: &corev1.HTTPGetAction{
					Path: "/healthz",
					Port: intstr.FromInt(8080),
				},
			},
			InitialDelaySeconds: 3,
			PeriodSeconds:       30,
		},
		Name: "argocd-server",
		Ports: []corev1.ContainerPort{
			{
				ContainerPort: 8080,
			}, {
				ContainerPort: 8083,
			},
		},
		ReadinessProbe: &corev1.Probe{
			Handler: corev1.Handler{
				HTTPGet: &corev1.HTTPGetAction{
					Path: "/healthz",
					Port: intstr.FromInt(8080),
				},
			},
			InitialDelaySeconds: 3,
			PeriodSeconds:       30,
		},
		Resources: getArgoServerResources(cr),
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "ssh-known-hosts",
				MountPath: "/app/config/ssh",
			}, {
				Name:      "tls-certs",
				MountPath: "/app/config/tls",
			},
		},
	}}

	deploy.Spec.Template.Spec.ServiceAccountName = "argocd-server"

	deploy.Spec.Template.Spec.Volumes = []corev1.Volume{
		{
			Name: "ssh-known-hosts",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: common.ArgoCDKnownHostsConfigMapName,
					},
				},
			},
		}, {
			Name: "tls-certs",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: common.ArgoCDTLSCertsConfigMapName,
					},
				},
			},
		},
	}

	if err := controllerutil.SetControllerReference(cr, deploy, r.scheme); err != nil {
		return err
	}
	return r.client.Create(context.TODO(), deploy)
}
