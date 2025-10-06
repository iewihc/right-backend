package service

import (
	"context"
	orderModels "right-backend/data-models/order"
	"right-backend/infra"
	"right-backend/model"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type OrderSummaryService struct {
	logger  zerolog.Logger
	mongoDB *infra.MongoDB
}

func NewOrderSummaryService(logger zerolog.Logger, mongoDB *infra.MongoDB) *OrderSummaryService {
	return &OrderSummaryService{
		logger:  logger.With().Str("module", "order_summary_service").Logger(),
		mongoDB: mongoDB,
	}
}

// OrderSummaryFilter 訂單報表過濾器
type OrderSummaryFilter struct {
	StartDate     string
	EndDate       string
	Fleet         string
	CustomerGroup string
	Status        []string
	PickupAddress string
	OrderID       string
	Driver        string
	PassengerID   string
}

// GetOrderSummary 獲取訂單報表列表，支援自訂排序
func (s *OrderSummaryService) GetOrderSummary(ctx context.Context, pageNum, pageSize int, filter *OrderSummaryFilter, sortField, sortOrder string) ([]*orderModels.OrderSummaryItem, int64, error) {
	collection := s.mongoDB.GetCollection("orders")

	// 基本過濾條件：排除流單狀態
	matchFilter := bson.M{
		"status": bson.M{"$ne": model.OrderStatusFailed},
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

		// 狀態過濾（排除流單狀態）
		if len(filter.Status) > 0 {
			// 過濾掉「流單」狀態
			filteredStatuses := []string{}
			for _, status := range filter.Status {
				if status != string(model.OrderStatusFailed) && status != "流單" {
					filteredStatuses = append(filteredStatuses, status)
				}
			}
			// 只有在有有效狀態時才添加過濾條件
			if len(filteredStatuses) > 0 {
				matchFilter["status"] = bson.M{"$in": filteredStatuses}
			}
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
	defer cursor.Close(ctx)

	var orders []*model.Order
	if err = cursor.All(ctx, &orders); err != nil {
		return nil, 0, err
	}

	// 轉換為 OrderSummaryItem 並獲取司機街口帳號
	orderSummaryItems := make([]*orderModels.OrderSummaryItem, len(orders))
	driverCollection := s.mongoDB.GetCollection("drivers")

	for i, order := range orders {
		item := &orderModels.OrderSummaryItem{
			Order: order,
			Driver: orderModels.DriverWithJkoAccount{
				Driver: order.Driver,
			},
		}

		// 如果有分派司機，獲取街口帳號和車輛顏色
		if order.Driver.AssignedDriver != "" {
			driverID, err := primitive.ObjectIDFromHex(order.Driver.AssignedDriver)
			if err == nil {
				var driverInfo model.DriverInfo
				err = driverCollection.FindOne(ctx, bson.M{"_id": driverID}).Decode(&driverInfo)
				if err == nil {
					item.Driver.JkoAccount = driverInfo.JkoAccount
					item.Driver.CarColor = driverInfo.CarColor
				}
			}
		}

		orderSummaryItems[i] = item
	}

	return orderSummaryItems, total, nil
}

// GetOrderByID 根據ID獲取訂單
func (s *OrderSummaryService) GetOrderByID(ctx context.Context, id string) (*model.Order, error) {
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

// CreateOrder 創建新訂單
func (s *OrderSummaryService) CreateOrder(ctx context.Context, order *model.Order) (*model.Order, error) {
	now := time.Now()
	id := primitive.NewObjectID()
	status := model.OrderStatusWaiting
	order.ID = &id

	order.CreatedAt = &now
	order.UpdatedAt = &now
	order.Status = status

	collection := s.mongoDB.GetCollection("orders")
	_, err := collection.InsertOne(ctx, order)
	if err != nil {
		return nil, err
	}

	return order, nil
}

// UpdateOrderSummary 更新訂單報表
func (s *OrderSummaryService) UpdateOrderSummary(ctx context.Context, id string, updateData *struct {
	Type          *model.OrderType   `json:"type,omitempty" doc:"訂單類型"`
	Status        *model.OrderStatus `json:"status,omitempty" doc:"訂單狀態"`
	Amount        *int               `json:"amount,omitempty" doc:"金額"`
	AmountNote    *string            `json:"amount_note,omitempty" doc:"金額備註"`
	PassengerID   *string            `json:"passenger_id,omitempty" doc:"乘客ID"`
	CustomerGroup *string            `json:"customer_group,omitempty" doc:"客群"`
	Customer      *model.Customer    `json:"customer,omitempty" doc:"客戶資訊"`
}) (*model.Order, error) {
	objectID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, err
	}

	collection := s.mongoDB.GetCollection("orders")
	filter := bson.M{"_id": objectID}

	updateFields := bson.M{
		"updated_at": time.Now(),
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
	options := options.FindOneAndUpdate().SetReturnDocument(options.After)

	var updatedOrder model.Order
	err = collection.FindOneAndUpdate(ctx, filter, update, options).Decode(&updatedOrder)
	if err != nil {
		return nil, err
	}

	return &updatedOrder, nil
}

// DeleteOrder 刪除訂單
func (s *OrderSummaryService) DeleteOrder(ctx context.Context, orderID string) error {
	objectID, err := primitive.ObjectIDFromHex(orderID)
	if err != nil {
		s.logger.Error().Str("order_id", orderID).Err(err).Msg("無效的訂單ID (Invalid order ID)")
		return err
	}

	collection := s.mongoDB.GetCollection("orders")
	filter := bson.M{"_id": objectID}

	result, err := collection.DeleteOne(ctx, filter)
	if err != nil {
		s.logger.Error().Str("order_id", orderID).Err(err).Msg("刪除訂單失敗 (Failed to delete order)")
		return err
	}

	if result.DeletedCount == 0 {
		s.logger.Warn().Str("order_id", orderID).Msg("訂單不存在 (Order not found)")
		return err
	}

	s.logger.Info().Str("order_id", orderID).Msg("訂單刪除成功 (Order deleted successfully)")
	return nil
}
