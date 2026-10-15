# Design Document: Manifest Sync (SourceManifest / AppliedManifest)
## OpenShift Data Foundation - Multicluster Orchestrator

---

## 1. Goal

Some storage vendors need a set of their own custom resources (CRs) to exist on
**both** clusters in a DR relationship, kept in agreement across the two. Today
this is manually (scripts run once per setup, re-run whenever a resource
changes). That is error-prone and does not keep up with day-2 changes.

We want MCO to transport these CRs across clusters **continuously** and in a
**vendor-agnostic** way, so any storage vendor that needs CRs on the peer
cluster can use the same mechanism. The first consumer is **IBM Storage
Scale**, which needs resources such as `FileSystem`, `LocalDisk`,
`StorageClass`, and `VolumeGroupReplicationClass` present on the peer cluster.

### Desired flow
- A vendor declares, on cluster A, the resources it wants on cluster B.
- MCO transports them to cluster B and keeps them in agreement.
- The vendor applies them on cluster B and reports back whether they are healthy.
- That health is reflected on the `MirrorPeer` on the hub.
- No manual steps, and it works for any vendor — not just IBM Storage Scale.

---

## 2. API Design

Two namespace-scoped CRDs in group `multicluster.odf.openshift.io/v1alpha1`. They are
**transport envelopes**: each carries a list of arbitrary Kubernetes objects as
`runtime.RawExtension`, so MCO does not need to understand the vendor's types.

There is a **1:1 relationship**: one `SourceManifest` on the source cluster maps
to one `AppliedManifest` on the destination cluster, with the same payload.

### 2.1 SourceManifest (source cluster, created by the vendor)

```go
type SourceManifestSpec struct {
    // Manifests is the list of Kubernetes objects to transport and apply
    // on the destination cluster.
    // +optional
    Manifests []runtime.RawExtension `json:"manifests,omitempty"`
}
```
- **Namespaced.** Carries an empty, unused `Status`. MCO reads it via ManagedClusterView.
- Created and updated by the storage vendor on the source cluster. The vendor
  updates it whenever the set of resources changes (day-2).

### 2.2 AppliedManifest (destination cluster, created by MCO)

```go
type AppliedManifestSpec struct {
    // Manifests contains the raw Kubernetes objects to be applied on this cluster.
    // +optional
    Manifests []runtime.RawExtension `json:"manifests,omitempty"`
}

type AppliedManifestStatus struct {
    Phase              AppliedManifestPhase  `json:"phase,omitempty"`
    ObservedGeneration int64                 `json:"observedGeneration,omitempty"`
    Conditions         []metav1.Condition    `json:"conditions,omitempty"`
}
```
- **Namespaced, with a status subresource.** Printcolumn on `.status.phase`.
- `Phase` is one of: `Pending`, `Applying`, `Applied`, `Failed`, `Degraded`,
  `Terminating`.
- Created by MCO on the destination cluster with the same payload as the
  matching `SourceManifest`.
- **Reconciled by the storage vendor**, which applies the contained objects and
  writes `Status` — phase, conditions, and `ObservedGeneration` set to the
  `.metadata.generation` it processed (so MCO can detect stale status).
- MCO watches this status and reflects it on the `MirrorPeer`.

### 2.3 Ownership summary

| CR | Cluster | Created by | Reconciled by | Status |
|----|---------|-----------|---------------|--------|
| `SourceManifest` | source | storage vendor | MCO (reads via MCV) | none |
| `AppliedManifest` | destination | MCO | storage vendor | phase + conditions |

---

## 3. Vendor Discovery

Before MCO can sync anything, it must learn which managed clusters participate
and where to find their `SourceManifest`s. This reuses the existing
ClusterClaim + ManagedClusterView pattern (mirroring how ODF is discovered via
`odf-info`).

**ClusterClaim (on the managed cluster):**
```
Key:   scale.spectrum.ibm.com
Value: <namespace>/<name>   e.g. ibm-spectrum-scale/ibm-storage-scale-info
```

When a `ManagedCluster` advertises this claim, MCO's managed-cluster controller
creates a **ManagedClusterView** for the referenced `ibm-storage-scale-info` ConfigMap, pulling
the vendor's cluster info onto the hub.

**`ibm-storage-scale-info` ConfigMap content** (format confirmed with the IBM Storage Scale team; the
`sourceManifests` list is what MCO's IBM Storage Scale `StorageVendor` adapter consumes):
```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: ibm-storage-scale-info
  namespace: ibm-spectrum-scale
data:
  ibm-storage-scale-cluster.config.yaml: |
    version: <ibm storage scale version>
    operator-namespace: ibm-spectrum-scale
    cluster:
      name: <scale clustername>
    sourceManifests:
      - <name of source manifest>
```

Each entry in `sourceManifests` is the **name** of a `SourceManifest` CR; it
lives in the `operator-namespace` (`ibm-spectrum-scale`).

---

## 4. Architecture

This is **not a separate controller.** MCO does this as part of the MirrorPeer
reconcile: its IBM Storage Scale `StorageVendor` implementation (`pkg/storagevendor/ibmstoragescale`) creates
the ManagedClusterViews and ManifestWorks directly, on the hub.

### 4.1 Reconcile flow

This runs **for each managed cluster in the MirrorPeer** — each cluster is a
source for its own `SourceManifest`s, synced to its peer (so it is
bidirectional). The steps below are for one source cluster:

1. The vendor creates `SourceManifest`(s) on the source cluster and lists them
   (by name) in its `ibm-storage-scale-info` ConfigMap.
2. MCO (hub) reads the synced `ibm-storage-scale-info` ConfigMap and gets its `sourceManifests`
   list.
3. For each listed `SourceManifest`, MCO creates a ManagedClusterView to read
   that `SourceManifest`'s `.spec.manifests` from the source cluster.
4. MCO creates a matching `AppliedManifest` on the peer cluster (via
   ManifestWork) with the same payload — 1:1 with the source.
5. The vendor operator on the peer cluster reconciles the `AppliedManifest`,
   applies the objects, and writes `.status` (phase + conditions +
   observedGeneration).
6. MCO watches the `AppliedManifest` `.status` via ManagedClusterView and
   reflects it on the `MirrorPeer` status.

### 4.2 Where this lives
No new controller. The logic lives in MCO's IBM Storage Scale `StorageVendor` implementation
(`pkg/storagevendor/ibmstoragescale`), invoked by the MirrorPeer controller's
`ConfigureStorageForDR` / `CleanupResources`.

### 4.3 Resource ownership
- MCO does **not** own the `SourceManifest` — it belongs to the vendor operator
  on the source cluster.
- MCO **owns the ManagedClusterViews and ManifestWorks it creates** for a vendor
  from the MirrorPeer (via owner references), so they are cleaned up with the
  MirrorPeer and in `CleanupResources`.

---

## 5. CRD Installation on Managed Clusters

Both CRDs must exist on the managed clusters — `AppliedManifest` because MCO
creates it on the destination, and `SourceManifest` because the vendor creates it
on the source.

MCO installs both CRDs via the addon manifests, using ManifestWork from the addon
framework.

---

## 6. Open Questions

- **Deletion / cleanup:** when a `SourceManifest` is removed or DR is disabled,
  how is the corresponding `AppliedManifest` (and the resources it created)
  cleaned up? How does the vendor learn a `FileSystem` should be deleted?
