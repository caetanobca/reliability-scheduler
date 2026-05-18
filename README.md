# Reliability Scheduler Plugin

Um plugin personalizado para o Kubernetes Scheduler que implementa escalonamento baseado em disponibilidade e espalhamento de pods.

## Visão Geral

O Reliability Scheduler é um plugin de escalonamento do Kubernetes que permite ao operador da aplicação definir o nível mínimo de disponibilidade aceitável. O plugin calcula automaticamente o espalhamento ideal dos pods entre os nós do cluster para atingir essa disponibilidade desejada.

## Conceitos

### Disponibilidade
Definida como a fração dos pods que estão em execução:
```
Disponibilidade = pods running / total de pods da aplicação
```

### Espalhamento
Número de nós que possuem pelo menos um pod da aplicação em execução:
```
Espalhamento = nós com ≥1 pod / total de pods da aplicação
```

### Hora/Falha
Métrica de confiabilidade histórica:
```
Hora/Falha = tempo total observado (horas) / quantidade de falhas
```
Onde "falha" é definida como um estado onde um ou mais pods não estão running.

### Espalhamento Alvo
Calculado usando regressão linear:
```
Espalhamento_Alvo = (Disponibilidade_Min - HourPerFailureWeight×Hora_Falha - TotalNodesWeight×Total_Nós - Intercept) / SpreadWeight
```

## Arquitetura

O plugin implementa quatro pontos de extensão do Kubernetes Scheduler Framework:

### 1. PreFilter
- Obtém a disponibilidade mínima desejada (annotation do pod)
- Obtém a métrica hora/falha da aplicação
- Conta o total de nós do cluster
- **Aplica o modelo de regressão linear** para calcular o espalhamento alvo
- Calcula o espalhamento atual
- Armazena o estado no CycleState

### 2. Filter
- **Se espalhamento_atual ≥ espalhamento_alvo**: permite escalonamento em qualquer nó
- **Se espalhamento_atual < espalhamento_alvo**: apenas permite escalonamento em nós que NÃO possuem pods dessa aplicação

### 3. Score
- Atribui pontuações mais altas para nós com **menos pods da aplicação**
- Promove espalhamento uniforme dos pods

### 4. NormalizeScore
- Normaliza as pontuações para o intervalo padrão [0, 100]

## Estrutura do Projeto

```
.
├── cmd/
│   └── scheduler/
│       └── main.go                    # Ponto de entrada
├── pkg/
│   └── reliabilityscheduler/
│       ├── plugin.go                  # Registro e carregamento de coeficientes
│       ├── types.go                   # Estruturas de dados
│       ├── prefilter.go               # Cálculo de espalhamento alvo/atual
│       ├── filter.go                  # Lógica de espalhamento
│       ├── score.go                   # Pontuação por densidade de pods
│       ├── normalize_score.go         # Normalização de scores
│       └── metrics_provider.go        # Integração com Prometheus
├── deploy/
│   ├── standalone/                    # Manifests modo standalone
│   ├── integrated/                    # Manifests modo integrado (recomendado)
│   └── scheduler-env-configmap.example.yaml
├── examples/
│   └── pod-example.yaml               # Exemplos de uso
├── Dockerfile
├── go.mod
└── README.md
```

## Configuração

### Variáveis de Ambiente

Todas as variáveis de ambiente têm precedência sobre os valores definidos no arquivo de configuração do scheduler.

#### Coeficientes do Modelo

| Variável | Padrão | Descrição |
|---|---|---|
| `RELIABILITY_SCHEDULER_INTERCEPT` | `0` | Intercepto da regressão linear |
| `RELIABILITY_SCHEDULER_SPREAD_WEIGHT` | `1` | Peso da disponibilidade mínima |
| `RELIABILITY_SCHEDULER_HOUR_PER_FAILURE_WEIGHT` | `0` | Peso da métrica hora/falha |
| `RELIABILITY_SCHEDULER_TOTAL_NODES_WEIGHT` | `0` | Peso do total de nós do cluster |
| `RELIABILITY_SCHEDULER_SPREAD_SMALL_APPS` | `0` | Ajuste de `SPREAD_WEIGHT` para apps com ≤10 pods (somado ao base) |
| `RELIABILITY_SCHEDULER_INTERCEPT_SMALL_APPS` | `0` | Ajuste de `INTERCEPT` para apps com ≤10 pods (somado ao base) |

#### Integração com Prometheus

| Variável | Padrão | Descrição |
|---|---|---|
| `PROMETHEUS_URL` | `http://prometheus-k8s.monitoring.svc:9090` | URL do serviço Prometheus |
| `DEFAULT_HOUR_PER_FAILURE` | `0.0` | Valor usado quando a métrica não está disponível no Prometheus (0.0 = neutro, sem efeito no cálculo) |
| `METRICS_CACHE_TTL` | `5m` | TTL do cache de métricas (ex: `1m`, `5m`, `10m`) |
| `METRICS_QUERY_TIMEOUT` | `100ms` | Timeout das queries ao Prometheus (ex: `50ms`, `200ms`) |

Essas variáveis são definidas via ConfigMap — veja `deploy/<modo>/scheduler-env-configmap.yaml`.

### Arquivo de Configuração do Scheduler

Os coeficientes também podem ser definidos diretamente no `configmap.yaml`:

```yaml
pluginConfig:
  - name: ReliabilityScheduler
    args:
      intercept: 0.5
      spreadWeight: 0.3
      hourPerFailureWeight: 0.1
      totalNodesWeight: 0.05
```

## Uso

### Definindo Disponibilidade Mínima no Pod

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: my-app
  labels:
    app: my-application  # Obrigatório: identifica a aplicação
  annotations:
    # Disponibilidade mínima desejada (0.0 - 1.0)
    reliability.scheduler/min-availability: "0.95"
spec:
  schedulerName: reliability-scheduler
  containers:
  - name: app
    image: my-app:latest
```

### Annotations

| Annotation | Obrigatória | Descrição | Exemplo |
|---|---|---|---|
| `reliability.scheduler/min-availability` | Sim | Disponibilidade mínima desejada (0.0 a 1.0) | "0.95" |
| `reliability.scheduler/hour-per-failure` | Não | Métrica de confiabilidade histórica. Se ausente, usa Prometheus ou `DEFAULT_HOUR_PER_FAILURE` | "100.0" |

### Labels Obrigatórias

| Label | Descrição | Exemplo |
|-------|-----------|---------|
| `app` | Identificador da aplicação (agrupa pods) | "my-application" |

## Exemplos

### Aplicação com Alta Disponibilidade
```yaml
apiVersion: v1
kind: Pod
metadata:
  name: critical-app
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

### Aplicação com Disponibilidade Padrão
```yaml
apiVersion: v1
kind: Pod
metadata:
  name: standard-app
  labels:
    app: standard-service
  annotations:
    reliability.scheduler/min-availability: "0.90"
spec:
  schedulerName: reliability-scheduler
  containers:
  - name: app
    image: standard-service:latest
```

## Compilação e Instalação

### Pré-requisitos
- Go 1.22+
- Acesso a um cluster Kubernetes
- Kubernetes 1.29+

### Build
```bash
go build -o bin/reliability-scheduler ./cmd/scheduler
```

### Executar
```bash
./bin/reliability-scheduler \
  --config=/path/to/scheduler-config.yaml \
  --kubeconfig=/path/to/kubeconfig
```

### Executar com Variáveis de Ambiente
```bash
export RELIABILITY_SCHEDULER_INTERCEPT=0.5
export RELIABILITY_SCHEDULER_SPREAD_WEIGHT=0.3
export RELIABILITY_SCHEDULER_HOUR_PER_FAILURE_WEIGHT=0.1
export RELIABILITY_SCHEDULER_TOTAL_NODES_WEIGHT=0.05

./bin/reliability-scheduler \
  --config=/path/to/scheduler-config.yaml \
  --kubeconfig=/path/to/kubeconfig
```

## Comportamento do Plugin

### Cenário 1: Espalhamento Insuficiente
```
Espalhamento Atual < Espalhamento Alvo
```
- **Filter**: Apenas nós SEM pods da aplicação são elegíveis
- **Score**: Nós sem pods recebem pontuação máxima
- **Resultado**: Força espalhamento em novos nós

### Cenário 2: Espalhamento Adequado
```
Espalhamento Atual ≥ Espalhamento Alvo
```
- **Filter**: Todos os nós são elegíveis
- **Score**: Nós com menos pods recebem pontuações mais altas
- **Resultado**: Favorece balanceamento uniforme

## Métricas e Monitoramento

### Visualizando o Espalhamento Atual
```bash
# Listar pods por nó para uma aplicação
kubectl get pods -l app=my-application -o wide
```

### Calculando Espalhamento Manualmente
```bash
# Total de pods
TOTAL_PODS=$(kubectl get pods -l app=my-application --no-headers | wc -l)

# Nós únicos com pods
NODES_WITH_PODS=$(kubectl get pods -l app=my-application -o wide --no-headers | awk '{print $7}' | sort -u | wc -l)

# Espalhamento
echo "scale=4; $NODES_WITH_PODS / $TOTAL_PODS" | bc
```

## Desenvolvimento

### Ajustando o Modelo de Regressão

1. Colete dados do seu cluster (disponibilidade, hora/falha, nós)
2. Treine um modelo de regressão linear
3. Extraia os coeficientes: Intercept, SpreadWeight, HourPerFailureWeight, TotalNodesWeight
4. Configure via variáveis de ambiente ou arquivo de config

### Testando
```bash
go test ./...
```

### Formatação
```bash
go fmt ./...
```

## Troubleshooting

### Pods não sendo escalonados
1. Verifique se as annotations obrigatórias estão presentes
2. Verifique se a label `app` está definida
3. Verifique os logs do scheduler:
```bash
kubectl logs -n kube-scheduler-reliability -l app=reliability-scheduler
```

### Espalhamento não está sendo respeitado
1. Verifique se os coeficientes da regressão estão corretos (Intercept, SpreadWeight, HourPerFailureWeight, TotalNodesWeight)
2. Verifique o cálculo do espalhamento alvo
3. Verifique eventos do pod:
```bash
kubectl describe pod <pod-name>
```

## Limitações

- Requer que todos os pods da mesma aplicação tenham a label `app` com o mesmo valor
- Requer a annotation `reliability.scheduler/min-availability` em cada pod
- Considera todos os pods (Running, Pending) para cálculo correto do total

## Documentação

- [deploy/README.md](./deploy/README.md) - Guia de implantação, modos e variáveis de ambiente
- [BUGFIX_SPREAD_CALCULATION.md](./BUGFIX_SPREAD_CALCULATION.md) - Correção crítica no cálculo de spread
- [examples/pod-example.yaml](./examples/pod-example.yaml) - Exemplos de uso
