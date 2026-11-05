# Design Document: Storage Vendor Discovery and Information Fetching
## Data Foundation - Multicluster Orchestrator

---

## Document Information

**Version:** 1.0  
**Date:** October 2026

---

## 1. Executive Summary

The ODF Multicluster Orchestrator (MCO) needs to discover which storage vendors are deployed on managed clusters and fetch vendor-specific information to orchestrate disaster recovery. This document describes the discovery mechanism using RHACM primitives (ClusterClaim, ManagedClusterView) and the vendor-agnostic pattern that allows any storage vendor to integrate with MCO.

**Key components**:
- **ClusterClaim**: Managed clusters advertise storage vendor presence
- **ManagedClusterView**: Hub fetches vendor-specific ConfigMaps from managed clusters
- **Vendor Info ConfigMap**: Standardized ConfigMap format for vendor metadata
- **Hub-side cache**: ConfigMap synced to hub for efficient access

This enables MCO to:
- Automatically discover storage deployments without manual configuration
- Fetch version, topology, and connection details for each vendor
- Support multiple vendors with a common discovery pattern
- Handle day-2 updates (version upgrades, topology changes)

---

## 2. Problem Statement

### 2.1 The Challenge

MCO orchestrates disaster recovery between storage clusters on different managed clusters. To do this, it needs to know:

1. **Which clusters have storage**: Not all managed clusters run storage
2. **Which vendor**: ODF, IBM Storage Scale, or future vendors
3. **Version information**: For compatibility validation
4. **Connection details**: Endpoints, cluster IDs, certificates
5. **Replication capabilities**: Which pools/volumes support mirroring

This information is vendor-specific and lives on the managed clusters, but MCO runs on the hub cluster.

### 2.2 Requirements

**Discovery must be**:
- **Automatic**: No manual registration or configuration
- **Vendor-agnostic**: Common pattern for all vendors
- **Dynamic**: Reflect changes (upgrades, topology updates)
- **Efficient**: Minimize cross-cluster queries
- **Secure**: No exposure of sensitive data

**Pattern must support**:
- Multiple storage vendors
- Multiple storage deployments per cluster (edge case)
- Day-2 updates without MCO changes
- Vendor-specific metadata formats

---

## 3. Discovery Architecture

### 3.1 Overall Flow

```
┌─────────────────────────────────────────────────────────────┐
│                    Managed Cluster                           │
│                                                               │
│  Step 1: Storage Vendor Deployment                           │
│  ┌─────────────────────────────────────────────────────┐    │
│  │  ODF Operator / Vendor Operator                      │    │
│  │  - Deploys storage system                            │    │
│  │  - Creates vendor-specific CRs                       │    │
│  └─────────────────────────────────────────────────────┘    │
│                           │                                   │
│  Step 2: Advertise via ClusterClaim                          │
│                           ▼                                   │
│  ┌─────────────────────────────────────────────────────┐    │
│  │  ClusterClaim                                         │    │
│  │  name: <vendor-claim-key>                            │    │
│  │  value: <namespace>/<configmap-name>                 │    │
│  └─────────────────────────────────────────────────────┘    │
│                           │                                   │
│  Step 3: Vendor Info ConfigMap                               │
│                           ▼                                   │
│  ┌─────────────────────────────────────────────────────┐    │
│  │  ConfigMap: <vendor-info-configmap>                  │    │
│  │  namespace: <storage-namespace>                      │    │
│  │  data:                                                │    │
│  │    <key1>: <vendor-specific-json>                    │    │
│  │    <key2>: <vendor-specific-json>                    │    │
│  └─────────────────────────────────────────────────────┘    │
└───────────────────────────┬──────────────────────────────────┘
                            │
                            │ ClusterClaim synced by RHACM
                            │
                            ▼
┌─────────────────────────────────────────────────────────────┐
│                         Hub Cluster                          │
│                                                               │
│  Step 4: Hub Watches ManagedCluster                          │
│  ┌─────────────────────────────────────────────────────┐    │
│  │  ManagedCluster Controller                           │    │
│  │  - Watches ManagedCluster CRs                        │    │
│  │  - Detects ClusterClaims                             │    │
│  │  - Creates ManagedClusterView                        │    │
│  └───────────────────────┬─────────────────────────────┘    │
│                           │                                   │
│  Step 5: Fetch ConfigMap via MCV                             │
│                           ▼                                   │
│  ┌─────────────────────────────────────────────────────┐    │
│  │  ManagedClusterView                                  │    │
│  │  spec:                                                │    │
│  │    scope:                                             │    │
│  │      name: <configmap-name>                          │    │
│  │      namespace: <namespace>                          │    │
│  │      kind: ConfigMap                                 │    │
│  │  status:                                              │    │
│  │    result: <configmap-content>                       │    │
│  └───────────────────────┬─────────────────────────────┘    │
│                           │                                   │
│  Step 6: Cache on Hub                                        │
│                           ▼                                   │
│  ┌─────────────────────────────────────────────────────┐    │
│  │  ConfigMap: <vendor>-client-info                     │    │
│  │  namespace: <mco-namespace>                          │    │
│  │  data:                                                │    │
│  │    <cluster1-key>: <vendor-json>                     │    │
│  │    <cluster2-key>: <vendor-json>                     │    │
│  └─────────────────────────────────────────────────────┘    │
│                           │                                   │
│  Step 7: MirrorPeer Uses Cache                               │
│                           ▼                                   │
│  ┌─────────────────────────────────────────────────────┐    │
│  │  MirrorPeer Controller / ODF Vendor                  │    │
│  │  - Reads cached ConfigMap                            │    │
│  │  - Validates compatibility                           │    │
│  │  - Configures DR                                     │    │
│  └─────────────────────────────────────────────────────┘    │
└─────────────────────────────────────────────────────────────┘
```

---

## 4. Component Details

### 4.1 ClusterClaim (Step 2)

**What is a ClusterClaim?**

A `ClusterClaim` is an RHACM CR (API group `cluster.open-cluster-management.io/v1alpha1`) that allows managed clusters to advertise capabilities or metadata to the hub.

**Purpose in MCO**: Storage vendors use ClusterClaims to advertise their presence on a managed cluster.

**ClusterClaim Format**:
```yaml
apiVersion: cluster.open-cluster-management.io/v1alpha1
kind: ClusterClaim
metadata:
  name: <vendor-specific-claim-name>
spec:
  value: "<namespace>/<configmap-name>"
```

**Key fields**:
- `metadata.name`: Unique claim name identifying the vendor
- `spec.value`: Pointer to the vendor info ConfigMap (format: `namespace/name`)

**RHACM behavior**:
- ClusterClaims are automatically synced to the hub
- Appear in `ManagedCluster.status.clusterClaims[]`

### 4.2 ODF ClusterClaim Example

```yaml
apiVersion: cluster.open-cluster-management.io/v1alpha1
kind: ClusterClaim
metadata:
  name: odf.openshift.io
spec:
  value: "openshift-storage/odf-info"
```

**Value interpretation**:
- Namespace: `openshift-storage`
- Name : `odf-info`

### 4.3 IBM Storage Scale ClusterClaim Example

```yaml
apiVersion: cluster.open-cluster-management.io/v1alpha1
kind: ClusterClaim
metadata:
  name: scale.spectrum.ibm.com
spec:
  value: "ibm-spectrum-scale/ibm-storage-scale-info"
```

**Value interpretation**:
- Namespace: `ibm-spectrum-scale`
- ConfigMap name: `ibm-storage-scale-info`

### 4.4 Vendor Info ConfigMap (Step 3)

**Purpose**: Contains vendor-specific metadata in a structured format (JSON).

**Created by**: Storage vendor's operator on the managed cluster

**Updated**: Whenever deployment changes (version upgrade, topology change, new pools)

**Format**:
```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: <vendor-info-configmap>
  namespace: <storage-namespace>
data:
  # Key: Unique identifier for this storage instance
  # Value: JSON-encoded vendor-specific metadata
  <storage-instance-key>: |
    {
      "clusterId": "...",
      "version": "...",
      "deploymentType": "...",
      ...vendor-specific fields...
    }
```

**Key structure**: 
- For ODF: `<cluster-name>_<storagecluster-name>`
- For IBM Scale: `<scale-cluster-name>`
- Vendor-defined, must be unique per storage instance

**Value structure**: Vendor-specific JSON. No enforced schema, but common patterns emerge.

---

## 5. Vendor-Specific Implementations

### 5.1 ODF Discovery

#### 5.1.1 ClusterClaim

Created by ODF operator when StorageCluster is deployed:

```yaml
apiVersion: cluster.open-cluster-management.io/v1alpha1
kind: ClusterClaim
metadata:
  name: odf.openshift.io
spec:
  value: "openshift-storage/odf-info"
```

#### 5.1.2 ConfigMap: ocs-client-info

**Location**: `openshift-storage/ocs-client-info`

**Data keys**: One per storage cluster

**Converged mode example**:
```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: ocs-client-info
  namespace: openshift-storage
data:
  cluster1_ocs-storagecluster: |
    {
      "clusterId": "abc-123-def",
      "name": "ocs-storagecluster",
      "clientId": "client-xyz",
      "clientManagedClusterName": "cluster1",
      "version": "4.17.0",
      "deploymentType": "internal",
      "cephClusterFSID": "a1b2c3d4-...",
      "infoCephBlockPools": [
        {
          "name": "ocs-storagecluster-cephblockpool",
          "mirrorEnabled": true
        }
      ]
    }
```

**Provider mode example** (includes ProviderInfo):
```yaml
data:
  cluster1_ocs-storageclient: |
    {
      "clusterId": "client-abc",
      "name": "ocs-storageclient",
      "clientId": "client-123",
      "clientManagedClusterName": "cluster1",
      "providerInfo": {
        "version": "4.17.0",
        "deploymentType": "internal",
        "providerManagedClusterName": "provider1",
        "namespacedName": {
          "name": "ocs-storagecluster",
          "namespace": "openshift-storage"
        },
        "cephClusterFSID": "provider-fsid-123",
        "storageProviderEndpoint": "https://provider-endpoint",
        "tlsInfo": {
          "serverSAN": "provider.example.com",
          "clientSAN": "client.example.com",
          "clientCertName": "client-cert",
          "serverCmContent": "<base64-ca>",
          "clientCmContent": "<base64-ca>"
        },
        "infoCephBlockPools": [...]
      }
    }
```

### 5.2 IBM Storage Scale Discovery

#### 5.2.1 ClusterClaim

```yaml
apiVersion: cluster.open-cluster-management.io/v1alpha1
kind: ClusterClaim
metadata:
  name: scale.spectrum.ibm.com
spec:
  value: "ibm-spectrum-scale/ibm-storage-scale-info"
```

#### 5.2.2 ConfigMap: ibm-storage-scale-info

**Location**: `ibm-spectrum-scale/ibm-storage-scale-info`

**Format**:
```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: ibm-storage-scale-info
  namespace: ibm-spectrum-scale
data:
  ibm-storage-scale-cluster.config.yaml: |
    version: "5.2.0.1"
    operator-namespace: ibm-spectrum-scale
    cluster:
      name: scale-cluster-1
    sourceManifests:
      - filesystem-manifest
      - storageclass-manifest
```

---

## 6. Hub-Side Discovery

### 6.1 ManagedCluster Controller

MCO runs a controller that watches `ManagedCluster` CRs to detect storage vendor deployments.

**Watches**:
- `ManagedCluster` CRs (cluster.open-cluster-management.io/v1)

**Reconcile logic**:

```go
func (r *ManagedClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    managedCluster := &clusterv1.ManagedCluster{}
    if err := r.Get(ctx, req.NamespacedName, managedCluster); err != nil {
        return ctrl.Result{}, client.IgnoreNotFound(err)
    }
    
    // Check for ODF ClusterClaim
    if claim := findClusterClaim(managedCluster, "odf.openshift.io"); claim != nil {
        // Parse claim value to get namespace/configmap
        namespace, configMapRef := parseClaimValue(claim.Value)
        
        // Create ManagedClusterView to fetch ConfigMap
        if err := r.createOrUpdateManagedClusterView(ctx, managedCluster.Name, namespace, configMapRef); err != nil {
            return ctrl.Result{}, err
        }
    }
    
    // Check for IBM Scale ClusterClaim
    if claim := findClusterClaim(managedCluster, "scale.spectrum.ibm.com"); claim != nil {
        namespace, configMapRef := parseClaimValue(claim.Value)
        if err := r.createOrUpdateManagedClusterView(ctx, managedCluster.Name, namespace, configMapRef); err != nil {
            return ctrl.Result{}, err
        }
    }
    
    // Future: Check for other vendor ClusterClaims
    
    return ctrl.Result{}, nil
}

func findClusterClaim(mc *clusterv1.ManagedCluster, name string) *clusterv1.ManagedClusterClaim {
    for i := range mc.Status.ClusterClaims {
        if mc.Status.ClusterClaims[i].Name == name {
            return &mc.Status.ClusterClaims[i]
        }
    }
    return nil
}
```

### 6.2 ManagedClusterView Creation

When a ClusterClaim is detected, the controller creates a `ManagedClusterView` to fetch the vendor ConfigMap:

```yaml
apiVersion: view.open-cluster-management.io/v1beta1
kind: ManagedClusterView
metadata:
  name: <vendor>-info-<cluster-name>
  namespace: <cluster-name>  # ManagedCluster namespace
  labels:
    app: odf-multicluster-orchestrator
    vendor: <vendor-name>
spec:
  scope:
    apiGroup: ""
    kind: ConfigMap
    name: <configmap-name>
    namespace: <storage-namespace>
    resource: configmaps
```

**Example for ODF**:
```yaml
apiVersion: view.open-cluster-management.io/v1beta1
kind: ManagedClusterView
metadata:
  name: odf-info-cluster1
  namespace: cluster1
spec:
  scope:
    kind: ConfigMap
    name: ocs-client-info
    namespace: openshift-storage
```

**RHACM behavior**:
- RHACM fetches the ConfigMap from the managed cluster
- Populates `ManagedClusterView.status.result` with ConfigMap content
- Updates whenever ConfigMap changes on managed cluster

### 6.3 Hub ConfigMap Cache

The MCO controller watches `ManagedClusterView` CRs and aggregates vendor info into a hub-side ConfigMap:

**Purpose**: Consolidate info from all managed clusters for efficient access

**ConfigMap per vendor**:
- ODF: `ocs-client-info` in MCO namespace
- IBM Scale: `scale-client-info` in MCO namespace

**Example aggregated ConfigMap**:
```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: ocs-client-info
  namespace: openshift-odf-multicluster-orchestrator
data:
  # Data from cluster1
  cluster1_ocs-storagecluster: |
    {
      "clusterId": "...",
      "clientManagedClusterName": "cluster1",
      ...
    }
  
  # Data from cluster2
  cluster2_ocs-storagecluster: |
    {
      "clusterId": "...",
      "clientManagedClusterName": "cluster2",
      ...
    }
  
  # Data from cluster3 (provider mode)
  cluster3_ocs-storageclient: |
    {
      "clusterId": "...",
      "clientManagedClusterName": "cluster3",
      "providerInfo": {...},
      ...
    }
```

**Update trigger**: When any `ManagedClusterView` for that vendor updates

---

## 7. MirrorPeer Usage of Cached Info

### 7.1 ODF Vendor Initialization

When the MirrorPeer controller creates an ODF vendor instance:

```go
func NewODFVendor(ctx context.Context, cl client.Client, scheme *runtime.Scheme, logger *slog.Logger, currentNamespace string) (*ODFVendor, error) {
    // Fetch the aggregated ConfigMap from hub
    clientInfoConfigMap, err := GetClientInfoConfigMap(ctx, cl, currentNamespace)
    if err != nil {
        return nil, err
    }
    
    return &ODFVendor{
        client:           cl,
        scheme:           scheme,
        logger:           logger.With("storageVendor", "odf"),
        currentNamespace: currentNamespace,
        infoMap:          clientInfoConfigMap.Data,  // Cached vendor info
    }, nil
}

func GetClientInfoConfigMap(ctx context.Context, c client.Client, currentNamespace string) (*corev1.ConfigMap, error) {
    configMap := &corev1.ConfigMap{}
    err := c.Get(ctx, types.NamespacedName{
        Name:      "ocs-client-info",
        Namespace: currentNamespace,
    }, configMap)
    return configMap, err
}
```

### 7.2 Accessing Cluster Info

The vendor uses the cached info map throughout reconciliation:

```go
func (odf *ODFVendor) ValidateVersion(ctx context.Context, mirrorPeer *multiclusterv1alpha1.MirrorPeer) (ctrl.Result, error) {
    for _, item := range mirrorPeer.Spec.Items {
        // Build key for this cluster's storage
        key := utils.GetKey(item.ClusterName, item.StorageClusterRef.Name)
        // e.g., "cluster1_ocs-storagecluster"
        
        // Get cached info
        clientInfo, err := GetClientInfoFromConfigMap(odf.infoMap, key)
        if err != nil {
            return ctrl.Result{}, fmt.Errorf("cluster %s storage %s not found: %w", 
                item.ClusterName, item.StorageClusterRef.Name, err)
        }
        
        // Validate version
        if !isVersionCompatible(clientInfo.Version, requiredVersion) {
            return ctrl.Result{}, fmt.Errorf("incompatible version: %s", clientInfo.Version)
        }
    }
    return ctrl.Result{}, nil
}

func GetClientInfoFromConfigMap(clientInfoMap map[string]string, key string) (ClientInfo, error) {
    clientInfoJSON, ok := clientInfoMap[key]
    if !ok {
        return ClientInfo{}, fmt.Errorf("client info for %s not found in ConfigMap", key)
    }
    
    var clientInfo ClientInfo
    if err := json.Unmarshal([]byte(clientInfoJSON), &clientInfo); err != nil {
        return ClientInfo{}, fmt.Errorf("failed to unmarshal client info for %s: %v", key, err)
    }
    
    return clientInfo, nil
}
```

**Benefits**:
- Single fetch at vendor init (efficient)
- No per-reconcile ManagedClusterView queries
- In-memory access to all cluster info
- ConfigMap update triggers vendor re-init (if needed)

---

## 9. Adding a New Storage Vendor

### 9.1 Vendor Integration Steps

To integrate a new storage vendor with MCO:

**Step 1**: Define ClusterClaim name
```yaml
# Example: NetApp vendor
apiVersion: cluster.open-cluster-management.io/v1alpha1
kind: ClusterClaim
metadata:
  name: flash.storage.io
spec:
  value: "flash-system/flash-cluster-info"
```

**Step 2**: Create vendor info ConfigMap format
```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: flash-cluster-info
  namespace: flash-system
data:
  <cluster-key>: |
    {
      "clusterId": "...",
      "version": "...",
      "replicationEndpoint": "...",
      ...vendor-specific fields...
    }
```

**Step 3**: Update vendor's operator to populate ConfigMap
- Watch vendor CRs
- Query storage system for metadata
- Update ConfigMap on changes

**Step 4**: Register claim in MCO ManagedCluster controller
```go
// In managedcluster controller
if claim := findClusterClaim(managedCluster, "flash.storage.io"); claim != nil {
    namespace, configMapRef := parseClaimValue(claim.Value)
    if err := r.createOrUpdateManagedClusterView(ctx, managedCluster.Name, namespace, configMapRef); err != nil {
        return ctrl.Result{}, err
    }
}
```

**Step 5**: Create hub cache ConfigMap
```go
// Aggregate netapp info from all clusters
func (r *ManagedClusterViewController) syncNetAppInfo(ctx context.Context) error {
    // Similar to ODF cache creation
    // ConfigMap: flash-client-info in MCO namespace
}
```

**Step 6**: Implement StorageVendor interface
```go
type FlashAppVendor struct {
    infoMap map[string]string  // Cached vendor info
}

func NewFlashVendor(ctx context.Context, cl client.Client, ...) (*FlashVendor, error) {
    configMap, err := GetClientInfoConfigMap(ctx, cl, currentNamespace, "flash-client-info")
    if err != nil {
        return nil, err
    }
    return &FlashVendor{infoMap: configMap.Data}, nil
}

func (v *FlashVendor) ValidateVersion(ctx, mirrorPeer) (ctrl.Result, error) {
    // Read from v.infoMap
}
```

**Step 7**: Register in vendor factory
```go
case multiclusterv1alpha1.NetApp:
    return netapp.NewFlashVendor(ctx, cl, scheme, logger, currentNamespace)
```

---

## 10. Security Considerations

### 10.1 ConfigMap Data Sensitivity

**What's in vendor ConfigMaps**:
- Cluster IDs, versions (not sensitive)
- Endpoints (semi-sensitive, network topology)
- **TLS certificates** (sensitive - CA certs, SANs)
- **No secrets/passwords** (must use separate Secrets)

**Best practices**:
- **Never put credentials** in ConfigMaps (use Secrets)
- **Certificates**: Public certs OK in ConfigMaps, private keys in Secrets

---

## 11. Related Resources

- **General MirrorPeer**: `design/mirrorpeer-overview.md`
- **Manifest Sync (IBM Scale)**: `design/manifest-sync.md`
- **RHACM ManagedClusterView**: https://open-cluster-management.io/concepts/managedclusterview/
- **RHACM ClusterClaim**: https://open-cluster-management.io/concepts/clusterclaim/
