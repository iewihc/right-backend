package service

import (
	"context"
	"right-backend/infra"
	"right-backend/model"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

// LineEventHandler LINE 事件處理器
type LineEventHandler struct {
	logger       zerolog.Logger
	eventManager *infra.RedisEventManager
	lineService  *LineService
	orderService *OrderService
	ctx          context.Context
	cancel       context.CancelFunc
	pubsub       *redis.PubSub
}

// NewLineEventHandler 建立 LINE 事件處理器
func NewLineEventHandler(logger zerolog.Logger, eventManager *infra.RedisEventManager, lineService *LineService, orderService *OrderService) *LineEventHandler {
	ctx, cancel := context.WithCancel(context.Background())

	return &LineEventHandler{
		logger:       logger.With().Str("service", "line_event_handler").Logger(),
		eventManager: eventManager,
		lineService:  lineService,
		orderService: orderService,
		ctx:          ctx,
		cancel:       cancel,
	}
}

// Start 啟動 LINE 事件處理器
func (h *LineEventHandler) Start() {
	h.logger.Info().Msg("啟動 LINE 事件處理器")

	// 訂閱 LINE 消息更新事件
	h.pubsub = h.eventManager.SubscribeLineUpdateEvents(h.ctx)

	// 啟動事件處理 goroutine
	go h.processLineEvents()
}

// Stop 停止 LINE 事件處理器
func (h *LineEventHandler) Stop() {
	h.logger.Info().Msg("停止 LINE 事件處理器")

	if h.pubsub != nil {
		h.pubsub.Close()
	}

	h.cancel()
}

// processLineEvents 處理 LINE 事件
func (h *LineEventHandler) processLineEvents() {
	defer func() {
		if r := recover(); r != nil {
			h.logger.Error().Interface("recover", r).Msg("LINE 事件處理器發生 panic，正在重啟")
			// 重啟處理器
			go func() {
				time.Sleep(5 * time.Second)
				h.processLineEvents()
			}()
		}
	}()

	channel := h.pubsub.Channel()

	for {
		select {
		case <-h.ctx.Done():
			h.logger.Info().Msg("LINE 事件處理器已停止")
			return

		case msg, ok := <-channel:
			if !ok {
				h.logger.Warn().Msg("LINE 事件頻道已關閉，正在重啟")
				// 重新建立連接
				time.Sleep(1 * time.Second)
				h.pubsub = h.eventManager.SubscribeLineUpdateEvents(h.ctx)
				channel = h.pubsub.Channel()
				continue
			}

			h.handleLineUpdateEvent(msg.Payload)
		}
	}
}

// handleLineUpdateEvent 處理 LINE 消息更新事件
func (h *LineEventHandler) handleLineUpdateEvent(payload string) {
	event, err := infra.ParseLineUpdateEvent(payload)
	if err != nil {
		h.logger.Error().Err(err).Str("payload", payload).Msg("解析 LINE 更新事件失敗")
		return
	}

	h.logger.Debug().
		Str("order_id", event.OrderID).
		Str("config_id", event.ConfigID).
		Str("user_id", event.UserID).
		Int("retry_count", event.RetryCount).
		Msg("收到 LINE 消息更新事件")

	// 獲取訂單資料
	order, err := h.orderService.GetOrderByID(h.ctx, event.OrderID)
	if err != nil {
		h.logger.Error().Err(err).
			Str("order_id", event.OrderID).
			Msg("獲取訂單資料失敗，無法更新 LINE 消息")
		return
	}

	// 檢查是否應該推送此狀態更新
	if !h.lineService.ShouldPushStatusUpdate(event.ConfigID, order.Status) {
		h.logger.Debug().
			Str("order_id", event.OrderID).
			Str("status", string(order.Status)).
			Str("config_id", event.ConfigID).
			Msg("跳過此狀態的 LINE 消息推送")
		return
	}

	// 推送 LINE 消息
	h.pushLineMessage(order, event)
}

// pushLineMessage 推送 LINE 消息
func (h *LineEventHandler) pushLineMessage(order *model.Order, event *infra.LineUpdateEvent) {
	// 格式化 Flex Message
	flexMessage := h.lineService.FormatOrderMessage(order)

	// 推送 Flex Message，傳入 order 以便在達到額度限制時能提供更好的文字 fallback
	err := h.lineService.PushFlexMessageWithOrder(event.ConfigID, event.UserID, flexMessage, order)
	if err != nil {
		h.logger.Error().Err(err).
			Str("order_id", order.ID.Hex()).
			Str("config_id", event.ConfigID).
			Str("user_id", event.UserID).
			Msg("推送 LINE Flex Message 失敗")
		return
	}

	// 記錄推送成功
	h.logger.Info().
		Str("order_id", order.ID.Hex()).
		Str("config_id", event.ConfigID).
		Str("user_id", event.UserID).
		Str("status", string(order.Status)).
		Int("retry_count", event.RetryCount).
		Msg("LINE Flex Message 推送完成")

	// 更新訂單的 LINE 消息記錄
	h.updateOrderLineMessages(order, event.ConfigID, event.UserID)
}

// updateOrderLineMessages 更新訂單的 LINE 消息記錄
func (h *LineEventHandler) updateOrderLineMessages(order *model.Order, configID, userID string) {
	// 添加新的 LINE 消息記錄
	lineMessage := model.LineMessageInfo{
		ConfigID:    configID,
		UserID:      userID,
		MessageType: h.getMessageType(order.Status),
		Timestamp:   time.Now(),
	}

	// 更新訂單
	order.LineMessages = append(order.LineMessages, lineMessage)
	_, err := h.orderService.UpdateOrder(context.Background(), order)
	if err != nil {
		h.logger.Error().Err(err).
			Str("order_id", order.ID.Hex()).
			Msg("更新訂單 LINE 消息記錄失敗")
	}
}

// getMessageType 根據訂單狀態獲取消息類型
func (h *LineEventHandler) getMessageType(status model.OrderStatus) string {
	switch status {
	case model.OrderStatusEnroute:
		return "driver_enroute"
	case model.OrderStatusExecuting:
		return "trip_started"
	case model.OrderStatusDriverArrived:
		return "driver_arrived"
	case model.OrderStatusCompleted:
		return "trip_completed"
	case model.OrderStatusFailed:
		return "order_failed"
	default:
		return "status_update"
	}
}

// PublishLineUpdateEventForOrder 為訂單發布 LINE 更新事件
func (h *LineEventHandler) PublishLineUpdateEventForOrder(ctx context.Context, order *model.Order) {
	// 檢查訂單是否有 LINE 消息資訊
	if len(order.LineMessages) == 0 {
		h.logger.Debug().
			Str("order_id", order.ID.Hex()).
			Msg("訂單沒有 LINE 消息記錄，跳過 LINE 事件發布")
		return
	}

	h.logger.Debug().
		Str("order_id", order.ID.Hex()).
		Int("line_messages_count", len(order.LineMessages)).
		Msg("開始處理 LINE 更新事件")

	// 獲取所有唯一的用戶/群組（避免重複發送給同一個用戶/群組）
	uniqueTargets := make(map[string]*model.LineMessageInfo)
	for i := range order.LineMessages {
		lineMsg := &order.LineMessages[i]
		key := lineMsg.ConfigID + ":" + lineMsg.UserID
		// 保留最新的消息記錄
		if existing, exists := uniqueTargets[key]; !exists || lineMsg.Timestamp.After(existing.Timestamp) {
			uniqueTargets[key] = lineMsg
		}
	}

	// 為每個唯一目標發布事件
	for _, lineMsg := range uniqueTargets {
		event := &infra.LineUpdateEvent{
			OrderID:   order.ID.Hex(),
			ConfigID:  lineMsg.ConfigID,
			UserID:    lineMsg.UserID,
			EventType: infra.LineEventUpdateMessage,
			Timestamp: time.Now(),
		}

		h.logger.Debug().
			Str("order_id", order.ID.Hex()).
			Str("config_id", lineMsg.ConfigID).
			Str("user_id", lineMsg.UserID).
			Msg("發布 LINE 更新事件")

		if err := h.eventManager.PublishLineUpdateEvent(ctx, event); err != nil {
			h.logger.Error().Err(err).
				Str("order_id", order.ID.Hex()).
				Str("config_id", lineMsg.ConfigID).
				Str("user_id", lineMsg.UserID).
				Msg("發布 LINE 更新事件失敗")
		} else {
			h.logger.Info().
				Str("order_id", order.ID.Hex()).
				Str("config_id", lineMsg.ConfigID).
				Str("user_id", lineMsg.UserID).
				Msg("LINE 更新事件發布成功")
		}
	}
}
