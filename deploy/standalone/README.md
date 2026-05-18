# Standalone Mode

Implanta apenas o ReliabilityScheduler, sem os plugins padrão do Kubernetes.

**Limitações:** taints/tolerations, resource requests, node selectors, volume binding e pod affinity são ignorados. Use este modo apenas para pesquisa ou ambientes de desenvolvimento simples. Para produção, use `../integrated/`.

## Arquivos

| Arquivo | Descrição |
|---|---|
| `rbac.yaml` | ServiceAccount, ClusterRole e ClusterRoleBinding |
| `configmap.yaml` | Configuração do scheduler (apenas ReliabilityScheduler) |
| `scheduler-env-configmap.yaml` | Coeficientes do modelo e configurações do Prometheus |
| `deployment.yaml` | Deployment do scheduler |
| `test-pod.yaml` | Pod de teste |
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

## Teste

```bash
kubectl apply -f test-pod.yaml
kubectl get pod test-reliability-scheduler -o wide
kubectl delete -f test-pod.yaml
```

## Troubleshooting

**Pod vai para nó com taint / ignora resource requests / não monta volume:**
Comportamento esperado no modo standalone. Migre para `../integrated/`.

**Pod fica Pending:**
```bash
kubectl describe pod <pod-name>
kubectl logs -n kube-scheduler-reliability -l app=reliability-scheduler
```
