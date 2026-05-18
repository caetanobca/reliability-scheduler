package reliabilityscheduler

import (
	"context"
	"fmt"

	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/scheduler/framework"
)

const (
	// ErrReasonTargetSpreadNotMet is the reason for filtering when target spread is not met
	ErrReasonTargetSpreadNotMet = "target spread not met, node already has pods from this application"
)

// Filter is called during the filtering phase.
// It enforces the spread policy based on the target spread calculated in PreFilter.
func (rs *ReliabilityScheduler) Filter(ctx context.Context, state *framework.CycleState, pod *v1.Pod, nodeInfo *framework.NodeInfo) *framework.Status {
	if nodeInfo.Node() == nil {
		klog.ErrorS(nil, "Node not found", "pod", klog.KObj(pod))
		return framework.NewStatus(framework.Error, "node not found")
	}

	node := nodeInfo.Node()
	nodeName := node.Name

	// Retrieve the cycle state from PreFilter
	cycleState, err := getPreFilterState(state)
	if err != nil {
		klog.ErrorS(err, "Failed to get PreFilter state in Filter", "pod", klog.KObj(pod), "node", nodeName)
		// If PreFilter state is not available, skip filtering
		return framework.NewStatus(framework.Success, "")
	}

	// Get pod count on this node
	podCountOnNode := 0
	hasAppPods := false
	if count, exists := cycleState.NodesWithAppPods[nodeName]; exists {
		podCountOnNode = count
		hasAppPods = count > 0
	}

	// Check if we can schedule on any node
	// If current spread >= target spread, allow scheduling on any node
	if cycleState.CanScheduleOnAnyNode {
		klog.V(5).InfoS("Filter: spread target met, allowing any node",
			"pod", klog.KObj(pod),
			"node", nodeName,
			"currentSpread", cycleState.CurrentSpread,
			"targetSpread", cycleState.TargetSpread,
			"podsOnNode", podCountOnNode,
			"decision", "ALLOW")
		return framework.NewStatus(framework.Success, "")
	}

	// Current spread < target spread
	// Only allow scheduling on nodes that DON'T have pods from this application
	if hasAppPods {
		klog.V(4).InfoS("Filter: spread target not met, rejecting node with existing pods",
			"pod", klog.KObj(pod),
			"node", nodeName,
			"currentSpread", cycleState.CurrentSpread,
			"targetSpread", cycleState.TargetSpread,
			"podsOnNode", podCountOnNode,
			"decision", "REJECT")
		return framework.NewStatus(
			framework.Unschedulable,
			fmt.Sprintf("%s (current: %.4f, target: %.4f)",
				ErrReasonTargetSpreadNotMet,
				cycleState.CurrentSpread,
				cycleState.TargetSpread),
		)
	}

	// Node doesn't have pods from this application, allow scheduling
	klog.V(5).InfoS("Filter: spread target not met, allowing node without existing pods",
		"pod", klog.KObj(pod),
		"node", nodeName,
		"currentSpread", cycleState.CurrentSpread,
		"targetSpread", cycleState.TargetSpread,
		"podsOnNode", podCountOnNode,
		"decision", "ALLOW")
	return framework.NewStatus(framework.Success, "")
}
