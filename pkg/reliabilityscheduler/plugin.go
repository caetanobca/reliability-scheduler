package reliabilityscheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/scheduler/framework"
)

const (
	// Name is the name of the plugin used in the plugin registry and configurations.
	Name = "ReliabilityScheduler"

	// Environment variables for linear regression coefficients
	EnvIntercept            = "RELIABILITY_SCHEDULER_INTERCEPT"
	EnvSpreadWeight         = "RELIABILITY_SCHEDULER_SPREAD_WEIGHT"
	EnvHourPerFailureWeight = "RELIABILITY_SCHEDULER_HOUR_PER_FAILURE_WEIGHT"
	EnvTotalNodesWeight     = "RELIABILITY_SCHEDULER_TOTAL_NODES_WEIGHT"
	EnvSpreadSmallApps    = "RELIABILITY_SCHEDULER_SPREAD_SMALL_APPS"
	EnvInterceptSmallApps = "RELIABILITY_SCHEDULER_INTERCEPT_SMALL_APPS"
)

var (
	// SchemeGroupVersion is group version used to register these objects
	SchemeGroupVersion = schema.GroupVersion{Group: "kubescheduler.config.k8s.io", Version: "v1"}

	// Scheme is the scheme for plugin configuration
	Scheme = runtime.NewScheme()
)

func init() {
	// Register types with the scheme
	AddToScheme(Scheme)
}

// AddToScheme adds all types of this package to the given scheme.
func AddToScheme(scheme *runtime.Scheme) error {
	scheme.AddKnownTypes(SchemeGroupVersion, &ReliabilitySchedulerArgs{})
	return nil
}

// ReliabilityScheduler is a plugin that implements PreFilter, Filter, Score, and NormalizeScore extensions.
type ReliabilityScheduler struct {
	handle          framework.Handle
	args            *ReliabilitySchedulerArgs
	metricsProvider *MetricsProvider
}

// Name returns the name of the plugin.
func (rs *ReliabilityScheduler) Name() string {
	return Name
}

// New initializes a new plugin and returns it.
func New(ctx context.Context, obj runtime.Object, h framework.Handle) (framework.Plugin, error) {
	// Create default args
	args := &ReliabilitySchedulerArgs{}

	// Try to convert the obj to our args type
	if obj != nil {
		klog.V(4).InfoS("ReliabilityScheduler New called", "objType", fmt.Sprintf("%T", obj))

		var ok bool
		args, ok = obj.(*ReliabilitySchedulerArgs)
		if !ok {
			// If obj is runtime.Unknown, try to extract and unmarshal
			if unknown, isUnknown := obj.(*runtime.Unknown); isUnknown {
				klog.V(4).InfoS("Received runtime.Unknown", "rawLen", len(unknown.Raw), "contentType", unknown.ContentType)

				// Try to marshal the entire object and unmarshal to our type
				// This works around the nil Raw field issue
				data, err := json.Marshal(obj)
				if err != nil {
					return nil, fmt.Errorf("failed to marshal Unknown object: %w", err)
				}

				klog.V(4).InfoS("Marshaled object", "data", string(data))

				if err := json.Unmarshal(data, args); err != nil {
					klog.ErrorS(err, "Failed to unmarshal to ReliabilitySchedulerArgs")
					// Use default args instead of failing
					klog.InfoS("Using default args due to unmarshal error")
					args = &ReliabilitySchedulerArgs{}
				}
			} else {
				return nil, fmt.Errorf("want args to be of type ReliabilitySchedulerArgs, got %T", obj)
			}
		}
	}

	klog.InfoS("ReliabilityScheduler initialized",
		"intercept", args.Intercept,
		"spreadWeight", args.SpreadWeight,
		"hourPerFailureWeight", args.HourPerFailureWeight,
		"totalNodesWeight", args.TotalNodesWeight)

	// Load coefficients from environment variables if available
	// Environment variables take precedence over config args
	if err := loadCoefficientsFromEnv(args); err != nil {
		return nil, fmt.Errorf("failed to load coefficients from environment: %w", err)
	}

	klog.InfoS("ReliabilityScheduler after env loading",
		"intercept", args.Intercept,
		"spreadWeight", args.SpreadWeight,
		"hourPerFailureWeight", args.HourPerFailureWeight,
		"totalNodesWeight", args.TotalNodesWeight)

	// Initialize MetricsProvider for Prometheus integration
	metricsProvider, err := NewMetricsProvider()
	if err != nil {
		return nil, fmt.Errorf("failed to create metrics provider: %w", err)
	}

	return &ReliabilityScheduler{
		handle:          h,
		args:            args,
		metricsProvider: metricsProvider,
	}, nil
}

// loadCoefficientsFromEnv loads linear regression coefficients from environment variables.
// Environment variables take precedence over config args.
func loadCoefficientsFromEnv(args *ReliabilitySchedulerArgs) error {
	// Load Intercept
	if val := os.Getenv(EnvIntercept); val != "" {
		intercept, err := strconv.ParseFloat(val, 64)
		if err != nil {
			return fmt.Errorf("failed to parse %s: %w", EnvIntercept, err)
		}
		args.Intercept = intercept
	}

	// Load MinAvailabilityWeight
	if val := os.Getenv(EnvSpreadWeight); val != "" {
		weight, err := strconv.ParseFloat(val, 64)
		if err != nil {
			return fmt.Errorf("failed to parse %s: %w", EnvSpreadWeight, err)
		}
		args.SpreadWeight = weight
	}

	// Load HourPerFailureWeight
	if val := os.Getenv(EnvHourPerFailureWeight); val != "" {
		weight, err := strconv.ParseFloat(val, 64)
		if err != nil {
			return fmt.Errorf("failed to parse %s: %w", EnvHourPerFailureWeight, err)
		}
		args.HourPerFailureWeight = weight
	}

	// Load TotalNodesWeight
	if val := os.Getenv(EnvTotalNodesWeight); val != "" {
		weight, err := strconv.ParseFloat(val, 64)
		if err != nil {
			return fmt.Errorf("failed to parse %s: %w", EnvTotalNodesWeight, err)
		}
		args.TotalNodesWeight = weight
	}

	if val := os.Getenv(EnvSpreadSmallApps); val != "" {
		weight, err := strconv.ParseFloat(val, 64)
		if err != nil {
			return fmt.Errorf("failed to parse %s: %w", EnvSpreadSmallApps, err)
		}
		args.SpreadSmallApps = weight
	}

	if val := os.Getenv(EnvInterceptSmallApps); val != "" {
		weight, err := strconv.ParseFloat(val, 64)
		if err != nil {
			return fmt.Errorf("failed to parse %s: %w", EnvInterceptSmallApps, err)
		}
		args.InterceptSmallApps = weight
	}

	return nil
}
