# Using GitOpsCluster resource
The following example will allow you to import all OpenShift clusters into Argo CD (OpenShift GitOps)

## Requires
* Install the OpenShift GitOps operator
* Import or provision clusters. Each managed cluster you want to import requires the following label:
   ```yaml
   metadata:
     labels:
       cluster.open-cluster-management.io/clusterset: all-openshift-clusters
       ...
    ```

## Configure
* Apply the following:
   ```shell
   oc apply -f .
   ```
* Any OpenShift clusters you applied the label to, will be imported into the Argo CD provisioned by OpenShift GitOps.
* Check the Configuration tab, to see the list of clusters

## Notes
* The managed cluster's name is used as the Argo CD server name
* The managed cluster's API is used as the Argo CD cluster URL