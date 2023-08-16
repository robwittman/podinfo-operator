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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// PodInfoSpec defines the desired state of PodInfo
type PodInfoSpec struct {
	ReplicaCount *int32           `json:"replicaCount,omitempty"`
	Resources    PodInfoResources `json:"resources,omitempty"`
	Image        PodInfoImage     `json:"image,omitempty"`
	Ui           PodInfoUi        `json:"ui,omitempty"`
	Redis        PodInfoRedis     `json:"redis,omitempty"`
}

type PodInfoResources struct {
	MemoryLimit string `json:"memoryLimit,omitempty"`
	CpuRequest  string `json:"cpuRequest,omitempty"`
}

type PodInfoImage struct {
	Repository string `json:"repository,omitempty"`
	Tag        string `json:"tag,omitempty"`
}

type PodInfoUi struct {
	Color   string `json:"color,omitempty"`
	Message string `json:"message,omitempty"`
}

type PodInfoRedis struct {
	Enabled bool   `json:"enabled,omitempty"`
	Version string `json:"version,omitempty"`
}

// PodInfoStatus defines the observed state of PodInfo
type PodInfoStatus struct {
	//Redis PodInfoRedisStatus `json:"redis,omitempty"`
}

//type PodInfoHelmReleaseStatus struct {
//	Revision  int    `json:"revision,omitempty"`
//	State     string `json:"state,omitempty"`
//	Namespace string `json:"namespace,omitempty"`
//	Name      string `json:"name,omitempty"`
//}

//type PodInfoRedisStatus struct {
//	PodInfoHelmReleaseStatus
//}
//
//type PodInfoReleaseStatus struct {
//	PodInfoHelmReleaseStatus
//}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// PodInfo is the Schema for the podinfoes API
type PodInfo struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PodInfoSpec   `json:"spec,omitempty"`
	Status PodInfoStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// PodInfoList contains a list of PodInfo
type PodInfoList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PodInfo `json:"items"`
}

func init() {
	SchemeBuilder.Register(&PodInfo{}, &PodInfoList{})
}
