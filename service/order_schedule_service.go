package service

import (
	"context"
	"fmt"
	"right-backend/model"
	"time"

	"github.com/rs/zerolog"
)

type OrderScheduleService struct {
	logger        zerolog.Logger
	orderService  *OrderService
	driverService *DriverService
}

func NewOrderScheduleService(logger zerolog.Logger, orderService *OrderService, driverService *DriverService) *OrderScheduleService {
	return &OrderScheduleService{
		logger:        logger.With().Str("module", "order_schedule_service").Logger(),
		orderService:  orderService,
		driverService: driverService,
	}
}

// AcceptScheduledOrder 司機接收預約訂單
func (s *OrderScheduleService) AcceptScheduledOrder(ctx context.Context, driverInfo *model.DriverInfo, orderID string, requestTime time.Time) (string, string, string, error) {
	return s.driverService.AcceptScheduledOrder(ctx, driverInfo, orderID, requestTime)
}

// ActivateScheduledOrder 司機激活預約單
func (s *OrderScheduleService) ActivateScheduledOrder(ctx context.Context, driverInfo *model.DriverInfo, orderID string, requestTime time.Time) (*model.Order, error) {
	return s.driverService.ActivateScheduledOrder(ctx, driverInfo, orderID, requestTime)
}

// GetCurrentScheduledOrder 獲取司機當前已接收的預約單
func (s *OrderScheduleService) GetCurrentScheduledOrder(ctx context.Context, driverInfo *model.DriverInfo) (*model.Order, error) {
	// 直接檢查司機的 currentScheduleId
	if driverInfo.CurrentOrderScheduleId == nil || *driverInfo.CurrentOrderScheduleId == "" {
		return nil, nil
	}
	// 根據 currentScheduleId 獲取訂單信息
	return s.orderService.GetOrderByID(ctx, *driverInfo.CurrentOrderScheduleId)
}

// GetOrderByID 獲取訂單資訊
func (s *OrderScheduleService) GetOrderByID(ctx context.Context, orderID string) (*model.Order, error) {
	return s.orderService.GetOrderByID(ctx, orderID)
}

// GetScheduleOrders 獲取預約訂單列表
func (s *OrderScheduleService) GetScheduleOrders(ctx context.Context, pageNum, pageSize int) ([]model.Order, int64, error) {
	orders, total, err := s.orderService.GetScheduleOrders(ctx, pageNum, pageSize)
	if err != nil {
		return nil, 0, err
	}

	// 轉換 []*model.Order 為 []model.Order
	result := make([]model.Order, len(orders))
	for i, order := range orders {
		result[i] = *order
	}

	return result, total, nil
}

// GetScheduleOrderCount 獲取可接預約單數量（合併版本，支持車隊過濾）
func (s *OrderScheduleService) GetScheduleOrderCount(ctx context.Context, fleet string) (int64, error) {
	return s.orderService.GetScheduleOrderCountByFleet(ctx, fleet)
}

// CalcDistanceAndMins 重新計算司機到客戶上車地點的距離和時間，並更新訂單
func (s *OrderScheduleService) CalcDistanceAndMins(ctx context.Context, driverInfo *model.DriverInfo, orderID string) (float64, int, error) {
	// 獲取訂單資訊
	order, err := s.orderService.GetOrderByID(ctx, orderID)
	if err != nil {
		s.logger.Error().Str("order_id", orderID).Err(err).Msg("獲取訂單失敗")
		return 0, 0, fmt.Errorf("獲取訂單失敗: %w", err)
	}

	// 檢查訂單是否屬於該司機
	if order.Driver.AssignedDriver != driverInfo.ID.Hex() {
		return 0, 0, fmt.Errorf("此訂單不屬於該司機")
	}

	// 使用 DriverService 的 CalcDistanceAndMins 方法計算距離和時間
	distanceKm, estPickupMins, err := s.driverService.CalcDistanceAndMins(ctx, driverInfo, order)
	if err != nil {
		s.logger.Error().
			Err(err).
			Str("driver_id", driverInfo.ID.Hex()).
			Str("order_id", orderID).
			Msg("計算距離和時間失敗")
		return 0, 0, fmt.Errorf("計算距離和時間失敗: %w", err)
	}

	// 計算預估到達時間（台北時間）
	taipeiLocation := time.FixedZone("Asia/Taipei", 8*3600)
	estPickupTime := time.Now().Add(time.Duration(estPickupMins) * time.Minute).In(taipeiLocation)
	estPickupTimeStr := estPickupTime.Format("15:04:05")

	// 更新訂單的預估數據
	err = s.orderService.UpdateOrderEstimatedData(ctx, orderID, distanceKm, estPickupMins, estPickupTimeStr)
	if err != nil {
		s.logger.Error().
			Err(err).
			Str("order_id", orderID).
			Float64("distance_km", distanceKm).
			Int("est_pickup_mins", estPickupMins).
			Msg("更新訂單預估數據失敗")
		return 0, 0, fmt.Errorf("更新訂單預估數據失敗: %w", err)
	}

	s.logger.Info().
		Str("driver_id", driverInfo.ID.Hex()).
		Str("order_id", orderID).
		Float64("distance_km", distanceKm).
		Int("est_pickup_mins", estPickupMins).
		Str("est_pickup_time", estPickupTimeStr).
		Msg("✅ 成功重新計算並更新訂單距離和時間")

	return distanceKm, estPickupMins, nil
}

// CancelScheduledOrder 取消預約訂單，並重置司機的預約單狀態
func (s *OrderScheduleService) CancelScheduledOrder(ctx context.Context, orderID string, cancelReason string, cancelledBy string) (*model.Order, error) {
	// 1. 使用 OrderService 的統一取消服務
	updatedOrder, err := s.orderService.CancelOrder(ctx, orderID, cancelReason, cancelledBy)
	if err != nil {
		return nil, err
	}

	// 2. 如果有指派司機，需要額外重置司機的預約單狀態
	if updatedOrder.Driver.AssignedDriver != "" {
		driverID := updatedOrder.Driver.AssignedDriver

		// 重置司機的預約單相關狀態
		if resetErr := s.driverService.ResetDriverScheduledOrder(ctx, driverID); resetErr != nil {
			s.logger.Error().Err(resetErr).
				Str("driver_id", driverID).
				Str("order_id", orderID).
				Msg("重置司機預約單狀態失敗")
			// 不返回錯誤，因為訂單已經取消成功
		} else {
			s.logger.Info().
				Str("driver_id", driverID).
				Str("order_id", orderID).
				Msg("已重置司機預約單狀態")
		}
	}

	s.logger.Info().
		Str("order_id", orderID).
		Str("short_id", updatedOrder.ShortID).
		Str("cancelled_by", cancelledBy).
		Msg("預約訂單已成功取消")

	return updatedOrder, nil
}
