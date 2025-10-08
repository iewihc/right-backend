package background

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"right-backend/infra"
	"right-backend/model"
	"right-backend/service"
	"right-backend/service/interfaces"
	"right-backend/utils"
	"sort"
	"strconv"
	"strings"
	"time"

	driverModels "right-backend/data-models/driver"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.opentelemetry.io/otel/trace"
)

const (
	haversineCandidatesCount   = 15               // 直線距離計算的候選司機數量上限（用於初步篩選）
	googleAPICandidatesCount   = 5                // Google Maps API 真實路徑計算的候選司機數量上限（最終派單數）
	sequentialCallTimeout      = 19 * time.Second // 依序呼叫每位司機的等待時間（前端顯示17秒）
	enableMaxDriverDistance    = true             // 啟用司機直線距離篩選功能
	maxDriverDistanceKm        = 15.0             // 司機直線距離超過15公里將被篩選排除
	enableMaxEstimatedTimeMins = true             // 啟用真實距離預估時間篩選功能
	maxEstimatedTimeMins       = 20               // 真實距離預估時間超過20分鐘將被篩選排除
)

type Dispatcher struct {
	logger              zerolog.Logger
	MongoDB             *infra.MongoDB
	RabbitMQ            *infra.RabbitMQ
	CrawlerSvc          *service.CrawlerService
	OrderSvc            *service.OrderService
	FcmSvc              interfaces.FCMService
	TrafficUsageLogSvc  *service.TrafficUsageLogService
	BlacklistSvc        *service.DriverBlacklistService
	EventManager        *infra.RedisEventManager // 新增：事件管理器
	NotificationService *service.NotificationService
	dispatcherID        string // 新增：調度器唯一ID
}

// NewDispatcher 建立新的 Dispatcher
func NewDispatcher(logger zerolog.Logger, mongoDB *infra.MongoDB, rabbitMQ *infra.RabbitMQ, crawlerSvc *service.CrawlerService, orderSvc *service.OrderService, fcmSvc interfaces.FCMService, trafficUsageLogSvc *service.TrafficUsageLogService, blacklistSvc *service.DriverBlacklistService, redisClient interface{}, notificationService *service.NotificationService) *Dispatcher {
	// 生成唯一的調度器ID
	dispatcherID := fmt.Sprintf("dispatcher_%d_%d", time.Now().Unix(), rand.Int63())

	// 建立事件管理器（假設 MongoDB.Redis 或傳入的 redisClient）
	var eventManager *infra.RedisEventManager
	if rc, ok := redisClient.(*redis.Client); ok {
		eventManager = infra.NewRedisEventManager(rc, logger)
	}

	return &Dispatcher{
		logger:              logger.With().Str("module", "dispatcher").Logger(),
		MongoDB:             mongoDB,
		RabbitMQ:            rabbitMQ,
		CrawlerSvc:          crawlerSvc,
		OrderSvc:            orderSvc,
		FcmSvc:              fcmSvc,
		TrafficUsageLogSvc:  trafficUsageLogSvc,
		BlacklistSvc:        blacklistSvc,
		EventManager:        eventManager,        // 新增
		NotificationService: notificationService, // 新增
		dispatcherID:        dispatcherID,        // 新增
	}
}

// logTrafficUsage 記錄流量使用到 traffic_usage_log 表
func (d *Dispatcher) logTrafficUsage(ctx context.Context, service, api string, params map[string]interface{}, fleet string, elements int) {
	if d.TrafficUsageLogSvc != nil {
		paramsJSON, _ := json.Marshal(params)
		trafficLog := &model.TrafficUsageLog{
			Service:   service,
			API:       api,
			Params:    string(paramsJSON),
			Fleet:     fleet,
			Elements:  elements,
			CreatedBy: "dispatcher",
		}
		if _, err := d.TrafficUsageLogSvc.CreateTrafficUsageLog(ctx, trafficLog); err != nil {
			d.logger.Error().Err(err).Msg("記錄流量使用失敗")
		}
	}
}

func (d *Dispatcher) Start(ctx context.Context) {
	msgs, err := d.RabbitMQ.Channel.Consume(
		infra.QueueNameOrders.String(), "", true, false, false, false, nil,
	)
	if err != nil {
		d.logger.Fatal().Err(err).Str("queue", infra.QueueNameOrders.String()).Msg("調度中心無法消費隊列")
	}
	d.logger.Info().Msg("調度中心已啟動，等待訂單...")
	for msg := range msgs {
		var order model.Order
		if err := json.Unmarshal(msg.Body, &order); err != nil {
			d.logger.Error().Err(err).Msg("調度中心訂單資料解析失敗")
			continue
		}
		go d.handleOrder(ctx, &order)
	}
}

func (d *Dispatcher) handleOrder(ctx context.Context, order *model.Order) {
	// 獲取當前 span
	span := trace.SpanFromContext(ctx)

	// 創建業務 span
	dispatchCtx, dispatchSpan := infra.StartSpan(ctx, "dispatcher_handle_order",
		infra.AttrOperation("handle_order"),
		infra.AttrOrderID(order.ID.Hex()),
		infra.AttrString("order.short_id", order.ShortID),
		infra.AttrString("order.type", string(order.Type)),
		infra.AttrString("order.fleet", string(order.Fleet)),
		infra.AttrString("pickup_address", order.Customer.PickupAddress),
		infra.AttrString("dest_address", order.Customer.DestAddress),
	)
	defer dispatchSpan.End()

	// 添加派單開始事件
	infra.AddEvent(dispatchSpan, "dispatch_started",
		infra.AttrOrderID(order.ID.Hex()),
		infra.AttrString("short_id", order.ShortID),
		infra.AttrString("ori_text", order.OriText),
	)

	d.logger.Debug().
		Str("short_id", order.ShortID).
		Str("order_id", order.ID.Hex()).
		Str("pickup", order.Customer.PickupAddress).
		Str("dest", order.Customer.DestAddress).
		Str("ori_text", order.OriText).
		Str("trace_id", span.SpanContext().TraceID().String()).
		Str("span_id", dispatchSpan.SpanContext().SpanID().String()).
		Msg("[調度中心-{short_id}]: 接收到新訂單 {ori_text}")

	// 檢查訂單類型，預約單應該已經被 ScheduledDispatcher 處理，這裡不應該接收到
	if order.Type == model.OrderTypeScheduled {
		infra.AddEvent(dispatchSpan, "invalid_order_type",
			infra.AttrString("order_type", string(order.Type)),
			infra.AttrString("reason", "scheduled_order_received_by_instant_dispatcher"),
		)
		infra.SetAttributes(dispatchSpan,
			infra.AttrString("dispatch.failure_reason", "invalid_order_type"),
			infra.AttrBool("dispatch.success", false),
		)
		d.logger.Warn().
			Str("short_id", order.ShortID).
			Str("order_id", order.ID.Hex()).
			Str("trace_id", span.SpanContext().TraceID().String()).
			Str("span_id", dispatchSpan.SpanContext().SpanID().String()).
			Msg("即時單調度器接收到預約單，這不應該發生")
		return
	}

	// 記錄每次調度處理（每個訂單經過dispatcher都記錄一次，使用googleAPICandidatesCount作為Elements）
	dispatchParams := map[string]interface{}{
		"order_id":       order.ID.Hex(),
		"pickup_address": order.Customer.PickupAddress,
		"dest_address":   order.Customer.DestAddress,
	}
	d.logTrafficUsage(ctx, "google", "dispatch", dispatchParams, string(order.Fleet), googleAPICandidatesCount)

	// 1. Find best candidate drivers
	infra.AddEvent(dispatchSpan, "finding_candidate_drivers")
	candidates, distances, durationMins, err := d.findCandidateDrivers(dispatchCtx, order)
	if err != nil {
		// 記錄錯誤到 span
		infra.RecordError(dispatchSpan, err, "Find candidate drivers failed",
			infra.AttrOrderID(order.ID.Hex()),
			infra.AttrString("error", err.Error()),
		)
		infra.AddEvent(dispatchSpan, "find_candidates_failed",
			infra.AttrString("error", err.Error()),
		)

		d.logger.Error().Err(err).
			Str("short_id", order.ShortID).
			Str("order_id", order.ID.Hex()).
			Str("trace_id", span.SpanContext().TraceID().String()).
			Str("span_id", dispatchSpan.SpanContext().SpanID().String()).
			Msg("調度中心尋找候選司機失敗")
		if err := d.failOrder(dispatchCtx, *order.ID, "尋找司機過程出錯"); err != nil {
			d.logger.Error().Err(err).
				Str("short_id", order.ShortID).
				Str("order_id", order.ID.Hex()).
				Str("trace_id", span.SpanContext().TraceID().String()).
				Str("span_id", dispatchSpan.SpanContext().SpanID().String()).
				Msg("調度中心更新訂單為失敗狀態時出錯")
		}
		return
	}

	if len(candidates) == 0 {
		infra.AddEvent(dispatchSpan, "no_candidates_found")
		infra.SetAttributes(dispatchSpan,
			infra.AttrString("dispatch.failure_reason", "no_candidates"),
			infra.AttrBool("dispatch.success", false),
			infra.AttrInt("candidates.count", 0),
		)
		d.logger.Warn().
			Str("short_id", order.ShortID).
			Str("order_id", order.ID.Hex()).
			Str("trace_id", span.SpanContext().TraceID().String()).
			Str("span_id", dispatchSpan.SpanContext().SpanID().String()).
			Msg("調度中心找不到任何可派單的司機")
		if err := d.failOrder(dispatchCtx, *order.ID, "附近無可用司機"); err != nil {
			d.logger.Error().Err(err).
				Str("short_id", order.ShortID).
				Str("order_id", order.ID.Hex()).
				Str("trace_id", span.SpanContext().TraceID().String()).
				Str("span_id", dispatchSpan.SpanContext().SpanID().String()).
				Msg("調度中心更新訂單為失敗狀態時出錯")
		}
		return
	}

	// 添加候選司機信息到 span
	infra.AddEvent(dispatchSpan, "candidates_found",
		infra.AttrInt("candidates_count", len(candidates)),
	)
	infra.SetAttributes(dispatchSpan,
		infra.AttrInt("candidates.count", len(candidates)),
	)

	// 2. Dispatch order to candidates in parallel
	infra.AddEvent(dispatchSpan, "dispatching_to_drivers",
		infra.AttrInt("candidates_count", len(candidates)),
	)
	orderMatched, err := d.dispatchOrderToDrivers(dispatchCtx, order, candidates, distances, durationMins)
	if err != nil {
		// 記錄錯誤到 span
		infra.RecordError(dispatchSpan, err, "Dispatch to drivers failed",
			infra.AttrOrderID(order.ID.Hex()),
			infra.AttrString("error", err.Error()),
		)
		infra.AddEvent(dispatchSpan, "dispatch_to_drivers_failed",
			infra.AttrString("error", err.Error()),
		)
		d.logger.Error().Err(err).
			Str("short_id", order.ShortID).
			Str("order_id", order.ID.Hex()).
			Str("trace_id", span.SpanContext().TraceID().String()).
			Str("span_id", dispatchSpan.SpanContext().SpanID().String()).
			Msg("調度中心派單過程發生錯誤")
	}

	// 3. Handle dispatch result
	if !orderMatched {
		infra.AddEvent(dispatchSpan, "order_not_matched")
		// 在標記失敗前，再次檢查訂單狀態，以處理邊界情況 (e.g. 司機在超時後一瞬間接單)
		finalOrder, checkErr := d.OrderSvc.GetOrderByID(dispatchCtx, order.ID.Hex())
		if checkErr == nil && finalOrder.Status != model.OrderStatusWaiting {
			infra.AddEvent(dispatchSpan, "order_already_processed",
				infra.AttrString("final_status", string(finalOrder.Status)),
			)
			infra.SetAttributes(dispatchSpan,
				infra.AttrString("dispatch.result", "already_processed"),
				infra.AttrBool("dispatch.success", true),
			)
			d.logger.Info().
				Str("short_id", order.ShortID).
				Str("order_id", order.ID.Hex()).
				Str("status", string(finalOrder.Status)).
				Str("trace_id", span.SpanContext().TraceID().String()).
				Str("span_id", dispatchSpan.SpanContext().SpanID().String()).
				Msg("調度中心訂單在最後確認時已被處理")
		} else {
			infra.AddEvent(dispatchSpan, "order_timeout_no_acceptance")
			infra.SetAttributes(dispatchSpan,
				infra.AttrString("dispatch.failure_reason", "no_driver_acceptance"),
				infra.AttrBool("dispatch.success", false),
			)
			d.logger.Warn().
				Str("order_id", order.ID.Hex()).
				Str("trace_id", span.SpanContext().TraceID().String()).
				Str("span_id", dispatchSpan.SpanContext().SpanID().String()).
				Msgf("[調度中心-%s]: (%s) 派單時間截止，訂單無人接受", order.ShortID, order.OriText)
			if err := d.failOrder(dispatchCtx, *order.ID, "無司機接單"); err != nil {
				d.logger.Error().Err(err).
					Str("short_id", order.ShortID).
					Str("order_id", order.ID.Hex()).
					Str("trace_id", span.SpanContext().TraceID().String()).
					Str("span_id", dispatchSpan.SpanContext().SpanID().String()).
					Msg("調度中心更新訂單為失敗狀態時出錯")
			}
		}
	} else {
		infra.AddEvent(dispatchSpan, "order_matched_successfully")
		infra.MarkSuccess(dispatchSpan,
			infra.AttrString("dispatch.result", "matched"),
			infra.AttrBool("dispatch.success", true),
		)
		d.logger.Info().
			Str("short_id", order.ShortID).
			Str("order_id", order.ID.Hex()).
			Str("trace_id", span.SpanContext().TraceID().String()).
			Str("span_id", dispatchSpan.SpanContext().SpanID().String()).
			Msg("調度中心訂單已成功匹配司機")
	}

	// 4. Finalize order processing by incrementing the dispatch round
	infra.AddEvent(dispatchSpan, "finalizing_dispatch_round")
	ordersColl := d.MongoDB.GetCollection("orders")
	_, err = ordersColl.UpdateOne(dispatchCtx, map[string]interface{}{"_id": order.ID}, map[string]interface{}{"$inc": map[string]interface{}{"rounds": 1}, "$set": map[string]interface{}{"updated_at": time.Now()}})
	if err != nil {
		infra.AddEvent(dispatchSpan, "rounds_update_failed",
			infra.AttrString("error", err.Error()),
		)
		d.logger.Error().Err(err).
			Str("short_id", order.ShortID).
			Str("trace_id", span.SpanContext().TraceID().String()).
			Str("span_id", dispatchSpan.SpanContext().SpanID().String()).
			Msg("調度中心rounds更新失敗")
	} else {
		infra.AddEvent(dispatchSpan, "dispatch_completed")
	}

	d.logger.Info().
		Str("short_id", order.ShortID).
		Str("order_id", order.ID.Hex()).
		Str("ori_text", order.OriText).
		Str("trace_id", span.SpanContext().TraceID().String()).
		Str("span_id", dispatchSpan.SpanContext().SpanID().String()).
		Msg("[調度中心-{short_id}]: ({ori_text}) 訂單派單流程完成")
}

// 第一步驟 查找並過濾出最佳的候選司機 (上線司機)
func (d *Dispatcher) findCandidateDrivers(ctx context.Context, order *model.Order) ([]*model.DriverInfo, []string, []int, error) {
	// 獲取當前 span
	span := trace.SpanFromContext(ctx)

	// 創建業務 span
	findCtx, findSpan := infra.StartSpan(ctx, "dispatcher_find_candidates",
		infra.AttrOperation("find_candidates"),
		infra.AttrOrderID(order.ID.Hex()),
		infra.AttrString("order.fleet", string(order.Fleet)),
	)
	defer findSpan.End()

	// 添加查找開始事件
	infra.AddEvent(findSpan, "find_candidates_started",
		infra.AttrOrderID(order.ID.Hex()),
	)
	// 1. 查詢所有上線司機
	infra.AddEvent(findSpan, "querying_online_drivers")
	driversColl := d.MongoDB.GetCollection("drivers")
	filter := map[string]interface{}{
		"is_online": true,
		"status": model.DriverStatusIdle, // 只查詢閒置狀態的司機，防止race condition
		"$and": []interface{}{
			map[string]interface{}{
				"$or": []interface{}{
					map[string]interface{}{"is_active": true},
					map[string]interface{}{"is_active": map[string]interface{}{"$exists": false}},
				},
			},
			map[string]interface{}{
				"$or": []interface{}{
					map[string]interface{}{"is_approved": true},
					map[string]interface{}{"is_approved": map[string]interface{}{"$exists": false}},
				},
			},
		},
	}

	cursor, err := driversColl.Find(findCtx, filter)
	if err != nil {
		infra.RecordError(findSpan, err, "Database query failed",
			infra.AttrString("error", err.Error()),
		)
		d.logger.Error().Err(err).
			Str("trace_id", span.SpanContext().TraceID().String()).
			Str("span_id", findSpan.SpanContext().SpanID().String()).
			Msg("查詢司機失敗")
		return nil, nil, nil, err
	}
	defer func() {
		if err := cursor.Close(ctx); err != nil {
			d.logger.Error().Err(err).Msg("關閉cursor失敗")
		}
	}()

	var drivers []*model.DriverInfo
	if err := cursor.All(findCtx, &drivers); err != nil {
		infra.RecordError(findSpan, err, "Driver parsing failed",
			infra.AttrString("error", err.Error()),
		)
		d.logger.Error().Err(err).
			Str("trace_id", span.SpanContext().TraceID().String()).
			Str("span_id", findSpan.SpanContext().SpanID().String()).
			Msg("解析司機失敗")
		return nil, nil, nil, err
	}

	// 添加初始司機數量事件
	infra.AddEvent(findSpan, "initial_drivers_found",
		infra.AttrInt("initial_count", len(drivers)),
	)

	// 2. 過濾掉黑名單、沒有有效經緯度和正在處理訂單的司機
	var validDrivers []*model.DriverInfo
	for _, drv := range drivers {
		// 檢查司機是否在黑名單中
		if infra.AppConfig.DriverBlacklist.Enabled && d.BlacklistSvc != nil {
			isBlacklisted, err := d.BlacklistSvc.IsDriverBlacklisted(ctx, drv.ID.Hex(), order.Customer.PickupAddress)
			if err != nil {
				d.logger.Error().Err(err).Str("driver_id", drv.ID.Hex()).Str("pickup_address", order.Customer.PickupAddress).Msg("檢查司機黑名單失敗，跳過此司機")
				continue
			}
			if isBlacklisted {
				d.logger.Debug().Str("short_id", order.ShortID).Str("driver_name", drv.Name).Str("car_plate", drv.CarPlate).Msg("調度中心司機在Redis黑名單中，已過濾")
				continue // 忽略黑名單中的司機
			}
		}

		// 忽略非閒置狀態的司機（防止重複派單）
		if drv.Status != model.DriverStatusIdle {
			d.logger.Debug().
				Str("short_id", order.ShortID).
				Str("driver_name", drv.Name).
				Str("car_plate", drv.CarPlate).
				Str("current_status", string(drv.Status)).
				Msg("🚫 調度中心司機非閒置狀態，跳過派單")
			continue
		}

		// 新增：檢查司機是否正在被其他訂單通知（Redis鎖檢查）
		if d.EventManager != nil {
			// 嘗試短暫獲取司機通知鎖來檢查可用性
			lockAcquired, releaseLock, lockErr := d.EventManager.AcquireDriverNotificationLock(ctx, drv.ID.Hex(), "test", d.dispatcherID, 1*time.Second)
			if lockErr != nil {
				d.logger.Warn().Err(lockErr).
					Str("short_id", order.ShortID).
					Str("driver_name", drv.Name).
					Str("car_plate", drv.CarPlate).
					Msg("檢查司機通知鎖失敗，跳過此司機")
				continue
			}
			if !lockAcquired {
				d.logger.Debug().
					Str("short_id", order.ShortID).
					Str("driver_name", drv.Name).
					Str("car_plate", drv.CarPlate).
					Msg("🔒 調度中心司機正在被其他訂單通知中，跳過派單")
				continue
			}
			// 立即釋放測試鎖
			if releaseLock != nil {
				releaseLock()
			}
		}

		// 新增：檢查司機是否有預約訂單時間衝突（只針對即時單）
		if order.Type == model.OrderTypeInstant && drv.HasSchedule && drv.ScheduledTime != nil {
			// 檢查司機的預約時間是否與當前即時單時間衝突（1小時內）
			now := time.Now()
			scheduledTime := *drv.ScheduledTime
			timeDiff := scheduledTime.Sub(now)

			// 如果預約時間在1小時內，跳過此司機
			if timeDiff > 0 && timeDiff <= 1*time.Hour {
				d.logger.Debug().
					Str("short_id", order.ShortID).
					Str("driver_name", drv.Name).
					Str("car_plate", drv.CarPlate).
					Time("scheduled_time", scheduledTime).
					Dur("time_until_scheduled", timeDiff).
					Msg("🕒 調度中心司機在1小時內有預約訂單，跳過即時單派送")
				continue
			}
		}

		// 檢查車隊匹配規則：
		// 1. WEI 車隊訂單只能派給 WEI 車隊司機
		// 2. RSK, KD 車隊訂單可以互相派送，但不能派給 WEI 車隊司機
		orderFleet := order.Fleet
		driverFleet := drv.Fleet

		if orderFleet == model.FleetTypeWEI && driverFleet != model.FleetTypeWEI {
			continue
		}

		if (orderFleet == model.FleetTypeRSK || orderFleet == model.FleetTypeKD) && driverFleet == model.FleetTypeWEI {
			continue
		}

		// 檢查司機的拒絕列表，如果該司機的拒絕列表包含此訂單的車隊，則跳過
		if drv.RejectList != nil {
			shouldSkip := false
			for _, rejectedFleet := range drv.RejectList {
				if rejectedFleet == string(order.Fleet) {
					shouldSkip = true
					d.logger.Debug().Str("short_id", order.ShortID).Str("driver_name", drv.Name).Str("car_plate", drv.CarPlate).Str("fleet", string(order.Fleet)).Msg("調度中心司機在拒絕列表中包含車隊，已過濾")
					break
				}
			}
			if shouldSkip {
				continue
			}
		}

		lat, _ := strconv.ParseFloat(drv.Lat, 64)
		lng, _ := strconv.ParseFloat(drv.Lng, 64)
		if lat != 0 && lng != 0 {
			validDrivers = append(validDrivers, drv)
		}
	}

	if len(validDrivers) == 0 {
		infra.AddEvent(findSpan, "no_valid_drivers")
		infra.SetAttributes(findSpan,
			infra.AttrInt("valid_drivers.count", 0),
			infra.AttrString("find.result", "no_valid_drivers"),
		)
		d.logger.Warn().
			Str("short_id", order.ShortID).
			Str("trace_id", span.SpanContext().TraceID().String()).
			Str("span_id", findSpan.SpanContext().SpanID().String()).
			Msg("調度中心無可派發的有效司機(可能全部被拒絕或無位置)")
		return nil, nil, nil, nil
	}

	// 添加有效司機數量事件
	infra.AddEvent(findSpan, "valid_drivers_filtered",
		infra.AttrInt("valid_count", len(validDrivers)),
		infra.AttrInt("initial_count", len(drivers)),
	)

	// 3. Haversine 算法找出距離最近的候選人
	lat, lng := 0.0, 0.0
	if order.Customer.PickupLat != nil {
		lat, _ = strconv.ParseFloat(*order.Customer.PickupLat, 64)
	}
	if order.Customer.PickupLng != nil {
		lng, _ = strconv.ParseFloat(*order.Customer.PickupLng, 64)
	}

	var driverDists []driverModels.DriverWithDist
	for _, drv := range validDrivers {
		driverLat, _ := strconv.ParseFloat(drv.Lat, 64)
		driverLng, _ := strconv.ParseFloat(drv.Lng, 64)
		dist := utils.Haversine(lat, lng, driverLat, driverLng)

		// 過濾掉直線距離超過15公里的司機（如果啟用），但 WEI 車隊訂單無視距離限制
		if enableMaxDriverDistance && dist > maxDriverDistanceKm && order.Fleet != model.FleetTypeWEI {
			d.logger.Debug().Str("short_id", order.ShortID).Str("driver_name", drv.Name).Str("car_plate", drv.CarPlate).Float64("distance_km", dist).Msg("調度中心司機直線距離超過15公里，已過濾")
			continue
		}

		driverDists = append(driverDists, driverModels.DriverWithDist{Driver: drv, Dist: dist})
	}
	sort.Slice(driverDists, func(i, j int) bool { return driverDists[i].Dist < driverDists[j].Dist })

	// 建立直線距離排名列表
	var driverHaversineInfos []string
	for i, d := range driverDists {
		if i >= 10 { // 只顯示前10名避免log太長
			break
		}
		driverHaversineInfos = append(driverHaversineInfos, fmt.Sprintf("排名%d: %s %s 直線距離:%.1f公里",
			i+1, d.Driver.Name, d.Driver.CarPlate, d.Dist))
	}
	d.logger.Info().
		Str("short_id", order.ShortID).
		Str("ori_text", order.OriText).
		Int("driver_count", len(driverDists)).
		Str("driver_rankings", strings.Join(driverHaversineInfos, " | ")).
		Msg("[調度中心-{short_id}]: ({ori_text}) 直線距離排序完成，共{driver_count}名司機，排名: {driver_rankings}")

	if len(driverDists) > haversineCandidatesCount {
		driverDists = driverDists[:haversineCandidatesCount]
	}

	// 4. 使用 CrawlerService 計算真實路徑，找出最佳候選人
	var origins []string
	var driverPlatesForApi []string
	for _, d := range driverDists {
		origins = append(origins, fmt.Sprintf("%s,%s", d.Driver.Lat, d.Driver.Lng))
		driverPlatesForApi = append(driverPlatesForApi, d.Driver.CarPlate)
	}
	var driverNames []string
	for _, d := range driverDists {
		driverNames = append(driverNames, d.Driver.Name+" "+d.Driver.CarPlate)
	}
	//d.logger.Info().Str("short_id", order.ShortID).Str("driver_names", strings.Join(driverNames, ", ")).Msg("調度中心計算真實路徑")

	passengerCoord := fmt.Sprintf("%f,%f", lat, lng)

	// 使用 CrawlerService 的 DirectionsMatrixInverse 方法
	var routes []service.RouteInfo
	var crawlerErr error

	infra.AddEvent(findSpan, "calculating_real_distances",
		infra.AttrInt("haversine_candidates", len(driverDists)),
	)

	if d.CrawlerSvc != nil {
		routes, crawlerErr = d.CrawlerSvc.DirectionsMatrixInverse(findCtx, origins, passengerCoord)
	} else {
		infra.AddEvent(findSpan, "crawler_service_not_initialized")
		d.logger.Error().
			Str("trace_id", span.SpanContext().TraceID().String()).
			Str("span_id", findSpan.SpanContext().SpanID().String()).
			Msg("CrawlerService 未初始化")
		crawlerErr = fmt.Errorf("CrawlerService 未初始化")
	}

	if crawlerErr != nil {
		infra.RecordError(findSpan, crawlerErr, "Crawler distance calculation failed",
			infra.AttrString("error", crawlerErr.Error()),
		)
		d.logger.Error().Err(crawlerErr).
			Str("trace_id", span.SpanContext().TraceID().String()).
			Str("span_id", findSpan.SpanContext().SpanID().String()).
			Msg("Crawler 距離計算失敗")
		return nil, nil, nil, crawlerErr
	}

	// 檢查結果數量是否匹配
	if len(routes) != len(origins) {
		d.logger.Warn().Int("routes_count", len(routes)).Int("drivers_count", len(origins)).Msg("Crawler返回的路線數量與司機數量不匹配")
	}

	// 建立結果結構，包含司機索引、距離和時間
	type DriverRoute struct {
		Index       int     // 司機在原陣列中的索引
		Distance    float64 // 距離（公里）
		Duration    int     // 時間（分鐘）
		DistanceStr string  // 距離字串
		DurationStr string  // 時間字串
	}

	var driverRoutes []DriverRoute
	for i, route := range routes {
		if i < len(origins) { // 確保索引不超出範圍
			// 過濾掉真實距離預估時間超過20分鐘的司機（如果啟用），但 WEI 車隊訂單無視時間限制
			if enableMaxEstimatedTimeMins && route.TimeInMinutes > maxEstimatedTimeMins && order.Fleet != model.FleetTypeWEI {
				driverInfo := "未知司機"
				if i < len(driverDists) {
					driverInfo = fmt.Sprintf("%s %s", driverDists[i].Driver.Name, driverDists[i].Driver.CarPlate)
				}
				d.logger.Debug().Str("short_id", order.ShortID).Str("driver_info", driverInfo).Int("estimated_mins", route.TimeInMinutes).Msg("調度中心司機真實距離預估時間超過20分鐘，已過濾")
				continue
			}

			driverRoutes = append(driverRoutes, DriverRoute{
				Index:       i,
				Distance:    route.DistanceKm,
				Duration:    route.TimeInMinutes,
				DistanceStr: route.Distance,
				DurationStr: route.Time,
			})
		}
	}

	// 根據時間排序（時間短的優先）
	sort.Slice(driverRoutes, func(i, j int) bool {
		return driverRoutes[i].Duration < driverRoutes[j].Duration
	})

	// 取前 N 名候選人
	candidateCount := googleAPICandidatesCount
	if len(driverRoutes) < candidateCount {
		candidateCount = len(driverRoutes)
	}

	// 提取結果
	var idxs []int
	var distances []string
	var durationMins []int
	for i := 0; i < candidateCount; i++ {
		route := driverRoutes[i]
		idxs = append(idxs, route.Index)
		distances = append(distances, route.DistanceStr)
		durationMins = append(durationMins, route.Duration)
	}

	var finalCandidates []*model.DriverInfo
	var driverInfos []string
	for n, i := range idxs {
		finalCandidates = append(finalCandidates, driverDists[i].Driver)
		driverInfos = append(driverInfos, fmt.Sprintf("排名%d: %s %s 預估時間:%d分鐘 距離:%s",
			n+1, driverDists[i].Driver.Name, driverDists[i].Driver.CarPlate, durationMins[n], distances[n]))
	}
	d.logger.Warn().
		Str("short_id", order.ShortID).
		Str("ori_text", order.OriText).
		Int("candidate_count", len(finalCandidates)).
		Str("final_rankings", strings.Join(driverInfos, " | ")).
		Msg("[調度中心-{short_id}]: ({ori_text}) 真實距離司機排名完成，共{candidate_count}名司機，依序發送，排名: {final_rankings}")
	// 添加最終結果事件
	infra.AddEvent(findSpan, "final_candidates_selected",
		infra.AttrInt("final_count", len(finalCandidates)),
		infra.AttrInt("google_api_limit", googleAPICandidatesCount),
	)

	// 設置成功的 span 屬性
	infra.MarkSuccess(findSpan,
		infra.AttrInt("candidates.initial", len(drivers)),
		infra.AttrInt("candidates.valid", len(validDrivers)),
		infra.AttrInt("candidates.haversine", len(driverDists)),
		infra.AttrInt("candidates.final", len(finalCandidates)),
	)

	return finalCandidates, distances, durationMins, nil
}

// buildOrderInfo 建立要發送給司機的訂單資訊
func (d *Dispatcher) buildOrderInfo(ctx context.Context, order *model.Order) (*model.OrderInfo, error) {
	// 預估從上車點到目的地的時間與距離
	var estPickToDestDist string
	var estPickToDestMins int
	if order.Customer.DestAddress != "" && order.Customer.PickupLat != nil && order.Customer.PickupLng != nil && order.Customer.DestLat != nil && order.Customer.DestLng != nil {
		pickupCoord := fmt.Sprintf("%s,%s", *order.Customer.PickupLat, *order.Customer.PickupLng)
		destCoord := fmt.Sprintf("%s,%s", *order.Customer.DestLat, *order.Customer.DestLng)

		// 使用 CrawlerService 計算距離和時間
		if d.CrawlerSvc != nil {
			routes, err := d.CrawlerSvc.GetGoogleMapsDirections(ctx, pickupCoord, destCoord)
			if err != nil {
				d.logger.Error().Err(err).Str("short_id", order.ShortID).Msg("調度中心無法計算上車點到目的地的距離")
			} else if len(routes) > 0 {
				estPickToDestDist = routes[0].Distance
				estPickToDestMins = routes[0].TimeInMinutes
				d.logger.Info().Str("short_id", order.ShortID).Str("distance", estPickToDestDist).Int("time_mins", estPickToDestMins).Msg("調度中心上車點到目的地路線計算完成")
			}
		} else {
			d.logger.Warn().Str("short_id", order.ShortID).Msg("調度中心CrawlerService未初始化，無法計算上車點到目的地的距離")
		}
	}

	orderInfo := &model.OrderInfo{
		ID:                 *order.ID,
		InputPickupAddress: order.Customer.InputPickupAddress,
		InputDestAddress:   order.Customer.InputDestAddress,
		PickupAddress:      order.Customer.PickupAddress,
		DestinationAddress: order.Customer.DestAddress,
		PickupLat:          order.Customer.PickupLat,
		PickupLng:          order.Customer.PickupLng,
		DestinationLat:     order.Customer.DestLat,
		DestinationLng:     order.Customer.DestLng,
		Remarks:            order.Customer.Remarks,
		Fleet:              order.Fleet,
		Timestamp:          *order.CreatedAt,
		EstPickToDestDist:  estPickToDestDist,
		EstPickToDestMins:  estPickToDestMins,
		OriText:            order.OriText,
		OriTextDisplay:     order.OriTextDisplay,
	}
	if estPickToDestMins > 0 {
		taipeiLocation := time.FixedZone("Asia/Taipei", 8*3600)
		orderInfo.EstPickToDestTime = time.Now().Add(time.Duration(estPickToDestMins) * time.Minute).In(taipeiLocation).Format("15:04:05")
	}

	return orderInfo, nil
}

// dispatchOrderToDrivers 執行訂單派送邏輯
func (d *Dispatcher) dispatchOrderToDrivers(ctx context.Context, order *model.Order, candidates []*model.DriverInfo, distances []string, durationMins []int) (bool, error) {
	// 僅使用 FCM 推送
	if d.FcmSvc != nil {
		return d.sendFcm(ctx, order, candidates, distances, durationMins)
	}

	d.logger.Error().Msg("FCM服務未初始化")
	return false, fmt.Errorf("FCM服務未初始化")
}

// sendFcm 使用FCM服務依序推送訂單給司機
func (d *Dispatcher) sendFcm(ctx context.Context, order *model.Order, candidates []*model.DriverInfo, distances []string, durationMins []int) (bool, error) {
	// 獲取當前 span
	span := trace.SpanFromContext(ctx)

	// 創建業務 span
	fcmCtx, fcmSpan := infra.StartSpan(ctx, "dispatcher_send_fcm",
		infra.AttrOperation("send_fcm"),
		infra.AttrOrderID(order.ID.Hex()),
		infra.AttrString("order.short_id", order.ShortID),
		infra.AttrInt("candidates_count", len(candidates)),
	)
	defer fcmSpan.End()

	// 添加FCM推送開始事件
	infra.AddEvent(fcmSpan, "fcm_dispatch_started",
		infra.AttrOrderID(order.ID.Hex()),
		infra.AttrInt("candidates_count", len(candidates)),
	)
	var candidatePlates []string
	var candidateInfos []string
	for _, c := range candidates {
		candidatePlates = append(candidatePlates, c.CarPlate)
		candidateInfos = append(candidateInfos, utils.GetDriverInfo(c))
	}

	// 1. 建立基礎訂單資訊
	infra.AddEvent(fcmSpan, "building_order_info")
	baseOrderInfo, err := d.buildOrderInfo(fcmCtx, order)
	if err != nil {
		infra.RecordError(fcmSpan, err, "Build order info failed",
			infra.AttrString("error", err.Error()),
		)
		d.logger.Error().Err(err).
			Str("trace_id", span.SpanContext().TraceID().String()).
			Str("span_id", fcmSpan.SpanContext().SpanID().String()).
			Msg("無法建立訂單資訊")
		return false, err
	}

	// 2. 檢查訂單初始狀態
	infra.AddEvent(fcmSpan, "checking_initial_order_status")
	currentOrder, err := d.OrderSvc.GetOrderByID(fcmCtx, order.ID.Hex())
	if err != nil {
		infra.RecordError(fcmSpan, err, "Initial order status check failed",
			infra.AttrString("error", err.Error()),
		)
		d.logger.Error().Err(err).
			Str("short_id", order.ShortID).
			Str("order_id", order.ID.Hex()).
			Str("trace_id", span.SpanContext().TraceID().String()).
			Str("span_id", fcmSpan.SpanContext().SpanID().String()).
			Msg("調度中心推送前檢查訂單狀態失敗")
		return false, err
	}
	if currentOrder.Status != model.OrderStatusWaiting {
		if currentOrder.Status == model.OrderStatusCancelled {
			infra.AddEvent(fcmSpan, "order_already_cancelled",
				infra.AttrString("status", string(currentOrder.Status)),
			)
			infra.SetAttributes(fcmSpan,
				infra.AttrString("fcm.result", "order_cancelled"),
			)
			d.logger.Info().
				Str("short_id", order.ShortID).
				Str("order_id", order.ID.Hex()).
				Str("status", string(currentOrder.Status)).
				Str("trace_id", span.SpanContext().TraceID().String()).
				Str("span_id", fcmSpan.SpanContext().SpanID().String()).
				Msg("調度中心訂單在推送開始前已被調度取消")
			return false, nil // 訂單已被取消，停止派單流程
		}
		infra.AddEvent(fcmSpan, "order_already_processed",
			infra.AttrString("status", string(currentOrder.Status)),
		)
		infra.MarkSuccess(fcmSpan,
			infra.AttrString("fcm.result", "order_already_processed"),
		)
		d.logger.Info().
			Str("short_id", order.ShortID).
			Str("order_id", order.ID.Hex()).
			Str("status", string(currentOrder.Status)).
			Str("trace_id", span.SpanContext().TraceID().String()).
			Str("span_id", fcmSpan.SpanContext().SpanID().String()).
			Msg("調度中心訂單在推送開始前已被處理")
		return true, nil
	}

	// 3. 依序發送FCM通知給每位司機
	infra.AddEvent(fcmSpan, "starting_sequential_notifications",
		infra.AttrInt("total_candidates", len(candidates)),
	)
	for i, driver := range candidates {
		// 添加當前司機通知事件
		infra.AddEvent(fcmSpan, "processing_driver",
			infra.AttrInt("driver_rank", i+1),
			infra.AttrDriverID(driver.ID.Hex()),
			infra.AttrString("car_plate", driver.CarPlate),
		)

		// 再次檢查訂單狀態，如果已被其他司機接受就停止
		currentOrder, err := d.OrderSvc.GetOrderByID(fcmCtx, order.ID.Hex())
		if err != nil {
			infra.AddEvent(fcmSpan, "order_status_check_failed",
				infra.AttrInt("driver_rank", i+1),
				infra.AttrString("error", err.Error()),
			)
			d.logger.Error().Err(err).
				Str("short_id", order.ShortID).
				Str("trace_id", span.SpanContext().TraceID().String()).
				Str("span_id", fcmSpan.SpanContext().SpanID().String()).
				Msg("調度中心檢查訂單狀態失敗")
			return false, err
		}
		if currentOrder.Status != model.OrderStatusWaiting {
			if currentOrder.Status == model.OrderStatusCancelled {
				infra.AddEvent(fcmSpan, "order_cancelled_during_dispatch",
					infra.AttrInt("driver_rank", i+1),
					infra.AttrString("status", string(currentOrder.Status)),
				)
				d.logger.Info().
					Str("short_id", order.ShortID).
					Str("status", string(currentOrder.Status)).
					Str("trace_id", span.SpanContext().TraceID().String()).
					Str("span_id", fcmSpan.SpanContext().SpanID().String()).
					Msg("調度中心訂單已被調度取消，停止派單流程")
				return false, nil // 訂單已被取消，停止派單流程
			}
			infra.AddEvent(fcmSpan, "order_accepted_during_dispatch",
				infra.AttrInt("driver_rank", i+1),
				infra.AttrString("status", string(currentOrder.Status)),
			)
			infra.MarkSuccess(fcmSpan,
				infra.AttrString("fcm.result", "order_accepted_during_dispatch"),
			)
			d.logger.Info().
				Str("short_id", order.ShortID).
				Str("status", string(currentOrder.Status)).
				Str("trace_id", span.SpanContext().TraceID().String()).
				Str("span_id", fcmSpan.SpanContext().SpanID().String()).
				Msg("調度中心訂單已被司機接受")
			return true, nil
		}

		if driver.FcmToken == "" {
			infra.AddEvent(fcmSpan, "driver_no_fcm_token",
				infra.AttrInt("driver_rank", i+1),
				infra.AttrString("car_plate", driver.CarPlate),
			)
			d.logger.Debug().
				Str("short_id", order.ShortID).
				Int("rank", i+1).
				Str("car_plate", driver.CarPlate).
				Str("trace_id", span.SpanContext().TraceID().String()).
				Str("span_id", fcmSpan.SpanContext().SpanID().String()).
				Msg("調度中心司機沒有FCM Token，跳過")
			continue
		}

		// 使用新的原子性檢查並通知司機
		var driverNotificationRelease func()
		if d.EventManager != nil {
			lockTTL := sequentialCallTimeout + 5*time.Second // 比等待時間稍長
			success, reason, atomicErr := d.EventManager.AtomicNotifyDriver(ctx,
				driver.ID.Hex(),
				order.ID.Hex(),
				d.dispatcherID,
				lockTTL)

			if atomicErr != nil {
				d.logger.Error().Err(atomicErr).
					Str("short_id", order.ShortID).
					Int("rank", i+1).
					Str("car_plate", driver.CarPlate).
					Msg("原子性司機通知檢查失敗，跳過此司機")
				continue
			}

			if !success {
				d.logger.Debug().
					Str("short_id", order.ShortID).
					Int("rank", i+1).
					Str("car_plate", driver.CarPlate).
					Str("reason", reason).
					Msg("🔒 司機或訂單不可用，跳過")
				continue
			}

			// 設置釋放函數
			driverNotificationRelease = func() {
				d.EventManager.ReleaseDriverNotification(ctx, driver.ID.Hex(), order.ID.Hex(), d.dispatcherID)
			}
		}

		// 為當前司機客製化預估到達資訊
		orderInfoForDriver := *baseOrderInfo
		estPickupMins := durationMins[i]

		// 計算補時：每個司機發送間隔17秒，累積超過30秒就補時1分鐘
		cumulativeWaitSeconds := i * int(sequentialCallTimeout.Seconds()) // 當前司機的累積等待時間（秒）
		compensationMins := cumulativeWaitSeconds / 30                    // 每30秒補時1分鐘
		finalEstPickupMins := estPickupMins + compensationMins

		orderInfoForDriver.EstPickUpDist = utils.ParseDistanceToKm(distances[i])
		orderInfoForDriver.EstPickupMins = finalEstPickupMins
		// 轉換為台北時間
		taipeiLocation := time.FixedZone("Asia/Taipei", 8*3600)
		orderInfoForDriver.EstPickupTime = time.Now().Add(time.Duration(finalEstPickupMins) * time.Minute).In(taipeiLocation).Format("15:04:05")

		distanceKm := utils.ParseDistanceToKm(distances[i])
		driverInfo := utils.GetDriverInfo(driver)
		d.logger.Info().Str("short_id", order.ShortID).Str("ori_text", order.OriText).Int("rank", i+1).Str("driver_info", driverInfo).Int("original_mins", estPickupMins).Int("compensation_mins", compensationMins).Int("final_mins", finalEstPickupMins).Int("cumulative_wait_seconds", cumulativeWaitSeconds).Float64("distance_km", distanceKm).Msg("[調度中心-{short_id}]: ({ori_text}) FCM 第{rank}順位司機: {driver_info} (累計等待{cumulative_wait_seconds}秒, 原始{original_mins}分鐘+補時{compensation_mins}分鐘=最終{final_mins}分鐘 {distance_km}公里)")

		// 使用統一的推送資料模型
		pushData := orderInfoForDriver.ToOrderPushData(int(sequentialCallTimeout.Seconds()))
		pushDataMap := pushData.ToMap()

		// 添加通知類型
		pushDataMap["notify_order_type"] = string(model.NotifyTypeNewOrder)

		notification := map[string]interface{}{
			"title": fmt.Sprintf("來自%s的新訂單", string(order.Fleet)),
			"body":  fmt.Sprintf("%s，預估%d分鐘(%.1f公里)可到達客上地點。", order.OriText, finalEstPickupMins, distanceKm),
			"sound": "new_order.wav",
		}

		// 添加FCM發送事件
		infra.AddEvent(fcmSpan, "sending_fcm_notification",
			infra.AttrInt("driver_rank", i+1),
			infra.AttrDriverID(driver.ID.Hex()),
			infra.AttrString("car_plate", driver.CarPlate),
			infra.AttrInt("estimated_pickup_mins", finalEstPickupMins),
			infra.AttrFloat64("distance_km", distanceKm),
		)

		// 原子性檢查已確保司機和訂單狀態正確，直接發送 FCM

		// 同步發送FCM推送，確保時間準確
		fcmSendErr := d.FcmSvc.Send(fcmCtx, driver.FcmToken, pushDataMap, notification)

		if fcmSendErr != nil {
			infra.AddEvent(fcmSpan, "fcm_send_failed",
				infra.AttrInt("driver_rank", i+1),
				infra.AttrDriverID(driver.ID.Hex()),
				infra.AttrString("error", fcmSendErr.Error()),
			)
			d.logger.Error().Err(fcmSendErr).
				Str("short_id", order.ShortID).
				Str("car_plate", driver.CarPlate).
				Str("trace_id", span.SpanContext().TraceID().String()).
				Str("span_id", fcmSpan.SpanContext().SpanID().String()).
				Msg("調度中心推送通知發送失敗")
			// FCM 發送失敗時釋放鎖
			if driverNotificationRelease != nil {
				driverNotificationRelease()
			}
			continue
		} else {
			infra.AddEvent(fcmSpan, "fcm_sent_successfully",
				infra.AttrInt("driver_rank", i+1),
				infra.AttrDriverID(driver.ID.Hex()),
				infra.AttrString("car_plate", driver.CarPlate),
			)
			// 推送成功後立即記錄準確的發送時間到 Redis
			if d.EventManager != nil {
				pushTime := time.Now() // FCM發送成功的當下時間
				d.recordNotifyingOrder(fcmCtx, order, driver, &orderInfoForDriver, pushTime, int(sequentialCallTimeout.Seconds()))
			}
		}

		// 使用事件驅動的等待機制，並傳遞鎖釋放函數
		accepted, shouldContinue := d.waitForDriverResponseEventDriven(ctx, order, driver)

		// 等待結束後釋放司機通知鎖
		if driverNotificationRelease != nil {
			driverNotificationRelease()
		}

		if accepted {
			driverInfo := utils.GetDriverInfo(driver)
			infra.AddEvent(fcmSpan, "driver_accepted_order",
				infra.AttrInt("driver_rank", i+1),
				infra.AttrDriverID(driver.ID.Hex()),
				infra.AttrString("driver_info", driverInfo),
			)
			infra.MarkSuccess(fcmSpan,
				infra.AttrString("fcm.result", "driver_accepted"),
				infra.AttrInt("accepting_driver_rank", i+1),
				infra.AttrDriverID(driver.ID.Hex()),
			)
			d.logger.Info().
				Str("short_id", order.ShortID).
				Str("ori_text", order.OriText).
				Str("driver_info", driverInfo).
				Str("trace_id", span.SpanContext().TraceID().String()).
				Str("span_id", fcmSpan.SpanContext().SpanID().String()).
				Msg("[調度中心-{short_id}]: ({ori_text}) 🎉 司機接單成功，調度完成 司機:{driver_info}")
			return true, nil // 成功匹配
		}

		if !shouldContinue {
			d.logger.Info().
				Str("short_id", order.ShortID).
				Str("ori_text", order.OriText).
				Msg("[調度中心-{short_id}]: ({ori_text}) 🛑 調度流程被中斷，停止派單")
			break
		}

		// shouldContinue == true，繼續下一個司機
		currentDriverInfo := utils.GetDriverInfo(driver)

		// 檢查是否還有下一個司機
		if i+1 < len(candidates) {
			nextDriverInfo := utils.GetDriverInfo(candidates[i+1])
			d.logger.Debug().
				Str("short_id", order.ShortID).
				Str("ori_text", order.OriText).
				Str("current_driver_info", currentDriverInfo).
				Str("next_driver_info", nextDriverInfo).
				Msg("[調度中心-{short_id}]: ({ori_text}) 繼續派給下一個司機: {next_driver_info}")
		} else {
			d.logger.Debug().
				Str("short_id", order.ShortID).
				Str("ori_text", order.OriText).
				Str("current_driver_info", currentDriverInfo).
				Msg("[調度中心-{short_id}]: ({ori_text}) 當前司機超時，但已無更多候選司機")
		}
	}

	// 所有司機都沒有接受訂單
	infra.AddEvent(fcmSpan, "all_drivers_exhausted")
	infra.SetAttributes(fcmSpan,
		infra.AttrString("fcm.result", "no_driver_accepted"),
		infra.AttrInt("total_candidates_processed", len(candidates)),
	)
	//d.logger.Warn().Str("short_id", order.ShortID).Int("candidate_count", len(candidates)).Msg("調度中心派單失敗")
	return false, nil // 未匹配
}

func (d *Dispatcher) failOrder(ctx context.Context, orderID primitive.ObjectID, reason string) error {
	// 獲取當前 span
	span := trace.SpanFromContext(ctx)

	// 創建業務 span
	failCtx, failSpan := infra.StartSpan(ctx, "dispatcher_fail_order",
		infra.AttrOperation("fail_order"),
		infra.AttrOrderID(orderID.Hex()),
		infra.AttrString("failure_reason", reason),
	)
	defer failSpan.End()

	// 添加訂單失敗開始事件
	infra.AddEvent(failSpan, "order_failure_started",
		infra.AttrOrderID(orderID.Hex()),
		infra.AttrString("reason", reason),
	)

	// 獲取訂單資料
	order, err := d.OrderSvc.GetOrderByID(failCtx, orderID.Hex())
	if err != nil {
		infra.AddEvent(failSpan, "get_order_failed",
			infra.AttrString("error", err.Error()),
		)
		d.logger.Error().Err(err).
			Str("order_id", orderID.Hex()).
			Str("trace_id", span.SpanContext().TraceID().String()).
			Str("span_id", failSpan.SpanContext().SpanID().String()).
			Msg("調度中心獲取訂單失敗")
		// 即使獲取訂單失敗，仍然嘗試更新為失敗狀態
	}

	infra.AddEvent(failSpan, "updating_order_status_to_failed")
	if err := d.OrderSvc.FailOrder(failCtx, orderID, reason); err != nil {
		shortID := order.ShortID
		infra.RecordError(failSpan, err, "Update order status failed",
			infra.AttrString("error", err.Error()),
		)
		d.logger.Error().Err(err).
			Str("short_id", shortID).
			Str("order_id", orderID.Hex()).
			Str("trace_id", span.SpanContext().TraceID().String()).
			Str("span_id", failSpan.SpanContext().SpanID().String()).
			Msg("調度中心更新訂單為失敗狀態時出錯")
		return err
	}

	// 添加成功事件
	infra.AddEvent(failSpan, "order_failed_successfully",
		infra.AttrString("reason", reason),
	)

	// 使用 NotificationService 統一處理通知（Discord、LINE等）
	if d.NotificationService != nil {
		infra.AddEvent(failSpan, "sending_failure_notifications")
		go func() {
			if err := d.NotificationService.NotifyOrderFailed(context.Background(), orderID.Hex(), reason); err != nil {
				d.logger.Error().Err(err).
					Str("order_id", orderID.Hex()).
					Str("trace_id", span.SpanContext().TraceID().String()).
					Str("span_id", failSpan.SpanContext().SpanID().String()).
					Msg("NotificationService 處理流單通知失敗")
			}
		}()
	}

	// 設置成功的 span 屬性
	infra.MarkSuccess(failSpan,
		infra.AttrString("order.failure_reason", reason),
	)

	return nil
}

// 新增：事件驅動的司機回應等待機制
func (d *Dispatcher) waitForDriverResponseEventDriven(ctx context.Context, order *model.Order, driver *model.DriverInfo) (accepted bool, shouldContinue bool) {
	orderID := order.ID.Hex()
	driverID := driver.ID.Hex()

	// 檢查事件管理器是否可用
	if d.EventManager == nil {
		d.logger.Error().Str("short_id", order.ShortID).Msg("事件管理器未初始化，降級為傳統等待模式")
		return d.waitForDriverResponseTraditional(ctx, order, driver)
	}

	// 1. 獲取調度鎖，確保調度狀態權威
	lockTTL := sequentialCallTimeout + 10*time.Second
	lockAcquired, lockValue, releaseLock, err := d.EventManager.AcquireDispatchLock(ctx, orderID, d.dispatcherID, lockTTL)
	if err != nil {
		d.logger.Error().Err(err).Str("short_id", order.ShortID).Msg("調度鎖獲取失敗")
		return false, true
	}
	if !lockAcquired {
		d.logger.Info().Str("short_id", order.ShortID).Msg("調度鎖已被其他流程持有，訂單可能已被處理")
		return false, false // 不繼續，直接結束
	}
	defer releaseLock()

	// 2. 訂閱司機回應事件、司機狀態變更事件和訂單狀態變更事件
	orderResponseSub := d.EventManager.SubscribeOrderResponses(ctx, orderID)
	defer func() {
		if err := orderResponseSub.Close(); err != nil {
			d.logger.Error().Err(err).Msg("關閉orderResponseSub失敗")
		}
	}()

	driverStatusSub := d.EventManager.SubscribeDriverStatusChanges(ctx)
	defer func() {
		if err := driverStatusSub.Close(); err != nil {
			d.logger.Error().Err(err).Msg("關閉driverStatusSub失敗")
		}
	}()

	orderStatusSub := d.EventManager.SubscribeOrderStatusChanges(ctx)
	defer func() {
		if err := orderStatusSub.Close(); err != nil {
			d.logger.Error().Err(err).Msg("關閉orderStatusSub失敗")
		}
	}()

	// 3. 設置多重檢查機制
	statusCheckTicker := time.NewTicker(1 * time.Second) // 每1秒檢查一次訂單狀態
	defer statusCheckTicker.Stop()

	timeoutTimer := time.NewTimer(sequentialCallTimeout)
	defer timeoutTimer.Stop()

	lockExtendTicker := time.NewTicker(5 * time.Second) // 每5秒延長鎖
	defer lockExtendTicker.Stop()

	driverInfo := utils.GetDriverInfo(driver)
	d.logger.Info().
		Str("short_id", order.ShortID).
		Str("ori_text", order.OriText).
		Dur("timeout", sequentialCallTimeout).
		Str("driver_info", driverInfo).
		Msg("[調度中心-{short_id}]: ({ori_text}) 開始事件驅動等待司機回應: {driver_info}")

	for {
		select {
		case <-timeoutTimer.C:
			// 17秒超時
			d.logger.Info().
				Str("short_id", order.ShortID).
				Str("ori_text", order.OriText).
				Str("driver_info", driverInfo).
				Msg("[調度中心-{short_id}]: ({ori_text}) 司機回應超時，進行超時處理: {driver_info}")
			d.handleDriverTimeout(ctx, order, driver)
			return false, true // 繼續下一個司機

		case msg := <-orderResponseSub.Channel():
			// 收到訂單回應事件
			response, parseErr := infra.ParseDriverResponse(msg.Payload)
			if parseErr != nil {
				d.logger.Warn().Err(parseErr).
					Str("short_id", order.ShortID).
					Str("payload", msg.Payload).
					Msg("解析司機回應事件失敗")
				continue
			}

			// 驗證事件是否屬於這個訂單
			if response.OrderID != orderID {
				continue
			}

			if response.Action == infra.DriverResponseAccept {
				if response.DriverID == driverID {
					d.logger.Info().
						Str("short_id", order.ShortID).
						Str("responding_driver", response.DriverID).
						Msg("✅ 當前司機接單成功，調度完成")
					return true, false // 當前司機接單成功，停止調度
				} else {
					d.logger.Info().
						Str("short_id", order.ShortID).
						Str("current_driver", driverID).
						Str("accepting_driver", response.DriverID).
						Msg("✅ 其他司機接單成功，停止調度")
					return true, false // 其他司機接單成功，停止調度
				}

			} else if response.Action == infra.DriverResponseReject && response.DriverID == driverID {
				d.logger.Info().
					Str("short_id", order.ShortID).
					Str("car_plate", driver.CarPlate).
					Msg("❌ 司機明確拒單，立即進入下一個司機")
				return false, true // 明確拒單，立即繼續下一個
			}

		case msg := <-driverStatusSub.Channel():
			// 收到司機狀態變更事件
			statusEvent, parseErr := infra.ParseDriverStatusEvent(msg.Payload)
			if parseErr != nil {
				d.logger.Warn().Err(parseErr).
					Str("short_id", order.ShortID).
					Str("payload", msg.Payload).
					Msg("解析司機狀態事件失敗")
				continue
			}

			// 如果當前等待的司機狀態變更為非閒置，立即停止等待
			if statusEvent.DriverID == driverID && statusEvent.NewStatus != string(model.DriverStatusIdle) {
				d.logger.Info().
					Str("short_id", order.ShortID).
					Str("driver_id", driverID).
					Str("car_plate", driver.CarPlate).
					Str("old_status", statusEvent.OldStatus).
					Str("new_status", statusEvent.NewStatus).
					Str("reason", statusEvent.Reason).
					Msg("🚫 司機狀態已變更為非閒置，停止等待此司機")
				return false, true // 停止等待此司機，繼續下一個
			}

		case msg := <-orderStatusSub.Channel():
			// 收到訂單狀態變更事件
			orderEvent, parseErr := infra.ParseOrderStatusEvent(msg.Payload)
			if parseErr != nil {
				d.logger.Warn().Err(parseErr).
					Str("short_id", order.ShortID).
					Str("payload", msg.Payload).
					Msg("解析訂單狀態事件失敗")
				continue
			}

			// 如果是當前訂單的狀態變更
			if orderEvent.OrderID == orderID {
				if orderEvent.NewStatus != string(model.OrderStatusWaiting) {
					d.logger.Info().
						Str("short_id", order.ShortID).
						Str("order_id", orderID).
						Str("old_status", orderEvent.OldStatus).
						Str("new_status", orderEvent.NewStatus).
						Str("event_type", string(orderEvent.EventType)).
						Str("reason", orderEvent.Reason).
						Msg("📋 訂單狀態已變更，停止調度")

					// 根據事件類型和訂單狀態返回不同結果
					if orderEvent.EventType == infra.OrderEventAccepted {
						return true, false // 訂單已被接受，調度成功
					} else if orderEvent.NewStatus == string(model.OrderStatusCancelled) {
						d.logger.Info().
							Str("short_id", order.ShortID).
							Str("reason", orderEvent.Reason).
							Msg("🚫 訂單已被調度取消，停止派單流程")
						return false, false // 訂單已被調度取消，調度結束
					} else {
						return false, false // 訂單已失敗，調度結束
					}
				}
			}

		case <-statusCheckTicker.C:
			// 定期檢查訂單狀態，防止遺漏狀態變更
			currentOrder, statusErr := d.OrderSvc.GetOrderByID(ctx, orderID)
			if statusErr != nil {
				d.logger.Error().Err(statusErr).
					Str("short_id", order.ShortID).
					Msg("訂單狀態檢查失敗")
				continue
			}

			if currentOrder.Status != model.OrderStatusWaiting {
				if currentOrder.Status == model.OrderStatusCancelled {
					d.logger.Info().
						Str("short_id", order.ShortID).
						Str("status", string(currentOrder.Status)).
						Msg("🔄 訂單已被調度取消，停止調度")
					return false, false // 訂單已被調度取消，調度結束
				}
				d.logger.Info().
					Str("short_id", order.ShortID).
					Str("status", string(currentOrder.Status)).
					Msg("🔄 訂單狀態已變更，停止調度")
				return true, false // 訂單已被處理，停止調度
			}

		case <-lockExtendTicker.C:
			// 定期延長鎖，確保調度過程中鎖不會過期
			extendErr := d.EventManager.ExtendDispatchLock(ctx, orderID, lockValue, lockTTL)
			if extendErr != nil {
				d.logger.Error().Err(extendErr).
					Str("short_id", order.ShortID).
					Msg("⚠️ 調度鎖延長失敗，可能被其他流程搶奪")
				return false, false // 鎖失效，停止調度
			}

		case <-ctx.Done():
			d.logger.Info().
				Str("short_id", order.ShortID).
				Msg("🛑 調度上下文取消")
			return false, false
		}
	}
}

// 傳統等待模式作為降級方案
func (d *Dispatcher) waitForDriverResponseTraditional(ctx context.Context, order *model.Order, driver *model.DriverInfo) (accepted bool, shouldContinue bool) {
	d.logger.Info().
		Str("short_id", order.ShortID).
		Str("car_plate", driver.CarPlate).
		Msg("⬇️ 使用傳統等待模式")

	// 等待這位司機回應
	time.Sleep(sequentialCallTimeout)

	// 檢查司機是否接受了訂單
	finalOrder, err := d.OrderSvc.GetOrderByID(ctx, order.ID.Hex())
	if err != nil {
		d.logger.Error().Err(err).Str("short_id", order.ShortID).Msg("調度中心檢查司機回應時發生錯誤")
		return false, true // 繼續嘗試下一位司機
	}

	if finalOrder.Status != model.OrderStatusWaiting {
		if finalOrder.Status == model.OrderStatusCancelled {
			d.logger.Info().
				Str("short_id", order.ShortID).
				Str("car_plate", driver.CarPlate).
				Str("status", string(finalOrder.Status)).
				Msg("調度中心訂單已被調度取消")
			return false, false // 訂單已被調度取消，調度結束
		}
		d.logger.Info().
			Str("short_id", order.ShortID).
			Str("car_plate", driver.CarPlate).
			Str("status", string(finalOrder.Status)).
			Msg("調度中心司機已接受訂單")
		return true, false // 成功匹配
	}

	// 這位司機沒有回應，處理超時
	d.handleDriverTimeout(ctx, order, driver)
	return false, true // 繼續下一個司機
}

// 新增：統一的司機超時處理
func (d *Dispatcher) handleDriverTimeout(ctx context.Context, order *model.Order, driver *model.DriverInfo) {
	// 新增：使用 Redis 鎖防止重複拒絕記錄
	var timeoutLockRelease func()
	if d.EventManager != nil {
		lockTTL := 10 * time.Second // 超時鎖存活10秒，足夠完成超時處理
		lockAcquired, releaseLock, lockErr := d.EventManager.AcquireOrderRejectLock(ctx, order.ID.Hex(), driver.ID.Hex(), "timeout", lockTTL)

		if lockErr != nil {
			d.logger.Error().Err(lockErr).
				Str("short_id", order.ShortID).
				Str("driver_id", driver.ID.Hex()).
				Str("car_plate", driver.CarPlate).
				Msg("調度中心獲取超時拒絕鎖失敗")
			// 不阻塞主流程，繼續執行後續邏輯
		} else if !lockAcquired {
			d.logger.Info().
				Str("short_id", order.ShortID).
				Str("driver_id", driver.ID.Hex()).
				Str("driver_name", driver.Name).
				Str("car_plate", driver.CarPlate).
				Msg("🔒 調度中心超時拒絕鎖已被持有，避免重複拒絕記錄")
			return // 直接返回，避免重複拒絕
		} else {
			timeoutLockRelease = releaseLock
			d.logger.Debug().
				Str("short_id", order.ShortID).
				Str("driver_id", driver.ID.Hex()).
				Str("car_plate", driver.CarPlate).
				Msg("✅ 調度中心超時拒絕鎖獲取成功，開始處理超時邏輯")
		}
	}

	// 確保在函數結束時釋放鎖
	defer func() {
		if timeoutLockRelease != nil {
			timeoutLockRelease()
		}
	}()

	// 檢查該司機在同一個round中是否已經處理過這個訂單（接單或拒絕）
	driverIDStr := driver.ID.Hex()
	currentRounds := 1
	if order.Rounds != nil {
		currentRounds = *order.Rounds
	}

	// 重新獲取訂單最新狀態和日誌，確保檢查的是最新數據
	latestOrder, err := d.OrderSvc.GetOrderByID(ctx, order.ID.Hex())
	if err != nil {
		d.logger.Error().Err(err).
			Str("short_id", order.ShortID).
			Str("driver_id", driverIDStr).
			Msg("調度中心超時處理時獲取訂單失敗")
		return
	}

	// 檢查該司機在當前round中是否已經有任何操作記錄
	for _, logEntry := range latestOrder.Logs {
		if logEntry.DriverID == driverIDStr && logEntry.Rounds == currentRounds {
			// 如果已經接單，不處理超時
			if logEntry.Action == model.OrderLogActionDriverAccept {
				d.logger.Info().
					Str("short_id", order.ShortID).
					Str("driver_id", driverIDStr).
					Str("driver_name", driver.Name).
					Str("car_plate", driver.CarPlate).
					Int("rounds", currentRounds).
					Time("accept_time", logEntry.Timestamp).
					Msg("調度中心司機在當前round已接單，跳過超時處理")
				return
			}
			// 如果已經拒絕，不處理超時
			if logEntry.Action == model.OrderLogActionDriverReject {
				d.logger.Info().
					Str("short_id", order.ShortID).
					Str("driver_id", driverIDStr).
					Str("driver_name", driver.Name).
					Str("car_plate", driver.CarPlate).
					Int("rounds", currentRounds).
					Time("reject_time", logEntry.Timestamp).
					Msg("調度中心司機在當前round已拒單，跳過超時處理")
				return
			}
			// 如果已經超時，不重複處理
			if logEntry.Action == model.OrderLogActionDriverTimeout {
				d.logger.Info().
					Str("short_id", order.ShortID).
					Str("driver_id", driverIDStr).
					Str("driver_name", driver.Name).
					Str("car_plate", driver.CarPlate).
					Int("rounds", currentRounds).
					Time("timeout_time", logEntry.Timestamp).
					Msg("調度中心司機在當前round已超時，跳過重複處理")
				return
			}
		}
	}

	// 添加到黑名單
	if infra.AppConfig.DriverBlacklist.Enabled && d.BlacklistSvc != nil {
		if err := d.BlacklistSvc.AddDriverToBlacklist(ctx, driver.ID.Hex(), order.Customer.PickupAddress); err != nil {
			d.logger.Error().Err(err).
				Str("short_id", order.ShortID).
				Str("car_plate", driver.CarPlate).
				Msg("調度中心將未回應司機加入Redis黑名單失敗")
		}
	}

	// 添加超時日誌（使用已定義的 currentRounds）

	// 從 Redis notifying_order 獲取準確的距離時間數據，沒有則使用 0
	distanceKm := 0.0
	estPickupMins := 0

	if d.EventManager != nil {
		notifyingOrderKey := fmt.Sprintf("notifying_order:%s", driver.ID.Hex())
		cachedData, cacheErr := d.EventManager.GetCache(ctx, notifyingOrderKey)

		if cacheErr == nil && cachedData != "" {
			var redisNotifyingOrder driverModels.RedisNotifyingOrder
			if unmarshalErr := json.Unmarshal([]byte(cachedData), &redisNotifyingOrder); unmarshalErr == nil {
				if redisNotifyingOrder.OrderID == order.ID.Hex() {
					distanceKm = redisNotifyingOrder.OrderData.EstPickUpDist
					estPickupMins = redisNotifyingOrder.OrderData.EstPickupMins
				}
			}
		}
	}

	details := fmt.Sprintf("%d分鐘(%.1f公里)", estPickupMins, distanceKm)
	if err := d.OrderSvc.AddOrderLog(ctx, order.ID.Hex(), model.OrderLogActionDriverTimeout,
		string(driver.Fleet), driver.Name, driver.CarPlate, driver.ID.Hex(), details, currentRounds); err != nil {
		d.logger.Error().Err(err).
			Str("short_id", order.ShortID).
			Msg("調度中心添加司機超時日誌失敗")
	}

	// 使用 NotificationService 處理司機超時通知（SSE等）
	if d.NotificationService != nil {
		go func() {
			if err := d.NotificationService.NotifyDriverTimeout(context.Background(), order.ID.Hex(), driver, distanceKm, estPickupMins); err != nil {
				d.logger.Error().Err(err).Str("order_id", order.ID.Hex()).Msg("NotificationService 處理司機超時通知失敗")
			}
		}()
	}

	// 註解：暫時不顯示司機逾時的 Discord 回覆訊息
}

// recordNotifyingOrder 記錄正在通知的訂單到 Redis，供 check-pending-orders API 使用
func (d *Dispatcher) recordNotifyingOrder(ctx context.Context, order *model.Order, driver *model.DriverInfo, orderInfo *model.OrderInfo, pushTime time.Time, timeoutSeconds int) {
	notifyingOrderKey := fmt.Sprintf("notifying_order:%s", driver.ID.Hex())

	// 構建 notifying order 資料
	notifyingOrderData := &driverModels.NotifyingOrderData{
		Fleet:              string(order.Fleet),
		PickupAddress:      order.Customer.PickupAddress,
		InputPickupAddress: order.Customer.InputPickupAddress,
		DestinationAddress: order.Customer.DestAddress,
		InputDestAddress:   order.Customer.InputDestAddress,
		Remarks:            order.Customer.Remarks,
		Timestamp:          pushTime.Unix(),
		PickupLat:          safeStringValue(order.Customer.PickupLat),
		PickupLng:          safeStringValue(order.Customer.PickupLng),
		DestinationLat:     order.Customer.DestLat,
		DestinationLng:     order.Customer.DestLng,
		OriText:            order.OriText,
		OriTextDisplay:     order.OriTextDisplay,
		EstPickUpDist:      orderInfo.EstPickUpDist,
		EstPickupMins:      orderInfo.EstPickupMins,
		EstPickupTime:      orderInfo.EstPickupTime,
		EstPickToDestDist:  orderInfo.EstPickToDestDist,
		EstPickToDestMins:  orderInfo.EstPickToDestMins,
		EstPickToDestTime:  orderInfo.EstPickToDestTime,
		TimeoutSeconds:     timeoutSeconds,
		OrderType:          string(order.Type),
	}

	// 新增：處理預約單時間
	if order.Type == model.OrderTypeScheduled && order.ScheduledAt != nil {
		// 轉換為台北時間
		taipeiLocation := time.FixedZone("Asia/Taipei", 8*3600)
		scheduledTimeStr := order.ScheduledAt.In(taipeiLocation).Format("2006-01-02 15:04:05")
		notifyingOrderData.ScheduledTime = &scheduledTimeStr
	}

	redisNotifyingOrder := &driverModels.RedisNotifyingOrder{
		OrderID:        order.ID.Hex(),
		DriverID:       driver.ID.Hex(),
		PushTime:       pushTime.Unix(),
		TimeoutSeconds: timeoutSeconds,
		OrderData:      notifyingOrderData,
	}

	// 序列化並儲存到 Redis
	notifyingOrderJSON, err := json.Marshal(redisNotifyingOrder)
	if err != nil {
		d.logger.Error().Err(err).
			Str("order_id", order.ID.Hex()).
			Str("driver_id", driver.ID.Hex()).
			Msg("序列化 notifying order 失敗")
		return
	}

	// 設定 TTL 為 sequentialCallTimeout + 5秒緩衝
	ttl := time.Duration(timeoutSeconds+5) * time.Second
	cacheErr := d.EventManager.SetCache(ctx, notifyingOrderKey, string(notifyingOrderJSON), ttl)
	if cacheErr != nil {
		d.logger.Error().Err(cacheErr).
			Str("order_id", order.ID.Hex()).
			Str("driver_id", driver.ID.Hex()).
			Str("cache_key", notifyingOrderKey).
			Msg("記錄 notifying order 到 Redis 失敗")
	} else {
		d.logger.Debug().
			Str("order_id", order.ID.Hex()).
			Str("driver_id", driver.ID.Hex()).
			Str("cache_key", notifyingOrderKey).
			Dur("ttl", ttl).
			Msg("成功記錄 notifying order 到 Redis")
	}
}

// safeStringValue 安全地將字串指標轉換為字串值
func safeStringValue(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
