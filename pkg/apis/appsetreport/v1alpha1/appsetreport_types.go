/*
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

// +kubebuilder:object:root=true

// MulticlusterApplicationSetReport is the Schema for the MulticlusterApplicationSetReport API.
// +kubebuilder:resource:scope="Namespaced"
// +kubebuilder:resource:shortName=appsetreport;appsetreports
type MulticlusterApplicationSetReport struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	Statuses AppConditions `json:"statuses,omitempty"`
}

// +kubebuilder:object:root=true

// MulticlusterApplicationSetReportList contains a list of MulticlusterApplicationSetReport.
type MulticlusterApplicationSetReportList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []MulticlusterApplicationSetReport `json:"items"`
}

// ResourceRef defines a kind of resource
type ResourceRef struct {
	APIVersion string `json:"apiVersion,omitempty"`
	Kind       string `json:"kind,omitempty"`
	Name       string `json:"name,omitempty"`
	Namespace  string `json:"namespace,omitempty"`
}

// Condition defines a type of error/warning
type Condition struct {
	Type    string `json:"type,omitempty"`
	Message string `json:"message,omitempty"`
}

// ClusterCondition defines all the error/warning conditions in one cluster per application
type ClusterCondition struct {
	Cluster      string      `json:"cluster,omitempty"`
	SyncStatus   string      `json:"syncStatus,omitempty"`
	HealthStatus string      `json:"healthStatus,omitempty"`
	Conditions   []Condition `json:"conditions,omitempty"`
}

// AppConditions defines all the error/warning conditions in all clusters per application
type AppConditions struct {
	// +optional
	Resources []ResourceRef `json:"resources,omitempty"`

	// +optional
	ClusterConditions []ClusterCondition `json:"clusterConditions,omitempty"`

	// Summary provides a summary of results
	// +optional
	Summary ReportSummary `json:"summary,omitempty"`
}

type ReportSummary struct {

	// Synced provides the count of synced applications
	// +optional
	Synced string `json:"synced"`

	// NotSynced provides the count of the out of sync applications
	// +optional
	NotSynced string `json:"notSynced"`

	// Healthy provides the count of healthy applications
	// +optional
	Healthy string `json:"healthy"`

	// NotHealthy provides the count of non-healthy applications
	// +optional
	NotHealthy string `json:"notHealthy"`

	// InProgress provides the count of applications that are in the process of being deployed
	// +optional
	InProgress string `json:"inProgress"`

	// Clusters provides the count of all managed clusters the application is deployed to
	// +optional
	Clusters string `json:"clusters"`
}

func init() {
	SchemeBuilder.Register(&MulticlusterApplicationSetReport{}, &MulticlusterApplicationSetReportList{})
}
