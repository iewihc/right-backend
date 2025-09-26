package service

import (
	"context"
	"fmt"
	"right-backend/infra"
	"right-backend/model"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

// NotificationTask 通知任務結構
type NotificationTask struct {
	Type          model.NotificationTaskType // Discord, LINE, SSE 通知類型
	OrderID       string
	Driver        *model.DriverInfo
	Order         *model.Order    // 預先查詢好的訂單，避免重複查詢
	EventType     model.EventType // 事件類型
	DistanceKm    float64         // 用於超時事件
	EstimatedMins int             // 用於超時事件
}

// NotificationService 統一的通知服務
type NotificationService struct {
	logger              zerolog.Logger
	orderService        *OrderService
	discordEventHandler interface{} // Discord 事件處理器
	lineEventHandler    interface{} // LINE 事件處理器
	sseEventManager     *SSEEventManager
	eventManager        *infra.RedisEventManager

	// Worker Pool
	notificationQueue chan NotificationTask
	workers           int
	stopCh            chan struct{}
	wg                sync.WaitGroup
	started           bool
	mu                sync.RWMutex
}

// NewNotificationService 創建新的通知服務
func NewNotificationService(
	logger zerolog.Logger,
	orderService *OrderService,
	discordEventHandler interface{},
	lineEventHandler interface{},
	sseEventManager *SSEEventManager,
	eventManager *infra.RedisEventManager,
	workers int,
	queueSize int,
) *NotificationService {
	if workers <= 0 {
		workers = 3 // 預設 3 個 worker
	}
	if queueSize <= 0 {
		queueSize = 100 // 預設隊列大小 100
	}

	ns := &NotificationService{
		logger:              logger.With().Str("module", "notification_service").Logger(),
		orderService:        orderService,
		discordEventHandler: discordEventHandler,
		lineEventHandler:    lineEventHandler,
		sseEventManager:     sseEventManager,
		eventManager:        eventManager,
		notificationQueue:   make(chan NotificationTask, queueSize),
		workers:             workers,
		stopCh:              make(chan struct{}),
	}

	return ns
}

// Start 啟動通知服務的 worker pool
func (ns *NotificationService) Start() {
	ns.mu.Lock()
	defer ns.mu.Unlock()

	if ns.started {
		return
	}

	// 啟動 worker pool
	for i := 0; i < ns.workers; i++ {
		ns.wg.Add(1)
		go ns.worker(i)
	}

	ns.started = true
	ns.logger.Info().Int("workers", ns.workers).Msg("NotificationService worker pool 已啟動")
}

// Stop 停止通知服務
func (ns *NotificationService) Stop() {
	ns.mu.Lock()
	defer ns.mu.Unlock()

	if !ns.started {
		return
	}

	close(ns.stopCh)
	ns.wg.Wait()

	ns.started = false
	ns.logger.Info().Msg("NotificationService 已停止")
}

// worker 處理通知任務的工作者
func (ns *NotificationService) worker(id int) {
	defer ns.wg.Done()

	ns.logger.Debug().Int("worker_id", id).Msg("NotificationService worker 已啟動")

	for {
		select {
		case task := <-ns.notificationQueue:
			ns.processTask(id, task)
		case <-ns.stopCh:
			ns.logger.Debug().Int("worker_id", id).Msg("NotificationService worker 正在停止")
			return
		}
	}
}

// processTask 處理單個通知任務
func (ns *NotificationService) processTask(workerID int, task NotificationTask) {
	ctx := context.Background() // 使用新 context 避免原請求 context 被取消

	startTime := time.Now()
	defer func() {
		duration := time.Since(startTime)
		ns.logger.Debug().
			Int("worker_id", workerID).
			Str("type", string(task.Type)).
			Str("order_id", task.OrderID).
			Dur("duration", duration).
			Msg("通知任務處理完成")
	}()

	switch task.Type {
	case model.NotificationDiscord:
		ns.processDiscordNotification(ctx, task)
	case model.NotificationLine:
		ns.processLineNotification(ctx, task)
	case model.NotificationSSE:
		ns.processSSENotification(ctx, task)
	default:
		ns.logger.Warn().Str("type", string(task.Type)).Msg("未知的通知類型")
	}
}

// processDiscordNotification 處理 Discord 卡片更新
func (ns *NotificationService) processDiscordNotification(ctx context.Context, task NotificationTask) {
	if ns.discordEventHandler == nil {
		return
	}

	// 檢查訂單是否有 Discord 資訊
	if task.Order.DiscordChannelID == "" || task.Order.DiscordMessageID == "" {
		return
	}

	// 特殊處理轉換說明訊息
	if task.EventType == model.EventConversionMessage {
		ns.sendConversionMessage(ctx, task)
		return
	}

	// 使用類型斷言來調用 Discord 事件處理器的方法
	if handler, ok := ns.discordEventHandler.(interface {
		PublishDiscordUpdateEventForOrder(context.Context, *model.Order)
	}); ok {
		// 調試：檢查傳遞給Discord的訂單資訊
		ns.logger.Info().
			Str("order_id", task.OrderID).
			Str("pickup_certificate_url", task.Order.PickupCertificateURL).
			Bool("is_photo_taken", task.Order.IsPhotoTaken).
			Str("event_type", string(task.EventType)).
			Msg("Discord 卡片更新 - 傳遞的訂單資訊")

		handler.PublishDiscordUpdateEventForOrder(ctx, task.Order)
		ns.logger.Info().
			Str("order_id", task.OrderID).
			Msg("Discord 卡片已更新")
	} else {
		ns.logger.Warn().Msg("Discord 事件處理器類型不匹配")
	}

	// 處理需要發送 Discord 回覆訊息的事件
	needsReply := (task.EventType == model.EventScheduledAccepted ||
		task.EventType == model.EventScheduledActivated ||
		task.EventType == model.EventDriverAccepted ||
		task.EventType == model.EventDriverArrived ||
		task.EventType == model.EventCustomerOnBoard)

	if needsReply && task.Driver != nil {
		if replyHandler, ok := ns.discordEventHandler.(interface {
			ReplyToOrderBanner(context.Context, *model.Order, string, string, string, string, string, float64, int)
		}); ok {
			var distanceKm float64
			var estimatedMins int
			var eventDescription string

			// 根據事件類型決定是否顯示距離時間
			switch task.EventType {
			case model.EventScheduledAccepted:
				// 預約單接受時不顯示距離時間
				distanceKm = 0.0
				estimatedMins = 0
				eventDescription = "預約單接受"
			case model.EventScheduledActivated:
				// 預約單激活時顯示距離時間
				distanceKm = task.DistanceKm
				estimatedMins = task.EstimatedMins
				eventDescription = "預約單激活"
			case model.EventDriverAccepted:
				// 司機接單時根據訂單類型決定是否顯示距離時間
				if task.Order.Type == model.OrderTypeInstant {
					// 即時單：顯示距離時間
					distanceKm = task.DistanceKm
					estimatedMins = task.EstimatedMins
				} else {
					// 預約單：不顯示距離時間
					distanceKm = 0.0
					estimatedMins = 0
				}
				eventDescription = "司機接單"
			case model.EventDriverArrived:
				// 司機到達時不顯示距離時間
				distanceKm = 0.0
				estimatedMins = 0
				eventDescription = "司機到達"
			case model.EventCustomerOnBoard:
				// 客人上車時不顯示距離時間
				distanceKm = 0.0
				estimatedMins = 0
				eventDescription = "客人上車"
			}

			// 記錄詳細信息
			ns.logger.Info().
				Str("order_id", task.OrderID).
				Str("event_type", string(task.EventType)).
				Str("order_type", string(task.Order.Type)).
				Str("driver_name", task.Driver.Name).
				Float64("distance_km", distanceKm).
				Int("estimated_mins", estimatedMins).
				Msgf("準備發送 %s Discord 回覆訊息", eventDescription)

			replyHandler.ReplyToOrderBanner(
				ctx,
				task.Order,
				string(task.EventType),
				string(task.Order.Fleet),
				task.Driver.Name,
				task.Driver.CarPlate,
				task.Driver.CarColor,
				distanceKm,
				estimatedMins,
			)
			ns.logger.Info().
				Str("order_id", task.OrderID).
				Str("event_type", string(task.EventType)).
				Float64("distance_km", distanceKm).
				Int("estimated_mins", estimatedMins).
				Msgf("%s Discord 回覆訊息已發送完成", eventDescription)
		}
	}
}

// processLineNotification 處理 LINE 訊息更新
func (ns *NotificationService) processLineNotification(ctx context.Context, task NotificationTask) {
	if ns.lineEventHandler == nil {
		return
	}

	// 檢查訂單是否有 LINE 訊息
	if len(task.Order.LineMessages) == 0 {
		return
	}

	// 使用類型斷言來調用 LINE 事件處理器的方法
	if handler, ok := ns.lineEventHandler.(interface {
		PublishLineUpdateEventForOrder(context.Context, *model.Order)
	}); ok {
		handler.PublishLineUpdateEventForOrder(ctx, task.Order)
		ns.logger.Info().
			Str("order_id", task.OrderID).
			Msg("LINE 訊息已更新")
	} else {
		ns.logger.Warn().Msg("LINE 事件處理器類型不匹配")
	}
}

// processSSENotification 處理 SSE 通知
func (ns *NotificationService) processSSENotification(ctx context.Context, task NotificationTask) {
	if ns.sseEventManager == nil {
		return
	}

	// 根據事件類型發送不同的 SSE 事件
	switch task.EventType {
	case model.EventDriverAccepted:
		// 司機接單事件
		distanceKm := task.Order.Driver.EstPickupDistKm
		estPickupMins := task.Order.Driver.EstPickupMins
		if task.Order.Driver.AdjustMins != nil {
			estPickupMins += *task.Order.Driver.AdjustMins
		}
		ns.sseEventManager.PushDriverAcceptedOrder(task.Order, task.Driver, distanceKm, estPickupMins)

	case model.EventScheduledActivated:
		// 預約單激活事件（司機開始前往）- 使用傳入的距離時間
		ns.sseEventManager.PushDriverAcceptedOrder(task.Order, task.Driver, task.DistanceKm, task.EstimatedMins)

	case model.EventDriverArrived:
		// 司機抵達事件
		distanceKm := task.Order.Driver.EstPickupDistKm
		estPickupMins := task.Order.Driver.EstPickupMins
		if task.Order.Driver.AdjustMins != nil {
			estPickupMins += *task.Order.Driver.AdjustMins
		}
		ns.sseEventManager.PushDriverArrived(task.Order, task.Driver, distanceKm, estPickupMins)

	case model.EventCustomerOnBoard:
		// 客人上車事件
		distanceKm := task.Order.Driver.EstPickupDistKm
		estPickupMins := task.Order.Driver.EstPickupMins
		if task.Order.Driver.AdjustMins != nil {
			estPickupMins += *task.Order.Driver.AdjustMins
		}
		ns.sseEventManager.PushCustomerOnBoard(task.Order, task.Driver, distanceKm, estPickupMins)

	case model.EventOrderCompleted:
		// 訂單完成事件
		distanceKm := task.Order.Driver.EstPickupDistKm
		estPickupMins := task.Order.Driver.EstPickupMins
		if task.Order.Driver.AdjustMins != nil {
			estPickupMins += *task.Order.Driver.AdjustMins
		}
		ns.sseEventManager.PushOrderCompleted(task.Order, task.Driver, distanceKm, estPickupMins)

	case model.EventOrderCancelled:
		// 訂單取消事件
		if ns.sseEventManager != nil {
			// 可以添加 SSE 取消事件，如果需要的話
			// ns.sseEventManager.PushOrderCancelled(task.Order, task.Driver)
		}

	case model.EventOrderFailed:
		// 訂單流單事件
		if ns.sseEventManager != nil {
			ns.sseEventManager.PushOrderFailed(task.Order, "系統流單")
		}

	case model.EventDriverRejected:
		// 司機拒單事件
		distanceKm := 0.0
		estPickupMins := 0
		// 從 task.Order.Driver 獲取距離時間信息（如果有的話）
		if task.Order.Driver.EstPickupDistKm != 0 {
			distanceKm = task.Order.Driver.EstPickupDistKm
		}
		if task.Order.Driver.EstPickupMins != 0 {
			estPickupMins = task.Order.Driver.EstPickupMins
		}
		ns.sseEventManager.PushDriverRejectedOrder(task.Order, task.Driver, distanceKm, estPickupMins)

	case model.EventDriverTimeout:
		// 司機超時事件
		if ns.sseEventManager != nil {
			ns.sseEventManager.PushDriverTimeoutOrder(task.Order, task.Driver, task.DistanceKm, task.EstimatedMins)
		}

	default:
		ns.logger.Warn().
			Str("event_type", string(task.EventType)).
			Str("order_id", task.OrderID).
			Msg("未知的 SSE 事件類型")
	}
}

// publishRedisEvent 發布 Redis 事件（同步執行）
func (ns *NotificationService) publishRedisEvent(ctx context.Context, order *model.Order, driver *model.DriverInfo, eventType string) error {
	if ns.eventManager == nil {
		return nil
	}

	details := map[string]interface{}{
		"driver_id":   driver.ID.Hex(),
		"driver_name": driver.Name,
		"car_plate":   driver.CarPlate,
	}

	var orderEvent *infra.OrderStatusEvent

	switch eventType {
	case "accepted":
		orderEvent = &infra.OrderStatusEvent{
			OrderID:   order.ID.Hex(),
			OldStatus: string(model.OrderStatusWaiting),
			NewStatus: string(model.OrderStatusEnroute),
			DriverID:  driver.ID.Hex(),
			Timestamp: time.Now(),
			Reason:    "司機接受訂單",
			EventType: infra.OrderEventAccepted,
			Details:   details,
		}
	default:
		ns.logger.Warn().Str("event_type", eventType).Msg("未知的 Redis 事件類型")
		return nil
	}

	if publishErr := ns.eventManager.PublishOrderStatusEvent(ctx, orderEvent); publishErr != nil {
		ns.logger.Error().Err(publishErr).
			Str("order_id", order.ID.Hex()).
			Str("driver_id", driver.ID.Hex()).
			Msg("Redis 事件發布失敗")
		return publishErr
	}

	ns.logger.Info().
		Str("order_id", order.ID.Hex()).
		Str("driver_id", driver.ID.Hex()).
		Str("driver_name", driver.Name).
		Msg("Redis 訂單接受事件已發布")

	return nil
}

// NotifyOrderAccepted 統一的訂單接受通知入口點（只查詢一次訂單）
func (ns *NotificationService) NotifyOrderAccepted(ctx context.Context, orderID string, driver *model.DriverInfo) error {
	// 只查詢一次完整訂單，解決 N+1 問題
	order, err := ns.orderService.GetOrderByID(ctx, orderID)
	if err != nil {
		ns.logger.Error().Err(err).Str("order_id", orderID).Msg("獲取訂單失敗")
		return err
	}

	// Redis 事件同步發布（重要，影響派單邏輯）
	if err := ns.publishRedisEvent(ctx, order, driver, "accepted"); err != nil {
		ns.logger.Error().Err(err).Str("order_id", orderID).Msg("Redis 事件發布失敗")
		// Redis 事件失敗不影響其他通知
	}

	// 根據訂單類型設定距離和時間信息
	var distanceKm float64
	var estimatedMins int
	
	if order.Type == model.OrderTypeInstant {
		// 即時單：從 order.Driver 獲取距離和時間信息
		distanceKm = order.Driver.EstPickupDistKm
		estimatedMins = order.Driver.EstPickupMins
		// 加上司機調整的時間
		if order.Driver.AdjustMins != nil {
			estimatedMins += *order.Driver.AdjustMins
		}
	} else {
		// 預約單：設為 0
		distanceKm = 0.0
		estimatedMins = 0
	}

	// 其他通知異步處理，不阻塞主流程
	notifications := []NotificationTask{
		{Type: model.NotificationDiscord, OrderID: orderID, Driver: driver, Order: order, EventType: model.EventDriverAccepted, DistanceKm: distanceKm, EstimatedMins: estimatedMins},
		{Type: model.NotificationLine, OrderID: orderID, Driver: driver, Order: order, EventType: model.EventDriverAccepted, DistanceKm: distanceKm, EstimatedMins: estimatedMins},
		{Type: model.NotificationSSE, OrderID: orderID, Driver: driver, Order: order, EventType: model.EventDriverAccepted, DistanceKm: distanceKm, EstimatedMins: estimatedMins},
	}

	for _, notification := range notifications {
		select {
		case ns.notificationQueue <- notification:
			// 成功加入隊列
		default:
			// 隊列滿了但不能跳過重要通知，改為阻塞等待
			ns.logger.Warn().
				Str("type", string(notification.Type)).
				Str("order_id", orderID).
				Msg("通知隊列已滿，等待處理...")

			// 對於重要通知（Discord/LINE卡片更新），必須確保送達
			ns.notificationQueue <- notification // 阻塞等待直到有空間
		}
	}

	return nil
}

// NotifyOrderCompleted 統一的訂單完成通知入口點
func (ns *NotificationService) NotifyOrderCompleted(ctx context.Context, orderID string, driver *model.DriverInfo) error {
	// 只查詢一次完整訂單
	order, err := ns.orderService.GetOrderByID(ctx, orderID)
	if err != nil {
		ns.logger.Error().Err(err).Str("order_id", orderID).Msg("獲取訂單失敗")
		return err
	}

	// 異步處理所有通知
	notifications := []NotificationTask{
		{Type: model.NotificationDiscord, OrderID: orderID, Driver: driver, Order: order, EventType: model.EventOrderCompleted},
		{Type: model.NotificationLine, OrderID: orderID, Driver: driver, Order: order, EventType: model.EventOrderCompleted},
		{Type: model.NotificationSSE, OrderID: orderID, Driver: driver, Order: order, EventType: model.EventOrderCompleted},
	}

	for _, notification := range notifications {
		select {
		case ns.notificationQueue <- notification:
			// 成功加入隊列
		default:
			// 隊列滿了但不能跳過重要通知，改為阻塞等待
			ns.logger.Warn().
				Str("type", string(notification.Type)).
				Str("order_id", orderID).
				Msg("通知隊列已滿，等待處理...")

			// 對於重要通知，必須確保送達
			ns.notificationQueue <- notification // 阻塞等待直到有空間
		}
	}

	return nil
}

// NotifyOrderCancelled 統一的訂單取消通知入口點
func (ns *NotificationService) NotifyOrderCancelled(ctx context.Context, orderID string, driver *model.DriverInfo) error {
	// 只查詢一次完整訂單
	order, err := ns.orderService.GetOrderByID(ctx, orderID)
	if err != nil {
		ns.logger.Error().Err(err).Str("order_id", orderID).Msg("獲取訂單失敗")
		return err
	}

	// 異步處理所有通知
	notifications := []NotificationTask{
		{Type: model.NotificationDiscord, OrderID: orderID, Driver: driver, Order: order, EventType: model.EventOrderCancelled},
		{Type: model.NotificationLine, OrderID: orderID, Driver: driver, Order: order, EventType: model.EventOrderCancelled},
		// 注意：SSE 通知通常由調用方直接處理，這裡不包含
	}

	for _, notification := range notifications {
		select {
		case ns.notificationQueue <- notification:
			// 成功加入隊列
		default:
			// 隊列滿了但不能跳過重要通知，改為阻塞等待
			ns.logger.Warn().
				Str("type", string(notification.Type)).
				Str("order_id", orderID).
				Msg("通知隊列已滿，等待處理...")

			ns.notificationQueue <- notification // 阻塞等待直到有空間
		}
	}

	return nil
}

// NotifyOrderFailed 統一的訂單流單通知入口點
func (ns *NotificationService) NotifyOrderFailed(ctx context.Context, orderID string, reason string) error {
	// 只查詢一次完整訂單
	order, err := ns.orderService.GetOrderByID(ctx, orderID)
	if err != nil {
		ns.logger.Error().Err(err).Str("order_id", orderID).Msg("獲取訂單失敗")
		return err
	}

	// 異步處理所有通知
	notifications := []NotificationTask{
		{Type: model.NotificationDiscord, OrderID: orderID, Driver: nil, Order: order, EventType: model.EventOrderFailed},
		{Type: model.NotificationLine, OrderID: orderID, Driver: nil, Order: order, EventType: model.EventOrderFailed},
		{Type: model.NotificationSSE, OrderID: orderID, Driver: nil, Order: order, EventType: model.EventOrderFailed},
	}

	for _, notification := range notifications {
		select {
		case ns.notificationQueue <- notification:
			// 成功加入隊列
		default:
			// 隊列滿了但不能跳過重要通知，改為阻塞等待
			ns.logger.Warn().
				Str("type", string(notification.Type)).
				Str("order_id", orderID).
				Msg("通知隊列已滿，等待處理...")

			ns.notificationQueue <- notification // 阻塞等待直到有空間
		}
	}

	return nil
}

// NotifyDriverArrived 統一的司機抵達通知入口點
func (ns *NotificationService) NotifyDriverArrived(ctx context.Context, orderID string, driver *model.DriverInfo, discordOnly bool) error {
	// 只查詢一次完整訂單
	order, err := ns.orderService.GetOrderByID(ctx, orderID)
	if err != nil {
		ns.logger.Error().Err(err).Str("order_id", orderID).Msg("獲取訂單失敗")
		return err
	}

	var notifications []NotificationTask

	if discordOnly {
		// 只更新 Discord 卡片
		notifications = []NotificationTask{
			{Type: model.NotificationDiscord, OrderID: orderID, Driver: driver, Order: order, EventType: model.EventDriverArrived},
		}
		ns.logger.Info().Str("order_id", orderID).Msg("處理司機抵達 - 僅 Discord 卡片更新")
	} else {
		// 完整通知（Discord + LINE + SSE）
		notifications = []NotificationTask{
			{Type: model.NotificationDiscord, OrderID: orderID, Driver: driver, Order: order, EventType: model.EventDriverArrived},
			{Type: model.NotificationLine, OrderID: orderID, Driver: driver, Order: order, EventType: model.EventDriverArrived},
			{Type: model.NotificationSSE, OrderID: orderID, Driver: driver, Order: order, EventType: model.EventDriverArrived},
		}
		ns.logger.Info().Str("order_id", orderID).Msg("處理司機抵達 - 完整通知")
	}

	for _, notification := range notifications {
		select {
		case ns.notificationQueue <- notification:
			// 成功加入隊列
		default:
			// 隊列滿了但不能跳過重要通知，改為阻塞等待
			ns.logger.Warn().
				Str("type", string(notification.Type)).
				Str("order_id", orderID).
				Msg("通知隊列已滿，等待處理...")

			ns.notificationQueue <- notification // 阻塞等待直到有空間
		}
	}

	return nil
}

// NotifyCustomerOnBoard 統一的客人上車通知入口點
func (ns *NotificationService) NotifyCustomerOnBoard(ctx context.Context, orderID string, driver *model.DriverInfo) error {
	// 只查詢一次完整訂單
	order, err := ns.orderService.GetOrderByID(ctx, orderID)
	if err != nil {
		ns.logger.Error().Err(err).Str("order_id", orderID).Msg("獲取訂單失敗")
		return err
	}

	// 異步處理所有通知
	notifications := []NotificationTask{
		{Type: model.NotificationDiscord, OrderID: orderID, Driver: driver, Order: order, EventType: model.EventCustomerOnBoard},
		{Type: model.NotificationLine, OrderID: orderID, Driver: driver, Order: order, EventType: model.EventCustomerOnBoard},
		{Type: model.NotificationSSE, OrderID: orderID, Driver: driver, Order: order, EventType: model.EventCustomerOnBoard},
	}

	for _, notification := range notifications {
		select {
		case ns.notificationQueue <- notification:
			// 成功加入隊列
		default:
			// 隊列滿了但不能跳過重要通知，改為阻塞等待
			ns.logger.Warn().
				Str("type", string(notification.Type)).
				Str("order_id", orderID).
				Msg("通知隊列已滿，等待處理...")

			ns.notificationQueue <- notification // 阻塞等待直到有空間
		}
	}

	return nil
}

// GetQueueLength 獲取當前隊列長度（用於監控）
func (ns *NotificationService) GetQueueLength() int {
	return len(ns.notificationQueue)
}

// GetWorkerCount 獲取 worker 數量
func (ns *NotificationService) GetWorkerCount() int {
	ns.mu.RLock()
	defer ns.mu.RUnlock()
	return ns.workers
}

// NotifyDriverArrivedWithPhoto 司機抵達 - 拍照證明場景（更新卡片+照片/訊息）
func (ns *NotificationService) NotifyDriverArrivedWithPhoto(ctx context.Context, orderID string, driver *model.DriverInfo) error {
	// 只查詢一次完整訂單
	order, err := ns.orderService.GetOrderByID(ctx, orderID)
	if err != nil {
		ns.logger.Error().Err(err).Str("order_id", orderID).Msg("獲取訂單失敗")
		return err
	}

	// 處理 Discord 和 LINE 通知（包含圖片更新）
	notifications := []NotificationTask{
		{Type: model.NotificationDiscord, OrderID: orderID, Driver: driver, Order: order, EventType: model.EventDriverArrived},
		{Type: model.NotificationLine, OrderID: orderID, Driver: driver, Order: order, EventType: model.EventDriverArrived},
	}

	for _, notification := range notifications {
		select {
		case ns.notificationQueue <- notification:
			// 成功加入隊列
		default:
			// 隊列滿了但不能跳過重要通知，改為阻塞等待
			ns.logger.Warn().
				Str("type", string(notification.Type)).
				Str("order_id", orderID).
				Msg("通知隊列已滿，等待處理...")

			ns.notificationQueue <- notification // 阻塞等待直到有空間
		}
	}

	ns.logger.Info().Str("order_id", orderID).Msg("司機抵達拍照通知處理完成（Discord+LINE 卡片和圖片更新）")
	return nil
}

// NotifyArrivedWithPhoto 司機抵達拍照場景通知（包含遲到檢查）
func (ns *NotificationService) NotifyArrivedWithPhoto(ctx context.Context, orderID string, driver *model.DriverInfo) error {
	// 只查詢一次完整訂單
	order, err := ns.orderService.GetOrderByID(ctx, orderID)
	if err != nil {
		ns.logger.Error().Err(err).Str("order_id", orderID).Msg("獲取訂單失敗")
		return err
	}

	// 調試：檢查訂單是否包含照片URL
	ns.logger.Info().
		Str("order_id", orderID).
		Str("pickup_certificate_url", order.PickupCertificateURL).
		Bool("is_photo_taken", order.IsPhotoTaken).
		Msg("NotifyArrivedWithPhoto - 訂單狀態檢查")

	// 檢查遲到並發送遲到通知
	if order.Driver.ArrivalDeviationSecs != nil && *order.Driver.ArrivalDeviationSecs > 0 {
		deviationSecs := *order.Driver.ArrivalDeviationSecs
		if err := ns.orderService.CheckAndNotifyDriverLateness(ctx, orderID, driver.Name, driver.CarPlate); err != nil {
			ns.logger.Error().Err(err).Str("order_id", orderID).Msg("發送司機遲到通知失敗")
		} else {
			ns.logger.Info().Str("order_id", orderID).Int("late_secs", deviationSecs).Msg("已發送司機遲到通知")
		}
	}

	// 處理 Discord 和 LINE 通知（包含圖片更新）
	notifications := []NotificationTask{
		{Type: model.NotificationDiscord, OrderID: orderID, Driver: driver, Order: order, EventType: model.EventDriverArrived},
		{Type: model.NotificationLine, OrderID: orderID, Driver: driver, Order: order, EventType: model.EventDriverArrived},
	}

	for _, notification := range notifications {
		select {
		case ns.notificationQueue <- notification:
			// 成功加入隊列
		default:
			// 隊列滿了但不能跳過重要通知，改為阻塞等待
			ns.logger.Warn().
				Str("type", string(notification.Type)).
				Str("order_id", orderID).
				Msg("通知隊列已滿，等待處理...")

			ns.notificationQueue <- notification // 阻塞等待直到有空間
		}
	}

	ns.logger.Info().Str("order_id", orderID).Msg("司機抵達拍照通知處理完成（Discord+LINE 卡片和圖片更新）")
	return nil
}

// NotifyArrivedSkipPhoto 司機抵達 - 略過拍照場景（包含遲到檢查）
func (ns *NotificationService) NotifyArrivedSkipPhoto(ctx context.Context, orderID string, driver *model.DriverInfo) error {
	// 只查詢一次完整訂單
	order, err := ns.orderService.GetOrderByID(ctx, orderID)
	if err != nil {
		ns.logger.Error().Err(err).Str("order_id", orderID).Msg("獲取訂單失敗")
		return err
	}

	// 檢查遲到並發送遲到通知
	if order.Driver.ArrivalDeviationSecs != nil && *order.Driver.ArrivalDeviationSecs > 0 {
		deviationSecs := *order.Driver.ArrivalDeviationSecs
		if err := ns.orderService.CheckAndNotifyDriverLateness(ctx, orderID, driver.Name, driver.CarPlate); err != nil {
			ns.logger.Error().Err(err).Str("order_id", orderID).Msg("發送司機遲到通知失敗")
		} else {
			ns.logger.Info().Str("order_id", orderID).Int("late_secs", deviationSecs).Msg("已發送司機遲到通知")
		}
	}

	// 完整通知（Discord + LINE + SSE）
	notifications := []NotificationTask{
		{Type: model.NotificationDiscord, OrderID: orderID, Driver: driver, Order: order, EventType: model.EventDriverArrived},
		{Type: model.NotificationLine, OrderID: orderID, Driver: driver, Order: order, EventType: model.EventDriverArrived},
		{Type: model.NotificationSSE, OrderID: orderID, Driver: driver, Order: order, EventType: model.EventDriverArrived},
	}

	for _, notification := range notifications {
		select {
		case ns.notificationQueue <- notification:
			// 成功加入隊列
		default:
			// 隊列滿了但不能跳過重要通知，改為阻塞等待
			ns.logger.Warn().
				Str("type", string(notification.Type)).
				Str("order_id", orderID).
				Msg("通知隊列已滿，等待處理...")

			ns.notificationQueue <- notification // 阻塞等待直到有空間
		}
	}

	ns.logger.Info().Str("order_id", orderID).Msg("司機抵達報告通知處理完成（完整通知）")
	return nil
}

// SetDiscordEventHandler 設定 Discord 事件處理器
func (ns *NotificationService) SetDiscordEventHandler(handler interface{}) {
	ns.mu.Lock()
	defer ns.mu.Unlock()
	ns.discordEventHandler = handler
}

// SetLineEventHandler 設定 LINE 事件處理器
func (ns *NotificationService) SetLineEventHandler(handler interface{}) {
	ns.mu.Lock()
	defer ns.mu.Unlock()
	ns.lineEventHandler = handler
}

// NotifyOrderRejected 統一的訂單拒絕通知入口點（純通知，不包含 Redis 事件）
func (ns *NotificationService) NotifyOrderRejected(ctx context.Context, orderID string, driver *model.DriverInfo) error {
	// 只查詢一次完整訂單
	order, err := ns.orderService.GetOrderByID(ctx, orderID)
	if err != nil {
		ns.logger.Error().Err(err).Str("order_id", orderID).Msg("獲取訂單失敗")
		return err
	}

	// 異步處理通知（不包含 Redis 事件，那是業務邏輯）
	notifications := []NotificationTask{
		{Type: model.NotificationSSE, OrderID: orderID, Driver: driver, Order: order, EventType: model.EventDriverRejected},
		// 可選：未來可添加 Discord/LINE 拒單通知
		// {Type: model.NotificationDiscord, OrderID: orderID, Driver: driver, Order: order, EventType: model.EventDriverRejected},
		// {Type: model.NotificationLine, OrderID: orderID, Driver: driver, Order: order, EventType: model.EventDriverRejected},
	}

	for _, notification := range notifications {
		select {
		case ns.notificationQueue <- notification:
			// 成功加入隊列
		default:
			// 隊列滿了，但拒單通知相對不那麼關鍵，記錄警告即可
			ns.logger.Warn().
				Str("type", string(notification.Type)).
				Str("order_id", orderID).
				Msg("拒單通知隊列已滿，跳過此通知")
		}
	}

	ns.logger.Info().Str("order_id", orderID).Msg("拒單通知處理完成")
	return nil
}

// NotifyDriverTimeout 統一的司機超時通知入口點
func (ns *NotificationService) NotifyDriverTimeout(ctx context.Context, orderID string, driver *model.DriverInfo, distanceKm float64, estimatedMins int) error {
	// 只查詢一次完整訂單
	order, err := ns.orderService.GetOrderByID(ctx, orderID)
	if err != nil {
		ns.logger.Error().Err(err).Str("order_id", orderID).Msg("獲取訂單失敗")
		return err
	}

	// 異步處理 SSE 通知（司機超時主要是 SSE 事件）
	notifications := []NotificationTask{
		{Type: model.NotificationSSE, OrderID: orderID, Driver: driver, Order: order, EventType: model.EventDriverTimeout, DistanceKm: distanceKm, EstimatedMins: estimatedMins},
	}

	for _, notification := range notifications {
		select {
		case ns.notificationQueue <- notification:
			// 成功加入隊列
		default:
			// 超時通知相對不那麼關鍵，記錄警告即可
			ns.logger.Warn().
				Str("type", string(notification.Type)).
				Str("order_id", orderID).
				Msg("超時通知隊列已滿，跳過此通知")
		}
	}

	ns.logger.Info().Str("order_id", orderID).Msg("司機超時通知處理完成")
	return nil
}

// NotifyScheduledOrderAccepted 預約單司機接收通知（尚未激活）
func (ns *NotificationService) NotifyScheduledOrderAccepted(ctx context.Context, orderID string, driver *model.DriverInfo) error {
	// 只查詢一次完整訂單
	order, err := ns.orderService.GetOrderByID(ctx, orderID)
	if err != nil {
		ns.logger.Error().Err(err).Str("order_id", orderID).Msg("獲取訂單失敗")
		return err
	}

	// 異步處理 Discord 和 LINE 通知（預約單接收主要更新卡片狀態）
	notifications := []NotificationTask{
		{Type: model.NotificationDiscord, OrderID: orderID, Driver: driver, Order: order, EventType: model.EventScheduledAccepted},
		{Type: model.NotificationLine, OrderID: orderID, Driver: driver, Order: order, EventType: model.EventScheduledAccepted},
	}

	for _, notification := range notifications {
		select {
		case ns.notificationQueue <- notification:
			// 成功加入隊列
		default:
			// 預約單接收通知很重要，必須確保送達
			ns.logger.Warn().
				Str("type", string(notification.Type)).
				Str("order_id", orderID).
				Msg("預約單接收通知隊列已滿，等待處理...")

			ns.notificationQueue <- notification // 阻塞等待直到有空間
		}
	}

	ns.logger.Info().
		Str("order_id", orderID).
		Str("driver_id", driver.ID.Hex()).
		Str("driver_name", driver.Name).
		Msg("預約單司機接收通知處理完成")

	return nil
}

// NotifyScheduledOrderActivated 預約單激活通知（司機開始前往）
func (ns *NotificationService) NotifyScheduledOrderActivated(ctx context.Context, orderID string, driver *model.DriverInfo, distanceKm float64, estimatedMins int) error {
	// 追蹤重複調用問題
	ns.logger.Info().
		Str("order_id", orderID).
		Str("driver_id", driver.ID.Hex()).
		Str("driver_name", driver.Name).
		Float64("distance_km", distanceKm).
		Int("estimated_mins", estimatedMins).
		Msg("NotifyScheduledOrderActivated 被調用")

	// 只查詢一次完整訂單
	order, err := ns.orderService.GetOrderByID(ctx, orderID)
	if err != nil {
		ns.logger.Error().Err(err).Str("order_id", orderID).Msg("獲取訂單失敗")
		return err
	}

	// 異步處理所有通知（預約單激活需要更新卡片狀態和發送回覆訊息）
	notifications := []NotificationTask{
		{Type: model.NotificationDiscord, OrderID: orderID, Driver: driver, Order: order, EventType: model.EventScheduledActivated, DistanceKm: distanceKm, EstimatedMins: estimatedMins},
		{Type: model.NotificationLine, OrderID: orderID, Driver: driver, Order: order, EventType: model.EventScheduledActivated, DistanceKm: distanceKm, EstimatedMins: estimatedMins},
		{Type: model.NotificationSSE, OrderID: orderID, Driver: driver, Order: order, EventType: model.EventScheduledActivated, DistanceKm: distanceKm, EstimatedMins: estimatedMins},
	}

	for _, notification := range notifications {
		select {
		case ns.notificationQueue <- notification:
			// 成功加入隊列
		default:
			// 預約單激活通知很重要，必須確保送達
			ns.logger.Warn().
				Str("type", string(notification.Type)).
				Str("order_id", orderID).
				Msg("預約單激活通知隊列已滿，等待處理...")

			ns.notificationQueue <- notification // 阻塞等待直到有空間
		}
	}

	ns.logger.Info().
		Str("order_id", orderID).
		Str("driver_id", driver.ID.Hex()).
		Str("driver_name", driver.Name).
		Float64("distance_km", distanceKm).
		Int("estimated_mins", estimatedMins).
		Msg("預約單激活通知處理完成")

	return nil
}

// NotifyScheduledOrderWaiting 預約單等待接單狀態通知
func (ns *NotificationService) NotifyScheduledOrderWaiting(ctx context.Context, orderID string) error {
	// 查詢完整訂單
	order, err := ns.orderService.GetOrderByID(ctx, orderID)
	if err != nil {
		ns.logger.Error().Err(err).Str("order_id", orderID).Msg("獲取預約單失敗")
		return err
	}

	// 異步處理 Discord 和 LINE 通知（更新卡片狀態為預約單等待接單）
	notifications := []NotificationTask{
		{Type: model.NotificationDiscord, OrderID: orderID, Driver: nil, Order: order, EventType: model.EventScheduledWaiting},
		{Type: model.NotificationLine, OrderID: orderID, Driver: nil, Order: order, EventType: model.EventScheduledWaiting},
	}

	for _, notification := range notifications {
		select {
		case ns.notificationQueue <- notification:
			// 成功加入隊列
		default:
			// 預約單狀態更新很重要，必須確保送達
			ns.logger.Warn().
				Str("type", string(notification.Type)).
				Str("order_id", orderID).
				Msg("通知隊列滿，使用同步處理預約單等待通知")
			go ns.processTask(-1, notification)
		}
	}

	ns.logger.Info().Str("order_id", orderID).Msg("預約單等待接單通知處理完成")
	return nil
}

// NotifyOrderConverted 預約單轉換為即時單的狀態更新通知
func (ns *NotificationService) NotifyOrderConverted(ctx context.Context, orderID string) error {
	// 查詢完整訂單
	order, err := ns.orderService.GetOrderByID(ctx, orderID)
	if err != nil {
		ns.logger.Error().Err(err).Str("order_id", orderID).Msg("獲取轉換後訂單失敗")
		return err
	}

	// 異步處理 Discord 和 LINE 通知（更新卡片狀態為轉換即時單等待接單）
	notifications := []NotificationTask{
		{Type: model.NotificationDiscord, OrderID: orderID, Driver: nil, Order: order, EventType: model.EventOrderConverted},
		{Type: model.NotificationLine, OrderID: orderID, Driver: nil, Order: order, EventType: model.EventOrderConverted},
	}

	for _, notification := range notifications {
		select {
		case ns.notificationQueue <- notification:
			// 成功加入隊列
		default:
			// 轉換狀態更新很重要，必須確保送達
			ns.logger.Warn().
				Str("type", string(notification.Type)).
				Str("order_id", orderID).
				Msg("通知隊列滿，使用同步處理訂單轉換通知")
			go ns.processTask(-1, notification)
		}
	}

	ns.logger.Info().Str("order_id", orderID).Msg("預約單轉換狀態更新通知處理完成")
	return nil
}

// NotifyOrderConversionMessage 發送預約單轉換為即時單的說明訊息
func (ns *NotificationService) NotifyOrderConversionMessage(ctx context.Context, orderID string) error {
	// 查詢完整訂單
	order, err := ns.orderService.GetOrderByID(ctx, orderID)
	if err != nil {
		ns.logger.Error().Err(err).Str("order_id", orderID).Msg("獲取轉換後訂單失敗")
		return err
	}

	// 異步處理 Discord 和 LINE 通知（發送轉換說明訊息）
	notifications := []NotificationTask{
		{Type: model.NotificationDiscord, OrderID: orderID, Driver: nil, Order: order, EventType: model.EventConversionMessage},
		{Type: model.NotificationLine, OrderID: orderID, Driver: nil, Order: order, EventType: model.EventConversionMessage},
	}

	for _, notification := range notifications {
		select {
		case ns.notificationQueue <- notification:
			// 成功加入隊列
		default:
			// 轉換說明訊息很重要，必須確保送達
			ns.logger.Warn().
				Str("type", string(notification.Type)).
				Str("order_id", orderID).
				Msg("通知隊列滿，使用同步處理轉換說明訊息")
			go ns.processTask(-1, notification)
		}
	}

	ns.logger.Info().Str("order_id", orderID).Msg("預約單轉換說明訊息處理完成")
	return nil
}

// sendConversionMessage 發送轉換說明訊息到 Discord
func (ns *NotificationService) sendConversionMessage(ctx context.Context, task NotificationTask) {
	if ns.discordEventHandler == nil {
		return
	}

	// 檢查是否有 Discord 服務可以發送訊息
	if handler, ok := ns.discordEventHandler.(interface {
		SendMessage(channelID, message string) (*interface{}, error)
	}); ok {
		// 構建轉換說明訊息
		notificationMessage := fmt.Sprintf(
			"🔄 **預約單自動轉換通知**\n"+
				"訂單編號：%s\n"+
				"原預約時間：%s\n"+
				"由於預約時間即將到達（30分鐘內），此預約單已自動轉換為即時單，將重新進行派單。",
			task.Order.ShortID,
			func() string {
				if task.Order.ScheduledAt != nil {
					return task.Order.ScheduledAt.Format("2006-01-02 15:04:05")
				}
				return "未設定"
			}(),
		)

		// 發送到同一個 Discord 頻道
		if _, err := handler.SendMessage(task.Order.DiscordChannelID, notificationMessage); err != nil {
			ns.logger.Error().
				Err(err).
				Str("order_id", task.OrderID).
				Str("channel_id", task.Order.DiscordChannelID).
				Msg("發送轉換說明訊息失敗")
		} else {
			ns.logger.Info().
				Str("order_id", task.OrderID).
				Str("channel_id", task.Order.DiscordChannelID).
				Msg("✅ 轉換說明訊息已發送")
		}
	} else {
		ns.logger.Warn().Msg("Discord 事件處理器不支援 SendMessage 方法")
	}
}
