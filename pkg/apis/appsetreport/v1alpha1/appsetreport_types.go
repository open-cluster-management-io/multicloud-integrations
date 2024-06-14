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

// MulticlusterApplicationSetReport provides a report of the status of an application from all the managed clusters
// where the application is deployed on. It provides a summary of the number of clusters in the various states.
// If an error or warning occurred when installing the application on a managed cluster, the conditions, including
// the waring and error message, is captured in the report.
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope="Namespaced"
// +kubebuilder:resource:shortName=appsetreport;appsetreports
type MulticlusterApplicationSetReport struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	Statuses AppConditions `json:"statuses,omitempty"`
}

// MulticlusterApplicationSetReportList contains a list of MulticlusterApplicationSetReports.
// +kubebuilder:object:root=true
type MulticlusterApplicationSetReportList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []MulticlusterApplicationSetReport `json:"items"`
}

// ResourceRef identifies the resource that is deployed by the application.
type ResourceRef struct {
	APIVersion string `json:"apiVersion,omitempty"`
	Kind       string `json:"kind,omitempty"`
	Name       string `json:"name,omitempty"`
	Namespace  string `json:"namespace,omitempty"`
}

// Condition defines a type of error/warning
type Condition struct {
	// Type identifies if the condition is a warning or an error.
	Type string `json:"type,omitempty"`

	// Message is the warning/error message associated with this condition.
	Message string `json:"message,omitempty"`
}

// ClusterCondition defines all the error/warning conditions in one cluster for an application.
type ClusterCondition struct {
	Cluster      string      `json:"cluster,omitempty"`
	SyncStatus   string      `json:"syncStatus,omitempty"`
	HealthStatus string      `json:"healthStatus,omitempty"`
	App          string      `json:"app,omitempty"`
	Conditions   []Condition `json:"conditions,omitempty"`
}

// AppConditions defines all the error/warning conditions in all clusters where a particular application is deployed.
type AppConditions struct {
	// +optional
	Resources []ResourceRef `json:"resources,omitempty"`

	// +optional
	ClusterConditions []ClusterCondition `json:"clusterConditions,omitempty"`

	// +optional
	Summary ReportSummary `json:"summary,omitempty"`
}

// ReportSummary provides a summary by providing a count of the total number of clusters where the application is
// deployed. It also provides a count of how many clusters where an application are in the following states:
// synced, notSynced, healthy, notHealthy, and inProgress.
type ReportSummary struct {

	// Synced provides the count of synced applications.
	// +optional
	Synced string `json:"synced"`

	// NotSynced provides the count of the out of sync applications.
	// +optional
	NotSynced string `json:"notSynced"`

	// Healthy provides the count of healthy applications.
	// +optional
	Healthy string `json:"healthy"`

	// NotHealthy provides the count of non-healthy applications.
	// +optional
	NotHealthy string `json:"notHealthy"`

	// InProgress provides the count of applications that are in the process of being deployed.
	// +optional
	InProgress string `json:"inProgress"`

	// Clusters provides the count of all managed clusters the application is deployed.
	// +optional
	Clusters string `json:"clusters"`
}

func init() {
	SchemeBuilder.Register(&MulticlusterApplicationSetReport{}, &MulticlusterApplicationSetReportList{})
}
