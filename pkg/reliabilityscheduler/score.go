package reliabilityscheduler

import (
	"context"
	"fmt"

	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/scheduler/framework"
)

const (
	// MaxScore is the maximum score a node can receive
	MaxScore = int64(framework.MaxNodeScore)

	// MinScore is the minimum score a node can receive
	MinScore = int64(framework.MinNodeScore)
)

// Score is called during the scoring phase.
// It assigns higher scores to nodes with fewer pods from the same application,
// promoting better spread across the cluster.
func (rs *ReliabilityScheduler) Score(ctx context.Context, state *framework.CycleState, pod *v1.Pod, nodeName string) (int64, *framework.Status) {
	// Retrieve the cycle state from PreFilter
	cycleState, err := getPreFilterState(state)
	if err != nil {
		klog.ErrorS(err, "Failed to get PreFilter state in Score", "pod", klog.KObj(pod), "node", nodeName)
		// If PreFilter state is not available, return neutral score
		return MinScore, framework.NewStatus(framework.Error, fmt.Sprintf("failed to get prefilter state: %v", err))
	}

	// Get the number of pods from this application on the current node
	podCountOnNode := 0
	if count, exists := cycleState.NodesWithAppPods[nodeName]; exists {
		podCountOnNode = count
	}

	// Get the total number of pods for this application
	totalPodsOfApp := cycleState.TotalAppPods

	// Calculate score: nodes with fewer pods get higher scores
	// Score formula: (1 - (podCountOnNode / totalPodsOfApp)) * MaxScore
	// This represents the inverse of the proportion of pods on this node
	var score int64
	var ratio float64
	if totalPodsOfApp == 0 {
		// No pods exist yet (first pod of the application), all nodes get max score
		score = MaxScore
		ratio = 0.0
	} else {
		// Calculate proportional score based on total pods
		// Score decreases as the proportion of pods on this node increases
		ratio = float64(podCountOnNode) / float64(totalPodsOfApp)
		score = int64((1.0 - ratio) * float64(MaxScore))
	}

	// Ensure score is within valid range
	originalScore := score
	if score < MinScore {
		score = MinScore
	}
	if score > MaxScore {
		score = MaxScore
	}

	klog.V(5).InfoS("Score calculated",
		"pod", klog.KObj(pod),
		"node", nodeName,
		"podCountOnNode", podCountOnNode,
		"totalPodsOfApp", totalPodsOfApp,
		"ratio", ratio,
		"calculatedScore", originalScore,
		"finalScore", score,
		"maxScore", MaxScore)

	return score, framework.NewStatus(framework.Success, "")
}

// ScoreExtensions returns a ScoreExtensions interface if it implements one, or nil if not.
func (rs *ReliabilityScheduler) ScoreExtensions() framework.ScoreExtensions {
	return rs
}
