/*
Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"fmt"
	"github.com/go-logr/logr"
	helmclient "github.com/mittwald/go-helm-client"
	"helm.sh/helm/v3/pkg/release"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	appsv1alpha1 "github.com/robwittman/podinfo-operator/api/v1alpha1"
)

// PodInfoReconciler reconciles a PodInfo object
type PodInfoReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=apps.podinfo.io,resources=podinfoes,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=apps.podinfo.io,resources=podinfoes/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=apps.podinfo.io,resources=podinfoes/finalizers,verbs=update

// Just deploy a single replica, without authentication
var defaultRedisValues = `
  architecture: standalone
  auth:
    enabled: false
`

const redisFinalizer = "apps.podinfo.io/finalizer"

func (r *PodInfoReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	contextLogger := log.FromContext(ctx)

	podInfo := &appsv1alpha1.PodInfo{}
	err := r.Get(ctx, req.NamespacedName, podInfo)
	if err != nil {
		if errors.IsNotFound(err) {
			contextLogger.Info("PodInfo resource not found, must be deleted")
			return ctrl.Result{}, nil
		}
		contextLogger.Error(err, "Failed to get PodInfo")
		return ctrl.Result{}, err
	}

	helmClient, err := helmclient.New(&helmclient.Options{
		Namespace: podInfo.Namespace, // Change this to the namespace you wish the client to operate in.
	})
	if err != nil {
		contextLogger.Error(err, "Failed getting helm client")
	}

	isPodInfoMarkedToBeDeleted := podInfo.GetDeletionTimestamp() != nil
	if isPodInfoMarkedToBeDeleted {
		if controllerutil.ContainsFinalizer(podInfo, redisFinalizer) {
			if err := r.uninstallRedis(contextLogger, podInfo, helmClient); err != nil {
				return ctrl.Result{}, err
			}

			// Remove the redisFinalizer. Once all finalizers have been
			// removed, the object will be deleted.
			controllerutil.RemoveFinalizer(podInfo, redisFinalizer)
			err := r.Update(ctx, podInfo)
			if err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	rel, err := helmClient.GetRelease(podInfo.Name)
	if err != nil {
		if isReleaseNotFoundError(err) {
			contextLogger.Info("Release not found, installing now")
			_, err := helmClient.InstallChart(ctx, &helmclient.ChartSpec{
				ReleaseName: podInfo.Name,
				ChartName:   "oci://registry-1.docker.io/bitnamicharts/redis",
				Namespace:   podInfo.Namespace,
				//Version:     podInfo.Spec.Redis.Version, // TODO: Can't seem to find specific versions
				//Wait:       true, // We probably don't want to wait in the controller
				ValuesYaml: defaultRedisValues,
			}, nil)
			if err != nil {
				contextLogger.Error(err, "Failed installing helm release")
				return ctrl.Result{}, err
			}
			return ctrl.Result{Requeue: true}, nil

		} else {
			contextLogger.Error(err, "Failed to check for the redis release", "namespace", podInfo.Namespace, "name", podInfo.Name)
			return ctrl.Result{}, err
		}
	}

	// Attach our finalizer, so we can clean up the redis helm install
	if !controllerutil.ContainsFinalizer(podInfo, redisFinalizer) {
		controllerutil.AddFinalizer(podInfo, redisFinalizer)
		err = r.Update(ctx, podInfo)
		if err != nil {
			return ctrl.Result{}, err
		}
	}

	// TODO: Check that the release is up to date
	if releaseNeedsUpdate(rel, podInfo) {
		contextLogger.Info("Redis release configuration changed, updating")
		// Update the helm release
	}

	// This could honestly be just another helm install, but in the spirit of
	// exercising the SDK a bit, we'll create a deployment / service / ingress
	// manually
	deployment := &appsv1.Deployment{}
	err = r.Get(ctx, types.NamespacedName{Name: podInfo.Name, Namespace: podInfo.Namespace}, deployment)
	if err != nil && errors.IsNotFound(err) {
		// Define a new deployment
		dep := r.podInfoDeployment(podInfo, rel)
		contextLogger.Info("Creating a new Deployment", "namespace", podInfo.Namespace, "name", podInfo.Name)
		err = r.Create(ctx, dep)
		if err != nil {
			contextLogger.Error(err, "Failed to create new Deployment", "namespace", podInfo.Namespace, "name", podInfo.Name)
			return ctrl.Result{}, err
		}
		// Deployment created successfully - return and requeue
		return ctrl.Result{Requeue: true}, nil
	} else if err != nil {
		contextLogger.Error(err, "Failed to get Deployment")
		return ctrl.Result{}, err
	}

	// Wire up the service
	service := &v1.Service{}
	err = r.Get(ctx, types.NamespacedName{Name: podInfo.Name, Namespace: podInfo.Namespace}, service)
	if err != nil && errors.IsNotFound(err) {
		// Define a new deployment
		svc := r.podInfoService(podInfo)
		contextLogger.Info("Creating a new service", "namespace", podInfo.Namespace, "name", podInfo.Name)
		err = r.Create(ctx, svc)
		if err != nil {
			contextLogger.Error(err, "Failed to create new service", "namespace", podInfo.Namespace, "name", podInfo.Name)
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	} else if err != nil {
		contextLogger.Error(err, "Failed to get service")
		return ctrl.Result{}, err
	}

	contextLogger.Info("Reconciliation completed")

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *PodInfoReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&appsv1alpha1.PodInfo{}).
		Owns(&appsv1.Deployment{}).
		Owns(&v1.Service{}).
		Complete(r)
}

func releaseNeedsUpdate(current *release.Release, podInfo *appsv1alpha1.PodInfo) bool {
	return false
}

// This is a pretty gross way to check if the helm release
// errors are "not found". Helm may expose a cleaner way
// to do so, ala `errors.IsNotFound(err), but this is what
// we'll use for now
func isReleaseNotFoundError(err error) bool {
	fmt.Println(err.Error())
	return err.Error() == "release: not found"
}

// Cleanup the Redis helm releases when a CRD is deleted
func (r *PodInfoReconciler) uninstallRedis(contextLogger logr.Logger, podInfo *appsv1alpha1.PodInfo, helmClient helmclient.Client) error {
	return helmClient.UninstallRelease(&helmclient.ChartSpec{
		ReleaseName: podInfo.Name,
		Namespace:   podInfo.Namespace,
		//Wait:        true, // TODO: Wait for deletion before completing. However, we were getting 'context deadline exceeded'
	})
}

func (r *PodInfoReconciler) podInfoDeployment(podInfo *appsv1alpha1.PodInfo, redis *release.Release) *appsv1.Deployment {
	labels := generateLabels(podInfo)
	replicaCount := podInfo.Spec.ReplicaCount
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podInfo.Name,
			Namespace: podInfo.Namespace,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: replicaCount,
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Template: v1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{{
						Name: "podinfo",
						Ports: []v1.ContainerPort{{
							ContainerPort: 9898,
							Name:          "http",
						}},
						Env: []v1.EnvVar{{
							Name:  "PODINFO_UI_COLOR",
							Value: podInfo.Spec.Ui.Color,
						}, {
							Name:  "PODINFO_UI_MESSAGE",
							Value: podInfo.Spec.Ui.Message,
						}, {
							Name:  "PODINFO_CACHE_SERVER",
							Value: fmt.Sprintf("tcp://%s-redis-master:6379", podInfo.Name),
						}},
						Image: fmt.Sprintf("%s:%s", podInfo.Spec.Image.Repository, podInfo.Spec.Image.Tag),
					}},
				},
			},
		},
	}

	ctrl.SetControllerReference(podInfo, deployment, r.Scheme)

	return deployment
}

func (r *PodInfoReconciler) podInfoService(podInfo *appsv1alpha1.PodInfo) *v1.Service {
	labels := generateLabels(podInfo)
	svc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podInfo.Name,
			Namespace: podInfo.Namespace,
			Labels:    labels,
		},
		Spec: v1.ServiceSpec{
			Ports: []v1.ServicePort{{
				Name: "http",
				Port: 9898,
				TargetPort: intstr.IntOrString{
					StrVal: "http",
					Type:   intstr.String,
				},
			}},
			Selector: labels,
		},
	}

	ctrl.SetControllerReference(podInfo, svc, r.Scheme)

	return svc
}

func generateLabels(podInfo *appsv1alpha1.PodInfo) map[string]string {
	return map[string]string{
		"app":         "podinfo",
		"podinfo-crd": podInfo.Name, // TODO: Can probably find a better label to use
	}
}
