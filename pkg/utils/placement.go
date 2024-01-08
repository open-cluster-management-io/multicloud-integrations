// Copyright 2019 The Kubernetes Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package utils

import (
	"context"
	"os"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog"
	spokeClusterV1 "open-cluster-management.io/api/cluster/v1"
	authv1beta1 "open-cluster-management.io/managed-serviceaccount/apis/authentication/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// IsReadyACMClusterRegistry check if ACM Cluster API service is ready or not.
func IsReadyACMClusterRegistry(clReader client.Reader) bool {
	cllist := &spokeClusterV1.ManagedClusterList{}

	listopts := &client.ListOptions{}

	err := clReader.List(context.TODO(), cllist, listopts)

	if err == nil {
		klog.Error("ACM Cluster API service ready")
		return true
	}

	klog.Error("ACM Cluster API service NOT ready: ", err)

	return false
}

// DetectClusterRegistry - Detect the ACM cluster API service every 10 seconds. the controller will be exited when it is ready
// The controller will be auto restarted by the multicluster-operators-application deployment CR later.
//
//nolint:unparam
func DetectClusterRegistry(ctx context.Context, clReader client.Reader) {
	if !IsReadyACMClusterRegistry(clReader) {
		go wait.UntilWithContext(ctx, func(ctx context.Context) {
			if IsReadyACMClusterRegistry(clReader) {
				os.Exit(1)
			}
		}, time.Duration(10)*time.Second)
	}
}

// IsReadyManagedServiceAccount check if managedServiceAccount API is ready or not.
func IsReadyManagedServiceAccount(clReader client.Reader) bool {
	managedList := &authv1beta1.ManagedServiceAccountList{}

	listopts := &client.ListOptions{}

	err := clReader.List(context.TODO(), managedList, listopts)
	if err != nil {
		klog.Error("ManagedServiceAccount API NOT ready: ", err)

		return false
	}

	klog.Info("ManagedServiceAccount API ready")

	return true
}

// DetectManagedServiceAccount - Detect the managedServiceAccount API every 10 seconds.
// The controller will be exited when it is ready and will automatically restart after.
//
//nolint:unparam
func DetectManagedServiceAccount(ctx context.Context, clReader client.Reader) {
	if !IsReadyManagedServiceAccount(clReader) {
		go wait.UntilWithContext(ctx, func(ctx context.Context) {
			if IsReadyManagedServiceAccount(clReader) {
				os.Exit(1)
			}
		}, time.Duration(300)*time.Second)
	}
}
