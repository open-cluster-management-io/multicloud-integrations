// Copyright 2021 The Kubernetes Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package gitopssyncresc

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"reflect"
	"testing"

	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/onsi/gomega"
	appsetreport "open-cluster-management.io/multicloud-integrations/pkg/apis/appsetreport/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const responseData1 = `
{
	"data":{
	   "searchResult":[
		  {
			 "items":[
				{
				   "_conditionOperationError":"one or more objects failed to apply",
				   "_hostingResource":"ApplicationSet/openshift-gitops/nginx-app-set",
				   "apigroup":"argoproj.io",
				   "apiversion":"v1alpha1",
				   "applicationSet":"",
				   "chart":"nginx-ingress",
				   "healthStatus":"Missing",
				   "kind":"Application",
				   "kind_plural":"applications",
				   "name":"cluster1-nginx-app",
				   "namespace":"openshift-gitops",
				   "syncStatus":"OutOfSync",
				   "targetRevision":"1.41.3"
				}
			 ],
			 "related":[
				{
				   "kind":"Role",
				   "count":1,
				   "items":[
					  {
						 "apigroup":"rbac.authorization.k8s.io",
						 "apiversion":"v1",
						 "cluster":"cluster1",
						 "kind":"Role",
						 "kind_plural":"roles",
						 "name":"nginx-ingress",
						 "namespace":"helm-nginx"
					  }
				   ]
				},
				{
				   "kind":"Cluster",
				   "count":1,
				   "items":[
					  {
						 "ClusterCertificateRotated":"True",
						 "HubAcceptedManagedCluster":"True",
						 "ManagedClusterConditionAvailable":"Unknown",
						 "ManagedClusterJoined":"True",
						 "apigroup":"internal.open-cluster-management.io",
						 "cpu":"8",
						 "kind":"Cluster",
						 "kind_plural":"managedclusterinfos",
						 "name":"cluster1"
					  }
				   ]
				},
				{
				   "kind":"RoleBinding",
				   "count":1,
				   "items":[
					  {
						 "apigroup":"rbac.authorization.k8s.io",
						 "apiversion":"v1",
						 "cluster":"cluster1",
						 "kind":"RoleBinding",
						 "kind_plural":"rolebindings",
						 "name":"nginx-ingress",
						 "namespace":"helm-nginx"
					  }
				   ]
				},
				{
				   "kind":"ClusterRole",
				   "count":1,
				   "items":[
					  {
						 "apigroup":"rbac.authorization.k8s.io",
						 "apiversion":"v1",
						 "cluster":"cluster1",
						 "kind":"ClusterRole",
						 "kind_plural":"clusterroles",
						 "name":"nginx-ingress"
					  }
				   ]
				},
				{
				   "kind":"ClusterRoleBinding",
				   "count":1,
				   "items":[
					  {
						 "apigroup":"rbac.authorization.k8s.io",
						 "apiversion":"v1",
						 "cluster":"cluster1",
						 "kind":"ClusterRoleBinding",
						 "kind_plural":"clusterrolebindings",
						 "name":"nginx-ingress"
					  }
				   ]
				}
			 ],
			 "count":1
		  }
	   ]
	}
 }
`

type TestDataSender struct {
	data string
}

func (c *TestDataSender) Send(httpClient *http.Client, req *http.Request) (map[string]interface{}, error) {
	respData := make(map[string]interface{})

	if err := json.Unmarshal([]byte(c.data), &respData); err != nil {
		return respData, err
	}

	return respData, nil
}

func TestCreateOrUpdateAppSetReport(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	synResc := &GitOpsSyncResource{
		Client:      nil,
		Interval:    10,
		ResourceDir: "/tmp",
	}

	appReportsMap := make(map[string]*appsetreport.MulticlusterApplicationSetReport)

	appset1 := make(map[string]interface{})
	appset1["namespace"] = "test-NS1"
	appset1["name"] = "app1"
	appset1["_hostingResource"] = "ApplicationSet/gitops/appset1"
	appset1["apigroup"] = "argoproj.io"
	appset1["apiversion"] = "v1alpha1"
	appset1["_uid"] = "cluster1/abc"
	appset1["_conditionSyncError"] = "blah reason: something's not right..."
	appset1["_conditionOperationError"] = "ah reason: something's not right ok ..."
	appset1["_conditionSharedResourceWarning"] = "I think it crashed"

	appset1Resources1 := make(map[string]interface{})
	appset1Resources1["kind"] = "Service"
	appset1Resources1["apiversion"] = "v1"
	appset1Resources1["name"] = "welcome-php"
	appset1Resources1["namespace"] = "welcome-waves-and-hooks"
	appset1Resources1["cluster"] = "cluster1"

	appset1Resources2 := make(map[string]interface{})
	appset1Resources2["apigroup"] = "batch"
	appset1Resources2["apiversion"] = "v1"
	appset1Resources2["kind"] = "Job"
	appset1Resources2["name"] = "welcome-presyncjob"
	appset1Resources2["namespace"] = "welcome-waves-and-hooks"
	appset1Resources2["cluster"] = "cluster1"

	appset1Resources3 := make(map[string]interface{})
	appset1Resources3["kind"] = "Deployment"
	appset1Resources3["apiversion"] = "v1"
	appset1Resources3["name"] = "welcome-presyncjob-kcbqk"
	appset1Resources3["namespace"] = "welcome-waves-and-hooks"
	appset1Resources3["cluster"] = "cluster2"

	related1 := make(map[string]interface{})
	related1["kind"] = "Service"
	related1["items"] = []interface{}{appset1Resources1}
	related2 := make(map[string]interface{})
	related2["kind"] = "Job"
	related2["items"] = []interface{}{appset1Resources2}
	related3 := make(map[string]interface{})
	related3["kind"] = "Deployment"
	related3["items"] = []interface{}{appset1Resources3}
	appset1["related"] = []interface{}{related1, related2, related3}

	managedClustersAppNameMap := make(map[string]map[string]string)
	c1ResourceListMap := getResourceMapList(appset1["related"].([]interface{}), "cluster1")
	err := synResc.createOrUpdateAppSetReportConditions(appReportsMap, appset1, "cluster1", managedClustersAppNameMap)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(appReportsMap["gitops_appset1"]).NotTo(gomega.BeNil())
	g.Expect(appReportsMap["gitops_appset1"].GetName()).To(gomega.Equal("gitops_appset1"))
	g.Expect(len(c1ResourceListMap)).To(gomega.Equal(2))
	g.Expect(len(appReportsMap["gitops_appset1"].Statuses.ClusterConditions)).To(gomega.Equal(1))
	g.Expect(appReportsMap["gitops_appset1"].Statuses.ClusterConditions[0].Cluster).To(gomega.Equal("cluster1"))
	g.Expect(len(appReportsMap["gitops_appset1"].Statuses.ClusterConditions[0].Conditions)).To(gomega.Equal(2))

	if appReportsMap["gitops_appset1"].Statuses.ClusterConditions[0].Conditions[0].Type == "SyncError" {
		g.Expect(appReportsMap["gitops_appset1"].Statuses.ClusterConditions[0].Conditions[1].Type).To(gomega.Equal("SharedResourceWarning"))
	} else {
		g.Expect(appReportsMap["gitops_appset1"].Statuses.ClusterConditions[0].Conditions[0].Type).To(gomega.Equal("SharedResourceWarning"))
		g.Expect(appReportsMap["gitops_appset1"].Statuses.ClusterConditions[0].Conditions[1].Type).To(gomega.Equal("SyncError"))
	}

	g.Expect(managedClustersAppNameMap["appset1"]["cluster1"], "test-NS1_app1")

	// Add to same appset from cluster2
	c2ResourceListMap := getResourceMapList(appset1["related"].([]interface{}), "cluster2")
	err = synResc.createOrUpdateAppSetReportConditions(appReportsMap, appset1, "cluster2", managedClustersAppNameMap)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(len(appReportsMap["gitops_appset1"].Statuses.ClusterConditions)).To(gomega.Equal(2))
	g.Expect(len(c2ResourceListMap)).To(gomega.Equal(1))
	g.Expect(appReportsMap["gitops_appset1"].Statuses.ClusterConditions[0].Cluster).To(gomega.Equal("cluster1"))
	g.Expect(len(appReportsMap["gitops_appset1"].Statuses.ClusterConditions[0].Conditions)).To(gomega.Equal(2))
	g.Expect(appReportsMap["gitops_appset1"].Statuses.ClusterConditions[1].Cluster).To(gomega.Equal("cluster2"))
	g.Expect(len(appReportsMap["gitops_appset1"].Statuses.ClusterConditions[1].Conditions)).To(gomega.Equal(2))
	g.Expect(managedClustersAppNameMap["appset1"]["cluster2"], "test-NS1_app1")
}

func TestGitOpsSyncResource_getSearchURL(t *testing.T) {
	searchSvc := getSearchSvc()

	tests := []struct {
		name    string
		service *corev1.Service
		want    string
		wantErr bool
	}{
		{
			name:    "Search service does not exist",
			service: nil,
			wantErr: true,
		},
		{
			name:    "Search service exists",
			service: searchSvc,
			want:    fmt.Sprintf("https://%v.%v.svc.cluster.local:%v/searchapi/graphql", SearchServiceName, SearchDefaultNs, 8080),
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := initClient()
			r := &GitOpsSyncResource{
				Client:      c,
				Interval:    60,
				ResourceDir: "/tmp",
			}
			if tt.service != nil {
				if err := c.Create(context.TODO(), tt.service, &client.CreateOptions{}); err != nil {
					t.Errorf("GitOpsSyncResource.getSearchURL() error creating service = %v", err)
					return
				}
			}

			got, err := r.getSearchURL()
			if (err != nil) != tt.wantErr {
				t.Errorf("GitOpsSyncResource.getSearchURL() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("GitOpsSyncResource.getSearchURL() = %v, want %v", got, tt.want)
			}

			if tt.service != nil {
				c.Delete(context.TODO(), tt.service)
			}
		})
	}
}

func TestGitOpsSyncResource_syncResources(t *testing.T) {
	c := initClient()
	report1 := getReport()
	searchSvc := getSearchSvc()

	if err := c.Create(context.TODO(), searchSvc, &client.CreateOptions{}); err != nil {
		t.Errorf("GitOpsSyncResource.getSearchURL() error creating service = %v", err)
		return
	}

	m1 := &clusterv1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster1"},
	}

	tests := []struct {
		name           string
		managedcluster *clusterv1.ManagedCluster
		data           string
		wantReportfile string
		wantReport     *appsetreport.MulticlusterApplicationSetReport
		wantErr        bool
	}{
		{
			name:           "No managed cluster",
			data:           responseData1,
			wantReportfile: "openshift-gitops_nginx-app-set.yaml",
		},
		{
			name:           "One managed cluster",
			managedcluster: m1,
			data:           responseData1,
			wantReportfile: "openshift-gitops_nginx-app-set.yaml",
			wantReport:     report1,
			wantErr:        false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &GitOpsSyncResource{
				Client:      c,
				Interval:    60,
				ResourceDir: "/tmp",
				DataSender:  &TestDataSender{tt.data},
			}
			if tt.managedcluster != nil {
				if err := c.Create(context.TODO(), tt.managedcluster, &client.CreateOptions{}); err != nil {
					t.Errorf("GitOpsSyncResource.syncResources() error creating managed cluster = %v", err)
					return
				}
			}

			reportfilePath := "/tmp/" + tt.wantReportfile
			if tt.wantReportfile != "" {
				if _, err := os.Stat(reportfilePath); err == nil {
					os.Remove(reportfilePath)
				}
			}

			if err := r.syncResources(); (err != nil) != tt.wantErr {
				t.Errorf("GitOpsSyncResource.syncResources() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantReport != nil {
				if _, err := os.Stat(reportfilePath); err != nil {
					t.Errorf("GitOpsSyncResource.syncResources() reportfile  %v not found, error = %v", reportfilePath, err)
					return
				}

				data, err := ioutil.ReadFile(reportfilePath)
				if err != nil {
					t.Errorf("GitOpsSyncResource.syncResources() error reading reportfile %v, error = %v", reportfilePath, err)
					return
				}

				report := &appsetreport.MulticlusterApplicationSetReport{}
				if err := yaml.Unmarshal(data, &report); err != nil {
					t.Errorf("GitOpsSyncResource.syncResources() error reading reportfile %v, error = %v", reportfilePath, err)
					return
				}

				if !reflect.DeepEqual(report.Statuses.Resources, tt.wantReport.Statuses.Resources) {
					t.Errorf("GitOpsSyncResource.syncResources() mismatch of statuses.resources in report, got = %v, want = %v",
						report.Statuses.Resources, tt.wantReport.Statuses.Resources)
					return
				}

				if !reflect.DeepEqual(report.Statuses.ClusterConditions, tt.wantReport.Statuses.ClusterConditions) {
					t.Errorf("GitOpsSyncResource.syncResources() mismatch of statuses.clusterconditions in report, got = %v, want = %v",
						report.Statuses.ClusterConditions, tt.wantReport.Statuses.ClusterConditions)
				}
			} else {
				if _, err := os.Stat(reportfilePath); err == nil {
					t.Errorf("GitOpsSyncResource.syncResources() found reportfile  %v, but it's expected not to exist",
						reportfilePath)
					return
				}
			}
		})
	}
}

func getSearchSvc() *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      SearchServiceName,
			Namespace: SearchDefaultNs,
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{{TargetPort: intstr.IntOrString{IntVal: 8080}}},
		},
	}
}

func getReport() *appsetreport.MulticlusterApplicationSetReport {
	return &appsetreport.MulticlusterApplicationSetReport{
		Statuses: appsetreport.AppConditions{
			Resources: []appsetreport.ResourceRef{
				{
					APIVersion: "rbac.authorization.k8s.io/v1",
					Kind:       "Role",
					Name:       "nginx-ingress",
					Namespace:  "helm-nginx",
				},
				{
					APIVersion: "rbac.authorization.k8s.io/v1",
					Kind:       "RoleBinding",
					Name:       "nginx-ingress",
					Namespace:  "helm-nginx",
				},
				{
					APIVersion: "rbac.authorization.k8s.io/v1",
					Kind:       "ClusterRole",
					Name:       "nginx-ingress",
				},
				{
					APIVersion: "rbac.authorization.k8s.io/v1",
					Kind:       "ClusterRoleBinding",
					Name:       "nginx-ingress",
				},
			},
			ClusterConditions: []appsetreport.ClusterCondition{
				{
					Cluster: "cluster1",
					Conditions: []appsetreport.Condition{
						{
							Type:    "OperationError",
							Message: "one or more objects failed to apply",
						},
					},
				},
			},
		},
	}
}

func initClient() client.Client {
	ncb := fake.NewClientBuilder()
	return ncb.Build()
}
