/*
Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicationlicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package application

import (
	"context"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	appsetreportV1alpha1 "open-cluster-management.io/multicloud-integrations/pkg/apis/appsetreport/v1alpha1"
	argov1alpha1 "open-cluster-management.io/multicloud-integrations/pkg/apis/argocd/v1alpha1"
)

// ApplicationStatusReconciler reconciles a Application object
type ApplicationStatusReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=argoproj.io,resources=applications,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=apps.open-cluster-management.io,resources=multiclusterapplicationsetreports,verbs=get;list;watch

// SetupWithManager sets up the controller with the Manager.
func (re *ApplicationStatusReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&appsetreportV1alpha1.MulticlusterApplicationSetReport{}).
		Complete(re)
}

// Reconcile populates the Application status based on the MulticlusterApplicationSetReport
func (r *ApplicationStatusReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	log.Info("reconciling Application for status update..")
	defer log.Info("done reconciling Application for status update")

	var report appsetreportV1alpha1.MulticlusterApplicationSetReport
	if err := r.Get(ctx, req.NamespacedName, &report); err != nil {
		log.Error(err, "unable to fetch MulticlusterApplicationSetReport")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !report.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}

	if report.Statuses.ClusterConditions == nil || len(report.Statuses.ClusterConditions) <= 0 {
		return ctrl.Result{}, nil
	}

	for _, cc := range report.Statuses.ClusterConditions {
		if cc.App != "" {
			appNsn := strings.Split(cc.App, "/")
			appNamespace := appNsn[0]
			appName := appNsn[1]

			application := argov1alpha1.Application{}
			if err := r.Get(ctx, types.NamespacedName{Namespace: appNamespace, Name: appName}, &application); err != nil {
				log.Error(err, "unable to fetch Application")
				return ctrl.Result{}, err
			}

			if cc.SyncStatus != "" {
				application.Status.Sync.Status = argov1alpha1.SyncStatusCode(cc.SyncStatus)
			}
			if cc.HealthStatus != "" {
				application.Status.Health.Status = cc.HealthStatus
			}

			appSetName := getAppSetOwnerName(application)
			if appSetName != "" && len(application.Status.Conditions) == 0 {
				application.Status.Conditions = []argov1alpha1.ApplicationCondition{{
					Type: "AdditionalStatusReport",
					Message: fmt.Sprintf("kubectl get multiclusterapplicationsetreports -n %s %s"+
						"\nAdditional details available in ManagedCluster %s"+
						"\nkubectl get applications -n %s %s",
						appNamespace, appSetName,
						cc.Cluster,
						appNamespace, appName),
				}}
			}

			err := r.Client.Update(ctx, &application)
			if err != nil {
				log.Error(err, "unable to update Application")
				return ctrl.Result{}, err
			}
		}
	}

	return ctrl.Result{}, nil
}
