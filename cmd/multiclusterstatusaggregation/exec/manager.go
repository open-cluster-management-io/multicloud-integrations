// Copyright 2021 The Kubernetes Authors.
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

package exec

import (
	"fmt"
	"os"

	manifestWorkV1 "open-cluster-management.io/api/work/v1"
	appsubapi "open-cluster-management.io/multicloud-integrations/pkg/apis"
	multiclusterappsetreport "open-cluster-management.io/multicloud-integrations/pkg/apis/appsetreport/v1alpha1"
	argov1alpha1 "open-cluster-management.io/multicloud-integrations/pkg/apis/argocd/v1alpha1"
	"open-cluster-management.io/multicloud-integrations/pkg/controller"
	appsubutils "open-cluster-management.io/multicloud-integrations/pkg/utils"

	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

// Change below variables to serve metrics on different host or port.
var (
	metricsHost = "0.0.0.0"
	metricsPort = 8383
)

// RunManager starts the actual manager
func RunManager() {
	enableLeaderElection := false

	if _, err := rest.InClusterConfig(); err == nil {
		klog.Info("LeaderElection enabled as running in a cluster")

		enableLeaderElection = true
	} else {
		klog.Info("LeaderElection disabled as not running in cluster")
	}

	klog.Info("kubeconfig:" + options.KubeConfig)

	// Create a new Cmd to provide shared dependencies and start components
	var err error

	cfg := ctrl.GetConfigOrDie()

	if options.KubeConfig != "" {
		cfg, err = appsubutils.GetClientConfigFromKubeConfig(options.KubeConfig)

		if err != nil {
			klog.Error(err, "")
			os.Exit(1)
		}
	}

	klog.Info("Leader election settings",
		"leaseDuration", options.LeaderElectionLeaseDuration,
		"renewDeadline", options.LeaderElectionRenewDeadline,
		"retryPeriod", options.LeaderElectionRetryPeriod)

	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Metrics: metricsserver.Options{
			BindAddress: fmt.Sprintf("%s:%d", metricsHost, metricsPort),
		},
		LeaderElection:          enableLeaderElection,
		LeaderElectionID:        "multicloud-operators-multiclusterstatusaggregation-leader.open-cluster-management.io",
		LeaderElectionNamespace: "kube-system",
		LeaseDuration:           &options.LeaderElectionLeaseDuration,
		RenewDeadline:           &options.LeaderElectionRenewDeadline,
		RetryPeriod:             &options.LeaderElectionRetryPeriod,
		NewClient:               NewNonCachingClient,
	})

	if err != nil {
		klog.Error(err, "")
		os.Exit(1)
	}

	klog.Info("Registering MulticlusterStatusAggregation Components.")

	if err := clientgoscheme.AddToScheme(mgr.GetScheme()); err != nil {
		klog.Error(err, "")
		os.Exit(1)
	}

	// Setup Scheme for all resources
	if err := appsubapi.AddToScheme(mgr.GetScheme()); err != nil {
		klog.Error(err, "")
		os.Exit(1)
	}

	// Setup manifestWork Scheme for manager
	if err := manifestWorkV1.AddToScheme(mgr.GetScheme()); err != nil {
		klog.Error(err, "")
		os.Exit(1)
	}

	// Setup Multiclusterappsetreport Scheme for manager
	if err := multiclusterappsetreport.AddToScheme(mgr.GetScheme()); err != nil {
		klog.Error(err, "")
		os.Exit(1)
	}

	// Setup ApplicationSet Scheme for manager
	if err := argov1alpha1.AddToScheme(mgr.GetScheme()); err != nil {
		klog.Error(err, "")
		os.Exit(1)
	}

	// Setup all Controllers
	if err := controller.AddMulticlusterStatusAggregationToManager(mgr, options.SyncInterval, options.AppSetResourceDir); err != nil {
		klog.Error(err, "")
		os.Exit(1)
	}

	sig := signals.SetupSignalHandler()

	klog.Info("Starting the Cmd.")

	// Start the Cmd
	if err := mgr.Start(sig); err != nil {
		klog.Error(err, "Manager exited non-zero")
		os.Exit(1)
	}
}

func NewNonCachingClient(config *rest.Config, options client.Options) (client.Client, error) {
	return client.New(config, client.Options{Scheme: clientgoscheme.Scheme})
}
