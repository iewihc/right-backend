package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	websocketModels "right-backend/data-models/websocket"

	"github.com/rs/zerolog"
)

// WebSocketService 處理WebSocket相關業務邏輯
type WebSocketService struct {
	logger        zerolog.Logger
	driverService *DriverService
}

// NewWebSocketService 建立WebSocket服務
func NewWebSocketService(logger zerolog.Logger, driverService *DriverService) *WebSocketService {
	return &WebSocketService{
		logger:        logger.With().Str("module", "websocket_service").Logger(),
		driverService: driverService,
	}
}

// HandleCheckNotifyingOrder 處理檢查通知中訂單請求
func (ws *WebSocketService) HandleCheckNotifyingOrder(ctx context.Context, driverID string) (*websocketModels.CheckNotifyingOrderResponse, error) {
	// 呼叫 DriverService 檢查通知中訂單
	data, err := ws.driverService.CheckNotifyingOrder(ctx, driverID)
	if err != nil {
		ws.logger.Error().Err(err).Str("driver_id", driverID).Msg("檢查通知中訂單失敗")
		return &websocketModels.CheckNotifyingOrderResponse{
			Success: false,
			Message: "檢查通知中訂單失敗",
			Data:    nil,
			Error:   err.Error(),
		}, err
	}

	// 根據是否找到訂單設置不同的訊息
	message := "沒找到訂單"
	if data.HasNotifyingOrder {
		message = "找到通知中訂單"
	}

	return &websocketModels.CheckNotifyingOrderResponse{
		Success: true,
		Message: message,
		Data:    data,
	}, nil
}

// HandleCheckCancelingOrder 處理檢查取消中訂單請求
func (ws *WebSocketService) HandleCheckCancelingOrder(ctx context.Context, driverID string) (*websocketModels.CheckCancelingOrderResponse, error) {
	// 呼叫 DriverService 檢查取消中訂單
	data, err := ws.driverService.CheckCancelingOrder(ctx, driverID)
	if err != nil {
		ws.logger.Error().Err(err).Str("driver_id", driverID).Msg("檢查取消中訂單失敗")
		return &websocketModels.CheckCancelingOrderResponse{
			Success: false,
			Message: "檢查取消中訂單失敗",
			Data:    nil,
			Error:   err.Error(),
		}, err
	}

	ws.logger.Debug().
		Str("driver_id", driverID).
		Bool("has_canceling", data.HasCancelingOrder).
		Msg("WebSocket檢查取消中訂單成功")

	return &websocketModels.CheckCancelingOrderResponse{
		Success: true,
		Message: "檢查取消中訂單成功",
		Data:    data,
	}, nil
}

// HandleLocationUpdate 處理位置更新請求
func (ws *WebSocketService) HandleLocationUpdate(ctx context.Context, driverID string, locationData interface{}) (*websocketModels.LocationUpdateResponse, error) {
	// 將 data 轉換為 LocationUpdateRequest
	dataBytes, err := json.Marshal(locationData)
	if err != nil {
		ws.logger.Error().Err(err).Str("driver_id", driverID).Msg("位置更新數據序列化失敗")
		return &websocketModels.LocationUpdateResponse{
			Success: false,
			Message: "更新司機位置失敗",
		}, err
	}

	var locationUpdate websocketModels.LocationUpdateRequest
	if err := json.Unmarshal(dataBytes, &locationUpdate); err != nil {
		ws.logger.Error().Err(err).Str("driver_id", driverID).Msg("位置更新數據格式錯誤")
		return &websocketModels.LocationUpdateResponse{
			Success: false,
			Message: "更新司機位置失敗",
		}, err
	}

	// 驗證位置資料
	if locationUpdate.Lat == "" || locationUpdate.Lng == "" {
		ws.logger.Error().Str("driver_id", driverID).Str("lat", locationUpdate.Lat).Str("lng", locationUpdate.Lng).Msg("位置資料不完整")
		return &websocketModels.LocationUpdateResponse{
			Success: false,
			Message: "更新司機位置失敗",
		}, fmt.Errorf("位置資料不完整")
	}

	// 呼叫 DriverService 更新位置
	_, err = ws.driverService.UpdateDriverLocation(ctx, driverID, locationUpdate.Lat, locationUpdate.Lng)
	if err != nil {
		ws.logger.Error().Err(err).Str("driver_id", driverID).Msg("更新司機位置失敗")
		return &websocketModels.LocationUpdateResponse{
			Success: false,
			Message: "更新司機位置失敗",
		}, err
	}

	//ws.logger.Debug().Str("driver_id", driverID).Str("lat", locationUpdate.Lat).Str("lng", locationUpdate.Lng).Msg("📍 WebSocket司機位置已更新")

	return &websocketModels.LocationUpdateResponse{
		Success: true,
		Message: "司機位置已更新",
	}, nil
}

// CreatePongResponse 創建心跳回應
func (ws *WebSocketService) CreatePongResponse() *websocketModels.PongResponse {
	return &websocketModels.PongResponse{
		Timestamp: getCurrentTimestamp(),
	}
}

// SerializeMessage 序列化WebSocket消息
func (ws *WebSocketService) SerializeMessage(message websocketModels.WSMessage) ([]byte, error) {
	data, err := json.Marshal(message)
	if err != nil {
		ws.logger.Error().Err(err).Msg("序列化WebSocket消息失敗")
		return nil, err
	}
	return data, nil
}

// DeserializeMessage 反序列化WebSocket消息
func (ws *WebSocketService) DeserializeMessage(data []byte) (*websocketModels.WSMessage, error) {
	var message websocketModels.WSMessage
	if err := json.Unmarshal(data, &message); err != nil {
		ws.logger.Error().Err(err).Msg("反序列化WebSocket消息失敗")
		return nil, err
	}
	return &message, nil
}

// getCurrentTimestamp 獲取當前時間戳
func getCurrentTimestamp() int64 {
	return time.Now().Unix()
}
