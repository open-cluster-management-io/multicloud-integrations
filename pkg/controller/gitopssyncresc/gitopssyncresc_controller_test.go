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
	"testing"

	"github.com/onsi/gomega"
	appsetreport "open-cluster-management.io/multicloud-integrations/pkg/apis/appsetreport/v1alpha1"
)

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
	appset1["applicationSet"] = "appset1"
	appset1["apigroup"] = "argoproj.io"
	appset1["apiversion"] = "v1alpha1"
	appset1["_uid"] = "cluster1/abc"
	appset1["conditionSyncError"] = "something's not right"
	appset1["conditionSharedResourceWarning"] = "I think it crashed"

	appset1Resources1 := make(map[string]string)
	appset1Resources1["SourceUID"] = "cluster1/abc"
	appset1Resources1["SourceKind"] = "ApplicationSet"
	appset1Resources1["DestUID"] = "test-NS1/appset1-cluster1-deployment"
	appset1Resources1["DestKind"] = "apps/v1/Deployment"
	appset1Resources1["cluster"] = "cluster1"

	appset1Resources2 := make(map[string]string)
	appset1Resources2["SourceUID"] = "cluster1/abc"
	appset1Resources2["SourceKind"] = "ApplicationSet"
	appset1Resources2["DestUID"] = "test-NS1/appset1-cluster1-configmap"
	appset1Resources2["DestKind"] = "/v1/ConfigMap"
	appset1Resources2["cluster"] = "cluster1"

	appset1Resources3 := make(map[string]string)
	appset1Resources3["SourceUID"] = "cluster1/abc"
	appset1Resources3["SourceKind"] = "ApplicationSet"
	appset1Resources3["DestUID"] = "test-NS1/appset1-cluster2-configmap"
	appset1Resources3["DestKind"] = "/v1/ConfigMap"
	appset1Resources3["cluster"] = "cluster2"

	related1 := make(map[string]interface{})
	related1["kind"] = "ApplicationSet"
	related1["items"] = []map[string]string{appset1Resources1, appset1Resources2, appset1Resources3}
	appset1["related"] = []interface{}{related1}

	c1ResourceListMap := getResourceMapList(appset1["related"].([]interface{}), "cluster1")
	err := synResc.createOrUpdateAppSetReport(appReportsMap, c1ResourceListMap, appset1, "cluster1")
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(appReportsMap["test-NS1-appset1"]).NotTo(gomega.BeNil())
	g.Expect(appReportsMap["test-NS1-appset1"].GetName()).To(gomega.Equal("test-NS1-appset1"))
	g.Expect(len(appReportsMap["test-NS1-appset1"].Statuses.Resources)).To(gomega.Equal(2))
	g.Expect(len(appReportsMap["test-NS1-appset1"].Statuses.ClusterConditions)).To(gomega.Equal(1))
	g.Expect(appReportsMap["test-NS1-appset1"].Statuses.ClusterConditions[0].Cluster).To(gomega.Equal("cluster1"))
	g.Expect(len(appReportsMap["test-NS1-appset1"].Statuses.ClusterConditions[0].Conditions)).To(gomega.Equal(2))

	// Add to same appset from cluster2
	c2ResourceListMap := getResourceMapList(appset1["related"].([]interface{}), "cluster2")
	err = synResc.createOrUpdateAppSetReport(appReportsMap, c2ResourceListMap, appset1, "cluster2")
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(len(appReportsMap["test-NS1-appset1"].Statuses.Resources)).To(gomega.Equal(2))
	g.Expect(len(appReportsMap["test-NS1-appset1"].Statuses.ClusterConditions)).To(gomega.Equal(2))
	g.Expect(appReportsMap["test-NS1-appset1"].Statuses.ClusterConditions[0].Cluster).To(gomega.Equal("cluster1"))
	g.Expect(len(appReportsMap["test-NS1-appset1"].Statuses.ClusterConditions[0].Conditions)).To(gomega.Equal(2))
	g.Expect(appReportsMap["test-NS1-appset1"].Statuses.ClusterConditions[1].Cluster).To(gomega.Equal("cluster2"))
	g.Expect(len(appReportsMap["test-NS1-appset1"].Statuses.ClusterConditions[1].Conditions)).To(gomega.Equal(2))

}
