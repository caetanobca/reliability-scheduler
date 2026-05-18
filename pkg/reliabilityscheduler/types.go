package reliabilityscheduler

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/kubernetes/pkg/scheduler/framework"
)

// ReliabilitySchedulerArgs holds arguments used to configure the ReliabilityScheduler plugin.
type ReliabilitySchedulerArgs struct {
	metav1.TypeMeta

	// Linear regression coefficients for target spread calculation
	// TargetSpread = Intercept + MinAvailabilityWeight*MinAvailability + HourPerFailureWeight*HourPerFailure + TotalNodesWeight*TotalNodes
	Intercept            float64 `json:"intercept,omitempty"`
	SpreadWeight         float64 `json:"spreadWeight,omitempty"`
	HourPerFailureWeight float64 `json:"hourPerFailureWeight,omitempty"`
	TotalNodesWeight     float64 `json:"totalNodesWeight,omitempty"`
	SpreadSmallApps    float64 `json:"spreadSmallApps,omitempty"`
	InterceptSmallApps float64 `json:"interceptSmallApps,omitempty"`
}

// DeepCopyObject creates a deep copy of ReliabilitySchedulerArgs
func (in *ReliabilitySchedulerArgs) DeepCopyObject() runtime.Object {
	if in == nil {
		return nil
	}
	out := new(ReliabilitySchedulerArgs)
	*out = *in
	out.TypeMeta = in.TypeMeta
	return out
}

// GetObjectKind returns the ObjectKind schema.
func (in *ReliabilitySchedulerArgs) GetObjectKind() schema.ObjectKind {
	return &in.TypeMeta
}

// CycleState is used to store state data for the scheduling cycle.
type CycleState struct {
	// Application identifier (label selector)
	AppSelector string

	// MinimumAvailability required by the application (from pod annotation/label)
	MinimumAvailability float64

	// HourPerFailure metric from last hour
	HourPerFailure float64

	// TotalNodes in the cluster
	TotalNodes int

	// TargetSpread calculated from linear regression model
	TargetSpread float64

	// CurrentSpread = nodes with >=1 pod / total pods of app
	CurrentSpread float64

	// TotalAppPods is the total number of pods for this application
	TotalAppPods int

	// NodesWithAppPods tracks which nodes have pods from this application
	NodesWithAppPods map[string]int // node name -> pod count

	// CanScheduleOnAnyNode indicates if spread target is already met
	CanScheduleOnAnyNode bool
}

// Clone creates a deep copy of CycleState.
// This method is required by framework.StateData interface.
func (s *CycleState) Clone() framework.StateData {
	if s == nil {
		return nil
	}

	nodesWithPods := make(map[string]int, len(s.NodesWithAppPods))
	for k, v := range s.NodesWithAppPods {
		nodesWithPods[k] = v
	}

	return &CycleState{
		AppSelector:          s.AppSelector,
		MinimumAvailability:  s.MinimumAvailability,
		HourPerFailure:       s.HourPerFailure,
		TotalNodes:           s.TotalNodes,
		TargetSpread:         s.TargetSpread,
		CurrentSpread:        s.CurrentSpread,
		TotalAppPods:         s.TotalAppPods,
		NodesWithAppPods:     nodesWithPods,
		CanScheduleOnAnyNode: s.CanScheduleOnAnyNode,
	}
}
