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
				"hosting-applicationset": "appset-ns-1/appset-1",
			},
			Labels: map[string]string{
				"apps.open-cluster-management.io/multicluster-application-set": "true",
			},
		},
		Status: v1.ManifestWorkStatus{
			ResourceStatus: v1.ManifestResourceStatus{
				Manifests: []v1.ManifestCondition{
					{StatusFeedbacks: v1.StatusFeedbackResult{Values: []v1.FeedbackValue{
						{Name: "healthStatus", Value: v1.FieldValue{String: &healthy}},
						{Name: "syncStatus", Value: v1.FieldValue{String: &synced}}}}},
				},
			},
		},
	}

	sampleManifestWork2 = &v1.ManifestWork{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "bgd-app-2",
			Namespace: "cluster1",
			Annotations: map[string]string{
				"hosting-applicationset": "appset-ns-2/appset-2",
			},
			Labels: map[string]string{
				"apps.open-cluster-management.io/multicluster-application-set": "true",
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
				"apps.open-cluster-management.io/hosting-applicationset": "appset-ns-1.appset-1",
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

	Add(mgr, 5) // 5 second interval

	ctx, cancel := context.WithTimeout(context.TODO(), 5*time.Minute)
	mgrStopped := StartTestManager(ctx, mgr, g)

	defer func() {
		cancel()
		mgrStopped.Wait()
	}()

	g.Expect(c.Create(ctx, sampleMulticlusterApplicationSet1)).NotTo(HaveOccurred())
	g.Expect(c.Create(ctx, sampleMulticlusterApplicationSet2)).NotTo(HaveOccurred())

	// need to create a manifestwork
	g.Expect(c.Create(ctx, sampleManifestWork1)).NotTo(HaveOccurred())
	g.Expect(c.Create(ctx, sampleManifestWork2)).NotTo(HaveOccurred())

	time.Sleep(4 * time.Second)

	// Test 1:
	mw := &v1.ManifestWork{}
	g.Expect(c.Get(ctx, types.NamespacedName{Namespace: "cluster1", Name: "bgd-app"}, mw)).NotTo(HaveOccurred())

	// Need to update the status
	mw.Status = v1.ManifestWorkStatus{
		ResourceStatus: v1.ManifestResourceStatus{
			Manifests: []v1.ManifestCondition{
				{Conditions: []metav1.Condition{},
					ResourceMeta: v1.ManifestResourceMeta{},
					StatusFeedbacks: v1.StatusFeedbackResult{Values: []v1.FeedbackValue{
						{Name: "healthStatus", Value: v1.FieldValue{String: &healthy}},
						{Name: "syncStatus", Value: v1.FieldValue{String: &synced}}}},
				},
			},
		},
	}

	g.Expect(c.Status().Update(ctx, sampleManifestWork1)).NotTo(HaveOccurred())

	time.Sleep(4 * time.Second)
	g.Expect(c.Get(ctx, types.NamespacedName{Namespace: "cluster1", Name: "bgd-app"}, mw)).NotTo(HaveOccurred())
	g.Expect(c.Get(ctx, types.NamespacedName{Namespace: "cluster1", Name: "bgd-app-2"}, mw)).NotTo(HaveOccurred())

	appset := &appsetreportV1alpha1.MulticlusterApplicationSetReport{}
	g.Expect(c.Get(ctx, types.NamespacedName{Name: "appset-1", Namespace: "appset-ns-1"}, appset)).NotTo(HaveOccurred())

	g.Expect(c.Get(ctx, types.NamespacedName{Name: "appset-2", Namespace: "appset-ns-2"}, appset)).NotTo(HaveOccurred())

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
