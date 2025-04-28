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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	appsetreportV1alpha1 "open-cluster-management.io/multicloud-integrations/pkg/apis/appsetreport/v1alpha1"
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
		appNsn := strings.Split(cc.App, "/")

		if len(appNsn) > 1 {
			appNamespace := appNsn[0]
			appName := appNsn[1]

			application := &unstructured.Unstructured{}
			application.SetGroupVersionKind(schema.GroupVersionKind{
				Group:   "argoproj.io",
				Version: "v1alpha1",
				Kind:    "Application",
			})

			err := r.Get(ctx, types.NamespacedName{Namespace: appNamespace, Name: appName}, application)
			if err != nil {
				if errors.IsNotFound(err) {
					log.Info("not found Application " + err.Error())
					continue
				}

				log.Error(err, "unable to fetch Application")

				return ctrl.Result{}, err
			}

			oldStatus, _, _ := unstructured.NestedMap(application.Object, "status")
			if oldStatus == nil {
				oldStatus = make(map[string]interface{})
			}

			newStatus := make(map[string]interface{})

			oldJSON, err := json.Marshal(oldStatus)
			if err != nil {
				log.Error(err, "unable to marshal oldStatus")
				return ctrl.Result{}, err
			}

			err = json.Unmarshal(oldJSON, &newStatus)
			if err != nil {
				log.Error(err, "unable to unmarshal oldStatus into newStatus")
				return ctrl.Result{}, err
			}

			if cc.HealthStatus != "" {
				if cc.HealthStatus == "Healthy" {
					// If the health status is Healthy then simulate Progressing first then set to Healthy for ApplicationSet controller
					if err := unstructured.SetNestedField(application.Object, "Progressing", "status", "health", "status"); err != nil {
						log.Error(err, "unable to set healthStatus in Application status")
						return ctrl.Result{}, err
					}

					log.Info("updating Application health status to Progressing")

					if err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
						return r.Client.Update(ctx, application)
					}); err != nil {
						log.Error(err, "unable to update Application")
						return ctrl.Result{}, err
					}
				}

				if err := unstructured.SetNestedField(newStatus, cc.HealthStatus, "health", "status"); err != nil {
					log.Error(err, "unable to set health")
					return ctrl.Result{}, err
				}
			}

			if cc.SyncStatus != "" {
				if err := unstructured.SetNestedField(newStatus, cc.SyncStatus, "sync", "status"); err != nil {
					log.Error(err, "unable to set sync")
					return ctrl.Result{}, err
				}
			}

			if cc.OperationStatePhase != "" {
				if err := unstructured.SetNestedField(newStatus, cc.OperationStatePhase, "operationState", "phase"); err != nil {
					log.Error(err, "unable to set OperationStatePhase")
					return ctrl.Result{}, err
				}

				if cc.OperationStateStartedAt == "" {
					cc.OperationStateStartedAt = time.Now().UTC().Format(time.RFC3339)
				}

				if err = unstructured.SetNestedField(newStatus, cc.OperationStateStartedAt, "operationState", "startedAt"); err != nil {
					log.Error(err, "unable to set OperationStateStartedAt")
					return ctrl.Result{}, err
				}

				if err = unstructured.SetNestedField(newStatus, map[string]interface{}{}, "operationState", "operation"); err != nil {
					// Application required field, set it to empty object
					log.Error(err, "unable to set operation")
					return ctrl.Result{}, err
				}
			}

			if cc.SyncRevision != "" {
				if err := unstructured.SetNestedField(newStatus, cc.SyncRevision, "sync", "revision"); err != nil {
					log.Error(err, "unable to set SyncRevision")
					return ctrl.Result{}, err
				}
			}

			appSetName := getAppSetOwnerName(application.GetOwnerReferences())
			if appSetName != "" {
				conditions, found, _ := unstructured.NestedSlice(newStatus, "conditions")
				if !found {
					conditions = []interface{}{}
				}

				if len(conditions) == 0 {
					newCondition := map[string]interface{}{
						"type": "AdditionalStatusReport",
						"message": fmt.Sprintf(
							"kubectl get multiclusterapplicationsetreports -n %s %s"+
								"\nAdditional details available in ManagedCluster %s"+
								"\nkubectl get applications -n %s %s",
							appNamespace, appSetName,
							cc.Cluster,
							appNamespace, appName,
						),
					}
					conditions = append(conditions, newCondition)

					err := unstructured.SetNestedSlice(newStatus, conditions, "conditions")
					if err != nil {
						log.Error(err, "unable to set conditions")
						return ctrl.Result{}, err
					}
				}
			}

			newJSON, err := json.Marshal(newStatus)
			if err != nil {
				log.Error(err, "unable to marshal newStatus")
				return ctrl.Result{}, err
			}

			if !bytes.Equal(oldJSON, newJSON) {
				err := unstructured.SetNestedField(application.Object, newStatus, "status")
				if err != nil {
					log.Error(err, "unable to set application status")
					return ctrl.Result{}, err
				}

				if err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
					return r.Client.Update(ctx, application)
				}); err != nil {
					log.Error(err, "unable to update Application")
					return ctrl.Result{}, err
				}
			}
		}
	}

	return ctrl.Result{}, nil
}
