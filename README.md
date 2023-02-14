## Overview

------

This repository hosts a controller that imports Open Cluster Management (OCM) `ManagedCluster` resources into Argo CD (OpenShift GitOps), based on OCM `Placement` resource.

## Quick start

------

1. Connect to Kubernetes cluster.
2. Run:
   ```shell
   kubectl apply -f deploy/crds
   
   kubectl apply -f deploy/controller
   ```

## Usage
1. Create a `ManagedClusterSet` and a `ManagedClusterSetBinding`.
2. Create a `Placement` resource.
3. Create a `GitOpsCluster` resource that points to `Placement` and an Argo CD namespace
4. Check Argo CD >> Configuration >> Clusters to make sure you see the imported ManagedClusters

See [examples](/examples/) for more details.

### Troubleshooting
1. Check the logs for the multicloud-integration pod. 
2. Make sure the `Placement` resource generated at least one `PlacementDecision` resource and that the status has a decision list.

## Community, discussion, contribution, and support

Check the [CONTRIBUTING Doc](CONTRIBUTING.md) for how to contribute to the repo.

### Communication channels

Slack channel: [#open-cluster-mgmt](https://kubernetes.slack.com/channels/open-cluster-mgmt)

## License

This code is released under the Apache 2.0 license. See the file LICENSE for more information.
