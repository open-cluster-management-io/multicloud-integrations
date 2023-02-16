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
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog"
	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	gitopsclusterV1beta1 "open-cluster-management.io/multicloud-integrations/pkg/apis/apps/v1beta1"
	"open-cluster-management.io/multicloud-integrations/pkg/utils"

	rbacv1 "k8s.io/api/rbac/v1"
	k8errors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
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
	authClient kubernetes.Interface
	scheme     *runtime.Scheme
	lock       sync.Mutex
}

// Add creates a new argocd cluster Controller and adds it to the Manager with default RBAC.
// The Manager will set fields on the Controller and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

var _ reconcile.Reconciler = &ReconcileGitOpsCluster{}

var errInvalidPlacementRef = errors.New("invalid placement reference")

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	authCfg := mgr.GetConfig()
	kubeClient := kubernetes.NewForConfigOrDie(authCfg)

	dsRS := &ReconcileGitOpsCluster{
		Client:     mgr.GetClient(),
		scheme:     mgr.GetScheme(),
		authClient: kubeClient,
		lock:       sync.Mutex{},
	}

	return dsRS
}

type placementDecisionMapper struct {
	client.Client
}

func (mapper *placementDecisionMapper) Map(obj client.Object) []reconcile.Request {
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
			&source.Kind{Type: &gitopsclusterV1beta1.GitOpsCluster{}},
			&handler.EnqueueRequestForObject{},
			utils.GitOpsClusterPredicateFunc)
		if err != nil {
			return err
		}

		// Watch for managed cluster secret changes in argo or managed cluster namespaces
		// The manager started with cache that filters all other secrets so no predicate needed
		err = c.Watch(
			&source.Kind{Type: &v1.Secret{}},
			&handler.EnqueueRequestForObject{},
			utils.ManagedClusterSecretPredicateFunc)
		if err != nil {
			return err
		}

		// Watch cluster list changes in placement decision
		pdMapper := &placementDecisionMapper{mgr.GetClient()}
		err = c.Watch(
			&source.Kind{Type: &clusterv1beta1.PlacementDecision{}},
			handler.EnqueueRequestsFromMapFunc(pdMapper.Map),
			utils.PlacementDecisionPredicateFunc)

		if err != nil {
			return err
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

			return reconcile.Result{Requeue: true, RequeueAfter: time.Duration(requeueInterval) * time.Minute}, err
		}
	}

	// Remove all invalid/orphan GitOps cluster secrets
	if !r.cleanupOrphanSecrets(orphanGitOpsClusterSecretList) {
		// If it failed to delete orphan GitOps managed cluster secrets, reconile again in 10 minutes.
		return reconcile.Result{Requeue: true, RequeueAfter: time.Duration(10) * time.Minute}, err
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

	klog.Infof("adding managed clusters %v into argo namespace %s", managedClusters, instance.Spec.ArgoServer.ArgoNamespace)

	// 3. Copy secret contents from the managed cluster namespaces and create the secret in spec.argoServer.argoNamespace
	// if spec.enablePullModel is true then do err on missing secret from the managed cluster namespace
	enablePullModel := false
	if instance.Spec.EnablePullModel != nil {
		enablePullModel = *instance.Spec.EnablePullModel
	}

	err = r.AddManagedClustersToArgo(instance.Spec.ArgoServer.ArgoNamespace, managedClusters, orphanSecretsList, enablePullModel)

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
	instance.Status.Message = fmt.Sprintf("Added managed clusters %v to gitops namespace %s", managedClusters, instance.Spec.ArgoServer.ArgoNamespace)

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

	for _, duckMap := range maps {
		configMap := v1.ConfigMap{}

		err := r.Get(context.Background(), types.NamespacedName{Namespace: namespace, Name: duckMap.Name}, &configMap)

		if err != nil && strings.Contains(err.Error(), " not found") {
			err = r.Create(context.Background(), &duckMap)
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
func (r *ReconcileGitOpsCluster) GetManagedClusters(namespace string, placementref v1.ObjectReference) ([]string, error) {
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

	clusters := make([]string, 0)

	for _, placementdecision := range placementDecisions.Items {
		klog.Info("getting cluster names from placement decision " + placementdecision.Name)

		for _, clusterDecision := range placementdecision.Status.Decisions {
			klog.Info("cluster name: " + clusterDecision.ClusterName)
			clusters = append(clusters, clusterDecision.ClusterName)
		}
	}

	return clusters, nil
}

// AddManagedClustersToArgo copies a managed cluster secret from the managed cluster namespace to ArgoCD namespace
func (r *ReconcileGitOpsCluster) AddManagedClustersToArgo(
	argoNamespace string,
	managedClusters []string,
	orphanSecretsList map[types.NamespacedName]string,
	enablePullModel bool) error {
	returnErr := errors.New("")
	errorOccurred := false

	for _, managedCluster := range managedClusters {
		klog.Infof("adding managed cluster %s to gitops namespace %s", managedCluster, argoNamespace)

		secretName := managedCluster + "-cluster-secret"
		managedClusterSecretKey := types.NamespacedName{Name: secretName, Namespace: managedCluster}

		managedClusterSecret := &v1.Secret{}
		err := r.Get(context.TODO(), managedClusterSecretKey, managedClusterSecret)

		if err != nil && !enablePullModel {
			klog.Error("failed to get managed cluster secret. err: ", err.Error())

			errorOccurred = true
			returnErr = err

			continue
		}

		err = r.CreateManagedClusterSecretInArgo(argoNamespace, *managedClusterSecret, managedCluster, enablePullModel)

		if err != nil {
			klog.Error("failed to create managed cluster secret. err: ", err.Error())

			errorOccurred = true
			returnErr = err

			continue
		}

		// Since thie secret is now added to Argo, it is not orphan.
		argoManagedClusterSecretKey := types.NamespacedName{Name: secretName, Namespace: argoNamespace}
		delete(orphanSecretsList, argoManagedClusterSecretKey)
	}

	if !errorOccurred {
		return nil
	}

	return returnErr
}

// CreateManagedClusterSecretInArgo creates a managed cluster secret with specific metadata in Argo namespace
func (r *ReconcileGitOpsCluster) CreateManagedClusterSecretInArgo(argoNamespace string, managedClusterSecret v1.Secret,
	managedCluster string, enablePullModel bool) error {
	// create the new cluster secret in the argocd server namespace
	var newSecret *v1.Secret

	if enablePullModel {
		newSecret = &v1.Secret{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Secret",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      managedCluster + "-cluster-secret",
				Namespace: argoNamespace,
				Labels: map[string]string{
					"argocd.argoproj.io/secret-type":                 "cluster",
					"apps.open-cluster-management.io/acm-cluster":    "true",
					"apps.open-cluster-management.io/cluster-name":   managedCluster,
					"apps.open-cluster-management.io/cluster-server": managedCluster + "-control-plane", // dummy value for pull model
				},
			},
			Type: "Opaque",
			StringData: map[string]string{
				"name":   managedCluster,
				"server": "https://" + managedCluster + "-control-plane", // dummy value for pull model
			},
		}
	} else {
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
				"server": string(managedClusterSecret.Data["server"]),
			},
		}
	}

	existingManagedClusterSecret := &v1.Secret{}

	// no managed cluster secret found
	if managedClusterSecret.Name == "" {
		err := r.Get(context.TODO(), types.NamespacedName{Name: newSecret.Name, Namespace: argoNamespace}, existingManagedClusterSecret)
		if err == nil {
			klog.Infof("already exist cluster secret in argo namespace: %v/%v", argoNamespace, newSecret.Name)
			return nil
		}

		if k8errors.IsNotFound(err) {
			klog.Infof("creating cluster secret in argo namespace: %v/%v", argoNamespace, newSecret.Name)

			err := r.Create(context.TODO(), newSecret)

			if err != nil {
				klog.Errorf("failed to create cluster secret. name: %v/%v, error: %v", argoNamespace, newSecret.Name, err)
				return err
			}

			return nil
		}

		klog.Errorf("failed to get cluster secret. name: %v/%v, error: %v", argoNamespace, newSecret.Name, err)

		return err
	}

	err := r.Get(context.TODO(), types.NamespacedName{Name: managedClusterSecret.Name, Namespace: argoNamespace}, existingManagedClusterSecret)
	if err == nil {
		klog.Infof("updating managed cluster secret in argo namespace: %v/%v", argoNamespace, managedClusterSecret.Name)

		newSecret = unionSecretData(newSecret, existingManagedClusterSecret)

		err := r.Update(context.TODO(), newSecret)

		if err != nil {
			klog.Errorf("failed to update managed cluster secret. name: %v/%v, error: %v", argoNamespace, managedClusterSecret.Name, err)
			return err
		}
	}

	if k8errors.IsNotFound(err) {
		klog.Infof("creating managed cluster secret in argo namespace: %v/%v", argoNamespace, managedClusterSecret.Name)

		err := r.Create(context.TODO(), newSecret)

		if err != nil {
			klog.Errorf("failed to create managed cluster secret. name: %v/%v, error: %v", argoNamespace, managedClusterSecret.Name, err)
			return err
		}
	}

	klog.Errorf("failed to get managed cluster secret. name: %v/%v, error: %v", argoNamespace, newSecret.Name, err)

	return err
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
