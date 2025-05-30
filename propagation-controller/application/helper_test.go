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
	"reflect"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	workv1 "open-cluster-management.io/api/work/v1"
)

func Test_containsValidPullLabel(t *testing.T) {
	type args struct {
		labels map[string]string
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			name: "valid pull label",
			args: args{
				map[string]string{LabelKeyPull: "true"},
			},
			want: true,
		},
		{
			name: "valid pull label case",
			args: args{
				map[string]string{LabelKeyPull: "True"},
			},
			want: true,
		},
		{
			name: "invalid pull label",
			args: args{
				map[string]string{LabelKeyPull + "a": "true"},
			},
			want: false,
		},
		{
			name: "empty value",
			args: args{
				map[string]string{LabelKeyPull: ""},
			},
			want: false,
		},
		{
			name: "false value",
			args: args{
				map[string]string{LabelKeyPull: "false"},
			},
			want: false,
		},
		{
			name: "no pull label",
			args: args{
				map[string]string{},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := containsValidPullLabel(tt.args.labels); got != tt.want {
				t.Errorf("containsValidPullLabel() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_containsValidPullAnnotation(t *testing.T) {
	type args struct {
		annos map[string]string
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			name: "valid pull annotation",
			args: args{
				map[string]string{AnnotationKeyOCMManagedCluster: "cluster1"},
			},
			want: true,
		},
		{
			name: "invalid pull annotation",
			args: args{
				map[string]string{AnnotationKeyOCMManagedCluster + "a": "cluster1"},
			},
			want: false,
		},
		{
			name: "empty value",
			args: args{
				map[string]string{AnnotationKeyOCMManagedCluster: ""},
			},
			want: false,
		},
		{
			name: "no pull annotation",
			args: args{
				map[string]string{},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := containsValidPullAnnotation(tt.args.annos); got != tt.want {
				t.Errorf("containsValidPullAnnotation() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_containsValidManifestWorkHubApplicationAnnotations(t *testing.T) {
	type args struct {
		manifestWork workv1.ManifestWork
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			name: "valid application annotations",
			args: args{
				workv1.ManifestWork{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							AnnotationKeyHubApplicationNamespace: "namespace1",
							AnnotationKeyHubApplicationName:      "app-name1",
						},
					},
				},
			},
			want: true,
		},
		{
			name: "missing application namespace annotation",
			args: args{
				workv1.ManifestWork{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							AnnotationKeyHubApplicationName: "app-name1",
						},
					},
				},
			},
			want: false,
		},
		{
			name: "missing application name annotation",
			args: args{
				workv1.ManifestWork{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							AnnotationKeyHubApplicationNamespace: "namespace1",
						},
					},
				},
			},
			want: false,
		},
		{
			name: "empty value",
			args: args{
				workv1.ManifestWork{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							AnnotationKeyHubApplicationNamespace: "",
							AnnotationKeyHubApplicationName:      "",
						},
					},
				},
			},
			want: false,
		},
		{
			name: "no application annotation",
			args: args{
				workv1.ManifestWork{},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := containsValidManifestWorkHubApplicationAnnotations(tt.args.manifestWork); got != tt.want {
				t.Errorf("containsValidManifestWorkHubApplicationAnnotations() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_generateAppNamespace(t *testing.T) {
	type args struct {
		namespace string
		annos     map[string]string
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "annotation only",
			args: args{
				annos: map[string]string{AnnotationKeyOCMManagedClusterAppNamespace: "gitops"},
			},
			want: "gitops",
		},
		{
			name: "annotation and namespace",
			args: args{
				annos:     map[string]string{AnnotationKeyOCMManagedClusterAppNamespace: "gitops"},
				namespace: "argocd",
			},
			want: "gitops",
		},
		{
			name: "namespace only",
			args: args{
				namespace: "gitops",
			},
			want: "gitops",
		},
		{
			name: "annotation and namespace not found",
			args: args{},
			want: "argocd",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := generateAppNamespace(tt.args.namespace, tt.args.annos); got != tt.want {
				t.Errorf("generateAppNamespace() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_generateManifestWorkName(t *testing.T) {
	type args struct {
		name string
		uid  types.UID
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "generate name",
			args: args{
				name: "app1",
				uid:  "abcdefghijk",
			},
			want: "app1-abcde",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := generateManifestWorkName(tt.args.name, tt.args.uid); got != tt.want {
				t.Errorf("generateManifestWorkName() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_prepareApplicationForWorkPayload(t *testing.T) {
	app := &unstructured.Unstructured{}
	app.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "argoproj.io",
		Version: "v1alpha1",
		Kind:    "Application",
	})
	app.SetName("app1")
	app.SetNamespace("argocd")
	app.SetFinalizers([]string{"app1-final"})
	app.SetAnnotations(map[string]string{
		AnnotationKeyAppSkipReconcile: "true",
	})
	app.Object["spec"] = map[string]interface{}{
		"destination": map[string]interface{}{
			"name":   "originalName",
			"server": "originalServer",
		},
	}
	app.Object["operation"] = map[string]interface{}{
		"info": []interface{}{
			map[string]interface{}{
				"name":  "Reason",
				"value": "ApplicationSet RollingSync triggered a sync of this Application resource.",
			},
		},
		"initiatedBy": map[string]interface{}{
			"automated": true,
			"username":  "applicationset-controller",
		},
		"retry": map[string]interface{}{},
		"sync": map[string]interface{}{
			"syncOptions": []interface{}{
				"CreateNamespace=true",
			},
		},
	}

	type args struct {
		application *unstructured.Unstructured
	}
	tests := []struct {
		name string
		args args
		want *unstructured.Unstructured
	}{
		{
			name: "modified app",
			args: args{application: app},
			want: func() *unstructured.Unstructured {
				expectedApp := &unstructured.Unstructured{}
				expectedApp.SetGroupVersionKind(schema.GroupVersionKind{
					Group:   "argoproj.io",
					Version: "v1alpha1",
					Kind:    "Application",
				})
				expectedApp.SetName("app1")
				expectedApp.SetNamespace("argocd")
				expectedApp.SetFinalizers([]string{"app1-final"})
				expectedApp.SetLabels(map[string]string{})
				expectedApp.SetAnnotations(map[string]string{})
				expectedApp.Object["spec"] = map[string]interface{}{
					"destination": map[string]interface{}{
						"name":   "",
						"server": KubernetesInternalAPIServerAddr,
					},
				}
				expectedApp.Object["operation"] = map[string]interface{}{
					"info": []interface{}{
						map[string]interface{}{
							"name":  "Reason",
							"value": "ApplicationSet RollingSync triggered a sync of this Application resource.",
						},
					},
					"initiatedBy": map[string]interface{}{
						"automated": true,
						"username":  "applicationset-controller",
					},
					"retry": map[string]interface{}{},
					"sync": map[string]interface{}{
						"syncOptions": []interface{}{
							"CreateNamespace=true",
						},
					},
				}
				return expectedApp
			}(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := prepareApplicationForWorkPayload(tt.args.application)
			if got.GetName() != tt.want.GetName() {
				t.Errorf("prepareApplicationForWorkPayload() Name = %v, want %v", got.GetName(), tt.want.GetName())
			}

			if got.GetNamespace() != tt.want.GetNamespace() {
				t.Errorf("prepareApplicationForWorkPayload() Namespace = %v, want %v", got.GetNamespace(), tt.want.GetNamespace())
			}

			if !reflect.DeepEqual(got.GetFinalizers(), tt.want.GetFinalizers()) {
				t.Errorf("prepareApplicationForWorkPayload() Finalizers = %v, want %v", got.GetFinalizers(), tt.want.GetFinalizers())
			}

			gotSpec, _, _ := unstructured.NestedMap(got.Object, "spec")
			wantSpec, _, _ := unstructured.NestedMap(tt.want.Object, "spec")

			if !reflect.DeepEqual(gotSpec, wantSpec) {
				t.Errorf("prepareApplicationForWorkPayload() Spec = %v, want %v", gotSpec, wantSpec)
			}

			gotOperation, _, _ := unstructured.NestedMap(got.Object, "operation")
			wantOperation, _, _ := unstructured.NestedMap(tt.want.Object, "operation")

			if !reflect.DeepEqual(gotOperation, wantOperation) {
				t.Errorf("prepareApplicationForWorkPayload() Operation = %v, want %v", gotOperation, wantOperation)
			}

			if !reflect.DeepEqual(got.GetLabels(), tt.want.GetLabels()) {
				t.Errorf("prepareApplicationForWorkPayload() Labels = %v, want %v", got.GetLabels(), tt.want.GetLabels())
			}

			if !reflect.DeepEqual(got.GetAnnotations(), tt.want.GetAnnotations()) {
				t.Errorf("prepareApplicationForWorkPayload() Annotations = %v, want %v", got.GetAnnotations(), tt.want.GetAnnotations())
			}
		})
	}
}

func Test_generateManifestWork(t *testing.T) {
	app := &unstructured.Unstructured{}
	app.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "argoproj.io",
		Version: "v1alpha1",
		Kind:    "Application",
	})
	app.SetName("app1")
	app.SetNamespace("argocd")
	app.SetOwnerReferences([]metav1.OwnerReference{
		{
			APIVersion: "argoproj.io/v1alpha1",
			Kind:       "ApplicationSet",
			Name:       "appset1",
		},
	})
	app.SetFinalizers([]string{ResourcesFinalizerName})

	type args struct {
		name        string
		namespace   string
		application *unstructured.Unstructured
	}

	type results struct {
		workLabel map[string]string
		workAnno  map[string]string
	}
	tests := []struct {
		name string
		args args
		want results
	}{
		{
			name: "sunny",
			args: args{
				name:        "app1-abcde",
				namespace:   "cluster1",
				application: app,
			},
			want: results{
				workLabel: map[string]string{
					LabelKeyAppSet:     "true",
					LabelKeyAppSetHash: "654ef46669ce896863583d1940559d145b39032b",
				},
				workAnno: map[string]string{
					AnnotationKeyAppSet:                  "argocd/appset1",
					AnnotationKeyHubApplicationNamespace: "argocd",
					AnnotationKeyHubApplicationName:      "app1",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := generateManifestWork(tt.args.name, tt.args.namespace, tt.args.application)
			if err != nil {
				t.Errorf("generateManifestWork() = got err %v", err)
			}

			if !reflect.DeepEqual(got.Annotations, tt.want.workAnno) {
				t.Errorf("generateManifestWork() = %v, want %v", got.Annotations, tt.want.workAnno)
			}

			if !reflect.DeepEqual(got.Labels, tt.want.workLabel) {
				t.Errorf("generateManifestWork() = %v, want %v", got.Labels, tt.want.workLabel)
			}

			if got.Spec.ManifestConfigs[0].UpdateStrategy.ServerSideApply.IgnoreFields[0].JSONPaths[0] != ".operation" {
				t.Errorf("generateManifestWork() does not contain operation ignore field")
			}
		})
	}
}

func Test_GenerateManifestWorkAppSetHashLabelValue(t *testing.T) {
	type args struct {
		appSetNamespace string
		appSetName      string
	}
	tests := []struct {
		name    string
		args    args
		want    string
		wantErr bool
	}{
		{
			name: "sunny",
			args: args{
				appSetNamespace: "argocd",
				appSetName:      "appset-1",
			},
			want:    "b208dbecfe0a5f65581d608faac0e95c9a302b34",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GenerateManifestWorkAppSetHashLabelValue(tt.args.appSetNamespace, tt.args.appSetName)
			if (err != nil) != tt.wantErr {
				t.Errorf("GenerateManifestWorkAppSetHashLabelValue() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if got != tt.want {
				t.Errorf("GenerateManifestWorkAppSetHashLabelValue() = %v, want %v", got, tt.want)
			}
		})
	}
}
