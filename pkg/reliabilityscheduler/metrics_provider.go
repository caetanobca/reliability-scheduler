package reliabilityscheduler

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/prometheus/client_golang/api"
	prometheus "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
)

const (
	// Environment variables
	EnvPrometheusURL         = "PROMETHEUS_URL"
	EnvDefaultHourPerFailure = "DEFAULT_HOUR_PER_FAILURE"
	EnvMetricsCacheTTL       = "METRICS_CACHE_TTL"
	EnvMetricsQueryTimeout   = "METRICS_QUERY_TIMEOUT"

	// Default values
	DefaultPrometheusURL       = "http://prometheus-k8s.monitoring.svc:9090"
	DefaultHourPerFailureValue = 0.0
	DefaultCacheTTL            = 5 * time.Minute
	DefaultQueryTimeout        = 100 * time.Millisecond
)

// CachedMetrics armazena as métricas cacheadas de uma aplicação
type CachedMetrics struct {
	HourPerFailure float64
	LastUpdated    time.Time
}

// MetricsProvider é responsável por obter métricas do Prometheus com cache
type MetricsProvider struct {
	prometheusClient      prometheus.API
	cache                 map[string]*CachedMetrics
	cacheMutex            sync.RWMutex
	cacheTTL              time.Duration
	queryTimeout          time.Duration
	defaultHourPerFailure float64
}

// NewMetricsProvider cria um novo provider de métricas
// Lê configurações de variáveis de ambiente
func NewMetricsProvider() (*MetricsProvider, error) {
	// Ler Prometheus URL
	prometheusURL := os.Getenv(EnvPrometheusURL)
	if prometheusURL == "" {
		prometheusURL = DefaultPrometheusURL
		klog.InfoS("Using default Prometheus URL", "url", prometheusURL)
	}

	// Ler valor padrão de hour-per-failure
	defaultHPF := DefaultHourPerFailureValue
	if hpfStr := os.Getenv(EnvDefaultHourPerFailure); hpfStr != "" {
		if parsed, err := strconv.ParseFloat(hpfStr, 64); err == nil {
			defaultHPF = parsed
		} else {
			klog.ErrorS(err, "Failed to parse DEFAULT_HOUR_PER_FAILURE, using default",
				"value", hpfStr,
				"default", defaultHPF)
		}
	}

	// Ler Cache TTL
	cacheTTL := DefaultCacheTTL
	if ttlStr := os.Getenv(EnvMetricsCacheTTL); ttlStr != "" {
		if parsed, err := time.ParseDuration(ttlStr); err == nil {
			cacheTTL = parsed
		} else {
			klog.ErrorS(err, "Failed to parse METRICS_CACHE_TTL, using default",
				"value", ttlStr,
				"default", cacheTTL)
		}
	}

	// Ler Query Timeout
	queryTimeout := DefaultQueryTimeout
	if timeoutStr := os.Getenv(EnvMetricsQueryTimeout); timeoutStr != "" {
		if parsed, err := time.ParseDuration(timeoutStr); err == nil {
			queryTimeout = parsed
		} else {
			klog.ErrorS(err, "Failed to parse METRICS_QUERY_TIMEOUT, using default",
				"value", timeoutStr,
				"default", queryTimeout)
		}
	}

	// Criar cliente Prometheus
	client, err := api.NewClient(api.Config{
		Address: prometheusURL,
		RoundTripper: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     90 * time.Second,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create Prometheus client: %w", err)
	}

	mp := &MetricsProvider{
		prometheusClient:      prometheus.NewAPI(client),
		cache:                 make(map[string]*CachedMetrics),
		cacheTTL:              cacheTTL,
		queryTimeout:          queryTimeout,
		defaultHourPerFailure: defaultHPF,
	}

	klog.InfoS("MetricsProvider initialized",
		"prometheusURL", prometheusURL,
		"cacheTTL", cacheTTL,
		"queryTimeout", queryTimeout,
		"defaultHourPerFailure", defaultHPF)

	return mp, nil
}

// GetHourPerFailure retorna o hour-per-failure de uma aplicação
// Estratégia de fallback:
//  1. Cache válido (não expirado) → retorna imediatamente
//  2. Cache miss → tenta Prometheus
//     a. Sucesso → atualiza cache e retorna
//     b. Erro → tenta usar cache expirado
//  3. Cache expirado existe → retorna valor antigo (melhor que default)
//  4. Sem cache → retorna valor padrão configurado
func (mp *MetricsProvider) GetHourPerFailure(ctx context.Context, pod *v1.Pod) float64 {
	appName := pod.Labels["app"]
	if appName == "" {
		klog.V(4).InfoS("Pod has no app label, using default",
			"pod", pod.Name,
			"default", mp.defaultHourPerFailure)
		return mp.defaultHourPerFailure
	}

	// 1. Tentar cache válido primeiro
	if cachedValue, valid := mp.getCached(appName); valid {
		klog.V(4).InfoS("Cache hit (valid)",
			"app", appName,
			"hourPerFailure", cachedValue)
		return cachedValue
	}

	// 2. Cache miss ou expirado - tentar Prometheus
	klog.V(4).InfoS("Cache miss, querying Prometheus", "app", appName)

	if value, err := mp.queryPrometheus(ctx, appName); err == nil {
		// Sucesso! Atualizar cache e retornar
		mp.updateCache(appName, value)
		klog.V(4).InfoS("Prometheus query succeeded",
			"app", appName,
			"hourPerFailure", value)
		return value
	} else {
		// Prometheus falhou
		klog.V(3).InfoS("Prometheus query failed, using fallback",
			"app", appName,
			"error", err)
	}

	// 3. Prometheus falhou - tentar usar cache expirado (melhor que default)
	if staleValue, exists := mp.getStaleCache(appName); exists {
		klog.V(3).InfoS("Using stale cache value (Prometheus unavailable)",
			"app", appName,
			"hourPerFailure", staleValue,
			"age", time.Since(mp.getCacheAge(appName)))
		return staleValue
	}

	// 4. Último recurso: valor padrão
	klog.V(3).InfoS("No cache available, using default",
		"app", appName,
		"default", mp.defaultHourPerFailure)
	return mp.defaultHourPerFailure
}

// getCached verifica se existe valor VÁLIDO no cache (não expirado)
// Retorna (valor, true) se válido, (0, false) se não existe ou expirado
func (mp *MetricsProvider) getCached(appName string) (float64, bool) {
	mp.cacheMutex.RLock()
	defer mp.cacheMutex.RUnlock()

	cached, exists := mp.cache[appName]
	if !exists {
		return 0, false
	}

	// Verificar se expirou
	if time.Since(cached.LastUpdated) > mp.cacheTTL {
		return 0, false
	}

	return cached.HourPerFailure, true
}

// getStaleCache retorna valor do cache mesmo que expirado (fallback quando Prometheus falha)
// Retorna (valor, true) se existe, (0, false) se não existe
func (mp *MetricsProvider) getStaleCache(appName string) (float64, bool) {
	mp.cacheMutex.RLock()
	defer mp.cacheMutex.RUnlock()

	cached, exists := mp.cache[appName]
	if !exists {
		return 0, false
	}

	return cached.HourPerFailure, true
}

// getCacheAge retorna a idade do cache para uma app (para logging)
func (mp *MetricsProvider) getCacheAge(appName string) time.Time {
	mp.cacheMutex.RLock()
	defer mp.cacheMutex.RUnlock()

	if cached, exists := mp.cache[appName]; exists {
		return cached.LastUpdated
	}

	return time.Time{}
}

// updateCache atualiza o cache com novo valor
func (mp *MetricsProvider) updateCache(appName string, hourPerFailure float64) {
	mp.cacheMutex.Lock()
	defer mp.cacheMutex.Unlock()

	mp.cache[appName] = &CachedMetrics{
		HourPerFailure: hourPerFailure,
		LastUpdated:    time.Now(),
	}

	klog.V(5).InfoS("Cache updated",
		"app", appName,
		"hourPerFailure", hourPerFailure)
}

// queryPrometheus consulta o Prometheus para obter hour-per-failure
func (mp *MetricsProvider) queryPrometheus(ctx context.Context, appName string) (float64, error) {
	// Criar contexto com timeout
	queryCtx, cancel := context.WithTimeout(ctx, mp.queryTimeout)
	defer cancel()

	// Query usando recording rule
	// Espera-se que o Prometheus tenha a recording rule:
	// app:reliability:hour_per_failure:1h
	query := fmt.Sprintf(`app:reliability:hour_per_failure:1h{app="%s"}`, appName)

	start := time.Now()
	result, warnings, err := mp.prometheusClient.Query(queryCtx, query, time.Now())
	queryDuration := time.Since(start)

	if err != nil {
		return 0, fmt.Errorf("prometheus query failed: %w", err)
	}

	if len(warnings) > 0 {
		klog.V(4).InfoS("Prometheus query warnings", "warnings", warnings)
	}

	klog.V(5).InfoS("Prometheus query completed",
		"app", appName,
		"duration", queryDuration)

	// Parse resultado
	return parsePrometheusResult(result, appName)
}

// parsePrometheusResult extrai o valor float64 do resultado do Prometheus
func parsePrometheusResult(result model.Value, appName string) (float64, error) {
	vector, ok := result.(model.Vector)
	if !ok {
		return 0, fmt.Errorf("unexpected result type: %T", result)
	}

	if len(vector) == 0 {
		// Nenhum resultado encontrado - pode ser app nova sem histórico
		// Retornar 0 (não é erro, só significa sem dados ainda)
		klog.V(4).InfoS("No metrics found in Prometheus (app may be new)",
			"app", appName)
		return 0.0, nil
	}

	// Pegar primeiro resultado (deve haver apenas 1 por app)
	value := float64(vector[0].Value)

	// Validar valor
	if value < 0 {
		klog.V(3).InfoS("Invalid negative value from Prometheus, using 0",
			"app", appName,
			"value", value)
		return 0.0, nil
	}

	return value, nil
}

// GetCacheStats retorna estatísticas do cache (útil para monitoramento)
func (mp *MetricsProvider) GetCacheStats() (entries int, validEntries int, oldestAge time.Duration) {
	mp.cacheMutex.RLock()
	defer mp.cacheMutex.RUnlock()

	entries = len(mp.cache)
	validEntries = 0

	var oldest time.Time
	now := time.Now()

	for _, cached := range mp.cache {
		// Contar entradas válidas (não expiradas)
		if now.Sub(cached.LastUpdated) <= mp.cacheTTL {
			validEntries++
		}

		// Encontrar entrada mais antiga
		if oldest.IsZero() || cached.LastUpdated.Before(oldest) {
			oldest = cached.LastUpdated
		}
	}

	if !oldest.IsZero() {
		oldestAge = time.Since(oldest)
	}

	return entries, validEntries, oldestAge
}

// CleanupExpiredEntries remove entradas muito antigas do cache
// Remove apenas entradas que expiraram há mais de 2x o TTL
// Retorna número de entradas removidas
func (mp *MetricsProvider) CleanupExpiredEntries() int {
	mp.cacheMutex.Lock()
	defer mp.cacheMutex.Unlock()

	now := time.Now()
	removed := 0

	// Manter entradas expiradas por até 2x o TTL (para fallback quando Prometheus falha)
	expirationThreshold := mp.cacheTTL * 2

	for appName, cached := range mp.cache {
		if now.Sub(cached.LastUpdated) > expirationThreshold {
			delete(mp.cache, appName)
			removed++
		}
	}

	if removed > 0 {
		klog.V(4).InfoS("Cleaned up expired cache entries",
			"removed", removed,
			"remaining", len(mp.cache))
	}

	return removed
}
