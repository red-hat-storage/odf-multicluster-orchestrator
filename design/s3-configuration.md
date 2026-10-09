# Design Document: S3Configuration API
## OpenShift Data Foundation - Multicluster Orchestrator

---

## Document Information

**Version:** 1.0  
**Date:** October 2026  
**Status:** Implementation  
**Related:** NEW_API_DESIGN_PROPOSAL.md, API_DESIGN_FINAL.md

---

## 1. Goal and Motivation

### 1.1 The Challenge We're Solving

Currently, S3 configuration for disaster recovery metadata storage is tightly coupled with the MirrorPeer resource. This creates several challenges:

1. **Coupling Issues**: S3 is infrastructure that should be managed independently from DR peering relationships
2. **Lifecycle Conflicts**: S3 lifecycle is tied to MirrorPeer lifecycle, making it difficult to update S3 configuration independently
3. **Reusability**: Cannot easily reuse the same S3 configuration across multiple MirrorPeers with different vendors
4. **Complex Coordination**: When multiple MirrorPeers exist, complex logic is needed to determine which one "manages" S3 (the `manageS3` field)
5. **Day 2 Operations**: Rotating S3 credentials or changing S3 backend requires updating all MirrorPeers

### 1.2 What Success Looks Like

Success means achieving complete separation of concerns:
- **S3Configuration** manages S3 configuration independently
- **MirrorPeer** focuses solely on storage peering between clusters
- One S3Configuration can serve multiple DRClusters across different vendors
- S3 credentials can be rotated without touching MirrorPeer resources
- Clear ownership model with no `manageS3` field needed

### 1.3 Why This Matters

Organizations using ODF's disaster recovery capabilities often have:
- Multiple storage vendors (ODF, Dell, Pure Flash, CNSA) in the same environment
- Shared S3 infrastructure that should be configured once and reused
- Security requirements for regular credential rotation
- Need to update S3 backend independently of DR relationships

Without separation of S3 from MirrorPeer, these operations become complex and error-prone.

---

## 2. API Design

### 2.1 S3Configuration Resource

The S3Configuration custom resource manages S3 configuration for DR metadata storage. It is a cluster-scoped resource since it references ManagedClusters.

```go
type S3Configuration struct {
    metav1.TypeMeta   `json:",inline"`
    metav1.ObjectMeta `json:"metadata,omitempty"`
    Spec   S3ConfigurationSpec   `json:"spec,omitempty"`
    Status S3ConfigurationStatus `json:"status,omitempty"`
}
```

### 2.2 S3ConfigurationSpec

The spec supports two mutually exclusive S3 types:

```go
type S3ConfigurationSpec struct {
    // InternalS3 for ODF-managed S3 (Noobaa/RGW)
    // +optional
    InternalS3 *InternalS3Spec `json:"internalS3,omitempty"`
    
    // ExternalS3 for vendor-provided or external S3
    // +optional
    ExternalS3 *ExternalS3Spec `json:"externalS3,omitempty"`
    
    // ManagedClusters that will use this S3 configuration
    // +required
    // +kubebuilder:validation:MinItems=1
    ManagedClusters []string `json:"managedClusters"`
}
```

#### 2.2.1 InternalS3 (ODF-Managed)

```go
type InternalS3Spec struct {
    // ProviderCluster where OBC will be created
    // +required
    ProviderCluster string `json:"providerCluster"`
    
    // StorageClassName for OBC
    // +required
    StorageClassName string `json:"storageClassName"`
    
    // Namespace where OBC will be created
    // +required
    Namespace string `json:"namespace"`
    
    // OBCName (optional, defaults to "odr-<s3config-name>")
    // +optional
    OBCName string `json:"obcName,omitempty"`
}
```

**Key Points:**
- OBC created on **ONE** cluster (specified in `providerCluster`)
- S3 endpoint from that cluster is shared by all clusters in `managedClusters`
- Addon agent on spoke syncs OBC secret to hub

#### 2.2.2 ExternalS3 (Vendor/External)

```go
type ExternalS3Spec struct {
    // SecretRef references hub secret with S3 credentials
    // +required
    SecretRef SecretReference `json:"secretRef"`
}

type SecretReference struct {
    // +required
    Name string `json:"name"`
    // +required
    Namespace string `json:"namespace"`
}
```

**Secret Format:**
```yaml
apiVersion: v1
kind: Secret
metadata:
  name: external-s3
  namespace: openshift-dr-system
stringData:
  AWS_ACCESS_KEY_ID: "..."
  AWS_SECRET_ACCESS_KEY: "..."
  s3Bucket: "dr-metadata-bucket"
  s3CompatibleEndpoint: "https://s3.amazonaws.com"
  s3Region: "us-west-2"  # required (AWS SDK signing region)
```

### 2.3 S3ConfigurationStatus

```go
type S3ConfigurationStatus struct {
    // +optional
    Phase S3ConfigurationPhase `json:"phase,omitempty"`
    
    // +optional
    Message string `json:"message,omitempty"`
    
    // +optional
    ConfiguredClusters []string `json:"configuredClusters,omitempty"`
}

type S3ConfigurationPhase string

const (
    S3ConfigurationPhasePending     S3ConfigurationPhase = "Pending"
    S3ConfigurationPhaseConfiguring S3ConfigurationPhase = "Configuring"
    S3ConfigurationPhaseReady       S3ConfigurationPhase = "Ready"
    S3ConfigurationPhaseFailed      S3ConfigurationPhase = "Failed"
)
```

---

## 3. Controller Architecture

### 3.1 S3Configuration Controller (Hub)

The S3Configuration controller runs on the hub and manages the full S3 configuration lifecycle.

**Responsibilities:**
1. Validate spec (mutual exclusivity, secret existence, cluster validation)
2. For InternalS3: Create ManifestWork to deploy OBC on spoke
3. For ExternalS3: Validate and read existing secret
4. Wait for S3 secret to appear on hub (from addon or user-provided)
5. Copy S3 secret to Ramen operator namespace
6. Update Ramen ConfigMap with S3 profile
7. Create/Update DRCluster for each managed cluster
8. Update status

**Reconciliation Flow:**

```
┌─────────────────────────────────────────────────────────────┐
│ 1. S3Configuration Created                                   │
│    - User creates S3Configuration CR                         │
└─────────────────┬───────────────────────────────────────────┘
                  ▼
┌─────────────────────────────────────────────────────────────┐
│ 2. Validation Phase                                          │
│    - Check InternalS3 XOR ExternalS3                        │
│    - Validate ManagedClusters exist                         │
│    - For ExternalS3: Validate secret exists                 │
│    - For InternalS3: Validate providerCluster in list       │
└─────────────────┬───────────────────────────────────────────┘
                  ▼
         ┌────────┴────────┐
         │                 │
    InternalS3        ExternalS3
         │                 │
         ▼                 ▼
┌──────────────────┐  ┌──────────────────┐
│ 3a. Internal S3  │  │ 3b. External S3  │
│ - Create MW      │  │ - Read secret    │
│ - Deploy OBC     │  │ - Validate keys  │
│ - Wait for addon │  └────────┬─────────┘
│   to sync secret │           │
└────────┬─────────┘           │
         │                     │
         └─────────┬───────────┘
                   ▼
┌─────────────────────────────────────────────────────────────┐
│ 4. S3 Secret Available on Hub                               │
│    - Secret exists in hub namespace                         │
│    - Contains all required keys                             │
└─────────────────┬───────────────────────────────────────────┘
                  ▼
┌─────────────────────────────────────────────────────────────┐
│ 5. Configure Ramen                                          │
│    - Copy secret to Ramen namespace                         │
│    - Update Ramen ConfigMap:                                │
│      s3StoreProfiles:                                       │
│      - s3ProfileName: <S3Configuration.name>               │
│        s3Bucket: ...                                        │
│        s3CompatibleEndpoint: ...                           │
└─────────────────┬───────────────────────────────────────────┘
                  ▼
┌─────────────────────────────────────────────────────────────┐
│ 6. Create/Update DRClusters                                 │
│    - For each cluster in managedClusters:                   │
│      * Create DRCluster on hub                              │
│      * Set spec.s3ProfileName = S3Configuration.name       │
│      * Add S3Configuration as owner                         │
└─────────────────┬───────────────────────────────────────────┘
                  ▼
┌─────────────────────────────────────────────────────────────┐
│ 7. Update Status                                            │
│    - Phase = Ready                                          │
│    - ConfiguredClusters = [cluster1, cluster2, ...]        │
└─────────────────────────────────────────────────────────────┘
```

### 3.2 Addon Agent (Spoke) - For InternalS3

For InternalS3, the existing addon pattern is reused:

**S3SecretReconciler** (on spoke):
- Watches OBC resources
- When OBC status = Bound:
  - Reads OBC secret (SpokeClient)
  - Reads OBC configmap (SpokeClient)
  - Reads S3 route (SpokeClient)
  - Creates "Blue Secret" on hub (HubClient with kubeconfig)

**Why Addon?**
- ✅ Secure: Direct HubClient access, proper RBAC
- ✅ Efficient: Event-driven, no polling
- ✅ Proven: Existing production pattern
- ✅ Standard: ACM-recommended approach

---

## 4. Examples

### 4.1 Example: ODF Internal S3

```yaml
apiVersion: multicluster.odf.openshift.io/v1alpha1
kind: S3Configuration
metadata:
  name: odf-internal-s3  # ← This becomes s3ProfileName
spec:
  internalS3:
    providerCluster: cluster1  # OBC created here
    storageClassName: openshift-storage.noobaa.io
    namespace: openshift-storage
    obcName: dr-metadata-obc
  managedClusters:
    - cluster1  # Uses local S3
    - cluster2  # Uses S3 from cluster1
    - cluster3  # Uses S3 from cluster1
```

**Result:**
1. OBC created on cluster1 only
2. Addon syncs secret to hub
3. DRCluster created for all 3 clusters on hub
4. All reference s3ProfileName: "odf-internal-s3"

### 4.2 Example: External S3 (AWS)

```yaml
# Step 1: Create secret
apiVersion: v1
kind: Secret
metadata:
  name: aws-s3-creds
  namespace: openshift-dr-system
stringData:
  AWS_ACCESS_KEY_ID: "AKIAIOSFODNN7EXAMPLE"
  AWS_SECRET_ACCESS_KEY: "wJalrXUtnFEMI/..."
  s3Bucket: "my-dr-bucket"
  s3CompatibleEndpoint: "https://s3.us-west-2.amazonaws.com"
  s3Region: "us-west-2"

---
# Step 2: Create S3Configuration
apiVersion: multicluster.odf.openshift.io/v1alpha1
kind: S3Configuration
metadata:
  name: aws-external-s3
spec:
  externalS3:
    secretRef:
      name: aws-s3-creds
      namespace: openshift-dr-system
  managedClusters:
    - cluster1
    - cluster2
```

### 4.3 Example: Multiple Vendors Sharing S3

```yaml
# One S3Configuration
apiVersion: multicluster.odf.openshift.io/v1alpha1
kind: S3Configuration
metadata:
  name: shared-s3
spec:
  externalS3:
    secretRef:
      name: shared-s3-secret
      namespace: openshift-dr-system
  managedClusters:
    - cluster1
    - cluster2

---
# MirrorPeer #1 - ODF
apiVersion: multicluster.odf.openshift.io/v1alpha1
kind: MirrorPeer
metadata:
  name: odf-dr
spec:
  storageVendor: odf
  items:
    - clusterName: cluster1
      storageClusterRef: {name: ocs-storagecluster}
    - clusterName: cluster2
      storageClusterRef: {name: ocs-storagecluster}
# MirrorPeer controller auto-discovers S3Configuration "shared-s3"

---
# MirrorPeer #2 - Dell
apiVersion: multicluster.odf.openshift.io/v1alpha1
kind: MirrorPeer
metadata:
  name: dell-dr
spec:
  storageVendor: dell
  items:
    - clusterName: cluster1
      storageClusterRef: {name: powerstore}
    - clusterName: cluster2
      storageClusterRef: {name: powerstore}
# MirrorPeer controller auto-discovers SAME S3Configuration "shared-s3"
```

---

## 5. Validation Rules

### 5.1 CEL Validations (Implemented)

The following validations are implemented using CEL (Common Expression Language) and enforced by the Kubernetes API server:

```go
// 1. Mutual exclusivity - must specify exactly one S3 type
// +kubebuilder:validation:XValidation:rule="(has(self.internalS3) && !has(self.externalS3)) || (!has(self.internalS3) && has(self.externalS3))",message="must specify exactly one of internalS3 or externalS3"

// 2. Cannot switch from InternalS3 to ExternalS3
// +kubebuilder:validation:XValidation:rule="!has(oldSelf.internalS3) || has(self.internalS3)",message="cannot change from internalS3 to externalS3"

// 3. Cannot switch from ExternalS3 to InternalS3
// +kubebuilder:validation:XValidation:rule="!has(oldSelf.externalS3) || has(self.externalS3)",message="cannot change from externalS3 to internalS3"

// 4. InternalS3 configuration is immutable
// +kubebuilder:validation:XValidation:rule="!has(oldSelf.internalS3) || !has(self.internalS3) || self.internalS3 == oldSelf.internalS3",message="internalS3 configuration is immutable"

// 5. ExternalS3 configuration is immutable
// +kubebuilder:validation:XValidation:rule="!has(oldSelf.externalS3) || !has(self.externalS3) || self.externalS3 == oldSelf.externalS3",message="externalS3 configuration is immutable"
```

**What's Immutable:**
- ❌ S3 type (internal vs external) - cannot change after creation
- ❌ `spec.internalS3.*` - all fields immutable after creation
- ❌ `spec.externalS3.*` - all fields immutable after creation
- ❌ `spec.[]managedClusters` - all fields immutable after creation

### 5.2 Controller-Side Validations

These are enforced by the controller at reconcile time (not at admission like the CEL rules).

**Cluster Uniqueness (Implemented):**
- Each cluster can only appear in ONE S3Configuration's `managedClusters` list
  (`validateManagedClustersUniqueness`). Because this is a controller-side check,
  there is an inherent race between near-simultaneous creates; a validating
  webhook would be needed for a hard guarantee.

**Deletion Protection (Implemented):**
- An S3Configuration cannot be deleted while any DRPolicy references one of its
  clusters (`validateNoDRPolicyUsesClusters`).

**ManagedCluster Existence (Future):**
- Validate that referenced ManagedCluster resources actually exist.

### 7. Secret Sync Mechanism

For InternalS3, secrets are synced from spoke to hub using the **addon pattern with dual clients**:

- **NOT using ManifestWork feedback** (only returns status, not data)
- **NOT using ManagedClusterView** (exposes secrets in status fields - insecure)
- **USING addon with HubClient** (secure, direct Secret creation on hub)

See `INTERNAL_S3_SYNC_MECHANISM.md` and `MANAGEDCLUSTERVIEW_SECURITY_ANALYSIS.md` for details.

## 8. Security Considerations

✅ **Separation of Concerns**: S3 managed independently from DR peering  
✅ **Reusability**: One S3Configuration serves multiple MirrorPeers/vendors  
✅ **Simplified Lifecycle**: Update S3 without touching MirrorPeer  
✅ **Clearer Ownership**: No `manageS3` field needed  
✅ **Better Day 2**: Credential rotation, S3 backend changes are easier  
✅ **Multi-Vendor**: Same S3 shared across ODF, Dell, Pure, CNSA  

---
