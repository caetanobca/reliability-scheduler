# Deployment

O Reliability Scheduler pode ser implantado de duas formas:

| | Standalone | Integrated |
|---|---|---|
| Plugins ativos | Apenas ReliabilityScheduler | Todos os plugins K8s + ReliabilityScheduler |
| Taints/Tolerations | Ignorado | Respeitado |
| Resource Requests/Limits | Ignorado | Respeitado |
| Node Selectors / Affinity | Ignorado | Respeitado |
| Volume Binding | Ignorado | Respeitado |
| Recomendado para | Pesquisa/Dev | **Produção** |

**Regra geral:** use `integrated/` a menos que você queira isolar o comportamento do ReliabilityScheduler para fins de pesquisa.

## Quick Start

### Integrated (recomendado)

```bash
kubectl apply -f deploy/integrated/rbac.yaml
kubectl apply -f deploy/integrated/configmap.yaml
kubectl apply -f deploy/integrated/scheduler-env-configmap.yaml
kubectl apply -f deploy/integrated/deployment.yaml

kubectl get pods -n kube-scheduler-reliability
```

### Standalone

```bash
kubectl apply -f deploy/standalone/rbac.yaml
kubectl apply -f deploy/standalone/configmap.yaml
kubectl apply -f deploy/standalone/scheduler-env-configmap.yaml
kubectl apply -f deploy/standalone/deployment.yaml

kubectl get pods -n kube-scheduler-reliability
```

## Variáveis de Ambiente

Definidas em `scheduler-env-configmap.yaml`. Consulte `scheduler-env-configmap.example.yaml` para um exemplo com valores e cálculos comentados.

### Coeficientes do Modelo

| Variável | Padrão | Descrição |
|---|---|---|
| `RELIABILITY_SCHEDULER_INTERCEPT` | `0` | Intercepto da regressão linear |
| `RELIABILITY_SCHEDULER_SPREAD_WEIGHT` | `1` | Peso da disponibilidade mínima |
| `RELIABILITY_SCHEDULER_HOUR_PER_FAILURE_WEIGHT` | `0` | Peso da métrica hora/falha |
| `RELIABILITY_SCHEDULER_TOTAL_NODES_WEIGHT` | `0` | Peso do total de nós do cluster |
| `RELIABILITY_SCHEDULER_SPREAD_SMALL_APPS` | `0` | Ajuste de `SPREAD_WEIGHT` para apps com ≤10 pods (somado ao base) |
| `RELIABILITY_SCHEDULER_INTERCEPT_SMALL_APPS` | `0` | Ajuste de `INTERCEPT` para apps com ≤10 pods (somado ao base) |

### Integração com Prometheus

| Variável | Padrão | Descrição |
|---|---|---|
| `PROMETHEUS_URL` | `http://prometheus-k8s.monitoring.svc:9090` | URL do serviço Prometheus |
| `DEFAULT_HOUR_PER_FAILURE` | `0.0` | Valor usado quando a métrica não está disponível (0.0 = neutro) |
| `METRICS_CACHE_TTL` | `5m` | TTL do cache de métricas (ex: `1m`, `5m`, `10m`) |
| `METRICS_QUERY_TIMEOUT` | `100ms` | Timeout das queries ao Prometheus (ex: `50ms`, `200ms`) |

Após editar, aplique e reinicie o scheduler:

```bash
kubectl apply -f deploy/<mode>/scheduler-env-configmap.yaml
kubectl rollout restart deployment/reliability-scheduler -n kube-scheduler-reliability
```
