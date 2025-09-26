package metrics

import (
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// ServiceType 定義服務類型
type ServiceType string

const (
	ServiceTypeOrder ServiceType = "order"
)

// OperationType 定義操作類型
type OperationType string

const (
	OperationSimpleCreate OperationType = "simple_create"
	OperationRedispatch   OperationType = "redispatch"
	OperationCancel       OperationType = "cancel"
)

// OperationStatus 定義操作狀態
type OperationStatus string

const (
	StatusSuccess OperationStatus = "success"
	StatusError   OperationStatus = "error"
)

// OperationSource 定義操作來源
type OperationSource string

const (
	SourceWeb     OperationSource = "web"
	SourceDiscord OperationSource = "discord"
	SourceLine    OperationSource = "line"
	SourceManual  OperationSource = "manual"
	SourceSystem  OperationSource = "system"
	SourceAPI     OperationSource = "api"
)

// DetermineSourceFromCreatedBy 根據 CreatedBy 字段判斷操作來源
func DetermineSourceFromCreatedBy(createdBy string) OperationSource {
	switch createdBy {
	case "discord", "Discord":
		return SourceDiscord
	case "line", "Line", "LINE":
		return SourceLine
	case "web", "Web", "manual", "Manual":
		return SourceWeb
	case "system", "System":
		return SourceSystem
	case "api", "API":
		return SourceAPI
	default:
		// 如果包含特定關鍵字則判斷來源
		createdByLower := strings.ToLower(createdBy)
		if strings.Contains(createdByLower, "discord") {
			return SourceDiscord
		}
		if strings.Contains(createdByLower, "line") {
			return SourceLine
		}
		// 默認為手動操作
		return SourceManual
	}
}

// DetermineSourceFromContext 根據上下文判斷操作來源 (更智能的判斷)
func DetermineSourceFromContext(createdBy, cancelledBy, userAgent string) OperationSource {
	// 優先檢查明確的來源標識
	if createdBy != "" {
		return DetermineSourceFromCreatedBy(createdBy)
	}

	if cancelledBy != "" {
		return DetermineSourceFromCreatedBy(cancelledBy)
	}

	// 檢查 User-Agent (如果有的話)
	if userAgent != "" {
		userAgentLower := strings.ToLower(userAgent)
		if strings.Contains(userAgentLower, "discord") {
			return SourceDiscord
		}
		if strings.Contains(userAgentLower, "line") {
			return SourceLine
		}
	}

	// 默認為手動操作
	return SourceManual
}

var (
	serviceOperationsTotal   *prometheus.CounterVec
	serviceOperationDuration *prometheus.HistogramVec
)

// InitServiceMetrics 初始化 Service 層 metrics
func InitServiceMetrics(registry *prometheus.Registry) error {
	serviceOperationsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "service_operations_total",
			Help: "Total number of service layer operations",
		},
		[]string{"service", "operation", "status", "source"},
	)

	serviceOperationDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "service_operation_duration_seconds",
			Help:    "Duration of service layer operations in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"service", "operation", "source"},
	)

	if err := registry.Register(serviceOperationsTotal); err != nil {
		return err
	}

	if err := registry.Register(serviceOperationDuration); err != nil {
		return err
	}

	return nil
}

// RecordServiceOperation 記錄 Service 層操作 metrics (使用 enum 提升代碼可讀性)
func RecordServiceOperation(service ServiceType, operation OperationType, status OperationStatus, source OperationSource, duration time.Duration) {
	if serviceOperationsTotal != nil && serviceOperationDuration != nil {
		serviceOperationsTotal.WithLabelValues(string(service), string(operation), string(status), string(source)).Inc()
		serviceOperationDuration.WithLabelValues(string(service), string(operation), string(source)).Observe(duration.Seconds())
	}
}

// RecordOrderOperation 專門記錄訂單操作的便利函數
func RecordOrderOperation(operation OperationType, status OperationStatus, source OperationSource, duration time.Duration) {
	RecordServiceOperation(ServiceTypeOrder, operation, status, source, duration)
}
