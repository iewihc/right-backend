package service

import (
	"right-backend/model"
	"time"

	"github.com/rs/zerolog"
)

// SSEEventType 定義 SSE 事件類型 enum
type SSEEventType string

const (
	EventDriverAcceptedOrder SSEEventType = "driver_accepted_order"
	EventDriverRejectedOrder SSEEventType = "driver_rejected_order"
	EventDriverTimeoutOrder  SSEEventType = "driver_timeout_order"
	EventDriverArrived       SSEEventType = "driver_arrived"
	EventCustomerOnBoard     SSEEventType = "customer_on_board"
	EventOrderCompleted      SSEEventType = "order_completed"
	EventOrderFailed         SSEEventType = "order_failed"
	EventOrderCancelled      SSEEventType = "order_cancelled"
)

// SSEPageType 定義 SSE 頁面類型 enum
type SSEPageType string

const (
	PageDashboard SSEPageType = "dashboard"
	PageOrders    SSEPageType = "orders"
	PageDispatch  SSEPageType = "dispatch"
	PageMap       SSEPageType = "map"
	PageTracking  SSEPageType = "tracking"
)

// SSEPages 提供常用的頁面組合
var SSEPages = struct {
	OrderRelated []SSEPageType
	MapRelated   []SSEPageType
	All          []SSEPageType
}{
	OrderRelated: []SSEPageType{PageDashboard, PageOrders, PageDispatch},
	MapRelated:   []SSEPageType{PageMap, PageTracking},
	All:          []SSEPageType{PageDashboard, PageOrders, PageDispatch, PageMap, PageTracking},
}

// SSEBroadcaster 定義 SSE 廣播接口，避免循環依賴
type SSEBroadcaster interface {
	BroadcastPageUpdate(eventName string, pages []string, data interface{})
	BroadcastCustomEvent(eventType string, data interface{})
	GetStats() map[string]interface{}
}

// SSEService 負責管理所有 SSE 推送邏輯
type SSEService struct {
	logger      zerolog.Logger
	broadcaster SSEBroadcaster
}

// SSEEvent 定義 SSE 事件結構
type SSEEvent struct {
	EventName string      `json:"event_name"`
	Pages     []string    `json:"pages"`
	Data      interface{} `json:"data"`
}

// NewSSEService 建立新的 SSE 服務
func NewSSEService(logger zerolog.Logger, broadcaster SSEBroadcaster) *SSEService {
	return &SSEService{
		logger:      logger.With().Str("module", "sse_service").Logger(),
		broadcaster: broadcaster,
	}
}

// convertPagesToStrings 轉換 SSEPageType 陣列為 string 陣列
func convertPagesToStrings(pages []SSEPageType) []string {
	result := make([]string, len(pages))
	for i, page := range pages {
		result[i] = string(page)
	}
	return result
}

// PushOrderEvent 通用的訂單事件推播方法
// 外部調用者可直接傳入：事件類型、要刷新的頁面、訂單ID、車隊資訊、額外數據
// 注意：對於訂單相關事件，建議使用PushOrderEventWithDetails方法來確保包含完整的訂單信息
func (s *SSEService) PushOrderEvent(pages []SSEPageType, eventType SSEEventType, orderID string, fleet string, additionalData map[string]interface{}) {
	if s.broadcaster == nil {
		s.logger.Warn().Msg("SSE廣播器未初始化，跳過推送 (SSE broadcaster not initialized, skipping push)")
		return
	}

	// 構建基本的事件數據
	data := map[string]interface{}{
		"order_id":  orderID,
		"fleet":     fleet,
		"timestamp": time.Now().Format("15:04"),
	}

	// 合併額外的數據
	for key, value := range additionalData {
		data[key] = value
	}

	pagesStrings := convertPagesToStrings(pages)
	event := SSEEvent{
		EventName: string(eventType),
		Pages:     pagesStrings,
		Data:      data,
	}

	s.pushEventSafely(string(eventType), event)
}

// PushOrderEventWithDetails 推送包含完整訂單信息的事件
// 此方法確保所有訂單相關事件都包含ori_text和short_id字段
func (s *SSEService) PushOrderEventWithDetails(pages []SSEPageType, eventType SSEEventType, orderID string, fleet string, oriText string, shortID string, additionalData map[string]interface{}) {
	if s.broadcaster == nil {
		s.logger.Warn().Msg("SSE廣播器未初始化，跳過推送 (SSE broadcaster not initialized, skipping push)")
		return
	}

	// 構建基本的事件數據，包含必要的訂單字段
	data := map[string]interface{}{
		"order_id":  orderID,
		"fleet":     fleet,
		"ori_text":  oriText,
		"short_id":  shortID,
		"timestamp": time.Now().Format("15:04"),
	}

	// 合併額外的數據
	for key, value := range additionalData {
		data[key] = value
	}

	pagesStrings := convertPagesToStrings(pages)
	event := SSEEvent{
		EventName: string(eventType),
		Pages:     pagesStrings,
		Data:      data,
	}

	s.pushEventSafely(string(eventType), event)
}

// PushDriverLocationUpdate 推送司機位置更新事件 (非訂單相關)
func (s *SSEService) PushDriverLocationUpdate(driverID string, lat, lng float64, fleet string) {
	if s.broadcaster == nil {
		s.logger.Warn().Msg("SSE廣播器未初始化，跳過推送 (SSE broadcaster not initialized, skipping push)")
		return
	}

	event := SSEEvent{
		EventName: "driver_location_updated",
		Pages:     []string{"map", "tracking"},
		Data: map[string]interface{}{
			"driver_id": driverID,
			"latitude":  lat,
			"longitude": lng,
			"fleet":     fleet,
			"timestamp": time.Now().Format("15:04"),
		},
	}

	s.pushEventSafely("司機位置更新", event)
}

// PushCustomEvent 推送自定義事件 (通用方法)
func (s *SSEService) PushCustomEvent(eventName string, pages []string, data interface{}) {
	if s.broadcaster == nil {
		s.logger.Warn().Msg("SSE廣播器未初始化，跳過推送 (SSE broadcaster not initialized, skipping push)")
		return
	}

	event := SSEEvent{
		EventName: eventName,
		Pages:     pages,
		Data: map[string]interface{}{
			"timestamp": time.Now().Format("15:04"),
			"data":      data,
		},
	}

	s.pushEventSafely("自定義事件", event)
}

// PushPageRefresh 推送頁面刷新事件 (非訂單相關)
func (s *SSEService) PushPageRefresh(pages []string, reason string) {
	if s.broadcaster == nil {
		s.logger.Warn().Msg("SSE廣播器未初始化，跳過推送 (SSE broadcaster not initialized, skipping push)")
		return
	}

	event := SSEEvent{
		EventName: "page_refresh",
		Pages:     pages,
		Data: map[string]interface{}{
			"reason":    reason,
			"timestamp": time.Now().Format("15:04"),
		},
	}

	s.pushEventSafely("頁面刷新", event)
}

// PushOrderEventWithDriverInfo 推送包含司機資訊的訂單事件
// 此方法專門處理需要包含司機距離和時間信息的事件
func (s *SSEService) PushOrderEventWithDriverInfo(
	pages []SSEPageType,
	eventType SSEEventType,
	orderID, fleet, oriText, shortID string,
	driver *model.DriverInfo,
	distanceKm float64,
	estimatedMins int,
	additionalData map[string]interface{}) {

	if s.broadcaster == nil {
		s.logger.Warn().Msg("SSE廣播器未初始化，跳過推送 (SSE broadcaster not initialized, skipping push)")
		return
	}

	// 構建基本事件數據
	data := map[string]interface{}{
		"order_id":  orderID,
		"fleet":     fleet,
		"ori_text":  oriText,
		"short_id":  shortID,
		"timestamp": time.Now().Format("15:04"),
	}

	// 如果有司機資訊，也加入原始司機欄位
	if driver != nil {
		data["driver_id"] = driver.ID.Hex()
		data["driver_name"] = driver.Name
		data["car_plate"] = driver.CarPlate
		data["car_color"] = driver.CarColor
		data["car_model"] = driver.CarModel
		data["estimated_mins"] = estimatedMins
		data["distance_km"] = distanceKm
	}

	// 合併額外數據
	for key, value := range additionalData {
		data[key] = value
	}

	pagesStrings := convertPagesToStrings(pages)
	event := SSEEvent{
		EventName: string(eventType),
		Pages:     pagesStrings,
		Data:      data,
	}

	s.pushEventSafely(string(eventType), event)
}

// pushEventSafely 安全地推送事件，即使失敗也不會影響主要業務邏輯
func (s *SSEService) pushEventSafely(eventType string, event SSEEvent) {
	// 使用 goroutine 異步推送，避免阻塞主要業務邏輯
	go func() {
		defer func() {
			if r := recover(); r != nil {
				s.logger.Error().Str("event_type", eventType).Interface("panic", r).Msg("SSE事件推送發生 panic (SSE event push panicked)")
			}
		}()

		if s.broadcaster != nil {
			s.broadcaster.BroadcastPageUpdate(event.EventName, event.Pages, event.Data)
			//s.logger.Debug().Str("event_type", eventType).Strs("pages", event.Pages).Msg("SSE事件推送成功 (SSE event pushed successfully)")
		}
	}()
}

// GetSSEStats 獲取 SSE 連接統計資訊
func (s *SSEService) GetSSEStats() map[string]interface{} {
	if s.broadcaster == nil {
		return map[string]interface{}{
			"connected_clients": 0,
			"status":            "未初始化",
		}
	}

	stats := s.broadcaster.GetStats()
	stats["status"] = "運行中"
	return stats
}

// IsEnabled 檢查 SSE 服務是否已啟用
func (s *SSEService) IsEnabled() bool {
	return s.broadcaster != nil
}
