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

package application

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	appsetreportV1alpha1 "open-cluster-management.io/multicloud-integrations/pkg/apis/appsetreport/v1alpha1"
)

var _ = Describe("Application Status controller", func() {

	const (
		appName      = "app-5"
		reportName   = "report-1"
		appNamespace = "default"
	)

	appKey := types.NamespacedName{Name: appName, Namespace: appNamespace}
	reportKey := types.NamespacedName{Name: reportName, Namespace: appNamespace}
	ctx := context.Background()

	Context("When MulticlusterApplicationSetReport is created/updated", func() {
		It("Should update Application status", func() {
			By("Creating the Application")
			app1 := &unstructured.Unstructured{}
			app1.SetGroupVersionKind(schema.GroupVersionKind{
				Group:   "argoproj.io",
				Version: "v1alpha1",
				Kind:    "Application",
			})
			app1.SetName(appName)
			app1.SetNamespace(appNamespace)
			app1.Object["spec"] = map[string]interface{}{
				"project": "default",
				"source": map[string]interface{}{
					"repoURL": "https://github.com/argoproj/argocd-example-apps.git",
				},
				"destination": map[string]interface{}{
					"server":    KubernetesInternalAPIServerAddr,
					"namespace": "default",
				},
			}
			Expect(k8sClient.Create(ctx, app1)).Should(Succeed())
			Expect(k8sClient.Get(ctx, appKey, app1)).Should(Succeed())

			By("Creating the MulticlusterApplicationSetReport")
			report1 := &appsetreportV1alpha1.MulticlusterApplicationSetReport{
				ObjectMeta: metav1.ObjectMeta{
					Name:      reportName,
					Namespace: appNamespace,
				},
			}
			Expect(k8sClient.Create(ctx, report1)).Should(Succeed())
			Expect(k8sClient.Get(ctx, reportKey, report1)).Should(Succeed())

			By("Updating the MulticlusterApplicationSetReport statuses")
			report1.Statuses = appsetreportV1alpha1.AppConditions{
				ClusterConditions: []appsetreportV1alpha1.ClusterCondition{
					{
						SyncStatus:              "Synced",
						HealthStatus:            "Healthy",
						OperationStateStartedAt: "2025-04-24T19:53:38Z",
						OperationStatePhase:     "Succeeded",
						SyncRevision:            "4773b9f1f8fd425f84174c338012771c4e9a989c",
						App:                     appNamespace + "/" + appName,
						Cluster:                 "cluster1",
					},
				},
			}
			Expect(k8sClient.Update(ctx, report1)).Should(Succeed())
			time.Sleep(3 * time.Second)

			Eventually(func() bool {
				if err := k8sClient.Get(ctx, appKey, app1); err != nil {
					return false
				}

				syncStatus, _, _ := unstructured.NestedString(app1.Object, "status", "sync", "status")
				healthStatus, _, _ := unstructured.NestedString(app1.Object, "status", "health", "status")
				operationStateStartedAtStatus, _, _ := unstructured.NestedString(app1.Object, "status", "operationState", "startedAt")
				operationStatePhaseStatus, _, _ := unstructured.NestedString(app1.Object, "status", "operationState", "phase")
				syncRevisionStatus, _, _ := unstructured.NestedString(app1.Object, "status", "sync", "revision")

				return syncStatus == "Synced" && healthStatus == "Healthy" &&
					operationStateStartedAtStatus == "2025-04-24T19:53:38Z" &&
					operationStatePhaseStatus == "Succeeded" &&
					syncRevisionStatus == "4773b9f1f8fd425f84174c338012771c4e9a989c"
			}).Should(BeTrue())
		})
	})
})
