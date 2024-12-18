package utils

import (
	"testing"
	"time"

	"github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	spokeClusterV1 "open-cluster-management.io/api/cluster/v1"
	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	gitopsclusterV1beta1 "open-cluster-management.io/multicloud-integrations/pkg/apis/apps/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

var (
	oldClusterCond1 = metav1.Condition{
		Type:   "ManagedClusterConditionAvailable",
		Status: "true",
	}

	oldClusterCond2 = metav1.Condition{
		Type:   "HubAcceptedManagedCluster",
		Status: "true",
	}

	oldCluster = &spokeClusterV1.ManagedCluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ManagedCluster",
			APIVersion: "cluster.open-cluster-management.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster1",
			Labels: map[string]string{
				"name": "cluster1",
				"key1": "c1v1",
				"key2": "c1v2",
			},
		},
		Status: spokeClusterV1.ManagedClusterStatus{
			Conditions: []metav1.Condition{oldClusterCond1, oldClusterCond2},
		},
	}

	newClusterCond = metav1.Condition{
		Type:   "ManagedClusterConditionAvailable",
		Status: "true",
	}

	newCluster = &spokeClusterV1.ManagedCluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ManagedCluster",
			APIVersion: "cluster.open-cluster-management.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster1",
			Labels: map[string]string{
				"name": "cluster1",
				"key1": "c1v1",
				"key2": "c1v2",
			},
		},
		Status: spokeClusterV1.ManagedClusterStatus{
			Conditions: []metav1.Condition{newClusterCond},
		},
	}

	oldSecret = &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster1-cluster-secret",
			Labels: map[string]string{
				"apps.open-cluster-management.io/secret-type": "non-acm-cluster",
			},
		},
	}

	newSecret = &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster1-cluster-secret",
			Labels: map[string]string{
				"apps.open-cluster-management.io/secret-type": "acm-cluster",
				"apps.open-cluster-management.io/acm-cluster": "true",
			},
		},
	}

	newGitOpsCluster = &gitopsclusterV1beta1.GitOpsCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "gitops-cluster",
		},
		Spec: gitopsclusterV1beta1.GitOpsClusterSpec{
			PlacementRef: &v1.ObjectReference{Name: "new-placement"},
		},
	}

	oldGitOpsCluster = &gitopsclusterV1beta1.GitOpsCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "gitops-cluster",
		},
		Spec: gitopsclusterV1beta1.GitOpsClusterSpec{
			PlacementRef: &v1.ObjectReference{Name: "old-placement"},
		},
	}

	newPlacementDecision = &clusterv1beta1.PlacementDecision{
		ObjectMeta: metav1.ObjectMeta{
			Name: "placement-decision",
		},
		Status: clusterv1beta1.PlacementDecisionStatus{
			Decisions: []clusterv1beta1.ClusterDecision{
				{
					ClusterName: "cluster1",
				},
			},
		},
	}

	oldPlacementDecision = &clusterv1beta1.PlacementDecision{
		ObjectMeta: metav1.ObjectMeta{
			Name: "placement-decision",
		},
		Status: clusterv1beta1.PlacementDecisionStatus{
			Decisions: []clusterv1beta1.ClusterDecision{
				{
					ClusterName: "cluster2",
				},
			},
		},
	}
)

func TestPredicate(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	// Test ClusterPredicateFunc
	instance := ClusterPredicateFunc

	updateEvt := event.TypedUpdateEvent[*spokeClusterV1.ManagedCluster]{
		ObjectOld: oldCluster,
		ObjectNew: newCluster,
	}
	ret := instance.Update(updateEvt)
	g.Expect(ret).To(gomega.Equal(true))

	updateEvt = event.TypedUpdateEvent[*spokeClusterV1.ManagedCluster]{
		ObjectOld: oldCluster,
		ObjectNew: oldCluster,
	}
	ret = instance.Update(updateEvt)
	g.Expect(ret).To(gomega.Equal(false))

	newCluster.DeletionTimestamp = &metav1.Time{Time: time.Now()}
	updateEvt = event.TypedUpdateEvent[*spokeClusterV1.ManagedCluster]{
		ObjectOld: oldCluster,
		ObjectNew: newCluster,
	}
	ret = instance.Update(updateEvt)
	g.Expect(ret).To(gomega.Equal(true))

	newCluster.DeletionTimestamp = nil
	newCluster.Labels = map[string]string{
		"env": "test",
	}
	updateEvt = event.TypedUpdateEvent[*spokeClusterV1.ManagedCluster]{
		ObjectOld: oldCluster,
		ObjectNew: newCluster,
	}
	ret = instance.Update(updateEvt)
	g.Expect(ret).To(gomega.Equal(true))

	// Test GitOpsClusterPredicateFunc
	instance1 := GitOpsClusterPredicateFunc

	createEvt1 := event.TypedCreateEvent[*gitopsclusterV1beta1.GitOpsCluster]{
		Object: oldGitOpsCluster,
	}
	ret = instance1.Create(createEvt1)
	g.Expect(ret).To(gomega.Equal(true))

	updateEvt1 := event.TypedUpdateEvent[*gitopsclusterV1beta1.GitOpsCluster]{
		ObjectOld: oldGitOpsCluster,
		ObjectNew: newGitOpsCluster,
	}

	ret = instance1.Update(updateEvt1)
	g.Expect(ret).To(gomega.Equal(true))

	// Test PlacementDecisionPredicateFunc
	instance2 := PlacementDecisionPredicateFunc

	createEvt2 := event.TypedCreateEvent[*clusterv1beta1.PlacementDecision]{
		Object: oldPlacementDecision,
	}
	ret = instance2.Create(createEvt2)
	g.Expect(ret).To(gomega.Equal(true))

	updateEvt2 := event.TypedUpdateEvent[*clusterv1beta1.PlacementDecision]{
		ObjectOld: oldPlacementDecision,
		ObjectNew: newPlacementDecision,
	}

	ret = instance2.Update(updateEvt2)
	g.Expect(ret).To(gomega.Equal(true))

	delEvt2 := event.TypedDeleteEvent[*clusterv1beta1.PlacementDecision]{
		Object: newPlacementDecision,
	}
	ret = instance2.Delete(delEvt2)
	g.Expect(ret).To(gomega.Equal(true))

	// Test ManagedClusterSecretPredicateFunc
	instance3 := ManagedClusterSecretPredicateFunc

	createEvt3 := event.TypedCreateEvent[*v1.Secret]{
		Object: newSecret,
	}
	ret = instance3.Create(createEvt3)
	g.Expect(ret).To(gomega.Equal(false))

	newSecret.Labels = map[string]string{
		"not-label": "argo",
	}

	createEvt3 = event.TypedCreateEvent[*v1.Secret]{
		Object: newSecret,
	}
	ret = instance3.Create(createEvt3)
	g.Expect(ret).To(gomega.Equal(true))

	updateEvt3 := event.TypedUpdateEvent[*v1.Secret]{
		ObjectOld: newSecret,
		ObjectNew: oldSecret,
	}

	ret = instance3.Update(updateEvt3)
	g.Expect(ret).To(gomega.Equal(true))

	delEvt3 := event.TypedDeleteEvent[*v1.Secret]{
		Object: oldSecret,
	}
	ret = instance3.Delete(delEvt3)
	g.Expect(ret).To(gomega.Equal(false))

	oldSecret.Labels = map[string]string{
		ACMClusterSecretLabel: "argo",
	}
	delEvt3 = event.TypedDeleteEvent[*v1.Secret]{
		Object: oldSecret,
	}
	ret = instance3.Delete(delEvt3)
	g.Expect(ret).To(gomega.Equal(false))

	// Test ManagedClusterSecretPredicateFunc
	instance4 := ManagedClusterSecretPredicateFunc

	newSecret.Labels = map[string]string{
		ArgocdClusterSecretLabel: "acm-cluster",
	}
	oldSecret.Labels = map[string]string{
		ArgocdClusterSecretLabel: "acm-cluster",
	}

	updateEvt4 := event.TypedUpdateEvent[*v1.Secret]{
		ObjectOld: oldSecret,
		ObjectNew: newSecret,
	}

	ret = instance4.Update(updateEvt4)
	g.Expect(ret).To(gomega.Equal(false))

	delEvt4 := event.TypedDeleteEvent[*v1.Secret]{
		Object: oldSecret,
	}
	ret = instance4.Delete(delEvt4)
	g.Expect(ret).To(gomega.Equal(true))
}
