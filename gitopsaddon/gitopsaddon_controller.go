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

package gitopsaddon

import (
	"context"
	"embed"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart/loader"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/rest"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

//nolint:all
//go:embed charts/openshift-gitops-operator/**
//go:embed charts/openshift-gitops-dependency/**
var ChartFS embed.FS

// GitopsAddonReconciler reconciles a openshift gitops operator
type GitopsAddonReconciler struct {
	client.Client
	Scheme              *runtime.Scheme
	Config              *rest.Config
	Interval            int
	GitopsOperatorImage string
	GitopsOperatorNS    string
	GitopsImage         string
	GitopsNS            string
	RedisImage          string
	ReconcileScope      string
	HTTP_PROXY          string
	HTTPS_PROXY         string
	NO_PROXY            string
	ACTION              string
}

func SetupWithManager(mgr manager.Manager, interval int, gitopsOperatorImage, gitopsOperatorNS,
	gitopsImage, gitopsNS, redisImage, reconcileScope,
	HTTP_PROXY, HTTPS_PROXY, NO_PROXY, ACTION string) error {
	dsRS := &GitopsAddonReconciler{
		Client:              mgr.GetClient(),
		Scheme:              mgr.GetScheme(),
		Config:              mgr.GetConfig(),
		Interval:            interval,
		GitopsOperatorImage: gitopsOperatorImage,
		GitopsOperatorNS:    gitopsOperatorNS,
		GitopsImage:         gitopsImage,
		GitopsNS:            gitopsNS,
		RedisImage:          redisImage,
		ReconcileScope:      reconcileScope,
		HTTP_PROXY:          HTTP_PROXY,
		HTTPS_PROXY:         HTTPS_PROXY,
		NO_PROXY:            NO_PROXY,
		ACTION:              ACTION,
	}

	return mgr.Add(dsRS)
}

func (r *GitopsAddonReconciler) Start(ctx context.Context) error {
	go wait.Until(func() {
		configFlags := genericclioptions.NewConfigFlags(false)
		configFlags.APIServer = &r.Config.Host
		configFlags.BearerToken = &r.Config.BearerToken
		configFlags.CAFile = &r.Config.CAFile
		configFlags.CertFile = &r.Config.CertFile
		configFlags.KeyFile = &r.Config.KeyFile

		r.houseKeeping(configFlags)
	}, time.Duration(r.Interval)*time.Second, ctx.Done())

	return nil
}

func (r *GitopsAddonReconciler) houseKeeping(configFlags *genericclioptions.ConfigFlags) {
	switch r.ACTION {
	case "Install":
		r.installOrUpdateOpenshiftGitops(configFlags)
	case "Delete-Operator":
		r.deleteOpenshiftGitopsOperator(configFlags)
	case "Delete-Instance":
		r.deleteOpenshiftGitopsInstance(configFlags)
	default:
		r.installOrUpdateOpenshiftGitops(configFlags)
	}
}

func (r *GitopsAddonReconciler) deleteOpenshiftGitopsOperator(configFlags *genericclioptions.ConfigFlags) {
	klog.Info("Start deleting openshift gitops operator...")

	if err := r.deleteChart(configFlags, r.GitopsOperatorNS, "openshift-gitops-operator"); err == nil {
		gitopsOperatorNsKey := types.NamespacedName{
			Name: r.GitopsOperatorNS,
		}
		r.unsetNamespace(gitopsOperatorNsKey, "openshift-gitops-operator")
	}
}

func (r *GitopsAddonReconciler) deleteOpenshiftGitopsInstance(configFlags *genericclioptions.ConfigFlags) {
	klog.Info("Start deleting openshift gitops instance...")

	if err := r.deleteChart(configFlags, r.GitopsNS, "openshift-gitops-dependency"); err == nil {
		gitopsNsKey := types.NamespacedName{
			Name: r.GitopsNS,
		}
		r.unsetNamespace(gitopsNsKey, "openshift-gitops")
	}
}

// install or update openshift gitops operator and its gitops instance
func (r *GitopsAddonReconciler) installOrUpdateOpenshiftGitops(configFlags *genericclioptions.ConfigFlags) {
	klog.Info("Start installing/updating openshift gitops operator and its instance...")

	// 1. install/update the openshift gitops operator manifest
	if !r.ShouldUpdateOpenshiftGiopsOperator() {
		klog.Info("Don't update openshift gitops operator")
	} else {
		// Install/upgrade the openshift-gitops-operator helm chart
		gitopsOperatorNsKey := types.NamespacedName{
			Name: r.GitopsOperatorNS,
		}

		if err := r.CreateUpdateNamespace(gitopsOperatorNsKey); err == nil {
			err := r.installOrUpgradeChart(configFlags, "charts/openshift-gitops-operator", r.GitopsOperatorNS, "openshift-gitops-operator")
			if err != nil {
				klog.Errorf("Failed to process openshift-gitops-operator: %v", err)
			} else {
				r.postUpdate(gitopsOperatorNsKey, "openshift-gitops-operator")
			}
		}
	}

	// 2. install/update the openshift gitops dependency manifests
	if !r.ShouldUpdateOpenshiftGiops() {
		klog.Info("Don't update openshift gitops dependency")
	} else {
		// Install/upgrade the openshift-gitops-dependency helm chart
		gitopsNsKey := types.NamespacedName{
			Name: r.GitopsNS,
		}

		if err := r.CreateUpdateNamespace(gitopsNsKey); err == nil {
			err := r.installOrUpgradeChart(configFlags, "charts/openshift-gitops-dependency", r.GitopsNS, "openshift-gitops-dependency")
			if err != nil {
				klog.Errorf("Failed to process openshift-gitops-dependency: %v", err)
			} else {
				r.postUpdate(gitopsNsKey, "openshift-gitops")
			}
		}
	}
}

func (r *GitopsAddonReconciler) deleteChart(configFlags *genericclioptions.ConfigFlags, namespace, releaseName string) error {
	actionConfig := new(action.Configuration)
	if err := actionConfig.Init(configFlags, namespace, "secret", log.Printf); err != nil {
		return fmt.Errorf("failed to init helm action config: %w", err)
	}

	helmDelete := action.NewUninstall(actionConfig)

	// Check if the release exists first
	listAction := action.NewList(actionConfig)
	releases, err := listAction.Run()

	if err != nil {
		klog.Infof("failed to list releases: %v", err)
		return nil
	}

	// Check if the release exists
	releaseExists := false

	for _, rel := range releases {
		if rel.Name == releaseName && rel.Namespace == namespace {
			releaseExists = true
			break
		}
	}

	if !releaseExists {
		klog.Infof("release %s not found in namespace %s", releaseName, namespace)
		return nil
	}

	// Perform the uninstall
	_, err = helmDelete.Run(releaseName)
	if err != nil {
		return fmt.Errorf("failed to uninstall release: %w", err)
	}

	fmt.Printf("Successfully deleted Helm chart %s from namespace %s\n", releaseName, namespace)

	return nil
}

func (r *GitopsAddonReconciler) installOrUpgradeChart(configFlags *genericclioptions.ConfigFlags, chartPath, namespace, releaseName string) error {
	actionConfig := new(action.Configuration)
	if err := actionConfig.Init(configFlags, namespace, "secret", log.Printf); err != nil {
		return fmt.Errorf("failed to init helm action config: %w", err)
	}

	// Create temp directory for chart files
	tempDir, err := os.MkdirTemp("", "helm-chart-*")
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tempDir)

	klog.Infof("temp dir: %v", tempDir)

	// Copy embedded chart files to temp directory
	err = r.copyEmbeddedToTemp(ChartFS, chartPath, tempDir, releaseName)
	if err != nil {
		return fmt.Errorf("failed to copy files: %w", err)
	}

	// Load the chart
	chart, err := loader.Load(tempDir)
	if err != nil {
		return fmt.Errorf("failed to load chart: %w", err)
	}

	// Check if the release exists
	listAction := action.NewList(actionConfig)
	releases, err := listAction.Run()

	if err != nil {
		return fmt.Errorf("failed to list releases: %w", err)
	}

	// Check if the release exists
	releaseExists := false

	for _, rel := range releases {
		if rel.Name == releaseName && rel.Namespace == namespace {
			releaseExists = true
			break
		}
	}

	if releaseExists {
		// Release exists, do upgrade
		helmUpgrade := action.NewUpgrade(actionConfig)
		helmUpgrade.Namespace = namespace
		helmUpgrade.Force = true // Enable force option for upgrades

		_, err = helmUpgrade.Run(releaseName, chart, nil)

		if err != nil {
			return fmt.Errorf("failed to upgrade helm chart: %v/%v, err: %w", namespace, releaseName, err)
		}

		klog.Infof("Successfully upgraded helm chart: %v/%v", namespace, releaseName)
	} else {
		// delete stuck helm release secret if it exists
		r.deleteHelmReleaseSecret(namespace, releaseName)

		// install the helm chart
		helmInstall := action.NewInstall(actionConfig)
		helmInstall.Namespace = namespace
		helmInstall.ReleaseName = releaseName
		helmInstall.Replace = true
		helmInstall.Force = true // Enable force option for installs

		_, err = helmInstall.Run(chart, nil)

		if err != nil {
			return fmt.Errorf("failed to install helm chart: %v/%v, err: %w", namespace, releaseName, err)
		}

		klog.Infof("Successfully installed helm chart: %v/%v", namespace, releaseName)
	}

	return nil
}

func (r *GitopsAddonReconciler) deleteHelmReleaseSecret(namespace, releaseName string) {
	secretName := fmt.Sprintf("sh.helm.release.v1.%s.v1", releaseName)

	secret := &corev1.Secret{}

	err := r.Get(context.TODO(), types.NamespacedName{Name: secretName, Namespace: namespace}, secret)

	if err == nil && secret.Type == "helm.sh/release.v1" {
		err := r.Delete(context.TODO(), secret)
		if err != nil {
			klog.Infof("failed to delete Helm release secret: %v, err: %v", secret.String(), err)
		}
	}
}

func (r *GitopsAddonReconciler) copyEmbeddedToTemp(fs embed.FS, srcPath, destPath, releaseName string) error {
	entries, err := fs.ReadDir(srcPath)
	if err != nil {
		return err
	}

	// Create destination directory
	err = os.MkdirAll(destPath, 0750)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		sourcePath := srcPath + "/" + entry.Name()
		destFilePath := destPath + "/" + entry.Name()

		if entry.IsDir() {
			err = r.copyEmbeddedToTemp(fs, sourcePath, destFilePath, releaseName)
			if err != nil {
				return err
			}

			continue
		}

		if entry.Name() == "values.yaml" {
			// overrirde the value.yaml, save it to the temp dir
			if releaseName == "openshift-gitops-operator" {
				err = r.updateOperatorValueYaml(fs, sourcePath, destFilePath)
				if err != nil {
					return err
				}
			} else if releaseName == "openshift-gitops-dependency" {
				err = r.updateDependencyValueYaml(fs, sourcePath, destFilePath)
				if err != nil {
					return err
				}
			}
		} else {
			// save other yaml files to the temp dir
			content, err := fs.ReadFile(sourcePath)
			if err != nil {
				return err
			}

			err = os.WriteFile(destFilePath, content, 0600)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (r *GitopsAddonReconciler) updateOperatorValueYaml(fs embed.FS, sourcePath, destFilePath string) error {
	file, err := fs.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	// Parse existing values into an unstructured map
	var values map[string]interface{}

	decoder := yaml.NewDecoder(file)
	if err := decoder.Decode(&values); err != nil {
		return fmt.Errorf("failed to decode YAML: %w", err)
	}

	// Navigate to the nested keys and update the values
	global, ok := values["global"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("failed to find 'global' key in YAML")
	}

	gitopsOperator, ok := global["openshift_gitops_operator"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("failed to find 'openshift_gitops_operator' key in YAML")
	}

	image, tag, err := ParseImageReference(r.GitopsOperatorImage)
	if err != nil {
		return fmt.Errorf("failed to parse images: %w", err)
	}

	// Update the image and tag
	gitopsOperator["image"] = image
	gitopsOperator["tag"] = tag

	// override http proxy configuration
	proxyConfig, ok := global["proxyConfig"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("proxyConfig field not found or invalid type")
	}

	if r.HTTP_PROXY > "" {
		proxyConfig["HTTP_PROXY"] = r.HTTP_PROXY
	}

	if r.HTTPS_PROXY > "" {
		proxyConfig["HTTPS_PROXY"] = r.HTTPS_PROXY
	}

	if r.NO_PROXY > "" {
		proxyConfig["NO_PROXY"] = r.NO_PROXY
	}

	// Write the updated data back to the file
	outputFile, err := os.Create(filepath.Clean(destFilePath))
	if err != nil {
		return fmt.Errorf("failed to open file for writing: %w", err)
	}
	defer outputFile.Close()

	encoder := yaml.NewEncoder(outputFile)
	encoder.SetIndent(2)

	if err := encoder.Encode(values); err != nil {
		return fmt.Errorf("failed to encode YAML: %w", err)
	}

	return nil
}

func (r *GitopsAddonReconciler) updateDependencyValueYaml(fs embed.FS, sourcePath, destFilePath string) error {
	file, err := fs.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	// Parse existing values into an unstructured map
	var values map[string]interface{}

	decoder := yaml.NewDecoder(file)
	if err := decoder.Decode(&values); err != nil {
		return fmt.Errorf("failed to decode YAML: %w", err)
	}

	// Navigate to the nested keys and update the values
	global, ok := values["global"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("failed to find 'global' key in YAML")
	}

	// override the gitops image and tag
	appController, ok := global["application_controller"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("application_controller field not found or invalid type")
	}

	gitOpsImage, gitOpsImagetag, err := ParseImageReference(r.GitopsImage)
	if err != nil {
		return fmt.Errorf("failed to parse images: %w", err)
	}

	appController["image"] = gitOpsImage
	appController["tag"] = gitOpsImagetag

	// override the redis image and tag
	redisServer, ok := global["redis"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("redis field not found or invalid type")
	}

	redisImage, reidsImagetag, err := ParseImageReference(r.RedisImage)
	if err != nil {
		return fmt.Errorf("failed to parse images: %w", err)
	}

	// Update the image and tag
	redisServer["image"] = redisImage
	redisServer["tag"] = reidsImagetag

	// override the reconcile_scope
	global["reconcile_scope"] = r.ReconcileScope

	// override http proxy configuration
	proxyConfig, ok := global["proxyConfig"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("proxyConfig field not found or invalid type")
	}

	if r.HTTP_PROXY > "" {
		proxyConfig["HTTP_PROXY"] = r.HTTP_PROXY
	}

	if r.HTTPS_PROXY > "" {
		proxyConfig["HTTPS_PROXY"] = r.HTTPS_PROXY
	}

	if r.NO_PROXY > "" {
		proxyConfig["NO_PROXY"] = r.NO_PROXY
	}

	// Write the updated data back to the file
	outputFile, err := os.Create(filepath.Clean(destFilePath))
	if err != nil {
		return fmt.Errorf("failed to open file for writing: %w", err)
	}
	defer outputFile.Close()

	encoder := yaml.NewEncoder(outputFile)
	encoder.SetIndent(2)

	if err := encoder.Encode(values); err != nil {
		return fmt.Errorf("failed to encode YAML: %w", err)
	}

	return nil
}

func ParseImageReference(imageRef string) (string, string, error) {
	parts := strings.SplitN(imageRef, "@", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid image reference format, expected image@digest: %s", imageRef)
	}

	image := parts[0]
	tag := parts[1]

	// Validate image and tag are not empty
	if image == "" {
		return "", "", fmt.Errorf("image part is empty")
	}

	if tag == "" {
		return "", "", fmt.Errorf("tag/digest part is empty")
	}

	// If the digest doesn't start with "sha256:", add it
	if !strings.HasPrefix(tag, "sha256:") {
		tag = "sha256:" + tag
	}

	return image, tag, nil
}

func (r *GitopsAddonReconciler) ShouldUpdateOpenshiftGiopsOperator() bool {
	nameSpaceKey := types.NamespacedName{
		Name: r.GitopsOperatorNS,
	}
	namespace := &corev1.Namespace{}
	err := r.Get(context.TODO(), nameSpaceKey, namespace)

	if err != nil {
		if errors.IsNotFound(err) {
			klog.Infof("no gitops operator namespace found")
			return true
		} else {
			klog.Errorf("Failed to get the %v namespace, err: %v", r.GitopsOperatorNS, err)
			return false
		}
	} else {
		if _, ok := namespace.Labels["apps.open-cluster-management.io/gitopsaddon"]; !ok {
			klog.Errorf("The %v namespace is not owned by gitops addon", r.GitopsOperatorNS)
			return false
		}

		if namespace.Annotations["apps.open-cluster-management.io/gitops-operator-image"] != r.GitopsOperatorImage ||
			namespace.Annotations["apps.open-cluster-management.io/gitops-operator-ns"] != r.GitopsOperatorNS {
			klog.Infof("new gitops operator manifest found")
			return true
		}
	}

	return false
}

func (r *GitopsAddonReconciler) ShouldUpdateOpenshiftGiops() bool {
	nameSpaceKey := types.NamespacedName{
		Name: r.GitopsNS,
	}
	namespace := &corev1.Namespace{}
	err := r.Get(context.TODO(), nameSpaceKey, namespace)

	if err != nil {
		if errors.IsNotFound(err) {
			klog.Infof("no gitops namespace found")
			return true
		} else {
			klog.Errorf("Failed to get the %v namespace, err: %v", r.GitopsNS, err)
			return false
		}
	} else {
		if _, ok := namespace.Labels["apps.open-cluster-management.io/gitopsaddon"]; !ok {
			klog.Errorf("The %v namespace is not owned by gitops addon", r.GitopsNS)
			return false
		}

		if namespace.Annotations["apps.open-cluster-management.io/gitops-image"] != r.GitopsImage ||
			namespace.Annotations["apps.open-cluster-management.io/gitops-ns"] != r.GitopsNS ||
			namespace.Annotations["apps.open-cluster-management.io/redis-image"] != r.RedisImage ||
			namespace.Annotations["apps.open-cluster-management.io/reconcile-scope"] != r.ReconcileScope {
			klog.Infof("new gitops manifest found")
			return true
		}
	}

	return false
}

func (r *GitopsAddonReconciler) CreateUpdateNamespace(nameSpaceKey types.NamespacedName) error {
	namespace := &corev1.Namespace{}
	err := r.Get(context.TODO(), nameSpaceKey, namespace)

	if err != nil {
		if errors.IsNotFound(err) {
			namespace = &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: nameSpaceKey.Name,
					Labels: map[string]string{
						"addon.open-cluster-management.io/namespace":  "true", //enable copying the image pull secret to the NS
						"apps.open-cluster-management.io/gitopsaddon": "true",
					},
				},
			}

			if err := r.Create(context.TODO(), namespace); err != nil {
				klog.Errorf("Failed to create the openshift-gitops-operator namespace, err: %v", err)
				return err
			}
		} else {
			klog.Errorf("Failed to get the openshift-gitops-operator namespace, err: %v", err)
			return err
		}
	} else {
		namespace.Labels["addon.open-cluster-management.io/namespace"] = "true"
		namespace.Labels["apps.open-cluster-management.io/gitopsaddon"] = "true"

		if err := r.Update(context.TODO(), namespace); err != nil {
			klog.Errorf("Failed to update the labels to the openshift-gitops-operator namespace, err: %v", err)
			return err
		}
	}

	// Waiting for the two resources ready
	// 1. the image pull secret `open-cluster-management-image-pull-credentials`  is generated
	// 2. the image pull secret ref is updated to the openshift-gitops/default SA
	secretName := "open-cluster-management-image-pull-credentials"
	secret := &corev1.Secret{}
	timeout := time.Minute
	interval := time.Second * 2
	start := time.Now()

	for time.Since(start) < timeout {
		err = r.Get(context.TODO(), types.NamespacedName{Name: secretName, Namespace: nameSpaceKey.Name}, secret)
		if err == nil {
			// Secret found
			klog.Infof("Secret %s found in namespace %s", secretName, nameSpaceKey.Name)

			// Patch the default sa in the openshift-gitops NS
			saKey := types.NamespacedName{
				Name:      "default",
				Namespace: nameSpaceKey.Name,
			}

			err = r.patchDefaultSA(saKey)

			if err == nil {
				return nil
			}
		}

		if !errors.IsNotFound(err) {
			klog.Errorf("Error while waiting for secret %s in namespace %s: %v", secretName, nameSpaceKey.Name, err)
			return err
		}

		klog.Infof("Either the image pull credentials secret or the default SA is NOT found, wait for next check, err: %v", err)

		time.Sleep(interval)
	}

	// Timeout reached
	klog.Errorf("Timeout waiting for secret %s in namespace %s", secretName, nameSpaceKey.Name)

	return fmt.Errorf("timeout waiting for secret %s in namespace %s", secretName, nameSpaceKey.Name)
}

func (r *GitopsAddonReconciler) patchDefaultSA(saKey types.NamespacedName) error {
	sa := &corev1.ServiceAccount{}
	err := r.Get(context.TODO(), saKey, sa)

	if err != nil {
		klog.Errorf("Failed to get the default service account, err: %v", err)
		return err
	}

	sa.ImagePullSecrets = append(sa.ImagePullSecrets, corev1.LocalObjectReference{
		Name: "open-cluster-management-image-pull-credentials",
	})

	if err := r.Update(context.TODO(), sa); err != nil {
		klog.Errorf("Failed to update the image pull secret to the default SA, err: %v", err)
		return err
	}

	return nil
}

func (r *GitopsAddonReconciler) postUpdate(nameSpaceKey types.NamespacedName, nsType string) {
	//1. save Latest Gitops metat To Namespace annotations
	namespace := &corev1.Namespace{}
	err := r.Get(context.TODO(), nameSpaceKey, namespace)

	if err != nil {
		klog.Errorf("Failed to get the namespace: %v, err: %v", nameSpaceKey.Name, err)
		return
	}

	namespacePatch := &corev1.Namespace{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Namespace",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        namespace.Name,
			Annotations: map[string]string{},
		},
	}

	if nsType == "openshift-gitops-operator" {
		namespacePatch.Annotations["apps.open-cluster-management.io/gitops-operator-image"] = r.GitopsOperatorImage
		namespacePatch.Annotations["apps.open-cluster-management.io/gitops-operator-ns"] = r.GitopsOperatorNS
	} else {
		namespacePatch.Annotations["apps.open-cluster-management.io/gitops-image"] = r.GitopsImage
		namespacePatch.Annotations["apps.open-cluster-management.io/gitops-ns"] = r.GitopsNS
		namespacePatch.Annotations["apps.open-cluster-management.io/redis-image"] = r.RedisImage
		namespacePatch.Annotations["apps.open-cluster-management.io/reconcile-scope"] = r.ReconcileScope
	}

	// Use server-side apply with force to handle concurrent modifications
	namespacePatch.ObjectMeta.ManagedFields = nil

	if err := r.Patch(context.TODO(), namespacePatch, client.Apply,
		client.FieldOwner("gitops-addon-reconciler"), client.ForceOwnership); err != nil {
		klog.Errorf("Failed to save the lastest gitops meta data to namespace: %v, err: %v", nameSpaceKey.Name, err)
		return
	}

	klog.Infof("Successfully saved the lastest gitops meta data to namespace: %v", nameSpaceKey.Name)
}

func (r *GitopsAddonReconciler) unsetNamespace(nameSpaceKey types.NamespacedName, nsType string) {
	namespace := &corev1.Namespace{}
	err := r.Get(context.TODO(), nameSpaceKey, namespace)

	if err != nil {
		klog.Errorf("Failed to get the namespace: %v, err: %v", nameSpaceKey.Name, err)
		return
	}

	namespacePatch := namespace.DeepCopy()
	namespacePatch.Kind = "Namespace"
	namespacePatch.APIVersion = "v1"

	if nsType == "openshift-gitops-operator" {
		if namespacePatch.Annotations != nil {
			delete(namespacePatch.Annotations, "apps.open-cluster-management.io/gitops-operator-image")
			delete(namespacePatch.Annotations, "apps.open-cluster-management.io/gitops-operator-ns")
		}
	} else {
		if namespacePatch.Annotations != nil {
			delete(namespacePatch.Annotations, "apps.open-cluster-management.io/gitops-image")
			delete(namespacePatch.Annotations, "apps.open-cluster-management.io/gitops-ns")
			delete(namespacePatch.Annotations, "apps.open-cluster-management.io/redis-image")
			delete(namespacePatch.Annotations, "apps.open-cluster-management.io/reconcile-scope")
		}
	}

	// Use server-side apply with force to handle concurrent modifications
	namespacePatch.ObjectMeta.ManagedFields = nil

	if err := r.Patch(context.TODO(), namespacePatch, client.Apply,
		client.FieldOwner("gitops-addon-reconciler"), client.ForceOwnership); err != nil {
		klog.Errorf("Failed to unset annotations from namespace: %v, err: %v", nameSpaceKey.Name, err)
		return
	}
}
