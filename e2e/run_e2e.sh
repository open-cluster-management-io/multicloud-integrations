#!/bin/bash
###############################################################################
# Copyright Contributors to the Open Cluster Management project
###############################################################################

set -o nounset
set -o pipefail

### Setup
echo "SETUP install Argo CD to Managed cluster"
kubectl config use-context kind-cluster1
kubectl create namespace argocd
kubectl apply -n argocd --force -f hack/test/e2e/argo-cd-install.yaml 
kubectl -n argocd scale deployment/argocd-dex-server --replicas 0
kubectl -n argocd scale deployment/argocd-repo-server --replicas 0
kubectl -n argocd scale deployment/argocd-server --replicas 0
kubectl -n argocd scale deployment/argocd-redis --replicas 0
kubectl -n argocd scale deployment/argocd-notifications-controller --replicas 0

echo "SETUP install Argo CD to Hub cluster"
kubectl config use-context kind-hub
kubectl create namespace argocd
kubectl apply -n argocd --force -f hack/test/e2e/argo-cd-install.yaml 
kubectl -n argocd scale deployment/argocd-dex-server --replicas 0
kubectl -n argocd scale deployment/argocd-repo-server --replicas 0
kubectl -n argocd scale deployment/argocd-server --replicas 0
kubectl -n argocd scale deployment/argocd-redis --replicas 0
kubectl -n argocd scale deployment/argocd-notifications-controller --replicas 0

echo "SETUP install multicloud-integrations"
kubectl config use-context kind-hub
kubectl apply -f deploy/crds/
kubectl apply -f deploy/controller/

sleep 60s

echo "SETUP print managed cluster setup"
kubectl config use-context kind-cluster1
kubectl -n argocd get deploy
kubectl -n argocd get statefulset

echo "SETUP print hub setup"
kubectl config use-context kind-hub
kubectl -n argocd get deploy
kubectl -n argocd get statefulset
kubectl -n open-cluster-management get deploy

### GitOpsCluster
echo "TEST GitOpsCluster"
kubectl config use-context kind-hub
kubectl apply -f examples/argocd/
sleep 10s
if kubectl -n argocd get gitopsclusters argo-ocm-importer -o yaml | grep successful; then
    echo "GitOpsCluster: status successful"
else
    echo "GitOpsCluster FAILED: status not successful"
    exit 1
fi
if kubectl -n argocd get secret cluster1-cluster-secret; then
    echo "GitOpsCluster: cluster1-cluster-secret created"
else
    echo "GitOpsCluster FAILED: cluster1-cluster-secret not created"
    exit 1
fi

### Propagation
echo "TEST Propagation"
kubectl config use-context kind-cluster1
kubectl apply -f e2e/managed/
kubectl config use-context kind-hub
kubectl apply -f e2e/hub/
sleep 10s
if kubectl -n argocd get application cluster1-guestbook-app; then
    echo "Propagation: application cluster1-guestbook-app created"
else
    echo "Propagation FAILED: application cluster1-guestbook-app not created"
    exit 1
fi
if kubectl -n cluster1 get manifestwork | grep cluster1-guestbook-app; then
    echo "Propagation: manifestwork created"
else
    echo "Propagation FAILED: manifestwork not created"
    exit 1
fi
kubectl config use-context kind-cluster1
exit 0
