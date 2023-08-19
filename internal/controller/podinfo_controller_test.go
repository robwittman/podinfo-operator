package controller

import (
	"context"
	"errors"
	"github.com/go-logr/logr"
	"github.com/golang/mock/gomock"
	helmclient "github.com/mittwald/go-helm-client"
	mockhelmclient "github.com/mittwald/go-helm-client/mock"
	appsv1alpha1 "github.com/robwittman/podinfo-operator/api/v1alpha1"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/release"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"reflect"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"testing"
)

func TestPodInfoReconciler_deploymentNeedsUpdate(t *testing.T) {
	defaultDeployment := generatedDeployment("ghcr.io/stefanprodan/podinfo:latest", 2, map[string]string{
		"PODINFO_UI_COLOR":   "#34577c",
		"PODINFO_UI_MESSAGE": "some string",
	}, v1.ResourceRequirements{
		Limits: v1.ResourceList{
			"memory": resource.MustParse("64Mi"),
		},
		Requests: v1.ResourceList{
			"cpu": resource.MustParse("100m"),
		},
	})

	tests := []struct {
		name     string
		current  *appsv1.Deployment
		podInfo  *appsv1alpha1.PodInfo
		expected bool
	}{
		{
			name:    "Default podinfo / deployment shows no changes",
			current: defaultDeployment,
			podInfo: generatePodInfo(appsv1alpha1.PodInfoUi{
				Message: "some string",
				Color:   "#34577c",
			}, appsv1alpha1.PodInfoResources{
				CpuRequest:  "100m",
				MemoryLimit: "64Mi",
			}, appsv1alpha1.PodInfoImage{
				Tag:        "latest",
				Repository: "ghcr.io/stefanprodan/podinfo",
			}, 2, "17.5.4"),
			expected: false,
		},
		{
			name:    "Custom CPU request requires update",
			current: defaultDeployment,
			podInfo: generatePodInfo(appsv1alpha1.PodInfoUi{
				Message: "some string",
				Color:   "#34577c",
			}, appsv1alpha1.PodInfoResources{
				CpuRequest:  "200m",
				MemoryLimit: "64Mi",
			}, appsv1alpha1.PodInfoImage{
				Tag:        "latest",
				Repository: "ghcr.io/stefanprodan/podinfo",
			}, 2, "17.5.4"),
			expected: true,
		},
		{
			name:    "Image tag change requires update",
			current: defaultDeployment,
			podInfo: generatePodInfo(appsv1alpha1.PodInfoUi{
				Message: "some string",
				Color:   "#34577c",
			}, appsv1alpha1.PodInfoResources{
				CpuRequest:  "100m",
				MemoryLimit: "64Mi",
			}, appsv1alpha1.PodInfoImage{
				Tag:        "v1.0.0",
				Repository: "ghcr.io/stefanprodan/podinfo",
			}, 2, "17.5.4"),
			expected: true,
		},
		{
			name:    "UI changes requires update",
			current: defaultDeployment,
			podInfo: generatePodInfo(appsv1alpha1.PodInfoUi{
				Message: "some new string",
				Color:   "#34577c",
			}, appsv1alpha1.PodInfoResources{
				CpuRequest:  "100m",
				MemoryLimit: "64Mi",
			}, appsv1alpha1.PodInfoImage{
				Tag:        "latest",
				Repository: "ghcr.io/stefanprodan/podinfo",
			}, 2, "17.5.4"),
			expected: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			check := deploymentNeedsUpdate(tt.current, tt.podInfo)
			if check != tt.expected {
				t.Errorf("deploymentNeedsUpdate() got = %v, want %v", check, tt.expected)
			}
		})
	}
}

func TestPodInfoReconciler_releaseNeedsUpdate(t *testing.T) {
	tests := []struct {
		name     string
		current  *release.Release
		podInfo  *appsv1alpha1.PodInfo
		expected bool
	}{
		{
			name: "Helm releases matching spec requires no update",
			current: &release.Release{
				Chart: &chart.Chart{
					Metadata: &chart.Metadata{
						Version: "17.5.4",
					},
				},
			},
			podInfo: generatePodInfo(appsv1alpha1.PodInfoUi{
				Message: "some string",
				Color:   "#34577c",
			}, appsv1alpha1.PodInfoResources{
				CpuRequest:  "100m",
				MemoryLimit: "64Mi",
			}, appsv1alpha1.PodInfoImage{
				Tag:        "v1.0.0",
				Repository: "ghcr.io/stefanprodan/podinfo",
			}, 2, "17.5.4"),
			expected: false,
		},
		{
			name: "Change in helm chart version requires update",
			current: &release.Release{
				Chart: &chart.Chart{
					Metadata: &chart.Metadata{
						Version: "17.5.4",
					},
				},
			},
			podInfo: generatePodInfo(appsv1alpha1.PodInfoUi{
				Message: "some string",
				Color:   "#34577c",
			}, appsv1alpha1.PodInfoResources{
				CpuRequest:  "100m",
				MemoryLimit: "64Mi",
			}, appsv1alpha1.PodInfoImage{
				Tag:        "v1.0.0",
				Repository: "ghcr.io/stefanprodan/podinfo",
			}, 2, "17.5.5"),
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			check := releaseNeedsUpdate(tt.current, tt.podInfo)
			if check != tt.expected {
				t.Errorf("releaseNeedsUpdate() got = %v, want %v", check, tt.expected)
			}
		})
	}
}

func TestPodInfoReconciler_podInfoService(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = v1.AddToScheme(scheme)
	_ = appsv1alpha1.AddToScheme(scheme)
	c := fake.NewFakeClientWithScheme(scheme)
	r := &PodInfoReconciler{Client: c, Scheme: scheme}

	input := generatePodInfo(appsv1alpha1.PodInfoUi{
		Message: "some string",
		Color:   "#34577c",
	}, appsv1alpha1.PodInfoResources{
		CpuRequest:  "100m",
		MemoryLimit: "64Mi",
	}, appsv1alpha1.PodInfoImage{
		Tag:        "v1.0.0",
		Repository: "ghcr.io/stefanprodan/podinfo",
	}, 2, "17.5.5")

	svc := r.podInfoService(input)
	expected := generateLabels(input)
	if !reflect.DeepEqual(svc.ObjectMeta.Labels, generateLabels(input)) {
		t.Errorf("podInfoService() check labels, got = %v, want %v", svc.ObjectMeta.Labels, expected)
	}
}

func TestPodInfoReconciler_isReleaseNotFoundError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "Not found error matches",
			err:      errors.New("release: not found"),
			expected: true,
		},
		{
			name:     "Unrelated error does not match",
			err:      errors.New("Some other helm error"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			check := isReleaseNotFoundError(tt.err)
			if check != tt.expected {
				t.Errorf("isReleaseNotFoundError(), got %v; expected %v", check, tt.expected)
			}
		})
	}
}
func TestPodInfoReconciler_podInfoDeployment(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = v1.AddToScheme(scheme)
	_ = appsv1alpha1.AddToScheme(scheme)
	c := fake.NewFakeClientWithScheme(scheme)
	r := &PodInfoReconciler{Client: c, Scheme: scheme}

	input := generatePodInfo(appsv1alpha1.PodInfoUi{
		Message: "some string",
		Color:   "#34577c",
	}, appsv1alpha1.PodInfoResources{
		CpuRequest:  "100m",
		MemoryLimit: "64Mi",
	}, appsv1alpha1.PodInfoImage{
		Tag:        "v1.0.0",
		Repository: "ghcr.io/stefanprodan/podinfo",
	}, 2, "17.5.5")

	dep := r.podInfoDeployment(input)
	expected := generateLabels(input)
	if !reflect.DeepEqual(dep.ObjectMeta.Labels, generateLabels(input)) {
		t.Errorf("podInfoDeployment() check labels, got = %v, want %v", dep.ObjectMeta.Labels, expected)
	}

	if !reflect.DeepEqual(dep.Spec.Template.Labels, dep.Spec.Selector.MatchLabels) {
		t.Errorf("podInfoDeployment() selector labels invalid, got = %v, want %v", dep.Spec.Selector.MatchLabels, dep.Spec.Template.Labels)
	}
}

func TestPodInfoReconciler_reconcileHelmReleaseInstallsChart(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = v1.AddToScheme(scheme)
	_ = appsv1alpha1.AddToScheme(scheme)
	c := fake.NewFakeClientWithScheme(scheme)
	r := &PodInfoReconciler{Client: c, Scheme: scheme}

	l, _ := logr.FromContext(context.TODO())
	input := defaultPodInfo()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockClient := mockhelmclient.NewMockClient(ctrl)
	if mockClient == nil {
		t.Fail()
	}

	mockClient.EXPECT().GetRelease(input.Name).Return(nil, errors.New("release: not found"))
	mockClient.EXPECT().InstallChart(context.TODO(), &helmclient.ChartSpec{
		ReleaseName: input.Name,
		ChartName:   "oci://registry-1.docker.io/bitnamicharts/redis",
		Namespace:   input.Namespace,
		//Version:     input.Spec.Redis.Version,
		ValuesYaml: defaultRedisValues,
	}, nil)

	_, _, err := r.reconcileHelmRelease(mockClient, l, input)
	if err != nil {
		t.Error(err)
	}
}

func TestPodInfoReconciler_reconcileHelmReleaseRequeuesIfNotDeployed(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = v1.AddToScheme(scheme)
	_ = appsv1alpha1.AddToScheme(scheme)
	input := defaultPodInfo()
	c := fake.NewFakeClientWithScheme(scheme, input)
	r := &PodInfoReconciler{Client: c, Scheme: scheme}

	l, _ := logr.FromContext(context.TODO())

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockClient := mockhelmclient.NewMockClient(ctrl)
	if mockClient == nil {
		t.Fail()
	}

	mockClient.EXPECT().GetRelease(input.Name).Return(&release.Release{
		Info: &release.Info{
			Status: "installing",
		},
	}, nil)

	_, res, err := r.reconcileHelmRelease(mockClient, l, input)
	if err != nil {
		t.Error(err)
	}
	if res.RequeueAfter == 0 {
		t.Error("reconcileHelmRelease() expected to requeue a release not in deployed state")
	}
}

func TestPodInfoReconciler_reconcileHelmReleaseContinuesIfInstalled(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = v1.AddToScheme(scheme)
	_ = appsv1alpha1.AddToScheme(scheme)
	input := defaultPodInfo()
	c := fake.NewFakeClientWithScheme(scheme, input)
	r := &PodInfoReconciler{Client: c, Scheme: scheme}

	l, _ := logr.FromContext(context.TODO())

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockClient := mockhelmclient.NewMockClient(ctrl)
	if mockClient == nil {
		t.Fail()
	}

	mockClient.EXPECT().GetRelease(input.Name).Return(&release.Release{
		Info: &release.Info{
			Status: "deployed",
		},
		Chart: &chart.Chart{
			Metadata: &chart.Metadata{
				Version: input.Spec.Redis.Version,
			},
		},
	}, nil)

	shouldContinue, _, err := r.reconcileHelmRelease(mockClient, l, input)
	if err != nil {
		t.Error(err)
	}

	if !shouldContinue {
		t.Error("reconcileHelmRelease() expected to continue with installed release")
	}
}

func generatedDeployment(image string, replicaCount int32, env map[string]string, resources v1.ResourceRequirements) *appsv1.Deployment {
	envVars := []v1.EnvVar{}
	for key, value := range env {
		envVars = append(envVars, v1.EnvVar{
			Name:  key,
			Value: value,
		})
	}

	return &appsv1.Deployment{
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicaCount,
			Template: v1.PodTemplateSpec{
				Spec: v1.PodSpec{
					Containers: []v1.Container{{
						Name: "podinfo",
						Ports: []v1.ContainerPort{{
							ContainerPort: 9898,
							Name:          "http",
						}},
						Env:       envVars,
						Image:     image,
						Resources: resources,
					}},
				},
			},
		},
	}
}

func generatePodInfo(ui appsv1alpha1.PodInfoUi, resources appsv1alpha1.PodInfoResources, image appsv1alpha1.PodInfoImage, rc int32, redisVersion string) *appsv1alpha1.PodInfo {
	return &appsv1alpha1.PodInfo{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "test",
		},
		Spec: appsv1alpha1.PodInfoSpec{
			Redis: appsv1alpha1.PodInfoRedis{
				Enabled: true,
				Version: redisVersion,
			},
			Ui:           ui,
			Resources:    resources,
			Image:        image,
			ReplicaCount: &rc,
		},
	}
}

func defaultPodInfo() *appsv1alpha1.PodInfo {
	return generatePodInfo(appsv1alpha1.PodInfoUi{
		Message: "some string",
		Color:   "#34577c",
	}, appsv1alpha1.PodInfoResources{
		CpuRequest:  "100m",
		MemoryLimit: "64Mi",
	}, appsv1alpha1.PodInfoImage{
		Tag:        "v1.0.0",
		Repository: "ghcr.io/stefanprodan/podinfo",
	}, 2, "17.5.5")
}
