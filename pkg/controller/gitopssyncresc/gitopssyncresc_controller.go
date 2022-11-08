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
	"os"
	"regexp"
	"strings"
	"time"

	"path/filepath"

	"gopkg.in/yaml.v2"
	"k8s.io/klog"
	"k8s.io/utils/strings/slices"

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

var ExcludeResourceList = []string{"ApplicationSet", "EndpointSlice"}

// Add creates a new argocd cluster Controller and adds it to the Manager with default RBAC.
// The Manager will set fields on the Controller and Start it when the Manager is Started.
func Add(mgr manager.Manager, interval int, resourceDir string) error {
	gitopsSyncResc := &GitOpsSyncResource{
		Client:      mgr.GetClient(),
		Interval:    interval,
		ResourceDir: resourceDir,
	}

	// Create resourceDir if it does not exist
	_, err := os.Stat(resourceDir)
	if err != nil && os.IsNotExist(err) {
		err = os.MkdirAll(resourceDir, 0777)
		if err != nil {
			klog.Errorf("failed to create directory for resource files:%v", resourceDir)
			return err
		}
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

	// Mapping of the app name for each cluster of an appset
	managedClustersAppNameMap := make(map[string]map[string]string)

	for _, managedcluster := range managedclusters {
		items, _, err := r.getArgoAppsFromSearch(managedcluster.GetName(), "", "")
		if err != nil {
			return err
		}

		for _, item := range items {
			if itemmap, ok := item.(map[string]interface{}); ok {
				klog.V(4).Info(fmt.Sprintf("item: %v", itemmap))

				r.createOrUpdateAppSetReportConditions(appReportsMap, itemmap, managedcluster.Name, managedClustersAppNameMap)
			}
		}
	}

	klog.Infof("managedClustersAppNameMap: %v", managedClustersAppNameMap)

	// Add resource to the AppSet report
	for _, v := range appReportsMap {
		if len(v.Statuses.Resources) != 0 {
			continue
		}

		for managedClusterName, appKey := range managedClustersAppNameMap[v.Name] {
			appNsn := strings.Split(appKey, "_")

			_, related, err := r.getArgoAppsFromSearch(managedClusterName, appNsn[0], appNsn[1])
			if err != nil {
				klog.Infof("failed to get app (%v/%v) from cluster: %v, err:%v", appNsn[0], appNsn[1], managedClusterName, err.Error())
				continue
			}

			if related == nil || len(related) == 0 {
				klog.Infof("no data for app (%v/%v) found on cluster: %v", appNsn[0], appNsn[1], managedClusterName)
				continue
			}

			v.Statuses.Resources = getResourceMapList(related, managedClusterName)
			klog.V(4).Infof("resources for app (%v/%v): %v", appNsn[0], appNsn[1], v.Statuses.Resources)
			break
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

func (r *GitOpsSyncResource) getArgoAppsFromSearch(cluster, appsetNs, appsetName string) ([]interface{}, []interface{}, error) {
	klog.Info(fmt.Sprintf("Start getting argo application for cluster: %v, app: %v/%v", cluster, appsetNs, appsetName))
	defer klog.Info(fmt.Sprintf("Finished getting argo application for cluster: %v, app: %v/%v", cluster, appsetNs, appsetName))

	httpClient, err := rest.HTTPClientFor(ctrl.GetConfigOrDie())
	if err != nil {
		return nil, nil, err
	}

	routeUrl, err := r.getSearchUrl()
	if err != nil {
		return nil, nil, err
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

	if appsetNs != "" && appsetName != "" {
		searchInput.Filters = append(searchInput.Filters, &model.SearchFilter{Property: "namespace", Values: []*string{&appsetNs}})
		searchInput.Filters = append(searchInput.Filters, &model.SearchFilter{Property: "name", Values: []*string{&appsetName}})
	}

	searchVars := make(map[string]interface{})
	searchVars["input"] = []*model.SearchInput{searchInput}

	searchQuery := make(map[string]interface{})
	searchQuery["query"] = "query mySearch($input: [SearchInput]) {searchResult: search(input: $input) {items, related { kind count items }, count}}"
	searchQuery["variables"] = searchVars

	postBody, _ := json.Marshal(searchQuery)
	klog.V(1).Infof("search: %v", string(postBody[:]))

	resp, err := httpClient.Post(routeUrl, "application/json", bytes.NewBuffer(postBody))
	if err != nil {
		klog.Info(err.Error())
		return nil, nil, err
	}

	// Parsse search results
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, err
	}

	str1 := string(body[:])
	klog.V(1).Infof("resp: %v", str1)

	respData := make(map[string]interface{})
	if err := json.Unmarshal(body, &respData); err != nil {
		return nil, nil, err
	}

	searchResults := respData["data"].(map[string]interface{})["searchResult"].([]interface{})
	if len(searchResults) == 0 {
		return nil, nil, nil
	}

	items := searchResults[0].(map[string]interface{})["items"].([]interface{})
	klog.V(1).Infof("Items: %v", items)

	var related []interface{}
	if r, ok := searchResults[0].(map[string]interface{})["related"]; ok && r != nil {
		related = searchResults[0].(map[string]interface{})["related"].([]interface{})
	}
	klog.V(1).Infof("Related: %v", related)

	return items, related, nil
}

func (r *GitOpsSyncResource) createOrUpdateAppSetReportConditions(appReportsMap map[string]*appsetreport.MulticlusterApplicationSetReport,
	appsetResource map[string]interface{}, managedClusterName string, managedClusterAppNameMap map[string]map[string]string) error {

	// Skip application that don't belong to an appset
	appNs := appsetResource["namespace"].(string)
	appsetName := appsetResource["applicationSet"].(string)
	appName := appsetResource["name"].(string)

	if appsetName == "" {
		klog.Infof("skip application %v/%v in cluster %v, it does not belong to an appset", appNs, appName, managedClusterName)
		return nil
	}

	reportKey := appNs + "_" + appsetName
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

	reportConditions := make([]appsetreport.Condition, 0)
	eCond, _ := regexp.Compile("_internalCondition.*Error")
	wCond, _ := regexp.Compile("_internalCondition.*Warning")
	for k, v := range appsetResource {
		// Add app name to map
		clusterAppsetNameMap := managedClusterAppNameMap[reportKey]
		if len(clusterAppsetNameMap) == 0 {
			clusterAppsetNameMap = make(map[string]string)
			managedClusterAppNameMap[reportKey] = clusterAppsetNameMap
		}
		clusterAppsetNameMap[managedClusterName] = appNs + "_" + appName

		if !eCond.MatchString(k) && !wCond.MatchString(k) {
			continue
		}

		repCond := &appsetreport.Condition{
			Type:    string(k[18:]),
			Message: v.(string),
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

func getResourceMapList(related []interface{}, cluster string) []appsetreport.ResourceRef {
	resourceList := make([]appsetreport.ResourceRef, 0)
	for _, rel := range related {
		if relatedmap, ok := rel.(map[string]interface{}); ok {
			klog.V(4).Info(fmt.Sprintf("related: %v", relatedmap))

			if slices.Contains(ExcludeResourceList, relatedmap["kind"].(string)) {
				klog.V(4).Infof("skip resource kind: %v", relatedmap["kind"].(string))
				continue
			}

			if relatedItems, ok := relatedmap["items"].([]interface{}); ok {
				for _, r := range relatedItems {
					relatedItem := r.(map[string]interface{})
					if itemCluster, ok := relatedItem["cluster"]; !ok || itemCluster != cluster {
						klog.V(4).Infof("skip resource from cluster:%v", itemCluster)
						continue
					}

					if ownerUID, ok := relatedItem["_ownerUID"]; ok && ownerUID != "" {
						klog.V(4).Infof("skip resource from _ownerUID:%v", ownerUID)
						continue
					}

					resourceRef := getResourceRef(relatedItem)
					klog.V(4).Infof("append resource: %v", resourceRef)
					resourceList = append(resourceList, *resourceRef)
				}
			}
		}
	}

	return resourceList
}

func getResourceRef(relatedItem map[string]interface{}) *appsetreport.ResourceRef {
	apigroup := ""
	if _apigroup, ok := relatedItem["apigroup"]; ok && _apigroup != "" {
		apigroup = _apigroup.(string) + "/"
	}

	namespace := ""
	if _ns, ok := relatedItem["namespace"]; ok && _ns != "" {
		namespace = _ns.(string)
	}

	repRef := &appsetreport.ResourceRef{
		APIVersion: apigroup + relatedItem["apiversion"].(string),
		Kind:       relatedItem["kind"].(string),
		Name:       relatedItem["name"].(string),
		Namespace:  namespace,
	}

	return repRef
}
