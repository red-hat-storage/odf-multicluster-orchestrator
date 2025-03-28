# ODF Multicluster Orchestrator

ODF Multicluster Orchestrator is a combination of Kubernetes Operator and
addons that work together to orchestrate OpenShift Data Foundation clusters
spread across multiple OpenShift clusters. It leverages Red Hat Advanced
Cluster Management for Kubernetes as it's control plane and also uses it's
addon framework.

## Requirements

* [Open Cluster Management](https://github.com/open-cluster-management-io/)
* [OpenShift Data Foundation Operator](https://github.com/red-hat-storage/odf-operator)

## Setting up operator locally for development

### Pre-requisites:
a. Have kubectl, minikube, docker/podman installed.  
b. Login to the relevant container registry (eg - docker, quay) 

### Setup minikube with OLM 
> minikube start && minikube addons enable olm

### Building the operator 
> export REGISTRY_NAMESPACE={YOUR_REGISTRY_NAMESPACE}
> export IMAGE_TAG={YOUR_IMAGE_TAG}
> make generate  
> make manifests  
> make build  
> make docker-build  
> make docker-push  
> make bundle  
> make bundle-build  
> make bundle-push  
> make catalog-build  
> make catalog-push  

### Edit the CatalogSource, Subscription and apply to cluster 
```
  apiVersion: operators.coreos.com/v1alpha1            
kind: CatalogSource      
metadata:
  name: odf-orchestrator
  namespace: olm      
spec:
  displayName: ODF Multicluster Orchestrator
  icon:                              
    base64data: PHN2ZyBpZD0iTGF5ZXJfMSIgZGF0YS1uYW1lPSJMYXllciAxIiB4bWxucz0iaHR0cDovL3d3dy53My5vcmcvMjAwMC9zdmciIHZpZXdCb3g9IjAgMCAxOTIgMTQ1Ij48ZGVmcz48c3R5bGU+LmNscy0xe2ZpbGw6I2UwMDt9PC9zdHlsZT48L2RlZnM+PHRpdGxlPlJlZEhhdC1Mb2dvLUhhdC1Db2xvcjwvdGl0bGU+PHBhdGggZD0iTTE1Ny43Nyw2Mi42MWExNCwxNCwwLDAsMSwuMzEsMy40MmMwLDE0Ljg4LTE4LjEsMTcuNDYtMzAuNjEsMTcuNDZDNzguODMsODMuNDksNDIuNTMsNTMuMjYsNDIuNTMsNDRhNi40Myw2LjQzLDAsMCwxLC4yMi0xLjk0bC0zLjY2LDkuMDZhMTguNDUsMTguNDUsMCwwLDAtMS41MSw3LjMzYzAsMTguMTEsNDEsNDUuNDgsODcuNzQsNDUuNDgsMjAuNjksMCwzNi40My03Ljc2LDM2LjQzLTIxLjc3LDAtMS4wOCwwLTEuOTQtMS43My0xMC4xM1oiLz48cGF0aCBjbGFzcz0iY2xzLTEiIGQ9Ik0xMjcuNDcsODMuNDljMTIuNTEsMCwzMC42MS0yLjU4LDMwLjYxLTE3LjQ2YTE0LDE0LDAsMCwwLS4zMS0zLjQybC03LjQ1LTMyLjM2Yy0xLjcyLTcuMTItMy4yMy0xMC4zNS0xNS43My0xNi42QzEyNC44OSw4LjY5LDEwMy43Ni41LDk3LjUxLjUsOTEuNjkuNSw5MCw4LDgzLjA2LDhjLTYuNjgsMC0xMS42NC01LjYtMTcuODktNS42LTYsMC05LjkxLDQuMDktMTIuOTMsMTIuNSwwLDAtOC40MSwyMy43Mi05LjQ5LDI3LjE2QTYuNDMsNi40MywwLDAsMCw0Mi41Myw0NGMwLDkuMjIsMzYuMywzOS40NSw4NC45NCwzOS40NU0xNjAsNzIuMDdjMS43Myw4LjE5LDEuNzMsOS4wNSwxLjczLDEwLjEzLDAsMTQtMTUuNzQsMjEuNzctMzYuNDMsMjEuNzdDNzguNTQsMTA0LDM3LjU4LDc2LjYsMzcuNTgsNTguNDlhMTguNDUsMTguNDUsMCwwLDEsMS41MS03LjMzQzIyLjI3LDUyLC41LDU1LC41LDc0LjIyYzAsMzEuNDgsNzQuNTksNzAuMjgsMTMzLjY1LDcwLjI4LDQ1LjI4LDAsNTYuNy0yMC40OCw1Ni43LTM2LjY1LDAtMTIuNzItMTEtMjcuMTYtMzAuODMtMzUuNzgiLz48L3N2Zz4=
    mediatype: image/svg+xml
  image: {YOUR_CATALOG_IMAGE}
  publisher: Red Hat
  sourceType: grpc
---
apiVersion: operators.coreos.com/v1alpha1            
kind: Subscription       
metadata:
  name: odf-orchestrator
  namespace: olm     
spec:
  channel: alpha       
  name: odf-multicluster-orchestrator
  source: odf-orchestrator
  sourceNamespace: olm
  ```

### Apply a sample mirrorpeer.yaml to trigger the reconcile

```
apiVersion: multicluster.odf.openshift.io/v1alpha1
kind: MirrorPeer
metadata:
  name: mirrorpeer-sample
spec:
  items:
  - clusterName: foo
    storageClusterRef:
      name: storagecluster-1
      namespace: openshift-storage
  - clusterName: bar
    storageClusterRef:
      name: storagecluster-2
      namespace: openshift-storage
```