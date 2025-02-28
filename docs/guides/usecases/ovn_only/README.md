# OVN Kubernetes

In this configuration OVN Kubernetes is offloaded to the DPU.

> [!WARNING]  
>
> Due to several known issues regarding the stability of this specific deployment 
> use-case (OVN Kubernetes), it should be considered a proof-of-concept in this release.  
>
> Please use it at your own risk!

<!-- toc -->
- [Prerequisites](#prerequisites)
  - [Software prerequisites](#software-prerequisites)
  - [Network prerequisites](#network-prerequisites)
    - [Control plane Nodes](#control-plane-nodes)
    - [Worker Nodes](#worker-nodes)
  - [Kubernetes prerequisites](#kubernetes-prerequisites)
    - [Control plane Nodes](#control-plane-nodes-1)
    - [Worker Nodes](#worker-nodes-1)
    - [Virtual functions](#virtual-functions)
- [Installation guide](#installation-guide)
  - [0. Required variables](#0-required-variables)
  - [1. CNI installation](#1-cni-installation)
    - [Create the Namespace](#create-the-namespace)
    - [Docker login and create image pull secret](#docker-login-and-create-image-pull-secret)
    - [Verification](#verification)
  - [2. DPF Operator installation](#2-dpf-operator-installation)
    - [Log in to private registries](#log-in-to-private-registries)
    - [Install cert-manager](#install-cert-manager)
    - [Install a CSI to back the DPUCluster etcd](#install-a-csi-to-back-the-dpucluster-etcd)
    - [Create secrets and storage required by the DPF Operator](#create-secrets-and-storage-required-by-the-dpf-operator)
    - [Deploy the DPF Operator](#deploy-the-dpf-operator)
    - [Verification](#verification-1)
  - [3. DPF System installation](#3-dpf-system-installation)
    - [Deploy the DPF System components](#deploy-the-dpf-system-components)
    - [Verification](#verification-2)
  - [4. Install components to enable accelerated CNI nodes](#4-install-components-to-enable-accelerated-cni-nodes)
    - [Install Multus and SRIOV Network Operator using NVIDIA Network Operator](#install-multus-and-sriov-network-operator-using-nvidia-network-operator)
    - [Install the OVN Kubernetes resource injection webhook](#install-the-ovn-kubernetes-resource-injection-webhook)
    - [Apply the NICClusterConfiguration and SriovNetworkNodePolicy](#apply-the-nicclusterconfiguration-and-sriovnetworknodepolicy)
    - [Verification](#verification-3)
  - [5. DPU Provisioning and Service Installation](#5-dpu-provisioning-and-service-installation)
    - [Label the image pull secret](#label-the-image-pull-secret)
    - [5.1. With User-Defined DPUSet and DPUService](#51-with-user-defined-dpuset-and-dpuservice)
      - [Create the DPF provisioning and DPUService objects](#create-the-dpf-provisioning-and-dpuservice-objects)
    - [5.2. With DPUDeployment](#52-with-dpudeployment)
      - [Create the DPUDeployment, DPUServiceConfig, DPUServiceTemplate and other necessary objects](#create-the-dpudeployment-dpuserviceconfig-dpuservicetemplate-and-other-necessary-objects)
    - [Verification](#verification-4)
  - [6. Test traffic](#6-test-traffic)
    - [Add worker nodes to the cluster](#add-worker-nodes-to-the-cluster)
    - [Deploy test pods](#deploy-test-pods)
  - [7. Deletion and clean up](#7-deletion-and-clean-up)
    - [Delete the test pods](#delete-the-test-pods)
    - [Delete DPF CNI acceleration components](#delete-dpf-cni-acceleration-components)
    - [Delete the DPF Operator system and DPF Operator](#delete-the-dpf-operator-system-and-dpf-operator)
    - [Delete DPF Operator dependencies](#delete-dpf-operator-dependencies)
- [Limitations of DPF Setup](#limitations-of-dpf-setup)
  - [Host network pod services](#host-network-pod-services)
<!-- /toc -->

## Prerequisites
The system is set up as described in the [system prerequisites](../prerequisites.md). The OVN Kubernetes deployment has these additional requirements: 

### Software prerequisites
This guide uses the following tools which must be installed on the machine where the commands contained in this guide run..
- kubectl
- helm
- envsubst

### Network prerequisites

#### Control plane Nodes
- Open vSwitch (OVS) packages installed - i.e. `openvswitch-switch` for Ubuntu 24.04
- out-of-band management port should be configured as OVS bridge port with "bridge-uplink" OVS metadata [This addresses a known issue](../../../release-notes/v24.10.0.md#known-issues-and-limitations).
- DNS stub resolver should be disabled if using systemd resolvd

#### Worker Nodes
- Open vSwitch (OVS) packages not installed
- [Host networking must be set up as expected](../host-network-configuration-prerequisite.md)
- Only a single DPU uplink is used with this deployment (p0).
- All worker nodes are connected to the same L2 broadcast domain (VLAN) on the high-speed network.
- Host high-speed port (Host PF0) must have DHCP enabled.
- An external DHCP Server should be used for the high-speed network:
  - The DHCP server must not assign a default gateway to the DHCP clients.
  - The DHCP server should assign a special route (option 121) for a "dummy" IP subnet with a next hop address of the actual default gateway router serving the high-speed network.<br>
  - The special route (which is configurable) is used by DPF to inject the default gateway into the overlay network. By default, DPF is looking for the subnet 169.254.99.100/32 in the special route and extracts the gateway address.<br>
  - The gateway value sent using option 121 should be calculated according to RFC3442 (An online calculator exists). For example, the value of "20:a9:fe:63:64:0a:00:7b:fe" represents a route to 169.254.99.100/32 via 10.0.123.254.

### Kubernetes prerequisites
- CNI not installed
- kube-proxy not installed
- coreDNS should be configured to run only on control plane nodes - e.g. using NodeAffinity.
- control plane setup is complete before starting this guide
- worker nodes are not added until indicated by this guide

#### Control plane Nodes
- Have the labels:
  - `"k8s.ovn.org/zone-name": $KUBERNETES_NODE_NAME`

#### Worker Nodes
- Have the labels:
  - `"k8s.ovn.org/dpu-host": ""`
  - `"k8s.ovn.org/zone-name": $KUBERNETES_NODE_NAME`
- Have the annotations:
  - `"k8s.ovn.org/remote-zone-migrated": $KUBERNETES_NODE_NAME`

#### Virtual functions
A number of virtual functions (VFs) will be created on hosts when provisioning DPUs. Certain of these VFs are marked for specific usage:
- The first VF (vf0) is used by provisioning components.
- The second VF (vf1) is used by ovn-kubernetes.
- The remaining VFs are allocated by SR-IOV Device Plugin. Each pod using OVN Kubernetes in DPU mode as its primary CNI will have one of these VFs injected at Pod creation time. 

## Installation guide

### 0. Required variables
The following variables are required by this guide. A sensible default is provided where it makes sense, but many will be specific to the target infrastructure.

Commands in this guide are run in the same directory that contains this readme.

```bash
## IP Address for the Kubernetes API server of the target cluster on which DPF is installed.
## This should never include a scheme or a port.
## e.g. 10.10.10.10
export TARGETCLUSTER_API_SERVER_HOST=

## Port for the Kubernetes API server of the target cluster on which DPF is installed.
export TARGETCLUSTER_API_SERVER_PORT=6443

## IP address range for hosts in the target cluster on which DPF is installed.
## This is a CIDR in the form e.g. 10.10.10.0/24
export TARGETCLUSTER_NODE_CIDR=

## Virtual IP used by the load balancer for the DPU Cluster. Must be a reserved IP from the management subnet and not allocated by DHCP.
export DPUCLUSTER_VIP=

## DPU_P0 is the name of the first port of the DPU. This name must be the same on all worker nodes.
export DPU_P0=

## DPU_P0_VF1 is the name of the second Virtual Function (VF) of the first port of the DPU. This name must be the same on all worker nodes.
export DPU_P0_VF1=

## Interface/bridge on which the DPUCluster load balancer will listen. Should be the management interface/bridge of the control plane node.
export DPUCLUSTER_INTERFACE=

# IP address to the NFS server used as storage for the BFB.
export NFS_SERVER_IP=

# API key for accessing containers and helm charts from the NGC private repository.
# Note: This isn't technically required when using public images but is included here to demonstrate the secret flow in DPF when using images from a private registry.
export NGC_API_KEY=

## POD_CIDR is the CIDR used for pods in the target Kubernetes cluster.
export POD_CIDR=10.233.64.0/18

## SERVICE_CIDR is the CIDR used for services in the target Kubernetes cluster.
## This is a CIDR in the form e.g. 10.10.10.0/24
export SERVICE_CIDR=10.233.0.0/18 

## DPF_VERSION is the version of the DPF components which will be deployed in this guide.
export DPF_VERSION=v24.10.0

## URL to the BFB used in the `bfb.yaml` and linked by the DPUSet.
export BLUEFIELD_BITSTREAM="https://content.mellanox.com/BlueField/BFBs/Ubuntu22.04/bf-bundle-2.9.1-30_24.11_ubuntu-22.04_prod.bfb"
```

### 1. CNI installation

OVN Kubernetes is used as the primary CNI for the cluster. On worker nodes the primary CNI will be accelerated by offloading work to the DPU. On control plane nodes OVN Kubernetes will run without offloading.

#### Create the Namespace

```shell
kubectl create ns ovn-kubernetes
```

#### Docker login and create image pull secret

The image pull secret is required when using a private registry to host images and helm charts. If using a public registry this section can be ignored.
```shell
helm registry login nvcr.io --username \$oauthtoken --password $NGC_API_KEY

kubectl -n ovn-kubernetes create secret docker-registry dpf-pull-secret --docker-server=nvcr.io --docker-username="\$oauthtoken" --docker-password=$NGC_API_KEY
````

#### Install OVN Kubernetes from the helm chart

Install the OVN Kubernetes CNI components from the helm chart. A number of [environment variables](#0-required-variables) must be set before running this command.
```shell
envsubst < manifests/01-cni-installation/helm-values/ovn-kubernetes.yml | helm upgrade --install -n ovn-kubernetes ovn-kubernetes oci://ghcr.io/nvidia/ovn-kubernetes-chart --version $DPF_VERSION --values -
```

<details><summary>Expand for detailed helm values</summary>

[embedmd]:#(manifests/01-cni-installation/helm-values/ovn-kubernetes.yml)
```yml
tags:
  ovn-kubernetes-resource-injector: false
global:
  imagePullSecretName: "dpf-pull-secret"
k8sAPIServer: https://$TARGETCLUSTER_API_SERVER_HOST:$TARGETCLUSTER_API_SERVER_PORT
ovnkube-node-dpu-host:
  nodeMgmtPortNetdev: $DPU_P0_VF1
  gatewayOpts: --gateway-interface=$DPU_P0
## Note this CIDR is followed by a trailing /24 which informs OVN Kubernetes on how to split the CIDR per node.
podNetwork: $POD_CIDR/24
serviceNetwork: $SERVICE_CIDR
ovn-kubernetes-resource-injector:
  resourceName: nvidia.com/bf3-p0-vfs
dpuServiceAccountNamespace: dpf-operator-system
```
</details>

#### Verification

These verification commands may need to be run multiple times to ensure the condition is met.

Verify the CNI installation with:
```shell
## Ensure all nodes in the cluster are ready.
kubectl wait --for=condition=ready nodes --all
## Ensure all pods in the ovn-kubernetes namespace are ready.
kubectl wait --for=condition=ready --namespace ovn-kubernetes pods --all --timeout=300s
```

### 2. DPF Operator installation

#### Log in to private registries
The login and secret is required when using a private registry to host images and helm charts. If using a public registry this section can be ignored.
```shell
kubectl create namespace dpf-operator-system
kubectl -n dpf-operator-system create secret docker-registry dpf-pull-secret --docker-server=nvcr.io --docker-username="\$oauthtoken" --docker-password=$NGC_API_KEY
helm registry login nvcr.io --username \$oauthtoken --password $NGC_API_KEY
```

#### Install cert-manager
Cert manager is a prerequisite which is used to provide certificates for webhooks used by DPF and its dependencies. 

```shell
helm repo add jetstack https://charts.jetstack.io --force-update
helm upgrade --install --create-namespace --namespace cert-manager cert-manager jetstack/cert-manager --version v1.16.1 -f ./manifests/02-dpf-operator-installation/helm-values/cert-manager.yml
```
<details><summary>Expand for detailed helm values</summary>

[embedmd]:#(manifests/02-dpf-operator-installation/helm-values/cert-manager.yml)
```yml
startupapicheck:
  enabled: false
crds:
  enabled: true
affinity:
  nodeAffinity:
    requiredDuringSchedulingIgnoredDuringExecution:
      nodeSelectorTerms:
        - matchExpressions:
            - key: node-role.kubernetes.io/master
              operator: Exists
        - matchExpressions:
            - key: node-role.kubernetes.io/control-plane
              operator: Exists
tolerations:
  - operator: Exists
    effect: NoSchedule
    key: node-role.kubernetes.io/control-plane
  - operator: Exists
    effect: NoSchedule
    key: node-role.kubernetes.io/master
cainjector:
  affinity:
    nodeAffinity:
      requiredDuringSchedulingIgnoredDuringExecution:
        nodeSelectorTerms:
          - matchExpressions:
              - key: node-role.kubernetes.io/master
                operator: Exists
          - matchExpressions:
              - key: node-role.kubernetes.io/control-plane
                operator: Exists
  tolerations:
    - operator: Exists
      effect: NoSchedule
      key: node-role.kubernetes.io/control-plane
    - operator: Exists
      effect: NoSchedule
      key: node-role.kubernetes.io/master
webhook:
  affinity:
    nodeAffinity:
      requiredDuringSchedulingIgnoredDuringExecution:
        nodeSelectorTerms:
          - matchExpressions:
              - key: node-role.kubernetes.io/master
                operator: Exists
          - matchExpressions:
              - key: node-role.kubernetes.io/control-plane
                operator: Exists
  tolerations:
    - operator: Exists
      effect: NoSchedule
      key: node-role.kubernetes.io/control-plane
    - operator: Exists
      effect: NoSchedule
      key: node-role.kubernetes.io/master
```
</details>

#### Install a CSI to back the DPUCluster etcd

In this guide the local-path-provisioner CSI from Rancher is used to back the etcd of the Kamaji based DPUCluster. This should be substituted for a reliable performant CNI to back etcd.

```shell
curl https://codeload.github.com/rancher/local-path-provisioner/tar.gz/v0.0.30 | tar -xz --strip=3 local-path-provisioner-0.0.30/deploy/chart/local-path-provisioner/
kubectl create ns local-path-provisioner
helm install -n local-path-provisioner local-path-provisioner ./local-path-provisioner --version 0.0.30 -f ./manifests/02-dpf-operator-installation/helm-values/local-path-provisioner.yml
```
<details><summary>Expand for detailed helm values</summary>

[embedmd]:#(manifests/02-dpf-operator-installation/helm-values/local-path-provisioner.yml)
```yml
tolerations:
  - operator: Exists
    effect: NoSchedule
    key: node-role.kubernetes.io/control-plane
  - operator: Exists
    effect: NoSchedule
    key: node-role.kubernetes.io/master
```
</details>

#### Create secrets and storage required by the DPF Operator
A number of [environment variables](#0-required-variables) must be set before running this command.

```shell
cat manifests/02-dpf-operator-installation/*.yaml | envsubst | kubectl apply -f - 
```

This deploys the following objects:

<details><summary>Secret for pulling images and helm charts</summary>

[embedmd]:#(manifests/02-dpf-operator-installation/helm-secret-dpf.yaml)
```yaml
---
apiVersion: v1
kind: Secret
metadata:
  name: ngc-doca-oci-helm
  namespace: dpf-operator-system
  labels:
    argocd.argoproj.io/secret-type: repository
stringData:
  name: nvstaging-doca-oci
  url: nvcr.io/nvstaging/doca
  type: helm
  ## Note `no_variable` here is used to ensure envsubst renders the correct username which is `$oauthtoken`
  username: $${no_variable}oauthtoken
  password: $NGC_API_KEY
---
apiVersion: v1
kind: Secret
metadata:
  name: ngc-doca-https-helm
  namespace: dpf-operator-system
  labels:
    argocd.argoproj.io/secret-type: repository
stringData:
  name: nvstaging-doca-https
  url: https://helm.ngc.nvidia.com/nvstaging/doca
  type: helm
  username: $${no_variable}oauthtoken
  password: $NGC_API_KEY
```
</details>

<details><summary>PersistentVolume and PersistentVolumeClaim for the provisioning controller</summary>

[embedmd]:#(manifests/02-dpf-operator-installation/nfs-storage-for-bfb-dpf-ga.yaml)
```yaml
---
apiVersion: v1
kind: PersistentVolume
metadata:
  name: bfb-pv
spec:
  capacity:
    storage: 10Gi
  volumeMode: Filesystem
  accessModes:
    - ReadWriteMany
  nfs: 
    path: /mnt/dpf_share/bfb
    server: $NFS_SERVER_IP
  persistentVolumeReclaimPolicy: Delete
---
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: bfb-pvc
  namespace: dpf-operator-system
spec:
  accessModes:
  - ReadWriteMany
  resources:
    requests:
      storage: 10Gi
  volumeMode: Filesystem
```
</details>

#### Deploy the DPF Operator

A number of [environment variables](#0-required-variables) must be set before running this command.
```shell
envsubst < ./manifests/02-dpf-operator-installation/helm-values/dpf-operator.yml | helm upgrade --install -n dpf-operator-system dpf-operator oci://ghcr.io/nvidia/dpf-operator --version=$DPF_VERSION --values -
```

<details><summary>Expand for detailed helm values</summary>

[embedmd]:#(manifests/02-dpf-operator-installation/helm-values/dpf-operator.yml)
```yml
imagePullSecrets:
  - name: dpf-pull-secret
kamaji-etcd:
  persistentVolumeClaim:
    storageClassName: local-path
node-feature-discovery:
  worker:
    extraEnvs:
      - name: "KUBERNETES_SERVICE_HOST"
        value: "$TARGETCLUSTER_API_SERVER_HOST"
      - name: "KUBERNETES_SERVICE_PORT"
        value: "$TARGETCLUSTER_API_SERVER_PORT"
```
</details>

#### Verification

These verification commands may need to be run multiple times to ensure the condition is met.

Verify the DPF Operator installation with:
```shell
## Ensure the DPF Operator deployment is available.
kubectl rollout status deployment --namespace dpf-operator-system dpf-operator-controller-manager
## Ensure all pods in the DPF Operator system are ready.
kubectl wait --for=condition=ready --namespace dpf-operator-system pods --all
```

### 3. DPF System installation
This section involves creating the DPF system components and some basic infrastructure required for a functioning DPF-enabled cluster.

#### Deploy the DPF System components
A number of [environment variables](#0-required-variables) must be set before running this command.
```shell
kubectl create ns dpu-cplane-tenant1
cat manifests/03-dpf-system-installation/*.yaml | envsubst | kubectl apply -f - 
```

This will create the following objects:
<details><summary>DPF Operator to install the DPF System components</summary>

[embedmd]:#(manifests/03-dpf-system-installation/operatorconfig.yaml)
```yaml
---
apiVersion: operator.dpu.nvidia.com/v1alpha1
kind: DPFOperatorConfig
metadata:
  name: dpfoperatorconfig
  namespace: dpf-operator-system
spec:
  imagePullSecrets:
  - dpf-pull-secret
  provisioningController:
    bfbPVCName: "bfb-pvc"
    dmsTimeout: 900
  kamajiClusterManager:
    disable: false
```
</details>

<details><summary>DPUCluster to serve as Kubernetes control plane for DPU nodes</summary>

[embedmd]:#(manifests/03-dpf-system-installation/dpucluster.yaml)
```yaml
---
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUCluster
metadata:
  name: dpu-cplane-tenant1
  namespace: dpu-cplane-tenant1
spec:
  type: kamaji
  maxNodes: 10
  version: v1.30.2
  clusterEndpoint:
    # deploy keepalived instances on the nodes that match the given nodeSelector.
    keepalived:
      # interface on which keepalived will listen. Should be the oob interface of the control plane node.
      interface: $DPUCLUSTER_INTERFACE
      # Virtual IP reserved for the DPU Cluster load balancer. Must not be allocatable by DHCP.
      vip: $DPUCLUSTER_VIP
      # virtualRouterID must be in range [1,255], make sure the given virtualRouterID does not duplicate with any existing keepalived process running on the host
      virtualRouterID: 126
      nodeSelector:
        node-role.kubernetes.io/control-plane: ""
```
</details>


#### Verification

These verification commands may need to be run multiple times to ensure the condition is met.

Verify the DPF System with:
```shell
## Ensure the provisioning and DPUService controller manager deployments are available.
kubectl rollout status deployment --namespace dpf-operator-system dpf-provisioning-controller-manager dpuservice-controller-manager
## Ensure all other deployments in the DPF Operator system are Available.
kubectl rollout status deployment --namespace dpf-operator-system
## Ensure the DPUCluster is ready for nodes to join.
kubectl wait --for=condition=ready --namespace dpu-cplane-tenant1 dpucluster --all
```

### 4. Install components to enable accelerated CNI nodes

OVN Kubernetes will accelerate traffic by attaching a VF to each pod using the primary CNI. This VF is used to offload flows to the DPU. This section details the components needed to connect pods to the offloaded OVN Kubernetes CNI.

#### Install Multus and SRIOV Network Operator using NVIDIA Network Operator

```shell
helm repo add nvidia https://helm.ngc.nvidia.com/nvidia --force-update
helm upgrade --no-hooks --install --create-namespace --namespace nvidia-network-operator network-operator nvidia/network-operator --version 24.7.0 -f ./manifests/04-enable-accelerated-cni/helm-values/network-operator.yml
```

<details><summary>Expand for detailed helm values</summary>

[embedmd]:#(manifests/04-enable-accelerated-cni/helm-values/network-operator.yml)
```yml
nfd:
  enabled: false
  deployNodeFeatureRules: false
sriovNetworkOperator:
  enabled: true
sriov-network-operator:
  operator:
    affinity:
      nodeAffinity:
        requiredDuringSchedulingIgnoredDuringExecution:
          nodeSelectorTerms:
            - matchExpressions:
                - key: node-role.kubernetes.io/master
                  operator: Exists
            - matchExpressions:
                - key: node-role.kubernetes.io/control-plane
                  operator: Exists
  crds:
    enabled: true
  sriovOperatorConfig:
    deploy: true
    configDaemonNodeSelector: null
operator:
  affinity:
    nodeAffinity:
      requiredDuringSchedulingIgnoredDuringExecution:
        nodeSelectorTerms:
          - matchExpressions:
              - key: node-role.kubernetes.io/master
                operator: Exists
          - matchExpressions:
              - key: node-role.kubernetes.io/control-plane
                operator: Exists
```
</details>

#### Install the OVN Kubernetes resource injection webhook

The OVN Kubernetes resource injection webhook injected each pod scheduled to a worker node with a request for a VF and a Network Attachment Definition. This webhook is part of the same helm chart as the other components of the OVN Kubernetes CNI. Here it is installed by adjusting the existing helm installation to add the webhook component to the installation. 

```shell
envsubst < manifests/04-enable-accelerated-cni/helm-values/ovn-kubernetes.yml | helm upgrade --install -n ovn-kubernetes ovn-kubernetes oci://ghcr.io/nvidia/ovn-kubernetes-chart --version $DPF_VERSION --values -
```

<details><summary>Expand for detailed helm values</summary>

[embedmd]:#(manifests/04-enable-accelerated-cni/helm-values/ovn-kubernetes.yml)
```yml
tags:
  ## Enable the ovn-kubernetes-resource-injector
  ovn-kubernetes-resource-injector: true
global:
  imagePullSecretName: "dpf-pull-secret"
k8sAPIServer: https://$TARGETCLUSTER_API_SERVER_HOST:$TARGETCLUSTER_API_SERVER_PORT
ovnkube-node-dpu-host:
  nodeMgmtPortNetdev: $DPU_P0_VF1
  gatewayOpts: --gateway-interface=$DPU_P0
## Note this CIDR is followed by a trailing /24 which informs OVN Kubernetes on how to split the CIDR per node.
podNetwork: $POD_CIDR/24
serviceNetwork: $SERVICE_CIDR
ovn-kubernetes-resource-injector:
  resourceName: nvidia.com/bf3-p0-vfs
dpuServiceAccountNamespace: dpf-operator-system
```
</details>

#### Apply the NICClusterConfiguration and SriovNetworkNodePolicy

```shell
cat manifests/04-enable-accelerated-cni/*.yaml | envsubst | kubectl apply -f -
```

This will deploy the following objects:

<details><summary>NICClusterPolicy for the NVIDIA Network Operator</summary>

[embedmd]:#(manifests/04-enable-accelerated-cni/nic_cluster_policy.yaml)
```yaml
---
apiVersion: mellanox.com/v1alpha1
kind: NicClusterPolicy
metadata:
  name: nic-cluster-policy
spec:
  secondaryNetwork:
    multus:
      image: multus-cni
      imagePullSecrets: []
      repository: ghcr.io/k8snetworkplumbingwg
      version: v3.9.3
```
</details>

<details><summary>SriovNetworkNodePolicy for the SR-IOV Network Operator</summary>

[embedmd]:#(manifests/04-enable-accelerated-cni/sriov_network_operator_policy.yaml)
```yaml
---
apiVersion: sriovnetwork.openshift.io/v1
kind: SriovNetworkNodePolicy
metadata:
  name: bf3-p0-vfs
  namespace: nvidia-network-operator
spec:
  mtu: 1500
  nicSelector:
    deviceID: "a2dc"
    vendor: "15b3"
    pfNames:
    - $DPU_P0#2-45
  nodeSelector:
    node-role.kubernetes.io/worker: ""
  numVfs: 46
  resourceName: bf3-p0-vfs
  isRdma: true
  externallyManaged: true
  deviceType: netdevice
  linkType: eth

```
</details>

#### Verification

These verification commands may need to be run multiple times to ensure the condition is met.

Verify the DPF System with:
```shell
## Ensure the provisioning and DPUService controller manager deployments are available.
kubectl wait --for=condition=Ready --namespace nvidia-network-operator pods --all
## Expect the following Daemonsets to be successfully rolled out.
kubectl rollout status daemonset --namespace nvidia-network-operator kube-multus-ds sriov-network-config-daemon sriov-device-plugin 
## Expect the network injector to be successfully rolled out.
kubectl rollout status deployment --namespace ovn-kubernetes ovn-kubernetes-ovn-kubernetes-resource-injector  
```

### 5. DPU Provisioning and Service Installation

In this step we deploy our DPUs and the services that will run on them. There are two ways to do this and that will be
explained in the following sections [5.1](#51-with-user-defined-dpuset-and-dpuservice) and [5.2](#52-with-dpudeployment).

#### Label the image pull secret

The image pull secret is required when using a private registry to host images and helm charts. If using a public registry this section can be ignored.

```shell
kubectl -n ovn-kubernetes label secret dpf-pull-secret dpu.nvidia.com/image-pull-secret=""
```

#### 5.1. With User-Defined DPUSet and DPUService

In this mode the user is expected to create their own DPUSet and DPUService objects.

##### Create the DPF provisioning and DPUService objects

A number of [environment variables](#0-required-variables) must be set before running this command.

```shell
cat manifests/05.1-dpuservice-installation/*.yaml | envsubst | kubectl apply -f - 
```

This will deploy the following objects:

<details><summary>BFB to download Bluefield Bitstream to a shared volume</summary>

[embedmd]:#(manifests/05.1-dpuservice-installation/bfb.yaml)
```yaml
---
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: BFB
metadata:
  name: bf-bundle
  namespace: dpf-operator-system
spec:
  url: $BLUEFIELD_BITSTREAM
```
</details>

<details><summary>DPUSet to provision DPUs on worker nodes</summary>

[embedmd]:#(manifests/05.1-dpuservice-installation/dpuset.yaml)
```yaml
---
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUSet
metadata:
  name: dpuset
  namespace: dpf-operator-system
spec:
  nodeSelector:
    matchLabels:
      feature.node.kubernetes.io/dpu-enabled: "true"
  strategy:
    rollingUpdate:
      maxUnavailable: "10%"
    type: RollingUpdate
  dpuTemplate:
    spec:
      dpuFlavor: dpf-provisioning-hbn-ovn
      bfb:
        name: bf-bundle
      nodeEffect:
        taint:
          key: "dpu"
          value: "provisioning"
          effect: NoSchedule
      automaticNodeReboot: true
```
</details>

<details><summary>OVN DPUService to deploy OVN workloads to the DPUs</summary>

[embedmd]:#(manifests/05.1-dpuservice-installation/ovn-dpuservice.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUService
metadata:
  name: ovn-dpu
  namespace: dpf-operator-system 
spec:
  helmChart:
    source:
      repoURL: oci://ghcr.io/nvidia
      chart: ovn-kubernetes-chart
      version: $DPF_VERSION
    values:
      tags:
        ovn-kubernetes-resource-injector: false
        ovnkube-node-dpu: true
        ovnkube-node-dpu-host: false
        ovnkube-single-node-zone: false
        ovnkube-control-plane: false
      k8sAPIServer: https://$TARGETCLUSTER_API_SERVER_HOST:$TARGETCLUSTER_API_SERVER_PORT
      podNetwork: $POD_CIDR/24
      serviceNetwork: $SERVICE_CIDR
      global:
        gatewayOpts: "--gateway-interface=br-ovn --gateway-uplink-port=puplinkbrovn"
        imagePullSecretName: dpf-pull-secret
      ovnkube-node-dpu:
        kubernetesSecretName: "ovn-dpu" # user needs to populate based on DPUServiceCredentialRequest
        hostCIDR: $TARGETCLUSTER_NODE_CIDR
        externalDHCP: true
        gatewayDiscoveryNetwork: "169.254.99.100/32" # This is a "dummy" subnet used to get the default gateway address from DHCP server (via option 121)
```
</details>

<details><summary>DOCA Telemetry Service DPUService to deploy DTS to the DPUs</summary>

[embedmd]:#(manifests/05.1-dpuservice-installation/dts-dpuservice.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUService
metadata:
  name: doca-telemetry-service
  namespace: dpf-operator-system
spec:
  helmChart:
    source:
      repoURL: https://helm.ngc.nvidia.com/nvidia/doca
      version: 0.2.3
      chart: doca-telemetry
```
</details>

<details><summary>Blueman DPUService to deploy Blueman to the DPUs</summary>

[embedmd]:#(manifests/05.1-dpuservice-installation/blueman-dpuservice.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUService
metadata:
  name: doca-blueman-service
  namespace: dpf-operator-system
spec:
  helmChart:
    source:
      repoURL: https://helm.ngc.nvidia.com/nvidia/doca
      version: 1.0.5
      chart: doca-blueman
```
</details>

<details><summary>OVN DPUServiceCredentialRequest to allow cross cluster communication</summary>

[embedmd]:#(manifests/05.1-dpuservice-installation/ovn-credentials.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceCredentialRequest
metadata:
  name: ovn-dpu
  namespace: dpf-operator-system 
spec:
  serviceAccount:
    name: ovn-dpu
    namespace: dpf-operator-system 
  duration: 24h
  type: tokenFile
  secret:
    name: ovn-dpu
    namespace: dpf-operator-system 
  metadata:
    labels:
      dpu.nvidia.com/image-pull-secret: ""
```
</details>

<details><summary>DPUServiceInterfaces for physical ports on the DPU</summary>

[embedmd]:#(manifests/05.1-dpuservice-installation/physical-ifaces.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceInterface
metadata:
  name: p0
  namespace: dpf-operator-system
spec:
  template:
    spec:
      template:
        metadata:
          labels:
            uplink: "p0"
        spec:
          interfaceType: physical
          physical:
            interfaceName: p0
```
</details>

<details><summary>OVN DPUServiceInterface to define the ports attached to OVN workloads on the DPU</summary>

[embedmd]:#(manifests/05.1-dpuservice-installation/ovn-iface.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceInterface
metadata:
  name: ovn
  namespace: dpf-operator-system
spec:
  template:
    spec:
      template:
        metadata:
          labels:
            port: ovn
        spec:
          interfaceType: ovn
```
</details>

<details><summary>DPUServiceFunctionChain to define the OVN ServiceFunctionChain</summary>

[embedmd]:#(manifests/05.1-dpuservice-installation/ovn-chain.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceChain
metadata:
  name: ovn-to-fabric
  namespace: dpf-operator-system
spec:
  template:
    spec:
      nodeSelector:
        matchExpressions:
        - key: kubernetes.io/os
          operator: In
          values:
          - "linux"
      template:
        spec:
          switches:
            - ports:
              - serviceInterface:
                  matchLabels:
                    uplink: p0
              - serviceInterface:
                  matchLabels:
                    port: ovn
```
</details>

#### 5.2. With DPUDeployment

In this mode the user is expected to create a DPUDeployment object that reflects a set of DPUServices that should run on a set of DPUs.

> If you want to learn more about `DPUDeployments`, feel free to check the [DPUDeployment documentation](../../dpudeployment.md).

##### Create the DPUDeployment, DPUServiceConfig, DPUServiceTemplate and other necessary objects

A number of [environment variables](#0-required-variables) must be set before running this command.
```shell
cat manifests/05.2-dpudeployment-installation/*.yaml | envsubst | kubectl apply -f - 
```

This will deploy the following objects:

<details><summary>BFB to download Bluefield Bitstream to a shared volume</summary>

[embedmd]:#(manifests/05.2-dpudeployment-installation/bfb.yaml)
```yaml
---
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: BFB
metadata:
  name: bf-bundle
  namespace: dpf-operator-system
spec:
  url: $BLUEFIELD_BITSTREAM
```
</details>

<details><summary>DPUDeployment to provision DPUs on worker nodes</summary>

[embedmd]:#(manifests/05.2-dpudeployment-installation/dpudeployment.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUDeployment
metadata:
  name: ovn
  namespace: dpf-operator-system
spec:
  dpus:
    bfb: bf-bundle
    flavor: dpf-provisioning-hbn-ovn
    dpuSets:
    - nameSuffix: "dpuset1"
      nodeSelector:
        matchLabels:
          feature.node.kubernetes.io/dpu-enabled: "true"
  services:
    ovn:
      serviceTemplate: ovn
      serviceConfiguration: ovn
    dts:
      serviceTemplate: dts
      serviceConfiguration: dts
    blueman:
      serviceTemplate: blueman
      serviceConfiguration: blueman
  serviceChains:
  - ports:
    - serviceInterface:
        matchLabels:
          uplink: p0
    - serviceInterface:
        matchLabels:
          port: ovn
```
</details>

<details><summary>OVN DPUServiceConfig and DPUServiceTemplate to deploy OVN workloads to the DPUs</summary>

[embedmd]:#(manifests/05.2-dpudeployment-installation/dpuserviceconfig_ovn.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceConfiguration
metadata:
  name: ovn
  namespace: dpf-operator-system
spec:
  deploymentServiceName: "ovn"
  serviceConfiguration:
    helmChart:
      values:
        k8sAPIServer: https://$TARGETCLUSTER_API_SERVER_HOST:$TARGETCLUSTER_API_SERVER_PORT
        podNetwork: $POD_CIDR/24
        serviceNetwork: $SERVICE_CIDR
        ovnkube-node-dpu:
          kubernetesSecretName: "ovn-dpu" # user needs to populate based on DPUServiceCredentialRequest
          hostCIDR: $TARGETCLUSTER_NODE_CIDR # user needs to populate
          externalDHCP: true
          gatewayDiscoveryNetwork: "169.254.99.100/32" # This is a "dummy" subnet used to get the default gateway address from DHCP server (via option 121)
```

[embedmd]:#(manifests/05.2-dpudeployment-installation/dpuservicetemplate_ovn.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceTemplate
metadata:
  name: ovn
  namespace: dpf-operator-system
spec:
  deploymentServiceName: "ovn"
  helmChart:
    source:
      repoURL: oci://ghcr.io/nvidia
      chart: ovn-kubernetes-chart
      version: $DPF_VERSION
    values:
      tags:
        ovn-kubernetes-resource-injector: false
        ovnkube-node-dpu: true
        ovnkube-node-dpu-host: false
        ovnkube-single-node-zone: false
        ovnkube-control-plane: false
      global:
        gatewayOpts: "--gateway-interface=br-ovn --gateway-uplink-port=puplinkbrovn"
        imagePullSecretName: dpf-pull-secret
```
</details>

<details><summary>DOCA Telemetry Service DPUServiceConfig and DPUServiceTemplate to deploy DTS to the DPUs</summary>

[embedmd]:#(manifests/05.2-dpudeployment-installation/dpuserviceconfig_dts.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceConfiguration
metadata:
  name: dts
  namespace: dpf-operator-system
spec:
  deploymentServiceName: "dts"
```

[embedmd]:#(manifests/05.2-dpudeployment-installation/dpuservicetemplate_dts.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceTemplate
metadata:
  name: dts
  namespace: dpf-operator-system
spec:
  deploymentServiceName: "dts"
  helmChart:
    source:
      repoURL: https://helm.ngc.nvidia.com/nvidia/doca
      version: 0.2.3
      chart: doca-telemetry
```
</details>

<details><summary>Blueman DPUServiceConfig and DPUServiceTemplate to deploy Blueman to the DPUs</summary>

[embedmd]:#(manifests/05.2-dpudeployment-installation/dpuserviceconfig_blueman.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceConfiguration
metadata:
  name: blueman
  namespace: dpf-operator-system
spec:
  deploymentServiceName: "blueman"
```

[embedmd]:#(manifests/05.2-dpudeployment-installation/dpuservicetemplate_blueman.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceTemplate
metadata:
  name: blueman
  namespace: dpf-operator-system
spec:
  deploymentServiceName: "blueman"
  helmChart:
    source:
      repoURL: https://helm.ngc.nvidia.com/nvidia/doca
      version: 1.0.5
      chart: doca-blueman
```
</details>

<details><summary>OVN DPUServiceCredentialRequest to allow cross cluster communication</summary>

[embedmd]:#(manifests/05.2-dpudeployment-installation/ovn-credentials.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceCredentialRequest
metadata:
  name: ovn-dpu
  namespace: dpf-operator-system 
spec:
  serviceAccount:
    name: ovn-dpu
    namespace: dpf-operator-system 
  duration: 24h
  type: tokenFile
  secret:
    name: ovn-dpu
    namespace: dpf-operator-system 
  metadata:
    labels:
      dpu.nvidia.com/image-pull-secret: ""
```
</details>

<details><summary>DPUServiceInterfaces for physical ports on the DPU</summary>

[embedmd]:#(manifests/05.2-dpudeployment-installation/physical-ifaces.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceInterface
metadata:
  name: p0
  namespace: dpf-operator-system
spec:
  template:
    spec:
      template:
        metadata:
          labels:
            uplink: "p0"
        spec:
          interfaceType: physical
          physical:
            interfaceName: p0
```
</details>

<details><summary>OVN DPUServiceInterface to define the ports attached to OVN workloads on the DPU</summary>

[embedmd]:#(manifests/05.2-dpudeployment-installation/ovn-iface.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceInterface
metadata:
  name: ovn
  namespace: dpf-operator-system
spec:
  template:
    spec:
      template:
        metadata:
          labels:
            port: ovn
        spec:
          interfaceType: ovn
```
</details>

#### Verification

These verification commands, which are common to both the [5.1 DPUService](#51-with-user-defined-dpuset-and-dpuservice) and [5.2 DPUDeployment](#52-with-dpudeployment) installations, may need to be run multiple times to ensure the condition is met.

Note that when using the DPUDeployment, the DPUService name will have the DPUDeployment name added as prefix. For example, `ovn-ovn`. Use the correct name for the verification.

Verify the DPU and Service installation with:
```shell
## Ensure the DPUServices are created and have been reconciled.
kubectl wait --for=condition=ApplicationsReconciled --namespace dpf-operator-system  dpuservices doca-blueman-service doca-telemetry-service
## Ensure the DPUServiceIPAMs have been reconciled
kubectl wait --for=condition=DPUIPAMObjectReconciled --namespace dpf-operator-system dpuserviceipam --all
## Ensure the DPUServiceInterfaces have been reconciled
kubectl wait --for=condition=ServiceInterfaceSetReconciled --namespace dpf-operator-system dpuserviceinterface --all
## Ensure the DPUServiceChains have been reconciled
kubectl wait --for=condition=ServiceChainSetReconciled --namespace dpf-operator-system dpuservicechain --all
```

With DPUDeployment, verify the Service installation with:

```shell
## Ensure the DPUServices are created and have been reconciled.
kubectl wait --for=condition=ApplicationsReconciled --namespace dpf-operator-system  dpuservices ovn-ovn
```

### 6. Test traffic
#### Add worker nodes to the cluster
At this point workers should be added to the cluster. Each worker node should be configured in line with [the prerequisites](../prerequisites.md) and the specific [OVN Kubernetes prerequisites](#worker-nodes).

As workers are added to the cluster DPUs will be provisioned and DPUServices will begin to be spun up.

#### Deploy test pods 

```shell
kubectl apply -f manifests/06-test-traffic
```

OVN functionality can be tested by pinging between the pods and services deployed in the default namespace.

TODO: Add specific user commands to test traffic.

### 7. Deletion and clean up

For DPF deletion follows a specific order defined below. The OVN Kubernetes primary CNI can not be safely deleted from the cluster.

#### Delete the test pods
```shell
kubectl delete -f manifests/06-test-traffic --wait
```

#### Delete DPF CNI acceleration components
```shell
kubectl delete -f manifests/04-enable-accelerated-cni --wait
helm uninstall -n nvidia-network-operator network-operator --wait

## Run `helm install` with the original values to delete the OVN Kubernetes webhook.
## Note: Uninstalling OVN Kubernetes as primary CNI is not supported but this command must be run to remove the webhook and restore a functioning cluster.
envsubst < manifests/01-cni-installation/helm-values/ovn-kubernetes.yml | helm upgrade --install -n ovn-kubernetes ovn-kubernetes oci://ghcr.io/nvidia/ovn-kubernetes-chart --version $DPF_VERSION --values -
```

#### Delete the DPF Operator system and DPF Operator

First we have to delete some DPUServiceInterfaces. This is necessary because of a known issue during uninstallation.

```shell
kubectl delete -n dpf-operator-system dpuserviceinterface p0 ovn --wait
```

Then we can delete the config and system namespace.

```shell
kubectl delete -n dpf-operator-system dpfoperatorconfig dpfoperatorconfig --wait
helm uninstall -n dpf-operator-system dpf-operator --wait
```

#### Delete DPF Operator dependencies
```shell
helm uninstall -n local-path-provisioner local-path-provisioner --wait 
kubectl delete ns local-path-provisioner --wait 
helm uninstall -n cert-manager cert-manager --wait 
kubectl -n dpf-operator-system delete secret dpf-pull-secret --wait
kubectl delete pv bfb-pv
kubectl delete namespace dpf-operator-system dpu-cplane-tenant1 cert-manager nvidia-network-operator --wait
```

Note: there can be a race condition with deleting the underlying Kamaji cluster which runs the DPU cluster control plane in this guide. If that happens it may be necessary to remove finalizers manually from `DPUCluster` and `Datastore` objects.

## Limitations of DPF Setup

### Host network pod services

The Kubelet process on the Kubernetes nodes use the OOB interface IP address to register in Kubernetes. This means that the nodes have the OOB IP addresses as node IP addresses. This means that pods using host networking have the OOB IP address of the hosts as pod IP address. However, that interface is not accelerated. This means that any component using the addresses of the pods using host networking will not benefit from hardware acceleration and high-speed ports.

For example, this means that when creating a Kubernetes NodePort service selecting pods using host networking, even if the user uses the high-speed IP of the host, the traffic will not be accelerated. In order to solve this, it is possible to create dedicated endpointSlices that contain the host high-speed port IP addresses instead of OOB port IP addresses. This way, the entire path to the pods will be accelerated and benefit from high performances, if the user uses the high speed IP address of the host with the nodePort port. This requires the workload running on the pod with host networking to also listen on the high-speed port IP address.
