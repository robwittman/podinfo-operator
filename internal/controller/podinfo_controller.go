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
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"time"

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
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=get;list;watch;create;update;patch;delete

// Just deploy a single replica, without authentication
var defaultRedisValues = `
  architecture: standalone
  auth:
    enabled: false
`

const redisFinalizer = "apps.podinfo.io/finalizer"

const (
	podInfoAvailable = "Available"
)

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

	// Reconcile the helm release. If the reconciliation fails,
	// or if there was an action taken, we want to requeue to
	// ensure the redis helm chart is installed before continuing
	if podInfo.Spec.Redis.Enabled {
		shouldContinue, result, err := r.reconcileHelmRelease(helmClient, contextLogger, podInfo)
		if err != nil || !shouldContinue {
			return result, err
		}

	} else {
		rel, _ := helmClient.GetRelease(podInfo.Name)
		if rel != nil {
			r.uninstallRedis(contextLogger, podInfo, helmClient)
			controllerutil.RemoveFinalizer(podInfo, redisFinalizer)
			err := r.Update(ctx, podInfo)
			if err != nil {
				return ctrl.Result{}, err
			}
		}
	}

	// This could honestly be just another helm install, but in the spirit of
	// exercising the SDK a bit, we'll create a deployment / service / ingress
	// manually
	deployment := &appsv1.Deployment{}
	err = r.Get(ctx, types.NamespacedName{Name: podInfo.Name, Namespace: podInfo.Namespace}, deployment)
	if err != nil && errors.IsNotFound(err) {
		// Define a new deployment
		dep := r.podInfoDeployment(podInfo)
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

	meta.SetStatusCondition(&podInfo.Status.Conditions, metav1.Condition{Type: podInfoAvailable,
		Status: metav1.ConditionTrue, Reason: "Reconciling",
		Message: fmt.Sprintf("Podinfo deployment for (%s) created successfully", podInfo.Name)})

	if err := r.Status().Update(ctx, podInfo); err != nil {
		contextLogger.Error(err, "Failed to update podInfo status")
		return ctrl.Result{}, err
	}

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
	// Ideally, this would check the installed version against the requested
	// version in the CRD spec. However, there were some issues configuring
	// go-helm-client to respect the version spec. Something I would
	// dig into given more time

	//if current.Chart.Metadata.Version != podInfo.Spec.Redis.Version {
	//	return true
	//}
	return false
}

func deploymentNeedsUpdate(current *appsv1.Deployment, podInfo *appsv1alpha1.PodInfo) bool {
	container := current.Spec.Template.Spec.Containers[0]
	if fmt.Sprintf(
		"%s:%s",
		podInfo.Spec.Image.Repository,
		podInfo.Spec.Image.Tag,
	) != container.Image {
		return true
	}

	if *podInfo.Spec.ReplicaCount != *current.Spec.Replicas {
		return true
	}

	resources := podInfo.Spec.Resources
	if resources.MemoryLimit != container.Resources.Limits.Memory().String() {
		return true
	}

	if resources.CpuRequest != container.Resources.Requests.Cpu().String() {
		return true
	}

	ui := podInfo.Spec.Ui
	for _, env := range container.Env {
		if env.Name == "PODINFO_UI_COLOR" && env.Value != ui.Color {
			return true
		}
		if env.Name == "PODINFO_UI_MESSAGE" && env.Value != ui.Message {
			return true
		}
	}

	return false
}

// This is a pretty gross way to check if the helm release
// errors are "not found". Helm may expose a cleaner way
// to do so, ala `errors.IsNotFound(err), but this is what
// we'll use for now
func isReleaseNotFoundError(err error) bool {
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

func (r *PodInfoReconciler) podInfoDeployment(podInfo *appsv1alpha1.PodInfo) *appsv1.Deployment {
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

// reconcileHelmRelease ensures that the required Redis helm chart is
// running in the cluster for use by podinfo
func (r *PodInfoReconciler) reconcileHelmRelease(helmClient helmclient.Client, contextLogger logr.Logger, podInfo *appsv1alpha1.PodInfo) (bool, reconcile.Result, error) {
	ctx := context.TODO()
	rel, err := helmClient.GetRelease(podInfo.Name)
	if err != nil {
		if isReleaseNotFoundError(err) {
			contextLogger.Info("Release not found, installing now")
			_, err := helmClient.InstallChart(ctx, &helmclient.ChartSpec{
				ReleaseName: podInfo.Name,
				ChartName:   podInfo.Spec.Redis.Registry,
				Namespace:   podInfo.Namespace,
				//Version:    podInfo.Spec.Redis.Version, // TODO: Can't seem to find specific versions
				ValuesYaml: defaultRedisValues,
			}, nil)
			if err != nil {
				contextLogger.Error(err, "Failed installing helm release")
				return false, ctrl.Result{}, err
			}
			return false, ctrl.Result{Requeue: true}, nil

		} else {
			contextLogger.Error(err, "Failed to check for the redis release", "namespace", podInfo.Namespace, "name", podInfo.Name)
			return false, ctrl.Result{}, err
		}
	}

	// Ensure we attach our finalizer, so we can clean up the redis helm install
	if !controllerutil.ContainsFinalizer(podInfo, redisFinalizer) {
		controllerutil.AddFinalizer(podInfo, redisFinalizer)
		err = r.Update(ctx, podInfo)
		if err != nil {
			return false, ctrl.Result{}, err
		}
	}

	if rel.Info.Status != "deployed" {
		contextLogger.Info("Waiting for helm release to be deployed", "namespace", podInfo.Namespace, "name", podInfo.Name)
		return false, ctrl.Result{RequeueAfter: time.Second * 5}, nil
	}

	if releaseNeedsUpdate(rel, podInfo) {
		contextLogger.Info("TODO: Redis release configuration changed, updating")
	}

	return true, ctrl.Result{}, nil
}
