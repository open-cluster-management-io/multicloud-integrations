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
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"path/filepath"

	"gopkg.in/yaml.v2"
	"k8s.io/klog"
	"k8s.io/utils/strings/slices"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/stolostron/search-v2-api/graph/model"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	appsetreport "open-cluster-management.io/multicloud-integrations/pkg/apis/appsetreport/v1alpha1"
)

const (
	SearchServiceName = "search-search-api"
	SearchDefaultNs   = "open-cluster-management"
	AccessToken       = "ACCESS_TOKEN"
)

type DataSender interface {
	Send(httpClient *http.Client, req *http.Request) (map[string]interface{}, error)
}

type HTTPDataSender struct{}

func (c *HTTPDataSender) Send(httpClient *http.Client, req *http.Request) (map[string]interface{}, error) {
	respData := make(map[string]interface{})

	resp, err := httpClient.Do(req)
	if err != nil {
		klog.Info(err.Error())
		return respData, err
	}

	// Parse search results
	defer func() {
		if err := resp.Body.Close(); err != nil {
			klog.Error(err, "Error parsing search results")
		}
	}()

	body, err := ioutil.ReadAll(resp.Body)

	if err != nil {
		return respData, err
	}

	str1 := string(body[:])
	klog.V(1).Infof("resp: %v", str1)

	if err := json.Unmarshal(body, &respData); err != nil {
		return respData, err
	}

	return respData, nil
}

type GitOpsSyncResource struct {
	Client      client.Client
	Interval    int
	ResourceDir string
	Token       string
	DataSender  DataSender
}

var ExcludeResourceList = []string{"ApplicationSet", "Application", "EndpointSlice", "Pod", "ReplicaSet", "Cluster"}

// Add creates a new argocd cluster Controller and adds it to the Manager with default RBAC.
// The Manager will set fields on the Controller and Start it when the Manager is Started.
func Add(mgr manager.Manager, interval int, resourceDir string) error {
	token := os.Getenv(AccessToken)
	if token == "" {
		token = mgr.GetConfig().BearerToken
	}

	gitopsSyncResc := &GitOpsSyncResource{
		Client:      mgr.GetClient(),
		Interval:    interval,
		ResourceDir: resourceDir,
		Token:       token,
		DataSender:  &HTTPDataSender{},
	}

	// Create resourceDir if it does not exist
	_, err := os.Stat(resourceDir)
	if err != nil && os.IsNotExist(err) {
		err = os.MkdirAll(resourceDir, 0750)
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
				klog.V(1).Info(fmt.Sprintf("item: %v", itemmap))

				_ = r.createOrUpdateAppSetReportConditions(appReportsMap, itemmap, managedcluster.Name, managedClustersAppNameMap)
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

			if len(related) == 0 {
				klog.Infof("no data for app (%v/%v) found on cluster: %v", appNsn[0], appNsn[1], managedClusterName)
				continue
			}

			v.Statuses.Resources = getResourceMapList(related, managedClusterName)
			klog.V(1).Infof("resources for app (%v/%v): %v", appNsn[0], appNsn[1], v.Statuses.Resources)

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

func (r *GitOpsSyncResource) getSearchURL() (string, error) {
	searchNs := os.Getenv("POD_NAMESPACE")
	if searchNs == "" {
		searchNs = SearchDefaultNs
	}

	svc := &corev1.Service{}
	if err := r.Client.Get(context.TODO(), types.NamespacedName{Name: SearchServiceName, Namespace: searchNs}, svc); err != nil {
		return "", err
	}

	if len(svc.Spec.Ports) == 0 {
		return "", fmt.Errorf("no ports in service: %v/%v", searchNs, SearchServiceName)
	}

	targetPort := svc.Spec.Ports[0].TargetPort.IntVal

	return fmt.Sprintf("https://%v.%v.svc.cluster.local:%v/searchapi/graphql", SearchServiceName, searchNs, targetPort), nil
}

func (r *GitOpsSyncResource) getArgoAppsFromSearch(cluster, appsetNs, appsetName string) ([]interface{}, []interface{}, error) {
	klog.Info(fmt.Sprintf("Start getting argo application for cluster: %v, app: %v/%v", cluster, appsetNs, appsetName))
	defer klog.Info(fmt.Sprintf("Finished getting argo application for cluster: %v, app: %v/%v", cluster, appsetNs, appsetName))

	httpClient := http.DefaultClient

	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
			DualStack: true,
		}).DialContext,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		// #nosec G402
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
			MinVersion:         tls.VersionTLS12,
		},
	}

	httpClient.Transport = transport

	routeURL, err := r.getSearchURL()
	if err != nil {
		return nil, nil, err
	}

	klog.Info(fmt.Sprintf("search url: %v", routeURL))

	// Build search body
	kind := "Application"
	apigroup := "argoproj.io"
	limit := int(-1)
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
		Limit: &limit,
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

	req, err := http.NewRequest(http.MethodPost, routeURL, bytes.NewBuffer(postBody))
	if err != nil {
		return nil, nil, err
	}

	req.Header.Set("Content-Type", "application/json")

	bearer := "Bearer " + r.Token

	req.Header.Add("Authorization", bearer)

	respData, err := r.DataSender.Send(httpClient, req)

	searchResults := respData["data"].(map[string]interface{})["searchResult"].([]interface{})
	if len(searchResults) == 0 {
		return nil, nil, nil
	}

	var items []interface{}
	if i, ok := searchResults[0].(map[string]interface{})["items"]; ok && i != nil {
		items = searchResults[0].(map[string]interface{})["items"].([]interface{})
	}

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
	appNs := appsetResource["namespace"].(string)
	appName := appsetResource["name"].(string)
	hostingAppsetName := appsetResource["_hostingResource"]

	// Skip application that don't belong to an appset
	if hostingAppsetName == nil {
		klog.Infof("skip application %v/%v on cluster %v, it does not belong to an appset", appNs, appName, managedClusterName)

		return nil
	}

	appsetNsn := strings.Split(hostingAppsetName.(string), "/")
	if len(appsetNsn) != 3 {
		err := fmt.Errorf("_hostingResource is not in the correct format: %v", hostingAppsetName)
		klog.Infof(err.Error())

		return err
	}

	reportKey := appsetNsn[1] + "_" + appsetNsn[2]
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

	var operationStateError *appsetreport.Condition

	reportConditions := make([]appsetreport.Condition, 0)
	eCond, _ := regexp.Compile("_condition.*Error")
	wCond, _ := regexp.Compile("_condition.*Warning")

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
			Type:    k[10:],
			Message: v.(string),
		}

		if repCond.Type == "OperationError" {
			operationStateError = repCond
		} else {
			reportConditions = append(reportConditions, *repCond)
		}
	}

	// Check for duplicate operation state in condition before adding it
	if operationStateError != nil {
		foundCondition := false

		for _, con := range reportConditions {
			opStateReason := ""

			index := strings.Index(con.Message, "reason:")
			if index != -1 {
				opStateReason = con.Message[index : len(con.Message)-3]
			}

			if opStateReason != "" && strings.Contains(operationStateError.Message, opStateReason) {
				foundCondition = true

				break
			}
		}

		if !foundCondition {
			reportConditions = append(reportConditions, *operationStateError)
		}
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
	reportJSON, err := yaml.Marshal(report)
	if err != nil {
		klog.Error(err, "error converting report to JSON")
		return err
	}

	reportName := filepath.Join(r.ResourceDir, report.Name+".yaml")
	klog.Info(fmt.Sprintf("writing appset report: %v", reportName))

	if err := ioutil.WriteFile(reportName, reportJSON, 0600); err != nil {
		klog.Error(err, fmt.Sprintf("failed to write appset report yaml file: %v", reportName))
		return err
	}

	return nil
}

func getResourceMapList(related []interface{}, cluster string) []appsetreport.ResourceRef {
	resourceList := make([]appsetreport.ResourceRef, 0)

	for _, rel := range related {
		if relatedmap, ok := rel.(map[string]interface{}); ok {
			klog.Info(fmt.Sprintf("related: %v", relatedmap))

			if slices.Contains(ExcludeResourceList, relatedmap["kind"].(string)) {
				klog.Infof("skip resource kind: %v", relatedmap["kind"].(string))
				continue
			}

			if relatedItems, ok := relatedmap["items"].([]interface{}); ok {
				for _, r := range relatedItems {
					relatedItem := r.(map[string]interface{})
					if itemCluster, ok := relatedItem["cluster"]; !ok || itemCluster != cluster {
						klog.Infof("skip resource from cluster:%v", itemCluster)
						continue
					}

					if ownerUID, ok := relatedItem["_ownerUID"]; ok && ownerUID != "" {
						klog.Infof("skip resource from _ownerUID:%v", ownerUID)
						continue
					}

					resourceRef := getResourceRef(relatedItem)
					klog.V(1).Infof("append resource: %v", resourceRef)
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
		apigroup = _apigroup.(string)
	}

	version := ""
	if _version, ok := relatedItem["apiversion"]; ok && _version != "" {
		version = _version.(string)
	}

	APIVersion := apigroup
	if version != "" {
		APIVersion = APIVersion + "/" + version
	}

	namespace := ""
	if _ns, ok := relatedItem["namespace"]; ok && _ns != "" {
		namespace = _ns.(string)
	}

	repRef := &appsetreport.ResourceRef{
		APIVersion: APIVersion,
		Kind:       relatedItem["kind"].(string),
		Name:       relatedItem["name"].(string),
		Namespace:  namespace,
	}

	return repRef
}
