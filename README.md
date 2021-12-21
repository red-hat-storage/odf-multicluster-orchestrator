# ODF Multicluster Orchestrator

ODF Multicluster Orchestrator is a combination of Kubernetes Operator and
addons that work together to orchestrate OpenShift Data Foundation clusters
spread across multiple OpenShift clusters. It leverages Red Hat Advanced
Cluster Management for Kubernetes as it's control plane and also uses it's
addon framework.

## Requirements

* [Open Cluster Management](https://github.com/open-cluster-management-io/)
* [OpenShift Data Foundation Operator](https://github.com/red-hat-storage/odf-operator)

## Setting up locally

#### Prerequisites

- Local kubernetes development tool, such as
  [`minikube`](https://minikube.sigs.k8s.io/docs).
- Any of the supported minikube virtualization drivers, such as [`kvm2`](https://minikube.sigs.k8s.io/docs/drivers/kvm2/).
- Virtualization client,
  [`virsh`](https://access.redhat.com/documentation/en-us/red_hat_enterprise_linux/6/html/virtualization_host_configuration_and_guest_installation_guide/sect-virtualization_host_configuration_and_guest_installation_guide-host_installation-installing_kvm_packages_on_an_existing_red_hat_enterprise_linux_system).
- Kubernetes context switcher, such as
  [`kubectx`](https://github.com/ahmetb/kubectx) (optional, but recommended).

First off, we need to create a bridge so the clusters can talk to each other.

```bash
cat << EOF > br10.xml
<network>
  <name>br10</name>
  <forward mode="nat">
    <nat>
      <port start="1024" end="65535"/>
    </nat>
  </forward>
    <bridge name="br10" stp="on" delay="0"/>
    <ip address="192.168.30.1" netmask="255.255.255.0">
      <dhcp>
        <range start="192.168.30.50" end="192.168.30.200"/>
      </dhcp>
    </ip>
</network>
EOF
```

Specify the network to share between the clusters. We will set the network to
autostart, and restart the `libvirt` daemon to apply the changes.

```bash
sudo virsh net-define br10.xml
sudo virsh net-start br10
sudo virsh net-autostart br10
sudo virsh net-list --all
ip addr show dev br10
sudo systemctl restart libvirtd
```

Next, create the clusters on the specified network. We will also need `olm`
support to install the operators.

```bash
minikube start --driver=kvm2 --network=br10 --profile=hub --addons=olm
minikube start --driver=kvm2 --network=br10 --profile=cluster1 --addons=olm
minikube profile list
```

Now we will use
[`registration-operator`](https://github.com/open-cluster-management-io/registration-operator#more-details-about-deployment) to leverage the core ACM components in
our local environment so that we do not need a full fledged ACM cluster. The
targets specified below from the makefile will help us achieve this.

```bash
git clone git@github.com:open-cluster-management-io/registration-operator.git
cd registration-operator
kubectx hub
make deploy-hub
kubectx cluster1
make deploy-spoke # make sure this is run from the same directory as the above,
                  # which contains the hub cluster's kubeconfig
                  # (.hub-kubeconfig)
```

The next few commands need to be executed in the `hub` cluster.

```bash
kubectx hub
```

At this point we need to check for pending certificate requests and approve
the ones requested from the spoke cluster for it to be able to connect to the
the hub cluster.

```bash
kubectl get csr -A
kubectl certificate approve [csr-name]
kubectl patch managedcluster cluster1 -p='{"spec":{"hubAcceptsClient":true}}' --type=merge
kubectl get managedcluster cluster1 -o yaml | grep HubClusterAdminAccepted
```

Build `odf-multicluster-orchestrator` images, and make them **publically
available**.

```bash
export REGISTRY_NAMESPACE=${REGISTRY_NAMESPACE} IMAGE_TAG=${IMAGE_TAG:-latest}
make generate && \
make manifests && \
make build && \
make REGISTRY_NAMESPACE=${REGISTRY_NAMESPACE} IMAGE_TAG=${IMAGE_TAG} docker-build && \
make REGISTRY_NAMESPACE=${REGISTRY_NAMESPACE} IMAGE_TAG=${IMAGE_TAG} docker-push && \
make REGISTRY_NAMESPACE=${REGISTRY_NAMESPACE} IMAGE_TAG=${IMAGE_TAG} bundle  && \
make REGISTRY_NAMESPACE=${REGISTRY_NAMESPACE} IMAGE_TAG=${IMAGE_TAG} bundle-build && \
make REGISTRY_NAMESPACE=${REGISTRY_NAMESPACE} IMAGE_TAG=${IMAGE_TAG} bundle-push && \
make REGISTRY_NAMESPACE=${REGISTRY_NAMESPACE} IMAGE_TAG=${IMAGE_TAG} catalog-build && \
make REGISTRY_NAMESPACE=${REGISTRY_NAMESPACE} IMAGE_TAG=${IMAGE_TAG} catalog-push
```

Create a CatalogSource for the hub cluster to fetch the Orchestrator from, and 
a Subscription that points to that.

```bash
kubectl get catalogsource odf-orchestrator -n olm -o yaml
cat << EOF | oc create -f -
apiVersion: operators.coreos.com/v1alpha1
kind: CatalogSource
metadata:
  name: odf-orchestrator
  namespace: olm
spec:
  displayName: ODF Multicluster Orchestrator
  image: quay.io/${REGISTRY_NAMESPACE}/odf-multicluster-orchestrator-catalog:${IMAGE_TAG}
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
EOF
```

Check if all the resources are in a `Running` state, and that the CSV is
successfully installed. 

```bash
kubectl get csv,subscription,pod,installplan -n olm
kubectl get pods -A | grep odfmo
kubectl get csv -A | grep odf-multicluster-orchestrator
```

If the CSV installation is `Failed` due to the `OwnNamespace` install mode not 
being supported, then edit the CSV and enable support for it.

```yaml
  installModes:
  - supported: true
    type: OwnNamespace
```

Finally, we will need a `ManagedClusterAddOn` resource for the
`token-exchange-agent` pods to be created.

```bash
cat << EOF | oc create -f -
apiVersion: addon.open-cluster-management.io/v1alpha1
kind: ManagedClusterAddOn
metadata:
  name: tokenexchange
  namespace: cluster1
spec:
  installNamespace: default
EOF
```

Follow the same steps as for `cluster1` to create another managed cluster and
link it to the hub.
