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
React's useMemo re-runs correlation on every resource change:

New ApplicationSet created → correlate all ApplicationSets
Placement status changes → correlate all affected applications
DRPC status updates → correlate all affected applications

The correlation algorithm has O(N*M) complexity (N applications × M DRPCs). With 100 ApplicationSets, 50 Subscriptions, and 200 DRPCs, a single DRPC update triggers tens of thousands of comparisons, causing noticeable UI lag.

#### 3.1.3 Memory Consumption
Each hook stores its own copy of watched resources. A Placement watched by three hooks exists three times in browser memory. Large clusters consume 50+ MB just for application data, contributing to browser crashes.

#### 3.1.4 Lack of Shared Caching
useK8sWatchResources maintains per-component state with no cross-component caching. Navigating between pages recreates all watches and refetches all data. Two components needing the same data (e.g., DRPolicies) independently watch and maintain it.

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

The proposed solution moves the correlation logic from the frontend to the backend, implementing it as Kubernetes controllers that maintain an aggregated view resource. This architectural shift is based on several key principles that address the limitations we've identified.

Rather than having correlation logic scattered across multiple frontend hooks, we create a single authoritative source of truth: a new Custom Resource Definition called ProtectedApplicationView. This CRD represents the aggregated information about a protected application, regardless of its type. Every protected application gets one corresponding ProtectedApplicationView resource, and the frontend simply watches and displays these views.

This approach provides strong consistency guarantees. When a user sees a ProtectedApplicationView resource, they know it reflects the current state of the underlying application and its DR configuration. There are no race conditions between multiple hooks trying to correlate the same data independently.


### 4.2 Architecture Overview

The proposed architecture introduces three new controllers and one new CRD. Let's examine each component and how they work together.

#### 4.2.1 The ProtectedApplicationView CRD

This new custom resource represents the aggregated view of a protected application. Its schema is designed to accommodate all three application types while making the common patterns easy to work with:

```yaml
apiVersion: ramendr.openshift.io/v1alpha1
kind: ProtectedApplicationView
metadata:
  name: appset-payment-service  # Prefixed with type for uniqueness
  namespace: openshift-gitops    # Same namespace as the source application
  
  # Owner reference ensures automatic cleanup
  ownerReferences:
    - apiVersion: argoproj.io/v1alpha1
      kind: ApplicationSet
      name: payment-service
      uid: <applicationset-uid>
      controller: true
      blockOwnerDeletion: true
  
  # Labels enable efficient querying
  labels:
    app.kubernetes.io/type: ApplicationSet
    ramendr.openshift.io/dr-policy: metro-dr-policy
    ramendr.openshift.io/protected: "true"

spec:
  # The type determines which fields are populated
  type: ApplicationSet  # or "Subscription" or "Discovered"
  
  # Reference back to the actual application resource
  applicationRef:
    apiVersion: argoproj.io/v1alpha1
    kind: ApplicationSet
    name: payment-service
    namespace: openshift-gitops
    uid: <applicationset-uid>
  
  # Placement information (common to all types)
  # An application can have multiple placements (common with Subscriptions)
  placements:
    - name: payment-service-placement
      namespace: openshift-gitops
      kind: Placement
      
      # The clusters where this placement has scheduled the application
      selectedClusters:
        - primary-datacenter
      
      numberOfClusters: 1
  
  # Disaster recovery configuration (if protected)
  # This is the most important section for the UI
  drInfo:
    enabled: true
    
    # Reference to the DRPC providing protection
    drpcRef:
      name: payment-service-drpc
      namespace: openshift-gitops
    
    # The DR policy defining the DR configuration
    drPolicyRef:
      name: metro-dr-policy
    
    # The clusters involved in disaster recovery
    drClusters:
      - primary-datacenter
      - secondary-datacenter
    
    # Current primary (active) cluster
    primaryCluster: primary-datacenter
    
    # Namespaces being protected
    protectedNamespaces:
      - payment-service
      - payment-service-config
    
    # Current DR status
    status:
      phase: Relocated
      conditions:
        - type: Available
          status: "True"
          lastTransitionTime: "2025-04-24T06:13:43Z"
          reason: RecoveryCompleted
          message: "Application successfully failed over to secondary datacenter"
    
    # For Subscription applications with multiple placements/DRPCs
    additionalDRPCs:
      - drpcRef:
          name: payment-backend-drpc
          namespace: openshift-gitops
        drPolicyRef:
          name: backup-policy
        primaryCluster: backup-datacenter
  
  # Type-specific information for ApplicationSets
  applicationSetInfo:
    gitRepoURLs:
      - https://github.com/example/payment-service
    targetRevision: main
    syncPolicy: automated
    
    # Sibling ApplicationSets sharing the same Placement
    siblingApplicationSets:
      - payment-service-canary

status:
  # Controller-maintained status
  observedGeneration: 1
  lastSyncTime: "2025-04-24T06:13:43Z"
  
  conditions:
    - type: Ready
      status: "True"
      lastTransitionTime: "2025-04-24T06:13:43Z"
      reason: FullySynchronized
      message: "View is up-to-date with source resources"
```

The key design decisions in this CRD are worth highlighting. First, the owner reference to the source application ensures that when an ApplicationSet is deleted, its corresponding view is automatically garbage collected. This prevents orphaned view resources from accumulating.

Second, the labels enable efficient queries. The frontend can watch only ProtectedApplicationViews with the label `ramendr.openshift.io/protected: "true"`, ensuring we only show protected applications. The type label allows filtering by application type without examining the spec.

Third, the `drInfo.additionalDRPCs` field handles the complex case of Subscription applications with multiple placements. The primary DRPC information is in the main `drInfo` section for easy display in the table, while additional DRPCs are available for drill-down views.

#### 4.2.2 The ApplicationSet Controller

This controller is responsible for watching ApplicationSet resources and creating corresponding ProtectedApplicationView resources when they're protected by a DRPC. Here's the high-level reconciliation logic:

```go
// Reconcile is triggered whenever an ApplicationSet or related resource changes
func (r *ApplicationSetReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    log := log.FromContext(ctx)
    
    // Step 1: Fetch the ApplicationSet that triggered this reconciliation
    appSet := &argov1alpha1.ApplicationSet{}
    if err := r.Get(ctx, req.NamespacedName, appSet); err != nil {
        if apierrors.IsNotFound(err) {
            // ApplicationSet was deleted - clean up the view if it exists
            // (Actually, owner references handle this automatically, but we log it)
            log.Info("ApplicationSet deleted, view will be garbage collected")
            return ctrl.Result{}, nil
        }
        return ctrl.Result{}, err
    }
    
    // Step 2: Find the Placement that this ApplicationSet uses
    // This implements the same logic as findPlacementFromArgoAppSet
    // but on the server side where we can maintain efficient indexes
    placement, err := r.findPlacementForApplicationSet(ctx, appSet)
    if err != nil {
        log.Error(err, "Failed to find Placement for ApplicationSet")
        // Not having a Placement might be temporary, retry later
        return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
    }
    
    // Step 3: Find the DRPC that protects this Placement
    drpc, err := r.findDRPCForPlacement(ctx, placement)
    if err != nil || drpc == nil {
        // No DRPC means this ApplicationSet is not protected
        // Delete the view if it exists (app was unprotected)
        return r.cleanupView(ctx, appSet), nil
    }
    
    // Step 4: Gather additional information needed for the view
    placementDecision, _ := r.findPlacementDecision(ctx, placement)
    drPolicy, drClusters, _ := r.getDRPolicyAndClusters(ctx, drpc)
    siblings, _ := r.findSiblingApplicationSets(ctx, appSet, placement)
    
    // Step 5: Build the ProtectedApplicationView resource
    view := r.buildProtectedApplicationView(
        appSet, placement, placementDecision,
        drpc, drPolicy, drClusters, siblings,
    )
    
    // Step 6: Create or update the view resource
    viewName := fmt.Sprintf("appset-%s", appSet.Name)
    existingView := &ramendrv1alpha1.ProtectedApplicationView{}
    err = r.Get(ctx, client.ObjectKey{
        Name: viewName,
        Namespace: appSet.Namespace,
    }, existingView)
    
    if err != nil && apierrors.IsNotFound(err) {
        // View doesn't exist, create it
        view.Name = viewName
        view.Namespace = appSet.Namespace
        
        // Set owner reference for automatic cleanup
        if err := ctrl.SetControllerReference(appSet, view, r.Scheme); err != nil {
            return ctrl.Result{}, err
        }
        
        if err := r.Create(ctx, view); err != nil {
            log.Error(err, "Failed to create ProtectedApplicationView")
            return ctrl.Result{}, err
        }
        
        log.Info("Created ProtectedApplicationView", "name", viewName)
    } else if err == nil {
        // View exists, update it if changed
        existingView.Spec = view.Spec
        if err := r.Update(ctx, existingView); err != nil {
            log.Error(err, "Failed to update ProtectedApplicationView")
            return ctrl.Result{}, err
        }
        
        log.Info("Updated ProtectedApplicationView", "name", viewName)
    } else {
        // Unexpected error fetching view
        return ctrl.Result{}, err
    }
    
    return ctrl.Result{}, nil
}
```

The controller also needs to watch not just ApplicationSets but also the related resources that might change:

```go
func (r *ApplicationSetReconciler) SetupWithManager(mgr ctrl.Manager) error {
    return ctrl.NewControllerManagedBy(mgr).
        // Primary watch: trigger when ApplicationSets change
        For(&argov1alpha1.ApplicationSet{}).
        
        // Also watch DRPCs - when a DRPC is created or deleted,
        // we need to reconcile affected ApplicationSets
        Watches(
            &ramendrv1alpha1.DRPlacementControl{},
            handler.EnqueueRequestsFromMapFunc(r.findApplicationSetsForDRPC),
        ).
        
        // Watch Placements - when placement cluster selection changes,
        // we need to update the view
        Watches(
            &placementv1beta1.Placement{},
            handler.EnqueueRequestsFromMapFunc(r.findApplicationSetsForPlacement),
        ).
        
        Complete(r)
}
```

The `EnqueueRequestsFromMapFunc` handlers are where the reverse mapping happens. When a DRPC changes, we need to find which ApplicationSets are affected:

```go
func (r *ApplicationSetReconciler) findApplicationSetsForDRPC(
    ctx context.Context,
    drpc client.Object,
) []reconcile.Request {
    // Get the Placement this DRPC references
    placement, err := r.getPlacementFromDRPC(ctx, drpc)
    if err != nil {
        return nil
    }
    
    // Find all ApplicationSets in the same namespace that use this Placement
    // We could optimize this with an index, but for now, list and filter
    appSetList := &argov1alpha1.ApplicationSetList{}
    if err := r.List(ctx, appSetList, client.InNamespace(drpc.GetNamespace())); err != nil {
        return nil
    }
    
    requests := []reconcile.Request{}
    for _, appSet := range appSetList.Items {
        // Check if this ApplicationSet uses the placement
        if r.applicationSetUsesPlacement(appSet, placement) {
            requests = append(requests, reconcile.Request{
                NamespacedName: types.NamespacedName{
                    Name:      appSet.Name,
                    Namespace: appSet.Namespace,
                },
            })
        }
    }
    
    return requests
}
```

This reverse mapping is more efficient in the controller than in the frontend because the controller maintains persistent state and can build indexes. The controller can also make intelligent decisions about when to reconcile based on what actually changed.

#### 4.2.3 The Subscription Controller

The Subscription controller follows a similar pattern but handles the additional complexity of grouping multiple Subscriptions and dealing with multiple placements:

```go
func (r *SubscriptionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    // Fetch the Application resource
    app := &appv1beta1.Application{}
    if err := r.Get(ctx, req.NamespacedName, app); err != nil {
        if apierrors.IsNotFound(err) {
            return ctrl.Result{}, nil
        }
        return ctrl.Result{}, err
    }
    
    // Find all Subscriptions that belong to this Application
    subscriptions, err := r.findSubscriptionsForApplication(ctx, app)
    if err != nil || len(subscriptions) == 0 {
        // No subscriptions means this isn't a Subscription-based application
        return r.cleanupView(ctx, app), nil
    }
    
    // Group Subscriptions by their Placement
    subscriptionGroups := r.groupSubscriptionsByPlacement(ctx, subscriptions)
    
    // Find DRPCs for each Placement group
    drpcs := []*ramendrv1alpha1.DRPlacementControl{}
    for _, group := range subscriptionGroups {
        if drpc, _ := r.findDRPCForPlacement(ctx, group.Placement); drpc != nil {
            drpcs = append(drpcs, drpc)
        }
    }
    
    // Only create a view if at least one DRPC exists (only show protected apps)
    if len(drpcs) == 0 {
        return r.cleanupView(ctx, app), nil
    }
    
    // Build the view with multi-DRPC support
    // The first DRPC is the "primary" shown in main fields
    // Additional DRPCs go in additionalDRPCs array
    view := r.buildProtectedApplicationView(app, subscriptionGroups, drpcs)
    
    // Create or update the view
    // ... similar to ApplicationSet controller
    
    return ctrl.Result{}, nil
}
```

The key difference is in how we handle multiple DRPCs. The view spec includes the primary DRPC information in the main `drInfo` section, and additional DRPCs are listed in `drInfo.additionalDRPCs`. This gives the frontend flexibility in how to display multi-DRPC applications.

#### 4.2.4 The Discovered Controller

The Discovered controller is the simplest because it has a direct one-to-one mapping from DRPC to view:

```go
func (r *DiscoveredReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    // Only process DRPCs in the discovered apps namespace
    if req.Namespace != "openshift-dr-ops" {
        return ctrl.Result{}, nil
    }
    
    // Fetch the DRPC
    drpc := &ramendrv1alpha1.DRPlacementControl{}
    if err := r.Get(ctx, req.NamespacedName, drpc); err != nil {
        if apierrors.IsNotFound(err) {
            return ctrl.Result{}, nil  // View will be garbage collected
        }
        return ctrl.Result{}, err
    }
    
    // Get related resources
    placement, _ := r.getPlacement(ctx, drpc)
    drPolicy, drClusters, _ := r.getDRPolicyAndClusters(ctx, drpc)
    
    // Build the view (simpler than the other types)
    view := r.buildProtectedApplicationView(drpc, placement, drPolicy, drClusters)
    
    // Create or update
    // ... similar pattern
    
    return ctrl.Result{}, nil
}
```

The Discovered controller only watches DRPCs in the `openshift-dr-ops` namespace, so it's very efficient. It doesn't need to do any correlation because the DRPC itself is the application.


### 4.3 Benefits of the Controller Approach


The performance benefits are substantial and measurable. The frontend goes from establishing fifteen watch connections to establishing one. This reduces API server load and network traffic proportionally.

The correlation logic runs once in the controller when resources change, not continuously in the browser. The controller can maintain efficient indexes and caches that persist across reconciliations. When a DRPC status updates, the controller updates only the affected ProtectedApplicationView resources, and the frontend receives only those specific updates.

We estimate page load time will drop from the current five to twelve seconds for all application types down to under one second, because the frontend just watches and displays pre-correlated data. Filtering and searching become instantaneous because they operate on the smaller set of view resources rather than the full set of source resources.


## 5. Conclusion

The controller-based aggregation approach addresses all the identified limitations of the current implementation while maintaining operational simplicity. By moving correlation logic to the backend as Kubernetes controllers, we achieve better performance, simpler frontend code, and a more maintainable architecture.
