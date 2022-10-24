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

package gitopssyncresc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"time"

	"path/filepath"

	"gopkg.in/yaml.v2"
	"k8s.io/klog"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	routev1 "github.com/openshift/api/route/v1"
	"github.com/stolostron/search-v2-api/graph/model"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	appsetreport "open-cluster-management.io/multicloud-integrations/pkg/apis/appsetreport/v1alpha1"
)

type GitOpsSyncResource struct {
	Client      client.Client
	Interval    int
	ResourceDir string
}

// Add creates a new argocd cluster Controller and adds it to the Manager with default RBAC.
// The Manager will set fields on the Controller and Start it when the Manager is Started.
func Add(mgr manager.Manager, interval int, resourceDir string) error {
	gitopsSyncResc := &GitOpsSyncResource{
		Client:      mgr.GetClient(),
		Interval:    interval,
		ResourceDir: resourceDir,
	}

	return mgr.Add(gitopsSyncResc)
}

func (r *GitOpsSyncResource) Start(ctx context.Context) error {
	go wait.Until(func() {
		err := r.syncResources()
		if err != nil {
			klog.Error(err, "Error syncing resources from search")
		}
	}, time.Duration(r.Interval)*time.Second, ctx.Done())

	return nil
}

func (r *GitOpsSyncResource) syncResources() error {
	klog.Info("Start syncing gitops resources from search...")
	defer klog.Info("Finished syncing gitops resources from search")

	appReportsMap := make(map[string]*appsetreport.MulticlusterApplicationSetReport)

	// Query search for argo apps
	managedclusters, err := getAllManagedClusterNames(r.Client)
	if err != nil {
		return err
	}

	for _, managedcluster := range managedclusters {
		items, err := r.getArgoAppsFromSearch(managedcluster.GetName())
		if err != nil {
			return err
		}

		for _, item := range items {
			if itemmap, ok := item.(map[string]interface{}); ok {
				klog.Info(fmt.Sprintf("item: %v", itemmap))

				r.createOrUpdateAppSetReport(appReportsMap, itemmap, managedcluster.Name)
			}
		}
	}

	// Write reports
	for _, v := range appReportsMap {
		if err = r.writeAppSetResourceFile(v); err != nil {
			return err
		}
	}

	return nil
}

func getAllManagedClusterNames(c client.Client) ([]clusterv1.ManagedCluster, error) {
	managedclusters := &clusterv1.ManagedClusterList{}
	if err := c.List(context.TODO(), managedclusters, &client.ListOptions{}); err != nil {
		return nil, err
	}

	return managedclusters.Items, nil
}

func (r *GitOpsSyncResource) getSearchUrl() (string, error) {
	route := &routev1.Route{}
	if err := r.Client.Get(context.TODO(), types.NamespacedName{Name: "search-api", Namespace: "open-cluster-management"}, route); err != nil {
		return "", err
	}

	return "https://" + route.Spec.Host + "/searchapi/graphql", nil
}

func (r *GitOpsSyncResource) getArgoAppsFromSearch(cluster string) ([]interface{}, error) {
	klog.Info(fmt.Sprintf("Start getting argo application for cluster: %v", cluster))
	defer klog.Info(fmt.Sprintf("Finished getting argo application for cluster: %v", cluster))

	httpClient, err := rest.HTTPClientFor(ctrl.GetConfigOrDie())
	if err != nil {
		return nil, err
	}

	routeUrl, err := r.getSearchUrl()
	if err != nil {
		return nil, err
	}
	klog.Info(fmt.Sprintf("search url: %v", routeUrl))

	// Build search body
	kind := "Application"
	apigroup := "argoproj.io"
	searchInput := &model.SearchInput{
		Filters: []*model.SearchFilter{
			{
				Property: "kind",
				Values:   []*string{&kind},
			},
			{
				Property: "apigroup",
				Values:   []*string{&apigroup},
			},
			{
				Property: "cluster",
				Values:   []*string{&cluster},
			},
		},
	}

	searchVars := make(map[string]interface{})
	searchVars["input"] = []*model.SearchInput{searchInput}

	searchQuery := make(map[string]interface{})
	searchQuery["query"] = "query mySearch($input: [SearchInput]) {searchResult: search(input: $input) {items, count}}"
	searchQuery["variables"] = searchVars

	postBody, _ := json.Marshal(searchQuery)
	klog.V(1).Infof("search: %v", string(postBody[:]))

	resp, err := httpClient.Post(routeUrl, "application/json", bytes.NewBuffer(postBody))
	if err != nil {
		klog.Info(err.Error())
		return nil, err
	}

	// Parsse search results
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	str1 := string(body[:])
	klog.V(1).Infof("resp: %v", str1)

	respData := make(map[string]interface{})
	if err := json.Unmarshal(body, &respData); err != nil {
		return nil, err
	}

	searchResults := respData["data"].(map[string]interface{})["searchResult"].([]interface{})
	if len(searchResults) == 0 {
		return nil, nil
	}

	items := searchResults[0].(map[string]interface{})["items"].([]interface{})
	klog.V(1).Infof("Items: %v", items)

	return items, nil
}

func (r *GitOpsSyncResource) createOrUpdateAppSetReport(appReportsMap map[string]*appsetreport.MulticlusterApplicationSetReport,
	appsetResource map[string]interface{}, managedClusterName string) error {

	reportKey := appsetResource["namespace"].(string) + "-" + appsetResource["applicationSet"].(string)
	klog.Info(fmt.Sprintf("report key: %v", reportKey))
	report := appReportsMap[reportKey]
	if report == nil {
		klog.Info(fmt.Sprintf("creating new report with key: %v", reportKey))
		report = &appsetreport.MulticlusterApplicationSetReport{
			ObjectMeta: metav1.ObjectMeta{
				Name:      reportKey,
				Namespace: managedClusterName,
			},
		}
		appReportsMap[reportKey] = report
	}

	if appsetResource["resources"] != nil && len(report.Statuses.Resources) == 0 {
		reportResources := make([]appsetreport.ResourceRef, 0)

		resources := appsetResource["resources"].([]map[string]string)
		for _, resc := range resources {
			repRef := &appsetreport.ResourceRef{
				APIVersion: resc["apiVersion"],
				Kind:       resc["kind"],
				Name:       resc["name"],
				Namespace:  resc["namespace"],
			}
			reportResources = append(reportResources, *repRef)
		}

		report.Statuses.Resources = reportResources
	}

	if appsetResource["conditions"] != nil {
		reportConditions := make([]appsetreport.Condition, 0)

		conditions := appsetResource["conditions"].([]map[string]string)
		for _, cond := range conditions {
			repCond := &appsetreport.Condition{
				Type:    cond["type"],
				Message: cond["message"],
			}
			reportConditions = append(reportConditions, *repCond)
		}

		clusterConditions := report.Statuses.ClusterConditions
		if len(clusterConditions) == 0 {
			clusterConditions = make([]appsetreport.ClusterCondition, 0)
		}
		clusterCondition := appsetreport.ClusterCondition{
			Cluster:    managedClusterName,
			Conditions: reportConditions,
		}
		clusterConditions = append(clusterConditions, clusterCondition)
		report.Statuses.ClusterConditions = clusterConditions
	}

	return nil
}

func (r *GitOpsSyncResource) writeAppSetResourceFile(report *appsetreport.MulticlusterApplicationSetReport) error {
	reportJson, err := yaml.Marshal(report)
	if err != nil {
		return err
	}

	reportName := filepath.Join(r.ResourceDir, report.Name+".yaml")
	klog.Info(fmt.Sprintf("writing appset report: %v", reportName))
	if err := ioutil.WriteFile(reportName, reportJson, 0600); err != nil {
		klog.Error(err, fmt.Sprintf("failed to write appset report yaml file: %v", reportName))
		return err
	}

	return nil
}
