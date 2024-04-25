// Copyright 2020 The Kubernetes Authors.
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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

func TestConvertLabels(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	namereq := metav1.LabelSelectorRequirement{}
	namereq.Key = "name"
	namereq.Operator = metav1.LabelSelectorOpIn

	namereq.Values = []string{"rollingendpoint"}
	labelSelector := &metav1.LabelSelector{
		MatchExpressions: []metav1.LabelSelectorRequirement{namereq},
	}

	_, err := ConvertLabels(labelSelector)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	_, err = ConvertLabels(nil)
	g.Expect(err).NotTo(gomega.HaveOccurred())
}

func TestCheckAndInstallCRD(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	// Setup the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request to a
	// channel when it is finished
	mgr, err := manager.New(cfg, manager.Options{
		Metrics: metricsserver.Options{
			BindAddress: "0",
		},
	})

	g.Expect(err).NotTo(gomega.HaveOccurred())

	//start manager mgr
	ctx, cancel := context.WithTimeout(context.TODO(), 5*time.Minute)
	mgrStopped := StartTestManager(ctx, mgr, g)

	defer func() {
		cancel()
		mgrStopped.Wait()
	}()

	//Test:  create testfoo crd
	err = CheckAndInstallCRD(cfg, "../../deploy/crds/apps.open-cluster-management.io_gitopsclusters.yaml")
	g.Expect(err).NotTo(gomega.HaveOccurred())
}
