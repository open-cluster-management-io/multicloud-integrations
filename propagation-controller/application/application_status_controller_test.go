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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	appsetreportV1alpha1 "open-cluster-management.io/multicloud-integrations/pkg/apis/appsetreport/v1alpha1"
	argov1alpha1 "open-cluster-management.io/multicloud-integrations/pkg/apis/argocd/v1alpha1"
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
			app1 := argov1alpha1.Application{
				ObjectMeta: metav1.ObjectMeta{
					Name:      appName,
					Namespace: appNamespace,
				},
				Spec: argov1alpha1.ApplicationSpec{Source: &argov1alpha1.ApplicationSource{RepoURL: "dummy"}},
			}
			Expect(k8sClient.Create(ctx, &app1)).Should(Succeed())

			By("Creating the MulticlusterApplicationSetReport")
			report1 := appsetreportV1alpha1.MulticlusterApplicationSetReport{
				ObjectMeta: metav1.ObjectMeta{
					Name:      reportName,
					Namespace: appNamespace,
				},
			}
			Expect(k8sClient.Create(ctx, &report1)).Should(Succeed())
			Expect(k8sClient.Get(ctx, reportKey, &report1)).Should(Succeed())

			By("Updating the MulticlusterApplicationSetReport statuses")
			report1.Statuses = appsetreportV1alpha1.AppConditions{
				ClusterConditions: []appsetreportV1alpha1.ClusterCondition{
					{
						SyncStatus:   "Synced",
						HealthStatus: "Healthy",
						App:          appNamespace + "/" + appName,
						Cluster:      "cluster1",
					},
				},
			}
			Expect(k8sClient.Update(ctx, &report1)).Should(Succeed())
			Eventually(func() bool {
				if err := k8sClient.Get(ctx, appKey, &app1); err != nil {
					return false
				}
				return app1.Status.Health.Status == "Healthy" && app1.Status.Sync.Status == "Synced"
			}).Should(BeTrue())
		})
	})
})
