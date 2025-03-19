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

package maestroAggregation

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	workv1client "open-cluster-management.io/api/client/work/clientset/versioned/typed/work/v1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

// MaestroAggregationReconciler reconciles a openshift gitops operator
type MaestroAggregationReconciler struct {
	client.Client
	Scheme            *runtime.Scheme
	Config            *rest.Config
	Interval          int
	MaestroWorkClient workv1client.WorkV1Interface
}

func SetupWithManager(mgr manager.Manager, interval int, maestroWorkClient workv1client.WorkV1Interface) error {
	dsRS := &MaestroAggregationReconciler{
		Client:            mgr.GetClient(),
		Scheme:            mgr.GetScheme(),
		Config:            mgr.GetConfig(),
		Interval:          interval,
		MaestroWorkClient: maestroWorkClient,
	}

	return mgr.Add(dsRS)
}

func (r *MaestroAggregationReconciler) Start(ctx context.Context) error {
	go wait.Until(func() {
		r.houseKeeping()
	}, time.Duration(r.Interval)*time.Second, ctx.Done())

	return nil
}

func (r *MaestroAggregationReconciler) houseKeeping() {
	klog.Info("Start aggregating the ArgoCD application status...")

	managedclusters, err := r.getAllManagedClusterNames()
	if err != nil {
		klog.Infof("failed to fetch managed clusters, err: %v", err)
		return
	}

	for _, cluster := range managedclusters {
		r.updateAllApplicationStatusPerCluster(cluster)
	}

	klog.Info("Finsihed aggregating the ArgoCD application status...")
}

func (r *MaestroAggregationReconciler) updateAllApplicationStatusPerCluster(managedClusterName string) {
	maestroManifestworkList, err := r.MaestroWorkClient.ManifestWorks(managedClusterName).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		klog.Infof("failed to list manifestwork from maestro, cluster: %v, err: %v", managedClusterName, err)
		return
	}

	for _, work := range maestroManifestworkList.Items {
		appName := work.Status.ResourceStatus.Manifests[0].ResourceMeta.Name // the appname is <app name>-<cluster name>
		appNamespace := work.Status.ResourceStatus.Manifests[0].ResourceMeta.Namespace
		appStatus := work.Status.ResourceStatus.Manifests[0].StatusFeedbacks.Values[0].Value.JsonRaw

		klog.Infof("appName: %v, appNamespace: %v, cluster: %v", appName, appNamespace, managedClusterName)

		err := r.UpdateArgoCDAppStatus(appStatus, appName, appNamespace)
		if err != nil {
			klog.Infof("failed to update argocd status. err: %v, appName: %v, appNamespace: %v, cluster: %v",
				err.Error(), appName, appNamespace, managedClusterName)
		}
	}

	return
}

func (r *MaestroAggregationReconciler) UpdateArgoCDAppStatus(jsonData *string, appName, appNamespace string) error {
	if jsonData == nil {
		return fmt.Errorf("jsonData is nil")
	}

	if *jsonData == "" {
		return fmt.Errorf("jsonData is empty")
	}

	if appName == "" {
		return fmt.Errorf("appName is empty")
	}

	if appNamespace == "" {
		return fmt.Errorf("appNamespace is empty")
	}

	app := &unstructured.Unstructured{}
	app.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "argoproj.io",
		Version: "v1alpha1",
		Kind:    "Application",
	})
	app.SetName(appName)
	app.SetNamespace(appNamespace)

	err := r.Get(context.TODO(), types.NamespacedName{Namespace: appNamespace, Name: appName}, app)

	if err != nil {
		klog.Infof("failed to fetch Application. err: %v", err.Error())
		return err
	}

	klog.V(1).Infof("app: %#v", app)

	oldStatus, _, _ := unstructured.NestedMap(app.Object, "status")
	if oldStatus == nil {
		oldStatus = make(map[string]interface{})
	}

	oldJSON, err := json.Marshal(oldStatus)
	if err != nil {
		klog.Errorf("unable to marshal oldStatus, err: %v", err)
		return err
	}

	newStatus := make(map[string]interface{})
	err = json.Unmarshal([]byte(*jsonData), &newStatus)

	if err != nil {
		klog.Infof("faile to unmarshal JSON data err: %v", err.Error())
		return err
	}

	newJSON, err := json.Marshal(newStatus)
	if err != nil {
		klog.Errorf("unable to marshal newStatus, err: %v", err)
		return err
	}

	if !bytes.Equal(oldJSON, newJSON) {
		err = unstructured.SetNestedField(app.Object, newStatus, "status")
		if err != nil {
			klog.Infof("failed to set Application status. err: %v", err.Error())
			return err
		}

		err = r.Update(context.TODO(), app)
		if err != nil {
			klog.Infof("failed to update Application status. err: %v", err.Error())
			return err
		}

		klog.Infof("successfully updated  Application status. app: %v/%v,", appNamespace, appName)
	}

	return nil
}

func (r *MaestroAggregationReconciler) getAllManagedClusterNames() ([]string, error) {
	managedclusters := &clusterv1.ManagedClusterList{}
	if err := r.List(context.TODO(), managedclusters, &client.ListOptions{}); err != nil {
		return nil, err
	}

	filteredClusters := []string{}

	for _, cluster := range managedclusters.Items {
		if value, exists := cluster.Labels["local-cluster"]; exists && value == "true" {
			continue
		}
		filteredClusters = append(filteredClusters, cluster.Name)
	}

	return filteredClusters, nil
}
