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
	"k8s.io/client-go/rest"
	"k8s.io/klog"
	"open-cluster-management.io/multicloud-integrations/maestroAggregation"
	"open-cluster-management.io/multicloud-integrations/pkg/utils"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

// MaestroAggregationOptions for command line flag parsing
type GitopsAddonAgentOptions struct {
	MetricsAddr                 string
	LeaderElectionLeaseDuration time.Duration
	LeaderElectionRenewDeadline time.Duration
	LeaderElectionRetryPeriod   time.Duration
	SyncInterval                int
}

var options = GitopsAddonAgentOptions{
	MetricsAddr:                 "",
	LeaderElectionLeaseDuration: 137 * time.Second,
	LeaderElectionRenewDeadline: 107 * time.Second,
	LeaderElectionRetryPeriod:   26 * time.Second,
	SyncInterval:                60,
}

var (
	scheme      = runtime.NewScheme()
	setupLog    = ctrl.Log.WithName("setup")
	metricsHost = "0.0.0.0"
	metricsPort = 8388
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(clusterv1.AddToScheme(scheme))
}

func main() {
	enableLeaderElection := false

	if _, err := rest.InClusterConfig(); err == nil {
		klog.Info("LeaderElection enabled as running in a cluster")

		enableLeaderElection = true
	} else {
		klog.Info("LeaderElection disabled as not running in a cluster")
	}

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
		&options.SyncInterval,
		"sync-interval",
		options.SyncInterval,
		"The interval of housekeeping in seconds.",
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
		"syncInterval", options.SyncInterval,
	)

	// Create a new Cmd to provide shared dependencies and start components
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: fmt.Sprintf("%s:%d", metricsHost, metricsPort),
		},
		LeaderElection:          enableLeaderElection,
		LeaderElectionID:        "maestro-aggregation-leader.open-cluster-management.io",
		LeaderElectionNamespace: "kube-system",
		LeaseDuration:           &options.LeaderElectionLeaseDuration,
		RenewDeadline:           &options.LeaderElectionRenewDeadline,
		RetryPeriod:             &options.LeaderElectionRetryPeriod,
		NewClient:               NewNonCachingClient,
	})
	if err != nil {
		setupLog.Error(err, "unable to start gitops addon agent")
		os.Exit(1)
	}

	maestroSeverAddr, err := utils.GetServiceURL(mgr.GetClient(), "maestro", "maestro", "http")

	if err != nil {
		setupLog.Error(err, "failed to get maestro api server url")
		os.Exit(1)
	}

	maestroGRPCServerAddr, err := utils.GetServiceURL(mgr.GetClient(), "maestro-grpc", "maestro", "")

	if err != nil {
		setupLog.Error(err, "failed to get maestro grpc server url")
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	maestroWorkClient, err := utils.BuildWorkClient(ctx, maestroSeverAddr, maestroGRPCServerAddr)

	if err != nil {
		setupLog.Error(err, "failed to build maestro work client")
		os.Exit(1)
	}

	if err = maestroAggregation.SetupWithManager(mgr, options.SyncInterval, maestroWorkClient); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "gitopsaddon")
		os.Exit(1)
	}

	setupLog.Info("starting maestro aggregation controller")

	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

func NewNonCachingClient(config *rest.Config, options client.Options) (client.Client, error) {
	return client.New(config, client.Options{Scheme: scheme})
}
