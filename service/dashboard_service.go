package service

import (
	"context"
	"fmt"
	"right-backend/data-models/common"
	"right-backend/data-models/dashboard"
	"right-backend/infra"
	"right-backend/model"
	"right-backend/utils"
	"sort"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type DashboardService struct {
	logger        zerolog.Logger
	mongoDB       *infra.MongoDB
	driverService *DriverService
}

func NewDashboardService(logger zerolog.Logger, mongoDB *infra.MongoDB, driverService *DriverService) *DashboardService {
	return &DashboardService{
		logger:        logger.With().Str("module", "dashboard_service").Logger(),
		mongoDB:       mongoDB,
		driverService: driverService,
	}
}

func (s *DashboardService) GetDashboardStats(ctx context.Context) (*dashboard.DashboardStats, error) {
	stats := &dashboard.DashboardStats{}

	todayStats, err := s.getTodayOrderStats(ctx)
	if err != nil {
		return nil, err
	}
	stats.TodayOrders = *todayStats

	reservationStats, err := s.getReservationOrderStats(ctx)
	if err != nil {
		return nil, err
	}
	stats.ReservationOrders = *reservationStats

	onlineDriverStats, err := s.getOnlineDriverStats(ctx)
	if err != nil {
		return nil, err
	}
	stats.OnlineDrivers = *onlineDriverStats

	idleDriverStats, err := s.getIdleDriverStats(ctx)
	if err != nil {
		return nil, err
	}
	stats.IdleDrivers = *idleDriverStats

	pickingUpCount, err := s.getPickingUpDriversCount(ctx)
	if err != nil {
		return nil, err
	}
	stats.PickingUpDrivers = pickingUpCount

	executingCount, err := s.getExecutingTaskDriversCount(ctx)
	if err != nil {
		return nil, err
	}
	stats.ExecutingTaskDrivers = executingCount

	return stats, nil
}

func (s *DashboardService) getTodayOrderStats(ctx context.Context) (*dashboard.OrderStats, error) {
	collection := s.mongoDB.GetCollection("orders")

	todayStart := time.Now().Truncate(24 * time.Hour)
	todayEnd := todayStart.Add(24 * time.Hour)

	todayFilter := bson.M{
		"created_at": bson.M{
			"$gte": todayStart,
			"$lt":  todayEnd,
		},
	}

	totalCount, err := collection.CountDocuments(ctx, todayFilter)
	if err != nil {
		return nil, err
	}

	successFilter := bson.M{
		"created_at": bson.M{
			"$gte": todayStart,
			"$lt":  todayEnd,
		},
		"status": model.OrderStatusCompleted,
	}

	successCount, err := collection.CountDocuments(ctx, successFilter)
	if err != nil {
		return nil, err
	}

	return &dashboard.OrderStats{
		SuccessCount: int(successCount),
		TotalCount:   int(totalCount),
	}, nil
}

func (s *DashboardService) getReservationOrderStats(ctx context.Context) (*dashboard.ReservationStats, error) {
	collection := s.mongoDB.GetCollection("orders")

	filter := bson.M{
		"type": model.OrderTypeScheduled,
		"status": bson.M{
			"$in": []model.OrderStatus{
				model.OrderStatusWaiting,
				model.OrderStatusEnroute,
				model.OrderStatusDriverArrived,
				model.OrderStatusExecuting,
			},
		},
	}

	count, err := collection.CountDocuments(ctx, filter)
	if err != nil {
		return nil, err
	}

	return &dashboard.ReservationStats{
		Count: int(count),
	}, nil
}

func (s *DashboardService) getOnlineDriverStats(ctx context.Context) (*dashboard.DriverStats, error) {
	collection := s.mongoDB.GetCollection("drivers")

	onlineDrivers, err := collection.CountDocuments(ctx, bson.M{
		"is_active": true,
		"is_online": true,
	})
	if err != nil {
		return nil, err
	}

	return &dashboard.DriverStats{
		Count: int(onlineDrivers),
	}, nil
}

// GetOnlineDriverStatsByFleet 取得按車隊分組的線上司機統計
func (s *DashboardService) GetOnlineDriverStatsByFleet(ctx context.Context) (map[string]int, error) {
	collection := s.mongoDB.GetCollection("drivers")

	pipeline := []bson.M{
		{
			"$match": bson.M{
				"is_active": true,
				"is_online": true,
			},
		},
		{
			"$group": bson.M{
				"_id":   "$fleet",
				"count": bson.M{"$sum": 1},
			},
		},
	}

	cursor, err := collection.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	fleetCounts := make(map[string]int)
	var totalOnline int

	for cursor.Next(ctx) {
		var result struct {
			Fleet string `bson:"_id"`
			Count int    `bson:"count"`
		}
		if err := cursor.Decode(&result); err != nil {
			return nil, err
		}

		// 處理空白車隊名稱
		fleet := result.Fleet
		if fleet == "" {
			fleet = "未分配"
		}

		fleetCounts[fleet] = result.Count
		totalOnline += result.Count
	}

	if err := cursor.Err(); err != nil {
		return nil, err
	}

	// 新增總計
	fleetCounts["all"] = totalOnline

	return fleetCounts, nil
}

func (s *DashboardService) getIdleDriverStats(ctx context.Context) (*dashboard.DriverStats, error) {
	collection := s.mongoDB.GetCollection("drivers")

	idleDrivers, err := collection.CountDocuments(ctx, bson.M{
		"is_active": true,
		"is_online": true,
		"status":    model.DriverStatusIdle,
	})
	if err != nil {
		return nil, err
	}

	return &dashboard.DriverStats{
		Count: int(idleDrivers),
	}, nil
}

func (s *DashboardService) getPickingUpDriversCount(ctx context.Context) (int, error) {
	collection := s.mongoDB.GetCollection("drivers")

	count, err := collection.CountDocuments(ctx, bson.M{
		"is_active": true,
		"is_online": true,
		"status": bson.M{
			"$in": []model.DriverStatus{
				model.DriverStatusEnroute,
				model.DriverStatusArrived,
			},
		},
	})
	if err != nil {
		return 0, err
	}

	return int(count), nil
}

func (s *DashboardService) getExecutingTaskDriversCount(ctx context.Context) (int, error) {
	collection := s.mongoDB.GetCollection("drivers")

	count, err := collection.CountDocuments(ctx, bson.M{
		"is_active": true,
		"is_online": true,
		"status":    model.DriverStatusExecuting,
	})
	if err != nil {
		return 0, err
	}

	return int(count), nil
}

func (s *DashboardService) GetAllDrivers(ctx context.Context) ([]model.DriverInfo, int, error) {
	collection := s.mongoDB.GetCollection("drivers")

	opts := options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}})

	cursor, err := collection.Find(ctx, bson.M{}, opts)
	if err != nil {
		return nil, 0, err
	}
	defer cursor.Close(ctx)

	var drivers []model.DriverInfo
	if err := cursor.All(ctx, &drivers); err != nil {
		return nil, 0, err
	}

	totalCount, err := collection.CountDocuments(ctx, bson.M{})
	if err != nil {
		return nil, 0, err
	}

	return drivers, int(totalCount), nil
}

func (s *DashboardService) GetDriverOrders(ctx context.Context, pageNum, pageSize int, driverStatus, searchKeyword string) ([]dashboard.DashboardItem, common.PaginationInfo, error) {
	// 設定默認分頁參數
	if pageNum <= 0 {
		pageNum = 1
	}
	if pageSize <= 0 {
		pageSize = 10
	}

	var allItems []dashboard.DashboardItem

	// 1. 獲取訂單項目
	orderItems, err := s.getOrderItems(ctx, driverStatus, searchKeyword)
	if err != nil {
		s.logger.Error().Err(err).Msg("取得訂單項目失敗 (Failed to get order items)")
		return nil, common.PaginationInfo{}, err
	}
	s.logger.Debug().Int("count", len(orderItems)).Msg("找到訂單項目 (Found order items)")
	allItems = append(allItems, orderItems...)

	// 2. 獲取司機狀態項目
	driverItems, err := s.getDriverStatusItems(ctx, driverStatus, searchKeyword)
	if err != nil {
		s.logger.Error().Err(err).Msg("取得司機項目失敗 (Failed to get driver items)")
		return nil, common.PaginationInfo{}, err
	}
	allItems = append(allItems, driverItems...)

	// 3. 按時間排序（最新的在前面）
	sort.Slice(allItems, func(i, j int) bool {
		if allItems[i].Time == nil && allItems[j].Time == nil {
			return false
		}
		if allItems[i].Time == nil {
			return false
		}
		if allItems[j].Time == nil {
			return true
		}
		return allItems[i].Time.After(*allItems[j].Time)
	})

	// 4. 計算分頁
	totalItems := int64(len(allItems))
	skip := (pageNum - 1) * pageSize
	end := skip + pageSize

	if skip >= len(allItems) {
		allItems = []dashboard.DashboardItem{}
	} else if end > len(allItems) {
		allItems = allItems[skip:]
	} else {
		allItems = allItems[skip:end]
	}

	pagination := common.NewPaginationInfo(pageNum, pageSize, totalItems)

	return allItems, pagination, nil
}

// getOrderItems 獲取訂單項目 - 包含非閒置司機的訂單和等待接單的訂單
func (s *DashboardService) getOrderItems(ctx context.Context, driverStatus, searchKeyword string) ([]dashboard.DashboardItem, error) {
	var items []dashboard.DashboardItem
	ordersCollection := s.mongoDB.GetCollection("orders")

	// 1. 獲取所有非閒置司機的進行中訂單
	driversCollection := s.mongoDB.GetCollection("drivers")

	// 構建司機過濾條件
	andConditions := []bson.M{
		{"is_online": true},
		{"is_active": true},
		{"status": bson.M{"$ne": model.DriverStatusIdle}}, // 排除閒置司機
	}

	// 根據司機狀態進一步過濾
	if driverStatus != "" && driverStatus != "all" && driverStatus != "idle" {
		andConditions = append(andConditions, bson.M{"status": driverStatus})
	}

	// 司機搜索條件
	if searchKeyword != "" && strings.TrimSpace(searchKeyword) != "" {
		keyword := strings.TrimSpace(searchKeyword)
		regex := primitive.Regex{Pattern: keyword, Options: "i"}
		andConditions = append(andConditions, bson.M{
			"$or": []bson.M{
				{"name": regex},
				{"car_plate": regex},
			},
		})
	}

	driverFilter := bson.M{"$and": andConditions}

	driverCursor, err := driversCollection.Find(ctx, driverFilter)
	if err != nil {
		return nil, err
	}
	defer driverCursor.Close(ctx)

	// 為每個非閒置司機找到他們正在進行的訂單
	for driverCursor.Next(ctx) {
		var driver model.DriverInfo
		if err := driverCursor.Decode(&driver); err != nil {
			continue
		}

		// 找這個司機正在進行的訂單
		orderFilter := bson.M{
			"driver.assigned_driver": driver.Name,
			"status": bson.M{
				"$in": []model.OrderStatus{
					model.OrderStatusEnroute,
					model.OrderStatusDriverArrived,
					model.OrderStatusExecuting,
				},
			},
		}

		// 如果有搜索關鍵字，檢查訂單欄位
		if searchKeyword != "" && strings.TrimSpace(searchKeyword) != "" {
			keyword := strings.TrimSpace(searchKeyword)
			regex := primitive.Regex{Pattern: keyword, Options: "i"}
			orderFilter["$or"] = []bson.M{
				{"customer.pickup_address": regex},
				{"customer.input_pickup_address": regex},
				{"ori_text": regex},
				{"ori_text_display": regex},
				{"hints": regex},
			}
		}

		var order model.Order
		err := ordersCollection.FindOne(ctx, orderFilter).Decode(&order)
		if err == nil {
			// 找到訂單，加入結果
			orderTime := order.CreatedAt
			if order.AcceptanceTime != nil {
				orderTime = order.AcceptanceTime
			}

			shortID := order.ShortID

			item := dashboard.DashboardItem{
				Type:           "order",
				Time:           orderTime,
				OrderID:        order.ID.Hex(),
				ShortID:        shortID,
				OrderStatus:    string(order.Status),
				PickupAddress:  order.Customer.PickupAddress,
				OriText:        order.OriText,
				OriTextDisplay: order.OriTextDisplay,
				Hints:          order.Hints,
				DriverStatus:   string(driver.Status),
			}

			items = append(items, item)
		}
	}

	// 2. 加入所有等待接單的訂單（流單）
	waitingOrderFilter := bson.M{
		"status": model.OrderStatusWaiting,
	}

	// 如果有訂單搜索關鍵字
	if searchKeyword != "" && strings.TrimSpace(searchKeyword) != "" {
		keyword := strings.TrimSpace(searchKeyword)
		regex := primitive.Regex{Pattern: keyword, Options: "i"}
		waitingOrderFilter["$or"] = []bson.M{
			{"customer.pickup_address": regex},
			{"customer.input_pickup_address": regex},
			{"ori_text": regex},
			{"ori_text_display": regex},
			{"hints": regex},
		}
	}

	waitingCursor, err := ordersCollection.Find(ctx, waitingOrderFilter, options.Find().SetSort(bson.M{"created_at": -1}).SetLimit(100))
	if err != nil {
		return items, nil // 即使等待訂單查詢失敗，仍返回已有的結果
	}
	defer waitingCursor.Close(ctx)

	// 加入等待接單的訂單
	for waitingCursor.Next(ctx) {
		var order model.Order
		if err := waitingCursor.Decode(&order); err != nil {
			continue
		}

		shortID := order.ShortID

		item := dashboard.DashboardItem{
			Type:           "order",
			Time:           order.CreatedAt,
			OrderID:        order.ID.Hex(),
			ShortID:        shortID,
			OrderStatus:    string(order.Status),
			PickupAddress:  order.Customer.PickupAddress,
			OriText:        order.OriText,
			OriTextDisplay: order.OriTextDisplay,
			Hints:          order.Hints,
			DriverStatus:   "", // 等待接單的訂單沒有司機狀態
		}

		items = append(items, item)
	}

	return items, nil
}

// getDriverStatusItems 獲取司機狀態項目
func (s *DashboardService) getDriverStatusItems(ctx context.Context, driverStatus, searchKeyword string) ([]dashboard.DashboardItem, error) {
	driversCollection := s.mongoDB.GetCollection("drivers")

	// 構建基本過濾條件：只獲取在線司機
	matchFilter := bson.M{
		"is_online": true,
		"is_active": true,
	}

	// 根據司機狀態過濾（外層狀態過濾）
	if driverStatus != "" && driverStatus != "all" {
		matchFilter["status"] = driverStatus
	}

	// 搜索條件
	if searchKeyword != "" && strings.TrimSpace(searchKeyword) != "" {
		keyword := strings.TrimSpace(searchKeyword)
		regex := primitive.Regex{Pattern: keyword, Options: "i"}
		matchFilter["$or"] = []bson.M{
			{"name": regex},
			{"car_plate": regex},
		}
	}

	cursor, err := driversCollection.Find(ctx, matchFilter, options.Find().SetSort(bson.M{"updated_at": -1}).SetLimit(1000))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var items []dashboard.DashboardItem
	for cursor.Next(ctx) {
		var driver model.DriverInfo
		if err := cursor.Decode(&driver); err != nil {
			continue
		}

		// 所有符合條件的司機都要顯示（因為外層已經過濾了）
		driverInfoStr := utils.GetDriverInfo(&driver)

		item := dashboard.DashboardItem{
			Type:         "driver",
			Time:         &driver.UpdatedAt,
			DriverStatus: string(driver.Status),
			DriverInfo:   driverInfoStr,
		}

		items = append(items, item)
	}

	return items, nil
}

// GetDriverWeeklyOrderRanks 獲取司機週接單排行榜
func (s *DashboardService) GetDriverWeeklyOrderRanks(ctx context.Context, weekOffset, pageNum, pageSize int) ([]dashboard.DriverWeeklyOrderRank, int64, string, string, error) {
	// 計算目標週的開始和結束時間
	now := time.Now()

	// 找到本週的週一
	weekday := int(now.Weekday())
	if weekday == 0 { // 星期日
		weekday = 7
	}
	daysFromMonday := weekday - 1

	// 計算目標週的週一
	targetMonday := now.AddDate(0, 0, -daysFromMonday+weekOffset*7)
	weekStart := time.Date(targetMonday.Year(), targetMonday.Month(), targetMonday.Day(), 0, 0, 0, 0, targetMonday.Location())
	weekEnd := weekStart.AddDate(0, 0, 7).Add(-time.Nanosecond)

	s.logger.Info().Str("week_start", weekStart.Format("2006-01-02")).Str("week_end", weekEnd.Format("2006-01-02")).Msg("查詢司機週接單排行榜 (Querying driver weekly order ranks)")

	// MongoDB聚合查詢
	ordersColl := s.mongoDB.GetCollection("orders")

	pipeline := []interface{}{
		// 第一階段：過濾條件
		map[string]interface{}{
			"$match": map[string]interface{}{
				"status": map[string]interface{}{
					"$ne": model.OrderStatusFailed, // 不包含流單
				},
				"driver.assigned_driver": map[string]interface{}{
					"$ne": "", // 必須有指派司機
				},
				"created_at": map[string]interface{}{
					"$gte": weekStart,
					"$lte": weekEnd,
				},
			},
		},
		// 第二階段：按司機ID分組並計算接單數
		map[string]interface{}{
			"$group": map[string]interface{}{
				"_id":         "$driver.assigned_driver",
				"order_count": map[string]interface{}{"$sum": 1},
			},
		},
		// 第三階段：按接單數降序排序
		map[string]interface{}{
			"$sort": map[string]interface{}{
				"order_count": -1,
			},
		},
	}

	cursor, err := ordersColl.Aggregate(ctx, pipeline)
	if err != nil {
		s.logger.Error().Err(err).Msg("聚合查詢失敗 (Aggregate query failed)")
		return nil, 0, "", "", fmt.Errorf("聚合查詢失敗: %w", err)
	}
	defer cursor.Close(ctx)

	type aggregateResult struct {
		DriverID   string `bson:"_id"`
		OrderCount int    `bson:"order_count"`
	}

	var results []aggregateResult
	if err := cursor.All(ctx, &results); err != nil {
		s.logger.Error().Err(err).Msg("解析聚合結果失敗 (Failed to parse aggregate results)")
		return nil, 0, "", "", fmt.Errorf("解析聚合結果失敗: %w", err)
	}

	// 獲取司機詳細資訊
	driversColl := s.mongoDB.GetCollection("drivers")
	var ranks []dashboard.DriverWeeklyOrderRank

	for i, result := range results {
		// 轉換 DriverID 為 ObjectID
		driverObjectID, err := primitive.ObjectIDFromHex(result.DriverID)
		if err != nil {
			s.logger.Warn().Str("driver_id", result.DriverID).Msg("無效的司機ID (Invalid driver ID)")
			continue
		}

		var driver model.DriverInfo
		err = driversColl.FindOne(ctx, map[string]interface{}{"_id": driverObjectID}).Decode(&driver)
		if err != nil {
			s.logger.Warn().Str("driver_id", result.DriverID).Msg("找不到司機資料 (Driver data not found)")
			continue
		}

		rank := dashboard.DriverWeeklyOrderRank{
			DriverID:   result.DriverID,
			DriverName: driver.Name,
			CarPlate:   driver.CarPlate,
			Fleet:      string(driver.Fleet),
			OrderCount: result.OrderCount,
			Rank:       i + 1,
		}
		ranks = append(ranks, rank)
	}

	// 計算總數和分頁
	totalCount := int64(len(ranks))

	// 應用分頁
	skip := (pageNum - 1) * pageSize
	end := skip + pageSize
	if skip >= len(ranks) {
		ranks = []dashboard.DriverWeeklyOrderRank{}
	} else {
		if end > len(ranks) {
			end = len(ranks)
		}
		ranks = ranks[skip:end]
	}

	weekStartStr := weekStart.Format("2006-01-02")
	weekEndStr := weekEnd.Format("2006-01-02")

	return ranks, totalCount, weekStartStr, weekEndStr, nil
}
