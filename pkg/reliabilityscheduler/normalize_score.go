package reliabilityscheduler

import (
	"context"
	"math"

	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/scheduler/framework"
)

// NormalizeScore is called after the Score phase to normalize the scores.
// It normalizes the scores of all nodes to a standard range [MinNodeScore, MaxNodeScore].
// This ensures that scores from different plugins are comparable.
func (rs *ReliabilityScheduler) NormalizeScore(ctx context.Context, state *framework.CycleState, pod *v1.Pod, scores framework.NodeScoreList) *framework.Status {
	klog.V(4).InfoS("NormalizeScore called", "pod", klog.KObj(pod), "nodeCount", len(scores))

	if len(scores) == 0 {
		return framework.NewStatus(framework.Success, "")
	}

	// Find the minimum and maximum scores
	var minScore, maxScore int64 = math.MaxInt64, math.MinInt64
	for _, score := range scores {
		if score.Score < minScore {
			minScore = score.Score
		}
		if score.Score > maxScore {
			maxScore = score.Score
		}
	}

	klog.V(5).InfoS("Score range before normalization",
		"pod", klog.KObj(pod),
		"minScore", minScore,
		"maxScore", maxScore)

	// If all scores are the same, set them all to the maximum score
	if minScore == maxScore {
		klog.V(5).InfoS("All scores equal, setting to max", "pod", klog.KObj(pod), "score", MaxScore)
		for i := range scores {
			scores[i].Score = MaxScore
		}
		return framework.NewStatus(framework.Success, "")
	}

	// Normalize scores to [MinNodeScore, MaxNodeScore] range
	scoreRange := maxScore - minScore
	for i := range scores {
		originalScore := scores[i].Score
		// Linear normalization: normalize score to [0, 1] then scale to [MinNodeScore, MaxNodeScore]
		normalizedScore := float64(scores[i].Score-minScore) / float64(scoreRange)
		scores[i].Score = MinScore + int64(normalizedScore*float64(MaxScore-MinScore))

		klog.V(6).InfoS("Score normalized",
			"pod", klog.KObj(pod),
			"node", scores[i].Name,
			"originalScore", originalScore,
			"normalizedScore", scores[i].Score)
	}

	// Log final scores summary
	klog.V(4).InfoS("Final normalized scores", "pod", klog.KObj(pod), "scores", scores)

	return framework.NewStatus(framework.Success, "")
}
