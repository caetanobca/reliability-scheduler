# Reliability Scheduler

A Kubernetes scheduler plugin that schedules pods based on application reliability goals, using a linear regression model to compute the optimal pod spread across cluster nodes.

## Table of Contents

- [Description](#description)
- [Architecture](#architecture)
- [Dependencies](#dependencies)
- [Build](#build)
- [Deploy](#deploy)
- [Configuration](#configuration)
- [Usage](#usage)
- [Examples](#examples)
- [Troubleshooting](#troubleshooting)

---

## Description

The Reliability Scheduler is a custom Kubernetes scheduler plugin that allows application operators to declare a minimum availability target per pod. The plugin automatically computes the required spread of pods across nodes to meet that availability target, using a configurable linear regression model trained on historical reliability data.

**Key concepts:**

- **Availability** — fraction of pods currently running: `running_pods / total_pods`
- **Spread** — fraction of nodes hosting at least one pod: `nodes_with_pod / total_pods`
- **Hour-per-Failure** — historical reliability metric: `total_observed_hours / number_of_failures` (a failure is any state where one or more pods are not running)
- **Target Spread** — computed from the linear regression model:
  ```
  TargetSpread = (MinAvailability - HourPerFailureWeight*HourPerFailure - TotalNodesWeight*TotalNodes - Intercept) / SpreadWeight
  ```

The plugin integrates with Prometheus to fetch `hour_per_failure` metrics automatically, with configurable fallback values.

---

## Architecture

The plugin implements four Kubernetes Scheduler Framework extension points, all running inside the same scheduler binary:

```
PreFilter → Filter → Score → NormalizeScore
```

### Extension Points

| Extension | What it does |
|-----------|-------------|
| **PreFilter** | Reads pod annotations and label, queries Prometheus for `hour_per_failure`, counts cluster nodes and app pods, computes `TargetSpread` and `CurrentSpread`, stores state in `CycleState` |
| **Filter** | If `CurrentSpread < TargetSpread`: rejects nodes that already have a pod from this app. If target is met: all nodes are eligible |
| **Score** | Scores each node inversely proportional to its pod density: `score = (1 - podsOnNode / totalPods) × MaxNodeScore` |
| **NormalizeScore** | Normalizes all node scores to `[MinNodeScore, MaxNodeScore]` using linear normalization |

### Communication

All extension points communicate via `framework.CycleState` — a per-scheduling-cycle store written by PreFilter and read by Filter, Score, and NormalizeScore.

The `MetricsProvider` component abstracts Prometheus access with an in-memory cache (configurable TTL, default 5 min). Fallback strategy: valid cache → Prometheus query → stale cache → default value.

### Project Structure

```
scheduler_oficial/
├── cmd/scheduler/main.go              # Entry point
├── pkg/reliabilityscheduler/
│   ├── plugin.go                      # Plugin registration, coefficient loading
│   ├── types.go                       # Data structures (args, cycle state)
│   ├── prefilter.go                   # Target/current spread calculation
│   ├── filter.go                      # Spread enforcement logic
│   ├── score.go                       # Pod density scoring
│   ├── normalize_score.go             # Score normalization
│   └── metrics_provider.go            # Prometheus integration with cache
├── deploy/
│   ├── standalone/                    # Standalone mode manifests
│   └── integrated/                    # Integrated mode manifests (recommended)
├── examples/
│   └── deployments_examples.yaml      # Usage examples
├── Dockerfile
├── Makefile
└── go.mod
```

---

## Dependencies

- **Go** 1.22+
- **Kubernetes** 1.29+
- **Prometheus** (optional) — for automatic `hour_per_failure` metric collection

---

## Build

### Local binary

```bash
go build -o bin/reliability-scheduler ./cmd/scheduler
```

### Docker image

```bash
docker build -t <your-registry>/reliability-scheduler:<version> .
docker push <your-registry>/reliability-scheduler:<version>
```

### Using Make

```bash
# Build and push image
make docker-build docker-push REGISTRY=<your-registry> VERSION=<version>

# Run tests
make test

# Format code
make fmt
```

---

## Deploy

See **[DEPLOYMENT.md](./DEPLOYMENT.md)** for full deployment instructions, including:

- Standalone vs. integrated mode comparison
- Cluster prerequisites
- Step-by-step installation with the manifests in `deploy/`
- Prometheus/ServiceMonitor configuration
- RBAC requirements

---

## Configuration

### Pod Annotations

Pods that should use this scheduler must set `schedulerName` and the required annotation:

| Annotation | Required | Description | Example |
|-----------|----------|-------------|---------|
| `reliability.scheduler/min-availability` | Yes | Minimum desired availability (0.0–1.0) | `"0.95"` |

### Required Labels

| Label | Description |
|-------|-------------|
| `app` | Application identifier — groups pods belonging to the same application |

### Environment Variables

Environment variables take precedence over values in the scheduler ConfigMap.

#### Linear Regression Coefficients

| Variable | Default | Description |
|----------|---------|-------------|
| `RELIABILITY_SCHEDULER_INTERCEPT` | `0` | Intercept of the linear regression |
| `RELIABILITY_SCHEDULER_SPREAD_WEIGHT` | `1` | Coefficient for spread |
| `RELIABILITY_SCHEDULER_HOUR_PER_FAILURE_WEIGHT` | `0` | Coefficient for hour-per-failure metric |
| `RELIABILITY_SCHEDULER_TOTAL_NODES_WEIGHT` | `0` | Coefficient for total node count |
| `RELIABILITY_SCHEDULER_SPREAD_SMALL_APPS` | `0` | `SpreadWeight` adjustment for apps with ≤10 pods (added to base value) |
| `RELIABILITY_SCHEDULER_INTERCEPT_SMALL_APPS` | `0` | `Intercept` adjustment for apps with ≤10 pods (added to base value) |

#### Prometheus Integration

| Variable | Default | Description |
|----------|---------|-------------|
| `PROMETHEUS_URL` | `http://prometheus-k8s.monitoring.svc:9090` | Prometheus service URL |
| `DEFAULT_HOUR_PER_FAILURE` | `0.0` | Fallback value when metric is unavailable (0.0 = neutral, no effect on calculation) |
| `METRICS_CACHE_TTL` | `5m` | Metrics cache TTL (e.g., `1m`, `5m`, `10m`) |
| `METRICS_QUERY_TIMEOUT` | `100ms` | Prometheus query timeout (e.g., `50ms`, `200ms`) |

These variables are typically set via a ConfigMap — see `deploy/<mode>/scheduler-env-configmap.yaml`.

### Scheduler ConfigMap

Coefficients can also be set directly in the scheduler `configmap.yaml`:

```yaml
pluginConfig:
  - name: ReliabilityScheduler
    args:
      intercept: 0.5
      spreadWeight: 0.3
      hourPerFailureWeight: 0.1
      totalNodesWeight: 0.05
```

---

## Usage

### Minimal pod spec

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: my-app
  labels:
    app: my-application          # Required: identifies the application group
  annotations:
    reliability.scheduler/min-availability: "0.95"
spec:
  schedulerName: reliability-scheduler
  containers:
  - name: app
    image: my-app:latest
```

### Scheduler behavior

**When `CurrentSpread < TargetSpread`:**
- Filter: only nodes without pods from this app are eligible
- Score: nodes without pods receive maximum score
- Result: forces spread to new nodes

**When `CurrentSpread >= TargetSpread`:**
- Filter: all nodes are eligible
- Score: nodes with fewer pods receive higher scores
- Result: promotes uniform load balancing

---

## Examples

See [`examples/deployments_examples.yaml`](./examples/deployments_examples.yaml) for complete deployment examples.

### High availability application

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: critical-service
spec:
  replicas: 6
  selector:
    matchLabels:
      app: critical-service
  template:
    metadata:
      labels:
        app: critical-service
      annotations:
        reliability.scheduler/min-availability: "0.99"
    spec:
      schedulerName: reliability-scheduler
      containers:
      - name: app
        image: critical-service:latest
```

### Migrate an existing deployment to use this scheduler

```bash
kubectl patch deployment <name> -p '{
  "spec": {
    "template": {
      "metadata": {
        "labels": {"app": "<app-name>"},
        "annotations": {"reliability.scheduler/min-availability": "0.95"}
      },
      "spec": {"schedulerName": "reliability-scheduler"}
    }
  }
}'
```

### Revert to default scheduler

```bash
kubectl patch deployment <name> -p '{"spec":{"template":{"spec":{"schedulerName":"default-scheduler"}}}}'
```

---

## Troubleshooting

### Pods not being scheduled

1. Check that the pod has the required annotation `reliability.scheduler/min-availability`
2. Check that the pod has the `app` label
3. Check scheduler logs:
   ```bash
   kubectl logs -n kube-scheduler-reliability -l app=reliability-scheduler
   ```

### Spread not being respected

1. Verify the regression coefficients are set correctly (Intercept, SpreadWeight, HourPerFailureWeight, TotalNodesWeight)
2. Check the pod events:
   ```bash
   kubectl describe pod <pod-name>
   ```

### Prometheus not connecting

```bash
kubectl get svc -A | grep prometheus   # find namespace and service name
# Update PROMETHEUS_URL in scheduler-env-configmap.yaml
```

### Inspect current spread for an app

```bash
APP=my-application
TOTAL=$(kubectl get pods -l app=${APP} --no-headers | wc -l)
NODES=$(kubectl get pods -l app=${APP} -o wide --no-headers | awk '{print $7}' | sort -u | wc -l)
echo "Spread: ${NODES}/${TOTAL}"
```

### Adjusting the regression model

1. Collect cluster data (availability, hour-per-failure, node count)
2. Train a linear regression model
3. Extract coefficients: Intercept, SpreadWeight, HourPerFailureWeight, TotalNodesWeight
4. Set via environment variables or ConfigMap

---

## Limitations

- All pods of the same application must share the same `app` label value
- The `reliability.scheduler/min-availability` annotation is required on each pod
- Both Running and Pending pods are counted when computing total pod count
