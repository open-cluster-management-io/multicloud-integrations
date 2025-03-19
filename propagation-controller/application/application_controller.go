/*
Copyright 2022.

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
	"reflect"
	"strings"

	"github.com/openshift-online/maestro/pkg/client/cloudevents/grpcsource"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	workv1client "open-cluster-management.io/api/client/work/clientset/versioned/typed/work/v1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

const (
	// Application annotation that dictates which managed cluster this Application should be pulled to
	AnnotationKeyOCMManagedCluster = "apps.open-cluster-management.io/ocm-managed-cluster"
	// Application annotation that dictates which managed cluster namespace this Application should be pulled to
	AnnotationKeyOCMManagedClusterAppNamespace = "apps.open-cluster-management.io/ocm-managed-cluster-app-namespace"
	// Application and ManifestWork annotation that shows which ApplicationSet is the grand parent of this work
	AnnotationKeyAppSet = "apps.open-cluster-management.io/hosting-applicationset"
	// Application annotation that enables the skip reconciliation of an application
	AnnotationKeyAppSkipReconcile = "argocd.argoproj.io/skip-reconcile"
	// ManifestWork annotation that shows the namespace of the hub Application.
	AnnotationKeyHubApplicationNamespace = "apps.open-cluster-management.io/hub-application-namespace"
	// ManifestWork annotation that shows the name of the hub Application.
	AnnotationKeyHubApplicationName = "apps.open-cluster-management.io/hub-application-name"
	// Application and ManifestWork label that shows that ApplicationSet is the grand parent of this work
	LabelKeyAppSet = "apps.open-cluster-management.io/application-set"
	// ManifestWork label with the ApplicationSet namespace and name in sha1 hash value
	LabelKeyAppSetHash = "apps.open-cluster-management.io/application-set-hash"
	// Application label that enables the pull controller to wrap the Application in ManifestWork payload
	LabelKeyPull = "apps.open-cluster-management.io/pull-to-ocm-managed-cluster"
	// ResourcesFinalizerName is the finalizer value which we inject to finalize deletion of an application
	ResourcesFinalizerName string = "resources-finalizer.argocd.argoproj.io"
)

// ApplicationReconciler reconciles a Application object
type ApplicationReconciler struct {
	client.Client
	Scheme            *runtime.Scheme
	MaestroWorkClient workv1client.WorkV1Interface
}

//+kubebuilder:rbac:groups=argoproj.io,resources=applications,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=cluster.open-cluster-management.io,resources=managedclusters,verbs=get;list;watch
//+kubebuilder:rbac:groups=work.open-cluster-management.io,resources=manifestworks,verbs=get;list;watch;create;update;patch;delete

// ApplicationPredicateFunctions defines which Application this controller should wrap inside ManifestWork's payload
var ApplicationPredicateFunctions = predicate.Funcs{
	UpdateFunc: func(e event.UpdateEvent) bool {
		newApp := e.ObjectNew.(*unstructured.Unstructured)
		oldApp := e.ObjectOld.(*unstructured.Unstructured)
		oldAppCopy := oldApp.DeepCopy()
		newAppCopy := newApp.DeepCopy()
		unstructured.RemoveNestedField(oldAppCopy.Object, "status")
		unstructured.RemoveNestedField(newAppCopy.Object, "status")
		isChanged := !reflect.DeepEqual(oldAppCopy.Object, newAppCopy.Object)
		return containsValidPullLabel(newApp.GetLabels()) && containsValidPullAnnotation(newApp.GetAnnotations()) && isChanged
	},
	CreateFunc: func(e event.CreateEvent) bool {
		app := e.Object.(*unstructured.Unstructured)
		return containsValidPullLabel(app.GetLabels()) && containsValidPullAnnotation(app.GetAnnotations())
	},

	DeleteFunc: func(e event.DeleteEvent) bool {
		app := e.Object.(*unstructured.Unstructured)
		return containsValidPullLabel(app.GetLabels()) && containsValidPullAnnotation(app.GetAnnotations())
	},
}

// SetupWithManager sets up the controller with the Manager.
func (r *ApplicationReconciler) SetupWithManager(mgr ctrl.Manager, maxConcurrentReconciles int) error {
	applicationGVK := &unstructured.Unstructured{}
	applicationGVK.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "argoproj.io",
		Version: "v1alpha1",
		Kind:    "Application",
	})

	return ctrl.NewControllerManagedBy(mgr).
		WithOptions(controller.Options{MaxConcurrentReconciles: maxConcurrentReconciles}).
		For(applicationGVK).
		WithEventFilter(ApplicationPredicateFunctions).
		Complete(r)
}

func (r *ApplicationReconciler) isLocalCluster(clusterName string) bool {
	managedCluster := &clusterv1.ManagedCluster{}
	managedClusterKey := types.NamespacedName{
		Name: clusterName,
	}
	err := r.Get(context.TODO(), managedClusterKey, managedCluster)

	if err != nil {
		klog.Errorf("Failed to find managed cluster: %v, error: %v ", clusterName, err)
		return false
	}

	labels := managedCluster.GetLabels()

	if labels == nil {
		labels = make(map[string]string)
	}

	if strings.EqualFold(labels["local-cluster"], "true") {
		klog.Infof("This is local-cluster: %v", clusterName)
		return true
	}

	return false
}

// Reconcile create/update/delete ManifestWork with the Application as its payload
func (r *ApplicationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	log.Info("reconciling Application...")

	application := &unstructured.Unstructured{}
	application.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "argoproj.io",
		Version: "v1alpha1",
		Kind:    "Application",
	})

	// if the propagated app is deleted directly, we just return without deleting its associated Manifestworks from maestro
	// In this case, we just got the application name and namespace,
	// We hardly parse the managed cluster name even though it is contained in the application name
	if err := r.Get(ctx, req.NamespacedName, application); err != nil {
		klog.Infof("unable to fetch Application, err: %v", err)

		return ctrl.Result{}, err
	}

	managedClusterName := application.GetAnnotations()[AnnotationKeyOCMManagedCluster]

	if r.isLocalCluster(managedClusterName) {
		log.Info("skipping Application with the local-cluster as Managed Cluster")

		return ctrl.Result{}, nil
	}

	mwName := generateMaestroManifestWorkName(application.GetNamespace(), application.GetName())

	// If the appset is deleted, the Application is being deleted with specifying deletionTimestamp, in this case
	// 1. delete its ManifestWork and delete that as well
	// 2. the app will be deleted via cleaning up its finalizers
	if application.GetDeletionTimestamp() != nil {
		// remove finalizer from Application but do not 'commit' yet
		if len(application.GetFinalizers()) != 0 {
			f := application.GetFinalizers()
			for i := 0; i < len(f); i++ {
				if f[i] == ResourcesFinalizerName {
					f = append(f[:i], f[i+1:]...)
					i--
				}
			}

			application.SetFinalizers(f)
		}

		err := r.deleteManifestworkFromMaestro(ctx, managedClusterName, mwName)
		if err != nil {
			return ctrl.Result{}, err
		}

		// deleted ManifestWork, commit the Application finalizer removal
		if err := r.Update(ctx, application); err != nil {
			log.Error(err, "unable to update Application")
			return ctrl.Result{}, err
		}

		return ctrl.Result{}, nil
	}

	// verify the ManagedCluster actually exists
	var managedCluster clusterv1.ManagedCluster
	if err := r.Get(ctx, types.NamespacedName{Name: managedClusterName}, &managedCluster); err != nil {
		log.Error(err, "unable to fetch ManagedCluster")
		return ctrl.Result{}, err
	}

	log.Info("generating ManifestWork for Application")
	newWork, err := generateManifestWork(mwName, managedClusterName, application)

	if err != nil {
		log.Error(err, "unable to generating ManifestWork")
		return ctrl.Result{}, err
	}

	// create or update the ManifestWork depends if it already exists or not
	work, err := r.MaestroWorkClient.ManifestWorks(managedClusterName).Get(ctx, mwName, metav1.GetOptions{})

	if errors.IsNotFound(err) {
		_, err = r.MaestroWorkClient.ManifestWorks(managedClusterName).Create(ctx, newWork, metav1.CreateOptions{})

		if err != nil {
			log.Error(err, "unable to create ManifestWork into Maestro")
			return ctrl.Result{}, err
		}

		klog.Infof("manifestwork created in maestro. cluster: %v, manifestwork: %v, sourceID: app-work-client",
			managedClusterName, mwName)
	} else if err == nil {
		patchData, err := grpcsource.ToWorkPatch(work, newWork)
		if err != nil {
			log.Error(err, "unable to get the patch data")
			return ctrl.Result{}, err
		}
		_, err = r.MaestroWorkClient.ManifestWorks(managedClusterName).Patch(ctx, mwName, types.MergePatchType, patchData, metav1.PatchOptions{})

		if err != nil {
			log.Error(err, "unable to update ManifestWork to Maestro")
			return ctrl.Result{}, err
		}

		klog.Infof("manifestwork updated to maestro. cluster: %v, manifestwork: %v, sourceID: app-work-client",
			managedClusterName, mwName)
	} else {
		log.Error(err, "unable to fetch ManifestWork")
		return ctrl.Result{}, err
	}

	log.Info("done reconciling Application")

	return ctrl.Result{}, nil
}

func (r *ApplicationReconciler) deleteManifestworkFromMaestro(ctx context.Context, managedClusterName, mwName string) error {
	_, err := r.MaestroWorkClient.ManifestWorks(managedClusterName).Get(ctx, mwName, metav1.GetOptions{})

	if errors.IsNotFound(err) {
		klog.Infof("can'find the manifestwork in the Maestro, manifestwork: %v, cluster: %v", mwName, managedClusterName)
		return nil
	} else if err != nil {
		klog.Errorf("failed to fetch ManifestWork from Maestro, err: %v", err)
		return err
	}

	err = r.MaestroWorkClient.ManifestWorks(managedClusterName).Delete(ctx, mwName, metav1.DeleteOptions{})

	if err != nil {
		klog.Errorf("unable to delete ManifestWork from Maestro, err: %v", err)
		return err
	}

	klog.Infof("manifestwork deleted from maestro. cluster: %v, manifestwork: %v, sourceID: app-work-client", managedClusterName, mwName)

	return nil
}
