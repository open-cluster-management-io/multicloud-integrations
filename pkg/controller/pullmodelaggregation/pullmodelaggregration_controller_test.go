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

package pullmodelaggregation

import (
	"context"
	"testing"
	"time"

	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	v1 "open-cluster-management.io/api/work/v1"
	appsetreportV1alpha1 "open-cluster-management.io/multicloud-integrations/pkg/apis/appsetreport/v1alpha1"

	"sigs.k8s.io/controller-runtime/pkg/manager"
)

var (
	healthy = "Healthy"
	synced  = "Synced"

	sampleManifestWork1 = &v1.ManifestWork{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "bgd-app",
			Namespace: "cluster1",
			Annotations: map[string]string{
				"apps.open-cluster-management.io/hosting-applicationset": "appset-ns-1/appset-1",
			},
			Labels: map[string]string{
				"apps.open-cluster-management.io/application-set": "true",
			},
		},
	}

	sampleManifestWork2 = &v1.ManifestWork{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "bgd-app-2",
			Namespace: "cluster1",
			Annotations: map[string]string{
				"apps.open-cluster-management.io/hosting-applicationset": "appset-ns-2/appset-2",
			},
			Labels: map[string]string{
				"apps.open-cluster-management.io/application-set": "true",
			},
		},
	}

	sampleManifestWork3 = &v1.ManifestWork{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "bgd-app-3",
			Namespace: "cluster1",
			Annotations: map[string]string{
				"apps.open-cluster-management.io/hosting-applicationset": "appset-ns-3/appset-3",
			},
			Labels: map[string]string{
				"apps.open-cluster-management.io/application-set": "true",
			},
		},
	}

	sampleManifestWork4 = &v1.ManifestWork{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "bgd-app-4",
			Namespace: "cluster1",
			Annotations: map[string]string{
				"apps.open-cluster-management.io/hosting-applicationset": "appset-ns-4/appset-4",
			},
			Labels: map[string]string{
				"apps.open-cluster-management.io/application-set": "true",
			},
		},
	}

	sampleMulticlusterApplicationSet1 = &appsetreportV1alpha1.MulticlusterApplicationSetReport{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "appset-1",
			Namespace: "appset-ns-1",
			Labels: map[string]string{
				"apps.open-cluster-management.io/hosting-applicationset": "appset-ns-1.appset-1",
			},
		},
	}

	sampleMulticlusterApplicationSet2 = &appsetreportV1alpha1.MulticlusterApplicationSetReport{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "appset-2",
			Namespace: "appset-ns-2",
			Labels: map[string]string{
				"apps.open-cluster-management.io/hosting-applicationset": "appset-ns-2.appset-2",
			},
		},
	}
)

func TestReconcilePullModel(t *testing.T) {
	g := NewGomegaWithT(t)

	// Setup the Manager and Controller
	mgr, err := manager.New(cfg, manager.Options{MetricsBindAddress: "0"})
	g.Expect(err).NotTo(HaveOccurred())

	c = mgr.GetClient()

	Add(mgr, 10, "../../../examples") // 10 second interval

	ctx, cancel := context.WithTimeout(context.TODO(), 5*time.Minute)
	mgrStopped := StartTestManager(ctx, mgr, g)

	defer func() {
		cancel()
		mgrStopped.Wait()
	}()

	g.Expect(c.Create(ctx, sampleMulticlusterApplicationSet1.DeepCopy())).NotTo(HaveOccurred())
	g.Expect(c.Create(ctx, sampleMulticlusterApplicationSet2.DeepCopy())).NotTo(HaveOccurred())

	// need to create a manifestwork
	g.Expect(c.Create(ctx, sampleManifestWork1.DeepCopy())).NotTo(HaveOccurred())
	g.Expect(c.Create(ctx, sampleManifestWork2.DeepCopy())).NotTo(HaveOccurred())
	g.Expect(c.Create(ctx, sampleManifestWork3.DeepCopy())).NotTo(HaveOccurred())
	g.Expect(c.Create(ctx, sampleManifestWork4.DeepCopy())).NotTo(HaveOccurred())

	time.Sleep(4 * time.Second)

	// Test 1: sycned and healthy with manifestwork and yaml
	mw := &v1.ManifestWork{}
	g.Expect(c.Get(ctx, types.NamespacedName{Namespace: "cluster1", Name: "bgd-app"}, mw)).NotTo(HaveOccurred())

	// Update manifestwork to be healthy & synced.
	newMw := mw.DeepCopy()
	newMw.Status = v1.ManifestWorkStatus{
		ResourceStatus: v1.ManifestResourceStatus{
			Manifests: []v1.ManifestCondition{
				{Conditions: []metav1.Condition{},
					ResourceMeta: v1.ManifestResourceMeta{},
					StatusFeedbacks: v1.StatusFeedbackResult{Values: []v1.FeedbackValue{
						{Name: "healthStatus", Value: v1.FieldValue{Type: v1.String, String: &healthy}},
						{Name: "syncStatus", Value: v1.FieldValue{Type: v1.String, String: &synced}}}},
				},
			},
		},
	}

	time.Sleep(4 * time.Second)
	g.Expect(c.Status().Update(ctx, newMw)).NotTo(HaveOccurred())
	time.Sleep(4 * time.Second)

	g.Expect(c.Get(ctx, types.NamespacedName{Namespace: "cluster1", Name: "bgd-app"}, mw)).NotTo(HaveOccurred())
	newMw = mw.DeepCopy()
	g.Expect(newMw.Status.ResourceStatus.Manifests[0].StatusFeedbacks.Values).To(Equal([]v1.FeedbackValue{
		{Name: "healthStatus", Value: v1.FieldValue{Type: v1.String, String: &healthy}},
		{Name: "syncStatus", Value: v1.FieldValue{Type: v1.String, String: &synced}}}))

	appset := &appsetreportV1alpha1.MulticlusterApplicationSetReport{}
	g.Expect(c.Get(ctx, types.NamespacedName{Name: "appset-1", Namespace: "appset-ns-1"}, appset)).NotTo(HaveOccurred())
	g.Expect(appset.Statuses.Summary).To(Equal(appsetreportV1alpha1.ReportSummary{
		Synced:     "1",
		NotSynced:  "0",
		Healthy:    "1",
		NotHealthy: "0",
		InProgress: "0",
		Clusters:   "1",
	}))
	g.Expect(appset.Statuses.Resources).To(Equal([]appsetreportV1alpha1.ResourceRef{
		{
			APIVersion: "v1",
			Kind:       "Service",
			Name:       "redis-master2",
			Namespace:  "playback-ns-2",
		},
		{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
			Name:       "redis-master2",
			Namespace:  "playback-ns-2",
		},
	}))
	g.Expect(appset.Statuses.ClusterConditions).To(Equal([]appsetreportV1alpha1.ClusterCondition{
		{
			Cluster:      "cluster1",
			SyncStatus:   "Synced",
			HealthStatus: "Healthy",
			Conditions:   []appsetreportV1alpha1.Condition{{Type: "SyncError", Message: "error message 1"}},
		},
		{
			Cluster:      "cluster3",
			SyncStatus:   "",
			HealthStatus: "",
			Conditions:   []appsetreportV1alpha1.Condition{{Type: "SyncError", Message: "error message 3"}},
		},
	}))

	// Test 2: no resource yaml but MulticlusterApplicationSetReport exists beforehand
	g.Expect(c.Get(ctx, types.NamespacedName{Name: "appset-2", Namespace: "appset-ns-2"}, appset)).NotTo(HaveOccurred())
	g.Expect(appset.Statuses.Resources).To(BeNil())
	g.Expect(appset.Statuses.ClusterConditions).To(Equal([]appsetreportV1alpha1.ClusterCondition{
		{
			Cluster:      "cluster1",
			SyncStatus:   "Unknown",
			HealthStatus: "Unknown",
			Conditions:   nil,
		},
	}))

	// Test 3: no resource yaml but MulticlusterApplicationSetReport does not exist beforehand
	g.Expect(c.Get(ctx, types.NamespacedName{Name: "appset-3", Namespace: "appset-ns-3"}, appset)).NotTo(HaveOccurred())
	g.Expect(appset.Statuses.Resources).To(BeNil())
	g.Expect(appset.Statuses.ClusterConditions).To(Equal([]appsetreportV1alpha1.ClusterCondition{
		{
			Cluster:      "cluster1",
			SyncStatus:   "Unknown",
			HealthStatus: "Unknown",
			Conditions:   nil,
		},
	}))

	time.Sleep(4 * time.Second)
	// Test 4: Update manifestwork to be synced & progressing. No existing MulticlusterApplicationSetReport
	g.Expect(c.Get(ctx, types.NamespacedName{Namespace: "cluster1", Name: "bgd-app-4"}, mw)).NotTo(HaveOccurred())

	newMw = mw.DeepCopy()
	progressing := "Progressing"
	newMw.Status = v1.ManifestWorkStatus{
		ResourceStatus: v1.ManifestResourceStatus{
			Manifests: []v1.ManifestCondition{
				{Conditions: []metav1.Condition{},
					ResourceMeta: v1.ManifestResourceMeta{},
					StatusFeedbacks: v1.StatusFeedbackResult{Values: []v1.FeedbackValue{
						{Name: "healthStatus", Value: v1.FieldValue{Type: v1.String, String: &progressing}},
						{Name: "syncStatus", Value: v1.FieldValue{Type: v1.String, String: &synced}}}},
				},
			},
		},
	}

	time.Sleep(4 * time.Second)
	g.Expect(c.Status().Update(ctx, newMw)).NotTo(HaveOccurred())
	time.Sleep(4 * time.Second)

	g.Expect(c.Get(ctx, types.NamespacedName{Namespace: "cluster1", Name: "bgd-app-4"}, mw)).NotTo(HaveOccurred())
	newMw = mw.DeepCopy()
	g.Expect(newMw.Status.ResourceStatus.Manifests[0].StatusFeedbacks.Values).To(Equal([]v1.FeedbackValue{
		{Name: "healthStatus", Value: v1.FieldValue{Type: v1.String, String: &progressing}},
		{Name: "syncStatus", Value: v1.FieldValue{Type: v1.String, String: &synced}}}))

	g.Expect(c.Get(ctx, types.NamespacedName{Name: "appset-4", Namespace: "appset-ns-4"}, appset)).NotTo(HaveOccurred())
	g.Expect(appset.Statuses.Resources).To(BeNil())
	g.Expect(appset.Statuses.ClusterConditions).To(Equal([]appsetreportV1alpha1.ClusterCondition{
		{
			Cluster:      "cluster1",
			SyncStatus:   "Synced",
			HealthStatus: "Progressing",
			Conditions:   nil,
		},
	}))
}

func TestParseNamespacedName(t *testing.T) {
	g := NewGomegaWithT(t)

	// missing "/" invalid namespace
	namespacedName := "mynamespacename"

	namespace, name := ParseNamespacedName(namespacedName)
	g.Expect(namespace).To(Equal(""))
	g.Expect(name).To(Equal(""))

	// valid namespace & name
	namespacedName = "mynamespace/name"

	namespace, name = ParseNamespacedName(namespacedName)
	g.Expect(namespace).To(Equal("mynamespace"))
	g.Expect(name).To(Equal("name"))
}
