# Using GitOpsCluster resource
The following example will allow you to import all OCM `ManagedCluster` resources 
with the `vendor: OpenShift` label into OpenShift GitOps (Argo CD).

## Requires
* Open Cluster Management or RHACM hub cluster.
* Install the OpenShift GitOps operator.
* Register/Import or provision Openshift clusters. The `ManagedCluster` resource should contain a `vendor` label with `OpenShift` value.

## Configure
* Apply the following:
   ```shell
   kubectl apply -f .
   ```
* Any OpenShift clusters with the correct label will be imported into the Argo CD provisioned by OpenShift GitOps.
* Check the Openshift GitOps (Argo CD) Configuration tab, to see the list of clusters

## Notes
* The `ManagedCluster`'s name is used as the Argo CD server name
* The `ManagedCluster`'s API is used as the Argo CD cluster URL
