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

	oldArgocdService = &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: "argocd-server",
			Labels: map[string]string{
				"app.kubernetes.io/part-of":   "argocd",
				"app.kubernetes.io/component": "server",
			},
		},
	}

	newArgocdService = &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: "argocd-server",
			Labels: map[string]string{
				"app.kubernetes.io/part-of":   "argocd",
				"app.kubernetes.io/component": "server",
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

	updateEvt := event.UpdateEvent{
		ObjectOld: oldCluster,
		ObjectNew: newCluster,
	}
	ret := instance.Update(updateEvt)
	g.Expect(ret).To(gomega.Equal(true))

	updateEvt = event.UpdateEvent{
		ObjectOld: oldCluster,
		ObjectNew: oldCluster,
	}
	ret = instance.Update(updateEvt)
	g.Expect(ret).To(gomega.Equal(false))

	newCluster.DeletionTimestamp = &metav1.Time{Time: time.Now()}
	updateEvt = event.UpdateEvent{
		ObjectOld: oldCluster,
		ObjectNew: newCluster,
	}
	ret = instance.Update(updateEvt)
	g.Expect(ret).To(gomega.Equal(true))

	newCluster.DeletionTimestamp = nil
	newCluster.Labels = map[string]string{
		"env": "test",
	}
	updateEvt = event.UpdateEvent{
		ObjectOld: oldCluster,
		ObjectNew: newCluster,
	}
	ret = instance.Update(updateEvt)
	g.Expect(ret).To(gomega.Equal(true))

	// Test AcmClusterSecretPredicateFunc
	instance = AcmClusterSecretPredicateFunc

	createEvt := event.CreateEvent{
		Object: newSecret,
	}
	ret = instance.Create(createEvt)
	g.Expect(ret).To(gomega.Equal(true))

	updateEvt = event.UpdateEvent{
		ObjectOld: oldSecret,
		ObjectNew: newSecret,
	}

	ret = instance.Update(updateEvt)
	g.Expect(ret).To(gomega.Equal(true))

	delEvt := event.DeleteEvent{
		Object: newSecret,
	}
	ret = instance.Delete(delEvt)
	g.Expect(ret).To(gomega.Equal(true))

	createEvt = event.CreateEvent{
		Object: newSecret,
	}
	ret = instance.Create(createEvt)
	g.Expect(ret).To(gomega.Equal(true))

	updateEvt = event.UpdateEvent{
		ObjectOld: newSecret,
		ObjectNew: oldSecret,
	}

	ret = instance.Update(updateEvt)
	g.Expect(ret).To(gomega.Equal(true))

	// Test ArgocdClusterSecretPredicateFunc
	instance = ArgocdClusterSecretPredicateFunc

	createEvt = event.CreateEvent{
		Object: newSecret,
	}
	ret = instance.Create(createEvt)
	g.Expect(ret).To(gomega.Equal(true))

	updateEvt = event.UpdateEvent{
		ObjectOld: oldSecret,
		ObjectNew: newSecret,
	}

	ret = instance.Update(updateEvt)
	g.Expect(ret).To(gomega.Equal(true))

	updateEvt = event.UpdateEvent{
		ObjectOld: newSecret,
		ObjectNew: oldSecret,
	}

	ret = instance.Update(updateEvt)
	g.Expect(ret).To(gomega.Equal(true))

	delEvt = event.DeleteEvent{
		Object: newSecret,
	}
	ret = instance.Delete(delEvt)
	g.Expect(ret).To(gomega.Equal(true))

	// Test ArgocdServerPredicateFunc
	instance = ArgocdServerPredicateFunc

	createEvt = event.CreateEvent{
		Object: newArgocdService,
	}
	ret = instance.Create(createEvt)
	g.Expect(ret).To(gomega.Equal(true))

	updateEvt = event.UpdateEvent{
		ObjectOld: oldArgocdService,
		ObjectNew: newArgocdService,
	}

	ret = instance.Update(updateEvt)
	g.Expect(ret).To(gomega.Equal(true))

	delEvt = event.DeleteEvent{
		Object: newArgocdService,
	}
	ret = instance.Delete(delEvt)
	g.Expect(ret).To(gomega.Equal(true))

	// Test GitOpsClusterPredicateFunc
	instance = GitOpsClusterPredicateFunc

	createEvt = event.CreateEvent{
		Object: oldGitOpsCluster,
	}
	ret = instance.Create(createEvt)
	g.Expect(ret).To(gomega.Equal(true))

	updateEvt = event.UpdateEvent{
		ObjectOld: oldGitOpsCluster,
		ObjectNew: newGitOpsCluster,
	}

	ret = instance.Update(updateEvt)
	g.Expect(ret).To(gomega.Equal(true))

	// Test PlacementDecisionPredicateFunc
	instance = PlacementDecisionPredicateFunc

	createEvt = event.CreateEvent{
		Object: oldPlacementDecision,
	}
	ret = instance.Create(createEvt)
	g.Expect(ret).To(gomega.Equal(true))

	updateEvt = event.UpdateEvent{
		ObjectOld: oldPlacementDecision,
		ObjectNew: newPlacementDecision,
	}

	ret = instance.Update(updateEvt)
	g.Expect(ret).To(gomega.Equal(true))

	delEvt = event.DeleteEvent{
		Object: newPlacementDecision,
	}
	ret = instance.Delete(delEvt)
	g.Expect(ret).To(gomega.Equal(true))

	// Test ManagedClusterSecretPredicateFunc
	instance = ManagedClusterSecretPredicateFunc

	createEvt = event.CreateEvent{
		Object: newSecret,
	}
	ret = instance.Create(createEvt)
	g.Expect(ret).To(gomega.Equal(false))

	newSecret.Labels = map[string]string{
		"not-label": "argo",
	}

	createEvt = event.CreateEvent{
		Object: newSecret,
	}
	ret = instance.Create(createEvt)
	g.Expect(ret).To(gomega.Equal(true))

	updateEvt = event.UpdateEvent{
		ObjectOld: newSecret,
		ObjectNew: oldSecret,
	}

	ret = instance.Update(updateEvt)
	g.Expect(ret).To(gomega.Equal(true))

	delEvt = event.DeleteEvent{
		Object: oldSecret,
	}
	ret = instance.Delete(delEvt)
	g.Expect(ret).To(gomega.Equal(false))

	oldSecret.Labels = map[string]string{
		ACMClusterSecretLabel: "argo",
	}
	delEvt = event.DeleteEvent{
		Object: oldSecret,
	}
	ret = instance.Delete(delEvt)
	g.Expect(ret).To(gomega.Equal(false))

	// Test AcmClusterSecretPredicateFunc
	instance = AcmClusterSecretPredicateFunc

	newSecret.Labels = nil

	createEvt = event.CreateEvent{
		Object: newSecret,
	}
	ret = instance.Create(createEvt)
	g.Expect(ret).To(gomega.Equal(false))

	newSecret.Labels = map[string]string{
		"apps.open-cluster-management.io/secret-type": "non-acm-cluster",
	}

	createEvt = event.CreateEvent{
		Object: newSecret,
	}
	ret = instance.Create(createEvt)
	g.Expect(ret).To(gomega.Equal(false))

	newSecret.Labels = nil
	oldSecret.Labels = nil
	updateEvt = event.UpdateEvent{
		ObjectOld: oldSecret,
		ObjectNew: newSecret,
	}

	ret = instance.Update(updateEvt)
	g.Expect(ret).To(gomega.Equal(false))

	delEvt = event.DeleteEvent{
		Object: newSecret,
	}
	ret = instance.Delete(delEvt)
	g.Expect(ret).To(gomega.Equal(false))

	newSecret.Labels = map[string]string{
		"apps.open-cluster-management.io/secret-type": "non-acm-cluster",
	}

	delEvt = event.DeleteEvent{
		Object: newSecret,
	}
	ret = instance.Delete(delEvt)
	g.Expect(ret).To(gomega.Equal(false))

	// Test ArgocdClusterSecretPredicateFunc
	instance = ArgocdClusterSecretPredicateFunc

	newSecret.Labels = nil
	oldSecret.Labels = nil

	updateEvt = event.UpdateEvent{
		ObjectOld: newSecret,
		ObjectNew: oldSecret,
	}

	ret = instance.Update(updateEvt)
	g.Expect(ret).To(gomega.Equal(false))

	createEvt = event.CreateEvent{
		Object: newSecret,
	}
	ret = instance.Create(createEvt)
	g.Expect(ret).To(gomega.Equal(false))

	newSecret.Labels = map[string]string{
		ArgocdClusterSecretLabel: "non-acm-cluster",
	}

	createEvt = event.CreateEvent{
		Object: newSecret,
	}
	ret = instance.Create(createEvt)
	g.Expect(ret).To(gomega.Equal(false))

	delEvt = event.DeleteEvent{
		Object: newSecret,
	}
	ret = instance.Delete(delEvt)
	g.Expect(ret).To(gomega.Equal(false))

	newSecret.Labels = nil

	delEvt = event.DeleteEvent{
		Object: newSecret,
	}
	ret = instance.Delete(delEvt)
	g.Expect(ret).To(gomega.Equal(false))

	// Test ManagedClusterSecretPredicateFunc
	instance = ManagedClusterSecretPredicateFunc

	newSecret.Labels = map[string]string{
		ArgocdClusterSecretLabel: "acm-cluster",
	}
	oldSecret.Labels = map[string]string{
		ArgocdClusterSecretLabel: "acm-cluster",
	}

	updateEvt = event.UpdateEvent{
		ObjectOld: oldSecret,
		ObjectNew: newSecret,
	}

	ret = instance.Update(updateEvt)
	g.Expect(ret).To(gomega.Equal(false))

	delEvt = event.DeleteEvent{
		Object: oldSecret,
	}
	ret = instance.Delete(delEvt)
	g.Expect(ret).To(gomega.Equal(true))

	// Test ArgocdServerPredicateFunc
	instance = ArgocdServerPredicateFunc

	oldArgocdService.Labels = nil
	updateEvt = event.UpdateEvent{
		ObjectOld: oldArgocdService,
		ObjectNew: newArgocdService,
	}

	ret = instance.Update(updateEvt)
	g.Expect(ret).To(gomega.Equal(true))

	newArgocdService.Labels = nil
	updateEvt = event.UpdateEvent{
		ObjectOld: oldArgocdService,
		ObjectNew: newArgocdService,
	}

	ret = instance.Update(updateEvt)
	g.Expect(ret).To(gomega.Equal(false))

	createEvt = event.CreateEvent{
		Object: newArgocdService,
	}
	ret = instance.Create(createEvt)
	g.Expect(ret).To(gomega.Equal(false))

	newArgocdService.Labels = map[string]string{
		"app.kubernetes.io/part-of":   "not-argocd",
		"app.kubernetes.io/component": "not-server",
	}
	createEvt = event.CreateEvent{
		Object: newArgocdService,
	}
	ret = instance.Create(createEvt)
	g.Expect(ret).To(gomega.Equal(false))

	delEvt = event.DeleteEvent{
		Object: newArgocdService,
	}
	ret = instance.Delete(delEvt)
	g.Expect(ret).To(gomega.Equal(false))

	newArgocdService.Labels = nil
	delEvt = event.DeleteEvent{
		Object: newArgocdService,
	}
	ret = instance.Delete(delEvt)
	g.Expect(ret).To(gomega.Equal(false))
}
