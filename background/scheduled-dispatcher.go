package background

import (
	"context"
	"encoding/json"
	"fmt"
	"right-backend/infra"
	"right-backend/model"
	"right-backend/service"
	"right-backend/service/interfaces"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"github.com/streadway/amqp"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

const (
	checkInterval                     = 1    // 檢查間隔（分鐘）- 同時用於提醒和自動轉換
	scheduleExpiryNotifyEnabled       = true // 預約單即將到期提醒通知開關
	scheduleExpiryNotifyThresholdMins = 60   // 預約單即將到期提醒的時間閾值（分鐘）
	scheduleToInstantEnabled          = true // 預約單自動轉即時單開關
	scheduleToInstantThresholdMins    = 20   // 預約單自動轉即時單的時間閾值（分鐘）
)

type ScheduledDispatcher struct {
	logger             zerolog.Logger
	MongoDB            *infra.MongoDB
	RabbitMQ           *infra.RabbitMQ
	CrawlerSvc         *service.CrawlerService
	OrderSvc           *service.OrderService
	FcmSvc             interfaces.FCMService
	TrafficUsageLogSvc *service.TrafficUsageLogService
	BlacklistSvc       *service.DriverBlacklistService
	NotificationSvc    *service.NotificationService
	dispatcherID       string
}

func NewScheduledDispatcher(
	logger zerolog.Logger,
	mongoDB *infra.MongoDB,
	rabbitMQ *infra.RabbitMQ,
	crawlerSvc *service.CrawlerService,
	orderSvc *service.OrderService,
	fcmSvc interfaces.FCMService,
	trafficUsageLogSvc *service.TrafficUsageLogService,
	blacklistSvc *service.DriverBlacklistService,
	notificationSvc *service.NotificationService,
) *ScheduledDispatcher {
	return &ScheduledDispatcher{
		logger:             logger.With().Str("component", "scheduled-dispatcher").Logger(),
		MongoDB:            mongoDB,
		RabbitMQ:           rabbitMQ,
		CrawlerSvc:         crawlerSvc,
		OrderSvc:           orderSvc,
		FcmSvc:             fcmSvc,
		TrafficUsageLogSvc: trafficUsageLogSvc,
		BlacklistSvc:       blacklistSvc,
		NotificationSvc:    notificationSvc,
		dispatcherID:       fmt.Sprintf("scheduled-dispatcher-%d", time.Now().Unix()),
	}
}

// Start 啟動預約單調度器，監聽預約單隊列
func (sd *ScheduledDispatcher) Start(ctx context.Context) {
	msgs, err := sd.RabbitMQ.Channel.Consume(
		infra.QueueNameOrdersSchedule.String(), "", true, false, false, false, nil,
	)
	if err != nil {
		sd.logger.Fatal().Err(err).Str("queue", infra.QueueNameOrdersSchedule.String()).Msg("預約單調度器無法消費隊列")
	}

	// 啟動自動轉換檢查器
	go sd.startAutoConvertChecker(ctx)

	sd.logger.Info().Msg("預約單調度器已啟動，等待預約訂單...")
	for msg := range msgs {
		var order model.Order
		if err := json.Unmarshal(msg.Body, &order); err != nil {
			sd.logger.Error().Err(err).Msg("預約單調度器訂單資料解析失敗")
			continue
		}
		go sd.handleScheduledOrder(ctx, &order)
	}
}

// handleScheduledOrder 處理預約單
func (sd *ScheduledDispatcher) handleScheduledOrder(ctx context.Context, order *model.Order) {
	ctx, span := infra.StartScheduledDispatcherSpan(ctx, "handle_scheduled_order",
		infra.AttrOrderID(order.ID.Hex()),
		infra.AttrString("order.short_id", order.ShortID),
		infra.AttrString("order.type", string(order.Type)),
	)
	defer span.End()

	infra.AddEvent(span, "scheduled_order_received",
		infra.AttrOrderID(order.ID.Hex()),
		infra.AttrString("order.short_id", order.ShortID),
	)

	sd.logger.Info().
		Str("order_id", order.ID.Hex()).
		Str("short_id", order.ShortID).
		Msg("預約單調度器接收到新預約訂單")

	if err := sd.DispatchScheduledOrder(ctx, order); err != nil {
		infra.RecordError(span, err, "預約單處理失敗",
			infra.AttrOrderID(order.ID.Hex()),
			infra.AttrString("error", err.Error()),
		)
		sd.logger.Error().Err(err).
			Str("order_id", order.ID.Hex()).
			Str("short_id", order.ShortID).
			Msg("預約單處理失敗")
	} else {
		infra.AddEvent(span, "scheduled_order_processed_successfully")
		infra.MarkSuccess(span, infra.AttrOrderID(order.ID.Hex()))
	}
}

// DispatchScheduledOrder 派送預約訂單通知給符合條件的司機
func (sd *ScheduledDispatcher) DispatchScheduledOrder(ctx context.Context, order *model.Order) error {
	if order.Type != model.OrderTypeScheduled {
		return fmt.Errorf("訂單類型不是預約單，無法使用預約單派送邏輯")
	}

	sd.logger.Info().
		Str("order_id", order.ID.Hex()).
		Str("short_id", order.ShortID).
		Time("scheduled_at", *order.ScheduledAt).
		Msg("開始處理預約訂單")

	// 1. 無論如何都要使用 NotificationService 更新 Discord 卡片
	if sd.NotificationSvc != nil {
		if err := sd.NotificationSvc.NotifyScheduledOrderWaiting(ctx, order.ID.Hex()); err != nil {
			sd.logger.Error().Err(err).
				Str("order_id", order.ID.Hex()).
				Msg("NotificationService 處理預約單 Discord 更新失敗")
		} else {
			sd.logger.Info().
				Str("order_id", order.ID.Hex()).
				Str("short_id", order.ShortID).
				Msg("預約單 Discord 卡片已更新為等待接單狀態")
		}
	}

	sd.logger.Info().
		Str("order_id", order.ID.Hex()).
		Str("short_id", order.ShortID).
		Msg("預約單 Discord 更新完成")

	return nil
}

// 預約單新訂單派送相關函數已移除
// 新的預約單邏輯只需要 Discord 更新，不需要 FCM 派送給司機
// FCM 通知改為在提醒階段使用司機 collection 查詢

// 輔助方法

// isFleetMatching 檢查車隊匹配規則
func (sd *ScheduledDispatcher) isFleetMatching(orderFleet, driverFleet model.FleetType) bool {
	// WEI 車隊訂單只能派給 WEI 車隊司機
	if orderFleet == model.FleetTypeWEI && driverFleet != model.FleetTypeWEI {
		return false
	}
	// RSK, KD 車隊訂單不能派給 WEI 車隊司機
	if (orderFleet == model.FleetTypeRSK || orderFleet == model.FleetTypeKD) && driverFleet == model.FleetTypeWEI {
		return false
	}
	return true
}

// isDriverRejectingFleet 檢查司機是否拒絕該車隊
func (sd *ScheduledDispatcher) isDriverRejectingFleet(driver *model.DriverInfo, fleet model.FleetType) bool {
	for _, rejectFleet := range driver.RejectList {
		if rejectFleet == string(fleet) {
			return true
		}
	}
	return false
}

// hasScheduleConflict 檢查兩個預約時間是否衝突（1小時內）
func (sd *ScheduledDispatcher) hasScheduleConflict(newSchedule, existingSchedule time.Time) bool {
	timeDiff := newSchedule.Sub(existingSchedule)
	if timeDiff < 0 {
		timeDiff = -timeDiff
	}
	return timeDiff <= 1*time.Hour
}

// startAutoConvertChecker 啟動自動轉換檢查器
func (sd *ScheduledDispatcher) startAutoConvertChecker(ctx context.Context) {
	ticker := time.NewTicker(checkInterval * time.Minute)
	defer ticker.Stop()

	sd.logger.Info().
		Int("check_interval_mins", checkInterval).
		Int("threshold_mins", scheduleToInstantThresholdMins).
		Msg("預約單自動轉換檢查器已啟動")

	for {
		select {
		case <-ctx.Done():
			sd.logger.Info().Msg("預約單自動轉換檢查器已停止")
			return
		case <-ticker.C:
			go sd.checkAndConvertScheduledOrders(ctx)
		}
	}
}

// checkAndConvertScheduledOrders 檢查並轉換符合條件的預約單（簡化版）
func (sd *ScheduledDispatcher) checkAndConvertScheduledOrders(ctx context.Context) {
	now := time.Now()

	sd.logger.Info().
		Time("current_time", now).
		Int("reminder_threshold_mins", scheduleExpiryNotifyThresholdMins).
		Bool("notify_enabled", scheduleExpiryNotifyEnabled).
		Msg("🔍 開始檢查預約單")

	// 1. 先檢查預約單即將到期的情況
	if scheduleExpiryNotifyEnabled {
		reminderThresholdTime := now.Add(scheduleExpiryNotifyThresholdMins * time.Minute)
		sd.logger.Info().
			Time("reminder_threshold", reminderThresholdTime).
			Msg("📱 檢查需要提醒的預約單")
		sd.checkAndNotifyScheduleExpiry(ctx, now, reminderThresholdTime)
	}

	// 2. 檢查需要轉換的預約單（如果啟用）
	if scheduleToInstantEnabled {
		thresholdTime := now.Add(scheduleToInstantThresholdMins * time.Minute)

		// 一次性查詢並轉換
		collection := sd.MongoDB.GetCollection("orders")
		cursor, err := collection.Find(ctx, bson.M{
			"type":         model.OrderTypeScheduled,
			"status":       model.OrderStatusWaiting,
			"scheduled_at": bson.M{"$lte": thresholdTime, "$gte": now},
		})

		if err != nil {
			sd.logger.Error().Err(err).Msg("查詢預約單失敗")
			return
		}
		defer cursor.Close(ctx)

		count := 0
		for cursor.Next(ctx) {
			var order model.Order
			if err := cursor.Decode(&order); err != nil {
				sd.logger.Error().Err(err).Msg("解析預約單失敗")
				continue
			}
			go sd.convertScheduledOrderToInstant(ctx, &order)
			count++
		}

		if count > 0 {
			sd.logger.Info().Int("count", count).Msg("處理預約單轉換")
		}
	}
}

// convertScheduledOrderToInstant 將預約單轉為即時單（簡化版）
func (sd *ScheduledDispatcher) convertScheduledOrderToInstant(ctx context.Context, order *model.Order) {
	ctx, span := infra.StartScheduledDispatcherSpan(ctx, "convert_scheduled_to_instant",
		infra.AttrOrderID(order.ID.Hex()),
		infra.AttrString("order.short_id", order.ShortID),
		infra.AttrString("order.type", string(order.Type)),
	)
	defer span.End()

	infra.AddEvent(span, "scheduled_to_instant_conversion_started",
		infra.AttrOrderID(order.ID.Hex()),
		infra.AttrString("order.short_id", order.ShortID),
	)

	sd.logger.Info().
		Str("order_id", order.ID.Hex()).
		Str("short_id", order.ShortID).
		Msg("轉換預約單為即時單")

	// 1. 原子性更新：改類型 + 發送到隊列
	collection := sd.MongoDB.GetCollection("orders")
	updateFilter := bson.M{
		"_id":    order.ID,
		"status": model.OrderStatusWaiting,
		"type":   model.OrderTypeScheduled,
	}

	result, err := collection.UpdateOne(ctx, updateFilter, bson.M{
		"$set": bson.M{
			"type":           model.OrderTypeInstant,
			"converted_from": "scheduled",
			"updated_at":     time.Now(),
		},
	})

	if err != nil || result.MatchedCount == 0 {
		if err != nil {
			infra.RecordError(span, err, "更新預約單類型失敗",
				infra.AttrOrderID(order.ID.Hex()),
				infra.AttrString("error", err.Error()),
			)
			sd.logger.Error().Err(err).Str("order_id", order.ID.Hex()).Msg("更新失敗")
		} else {
			infra.AddEvent(span, "order_already_processed",
				infra.AttrOrderID(order.ID.Hex()),
			)
			sd.logger.Debug().Str("order_id", order.ID.Hex()).Msg("預約單已被處理，跳過")
		}
		return
	}

	infra.AddEvent(span, "order_type_updated_successfully",
		infra.AttrOrderID(order.ID.Hex()),
		infra.AttrString("new_type", string(model.OrderTypeInstant)),
	)

	// 2. 直接修改 order 並發送到隊列（避免重新查詢）
	order.Type = model.OrderTypeInstant
	order.ConvertedFrom = "scheduled"

	// 3. 更新原始預約單 Discord 卡片
	if err := sd.updateOriginalDiscordCard(ctx, order); err != nil {
		sd.logger.Error().Err(err).Str("order_id", order.ID.Hex()).Msg("更新原始 Discord 卡片失敗")
	}

	// 4. 發送轉換通知訊息
	if err := sd.sendConversionNotification(ctx, order); err != nil {
		sd.logger.Error().Err(err).Str("order_id", order.ID.Hex()).Msg("發送轉換通知失敗")
	}

	// 5. 發送到即時單隊列
	if err := sd.sendToInstantOrderQueue(ctx, order); err != nil {
		infra.RecordError(span, err, "發送到即時單隊列失敗",
			infra.AttrOrderID(order.ID.Hex()),
			infra.AttrString("error", err.Error()),
		)
		sd.logger.Error().Err(err).Str("order_id", order.ID.Hex()).Msg("發送到隊列失敗")
		return
	}

	infra.AddEvent(span, "scheduled_to_instant_conversion_completed")
	infra.MarkSuccess(span,
		infra.AttrOrderID(order.ID.Hex()),
		infra.AttrString("result.type", string(model.OrderTypeInstant)),
		infra.AttrBool("conversion.success", true),
	)

	sd.logger.Info().Str("order_id", order.ID.Hex()).Msg("✅ 預約單已轉為即時單")
}

// sendToInstantOrderQueue 將轉換後的即時單發送到即時單派單隊列
func (sd *ScheduledDispatcher) sendToInstantOrderQueue(ctx context.Context, order *model.Order) error {
	sd.logger.Info().
		Str("order_id", order.ID.Hex()).
		Str("short_id", order.ShortID).
		Msg("將轉換後的即時單發送到派單隊列")

	// 將訂單序列化為 JSON
	orderJSON, err := json.Marshal(order)
	if err != nil {
		sd.logger.Error().Err(err).
			Str("order_id", order.ID.Hex()).
			Msg("序列化轉換後的即時單失敗")
		return fmt.Errorf("序列化訂單失敗: %w", err)
	}

	// 發送到即時單隊列 (QueueNameOrders)
	err = sd.RabbitMQ.Channel.Publish(
		"",                             // exchange
		infra.QueueNameOrders.String(), // routing key (即時單隊列)
		false,                          // mandatory
		false,                          // immediate
		amqp.Publishing{
			ContentType: "application/json",
			Body:        orderJSON,
		},
	)

	if err != nil {
		sd.logger.Error().Err(err).
			Str("order_id", order.ID.Hex()).
			Str("queue", infra.QueueNameOrders.String()).
			Msg("發送轉換後的即時單到隊列失敗")
		return fmt.Errorf("發送到即時單隊列失敗: %w", err)
	}

	sd.logger.Info().
		Str("order_id", order.ID.Hex()).
		Str("short_id", order.ShortID).
		Str("queue", infra.QueueNameOrders.String()).
		Msg("✅ 轉換後的即時單已成功發送到派單隊列")

	return nil
}

// updateOriginalDiscordCard 更新原始預約單 Discord 卡片，顯示已轉換狀態
func (sd *ScheduledDispatcher) updateOriginalDiscordCard(ctx context.Context, order *model.Order) error {
	if sd.NotificationSvc == nil {
		return fmt.Errorf("NotificationService 未初始化")
	}

	// 使用 NotificationService 更新 Discord 卡片以顯示轉換狀態
	// order 此時已經是 OrderTypeInstant，Discord 服務會顯示 "🔄 轉換即時單－等待接單"
	if err := sd.NotificationSvc.NotifyOrderConverted(ctx, order.ID.Hex()); err != nil {
		return fmt.Errorf("更新 Discord 卡片失敗: %w", err)
	}

	sd.logger.Info().
		Str("order_id", order.ID.Hex()).
		Str("short_id", order.ShortID).
		Msg("✅ 已更新原始預約單 Discord 卡片為轉換狀態")

	return nil
}

// sendConversionNotification 發送預約單轉換即時單的通知訊息
func (sd *ScheduledDispatcher) sendConversionNotification(ctx context.Context, order *model.Order) error {
	if sd.NotificationSvc == nil {
		return fmt.Errorf("NotificationService 未初始化")
	}

	// 使用 NotificationService 發送轉換通知
	if err := sd.NotificationSvc.NotifyOrderConversionMessage(ctx, order.ID.Hex()); err != nil {
		return fmt.Errorf("發送轉換通知失敗: %w", err)
	}

	sd.logger.Info().
		Str("order_id", order.ID.Hex()).
		Str("short_id", order.ShortID).
		Msg("✅ 已發送預約單轉換通知")

	return nil
}

// checkAndNotifyScheduleExpiry 檢查並發送預約單即將到期提醒
func (sd *ScheduledDispatcher) checkAndNotifyScheduleExpiry(ctx context.Context, now, reminderThreshold time.Time) {
	orderCollection := sd.MongoDB.GetCollection("orders")

	// 查詢已接受但尚未通知的預約單，且預約時間在提醒閾值內
	filter := bson.M{
		"type":                   model.OrderTypeScheduled,                       // 預約單
		"status":                 model.OrderStatusScheduleAccepted,              // 已接受狀態
		"scheduled_at":           bson.M{"$lte": reminderThreshold, "$gte": now}, // 在提醒時間範圍內
		"driver_notified":        bson.M{"$ne": true},                            // 尚未通知司機
		"driver.assigned_driver": bson.M{"$exists": true, "$ne": ""},             // 有分派司機
	}

	sd.logger.Info().
		Interface("filter", filter).
		Msg("🔍 查詢預約單即將到期的訂單")

	cursor, err := orderCollection.Find(ctx, filter)
	if err != nil {
		sd.logger.Error().Err(err).Msg("查詢預約單即將到期的訂單失敗")
		return
	}
	defer cursor.Close(ctx)

	// 收集所有需要即將到期通知的訂單
	var ordersForExpiryNotify []*model.Order
	for cursor.Next(ctx) {
		var order model.Order
		if err := cursor.Decode(&order); err != nil {
			sd.logger.Error().Err(err).Msg("解析訂單資料失敗")
			continue
		}
		ordersForExpiryNotify = append(ordersForExpiryNotify, &order)
	}

	// 批次發送通知
	if len(ordersForExpiryNotify) > 0 {
		sd.logger.Info().Int("expiry_notify_count", len(ordersForExpiryNotify)).Msg("開始批次發送預約單即將到期通知")
		sd.batchSendScheduleExpiryNotifications(ctx, ordersForExpiryNotify)
	} else {
		sd.logger.Info().Msg("🔍 沒有找到需要即將到期通知的預約單")
	}
}

// batchSendScheduleExpiryNotifications 批次發送預約單即將到期通知給多個司機
func (sd *ScheduledDispatcher) batchSendScheduleExpiryNotifications(ctx context.Context, orders []*model.Order) {
	const maxConcurrency = 10 // 最大併發數，避免過多goroutine

	// 使用channel來控制併發數量
	semaphore := make(chan struct{}, maxConcurrency)
	var wg sync.WaitGroup

	successCount := 0
	for _, order := range orders {
		wg.Add(1)
		go func(ord *model.Order) {
			defer wg.Done()

			// 獲取semaphore
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			// 發送即將到期通知
			if err := sd.sendScheduleExpiryNotificationSingle(ctx, ord); err != nil {
				sd.logger.Error().Err(err).
					Str("order_id", ord.ID.Hex()).
					Str("short_id", ord.ShortID).
					Msg("批次發送預約單即將到期通知失敗")
			} else {
				successCount++
			}
		}(order)
	}

	wg.Wait()
	sd.logger.Info().
		Int("total_orders", len(orders)).
		Int("success_count", successCount).
		Msg("批次預約單即將到期通知發送完成")
}

// sendScheduleExpiryNotificationSingle 發送預約單即將到期通知給單一司機（返回error供批次處理使用）
func (sd *ScheduledDispatcher) sendScheduleExpiryNotificationSingle(ctx context.Context, order *model.Order) error {
	if sd.FcmSvc == nil {
		return fmt.Errorf("FCM服務未初始化")
	}

	// 檢查是否有分派司機
	if order.Driver.AssignedDriver == "" {
		return fmt.Errorf("訂單沒有分派司機")
	}

	// 獲取司機資訊
	driverCollection := sd.MongoDB.GetCollection("drivers")
	driverObjectID, err := primitive.ObjectIDFromHex(order.Driver.AssignedDriver)
	if err != nil {
		return fmt.Errorf("司機ID格式錯誤: %w", err)
	}

	var driver model.DriverInfo
	err = driverCollection.FindOne(ctx, bson.M{"_id": driverObjectID}).Decode(&driver)
	if err != nil {
		return fmt.Errorf("獲取司機資訊失敗: %w", err)
	}

	// 檢查司機是否有FCM Token
	if driver.FcmToken == "" {
		return fmt.Errorf("司機沒有FCM Token")
	}

	// 構建即將到期通知資料
	reminderData := map[string]interface{}{
		"notify_order_type": string(model.NotifyTypeScheduleOrderNotify),
		"order_id":          order.ID.Hex(),
		"message":           "預約訂單即將到期提醒",
		"scheduled_time":    "",
	}

	// 格式化預約時間（轉換為台北時間顯示）
	var scheduledTimeDisplay string
	if order.ScheduledAt != nil {
		taipeiLocation := time.FixedZone("Asia/Taipei", 8*3600)
		scheduledTimeDisplay = order.ScheduledAt.In(taipeiLocation).Format("15:04")
		reminderData["scheduled_time"] = scheduledTimeDisplay
	}

	// 構建通知內容
	title := "預約訂單通知"
	body := fmt.Sprintf("提醒您，您的預約單 %s %s 即將在%d分鐘內到期", order.OriTextDisplay, scheduledTimeDisplay, scheduleExpiryNotifyThresholdMins)

	notification := map[string]interface{}{
		"title": title,
		"body":  body,
		"sound": "default",
	}

	// 發送FCM通知
	if err := sd.FcmSvc.Send(ctx, driver.FcmToken, reminderData, notification); err != nil {
		sd.logger.Error().Err(err).
			Str("driver_id", driver.ID.Hex()).
			Str("driver_name", driver.Name).
			Str("order_id", order.ID.Hex()).
			Msg("發送預約單即將到期通知失敗")
		return fmt.Errorf("發送FCM通知失敗: %w", err)
	}

	// 標記訂單已通知司機，避免重複發送
	orderCollection := sd.MongoDB.GetCollection("orders")
	_, updateErr := orderCollection.UpdateOne(ctx,
		bson.M{"_id": order.ID},
		bson.M{"$set": bson.M{"driver_notified": true}},
	)

	if updateErr != nil {
		sd.logger.Warn().Err(updateErr).
			Str("order_id", order.ID.Hex()).
			Msg("標記訂單已通知司機失敗（但通知已成功發送）")
		// 不返回錯誤，因為通知已經發送成功
	}

	sd.logger.Info().
		Str("driver_id", driver.ID.Hex()).
		Str("driver_name", driver.Name).
		Str("order_id", order.ID.Hex()).
		Str("short_id", order.ShortID).
		Str("scheduled_time", scheduledTimeDisplay).
		Msg("✅ 預約單即將到期通知發送成功")

	return nil
}
