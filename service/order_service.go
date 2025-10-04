package service

import (
	"context"
	"encoding/json"
	"fmt"
	driverModels "right-backend/data-models/driver"
	orderModels "right-backend/data-models/order"
	"right-backend/infra"
	"right-backend/metrics"
	"right-backend/model"
	"right-backend/service/interfaces"
	"right-backend/utils"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"github.com/streadway/amqp"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// CurrentOrderInfo 包含當前訂單及相關狀態信息
type CurrentOrderInfo struct {
	Order        *model.Order       `json:"order"`
	OrderStatus  model.OrderStatus  `json:"order_status"`
	DriverStatus model.DriverStatus `json:"driver_status"`
}

type OrderService struct {
	logger              zerolog.Logger
	mongoDB             *infra.MongoDB
	rabbitMQ            *infra.RabbitMQ
	googleService       *GoogleMapService
	crawlerService      *CrawlerService
	driverService       *DriverService
	eventManager        *infra.RedisEventManager // 事件管理器
	fcmService          interfaces.FCMService    // FCM 推送服務
	notificationService *NotificationService     // 統一通知服務
}

func NewOrderService(logger zerolog.Logger, mongoDB *infra.MongoDB, rabbitMQ *infra.RabbitMQ, googleService *GoogleMapService, crawlerService *CrawlerService, eventManager *infra.RedisEventManager) *OrderService {
	return &OrderService{
		logger:         logger.With().Str("module", "order_service").Logger(),
		mongoDB:        mongoDB,
		rabbitMQ:       rabbitMQ,
		googleService:  googleService,
		crawlerService: crawlerService,
		eventManager:   eventManager,
	}
}

// SetDriverService 設定司機服務依賴（避免循環依賴）
func (s *OrderService) SetDriverService(driverService *DriverService) {
	s.driverService = driverService
}

// SetFCMService 設定 FCM 推送服務依賴
func (s *OrderService) SetFCMService(fcmService interfaces.FCMService) {
	s.fcmService = fcmService
}

// SetNotificationService 設定統一通知服務依賴（避免循環依賴）
func (s *OrderService) SetNotificationService(notificationService *NotificationService) {
	s.notificationService = notificationService
}

// GetDriverService 獲取司機服務實例
func (s *OrderService) GetDriverService() *DriverService {
	return s.driverService
}

// formatDriverInfoForOrder 格式化司機資訊為 "車隊|司機編號|司機名稱"
func (s *OrderService) formatDriverInfoForOrder(ctx context.Context, driverID string) string {
	if driverID == "" {
		return ""
	}

	driver, err := s.driverService.GetDriverByID(ctx, driverID)
	if err != nil || driver == nil {
		return "未知車隊|未知編號|未知司機"
	}

	return utils.GetDriverInfo(driver)
}

// GetDispatchOrdersFromInput 從輸入參數獲取調度訂單，處理所有過濾和排序邏輯
func (s *OrderService) GetDispatchOrdersFromInput(ctx context.Context, input *orderModels.GetDispatchOrdersInput) ([]*orderModels.OrderWithShortID, int64, error) {
	// 建立過濾器
	var filter *orderModels.DispatchOrdersFilter
	if input.StartDate != "" || input.EndDate != "" || input.Fleet != "" ||
		input.CustomerGroup != "" || input.Status != "" || input.PickupAddress != "" ||
		input.OrderID != "" || input.Driver != "" {

		// 處理多個狀態值 (以逗號分隔)
		var statusList []string
		if input.Status != "" {
			statusList = strings.Split(input.Status, ",")
			// 去除空白字元
			for i, status := range statusList {
				statusList[i] = strings.TrimSpace(status)
			}
		}

		filter = &orderModels.DispatchOrdersFilter{
			StartDate:     input.StartDate,
			EndDate:       input.EndDate,
			Fleet:         input.Fleet,
			CustomerGroup: input.CustomerGroup,
			Status:        statusList,
			PickupAddress: input.PickupAddress,
			OrderID:       input.OrderID,
			Driver:        input.Driver,
		}
	}

	// 解析排序參數
	var sortField, sortOrder string
	if input.Sort != "" {
		parts := strings.Split(input.Sort, ":")
		if len(parts) == 2 {
			sortField = parts[0]
			sortOrder = parts[1]
		}
	}

	// 直接執行主要邏輯，不再調用其他方法
	orders, total, err := s.GetDispatchOrdersWithFilterAndSort(ctx, input.GetPageNum(), input.GetPageSize(), filter, sortField, sortOrder)
	if err != nil {
		return nil, 0, err
	}

	ordersWithShortID := make([]*orderModels.OrderWithShortID, len(orders))
	for i, o := range orders {
		shortID := o.ShortID

		driverInfo := s.formatDriverInfoForOrder(ctx, o.Driver.AssignedDriver)

		// 獲取完整的司機詳細信息
		var driverDetail *model.DriverInfo
		if o.Driver.AssignedDriver != "" {
			driver, err := s.driverService.GetDriverByID(ctx, o.Driver.AssignedDriver)
			if err == nil && driver != nil {
				driverDetail = driver
			}
		}

		ordersWithShortID[i] = &orderModels.OrderWithShortID{
			Order:        o,
			ShortID:      shortID,
			DriverInfo:   driverInfo,
			DriverDetail: driverDetail,
		}
	}

	return ordersWithShortID, total, nil
}

func (s *OrderService) resolveAddress(ctx context.Context, addr string, options ...string) (resolvedAddr string, lat float64, lng float64, err error) {
	if addr == "" {
		return "", 0, 0, nil
	}

	// 使用 GoogleMapService.FindPlaceFromText
	if s.googleService != nil {
		candidates, googleErr := s.googleService.FindPlaceFromText(ctx, addr, options...)

		if googleErr == nil && len(candidates) > 0 {
			if len(candidates) == 1 {
				// 只有一個候選結果，直接使用
				candidate := candidates[0]
				if candidateMap, ok := candidate.(map[string]interface{}); ok {
					// 提取地址資訊
					name, _ := candidateMap["name"].(string)
					formattedAddress, _ := candidateMap["formatted_address"].(string)

					// 提取座標資訊
					var lat, lng float64
					if geometry, ok := candidateMap["geometry"].(map[string]interface{}); ok {
						if location, ok := geometry["location"].(map[string]interface{}); ok {
							if latVal, ok := location["lat"].(float64); ok {
								lat = latVal
							}
							if lngVal, ok := location["lng"].(float64); ok {
								lng = lngVal
							}
						}
					}

					// 優先使用 formatted_address，如果沒有則使用 name
					resolved := formattedAddress
					if resolved == "" {
						resolved = name
					}
					if resolved == "" {
						resolved = addr // 如果都沒有，使用原地址
					}

					//s.logger.Info().Str("original_address", addr).Str("resolved_address", resolved).Float64("lat", lat).Float64("lng", lng).Msg("地址解析成功 (Address resolved successfully)")
					return resolved, lat, lng, nil
				}
			} else {
				// 多個候選結果，返回建議列表
				s.logger.Info().Int("candidate_count", len(candidates)).Str("address", addr).Msg("找到多個地址候選項 (Found multiple address candidates)")

				// 轉換 candidates 格式，提取座標並平鋪到外層
				suggestions := make([]interface{}, len(candidates))
				for i, candidate := range candidates {
					if candidateMap, ok := candidate.(map[string]interface{}); ok {
						// 建立新的建議對象
						suggestion := make(map[string]interface{})

						// 複製基本資訊
						if placeID, ok := candidateMap["place_id"].(string); ok {
							suggestion["place_id"] = placeID
						}
						if name, ok := candidateMap["name"].(string); ok {
							suggestion["name"] = name
						}
						if formattedAddress, ok := candidateMap["formatted_address"].(string); ok {
							suggestion["formatted_address"] = formattedAddress
						}

						// 提取並平鋪座標資訊
						if geometry, ok := candidateMap["geometry"].(map[string]interface{}); ok {
							if location, ok := geometry["location"].(map[string]interface{}); ok {
								if lat, ok := location["lat"].(float64); ok {
									suggestion["lat"] = lat
								}
								if lng, ok := location["lng"].(float64); ok {
									suggestion["lng"] = lng
								}
							}
						}

						suggestions[i] = suggestion

						name, _ := candidateMap["name"].(string)
						formattedAddress, _ := candidateMap["formatted_address"].(string)
						s.logger.Debug().Int("suggestion_index", i+1).Str("name", name).Str("formatted_address", formattedAddress).Msg("地址建議項目 (Address suggestion item)")
					}
				}

				s.logger.Info().Int("suggestion_count", len(suggestions)).Msg("返回地址建議給前端 (Returning address suggestions to frontend)")

				// 構建建議地址列表文字
				var suggestionTexts []string
				for _, suggestion := range suggestions {
					if suggestionMap, ok := suggestion.(map[string]interface{}); ok {
						if formattedAddress, exists := suggestionMap["formatted_address"].(string); exists && formattedAddress != "" {
							suggestionTexts = append(suggestionTexts, formattedAddress)
						} else if name, exists := suggestionMap["name"].(string); exists && name != "" {
							suggestionTexts = append(suggestionTexts, name)
						}
					}
				}

				// 回傳建議錯誤，讓前端顯示選項
				return addr, 0, 0, &orderModels.AddressSuggestionError{
					Message:     strings.Join(suggestionTexts, " | "),
					Suggestions: suggestions,
				}
			}
		} else {
			s.logger.Warn().Str("address", addr).Err(googleErr).Msg("Google地圖服務查詢失敗 (Google Maps service query failed)")
		}
	}

	// Google 服務不可用或查詢失敗
	s.logger.Error().Str("address", addr).Msg("Google地圖服務失敗 (Google Maps service failed)")
	return addr, 0, 0, fmt.Errorf("Google地圖服務失敗 (Google Maps service failed) for address '%s'", addr)
}

func (s *OrderService) CreateOrder(ctx context.Context, order *model.Order) (*model.Order, error) {
	s.logger.Info().Str("ori_text", order.OriText).Msg("開始建立訂單")

	now := utils.NowUTC()
	id := primitive.NewObjectID()
	status := model.OrderStatusWaiting
	order.ID = &id

	// 生成訂單短ID
	order.ShortID = utils.GetOrderShortID(id.Hex())

	order.CreatedAt = &now
	order.UpdatedAt = &now
	order.Status = status
	order.Driver = model.Driver{}
	rounds := 0
	order.Rounds = &rounds
	order.HasCopy = false
	order.HasNotify = false

	// Google 地址查詢
	if order.Customer.InputPickupAddress != "" {
		resolved, lat, lng, err := s.resolveAddress(ctx, order.Customer.InputPickupAddress, string(order.Fleet), "")
		if err == nil {
			order.Customer.PickupAddress = resolved
			latStr := fmt.Sprintf("%.6f", lat)
			lngStr := fmt.Sprintf("%.6f", lng)
			order.Customer.PickupLat = &latStr
			order.Customer.PickupLng = &lngStr
		} else {
			// 檢查是否為地址建議錯誤
			if suggestionErr, ok := err.(*orderModels.AddressSuggestionError); ok {
				// 如果是建議錯誤，直接返回而不建立訂單
				return nil, suggestionErr
			}

			// 其他錯誤才建立系統失敗訂單
			failedStatus := model.OrderStatusSystemFailed
			order.Status = failedStatus
			collection := s.mongoDB.GetCollection("orders")
			_, _ = collection.InsertOne(ctx, order)
			s.logger.Error().Err(err).Msg("上車地點查詢失敗 (Pickup address query failed)")
			return order, fmt.Errorf("上車地點查詢失敗 (Pickup address query failed): %w", err)
		}
	}
	//if order.Customer.InputDestAddress != "" {
	//	resolved, lat, lng, err := s.resolveAddress(ctx, order.Customer.InputDestAddress, string(order.Fleet), "")
	//	if err == nil {
	//		order.Customer.DestAddress = resolved
	//		latStr := fmt.Sprintf("%.6f", lat)
	//		lngStr := fmt.Sprintf("%.6f", lng)
	//		order.Customer.DestLat = &latStr
	//		order.Customer.DestLng = &lngStr
	//	}
	//}

	collection := s.mongoDB.GetCollection("orders")
	_, err := collection.InsertOne(ctx, order)
	if err != nil {
		return nil, err
	}

	// 添加訂單建立日誌
	currentRounds := 0
	if order.Rounds != nil {
		currentRounds = *order.Rounds
	}
	if err := s.AddOrderLog(ctx, order.ID.Hex(), model.OrderLogActionCreated, "", "", "", "", fmt.Sprintf("訂單建立 - 車隊: %s", order.Fleet), currentRounds); err != nil {
		s.logger.Error().Err(err).Msg("添加訂單建立日誌失敗 (Failed to add order creation log)")
	}

	if err := s.publishOrderToQueue(order); err != nil {
		s.logger.Error().Err(err).Msg("推送訂單到隊列失敗 (Failed to push order to queue)")
		return nil, fmt.Errorf("推送訂單到隊列失敗 (Failed to push order to queue): %w", err)
	}

	return order, nil
}

// SimpleCreateOrder 使用文字輸入創建訂單並返回詳細結果
func (s *OrderService) SimpleCreateOrder(ctx context.Context, orderText string, fleet string, createdBy model.CreatedBy) (*model.CreateOrderResult, error) {
	startTime := time.Now()
	source := metrics.DetermineSourceFromCreatedBy(string(createdBy))
	status := metrics.StatusSuccess

	defer func() {
		duration := time.Since(startTime)
		metrics.RecordOrderOperation(metrics.OperationSimpleCreate, status, source, duration)
	}()

	line := strings.TrimSpace(orderText)
	if line == "" {
		status = metrics.StatusError
		s.logger.Warn().Msg("輸入文字為空 (Input text is empty)")
		return nil, fmt.Errorf("輸入文字為空 (Input text is empty)")
	}

	// 使用改進的 ExOriText 解析完整的文字輸入
	customerGroup, address, remarks, scheduledTime, isErrand := utils.ExOriText(line)

	// 檢查是否成功解析出客群
	if customerGroup == "" {
		status = metrics.StatusError
		s.logger.Warn().Str("input", line).Msg("無效格式 (Invalid format)")
		return nil, fmt.Errorf("無效格式：期望 'CustomerGroup / Details' (Invalid format: expected 'CustomerGroup / Details')")
	}

	order := &model.Order{
		OriText:        orderText,                     // 保存原始輸入文字
		OriTextDisplay: customerGroup + "/" + address, // 保存客群/地址部分，不含hint內容
		Hints:          remarks,                       // hints 統一設為空白
		CreatedBy:      string(createdBy),             // 設置建立者姓名
		IsErrand:       isErrand,                      // 設置跑腿標記
	}

	// 設置客群和車隊
	order.CustomerGroup = customerGroup

	// 使用傳入的 fleet 參數設置車隊，如果未指定則根據客群前綴自動判斷
	if fleet != "" {
		switch strings.ToUpper(fleet) {
		case "RSK":
			order.Fleet = model.FleetTypeRSK
		case "KD":
			order.Fleet = model.FleetTypeKD
		case "WEI":
			order.Fleet = model.FleetTypeWEI
		default:
			return nil, fmt.Errorf("無效的車隊類型: %s", fleet)
		}
	} else {
		// 如果沒有指定 fleet，則根據客群前綴自動判斷 (customerGroup已轉為大寫)
		if strings.HasPrefix(customerGroup, "R") {
			order.Fleet = model.FleetTypeRSK
		} else if strings.HasPrefix(customerGroup, "E") {
			order.Fleet = model.FleetTypeKD
		} else {
			order.Fleet = model.FleetTypeWEI
		}
	}

	// 直接使用解析出的地址，讓 CreateOrder 中的 resolveAddress 處理地址解析和快取
	processedAddress := address

	// 預設為即時單
	order.Type = model.OrderTypeInstant
	order.IsScheduled = false

	// 根據是否有 scheduledTime 決定訂單類型
	if scheduledTime != nil {
		// 檢查預約時間是否超過30分鐘
		now := utils.NowUTC()
		timeDiff := scheduledTime.Sub(now)
		minScheduleThreshold := 30 * time.Minute

		if timeDiff >= minScheduleThreshold {
			// 預約單（時間足夠）
			order.Type = model.OrderTypeScheduled
			order.ScheduledAt = scheduledTime
			order.IsScheduled = true
		}
	}

	order.Customer.InputPickupAddress = processedAddress
	order.Customer.Remarks = remarks

	// 呼叫現有的 CreateOrder 函數來處理單一訂單的創建
	createdOrder, err := s.CreateOrder(ctx, order)
	if err != nil {
		return nil, err
	}

	// 根據訂單類型設置返回結果
	var message string

	if createdOrder.IsScheduled {
		message = "預約訂單已創建"
	} else {
		message = "訂單已創建"
	}

	return &model.CreateOrderResult{
		IsScheduled: createdOrder.IsScheduled,
		Order:       createdOrder,
		Message:     message,
	}, nil
}

func (s *OrderService) publishOrderToQueue(order *model.Order) error {
	if s.rabbitMQ == nil {
		s.logger.Warn().Msg("RabbitMQ服務不可用，跳過隊列發布 (RabbitMQ service not available, skipping queue publish)")
		return nil
	}

	b, err := json.Marshal(order)
	if err != nil {
		s.logger.Error().Err(err).Msg("序列化訂單失敗 (Failed to marshal order for queue)")
		return fmt.Errorf("序列化訂單失敗 (Failed to marshal order for queue): %w", err)
	}

	// 根據訂單類型選擇不同的隊列
	var queueName infra.QueueName
	if order.Type == model.OrderTypeScheduled {
		queueName = infra.QueueNameOrdersSchedule
		s.logger.Info().
			Str("order_id", order.ID.Hex()).
			Str("short_id", order.ShortID).
			Str("queue", queueName.String()).
			Msg("預約單發送到隊列")
	} else {
		queueName = infra.QueueNameOrders
		s.logger.Debug().
			Str("order_id", order.ID.Hex()).
			Str("short_id", order.ShortID).
			Str("queue", queueName.String()).
			Msg("即時單發送到隊列")
	}

	return s.rabbitMQ.Channel.Publish(
		"", queueName.String(), false, false,
		amqp.Publishing{
			ContentType: "application/json",
			Body:        b,
		},
	)
}

func (s *OrderService) RedispatchOrder(ctx context.Context, orderID string) (*model.Order, error) {
	startTime := time.Now()
	status := metrics.StatusSuccess

	defer func() {
		duration := time.Since(startTime)
		metrics.RecordOrderOperation(metrics.OperationRedispatch, status, metrics.SourceManual, duration)
	}()

	// 1. Get the original order
	order, err := s.GetOrderByID(ctx, orderID)
	if err != nil {
		status = metrics.StatusError
		s.logger.Error().Str("order_id", orderID).Err(err).Msg("重新派單失敗，找不到訂單 (Redispatch failed, cannot find order)")
		return nil, fmt.Errorf("重新派單失敗，找不到訂單 (Redispatch failed, cannot find order): %w", err)
	}

	// 2. Reset status and clear previous dispatch information
	order.Status = model.OrderStatusWaiting
	order.Driver = model.Driver{}

	// 增加派單輪數
	if order.Rounds == nil {
		rounds := 1
		order.Rounds = &rounds
	} else {
		*order.Rounds++
	}

	// 更新建立時間為重新派單時間
	now := utils.NowUTC()
	order.CreatedAt = &now

	// 3. Save the updated order
	updatedOrder, err := s.UpdateOrder(ctx, order)
	if err != nil {
		s.logger.Error().Str("order_id", orderID).Err(err).Msg("重新派單失敗，無法更新訂單 (Redispatch failed, cannot update order)")
		return nil, fmt.Errorf("重新派單失敗，無法更新訂單 (Redispatch failed, cannot update order): %w", err)
	}

	// 4. Publish to the queue to trigger dispatcher
	if err := s.publishOrderToQueue(updatedOrder); err != nil {
		// Even if publishing fails, the order is already updated in DB.
		// A separate recovery mechanism might be needed for queue failures.
		s.logger.Error().Str("order_id", orderID).Err(err).Msg("重新派單發布失敗 (Failed to publish redispatched order)")
		s.logger.Error().Str("order_id", orderID).Err(err).Msg("訂單已更新但發布到隊列失敗 (Order updated but failed to publish to queue)")
		return updatedOrder, fmt.Errorf("訂單已更新但發布到隊列失敗 (Order updated but failed to publish to queue): %w", err)
	}

	s.logger.Info().Str("order_id", orderID).Msg("訂單重新派單成功 (Order redispatched successfully)")
	return updatedOrder, nil
}

func (s *OrderService) GetOrderByID(ctx context.Context, id string) (*model.Order, error) {
	objectID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, err
	}

	collection := s.mongoDB.GetCollection("orders")
	var order model.Order
	err = collection.FindOne(ctx, bson.M{"_id": objectID}).Decode(&order)
	if err != nil {
		return nil, err
	}

	return &order, nil
}

// GetOrderByShortID 根據 ShortID 獲取訂單
func (s *OrderService) GetOrderByShortID(ctx context.Context, shortID string) (*model.Order, error) {
	collection := s.mongoDB.GetCollection("orders")
	var order model.Order
	err := collection.FindOne(ctx, bson.M{"short_id": shortID}).Decode(&order)
	if err != nil {
		return nil, err
	}
	return &order, nil
}

// GetOrderByDiscordMessage 根據Discord消息資訊獲取訂單
func (s *OrderService) GetOrderByDiscordMessage(ctx context.Context, channelID, messageID string) (*model.Order, error) {
	collection := s.mongoDB.GetCollection("orders")

	filter := bson.M{
		"discordChannelId": channelID,
		"discordMessageId": messageID,
	}

	var order model.Order
	err := collection.FindOne(ctx, filter).Decode(&order)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, fmt.Errorf("找不到對應的訂單")
		}
		return nil, fmt.Errorf("查詢訂單失敗: %w", err)
	}

	return &order, nil
}

func (s *OrderService) GetOrderInfo(ctx context.Context, id string) (*model.OrderInfo, error) {
	order, err := s.GetOrderByID(ctx, id)
	if err != nil {
		return nil, err
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
		OriText:            order.OriText,
		OriTextDisplay:     order.OriTextDisplay,
	}

	return orderInfo, nil
}

func (s *OrderService) GetOrders(ctx context.Context, limit, skip int64) ([]*model.Order, error) {
	collection := s.mongoDB.GetCollection("orders")

	opts := options.Find()
	opts.SetSort(bson.D{primitive.E{Key: "created_at", Value: -1}})
	opts.SetLimit(limit)
	opts.SetSkip(skip)

	cursor, err := collection.Find(ctx, bson.M{}, opts)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := cursor.Close(ctx); err != nil {
			s.logger.Error().Err(err).Msg("關閉cursor失敗")
		}
	}()

	var orders []*model.Order
	if err = cursor.All(ctx, &orders); err != nil {
		return nil, err
	}

	return orders, nil
}

func (s *OrderService) GetDispatchOrders(ctx context.Context, pageNum, pageSize int) ([]*model.Order, int64, error) {
	return s.GetDispatchOrdersWithFilter(ctx, pageNum, pageSize, nil)
}

func (s *OrderService) GetDispatchOrdersWithFilter(ctx context.Context, pageNum, pageSize int, filter *orderModels.DispatchOrdersFilter) ([]*model.Order, int64, error) {
	return s.GetDispatchOrdersWithFilterAndSort(ctx, pageNum, pageSize, filter, "", "")
}

func (s *OrderService) GetDispatchOrdersWithFilterAndSort(ctx context.Context, pageNum, pageSize int, filter *orderModels.DispatchOrdersFilter, sortField, sortOrder string) ([]*model.Order, int64, error) {
	collection := s.mongoDB.GetCollection("orders")

	// 基本過濾條件：只篩選調度相關的訂單狀態
	dispatchStatuses := []model.OrderStatus{
		model.OrderStatusFailed,        // 流單
		model.OrderStatusWaiting,       // 等待接單
		model.OrderStatusEnroute,       // 前往上車點
		model.OrderStatusDriverArrived, // 司機抵達
		model.OrderStatusExecuting,     // 執行任務
	}

	matchFilter := bson.M{
		"status": bson.M{"$in": dispatchStatuses},
	}

	// 應用過濾器
	if filter != nil {
		// 日期區間過濾
		if filter.StartDate != "" || filter.EndDate != "" {
			dateFilter := bson.M{}
			if filter.StartDate != "" {
				if startTime, err := time.Parse("2006-01-02", filter.StartDate); err == nil {
					dateFilter["$gte"] = startTime
				}
			}
			if filter.EndDate != "" {
				if endTime, err := time.Parse("2006-01-02", filter.EndDate); err == nil {
					// 設置為當天的23:59:59
					endTime = endTime.Add(23*time.Hour + 59*time.Minute + 59*time.Second)
					dateFilter["$lte"] = endTime
				}
			}
			if len(dateFilter) > 0 {
				matchFilter["created_at"] = dateFilter
			}
		}

		// 車隊過濾
		if filter.Fleet != "" {
			matchFilter["fleet"] = filter.Fleet
		}

		// 客群過濾
		if filter.CustomerGroup != "" {
			matchFilter["customer_group"] = bson.M{"$regex": filter.CustomerGroup, "$options": "i"}
		}

		// 狀態過濾 (覆蓋預設的狀態過濾)
		if len(filter.Status) > 0 {
			matchFilter["status"] = bson.M{"$in": filter.Status}
		}

		// 上車地點過濾
		if filter.PickupAddress != "" {
			matchFilter["$or"] = []bson.M{
				{"customer.input_pickup_address": bson.M{"$regex": filter.PickupAddress, "$options": "i"}},
				{"customer.pickup_address": bson.M{"$regex": filter.PickupAddress, "$options": "i"}},
			}
		}

		// 訂單號碼過濾 (支援完整ID和shortId)
		if filter.OrderID != "" {
			if strings.HasPrefix(filter.OrderID, "#") {
				// shortId格式: #xxxxx，從所有訂單中找到ID結尾匹配的
				shortId := strings.TrimPrefix(filter.OrderID, "#")
				matchFilter["_id"] = bson.M{"$regex": shortId + "$"}
			} else {
				// 完整ID或部分ID匹配
				if objectID, err := primitive.ObjectIDFromHex(filter.OrderID); err == nil {
					matchFilter["_id"] = objectID
				} else {
					// 如果不是有效的ObjectID，就用正則匹配
					matchFilter["_id"] = bson.M{"$regex": filter.OrderID, "$options": "i"}
				}
			}
		}

		// 司機過濾
		if filter.Driver != "" {
			matchFilter["driver.name"] = bson.M{"$regex": filter.Driver, "$options": "i"}
		}

		// 乘客ID過濾
		if filter.PassengerID != "" {
			matchFilter["passenger_id"] = bson.M{"$regex": filter.PassengerID, "$options": "i"}
		}

	}

	// 計算總數
	total, err := collection.CountDocuments(ctx, matchFilter)
	if err != nil {
		return nil, 0, err
	}

	// 計算跳過的數量
	skip := int64((pageNum - 1) * pageSize)

	var cursor *mongo.Cursor

	// 檢查是否有自定義排序
	if sortField != "" && sortOrder != "" {
		// 使用自定義排序
		var sortDirection int
		if sortOrder == "asc" {
			sortDirection = 1
		} else {
			sortDirection = -1
		}

		// 自定義排序，但仍然以created_at作為第二排序條件（避免重複鍵）
		var sortDoc bson.D
		if sortField == "created_at" {
			// 如果自定義排序就是 created_at，就只用一個排序條件
			sortDoc = bson.D{
				{Key: "created_at", Value: sortDirection},
			}
		} else {
			// 否則使用兩個排序條件
			sortDoc = bson.D{
				{Key: sortField, Value: sortDirection},
				{Key: "created_at", Value: -1}, // 以created_at降序作為第二排序條件
			}
		}

		findOptions := options.Find()
		findOptions.SetSort(sortDoc)
		findOptions.SetLimit(int64(pageSize))
		findOptions.SetSkip(skip)

		cursor, err = collection.Find(ctx, matchFilter, findOptions)
		if err != nil {
			return nil, 0, err
		}
	} else {
		// 使用默認的狀態優先級排序
		pipeline := []bson.M{
			{"$match": matchFilter},
			{"$addFields": bson.M{
				"status_priority": bson.M{
					"$switch": bson.M{
						"branches": []bson.M{
							{"case": bson.M{"$eq": []interface{}{"$status", model.OrderStatusFailed}}, "then": 1},        // 流單優先級最高
							{"case": bson.M{"$eq": []interface{}{"$status", model.OrderStatusWaiting}}, "then": 2},       // 等待接單
							{"case": bson.M{"$eq": []interface{}{"$status", model.OrderStatusEnroute}}, "then": 3},       // 前往上車點
							{"case": bson.M{"$eq": []interface{}{"$status", model.OrderStatusDriverArrived}}, "then": 4}, // 司機抵達
							{"case": bson.M{"$eq": []interface{}{"$status", model.OrderStatusExecuting}}, "then": 5},     // 執行任務
						},
						"default": 6, // 其他狀態
					},
				},
			}},
			{"$sort": bson.M{
				"status_priority": 1,  // 先按狀態優先級排序
				"created_at":      -1, // 再按訂單派的時間排序（最新的在前）
			}},
			{"$skip": skip},
			{"$limit": int64(pageSize)},
			{"$project": bson.M{"status_priority": 0}}, // 移除輔助欄位
		}

		cursor, err = collection.Aggregate(ctx, pipeline)
		if err != nil {
			return nil, 0, err
		}
	}

	defer func() {
		if err := cursor.Close(ctx); err != nil {
			s.logger.Error().Err(err).Msg("關閉cursor失敗")
		}
	}()

	var orders []*model.Order
	if err = cursor.All(ctx, &orders); err != nil {
		return nil, 0, err
	}

	return orders, total, nil
}

func (s *OrderService) UpdateOrderStatus(ctx context.Context, id string, status model.OrderStatus) (*model.Order, error) {
	objectID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, err
	}

	collection := s.mongoDB.GetCollection("orders")
	filter := bson.M{"_id": objectID}
	update := bson.M{
		"$set": bson.M{
			"status":     status,
			"updated_at": utils.NowUTC(),
		},
	}
	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)

	var updatedOrder model.Order
	err = collection.FindOneAndUpdate(ctx, filter, update, opts).Decode(&updatedOrder)
	if err != nil {
		return nil, err
	}

	return &updatedOrder, nil
}

// UpdateOrderStatusAndCertificate 更新訂單狀態、證明照片和拍照狀態，但不觸發事件（避免重複通知）
func (s *OrderService) UpdateOrderStatusAndCertificate(ctx context.Context, id string, status model.OrderStatus, certURL string, isPhotoTaken ...bool) (*model.Order, error) {
	objectID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, err
	}

	collection := s.mongoDB.GetCollection("orders")
	filter := bson.M{"_id": objectID}
	updateFields := bson.M{
		"status":                 status,
		"pickup_certificate_url": certURL,
		"updated_at":             utils.NowUTC(),
	}

	// 如果提供了 isPhotoTaken 參數，則更新該狀態
	if len(isPhotoTaken) > 0 {
		updateFields["is_photo_taken"] = isPhotoTaken[0]
	}

	update := bson.M{"$set": updateFields}
	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)

	var updatedOrder model.Order
	err = collection.FindOneAndUpdate(ctx, filter, update, opts).Decode(&updatedOrder)
	if err != nil {
		return nil, err
	}

	return &updatedOrder, nil
}

// UpdateOrderStatusWithPickupTime 更新訂單狀態並設置客人上車時間，但不觸發事件（避免重複通知）
func (s *OrderService) UpdateOrderStatusWithPickupTime(ctx context.Context, id string, status model.OrderStatus, hasMeterJump bool, requestTime time.Time) (*model.Order, error) {
	objectID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, err
	}

	collection := s.mongoDB.GetCollection("orders")
	filter := bson.M{"_id": objectID}
	update := bson.M{
		"$set": bson.M{
			"status":         status,
			"pickup_time":    requestTime,
			"has_meter_jump": hasMeterJump,
			"updated_at":     requestTime,
		},
	}
	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)

	var updatedOrder model.Order
	err = collection.FindOneAndUpdate(ctx, filter, update, opts).Decode(&updatedOrder)
	if err != nil {
		return nil, err
	}

	// 注意：此方法不觸發 Discord/LINE 事件，避免重複通知
	s.logger.Info().
		Str("order_id", id).
		Str("status", string(status)).
		Bool("has_meter_jump", hasMeterJump).
		Msg("訂單狀態和上車時間已更新（無事件觸發）")

	return &updatedOrder, nil
}

// SetArrivalTime 設置訂單的到達時間和司機到達偏差時間
func (s *OrderService) SetArrivalTime(ctx context.Context, orderID string, arrivalTime time.Time, deviationSecs *int) error {
	objectID, err := primitive.ObjectIDFromHex(orderID)
	if err != nil {
		return err
	}

	collection := s.mongoDB.GetCollection("orders")
	filter := bson.M{"_id": objectID}

	updateFields := bson.M{
		"arrival_time": arrivalTime,
		"updated_at":   arrivalTime,
	}

	// 如果提供了偏差時間，一起更新
	if deviationSecs != nil {
		updateFields["driver.arrival_deviation_secs"] = *deviationSecs
	}

	update := bson.M{"$set": updateFields}

	result, err := collection.UpdateOne(ctx, filter, update)
	if err != nil {
		s.logger.Error().Str("order_id", orderID).Err(err).Msg("設置到達時間失敗")
		return err
	}

	if result.MatchedCount == 0 {
		return fmt.Errorf("訂單不存在")
	}

	if deviationSecs != nil {
		s.logger.Info().Str("order_id", orderID).Time("arrival_time", arrivalTime).Int("deviation_secs", *deviationSecs).Msg("已設置訂單到達時間和偏差時間")
	} else {
		s.logger.Info().Str("order_id", orderID).Time("arrival_time", arrivalTime).Msg("已設置訂單到達時間")
	}
	return nil
}

// CheckAndNotifyDriverLateness 檢查司機是否遲到並發送 Discord 通知
func (s *OrderService) CheckAndNotifyDriverLateness(ctx context.Context, orderID string, driverName, carPlate string) error {
	order, err := s.GetOrderByID(ctx, orderID)
	if err != nil {
		return fmt.Errorf("獲取訂單失敗: %w", err)
	}

	// 檢查是否有遲到記錄
	if order.Driver.ArrivalDeviationSecs != nil && *order.Driver.ArrivalDeviationSecs > 0 {
		lateSecs := *order.Driver.ArrivalDeviationSecs
		lateMinutes := lateSecs / 60
		remainingSecs := lateSecs % 60

		// 格式化遲到時間顯示
		var latenessText string
		if lateMinutes > 0 && remainingSecs > 0 {
			latenessText = fmt.Sprintf("%d分%d秒", lateMinutes, remainingSecs)
		} else if lateMinutes > 0 {
			latenessText = fmt.Sprintf("%d分鐘", lateMinutes)
		} else {
			latenessText = fmt.Sprintf("%d秒", lateSecs)
		}

		// 司機遲到通知應由 NotificationService 統一處理
		// 這裡只記錄遲到事實，具體通知交由調用方處理
		s.logger.Info().
			Str("order_id", orderID).
			Str("driver_name", driverName).
			Str("car_plate", carPlate).
			Int("late_secs", lateSecs).
			Str("lateness_text", latenessText).
			Msg("檢測到司機遲到")
	}

	return nil
}

// UpdateOrderPickupCertificateURL 只更新訂單的抵達證明照片URL，不改變訂單狀態
func (s *OrderService) UpdateOrderPickupCertificateURL(ctx context.Context, id string, certURL string) error {
	objectID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return err
	}

	collection := s.mongoDB.GetCollection("orders")
	filter := bson.M{"_id": objectID}
	update := bson.M{
		"$set": bson.M{
			"pickup_certificate_url": certURL,
			"is_photo_taken":         true,
			"updated_at":             utils.NowUTC(),
		},
	}

	_, err = collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return err
	}

	return nil
}

// sendLineImageToAllTargets 此方法已廢棄，圖片發送應由 NotificationService 統一處理
func (s *OrderService) sendLineImageToAllTargets(ctx context.Context, order *model.Order, imageURL string) {
	// 圖片發送通知應由 NotificationService 統一處理
	// 這裡只記錄圖片資訊，具體發送交由調用方處理
	s.logger.Info().
		Str("order_id", order.ID.Hex()).
		Str("image_url", imageURL).
		Int("line_targets", len(order.LineMessages)).
		Msg("訂單圖片準備就緒，等待通知發送")
}

// UpdateOrderPhotoTakenStatus 更新訂單的拍照狀態
func (s *OrderService) UpdateOrderPhotoTakenStatus(ctx context.Context, id string, isPhotoTaken bool) error {
	objectID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return err
	}

	collection := s.mongoDB.GetCollection("orders")
	filter := bson.M{"_id": objectID}
	update := bson.M{
		"$set": bson.M{
			"is_photo_taken": isPhotoTaken,
			"updated_at":     utils.NowUTC(),
		},
	}

	_, err = collection.UpdateOne(ctx, filter, update)
	return err
}

// CancelOrder 統一的訂單取消服務，處理所有通知和狀態更新
func (s *OrderService) CancelOrder(ctx context.Context, orderID string, cancelReason string, cancelledBy string) (*model.Order, error) {
	startTime := time.Now()
	status := metrics.StatusSuccess
	source := metrics.DetermineSourceFromCreatedBy(cancelledBy)

	defer func() {
		duration := time.Since(startTime)
		metrics.RecordOrderOperation(metrics.OperationCancel, status, source, duration)
	}()

	// 1. 獲取訂單資訊
	currentOrder, err := s.GetOrderByID(ctx, orderID)
	if err != nil {
		status = metrics.StatusError
		s.logger.Error().Err(err).Str("order_id", orderID).Msg("訂單不存在")
		return nil, fmt.Errorf("訂單不存在: %w", err)
	}

	// 2. 驗證訂單狀態 - 允許在 Waiting、Enroute 或 DriverArrived 狀態下取消
	if currentOrder.Status != model.OrderStatusWaiting &&
		currentOrder.Status != model.OrderStatusScheduleAccepted &&
		currentOrder.Status != model.OrderStatusEnroute &&
		currentOrder.Status != model.OrderStatusDriverArrived {
		return nil, fmt.Errorf("訂單狀態為 %s，無法取消。只有「等待接單」、「前往上車點」或「司機抵達」狀態的訂單可以取消", currentOrder.Status)
	}

	// 3. 同步更新訂單狀態為已取消並通知 dispatcher
	logReason := fmt.Sprintf("%s by %s", cancelReason, cancelledBy)
	if err := s.UpdateOrderStatusWithEvent(ctx, orderID, model.OrderStatusCancelled, logReason, "", nil); err != nil {
		s.logger.Error().Err(err).Str("order_id", orderID).Msg("更新訂單狀態為取消失敗")
		return nil, fmt.Errorf("更新訂單狀態失敗: %w", err)
	}

	// 獲取更新後的訂單
	updatedOrder, err := s.GetOrderByID(ctx, orderID)
	if err != nil {
		return nil, fmt.Errorf("獲取更新後訂單失敗: %w", err)
	}

	// 4. 異步處理所有後續操作
	go func() {
		// 記錄 order log
		if logErr := s.AddOrderLog(context.Background(), orderID, model.OrderLogActionDispatchCancel, "", "", "", "", logReason, 0); logErr != nil {
			s.logger.Error().Err(logErr).
				Str("order_id", orderID).
				Str("reason", logReason).
				Msg("異步記錄取消訂單日誌失敗")
		}

		// 記錄取消訂單到 Redis 供司機查詢
		if updatedOrder.Driver.AssignedDriver != "" {
			if err := s.RecordCancelingOrder(context.Background(), updatedOrder, logReason, 30); err != nil {
				s.logger.Error().Err(err).
					Str("order_id", orderID).
					Str("driver_id", updatedOrder.Driver.AssignedDriver).
					Msg("記錄取消中訂單到 Redis 失敗")
			}
		}

		// 重置司機狀態為閒置
		if currentOrder.Driver.AssignedDriver != "" && s.driverService != nil {
			driverID := currentOrder.Driver.AssignedDriver
			if resetErr := s.driverService.UpdateDriverStatusType(context.Background(), driverID, model.DriverStatusIdle, "訂單取消"); resetErr != nil {
				s.logger.Error().Err(resetErr).
					Str("driver_id", driverID).
					Str("car_plate", currentOrder.Driver.CarNo).
					Msg("重置司機狀態失敗")
			}

			// 根據訂單類型清除對應的訂單ID
			if currentOrder.Type == model.OrderTypeScheduled {
				// 預約單：重置司機的預約單相關狀態
				if resetErr := s.driverService.ResetDriverScheduledOrder(context.Background(), driverID); resetErr != nil {
					s.logger.Error().Err(resetErr).
						Str("driver_id", driverID).
						Str("order_id", orderID).
						Msg("重置司機預約單狀態失敗")
				} else {
					s.logger.Info().
						Str("driver_id", driverID).
						Str("order_id", orderID).
						Msg("已重置司機預約單狀態")
				}
			} else {
				// 即時單：清除 CurrentOrderId
				if resetErr := s.driverService.UpdateDriverCurrentOrderId(context.Background(), driverID, ""); resetErr != nil {
					s.logger.Error().Err(resetErr).
						Str("driver_id", driverID).
						Str("order_id", orderID).
						Msg("清除司機CurrentOrderId失敗")
				} else {
					s.logger.Info().
						Str("driver_id", driverID).
						Str("order_id", orderID).
						Msg("已清除司機CurrentOrderId")
				}
			}

			// 同步清除 Redis driver_state，讓司機可以接新單
			if s.eventManager != nil {
				if clearErr := s.eventManager.ClearDriverStateAfterComplete(context.Background(), driverID); clearErr != nil {
					s.logger.Error().Err(clearErr).
						Str("driver_id", driverID).
						Str("order_id", orderID).
						Msg("清除 Redis driver_state 失敗 (訂單取消)")
				} else {
					s.logger.Info().
						Str("driver_id", driverID).
						Str("order_id", orderID).
						Msg("✅ Redis driver_state 已清除 (訂單取消)")
				}
			}
		}

		// 發送 FCM 取消通知給司機
		if currentOrder.Driver.AssignedDriver != "" && s.fcmService != nil && s.driverService != nil {
			driverID := currentOrder.Driver.AssignedDriver
			if driver, err := s.driverService.GetDriverByID(context.Background(), driverID); err == nil && driver != nil && driver.FcmToken != "" {
				s.sendFCMCancellationNotification(context.Background(), orderID, driver, cancelledBy, cancelReason)
			}
		}

		// 統一通知處理 (Discord, LINE, SSE) - 不包含 FCM 因為已單獨處理
		if s.notificationService != nil && updatedOrder.Driver.AssignedDriver != "" {
			driver := &model.DriverInfo{
				Name:     updatedOrder.Driver.Name,
				CarPlate: updatedOrder.Driver.CarNo,
			}
			if err := s.notificationService.NotifyOrderCancelled(context.Background(), orderID, driver); err != nil {
				s.logger.Error().Err(err).Str("order_id", orderID).Msg("NotificationService 處理取消通知失敗")
			}
		}
	}()

	s.logger.Info().
		Str("order_id", orderID).
		Str("short_id", updatedOrder.ShortID).
		Str("previous_status", string(currentOrder.Status)).
		Str("cancelled_by", cancelledBy).
		Msg("訂單已成功取消")

	return updatedOrder, nil
}

func (s *OrderService) UpdateOrder(ctx context.Context, order *model.Order) (*model.Order, error) {
	if order.ID == nil {
		s.logger.Error().Msg("訂單ID為空 (Order ID is empty)")
		return nil, fmt.Errorf("訂單ID為空 (Order ID is empty)")
	}
	collection := s.mongoDB.GetCollection("orders")
	filter := bson.M{"_id": *order.ID}
	now := utils.NowUTC()
	order.UpdatedAt = &now
	_, err := collection.ReplaceOne(ctx, filter, order)
	if err != nil {
		return nil, err
	}
	return order, nil
}

func (s *OrderService) UpdateDispatchOrderStatus(ctx context.Context, id string, hasCopy *bool, hasNotify *bool) (*model.Order, error) {
	objectID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		s.logger.Error().Str("order_id", id).Err(err).Msg("無效的訂單ID格式 (Invalid order ID format)")
		return nil, fmt.Errorf("無效的訂單ID格式 (Invalid order ID format): %w", err)
	}

	collection := s.mongoDB.GetCollection("orders")
	filter := bson.M{"_id": objectID}

	updateFields := bson.M{
		"updated_at": utils.NowUTC(),
	}

	if hasCopy != nil {
		updateFields["has_copy"] = *hasCopy
	}
	if hasNotify != nil {
		updateFields["has_notify"] = *hasNotify
	}

	update := bson.M{"$set": updateFields}

	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)
	var updatedOrder model.Order
	err = collection.FindOneAndUpdate(ctx, filter, update, opts).Decode(&updatedOrder)
	if err != nil {
		s.logger.Error().Str("order_id", id).Err(err).Msg("更新調度訂單狀態失敗 (Failed to update dispatch order status)")
		return nil, fmt.Errorf("更新調度訂單狀態失敗 (Failed to update dispatch order status): %w", err)
	}

	return &updatedOrder, nil
}

func (s *OrderService) AcceptOrderAction(ctx context.Context, orderID string, driver model.Driver, matchedStatus model.OrderStatus, acceptanceTime *time.Time) (bool, error) {
	objectID, err := primitive.ObjectIDFromHex(orderID)
	if err != nil {
		return false, err
	}

	// 直接執行原子性更新（CAS操作），不做額外查詢
	collection := s.mongoDB.GetCollection("orders")
	filter := bson.M{"_id": objectID, "status": model.OrderStatusWaiting}
	update := bson.M{
		"$set": bson.M{
			"driver":          driver,
			"status":          matchedStatus,
			"updated_at":      utils.NowUTC(),
			"acceptance_time": acceptanceTime,
		},
	}

	result, err := collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return false, err
	}

	return result.ModifiedCount > 0, nil
}

// AcceptScheduledOrderWithCondition 專用於預約單接受的原子操作
func (s *OrderService) AcceptScheduledOrderWithCondition(ctx context.Context, orderID string, driver model.Driver, matchedStatus model.OrderStatus, acceptanceTime *time.Time) (bool, error) {
	objectID, err := primitive.ObjectIDFromHex(orderID)
	if err != nil {
		return false, err
	}

	// 預約單接受，分配司機並設置為已接受狀態
	collection := s.mongoDB.GetCollection("orders")
	filter := bson.M{"_id": objectID, "status": model.OrderStatusWaiting}
	update := bson.M{
		"$set": bson.M{
			"driver":     driver,
			"status":     model.OrderStatusScheduleAccepted, // 設置為預約單已接受狀態
			"updated_at": utils.NowUTC(),
		},
	}

	result, err := collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return false, err
	}

	return result.ModifiedCount > 0, nil
}

// ActivateScheduledOrderWithCondition 專用於預約單激活的原子操作
func (s *OrderService) ActivateScheduledOrderWithCondition(ctx context.Context, orderID string, driver model.Driver, matchedStatus model.OrderStatus, activateTime *time.Time) (bool, error) {
	objectID, err := primitive.ObjectIDFromHex(orderID)
	if err != nil {
		return false, err
	}

	// 預約單激活後設置為前往上車點狀態
	collection := s.mongoDB.GetCollection("orders")
	filter := bson.M{"_id": objectID, "status": model.OrderStatusScheduleAccepted}
	update := bson.M{
		"$set": bson.M{
			"driver":          driver,
			"status":          model.OrderStatusEnroute, // 激活後設置為前往上車點狀態
			"updated_at":      utils.NowUTC(),
			"activate_time":   activateTime,
			"acceptance_time": activateTime, // 激活時同時設置接單時間
		},
	}

	result, err := collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return false, err
	}

	return result.ModifiedCount > 0, nil
}

// GetAllAssignedScheduledOrders 獲取所有已分配的預約單（用於Discord查詢）
func (s *OrderService) GetAllAssignedScheduledOrders(ctx context.Context) ([]*model.Order, error) {
	collection := s.mongoDB.GetCollection("orders")

	// 查詢條件：預約單類型且已分配司機且狀態為已接受或前往上車點
	filter := bson.M{
		"type":                   model.OrderTypeScheduled,
		"driver.assigned_driver": bson.M{"$ne": ""},
		"status": bson.M{
			"$in": []model.OrderStatus{
				model.OrderStatusScheduleAccepted, // 預約單已接受
				model.OrderStatusEnroute,          // 前往上車點（已激活的預約單）
				model.OrderStatusDriverArrived,    // 司機抵達（預約單進行中）
				model.OrderStatusExecuting,        // 執行任務（預約單進行中）
			},
		},
	}

	// 按預約時間排序
	opts := options.Find().SetSort(bson.D{{"scheduled_at", 1}})

	cursor, err := collection.Find(ctx, filter, opts)
	if err != nil {
		s.logger.Error().Err(err).Msg("查詢所有已分配預約單資料庫錯誤")
		return nil, err
	}
	defer func() {
		if err := cursor.Close(ctx); err != nil {
			s.logger.Error().Err(err).Msg("關閉cursor失敗")
		}
	}()

	var orders []*model.Order
	if err := cursor.All(ctx, &orders); err != nil {
		s.logger.Error().Err(err).Msg("解析已分配預約單資料失敗")
		return nil, err
	}

	return orders, nil
}

// GetUnassignedScheduledOrders 獲取所有未分配的預約單（用於Discord查詢）
func (s *OrderService) GetUnassignedScheduledOrders(ctx context.Context) ([]*model.Order, error) {
	collection := s.mongoDB.GetCollection("orders")

	// 查詢條件：預約單類型且未分配司機且狀態為等待接單
	filter := bson.M{
		"type": model.OrderTypeScheduled,
		"$or": []bson.M{
			{"driver.assigned_driver": ""},
			{"driver.assigned_driver": bson.M{"$exists": false}},
		},
		"status": model.OrderStatusWaiting, // 等待接單
	}

	// 按預約時間排序
	opts := options.Find().SetSort(bson.D{{"scheduled_at", 1}})

	cursor, err := collection.Find(ctx, filter, opts)
	if err != nil {
		s.logger.Error().Err(err).Msg("查詢所有未分配預約單資料庫錯誤")
		return nil, err
	}
	defer func() {
		if err := cursor.Close(ctx); err != nil {
			s.logger.Error().Err(err).Msg("關閉cursor失敗")
		}
	}()

	var orders []*model.Order
	if err := cursor.All(ctx, &orders); err != nil {
		s.logger.Error().Err(err).Msg("解析未分配預約單資料失敗")
		return nil, err
	}

	return orders, nil
}

// 新增：統一的訂單狀態更新方法(帶事件發佈)
func (s *OrderService) UpdateOrderStatusWithEvent(ctx context.Context, orderID string, newStatus model.OrderStatus, reason string, driverID string, details map[string]interface{}) error {
	objectID, err := primitive.ObjectIDFromHex(orderID)
	if err != nil {
		s.logger.Error().Str("order_id", orderID).Err(err).Msg("無效的訂單ID格式")
		return err
	}

	// 首先獲取訂單當前狀態
	collection := s.mongoDB.GetCollection("orders")
	var currentOrder model.Order
	err = collection.FindOne(ctx, bson.M{"_id": objectID}).Decode(&currentOrder)
	if err != nil {
		s.logger.Error().
			Str("order_id", orderID).
			Err(err).
			Msg("獲取訂單當前狀態失敗")
		return err
	}

	oldStatus := currentOrder.Status

	// 如果狀態沒有變化，直接返回
	if oldStatus == newStatus {
		s.logger.Debug().
			Str("order_id", orderID).
			Str("status", string(newStatus)).
			Msg("訂單狀態無變化，跳過更新")
		return nil
	}

	// 更新訂單狀態
	filter := bson.M{"_id": objectID}
	update := bson.M{
		"$set": bson.M{
			"status":     newStatus,
			"updated_at": utils.NowUTC(),
		},
	}

	_, err = collection.UpdateOne(ctx, filter, update)
	if err != nil {
		s.logger.Error().
			Str("order_id", orderID).
			Str("old_status", string(oldStatus)).
			Str("new_status", string(newStatus)).
			Err(err).
			Msg("更新訂單狀態失敗")
		return err
	}

	// 發布訂單狀態變更事件
	if s.eventManager != nil {
		var eventType infra.OrderEventType
		switch newStatus {
		case model.OrderStatusEnroute:
			eventType = infra.OrderEventAccepted
		case model.OrderStatusFailed:
			eventType = infra.OrderEventFailed
		case model.OrderStatusCompleted:
			eventType = infra.OrderEventCompleted
		case model.OrderStatusCancelled:
			eventType = infra.OrderEventCancelled
		default:
			eventType = infra.OrderEventStatusChange
		}

		statusEvent := &infra.OrderStatusEvent{
			OrderID:   orderID,
			OldStatus: string(oldStatus),
			NewStatus: string(newStatus),
			DriverID:  driverID,
			Timestamp: utils.NowUTC(),
			Reason:    reason,
			EventType: eventType,
			Details:   details,
		}

		if publishErr := s.eventManager.PublishOrderStatusEvent(ctx, statusEvent); publishErr != nil {
			s.logger.Error().Err(publishErr).
				Str("order_id", orderID).
				Str("old_status", string(oldStatus)).
				Str("new_status", string(newStatus)).
				Msg("發送訂單狀態變更事件失敗")
			// 不影響主流程，繼續執行
		} else {
			s.logger.Info().
				Str("order_id", orderID).
				Str("old_status", string(oldStatus)).
				Str("new_status", string(newStatus)).
				Str("event_type", string(eventType)).
				Str("reason", reason).
				Msg("訂單狀態變更事件已發布")
		}
	} else {
		s.logger.Warn().
			Str("order_id", orderID).
			Msg("事件管理器未初始化，無法發送訂單狀態變更通知")
	}

	return nil
}

// MatchOrder is called when a driver accepts an order via WebSocket.
func (s *OrderService) MatchOrder(ctx context.Context, orderID primitive.ObjectID, driverID string, estPickupMins int, estPickupTime string, adjustMins *int) error {
	orders := s.mongoDB.GetCollection("orders")
	drivers := s.mongoDB.GetCollection("drivers")

	// 1. 取得司機資訊
	driverObjID, err := primitive.ObjectIDFromHex(driverID)
	if err != nil {
		s.logger.Error().Str("driver_id", driverID).Err(err).Msg("無效的司機ID格式 (Invalid driver ID format)")
		return fmt.Errorf("無效的司機ID格式 (Invalid driver ID format): %w", err)
	}
	var driver model.DriverInfo
	if err := drivers.FindOne(ctx, bson.M{"_id": driverObjID}).Decode(&driver); err != nil {
		s.logger.Error().Str("driver_id", driverID).Err(err).Msg("找不到司機 (Driver not found)")
		return fmt.Errorf("找不到司機 (Driver not found): %w", err)
	}

	// 2. 使用原子操作更新訂單，確保只有一位司機可以成功接單
	now := utils.NowUTC()
	update := bson.M{
		"$set": bson.M{
			"status": model.OrderStatusEnroute,
			"driver": bson.M{
				"assigned_driver": driver.ID.Hex(),
				"name":            driver.Name,
				"car_no":          driver.CarPlate,
				"line_user_id":    driver.LineUID,
				"est_pickup_mins": estPickupMins,
				"est_pickup_time": estPickupTime,
				"adjust_mins":     adjustMins,
			},
			"updated_at": &now,
		},
	}

	// 條件：訂單必須是 "等待接單" 狀態
	filter := bson.M{
		"_id":    orderID,
		"status": model.OrderStatusWaiting,
	}

	result, err := orders.UpdateOne(ctx, filter, update)
	if err != nil {
		s.logger.Error().Str("order_id", orderID.Hex()).Err(err).Msg("資料庫更新失敗 (Database update failed)")
		return fmt.Errorf("資料庫更新失敗 (Database update failed): %w", err)
	}

	if result.MatchedCount == 0 {
		s.logger.Warn().Str("order_id", orderID.Hex()).Msg("訂單已被接走或取消 (Order already taken or cancelled)")
		return fmt.Errorf("訂單已被接走或取消 (Order already taken or cancelled)")
	}

	s.logger.Info().Str("order_id", orderID.Hex()).Str("driver_id", driverID).Msg("訂單匹配司機成功 (Order matched with driver)")
	return nil
}

// FailOrder marks an order as failed, usually when no driver accepts it.
func (s *OrderService) FailOrder(ctx context.Context, orderID primitive.ObjectID, reason string) error {
	//s.logger.Info().Str("order_id", orderID.Hex()).Str("reason", reason).Msg("嘗試將訂單設為流單 (Attempting to fail order)")
	collection := s.mongoDB.GetCollection("orders")

	// Atomically find an order that is in "Waiting" status and update it to "Failed".
	// This prevents a race condition where a driver accepts the order just as the dispatcher tries to fail it.
	filter := bson.M{
		"_id":    orderID,
		"status": model.OrderStatusWaiting,
	}

	update := bson.M{
		"$set": bson.M{
			"status":     model.OrderStatusFailed,
			"updated_at": utils.NowUTC(),
		},
	}

	result, err := collection.UpdateOne(ctx, filter, update)
	if err != nil {
		s.logger.Error().Str("order_id", orderID.Hex()).Err(err).Msg("執行流單更新失敗 (Failed to execute update for failing order)")
		return err
	}

	if result.MatchedCount == 0 {
		s.logger.Info().Str("order_id", orderID.Hex()).Msg("訂單未能設為流單，可能已被接單或取消 (Order was not failed, likely already accepted or cancelled)")
		// Returning nil because this is not a system error. The order was already in a final state.
		return nil
	}

	// 成功更新後，發布訂單失敗事件
	if s.eventManager != nil {
		details := map[string]interface{}{
			"failure_reason": reason,
		}

		statusEvent := &infra.OrderStatusEvent{
			OrderID:   orderID.Hex(),
			OldStatus: string(model.OrderStatusWaiting),
			NewStatus: string(model.OrderStatusFailed),
			DriverID:  "",
			Timestamp: utils.NowUTC(),
			Reason:    reason,
			EventType: infra.OrderEventFailed,
			Details:   details,
		}

		if publishErr := s.eventManager.PublishOrderStatusEvent(ctx, statusEvent); publishErr != nil {
			s.logger.Error().Err(publishErr).
				Str("order_id", orderID.Hex()).
				Str("reason", reason).
				Msg("發送訂單失敗事件失敗")
			// 不影響主流程，訂單已經成功標記為失敗
		}
	}

	//s.logger.Info().Str("order_id", orderID.Hex()).Msg("訂單成功設為流單 (Order successfully failed)")
	return nil
}

// UpdateOrderTripDetails 更新訂單中從上車點到目的地的行程預估資訊
func (s *OrderService) UpdateOrderTripDetails(ctx context.Context, id string, dist string, mins int, timeStr string) error {
	objectID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return err
	}

	collection := s.mongoDB.GetCollection("orders")
	filter := bson.M{"_id": objectID}
	update := bson.M{
		"$set": bson.M{
			"customer.est_pick_to_dest_dist": &dist,
			"customer.est_pick_to_dest_mins": &mins,
			"customer.est_pick_to_dest_time": &timeStr,
			"updated_at":                     utils.NowUTC(),
		},
	}

	_, err = collection.UpdateOne(ctx, filter, update)
	return err
}

func (s *OrderService) GetOrdersByDriverID(ctx context.Context, driverID string, pageNum, pageSize int) ([]*model.Order, int64, error) {
	collection := s.mongoDB.GetCollection("orders")
	filter := bson.M{"driver.assigned_driver": driverID}

	total, err := collection.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, err
	}

	findOptions := options.Find()
	findOptions.SetSort(bson.D{primitive.E{Key: "_id", Value: -1}})
	findOptions.SetLimit(int64(pageSize))
	findOptions.SetSkip(int64((pageNum - 1) * pageSize))

	cursor, err := collection.Find(ctx, filter, findOptions)
	if err != nil {
		return nil, 0, err
	}
	defer func() {
		if err := cursor.Close(ctx); err != nil {
			s.logger.Error().Err(err).Msg("關閉cursor失敗")
		}
	}()

	var orders []*model.Order
	if err = cursor.All(ctx, &orders); err != nil {
		return nil, 0, err
	}

	return orders, total, nil
}

// GetActiveOrderByDriverID finds an active order (Matched or Arrived) for a specific driver.
func (s *OrderService) GetActiveOrderByDriverID(ctx context.Context, driverID string) (*model.Order, error) {
	collection := s.mongoDB.GetCollection("orders")
	var order model.Order

	// Define the statuses that are considered "active" for a driver
	activeStatuses := []model.OrderStatus{
		model.OrderStatusDriverArrived,
		model.OrderStatusExecuting,
	}

	filter := bson.M{
		"driver.assigned_driver": driverID,
		"status":                 bson.M{"$in": activeStatuses},
	}

	// Find one order that matches the criteria
	err := collection.FindOne(ctx, filter).Decode(&order)
	if err != nil {
		return nil, err // This will return mongo.ErrNoDocuments if no active order is found
	}

	return &order, nil
}

// GetCurrentOrderByDriverID 根據司機ID查找當前正在進行中的訂單，同時返回狀態信息
func (s *OrderService) GetCurrentOrderByDriverID(ctx context.Context, driverID string) (*CurrentOrderInfo, error) {
	// 步驟1：先從 driver collection 查找司機的 current_order_id
	driverObjectID, err := primitive.ObjectIDFromHex(driverID)
	if err != nil {
		s.logger.Error().Str("driver_id", driverID).Err(err).Msg("無效的司機ID格式")
		return nil, err
	}

	driverCollection := s.mongoDB.GetCollection("drivers")
	var driver model.DriverInfo

	err = driverCollection.FindOne(ctx, bson.M{"_id": driverObjectID}).Decode(&driver)
	if err != nil {
		s.logger.Warn().Str("driver_id", driverID).Err(err).Msg("找不到司機資料")
		return nil, err
	}

	// 檢查司機是否有當前訂單ID
	if driver.CurrentOrderId == nil || *driver.CurrentOrderId == "" {
		s.logger.Info().Str("driver_id", driverID).Msg("司機沒有當前進行中的訂單")
		return nil, mongo.ErrNoDocuments
	}

	// 步驟2：根據 current_order_id 反查訂單
	orderObjectID, err := primitive.ObjectIDFromHex(*driver.CurrentOrderId)
	if err != nil {
		s.logger.Error().Str("order_id", *driver.CurrentOrderId).Err(err).Msg("無效的訂單ID格式")
		return nil, err
	}

	orderCollection := s.mongoDB.GetCollection("orders")
	var order model.Order

	err = orderCollection.FindOne(ctx, bson.M{"_id": orderObjectID}).Decode(&order)
	if err != nil {
		s.logger.Warn().
			Str("driver_id", driverID).
			Str("order_id", *driver.CurrentOrderId).
			Err(err).
			Msg("根據司機的 current_order_id 找不到對應的訂單")
		return nil, err
	}

	// 步驟3：驗證訂單狀態是否為進行中狀態
	inProgressStatuses := []model.OrderStatus{
		model.OrderStatusEnroute,       // 前往上車點
		model.OrderStatusDriverArrived, // 司機抵達
		model.OrderStatusExecuting,     // 執行任務(客上)
	}

	isInProgress := false
	for _, status := range inProgressStatuses {
		if order.Status == status {
			isInProgress = true
			break
		}
	}

	if !isInProgress {
		s.logger.Warn().
			Str("driver_id", driverID).
			Str("order_id", order.ID.Hex()).
			Str("order_status", string(order.Status)).
			Msg("訂單狀態不是進行中狀態")
		return nil, mongo.ErrNoDocuments
	}

	// 返回完整的信息
	return &CurrentOrderInfo{
		Order:        &order,
		OrderStatus:  order.Status,
		DriverStatus: driver.Status,
	}, nil
}

// DeleteFailedOrder 刪除流單狀態的訂單
func (s *OrderService) DeleteFailedOrder(ctx context.Context, orderID string) error {
	objectID, err := primitive.ObjectIDFromHex(orderID)
	if err != nil {
		s.logger.Error().Str("order_id", orderID).Err(err).Msg("無效的訂單ID (Invalid order ID)")
		return fmt.Errorf("無效的訂單ID (Invalid order ID): %w", err)
	}

	collection := s.mongoDB.GetCollection("orders")

	// 只允許刪除流單狀態的訂單
	filter := bson.M{
		"_id":    objectID,
		"status": model.OrderStatusFailed, // 只能刪除流單
	}

	result, err := collection.DeleteOne(ctx, filter)
	if err != nil {
		s.logger.Error().Str("order_id", orderID).Err(err).Msg("刪除訂單失敗 (Failed to delete order)")
		return fmt.Errorf("刪除訂單失敗 (Failed to delete order): %w", err)
	}

	if result.DeletedCount == 0 {
		s.logger.Warn().Str("order_id", orderID).Msg("訂單不存在或非流單狀態 (Order not found or not in failed status)")
		return fmt.Errorf("訂單不存在或非流單狀態 (Order not found or not in failed status)")
	}

	s.logger.Info().Str("order_id", orderID).Msg("流單訂單刪除成功 (Failed order deleted successfully)")
	return nil
}

// DeleteFailedOrdersByFleet 根據車隊刪除流單狀態的訂單
func (s *OrderService) DeleteFailedOrdersByFleet(ctx context.Context, fleet model.FleetType) (int, error) {
	collection := s.mongoDB.GetCollection("orders")

	// 只刪除指定車隊的流單狀態訂單
	filter := bson.M{
		"status": model.OrderStatusFailed, // 只刪除流單
		"fleet":  fleet,                   // 指定車隊
	}

	result, err := collection.DeleteMany(ctx, filter)
	if err != nil {
		s.logger.Error().Err(err).Str("fleet", string(fleet)).Msg("批量刪除車隊流單失敗 (Failed to delete fleet failed orders)")
		return 0, fmt.Errorf("批量刪除車隊流單失敗 (Failed to delete fleet failed orders): %w", err)
	}

	deletedCount := int(result.DeletedCount)
	s.logger.Info().Int("deleted_count", deletedCount).Str("fleet", string(fleet)).Msg("批量刪除車隊流單完成 (Batch delete fleet failed orders completed)")
	return deletedCount, nil
}

// DeleteAllOrdersByFleet 刪除指定車隊的所有訂單
func (s *OrderService) DeleteAllOrdersByFleet(ctx context.Context, fleet model.FleetType) (int, error) {
	collection := s.mongoDB.GetCollection("orders")

	// 刪除指定車隊的所有訂單
	filter := bson.M{
		"fleet": fleet, // 指定車隊
	}

	result, err := collection.DeleteMany(ctx, filter)
	if err != nil {
		s.logger.Error().Err(err).Str("fleet", string(fleet)).Msg("批量刪除車隊所有訂單失敗")
		return 0, fmt.Errorf("批量刪除車隊所有訂單失敗: %w", err)
	}

	deletedCount := int(result.DeletedCount)
	s.logger.Info().Int("deleted_count", deletedCount).Str("fleet", string(fleet)).Msg("批量刪除車隊所有訂單完成")
	return deletedCount, nil
}

// AddOrderLog 添加訂單日誌記錄
func (s *OrderService) AddOrderLog(ctx context.Context, orderID string, action model.OrderLogAction, fleet, driverName, carPlate, driverID, details string, rounds int) error {
	objectID, err := primitive.ObjectIDFromHex(orderID)
	if err != nil {
		s.logger.Error().Str("order_id", orderID).Err(err).Msg("無效的訂單ID (Invalid order ID)")
		return fmt.Errorf("無效的訂單ID (Invalid order ID): %w", err)
	}

	// 生成司機資訊：車隊-司機編號-司機名稱
	driverInfo := s.formatDriverInfoForOrder(ctx, driverID)

	logEntry := model.OrderLogEntry{
		Action:     action,
		Timestamp:  utils.NowUTC(),
		Fleet:      model.FleetType(fleet),
		DriverName: driverName,
		CarPlate:   carPlate,
		DriverID:   driverID,
		DriverInfo: driverInfo,
		Details:    details,
		Rounds:     rounds,
	}

	collection := s.mongoDB.GetCollection("orders")
	filter := bson.M{"_id": objectID}
	update := bson.M{
		"$push": bson.M{"logs": logEntry},
		"$set":  bson.M{"updated_at": utils.NowUTC()},
	}

	_, err = collection.UpdateOne(ctx, filter, update)
	if err != nil {
		s.logger.Error().Str("order_id", orderID).Err(err).Msg("添加訂單日誌失敗 (Failed to add order log)")
		return fmt.Errorf("添加訂單日誌失敗 (Failed to add order log): %w", err)
	}

	//s.logger.Info().Str("order_id", orderID).Str("action", string(action)).Str("driver_name", driverName).Str("car_plate", carPlate).Msg("新增訂單日誌 (Added order log)")
	return nil
}

// RecordCancelingOrder 記錄取消中訂單到 Redis，供司機查詢
func (s *OrderService) RecordCancelingOrder(ctx context.Context, order *model.Order, cancelReason string, timeoutSeconds int) error {
	// 檢查訂單是否有分配的司機
	if order.Driver.AssignedDriver == "" {
		s.logger.Debug().Str("order_id", order.ID.Hex()).Msg("訂單沒有分配司機，無需記錄取消資訊")
		return nil
	}

	driverID := order.Driver.AssignedDriver
	cancelTime := utils.NowUTC()

	// 構建取消訂單資料
	cancelingOrderData := &driverModels.CancelingOrderData{
		Fleet:              string(order.Fleet),
		PickupAddress:      order.Customer.PickupAddress,
		InputPickupAddress: order.Customer.InputPickupAddress,
		DestinationAddress: order.Customer.DestAddress,
		InputDestAddress:   order.Customer.InputDestAddress,
		Remarks:            order.Customer.Remarks,
		CancelTime:         cancelTime.Unix(),
		PickupLat:          safeStringValue(order.Customer.PickupLat),
		PickupLng:          safeStringValue(order.Customer.PickupLng),
		DestinationLat:     order.Customer.DestLat,
		DestinationLng:     order.Customer.DestLng,
		OriText:            order.OriText,
		OriTextDisplay:     order.OriTextDisplay,
		CancelReason:       cancelReason,
		TimeoutSeconds:     timeoutSeconds,
		OrderType:          string(order.Type),
	}

	// 新增：處理預約單時間
	if order.Type == model.OrderTypeScheduled && order.ScheduledAt != nil {
		// 格式化為 UTC 時區
		scheduledTimeStr := order.ScheduledAt.Format("2006-01-02 15:04:05")
		cancelingOrderData.ScheduledTime = &scheduledTimeStr
	}

	redisCancelingOrder := &driverModels.RedisCancelingOrder{
		OrderID:        order.ID.Hex(),
		DriverID:       driverID,
		CancelTime:     cancelTime.Unix(),
		TimeoutSeconds: timeoutSeconds,
		OrderData:      cancelingOrderData,
	}

	// 序列化並儲存到 Redis
	cancelingOrderJSON, err := json.Marshal(redisCancelingOrder)
	if err != nil {
		s.logger.Error().Err(err).
			Str("order_id", order.ID.Hex()).
			Str("driver_id", driverID).
			Msg("序列化 canceling order 失敗")
		return fmt.Errorf("序列化取消中訂單資料失敗: %v", err)
	}

	// Redis key 格式: canceling_order:{driver_id}
	cancelingOrderKey := fmt.Sprintf("canceling_order:%s", driverID)

	// 設定 TTL 為指定秒數（預設30秒）
	if timeoutSeconds <= 0 {
		timeoutSeconds = 30 // 預設30秒
	}
	ttl := time.Duration(timeoutSeconds) * time.Second

	// 使用 EventManager 設置緩存
	if s.eventManager != nil {
		cacheErr := s.eventManager.SetCache(ctx, cancelingOrderKey, string(cancelingOrderJSON), ttl)
		if cacheErr != nil {
			s.logger.Error().Err(cacheErr).
				Str("order_id", order.ID.Hex()).
				Str("driver_id", driverID).
				Str("cache_key", cancelingOrderKey).
				Msg("記錄 canceling order 到 Redis 失敗")
			return fmt.Errorf("記錄取消中訂單到 Redis 失敗: %v", cacheErr)
		}

		s.logger.Info().
			Str("order_id", order.ID.Hex()).
			Str("driver_id", driverID).
			Str("cache_key", cancelingOrderKey).
			Dur("ttl", ttl).
			Str("cancel_reason", cancelReason).
			Msg("成功記錄 canceling order 到 Redis")
	} else {
		s.logger.Warn().Msg("EventManager 未初始化，無法記錄取消中訂單到 Redis")
		return fmt.Errorf("EventManager 未初始化")
	}

	return nil
}

// safeStringValue 安全地將字串指標轉換為字串值
func safeStringValue(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// GetOrderSummary 獲取訂單報表列表，支援自訂排序
func (s *OrderService) GetOrderSummary(ctx context.Context, pageNum, pageSize int, filter *orderModels.DispatchOrdersFilter, sortField, sortOrder string) ([]*model.Order, int64, error) {
	collection := s.mongoDB.GetCollection("orders")

	// 基本過濾條件
	matchFilter := bson.M{}

	// 應用過濾器
	if filter != nil {
		// 日期區間過濾
		if filter.StartDate != "" || filter.EndDate != "" {
			dateFilter := bson.M{}
			if filter.StartDate != "" {
				if startTime, err := time.Parse("2006-01-02", filter.StartDate); err == nil {
					dateFilter["$gte"] = startTime
				}
			}
			if filter.EndDate != "" {
				if endTime, err := time.Parse("2006-01-02", filter.EndDate); err == nil {
					// 設置為當天的23:59:59
					endTime = endTime.Add(23*time.Hour + 59*time.Minute + 59*time.Second)
					dateFilter["$lte"] = endTime
				}
			}
			if len(dateFilter) > 0 {
				matchFilter["created_at"] = dateFilter
			}
		}

		// 車隊過濾
		if filter.Fleet != "" {
			matchFilter["fleet"] = filter.Fleet
		}

		// 客群過濾
		if filter.CustomerGroup != "" {
			matchFilter["customer_group"] = bson.M{"$regex": filter.CustomerGroup, "$options": "i"}
		}

		// 狀態過濾
		if len(filter.Status) > 0 {
			matchFilter["status"] = bson.M{"$in": filter.Status}
		}

		// 上車地點過濾
		if filter.PickupAddress != "" {
			matchFilter["$or"] = []bson.M{
				{"customer.input_pickup_address": bson.M{"$regex": filter.PickupAddress, "$options": "i"}},
				{"customer.pickup_address": bson.M{"$regex": filter.PickupAddress, "$options": "i"}},
			}
		}

		// 訂單號碼過濾
		if filter.OrderID != "" {
			if strings.HasPrefix(filter.OrderID, "#") {
				shortId := strings.TrimPrefix(filter.OrderID, "#")
				matchFilter["_id"] = bson.M{"$regex": shortId + "$"}
			} else {
				if objectID, err := primitive.ObjectIDFromHex(filter.OrderID); err == nil {
					matchFilter["_id"] = objectID
				} else {
					matchFilter["_id"] = bson.M{"$regex": filter.OrderID, "$options": "i"}
				}
			}
		}

		// 司機過濾
		if filter.Driver != "" {
			matchFilter["driver.name"] = bson.M{"$regex": filter.Driver, "$options": "i"}
		}

		// 乘客ID過濾
		if filter.PassengerID != "" {
			matchFilter["passenger_id"] = bson.M{"$regex": filter.PassengerID, "$options": "i"}
		}

	}

	// 計算總數
	total, err := collection.CountDocuments(ctx, matchFilter)
	if err != nil {
		return nil, 0, err
	}

	// 計算跳過的數量
	skip := int64((pageNum - 1) * pageSize)

	// 設定排序
	var sortDirection int
	if sortOrder == "asc" {
		sortDirection = 1
	} else {
		sortDirection = -1
	}

	// 查詢選項
	findOptions := options.Find()
	findOptions.SetSort(bson.D{{Key: sortField, Value: sortDirection}})
	findOptions.SetLimit(int64(pageSize))
	findOptions.SetSkip(skip)

	cursor, err := collection.Find(ctx, matchFilter, findOptions)
	if err != nil {
		return nil, 0, err
	}
	defer func() {
		if err := cursor.Close(ctx); err != nil {
			s.logger.Error().Err(err).Msg("關閉cursor失敗")
		}
	}()

	var orders []*model.Order
	if err = cursor.All(ctx, &orders); err != nil {
		return nil, 0, err
	}

	return orders, total, nil
}

// UpdateOrderSummary 更新訂單報表
func (s *OrderService) UpdateOrderSummary(ctx context.Context, id string, updateData *struct {
	Type          *model.OrderType   `json:"type,omitempty"`
	Status        *model.OrderStatus `json:"status,omitempty"`
	Amount        *int               `json:"amount,omitempty"`
	AmountNote    *string            `json:"amount_note,omitempty"`
	PassengerID   *string            `json:"passenger_id,omitempty"`
	CustomerGroup *string            `json:"customer_group,omitempty"`
	Customer      *model.Customer    `json:"customer,omitempty"`
}) (*model.Order, error) {
	objectID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, err
	}

	collection := s.mongoDB.GetCollection("orders")
	filter := bson.M{"_id": objectID}

	updateFields := bson.M{
		"updated_at": utils.NowUTC(),
	}

	// 只更新有提供的欄位
	if updateData.Type != nil {
		updateFields["type"] = *updateData.Type
	}
	if updateData.Status != nil {
		updateFields["status"] = *updateData.Status
	}
	if updateData.Amount != nil {
		updateFields["amount"] = *updateData.Amount
	}
	if updateData.AmountNote != nil {
		updateFields["amount_note"] = *updateData.AmountNote
	}
	if updateData.PassengerID != nil {
		updateFields["passenger_id"] = *updateData.PassengerID
	}
	if updateData.CustomerGroup != nil {
		updateFields["customer_group"] = *updateData.CustomerGroup
	}
	if updateData.Customer != nil {
		updateFields["customer"] = *updateData.Customer
	}

	update := bson.M{"$set": updateFields}
	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)

	var updatedOrder model.Order
	err = collection.FindOneAndUpdate(ctx, filter, update, opts).Decode(&updatedOrder)
	if err != nil {
		return nil, err
	}

	return &updatedOrder, nil
}

// DeleteOrder 刪除訂單
func (s *OrderService) DeleteOrder(ctx context.Context, orderID string) error {
	objectID, err := primitive.ObjectIDFromHex(orderID)
	if err != nil {
		s.logger.Error().Str("order_id", orderID).Err(err).Msg("無效的訂單ID (Invalid order ID)")
		return fmt.Errorf("無效的訂單ID (Invalid order ID): %w", err)
	}

	collection := s.mongoDB.GetCollection("orders")
	filter := bson.M{"_id": objectID}

	result, err := collection.DeleteOne(ctx, filter)
	if err != nil {
		s.logger.Error().Str("order_id", orderID).Err(err).Msg("刪除訂單失敗 (Failed to delete order)")
		return fmt.Errorf("刪除訂單失敗 (Failed to delete order): %w", err)
	}

	if result.DeletedCount == 0 {
		s.logger.Warn().Str("order_id", orderID).Msg("訂單不存在 (Order not found)")
		return fmt.Errorf("訂單不存在 (Order not found)")
	}

	s.logger.Info().Str("order_id", orderID).Msg("訂單刪除成功 (Order deleted successfully)")
	return nil
}

// sendFCMCancellationNotification 發送統一的 FCM 取消通知給司機（支援所有平台）
func (s *OrderService) sendFCMCancellationNotification(ctx context.Context, orderID string, driver *model.DriverInfo, cancelledBy string, cancelReason string) {
	// 獲取訂單資訊以取得顯示文字
	order, err := s.GetOrderByID(ctx, orderID)
	if err != nil {
		s.logger.Error().Err(err).Str("order_id", orderID).Msg("獲取訂單資訊失敗，無法發送 FCM 通知")
		return
	}

	// 根據訂單類型決定通知類型
	var notifyType model.NotifyType
	var message string
	if order.IsScheduled {
		notifyType = model.NotifyTypeCancelScheduleOrder
		message = "預約訂單已被取消"
	} else {
		notifyType = model.NotifyTypeCancelOrder
		message = "訂單已被取消"
	}

	// 建立取消通知資料
	cancelData := map[string]interface{}{
		"notify_order_type": string(notifyType),
		"order_id":          orderID,
		"message":           message,
	}

	// 根據取消原因決定通知內容
	var bodyMessage string
	var orderType = "訂單"
	if order.IsScheduled {
		orderType = "預約訂單"
	}

	switch {
	case strings.Contains(cancelReason, "網頁"):
		bodyMessage = fmt.Sprintf("%s %s 已被網頁用戶 %s 取消", orderType, order.OriTextDisplay, cancelledBy)
	case strings.Contains(cancelReason, "LINE"):
		bodyMessage = fmt.Sprintf("%s %s 已被 LINE 用戶取消", orderType, order.OriTextDisplay)
	case strings.Contains(cancelReason, "Discord"):
		bodyMessage = fmt.Sprintf("%s %s 已被 Discord 用戶 %s 取消", orderType, order.OriTextDisplay, cancelledBy)
	default:
		bodyMessage = fmt.Sprintf("%s %s 已被 %s 取消", orderType, order.OriTextDisplay, cancelledBy)
	}

	// 決定通知標題
	var title string
	if order.IsScheduled {
		title = "預約訂單取消通知"
	} else {
		title = "訂單取消通知"
	}

	notification := map[string]interface{}{
		"title": title,
		"body":  bodyMessage,
		"sound": "cancel_order.wav",
	}

	// 發送推送通知
	if err := s.fcmService.Send(ctx, driver.FcmToken, cancelData, notification); err != nil {
		s.logger.Error().Err(err).
			Str("order_id", orderID).
			Str("driver_id", driver.ID.Hex()).
			Str("car_plate", driver.CarPlate).
			Str("cancelled_by", cancelledBy).
			Str("cancel_reason", cancelReason).
			Msg("發送統一 FCM 取消通知給司機失敗")
	} else {
		s.logger.Info().
			Str("order_id", orderID).
			Str("driver_id", driver.ID.Hex()).
			Str("car_plate", driver.CarPlate).
			Str("cancelled_by", cancelledBy).
			Str("cancel_reason", cancelReason).
			Msg("已發送統一 FCM 取消通知給司機")
	}
}

// GetScheduleOrders 獲取預約訂單列表（分頁）
// 條件：Type = OrderTypeScheduled, Driver 為空, Status = OrderStatusWaitingAccept
func (s *OrderService) GetScheduleOrders(ctx context.Context, pageNum, pageSize int) ([]*model.Order, int64, error) {
	collection := s.mongoDB.GetCollection("orders")

	// 計算昨天和今天的日期範圍
	now := utils.NowUTC()
	// UTC 時區
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	yesterdayStart := todayStart.AddDate(0, 0, -1)
	tomorrowStart := todayStart.AddDate(0, 0, 1)

	// 構建過濾條件
	filter := bson.M{
		"type":   model.OrderTypeScheduled, // 預約單
		"status": model.OrderStatusWaiting, // 只包含等待接單的訂單
		"$and": []bson.M{
			// 司機未分配條件
			{
				"$or": []bson.M{
					{"driver.assigned_driver": bson.M{"$exists": false}}, // driver.assigned_driver 不存在
					{"driver.assigned_driver": ""},                       // driver.assigned_driver 為空字串
					{"driver.assigned_driver": nil},                      // driver.assigned_driver 為 nil
				},
			},
			// 日期範圍條件：只顯示昨天和今天的預約訂單
			{
				"created_at": bson.M{
					"$gte": yesterdayStart,
					"$lt":  tomorrowStart,
				},
			},
			// 預約時間條件：只顯示預約時間大於等於當前時間的訂單（排除過期預約單）
			{
				"$or": []bson.M{
					{"scheduled_at": bson.M{"$gte": now}},      // 預約時間大於等於當前時間
					{"scheduled_at": bson.M{"$exists": false}}, // 或者沒有設定預約時間的訂單
					{"scheduled_at": nil},                      // 或者預約時間為 nil
				},
			},
		},
	}

	// 計算總數
	total, err := collection.CountDocuments(ctx, filter)
	if err != nil {
		s.logger.Error().Err(err).Msg("計算預約訂單總數失敗")
		return nil, 0, err
	}

	// 設置查詢選項
	skip := int64((pageNum - 1) * pageSize)
	opts := options.Find()
	opts.SetSkip(skip)
	opts.SetLimit(int64(pageSize))
	opts.SetSort(bson.D{
		{Key: "created_at", Value: -1},  // 按創建時間降序排列（最新到最舊）
		{Key: "scheduled_at", Value: 1}, // 次要排序按預約時間升序
	})

	// 執行查詢
	cursor, err := collection.Find(ctx, filter, opts)
	if err != nil {
		s.logger.Error().Err(err).Msg("查詢預約訂單失敗")
		return nil, 0, err
	}
	defer func() {
		if err := cursor.Close(ctx); err != nil {
			s.logger.Error().Err(err).Msg("關閉cursor失敗")
		}
	}()

	var orders []*model.Order
	if err = cursor.All(ctx, &orders); err != nil {
		s.logger.Error().Err(err).Msg("解碼預約訂單資料失敗")
		return nil, 0, err
	}

	s.logger.Info().
		Int("page", pageNum).
		Int("page_size", pageSize).
		Int64("total", total).
		Int("returned", len(orders)).
		Msg("獲取預約訂單列表成功")

	return orders, total, nil
}

// GetDriverScheduledOrdersInTimeRange 查詢司機在指定時間範圍內的預約訂單
func (s *OrderService) GetDriverScheduledOrdersInTimeRange(ctx context.Context, driverID string, startTime, endTime time.Time) ([]*model.Order, error) {
	collection := s.mongoDB.GetCollection("orders")

	// 構建過濾條件
	filter := bson.M{
		"type":                   model.OrderTypeScheduled, // 預約單
		"driver.assigned_driver": driverID,                 // 已分派給該司機
		"scheduled_at": bson.M{
			"$gte": startTime,
			"$lte": endTime,
		},
		// 只查詢非取消和非失敗的訂單
		"status": bson.M{
			"$nin": []model.OrderStatus{
				model.OrderStatusCancelled,
				model.OrderStatusFailed,
				model.OrderStatusSystemFailed,
			},
		},
	}

	cursor, err := collection.Find(ctx, filter)
	if err != nil {
		s.logger.Error().Err(err).
			Str("driver_id", driverID).
			Time("start_time", startTime).
			Time("end_time", endTime).
			Msg("查詢司機預約訂單失敗")
		return nil, err
	}
	defer func() {
		if err := cursor.Close(ctx); err != nil {
			s.logger.Error().Err(err).Msg("關閉cursor失敗")
		}
	}()

	var orders []*model.Order
	if err = cursor.All(ctx, &orders); err != nil {
		s.logger.Error().Err(err).Msg("解碼司機預約訂單資料失敗")
		return nil, err
	}

	s.logger.Info().
		Str("driver_id", driverID).
		Time("start_time", startTime).
		Time("end_time", endTime).
		Int("found_orders", len(orders)).
		Msg("查詢司機預約訂單成功")

	return orders, nil
}

// GetAvailableScheduledOrdersCount 獲取可用預約單數量
func (s *OrderService) GetAvailableScheduledOrdersCount(ctx context.Context, driverID string) (totalCount int64, availableCount int64, err error) {
	s.logger.Info().
		Str("driver_id", driverID).
		Msg("開始獲取可用預約單數量")

	collection := s.mongoDB.GetCollection("orders")

	// 獲取總預約單數量（等待接單的預約單）
	totalFilter := bson.M{
		"type":   model.OrderTypeScheduled,
		"status": model.OrderStatusWaiting,
	}

	totalCount, err = collection.CountDocuments(ctx, totalFilter)
	if err != nil {
		s.logger.Error().
			Err(err).
			Str("driver_id", driverID).
			Msg("獲取總預約單數量失敗")
		return 0, 0, fmt.Errorf("獲取總預約單數量失敗：%w", err)
	}

	// 使用現有的 driverService 依賴
	if s.driverService == nil {
		s.logger.Error().
			Str("driver_id", driverID).
			Msg("DriverService 依賴未設置")
		return 0, 0, fmt.Errorf("DriverService 依賴未設置")
	}

	driver, err := s.driverService.GetDriverByID(ctx, driverID)
	if err != nil {
		s.logger.Error().
			Err(err).
			Str("driver_id", driverID).
			Msg("獲取司機資訊失敗")
		return 0, 0, fmt.Errorf("獲取司機資訊失敗：%w", err)
	}

	// 獲取對司機可用的預約單數量（同車隊且沒有時間衝突）
	availableFilter := bson.M{
		"type":   model.OrderTypeScheduled,
		"status": model.OrderStatusWaiting,
		"fleet":  driver.Fleet, // 只顯示同車隊的預約單
	}

	// 如果司機已有預約單，需要過濾掉有時間衝突的預約單
	if driver.HasSchedule && driver.ScheduledTime != nil {
		// 設定衝突檢查範圍（前後2小時）
		conflictBuffer := 2 * time.Hour
		conflictStart := driver.ScheduledTime.Add(-conflictBuffer)
		conflictEnd := driver.ScheduledTime.Add(conflictBuffer)

		// 排除有時間衝突的預約單
		availableFilter["scheduled_at"] = bson.M{
			"$not": bson.M{
				"$gte": conflictStart,
				"$lte": conflictEnd,
			},
		}

		s.logger.Info().
			Str("driver_id", driverID).
			Time("driver_scheduled_time", *driver.ScheduledTime).
			Time("conflict_start", conflictStart).
			Time("conflict_end", conflictEnd).
			Msg("司機已有預約單，過濾衝突時間的預約單")
	}

	availableCount, err = collection.CountDocuments(ctx, availableFilter)
	if err != nil {
		s.logger.Error().
			Err(err).
			Str("driver_id", driverID).
			Msg("獲取可用預約單數量失敗")
		return 0, 0, fmt.Errorf("獲取可用預約單數量失敗：%w", err)
	}

	s.logger.Info().
		Str("driver_id", driverID).
		Str("driver_fleet", string(driver.Fleet)).
		Int64("total_count", totalCount).
		Int64("available_count", availableCount).
		Bool("driver_has_schedule", driver.HasSchedule).
		Msg("成功獲取可用預約單數量")

	return totalCount, availableCount, nil
}

// GetScheduleOrderCount 獲取當前可接預約單數量
// 參考 GetScheduleOrders 的邏輯，只返回數量
func (s *OrderService) GetScheduleOrderCount(ctx context.Context) (int64, error) {
	collection := s.mongoDB.GetCollection("orders")

	// 計算昨天和今天的日期範圍（與 GetScheduleOrders 保持一致）
	now := utils.NowUTC()
	// UTC 時區
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	yesterdayStart := todayStart.AddDate(0, 0, -1)
	tomorrowStart := todayStart.AddDate(0, 0, 1)

	// 構建過濾條件（與 GetScheduleOrders 保持一致）
	filter := bson.M{
		"type":   model.OrderTypeScheduled, // 預約單
		"status": model.OrderStatusWaiting, // 只包含等待接單的訂單
		"$and": []bson.M{
			// 司機未分配條件
			{
				"$or": []bson.M{
					{"driver.assigned_driver": bson.M{"$exists": false}}, // driver.assigned_driver 不存在
					{"driver.assigned_driver": ""},                       // driver.assigned_driver 為空字串
					{"driver.assigned_driver": nil},                      // driver.assigned_driver 為 nil
				},
			},
			// 日期範圍條件：只顯示昨天和今天的預約訂單
			{
				"created_at": bson.M{
					"$gte": yesterdayStart,
					"$lt":  tomorrowStart,
				},
			},
			// 預約時間條件：只顯示預約時間大於等於當前時間的訂單（排除過期預約單）
			{
				"$or": []bson.M{
					{"scheduled_at": bson.M{"$gte": now}},      // 預約時間大於等於當前時間
					{"scheduled_at": bson.M{"$exists": false}}, // 或者沒有設定預約時間的訂單
					{"scheduled_at": nil},                      // 或者預約時間為 nil
				},
			},
		},
	}

	// 計算總數
	count, err := collection.CountDocuments(ctx, filter)
	if err != nil {
		s.logger.Error().Err(err).Msg("計算可接預約單數量失敗")
		return 0, fmt.Errorf("計算可接預約單數量失敗：%w", err)
	}

	s.logger.Info().
		Int64("schedule_order_count", count).
		Time("yesterday_start", yesterdayStart).
		Time("tomorrow_start", tomorrowStart).
		Msg("成功獲取可接預約單數量")

	return count, nil
}

func (s *OrderService) GetScheduleOrderCountByFleet(ctx context.Context, fleet string) (int64, error) {
	collection := s.mongoDB.GetCollection("orders")
	// 計算昨天和今天的日期範圍（與 GetScheduleOrders 保持一致）
	now := utils.NowUTC()
	// UTC 時區
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	yesterdayStart := todayStart.AddDate(0, 0, -1)
	tomorrowStart := todayStart.AddDate(0, 0, 1)

	// 構建過濾條件（與 GetScheduleOrders 保持一致）
	filter := bson.M{
		"type":   model.OrderTypeScheduled, // 預約單
		"status": model.OrderStatusWaiting, // 只包含等待接單的訂單
		"$and": []bson.M{
			// 司機未分配條件
			{
				"$or": []bson.M{
					{"driver.assigned_driver": bson.M{"$exists": false}}, // driver.assigned_driver 不存在
					{"driver.assigned_driver": ""},                       // driver.assigned_driver 為空字串
					{"driver.assigned_driver": nil},                      // driver.assigned_driver 為 nil
				},
			},
			// 日期範圍條件：只顯示昨天和今天的預約訂單
			{
				"created_at": bson.M{
					"$gte": yesterdayStart,
					"$lt":  tomorrowStart,
				},
			},
			// 預約時間條件：只顯示預約時間大於等於當前時間的訂單（排除過期預約單）
			{
				"$or": []bson.M{
					{"scheduled_at": bson.M{"$gte": now}},      // 預約時間大於等於當前時間
					{"scheduled_at": bson.M{"$exists": false}}, // 或者沒有設定預約時間的訂單
					{"scheduled_at": nil},                      // 或者預約時間為 nil
				},
			},
		},
	}

	// 如果指定了車隊，添加車隊過濾條件
	if fleet != "" {
		filter["fleet"] = fleet
	}

	// 計算總數
	count, err := collection.CountDocuments(ctx, filter)
	if err != nil {
		s.logger.Error().Err(err).Str("fleet", fleet).Msg("計算可接預約單數量失敗")
		return 0, fmt.Errorf("計算可接預約單數量失敗：%w", err)
	}

	s.logger.Info().
		Int64("schedule_order_count", count).
		Str("fleet", fleet).
		Time("yesterday_start", yesterdayStart).
		Time("tomorrow_start", tomorrowStart).
		Msg("成功獲取可接預約單數量（按車隊過濾）")
	return count, nil
}

// UpdateOrderEstimatedData 更新訂單的預估距離、時間等數據
func (s *OrderService) UpdateOrderEstimatedData(ctx context.Context, orderID string, distanceKm float64, estPickupMins int, estPickupTime string) error {
	ordersColl := s.mongoDB.GetCollection("orders")

	// 轉換訂單ID
	objectID, err := primitive.ObjectIDFromHex(orderID)
	if err != nil {
		s.logger.Error().Str("order_id", orderID).Err(err).Msg("轉換訂單ID失敗")
		return fmt.Errorf("無效的訂單ID")
	}

	// 準備更新數據
	updateData := bson.M{
		"$set": bson.M{
			"driver.est_pickup_dist_km": distanceKm,
			"driver.est_pickup_mins":    estPickupMins,
			"driver.est_pickup_time":    estPickupTime,
			"updated_at":                time.Now(),
		},
	}

	// 執行更新
	result, err := ordersColl.UpdateOne(ctx, bson.M{"_id": objectID}, updateData)
	if err != nil {
		s.logger.Error().
			Str("order_id", orderID).
			Float64("distance_km", distanceKm).
			Int("est_pickup_mins", estPickupMins).
			Err(err).
			Msg("更新訂單預估數據失敗")
		return fmt.Errorf("更新訂單預估數據失敗: %w", err)
	}

	if result.MatchedCount == 0 {
		s.logger.Error().Str("order_id", orderID).Msg("找不到要更新的訂單")
		return fmt.Errorf("找不到要更新的訂單")
	}

	s.logger.Info().
		Str("order_id", orderID).
		Float64("distance_km", distanceKm).
		Int("est_pickup_mins", estPickupMins).
		Str("est_pickup_time", estPickupTime).
		Int64("modified_count", result.ModifiedCount).
		Msg("✅ 成功更新訂單預估數據")

	return nil
}

// GetFailedOrders 獲取流單列表，支援按司機位置排序
func (s *OrderService) GetFailedOrders(ctx context.Context, driver *model.DriverInfo, limit int) ([]*orderModels.FailedOrderWithDistance, int, error) {
	collection := s.mongoDB.GetCollection("orders")

	// 構建過濾條件：狀態為流單且類型不是預約單
	filter := bson.M{
		"status": model.OrderStatusFailed,
		"type":   bson.M{"$ne": model.OrderTypeScheduled}, // 不等於預約單
	}

	// 查詢總數
	total, err := collection.CountDocuments(ctx, filter)
	if err != nil {
		s.logger.Error().Err(err).Msg("統計流單總數失敗")
		return nil, 0, fmt.Errorf("統計流單總數失敗: %w", err)
	}

	// 查詢訂單
	var orders []*model.Order
	cursor, err := collection.Find(ctx, filter, options.Find().SetSort(bson.D{{"created_at", -1}}))
	if err != nil {
		s.logger.Error().Err(err).Msg("查詢流單失敗")
		return nil, 0, fmt.Errorf("查詢流單失敗: %w", err)
	}
	defer cursor.Close(ctx)

	if err = cursor.All(ctx, &orders); err != nil {
		s.logger.Error().Err(err).Msg("解析流單數據失敗")
		return nil, 0, fmt.Errorf("解析流單數據失敗: %w", err)
	}

	// 轉換為帶距離信息的結構
	result := make([]*orderModels.FailedOrderWithDistance, 0, len(orders))

	// 解析司機的經緯度
	var driverLat, driverLng float64
	var hasDriverLocation bool
	if driver != nil && driver.Lat != "" && driver.Lng != "" {
		if lat, err1 := strconv.ParseFloat(driver.Lat, 64); err1 == nil {
			if lng, err2 := strconv.ParseFloat(driver.Lng, 64); err2 == nil {
				driverLat, driverLng = lat, lng
				hasDriverLocation = true
			}
		}
	}

	for _, order := range orders {
		failedOrder := &orderModels.FailedOrderWithDistance{
			Order: order,
		}

		// 如果有司機位置，計算距離
		if hasDriverLocation && order.Customer.PickupLat != nil && order.Customer.PickupLng != nil {
			// 解析訂單的經緯度
			pickupLat, err1 := strconv.ParseFloat(*order.Customer.PickupLat, 64)
			pickupLng, err2 := strconv.ParseFloat(*order.Customer.PickupLng, 64)

			if err1 == nil && err2 == nil {
				// 使用 Haversine 公式計算直線距離
				distance := utils.Haversine(driverLat, driverLng, pickupLat, pickupLng)
				failedOrder.Distance = &distance
			}
		}

		result = append(result, failedOrder)
	}

	// 如果有司機位置，按距離排序
	if hasDriverLocation {
		// 使用自定義排序：有距離的在前，按距離由近到遠
		for i := 0; i < len(result)-1; i++ {
			for j := i + 1; j < len(result); j++ {
				// 如果 i 沒有距離但 j 有距離，交換
				if result[i].Distance == nil && result[j].Distance != nil {
					result[i], result[j] = result[j], result[i]
				} else if result[i].Distance != nil && result[j].Distance != nil {
					// 兩者都有距離，按距離排序
					if *result[i].Distance > *result[j].Distance {
						result[i], result[j] = result[j], result[i]
					}
				}
			}
		}
	}

	// 限制返回數量
	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}

	s.logger.Info().
		Int("total", int(total)).
		Int("returned", len(result)).
		Bool("has_driver_location", hasDriverLocation).
		Interface("driver_info", map[string]interface{}{
			"driver_id": func() string {
				if driver != nil {
					return driver.ID.Hex()
				}
				return ""
			}(),
			"has_location": hasDriverLocation,
		}).
		Msg("成功獲取流單列表")

	return result, int(total), nil
}
