# Design Document: MirrorPeer Architecture
## Data Foundation - Multicluster Orchestrator

---

## Document Information

**Version:** 1.0  
**Date:** October 2026

---

## 1. Executive Summary

The MirrorPeer is the foundational custom resource (CR) for orchestrating disaster recovery (DR) across two storage clusters in a Red Hat Advanced Cluster Management (RHACM) multi-cluster environment. It provides a vendor-agnostic abstraction for establishing and managing replication relationships between storage systems deployed on different managed clusters.

MirrorPeer supports two DR modes:
- **Async**: Asynchronous replication for geographically distributed sites (Regional DR)
- **Sync**: Synchronous replication for metropolitan area sites (Metro DR)

The MirrorPeer controller delegates vendor-specific implementation to pluggable StorageVendor interfaces, allowing support for multiple storage platforms while providing a consistent user experience.

---

## 2. Problem Statement

### 2.1 The Challenge

Organizations deploying stateful workloads across multiple OpenShift clusters need disaster recovery capabilities to protect against site failures. Setting up DR between storage systems involves coordinating numerous resources across multiple clusters:

1. **Storage cluster pairing**: Establishing connectivity and trust
2. **Replication configuration**: Enabling data mirroring (block, file, object)
3. **Credential management**: Distributing secrets for cross-cluster access
4. **DR orchestration setup**: Configuring DR policies and cluster metadata
5. **Addon lifecycle**: Managing cluster agents and operators

Manually orchestrating these components is:
- **Error-prone**: Many moving parts across multiple clusters
- **Time-consuming**: Requires deep knowledge of each storage platform
- **Not scalable**: Day-2 operations (upgrades, credential rotation) need automation
- **Vendor-specific**: Each storage platform has different APIs and requirements

### 2.2 The Solution: MirrorPeer

MirrorPeer provides a single declarative API that:
- **Abstracts complexity**: Users declare desired peer relationship; controllers handle details
- **Ensures consistency**: Both peer clusters are configured identically
- **Supports multiple modes**: Both synchronous and asynchronous replication
- **Enables vendor plugins**: Storage vendors implement the StorageVendor interface
- **Automates day-2 ops**: Controllers handle updates, rotation, and cleanup

---

## 3. Architecture Overview

### 3.1 System Components

```
┌─────────────────────────────────────────────────────────────────┐
│                       Hub Cluster (RHACM)                        │
│                                                                   │
│  ┌────────────────────────────────────────────────────────────┐ │
│  │  MirrorPeer Controller                                      │ │
│  │  ┌──────────────────────────────────────────────────────┐  │ │
│  │  │  Reconciliation Loop                                  │  │ │
│  │  │  1. Validate MirrorPeer spec                         │  │ │
│  │  │  2. Instantiate StorageVendor implementation         │  │ │
│  │  │  3. Delegate to vendor methods                       │  │ │
│  │  │  4. Update MirrorPeer status                         │  │ │
│  │  └──────────────────────────────────────────────────────┘  │ │
│  └────────────────────────────────────────────────────────────┘ │
│                           │                                       │
│                           │ delegates to                          │
│                           ▼                                       │
│  ┌────────────────────────────────────────────────────────────┐ │
│  │  StorageVendor Interface                                    │ │
│  │                                                              │ │
│  │  ┌──────────────┐  ┌──────────────┐  ┌─────────────────┐  │ │
│  │  │ ODF Vendor   │  │ Vendor B     │  │ Vendor C        │  │ │
│  │  │              │  │              │  │                 │  │ │
│  │  │ - Validate   │  │ - Validate   │  │ - Validate      │  │ │
│  │  │ - Configure  │  │ - Configure  │  │ - Configure     │  │ │
│  │  │ - Cleanup    │  │ - Cleanup    │  │ - Cleanup       │  │ │
│  │  └──────────────┘  └──────────────┘  └─────────────────┘  │ │
│  └────────────────────────────────────────────────────────────┘ │
│                                                                   │
│                    Uses RHACM Primitives:                         │
│         ManifestWork | ManagedClusterView | ManagedClusterAddOn  │
└─────────────────────────────────────────────────────────────────┘
                              │
                              │ OCM Distribution
                              │
        ┌─────────────────────┴─────────────────────┐
        │                                             │
┌───────▼────────────┐                    ┌──────────▼───────────┐
│  Managed Cluster 1 │                    │  Managed Cluster 2   │
│                    │                    │                      │
│  Storage System    │◄──────────────────►│  Storage System      │
│  (Vendor-specific) │   Replication      │  (Vendor-specific)   │
│                    │                    │                      │
└────────────────────┘                    └──────────────────────┘
```

### 3.2 Control Flow

1. **User creates MirrorPeer** on hub cluster
2. **Controller validates** spec (clusters exist, vendor supported)
3. **StorageVendor instantiation**: Factory creates vendor implementation
4. **Validation phase**: `vendor.ValidateVersion()` checks compatibility
5. **Configuration phase**: `vendor.ConfigureStorageForDR()` sets up replication
6. **Status updates**: Controller reflects vendor-reported status in MirrorPeer
7. **Deletion handling**: `vendor.CleanupResources()` cleans up when MirrorPeer deleted

---

## 4. API Design

### 4.1 MirrorPeer Custom Resource

**API Group**: `multicluster.odf.openshift.io/v1alpha1`  
**Kind**: `MirrorPeer`  
**Scope**: Cluster-scoped

```yaml
apiVersion: multicluster.odf.openshift.io/v1alpha1
kind: MirrorPeer
metadata:
  name: example-mirrorpeer
spec:
  # Which storage vendor (odf, vendor-b, etc.)
  storageVendor: odf
  
  # DR type: async or sync
  type: async
  
  # Exactly two peer references
  items:
    - clusterName: cluster-1
      storageClusterRef:
        name: storage-system-1
        namespace: storage-namespace
    
    - clusterName: cluster-2
      storageClusterRef:
        name: storage-system-2
        namespace: storage-namespace

status:
  phase: Ready
  message: "Setup is completed"
  conditions:
    - type: Validated
      status: "True"
      reason: MirrorPeerValid
      message: "MirrorPeer is valid"
    
    - type: Configured
      status: "True"
      reason: Configured
      message: "MirrorPeer configuration done"
```

### 4.2 Spec Fields

#### `.spec.storageVendor`
- **Type**: `VendorType` (string enum)
- **Required**: No (defaults to `odf`)
- **Immutable**: Yes
- **Description**: Identifies which StorageVendor implementation to use
- **Validation**: Must be a registered vendor type

#### `.spec.type`
- **Type**: `DRType` (string enum)
- **Values**: `async` | `sync`
- **Default**: `async`
- **Immutable**: Yes
- **Description**: 
  - `async`: Asynchronous replication (higher RPO, suitable for WAN)
  - `sync`: Synchronous replication (near-zero RPO, requires low latency)

#### `.spec.items`
- **Type**: `[]PeerRef`
- **MinItems**: 2
- **MaxItems**: 2
- **Description**: References to the two storage systems to peer
- **Immutability**: `clusterName` and `storageClusterRef.name` are immutable after creation

**PeerRef Structure**:
```go
type PeerRef struct {
    // ClusterName: Name of the ManagedCluster in RHACM
    ClusterName string `json:"clusterName"`
    
    // StorageClusterRef: Reference to the storage system on that cluster
    // Interpretation is vendor-specific (StorageCluster, StorageClient, etc.)
    StorageClusterRef StorageClusterRef `json:"storageClusterRef"`
}

type StorageClusterRef struct {
    // Name of the storage system CR
    Name string `json:"name"`
    
    // Namespace where the storage system exists
    Namespace string `json:"namespace,omitempty"`
}
```

### 4.3 Status Fields

#### `.status.phase`
- **Type**: `PhaseType` (string enum)
- **Values**: 
  - `Initializing`: Initial creation, vendor instantiation
  - `Configuring`: Vendor configuration in progress
  - `Ready`: All configuration complete, DR active
  - `Failed`: Validation or configuration error
  - `Deleting`: Finalizer cleanup in progress

#### `.status.message`
- **Type**: `PhaseMessage` (string)
- **Description**: Human-readable message about current state

#### `.status.conditions`
Standard Kubernetes conditions:

| Type | Description |
|------|-------------|
| `Validated` | Whether the MirrorPeer spec passed validation |
| `Configured` | Whether all DR configuration is complete |
| `Deleted` | Whether deletion cleanup is in progress |

Each condition includes:
- `status`: `True` | `False` | `Unknown`
- `reason`: Machine-readable reason code
- `message`: Human-readable details
- `lastTransitionTime`: When status last changed

---

## 5. StorageVendor Interface

### 5.1 Interface Definition

Storage vendors implement this interface to provide DR orchestration:

```go
package storagevendor

import (
    "context"
    multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
    ctrl "sigs.k8s.io/controller-runtime"
)

// StorageVendor defines the interface for vendor-specific DR implementations
type StorageVendor interface {
    // ValidateVersion performs vendor-specific version compatibility checks
    // Returns error if clusters are incompatible for DR
    ValidateVersion(ctx context.Context, mirrorPeer *multiclusterv1alpha1.MirrorPeer) (ctrl.Result, error)
    
    // ConfigureStorageForDR sets up all DR resources for the vendor
    // May return Requeue result if waiting for async operations
    ConfigureStorageForDR(ctx context.Context, mirrorPeer *multiclusterv1alpha1.MirrorPeer) (ctrl.Result, error)
    
    // CleanupResources removes all DR configuration when MirrorPeer is deleted
    // Called by finalizer before allowing deletion
    CleanupResources(ctx context.Context, mirrorPeer *multiclusterv1alpha1.MirrorPeer) (ctrl.Result, error)
}
```

### 5.2 Vendor Registration

Vendors register via the factory pattern:

```go
// pkg/storagevendor/factory.go
func New(
    ctx context.Context,
    client client.Client,
    scheme *runtime.Scheme,
    logger *slog.Logger,
    currentNamespace string,
    vendorType multiclusterv1alpha1.VendorType,
) (StorageVendor, error) {
    switch vendorType {
    case multiclusterv1alpha1.ODF:
        return odf.NewODFVendor(ctx, client, scheme, logger, currentNamespace)
    // Future vendors register here
    default:
        return nil, fmt.Errorf("unsupported storage vendor: %s", vendorType)
    }
}
```

### 5.3 Vendor Responsibilities

Each StorageVendor implementation must:

1. **ValidateVersion**:
   - Query managed clusters for storage system version/configuration
   - Check compatibility between peers
   - Verify storage system exists and is healthy
   - Return error with descriptive message if invalid

2. **ConfigureStorageForDR**:
   - Create vendor-specific resources (via ManifestWork)
   - Deploy any required addons (via ManagedClusterAddOn)
   - Configure replication (block, file, object as applicable)
   - Monitor configuration progress
   - Update MirrorPeer status conditions
   - Return Requeue result if waiting for async operations

3. **CleanupResources**:
   - Delete addons gracefully
   - Remove replication configuration
   - Clean up vendor-specific resources
   - Ensure no orphaned resources remain

### 5.4 Vendor Guidelines

**Discovery**: Use ManagedClusterView to read vendor-specific ConfigMaps/CRs from managed clusters

**Distribution**: Use ManifestWork to create resources on managed clusters

**Ownership**: Set OwnerReferences on created resources pointing to MirrorPeer for cascading delete

**Status**: Update MirrorPeer conditions with vendor-specific progress/errors

**Idempotency**: Methods called repeatedly; must be idempotent

**Error Handling**: Return errors with actionable messages for users

---

## 6. Controller Reconciliation

### 6.1 Reconciliation Loop

```go
func (r *MirrorPeerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    // 1. Fetch MirrorPeer
    mirrorPeer := &multiclusterv1alpha1.MirrorPeer{}
    if err := r.Get(ctx, req.NamespacedName, mirrorPeer); err != nil {
        return ctrl.Result{}, client.IgnoreNotFound(err)
    }
    
    // 2. Reconcile phases (validation, configuration, deletion)
    result, reconcileErr := r.reconcilePhases(ctx, logger, mirrorPeer)
    
    // 3. Update status
    statusErr := r.Client.Status().Update(ctx, mirrorPeer)
    
    // 4. Return result
    if reconcileErr != nil {
        return ctrl.Result{}, reconcileErr
    }
    return result, statusErr
}
```

### 6.2 Phase: Initialization

**Sets**: `phase: Initializing`

1. Check for deletion timestamp
   - If deleting: run finalizer cleanup
2. Add finalizer if not present
3. Instantiate StorageVendor based on `.spec.storageVendor`
4. If vendor instantiation fails: `phase: Failed`

### 6.3 Phase: Validation

**Condition**: `Validated`

1. Call `vendor.ValidateVersion(ctx, mirrorPeer)`
2. If error:
   - Set `phase: Failed`
   - Set condition `Validated: False` with error details
   - Return error (controller will requeue)
3. If success:
   - Set condition `Validated: True`
   - Proceed to configuration

### 6.4 Phase: Configuration

**Sets**: `phase: Configuring`  
**Condition**: `Configured`

1. Call `vendor.ConfigureStorageForDR(ctx, mirrorPeer)`
2. Vendor updates conditions during progress:
   - Vendor-specific sub-conditions (e.g., `PeeringInProgress`, `S3ConfigurationInProgress`)
3. If error:
   - Set `Configured: False` with error details
   - Return error
4. If requeue needed (async operations pending):
   - Return `Result{RequeueAfter: duration}`
5. If complete:
   - Set condition `Configured: True`
   - Set `phase: Ready`

### 6.5 Phase: Ready

**Sets**: `phase: Ready`

1. All configuration complete
2. Vendor continues to watch for drift and correct
3. Reconcile triggered by:
   - Managed cluster changes (via ManagedClusterView updates)
   - Addon status changes
   - User updates to MirrorPeer

### 6.6 Phase: Deletion

**Sets**: `phase: Deleting`  
**Condition**: `Deleted: True`

Finalizer: `hub.multicluster.odf.openshift.io`

1. Deletion timestamp detected
2. Call `vendor.CleanupResources(ctx, mirrorPeer)`
3. If error:
   - Set `Deleted: False` with error details
   - Return error (retry)
4. If success:
   - Remove finalizer
   - MirrorPeer is deleted by Kubernetes

---

## 7. RHACM Integration

### 7.1 ManagedCluster Discovery

MirrorPeer references managed clusters by name:

```yaml
spec:
  items:
    - clusterName: prod-east  # Must be a ManagedCluster name
```

The controller validates that each `clusterName` corresponds to an existing `ManagedCluster` CR.

### 7.2 ManifestWork for Resource Distribution

Vendors use ManifestWork to create resources on managed clusters:

```yaml
apiVersion: work.open-cluster-management.io/v1
kind: ManifestWork
metadata:
  name: mirrorpeer-<resource-type>-<hash>
  namespace: prod-east  # ManagedCluster namespace
  ownerReferences:
    - apiVersion: multicluster.odf.openshift.io/v1alpha1
      kind: MirrorPeer
      name: example-mirrorpeer
      controller: true
spec:
  workload:
    manifests:
      - apiVersion: v1
        kind: Secret
        metadata:
          name: peer-credentials
          namespace: storage-namespace
        data:
          token: <base64>
```

**Best practices**:
- Set OwnerReference to MirrorPeer for cascading delete
- Use deterministic naming for idempotency
- Include namespace in manifest (don't rely on default)

### 7.3 ManagedClusterView for Reading State

Vendors use ManagedClusterView to fetch resources from managed clusters:

```yaml
apiVersion: view.open-cluster-management.io/v1beta1
kind: ManagedClusterView
metadata:
  name: mirrorpeer-storage-info-<cluster>
  namespace: prod-east
  ownerReferences:
    - apiVersion: multicluster.odf.openshift.io/v1alpha1
      kind: MirrorPeer
      name: example-mirrorpeer
spec:
  scope:
    apiGroup: v1
    kind: ConfigMap
    name: storage-info
    namespace: storage-namespace
```

The `.status.result` contains the fetched resource (synced by RHACM).

### 7.4 ManagedClusterAddOn for Agents

Vendors can deploy addons (agents running on managed clusters):

```yaml
apiVersion: addon.open-cluster-management.io/v1alpha1
kind: ManagedClusterAddOn
metadata:
  name: vendor-dr-agent
  namespace: prod-east
  ownerReferences:
    - apiVersion: multicluster.odf.openshift.io/v1alpha1
      kind: MirrorPeer
      name: example-mirrorpeer
spec:
  installNamespace: storage-namespace
```

Addons can:
- Watch local resources and sync state to hub
- Distribute secrets/config between peers
- Perform agent-specific reconciliation

---

## 8. Lifecycle Management

### 8.1 Creation Flow

```
User creates MirrorPeer
         │
         ▼
Controller fetches MirrorPeer
         │
         ▼
Add finalizer
         │
         ▼
Instantiate StorageVendor
         │
         ▼
ValidateVersion()
    │           │
    │ (error)   │ (success)
    ▼           ▼
Set Failed    Set Validated: True
              │
              ▼
         ConfigureStorageForDR()
              │
              ├─── (error) ──► Set Configured: False
              │
              ├─── (requeue) ─► Wait and retry
              │
              └─── (success) ─► Set Ready
```

### 8.2 Update Handling

MirrorPeer immutability:
- `spec.storageVendor` is immutable
- `spec.type` is immutable
- `spec.items[*].clusterName` is immutable
- `spec.items[*].storageClusterRef.name` is immutable

Updates to mutable fields trigger reconciliation; vendor decides how to handle.

### 8.3 Deletion Flow

```
User deletes MirrorPeer
         │
         ▼
Deletion timestamp set
         │
         ▼
Finalizer processing starts
         │
         ▼
CleanupResources()
    │           │
    │ (error)   │ (success)
    ▼           ▼
Requeue      Remove finalizer
             │
             ▼
        MirrorPeer deleted
```

---

## 9. Status and Observability

### 9.1 Checking MirrorPeer Status

```bash
kubectl get mirrorpeer
```

Output:
```
NAME                TYPE    PHASE   AGE
example-mirrorpeer  async   Ready   10m
```

### 9.2 Detailed Status

```bash
kubectl get mirrorpeer example-mirrorpeer -o yaml
```

Key status fields:
```yaml
status:
  phase: Ready
  message: "Setup is completed"
  conditions:
    - type: Validated
      status: "True"
      reason: MirrorPeerValid
      lastTransitionTime: "2026-10-26T10:00:00Z"
    - type: Configured
      status: "True"
      reason: Configured
      lastTransitionTime: "2026-10-26T10:05:00Z"
```

### 9.3 Troubleshooting Failed MirrorPeer

**Symptom**: `phase: Failed`

**Steps**:
1. Check conditions:
   ```bash
   kubectl get mirrorpeer example-mirrorpeer -o jsonpath='{.status.conditions}' | jq
   ```
2. Look for condition with `status: "False"`
3. Read `reason` and `message` for details
4. Check controller logs:
   ```bash
   kubectl logs -n <namespace> deployment/odf-multicluster-orchestrator-controller-manager
   ```

**Common issues**:
- `Validated: False` → Cluster not found, version incompatible, storage system missing
- `Configured: False` → Network issues, RBAC errors, resource creation failed

---

## 10. Vendor Implementation Guide

### 10.1 Implementing a New Vendor

To add a new storage vendor:

1. **Create vendor package**: `pkg/storagevendor/<vendor-name>/`

2. **Implement StorageVendor interface**:
   ```go
   type MyVendor struct {
       client client.Client
       scheme *runtime.Scheme
       logger *slog.Logger
   }
   
   func NewMyVendor(...) (*MyVendor, error) { ... }
   
   func (v *MyVendor) ValidateVersion(ctx, mirrorPeer) (ctrl.Result, error) { ... }
   func (v *MyVendor) ConfigureStorageForDR(ctx, mirrorPeer) (ctrl.Result, error) { ... }
   func (v *MyVendor) CleanupResources(ctx, mirrorPeer) (ctrl.Result, error) { ... }
   ```

3. **Register in factory**: Edit `pkg/storagevendor/factory.go`:
   ```go
   case "my-vendor":
       return myvendor.NewMyVendor(ctx, cl, scheme, logger, currentNamespace)
   ```

4. **Add VendorType constant**: Edit `api/v1alpha1/mirrorpeer_types.go`:
   ```go
   const (
       ODF      VendorType = "odf"
       MyVendor VendorType = "my-vendor"
   )
   ```

5. **Update kubebuilder validation**: Add to enum:
   ```go
   // +kubebuilder:validation:Enum=odf;my-vendor
   ```

### 10.2 Vendor Best Practices

**Discovery Pattern**:
- Define a ClusterClaim for managed clusters to advertise your storage
- Create a ConfigMap with storage metadata
- Use ManagedClusterView to sync to hub

**Resource Naming**:
- Use deterministic names: `<mirrorpeer-name>-<resource-type>-<hash>`
- Include cluster name in resources created per-cluster
- Avoid collisions between multiple MirrorPeers
- Do not use UID for any naming convention to avoid problems during Hub Recovery

**Ownership**:
- Always set OwnerReference to MirrorPeer on created resources
- Use `controller: true` for primary owned resources

**Status Reporting**:
- Update MirrorPeer conditions as you progress
- Use specific reason codes for failures
- Include actionable error messages

**Idempotency**:
- Assume methods called multiple times
- Check if resources exist before creating
- Update existing resources if spec changed

---

## 11. Security Considerations

### 11.1 RBAC Requirements

**Hub Cluster** (MirrorPeer controller):
- MirrorPeer: get, list, watch, update, patch, delete
- MirrorPeer/status: update, patch
- MirrorPeer/finalizers: update
- ManifestWork: get, list, watch, create, update, delete
- ManagedClusterView: get, list, watch, create, update, delete
- ManagedClusterAddOn: get, list, watch, create, update, delete
- ManagedCluster: get, list, watch

**Managed Clusters** (Vendor-specific, via addons):
- Determined by vendor implementation
- Typically: read/write vendor CRs and Secrets in storage namespace

### 11.2 Network Security

Replication traffic between clusters:
- Runs on network paths configured by vendor
- May require firewall rules, VPN, or direct connectivity
- Vendors should support TLS for encrypted replication
- Document network requirements in vendor-specific design docs

---

## 12. Glossary

- **MirrorPeer**: Cluster-scoped CR orchestrating DR between two storage clusters
- **StorageVendor**: Interface for vendor-specific DR implementation
- **ManagedCluster**: RHACM CR representing an OpenShift cluster
- **ManifestWork**: RHACM CR for distributing resources to managed clusters
- **ManagedClusterView**: RHACM CR for reading resources from managed clusters
- **ManagedClusterAddOn**: RHACM CR for deploying agents to managed clusters
- **RDR**: Regional Disaster Recovery (async replication)
- **MDR**: Metro Disaster Recovery (sync replication)
- **RPO**: Recovery Point Objective (acceptable data loss)
- **RTO**: Recovery Time Objective (acceptable downtime)
