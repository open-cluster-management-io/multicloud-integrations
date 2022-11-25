# Using GitOpsCluster resource
The following example will allow you to import all OCM `ManagedCluster` resources 

## Requires
* Open Cluster Management hub cluster.
* Install the Argo CD.
* Register/Import/Join clusters.

## Configure
* Apply the following:
   ```shell
   kubectl apply -f .
   ```
* All `ManagedCluster` will be imported into the Argo CD.
* Check the Argo CD Configuration tab, to see the list of clusters

## Notes
* The `ManagedCluster`'s name is used as the Argo CD server name
* The `ManagedCluster`'s API is used as the Argo CD cluster URL
