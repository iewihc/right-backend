package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"right-backend/auth"
	"right-backend/infra"
	"right-backend/middleware"
	"right-backend/model"
	"right-backend/service"
	"sync"
	"time"

	websocketModels "right-backend/data-models/websocket"

	"github.com/danielgtaylor/huma/v2"
	"github.com/gorilla/websocket"
	"github.com/rs/zerolog"
)

// WebSocketController 負責管理所有 WebSocket 連線與訊息推送。
// 支援司機和用戶的混合連接管理。
type WebSocketController struct {
	logger           zerolog.Logger
	driverService    *service.DriverService
	userService      *service.UserService
	websocketService *service.WebSocketService
	chatController   *ChatController
	jwtSecretKey     string
	upgrader         websocket.Upgrader
	connections      map[string]*websocketModels.Connection // 統一連接管理
	connectionsMu    sync.RWMutex
}

func NewWebSocketController(logger zerolog.Logger, driverService *service.DriverService, userService *service.UserService, chatController *ChatController, jwtSecretKey string) *WebSocketController {
	websocketService := service.NewWebSocketService(logger, driverService)
	wsc := &WebSocketController{
		logger:           logger.With().Str("module", "websocket_controller").Logger(),
		driverService:    driverService,
		userService:      userService,
		websocketService: websocketService,
		chatController:   chatController,
		jwtSecretKey:     jwtSecretKey,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin: func(r *http.Request) bool {
				return true // 允許跨域
			},
		},
		connections: make(map[string]*websocketModels.Connection),
	}

	go wsc.healthCheck()
	return wsc
}

// handleWebSocket 統一的WebSocket處理函數，支援driver和user
func (wsc *WebSocketController) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		wsc.logger.Error().Msg("缺少token參數")
		http.Error(w, "缺少token參數", http.StatusUnauthorized)
		return
	}

	// 驗證token並獲取用戶信息
	connInfo, err := wsc.validateToken(token)
	if err != nil {
		wsc.logger.Error().Err(err).Msg("token驗證失敗")
		http.Error(w, fmt.Sprintf("token驗證失敗: %v", err), http.StatusUnauthorized)
		return
	}

	conn, err := wsc.upgrader.Upgrade(w, r, nil)
	if err != nil {
		wsc.logger.Error().Err(err).Msg("WebSocket升級失敗")
		return
	}

	connection := &websocketModels.Connection{
		ID:           connInfo.ID,
		Type:         connInfo.Type,
		Conn:         conn,
		Fleet:        connInfo.Fleet,
		LastPing:     time.Now(),
		SendChannel:  make(chan []byte, 256),
		CloseChannel: make(chan struct{}),
		Status:       websocketModels.ConnectionStatusConnected,
		UserInfo:     connInfo.UserInfo,
	}

	wsc.registerConnection(connection)

	go wsc.handleSender(connection)
	go wsc.handleReader(connection)

	<-connection.CloseChannel
	wsc.unregisterConnection(connection)
}

// ConnectionInfo 連接信息結構
type ConnectionInfo struct {
	ID       string
	Type     websocketModels.ConnectionType
	Fleet    string
	UserInfo interface{} // 可以是 *model.DriverInfo 或 *model.User
}

// validateToken 通用token驗證，支援driver和user
func (wsc *WebSocketController) validateToken(tokenString string) (*ConnectionInfo, error) {
	// 使用與 REST API 相同的認證邏輯
	claims, err := auth.ValidateJWTToken(tokenString, wsc.jwtSecretKey)
	if err != nil {
		return nil, fmt.Errorf("invalid token: %v", err)
	}

	// 檢查 token 類型
	tokenType, ok := claims["type"].(string)
	if !ok {
		return nil, fmt.Errorf("missing token type")
	}

	switch tokenType {
	case string(model.TokenTypeDriver):
		return wsc.validateDriverTokenClaims(claims)
	case string(model.TokenTypeUser):
		return wsc.validateUserTokenClaims(claims)
	default:
		return nil, fmt.Errorf("unsupported token type: %s", tokenType)
	}
}

// validateDriverTokenClaims 驗證司機token
func (wsc *WebSocketController) validateDriverTokenClaims(claims map[string]interface{}) (*ConnectionInfo, error) {
	// 提取司機ID
	driverID, ok := claims["driver_id"].(string)
	if !ok {
		return nil, fmt.Errorf("missing driver_id in token")
	}

	// 從資料庫獲取完整司機資訊
	driver, err := wsc.driverService.GetDriverByID(context.Background(), driverID)
	if err != nil {
		return nil, fmt.Errorf("driver not found: %v", err)
	}

	return &ConnectionInfo{
		ID:       driver.ID.Hex(),
		Type:     websocketModels.ConnectionTypeDriver,
		Fleet:    string(driver.Fleet),
		UserInfo: driver,
	}, nil
}

// validateUserTokenClaims 驗證用戶token
func (wsc *WebSocketController) validateUserTokenClaims(claims map[string]interface{}) (*ConnectionInfo, error) {
	// 提取用戶ID
	userID, ok := claims["user_id"].(string)
	if !ok {
		return nil, fmt.Errorf("missing user_id in token")
	}

	// 從資料庫獲取完整用戶資訊
	user, err := wsc.userService.GetUserByID(context.Background(), userID)
	if err != nil {
		return nil, fmt.Errorf("user not found: %v", err)
	}

	return &ConnectionInfo{
		ID:       user.ID.Hex(),
		Type:     websocketModels.ConnectionTypeUser,
		Fleet:    string(user.Fleet),
		UserInfo: user,
	}, nil
}

// registerConnection 註冊通用連接
func (wsc *WebSocketController) registerConnection(conn *websocketModels.Connection) {
	wsc.connectionsMu.Lock()
	defer wsc.connectionsMu.Unlock()

	if existingConn, exists := wsc.connections[conn.ID]; exists {
		wsc.logger.Info().
			Str("user_id", conn.ID).
			Str("type", string(conn.Type)).
			Msg("用戶重複連線，關閉舊的連線")

		// 先從 map 中移除舊連線，避免競態條件
		delete(wsc.connections, conn.ID)

		// 然後關閉舊連線
		existingConn.CloseOnce.Do(func() {
			close(existingConn.CloseChannel)
		})
		existingConn.Conn.Close()
	}

	wsc.connections[conn.ID] = conn
}

// unregisterConnection 註銷通用連接
func (wsc *WebSocketController) unregisterConnection(conn *websocketModels.Connection) {
	wsc.connectionsMu.Lock()
	defer wsc.connectionsMu.Unlock()

	// Only remove the connection from the map if it's the one we're currently cleaning up.
	// This prevents the cleanup routine of an old connection from removing a newer one.
	if currentConn, exists := wsc.connections[conn.ID]; exists && currentConn == conn {
		delete(wsc.connections, conn.ID)
	}
}

// handleReader 處理通用連接的消息讀取
func (wsc *WebSocketController) handleReader(conn *websocketModels.Connection) {
	defer func() {
		// Add panic recovery as a safety net to prevent the whole server from crashing.
		if r := recover(); r != nil {
			wsc.logger.Error().Interface("panic", r).Msg("RECOVERED in handleReader from panic")
		}
		conn.CloseOnce.Do(func() {
			close(conn.CloseChannel)
		})
		conn.Conn.Close()
	}()

	conn.Conn.SetReadLimit(1024) // 設定讀取限制
	conn.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	conn.Conn.SetPongHandler(func(string) error {
		conn.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		wsc.connectionsMu.Lock()
		conn.LastPing = time.Now()
		wsc.connectionsMu.Unlock()
		return nil
	})

	for {
		_, messageBytes, err := conn.Conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure, websocket.CloseNormalClosure) {
				wsc.logger.Error().Err(err).Str("user_id", conn.ID).Msg("Websocket read error")
			} else {
				// 正常關閉，記錄為 Info 級別
				//wsc.logger.Info().Err(err).Str("user_id", conn.ID).Msg("Websocket 正常關閉")
			}
			break
		}

		// 解析收到的消息
		wsMessage, err := wsc.websocketService.DeserializeMessage(messageBytes)
		if err != nil {
			wsc.logger.Error().Err(err).Str("user_id", conn.ID).Msg("無法解析 WebSocket 消息")
			continue
		}

		// 根據連接類型處理不同的消息
		wsc.handleMessage(conn, *wsMessage)
	}
}

// handleSender 處理通用連接的消息發送
func (wsc *WebSocketController) handleSender(conn *websocketModels.Connection) {
	// 創建心跳定時器
	pingTicker := time.NewTicker(10 * time.Second)
	defer pingTicker.Stop()

	for {
		select {
		case message := <-conn.SendChannel:
			conn.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := conn.Conn.WriteMessage(websocket.TextMessage, message); err != nil {
				wsc.logger.Error().Err(err).Msg("發送訊息失敗")
				return
			}
		case <-pingTicker.C:
			// 發送 ping 消息保持連線活躍
			conn.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := conn.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				wsc.logger.Error().Err(err).Str("user_id", conn.ID).Msg("發送 ping 失敗")
				return
			}
		case <-conn.CloseChannel:
			return
		}
	}
}

// handleMessage 根據連接類型處理不同的消息
func (wsc *WebSocketController) handleMessage(conn *websocketModels.Connection, wsMessage websocketModels.WSMessage) {
	switch wsMessage.Type {
	case websocketModels.MessageTypePing:
		// 處理客戶端主動發送的 ping，更新最後活動時間
		wsc.connectionsMu.Lock()
		conn.LastPing = time.Now()
		wsc.connectionsMu.Unlock()

		// 回應 pong
		pongResponse := wsc.websocketService.CreatePongResponse()
		response := websocketModels.WSMessage{
			Type: websocketModels.MessageTypePong,
			Data: pongResponse,
		}
		wsc.sendResponseToConnection(conn, response)

	// 聊天相關消息處理 - 支援所有用戶類型
	case websocketModels.MessageTypeChatSendMessage:
		wsc.handleChatSendMessageForConnection(conn, wsMessage.Data)
	case websocketModels.MessageTypeChatGetHistory:
		wsc.handleChatGetHistoryForConnection(conn, wsMessage.Data)
	case websocketModels.MessageTypeChatMarkAsRead:
		wsc.handleChatMarkAsReadForConnection(conn, wsMessage.Data)
	case websocketModels.MessageTypeChatRecallMessage:
		wsc.handleChatRecallMessageForConnection(conn, wsMessage.Data)
	case websocketModels.MessageTypeChatTypingStart:
		wsc.handleChatTypingStartForConnection(conn, wsMessage.Data)
	case websocketModels.MessageTypeChatTypingEnd:
		wsc.handleChatTypingEndForConnection(conn, wsMessage.Data)

	// 司機特有的消息類型
	case websocketModels.MessageTypeCheckNotifyingOrder:
		if conn.Type == websocketModels.ConnectionTypeDriver {
			wsc.handleCheckNotifyingOrderForConnection(conn)
		}
	case websocketModels.MessageTypeCheckCancelingOrder:
		if conn.Type == websocketModels.ConnectionTypeDriver {
			wsc.handleCheckCancelingOrderForConnection(conn)
		}
	case websocketModels.MessageTypeLocationUpdate:
		if conn.Type == websocketModels.ConnectionTypeDriver {
			wsc.handleLocationUpdateForConnection(conn, wsMessage.Data)
		}

	default:
		wsc.logger.Warn().
			Str("user_id", conn.ID).
			Str("type", string(conn.Type)).
			Str("message_type", wsMessage.Type).
			Msg("未知的 WebSocket 消息類型")
	}
}

// sendResponseToConnection 發送回應給通用連接
func (wsc *WebSocketController) sendResponseToConnection(conn *websocketModels.Connection, message websocketModels.WSMessage) {
	data, err := wsc.websocketService.SerializeMessage(message)
	if err != nil {
		wsc.logger.Error().Err(err).Msg("序列化回應消息失敗")
		return
	}

	select {
	case conn.SendChannel <- data:
		// 成功發送
	default:
		wsc.logger.Error().Str("user_id", conn.ID).Msg("發送回應失敗：用戶的發送頻道已滿")
	}
}

// sendToDriver 將格式化後的訊息發送給指定司機。
func (wsc *WebSocketController) sendToDriver(driverID string, message websocketModels.WSMessage) bool {
	wsc.connectionsMu.RLock()
	conn, ok := wsc.connections[driverID]
	wsc.connectionsMu.RUnlock()

	if !ok {
		wsc.logger.Error().Str("driver_id", driverID).Msg("發送失敗：司機未連線")
		return false
	}

	data, err := wsc.websocketService.SerializeMessage(message)
	if err != nil {
		wsc.logger.Error().Err(err).Msg("序列化訊息失敗")
		return false
	}

	select {
	case conn.SendChannel <- data:
		return true
	default:
		wsc.logger.Error().Str("driver_id", driverID).Msg("發送失敗：司機的發送頻道已滿")
		return false
	}
}

func (wsc *WebSocketController) healthCheck() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		wsc.connectionsMu.Lock()
		var toRemove []string
		for userID, conn := range wsc.connections {
			if time.Since(conn.LastPing) > 60*time.Second {
				wsc.logger.Info().Str("user_id", userID).Msg("用戶連線超時，關閉連線")
				// 使用 closeOnce 確保只關閉一次
				conn.CloseOnce.Do(func() {
					close(conn.CloseChannel)
				})
				conn.Conn.Close()
				toRemove = append(toRemove, userID)
			}
		}
		// 批量移除超時的連線
		for _, userID := range toRemove {
			delete(wsc.connections, userID)
		}
		wsc.connectionsMu.Unlock()
	}
}

func (wsc *WebSocketController) RegisterRoutes(api huma.API) {}

func (wsc *WebSocketController) GetWebSocketHandler() http.HandlerFunc {
	return wsc.handleWebSocket
}

func (wsc *WebSocketController) GetStats() *websocketModels.ConnectionStats {
	wsc.connectionsMu.RLock()
	defer wsc.connectionsMu.RUnlock()

	connectionsByFleet := make(map[string]int)
	connectionsByType := make(map[string]int)
	connectionStatus := make(map[string]websocketModels.ConnectionStatus)

	driverCount := 0
	userCount := 0

	for userID, conn := range wsc.connections {
		// 統計各車隊的連線數
		connectionsByFleet[conn.Fleet]++
		// 統計各類型的連線數
		connectionsByType[string(conn.Type)]++
		// 記錄連線狀態
		connectionStatus[userID] = conn.Status

		// 統計用戶類型
		switch conn.Type {
		case websocketModels.ConnectionTypeDriver:
			driverCount++
		case websocketModels.ConnectionTypeUser:
			userCount++
		}
	}

	return &websocketModels.ConnectionStats{
		ConnectedDrivers:   driverCount,
		ConnectedUsers:     userCount,
		TotalConnections:   len(wsc.connections),
		ConnectionsByFleet: connectionsByFleet,
		ConnectionsByType:  connectionsByType,
		ConnectionStatus:   connectionStatus,
	}
}

// 聊天相關處理方法 - 通用連接版本

func (wsc *WebSocketController) handleChatSendMessageForConnection(conn *websocketModels.Connection, data interface{}) {
	ctx := context.Background()
	startTime := time.Now()

	senderType := model.SenderTypeDriver
	if conn.Type == websocketModels.ConnectionTypeUser {
		senderType = model.SenderTypeUser
	}

	responseData, err := wsc.chatController.HandleChatSendMessage(ctx, conn.ID, senderType, data)
	if err != nil {
		duration := time.Since(startTime)
		wsc.logger.Error().
			Err(err).
			Str("user_id", conn.ID).
			Str("type", string(conn.Type)).
			Dur("duration", duration).
			Msg("處理發送聊天消息失敗")

		errorCode := websocketModels.ChatErrorInvalidMessage
		if fmt.Sprintf("%v", err) == "訂單不存在" {
			errorCode = websocketModels.ChatErrorOrderNotFound
		}

		wsc.sendChatErrorToConnection(conn, errorCode, err.Error(), nil)
		return
	}

	duration := time.Since(startTime)
	wsc.logger.Info().
		Str("user_id", conn.ID).
		Str("type", string(conn.Type)).
		Str("message_id", responseData.Message.ID).
		Dur("duration", duration).
		Msg("聊天消息處理成功")

	// 廣播消息給相關用戶
	wsc.broadcastChatMessageToAll(responseData.OrderID, *responseData, conn.ID)

	// 發送簡單的成功確認給發送者
	successResponse := websocketModels.WSMessage{
		Type: websocketModels.MessageTypeChatSendResponse,
		Data: map[string]interface{}{
			"success":    true,
			"message_id": responseData.Message.ID,
			"timestamp":  responseData.Message.Timestamp,
		},
	}
	wsc.sendResponseToConnection(conn, successResponse)
}

func (wsc *WebSocketController) handleChatGetHistoryForConnection(conn *websocketModels.Connection, data interface{}) {
	ctx := context.Background()

	senderType := model.SenderTypeDriver
	if conn.Type == websocketModels.ConnectionTypeUser {
		senderType = model.SenderTypeUser
	}

	responseData, err := wsc.chatController.HandleChatGetHistory(ctx, conn.ID, senderType, data)
	if err != nil {
		wsc.logger.Error().Err(err).Str("user_id", conn.ID).Msg("處理獲取聊天歷史失敗")
		wsc.sendChatErrorToConnection(conn, websocketModels.ChatErrorInvalidMessage, err.Error(), nil)
		return
	}

	response := websocketModels.WSMessage{
		Type: websocketModels.MessageTypeChatHistoryResponse,
		Data: responseData,
	}
	wsc.sendResponseToConnection(conn, response)
}

func (wsc *WebSocketController) handleChatMarkAsReadForConnection(conn *websocketModels.Connection, data interface{}) {
	ctx := context.Background()

	senderType := model.SenderTypeDriver
	if conn.Type == websocketModels.ConnectionTypeUser {
		senderType = model.SenderTypeUser
	}

	err := wsc.chatController.HandleChatMarkAsRead(ctx, conn.ID, senderType, data)
	if err != nil {
		wsc.logger.Error().Err(err).Str("user_id", conn.ID).Msg("處理標記已讀失敗")
		wsc.sendChatErrorToConnection(conn, websocketModels.ChatErrorInvalidMessage, err.Error(), nil)
		return
	}

	wsc.logger.Debug().Str("user_id", conn.ID).Msg("已標記聊天為已讀")
}

func (wsc *WebSocketController) handleChatRecallMessageForConnection(conn *websocketModels.Connection, data interface{}) {
	ctx := context.Background()
	startTime := time.Now()

	senderType := model.SenderTypeDriver
	if conn.Type == websocketModels.ConnectionTypeUser {
		senderType = model.SenderTypeUser
	}

	responseData, err := wsc.chatController.HandleChatRecallMessage(ctx, conn.ID, senderType, data)
	if err != nil {
		duration := time.Since(startTime)
		wsc.logger.Error().
			Err(err).
			Str("user_id", conn.ID).
			Dur("duration", duration).
			Msg("處理收回聊天訊息失敗")

		var errorCode websocketModels.ChatErrorType
		switch err.Error() {
		case "訊息不存在":
			errorCode = websocketModels.ChatErrorMessageNotFound
		case "收回時間已超過24小時":
			errorCode = websocketModels.ChatErrorRecallTimeExceeded
		case "沒有權限收回此訊息":
			errorCode = websocketModels.ChatErrorPermissionDenied
		default:
			errorCode = websocketModels.ChatErrorRecallFailed
		}

		wsc.sendChatErrorToConnection(conn, errorCode, err.Error(), nil)
		return
	}

	duration := time.Since(startTime)
	wsc.logger.Info().
		Str("user_id", conn.ID).
		Str("message_id", responseData.MessageID).
		Dur("duration", duration).
		Msg("聊天訊息收回成功")

	wsc.broadcastMessageRecalledToAll(responseData.OrderID, *responseData)
}

func (wsc *WebSocketController) handleChatTypingStartForConnection(conn *websocketModels.Connection, data interface{}) {
	var request websocketModels.ChatTypingRequest
	if err := wsc.parseTypingData(data, &request); err != nil {
		wsc.logger.Error().
			Err(err).
			Str("user_id", conn.ID).
			Msg("解析輸入狀態請求失敗")
		return
	}

	wsc.broadcastTypingStatusToAll(request.OrderID, conn.ID, true, conn.ID)
}

func (wsc *WebSocketController) handleChatTypingEndForConnection(conn *websocketModels.Connection, data interface{}) {
	var request websocketModels.ChatTypingRequest
	if err := wsc.parseTypingData(data, &request); err != nil {
		wsc.logger.Error().
			Err(err).
			Str("user_id", conn.ID).
			Msg("解析輸入狀態請求失敗")
		return
	}

	wsc.broadcastTypingStatusToAll(request.OrderID, conn.ID, false, conn.ID)
}

// 司機特有功能的處理方法 - 通用連接版本

func (wsc *WebSocketController) handleCheckNotifyingOrderForConnection(conn *websocketModels.Connection) {
	ctx := context.Background()

	responseData, _ := wsc.websocketService.HandleCheckNotifyingOrder(ctx, conn.ID)

	response := websocketModels.WSMessage{
		Type: websocketModels.MessageTypeCheckNotifyingOrderResponse,
		Data: responseData,
	}
	wsc.sendResponseToConnection(conn, response)
}

func (wsc *WebSocketController) handleCheckCancelingOrderForConnection(conn *websocketModels.Connection) {
	ctx := context.Background()

	responseData, _ := wsc.websocketService.HandleCheckCancelingOrder(ctx, conn.ID)

	response := websocketModels.WSMessage{
		Type: websocketModels.MessageTypeCheckCancelingOrderResponse,
		Data: responseData,
	}
	wsc.sendResponseToConnection(conn, response)
}

func (wsc *WebSocketController) handleLocationUpdateForConnection(conn *websocketModels.Connection, data interface{}) {
	ctx := context.Background()

	// 添加 WebSocket 位置更新的 tracing
	ctx, span := infra.StartSpan(ctx, "websocket_location_update",
		infra.AttrOperation("location_update"),
		infra.AttrString("driver_id", conn.ID),
		infra.AttrString("connection_type", "websocket"),
		infra.AttrString("fleet", conn.Fleet),
	)
	defer span.End()

	infra.AddEvent(span, "websocket_location_update_started",
		infra.AttrString("driver_id", conn.ID),
	)

	responseData, err := wsc.websocketService.HandleLocationUpdate(ctx, conn.ID, data)
	if err != nil {
		// 記錄失敗的 metric
		middleware.RecordWebSocketLocationUpdate(conn.ID, conn.Fleet, "error")

		infra.RecordError(span, err, "WebSocket位置更新失敗",
			infra.AttrString("driver_id", conn.ID),
			infra.AttrString("error", err.Error()),
		)
		wsc.logger.Error().Err(err).Str("driver_id", conn.ID).Msg("WebSocket位置更新失敗")
		return
	}

	// 記錄成功的 metric
	middleware.RecordWebSocketLocationUpdate(conn.ID, conn.Fleet, "success")

	infra.AddEvent(span, "websocket_location_update_completed")
	infra.MarkSuccess(span,
		infra.AttrString("driver_id", conn.ID),
		infra.AttrString("connection_type", "websocket"),
	)

	response := websocketModels.WSMessage{
		Type: websocketModels.MessageTypeLocationUpdateResponse,
		Data: responseData,
	}
	wsc.sendResponseToConnection(conn, response)
}

// 輔助方法

func (wsc *WebSocketController) sendChatErrorToConnection(conn *websocketModels.Connection, errorCode websocketModels.ChatErrorType, message string, orderID *string) {
	errorEvent := websocketModels.ChatErrorEvent{
		Code:    errorCode,
		Message: message,
		OrderID: orderID,
	}

	response := websocketModels.WSMessage{
		Type: websocketModels.MessageTypeChatError,
		Data: errorEvent,
	}

	wsc.sendResponseToConnection(conn, response)
}

func (wsc *WebSocketController) broadcastChatMessageToAll(orderID string, message websocketModels.ChatReceiveMessageEvent, excludeUserID string) {
	// 廣播給所有相關用戶，除了發送者
	wsc.connectionsMu.RLock()
	defer wsc.connectionsMu.RUnlock()

	broadcastMessage := websocketModels.WSMessage{
		Type: websocketModels.MessageTypeChatReceiveMessage,
		Data: message,
	}

	for userID, conn := range wsc.connections {
		if userID != excludeUserID {
			wsc.sendResponseToConnection(conn, broadcastMessage)
		}
	}
}

func (wsc *WebSocketController) broadcastMessageRecalledToAll(orderID string, recallEvent websocketModels.ChatMessageRecalledEvent) {
	wsc.connectionsMu.RLock()
	defer wsc.connectionsMu.RUnlock()

	broadcastMessage := websocketModels.WSMessage{
		Type: websocketModels.MessageTypeChatMessageRecalled,
		Data: recallEvent,
	}

	for _, conn := range wsc.connections {
		wsc.sendResponseToConnection(conn, broadcastMessage)
	}
}

func (wsc *WebSocketController) broadcastTypingStatusToAll(orderID, userID string, isTyping bool, excludeUserID string) {
	wsc.connectionsMu.RLock()
	defer wsc.connectionsMu.RUnlock()

	typingEvent := websocketModels.ChatTypingUpdateEvent{
		OrderID:  orderID,
		UserID:   userID,
		IsTyping: isTyping,
	}

	broadcastMessage := websocketModels.WSMessage{
		Type: websocketModels.MessageTypeChatTypingUpdate,
		Data: typingEvent,
	}

	for connUserID, conn := range wsc.connections {
		if connUserID != excludeUserID {
			wsc.sendResponseToConnection(conn, broadcastMessage)
		}
	}
}

// parseTypingData 解析輸入狀態數據
func (wsc *WebSocketController) parseTypingData(data interface{}, target *websocketModels.ChatTypingRequest) error {
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("序列化數據失敗: %w", err)
	}

	if err := json.Unmarshal(jsonBytes, target); err != nil {
		return fmt.Errorf("反序列化失敗: %w", err)
	}

	return nil
}

// BroadcastChatMessage 廣播聊天消息給相關用戶
func (wsc *WebSocketController) BroadcastChatMessage(orderID string, message websocketModels.ChatReceiveMessageEvent) {
	wsc.connectionsMu.RLock()
	defer wsc.connectionsMu.RUnlock()

	broadcastMessage := websocketModels.WSMessage{
		Type: websocketModels.MessageTypeChatReceiveMessage,
		Data: message,
	}

	// 廣播給所有連線的用戶
	for _, conn := range wsc.connections {
		wsc.sendResponseToConnection(conn, broadcastMessage)
	}
}

// BroadcastUnreadCountUpdate 廣播未讀數量更新
func (wsc *WebSocketController) BroadcastUnreadCountUpdate(orderID, userID string, unreadCount int) {
	wsc.connectionsMu.RLock()
	defer wsc.connectionsMu.RUnlock()

	updateEvent := websocketModels.ChatUnreadCountUpdateEvent{
		OrderID:     orderID,
		UnreadCount: unreadCount,
	}

	broadcastMessage := websocketModels.WSMessage{
		Type: websocketModels.MessageTypeChatUnreadCountUpdate,
		Data: updateEvent,
	}

	// 發送給指定用戶
	if conn, exists := wsc.connections[userID]; exists {
		wsc.logger.Debug().
			Str("order_id", orderID).
			Str("user_id", userID).
			Int("unread_count", unreadCount).
			Msg("廣播未讀數量更新")

		wsc.sendResponseToConnection(conn, broadcastMessage)
	}
}
