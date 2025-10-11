package controller

import (
	"context"
	"right-backend/auth"
	"right-backend/data-models/order_schedule"
	"right-backend/middleware"
	"right-backend/service"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/rs/zerolog"
)

type OrderScheduleController struct {
	logger               zerolog.Logger
	orderScheduleService *service.OrderScheduleService
	driverAuthMiddleware *middleware.DriverAuthMiddleware
	userAuthMiddleware   *middleware.UserAuthMiddleware
}

func NewOrderScheduleController(
	logger zerolog.Logger,
	orderScheduleService *service.OrderScheduleService,
	driverAuthMiddleware *middleware.DriverAuthMiddleware,
	userAuthMiddleware *middleware.UserAuthMiddleware,
) *OrderScheduleController {
	return &OrderScheduleController{
		logger:               logger.With().Str("module", "order_schedule_controller").Logger(),
		orderScheduleService: orderScheduleService,
		driverAuthMiddleware: driverAuthMiddleware,
		userAuthMiddleware:   userAuthMiddleware,
	}
}

func (c *OrderScheduleController) RegisterRoutes(api huma.API) {
	// 司機接收預約訂單
	huma.Register(api, huma.Operation{
		OperationID: "accept-scheduled-order",
		Method:      "POST",
		Path:        "/order-schedule/accept",
		Summary:     "司機從列表接收預約訂單(1)",
		Description: "司機從列表接收預約訂單，系統會檢查司機是否有時間衝突",
		Tags:        []string{"order-schedule"},
		Middlewares: huma.Middlewares{c.driverAuthMiddleware.Auth()},
		Security: []map[string][]string{
			{"bearerAuth": {}},
		},
	}, func(ctx context.Context, input *order_schedule.AcceptScheduledOrderInput) (*order_schedule.AcceptScheduledOrderResponse, error) {
		requestTime := time.Now() // 在第一行記錄請求時間
		d, err := auth.GetDriverFromContext(ctx)
		if err != nil {
			return nil, huma.Error401Unauthorized("無效的司機Auth")
		}
		c.logger.Info().
			Str("driver_id", d.ID.Hex()).
			Str("driver_name", d.Name).
			Str("car_plate", d.CarPlate).
			Str("fleet", string(d.Fleet)).
			Str("order_id", input.Body.OrderID).
			Msg("預約單接單操作 - 司機接預約單")

		scheduledTime, pickupAddress, _, err := c.orderScheduleService.AcceptScheduledOrder(ctx, d, input.Body.OrderID, requestTime)
		if err != nil {
			c.logger.Error().
				Err(err).
				Str("driver_id", d.ID.Hex()).
				Str("order_id", input.Body.OrderID).
				Msg("接預約單失敗")
			return nil, huma.Error400BadRequest("接預約單失敗: " + err.Error())
		}

		resp := &order_schedule.AcceptScheduledOrderResponse{}
		resp.Body.Message = "預約單接單成功"
		resp.Body.ScheduledTime = scheduledTime
		resp.Body.PickupAddress = pickupAddress
		return resp, nil
	})

	// 司機激活預約單（前往客上）
	huma.Register(api, huma.Operation{
		OperationID: "activate-scheduled-order",
		Method:      "POST",
		Path:        "/order-schedule/{id}/activate",
		Summary:     "司機激活預約單為即時單-開始前往客上(2)",
		Description: "司機激活已接受的預約單，開始前往上車點。只有狀態為「預約單已接受」的訂單才能被激活",
		Tags:        []string{"order-schedule"},
		Middlewares: huma.Middlewares{c.driverAuthMiddleware.Auth()},
		Security: []map[string][]string{
			{"bearerAuth": {}},
		},
	}, func(ctx context.Context, input *order_schedule.ActivateScheduledOrderInput) (*order_schedule.ActivateScheduledOrderResponse, error) {
		requestTime := time.Now()
		d, err := auth.GetDriverFromContext(ctx)
		if err != nil {
			return nil, huma.Error401Unauthorized("無效的司機Auth")
		}
		c.logger.Info().
			Str("driver_id", d.ID.Hex()).
			Str("driver_name", d.Name).
			Str("car_plate", d.CarPlate).
			Str("fleet", string(d.Fleet)).
			Str("order_id", input.OrderID).
			Msg("預約單激活操作 - 司機激活預約單")

		order, err := c.orderScheduleService.ActivateScheduledOrder(ctx, d, input.OrderID, requestTime)
		if err != nil {
			c.logger.Error().
				Err(err).
				Str("driver_id", d.ID.Hex()).
				Str("order_id", input.OrderID).
				Msg("激活預約單失敗")
			return nil, huma.Error400BadRequest("激活預約單失敗: " + err.Error())
		}

		resp := &order_schedule.ActivateScheduledOrderResponse{}
		resp.Body.OrderID = order.ID.Hex()
		if order.ScheduledAt != nil {
			taipeiLocation := time.FixedZone("Asia/Taipei", 8*3600)
			resp.Body.ScheduledTime = order.ScheduledAt.In(taipeiLocation).Format("2006-01-02 15:04:05")
		}
		resp.Body.PickupAddress = order.Customer.PickupAddress
		resp.Body.OrderStatus = string(order.Status)
		resp.Body.DriverStatus = string(d.Status)
		return resp, nil
	})

	// 獲取司機當前已接收的預約單
	huma.Register(api, huma.Operation{
		OperationID: "get-current-scheduled-order",
		Method:      "GET",
		Path:        "/order-schedule/current",
		Summary:     "獲取司機當前已接收的預約單",
		Description: "獲取司機當前已接收但尚未完成的預約單資訊，包括已接受和已激活的預約單",
		Tags:        []string{"order-schedule"},
		Middlewares: huma.Middlewares{c.driverAuthMiddleware.Auth()},
		Security: []map[string][]string{
			{"bearerAuth": {}},
		},
	}, func(ctx context.Context, input *order_schedule.CurrentScheduledOrderInput) (*order_schedule.CurrentScheduledOrderResponse, error) {
		d, err := auth.GetDriverFromContext(ctx)
		if err != nil {
			return nil, huma.Error401Unauthorized("無效的司機Auth")
		}

		order, err := c.orderScheduleService.GetCurrentScheduledOrder(ctx, d)
		if err != nil {
			c.logger.Error().
				Err(err).
				Str("driver_id", d.ID.Hex()).
				Msg("獲取當前預約單失敗")
			return nil, huma.Error500InternalServerError("獲取當前預約單失敗", err)
		}

		resp := &order_schedule.CurrentScheduledOrderResponse{}
		if order != nil {
			taipeiLocation := time.FixedZone("Asia/Taipei", 8*3600)

			scheduledTime := ""
			if order.ScheduledAt != nil {
				scheduledTime = order.ScheduledAt.In(taipeiLocation).Format("2006-01-02 15:04:05")
			}

			createdAt := ""
			if order.CreatedAt != nil {
				createdAt = order.CreatedAt.In(taipeiLocation).Format("2006-01-02 15:04:05")
			}

			updatedAt := ""
			if order.UpdatedAt != nil {
				updatedAt = order.UpdatedAt.In(taipeiLocation).Format("2006-01-02 15:04:05")
			}

			resp.Body = &order_schedule.CurrentScheduledOrderData{
				OrderID:        order.ID.Hex(),
				ShortID:        order.ShortID,
				Type:           string(order.Type),
				Status:         string(order.Status),
				OrderStatus:    string(order.Status),
				DriverStatus:   string(d.Status),
				ScheduledTime:  scheduledTime,
				AmountNote:     order.AmountNote,
				Income:         order.Income,
				Expense:        order.Expense,
				PassengerID:    order.PassengerID,
				Fleet:          string(order.Fleet),
				Rounds:         order.Rounds,
				IsPhotoTaken:   order.IsPhotoTaken,
				HasMeterJump:   order.HasMeterJump,
				IsErrand:       order.IsErrand,
				IsScheduled:    order.IsScheduled,
				CreatedBy:      order.CreatedBy,
				CreatedAt:      createdAt,
				UpdatedAt:      updatedAt,
				OriText:        order.OriText,
				OriTextDisplay: order.OriTextDisplay,
				Customer: order_schedule.CustomerData{
					InputPickupAddress: order.Customer.InputPickupAddress,
					PickupAddress:      order.Customer.PickupAddress,
					PickupLat:          order.Customer.PickupLat,
					PickupLng:          order.Customer.PickupLng,
					Remarks:            order.Customer.Remarks,
					InputDestAddress:   order.Customer.InputDestAddress,
					DestAddress:        order.Customer.DestAddress,
					DestLat:            order.Customer.DestLat,
					DestLng:            order.Customer.DestLng,
					EstPickToDestMins:  order.Customer.EstPickToDestMins,
					EstPickToDestDist:  order.Customer.EstPickToDestDist,
					EstPickToDestTime:  order.Customer.EstPickToDestTime,
					LineUserID:         order.Customer.LineUserID,
				},
				Driver: order_schedule.DriverData{
					AssignedDriver:       order.Driver.AssignedDriver,
					CarNo:                order.Driver.CarNo,
					CarColor:             order.Driver.CarColor,
					EstPickupMins:        order.Driver.EstPickupMins,
					EstPickupDistKm:      order.Driver.EstPickupDistKm,
					EstPickupTime:        order.Driver.EstPickupTime,
					AdjustMins:           order.Driver.AdjustMins,
					Lat:                  order.Driver.Lat,
					Lng:                  order.Driver.Lng,
					LineUserID:           order.Driver.LineUserID,
					Name:                 order.Driver.Name,
					Duration:             order.Driver.Duration,
					ArrivalDeviationSecs: order.Driver.ArrivalDeviationSecs,
				},
			}
		}
		return resp, nil
	})

	// 獲取預約訂單列表（管理端）
	huma.Register(api, huma.Operation{
		OperationID: "get-schedule-orders",
		Method:      "GET",
		Path:        "/order-schedule/list",
		Summary:     "獲取預約訂單列表",
		Description: "獲取預約訂單列表（只包含等待接單狀態，司機未分配）",
		Tags:        []string{"order-schedule"},
	}, func(ctx context.Context, input *order_schedule.GetScheduleOrdersInput) (*order_schedule.GetScheduleOrdersResponse, error) {
		c.logger.Info().
			Int("page_num", input.GetPageNum()).
			Int("page_size", input.GetPageSize()).
			Msg("獲取預約訂單列表")

		orders, total, err := c.orderScheduleService.GetScheduleOrders(ctx, input.GetPageNum(), input.GetPageSize())
		if err != nil {
			c.logger.Error().Err(err).Msg("獲取預約訂單列表失敗")
			return nil, huma.Error500InternalServerError("獲取預約訂單列表失敗", err)
		}

		resp := &order_schedule.GetScheduleOrdersResponse{}
		resp.Body.Orders = make([]order_schedule.ScheduleOrderData, len(orders))
		taipeiLocation := time.FixedZone("Asia/Taipei", 8*3600)

		for i, order := range orders {
			scheduledTime := ""
			if order.ScheduledAt != nil {
				scheduledTime = order.ScheduledAt.In(taipeiLocation).Format("2006-01-02 15:04:05")
			}
			createdAt := ""
			if order.CreatedAt != nil {
				createdAt = order.CreatedAt.In(taipeiLocation).Format("2006-01-02 15:04:05")
			}
			resp.Body.Orders[i] = order_schedule.ScheduleOrderData{
				ID:            order.ID.Hex(),
				ShortID:       order.ShortID,
				PickupAddress: order.Customer.PickupAddress,
				OriText:       order.OriText,
				ScheduledTime: scheduledTime,
				Fleet:         string(order.Fleet),
				Status:        string(order.Status),
				CreatedAt:     createdAt,
			}
		}
		resp.Body.Total = total
		return resp, nil
	})

	// 獲取可接預約單數量（合併版本，支持車隊過濾）
	huma.Register(api, huma.Operation{
		OperationID: "get-schedule-order-count",
		Method:      "GET",
		Path:        "/order-schedule/count",
		Summary:     "獲取當前可接預約單數量",
		Description: "獲取系統中當前可接預約單的總數量，只包含司機未分配且在昨天到今天範圍內的預約訂單，可以按車隊過濾",
		Tags:        []string{"order-schedule"},
	}, func(ctx context.Context, input *order_schedule.ScheduleOrderCountInput) (*order_schedule.ScheduleOrderCountResponse, error) {
		c.logger.Info().
			Str("fleet", input.Fleet).
			Msg("查詢可接預約單數量")

		count, err := c.orderScheduleService.GetScheduleOrderCount(ctx, input.Fleet)
		if err != nil {
			c.logger.Error().
				Err(err).
				Str("fleet", input.Fleet).
				Msg("獲取可接預約單數量失敗")
			return nil, huma.Error500InternalServerError("獲取可接預約單數量失敗", err)
		}

		resp := &order_schedule.ScheduleOrderCountResponse{}
		resp.Body.Count = count
		return resp, nil
	})

	// 重新計算距離和時間
	huma.Register(api, huma.Operation{
		OperationID: "calc-distance-and-mins",
		Method:      "POST",
		Path:        "/order-schedule/{id}/calc-distance-and-mins",
		Summary:     "重新計算司機到客戶上車地點的距離和時間",
		Description: "使用司機當前位置和訂單上車地點重新計算預估距離和時間，並更新訂單數據",
		Tags:        []string{"order-schedule"},
		Middlewares: huma.Middlewares{c.driverAuthMiddleware.Auth()},
		Security: []map[string][]string{
			{"bearerAuth": {}},
		},
	}, func(ctx context.Context, input *order_schedule.CalcDistanceAndMinsInput) (*order_schedule.CalcDistanceAndMinsResponse, error) {
		d, err := auth.GetDriverFromContext(ctx)
		if err != nil {
			return nil, huma.Error401Unauthorized("無效的司機Auth")
		}

		c.logger.Info().
			Str("driver_id", d.ID.Hex()).
			Str("driver_name", d.Name).
			Str("car_plate", d.CarPlate).
			Str("fleet", string(d.Fleet)).
			Str("order_id", input.OrderID).
			Msg("重新計算距離和時間操作")

		distanceKm, estPickupMins, err := c.orderScheduleService.CalcDistanceAndMins(ctx, d, input.OrderID)
		if err != nil {
			c.logger.Error().
				Err(err).
				Str("driver_id", d.ID.Hex()).
				Str("order_id", input.OrderID).
				Msg("重新計算距離和時間失敗")
			return nil, huma.Error400BadRequest("重新計算距離和時間失敗: " + err.Error())
		}

		// 計算預估到達時間（轉換為台北時間）
		taipeiLocation := time.FixedZone("Asia/Taipei", 8*3600)
		estPickupTime := time.Now().In(taipeiLocation).Add(time.Duration(estPickupMins) * time.Minute)
		estPickupTimeStr := estPickupTime.Format("15:04:05")

		resp := &order_schedule.CalcDistanceAndMinsResponse{}
		resp.Body.OrderID = input.OrderID
		resp.Body.DistanceKm = distanceKm
		resp.Body.EstPickupMins = estPickupMins
		resp.Body.EstPickupTime = estPickupTimeStr
		resp.Body.UpdatedAt = time.Now().In(taipeiLocation).Format("2006-01-02 15:04:05")

		c.logger.Info().
			Str("driver_id", d.ID.Hex()).
			Str("order_id", input.OrderID).
			Float64("distance_km", distanceKm).
			Int("est_pickup_mins", estPickupMins).
			Msg("✅ 重新計算距離和時間成功")

		return resp, nil
	})
}
