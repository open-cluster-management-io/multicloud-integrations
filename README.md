## Overview 

------

This repository hosts a controller that imports ManagedCluster kind resources into Argo CD (OpenShift GitOps), based on Placement resource. 

## Quick start

------

1. Connect to OpenShift
2. Run:
   ```shell
   oc apply -f deploy/crds
   
   #TBD oc apply -f deploy/controller
   ```

## Usage
1. Create a placement resource
2. Create a GitOpsCluster resource kind that points to placement and an Argo CD namespace
3. Check Argo CD >> Configuration >> Clusters to make sure you see the imported ManagedClusters


### Troubleshooting
1. Check the logs for the multicloud-integration pod. 
2. Make sure the Placement resource generated a PlacementDecision resource kind and that the status has a decision list.

## Community, discussion, contribution, and support

Check the [CONTRIBUTING Doc](CONTRIBUTING.md) for how to contribute to the repo.

### Communication channels

Slack channel: [#open-cluster-mgmt](http://slack.k8s.io/#open-cluster-mgmt)

## License

This code is released under the Apache 2.0 license. See the file LICENSE for more information.
