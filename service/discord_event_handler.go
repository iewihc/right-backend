package service

import (
	"context"
	"right-backend/infra"
	"right-backend/model"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

// DiscordEventHandler Discord 事件處理器
type DiscordEventHandler struct {
	logger         zerolog.Logger
	eventManager   *infra.RedisEventManager
	discordService *DiscordService
	orderService   *OrderService
	ctx            context.Context
	cancel         context.CancelFunc
	pubsub         *redis.PubSub
}

// NewDiscordEventHandler 建立 Discord 事件處理器
func NewDiscordEventHandler(logger zerolog.Logger, eventManager *infra.RedisEventManager, discordService *DiscordService, orderService *OrderService) *DiscordEventHandler {
	ctx, cancel := context.WithCancel(context.Background())

	return &DiscordEventHandler{
		logger:         logger.With().Str("service", "discord_event_handler").Logger(),
		eventManager:   eventManager,
		discordService: discordService,
		orderService:   orderService,
		ctx:            ctx,
		cancel:         cancel,
	}
}

// Start 啟動 Discord 事件處理器
func (h *DiscordEventHandler) Start() {
	h.logger.Info().Msg("啟動 Discord 事件處理器")

	// 訂閱 Discord 消息更新事件
	h.pubsub = h.eventManager.SubscribeDiscordUpdateEvents(h.ctx)

	// 啟動事件處理 goroutine
	go h.processDiscordEvents()
}

// Stop 停止 Discord 事件處理器
func (h *DiscordEventHandler) Stop() {
	h.logger.Info().Msg("停止 Discord 事件處理器")

	if h.pubsub != nil {
		h.pubsub.Close()
	}

	h.cancel()
}

// processDiscordEvents 處理 Discord 事件
func (h *DiscordEventHandler) processDiscordEvents() {
	defer func() {
		if r := recover(); r != nil {
			h.logger.Error().Interface("recover", r).Msg("Discord 事件處理器發生 panic，正在重啟")
			// 重啟處理器
			go func() {
				time.Sleep(5 * time.Second)
				h.processDiscordEvents()
			}()
		}
	}()

	channel := h.pubsub.Channel()

	for {
		select {
		case <-h.ctx.Done():
			h.logger.Info().Msg("Discord 事件處理器已停止")
			return

		case msg, ok := <-channel:
			if !ok {
				h.logger.Warn().Msg("Discord 事件頻道已關閉，正在重啟")
				// 重新建立連接
				time.Sleep(1 * time.Second)
				h.pubsub = h.eventManager.SubscribeDiscordUpdateEvents(h.ctx)
				channel = h.pubsub.Channel()
				continue
			}

			h.handleDiscordUpdateEvent(msg.Payload)
		}
	}
}

// handleDiscordUpdateEvent 處理 Discord 消息更新事件
func (h *DiscordEventHandler) handleDiscordUpdateEvent(payload string) {
	event, err := infra.ParseDiscordUpdateEvent(payload)
	if err != nil {
		h.logger.Error().Err(err).Str("payload", payload).Msg("解析 Discord 更新事件失敗")
		return
	}

	//h.logger.Debug().
	//	Str("order_id", event.OrderID).
	//	Str("channel_id", event.ChannelID).
	//	Str("message_id", event.MessageID).
	//	Int("retry_count", event.RetryCount).
	//	Msg("收到 Discord 消息更新事件")

	// 獲取訂單資料
	order, err := h.orderService.GetOrderByID(h.ctx, event.OrderID)
	if err != nil {
		h.logger.Error().Err(err).
			Str("order_id", event.OrderID).
			Msg("獲取訂單資料失敗，無法更新 Discord 消息")
		return
	}

	// 驗證 Discord 消息資訊是否一致
	if order.DiscordChannelID != event.ChannelID || order.DiscordMessageID != event.MessageID {
		h.logger.Warn().
			Str("order_id", event.OrderID).
			Str("order_channel_id", order.DiscordChannelID).
			Str("event_channel_id", event.ChannelID).
			Str("order_message_id", order.DiscordMessageID).
			Str("event_message_id", event.MessageID).
			Msg("Discord 消息資訊不一致，跳過更新")
		return
	}

	// 更新 Discord 消息
	h.updateDiscordMessage(order, event)
}

// updateDiscordMessage 更新 Discord 消息，包含重試機制
func (h *DiscordEventHandler) updateDiscordMessage(order *model.Order, event *infra.DiscordUpdateEvent) {
	// 執行 Discord 消息更新
	h.discordService.UpdateOrderCard(order)

	h.logger.Info().
		Str("order_id", order.ID.Hex()).
		Str("channel_id", order.DiscordChannelID).
		Str("message_id", order.DiscordMessageID).
		Str("status", string(order.Status)).
		Int("retry_count", event.RetryCount).
		Msg("Discord 消息更新完成")

	// Discord 回覆消息現在統一由 NotificationService 處理，避免重複發送
}

// PublishDiscordUpdateEventForOrder 為訂單發布 Discord 更新事件
func (h *DiscordEventHandler) PublishDiscordUpdateEventForOrder(ctx context.Context, order *model.Order) {
	// 檢查訂單是否有 Discord 消息資訊
	if order.DiscordChannelID == "" || order.DiscordMessageID == "" {
		return
	}

	event := &infra.DiscordUpdateEvent{
		OrderID:   order.ID.Hex(),
		ChannelID: order.DiscordChannelID,
		MessageID: order.DiscordMessageID,
		EventType: infra.DiscordEventUpdateMessage,
		Timestamp: time.Now(),
	}

	if err := h.eventManager.PublishDiscordUpdateEvent(ctx, event); err != nil {
		h.logger.Error().Err(err).
			Str("order_id", order.ID.Hex()).
			Str("channel_id", order.DiscordChannelID).
			Str("message_id", order.DiscordMessageID).
			Msg("發布 Discord 更新事件失敗")
	}
}

// getEventTypeByOrderStatus 根據訂單狀態決定對應的事件類型
func (h *DiscordEventHandler) getEventTypeByOrderStatus(status model.OrderStatus) string {
	switch status {
	case model.OrderStatusEnroute:
		return "driver_accepted_order"
	case model.OrderStatusDriverArrived:
		return "driver_arrived"
	case model.OrderStatusExecuting:
		return "customer_on_board"
	case model.OrderStatusCompleted:
		return "" // 不發送Discord回覆提示
	case model.OrderStatusFailed:
		return "order_failed"
	case model.OrderStatusCancelled:
		return "order_cancelled"
	default:
		return ""
	}
}

// ReplyToOrderBanner 為訂單回覆消息到第一張訂單卡片（獨立的通知模組）
func (h *DiscordEventHandler) ReplyToOrderBanner(ctx context.Context, order *model.Order, eventType, fleet, driverName, carPlate, carColor string, distanceKm float64, estimatedMins int) {
	// 追蹤重複調用問題
	h.logger.Info().
		Str("order_id", order.ID.Hex()).
		Str("event_type", eventType).
		Str("order_type", string(order.Type)).
		Str("driver_name", driverName).
		Float64("distance_km", distanceKm).
		Int("estimated_mins", estimatedMins).
		Msg("ReplyToOrderBanner 被調用")

	// 檢查訂單是否有 Discord 消息資訊
	if order.DiscordChannelID == "" || order.DiscordMessageID == "" {
		h.logger.Debug().
			Str("order_id", order.ID.Hex()).
			Msg("訂單沒有 Discord 消息資訊，跳過回覆")
		return
	}

	// 格式化回覆消息
	eventChineseName := h.discordService.GetEventChineseName(eventType)
	var replyText string

	// 檢查是否為預約單，所有預約單相關回覆都要加上「預約單|」前綴
	if order.Type == model.OrderTypeScheduled {
		// 預約單格式：根據事件類型決定是否包含距離時間
		if eventType == string(model.EventScheduledAccepted) || eventType == string(model.EventDriverAccepted) {
			// 預約單接受或預約單司機接單：不顯示距離時間
			replyText = h.discordService.FormatScheduledEventReply(fleet, order.ShortID, eventChineseName, order.OriText, carPlate, carColor, driverName)
		} else if eventType == string(model.EventScheduledActivated) {
			// 預約單激活：顯示距離時間
			replyText = h.discordService.FormatEventReply(fleet, order.ShortID, eventChineseName, order.OriText, carPlate, carColor, driverName, distanceKm, estimatedMins)
			// 加上 "預約單 |" 前綴
			replyText = strings.Replace(replyText, "】: ", "】: 預約單 | ", 1)
		} else {
			// 預約單其他事件（司機抵達、客人上車等）：不顯示距離時間
			replyText = h.discordService.FormatScheduledEventReply(fleet, order.ShortID, eventChineseName, order.OriText, carPlate, carColor, driverName)
		}
	} else {
		// 一般即時單格式：根據事件類型決定是否顯示距離時間
		if eventType == string(model.EventDriverAccepted) {
			// 即時單司機接單：顯示距離時間
			replyText = h.discordService.FormatEventReply(fleet, order.ShortID, eventChineseName, order.OriText, carPlate, carColor, driverName, distanceKm, estimatedMins)
		} else {
			// 即時單其他事件（司機抵達、客人上車等）：不顯示距離時間
			replyText = h.discordService.FormatEventReplyWithoutDistance(fleet, order.ShortID, eventChineseName, order.OriText, carPlate, carColor, driverName)
		}
	}

	// 根據事件類型獲取對應顏色
	eventColor := h.discordService.GetEventColor(eventType)

	// 發送回覆到第一張訂單卡片（使用footer嵌入orderID和事件顏色）
	_, err := h.discordService.ReplyToMessageWithOrderIDAndColor(order.DiscordChannelID, order.DiscordMessageID, replyText, order.ID.Hex(), eventColor)
	if err != nil {
		h.logger.Error().Err(err).
			Str("order_id", order.ID.Hex()).
			Str("channel_id", order.DiscordChannelID).
			Str("message_id", order.DiscordMessageID).
			Str("event_type", eventType).
			Msg("Discord 訂單卡片回覆失敗")
		return
	}

	h.logger.Info().
		Str("order_id", order.ID.Hex()).
		Str("channel_id", order.DiscordChannelID).
		Str("message_id", order.DiscordMessageID).
		Str("event_type", eventType).
		Str("reply_text", replyText).
		Msg("Discord 訂單卡片回覆成功")
}

// ReplyToOrderWithSSEEvent 為訂單回覆 SSE 事件消息（保留舊方法以維持兼容性）
func (h *DiscordEventHandler) ReplyToOrderWithSSEEvent(ctx context.Context, order *model.Order, eventType, fleet, driverName, carPlate, carColor string, distanceKm float64, estimatedMins int) {
	// 直接調用新的獨立方法
	h.ReplyToOrderBanner(ctx, order, eventType, fleet, driverName, carPlate, carColor, distanceKm, estimatedMins)
}
