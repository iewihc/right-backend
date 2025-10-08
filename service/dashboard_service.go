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

	offlineDriverStats, err := s.getOfflineDriverStats(ctx)
	if err != nil {
		return nil, err
	}
	stats.OfflineDrivers = *offlineDriverStats

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

func (s *DashboardService) getOfflineDriverStats(ctx context.Context) (*dashboard.DriverStats, error) {
	collection := s.mongoDB.GetCollection("drivers")

	offlineDrivers, err := collection.CountDocuments(ctx, bson.M{
		"is_active": true,
		"is_online": false,
	})
	if err != nil {
		return nil, err
	}

	return &dashboard.DriverStats{
		Count: int(offlineDrivers),
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

func (s *DashboardService) GetDriverOrders(ctx context.Context, pageNum, pageSize int, driverStatus, searchKeyword string) ([]dashboard.DriverOrderItem, common.PaginationInfo, error) {
	// 設定默認分頁參數
	if pageNum <= 0 {
		pageNum = 1
	}
	if pageSize <= 0 {
		pageSize = 10
	}

	driversCollection := s.mongoDB.GetCollection("drivers")
	ordersCollection := s.mongoDB.GetCollection("orders")

	// 1. 構建司機過濾條件
	driverFilter := bson.M{
		"is_active": true,
		"is_online": true,
	}

	// 根據司機狀態過濾
	if driverStatus != "" && driverStatus != "all" {
		driverFilter["status"] = driverStatus
	}

	// 司機資訊搜尋（車牌、名稱、編號、帳號）
	if searchKeyword != "" && strings.TrimSpace(searchKeyword) != "" {
		keyword := strings.TrimSpace(searchKeyword)
		regex := primitive.Regex{Pattern: keyword, Options: "i"}
		driverFilter["$or"] = []bson.M{
			{"name": regex},
			{"car_plate": regex},
			{"driver_no": regex},
			{"account": regex},
		}
	}

	// 2. 載入所有符合條件的線上司機
	driverCursor, err := driversCollection.Find(ctx, driverFilter, options.Find().SetSort(bson.M{"updated_at": -1}))
	if err != nil {
		return nil, common.PaginationInfo{}, err
	}
	defer driverCursor.Close(ctx)

	var allDrivers []model.DriverInfo
	if err := driverCursor.All(ctx, &allDrivers); err != nil {
		return nil, common.PaginationInfo{}, err
	}

	// 3. 處理每個司機，建立 DriverOrderItem
	var allItems []dashboard.DriverOrderItem

	for _, driver := range allDrivers {
		item := dashboard.DriverOrderItem{
			DriverInfo:   utils.GetDriverInfoWithPlate(&driver),
			DriverFleet:  string(driver.Fleet),
			DriverStatus: string(driver.Status),
		}

		// 處理即時訂單
		if driver.CurrentOrderId != nil && *driver.CurrentOrderId != "" {
			orderID, err := primitive.ObjectIDFromHex(*driver.CurrentOrderId)
			if err == nil {
				var order model.Order
				err = ordersCollection.FindOne(ctx, bson.M{"_id": orderID}).Decode(&order)
				if err == nil {
					item.OrderID = order.ID.Hex()
					item.ShortID = order.ShortID
					item.OrderStatus = string(order.Status)
					item.PickupAddress = order.Customer.PickupAddress
					item.OriText = order.OriText
					item.OriTextDisplay = order.OriTextDisplay
					item.Hints = order.Hints
					// 非閒置司機：使用訂單的 created_at
					if order.CreatedAt != nil {
						item.Time = order.CreatedAt
					}
				}
			}
		}

		// 處理預約訂單
		if driver.CurrentOrderScheduleId != nil && *driver.CurrentOrderScheduleId != "" {
			scheduleID, err := primitive.ObjectIDFromHex(*driver.CurrentOrderScheduleId)
			if err == nil {
				var scheduleOrder model.Order
				err = ordersCollection.FindOne(ctx, bson.M{"_id": scheduleID}).Decode(&scheduleOrder)
				if err == nil {
					item.ScheduleOrderID = scheduleOrder.ID.Hex()
					item.ScheduleShortID = scheduleOrder.ShortID
					item.ScheduleOrderStatus = string(scheduleOrder.Status)
					item.ScheduleOriText = scheduleOrder.OriText
					item.SchedulePickup = scheduleOrder.Customer.PickupAddress
					item.ScheduleTime = scheduleOrder.ScheduledAt
					item.ScheduleHints = scheduleOrder.Hints
				}
			}
		}

		// 閒置司機：使用司機的 updated_at
		if item.DriverStatus == string(model.DriverStatusIdle) {
			item.Time = &driver.UpdatedAt
		}

		allItems = append(allItems, item)
	}

	// 4. 排序：按 Time 降序排列（最新的在最上面）
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

	// 5. 計算分頁
	totalItems := int64(len(allItems))
	skip := (pageNum - 1) * pageSize
	end := skip + pageSize

	if skip >= len(allItems) {
		allItems = []dashboard.DriverOrderItem{}
	} else if end > len(allItems) {
		allItems = allItems[skip:]
	} else {
		allItems = allItems[skip:end]
	}

	pagination := common.NewPaginationInfo(pageNum, pageSize, totalItems)

	return allItems, pagination, nil
}

// filterDriverOrderItems 過濾司機訂單項目（支援司機編號、帳號、名稱、訂單短ID、ori_text 搜尋）
func (s *DashboardService) filterDriverOrderItems(items []dashboard.DriverOrderItem, keyword string) []dashboard.DriverOrderItem {
	keyword = strings.ToLower(strings.TrimSpace(keyword))
	var filtered []dashboard.DriverOrderItem

	for _, item := range items {
		// 檢查司機資訊（DriverInfo 已包含車牌、名稱、車隊等資訊）
		if strings.Contains(strings.ToLower(item.DriverInfo), keyword) {
			filtered = append(filtered, item)
			continue
		}

		// 檢查即時訂單資訊（只有在非閒置狀態時才檢查）
		if item.DriverStatus != string(model.DriverStatusIdle) {
			if strings.Contains(strings.ToLower(item.ShortID), keyword) ||
				strings.Contains(strings.ToLower(item.OriText), keyword) ||
				strings.Contains(strings.ToLower(item.PickupAddress), keyword) {
				filtered = append(filtered, item)
				continue
			}

			// 檢查預約訂單資訊
			if strings.Contains(strings.ToLower(item.ScheduleShortID), keyword) ||
				strings.Contains(strings.ToLower(item.ScheduleOriText), keyword) {
				filtered = append(filtered, item)
				continue
			}
		}
	}

	return filtered
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
