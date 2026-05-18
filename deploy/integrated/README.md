# Integrated Mode

Implanta o ReliabilityScheduler junto com todos os plugins padrão do Kubernetes. Modo recomendado para produção.

O ciclo de scheduling executa os plugins na seguinte ordem:

```
PreFilter → Filter → Score → NormalizeScore
 (default + ReliabilityScheduler)
```

O ReliabilityScheduler tem peso `10` no Score, tornando o espalhamento 10x mais influente que os outros fatores. Ajuste em `configmap.yaml` conforme necessário.

## Arquivos

| Arquivo | Descrição |
|---|---|
| `rbac.yaml` | ServiceAccount, ClusterRole e ClusterRoleBinding |
| `configmap.yaml` | Configuração do scheduler (todos os plugins + ReliabilityScheduler) |
| `scheduler-env-configmap.yaml` | Coeficientes do modelo e configurações do Prometheus |
| `deployment.yaml` | Deployment do scheduler |
| `test-pod.yaml` | Pod de teste simples |
| `test-deployment.yaml` | Deployment de teste com múltiplas réplicas |
| `servicemonitor.yaml` | ServiceMonitor para Prometheus Operator (opcional) |
| `prometheus-rules.yaml` | Recording rules para cálculo automático de hour-per-failure (opcional) |

## Deploy

```bash
kubectl apply -f rbac.yaml
kubectl apply -f configmap.yaml
kubectl apply -f scheduler-env-configmap.yaml
kubectl apply -f deployment.yaml

# Verificar
kubectl get pods -n kube-scheduler-reliability
kubectl logs -n kube-scheduler-reliability -l app=reliability-scheduler
```

### Prometheus (opcional)

Se você tem Prometheus Operator instalado, aplique as recording rules para habilitar o cálculo automático de `hour-per-failure`:

```bash
kubectl apply -f prometheus-rules.yaml
kubectl apply -f servicemonitor.yaml
```

Sem isso, o scheduler usa o valor `DEFAULT_HOUR_PER_FAILURE` definido em `scheduler-env-configmap.yaml`.

## Teste

```bash
kubectl apply -f test-pod.yaml
kubectl get pod test-reliability-scheduler -o wide
kubectl delete -f test-pod.yaml
```

## Troubleshooting

**Pod não espalha (vai sempre para o mesmo nó):**
- Verificar se o pod tem label `app` e annotation `reliability.scheduler/min-availability`
- Verificar logs: `kubectl logs -n kube-scheduler-reliability -l app=reliability-scheduler`

**Pod fica Pending:**
```bash
kubectl describe pod <pod-name>
# Identificar qual plugin está bloqueando (TaintToleration, NodeResourcesFit, ReliabilityScheduler, etc.)
```

**Prometheus não conecta:**
```bash
kubectl get svc -A | grep prometheus  # Verificar namespace e nome do serviço
# Atualizar PROMETHEUS_URL em scheduler-env-configmap.yaml
```
