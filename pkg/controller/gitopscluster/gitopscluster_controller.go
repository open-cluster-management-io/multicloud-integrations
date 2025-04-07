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

package gitopscluster

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog"
	spokeclusterv1 "open-cluster-management.io/api/cluster/v1"
	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	gitopsclusterV1beta1 "open-cluster-management.io/multicloud-integrations/pkg/apis/apps/v1beta1"
	"open-cluster-management.io/multicloud-integrations/pkg/utils"

	yaml "gopkg.in/yaml.v3"
	rbacv1 "k8s.io/api/rbac/v1"
	k8errors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	authv1beta1 "open-cluster-management.io/managed-serviceaccount/apis/authentication/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// ReconcileGitOpsCluster reconciles a GitOpsCluster object.
type ReconcileGitOpsCluster struct {
	client.Client
	dynamic.DynamicClient
	authClient kubernetes.Interface
	scheme     *runtime.Scheme
	lock       sync.Mutex
}

// TokenConfig defines a token configuration used in ArgoCD cluster secret
type TokenConfig struct {
	BearerToken     string `json:"bearerToken"`
	TLSClientConfig struct {
		Insecure bool `json:"insecure"`
	} `json:"tlsClientConfig"`
}

// Add creates a new argocd cluster Controller and adds it to the Manager with default RBAC.
// The Manager will set fields on the Controller and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	reconciler, err := newReconciler(mgr)
	if err != nil {
		return err
	}

	return add(mgr, reconciler)
}

var _ reconcile.Reconciler = &ReconcileGitOpsCluster{}

var errInvalidPlacementRef = errors.New("invalid placement reference")

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) (reconcile.Reconciler, error) {
	authCfg := mgr.GetConfig()
	kubeClient := kubernetes.NewForConfigOrDie(authCfg)

	dynamicClient, err := dynamic.NewForConfig(authCfg)
	if err != nil {
		klog.Error("failed to create dynamic client, error: ", err)

		return nil, err
	}

	dsRS := &ReconcileGitOpsCluster{
		Client:        mgr.GetClient(),
		DynamicClient: *dynamicClient,
		scheme:        mgr.GetScheme(),
		authClient:    kubeClient,
		lock:          sync.Mutex{},
	}

	return dsRS, nil
}

type placementDecisionMapper struct {
	client.Client
}

func (mapper *placementDecisionMapper) Map(ctx context.Context, obj *clusterv1beta1.PlacementDecision) []reconcile.Request {
	var requests []reconcile.Request

	gitOpsClusterList := &gitopsclusterV1beta1.GitOpsClusterList{}
	listopts := &client.ListOptions{Namespace: obj.GetNamespace()}
	err := mapper.List(context.TODO(), gitOpsClusterList, listopts)

	if err != nil {
		klog.Error("failed to list GitOpsClusters, error:", err)
	}

	labels := obj.GetLabels()

	// if placementDecision is created/updated/deleted, its relative GitOpsCluster should be reconciled.
	for _, gitOpsCluster := range gitOpsClusterList.Items {
		if strings.EqualFold(gitOpsCluster.Spec.PlacementRef.Name, labels["cluster.open-cluster-management.io/placement"]) &&
			strings.EqualFold(gitOpsCluster.Namespace, obj.GetNamespace()) {
			klog.Infof("Placement decision %s/%s affects GitOpsCluster %s/%s",
				obj.GetNamespace(),
				obj.GetName(),
				gitOpsCluster.Namespace,
				gitOpsCluster.Name)

			requests = append(requests, reconcile.Request{NamespacedName: types.NamespacedName{Namespace: gitOpsCluster.Namespace, Name: gitOpsCluster.Name}})

			// just one GitOpsCluster is enough. The reconcile will process all GitOpsClusters
			break
		}
	}

	klog.Info("Out placement decision mapper with requests:", requests)

	return requests
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("gitopscluster-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	if utils.IsReadyACMClusterRegistry(mgr.GetAPIReader()) {
		// Watch gitopscluster changes
		err = c.Watch(
			source.Kind(
				mgr.GetCache(),
				&gitopsclusterV1beta1.GitOpsCluster{},
				&handler.TypedEnqueueRequestForObject[*gitopsclusterV1beta1.GitOpsCluster]{},
				utils.GitOpsClusterPredicateFunc,
			),
		)

		if err != nil {
			return err
		}

		// Watch for managed cluster secret changes in argo or managed cluster namespaces
		// The manager started with cache that filters all other secrets so no predicate needed
		err = c.Watch(
			source.Kind(
				mgr.GetCache(),
				&v1.Secret{},
				&handler.TypedEnqueueRequestForObject[*v1.Secret]{},
				utils.ManagedClusterSecretPredicateFunc,
			),
		)

		if err != nil {
			return err
		}

		// Watch cluster list changes in placement decision
		pdMapper := &placementDecisionMapper{mgr.GetClient()}
		err = c.Watch(
			source.Kind(
				mgr.GetCache(),
				&clusterv1beta1.PlacementDecision{},
				handler.TypedEnqueueRequestsFromMapFunc[*clusterv1beta1.PlacementDecision](pdMapper.Map),
				utils.PlacementDecisionPredicateFunc,
			),
		)

		if err != nil {
			return err
		}

		// Watch cluster changes to update cluster labels
		err = c.Watch(
			source.Kind(
				mgr.GetCache(),
				&spokeclusterv1.ManagedCluster{},
				&handler.TypedEnqueueRequestForObject[*spokeclusterv1.ManagedCluster]{},
				utils.ClusterPredicateFunc,
			),
		)
		if err != nil {
			return err
		}

		// Watch changes to Managed service account's tokenSecretRef
		if utils.IsReadyManagedServiceAccount(mgr.GetAPIReader()) {
			err = c.Watch(
				source.Kind(
					mgr.GetCache(),
					&authv1beta1.ManagedServiceAccount{},
					&handler.TypedEnqueueRequestForObject[*authv1beta1.ManagedServiceAccount]{},
					utils.ManagedServiceAccountPredicateFunc,
				),
			)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (r *ReconcileGitOpsCluster) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	klog.Info("Reconciling GitOpsClusters for watched resource change: ", request.NamespacedName)

	// Get all existing GitOps managed cluster secrets, not the ones from the managed cluster namespaces
	managedClusterSecretsInArgo, err := r.GetAllManagedClusterSecretsInArgo()

	if err != nil {
		klog.Error("failed to get all existing managed cluster secrets for ArgoCD, ", err)
		return reconcile.Result{Requeue: false}, nil
	}

	// Then save it in a map. As we create/update GitOps managed cluster secrets while
	// reconciling each GitOpsCluster resource, remove the secret from this list.
	// After reconciling all GitOpsCluster resources, the secrets left in this list are
	// orphan secrets to be removed.
	orphanGitOpsClusterSecretList := map[types.NamespacedName]string{}

	for _, secret := range managedClusterSecretsInArgo.Items {
		orphanGitOpsClusterSecretList[types.NamespacedName{Name: secret.Name, Namespace: secret.Namespace}] = secret.Namespace + "/" + secret.Name
	}

	// Get all GitOpsCluster resources
	gitOpsClusters, err := r.GetAllGitOpsClusters()

	if err != nil {
		return reconcile.Result{Requeue: false}, nil
	}

	var returnErr error

	var returnRequeueInterval int

	// For any watched resource change, process all GitOpsCluster CRs to create new secrets or update existing secrets.
	for _, gitOpsCluster := range gitOpsClusters.Items {
		klog.Info("Process GitOpsCluster: " + gitOpsCluster.Namespace + "/" + gitOpsCluster.Name)

		instance := &gitopsclusterV1beta1.GitOpsCluster{}

		err := r.Get(context.TODO(), types.NamespacedName{Name: gitOpsCluster.Name, Namespace: gitOpsCluster.Namespace}, instance)

		if err != nil && k8errors.IsNotFound(err) {
			klog.Infof("GitOpsCluster %s/%s deleted", gitOpsCluster.Namespace, gitOpsCluster.Name)
			// deleted? just skip to the next GitOpsCluster resource
			continue
		}

		// reconcile one GitOpsCluster resource
		requeueInterval, err := r.reconcileGitOpsCluster(*instance, orphanGitOpsClusterSecretList)

		if err != nil {
			klog.Error(err.Error())

			returnErr = err
			returnRequeueInterval = requeueInterval
		}
	}

	// Remove all invalid/orphan GitOps cluster secrets
	if !r.cleanupOrphanSecrets(orphanGitOpsClusterSecretList) {
		// If it failed to delete orphan GitOps managed cluster secrets, reconile again in 10 minutes.
		if returnErr == nil {
			return reconcile.Result{Requeue: true, RequeueAfter: time.Duration(10) * time.Minute}, err
		}
	}

	if returnErr != nil {
		klog.Info("reconcile failed, requeue")

		return reconcile.Result{Requeue: true, RequeueAfter: time.Duration(returnRequeueInterval) * time.Minute}, returnErr
	}

	return reconcile.Result{}, nil
}

func (r *ReconcileGitOpsCluster) cleanupOrphanSecrets(orphanGitOpsClusterSecretList map[types.NamespacedName]string) bool {
	cleanupSuccessful := true

	// 4. Delete all orphan GitOps managed cluster secrets
	for key, secretName := range orphanGitOpsClusterSecretList {
		secretToDelete := &v1.Secret{}

		err := r.Get(context.TODO(), key, secretToDelete)

		if err == nil {
			klog.Infof("Deleting orphan GitOps managed cluster secret %s", secretName)

			err = r.Delete(context.TODO(), secretToDelete)

			if err != nil {
				klog.Errorf("failed to delete orphan managed cluster secret %s, err: %s", key, err.Error())

				cleanupSuccessful = false

				continue
			}
		}
	}

	return cleanupSuccessful
}

func (r *ReconcileGitOpsCluster) reconcileGitOpsCluster(
	gitOpsCluster gitopsclusterV1beta1.GitOpsCluster,
	orphanSecretsList map[types.NamespacedName]string) (int, error) {
	instance := gitOpsCluster.DeepCopy()

	annotations := instance.GetAnnotations()

	// Create default policy template
	createPolicyTemplate := false
	if instance.Spec.CreatePolicyTemplate != nil {
		createPolicyTemplate = *instance.Spec.CreatePolicyTemplate
	}

	if createPolicyTemplate && instance.Spec.PlacementRef != nil &&
		instance.Spec.PlacementRef.Kind == "Placement" &&
		instance.Spec.ManagedServiceAccountRef != "" {
		if err := r.createNamespaceScopedResourceFromYAML(generatePlacementYamlString(*instance)); err != nil {
			klog.Error("failed to create default policy template local placement: ", err)
		}

		if err := r.createNamespaceScopedResourceFromYAML(generatePlacementBindingYamlString(*instance)); err != nil {
			klog.Error("failed to create default policy template local placement binding, ", err)
		}

		if err := r.createNamespaceScopedResourceFromYAML(generatePolicyTemplateYamlString(*instance)); err != nil {
			klog.Error("failed to create default policy template, ", err)
		}
	}

	// 1. Verify that spec.argoServer.argoNamespace is a valid ArgoCD namespace
	// skipArgoNamespaceVerify annotation just in case the service labels we use for verification change in future
	if !r.VerifyArgocdNamespace(gitOpsCluster.Spec.ArgoServer.ArgoNamespace) &&
		annotations["skipArgoNamespaceVerify"] != "true" {
		klog.Info("invalid argocd namespace because argo server pod was not found")

		instance.Status.LastUpdateTime = metav1.Now()
		instance.Status.Phase = "failed"
		instance.Status.Message = "invalid gitops namespace because argo server pod was not found"

		err := r.Client.Status().Update(context.TODO(), instance)

		if err != nil {
			klog.Errorf("failed to update GitOpsCluster %s status, will try again: %s", instance.Namespace+"/"+instance.Name, err)
			return 1, err
		}

		return 1, errors.New("invalid gitops namespace because argo server pod was not found, will try again")
	}

	// 1a. Add configMaps to be used by ArgoCD ApplicationSets
	err := r.CreateApplicationSetConfigMaps(gitOpsCluster.Spec.ArgoServer.ArgoNamespace)
	if err != nil {
		klog.Warningf("there was a problem creating the configMaps: %v", err.Error())
	}

	// 1b. Add roles so applicationset-controller can read placementRules and placementDecisions
	err = r.CreateApplicationSetRbac(gitOpsCluster.Spec.ArgoServer.ArgoNamespace)
	if err != nil {
		klog.Warningf("there was a problem creating the role or binding: %v", err.Error())
	}

	// 2. Get the list of managed clusters
	// The placement must be in the same namespace as GitOpsCluster
	managedClusters, err := r.GetManagedClusters(instance.Namespace, *instance.Spec.PlacementRef)
	// 2a. Get the placement decision
	// 2b. Get the managed cluster names from the placement decision
	if err != nil {
		klog.Info("failed to get managed cluster list")

		instance.Status.LastUpdateTime = metav1.Now()
		instance.Status.Phase = "failed"
		instance.Status.Message = err.Error()

		err2 := r.Client.Status().Update(context.TODO(), instance)

		if err2 != nil {
			klog.Errorf("failed to update GitOpsCluster %s status, will try again in 3 minutes: %s", instance.Namespace+"/"+instance.Name, err2)
			return 3, err2
		}
	}

	managedClusterNames := []string{}
	for _, managedCluster := range managedClusters {
		managedClusterNames = append(managedClusterNames, managedCluster.Name)
	}

	klog.V(1).Infof("adding managed clusters %v into argo namespace %s", managedClusterNames, instance.Spec.ArgoServer.ArgoNamespace)

	// 3. Copy secret contents from the managed cluster namespaces and create the secret in spec.argoServer.argoNamespace
	// if spec.createBlankClusterSecrets is true then do err on missing secret from the managed cluster namespace
	createBlankClusterSecrets := false
	if instance.Spec.CreateBlankClusterSecrets != nil {
		createBlankClusterSecrets = *instance.Spec.CreateBlankClusterSecrets
	}

	err = r.AddManagedClustersToArgo(instance, managedClusters, orphanSecretsList, createBlankClusterSecrets)

	if err != nil {
		klog.Info("failed to add managed clusters to argo")

		instance.Status.LastUpdateTime = metav1.Now()
		instance.Status.Phase = "failed"
		instance.Status.Message = err.Error()

		err2 := r.Client.Status().Update(context.TODO(), instance)

		if err2 != nil {
			klog.Errorf("failed to update GitOpsCluster %s status, will try again in 3 minutes: %s", instance.Namespace+"/"+instance.Name, err2)
			return 3, err2
		}

		// it passed all vaidations but simply failed to create or update secrets. Reconile again in 1 minute.
		return 1, err
	}

	instance.Status.LastUpdateTime = metav1.Now()
	instance.Status.Phase = "successful"

	managedClustersStr := strings.Join(managedClusterNames, " ")
	if len(managedClustersStr) > 4096 {
		managedClustersStr = fmt.Sprintf("%.4096v", managedClustersStr) + "..."
	}

	instance.Status.Message = fmt.Sprintf("Added managed clusters [%v] to gitops namespace %s", managedClustersStr, instance.Spec.ArgoServer.ArgoNamespace)

	err = r.Client.Status().Update(context.TODO(), instance)

	if err != nil {
		klog.Errorf("failed to update GitOpsCluster %s status, will try again in 3 minutes: %s", instance.Namespace+"/"+instance.Name, err)
		return 3, err
	}

	return 0, nil
}

// GetAllManagedClusterSecretsInArgo returns list of secrets from all GitOps managed cluster
func (r *ReconcileGitOpsCluster) GetAllManagedClusterSecretsInArgo() (v1.SecretList, error) {
	klog.Info("Getting all managed cluster secrets from argo namespaces")

	secretList := &v1.SecretList{}
	listopts := &client.ListOptions{}

	secretSelector := &metav1.LabelSelector{
		MatchLabels: map[string]string{
			"apps.open-cluster-management.io/acm-cluster": "true",
			"argocd.argoproj.io/secret-type":              "cluster",
		},
	}

	secretSelectionLabel, err := utils.ConvertLabels(secretSelector)
	if err != nil {
		klog.Error("Failed to convert managed cluster secret selector, err:", err)
		return *secretList, err
	}

	listopts.LabelSelector = secretSelectionLabel
	err = r.List(context.TODO(), secretList, listopts)

	if err != nil {
		klog.Error("Failed to list managed cluster secrets in argo, err:", err)
		return *secretList, err
	}

	return *secretList, nil
}

// GetAllManagedClusterSecretsInArgo returns list of secrets from all GitOps managed cluster.
// these secrets are not gnerated by ACM ArgoCD push model, they are created by end users themselves
func (r *ReconcileGitOpsCluster) GetAllNonAcmManagedClusterSecretsInArgo(argoNs string) (map[string][]*v1.Secret, error) {
	klog.Info("Getting all non-acm managed cluster secrets from argo namespaces")

	secretMap := make(map[string][]*v1.Secret, 0)

	secretList := &v1.SecretList{}
	listopts := &client.ListOptions{}

	secretSelector := &metav1.LabelSelector{
		MatchLabels: map[string]string{
			"argocd.argoproj.io/secret-type": "cluster",
		},
	}

	secretSelectionLabel, err := utils.ConvertLabels(secretSelector)
	if err != nil {
		klog.Error("Failed to convert managed cluster secret selector, err:", err)
		return secretMap, err
	}

	listopts.Namespace = argoNs

	listopts.LabelSelector = secretSelectionLabel
	err = r.List(context.TODO(), secretList, listopts)

	if err != nil {
		klog.Error("Failed to list managed cluster secrets in argo, err:", err)
		return secretMap, err
	}

	// Add non-ACM secrets to map by cluster name
	for i := range secretList.Items {
		s := secretList.Items[i]

		_, acmcluster := s.Labels["apps.open-cluster-management.io/acm-cluster"]
		if !acmcluster {
			cluster := s.Data["name"]

			if cluster != nil {
				secrets := secretMap[string(cluster)]
				if secrets == nil {
					secrets = []*v1.Secret{}
				}

				secrets = append(secrets, &s)
				secretMap[string(cluster)] = secrets
			}
		}
	}

	return secretMap, nil
}

// GetAllGitOpsClusters returns all GitOpsCluster CRs
func (r *ReconcileGitOpsCluster) GetAllGitOpsClusters() (gitopsclusterV1beta1.GitOpsClusterList, error) {
	klog.Info("Getting all GitOpsCluster resources")

	gitOpsClusterList := &gitopsclusterV1beta1.GitOpsClusterList{}

	err := r.List(context.TODO(), gitOpsClusterList)

	if err != nil {
		klog.Error("Failed to list GitOpsCluster resources, err:", err)
		return *gitOpsClusterList, err
	}

	return *gitOpsClusterList, nil
}

// VerifyArgocdNamespace verifies that the given argoNamespace is a valid namspace by verifying that ArgoCD is actually
// installed in that namespace
func (r *ReconcileGitOpsCluster) VerifyArgocdNamespace(argoNamespace string) bool {
	return r.FindServiceWithLabelsAndNamespace(argoNamespace,
		map[string]string{"app.kubernetes.io/component": "server", "app.kubernetes.io/part-of": "argocd"})
}

// FindServiceWithLabelsAndNamespace finds a list of services with provided labels from the specified namespace
func (r *ReconcileGitOpsCluster) FindServiceWithLabelsAndNamespace(namespace string, labels map[string]string) bool {
	serviceList := &v1.ServiceList{}
	listopts := &client.ListOptions{}

	serviceSelector := &metav1.LabelSelector{
		MatchLabels: labels,
	}

	serviceLabels, err := utils.ConvertLabels(serviceSelector)
	if err != nil {
		klog.Error("Failed to convert label selector, err:", err)
		return false
	}

	listopts.LabelSelector = serviceLabels
	listopts.Namespace = namespace
	err = r.List(context.TODO(), serviceList, listopts)

	if err != nil {
		klog.Error("Failed to list services, err:", err)
		return false
	}

	if len(serviceList.Items) == 0 {
		klog.Infof("No service with labels %v found", labels)
		return false
	}

	for _, service := range serviceList.Items {
		klog.Info("Found service ", service.GetName(), " in namespace ", service.GetNamespace())
	}

	return true
}

const configMapNameOld = "acm-placementrule"
const configMapNameNew = "acm-placement"

func getConfigMapDuck(configMapName string, namespace string, apiVersion string, kind string) v1.ConfigMap {
	return v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      configMapName,
			Namespace: namespace,
		},
		Data: map[string]string{
			"apiVersion":    apiVersion,
			"kind":          kind,
			"statusListKey": "decisions",
			"matchKey":      "clusterName",
		},
	}
}

const RoleSuffix = "-applicationset-controller-placement"

func getRoleDuck(namespace string) *rbacv1.Role {
	return &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{Name: namespace + RoleSuffix, Namespace: namespace},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{"apps.open-cluster-management.io"},
				Resources: []string{"placementrules"},
				Verbs:     []string{"list"},
			},
			{
				APIGroups: []string{"cluster.open-cluster-management.io"},
				Resources: []string{"placementdecisions"},
				Verbs:     []string{"list"},
			},
		},
	}
}

func (r *ReconcileGitOpsCluster) getAppSetServiceAccountName(namespace string) string {
	saName := namespace + RoleSuffix // if every attempt fails, use this name

	// First, try to get the applicationSet controller service account by label
	saList := &v1.ServiceAccountList{}

	listopts := &client.ListOptions{Namespace: namespace}

	saSelector := &metav1.LabelSelector{
		MatchLabels: map[string]string{
			"app.kubernetes.io/part-of": "argocd-applicationset",
		},
	}

	saSelectionLabel, err := utils.ConvertLabels(saSelector)

	if err != nil {
		klog.Error("Failed to convert managed cluster secret selector, err:", err)
	} else {
		listopts.LabelSelector = saSelectionLabel
	}

	err = r.List(context.TODO(), saList, listopts)

	if err != nil {
		klog.Error("Failed to get service account list, err:", err) // Just return the default SA name

		return saName
	}

	if len(saList.Items) == 1 {
		klog.Info("found the application set controller service account name by label: " + saList.Items[0].Name)

		return saList.Items[0].Name
	}

	// find the SA name that ends with -applicationset-controller
	for _, sa := range saList.Items {
		if strings.HasSuffix(sa.Name, "-applicationset-controller") {
			klog.Info("found the application set controller service account name from list: " + sa.Name)

			return sa.Name
		}
	}

	klog.Warning("could not find application set controller service account name")

	return saName
}

func (r *ReconcileGitOpsCluster) getRoleBindingDuck(namespace string) *rbacv1.RoleBinding {
	saName := r.getAppSetServiceAccountName(namespace)

	return &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: namespace + RoleSuffix, Namespace: namespace},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "Role",
			Name:     namespace + RoleSuffix,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      saName,
				Namespace: namespace,
			},
		},
	}
}

// ApplyApplicationSetConfigMaps creates the required configMap to allow ArgoCD ApplicationSet
// to identify our two forms of placement
func (r *ReconcileGitOpsCluster) CreateApplicationSetConfigMaps(namespace string) error {
	if namespace == "" {
		return errors.New("no namespace provided")
	}

	// Create two configMaps, one for placementrules.apps and placementdecisions.cluster
	maps := []v1.ConfigMap{
		getConfigMapDuck(configMapNameOld, namespace, "apps.open-cluster-management.io/v1", "placementrules"),
		getConfigMapDuck(configMapNameNew, namespace, "cluster.open-cluster-management.io/v1beta1", "placementdecisions"),
	}

	for i := range maps {
		configMap := v1.ConfigMap{}

		err := r.Get(context.Background(), types.NamespacedName{Namespace: namespace, Name: maps[i].Name}, &configMap)

		if err != nil && strings.Contains(err.Error(), " not found") {
			err = r.Create(context.Background(), &maps[i])
			if err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
	}

	return nil
}

// CreateApplicationSetRbac sets up required role and roleBinding so that the applicationset-controller
// can work with placementRules and placementDecisions
func (r *ReconcileGitOpsCluster) CreateApplicationSetRbac(namespace string) error {
	if namespace == "" {
		return errors.New("no namespace provided")
	}

	err := r.Get(context.Background(), types.NamespacedName{Name: namespace + RoleSuffix, Namespace: namespace}, &rbacv1.Role{})
	if k8errors.IsNotFound(err) {
		klog.Infof("creating role %s, in namespace %s", namespace+RoleSuffix, namespace)

		err = r.Create(context.Background(), getRoleDuck(namespace))
		if err != nil {
			return err
		}
	}

	err = r.Get(context.Background(), types.NamespacedName{Name: namespace + RoleSuffix, Namespace: namespace}, &rbacv1.RoleBinding{})
	if k8errors.IsNotFound(err) {
		klog.Infof("creating roleBinding %s, in namespace %s", namespace+RoleSuffix, namespace)

		err = r.Create(context.Background(), r.getRoleBindingDuck(namespace))
		if err != nil {
			return err
		}
	}

	return nil
}

// GetManagedClusters retrieves managed cluster names from placement decision
func (r *ReconcileGitOpsCluster) GetManagedClusters(namespace string, placementref v1.ObjectReference) ([]*spokeclusterv1.ManagedCluster, error) {
	if !(placementref.Kind == "Placement" &&
		(strings.EqualFold(placementref.APIVersion, "cluster.open-cluster-management.io/v1alpha1") ||
			strings.EqualFold(placementref.APIVersion, "cluster.open-cluster-management.io/v1beta1"))) {
		klog.Error("Invalid Kind or APIVersion, kind: " + placementref.Kind + " apiVerion: " + placementref.APIVersion)
		return nil, errInvalidPlacementRef
	}

	placement := &clusterv1beta1.Placement{}
	err := r.Get(context.TODO(), types.NamespacedName{Namespace: namespace, Name: placementref.Name}, placement)

	if err != nil {
		klog.Error("failed to get placement. err: ", err.Error())
		return nil, err
	}

	klog.Infof("looking for placement decisions for placement %s", placementref.Name)

	placementDecisions := &clusterv1beta1.PlacementDecisionList{}

	listopts := &client.ListOptions{Namespace: namespace}

	secretSelector := &metav1.LabelSelector{
		MatchLabels: map[string]string{
			"cluster.open-cluster-management.io/placement": placementref.Name,
		},
	}

	placementDecisionSelectionLabel, err := utils.ConvertLabels(secretSelector)
	if err != nil {
		klog.Error("Failed to convert placement decision selector, err:", err)
		return nil, err
	}

	listopts.LabelSelector = placementDecisionSelectionLabel
	err = r.List(context.TODO(), placementDecisions, listopts)

	if err != nil {
		klog.Error("Failed to list placement decisions, err:", err)
		return nil, err
	}

	if len(placementDecisions.Items) < 1 {
		klog.Info("no placement decision found for placement: " + placementref.Name)
		return nil, errors.New("no placement decision found for placement: " + placementref.Name)
	}

	clusterList := &spokeclusterv1.ManagedClusterList{}

	err = r.List(context.TODO(), clusterList, &client.ListOptions{})
	if err != nil {
		klog.Error("Failed to list managed clusters, err:", err)
		return nil, err
	}

	clusterMap := make(map[string]*spokeclusterv1.ManagedCluster)
	for i, cluster := range clusterList.Items {
		clusterMap[cluster.Name] = &clusterList.Items[i]
	}

	clusters := make([]*spokeclusterv1.ManagedCluster, 0)

	for _, placementdecision := range placementDecisions.Items {
		klog.Info("getting cluster names from placement decision " + placementdecision.Name)

		for _, clusterDecision := range placementdecision.Status.Decisions {
			klog.Info("cluster name: " + clusterDecision.ClusterName)

			if cluster, ok := clusterMap[clusterDecision.ClusterName]; ok {
				clusters = append(clusters, cluster)
			} else {
				klog.Info("could not find managed cluster: " + clusterDecision.ClusterName)
			}
		}
	}

	return clusters, nil
}

const componentName = "application-manager"

// AddManagedClustersToArgo copies a managed cluster secret from the managed cluster namespace to ArgoCD namespace
func (r *ReconcileGitOpsCluster) AddManagedClustersToArgo(
	gitOpsCluster *gitopsclusterV1beta1.GitOpsCluster, managedClusters []*spokeclusterv1.ManagedCluster,
	orphanSecretsList map[types.NamespacedName]string, createBlankClusterSecrets bool) error {
	returnErr := errors.New("")
	errorOccurred := false
	argoNamespace := gitOpsCluster.Spec.ArgoServer.ArgoNamespace

	nonAcmClusterSecrets, err := r.GetAllNonAcmManagedClusterSecretsInArgo(argoNamespace)
	if err != nil {
		klog.Error("failed to get all non-acm managed cluster secrets. err: ", err.Error())

		return err
	}

	for _, managedCluster := range managedClusters {
		klog.Infof("adding managed cluster %s to gitops namespace %s", managedCluster.Name, argoNamespace)

		var newSecret *v1.Secret
		msaExists := false

		// Check if there are existing non-acm created cluster secrets
		if len(nonAcmClusterSecrets[managedCluster.Name]) > 0 {
			returnErr = fmt.Errorf("founding existing non-ACM ArgoCD clusters secrets for cluster: %v", managedCluster)
			klog.Error(returnErr.Error())

			errorOccurred = true

			continue
		}

		if createBlankClusterSecrets || gitOpsCluster.Spec.ManagedServiceAccountRef == "" {
			secretName := managedCluster.Name + "-cluster-secret"
			managedClusterSecretKey := types.NamespacedName{Name: secretName, Namespace: managedCluster.Name}

			managedClusterSecret := &v1.Secret{}
			err := r.Get(context.TODO(), managedClusterSecretKey, managedClusterSecret)

			if err != nil {
				// try with CreateMangedClusterSecretFromManagedServiceAccount generated name
				secretName = managedCluster.Name + "-" + componentName + "-cluster-secret"
				managedClusterSecretKey = types.NamespacedName{Name: secretName, Namespace: managedCluster.Name}
				err = r.Get(context.TODO(), managedClusterSecretKey, managedClusterSecret)
			}

			// managed cluster secret doesn't need to exist for pull model
			if err != nil && !createBlankClusterSecrets {
				// check for a ManagedServiceAccount to see if we need to create the secret
				ManagedServiceAccount := &authv1beta1.ManagedServiceAccount{}
				ManagedServiceAccountName := types.NamespacedName{Namespace: managedCluster.Name, Name: componentName}
				err = r.Get(context.TODO(), ManagedServiceAccountName, ManagedServiceAccount)

				if err == nil {
					// get the secret
					managedClusterSecretKey = types.NamespacedName{Name: componentName, Namespace: managedCluster.Name}
					err = r.Get(context.TODO(), managedClusterSecretKey, managedClusterSecret)

					if err == nil {
						klog.Infof("Found ManagedServiceAccount %s created by managed cluster %s", componentName, managedCluster.Name)
						msaExists = true
					} else {
						klog.Error("failed to find ManagedServiceAccount created secret application-manager")
					}
				} else {
					klog.Error("failed to find ManagedServiceAccount CR in namespace " + managedCluster.Name)
				}

				if !msaExists {
					klog.Error("failed to get managed cluster secret. err: ", err.Error())

					errorOccurred = true
					returnErr = err

					continue
				}
			}

			if msaExists {
				newSecret, err = r.CreateMangedClusterSecretFromManagedServiceAccount(
					argoNamespace, managedCluster, componentName, false)
			} else {
				newSecret, err = r.CreateManagedClusterSecretInArgo(
					argoNamespace, managedClusterSecret, managedCluster, createBlankClusterSecrets)
			}

			if err != nil {
				klog.Error("failed to create managed cluster secret. err: ", err.Error())

				errorOccurred = true
				returnErr = err

				continue
			}
		} else {
			klog.Infof("create cluster secret using managed service account: %s/%s", managedCluster.Name, gitOpsCluster.Spec.ManagedServiceAccountRef)

			newSecret, err = r.CreateMangedClusterSecretFromManagedServiceAccount(argoNamespace, managedCluster, gitOpsCluster.Spec.ManagedServiceAccountRef, true)
			if err != nil {
				klog.Error("failed to create managed cluster secret. err: ", err.Error())

				errorOccurred = true
				returnErr = err

				continue
			}
		}

		existingManagedClusterSecret := &v1.Secret{}

		err = r.Get(context.TODO(), types.NamespacedName{Name: newSecret.Name, Namespace: newSecret.Namespace}, existingManagedClusterSecret)
		if err == nil {
			klog.Infof("updating managed cluster secret in argo namespace: %v/%v", newSecret.Namespace, newSecret.Name)

			newSecret = unionSecretData(newSecret, existingManagedClusterSecret)

			err := r.Update(context.TODO(), newSecret)

			if err != nil {
				klog.Errorf("failed to update managed cluster secret. name: %v/%v, error: %v", newSecret.Namespace, newSecret.Name, err)

				errorOccurred = true
				returnErr = err

				continue
			}
		} else if k8errors.IsNotFound(err) {
			klog.Infof("creating managed cluster secret in argo namespace: %v/%v", newSecret.Namespace, newSecret.Name)

			err := r.Create(context.TODO(), newSecret)

			if err != nil {
				klog.Errorf("failed to create managed cluster secret. name: %v/%v, error: %v", newSecret.Namespace, newSecret.Name, err)

				errorOccurred = true
				returnErr = err

				continue
			}
		} else {
			klog.Errorf("failed to get managed cluster secret. name: %v/%v, error: %v", newSecret.Namespace, newSecret.Name, err)

			errorOccurred = true
			returnErr = err

			continue
		}

		// Managed cluster secret successfully created/updated - remove from orphan list
		delete(orphanSecretsList, client.ObjectKeyFromObject(newSecret))
	}

	if !errorOccurred {
		return nil
	}

	return returnErr
}

// CreateManagedClusterSecretInArgo creates a managed cluster secret with specific metadata in Argo namespace
func (r *ReconcileGitOpsCluster) CreateManagedClusterSecretInArgo(argoNamespace string, managedClusterSecret *v1.Secret,
	managedCluster *spokeclusterv1.ManagedCluster, createBlankClusterSecrets bool) (*v1.Secret, error) {
	// create the new cluster secret in the argocd server namespace
	var newSecret *v1.Secret

	clusterURL := ""

	if createBlankClusterSecrets {
		newSecret = &v1.Secret{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Secret",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      managedCluster.Name + "-cluster-secret",
				Namespace: argoNamespace,
				Labels: map[string]string{
					"argocd.argoproj.io/secret-type":                 "cluster",
					"apps.open-cluster-management.io/acm-cluster":    "true",
					"apps.open-cluster-management.io/cluster-name":   managedCluster.Name,
					"apps.open-cluster-management.io/cluster-server": managedCluster.Name + "-control-plane", // dummy value for pull model
				},
			},
			Type: "Opaque",
			StringData: map[string]string{
				"name":   managedCluster.Name,
				"server": "https://" + managedCluster.Name + "-control-plane", // dummy value for pull model
			},
		}
	} else {
		if string(managedClusterSecret.Data["server"]) == "" {
			clusterToken, err := getManagedClusterToken(managedClusterSecret.Data["config"])
			if err != nil {
				klog.Error(err)

				return nil, err
			}

			clusterURL, err = getManagedClusterURL(managedCluster, clusterToken)
			if err != nil {
				klog.Error(err)

				return nil, err
			}
		} else {
			clusterURL = string(managedClusterSecret.Data["server"])
		}

		labels := managedClusterSecret.GetLabels()

		newSecret = &v1.Secret{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Secret",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      managedClusterSecret.Name,
				Namespace: argoNamespace,
				Labels: map[string]string{
					"argocd.argoproj.io/secret-type":                 "cluster",
					"apps.open-cluster-management.io/acm-cluster":    "true",
					"apps.open-cluster-management.io/cluster-name":   labels["apps.open-cluster-management.io/cluster-name"],
					"apps.open-cluster-management.io/cluster-server": labels["apps.open-cluster-management.io/cluster-server"],
				},
			},
			Type: "Opaque",
			StringData: map[string]string{
				"config": string(managedClusterSecret.Data["config"]),
				"name":   string(managedClusterSecret.Data["name"]),
				"server": clusterURL,
			},
		}
	}

	// Collect labels to add to the secret
	// Labels created above have precedence
	for key, val := range managedCluster.Labels {
		if _, ok := newSecret.Labels[key]; !ok {
			newSecret.Labels[key] = val
		}
	}

	return newSecret, nil
}

func (r *ReconcileGitOpsCluster) CreateMangedClusterSecretFromManagedServiceAccount(argoNamespace string,
	managedCluster *spokeclusterv1.ManagedCluster, managedServiceAccountRef string, enableTLS bool) (*v1.Secret, error) {
	// Find managedserviceaccount in the managed cluster namespace
	account := &authv1beta1.ManagedServiceAccount{}
	if err := r.Get(context.TODO(), types.NamespacedName{Name: managedServiceAccountRef, Namespace: managedCluster.Name}, account); err != nil {
		klog.Errorf("failed to get managed service account: %v/%v", managedCluster.Name, managedServiceAccountRef)

		return nil, err
	}

	// Get secret from managedserviceaccount
	tokenSecretRef := account.Status.TokenSecretRef
	if tokenSecretRef == nil {
		err := fmt.Errorf("no token reference secret found in the managed service account: %v/%v", managedCluster.Name, managedServiceAccountRef)
		klog.Error(err)

		return nil, err
	}

	tokenSecret := &v1.Secret{}
	if err := r.Get(context.TODO(), types.NamespacedName{Name: tokenSecretRef.Name, Namespace: managedCluster.Name}, tokenSecret); err != nil {
		klog.Errorf("failed to get token secret: %v/%v", managedCluster.Name, tokenSecretRef.Name)

		return nil, err
	}

	clusterSecretName := fmt.Sprintf("%v-%v-cluster-secret", managedCluster.Name, managedServiceAccountRef)

	tlsClientConfig := map[string]interface{}{
		"insecure": true,
	}
	caCrt := base64.StdEncoding.EncodeToString(tokenSecret.Data["ca.crt"])
	if enableTLS {
		tlsClientConfig = map[string]interface{}{
			"insecure": false,
			"caData":   caCrt,
		}
	}

	config := map[string]interface{}{
		"bearerToken":     string(tokenSecret.Data["token"]),
		"tlsClientConfig": tlsClientConfig,
	}

	encodedConfig, err := json.Marshal(config)
	if err != nil {
		klog.Error(err, "failed to encode data for the cluster secret")

		return nil, err
	}

	clusterURL, err := getManagedClusterURL(managedCluster, string(tokenSecret.Data["token"]))
	if err != nil {
		klog.Error(err)

		return nil, err
	}

	klog.Infof("managed cluster %v, URL: %v", managedCluster.Name, clusterURL)

	// For use in label - remove the protocol and port (contains invalid characters for label)
	strippedClusterURL := clusterURL

	index := strings.Index(strippedClusterURL, "://")
	if index > 0 {
		strippedClusterURL = strippedClusterURL[index+3:]
	}

	index = strings.Index(strippedClusterURL, ":")
	if index > 0 {
		strippedClusterURL = strippedClusterURL[:index]
	}

	newSecret := &v1.Secret{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Secret",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterSecretName,
			Namespace: argoNamespace,
			Labels: map[string]string{
				"argocd.argoproj.io/secret-type":                 "cluster",
				"apps.open-cluster-management.io/acm-cluster":    "true",
				"apps.open-cluster-management.io/cluster-name":   managedCluster.Name,
				"apps.open-cluster-management.io/cluster-server": fmt.Sprintf("%.63s", strippedClusterURL),
				"cluster.open-cluster-management.io/backup":      "",
			},
		},
		Type: "Opaque",
		StringData: map[string]string{
			"config": string(encodedConfig),
			"name":   managedCluster.Name,
			"server": clusterURL,
		},
	}

	// Collect labels to add to the secret
	// Labels created above have precedence
	for key, val := range managedCluster.Labels {
		if _, ok := newSecret.Labels[key]; !ok {
			newSecret.Labels[key] = val
		}
	}

	return newSecret, nil
}

func unionSecretData(newSecret, existingSecret *v1.Secret) *v1.Secret {
	// union of labels
	newLabels := newSecret.GetLabels()
	existingLabels := existingSecret.GetLabels()

	if newLabels == nil {
		newLabels = make(map[string]string)
	}

	if existingLabels == nil {
		existingLabels = make(map[string]string)
	}

	for key, val := range existingLabels {
		if _, ok := newLabels[key]; !ok {
			newLabels[key] = val
		}
	}

	newSecret.SetLabels(newLabels)

	// union of annotations (except for kubectl.kubernetes.io/last-applied-configuration)
	newAnnotations := newSecret.GetAnnotations()
	existingAnnotations := existingSecret.GetAnnotations()

	if newAnnotations == nil {
		newAnnotations = make(map[string]string)
	}

	if existingAnnotations == nil {
		existingAnnotations = make(map[string]string)
	}

	for key, val := range existingAnnotations {
		if _, ok := newAnnotations[key]; !ok {
			if key != "kubectl.kubernetes.io/last-applied-configuration" {
				newAnnotations[key] = val
			}
		}
	}

	newSecret.SetAnnotations(newAnnotations)

	// union of data
	newData := newSecret.StringData
	existingData := existingSecret.Data // api never returns stringData as the field is write-only

	if newData == nil {
		newData = make(map[string]string)
	}

	if existingData == nil {
		existingData = make(map[string][]byte)
	}

	for key, val := range existingData {
		if _, ok := newData[key]; !ok {
			newData[key] = string(val[:])
		}
	}

	newSecret.StringData = newData

	return newSecret
}

func getManagedClusterToken(dataConfig []byte) (string, error) {
	if dataConfig == nil {
		return "", fmt.Errorf("empty secrect data config")
	}

	// Unmarshal the decoded JSON into the Config struct
	var config TokenConfig
	err := json.Unmarshal(dataConfig, &config)

	if err != nil {
		return "", fmt.Errorf("failed to unmarshal JSON: %w", err)
	}

	return config.BearerToken, nil
}

func getManagedClusterURL(managedCluster *spokeclusterv1.ManagedCluster, token string) (string, error) {
	clientConfigs := managedCluster.Spec.ManagedClusterClientConfigs
	if len(clientConfigs) == 0 {
		err := fmt.Errorf("no client configs found for managed cluster: %v", managedCluster.Name)

		return "", err
	}

	// If only one clientconfig, always return the first
	if len(clientConfigs) == 1 {
		return clientConfigs[0].URL, nil
	}

	for _, config := range clientConfigs {
		req, err := http.NewRequest(http.MethodGet, config.URL, nil)
		if err != nil {
			klog.Infof("error building new http request to %v", config.URL)

			continue
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Add("Authorization", "Bearer "+token)

		caCertPool := x509.NewCertPool()
		caCertPool.AppendCertsFromPEM(config.CABundle)

		httpClient := http.DefaultClient

		httpClient.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs:    caCertPool,
				MinVersion: gitopsclusterV1beta1.TLSMinVersionInt, //#nosec G402
			},
		}

		resp, err := httpClient.Do(req)
		if err == nil {
			return config.URL, nil
		}

		defer func() {
			if resp != nil {
				if err := resp.Body.Close(); err != nil {
					klog.Error("Error closing response: ", err)
				}
			}
		}()

		klog.Infof("error sending http request to %v, error: %v", config.URL, err.Error())
	}

	err := fmt.Errorf("failed to find an accessible URL for the managed cluster: %v", managedCluster.Name)

	return "", err
}

func (r *ReconcileGitOpsCluster) createNamespaceScopedResourceFromYAML(yamlString string) error {
	var obj map[string]interface{}
	if err := yaml.Unmarshal([]byte(yamlString), &obj); err != nil {
		klog.Error("failed to unmarshal yaml string: ", err)

		return err
	}

	unstructuredObj := &unstructured.Unstructured{Object: obj}

	// Get API resource information from unstructured object.
	apiResource := unstructuredObj.GroupVersionKind().GroupVersion().WithResource(
		strings.ToLower(unstructuredObj.GetKind()) + "s",
	)

	if apiResource.Resource == "policys" {
		apiResource.Resource = "policies"
	}

	namespace := unstructuredObj.GetNamespace()
	name := unstructuredObj.GetName()

	// Check if the resource already exists.
	existingObj, err := r.DynamicClient.Resource(apiResource).Namespace(namespace).Get(context.TODO(), name, metav1.GetOptions{})
	if err == nil {
		// Resource exists, perform an update.
		unstructuredObj.SetResourceVersion(existingObj.GetResourceVersion())
		_, err = r.DynamicClient.Resource(apiResource).Namespace(namespace).Update(
			context.TODO(),
			unstructuredObj,
			metav1.UpdateOptions{},
		)

		if err != nil {
			klog.Error("failed to update resource: ", err)

			return err
		}

		klog.Infof("resource updated: %s/%s\n", namespace, name)
	} else if k8errors.IsNotFound(err) {
		// Resource does not exist, create it.
		_, err = r.DynamicClient.Resource(apiResource).Namespace(namespace).Create(
			context.TODO(),
			unstructuredObj,
			metav1.CreateOptions{},
		)
		if err != nil {
			klog.Error("failed to create resource: ", err)

			return err
		}

		klog.Infof("resource created: %s/%s\n", namespace, name)
	} else {
		klog.Error("failed to get resource: ", err)

		return err
	}

	return nil
}

func generatePlacementYamlString(gitOpsCluster gitopsclusterV1beta1.GitOpsCluster) string {
	yamlString := fmt.Sprintf(`
apiVersion: cluster.open-cluster-management.io/v1beta1
kind: Placement
metadata:
  name: %s
  namespace: %s
  ownerReferences:
  - apiVersion: apps.open-cluster-management.io/v1beta1
    kind: GitOpsCluster
    name: %s
    uid: %s
spec:
  clusterSets:
    - global
  predicates:
    - requiredClusterSelector:
        labelSelector:
          matchExpressions:
            - key: local-cluster
              operator: In
              values:
                - "true"
`,
		gitOpsCluster.Name+"-policy-local-placement", gitOpsCluster.Namespace,
		gitOpsCluster.Name, string(gitOpsCluster.UID))

	return yamlString
}

func generatePlacementBindingYamlString(gitOpsCluster gitopsclusterV1beta1.GitOpsCluster) string {
	yamlString := fmt.Sprintf(`
apiVersion: policy.open-cluster-management.io/v1
kind: PlacementBinding
metadata:
  name: %s
  namespace: %s
  ownerReferences:
  - apiVersion: apps.open-cluster-management.io/v1beta1
    kind: GitOpsCluster
    name: %s
    uid: %s
placementRef:
  name: %s
  kind: Placement
  apiGroup: cluster.open-cluster-management.io
subjects:
  - name: %s
    kind: Policy
    apiGroup: policy.open-cluster-management.io
`,
		gitOpsCluster.Name+"-policy-local-placement-binding", gitOpsCluster.Namespace,
		gitOpsCluster.Name, string(gitOpsCluster.UID),
		gitOpsCluster.Name+"-policy-local-placement", gitOpsCluster.Name+"-policy")

	return yamlString
}

func generatePolicyTemplateYamlString(gitOpsCluster gitopsclusterV1beta1.GitOpsCluster) string {
	yamlString := fmt.Sprintf(`
apiVersion: policy.open-cluster-management.io/v1
kind: Policy
metadata:
  name: %s
  namespace: %s
  annotations:
    policy.open-cluster-management.io/standards: NIST-CSF
    policy.open-cluster-management.io/categories: PR.PT Protective Technology
    policy.open-cluster-management.io/controls: PR.PT-3 Least Functionality
  ownerReferences:
  - apiVersion: apps.open-cluster-management.io/v1beta1
    kind: GitOpsCluster
    name: %s
    uid: %s
spec:
  remediationAction: enforce
  disabled: false
  policy-templates:
    - objectDefinition:
        apiVersion: policy.open-cluster-management.io/v1
        kind: ConfigurationPolicy
        metadata:
          name: %s
        spec:
          pruneObjectBehavior: DeleteIfCreated
          remediationAction: enforce
          severity: low
          object-templates-raw: |
            {{ range $placedec := (lookup "cluster.open-cluster-management.io/v1beta1" "PlacementDecision" "%s" "" "cluster.open-cluster-management.io/placement=%s").items }}
            {{ range $clustdec := $placedec.status.decisions }}
            - complianceType: musthave
              objectDefinition:
                apiVersion: authentication.open-cluster-management.io/v1alpha1
                kind: ManagedServiceAccount
                metadata:
                  name: %s
                  namespace: {{ $clustdec.clusterName }}
                spec:
                  rotation: {}
            - complianceType: musthave
              objectDefinition:
                apiVersion: rbac.open-cluster-management.io/v1alpha1
                kind: ClusterPermission
                metadata:
                  name: %s
                  namespace: {{ $clustdec.clusterName }}
                spec: {}
            {{ end }}
            {{ end }}
`,
		gitOpsCluster.Name+"-policy", gitOpsCluster.Namespace,
		gitOpsCluster.Name, string(gitOpsCluster.UID),
		gitOpsCluster.Name+"-config-policy",
		gitOpsCluster.Namespace, gitOpsCluster.Spec.PlacementRef.Name,
		gitOpsCluster.Spec.ManagedServiceAccountRef, gitOpsCluster.Name+"-cluster-permission")

	return yamlString
}
