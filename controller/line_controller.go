package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/line/line-bot-sdk-go/v8/linebot/webhook"

	"right-backend/model"
	"right-backend/service"

	"github.com/rs/zerolog"
)

// LineConfig 代表單一 LINE 配置
type LineConfig struct {
	ID            string `json:"id"`             // 配置 ID
	Name          string `json:"name"`           // 配置名稱
	ChannelSecret string `json:"channel_secret"` // LINE Channel Secret
	ChannelToken  string `json:"channel_token"`  // LINE Channel Access Token
}

// LineController 負責處理來自 LINE 的 Webhook 事件
type LineController struct {
	logger              zerolog.Logger
	driverSvc           *service.DriverService
	lineService         *service.LineService
	orderSvc            *service.OrderService
	notificationService *service.NotificationService
	configs             map[string]*LineConfig // 以 ID 為鍵的配置映射
}

// NewLineController 建立一個新的 LineController
func NewLineController(logger zerolog.Logger, driverSvc *service.DriverService, lineService *service.LineService, orderSvc *service.OrderService, notificationService *service.NotificationService, configs []*LineConfig) *LineController {
	lc := &LineController{
		logger:              logger.With().Str("module", "line_controller").Logger(),
		driverSvc:           driverSvc,
		lineService:         lineService,
		orderSvc:            orderSvc,
		notificationService: notificationService,
		configs:             make(map[string]*LineConfig),
	}

	// 加載配置
	for _, config := range configs {
		lc.AddLineConfig(config)
	}

	return lc
}

// AddLineConfig 添加 LINE 配置
func (lc *LineController) AddLineConfig(config *LineConfig) {
	lc.configs[config.ID] = config
	lc.logger.Info().
		Str("config_id", config.ID).
		Str("config_name", config.Name).
		Msg("已添加 LINE 配置")
}

// RemoveLineConfig 移除 LINE 配置
func (lc *LineController) RemoveLineConfig(configID string) {
	delete(lc.configs, configID)
	lc.logger.Info().
		Str("config_id", configID).
		Msg("已移除 LINE 配置")
}

// GetLineConfig 獲取 LINE 配置
func (lc *LineController) GetLineConfig(configID string) (*LineConfig, error) {
	config, exists := lc.configs[configID]
	if !exists {
		return nil, errors.New("找不到指定的 LINE 配置")
	}
	return config, nil
}

// WebhookInput 定義了 LINE Webhook Handler 的輸入結構
type WebhookInput struct {
	ConfigID       string `path:"configID"`
	XLineSignature string `header:"X-Line-Signature"`

	// 這個欄位會在 Handler 被呼叫前，由 Resolve 方法填入
	BodyBytes []byte `doc:"-"`
}

// Resolve 實現了 huma.Resolver 介面
func (i *WebhookInput) Resolve(ctx huma.Context) []error {
	if i.XLineSignature == "" {
		return []error{huma.NewError(http.StatusBadRequest, "缺少 X-Line-Signature 標頭")}
	}

	body, err := io.ReadAll(ctx.BodyReader())
	if err != nil {
		return []error{huma.NewError(http.StatusInternalServerError, "讀取請求內文失敗", err)}
	}
	i.BodyBytes = body
	return nil
}

// WebhookOutput 定義了 Webhook Handler 的輸出結構
type WebhookOutput struct {
	Body string
}

// RegisterRoutes 註冊 LINE Webhook 的路由
func (lc *LineController) RegisterRoutes(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "line-webhook",
		Method:      http.MethodPost,
		Path:        "/line/webhook/{configID}",
		Summary:     "LINE Bot Webhook",
		Description: "處理來自 LINE Platform 的 Webhook 事件，根據配置 ID 路由到對應的 LINE 設定",
		Tags:        []string{"LINE"},
	}, lc.Webhook)
}

// Webhook 是處理傳入 LINE 事件的主要函式
func (lc *LineController) Webhook(ctx context.Context, input *WebhookInput) (*WebhookOutput, error) {
	log.Printf("收到來自配置 ID '%s' 的 Webhook 請求", input.ConfigID)

	// 獲取對應的配置
	config, err := lc.GetLineConfig(input.ConfigID)
	if err != nil {
		log.Printf("找不到配置 %s: %v", input.ConfigID, err)
		return nil, huma.NewError(http.StatusNotFound, "找不到指定的 LINE 配置")
	}

	// 驗證簽名
	if !webhook.ValidateSignature(config.ChannelSecret, input.XLineSignature, input.BodyBytes) {
		log.Printf("配置 %s 的簽名無效", input.ConfigID)
		return nil, huma.NewError(http.StatusUnauthorized, "簽名無效")
	}

	// 將請求內文解析為一個通用的 map
	var rawData map[string]interface{}
	if err := json.Unmarshal(input.BodyBytes, &rawData); err != nil {
		log.Printf("解析配置 %s 的原始 JSON 內文時發生錯誤: %v", input.ConfigID, err)
		return nil, huma.NewError(http.StatusBadRequest, "解析請求內文時發生錯誤")
	}

	// 在背景執行事件處理，避免阻塞 Webhook 的回應
	go lc.handleEvents(context.Background(), rawData, config)

	return &WebhookOutput{Body: "OK"}, nil
}

// handleEvents 負責處理事件
func (lc *LineController) handleEvents(ctx context.Context, data map[string]interface{}, config *LineConfig) {
	events, ok := data["events"].([]interface{})
	if !ok {
		log.Printf("[警告] 收到的資料不包含 'events' 陣列，將印出完整資料：")
		prettyPrintJSON(data)
		return
	}

	for _, eventData := range events {
		eventMap, ok := eventData.(map[string]interface{})
		if !ok {
			continue
		}

		eventType, _ := eventMap["type"].(string)

		// 處理訊息事件
		if eventType == "message" {
			lc.handleMessageEvent(ctx, eventMap, config)
		}

		// 處理 postback 事件（按鈕點擊等）
		if eventType == "postback" {
			lc.handlePostbackEvent(ctx, eventMap, config)
		}
	}
}

// handleMessageEvent 處理訊息事件
func (lc *LineController) handleMessageEvent(ctx context.Context, eventMap map[string]interface{}, config *LineConfig) {
	source, sourceOk := eventMap["source"].(map[string]interface{})
	if !sourceOk {
		return
	}

	// 支援群組和個人對話
	var sourceID string
	var sourceType string

	if groupID, ok := source["groupId"].(string); ok && groupID != "" {
		sourceID = groupID
		sourceType = "group"
	} else if userID, ok := source["userId"].(string); ok && userID != "" {
		sourceID = userID
		sourceType = "user"
	} else {
		lc.logger.Warn().Msg("無法獲取 LINE 來源 ID")
		return
	}

	// 獲取 reply token
	replyToken, _ := eventMap["replyToken"].(string)

	// 預設發送者是未知的
	senderName := "未知使用者"
	if sourceType == "user" {
		// 個人對話時嘗試查找司機資訊
		driver, err := lc.driverSvc.GetDriverByLineUID(ctx, sourceID)
		if err == nil && driver != nil {
			senderName = driver.Name
		}
	} else {
		// 群組對話
		senderName = fmt.Sprintf("群組[%s]", sourceID)
	}

	message, messageOk := eventMap["message"].(map[string]interface{})
	if !messageOk {
		return
	}

	if messageText, textOk := message["text"].(string); textOk {
		lc.logger.Info().
			Str("config_name", config.Name).
			Str("sender", senderName).
			Str("message", messageText).
			Msg("收到 LINE 文字訊息")

		// 處理文字訊息
		lc.handleTextMessage(ctx, messageText, config.ID, sourceID, replyToken)
	} else {
		messageType, _ := message["type"].(string)
		lc.logger.Info().
			Str("config_name", config.Name).
			Str("sender", senderName).
			Str("message_type", messageType).
			Msg("收到 LINE 非文字訊息")
	}
}

// handleTextMessage 處理文字訊息
func (lc *LineController) handleTextMessage(ctx context.Context, messageText, configID, sourceID, replyToken string) {
	// 檢查是否為交互指令
	if lc.handleInteractiveCommand(ctx, messageText, configID, sourceID, replyToken) {
		return
	}

	// 檢查格式是否包含斜線分隔（類似 Discord）
	if !strings.Contains(messageText, "/") {
		// 不是訂單格式，回覆說明訊息
		helpMessage := "請使用正確的訂單格式，例如：\n台北車站/松山機場"
		lc.lineService.ReplyMessage(configID, replyToken, helpMessage)
		return
	}

	// TODO: 防止重複處理相同訊息 (類似 Discord 的重複檢查機制)
	// messageProcessingKey := fmt.Sprintf("line_msg_processed:%s:%s", configID, lineUID)

	// 發送 "創建中" Flex Message 回覆
	creatingFlexMessage := lc.lineService.GetCreatingFlexMessage()
	err := lc.lineService.ReplyFlexMessage(configID, replyToken, creatingFlexMessage)
	if err != nil {
		lc.logger.Error().
			Err(err).
			Str("config_id", configID).
			Str("source_id", sourceID).
			Msg("發送創建中 Flex Message 回覆失敗")
		return
	}

	// 創建訂單
	createdOrder, err := lc.lineService.CreateOrderFromMessage(ctx, messageText, configID, sourceID)
	if err != nil {
		lc.logger.Error().
			Err(err).
			Str("config_id", configID).
			Str("source_id", sourceID).
			Str("message", messageText).
			Msg("從 LINE 訊息創建訂單失敗")

		// 發送錯誤訊息
		errorMessage := fmt.Sprintf("❌ 訂單建立失敗\n\n原因: %v", err)
		lc.lineService.PushMessage(configID, sourceID, errorMessage)
		return
	}

	// 發送訂單確認 Flex Message
	confirmationFlexMessage := lc.lineService.FormatOrderMessage(createdOrder)
	err = lc.lineService.PushFlexMessage(configID, sourceID, confirmationFlexMessage)
	if err != nil {
		lc.logger.Error().
			Err(err).
			Str("order_id", createdOrder.ID.Hex()).
			Msg("發送訂單確認 Flex Message 失敗")
	} else {
		lc.logger.Info().
			Str("order_id", createdOrder.ID.Hex()).
			Str("short_id", createdOrder.ShortID).
			Str("config_id", configID).
			Str("source_id", sourceID).
			Msg("LINE 訂單創建完成")
	}
}

// handleInteractiveCommand 處理交互指令
func (lc *LineController) handleInteractiveCommand(ctx context.Context, messageText, configID, sourceID, replyToken string) bool {
	messageText = strings.TrimSpace(messageText)

	lc.logger.Debug().
		Str("message", messageText).
		Str("config_id", configID).
		Str("source_id", sourceID).
		Msg("檢查交互指令")

	// 處理取消指令
	if strings.HasPrefix(messageText, "取消 ") {
		orderID := strings.TrimPrefix(messageText, "取消 ")
		lc.logger.Info().
			Str("extracted_order_id", orderID).
			Str("original_message", messageText).
			Msg("識別到取消指令")
		lc.handleCancelCommand(ctx, orderID, configID, sourceID, replyToken)
		return true
	}

	// 處理重派指令
	if strings.HasPrefix(messageText, "重派 ") {
		orderID := strings.TrimPrefix(messageText, "重派 ")
		lc.logger.Info().
			Str("extracted_order_id", orderID).
			Str("original_message", messageText).
			Msg("識別到重派指令")
		lc.handleRedispatchCommand(ctx, orderID, configID, sourceID, replyToken)
		return true
	}

	// 處理重新派單指令（舊格式兼容）
	if strings.HasPrefix(messageText, "重新派單 ") {
		shortID := strings.TrimPrefix(messageText, "重新派單 ")
		lc.handleRedispatchCommand(ctx, shortID, configID, sourceID, replyToken)
		return true
	}

	// 處理狀態查詢指令
	if strings.HasPrefix(messageText, "查詢 ") {
		shortID := strings.TrimPrefix(messageText, "查詢 ")
		lc.handleStatusInquiry(ctx, shortID, configID, sourceID, replyToken)
		return true
	}

	return false
}

// handleCancelCommand 處理取消指令
func (lc *LineController) handleCancelCommand(ctx context.Context, orderID, configID, sourceID, replyToken string) {
	lc.logger.Info().
		Str("order_id", orderID).
		Str("config_id", configID).
		Str("source_id", sourceID).
		Msg("處理 LINE 取消指令")

	// 使用統一的取消服務（包含所有驗證邏輯）
	updatedOrder, err := lc.orderSvc.CancelOrder(ctx, orderID, "LINE取消", "LINE用戶")
	if err != nil {
		lc.logger.Error().Err(err).Str("order_id", orderID).Msg("LINE取消訂單失敗")
		lc.lineService.ReplyMessage(configID, replyToken, fmt.Sprintf("❌ %s", err.Error()))
		return
	}

	// 4. 回覆成功訊息
	lc.logger.Info().
		Str("order_id", orderID).
		Str("short_id", updatedOrder.ShortID).
		Msg("LINE 取消訂單成功")

	lc.lineService.ReplyMessage(configID, replyToken,
		fmt.Sprintf("✅ 訂單 %s 已成功取消", updatedOrder.ShortID))
}

// handleRedispatchCommand 處理重派指令
func (lc *LineController) handleRedispatchCommand(ctx context.Context, orderID, configID, sourceID, replyToken string) {
	lc.logger.Info().
		Str("order_id", orderID).
		Str("config_id", configID).
		Str("source_id", sourceID).
		Msg("處理 LINE 重派指令")

	// 1. 獲取訂單資訊
	currentOrder, err := lc.orderSvc.GetOrderByID(ctx, orderID)
	if err != nil {
		lc.logger.Error().Err(err).Str("order_id", orderID).Msg("訂單不存在")
		lc.lineService.ReplyMessage(configID, replyToken, "❌ 訂單不存在或已被刪除")
		return
	}

	// 2. 驗證訂單狀態 - 通常是失敗狀態才需要重派
	if !lc.isOrderRedispatchable(currentOrder.Status) {
		lc.logger.Warn().
			Str("order_id", orderID).
			Str("current_status", string(currentOrder.Status)).
			Msg("訂單狀態不需要重派")
		lc.lineService.ReplyMessage(configID, replyToken,
			fmt.Sprintf("❌ 訂單狀態為「%s」，無需重派。只有失敗狀態的訂單可以重派", currentOrder.Status))
		return
	}

	// 3. 執行重派操作
	redispatchedOrder, err := lc.orderSvc.RedispatchOrder(ctx, orderID)
	if err != nil {
		lc.logger.Error().Err(err).Str("order_id", orderID).Msg("重派訂單失敗")
		lc.lineService.ReplyMessage(configID, replyToken, "❌ 重派訂單失敗，請稍後再試")
		return
	}

	// 4. 回覆成功訊息
	lc.logger.Info().
		Str("order_id", orderID).
		Str("short_id", redispatchedOrder.ShortID).
		Str("previous_status", string(currentOrder.Status)).
		Msg("LINE 重派訂單成功")

	lc.lineService.ReplyMessage(configID, replyToken,
		fmt.Sprintf("✅ 訂單 %s 已重新派單，正在尋找司機", redispatchedOrder.ShortID))
}

// handleStatusInquiry 處理狀態查詢指令
func (lc *LineController) handleStatusInquiry(ctx context.Context, shortID, configID, sourceID, replyToken string) {
	lc.logger.Info().
		Str("short_id", shortID).
		Str("config_id", configID).
		Str("source_id", sourceID).
		Msg("處理 LINE 狀態查詢指令")

	// 暫時回覆查詢結果
	replyMsg := fmt.Sprintf("查詢訂單 %s 的狀態...", shortID)
	lc.lineService.ReplyMessage(configID, replyToken, replyMsg)

	// TODO: 實現狀態查詢邏輯
	// order := orderService.GetOrderByShortID(ctx, shortID)
	// statusMessage := lineService.FormatOrderMessage(order)
	// lineService.PushMessage(configID, lineUID, statusMessage)
}

// handlePostbackEvent 處理 postback 事件（按鈕點擊等）
func (lc *LineController) handlePostbackEvent(ctx context.Context, eventMap map[string]interface{}, config *LineConfig) {
	source, sourceOk := eventMap["source"].(map[string]interface{})
	if !sourceOk {
		return
	}

	// 支援群組和個人對話
	var sourceID string
	if groupID, ok := source["groupId"].(string); ok && groupID != "" {
		sourceID = groupID
	} else if userID, ok := source["userId"].(string); ok && userID != "" {
		sourceID = userID
	} else {
		return
	}

	postback, postbackOk := eventMap["postback"].(map[string]interface{})
	if !postbackOk {
		return
	}

	data, _ := postback["data"].(string)
	replyToken, _ := eventMap["replyToken"].(string)

	lc.logger.Info().
		Str("config_id", config.ID).
		Str("source_id", sourceID).
		Str("postback_data", data).
		Msg("收到 LINE postback 事件")

	// 處理 postback 資料
	lc.handlePostbackData(ctx, data, config.ID, sourceID, replyToken)
}

// handlePostbackData 處理 postback 資料
func (lc *LineController) handlePostbackData(ctx context.Context, data, configID, sourceID, replyToken string) {
	// 解析 postback 資料
	lc.logger.Info().
		Str("config_id", configID).
		Str("source_id", sourceID).
		Str("postback_data", data).
		Msg("處理 LINE postback 資料")

	// 處理複製地址功能
	if strings.Contains(data, "action=copy") {
		// 解析地址資訊
		parts := strings.Split(data, "&")
		address := ""
		for _, part := range parts {
			if strings.HasPrefix(part, "address=") {
				address = strings.TrimPrefix(part, "address=")
				break
			}
		}

		if address != "" {
			replyMsg := fmt.Sprintf("📋 已為您複製地址：\n%s", address)
			lc.lineService.ReplyMessage(configID, replyToken, replyMsg)
		} else {
			replyMsg := "❌ 無法取得地址資訊"
			lc.lineService.ReplyMessage(configID, replyToken, replyMsg)
		}
		return
	}

	// 處理重新派單
	if strings.Contains(data, "action=redispatch") {
		replyMsg := "收到重新派單請求，正在處理中..."
		lc.lineService.ReplyMessage(configID, replyToken, replyMsg)
		return
	}

	// 未知的 postback 資料
	lc.logger.Warn().
		Str("config_id", configID).
		Str("source_id", sourceID).
		Str("postback_data", data).
		Msg("收到未知的 postback 資料")
}

// prettyPrintJSON 是一個輔助函式，用來將資料以美化過的 JSON 格式打印出來
func prettyPrintJSON(data interface{}) {
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		log.Printf("[錯誤] 無法將資料格式化為 JSON: %v", err)
		return
	}
	log.Printf("收到的原始資料：\n%s", string(b))
}

// isOrderRedispatchable 檢查訂單是否可以被重派
func (lc *LineController) isOrderRedispatchable(status model.OrderStatus) bool {
	return status == model.OrderStatusFailed
}
