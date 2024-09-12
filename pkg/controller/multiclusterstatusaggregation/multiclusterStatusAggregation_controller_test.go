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

package multiclusterstatusaggregation

import (
	"context"
	"os"
	"testing"
	"time"

	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	v1 "open-cluster-management.io/api/work/v1"
	appsetreportV1alpha1 "open-cluster-management.io/multicloud-integrations/pkg/apis/appsetreport/v1alpha1"
	argov1alpha1 "open-cluster-management.io/multicloud-integrations/pkg/apis/argocd/v1alpha1"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"sigs.k8s.io/controller-runtime/pkg/manager"
)

var (
	healthy = "Healthy"
	synced  = "Synced"

	// ManifestWorks

	longResourceManifestWork = &v1.ManifestWork{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cluster1-long-resource-name-truncate-the-name-over-46-chars",
			Namespace: "cluster1",
			Annotations: map[string]string{
				"apps.open-cluster-management.io/hosting-applicationset": "openshift-gitops/long-resource-name-truncate-the-name-over-46-chars",
				"apps.open-cluster-management.io/hub-application-name":   "long-resource-name-truncate-the-name-over-46-chars",
			},
			Labels: map[string]string{
				"apps.open-cluster-management.io/application-set": "true",
			},
		},
	}

	sampleManifestWork1 = &v1.ManifestWork{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cluster1-bgd-app",
			Namespace: "cluster1",
			Annotations: map[string]string{
				"apps.open-cluster-management.io/hosting-applicationset": "openshift-gitops/appset-1",
				"apps.open-cluster-management.io/hub-application-name":   "bgd-app",
			},
			Labels: map[string]string{
				"apps.open-cluster-management.io/application-set": "true",
			},
		},
	}

	sampleManifestWork2 = &v1.ManifestWork{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cluster1-bgd-app-2",
			Namespace: "cluster1",
			Annotations: map[string]string{
				"apps.open-cluster-management.io/hosting-applicationset": "openshift-gitops/appset-2",
				"apps.open-cluster-management.io/hub-application-name":   "bgd-app-2",
			},
			Labels: map[string]string{
				"apps.open-cluster-management.io/application-set": "true",
			},
		},
	}

	sampleManifestWork3 = &v1.ManifestWork{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cluster1-bgd-app-3",
			Namespace: "cluster1",
			Annotations: map[string]string{
				"apps.open-cluster-management.io/hosting-applicationset": "openshift-gitops/appset-3",
				"apps.open-cluster-management.io/hub-application-name":   "bgd-app-3",
			},
			Labels: map[string]string{
				"apps.open-cluster-management.io/application-set": "true",
			},
		},
	}

	sampleManifestWork4 = &v1.ManifestWork{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cluster1-bgd-app-4",
			Namespace: "cluster1",
			Annotations: map[string]string{
				"apps.open-cluster-management.io/hosting-applicationset":    "openshift-gitops/appset-4",
				"apps.open-cluster-management.io/hub-application-namespace": "openshift-gitops",
				"apps.open-cluster-management.io/hub-application-name":      "sample-app",
			},
			Labels: map[string]string{
				"apps.open-cluster-management.io/application-set": "true",
			},
		},
	}
	sampleManifestWork5 = &v1.ManifestWork{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cluster1-bgd-app-5",
			Namespace: "cluster1",
			Annotations: map[string]string{
				"apps.open-cluster-management.io/hosting-applicationset": "openshift-gitops/bgd-app-5",
				"apps.open-cluster-management.io/hub-application-name":   "bgd-app-5",
			},
			Labels: map[string]string{
				"apps.open-cluster-management.io/application-set": "true",
			},
		},
	}

	sampleManifestWorkDummy = &v1.ManifestWork{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cluster1-dummy-app",
			Namespace: "cluster1",
			Annotations: map[string]string{
				"apps.open-cluster-management.io/hosting-applicationset": "",
				"apps.open-cluster-management.io/hub-application-name":   "dummy-app",
			},
			Labels: map[string]string{
				"apps.open-cluster-management.io/application-set": "true",
			},
		},
	}

	// AppSetReports

	sampleMulticlusterApplicationSetReport1 = &appsetreportV1alpha1.MulticlusterApplicationSetReport{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "appset-1",
			Namespace: "openshift-gitops",
			Labels: map[string]string{
				"apps.open-cluster-management.io/hosting-applicationset": "openshift-gitops.appset-1",
			},
		},
	}

	sampleMulticlusterApplicationSetReport2 = &appsetreportV1alpha1.MulticlusterApplicationSetReport{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "appset-2",
			Namespace: "openshift-gitops",
			Labels: map[string]string{
				"apps.open-cluster-management.io/hosting-applicationset": "openshift-gitops.appset-2",
				"diff-labels-test": "true",
			},
		},
	}

	sampleMulticlusterApplicationSetReportBgd5 = &appsetreportV1alpha1.MulticlusterApplicationSetReport{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "bgd-app-5",
			Namespace: "openshift-gitops",
			Labels: map[string]string{
				"apps.open-cluster-management.io/hosting-applicationset": "openshift-gitops.bgd-app-5",
			},
		},
	}

	sampleMulticlusterApplicationSetReportDummy = &appsetreportV1alpha1.MulticlusterApplicationSetReport{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "dummy-app",
			Namespace: "openshift-gitops",
			Labels: map[string]string{
				"apps.open-cluster-management.io/hosting-applicationset": "",
			},
		},
	}

	// Appsets

	longAppsetName = &argov1alpha1.ApplicationSet{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ApplicationSet",
			APIVersion: "argoproj.io/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "long-resource-name-truncate-the-name-over-46-chars",
			Namespace: "openshift-gitops",
		},
		Spec: argov1alpha1.ApplicationSetSpec{
			Generators: []argov1alpha1.ApplicationSetGenerator{},
			Template: argov1alpha1.ApplicationSetTemplate{
				Spec: argov1alpha1.ApplicationSpec{
					Source: &argov1alpha1.ApplicationSource{
						RepoURL: "",
					},
					Destination: argov1alpha1.ApplicationDestination{},
					Project:     "",
				},
			},
		},
	}

	sampleAppset1 = &argov1alpha1.ApplicationSet{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ApplicationSet",
			APIVersion: "argoproj.io/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "appset-1",
			Namespace: "openshift-gitops",
		},
		Spec: argov1alpha1.ApplicationSetSpec{
			Generators: []argov1alpha1.ApplicationSetGenerator{},
			Template: argov1alpha1.ApplicationSetTemplate{
				Spec: argov1alpha1.ApplicationSpec{
					Source: &argov1alpha1.ApplicationSource{
						RepoURL: "",
					},
					Destination: argov1alpha1.ApplicationDestination{},
					Project:     "",
				},
			},
		},
	}

	sampleAppset2 = &argov1alpha1.ApplicationSet{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ApplicationSet",
			APIVersion: "argoproj.io/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "appset-2",
			Namespace: "openshift-gitops",
		},
		Spec: argov1alpha1.ApplicationSetSpec{
			Generators: []argov1alpha1.ApplicationSetGenerator{},
			Template: argov1alpha1.ApplicationSetTemplate{
				Spec: argov1alpha1.ApplicationSpec{
					Source: &argov1alpha1.ApplicationSource{
						RepoURL: "",
					},
					Destination: argov1alpha1.ApplicationDestination{},
					Project:     "",
				},
			},
		},
	}
	sampleAppset3 = &argov1alpha1.ApplicationSet{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ApplicationSet",
			APIVersion: "argoproj.io/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "appset-3",
			Namespace: "openshift-gitops",
		},
		Spec: argov1alpha1.ApplicationSetSpec{
			Generators: []argov1alpha1.ApplicationSetGenerator{},
			Template: argov1alpha1.ApplicationSetTemplate{
				Spec: argov1alpha1.ApplicationSpec{
					Source: &argov1alpha1.ApplicationSource{
						RepoURL: "",
					},
					Destination: argov1alpha1.ApplicationDestination{},
					Project:     "",
				},
			},
		},
	}
	sampleAppset4 = &argov1alpha1.ApplicationSet{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ApplicationSet",
			APIVersion: "argoproj.io/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "appset-4",
			Namespace: "openshift-gitops",
		},
		Spec: argov1alpha1.ApplicationSetSpec{
			Generators: []argov1alpha1.ApplicationSetGenerator{},
			Template: argov1alpha1.ApplicationSetTemplate{
				Spec: argov1alpha1.ApplicationSpec{
					Source: &argov1alpha1.ApplicationSource{
						RepoURL: "",
					},
					Destination: argov1alpha1.ApplicationDestination{},
					Project:     "",
				},
			},
		},
	}
	sampleAppsetBgd1 = &argov1alpha1.ApplicationSet{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ApplicationSet",
			APIVersion: "argoproj.io/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "bgd-app",
			Namespace: "openshift-gitops",
		},
		Spec: argov1alpha1.ApplicationSetSpec{
			Generators: []argov1alpha1.ApplicationSetGenerator{},
			Template: argov1alpha1.ApplicationSetTemplate{
				Spec: argov1alpha1.ApplicationSpec{
					Source: &argov1alpha1.ApplicationSource{
						RepoURL: "",
					},
					Destination: argov1alpha1.ApplicationDestination{},
					Project:     "",
				},
			},
		},
	}
	sampleAppsetBgd2 = &argov1alpha1.ApplicationSet{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ApplicationSet",
			APIVersion: "argoproj.io/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "bgd-app-2",
			Namespace: "openshift-gitops",
		},
		Spec: argov1alpha1.ApplicationSetSpec{
			Generators: []argov1alpha1.ApplicationSetGenerator{},
			Template: argov1alpha1.ApplicationSetTemplate{
				Spec: argov1alpha1.ApplicationSpec{
					Source: &argov1alpha1.ApplicationSource{
						RepoURL: "",
					},
					Destination: argov1alpha1.ApplicationDestination{},
					Project:     "",
				},
			},
		},
	}
	sampleAppsetBgd3 = &argov1alpha1.ApplicationSet{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ApplicationSet",
			APIVersion: "argoproj.io/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "bgd-app-3",
			Namespace: "openshift-gitops",
		},
		Spec: argov1alpha1.ApplicationSetSpec{
			Generators: []argov1alpha1.ApplicationSetGenerator{},
			Template: argov1alpha1.ApplicationSetTemplate{
				Spec: argov1alpha1.ApplicationSpec{
					Source: &argov1alpha1.ApplicationSource{
						RepoURL: "",
					},
					Destination: argov1alpha1.ApplicationDestination{},
					Project:     "",
				},
			},
		},
	}
	sampleAppsetBgd4 = &argov1alpha1.ApplicationSet{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ApplicationSet",
			APIVersion: "argoproj.io/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "bgd-app-4",
			Namespace: "openshift-gitops",
		},
		Spec: argov1alpha1.ApplicationSetSpec{
			Generators: []argov1alpha1.ApplicationSetGenerator{},
			Template: argov1alpha1.ApplicationSetTemplate{
				Spec: argov1alpha1.ApplicationSpec{
					Source: &argov1alpha1.ApplicationSource{
						RepoURL: "",
					},
					Destination: argov1alpha1.ApplicationDestination{},
					Project:     "",
				},
			},
		},
	}
	sampleAppsetDummy = &argov1alpha1.ApplicationSet{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ApplicationSet",
			APIVersion: "argoproj.io/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "dummy-app",
			Namespace: "openshift-gitops",
		},
		Spec: argov1alpha1.ApplicationSetSpec{
			Generators: []argov1alpha1.ApplicationSetGenerator{},
			Template: argov1alpha1.ApplicationSetTemplate{
				Spec: argov1alpha1.ApplicationSpec{
					Source: &argov1alpha1.ApplicationSource{
						RepoURL: "",
					},
					Destination: argov1alpha1.ApplicationDestination{},
					Project:     "",
				},
			},
		},
	}

	// Resource list yaml

	bgdAppData5 = `statuses:
  resources:
  - apiVersion: apps/v1
    kind: Deployment
    name: redis-master5
    namespace: playback-ns-5
  - apiVersion: v1
    kind: Service
    name: redis-master5
    namespace: playback-ns-5
  clusterConditions:
  - cluster: cluster1
    conditions:
    - type: SyncError
      message: "error message 1"
  - cluster: cluster3
    conditions:
    - type: SyncError
      message: "error message 3"`

	longResourceName = `statuses:
  resources:
  - apiVersion: apps/v1
    kind: Deployment
    name: redis-master-long
    namespace: playback-ns-5
  - apiVersion: v1
    kind: Service
    name: redis-master-long
    namespace: playback-ns-5
  clusterConditions:
  - cluster: cluster1
    conditions:
    - type: SyncError
      message: "error message 1"`
)

func TestReconcilePullModel(t *testing.T) {
	g := NewGomegaWithT(t)

	// Setup the Manager and Controller
	mgr, err := manager.New(cfg, manager.Options{
		Metrics: metricsserver.Options{
			BindAddress: "0",
		},
	})

	g.Expect(err).NotTo(HaveOccurred())

	c = mgr.GetClient()

	ctx, cancel := context.WithTimeout(context.TODO(), 5*time.Minute)

	// Create appsets before the aggregation controller is started.
	// Or the hack/test/openshift-gitops_appset-1.yaml will be deleted as its related appset is not created yet
	g.Expect(c.Create(ctx, sampleAppset1.DeepCopy())).NotTo(HaveOccurred())
	g.Expect(c.Create(ctx, sampleAppset2.DeepCopy())).NotTo(HaveOccurred())
	g.Expect(c.Create(ctx, sampleAppset3.DeepCopy())).NotTo(HaveOccurred())
	g.Expect(c.Create(ctx, sampleAppset4.DeepCopy())).NotTo(HaveOccurred())
	g.Expect(c.Create(ctx, sampleAppsetBgd1.DeepCopy())).NotTo(HaveOccurred())
	g.Expect(c.Create(ctx, sampleAppsetBgd2.DeepCopy())).NotTo(HaveOccurred())
	g.Expect(c.Create(ctx, sampleAppsetBgd3.DeepCopy())).NotTo(HaveOccurred())
	g.Expect(c.Create(ctx, sampleAppsetBgd4.DeepCopy())).NotTo(HaveOccurred())
	g.Expect(c.Create(ctx, sampleAppsetDummy.DeepCopy())).NotTo(HaveOccurred())
	g.Expect(c.Create(ctx, longAppsetName.DeepCopy())).NotTo(HaveOccurred())

	Add(mgr, 10, "../../../hack/test") // 10 second interval

	mgrStopped := StartTestManager(ctx, mgr, g)

	defer func() {
		cancel()
		mgrStopped.Wait()
	}()

	// Create appset reports
	g.Expect(c.Create(ctx, sampleMulticlusterApplicationSetReport1.DeepCopy())).NotTo(HaveOccurred())
	g.Expect(c.Create(ctx, sampleMulticlusterApplicationSetReport2.DeepCopy())).NotTo(HaveOccurred())
	g.Expect(c.Create(ctx, sampleMulticlusterApplicationSetReportBgd5.DeepCopy())).NotTo(HaveOccurred())
	g.Expect(c.Create(ctx, sampleMulticlusterApplicationSetReportDummy.DeepCopy())).NotTo(HaveOccurred())

	// need to create a manifestwork
	g.Expect(c.Create(ctx, sampleManifestWork1.DeepCopy())).NotTo(HaveOccurred())
	g.Expect(c.Create(ctx, sampleManifestWork2.DeepCopy())).NotTo(HaveOccurred())
	g.Expect(c.Create(ctx, sampleManifestWork3.DeepCopy())).NotTo(HaveOccurred())
	g.Expect(c.Create(ctx, sampleManifestWork4.DeepCopy())).NotTo(HaveOccurred())
	g.Expect(c.Create(ctx, sampleManifestWork5.DeepCopy())).NotTo(HaveOccurred())
	g.Expect(c.Create(ctx, sampleManifestWorkDummy.DeepCopy())).NotTo(HaveOccurred())
	g.Expect(c.Create(ctx, longResourceManifestWork.DeepCopy())).NotTo(HaveOccurred())

	// create bgd-app-5 yaml
	f, err := os.Create("../../../hack/test/openshift-gitops_bgd-app-5.yaml")
	g.Expect(err).NotTo(HaveOccurred())

	_, err = f.WriteString(bgdAppData5)
	g.Expect(err).NotTo(HaveOccurred())

	f, err = os.Create("../../../hack/test/openshift-gitops_long-resource-name-truncate-the-name-over-46-chars.yaml")
	g.Expect(err).NotTo(HaveOccurred())

	_, err = f.WriteString(longResourceName)
	g.Expect(err).NotTo(HaveOccurred())

	defer os.Remove("../../../hack/test/openshift-gitops_long-resource-name-truncate-the-name-over-46-chars.yaml")

	defer f.Close()

	time.Sleep(4 * time.Second)

	// Test 1: sycned and healthy status with existing manifestwork and resource list yaml
	mw := &v1.ManifestWork{}
	g.Expect(c.Get(ctx, types.NamespacedName{Namespace: "cluster1", Name: "cluster1-bgd-app"}, mw)).NotTo(HaveOccurred())

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

	// verify manifestwork was updated properly.
	g.Expect(c.Get(ctx, types.NamespacedName{Namespace: "cluster1", Name: "cluster1-bgd-app"}, mw)).NotTo(HaveOccurred())
	newMw = mw.DeepCopy()
	g.Expect(newMw.Status.ResourceStatus.Manifests[0].StatusFeedbacks.Values).To(Equal([]v1.FeedbackValue{
		{Name: "healthStatus", Value: v1.FieldValue{Type: v1.String, String: &healthy}},
		{Name: "syncStatus", Value: v1.FieldValue{Type: v1.String, String: &synced}}}))

	// verify truncating label is working properly
	appsetReport := &appsetreportV1alpha1.MulticlusterApplicationSetReport{}
	g.Expect(c.Get(ctx, types.NamespacedName{Namespace: "openshift-gitops",
		Name: "long-resource-name-truncate-the-name-over-46-chars"}, appsetReport)).NotTo(HaveOccurred())
	g.Expect(appsetReport.Labels["apps.open-cluster-management.io/hosting-applicationset"]).
		To(Equal("openshift-gitops.long-resource-name-truncate-the-name-over-46-c"))

	g.Expect(appsetReport.Statuses.Resources).To(Equal([]appsetreportV1alpha1.ResourceRef{
		{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
			Name:       "redis-master-long",
			Namespace:  "playback-ns-5",
		},
		{
			APIVersion: "v1",
			Kind:       "Service",
			Name:       "redis-master-long",
			Namespace:  "playback-ns-5",
		},
	}))

	g.Expect(c.Get(ctx, types.NamespacedName{Namespace: "openshift-gitops", Name: "appset-1"}, appsetReport)).NotTo(HaveOccurred())
	g.Expect(appsetReport.Statuses.Summary).To(Equal(appsetreportV1alpha1.ReportSummary{
		Synced:     "1",
		NotSynced:  "0",
		Healthy:    "1",
		NotHealthy: "0",
		InProgress: "0",
		Clusters:   "1",
	}))
	g.Expect(appsetReport.Statuses.Resources).To(Equal([]appsetreportV1alpha1.ResourceRef{
		{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
			Name:       "redis-master2",
			Namespace:  "playback-ns-2",
		},
		{
			APIVersion: "v1",
			Kind:       "Service",
			Name:       "redis-master2",
			Namespace:  "playback-ns-2",
		},
	}))
	g.Expect(appsetReport.Statuses.ClusterConditions).To(Equal([]appsetreportV1alpha1.ClusterCondition{
		{
			Cluster:      "cluster1",
			SyncStatus:   "Synced",
			HealthStatus: "Healthy",
			Conditions:   []appsetreportV1alpha1.Condition{{Type: "SyncError", Message: "error message 1"}},
			App:          "openshift-gitops/bgd-app/cluster1/cluster1-bgd-app",
		},
		{
			Cluster:      "cluster3",
			SyncStatus:   "",
			HealthStatus: "",
			Conditions:   []appsetreportV1alpha1.Condition{{Type: "SyncError", Message: "error message 3"}},
			App:          "", // No corresponding appset
		},
	}))

	// Test 2: no corresponding resource list yaml but MulticlusterApplicationSetReport exists beforehand
	g.Expect(c.Get(ctx, types.NamespacedName{Namespace: "openshift-gitops", Name: "appset-2"}, appsetReport)).NotTo(HaveOccurred())
	g.Expect(appsetReport.Statuses.Resources).To(BeNil())
	g.Expect(appsetReport.Statuses.ClusterConditions).To(Equal([]appsetreportV1alpha1.ClusterCondition{
		{
			Cluster:      "cluster1",
			SyncStatus:   "Unknown",
			HealthStatus: "Unknown",
			Conditions:   nil,
			App:          "openshift-gitops/bgd-app-2/cluster1/cluster1-bgd-app-2",
		},
	}))

	// Test 3: no corresponding resource list yaml but MulticlusterApplicationSetReport does not exist beforehand
	g.Expect(c.Get(ctx, types.NamespacedName{Namespace: "openshift-gitops", Name: "appset-3"}, appsetReport)).NotTo(HaveOccurred())
	g.Expect(appsetReport.Statuses.Resources).To(BeNil())
	g.Expect(appsetReport.Statuses.ClusterConditions).To(Equal([]appsetreportV1alpha1.ClusterCondition{
		{
			Cluster:      "cluster1",
			SyncStatus:   "Unknown",
			HealthStatus: "Unknown",
			Conditions:   nil,
			App:          "openshift-gitops/bgd-app-3/cluster1/cluster1-bgd-app-3",
		},
	}))
	time.Sleep(4 * time.Second)

	// Test 4: Update manifestwork status to be synced & progressing. No existing MulticlusterApplicationSetReport
	g.Expect(c.Get(ctx, types.NamespacedName{Namespace: "cluster1", Name: "cluster1-bgd-app-4"}, mw)).NotTo(HaveOccurred())

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

	// verify manifestwork was updated properly.
	g.Expect(c.Get(ctx, types.NamespacedName{Namespace: "cluster1", Name: "cluster1-bgd-app-4"}, mw)).NotTo(HaveOccurred())
	newMw = mw.DeepCopy()
	g.Expect(newMw.Status.ResourceStatus.Manifests[0].StatusFeedbacks.Values).To(Equal([]v1.FeedbackValue{
		{Name: "healthStatus", Value: v1.FieldValue{Type: v1.String, String: &progressing}},
		{Name: "syncStatus", Value: v1.FieldValue{Type: v1.String, String: &synced}}}))

	time.Sleep(15 * time.Second)

	g.Expect(c.Get(ctx, types.NamespacedName{Namespace: "openshift-gitops", Name: "appset-4"}, appsetReport)).NotTo(HaveOccurred())
	g.Expect(appsetReport.Statuses.Resources).To(BeNil())
	g.Expect(appsetReport.Statuses.ClusterConditions).To(Equal([]appsetreportV1alpha1.ClusterCondition{
		{
			Cluster:      "cluster1",
			SyncStatus:   "Synced",
			HealthStatus: "Progressing",
			Conditions:   nil,
			App:          "openshift-gitops/sample-app/cluster1/cluster1-bgd-app-4",
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
