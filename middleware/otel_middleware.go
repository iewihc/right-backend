package middleware

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	otelprom "go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/exporters/stdout/stdoutmetric"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
	"go.opentelemetry.io/otel/trace"
)

type OtelConfig struct {
	ServiceName     string
	ServiceVersion  string
	Environment     string
	OTLPEndpoint    string // 改為通用的 OTLP 端點
	JaegerEndpoint  string // 向後兼容
	PrometheusPort  string
	Enabled         bool
	MetricsEnabled  bool
	TracesEnabled   bool
	DevelopmentMode bool // 開發模式使用 stdout，生產模式使用 OTLP
}

// 全局遙測變數
var (
	tracer             trace.Tracer
	meter              metric.Meter
	requestCounter     metric.Int64Counter
	requestDuration    metric.Float64Histogram
	activeRequests     metric.Int64UpDownCounter
	prometheusExporter *otelprom.Exporter
	prometheusRegistry *prometheus.Registry
)

// InitOpenTelemetry 初始化完整的 OpenTelemetry 堆疊（Traces, Metrics, Logs）
func InitOpenTelemetry(config OtelConfig, logger zerolog.Logger) (func(), error) {
	if !config.Enabled {
		return func() {}, nil
	}

	// 向後兼容：如果 OTLPEndpoint 為空但 JaegerEndpoint 有值，使用 JaegerEndpoint
	if config.OTLPEndpoint == "" && config.JaegerEndpoint != "" {
		config.OTLPEndpoint = config.JaegerEndpoint
	}

	ctx := context.Background()
	var shutdownFuncs []func(context.Context) error

	// 創建 resource（不使用 Default 避免版本衝突）
	res := resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceNameKey.String(config.ServiceName),
		semconv.ServiceVersionKey.String(config.ServiceVersion),
		semconv.DeploymentEnvironmentKey.String(config.Environment),
		semconv.ServiceNamespaceKey.String("right-backend"),
		semconv.ServiceInstanceIDKey.String(fmt.Sprintf("%s-%d", config.ServiceName, time.Now().Unix())),
	)

	// 初始化 Traces
	if config.TracesEnabled {
		traceShutdown, err := setupTraceProvider(ctx, res, config, logger)
		if err != nil {
			return nil, fmt.Errorf("failed to setup trace provider: %w", err)
		}
		shutdownFuncs = append(shutdownFuncs, traceShutdown)
	}

	// 初始化 Metrics
	if config.MetricsEnabled {
		metricShutdown, err := setupMeterProvider(ctx, res, config, logger)
		if err != nil {
			return nil, fmt.Errorf("failed to setup meter provider: %w", err)
		}
		shutdownFuncs = append(shutdownFuncs, metricShutdown)
	}

	// 日誌使用現有的 zerolog，不需要額外的 OpenTelemetry logs

	// 設置全局 propagator
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	// 初始化全局遙測物件
	if config.TracesEnabled {
		tracer = otel.Tracer(config.ServiceName)
	}
	if config.MetricsEnabled {
		meter = otel.Meter(config.ServiceName)
		err := initializeMetrics()
		if err != nil {
			return nil, fmt.Errorf("failed to initialize metrics: %w", err)
		}
	}

	logger.Info().
		Str("service", config.ServiceName).
		Str("version", config.ServiceVersion).
		Str("environment", config.Environment).
		Str("otlp_endpoint", config.OTLPEndpoint).
		Bool("traces_enabled", config.TracesEnabled).
		Bool("metrics_enabled", config.MetricsEnabled).
		Bool("development_mode", config.DevelopmentMode).
		Msg("OpenTelemetry 初始化成功")

	// 返回統一的清理函數
	return func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		for _, shutdown := range shutdownFuncs {
			if err := shutdown(shutdownCtx); err != nil {
				logger.Error().Err(err).Msg("Error during OpenTelemetry shutdown")
			}
		}
		logger.Info().Msg("OpenTelemetry 清理完成")
	}, nil
}

// OpenTelemetryMiddleware 創建完整的 OpenTelemetry 中介軟體（包含 Traces, Metrics, Logs）
func OpenTelemetryMiddleware(config OtelConfig, logger zerolog.Logger) func(huma.Context, func(huma.Context)) {
	if !config.Enabled {
		// 如果禁用，返回一個空的中介軟體
		return func(ctx huma.Context, next func(huma.Context)) {
			next(ctx)
		}
	}

	return func(ctx huma.Context, next func(huma.Context)) {
		startTime := time.Now()

		// 從 HTTP headers 中提取 trace context
		carrier := &HeaderCarrier{ctx: ctx}
		parentCtx := otel.GetTextMapPropagator().Extract(ctx.Context(), carrier)

		// 創建 span 名稱
		spanName := fmt.Sprintf("%s %s", ctx.Method(), ctx.URL().Path)

		var span trace.Span
		var spanCtx context.Context

		// 初始化 Tracing
		if config.TracesEnabled && tracer != nil {
			spanCtx, span = tracer.Start(parentCtx, spanName,
				trace.WithAttributes(
					semconv.HTTPMethodKey.String(ctx.Method()),
					semconv.HTTPRouteKey.String(ctx.URL().Path),
					semconv.HTTPSchemeKey.String(ctx.URL().Scheme),
					semconv.HTTPUserAgentKey.String(ctx.Header("User-Agent")),
					attribute.String("http.host", ctx.Header("Host")),
					attribute.String("net.peer.ip", ctx.RemoteAddr()),
					attribute.String("service.name", config.ServiceName),
					attribute.String("service.environment", config.Environment),
				),
			)
			defer span.End()
		} else {
			spanCtx = parentCtx
		}

		// 獲取或生成 request ID
		requestID := GetRequestIDFromContext(ctx)
		if requestID == "" && span != nil {
			requestID = fmt.Sprintf("req_%s", span.SpanContext().TraceID().String()[:8])
		}

		// 設置 trace 信息到 HTTP headers
		if span != nil {
			traceID := span.SpanContext().TraceID().String()
			spanID := span.SpanContext().SpanID().String()
			ctx.SetHeader("X-Request-ID", requestID)
			ctx.SetHeader("X-Trace-ID", traceID)
			ctx.SetHeader("X-Span-ID", spanID)

			// 將 trace context 注入到響應 headers
			otel.GetTextMapPropagator().Inject(spanCtx, carrier)
		}

		// 增加活躍請求計數
		if config.MetricsEnabled && activeRequests != nil {
			activeRequests.Add(spanCtx, 1, metric.WithAttributes(
				attribute.String("method", ctx.Method()),
				attribute.String("route", ctx.URL().Path),
			))
		}

		// 添加 span 事件
		if span != nil {
			span.AddEvent("request.start")
		}

		// 執行下一個中介軟體或處理器
		next(ctx)

		// 計算請求處理時間
		duration := time.Since(startTime)
		statusCode := ctx.Status()

		// 記錄 Metrics
		if config.MetricsEnabled {
			metricAttrs := metric.WithAttributes(
				attribute.String("method", ctx.Method()),
				attribute.String("route", ctx.URL().Path),
				attribute.Int("status_code", statusCode),
				attribute.String("status_class", fmt.Sprintf("%dxx", statusCode/100)),
			)

			if requestCounter != nil {
				requestCounter.Add(spanCtx, 1, metricAttrs)
			}
			if requestDuration != nil {
				requestDuration.Record(spanCtx, duration.Seconds(), metricAttrs)
			}
			if activeRequests != nil {
				activeRequests.Add(spanCtx, -1, metric.WithAttributes(
					attribute.String("method", ctx.Method()),
					attribute.String("route", ctx.URL().Path),
				))
			}
		}

		// 更新 Trace 信息
		if span != nil {
			span.SetAttributes(
				semconv.HTTPStatusCodeKey.Int(statusCode),
				attribute.Float64("http.request.duration_ms", float64(duration.Nanoseconds())/1e6),
				attribute.String("http.request_id", requestID),
			)

			// 添加完成事件
			span.AddEvent("request.complete", trace.WithAttributes(
				attribute.Int("status_code", statusCode),
				attribute.Float64("duration_ms", float64(duration.Nanoseconds())/1e6),
			))

			// 根據狀態碼設置 span 狀態
			if statusCode >= 400 {
				span.RecordError(fmt.Errorf("HTTP %d", statusCode))
				if statusCode >= 500 {
					span.SetStatus(codes.Error, fmt.Sprintf("HTTP %d", statusCode))
				} else {
					span.SetStatus(codes.Error, fmt.Sprintf("Client Error %d", statusCode))
				}
			} else {
				span.SetStatus(codes.Ok, "")
			}
		}

		// 結構化日誌記錄
		logEvent := logger.Debug()
		if statusCode >= 500 {
			logEvent = logger.Error()
		} else if statusCode >= 400 {
			logEvent = logger.Warn()
		} else {
			logEvent = logger.Info()
		}

		logEvent.
			Str("request_id", requestID).
			Str("method", ctx.Method()).
			Str("path", ctx.URL().Path).
			Int("status_code", statusCode).
			Float64("duration_ms", float64(duration.Nanoseconds())/1e6).
			Str("user_agent", ctx.Header("User-Agent")).
			Str("remote_addr", ctx.RemoteAddr()).
			Msg("HTTP request completed")

		if span != nil {
			logEvent.
				Str("trace_id", span.SpanContext().TraceID().String()).
				Str("span_id", span.SpanContext().SpanID().String())
		}
	}
}

// GetTraceIDFromContext 從 HTTP headers 獲取 trace ID
func GetTraceIDFromContext(ctx huma.Context) string {
	return ctx.Header("X-Trace-ID")
}

// GetSpanIDFromContext 從 HTTP headers 獲取 span ID
func GetSpanIDFromContext(ctx huma.Context) string {
	return ctx.Header("X-Span-ID")
}

// GetRequestIDFromContext 從 HTTP headers 獲取 request ID
func GetRequestIDFromContext(ctx huma.Context) string {
	return ctx.Header("X-Request-ID")
}

// GetSpanFromContext 從當前 context 獲取 span
func GetSpanFromContext(ctx huma.Context) trace.Span {
	return trace.SpanFromContext(ctx.Context())
}

// AddSpanEvent 向當前 span 添加事件
func AddSpanEvent(ctx huma.Context, name string, attributes ...attribute.KeyValue) {
	if span := GetSpanFromContext(ctx); span != nil {
		span.AddEvent(name, trace.WithAttributes(attributes...))
	}
}

// SetSpanAttributes 向當前 span 設置屬性
func SetSpanAttributes(ctx huma.Context, attributes ...attribute.KeyValue) {
	if span := GetSpanFromContext(ctx); span != nil {
		span.SetAttributes(attributes...)
	}
}

// RecordSpanError 記錄 span 錯誤
func RecordSpanError(ctx huma.Context, err error, description string) {
	if span := GetSpanFromContext(ctx); span != nil {
		span.RecordError(err)
		if description != "" {
			span.SetStatus(codes.Error, description)
		}
	}
}

// setupTraceProvider 配置 trace export
func setupTraceProvider(ctx context.Context, res *resource.Resource, config OtelConfig, logger zerolog.Logger) (func(context.Context) error, error) {
	var exporter sdktrace.SpanExporter
	var err error

	if config.DevelopmentMode {
		// 開發模式使用 stdout exporter
		exporter, err = stdouttrace.New(stdouttrace.WithPrettyPrint())
		logger.Info().Msg("使用 stdout trace exporter（開發模式）")
	} else {
		// 生產模式使用 OTLP gRPC exporter
		exporter, err = otlptracegrpc.New(ctx,
			otlptracegrpc.WithEndpoint(config.OTLPEndpoint),
			otlptracegrpc.WithInsecure(),
		)
		logger.Info().Str("endpoint", config.OTLPEndpoint).Msg("使用 OTLP gRPC trace exporter（生產模式）")
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create trace exporter: %w", err)
	}

	// 創建 trace provider
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.AlwaysSample()), // 可以根據環境調整採樣率
	)

	// 設置全局 tracer provider
	otel.SetTracerProvider(tp)

	return tp.Shutdown, nil
}

// setupMeterProvider 配置 metric export
func setupMeterProvider(ctx context.Context, res *resource.Resource, config OtelConfig, logger zerolog.Logger) (func(context.Context) error, error) {
	var readers []sdkmetric.Reader
	var shutdownFuncs []func(context.Context) error

	// 總是創建 Prometheus exporter（用於 /metrics 端點）
	var err error
	prometheusRegistry = prometheus.NewRegistry()
	prometheusExporter, err = otelprom.New()
	if err != nil {
		return nil, fmt.Errorf("failed to create prometheus exporter: %w", err)
	}
	readers = append(readers, prometheusExporter)
	logger.Info().Msg("已啟用 Prometheus metrics exporter")

	// 根據配置添加其他 exporter
	if config.DevelopmentMode {
		// 開發模式使用 stdout exporter
		stdoutExporter, err := stdoutmetric.New()
		if err != nil {
			return nil, fmt.Errorf("failed to create stdout metric exporter: %w", err)
		}
		readers = append(readers, sdkmetric.NewPeriodicReader(stdoutExporter,
			sdkmetric.WithInterval(30*time.Second)))
		logger.Info().Msg("已啟用 stdout metric exporter（開發模式）")
	} else {
		// 生產模式使用 OTLP gRPC exporter
		otlpExporter, err := otlpmetricgrpc.New(ctx,
			otlpmetricgrpc.WithEndpoint(config.OTLPEndpoint),
			otlpmetricgrpc.WithInsecure(),
		)
		if err != nil {
			logger.Warn().Err(err).Msg("無法創建 OTLP metric exporter，將只使用 Prometheus")
		} else {
			readers = append(readers, sdkmetric.NewPeriodicReader(otlpExporter,
				sdkmetric.WithInterval(30*time.Second)))
			logger.Info().Str("endpoint", config.OTLPEndpoint).Msg("已啟用 OTLP gRPC metric exporter（生產模式）")

			// 添加 OTLP exporter 的 shutdown 函數
			shutdownFuncs = append(shutdownFuncs, func(ctx context.Context) error {
				return otlpExporter.Shutdown(ctx)
			})
		}
	}

	// 創建 meter provider options
	mpOptions := []sdkmetric.Option{sdkmetric.WithResource(res)}
	for _, reader := range readers {
		mpOptions = append(mpOptions, sdkmetric.WithReader(reader))
	}

	// 創建 meter provider
	mp := sdkmetric.NewMeterProvider(mpOptions...)

	// 設置全局 meter provider
	otel.SetMeterProvider(mp)

	// 返回統一的 shutdown 函數
	return func(ctx context.Context) error {
		// 先關閉 meter provider
		if err := mp.Shutdown(ctx); err != nil {
			logger.Error().Err(err).Msg("Error shutting down meter provider")
		}

		// 然後關閉其他 exporters
		for _, shutdown := range shutdownFuncs {
			if err := shutdown(ctx); err != nil {
				logger.Error().Err(err).Msg("Error shutting down metric exporter")
			}
		}
		return nil
	}, nil
}

// 日誌功能使用現有的 zerolog，不需要額外的 OpenTelemetry logs
// 但我們會在日誌中添加 trace 關聯資訊

// initializeMetrics 初始化所有 metrics
func initializeMetrics() error {
	var err error

	// HTTP 請求計數器
	requestCounter, err = meter.Int64Counter(
		"http_requests_total",
		metric.WithDescription("Total number of HTTP requests"),
		metric.WithUnit("{request}"),
	)
	if err != nil {
		return fmt.Errorf("failed to create request counter: %w", err)
	}

	// HTTP 請求持續時間直方圖
	requestDuration, err = meter.Float64Histogram(
		"http_request_duration_seconds",
		metric.WithDescription("Duration of HTTP requests in seconds"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.001, 0.01, 0.1, 0.5, 1.0, 2.5, 5.0, 10.0),
	)
	if err != nil {
		return fmt.Errorf("failed to create request duration histogram: %w", err)
	}

	// 活躍請求計數器
	activeRequests, err = meter.Int64UpDownCounter(
		"http_requests_active",
		metric.WithDescription("Number of active HTTP requests"),
		metric.WithUnit("{request}"),
	)
	if err != nil {
		return fmt.Errorf("failed to create active requests counter: %w", err)
	}

	return nil
}

// GetPrometheusHandler 返回 Prometheus metrics handler
func GetPrometheusHandler() http.Handler {
	if prometheusRegistry == nil {
		// 如果沒有初始化 Prometheus registry，返回空的 handler
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte("Prometheus registry not initialized"))
		})
	}

	// 使用 Prometheus registry 創建 handler
	return promhttp.HandlerFor(prometheusRegistry, promhttp.HandlerOpts{})
}

// HeaderCarrier 實現 propagation.TextMapCarrier 接口
type HeaderCarrier struct {
	ctx huma.Context
}

func (h *HeaderCarrier) Get(key string) string {
	return h.ctx.Header(key)
}

func (h *HeaderCarrier) Set(key, value string) {
	h.ctx.SetHeader(key, value)
}

func (h *HeaderCarrier) Keys() []string {
	// 由於 huma.Context 沒有提供獲取所有 header keys 的方法
	// 我們返回空 slice，這對於 extract 操作是可以的
	return []string{}
}
