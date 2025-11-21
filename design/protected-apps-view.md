# Design Document: Protected Applications Unified View
## OpenShift Data Foundation - Disaster Recovery

---

## Document Information

**Version:** 1.0  
**Date:** November 2025  
**Status:** Proposal  
**Epic:** Enhance Protected Applications page to support managed and discovered applications

---

## 1. Goal and Motivation

### 1.1 The Challenge We're Solving

Currently, our Protected Applications page displays only discovered applications, which are applications protected at the namespace level through DRPlacementControl (DRPC) resources in the `openshift-dr-ops` namespace. However, Red Hat Advanced Cluster Management (ACM) supports two additional application types that can also be protected with disaster recovery capabilities: ApplicationSets (deployed via GitOps/ArgoCD) and Subscriptions (the legacy ACM application deployment model). 

Users who have protected these managed applications need visibility into their disaster recovery status alongside their discovered applications. Without a unified view, users must navigate to different pages or use multiple interfaces to understand the complete picture of their protected workloads. This fragmentation creates confusion and increases the operational burden on cluster administrators who need to manage disaster recovery operations across all application types.

### 1.2 What Success Looks Like

Success for this enhancement means achieving a single, unified Protected Applications page where users can view and manage all protected applications regardless of their deployment type. The page should provide the same operational capabilities for ApplicationSets and Subscriptions that currently exist for discovered applications, including the ability to perform failover, relocate, and disable disaster recovery operations. Users should be able to filter and sort across all application types seamlessly, and the performance of the page should remain acceptable even when displaying hundreds of applications across all three types.

### 1.3 Why This Matters

Organizations using OpenShift Data Foundation's disaster recovery capabilities often deploy applications using multiple methods. Development teams might use GitOps with ApplicationSets for modern cloud-native applications, while operations teams might maintain legacy applications deployed through ACM Subscriptions. Critical infrastructure components might be protected as discovered applications at the namespace level. Without a unified view, disaster recovery orchestration becomes complex and error-prone. A cluster administrator preparing for a planned datacenter migration needs to see all protected workloads in one place to ensure nothing is missed during the failover operation.

---

## 2. Understanding the Current Implementation

### 2.1 Application Types and DR Protection Model

Before diving into the technical implementation, it's important to understand how different application types relate to disaster recovery in our system. All three application types can be protected through the same disaster recovery mechanism, but they reach that protection through different paths.

At the heart of our disaster recovery system is the DRPlacementControl (DRPC) resource. This is the Kubernetes custom resource that actually controls disaster recovery operations. A DRPC references a DRPolicy, which defines the disaster recovery configuration including which clusters are involved in the DR relationship and how data replication should be handled. When an application is "protected," what we really mean is that a DRPC exists that manages disaster recovery for that application.

#### 2.1.1 Discovered Applications (Current Implementation)

Discovered applications represent the simplest protection model. In this model, a cluster administrator directly creates a DRPC in the special `openshift-dr-ops` namespace to protect one or more namespaces. The DRPC name becomes the application name, and the protected namespaces are listed explicitly in the DRPC specification.

Here's a concrete example of a discovered application:

```yaml
apiVersion: ramendr.openshift.io/v1alpha1
kind: DRPlacementControl
metadata:
  name: critical-database-app  # This becomes the application name
  namespace: openshift-dr-ops  # Special namespace for discovered apps
spec:
  # Which namespaces are being protected
  protectedNamespaces:
    - prod-database
    - prod-database-config
  
  # The DR policy defining replication and clusters
  drPolicyRef:
    name: metro-dr-policy
  
  # The placement that determines where the app runs
  placementRef:
    name: critical-database-placement
    kind: Placement
```

The current Protected Applications page watches for these DRPCs in the `openshift-dr-ops` namespace and displays each one as an application. This works well because there's a direct one-to-one mapping: one DRPC equals one application entry in the table.

#### 2.1.2 ApplicationSets (GitOps Deployment)

ApplicationSets are a more complex case because they are deployed through the GitOps workflow using ArgoCD. An ApplicationSet doesn't directly reference disaster recovery resources. Instead, the relationship is established through an ACM Placement resource.

The connection works like this: An ApplicationSet specifies which clusters it should be deployed to by referencing a Placement through a label selector in its cluster decision resource generator. Separately, a DRPC can be created that references the same Placement to provide disaster recovery for that application. The Placement acts as the bridge between the application deployment (ApplicationSet) and disaster recovery (DRPC).

Here's what this looks like in practice:

```yaml
# The ApplicationSet defines the application
apiVersion: argoproj.io/v1alpha1
kind: ApplicationSet
metadata:
  name: payment-service
  namespace: openshift-gitops
spec:
  generators:
    - clusterDecisionResource:
        configMapRef: acm-placement
        labelSelector:
          matchLabels:
            # This label connects to the Placement
            cluster.open-cluster-management.io/placement: payment-service-placement

---
# The Placement determines where the app runs
apiVersion: cluster.open-cluster-management.io/v1beta1
kind: Placement
metadata:
  name: payment-service-placement
  namespace: openshift-gitops
spec:
  clusterSets:
    - default
  predicates:
    - requiredClusterSelector:
        labelSelector:
          matchExpressions:
            - key: name
              operator: In
              values:
                - primary-datacenter

---
# The DRPC provides DR protection by referencing the same Placement
apiVersion: ramendr.openshift.io/v1alpha1
kind: DRPlacementControl
metadata:
  name: payment-service-drpc
  namespace: openshift-gitops  # Same namespace as the ApplicationSet
spec:
  placementRef:
    name: payment-service-placement  # References the same Placement
    kind: Placement
  drPolicyRef:
    name: metro-dr-policy
```

Notice how the DRPC doesn't directly reference the ApplicationSet. Instead, both the ApplicationSet and the DRPC reference the same Placement. This indirection is what makes the correlation complex.

#### 2.1.3 Subscriptions (Legacy ACM Applications)

Subscriptions represent the older ACM application deployment model, which is now deprecated but still widely used in existing deployments. The Subscription model is even more complex because a single logical application can consist of multiple Subscription resources, each potentially referencing different Placements.

The structure involves three resources: An Application resource (not to be confused with ArgoCD's Application) acts as a grouper that uses label selectors to identify which Subscriptions belong to it. Each Subscription deploys specific content (typically from a Git repository path) and references a Placement to determine where it should run. Again, a DRPC can reference any of these Placements to provide disaster recovery.

Here's an example showing this complexity:

```yaml
# The Application groups subscriptions together
apiVersion: app.k8s.io/v1beta1
kind: Application
metadata:
  name: web-application
  namespace: web-app-ns
spec:
  componentKinds:
    - group: apps.open-cluster-management.io
      kind: Subscription
  selector:
    matchExpressions:
      - key: app
        operator: In
        values:
          - web-application  # Subscriptions with this label belong to this app

---
# First subscription deploys the frontend
apiVersion: apps.open-cluster-management.io/v1
kind: Subscription
metadata:
  name: web-frontend-subscription
  namespace: web-app-ns
  labels:
    app: web-application  # Matches the Application selector
spec:
  channel: git-repo-channel
  placement:
    placementRef:
      name: web-frontend-placement
      kind: Placement

---
# Second subscription deploys the backend
apiVersion: apps.open-cluster-management.io/v1
kind: Subscription
metadata:
  name: web-backend-subscription
  namespace: web-app-ns
  labels:
    app: web-application  # Same label, same Application
spec:
  channel: git-repo-channel
  placement:
    placementRef:
      name: web-backend-placement  # Different placement!
      kind: Placement

---
# DRPC protects the frontend placement
apiVersion: ramendr.openshift.io/v1alpha1
kind: DRPlacementControl
metadata:
  name: web-frontend-drpc
  namespace: web-app-ns
spec:
  placementRef:
    name: web-frontend-placement
    kind: Placement
  drPolicyRef:
    name: metro-dr-policy

---
# Separate DRPC protects the backend placement
apiVersion: ramendr.openshift.io/v1alpha1
kind: DRPlacementControl
metadata:
  name: web-backend-drpc
  namespace: web-app-ns
spec:
  placementRef:
    name: web-backend-placement
    kind: Placement
  drPolicyRef:
    name: metro-dr-policy
```

This example illustrates a challenging scenario: a single logical application (the web application) has two subscriptions, each with its own placement, and potentially its own DRPC. When displaying this in the Protected Applications page, we need to decide whether to show this as one row or two, and if one row, which DRPC's information should be displayed.

**Important Note:** In the proposed design, this multi-DRPC subscription scenario results in **two separate table rows** (one per DRPC):
- Row 1: `web-app-frontend-drpc` (Subscription type) - protects frontend placement
- Row 2: `web-app-backend-drpc` (Subscription type) - protects backend placement

Each row represents one protection boundary (DRPC), making actions unambiguous. This is acceptable since Subscriptions are a deprecated API and this multi-DRPC pattern is rare.

### 2.2 The Placement Resource as the Bridge

Throughout all three application types, the Placement resource (or its predecessor, PlacementRule) serves as the critical link between application deployment and disaster recovery. This is not accidental but by design. The Placement resource encodes the scheduling decision of where an application should run, and the DRPC needs to know this scheduling information to properly orchestrate disaster recovery operations.

When a failover occurs, the DRPC modifies the Placement's cluster selection to redirect the application to the secondary datacenter. This is why the DRPC must reference a Placement, and why finding the DRPC for an application always involves first finding the application's Placement.

### 2.3 Current Frontend Architecture

The current frontend implementation uses a series of specialized React hooks to watch Kubernetes resources and perform the correlation logic needed to display applications with their disaster recovery status. Let's examine how this works today for the discovered applications that are currently displayed.

#### 2.3.1 The Simple Case: Discovered Applications

The current Protected Applications list page uses a straightforward approach because discovered applications have a direct mapping. Here's the essential code:

```typescript
export const ProtectedApplicationsListPage: React.FC = () => {
  const { t } = useCustomTranslation();
  const launcher = useModal();
  const navigate = useNavigate();

  // Watch all DRPCs in the special discovered apps namespace
  const [discoveredApps, discoveredAppsLoaded, discoveredAppsError] =
    useK8sWatchResource<DRPlacementControlKind[]>(
      getDRPlacementControlResourceObj({
        namespace: DISCOVERED_APP_NS, // "openshift-dr-ops"
      })
    );

  // Each DRPC is directly displayed as one application
  return (
    <PaginatedListPage
      filteredData={discoveredApps}
      composableTableProps={{
        columns: getHeaderColumns(t),
        RowComponent: ProtectedAppsTableRow,
        // ... other props
      }}
    />
  );
};
```

This works beautifully because each DRPC contains all the information needed for display. The DRPC's name is the application name, its `spec.protectedNamespaces` array provides the namespace count, its `status.phase` and `status.conditions` provide the disaster recovery status, and its `spec.drPolicyRef.name` provides the DR policy name.

#### 2.3.2 The Complex Case: Managed Applications

For ApplicationSets and Subscriptions, the situation is far more complex. We have existing hooks that perform the necessary correlation, but they were designed for different use cases and pages. Let's examine each one to understand what correlation logic would be needed.

**The ApplicationSet Hook**

The `useArgoApplicationSetResourceWatch` hook demonstrates the correlation complexity for ApplicationSets:

```typescript
export const useArgoApplicationSetResourceWatch = (resource) => {
  // Watch multiple resource types simultaneously
  const response = useK8sWatchResources<WatchResourceType>({
    applications: getApplicationSetResourceObj(),      // All ApplicationSets
    placements: getPlacementResourceObj(),              // All Placements
    placementDecisions: getPlacementDecisionsResourceObj(), // Cluster assignments
    managedClusters: getManagedClusterResourceObj()     // Available clusters
  });

  // Also receives pre-fetched DR resources as input
  const { drResources } = resource;

  return React.useMemo(() => {
    const formatted = [];
    
    appList.forEach((application) => {
      // Step 1: Find the Placement that this ApplicationSet uses
      // This involves parsing the generators[].clusterDecisionResource.labelSelector
      const placement = findPlacementFromArgoAppSet(placementList, application);
      
      // Step 2: Find the DRPC that references this Placement
      // This searches through all DRPCs looking for one whose placementRef matches
      const drResource = findDRResourceUsingPlacement(
        getName(placement),
        getNamespace(placement),
        drResources?.formattedResources
      );
      
      // Step 3: Build the aggregated data structure
      formatted.push({
        application,
        placements: [{
          placement,
          placementDecision: findPlacementDecisionUsingPlacement(...),
          drPolicy: drResource?.drPolicy,
          drClusters: drResource?.drClusters,
          drPlacementControl: drResource?.drPlacementControls?.[0],
        }],
        // Additional correlation for sibling ApplicationSets and clusters
        siblingApplications: findSiblingArgoAppSetsFromPlacement(...),
        managedClusters: filterManagedClusterUsingDRClusters(...),
      });
    });
    
    return [{ formattedResources: formatted }, loaded, loadError];
  }, [applications, placements, drResources, /* ... */]);
};
```

The key insight here is that the correlation happens in the `useMemo` block. Every time any of the watched resources change (which can happen frequently in a live cluster), this entire correlation logic re-executes. For each ApplicationSet, it must search through all Placements to find the matching one, then search through all DRPCs to find one that references that Placement.

**The Subscription Hook**

The `useSubscriptionResourceWatch` hook is even more complex because it must handle the grouping of multiple Subscriptions into a single Application:

```typescript
export const useSubscriptionResourceWatch = (resource) => {
  const response = useK8sWatchResources<WatchResourceType>({
    applications: getApplicationResourceObj(),
    subscriptions: getSubscriptionResourceObj(),
    placements: getPlacementResourceObj(),
    placementRules: getPlacementRuleResourceObj(), // Legacy placement API
    placementDecisions: getPlacementDecisionsResourceObj()
  });

  return React.useMemo(() => {
    const result = [];
    
    // Step 1: Group resources by namespace for efficient lookup
    const namespaceToApplications = getNamespaceWiseApplications(applicationList);
    const namespaceToSubscriptions = getNamespaceWiseSubscriptions(subscriptionsList);
    const namespaceToPlacementMap = getNamespaceWisePlacements(placements);
    
    // Step 2: For each Application, find its Subscriptions
    Object.keys(namespaceToApplications).forEach((namespace) => {
      namespaceToApplications[namespace].forEach((application) => {
        
        // Find all Subscriptions that match this Application's label selector
        const matchingSubscriptions = namespaceToSubscriptions[namespace].filter(sub =>
          matchApplicationToSubscription(sub, application)
        );
        
        // Step 3: Group Subscriptions by their Placement
        // Multiple Subscriptions can share the same Placement
        const subscriptionGroups = generateSubscriptionGroupInfo(
          application,
          matchingSubscriptions,
          namespaceToPlacementMap[namespace],
          placementDecisions[namespace],
          drResources
        );
        
        // Step 4: For each Placement group, find the DRPC
        subscriptionGroups.forEach(group => {
          group.drInfo = findDRResourceUsingPlacement(
            group.placement.name,
            group.placement.namespace,
            drResources.formattedResources
          );
        });
        
        result.push({
          application,
          subscriptionGroupInfo: subscriptionGroups
        });
      });
    });
    
    return [result, loaded, loadError];
  }, [applications, subscriptions, placements, drResources, /* ... */]);
};
```

The complexity here stems from the many-to-many relationships: one Application can have many Subscriptions, and multiple Subscriptions can share a Placement. The correlation logic must navigate this complex graph to build the final data structure.

**The DR Resources Hook**

Both ApplicationSet and Subscription hooks depend on a shared hook that fetches all disaster recovery resources:

```typescript
export const useDisasterRecoveryResourceWatch = (resource) => {
  const response = useK8sWatchResources<WatchResourceType>({
    drPolicies: getDRPolicyResourceObj(),
    drClusters: getDRClusterResourceObj(),
    drPlacementControls: getDRPlacementControlResourceObj() // All DRPCs, all namespaces
  });

  return React.useMemo(() => {
    const formatted = [];
    
    // Group DR resources by DRPolicy for efficient lookup
    drPolicyList.forEach((drPolicy) => {
      const drpcs = findDRPCUsingDRPolicy(drpcList, drPolicy.name);
      const clusters = filterDRClustersUsingDRPolicy(drPolicy, drClusterList);
      
      formatted.push({
        drPolicy,
        drClusters: clusters,
        drPlacementControls: drpcs
      });
    });
    
    return [{ formattedResources: formatted }, loaded, loadError];
  }, [drPolicies, drClusters, drPlacementControls]);
};
```

This hook pre-groups DR resources by policy, which helps make the lookups in the other hooks more efficient. However, it still requires fetching all DRPCs from all namespaces, which can be expensive in large clusters.

### 2.4 The Correlation Algorithm

At the heart of all this complexity is the correlation algorithm that connects Applications to DRPCs through Placements. This algorithm is worth understanding in detail because it reveals both why the current approach is expensive and what opportunities exist for optimization.

The algorithm uses what we call a "Placement Unique Key" to match resources:

```typescript
// Step 1: Build a unique identifier for each Placement
const getPlacementUniqueKey = (name: string, kind: string, namespace: string): string => {
  return `${name}-${kind}-${namespace}`;
  // Example: "payment-service-placement-Placement-openshift-gitops"
};

// Step 2: Build a map from Placement keys to DRPCs
const buildPlacementToDRPCMap = (drpcs: DRPlacementControlKind[]) => {
  const map = {};
  
  drpcs.forEach(drpc => {
    const placementRef = drpc.spec.placementRef;
    const key = getPlacementUniqueKey(
      placementRef.name,
      placementRef.kind,
      drpc.namespace // Note: DRPC namespace, not Placement namespace (they're the same)
    );
    
    map[key] = drpc;
  });
  
  return map;
  // Example map:
  // {
  //   "payment-service-placement-Placement-openshift-gitops": <DRPC object>,
  //   "web-frontend-placement-Placement-web-app-ns": <DRPC object>
  // }
};

// Step 3: For any Placement, look up its DRPC
const findDRPCForPlacement = (placement: PlacementKind, drpcMap: PlacementToDRPCMap) => {
  const key = getPlacementUniqueKey(
    placement.name,
    placement.kind,
    placement.namespace
  );
  
  return drpcMap[key]; // O(1) lookup
};
```

This algorithm is elegant and efficient once the map is built. The problem is that in the current frontend implementation, this map must be rebuilt every time any watched resource changes, and the map building requires iterating through all DRPCs in the cluster.

---

## 3. Caveats and Limitations of Current Approach

### 3.1 Performance and Scalability Concerns

#### 3.1.1 Redundant Kubernetes Watches
Combining all three application types creates 15 separate watch connections to the Kubernetes API:
ApplicationSet hook: ApplicationSets, Placements, PlacementDecisions, ManagedClusters
Subscription hook: Applications, Subscriptions, Placements (duplicate), PlacementRules, PlacementDecisions (duplicate)
DR Resources hook: DRPolicies, DRClusters, DRPlacementControls (all namespaces)
Discovered hook: DRPlacementControls (openshift-dr-ops namespace - duplicate)
Each watch maintains a websocket connection streaming all changes cluster-wide, creating significant API server load and network traffic.

#### 3.1.2 Browser-Side Correlation Overhead
The primary performance bottleneck is waiting for 10-20 simultaneous API calls to complete before displaying the UI. Each hook must establish watches and fetch initial data for multiple resource types. The page remains in loading state until all watches sync, typically 5-12 seconds in large clusters.
While browser-side correlation (O(N*M) complexity with nested loops) does contribute overhead during updates especially during incident response when operations teams need immediate visibility.

#### 3.1.3 Memory Consumption
While useK8sWatchResources shares Redux state and stores objects by reference (not deep copies), multiple hooks watching overlapping resource sets still increase memory footprint. In large clusters with hundreds of applications, the accumulated state from multiple active hooks contributes to console instances consuming over fifty megabytes just for application data, impacting performance on low-spec devices.

#### 3.1.4 Watch Multiplicity
While useK8sWatchResources shares Redux state across components (identical resource objects create single websocket connections), different hooks watch slightly different resource sets. The ApplicationSet hook watches Placements, while the Subscription hook watches both Placements and PlacementRules. This prevents full deduplication despite the shared state mechanism.it.

### 3.2 Code Maintainability Challenges
Correlation logic for connecting Applications → Placements → DRPCs exists in multiple hooks with different implementations:

ApplicationSet hook: finds Placements via generator labelSelector
Subscription hook: groups Subscriptions, handles multiple Placements
Discovered hook: no correlation needed

Bug fixes or enhancements require changes across all implementations, violating DRY and creating divergence risks.

### 3.3 User Experience Limitations
Page Load Performance: 5-12 seconds loading time while establishing watches and fetching data. During incident response, operations teams must wait before assessing which applications need attention.
Stale Data: Browser-side correlation creates a window where UI shows inconsistent state (e.g., ApplicationSet visible but no DR info shown), causing confusion about protection status. Users frequently refresh to resolve inconsistencies.
Limited Filtering: Client-side filtering requires fetching and correlating all applications first. No efficient queries like "all ApplicationSets protected by metro-dr-policy."

---

## 4. Proposed Solution: Controller-Based Aggregation

### 4.1 Design Philosophy
The proposed solution moves the correlation logic from the frontend to the backend, implementing it as a Kubernetes controller that maintains an aggregated view resource. This architectural shift is based on several key principles that address the limitations we've identified.
Single Source of Truth: Rather than having correlation logic scattered across multiple frontend hooks, we create a single authoritative source: a new Custom Resource Definition called ProtectedApplicationView. This CRD represents the aggregated information about a protected application. The frontend simply watches and displays these views.
DRPC as Protection Boundary: Since disaster recovery protection is defined by the existence of a DRPlacementControl (DRPC), we use DRPC as the trigger for creating views. No DRPC means no protection, which means no view. This ensures we only show protected applications as required.
One View Per DRPC: Each DRPC gets exactly one ProtectedApplicationView, providing a clear 1:1 mapping between protection resources and table rows. This eliminates ambiguity in actions—clicking Failover on a row acts on that row's DRPC.


### 4.2 Architecture Overview

## Architecture Diagram
```
┌─────────────────────────────────────────────────────────────────┐
│  Frontend (React)                                               │
│  - Watches ProtectedApplicationView CRs (new CRD)               │
│  - Simple useK8sWatchResource hook                              │
│  - No correlation logic                                         │
└────────────────────┬────────────────────────────────────────────┘
                     │
                     │ Single watch
                     ▼
┌─────────────────────────────────────────────────────────────────┐
│  Kubernetes API Server                                          │
│                                                                 │
│  ┌────────────────────────────────────────────────────────────┐ │
│  │  ProtectedApplicationView CRs (one per DRPC)               │ │
│  │  ┌──────────────────────────────────────────────────────┐  │ │
│  │  │ appset-payment-service (ApplicationSet type)         │  │ │
│  │  ├──────────────────────────────────────────────────────┤  │ │
│  │  │ subscription-web-frontend-drpc (Subscription type)   │  │ │
│  │  ├──────────────────────────────────────────────────────┤  │ │
│  │  │ subscription-web-backend-drpc (Subscription type)    │  │ │
│  │  ├──────────────────────────────────────────────────────┤  │ │
│  │  │ discovered-critical-db (Discovered type)             │  │ │
│  │  └──────────────────────────────────────────────────────┘  │ │
│  └────────────────────────────────────────────────────────────┘ │
└──────────────────────┬──────────────────────────────────────────┘
                       │
                       │ Reconciled by
                       ▼
┌──────────────────────────────────────────────────────────────────┐
│  ODF MCO Operator (Backend)                                      │
│                                                                  │
│  ┌─────────────────────────────────────────────────────────────┐ │
│  │  DRPC Controller (Single Controller)                        │ │
│  │                                                             │ │
│  │  Watches: DRPlacementControl (all namespaces)               │ │
│  │                                                             │ │
│  │  For each DRPC:                                             │ │
│  │    1. Get Placement from DRPC.spec.placementRef             │ │
│  │    2. Reverse map to find application:                      │ │
│  │       - Check namespace: openshift-dr-ops? → Discovered     │ │
│  │       - Find ApplicationSet using this Placement?           │ │
│  │       - Find Subscriptions using this Placement?            │ │
│  │    3. Build ProtectedApplicationView                        │ │
│  │    4. Set owner reference (ApplicationSet or DRPC)          │ │
│  │    5. Create/update view                                    │ │
│  │                                                             │ │
│  │  Secondary watches:                                         │ │
│  │    - ApplicationSet → find affected DRPCs → reconcile       │ │
│  │    - Application (Subscription) → find affected DRPCs       │ │
│  │    - Placement → find affected DRPCs → reconcile            │ │
│  └─────────────────────────────────────────────────────────────┘ │
└──────────────────────────────────────────────────────────────────┘
```

#### 4.2.1 The ProtectedApplicationView CRD

This new custom resource represents the aggregated view of a protected application. Each DRPC gets one corresponding view.

```yaml
apiVersion: multicluster.odf.openshift.io/v1alpha1
kind: ProtectedApplicationView
metadata:
  name: applicationset-payment-service-drpc
  namespace: payment-app
  labels:
    app.kubernetes.io/type: ApplicationSet
    app.kubernetes.io/name: payment-service
    ramendr.openshift.io/dr-policy: metro-dr
  ownerReferences:
  - apiVersion: ramendr.openshift.io/v1alpha1
    kind: DRPlacementControl
    name: payment-service-drpc
    uid: 12345678-1234-1234-1234-123456789abc
    controller: true
    blockOwnerDeletion: true
spec:
  drpcRef:
    name: payment-service-drpc
    namespace: payment-app
  applicationRef:
    apiVersion: argoproj.io/v1alpha1
    kind: ApplicationSet
    name: payment-service
    namespace: payment-app
    uid: abcdef12-3456-7890-abcd-ef1234567890
status:
  type: ApplicationSet 
  placements:
  - name: payment-placement
    namespace: payment-app
    kind: Placement
    selectedClusters:
    - cluster-east
    - cluster-west
    numberOfClusters: 2

  drInfo:
    drPolicyRef:
      name: metro-dr
    drClusters:
    - cluster-east
    - cluster-west
    primaryCluster: cluster-east
    protectedNamespaces:
    - payment-app
    - payment-db
    status:
      phase: Relocated
      conditions:
      - type: Available
        status: "True"
        lastTransitionTime: "2025-11-18T08:00:00Z"
        reason: Healthy
        message: Application is healthy on primary cluster
      - type: PeerReady
        status: "True"
        lastTransitionTime: "2025-11-18T08:00:00Z"
        reason: Ready
        message: Peer cluster is ready
  
  observedGeneration: 3
  lastSyncTime: "2025-11-18T08:05:23Z"
  
  conditions:
  - type: Ready
    status: "True"
    lastTransitionTime: "2025-11-18T08:00:00Z"
    reason: ViewSynced
    message: Protected application view successfully synced
```
```yaml
apiVersion: multicluster.odf.openshift.io/v1alpha1
kind: ProtectedApplicationView
metadata:
  name: subscription-web-frontend-drpc
  namespace: web-app
spec:
  drpcRef:
    name: web-frontend-drpc
    namespace: web-app
  applicationRef:
    apiVersion: apps.open-cluster-management.io/v1
    kind: Application
    name: web-frontend
    namespace: web-app
status:
  type: Subscription
  placements:
  - name: web-placement
    namespace: web-app
    kind: PlacementRule
    selectedClusters:
    - prod-cluster
    numberOfClusters: 1
  drInfo:
    drPolicyRef:
      name: async-dr
    drClusters:
    - prod-cluster
    - dr-cluster
    primaryCluster: prod-cluster
    status:
      phase: FailedOver
  subscriptionInfo:
    subscriptionRefs:
    - name: web-frontend-sub
      namespace: web-app
  lastSyncTime: "2025-11-18T08:10:15Z"
```

**Key Design Decisions:**

**Owner Reference Strategy:**
ApplicationSet type: Owner = DRPC CR (when DRPC deleted → view deleted)
Subscription type: Owner = DRPC (deprecated API, DRPC is the stable resource)
Discovered type: Owner = DRPC (DRPC is the application)

**Labels for Efficient Querying:**
app.kubernetes.io/type: Filter by application type
ramendr.openshift.io/dr-policy: Filter by DR policy

**No additionalDRPCs Field:**
One view per DRPC means no need to track multiple DRPCs
Subscriptions with multiple placements appear as multiple rows (one per DRPC)

#### 4.2.2 The DRPC Controller

A single controller watches all DRPCs and performs reverse mapping to find the associated application.

```go
// DRPCReconciler watches DRPCs and creates ProtectedApplicationViews
type DRPCReconciler struct {
    client.Client
    Scheme *runtime.Scheme
}

func (r *DRPCReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    log := log.FromContext(ctx)
    
    // Step 1: Fetch DRPC
    drpc := &multiclusterv1alpha1.DRPlacementControl{}
    if err := r.Get(ctx, req.NamespacedName, drpc); err != nil {
        if apierrors.IsNotFound(err) {
            // DRPC deleted → view auto-deleted via owner reference
            return ctrl.Result{}, nil
        }
        return ctrl.Result{}, err
    }
    
    // Step 2: Get Placement from DRPC
    placement, err := r.getPlacement(ctx, drpc)
    if err != nil {
        log.Error(err, "Failed to get Placement for DRPC")
        return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
    }
    
    // Step 3: Reverse map to find application type and reference
    appType, appRef, typeSpecificInfo, err := r.findApplicationForDRPC(ctx, drpc, placement)
    if err != nil {
        log.Error(err, "Failed to find application for DRPC")
        // Orphaned DRPC - no application found, skip view creation
        return r.cleanupView(ctx, drpc), nil
    }
    
    // Step 4: Get DR policy and cluster information
    drPolicy, drClusters, _ := r.getDRPolicyAndClusters(ctx, drpc)
    placementDecision, _ := r.getPlacementDecision(ctx, placement)
    
    // Step 5: Build ProtectedApplicationView
    view := &multiclusterv1alpha1.ProtectedApplicationView{
        ObjectMeta: metav1.ObjectMeta{
            Name:      r.generateViewName(appType, appRef, drpc),
            Namespace: drpc.Namespace,
            Labels: map[string]string{
                "app.kubernetes.io/type":           appType,
                "ramendr.openshift.io/dr-policy":   drpc.Spec.DRPolicyRef.Name,
            },
        },
        Spec: multiclusterv1alpha1.ProtectedApplicationViewSpec{
            Type:           appType,
            ApplicationRef: appRef,
            Placements:     r.buildPlacementInfo(placement, placementDecision),
            DRInfo:         r.buildDRInfo(drpc, drPolicy, drClusters),
        },
    }
    
    // Add type-specific information
    r.addTypeSpecificInfo(view, appType, typeSpecificInfo)
    
    // Step 6: Set owner reference
    if err := r.setOwnerReference(view, appType, appRef, drpc); err != nil {
        return ctrl.Result{}, err
    }
    
    // Step 7: Create or update view
    existingView := &multiclusterv1alpha1.ProtectedApplicationView{}
    err = r.Get(ctx, client.ObjectKey{
        Name: view.Name, Namespace: view.Namespace,
    }, existingView)
    
    if err != nil && apierrors.IsNotFound(err) {
        if err := r.Create(ctx, view); err != nil {
            return ctrl.Result{}, err
        }
        log.Info("Created ProtectedApplicationView", "name", view.Name)
    } else if err == nil {
        existingView.Spec = view.Spec
        if err := r.Update(ctx, existingView); err != nil {
            return ctrl.Result{}, err
        }
        log.Info("Updated ProtectedApplicationView", "name", view.Name)
    } else {
        return ctrl.Result{}, err
    }
    
    return ctrl.Result{}, nil

    
}
```

**Reverse Mapping Implementation:**
```go
func (r *DRPCReconciler) findApplicationForDRPC(
    ctx context.Context,
    drpc *ramendrv1alpha1.DRPlacementControl,
    placement *placementv1beta1.Placement,
) (appType string, appRef corev1.ObjectReference, typeInfo interface{}, err error) {
    
    // Try Case 1: ApplicationSet using this Placement
    appSet, _ := r.findApplicationSetByPlacement(ctx, placement)
    if appSet != nil {
        return "ApplicationSet", corev1.ObjectReference{
            Kind: appSet.Kind, Name: appSet.Name, Namespace: appSet.Namespace,
        }, r.extractAppSetInfo(appSet), nil
    }
    
    // Try Case 2: Subscriptions using this Placement
    subscriptions, _ := r.findSubscriptionsByPlacement(ctx, placement)
    if len(subscriptions) > 0 {
        app, _ := r.findParentApplication(ctx, subscriptions)
        return "Subscription", corev1.ObjectReference{
            Kind: app.Kind, Name: app.Name, Namespace: app.Namespace,
        }, r.extractSubscriptionInfo(subscriptions), nil
    }
    
    // Case 3: No app found → Discovered
    // DRPC itself is the application
    return "Discovered", corev1.ObjectReference{
        APIVersion: drpc.APIVersion,
        Kind:       drpc.Kind,
        Name:       drpc.Name,
        Namespace:  drpc.Namespace,
        UID:        drpc.UID,
    }, nil, nil
}
```
The controller also needs to watch not just ApplicationSets but also the related resources that might change:

**Watch Configuration:**

```go
func (r *DRPCReconciler) SetupWithManager(mgr ctrl.Manager) error {
    return ctrl.NewControllerManagedBy(mgr).
        // Primary watch: ALL DRPCs trigger reconciliation
        For(&ramendrv1alpha1.DRPlacementControl{}).
        
        // Secondary watches: application changes trigger DRPC reconciliation
        Watches(
            &argov1alpha1.ApplicationSet{},
            handler.EnqueueRequestsFromMapFunc(r.findDRPCsForApplicationSet),
        ).
        Watches(
            &appv1beta1.Application{},  // Subscription parent
            handler.EnqueueRequestsFromMapFunc(r.findDRPCsForApplication),
        ).
        Watches(
            &placementv1beta1.Placement{},
            handler.EnqueueRequestsFromMapFunc(r.findDRPCsForPlacement),
        ).
        Watches(
            &placementv1beta1.PlacementDecision{},
            handler.EnqueueRequestsFromMapFunc(r.findDRPCsForPlacementDecision),
        ).
        Complete(r)
}
```

```go
func (r *DRPCReconciler) setOwnerReference(
    view *multiclusterv1alpha1.ProtectedApplicationView,
    drpc *ramendrv1alpha1.DRPlacementControl,
) error {
    // Owner = DRPC for ALL types
    // DRPC represents the protection boundary
    // When DRPC deleted (protection removed) → view deleted
    view.OwnerReferences = []metav1.OwnerReference{
        {
            APIVersion:         drpc.APIVersion,
            Kind:               drpc.Kind,
            Name:               drpc.Name,
            UID:                drpc.UID,
            Controller:         pointer.Bool(true),
            BlockOwnerDeletion: pointer.Bool(true),
        },
    }
    
    return nil
}
```

The `EnqueueRequestsFromMapFunc` handlers are where the reverse mapping happens. When a DRPC changes, we need to find which ApplicationSets are affected:

```go
func (r *DRPCReconciler) findDRPCsForApplicationSet(
    ctx context.Context,
    appSet client.Object,
) []reconcile.Request {
    // Find Placement this ApplicationSet uses
    placement := r.extractPlacementFromApplicationSet(appSet)
    if placement == nil {
        return nil
    }
    
    // Find DRPC that references this Placement
    drpc := r.findDRPCByPlacement(ctx, placement)
    if drpc == nil {
        return nil
    }
    
    return []reconcile.Request{{
        NamespacedName: types.NamespacedName{
            Name:      drpc.Name,
            Namespace: drpc.Namespace,
        },
    }}
}
```

This reverse mapping is more efficient in the controller than in the frontend because the controller maintains persistent state and can build indexes. The controller can also make intelligent decisions about when to reconcile based on what actually changed.

### 4.3 Benefits of the Controller Approach

The performance benefits are substantial and measurable. The frontend goes from establishing fifteen watch connections to establishing one. This reduces API server load and network traffic proportionally.

The correlation logic runs once in the controller when resources change, not continuously in the browser. The controller can maintain efficient indexes and caches that persist across reconciliations. When a DRPC status updates, the controller updates only the affected ProtectedApplicationView resources, and the frontend receives only those specific updates.

We estimate page load time will drop from the current five to twelve seconds for all application types down to under one second, because the frontend just watches and displays pre-correlated data. Filtering and searching become instantaneous because they operate on the smaller set of view resources rather than the full set of source resources.


## 5. Conclusion

The controller-based aggregation approach with a single DRPC controller addresses all identified limitations while maintaining operational simplicity. By using DRPC as the source of truth and performing reverse mapping to applications, we achieve:

**Performance:** Sub-second page loads (from 5-12 seconds), single watch (from 10-20), instant filtering

**Simplicity:** One controller, one CRD, clear 1:1 DRPC-to-row mapping, reusable frontend patterns

**Correctness:** Strong consistency, automatic cleanup, no race conditions, proper owner references

**Maintainability:** Centralized logic, standard Kubernetes patterns, easy testing with envtest

This design positions the Protected Applications page to scale to hundreds of applications across all three types while providing a superior user experience for disaster recovery operations. The 3-week implementation timeline with incremental rollout minimizes risk and allows for validation at each stage.
