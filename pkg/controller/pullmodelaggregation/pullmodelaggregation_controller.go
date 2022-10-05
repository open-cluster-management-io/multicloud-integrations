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

package pullmodelaggregation

import (
	"context"
	"runtime"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog"
	appsetreportV1alpha1 "open-cluster-management.io/multicloud-integrations/pkg/apis/appsetreport/v1alpha1"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// Reconcile PullModelAggregation reonciles a
type ReconcilePullModelAggregation struct {
	client.Client
	Interval int
}

// AppSetClusterResourceSorter sorts appsetreport resources by name
type AppSetClusterResourceSorter []appsetreportV1alpha1.ResourceRef

func (a AppSetClusterResourceSorter) Len() int           { return len(a) }
func (a AppSetClusterResourceSorter) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a AppSetClusterResourceSorter) Less(i, j int) bool { return a[i].Name < a[j].Name }

// AppSetClusterConditionsSorter sorts appsetreport clusterconditions by cluster
type AppSetClusterConditionsSorter []appsetreportV1alpha1.ClusterCondition

func (a AppSetClusterConditionsSorter) Len() int           { return len(a) }
func (a AppSetClusterConditionsSorter) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a AppSetClusterConditionsSorter) Less(i, j int) bool { return a[i].Cluster < a[j].Cluster }

func Add(mgr manager.Manager, interval int) error {
	dsRS := &ReconcilePullModelAggregation{
		Client:   mgr.GetClient(),
		Interval: interval,
	}

	return mgr.Add(dsRS)
}

func (r *ReconcilePullModelAggregation) Start(ctx context.Context) error {
	go wait.Until(func() {
		r.houseKeeping()
	}, time.Duration(r.Interval)*time.Second, ctx.Done())

	return nil
}

func (r *ReconcilePullModelAggregation) houseKeeping() {
	klog.Info("Start aggregating all ArgoCD application manifestworks per cluster...")

	// need to fetch some data from somewhere
	// need to organize data and then use that organized data to
	// update some relative resources to the k8s clusters.

	err := r.generateAggregation()
	if err != nil {
		klog.Warning("error while generating ArgoCD application aggregation: ", err)
	}

	klog.Info("Finish aggregating all ArgoCD application manifestworks.")
}

func (r *ReconcilePullModelAggregation) generateAggregation() error {
	PrintMemUsage("Prepare to aggregate manifestwork statuses")

	return nil
}

func PrintMemUsage(title string) {
	var m runtime.MemStats

	runtime.ReadMemStats(&m)

	// For info on each, see: https://golang.org/pkg/runtime/#MemStats
	klog.Infof("%v", title)
	klog.Infof("Alloc = %v MiB", bToMb(m.Alloc))
	klog.Infof("\tTotalAlloc = %v MiB", bToMb(m.TotalAlloc))
	klog.Infof("\tSys = %v MiB", bToMb(m.Sys))
	klog.Infof("\tNumGC = %v\n", m.NumGC)
}

func bToMb(b uint64) uint64 {
	return b / 1024 / 1024
}
