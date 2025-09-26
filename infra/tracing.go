package infra

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const (
	ServiceName = "right-backend"
)

// 全局 tracer 實例
var globalTracer trace.Tracer

// InitTracer 初始化全局 tracer
func InitTracer() {
	globalTracer = otel.Tracer(ServiceName)
}

// GetTracer 獲取全局 tracer
func GetTracer() trace.Tracer {
	if globalTracer == nil {
		InitTracer()
	}
	return globalTracer
}

// TracingHelper 提供便捷的 tracing 方法
type TracingHelper struct {
	tracer trace.Tracer
}

// NewTracingHelper 創建新的 TracingHelper
func NewTracingHelper() *TracingHelper {
	return &TracingHelper{
		tracer: GetTracer(),
	}
}

// StartSpan 開始一個新的 span
func (t *TracingHelper) StartSpan(ctx context.Context, operationName string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	ctx, span := t.tracer.Start(ctx, operationName)
	if len(attrs) > 0 {
		span.SetAttributes(attrs...)
	}
	return ctx, span
}

// AddEvent 向 span 添加事件
func (t *TracingHelper) AddEvent(span trace.Span, eventName string, attrs ...attribute.KeyValue) {
	if span != nil {
		span.AddEvent(eventName, trace.WithAttributes(attrs...))
	}
}

// SetAttributes 設置 span 屬性
func (t *TracingHelper) SetAttributes(span trace.Span, attrs ...attribute.KeyValue) {
	if span != nil {
		span.SetAttributes(attrs...)
	}
}

// RecordError 記錄錯誤到 span
func (t *TracingHelper) RecordError(span trace.Span, err error, description string, attrs ...attribute.KeyValue) {
	if span != nil {
		span.RecordError(err)
		if description != "" {
			span.SetStatus(codes.Error, description)
		}
		if len(attrs) > 0 {
			span.SetAttributes(attrs...)
		}
	}
}

// MarkSuccess 標記 span 為成功
func (t *TracingHelper) MarkSuccess(span trace.Span, attrs ...attribute.KeyValue) {
	if span != nil {
		span.SetStatus(codes.Ok, "")
		if len(attrs) > 0 {
			span.SetAttributes(attrs...)
		}
	}
}

// WithSpan 在 span 中執行函數（自動管理 span 生命週期）
func (t *TracingHelper) WithSpan(ctx context.Context, operationName string, fn func(context.Context, trace.Span) error, attrs ...attribute.KeyValue) error {
	ctx, span := t.StartSpan(ctx, operationName, attrs...)
	defer span.End()

	err := fn(ctx, span)
	if err != nil {
		t.RecordError(span, err, "Operation failed")
		return err
	}

	t.MarkSuccess(span)
	return nil
}

// 全局便捷函數
var defaultHelper = NewTracingHelper()

// StartSpan 全局函數，開始一個新的 span
func StartSpan(ctx context.Context, operationName string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	return defaultHelper.StartSpan(ctx, operationName, attrs...)
}

// AddEvent 全局函數，向 span 添加事件
func AddEvent(span trace.Span, eventName string, attrs ...attribute.KeyValue) {
	defaultHelper.AddEvent(span, eventName, attrs...)
}

// SetAttributes 全局函數，設置 span 屬性
func SetAttributes(span trace.Span, attrs ...attribute.KeyValue) {
	defaultHelper.SetAttributes(span, attrs...)
}

// RecordError 全局函數，記錄錯誤到 span
func RecordError(span trace.Span, err error, description string, attrs ...attribute.KeyValue) {
	defaultHelper.RecordError(span, err, description, attrs...)
}

// MarkSuccess 全局函數，標記 span 為成功
func MarkSuccess(span trace.Span, attrs ...attribute.KeyValue) {
	defaultHelper.MarkSuccess(span, attrs...)
}

// WithSpan 全局函數，在 span 中執行函數
func WithSpan(ctx context.Context, operationName string, fn func(context.Context, trace.Span) error, attrs ...attribute.KeyValue) error {
	return defaultHelper.WithSpan(ctx, operationName, fn, attrs...)
}

// 常用的屬性建構函數
func AttrString(key, value string) attribute.KeyValue {
	return attribute.String(key, value)
}

func AttrInt(key string, value int) attribute.KeyValue {
	return attribute.Int(key, value)
}

func AttrBool(key string, value bool) attribute.KeyValue {
	return attribute.Bool(key, value)
}

func AttrFloat64(key string, value float64) attribute.KeyValue {
	return attribute.Float64(key, value)
}

// 業務相關的屬性建構函數
func AttrDriverID(id string) attribute.KeyValue {
	return attribute.String("driver.id", id)
}

func AttrDriverAccount(account string) attribute.KeyValue {
	return attribute.String("driver.account", account)
}

func AttrUserID(id string) attribute.KeyValue {
	return attribute.String("user.id", id)
}

func AttrOrderID(id string) attribute.KeyValue {
	return attribute.String("order.id", id)
}

func AttrOperation(operation string) attribute.KeyValue {
	return attribute.String("service.operation", operation)
}

func AttrErrorType(errorType string) attribute.KeyValue {
	return attribute.String("error.type", errorType)
}

// Driver Controller 專用的 tracing helper 函數
func StartDriverControllerSpan(ctx context.Context, operation string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	operationName := "driver_controller_" + operation
	baseAttrs := []attribute.KeyValue{
		AttrOperation(operation),
	}
	baseAttrs = append(baseAttrs, attrs...)
	return StartSpan(ctx, operationName, baseAttrs...)
}

// Dispatcher 專用的 tracing helper 函數
func StartDispatcherSpan(ctx context.Context, operation string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	operationName := "dispatcher_" + operation
	baseAttrs := []attribute.KeyValue{
		AttrOperation(operation),
	}
	baseAttrs = append(baseAttrs, attrs...)
	return StartSpan(ctx, operationName, baseAttrs...)
}

// 記錄 driver controller 操作成功
func RecordDriverControllerSuccess(span trace.Span, driverID, orderID string, additionalAttrs ...attribute.KeyValue) {
	AddEvent(span, "operation_completed_successfully")
	baseAttrs := []attribute.KeyValue{
		AttrDriverID(driverID),
		AttrOrderID(orderID),
		AttrBool("operation.success", true),
	}
	baseAttrs = append(baseAttrs, additionalAttrs...)
	MarkSuccess(span, baseAttrs...)
}

// 記錄 driver controller 操作失敗
func RecordDriverControllerError(span trace.Span, err error, driverID, orderID, description string) {
	RecordError(span, err, description,
		AttrDriverID(driverID),
		AttrOrderID(orderID),
		AttrString("error", err.Error()),
		AttrBool("operation.success", false),
	)
	AddEvent(span, "operation_failed",
		AttrString("error", err.Error()),
	)
}

// Scheduled Dispatcher 專用的 tracing helper 函數
func StartScheduledDispatcherSpan(ctx context.Context, operation string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	operationName := "scheduled_dispatcher_" + operation
	baseAttrs := []attribute.KeyValue{
		AttrOperation(operation),
	}
	baseAttrs = append(baseAttrs, attrs...)
	return StartSpan(ctx, operationName, baseAttrs...)
}

// Order Controller 專用的 tracing helper 函數
func StartOrderControllerSpan(ctx context.Context, operation string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	operationName := "order_controller_" + operation
	baseAttrs := []attribute.KeyValue{
		AttrOperation(operation),
	}
	baseAttrs = append(baseAttrs, attrs...)
	return StartSpan(ctx, operationName, baseAttrs...)
}

// 記錄 order controller 操作成功
func RecordOrderControllerSuccess(span trace.Span, orderID string, additionalAttrs ...attribute.KeyValue) {
	AddEvent(span, "operation_completed_successfully")
	baseAttrs := []attribute.KeyValue{
		AttrOrderID(orderID),
		AttrBool("operation.success", true),
	}
	baseAttrs = append(baseAttrs, additionalAttrs...)
	MarkSuccess(span, baseAttrs...)
}

// 記錄 order controller 操作失敗
func RecordOrderControllerError(span trace.Span, err error, orderID, description string) {
	RecordError(span, err, description,
		AttrOrderID(orderID),
		AttrString("error", err.Error()),
		AttrBool("operation.success", false),
	)
	AddEvent(span, "operation_failed",
		AttrString("error", err.Error()),
	)
}
