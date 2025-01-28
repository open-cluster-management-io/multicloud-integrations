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
	"flag"
	"fmt"
	"os"
	"time"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/rest"
	"k8s.io/klog"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"open-cluster-management.io/multicloud-integrations/gitopsaddon"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

// GitopsAddonAgentOptions for command line flag parsing
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
	metricsPort = 8387

	// The default values for the latest openshift gitops operator. It requires to refresh in each ACM major release GA
	GitopsOperatorImage = "registry.redhat.io/openshift-gitops-1/gitops-rhel8-operator@sha256:2a18db52d160d864f06acf9f995212779fb7c7ecd113363ef7205a7509d80c7f"
	GitopsOperatorNS    = "openshift-gitops-operator"
	GitopsImage         = "registry.redhat.io/openshift-gitops-1/argocd-rhel8@sha256:2808a4a071a0ffa6960e40cdec1d2d2905d6116fe26c553612edabc7c2235a34"
	GitopsNS            = "openshift-gitops"
	RedisImage          = "registry.redhat.io/rhel8/redis-6@sha256:5250d644e80aea794b0f732e9285883a4348dace83557f177f64d78fe4dcabd0"
	ReconcileScope      = "Single-Namespace"
	HTTP_PROXY          = ""
	HTTPS_PROXY         = ""
	NO_PROXY            = ""
	ACTION              = "Install" // Other options: "Delete-Operator", "Delete-Instance"
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
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

	newGitopsOperatorImage, found := os.LookupEnv("GITOPS_OPERATOR_IMAGE")
	if found && newGitopsOperatorImage > "" {
		GitopsOperatorImage = newGitopsOperatorImage
	}

	newGitopsOperatorNS, found := os.LookupEnv("GITOPS_OPERATOR_NAMESPACE")
	if found && newGitopsOperatorNS > "" {
		GitopsOperatorNS = newGitopsOperatorNS
	}

	newGitopsImage, found := os.LookupEnv("GITOPS_IMAGE")
	if found && newGitopsImage > "" {
		GitopsImage = newGitopsImage
	}

	newGitopsNS, found := os.LookupEnv("GITOPS_NAMESPACE")
	if found && newGitopsNS > "" {
		GitopsNS = newGitopsNS
	}

	newRedisImage, found := os.LookupEnv("REDIS_IMAGE")
	if found && newRedisImage > "" {
		RedisImage = newRedisImage
	}

	newHTTP_PROXY, found := os.LookupEnv("HTTP_PROXY")
	if found && newHTTP_PROXY > "" {
		HTTP_PROXY = newHTTP_PROXY
	}

	newHTTPS_PROXY, found := os.LookupEnv("HTTPS_PROXY")
	if found && newHTTPS_PROXY > "" {
		HTTPS_PROXY = newHTTPS_PROXY
	}

	newNO_PROXY, found := os.LookupEnv("NO_PROXY")
	if found && newNO_PROXY > "" {
		NO_PROXY = newNO_PROXY
	}

	newReconcileScope, found := os.LookupEnv("RECONCILE_SCOPE")
	if found && newReconcileScope > "" {
		ReconcileScope = newReconcileScope
	}

	newACTION, found := os.LookupEnv("ACTION")
	if found && newACTION > "" {
		ACTION = newACTION
	}

	setupLog.Info("Leader election settings",
		"leaseDuration", options.LeaderElectionLeaseDuration,
		"renewDeadline", options.LeaderElectionRenewDeadline,
		"retryPeriod", options.LeaderElectionRetryPeriod,
		"syncInterval", options.SyncInterval,
		"GitopsOperatorImage", GitopsOperatorImage,
		"GitopsOperatorNS", GitopsOperatorNS,
		"GitopsImage", GitopsImage,
		"GitopsNS", GitopsNS,
		"RedisImage", RedisImage,
		"ReconcileScope", ReconcileScope,
		"HTTP_PROXY", HTTP_PROXY,
		"HTTPS_PROXY", HTTPS_PROXY,
		"NO_PROXY", NO_PROXY,
		"ACTION", ACTION,
	)

	// Create a new Cmd to provide shared dependencies and start components
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: fmt.Sprintf("%s:%d", metricsHost, metricsPort),
		},
		LeaderElection:          enableLeaderElection,
		LeaderElectionID:        "gitops-addon-agent-leader.open-cluster-management.io",
		LeaderElectionNamespace: "kube-system",
		LeaseDuration:           &options.LeaderElectionLeaseDuration,
		RenewDeadline:           &options.LeaderElectionRenewDeadline,
		RetryPeriod:             &options.LeaderElectionRetryPeriod,
	})
	if err != nil {
		setupLog.Error(err, "unable to start gitops addon agent")
		os.Exit(1)
	}

	if err = gitopsaddon.SetupWithManager(mgr, options.SyncInterval, GitopsOperatorImage, GitopsOperatorNS,
		GitopsImage, GitopsNS, RedisImage, ReconcileScope, HTTP_PROXY, HTTPS_PROXY, NO_PROXY, ACTION); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "gitopsaddon")
		os.Exit(1)
	}

	setupLog.Info("starting gitops addon agent")

	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
