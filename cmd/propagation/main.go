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

	clusterv1 "open-cluster-management.io/api/cluster/v1"
	workv1 "open-cluster-management.io/api/work/v1"
	argov1alpha1 "open-cluster-management.io/multicloud-integrations/pkg/apis/argocd/v1alpha1"
	"open-cluster-management.io/multicloud-integrations/propagation-controller/application"
)

// PropagationCMDOptions for command line flag parsing
type PropagationCMDOptions struct {
	MetricsAddr                        string
	LeaderElectionLeaseDurationSeconds int
	RenewDeadlineSeconds               int
	RetryPeriodSeconds                 int
}

var options = PropagationCMDOptions{
	MetricsAddr:                        "",
	LeaderElectionLeaseDurationSeconds: 137,
	RenewDeadlineSeconds:               107,
	RetryPeriodSeconds:                 26,
}

var (
	scheme              = runtime.NewScheme()
	setupLog            = ctrl.Log.WithName("setup")
	metricsHost         = "0.0.0.0"
	metricsPort         = 8386
	operatorMetricsPort = 8698
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(argov1alpha1.AddToScheme(scheme))
	utilruntime.Must(clusterv1.AddToScheme(scheme))
	utilruntime.Must(workv1.AddToScheme(scheme))
}

func main() {
	var enableLeaderElection bool
	flag.StringVar(
		&options.MetricsAddr,
		"metrics-addr",
		options.MetricsAddr,
		"The address the metric endpoint binds to.",
	)

	flag.IntVar(
		&options.LeaderElectionLeaseDurationSeconds,
		"leader-election-lease-duration",
		options.LeaderElectionLeaseDurationSeconds,
		"The leader election lease duration in seconds.",
	)

	flag.IntVar(
		&options.RenewDeadlineSeconds,
		"renew-deadline",
		options.RenewDeadlineSeconds,
		"The renew deadline in seconds.",
	)

	flag.IntVar(
		&options.RetryPeriodSeconds,
		"retry-period",
		options.RetryPeriodSeconds,
		"The retry period in seconds.",
	)
	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	leaseDuration := time.Duration(options.LeaderElectionLeaseDurationSeconds) * time.Second
	renewDeadline := time.Duration(options.RenewDeadlineSeconds) * time.Second
	retryPeriod := time.Duration(options.RetryPeriodSeconds) * time.Second
	// Create a new Cmd to provide shared dependencies and start components
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                  scheme,
		MetricsBindAddress:      fmt.Sprintf("%s:%d", metricsHost, metricsPort),
		Port:                    operatorMetricsPort,
		LeaderElection:          enableLeaderElection,
		LeaderElectionID:        "multicloud-operators-propagation-leader.open-cluster-management.io",
		LeaderElectionNamespace: "kube-system",
		LeaseDuration:           &leaseDuration,
		RenewDeadline:           &renewDeadline,
		RetryPeriod:             &retryPeriod,
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
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Application")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
