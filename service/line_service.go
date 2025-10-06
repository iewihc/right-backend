package service

import (
	"context"
	"fmt"
	"right-backend/model"
	"strings"
	"time"

	"github.com/line/line-bot-sdk-go/v8/linebot/messaging_api"
	"github.com/rs/zerolog"
)

// LINE 訊息模式常數
const (
	// UseFlexMessage 使用 Flex Message（預設）
	UseFlexMessage = false
	// UseTextMessage 強制使用文字訊息（當 LINE API 額度不足時可設為 false）
	UseTextMessage = true
)

// 目前使用的訊息模式 - 可以修改這個值來切換模式
const CurrentMessageMode = UseTextMessage

// LineConfig 代表單一 LINE 配置
type LineConfig struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	ChannelSecret string   `json:"channel_secret"`
	ChannelToken  string   `json:"channel_token"`
	Enabled       bool     `json:"enabled"`
	PushTriggers  []string `json:"push_triggers,omitempty"`
}

// LineService handles interactions with LINE Messaging API.
type LineService struct {
	logger             zerolog.Logger
	configs            map[string]*LineConfig
	clients            map[string]*messaging_api.MessagingApiAPI
	orderService       *OrderService
	flexMessageService *FlexMessageService
}

// NewLineService creates and initializes a new LineService.
func NewLineService(logger zerolog.Logger, configs []*LineConfig, orderService *OrderService) (*LineService, error) {
	service := &LineService{
		logger:             logger.With().Str("service", "line").Logger(),
		configs:            make(map[string]*LineConfig),
		clients:            make(map[string]*messaging_api.MessagingApiAPI),
		orderService:       orderService,
		flexMessageService: NewFlexMessageService(logger),
	}

	// 初始化每個配置的 LINE 客戶端
	for _, config := range configs {
		if config.Enabled {
			client, err := messaging_api.NewMessagingApiAPI(config.ChannelToken)
			if err != nil {
				logger.Error().
					Err(err).
					Str("config_id", config.ID).
					Msg("Failed to create LINE client")
				continue
			}

			service.configs[config.ID] = config
			service.clients[config.ID] = client

			logger.Info().
				Str("config_id", config.ID).
				Str("config_name", config.Name).
				Msg("LINE client initialized")
		}
	}

	return service, nil
}

// SetOrderService allows for delayed injection of the OrderService to break circular dependencies.
func (s *LineService) SetOrderService(orderService *OrderService) {
	s.orderService = orderService
}

// PushFlexMessage sends a Flex Message to a LINE user with fallback to text message.
func (s *LineService) PushFlexMessage(configID, userID string, flexMessage messaging_api.MessageInterface) error {
	return s.PushFlexMessageWithOrder(configID, userID, flexMessage, nil)
}

// PushFlexMessageWithOrder sends a Flex Message with order context for better fallback.
func (s *LineService) PushFlexMessageWithOrder(configID, userID string, flexMessage messaging_api.MessageInterface, order *model.Order) error {
	// 檢查全域設定，決定是否直接使用文字訊息
	if CurrentMessageMode == UseTextMessage {
		s.logger.Info().
			Str("config_id", configID).
			Str("user_id", userID).
			Msg("強制使用文字訊息模式，跳過 Flex Message")

		// 直接發送文字版本
		return s.sendTextFallbackWithOrder(configID, userID, flexMessage, order)
	}

	client, exists := s.clients[configID]
	if !exists {
		return fmt.Errorf("LINE client not found for config: %s", configID)
	}

	request := &messaging_api.PushMessageRequest{
		To:       userID,
		Messages: []messaging_api.MessageInterface{flexMessage},
	}

	_, err := client.PushMessage(request, "")
	if err != nil {
		// 檢查是否為額度限制錯誤 (HTTP 429)
		if s.isRateLimitError(err) {
			s.logger.Warn().
				Err(err).
				Str("config_id", configID).
				Str("user_id", userID).
				Msg("LINE API rate limit reached, falling back to text message")

			// 使用文字版 fallback
			return s.sendTextFallbackWithOrder(configID, userID, flexMessage, order)
		}

		s.logger.Error().
			Err(err).
			Str("config_id", configID).
			Str("user_id", userID).
			Msg("Failed to push LINE flex message")
		return err
	}

	s.logger.Info().
		Str("config_id", configID).
		Str("user_id", userID).
		Msg("LINE flex message sent successfully")

	return nil
}

// PushMessage sends a text message to a LINE user.
func (s *LineService) PushMessage(configID, userID, message string) error {
	client, exists := s.clients[configID]
	if !exists {
		return fmt.Errorf("LINE client not found for config: %s", configID)
	}

	request := &messaging_api.PushMessageRequest{
		To: userID,
		Messages: []messaging_api.MessageInterface{
			&messaging_api.TextMessage{
				Text: message,
			},
		},
	}

	_, err := client.PushMessage(request, "")
	if err != nil {
		s.logger.Error().
			Err(err).
			Str("config_id", configID).
			Str("user_id", userID).
			Msg("Failed to push LINE message")
		return err
	}

	s.logger.Info().
		Str("config_id", configID).
		Str("user_id", userID).
		Msg("LINE message sent successfully")

	return nil
}

// PushImageMessage sends an image message to a LINE user.
func (s *LineService) PushImageMessage(configID, userID, imageURL string) error {
	client, exists := s.clients[configID]
	if !exists {
		return fmt.Errorf("LINE client not found for config: %s", configID)
	}

	request := &messaging_api.PushMessageRequest{
		To: userID,
		Messages: []messaging_api.MessageInterface{
			&messaging_api.ImageMessage{
				OriginalContentUrl: imageURL,
				PreviewImageUrl:    imageURL,
			},
		},
	}

	_, err := client.PushMessage(request, "")
	if err != nil {
		s.logger.Error().
			Err(err).
			Str("config_id", configID).
			Str("user_id", userID).
			Str("image_url", imageURL).
			Msg("Failed to push LINE image message")
		return err
	}

	s.logger.Info().
		Str("config_id", configID).
		Str("user_id", userID).
		Str("image_url", imageURL).
		Msg("LINE image message sent successfully")

	return nil
}

// ReplyFlexMessage sends a reply flex message to a LINE user.
func (s *LineService) ReplyFlexMessage(configID, replyToken string, flexMessage messaging_api.MessageInterface) error {
	return s.ReplyFlexMessageWithOrder(configID, replyToken, flexMessage, nil)
}

// ReplyFlexMessageWithOrder sends a reply flex message with order context for better fallback.
func (s *LineService) ReplyFlexMessageWithOrder(configID, replyToken string, flexMessage messaging_api.MessageInterface, order *model.Order) error {
	// 檢查全域設定，決定是否直接使用文字訊息
	if CurrentMessageMode == UseTextMessage {
		s.logger.Info().
			Str("config_id", configID).
			Str("reply_token", replyToken).
			Msg("強制使用文字訊息模式，回覆文字訊息代替 Flex Message")

		// 直接回覆文字版本
		return s.sendTextReplyWithOrder(configID, replyToken, flexMessage, order)
	}

	client, exists := s.clients[configID]
	if !exists {
		return fmt.Errorf("LINE client not found for config: %s", configID)
	}

	request := &messaging_api.ReplyMessageRequest{
		ReplyToken: replyToken,
		Messages:   []messaging_api.MessageInterface{flexMessage},
	}

	_, err := client.ReplyMessage(request)
	if err != nil {
		// 檢查是否為額度限制錯誤 (HTTP 429)
		if s.isRateLimitError(err) {
			s.logger.Warn().
				Err(err).
				Str("config_id", configID).
				Str("reply_token", replyToken).
				Msg("LINE API rate limit reached, falling back to text reply")

			// 使用文字版 fallback
			return s.sendTextReplyWithOrder(configID, replyToken, flexMessage, order)
		}

		s.logger.Error().
			Err(err).
			Str("config_id", configID).
			Str("reply_token", replyToken).
			Msg("Failed to reply LINE flex message")
		return err
	}

	s.logger.Info().
		Str("config_id", configID).
		Str("reply_token", replyToken).
		Msg("LINE flex reply message sent successfully")

	return nil
}

// ReplyMessage sends a reply message to a LINE user.
func (s *LineService) ReplyMessage(configID, replyToken, message string) error {
	client, exists := s.clients[configID]
	if !exists {
		return fmt.Errorf("LINE client not found for config: %s", configID)
	}

	request := &messaging_api.ReplyMessageRequest{
		ReplyToken: replyToken,
		Messages: []messaging_api.MessageInterface{
			&messaging_api.TextMessage{
				Text: message,
			},
		},
	}

	_, err := client.ReplyMessage(request)
	if err != nil {
		s.logger.Error().
			Err(err).
			Str("config_id", configID).
			Str("reply_token", replyToken).
			Msg("Failed to reply LINE message")
		return err
	}

	s.logger.Info().
		Str("config_id", configID).
		Str("reply_token", replyToken).
		Msg("LINE reply message sent successfully")

	return nil
}

// FormatOrderMessage formats an order into a Flex Message based on its status.
func (s *LineService) FormatOrderMessage(order *model.Order) messaging_api.MessageInterface {
	return s.flexMessageService.GetFlexMessage(order)
}

// GetCreatingFlexMessage returns a Flex Message for order creation in progress.
func (s *LineService) GetCreatingFlexMessage() messaging_api.MessageInterface {
	return s.flexMessageService.GetCreatingFlexMessage()
}

// FormatOrderMessageAsText formats an order into a text message based on its status (legacy method).
func (s *LineService) FormatOrderMessageAsText(order *model.Order) string {
	switch order.Status {
	case model.OrderStatusEnroute:
		return s.formatEnrouteMessage(order)
	case model.OrderStatusExecuting:
		return s.formatExecutingMessage(order)
	case model.OrderStatusFailed:
		return s.formatFailedMessage(order)
	case model.OrderStatusDriverArrived:
		return s.formatDriverArrivedMessage(order)
	case model.OrderStatusCompleted:
		return s.formatCompletedMessage(order)
	default:
		return s.formatDefaultMessage(order)
	}
}

// formatEnrouteMessage formats a message for driver en route status.
func (s *LineService) formatEnrouteMessage(order *model.Order) string {
	shortID := order.ShortID
	displayMins := order.Driver.EstPickupMins
	if order.Driver.AdjustMins != nil {
		displayMins += *order.Driver.AdjustMins
	}

	// 計算預計到達的具體時間
	arrivalTime := time.Now().Add(time.Minute * time.Duration(displayMins))
	arrivalTimeFormatted := arrivalTime.Format("15:04")

	var msg strings.Builder
	msg.WriteString(fmt.Sprintf("🚗 司機前往上車點 (#%s)\n\n", shortID))
	msg.WriteString(fmt.Sprintf("客戶群組: %s\n", order.CustomerGroup))
	msg.WriteString(fmt.Sprintf("狀態: %s\n", string(order.Status)))
	msg.WriteString(fmt.Sprintf("上車地點: %s\n", order.OriText))
	msg.WriteString(fmt.Sprintf("備註: %s\n", order.Customer.Remarks))
	msg.WriteString(fmt.Sprintf("駕駛: %s (%s)\n", order.Driver.Name, order.Driver.CarNo))
	msg.WriteString(fmt.Sprintf("預計到達: %d 分鐘 (%s)", displayMins, arrivalTimeFormatted))

	return msg.String()
}

// formatExecutingMessage formats a message for order executing status.
func (s *LineService) formatExecutingMessage(order *model.Order) string {
	shortID := order.ShortID

	var msg strings.Builder
	msg.WriteString(fmt.Sprintf("🚗 乘客已上車 (#%s)\n\n", shortID))
	msg.WriteString(fmt.Sprintf("客戶群組: %s\n", order.CustomerGroup))
	msg.WriteString(fmt.Sprintf("狀態: %s\n", string(order.Status)))
	msg.WriteString(fmt.Sprintf("上車地點: %s\n", order.OriText))
	msg.WriteString(fmt.Sprintf("備註: %s\n", order.Customer.Remarks))
	msg.WriteString(fmt.Sprintf("駕駛: %s (%s)", order.Driver.Name, order.Driver.CarNo))

	return msg.String()
}

// formatFailedMessage formats a message for failed order status.
func (s *LineService) formatFailedMessage(order *model.Order) string {
	shortID := order.ShortID

	var msg strings.Builder
	msg.WriteString(fmt.Sprintf("❌ 派單失敗 (#%s)\n\n", shortID))
	msg.WriteString(fmt.Sprintf("客戶群組: %s\n", order.CustomerGroup))
	msg.WriteString(fmt.Sprintf("狀態: %s\n", string(order.Status)))
	msg.WriteString(fmt.Sprintf("上車地點: %s\n", order.OriText))
	msg.WriteString(fmt.Sprintf("備註: %s\n", order.Customer.Remarks))
	msg.WriteString("原因: 很抱歉，目前沒有可用的司機。\n\n")
	msg.WriteString(fmt.Sprintf("💡 需要重新派單嗎？請回覆「重新派單 %s」", shortID))

	return msg.String()
}

// formatDriverArrivedMessage formats a message for driver arrived status.
func (s *LineService) formatDriverArrivedMessage(order *model.Order) string {
	shortID := order.ShortID

	var msg strings.Builder
	msg.WriteString(fmt.Sprintf("📍 司機到達客上位置 (#%s)\n\n", shortID))
	msg.WriteString(fmt.Sprintf("客戶群組: %s\n", order.CustomerGroup))
	msg.WriteString("狀態: 調度請通知乘客\n")
	msg.WriteString(fmt.Sprintf("上車地點: %s\n", order.OriText))
	msg.WriteString(fmt.Sprintf("備註: %s\n", order.Customer.Remarks))
	msg.WriteString(fmt.Sprintf("駕駛: %s (%s)", order.Driver.Name, order.Driver.CarNo))

	if order.PickupCertificateURL != "" {
		msg.WriteString("\n\n📸 司機已上傳到達證明照片")
	}

	return msg.String()
}

// formatCompletedMessage formats a message for completed order status.
func (s *LineService) formatCompletedMessage(order *model.Order) string {
	shortID := order.ShortID

	var msg strings.Builder
	msg.WriteString(fmt.Sprintf("🏁 訂單已完成 (#%s)\n\n", shortID))
	msg.WriteString(fmt.Sprintf("客戶群組: %s\n", order.CustomerGroup))
	msg.WriteString("狀態: 已完成\n")
	msg.WriteString(fmt.Sprintf("上車地點: %s\n", order.OriText))
	msg.WriteString(fmt.Sprintf("備註: %s\n", order.Customer.Remarks))
	msg.WriteString(fmt.Sprintf("駕駛: %s (%s)", order.Driver.Name, order.Driver.CarNo))
	if order.Amount != nil {
		msg.WriteString(fmt.Sprintf("\n車資: $%d", *order.Amount))
	}

	return msg.String()
}

// formatDefaultMessage formats a default message for other order statuses.
func (s *LineService) formatDefaultMessage(order *model.Order) string {
	shortID := order.ShortID
	statusText := string(order.Status)

	var msg strings.Builder
	msg.WriteString(fmt.Sprintf("⏳ %s (#%s)\n\n", statusText, shortID))
	msg.WriteString(fmt.Sprintf("客戶群組: %s\n", order.CustomerGroup))
	msg.WriteString(fmt.Sprintf("狀態: %s\n", statusText))
	msg.WriteString(fmt.Sprintf("上車地點: %s\n", order.OriText))
	msg.WriteString(fmt.Sprintf("備註: %s", order.Customer.Remarks))

	return msg.String()
}

// CreateOrderFromMessage parses user input and creates an order.
func (s *LineService) CreateOrderFromMessage(ctx context.Context, message, configID, sourceID string) (*model.Order, error) {
	if s.orderService == nil {
		return nil, fmt.Errorf("orderService not initialized")
	}

	// 使用 SimpleCreateOrder 處理用戶輸入（使用 sourceID 作為建立者名稱）
	result, err := s.orderService.SimpleCreateOrder(ctx, message, "", model.CreatedByLine, sourceID)
	if err != nil {
		s.logger.Error().
			Err(err).
			Str("config_id", configID).
			Str("source_id", sourceID).
			Str("message", message).
			Msg("Failed to create order from LINE message")
		return nil, err
	}

	createdOrder := result.Order

	// 設置 LINE 相關資訊
	lineMessage := model.LineMessageInfo{
		ConfigID:    configID,
		UserID:      sourceID, // 可能是 groupID 或 userID
		MessageType: "order_created",
		Timestamp:   time.Now(),
	}
	createdOrder.LineMessages = append(createdOrder.LineMessages, lineMessage)

	// 更新訂單以保存 LINE 資訊
	updatedOrder, err := s.orderService.UpdateOrder(ctx, createdOrder)
	if err != nil {
		s.logger.Error().
			Err(err).
			Str("order_id", createdOrder.ID.Hex()).
			Msg("Failed to update order with LINE info")
		return createdOrder, nil // 繼續執行，不阻止訂單創建
	}

	return updatedOrder, nil
}

// ShouldPushStatusUpdate determines if a status change should trigger a push notification.
func (s *LineService) ShouldPushStatusUpdate(configID string, newStatus model.OrderStatus) bool {
	config, exists := s.configs[configID]
	if !exists || !config.Enabled {
		return false
	}

	// 跳過取消狀態的自動推送，因為取消操作會直接回覆確認訊息
	// 避免重複發送訊息
	if newStatus == model.OrderStatusCancelled {
		return false
	}

	// 支援其他所有狀態更新
	return true
}

// GetConfig returns the LINE config for the given configID.
func (s *LineService) GetConfig(configID string) (*LineConfig, bool) {
	config, exists := s.configs[configID]
	return config, exists
}

// isRateLimitError 檢查錯誤是否為 LINE API 額度限制
func (s *LineService) isRateLimitError(err error) bool {
	errStr := err.Error()

	// 記錄詳細的錯誤類型以便調試
	if strings.Contains(errStr, "429") {
		if strings.Contains(errStr, "monthly limit") {
			s.logger.Debug().Msg("檢測到月度額度限制錯誤")
		} else if strings.Contains(errStr, "rate limit") {
			s.logger.Debug().Msg("檢測到頻率限制錯誤")
		} else {
			s.logger.Debug().Str("error", errStr).Msg("檢測到 429 錯誤，但原因不明")
		}
		return true
	}

	return strings.Contains(errStr, "monthly limit") ||
		strings.Contains(errStr, "rate limit")
}

// sendTextFallback 發送文字版 fallback 訊息
func (s *LineService) sendTextFallback(configID, userID string, flexMessage messaging_api.MessageInterface) error {
	return s.sendTextFallbackWithOrder(configID, userID, flexMessage, nil)
}

// sendTextFallbackWithOrder 發送帶有訂單資訊的文字版 fallback 訊息
func (s *LineService) sendTextFallbackWithOrder(configID, userID string, flexMessage messaging_api.MessageInterface, order *model.Order) error {
	var textMessage string

	if order != nil {
		// 如果有訂單資訊，生成詳細的文字訊息
		textMessage = s.createOrderTextMessage(order)
	} else {
		// 從 Flex Message 提取基本資訊並轉換為文字
		textMessage = s.convertFlexToText(flexMessage)
	}

	err := s.PushMessage(configID, userID, textMessage)
	if err != nil {
		// 檢查文字訊息是否也遇到額度限制
		if s.isRateLimitError(err) {
			s.logger.Warn().
				Err(err).
				Str("config_id", configID).
				Str("user_id", userID).
				Msg("LINE API 月度額度已完全用盡，無法發送任何訊息（包含文字訊息）")
			// 返回 nil 避免上層繼續報錯，因為這是預期的額度限制情況
			return nil
		}
		// 其他錯誤繼續回傳
		return err
	}

	// 如果有證明照片URL，也發送照片
	if order != nil && order.PickupCertificateURL != "" {
		photoErr := s.PushImageMessage(configID, userID, order.PickupCertificateURL)
		if photoErr != nil {
			s.logger.Warn().
				Err(photoErr).
				Str("config_id", configID).
				Str("user_id", userID).
				Str("photo_url", order.PickupCertificateURL).
				Msg("發送證明照片失敗，但文字訊息已成功發送")
			// 不返回錯誤，因為文字訊息已經成功發送
		} else {
			s.logger.Info().
				Str("config_id", configID).
				Str("user_id", userID).
				Str("photo_url", order.PickupCertificateURL).
				Msg("成功發送證明照片")
		}
	}

	s.logger.Info().
		Str("config_id", configID).
		Str("user_id", userID).
		Msg("成功發送文字版 fallback 訊息")

	return nil
}

// sendTextReplyWithOrder 回覆帶有訂單資訊的文字版 fallback 訊息
func (s *LineService) sendTextReplyWithOrder(configID, replyToken string, flexMessage messaging_api.MessageInterface, order *model.Order) error {
	var textMessage string

	if order != nil {
		// 如果有訂單資訊，生成詳細的文字訊息
		textMessage = s.createOrderTextMessage(order)
	} else {
		// 從 Flex Message 提取基本資訊並轉換為文字
		textMessage = s.convertFlexToText(flexMessage)
	}

	err := s.ReplyMessage(configID, replyToken, textMessage)
	if err != nil {
		// 檢查文字訊息是否也遇到額度限制
		if s.isRateLimitError(err) {
			s.logger.Warn().
				Err(err).
				Str("config_id", configID).
				Str("reply_token", replyToken).
				Msg("LINE API 月度額度已完全用盡，無法回覆任何訊息（包含文字訊息）")
			// 返回 nil 避免上層繼續報錯，因為這是預期的額度限制情況
			return nil
		}
		// 其他錯誤繼續回傳
		return err
	}

	s.logger.Info().
		Str("config_id", configID).
		Str("reply_token", replyToken).
		Msg("成功回覆文字版 fallback 訊息")

	return nil
}

// convertFlexToText 將 Flex Message 轉換為文字訊息
func (s *LineService) convertFlexToText(flexMessage messaging_api.MessageInterface) string {
	// 嘗試從 AltText 提取狀態資訊
	altText := s.extractAltText(flexMessage)

	// 根據 AltText 判斷訂單狀態並生成對應的 emoji 文字訊息
	switch {
	case strings.Contains(altText, "等待司機接單"):
		return s.createWaitingTextMessage(altText)
	case strings.Contains(altText, "司機前往上車點"):
		return s.createEnrouteTextMessage(altText)
	case strings.Contains(altText, "司機已到達"):
		return s.createArrivedTextMessage(altText)
	case strings.Contains(altText, "乘客已上車"):
		return s.createExecutingTextMessage(altText)
	case strings.Contains(altText, "訂單已完成"):
		return s.createCompletedTextMessage(altText)
	case strings.Contains(altText, "派單失敗"):
		return s.createFailedTextMessage(altText)
	case strings.Contains(altText, "訂單已取消"):
		return s.createCancelledTextMessage(altText)
	case strings.Contains(altText, "正在建立訂單"):
		return s.createCreatingTextMessage(altText)
	default:
		return "📋 訂單狀態更新\n\n" + altText
	}
}

// extractAltText 從 Flex Message 提取 AltText
func (s *LineService) extractAltText(flexMessage messaging_api.MessageInterface) string {
	if flexMsg, ok := flexMessage.(*messaging_api.FlexMessage); ok {
		return flexMsg.AltText
	}
	return "訂單狀態更新"
}

// GetAllConfigs returns all LINE configs.
func (s *LineService) GetAllConfigs() map[string]*LineConfig {
	return s.configs
}

// createOrderTextMessage 根據訂單狀態創建完整的文字訊息
func (s *LineService) createOrderTextMessage(order *model.Order) string {
	switch order.Status {
	case model.OrderStatusWaiting:
		return s.createWaitingText(order)
	case model.OrderStatusEnroute:
		return s.createEnrouteText(order)
	case model.OrderStatusDriverArrived:
		return s.createArrivedText(order)
	case model.OrderStatusExecuting:
		return s.createExecutingText(order)
	case model.OrderStatusCompleted:
		return s.createCompletedText(order)
	case model.OrderStatusFailed:
		return s.createFailedText(order)
	case model.OrderStatusCancelled:
		return s.createCancelledText(order)
	default:
		return s.createWaitingText(order)
	}
}

// createWaitingText 等待司機接單
func (s *LineService) createWaitingText(order *model.Order) string {
	return fmt.Sprintf(`🟡 等待司機接單 %s

📍 單號: %s
📞 如需取消請輸入：取消 %s`,
		order.ShortID,
		order.OriText,
		order.ID.Hex())
}

// createEnrouteText 司機前往上車點
func (s *LineService) createEnrouteText(order *model.Order) string {
	msg := fmt.Sprintf(`🔵 司機前往上車點 %s

📍 單號: %s`,
		order.ShortID,
		order.OriText)

	if order.Driver.Name != "" {
		msg += fmt.Sprintf(`
🚗 司機: %s (%s)`, order.Driver.Name, order.Driver.CarNo)

		if order.Driver.EstPickupMins > 0 {
			displayMins := order.Driver.EstPickupMins
			if order.Driver.AdjustMins != nil {
				displayMins += *order.Driver.AdjustMins
			}
			arrivalTime := time.Now().Add(time.Minute * time.Duration(displayMins))
			msg += fmt.Sprintf(`
⏰ 預計到達: %d 分鐘 (%s)`, displayMins, arrivalTime.Format("15:04"))
		}
	}

	msg += fmt.Sprintf(`

📞 如需取消請輸入：取消 %s`, order.ID.Hex())
	return msg
}

// createArrivedText 司機已到達
func (s *LineService) createArrivedText(order *model.Order) string {
	msg := fmt.Sprintf(`🟠 司機已到達 %s

📍 單號: %s
💬 調度請通知乘客`,
		order.ShortID,
		order.OriText)

	if order.Driver.Name != "" {
		msg += fmt.Sprintf(`
🚗 司機: %s (%s)`, order.Driver.Name, order.Driver.CarNo)
	}

	// 證明照片會另外發送圖片訊息，此處不需要文字說明

	msg += fmt.Sprintf(`

📞 如需取消請輸入：取消 %s`, order.ID.Hex())
	return msg
}

// createExecutingText 乘客已上車
func (s *LineService) createExecutingText(order *model.Order) string {
	msg := fmt.Sprintf(`🟢 乘客已上車 %s

📍 單號: %s`,
		order.ShortID,
		order.OriText)

	if order.Driver.Name != "" {
		msg += fmt.Sprintf(`
🚗 司機: %s (%s)`, order.Driver.Name, order.Driver.CarNo)
	}

	return msg
}

// createCompletedText 訂單已完成
func (s *LineService) createCompletedText(order *model.Order) string {
	msg := fmt.Sprintf(`🟢 訂單已完成 %s

📍 單號: %s`,
		order.ShortID,
		order.OriText)

	if order.Driver.Name != "" {
		msg += fmt.Sprintf(`
🚗 司機: %s (%s)`, order.Driver.Name, order.Driver.CarNo)
	}

	return msg
}

// createFailedText 派單失敗
func (s *LineService) createFailedText(order *model.Order) string {
	return fmt.Sprintf(`🔴 派單失敗 %s

📍 單號: %s
😔 很抱歉，目前沒有可用的司機

💡 可以嘗試重新派單或聯繫客服
📞 重派請輸入：重派 %s`,
		order.ShortID,
		order.OriText,
		order.ID.Hex())
}

// createCancelledText 訂單已取消
func (s *LineService) createCancelledText(order *model.Order) string {
	return fmt.Sprintf(`🟤 訂單已取消 %s

📍 單號: %s
✅ 訂單已被成功取消

💡 如需重新預約，請重新輸入行程`,
		order.ShortID,
		order.OriText)
}

// createWaitingTextMessage 根據 AltText 創建等待文字訊息
func (s *LineService) createWaitingTextMessage(altText string) string {
	return "🟡 " + altText + "\n\n📞 如需取消，請輸入：取消 [訂單ID]"
}

// createEnrouteTextMessage 根據 AltText 創建前往文字訊息
func (s *LineService) createEnrouteTextMessage(altText string) string {
	return "🔵 " + altText + "\n\n📞 如需取消，請輸入：取消 [訂單ID]"
}

// createArrivedTextMessage 根據 AltText 創建到達文字訊息
func (s *LineService) createArrivedTextMessage(altText string) string {
	return "🟠 " + altText + "\n💬 調度請通知乘客\n\n📞 如需取消，請輸入：取消 [訂單ID]"
}

// createExecutingTextMessage 根據 AltText 創建執行中文字訊息
func (s *LineService) createExecutingTextMessage(altText string) string {
	return "🟢 " + altText
}

// createCompletedTextMessage 根據 AltText 創建完成文字訊息
func (s *LineService) createCompletedTextMessage(altText string) string {
	return "🟢 " + altText
}

// createFailedTextMessage 根據 AltText 創建失敗文字訊息
func (s *LineService) createFailedTextMessage(altText string) string {
	return "🔴 " + altText + "\n\n💡 可以嘗試重新派單或聯繫客服\n📞 重派請輸入：重派 [訂單ID]"
}

// createCancelledTextMessage 根據 AltText 創建取消文字訊息
func (s *LineService) createCancelledTextMessage(altText string) string {
	return "🟤 " + altText + "\n\n💡 如需重新預約，請重新輸入行程"
}

// createCreatingTextMessage 根據 AltText 創建建立中文字訊息
func (s *LineService) createCreatingTextMessage(altText string) string {
	return "🟣 " + altText + "\n\n請稍候，系統正在處理您的需求..."
}

// Close closes all LINE clients.
func (s *LineService) Close() {
	s.logger.Info().Msg("Closing LINE service")
	// LINE SDK 不需要特別的清理操作
}
