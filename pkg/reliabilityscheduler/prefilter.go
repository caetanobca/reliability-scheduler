package reliabilityscheduler

import (
	"context"
	"fmt"
	"strconv"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/scheduler/framework"
)

const (
	preFilterStateKey = "PreFilter-" + Name

	// Annotations for pod configuration
	AnnotationMinAvailability = "reliability.scheduler/min-availability"

	// Label selector for application grouping
	LabelAppName = "app"

	EPSLON = 1e-6
)

// PreFilter is called at the beginning of the scheduling cycle.
// It calculates the target spread and current spread for the application.
func (rs *ReliabilityScheduler) PreFilter(ctx context.Context, state *framework.CycleState, pod *v1.Pod) (*framework.PreFilterResult, *framework.Status) {
	klog.InfoS("ReliabilityScheduler PreFilter started", "pod", klog.KObj(pod))

	// Get minimum availability from pod annotations
	minAvailability, err := getMinimumAvailability(pod)
	if err != nil {
		klog.ErrorS(err, "Failed to get minimum availability", "pod", klog.KObj(pod))
		return nil, framework.NewStatus(framework.Error, fmt.Sprintf("failed to get minimum availability: %v", err))
	}
	klog.V(4).InfoS("Retrieved min availability", "pod", klog.KObj(pod), "minAvailability", minAvailability)

	// Get hour per failure metric from Prometheus (with cache and fallback)
	hourPerFailure := rs.metricsProvider.GetHourPerFailure(ctx, pod)
	klog.V(4).InfoS("Retrieved hour per failure", "pod", klog.KObj(pod), "hourPerFailure", hourPerFailure)

	// Get application selector from pod labels
	appSelector, err := getAppSelector(pod)
	if err != nil {
		klog.ErrorS(err, "Failed to get app selector", "pod", klog.KObj(pod))
		return nil, framework.NewStatus(framework.Error, fmt.Sprintf("failed to get app selector: %v", err))
	}
	klog.V(4).InfoS("Retrieved app selector", "pod", klog.KObj(pod), "appSelector", appSelector)

	// Get all nodes from the cluster
	nodeInfos, err := rs.handle.SnapshotSharedLister().NodeInfos().List()
	if err != nil {
		klog.ErrorS(err, "Failed to list nodes", "pod", klog.KObj(pod))
		return nil, framework.NewStatus(framework.Error, fmt.Sprintf("failed to list nodes: %v", err))
	}

	totalNodes := len(nodeInfos)
	klog.V(4).InfoS("Retrieved cluster nodes", "pod", klog.KObj(pod), "totalNodes", totalNodes)

	// Get all pods of the same application from the node snapshot
	appPods, nodesWithAppPods := rs.getApplicationPods(ctx, appSelector, nodeInfos)

	// Get the desired replica count from the pod's owner (Deployment/ReplicaSet/StatefulSet)
	desiredReplicas := rs.getDesiredReplicas(ctx, pod)

	// Use desired replicas if available, otherwise count existing pods
	totalAppPods := desiredReplicas
	if totalAppPods == 0 {
		// Fallback: use existing pods count if we can't determine desired replicas
		totalAppPods = len(appPods)
		if totalAppPods == 0 {
			totalAppPods = 1 // At least the current pod being scheduled
		}
	}

	klog.InfoS("Retrieved application pods",
		"pod", klog.KObj(pod),
		"appSelector", appSelector,
		"desiredReplicas", desiredReplicas,
		"totalAppPodsFound", len(appPods),
		"totalAppPodsUsed", totalAppPods,
		"nodesWithPods", len(nodesWithAppPods),
		"podDistribution", nodesWithAppPods)

	// Calculate current spread
	// CurrentSpread = (nodes with >= 1 pod) / (total pods of app)
	// Use desired replicas from owner, not just pods that exist
	nodesWithPods := len(nodesWithAppPods)

	var currentSpread float64
	if totalAppPods > 0 {
		currentSpread = float64(nodesWithPods) / float64(totalAppPods)
	} else {
		// First pod of the application (should not happen with +1 above)
		currentSpread = 0.0
	}

	klog.InfoS("Calculated current spread",
		"pod", klog.KObj(pod),
		"totalAppPods", totalAppPods,
		"nodesWithPods", nodesWithPods,
		"currentSpread", currentSpread)

	// Calculate target spread using linear regression model
	targetSpread := rs.computeTargetSpread(minAvailability, hourPerFailure, totalNodes, totalAppPods)

	klog.InfoS("Calculated target spread using linear regression",
		"pod", klog.KObj(pod),
		"intercept", rs.args.Intercept,
		"spreadWeight", rs.args.SpreadWeight,
		"hourPerFailureWeight", rs.args.HourPerFailureWeight,
		"totalNodesWeight", rs.args.TotalNodesWeight,
		"targetSpread", targetSpread)

	// Determine if we can schedule on any node
	canScheduleOnAnyNode := currentSpread >= targetSpread

	klog.InfoS("PreFilter decision",
		"pod", klog.KObj(pod),
		"currentSpread", currentSpread,
		"targetSpread", targetSpread,
		"canScheduleOnAnyNode", canScheduleOnAnyNode,
		"spreadStatus", fmt.Sprintf("%.4f/%.4f", currentSpread, targetSpread))

	// Initialize cycle state
	cycleState := &CycleState{
		AppSelector:          appSelector,
		MinimumAvailability:  minAvailability,
		HourPerFailure:       hourPerFailure,
		TotalNodes:           totalNodes,
		TargetSpread:         targetSpread,
		CurrentSpread:        currentSpread,
		TotalAppPods:         totalAppPods,
		NodesWithAppPods:     nodesWithAppPods,
		CanScheduleOnAnyNode: canScheduleOnAnyNode,
	}

	// Store the cycle state for use in Filter and Score phases
	state.Write(preFilterStateKey, cycleState)

	return nil, framework.NewStatus(framework.Success, "")
}

// PreFilterExtensions returns prefilter extensions, pod add and remove.
func (rs *ReliabilityScheduler) PreFilterExtensions() framework.PreFilterExtensions {
	return rs
}

// AddPod is called when a pod is added during scheduling.
func (rs *ReliabilityScheduler) AddPod(ctx context.Context, state *framework.CycleState, podToSchedule *v1.Pod,
	podInfoToAdd *framework.PodInfo, nodeInfo *framework.NodeInfo) *framework.Status {

	// Retrieve the cycle state
	cycleState, err := getPreFilterState(state)
	if err != nil {
		return framework.AsStatus(err)
	}

	// Check if the added pod belongs to the same application
	if !podBelongsToApp(podInfoToAdd.Pod, cycleState.AppSelector) {
		return framework.NewStatus(framework.Success, "")
	}

	// Update node count for this application
	if nodeInfo.Node() != nil {
		nodeName := nodeInfo.Node().Name
		cycleState.NodesWithAppPods[nodeName]++

		// Recalculate current spread
		cycleState.TotalAppPods++
		nodesWithPods := len(cycleState.NodesWithAppPods)
		if cycleState.TotalAppPods > 0 {
			cycleState.CurrentSpread = float64(nodesWithPods) / float64(cycleState.TotalAppPods)
		}

		// Update scheduling decision
		cycleState.CanScheduleOnAnyNode = cycleState.CurrentSpread >= cycleState.TargetSpread
	}

	return framework.NewStatus(framework.Success, "")
}

// RemovePod is called when a pod is removed during scheduling.
func (rs *ReliabilityScheduler) RemovePod(ctx context.Context, state *framework.CycleState, podToSchedule *v1.Pod,
	podInfoToRemove *framework.PodInfo, nodeInfo *framework.NodeInfo) *framework.Status {

	// Retrieve the cycle state
	cycleState, err := getPreFilterState(state)
	if err != nil {
		return framework.AsStatus(err)
	}

	// Check if the removed pod belongs to the same application
	if !podBelongsToApp(podInfoToRemove.Pod, cycleState.AppSelector) {
		return framework.NewStatus(framework.Success, "")
	}

	// Update node count for this application
	if nodeInfo.Node() != nil {
		nodeName := nodeInfo.Node().Name
		if count, exists := cycleState.NodesWithAppPods[nodeName]; exists {
			if count > 1 {
				cycleState.NodesWithAppPods[nodeName]--
			} else {
				delete(cycleState.NodesWithAppPods, nodeName)
			}
		}

		// Recalculate current spread
		cycleState.TotalAppPods--
		nodesWithPods := len(cycleState.NodesWithAppPods)
		if cycleState.TotalAppPods > 0 {
			cycleState.CurrentSpread = float64(nodesWithPods) / float64(cycleState.TotalAppPods)
		} else {
			cycleState.CurrentSpread = 0.0
		}

		// Update scheduling decision
		cycleState.CanScheduleOnAnyNode = cycleState.CurrentSpread >= cycleState.TargetSpread
	}

	return framework.NewStatus(framework.Success, "")
}

func (rs *ReliabilityScheduler) computeTargetSpread(minAvailability float64, hourPerFailure float64, totalNodes int, appSize int) float64 {

	// minAvailabilityApp := math.Log((minAvailability + EPSLON) / (1.0 - minAvailability + EPSLON))

	interceptApp := getIntercept(appSize, rs.args.InterceptSmallApps, rs.args.Intercept)
	spreadWeightApp := getSpreadWeight(appSize, rs.args.SpreadSmallApps, rs.args.SpreadWeight)

	targetSpread := (minAvailability - hourPerFailure*rs.args.HourPerFailureWeight - float64(totalNodes)*rs.args.TotalNodesWeight - interceptApp) / spreadWeightApp

	switch {
	case targetSpread < 0.0:
		targetSpread = 0.0
	case targetSpread > 1.0:
		targetSpread = 1.0
	}
	return targetSpread
}

func getIntercept(totalAppPods int, interceptSmallApps float64, intercept float64) float64 {
	if totalAppPods <= 10 {
		return intercept + interceptSmallApps // Normal
	}
	return intercept // Big
}

func getSpreadWeight(appSize int, spreadSmallApps float64, spreadWeight float64) float64 {
	if appSize <= 10 {
		return spreadWeight + spreadSmallApps // Normal
	}
	return spreadWeight // Big
}

// getMinimumAvailability extracts the minimum availability requirement from pod annotations.
func getMinimumAvailability(pod *v1.Pod) (float64, error) {
	if pod.Annotations == nil {
		return 0.0, fmt.Errorf("pod has no annotations")
	}

	minAvailStr, exists := pod.Annotations[AnnotationMinAvailability]
	if !exists {
		return 0.0, fmt.Errorf("annotation %s not found", AnnotationMinAvailability)
	}

	minAvail, err := strconv.ParseFloat(minAvailStr, 64)
	if err != nil {
		return 0.0, fmt.Errorf("failed to parse min availability: %w", err)
	}

	return minAvail, nil
}

// getAppSelector extracts the application label selector from pod labels.
func getAppSelector(pod *v1.Pod) (string, error) {
	if pod.Labels == nil {
		return "", fmt.Errorf("pod has no labels")
	}

	appName, exists := pod.Labels[LabelAppName]
	if !exists {
		return "", fmt.Errorf("label %s not found", LabelAppName)
	}

	return appName, nil
}

// getApplicationPods retrieves all pods of the same application and their distribution across nodes.
// IMPORTANT: Uses BOTH SharedInformerFactory AND nodeInfos to get complete picture:
// - SharedInformerFactory: gets ALL pods including Pending without NodeName (for total count)
// - nodeInfos: gets pods that scheduler has "assumed" (decided placement but not yet bound)
// This ensures we see pods being scheduled in the current cycle, not just already-bound pods.
func (rs *ReliabilityScheduler) getApplicationPods(ctx context.Context, appSelector string, nodeInfos []*framework.NodeInfo) ([]*v1.Pod, map[string]int) {
	nodesWithPods := make(map[string]int)
	allPods := make([]*v1.Pod, 0)
	seenPods := make(map[string]bool) // Track pods we've already counted

	// STEP 1: Get ALL pods from informer cache for total count
	podLister := rs.handle.SharedInformerFactory().Core().V1().Pods().Lister()
	allClusterPods, err := podLister.List(labels.Everything())
	if err != nil {
		// Fallback to nodeInfos if lister fails
		return rs.getApplicationPodsFromNodeInfos(appSelector, nodeInfos)
	}

	// Count all pods of this app (for total count)
	for _, pod := range allClusterPods {
		if !podBelongsToApp(pod, appSelector) {
			continue
		}
		if pod.Status.Phase != v1.PodSucceeded && pod.Status.Phase != v1.PodFailed {
			allPods = append(allPods, pod)
			seenPods[string(pod.UID)] = true
		}
	}

	// STEP 2: Get pods from nodeInfos (includes "assumed" pods - pods being scheduled right now)
	// This is CRITICAL for correct spread calculation during concurrent scheduling
	for _, nodeInfo := range nodeInfos {
		if nodeInfo.Node() == nil {
			continue
		}
		nodeName := nodeInfo.Node().Name

		for _, podInfo := range nodeInfo.Pods {
			pod := podInfo.Pod
			if !podBelongsToApp(pod, appSelector) {
				continue
			}
			if pod.Status.Phase == v1.PodSucceeded || pod.Status.Phase == v1.PodFailed {
				continue
			}

			// Count this pod on this node
			nodesWithPods[nodeName]++

			// Add to allPods if we haven't seen it yet (it might be "assumed" and not in informer yet)
			if !seenPods[string(pod.UID)] {
				allPods = append(allPods, pod)
				seenPods[string(pod.UID)] = true
				klog.V(5).InfoS("Found assumed pod in nodeInfos",
					"pod", klog.KObj(pod),
					"nodeName", nodeName,
					"phase", pod.Status.Phase)
			}

			klog.V(5).InfoS("Counting pod on node",
				"pod", klog.KObj(pod),
				"nodeName", nodeName,
				"phase", pod.Status.Phase,
				"appSelector", appSelector)
		}
	}

	klog.V(4).InfoS("Application pods summary",
		"appSelector", appSelector,
		"totalPodsFound", len(allPods),
		"nodesWithPods", nodesWithPods,
		"uniqueNodesCount", len(nodesWithPods))

	return allPods, nodesWithPods
}

// getApplicationPodsFromNodeInfos is a fallback method that uses nodeInfos snapshot.
// This is less accurate because it misses Pending pods without NodeName.
func (rs *ReliabilityScheduler) getApplicationPodsFromNodeInfos(appSelector string, nodeInfos []*framework.NodeInfo) ([]*v1.Pod, map[string]int) {
	nodesWithPods := make(map[string]int)
	allPods := make([]*v1.Pod, 0)

	// Iterate through all nodes in the snapshot
	for _, nodeInfo := range nodeInfos {
		if nodeInfo.Node() == nil {
			continue
		}

		nodeName := nodeInfo.Node().Name

		// Iterate through all pods on this node
		for _, podInfo := range nodeInfo.Pods {
			pod := podInfo.Pod

			// Check if pod belongs to this application
			if !podBelongsToApp(pod, appSelector) {
				continue
			}

			// Include all non-terminated pods
			if pod.Status.Phase != v1.PodSucceeded && pod.Status.Phase != v1.PodFailed {
				allPods = append(allPods, pod)
				if pod.Spec.NodeName != "" {
					nodesWithPods[nodeName]++
				}
			}
		}
	}

	return allPods, nodesWithPods
}

// podBelongsToApp checks if a pod belongs to the specified application.
func podBelongsToApp(pod *v1.Pod, appSelector string) bool {
	if pod.Labels == nil {
		return false
	}

	appName, exists := pod.Labels[LabelAppName]
	return exists && appName == appSelector
}

// getDesiredReplicas retrieves the desired replica count from the pod's owner.
// Supports Deployment, ReplicaSet, StatefulSet, DaemonSet, Job, and CronJob.
func (rs *ReliabilityScheduler) getDesiredReplicas(ctx context.Context, pod *v1.Pod) int {
	// Check if pod has owner references
	if len(pod.OwnerReferences) == 0 {
		klog.V(5).InfoS("Pod has no owner references", "pod", klog.KObj(pod))
		return 0
	}

	// Find the owner reference (typically ReplicaSet for Deployment pods)
	for _, ownerRef := range pod.OwnerReferences {
		if ownerRef.Controller != nil && *ownerRef.Controller {
			replicas := rs.getReplicasFromOwner(ctx, pod.Namespace, ownerRef)
			if replicas > 0 {
				klog.V(4).InfoS("Found desired replicas from owner",
					"pod", klog.KObj(pod),
					"ownerKind", ownerRef.Kind,
					"ownerName", ownerRef.Name,
					"replicas", replicas)
				return replicas
			}
		}
	}

	return 0
}

// getReplicasFromOwner retrieves the replica count from the owner resource.
func (rs *ReliabilityScheduler) getReplicasFromOwner(ctx context.Context, namespace string, ownerRef metav1.OwnerReference) int {
	switch ownerRef.Kind {
	case "ReplicaSet":
		return rs.getReplicasFromReplicaSet(ctx, namespace, ownerRef.Name)
	case "Deployment":
		return rs.getReplicasFromDeployment(ctx, namespace, ownerRef.Name)
	case "StatefulSet":
		return rs.getReplicasFromStatefulSet(ctx, namespace, ownerRef.Name)
	default:
		klog.V(5).InfoS("Unsupported owner kind", "kind", ownerRef.Kind, "name", ownerRef.Name)
		return 0
	}
}

// getReplicasFromReplicaSet retrieves replicas from a ReplicaSet.
func (rs *ReliabilityScheduler) getReplicasFromReplicaSet(ctx context.Context, namespace, name string) int {
	rsLister := rs.handle.SharedInformerFactory().Apps().V1().ReplicaSets().Lister()
	replicaSet, err := rsLister.ReplicaSets(namespace).Get(name)
	if err != nil {
		klog.V(5).InfoS("Failed to get ReplicaSet", "namespace", namespace, "name", name, "error", err)
		return 0
	}

	if replicaSet.Spec.Replicas != nil {
		return int(*replicaSet.Spec.Replicas)
	}

	return 0
}

// getReplicasFromDeployment retrieves replicas from a Deployment.
func (rs *ReliabilityScheduler) getReplicasFromDeployment(ctx context.Context, namespace, name string) int {
	deployLister := rs.handle.SharedInformerFactory().Apps().V1().Deployments().Lister()
	deployment, err := deployLister.Deployments(namespace).Get(name)
	if err != nil {
		klog.V(5).InfoS("Failed to get Deployment", "namespace", namespace, "name", name, "error", err)
		return 0
	}

	if deployment.Spec.Replicas != nil {
		return int(*deployment.Spec.Replicas)
	}

	return 0
}

// getReplicasFromStatefulSet retrieves replicas from a StatefulSet.
func (rs *ReliabilityScheduler) getReplicasFromStatefulSet(ctx context.Context, namespace, name string) int {
	ssLister := rs.handle.SharedInformerFactory().Apps().V1().StatefulSets().Lister()
	statefulSet, err := ssLister.StatefulSets(namespace).Get(name)
	if err != nil {
		klog.V(5).InfoS("Failed to get StatefulSet", "namespace", namespace, "name", name, "error", err)
		return 0
	}

	if statefulSet.Spec.Replicas != nil {
		return int(*statefulSet.Spec.Replicas)
	}

	return 0
}

// getPreFilterState retrieves the PreFilter state from the cycle state.
func getPreFilterState(state *framework.CycleState) (*CycleState, error) {
	data, err := state.Read(preFilterStateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to read prefilter state: %w", err)
	}

	cycleState, ok := data.(*CycleState)
	if !ok {
		return nil, fmt.Errorf("unable to convert state to CycleState")
	}

	return cycleState, nil
}
