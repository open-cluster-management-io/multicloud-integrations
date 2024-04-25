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

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	clientsetx "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"k8s.io/client-go/rest"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	workv1 "open-cluster-management.io/api/work/v1"
	appsetreportV1alpha1 "open-cluster-management.io/multicloud-integrations/pkg/apis/appsetreport/v1alpha1"
	argov1alpha1 "open-cluster-management.io/multicloud-integrations/pkg/apis/argocd/v1alpha1"
	"open-cluster-management.io/multicloud-integrations/propagation-controller/application"
	"sigs.k8s.io/controller-runtime/pkg/client"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

// PropagationCMDOptions for command line flag parsing
type PropagationCMDOptions struct {
	MetricsAddr                 string
	LeaderElectionLeaseDuration time.Duration
	LeaderElectionRenewDeadline time.Duration
	LeaderElectionRetryPeriod   time.Duration
	MaxConcurrentReconciles     int
}

var options = PropagationCMDOptions{
	MetricsAddr:                 "",
	LeaderElectionLeaseDuration: 137 * time.Second,
	LeaderElectionRenewDeadline: 107 * time.Second,
	LeaderElectionRetryPeriod:   26 * time.Second,
	MaxConcurrentReconciles:     10,
}

var (
	scheme      = runtime.NewScheme()
	setupLog    = ctrl.Log.WithName("setup")
	metricsHost = "0.0.0.0"
	metricsPort = 8386
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(argov1alpha1.AddToScheme(scheme))
	utilruntime.Must(clusterv1.AddToScheme(scheme))
	utilruntime.Must(workv1.AddToScheme(scheme))
	utilruntime.Must(appsetreportV1alpha1.AddToScheme(scheme))
}

func main() {
	var enableLeaderElection bool

	flag.StringVar(
		&options.MetricsAddr,
		"metrics-addr",
		options.MetricsAddr,
		"The address the metric endpoint binds to.",
	)

	flag.DurationVar(
		&options.LeaderElectionLeaseDuration,
		"leader-election-lease-duration",
		options.LeaderElectionLeaseDuration,
		"The duration that non-leader candidates will wait after observing a leadership "+
			"renewal until attempting to acquire leadership of a led but unrenewed leader "+
			"slot. This is effectively the maximum duration that a leader can be stopped "+
			"before it is replaced by another candidate. This is only applicable if leader "+
			"election is enabled.",
	)

	flag.DurationVar(
		&options.LeaderElectionRenewDeadline,
		"leader-election-renew-deadline",
		options.LeaderElectionRenewDeadline,
		"The interval between attempts by the acting master to renew a leadership slot "+
			"before it stops leading. This must be less than or equal to the lease duration. "+
			"This is only applicable if leader election is enabled.",
	)

	flag.DurationVar(
		&options.LeaderElectionRetryPeriod,
		"leader-election-retry-period",
		options.LeaderElectionRetryPeriod,
		"The duration the clients should wait between attempting acquisition and renewal "+
			"of a leadership. This is only applicable if leader election is enabled.",
	)

	flag.IntVar(
		&options.MaxConcurrentReconciles,
		"max-concurrent-reconciles",
		options.MaxConcurrentReconciles,
		"The maxium concurrent reconciles the controller can run.",
	)

	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	setupLog.Info("Leader election settings",
		"leaseDuration", options.LeaderElectionLeaseDuration,
		"renewDeadline", options.LeaderElectionRenewDeadline,
		"retryPeriod", options.LeaderElectionRetryPeriod,
		"maxConcurrentReconciles", options.MaxConcurrentReconciles)

	// Create a new Cmd to provide shared dependencies and start components
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: fmt.Sprintf("%s:%d", metricsHost, metricsPort),
		},
		LeaderElection:          enableLeaderElection,
		LeaderElectionID:        "multicloud-operators-propagation-leader.open-cluster-management.io",
		LeaderElectionNamespace: "kube-system",
		LeaseDuration:           &options.LeaderElectionLeaseDuration,
		RenewDeadline:           &options.LeaderElectionRenewDeadline,
		RetryPeriod:             &options.LeaderElectionRetryPeriod,
		NewClient:               NewNonCachingClient,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	// create the clientset for the CRDs
	crdx, err := clientsetx.NewForConfig(mgr.GetConfig())
	if err != nil {
		setupLog.Error(err, "unable to build clientsetx for Argo CD Application CRD check")
		os.Exit(1)
	}

	for {
		// Only start the controller if the Application CRD exists.
		_, err = crdx.ApiextensionsV1().CustomResourceDefinitions().Get(context.TODO(), "applications.argoproj.io", v1.GetOptions{})
		if err == nil {
			setupLog.Info("found CRD applications.argoproj.io")
			break
		} else {
			setupLog.Error(err, "failed to find CRD applications.argoproj.io, checking again after 10s")
			time.Sleep(10 * time.Second)
		}
	}

	if err = (&application.ApplicationReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr, options.MaxConcurrentReconciles); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Application")
		os.Exit(1)
	}

	if err = (&application.ApplicationStatusReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create application status controller", "application status controller", "Application")
		os.Exit(1)
	}

	setupLog.Info("starting manager")

	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

func NewNonCachingClient(config *rest.Config, options client.Options) (client.Client, error) {
	return client.New(config, client.Options{Scheme: scheme})
}
