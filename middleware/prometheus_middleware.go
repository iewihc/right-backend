package middleware

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog"
)

var (
	// Prometheus metrics
	httpRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Total number of HTTP requests by method, route, and status code",
		},
		[]string{"method", "route", "status_code"},
	)

	httpRequestDurationSeconds = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "Duration of HTTP requests in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "route"},
	)

	httpRequestsActive = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "http_requests_active",
			Help: "Number of active HTTP requests",
		},
		[]string{"method", "route"},
	)

	// WebSocket metrics
	websocketLocationUpdatesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "websocket_location_updates_total",
			Help: "Total number of WebSocket location updates",
		},
		[]string{"driver_id", "fleet", "status"},
	)

	// WebSocket connection metrics
	websocketConnectionsTotal = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "websocket_connections_total",
			Help: "Total number of active WebSocket connections",
		},
		[]string{"type", "fleet"},
	)

	onlineDriversTotal = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "online_drivers_total",
			Help: "Total number of online drivers from database",
		},
		[]string{"fleet"},
	)

	// Infrastructure health metrics
	infraHealthStatus = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "infrastructure_health_status",
			Help: "Health status of infrastructure components (1=healthy, 0=unhealthy)",
		},
		[]string{"service", "component"},
	)

	infraConnectionLatency = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "infrastructure_connection_latency_ms",
			Help: "Connection latency to infrastructure components in milliseconds",
		},
		[]string{"service", "component"},
	)

	// Prometheus registry
	promRegistry *prometheus.Registry
)

// InitPrometheusMetrics 初始化 Prometheus metrics
func InitPrometheusMetrics(logger zerolog.Logger) error {
	// 創建新的 registry
	promRegistry = prometheus.NewRegistry()

	// 註冊所有 metrics
	if err := promRegistry.Register(httpRequestsTotal); err != nil {
		return fmt.Errorf("failed to register http_requests_total: %w", err)
	}

	if err := promRegistry.Register(httpRequestDurationSeconds); err != nil {
		return fmt.Errorf("failed to register http_request_duration_seconds: %w", err)
	}

	if err := promRegistry.Register(httpRequestsActive); err != nil {
		return fmt.Errorf("failed to register http_requests_active: %w", err)
	}

	if err := promRegistry.Register(websocketLocationUpdatesTotal); err != nil {
		return fmt.Errorf("failed to register websocket_location_updates_total: %w", err)
	}

	if err := promRegistry.Register(websocketConnectionsTotal); err != nil {
		return fmt.Errorf("failed to register websocket_connections_total: %w", err)
	}

	if err := promRegistry.Register(onlineDriversTotal); err != nil {
		return fmt.Errorf("failed to register online_drivers_total: %w", err)
	}

	if err := promRegistry.Register(infraHealthStatus); err != nil {
		return fmt.Errorf("failed to register infrastructure_health_status: %w", err)
	}

	if err := promRegistry.Register(infraConnectionLatency); err != nil {
		return fmt.Errorf("failed to register infrastructure_connection_latency_ms: %w", err)
	}

	// 也註冊默認的 Go metrics
	promRegistry.MustRegister(prometheus.NewGoCollector())
	promRegistry.MustRegister(prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}))

	logger.Info().Msg("Prometheus metrics 初始化成功")
	return nil
}

// GetStandardPrometheusHandler 返回標準的 Prometheus metrics handler
func GetStandardPrometheusHandler() http.Handler {
	if promRegistry == nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte("Prometheus registry not initialized"))
		})
	}

	return promhttp.HandlerFor(promRegistry, promhttp.HandlerOpts{})
}

// GetPrometheusRegistry 返回 Prometheus registry 供其他包使用
func GetPrometheusRegistry() *prometheus.Registry {
	return promRegistry
}

// PrometheusMiddleware HTTP metrics 中間件
func PrometheusMiddleware(logger zerolog.Logger) func(huma.Context, func(huma.Context)) {
	return func(ctx huma.Context, next func(huma.Context)) {
		if promRegistry == nil {
			// 如果 Prometheus 沒有初始化，直接繼續
			next(ctx)
			return
		}

		startTime := time.Now()
		method := ctx.Method()
		route := ctx.URL().Path

		// 增加活躍請求數
		httpRequestsActive.WithLabelValues(method, route).Inc()

		// 執行下一個中間件
		next(ctx)

		// 記錄 metrics
		duration := time.Since(startTime)
		statusCode := ctx.Status()
		statusCodeStr := strconv.Itoa(statusCode)

		// 記錄請求計數
		httpRequestsTotal.WithLabelValues(method, route, statusCodeStr).Inc()

		// 記錄請求持續時間
		httpRequestDurationSeconds.WithLabelValues(method, route).Observe(duration.Seconds())

		// 減少活躍請求數
		httpRequestsActive.WithLabelValues(method, route).Dec()

		logger.Debug().
			Str("method", method).
			Str("route", route).
			Int("status_code", statusCode).
			Float64("duration_seconds", duration.Seconds()).
			Msg("HTTP metrics recorded")
	}
}

// RecordWebSocketLocationUpdate 記錄 WebSocket 位置更新 metric
func RecordWebSocketLocationUpdate(driverID, fleet, status string) {
	if promRegistry != nil && websocketLocationUpdatesTotal != nil {
		websocketLocationUpdatesTotal.WithLabelValues(driverID, fleet, status).Inc()
	}
}

// UpdateWebSocketConnections 更新 WebSocket 連接統計
func UpdateWebSocketConnections(connectionsByType map[string]int, connectionsByFleet map[string]int) {
	if promRegistry == nil || websocketConnectionsTotal == nil {
		return
	}

	// 清除舊數據
	websocketConnectionsTotal.Reset()

	// 按類型更新連接數
	for connType, count := range connectionsByType {
		websocketConnectionsTotal.WithLabelValues(connType, "all").Set(float64(count))
	}

	// 按車隊更新連接數
	for fleet, count := range connectionsByFleet {
		websocketConnectionsTotal.WithLabelValues("driver", fleet).Set(float64(count))
	}
}

// UpdateOnlineDrivers 更新在線司機統計
func UpdateOnlineDrivers(fleetCounts map[string]int) {
	if promRegistry == nil || onlineDriversTotal == nil {
		return
	}

	// 清除舊數據
	onlineDriversTotal.Reset()

	// 更新各車隊在線司機數
	for fleet, count := range fleetCounts {
		onlineDriversTotal.WithLabelValues(fleet).Set(float64(count))
	}
}

// UpdateInfrastructureHealth 更新基礎設施健康狀態
func UpdateInfrastructureHealth(service, component string, isHealthy bool, latencyMs float64) {
	if promRegistry == nil || infraHealthStatus == nil || infraConnectionLatency == nil {
		return
	}

	healthValue := 0.0
	if isHealthy {
		healthValue = 1.0
	}

	infraHealthStatus.WithLabelValues(service, component).Set(healthValue)
	if latencyMs >= 0 {
		infraConnectionLatency.WithLabelValues(service, component).Set(latencyMs)
	}
}
