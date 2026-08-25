# Deployment Guide

This document covers deploying the Reliability Scheduler to a Kubernetes cluster.

## Table of Contents

- [Prerequisites](#prerequisites)
- [Deployment Modes](#deployment-modes)
- [Quick Start](#quick-start)
- [Standalone Mode](#standalone-mode)
- [Integrated Mode](#integrated-mode-recommended)
- [Prometheus Integration](#prometheus-integration)
- [RBAC](#rbac)
- [Verifying the Deployment](#verifying-the-deployment)
- [Updating Coefficients](#updating-coefficients)
- [Undeploying](#undeploying)
- [Production Considerations](#production-considerations)

---

## Prerequisites

- Kubernetes 1.29+
- `kubectl` configured with access to your cluster
- A container registry to push the scheduler image
- Go 1.22+ and Docker (for building)

---

## Deployment Modes

The Reliability Scheduler can be deployed in two modes:

| Aspect | Standalone | Integrated (recommended) |
|--------|-----------|--------------------------|
| Active plugins | ReliabilityScheduler only | All default K8s plugins + ReliabilityScheduler |
| Resource limits | Ignored | Validated |
| Node selectors | Ignored | Respected |
| Taints/tolerations | Ignored | Respected |
| Volume binding | Ignored | Works |
| Pod affinity | Ignored | Works |
| Topology spread | Ignored | Works |
| Use case | Research / controlled environments | Production / any real workload |

**In standalone mode**, the scheduler runs with only the ReliabilityScheduler plugin. Standard Kubernetes constraints (resource requests, taints, node selectors, volumes, affinity) are **not enforced**. Only use this mode in homogeneous, controlled clusters or for isolated research experiments.

**In integrated mode**, the ReliabilityScheduler runs alongside all standard Kubernetes plugins. All standard constraints are enforced. The ReliabilityScheduler has a score weight of `10` by default, making spread 10× more influential than other scoring factors (adjustable in `configmap.yaml`).

---

## Quick Start

### 1. Build and push the image

```bash
export REGISTRY="your-registry.com"
export VERSION="v1.0.0"

docker build -t ${REGISTRY}/reliability-scheduler:${VERSION} .
docker push ${REGISTRY}/reliability-scheduler:${VERSION}
```

### 2. Update the image reference

Edit `deploy/<mode>/deployment.yaml` and set the container image to `${REGISTRY}/reliability-scheduler:${VERSION}`.

### 3. Apply manifests

```bash
# Choose a mode: standalone or integrated
MODE=integrated

kubectl apply -f deploy/${MODE}/rbac.yaml
kubectl apply -f deploy/${MODE}/configmap.yaml
kubectl apply -f deploy/${MODE}/scheduler-env-configmap.yaml
kubectl apply -f deploy/${MODE}/deployment.yaml
```

### 4. Verify

```bash
kubectl get pods -n kube-scheduler-reliability
kubectl logs -n kube-scheduler-reliability -l app=reliability-scheduler
```

---

## Standalone Mode

Manifests are in `deploy/standalone/`.

| File | Description |
|------|-------------|
| `rbac.yaml` | ServiceAccount, ClusterRole, ClusterRoleBinding |
| `configmap.yaml` | Scheduler config (ReliabilityScheduler only) |
| `scheduler-env-configmap.yaml` | Regression coefficients and Prometheus settings |
| `deployment.yaml` | Scheduler Deployment |
| `test-pod.yaml` | Test pod |
| `servicemonitor.yaml` | ServiceMonitor for Prometheus Operator (optional) |
| `prometheus-rules.yaml` | Recording rules for automatic hour-per-failure (optional) |

```bash
kubectl apply -f deploy/standalone/rbac.yaml
kubectl apply -f deploy/standalone/configmap.yaml
kubectl apply -f deploy/standalone/scheduler-env-configmap.yaml
kubectl apply -f deploy/standalone/deployment.yaml
```

**Warning:** In standalone mode the scheduler may place pods on nodes without sufficient resources, ignoring taints and node selectors. Only use in controlled/homogeneous clusters.

---

## Integrated Mode (recommended)

Manifests are in `deploy/integrated/`.

| File | Description |
|------|-------------|
| `rbac.yaml` | ServiceAccount, ClusterRole, ClusterRoleBinding |
| `configmap.yaml` | Scheduler config (all plugins + ReliabilityScheduler) |
| `scheduler-env-configmap.yaml` | Regression coefficients and Prometheus settings |
| `deployment.yaml` | Scheduler Deployment |
| `test-pod.yaml` | Single test pod |
| `test-deployment.yaml` | Multi-replica test deployment |
| `servicemonitor.yaml` | ServiceMonitor for Prometheus Operator (optional) |
| `prometheus-rules.yaml` | Recording rules for automatic hour-per-failure (optional) |

```bash
kubectl apply -f deploy/integrated/rbac.yaml
kubectl apply -f deploy/integrated/configmap.yaml
kubectl apply -f deploy/integrated/scheduler-env-configmap.yaml
kubectl apply -f deploy/integrated/deployment.yaml
```

---

## Prometheus Integration

The scheduler can automatically fetch `hour_per_failure` metrics from Prometheus. This is optional — without it, the scheduler uses the `DEFAULT_HOUR_PER_FAILURE` value configured in `scheduler-env-configmap.yaml`.

### Enable Prometheus integration

If you have the Prometheus Operator installed:

```bash
# Apply recording rules for automatic hour-per-failure calculation
kubectl apply -f deploy/<mode>/prometheus-rules.yaml

# Apply ServiceMonitor so Prometheus scrapes the scheduler
kubectl apply -f deploy/<mode>/servicemonitor.yaml
```

The recording rule computes:
```
app:reliability:hour_per_failure:1h{app="<app-name>"}
```

### Configure the Prometheus URL

Edit `deploy/<mode>/scheduler-env-configmap.yaml`:

```yaml
data:
  PROMETHEUS_URL: "http://prometheus-k8s.monitoring.svc:9090"
  DEFAULT_HOUR_PER_FAILURE: "0.0"
  METRICS_CACHE_TTL: "5m"
  METRICS_QUERY_TIMEOUT: "100ms"
```

After editing:

```bash
kubectl apply -f deploy/<mode>/scheduler-env-configmap.yaml
kubectl rollout restart deployment -n kube-scheduler-reliability reliability-scheduler
```

---

## RBAC

The `rbac.yaml` manifest creates:

- **ServiceAccount** `reliability-scheduler` in namespace `kube-scheduler-reliability`
- **ClusterRole** with permissions to:
  - Read pods, nodes, replicasets, deployments, statefulsets (for spread calculation)
  - Read and watch configmaps (for scheduler config)
  - Create events (for scheduling decisions)
  - Create pod bindings (to assign pods to nodes)
- **ClusterRoleBinding** linking the ServiceAccount to the ClusterRole

Apply before deploying the scheduler:

```bash
kubectl apply -f deploy/<mode>/rbac.yaml
```

---

## Verifying the Deployment

```bash
# Check scheduler pod is running
kubectl get pods -n kube-scheduler-reliability

# Follow logs
kubectl logs -n kube-scheduler-reliability -l app=reliability-scheduler -f

# Run a test pod
kubectl apply -f deploy/<mode>/test-pod.yaml
kubectl get pod test-reliability-scheduler -o wide
kubectl delete -f deploy/<mode>/test-pod.yaml

# Check all resources in the namespace
kubectl get all -n kube-scheduler-reliability

# Check recent events
kubectl get events -n kube-scheduler-reliability --sort-by='.lastTimestamp'

# Verify scheduler health endpoint
kubectl exec -n kube-scheduler-reliability deployment/reliability-scheduler -- \
  wget -qO- https://localhost:10259/healthz --no-check-certificate
# Expected output: ok
```

---

## Updating Coefficients

### Via ConfigMap (persistent)

```bash
kubectl edit configmap -n kube-scheduler-reliability reliability-scheduler-env-config
# Restart to apply
kubectl rollout restart deployment -n kube-scheduler-reliability reliability-scheduler
```

### Via environment variable patch (one-time)

```bash
kubectl set env deployment/reliability-scheduler \
  -n kube-scheduler-reliability \
  RELIABILITY_SCHEDULER_INTERCEPT=0.6 \
  RELIABILITY_SCHEDULER_SPREAD_WEIGHT=0.35
```

---

## Undeploying

```bash
MODE=integrated  # or standalone

kubectl delete -f deploy/${MODE}/deployment.yaml
kubectl delete -f deploy/${MODE}/configmap.yaml
kubectl delete -f deploy/${MODE}/scheduler-env-configmap.yaml
kubectl delete -f deploy/${MODE}/rbac.yaml

# If Prometheus was configured:
kubectl delete -f deploy/${MODE}/servicemonitor.yaml
kubectl delete -f deploy/${MODE}/prometheus-rules.yaml
```

To revert workloads to the default scheduler:

```bash
# Revert a single deployment
kubectl patch deployment <name> -p '{"spec":{"template":{"spec":{"schedulerName":"default-scheduler"}}}}'

# Revert all deployments using reliability-scheduler across all namespaces
for ns in $(kubectl get ns -o jsonpath='{.items[*].metadata.name}'); do
  kubectl get deployments -n $ns -o json | \
  jq -r '.items[] | select(.spec.template.spec.schedulerName == "reliability-scheduler") | .metadata.name' | \
  while read deploy; do
    echo "Reverting $deploy in $ns"
    kubectl patch deployment $deploy -n $ns \
      -p '{"spec":{"template":{"spec":{"schedulerName":"default-scheduler"}}}}'
  done
done
```

---

## Production Considerations

### High availability

Scale the scheduler to 2+ replicas and add a PodDisruptionBudget:

```bash
kubectl scale deployment -n kube-scheduler-reliability reliability-scheduler --replicas=2

cat <<EOF | kubectl apply -f -
apiVersion: policy/v1
kind: PodDisruptionBudget
metadata:
  name: reliability-scheduler-pdb
  namespace: kube-scheduler-reliability
spec:
  minAvailable: 1
  selector:
    matchLabels:
      app: reliability-scheduler
EOF
```

### Migrating between modes

```bash
# Remove current mode
kubectl delete -f deploy/standalone/deployment.yaml
kubectl delete -f deploy/standalone/configmap.yaml
# ... etc

# Deploy new mode
kubectl apply -f deploy/integrated/rbac.yaml
kubectl apply -f deploy/integrated/configmap.yaml
kubectl apply -f deploy/integrated/scheduler-env-configmap.yaml
kubectl apply -f deploy/integrated/deployment.yaml
```

Existing pods are not affected when changing the scheduler deployment.
