// Copyright 2022 The Kubernetes Authors.
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
	"testing"
	"time"

	"github.com/onsi/gomega"
	"k8s.io/client-go/kubernetes/scheme"
	authv1beta1 "open-cluster-management.io/managed-serviceaccount/apis/authentication/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

func TestIsReadyACMClusterRegistry(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	// Setup the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request to a
	// channel when it is finished.
	mgr, err := manager.New(cfg, manager.Options{
		Metrics: metricsserver.Options{
			BindAddress: "0",
		},
	})

	g.Expect(err).NotTo(gomega.HaveOccurred())

	ctx, cancel := context.WithTimeout(context.TODO(), 5*time.Minute)
	mgrStopped := StartTestManager(ctx, mgr, g)

	defer func() {
		cancel()
		mgrStopped.Wait()
	}()

	ret := IsReadyACMClusterRegistry(mgr.GetAPIReader())
	g.Expect(ret).To(gomega.Equal(true))
}

func TestIsReadyManagedServiceAccount(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	var (
		err error
	)

	// Setup the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request to a
	// channel when it is finished.
	mgr, err := manager.New(cfg, manager.Options{
		Metrics: metricsserver.Options{
			BindAddress: "0",
		},
	})

	g.Expect(err).NotTo(gomega.HaveOccurred())

	ctx, cancel := context.WithTimeout(context.TODO(), 5*time.Minute)
	mgrStopped := StartTestManager(ctx, mgr, g)

	defer func() {
		cancel()
		mgrStopped.Wait()
	}()

	// ManagedServiceAccount API should NOT BE ready
	ret := IsReadyManagedServiceAccount(mgr.GetAPIReader())
	g.Expect(ret).To(gomega.BeFalse())

	// Add CRD to scheme.
	authv1beta1.SchemeBuilder.AddToScheme(scheme.Scheme)

	mgr2, err := manager.New(cfg, manager.Options{
		Metrics: metricsserver.Options{
			BindAddress: "0",
		},
	})

	g.Expect(err).NotTo(gomega.HaveOccurred())

	ctx2, cancel2 := context.WithTimeout(context.TODO(), 5*time.Minute)
	mgrStopped2 := StartTestManager(ctx2, mgr2, g)

	defer func() {
		cancel2()
		mgrStopped2.Wait()
	}()

	// ManagedServiceAccount API should BE ready
	ret = IsReadyManagedServiceAccount(mgr2.GetAPIReader())
	g.Expect(ret).To(gomega.BeTrue())

	DetectManagedServiceAccount(ctx2, mgr2.GetAPIReader())
}
