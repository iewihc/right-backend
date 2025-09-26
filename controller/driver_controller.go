package controller

import (
	"context"
	"fmt"
	"mime/multipart"
	"right-backend/auth"
	"right-backend/data-models/common"
	"right-backend/data-models/driver"
	"right-backend/data-models/order"
	"right-backend/infra"
	"right-backend/middleware"
	"right-backend/model"
	"right-backend/service"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel/trace"
)

type DriverController struct {
	logger               zerolog.Logger
	driverService        *service.DriverService
	orderService         *service.OrderService
	orderScheduleService *service.OrderScheduleService
	authMiddleware       *middleware.DriverAuthMiddleware
	fileStorageService   *service.FileStorageService
	baseURL              string
}

func NewDriverController(
	logger zerolog.Logger,
	driverService *service.DriverService,
	orderService *service.OrderService,
	orderScheduleService *service.OrderScheduleService,
	authMiddleware *middleware.DriverAuthMiddleware,
	fileStorageService *service.FileStorageService,
	baseURL string,
) *DriverController {
	return &DriverController{
		logger:               logger.With().Str("module", "driver_controller").Logger(),
		driverService:        driverService,
		orderService:         orderService,
		orderScheduleService: orderScheduleService,
		authMiddleware:       authMiddleware,
		fileStorageService:   fileStorageService,
		baseURL:              baseURL,
	}
}

func (c *DriverController) RegisterRoutes(api huma.API) {
	// 根據ID獲取司機
	huma.Register(api, huma.Operation{
		OperationID: "get-driver-by-id",
		Method:      "GET",
		Path:        "/drivers/{id}",
		Summary:     "根據ID獲取司機",
		Tags:        []string{"drivers"},
	}, func(ctx context.Context, input *driver.DriverIDInput) (*driver.DriverResponse, error) {
		d, err := c.driverService.GetDriverByID(ctx, input.ID)
		if err != nil {
			c.logger.Warn().Err(err).Str("driver_id", input.ID).Msg("根據ID獲取司機失敗")
			return nil, huma.Error404NotFound("司機不存在", err)
		}
		return &driver.DriverResponse{Body: d}, nil
	})

	// 更新司機個人資料
	huma.Register(api, huma.Operation{
		OperationID: "update-driver-profile",
		Method:      "PUT",
		Path:        "/drivers/profile",
		Summary:     "更新當前司機的個人資料",
		Description: "更新當前登入司機的個人資料，如姓名、車型、車齡。司機ID會從JWT Bearer token中自動獲取。",
		Tags:        []string{"drivers"},
		Middlewares: huma.Middlewares{c.authMiddleware.Auth()},
		Security: []map[string][]string{
			{"bearerAuth": {}},
		},
	}, func(ctx context.Context, input *driver.UpdateDriverProfileInput) (*driver.DriverResponse, error) {
		driverFromToken, err := auth.GetDriverFromContext(ctx)
		if err != nil {
			c.logger.Error().Err(err).Msg("無法從token中獲取司機資訊")
			return nil, huma.Error500InternalServerError("無法從token中獲取司機資訊")
		}
		driverID := driverFromToken.ID.Hex()

		c.logger.Info().Str("driver_id", driverID).Str("driver_name", driverFromToken.Name).Msg("司機正在更新個人資料")

		updatedDriver, err := c.driverService.UpdateDriverProfile(ctx, driverID, &input.Body)
		if err != nil {
			c.logger.Error().Err(err).Str("driver_id", driverID).Msg("更新司機資料失敗")
			return nil, huma.Error400BadRequest("更新司機資料失敗", err)
		}

		c.logger.Info().Str("driver_id", driverID).Str("driver_name", updatedDriver.Name).Msg("司機資料更新成功")
		return &driver.DriverResponse{Body: updatedDriver}, nil
	})

	// 獲取當前司機的完整資料
	huma.Register(api, huma.Operation{
		OperationID: "get-current-driver",
		Method:      "GET",
		Path:        "/drivers/me",
		Summary:     "獲取當前登入司機的完整資料",
		Description: "根據JWT Bearer token獲取當前登入司機的所有資料。",
		Tags:        []string{"drivers"},
		Middlewares: huma.Middlewares{c.authMiddleware.Auth()},
		Security: []map[string][]string{
			{"bearerAuth": {}},
		},
	}, func(ctx context.Context, input *struct{}) (*driver.DriverResponse, error) {
		driverFromToken, err := auth.GetDriverFromContext(ctx)
		if err != nil {
			c.logger.Error().Err(err).Msg("無法從token中獲取司機資訊")
			return nil, huma.Error500InternalServerError("無法從token中獲取司機資訊")
		}
		driverID := driverFromToken.ID.Hex()

		d, err := c.driverService.GetDriverByID(ctx, driverID)
		if err != nil {
			c.logger.Warn().Err(err).Str("driver_id", driverID).Msg("獲取當前司機資料失敗")
			return nil, huma.Error404NotFound("司機不存在", err)
		}

		// 如果司機有頭像，轉換為完整URL
		if d.AvatarPath != nil && *d.AvatarPath != "" {
			avatarURL := c.driverService.GetDriverAvatarURL(d.AvatarPath, c.baseURL)
			// 創建一個新的司機結構體副本，包含頭像URL
			driverWithAvatar := *d
			avatarURLField := avatarURL
			driverWithAvatar.AvatarPath = &avatarURLField

			c.logger.Debug().
				Str("driver_id", driverID).
				Str("avatar_path", *d.AvatarPath).
				Str("avatar_url", avatarURL).
				Msg("成功轉換司機頭像URL")

			return &driver.DriverResponse{Body: &driverWithAvatar}, nil
		}

		return &driver.DriverResponse{Body: d}, nil
	})

	// 獲取當前司機的狀態
	huma.Register(api, huma.Operation{
		OperationID: "get-current-driver-status",
		Method:      "GET",
		Path:        "/drivers/me/status",
		Summary:     "獲取當前登入司機的狀態(恢復用)",
		Description: "根據JWT Bearer token獲取當前登入司機的基本狀態資訊。",
		Tags:        []string{"drivers"},
		Middlewares: huma.Middlewares{c.authMiddleware.Auth()},
		Security: []map[string][]string{
			{"bearerAuth": {}},
		},
	}, func(ctx context.Context, input *struct{}) (*driver.DriverStatusResponse, error) {
		driverFromToken, err := auth.GetDriverFromContext(ctx)
		if err != nil {
			c.logger.Error().Err(err).Msg("無法從token中獲取司機資訊")
			return nil, huma.Error500InternalServerError("無法從token中獲取司機資訊")
		}
		driverID := driverFromToken.ID.Hex()

		d, err := c.driverService.GetDriverByID(ctx, driverID)
		if err != nil {
			c.logger.Warn().Err(err).Str("driver_id", driverID).Msg("獲取當前司機資料失敗")
			return nil, huma.Error404NotFound("司機不存在", err)
		}

		// 獲取司機當前訂單
		var orderId *string = nil
		var orderStatus *string = nil

		if orderInfo, err := c.orderService.GetCurrentOrderByDriverID(ctx, driverID); err == nil && orderInfo != nil {
			orderIDString := orderInfo.Order.ID.Hex()
			orderId = &orderIDString
			orderStatusString := string(orderInfo.OrderStatus)
			orderStatus = &orderStatusString
		}

		// 獲取司機當前預約訂單
		var scheduleOrderId *string = nil
		var scheduleOrderStatus *string = nil

		if scheduleOrder, err := c.orderScheduleService.GetCurrentScheduledOrder(ctx, d); err == nil && scheduleOrder != nil {
			scheduleOrderIDString := scheduleOrder.ID.Hex()
			scheduleOrderId = &scheduleOrderIDString
			// 預約訂單狀態使用訂單狀態
			scheduleOrderStatusString := string(scheduleOrder.Status)
			scheduleOrderStatus = &scheduleOrderStatusString
		}

		response := &driver.DriverStatusResponse{}
		response.Body.OrderId = orderId
		response.Body.ScheduleOrderId = scheduleOrderId
		response.Body.OrderStatus = orderStatus
		response.Body.ScheduleOrderStatus = scheduleOrderStatus
		response.Body.DriverStatus = string(d.Status)

		return response, nil
	})

	// 獲取當前司機的進行中訂單
	huma.Register(api, huma.Operation{
		OperationID: "get-current-driver-order",
		Method:      "GET",
		Path:        "/drivers/me/current-order",
		Summary:     "獲取當前司機的進行中訂單(復原用)",
		Description: "根據JWT Bearer token獲取當前登入司機正在進行中的訂單和當前司機狀態。如果司機為閒置狀態或沒有進行中的訂單，將返回404。",
		Tags:        []string{"drivers"},
		Middlewares: huma.Middlewares{c.authMiddleware.Auth()},
	}, func(ctx context.Context, input *struct{}) (*order.CurrentOrderWithDriverStatusResponse, error) {
		driverFromToken, err := auth.GetDriverFromContext(ctx)
		if err != nil {
			c.logger.Error().Err(err).Msg("無法從token中獲取司機資訊")
			return nil, huma.Error500InternalServerError("無法從token中獲取司機資訊")
		}
		driverID := driverFromToken.ID.Hex()

		// 從資料庫獲取完整的司機資料以檢查狀態
		fullDriver, err := c.driverService.GetDriverByID(ctx, driverID)
		if err != nil {
			c.logger.Warn().Err(err).Str("driver_id", driverID).Msg("找不到司機資料")
			return nil, huma.Error404NotFound("找不到司機資料", err)
		}

		// 檢查司機本身狀態是否為閒置
		if fullDriver.Status == model.DriverStatusIdle {
			c.logger.Info().Str("driver_id", driverID).Msg("司機為閒置狀態，沒有進行中的訂單")
			return nil, huma.Error404NotFound("司機為閒置狀態，沒有進行中的訂單")
		}

		// 呼叫新的 service 函式
		orderInfo, err := c.orderService.GetCurrentOrderByDriverID(ctx, driverID)
		if err != nil {
			// 這邊的錯誤很可能是 mongo.ErrNoDocuments，表示找不到符合條件的訂單
			c.logger.Warn().Err(err).Str("driver_id", driverID).Msg("找不到進行中的訂單")
			return nil, huma.Error404NotFound("找不到進行中的訂單", err)
		}
		// 構建包含司機狀態的響應
		response := &order.CurrentOrderWithDriverStatusResponse{}
		response.Body.Order = orderInfo.Order
		response.Body.DriverStatus = string(fullDriver.Status)

		return response, nil
	})

	// 司機更新位置
	huma.Register(api, huma.Operation{
		OperationID: "update-driver-location",
		Method:      "POST",
		Path:        "/drivers/update-location",
		Summary:     "司機更新經緯度",
		Description: "更新當前登入司機的經緯度。司機ID會從JWT Bearer token中自動獲取。",
		Tags:        []string{"drivers"},
		Middlewares: huma.Middlewares{c.authMiddleware.Auth()}, // 應用 JWT 驗證中間件
	}, func(ctx context.Context, input *driver.UpdateDriverLocationInput) (*driver.SimpleResponse, error) {
		// 獲取當前 span
		span := trace.SpanFromContext(ctx)

		// 創建 controller span
		controllerCtx, controllerSpan := infra.StartSpan(ctx, "api_location_update",
			infra.AttrOperation("update_driver_location"),
			infra.AttrString("location.lat", input.Body.Lat),
			infra.AttrString("location.lng", input.Body.Lng),
			infra.AttrString("connection_type", "api"),
		)
		defer controllerSpan.End()

		// 從 context 中獲取司機資訊（由中間件設置）
		infra.AddEvent(controllerSpan, "extracting_driver_from_token")
		driverFromToken, err := auth.GetDriverFromContext(controllerCtx)
		if err != nil {
			infra.RecordError(controllerSpan, err, "Failed to extract driver from token",
				infra.AttrErrorType("token_extraction_error"),
			)
			return nil, huma.Error500InternalServerError("無法從token中獲取司機Auth")
		}

		driverID := driverFromToken.ID.Hex()
		infra.AddEvent(controllerSpan, "driver_extracted_from_token")
		infra.SetAttributes(controllerSpan,
			infra.AttrDriverID(driverID),
			infra.AttrString("driver.name", driverFromToken.Name),
			infra.AttrString("driver.car_plate", driverFromToken.CarPlate),
		)

		// 調用服務層更新位置
		infra.AddEvent(controllerSpan, "calling_update_location_service")
		_, err = c.driverService.UpdateDriverLocation(controllerCtx, driverID, input.Body.Lat, input.Body.Lng)
		if err != nil {
			infra.RecordError(controllerSpan, err, "Update location service failed",
				infra.AttrDriverID(driverID),
				infra.AttrErrorType("service_error"),
			)
			infra.AddEvent(controllerSpan, "location_update_failed")
			c.logger.Error().
				Err(err).
				Str("driver_id", driverID).
				Str("trace_id", span.SpanContext().TraceID().String()).
				Str("span_id", controllerSpan.SpanContext().SpanID().String()).
				Msg("更新司機位置失敗")
			return &driver.SimpleResponse{Success: false, Message: "更新司機位置失敗"}, huma.Error500InternalServerError("更新司機位置失敗", err)
		}

		// 標記成功
		infra.AddEvent(controllerSpan, "location_update_success")
		infra.MarkSuccess(controllerSpan,
			infra.AttrDriverID(driverID),
			infra.AttrString("location.lat", input.Body.Lat),
			infra.AttrString("location.lng", input.Body.Lng),
			infra.AttrString("connection_type", "api"),
		)

		c.logger.Debug().
			Str("driver_id", driverID).
			Str("driver_name", driverFromToken.Name).
			Str("car_plate", driverFromToken.CarPlate).
			Str("lat", input.Body.Lat).
			Str("lng", input.Body.Lng).
			Str("trace_id", span.SpanContext().TraceID().String()).
			Str("span_id", controllerSpan.SpanContext().SpanID().String()).
			Msg("司機位置更新成功")

		return &driver.SimpleResponse{Success: true, Message: "司機位置已更新"}, nil
	})

	// 司機更新 FCM Token
	huma.Register(api, huma.Operation{
		OperationID: "update-driver-fcm-token",
		Method:      "POST",
		Path:        "/drivers/update-fcm-token",
		Summary:     "司機更新FCM推播令牌",
		Description: "更新當前登入司機的FCM推播令牌。司機ID會從JWT Bearer token中自動獲取。",
		Tags:        []string{"drivers"},
		Middlewares: huma.Middlewares{c.authMiddleware.Auth()}, // 應用 JWT 驗證中間件
	}, func(ctx context.Context, input *driver.UpdateFCMTokenInput) (*driver.SimpleResponse, error) {
		driverFromToken, err := auth.GetDriverFromContext(ctx)
		if err != nil {
			c.logger.Error().Err(err).Msg("無法從token中獲取司機資訊")
			return nil, huma.Error500InternalServerError("無法從token中獲取司機資訊")
		}
		driverID := driverFromToken.ID.Hex()

		c.logger.Info().Str("driver_id", driverID).Str("driver_name", driverFromToken.Name).Str("fcm_type", input.Body.FCMType).Msg("司機更新FCM Token")

		_, err = c.driverService.UpdateFCMToken(ctx, driverID, input.Body.FCMToken, input.Body.FCMType)
		if err != nil {
			c.logger.Error().Err(err).Str("driver_id", driverID).Msg("更新FCM令牌失敗")
			return &driver.SimpleResponse{Success: false, Message: "更新FCM令牌失敗"}, huma.Error500InternalServerError("更新FCM令牌失敗", err)
		}
		return &driver.SimpleResponse{Success: true, Message: "FCM令牌已更新"}, nil
	})

	// 司機更新設備資訊
	huma.Register(api, huma.Operation{
		OperationID: "update-device-info",
		Method:      "POST",
		Path:        "/drivers/update-device-info",
		Summary:     "司機更新設備資訊",
		Description: "更新當前登入司機的設備資訊，包括設備型號、名稱、品牌、製造商和應用版本。司機ID會從JWT Bearer token中自動獲取。",
		Tags:        []string{"drivers"},
		Middlewares: huma.Middlewares{c.authMiddleware.Auth()}, // 應用 JWT 驗證中間件
	}, func(ctx context.Context, input *driver.UpdateDeviceInput) (*driver.SimpleResponse, error) {
		driverFromToken, err := auth.GetDriverFromContext(ctx)
		if err != nil {
			c.logger.Error().Err(err).Msg("無法從token中獲取司機資訊")
			return nil, huma.Error500InternalServerError("無法從token中獲取司機資訊")
		}
		driverID := driverFromToken.ID.Hex()

		c.logger.Info().
			Str("driver_id", driverID).
			Str("driver_name", driverFromToken.Name).
			Str("device_model", input.Body.DeviceModelName).
			Str("device_brand", input.Body.DeviceBrand).
			Str("app_version", input.Body.DeviceAppVersion).
			Msg("司機更新設備資訊")

		err = c.driverService.UpdateDriverDeviceInfo(ctx, driverID,
			input.Body.DeviceModelName,
			input.Body.DeviceDeviceName,
			input.Body.DeviceBrand,
			input.Body.DeviceManufacturer,
			input.Body.DeviceAppVersion)
		if err != nil {
			c.logger.Error().Err(err).Str("driver_id", driverID).Msg("更新設備資訊失敗")
			return &driver.SimpleResponse{Success: false, Message: "更新設備資訊失敗"}, huma.Error500InternalServerError("更新設備資訊失敗", err)
		}
		return &driver.SimpleResponse{Success: true, Message: "設備資訊已更新"}, nil
	})

	// 更新司機狀態
	huma.Register(api, huma.Operation{
		OperationID: "update-driver-status",
		Method:      "PUT",
		Path:        "/drivers/switch-online",
		Summary:     "更新司機在線狀態",
		Tags:        []string{"drivers"},
		Middlewares: huma.Middlewares{c.authMiddleware.Auth()}, // 應用 JWT 驗證中間件
	}, func(ctx context.Context, input *driver.UpdateStatusInput) (*driver.DriverResponse, error) {
		// 從 context 中獲取司機資訊（由中間件設置）
		driverFromToken, err := auth.GetDriverFromContext(ctx)
		if err == nil {
			c.logger.Info().Str("driver_id", driverFromToken.ID.Hex()).Str("driver_name", driverFromToken.Name).Str("car_plate", driverFromToken.CarPlate).Msg("Handler 收到的司機資料")
		}

		d, err := c.driverService.UpdateDriverStatus(ctx, driverFromToken.ID.Hex(), input.Body.IsOnline)
		if err != nil {
			c.logger.Error().Err(err).Str("driver_id", driverFromToken.ID.Hex()).Bool("is_online", input.Body.IsOnline).Msg("更新司機狀態失敗")
			return nil, huma.Error400BadRequest("更新司機狀態失敗", err)
		}

		return &driver.DriverResponse{Body: d}, nil
	})

	// 司機接收訂單
	huma.Register(api, huma.Operation{
		OperationID: "accept-order",
		Method:      "POST",
		Path:        "/drivers/accept-order",
		Summary:     "司機接收訂單(1)",
		Tags:        []string{"drivers", "flow"},
		Middlewares: huma.Middlewares{c.authMiddleware.Auth()}, // 應用 JWT 驗證中間件
		Security: []map[string][]string{
			{"bearerAuth": {}},
		},
	}, func(ctx context.Context, input *driver.AcceptOrderInput) (*driver.AcceptOrderResponse, error) {
		// 創建 driver controller span
		ctx, span := infra.StartDriverControllerSpan(ctx, "accept_order",
			infra.AttrOrderID(input.Body.OrderID),
			infra.AttrInt("adjust_mins", input.Body.AdjustMins),
		)
		defer span.End()

		requestTime := time.Now()

		// 提取司機資訊
		infra.AddEvent(span, "extracting_driver_from_token")
		d, err := auth.GetDriverFromContext(ctx)
		if err != nil {
			infra.RecordDriverControllerError(span, err, "", input.Body.OrderID, "Failed to extract driver from token")
			return nil, huma.Error401Unauthorized("無效的司機Auth")
		}

		// 添加司機資訊到 span
		infra.SetAttributes(span,
			infra.AttrDriverID(d.ID.Hex()),
			infra.AttrString("driver.name", d.Name),
			infra.AttrString("driver.car_plate", d.CarPlate),
			infra.AttrString("driver.fleet", string(d.Fleet)),
		)

		infra.AddEvent(span, "accept_order_started")

		c.logger.Info().Str("driver_id", d.ID.Hex()).Str("driver_name", d.Name).Str("car_plate", d.CarPlate).Str("fleet", string(d.Fleet)).Int("adjust_mins", input.Body.AdjustMins).Str("trace_id", trace.SpanFromContext(ctx).SpanContext().TraceID().String()).Str("span_id", span.SpanContext().SpanID().String()).Msg("接單操作 - 司機接單")

		// 調用服務層處理接單
		infra.AddEvent(span, "calling_accept_order_service")
		distanceKm, estPickupMins, _, driverStatus, orderStatus, err := c.driverService.AcceptOrder(ctx, d, input.Body.OrderID, &input.Body.AdjustMins, requestTime)
		if err != nil {
			infra.RecordDriverControllerError(span, err, d.ID.Hex(), input.Body.OrderID, "Accept order service failed")
			c.logger.Error().Err(err).Str("driver_id", d.ID.Hex()).Str("order_id", input.Body.OrderID).Str("trace_id", trace.SpanFromContext(ctx).SpanContext().TraceID().String()).Str("span_id", span.SpanContext().SpanID().String()).Msg("接單失敗")
			return nil, huma.Error400BadRequest("接單失敗")
		}

		// 記錄成功操作
		infra.RecordDriverControllerSuccess(span, d.ID.Hex(), input.Body.OrderID,
			infra.AttrFloat64("distance_km", distanceKm),
			infra.AttrInt("estimated_pickup_mins", estPickupMins),
			infra.AttrString("result.driver_status", driverStatus),
			infra.AttrString("result.order_status", orderStatus),
		)

		// 轉換預估到達時間為台北時間
		taipeiLocation := time.FixedZone("Asia/Taipei", 8*3600)
		estArrivalTime := requestTime.Add(time.Duration(estPickupMins) * time.Minute).In(taipeiLocation)
		taipeiEstPickupTimeStr := estArrivalTime.Format("15:04:05")

		resp := &driver.AcceptOrderResponse{}
		resp.Body.Success = true
		resp.Body.Message = "司機已成功接單"
		resp.Body.DriverStatus = driverStatus
		resp.Body.OrderStatus = orderStatus
		resp.Body.Distance = distanceKm
		resp.Body.EstimatedTime = estPickupMins
		resp.Body.EstimatedArrivalTime = taipeiEstPickupTimeStr
		resp.Body.AcceptanceTime = requestTime.In(taipeiLocation).Format("2006-01-02T15:04:05Z07:00")

		return resp, nil
	})

	// 司機拒絕訂單
	huma.Register(api, huma.Operation{
		OperationID: "reject-order",
		Method:      "POST",
		Path:        "/drivers/reject-order",
		Summary:     "司機拒絕訂單",
		Tags:        []string{"drivers"},
		Middlewares: huma.Middlewares{c.authMiddleware.Auth()},
	}, func(ctx context.Context, input *driver.RejectOrderInput) (*driver.SimpleResponse, error) {
		d, err := auth.GetDriverFromContext(ctx)
		if err != nil {
			return nil, huma.Error401Unauthorized("無效的司機Auth")
		}
		c.logger.Info().Str("driver_id", d.ID.Hex()).Str("driver_name", d.Name).Str("car_plate", d.CarPlate).Str("order_id", input.Body.OrderID).Msg("拒單操作")

		err = c.driverService.RejectOrder(ctx, d, input.Body.OrderID)
		if err != nil {
			return nil, huma.Error500InternalServerError("拒絕訂單失敗", err)
		}

		return &driver.SimpleResponse{Success: true, Message: "司機已拒絕訂單"}, nil
	})

	// 司機回報已抵達上車點
	huma.Register(api, huma.Operation{
		OperationID: "arrive-pickup-location",
		Method:      "POST",
		Path:        "/driver/arrive-pickup-location",
		Summary:     "司機抵達時間記錄(2)",
		Description: "司機回報已抵達乘客上車點，僅記錄抵達時間和計算遲到時間，不更新狀態",
		Tags:        []string{"drivers", "flow"},
		Middlewares: huma.Middlewares{c.authMiddleware.Auth()},
	}, func(ctx context.Context, input *driver.ArrivePickupLocationInput) (*driver.ArrivePickupLocationResponse, error) {
		// 創建 driver controller span
		ctx, span := infra.StartDriverControllerSpan(ctx, "arrive_pickup_location",
			infra.AttrOrderID(input.Body.OrderID),
		)
		defer span.End()

		requestTime := time.Now()

		// 提取司機資訊
		infra.AddEvent(span, "extracting_driver_from_token")
		d, err := auth.GetDriverFromContext(ctx)
		if err != nil {
			infra.RecordDriverControllerError(span, err, "", input.Body.OrderID, "Failed to extract driver from token")
			return nil, huma.Error401Unauthorized("無效的司機Auth")
		}

		// 添加司機資訊到 span
		infra.SetAttributes(span,
			infra.AttrDriverID(d.ID.Hex()),
			infra.AttrString("driver.name", d.Name),
			infra.AttrString("driver.car_plate", d.CarPlate),
		)

		infra.AddEvent(span, "arrive_pickup_location_started")

		c.logger.Info().Str("driver_name", d.Name).Str("car_plate", d.CarPlate).Str("order_id", input.Body.OrderID).Str("trace_id", trace.SpanFromContext(ctx).SpanContext().TraceID().String()).Str("span_id", span.SpanContext().SpanID().String()).Msg("司機回報抵達上車點")

		// 使用服務層處理業務邏輯
		infra.AddEvent(span, "calling_arrive_pickup_location_service")
		driverStatus, orderStatus, deviationSecs, arrivalTime, err := c.driverService.ArrivePickupLocation(ctx, d, input.Body.OrderID, requestTime)
		if err != nil {
			infra.RecordDriverControllerError(span, err, d.ID.Hex(), input.Body.OrderID, "Arrive pickup location service failed")
			c.logger.Error().Err(err).Str("driver_id", d.ID.Hex()).Str("order_id", input.Body.OrderID).Str("trace_id", trace.SpanFromContext(ctx).SpanContext().TraceID().String()).Str("span_id", span.SpanContext().SpanID().String()).Msg("處理司機抵達失敗")
			return nil, huma.Error500InternalServerError("處理司機抵達失敗", err)
		}

		// 記錄成功操作
		infra.RecordDriverControllerSuccess(span, d.ID.Hex(), input.Body.OrderID,
			infra.AttrString("result.driver_status", driverStatus),
			infra.AttrString("result.order_status", orderStatus),
		)

		resp := &driver.ArrivePickupLocationResponse{}
		resp.Body.Success = true
		resp.Body.Message = "已成功回報抵達上車點"
		resp.Body.DriverStatus = driverStatus
		resp.Body.OrderStatus = orderStatus
		resp.Body.DeviationSecs = deviationSecs
		resp.Body.ArrivalTime = arrivalTime.Format("2006-01-02T15:04:05Z07:00")

		return resp, nil
	})

	// 司機上傳抵達證明
	huma.Register(api, huma.Operation{
		OperationID: "upload-pickup-certificate",
		Method:      "POST",
		Path:        "/drivers/orders/{orderId}/upload-pickup-certificate",
		Summary:     "司機上傳抵達證明(2.1)",
		Description: "司機上傳照片作為抵達乘客上車點的證明。此操作會立即回應，並在背景處理檔案儲存。",
		Tags:        []string{"drivers", "flow"},
		Middlewares: huma.Middlewares{c.authMiddleware.Auth()},
	}, func(ctx context.Context, input *driver.UploadPickupCertificateInput) (*driver.DriverArrivalResponse, error) {
		// 創建 driver controller span
		ctx, span := infra.StartDriverControllerSpan(ctx, "upload_pickup_certificate",
			infra.AttrOrderID(input.OrderID),
		)
		defer span.End()

		requestTime := time.Now()

		// 提取司機資訊
		infra.AddEvent(span, "extracting_driver_from_token")
		d, err := auth.GetDriverFromContext(ctx)
		if err != nil {
			infra.RecordDriverControllerError(span, err, "", input.OrderID, "Failed to extract driver from token")
			return nil, huma.Error401Unauthorized("無效的司機Auth")
		}

		// 添加司機資訊到 span
		infra.SetAttributes(span,
			infra.AttrDriverID(d.ID.Hex()),
			infra.AttrString("driver.name", d.Name),
			infra.AttrString("driver.car_plate", d.CarPlate),
		)

		infra.AddEvent(span, "upload_pickup_certificate_started",
			infra.AttrDriverID(d.ID.Hex()),
			infra.AttrOrderID(input.OrderID),
		)

		// 檢查檔案
		infra.AddEvent(span, "checking_certificate_file")
		files, ok := input.RawBody.File["certificate"]
		if !ok || len(files) == 0 {
			infra.RecordError(span, nil, "Missing certificate file",
				infra.AttrErrorType("missing_file"),
			)
			c.logger.Warn().Str("driver_id", d.ID.Hex()).Str("trace_id", span.SpanContext().TraceID().String()).Str("span_id", span.SpanContext().SpanID().String()).Msg("缺少名為 'certificate' 的照片檔案")
			return nil, huma.Error400BadRequest("缺少名為 'certificate' 的照片檔案")
		}
		certFileHeader := files[0]

		infra.AddEvent(span, "opening_certificate_file",
			infra.AttrString("filename", certFileHeader.Filename),
			infra.AttrInt("file_size", int(certFileHeader.Size)),
		)

		file, err := certFileHeader.Open()
		if err != nil {
			infra.RecordError(span, err, "Failed to open certificate file",
				infra.AttrErrorType("file_open_error"),
				infra.AttrString("filename", certFileHeader.Filename),
			)
			c.logger.Error().Err(err).Str("driver_id", d.ID.Hex()).Str("trace_id", span.SpanContext().TraceID().String()).Str("span_id", span.SpanContext().SpanID().String()).Msg("無法開啟上傳的檔案")
			return nil, huma.Error500InternalServerError("伺服器錯誤，無法處理檔案")
		}
		defer func() {
			if closeErr := file.Close(); closeErr != nil {
				c.logger.Warn().Err(closeErr).Str("driver_id", d.ID.Hex()).Msg("關閉檔案時發生錯誤")
			}
		}()

		// 同步處理文件上傳和抵達邏輯
		infra.AddEvent(span, "calling_certificate_upload_service")
		driverStatus, orderStatus, err := c.driverService.HandlePickupCertificateUpload(ctx, d, input.OrderID, file, certFileHeader, requestTime, c.fileStorageService)
		if err != nil {
			infra.RecordError(span, err, "Certificate upload service failed",
				infra.AttrDriverID(d.ID.Hex()),
				infra.AttrOrderID(input.OrderID),
				infra.AttrErrorType("service_error"),
			)
			infra.AddEvent(span, "upload_pickup_certificate_failed")
			c.logger.Error().Err(err).Str("driver_id", d.ID.Hex()).Str("order_id", input.OrderID).Str("trace_id", span.SpanContext().TraceID().String()).Str("span_id", span.SpanContext().SpanID().String()).Msg("處理抵達證明上傳失敗")
			return nil, huma.Error500InternalServerError("處理抵達證明上傳失敗")
		}

		// 標記成功
		infra.AddEvent(span, "upload_pickup_certificate_success",
			infra.AttrString("driver_status", driverStatus),
			infra.AttrString("order_status", orderStatus),
			infra.AttrString("filename", certFileHeader.Filename),
		)
		infra.MarkSuccess(span,
			infra.AttrDriverID(d.ID.Hex()),
			infra.AttrOrderID(input.OrderID),
			infra.AttrString("driver_status", driverStatus),
			infra.AttrString("order_status", orderStatus),
		)

		resp := &driver.DriverArrivalResponse{}
		resp.Body.Success = true
		resp.Body.Message = "抵達證明上傳成功"
		resp.Body.DriverStatus = driverStatus
		resp.Body.OrderStatus = orderStatus
		resp.Body.IsPhotoTaken = true

		return resp, nil
	})

	// 司機回報已抵達上車點（不需要圖片）
	huma.Register(api, huma.Operation{
		OperationID: "skip-pickup-certificate",
		Method:      "POST",
		Path:        "/drivers/orders/{orderId}/skip-pickup-certificate",
		Summary:     "司機回報已抵達上車點-略過照片(2.2)",
		Description: "司機回報已抵達乘客上車點，此操作僅記錄抵達動作。",
		Tags:        []string{"drivers", "flow"},
		Middlewares: huma.Middlewares{c.authMiddleware.Auth()},
	}, func(ctx context.Context, input *driver.OrderIDInput) (*driver.DriverArrivalResponse, error) {
		// 獲取當前 span
		span := trace.SpanFromContext(ctx)

		// 創建略過證明 span
		skipCtx, skipSpan := infra.StartSpan(ctx, "skip_pickup_certificate_controller",
			infra.AttrOperation("skip_pickup_certificate"),
			infra.AttrOrderID(input.OrderID),
		)
		defer skipSpan.End()

		// 提取司機資訊
		infra.AddEvent(skipSpan, "extracting_driver_from_token")
		d, err := auth.GetDriverFromContext(skipCtx)
		if err != nil {
			infra.RecordError(skipSpan, err, "Failed to extract driver from token",
				infra.AttrErrorType("token_extraction_error"),
			)
			c.logger.Warn().Err(err).Str("trace_id", span.SpanContext().TraceID().String()).Str("span_id", skipSpan.SpanContext().SpanID().String()).Msg("無效的司機憑證")
			return nil, huma.Error401Unauthorized("無效的司機憑證")
		}

		// 添加司機資訊到 span
		infra.SetAttributes(skipSpan,
			infra.AttrDriverID(d.ID.Hex()),
			infra.AttrString("driver.name", d.Name),
			infra.AttrString("driver.car_plate", d.CarPlate),
		)

		infra.AddEvent(skipSpan, "skip_pickup_certificate_started",
			infra.AttrDriverID(d.ID.Hex()),
			infra.AttrOrderID(input.OrderID),
			infra.AttrString("driver_name", d.Name),
			infra.AttrString("car_plate", d.CarPlate),
		)

		c.logger.Info().Str("driver_name", d.Name).Str("car_plate", d.CarPlate).Str("order_id", input.OrderID).Str("trace_id", span.SpanContext().TraceID().String()).Str("span_id", skipSpan.SpanContext().SpanID().String()).Msg("司機回報抵達上車點")

		// 使用統一的司機抵達處理邏輯（無照片場景）
		infra.AddEvent(skipSpan, "calling_driver_arrival_service")
		driverStatus, orderStatus, err := c.driverService.HandleDriverArrival(skipCtx, d, input.OrderID, "")
		if err != nil {
			infra.RecordError(skipSpan, err, "Driver arrival service failed",
				infra.AttrDriverID(d.ID.Hex()),
				infra.AttrOrderID(input.OrderID),
				infra.AttrErrorType("service_error"),
			)
			infra.AddEvent(skipSpan, "skip_pickup_certificate_failed")
			c.logger.Error().Err(err).Str("driver_id", d.ID.Hex()).Str("order_id", input.OrderID).Str("trace_id", span.SpanContext().TraceID().String()).Str("span_id", skipSpan.SpanContext().SpanID().String()).Msg("處理司機抵達報告失敗")
			return nil, huma.Error500InternalServerError("處理司機抵達報告失敗")
		}

		// 標記成功
		infra.AddEvent(skipSpan, "skip_pickup_certificate_success",
			infra.AttrString("driver_status", driverStatus),
			infra.AttrString("order_status", orderStatus),
		)
		infra.MarkSuccess(skipSpan,
			infra.AttrDriverID(d.ID.Hex()),
			infra.AttrOrderID(input.OrderID),
			infra.AttrString("driver_status", driverStatus),
			infra.AttrString("order_status", orderStatus),
		)

		resp := &driver.DriverArrivalResponse{}
		resp.Body.Success = true
		resp.Body.Message = "已成功回報抵達上車點"
		resp.Body.DriverStatus = driverStatus
		resp.Body.OrderStatus = orderStatus
		resp.Body.IsPhotoTaken = true

		return resp, nil
	})

	// 司機回報客人已上車
	huma.Register(api, huma.Operation{
		OperationID: "onboard-customer",
		Method:      "POST",
		Path:        "/drivers/orders/{orderId}/onboard",
		Summary:     "司機回報客人已上車(3)",
		Description: "司機回報已接到乘客，此操作將更新訂單狀態為「客上」。",
		Tags:        []string{"drivers", "flow"},
		Middlewares: huma.Middlewares{c.authMiddleware.Auth()},
	}, func(ctx context.Context, input *driver.PickupCustomerInput) (*driver.PickupCustomerResponse, error) {
		// 獲取當前 span
		span := trace.SpanFromContext(ctx)

		// 創建業務 span
		onboardCtx, onboardSpan := infra.StartSpan(ctx, "onboard_customer_controller",
			infra.AttrOperation("onboard_customer"),
			infra.AttrOrderID(input.OrderID),
		)
		defer onboardSpan.End()

		// 添加客人上車開始事件
		infra.AddEvent(onboardSpan, "customer_onboard_started",
			infra.AttrOrderID(input.OrderID),
		)

		requestTime := time.Now()

		d, err := auth.GetDriverFromContext(ctx)
		if err != nil {
			// 記錄錯誤到 span
			infra.RecordError(onboardSpan, err, "Driver authentication failed",
				infra.AttrString("error", err.Error()),
			)
			infra.AddEvent(onboardSpan, "driver_auth_failed",
				infra.AttrString("error", err.Error()),
			)
			return nil, huma.Error401Unauthorized("無效的司機Auth")
		}

		// 添加司機信息到 span
		infra.SetAttributes(onboardSpan,
			infra.AttrDriverID(d.ID.Hex()),
			infra.AttrDriverAccount(d.Account),
			infra.AttrString("driver.name", d.Name),
		)

		// 添加服務調用事件
		infra.AddEvent(onboardSpan, "calling_pickup_customer_service",
			infra.AttrOrderID(input.OrderID),
			infra.AttrBool("has_meter_jump", input.Body.HasMeterJump),
		)

		driverStatus, orderStatus, err := c.driverService.PickUpCustomer(onboardCtx, d, input.OrderID, input.Body.HasMeterJump, requestTime)
		if err != nil {
			// 記錄錯誤到 span
			infra.RecordError(onboardSpan, err, "Customer onboard failed",
				infra.AttrOrderID(input.OrderID),
				infra.AttrDriverID(d.ID.Hex()),
				infra.AttrString("error", err.Error()),
			)
			infra.AddEvent(onboardSpan, "customer_onboard_failed",
				infra.AttrOrderID(input.OrderID),
				infra.AttrString("error", err.Error()),
			)

			c.logger.Error().Err(err).
				Str("driver_name", d.Name).
				Str("order_id", input.OrderID).
				Str("trace_id", span.SpanContext().TraceID().String()).
				Str("span_id", onboardSpan.SpanContext().SpanID().String()).
				Msg("司機回報訂單上車失敗")
			return nil, huma.Error500InternalServerError("處理客人上車失敗", err)
		}

		// 添加成功事件
		infra.AddEvent(onboardSpan, "customer_onboard_success",
			infra.AttrOrderID(input.OrderID),
			infra.AttrDriverID(d.ID.Hex()),
			infra.AttrString("driver_status", driverStatus),
			infra.AttrString("order_status", orderStatus),
			infra.AttrBool("has_meter_jump", input.Body.HasMeterJump),
		)

		// 設置成功的 span 屬性
		infra.MarkSuccess(onboardSpan,
			infra.AttrString("result.driver_status", driverStatus),
			infra.AttrString("result.order_status", orderStatus),
			infra.AttrBool("meter_jumped", input.Body.HasMeterJump),
		)

		c.logger.Info().
			Str("driver_name", d.Name).
			Str("car_plate", d.CarPlate).
			Str("order_id", input.OrderID).
			Str("driver_status", driverStatus).
			Str("order_status", orderStatus).
			Str("trace_id", span.SpanContext().TraceID().String()).
			Str("span_id", onboardSpan.SpanContext().SpanID().String()).
			Msg("司機回報訂單客人上車")

		resp := &driver.PickupCustomerResponse{}
		resp.Body.Success = true
		resp.Body.Message = "訂單狀態已更新為客上"
		resp.Body.DriverStatus = driverStatus
		resp.Body.OrderStatus = orderStatus
		resp.Body.HasMeterJump = input.Body.HasMeterJump
		resp.Body.PickupTime = requestTime.Format("2006-01-02T15:04:05Z07:00")

		return resp, nil
	})

	// 司機完成訂單
	huma.Register(api, huma.Operation{
		OperationID: "complete-order",
		Method:      "POST",
		Path:        "/drivers/complete-order",
		Summary:     "司機完成訂單(4)",
		Tags:        []string{"drivers", "flow"},
		Middlewares: huma.Middlewares{c.authMiddleware.Auth()},
	}, func(ctx context.Context, input *driver.CompleteOrderInput) (*driver.CompleteOrderResponse, error) {
		// 獲取當前 span
		span := trace.SpanFromContext(ctx)

		// 創建業務 span
		completeCtx, completeSpan := infra.StartSpan(ctx, "complete_order_controller",
			infra.AttrOperation("complete_order"),
			infra.AttrOrderID(input.Body.OrderID),
			infra.AttrInt("duration_minutes", input.Body.Duration),
		)
		defer completeSpan.End()

		// 添加完成訂單開始事件
		infra.AddEvent(completeSpan, "order_completion_started",
			infra.AttrOrderID(input.Body.OrderID),
			infra.AttrInt("duration", input.Body.Duration),
		)

		requestTime := time.Now()

		d, err := auth.GetDriverFromContext(ctx)
		if err != nil {
			// 記錄錯誤到 span
			infra.RecordError(completeSpan, err, "Driver authentication failed",
				infra.AttrString("error", err.Error()),
			)
			infra.AddEvent(completeSpan, "driver_auth_failed",
				infra.AttrString("error", err.Error()),
			)
			return nil, huma.Error401Unauthorized("無效的司機Auth")
		}

		// 添加司機信息到 span
		infra.SetAttributes(completeSpan,
			infra.AttrDriverID(d.ID.Hex()),
			infra.AttrDriverAccount(d.Account),
			infra.AttrString("driver.name", d.Name),
		)

		c.logger.Info().
			Str("driver_name", d.Name).
			Str("driver_id", d.ID.Hex()).
			Str("trace_id", span.SpanContext().TraceID().String()).
			Str("span_id", completeSpan.SpanContext().SpanID().String()).
			Msg("完成訂單操作")

		// 添加服務調用事件
		infra.AddEvent(completeSpan, "calling_complete_order_service",
			infra.AttrOrderID(input.Body.OrderID),
			infra.AttrInt("duration", input.Body.Duration),
		)

		driverStatus, orderStatus, err := c.driverService.CompleteOrder(completeCtx, d, input.Body.OrderID, input.Body.Duration, requestTime)
		if err != nil {
			// 記錄錯誤到 span
			infra.RecordError(completeSpan, err, "Order completion failed",
				infra.AttrOrderID(input.Body.OrderID),
				infra.AttrDriverID(d.ID.Hex()),
				infra.AttrString("error", err.Error()),
			)
			infra.AddEvent(completeSpan, "order_completion_failed",
				infra.AttrOrderID(input.Body.OrderID),
				infra.AttrString("error", err.Error()),
			)
			return nil, huma.Error500InternalServerError("完成訂單失敗", err)
		}

		// 添加成功事件
		infra.AddEvent(completeSpan, "order_completion_success",
			infra.AttrOrderID(input.Body.OrderID),
			infra.AttrDriverID(d.ID.Hex()),
			infra.AttrString("driver_status", driverStatus),
			infra.AttrString("order_status", orderStatus),
			infra.AttrInt("duration", input.Body.Duration),
		)

		// 設置成功的 span 屬性
		infra.MarkSuccess(completeSpan,
			infra.AttrString("result.driver_status", driverStatus),
			infra.AttrString("result.order_status", orderStatus),
			infra.AttrInt("trip.duration_minutes", input.Body.Duration),
		)

		resp := &driver.CompleteOrderResponse{}
		resp.Body.Success = true
		resp.Body.Message = "司機已完成訂單"
		resp.Body.DriverStatus = driverStatus
		resp.Body.OrderStatus = orderStatus
		resp.Body.CompleteTime = requestTime.Format("2006-01-02T15:04:05Z07:00")

		return resp, nil
	})

	// 司機取得歷史訂單
	huma.Register(api, huma.Operation{
		OperationID: "get-driver-orders",
		Method:      "GET",
		Path:        "/drivers/orders",
		Summary:     "取得司機歷史訂單",
		Tags:        []string{"drivers"},
		Middlewares: huma.Middlewares{c.authMiddleware.Auth()},
	}, func(ctx context.Context, input *driver.GetDriverOrdersInput) (*driver.PaginatedOrdersResponse, error) {
		d, ok := ctx.Value("driver").(*model.DriverInfo)
		if !ok {
			c.logger.Error().Msg("無法從token中獲取司機資訊")
			return nil, huma.Error500InternalServerError("無法從token中獲取司機資訊")
		}
		orders, total, err := c.driverService.GetOrdersByDriverID(ctx, d.ID.Hex(), input.GetPageNum(), input.GetPageSize())
		if err != nil {
			c.logger.Error().Err(err).Str("driver_id", d.ID.Hex()).Msg("查詢歷史訂單失敗")
			return nil, huma.Error500InternalServerError("查詢歷史訂單失敗", err)
		}

		resp := &driver.PaginatedOrdersResponse{}
		resp.Body.Orders = orders
		resp.Body.Pagination = common.NewPaginationInfo(input.GetPageNum(), input.GetPageSize(), total)

		return resp, nil
	})

	// 創建司機的 Traffic Usage Log
	huma.Register(api, huma.Operation{
		OperationID: "create-driver-traffic-usage-log",
		Method:      "POST",
		Path:        "/drivers/traffic-logs",
		Summary:     "建立司機的 Map Usage Log",
		Tags:        []string{"drivers"},
		Middlewares: huma.Middlewares{c.authMiddleware.Auth()}, // Driver auth
	}, func(ctx context.Context, input *driver.CreateDriverTrafficLogInput) (*driver.CreateDriverTrafficLogResponse, error) {
		d, err := auth.GetDriverFromContext(ctx)
		if err != nil {
			c.logger.Warn().Err(err).Msg("無效的司機憑證")
			return nil, huma.Error401Unauthorized("無效的司機憑證")
		}

		logEntry := model.TrafficUsageLog{
			Service:   input.Body.Service,
			API:       input.Body.API,
			Params:    input.Body.Params,
			Fleet:     string(d.Fleet),
			CreatedBy: d.Name,
		}

		createdLog, err := c.driverService.CreateTrafficUsageLog(ctx, &logEntry)
		if err != nil {
			c.logger.Error().Err(err).Str("driver_id", d.ID.Hex()).Msg("建立 Traffic Usage Log 失敗")
			return nil, huma.Error400BadRequest("建立 Traffic Usage Log 失敗", err)
		}

		return &driver.CreateDriverTrafficLogResponse{Body: createdLog}, nil
	})

	// 司機取得歷史訂單（新端點）
	huma.Register(api, huma.Operation{
		OperationID: "driver-get-history-order",
		Method:      "GET",
		Path:        "/drivers/history-orders",
		Summary:     "取得司機完整歷史訂單",
		Description: "司機使用Bearer token獲取自己的歷史訂單，包含完整的分派資訊。此端點與 get-driver-orders 相獨立，提供完整的訂單資料渲染。",
		Tags:        []string{"drivers"},
		Middlewares: huma.Middlewares{c.authMiddleware.Auth()},
	}, func(ctx context.Context, input *driver.GetDriverHistoryOrdersInput) (*driver.DriverHistoryOrdersResponse, error) {
		d, ok := ctx.Value("driver").(*model.DriverInfo)
		if !ok {
			c.logger.Error().Msg("無法從token中獲取司機資訊")
			return nil, huma.Error500InternalServerError("無法從token中獲取司機資訊")
		}

		driverID := d.ID.Hex()
		orders, total, err := c.driverService.GetHistoryOrdersByDriverID(ctx, driverID, input.GetPageNum(), input.GetPageSize())
		if err != nil {
			c.logger.Error().Err(err).Str("driver_id", driverID).Msg("查詢司機歷史訂單失敗")
			return nil, huma.Error500InternalServerError("查詢司機歷史訂單失敗", err)
		}

		c.logger.Info().Str("driver_id", driverID).Str("driver_name", d.Name).Int("total_orders", int(total)).Int("page_num", input.GetPageNum()).Int("page_size", input.GetPageSize()).Msg("司機成功獲取歷史訂單")

		resp := &driver.DriverHistoryOrdersResponse{}
		resp.Body.Orders = orders
		resp.Body.Pagination = common.NewPaginationInfo(input.GetPageNum(), input.GetPageSize(), total)

		return resp, nil
	})

	// Check Notifying Orders
	huma.Register(api, huma.Operation{
		OperationID: "check-notifying-order",
		Method:      "GET",
		Path:        "/driver/check-notifying-order",
		Summary:     "檢查司機通知中訂單",
		Description: "查詢當前登入司機是否有待回應的訂單推送通知，包含剩餘回應時間和完整訂單資訊。司機ID從JWT Bearer token中自動獲取。",
		Tags:        []string{"drivers"},
		Middlewares: huma.Middlewares{c.authMiddleware.Auth()},
		Security: []map[string][]string{
			{"bearerAuth": {}},
		},
	}, func(ctx context.Context, input *driver.CheckNotifyingOrderInput) (*driver.CheckNotifyingOrderResponse, error) {
		// 從 context 中獲取司機資訊（由中間件設置）
		driverFromToken, err := auth.GetDriverFromContext(ctx)
		if err != nil {
			c.logger.Error().Err(err).Msg("無法從token中獲取司機資訊")
			errorResponse := common.ErrorResponse[driver.CheckNotifyingOrderData]("無法從token中獲取司機資訊", "認證失敗")
			return &driver.CheckNotifyingOrderResponse{Body: *errorResponse}, nil
		}
		driverID := driverFromToken.ID.Hex()

		// 檢查通知中訂單
		data, err := c.driverService.CheckNotifyingOrder(ctx, driverID)
		if err != nil {
			c.logger.Error().Err(err).
				Str("driver_id", driverID).
				Str("driver_name", driverFromToken.Name).
				Msg("檢查通知中訂單失敗")

			errorResponse := common.ErrorResponse[driver.CheckNotifyingOrderData]("檢查通知中訂單失敗", err.Error())
			return &driver.CheckNotifyingOrderResponse{Body: *errorResponse}, nil
		}

		c.logger.Debug().
			Str("driver_id", driverID).
			Str("driver_name", driverFromToken.Name).
			Bool("has_notifying", data.HasNotifyingOrder).
			Msg("檢查通知中訂單成功")

		successResponse := common.SuccessResponse("檢查通知中訂單成功", data)
		return &driver.CheckNotifyingOrderResponse{Body: *successResponse}, nil
	})

	// Check Canceling Orders
	huma.Register(api, huma.Operation{
		OperationID: "check-canceling-order",
		Method:      "GET",
		Path:        "/driver/check-canceling-order",
		Summary:     "檢查司機取消中訂單",
		Description: "查詢當前登入司機是否有被取消的訂單通知，包含剩餘顯示時間和完整訂單資訊。司機ID從JWT Bearer token中自動獲取。",
		Tags:        []string{"drivers"},
		Middlewares: huma.Middlewares{c.authMiddleware.Auth()},
		Security: []map[string][]string{
			{"bearerAuth": {}},
		},
	}, func(ctx context.Context, input *driver.CheckCancelingOrderInput) (*driver.CheckCancelingOrderResponse, error) {
		// 從 context 中獲取司機資訊（由中間件設置）
		driverFromToken, err := auth.GetDriverFromContext(ctx)
		if err != nil {
			c.logger.Error().Err(err).Msg("無法從token中獲取司機資訊")
			errorResponse := common.ErrorResponse[driver.CheckCancelingOrderData]("無法從token中獲取司機資訊", "認證失敗")
			return &driver.CheckCancelingOrderResponse{Body: *errorResponse}, nil
		}
		driverID := driverFromToken.ID.Hex()

		// 檢查取消中訂單
		data, err := c.driverService.CheckCancelingOrder(ctx, driverID)
		if err != nil {
			c.logger.Error().Err(err).
				Str("driver_id", driverID).
				Str("driver_name", driverFromToken.Name).
				Msg("檢查取消中訂單失敗")

			errorResponse := common.ErrorResponse[driver.CheckCancelingOrderData]("檢查取消中訂單失敗", err.Error())
			return &driver.CheckCancelingOrderResponse{Body: *errorResponse}, nil
		}

		c.logger.Debug().
			Str("driver_id", driverID).
			Str("driver_name", driverFromToken.Name).
			Bool("has_canceling", data.HasCancelingOrder).
			Msg("檢查取消中訂單成功")

		successResponse := common.SuccessResponse("檢查取消中訂單成功", data)
		return &driver.CheckCancelingOrderResponse{Body: *successResponse}, nil
	})

	// 上傳司機頭像
	huma.Register(api, huma.Operation{
		OperationID: "upload-driver-avatar",
		Method:      "POST",
		Path:        "/drivers/upload-avatar",
		Summary:     "上傳司機頭像",
		Description: "上傳司機頭像圖片文件，支援 multipart/form-data",
		Tags:        []string{"drivers"},
		Middlewares: huma.Middlewares{c.authMiddleware.Auth()},
		Security: []map[string][]string{
			{"bearerAuth": {}},
		},
	}, c.handleUploadAvatar)

}

// 頭像上傳相關的請求和回應結構體

type UploadAvatarRequest struct {
	RawBody huma.MultipartFormFiles[struct {
		Avatar huma.FormFile `form:"avatar" required:"true" doc:"頭像圖片文件"`
	}]
}

type AvatarUploadResponse struct {
	Body struct {
		Success bool `json:"success"`
		Data    struct {
			AvatarURL string `json:"avatar_url" doc:"頭像完整URL"`
			DriverID  string `json:"driver_id" doc:"司機ID"`
		} `json:"data,omitempty"`
		Error *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error,omitempty"`
	}
}

// handleUploadAvatar 處理司機頭像上傳
func (c *DriverController) handleUploadAvatar(ctx context.Context, req *UploadAvatarRequest) (*AvatarUploadResponse, error) {
	// 從 JWT token 獲取司機信息
	driverFromToken, err := auth.GetDriverFromContext(ctx)
	if err != nil {
		c.logger.Error().Err(err).Msg("無法從token中獲取司機資訊")
		return &AvatarUploadResponse{
			Body: struct {
				Success bool `json:"success"`
				Data    struct {
					AvatarURL string `json:"avatar_url" doc:"頭像完整URL"`
					DriverID  string `json:"driver_id" doc:"司機ID"`
				} `json:"data,omitempty"`
				Error *struct {
					Code    string `json:"code"`
					Message string `json:"message"`
				} `json:"error,omitempty"`
			}{
				Success: false,
				Error: &struct {
					Code    string `json:"code"`
					Message string `json:"message"`
				}{
					Code:    "AUTH_ERROR",
					Message: "無法獲取司機資訊",
				},
			},
		}, nil
	}

	// 獲取表單數據
	formData := req.RawBody.Data()

	c.logger.Info().
		Str("driver_id", driverFromToken.ID.Hex()).
		Str("car_plate", driverFromToken.CarPlate).
		Msg("收到頭像上傳請求")

	// 檢查文件是否存在
	if !formData.Avatar.IsSet {
		c.logger.Error().Str("driver_id", driverFromToken.ID.Hex()).Msg("沒有上傳頭像文件")
		return &AvatarUploadResponse{
			Body: struct {
				Success bool `json:"success"`
				Data    struct {
					AvatarURL string `json:"avatar_url" doc:"頭像完整URL"`
					DriverID  string `json:"driver_id" doc:"司機ID"`
				} `json:"data,omitempty"`
				Error *struct {
					Code    string `json:"code"`
					Message string `json:"message"`
				} `json:"error,omitempty"`
			}{
				Success: false,
				Error: &struct {
					Code    string `json:"code"`
					Message string `json:"message"`
				}{
					Code:    "FILE_NOT_FOUND",
					Message: "沒有上傳頭像文件",
				},
			},
		}, nil
	}

	c.logger.Info().
		Str("filename", formData.Avatar.Filename).
		Int64("size", formData.Avatar.Size).
		Str("content_type", formData.Avatar.ContentType).
		Msg("開始處理頭像上傳")

	// 創建一個虛擬的FileHeader來兼容現有的FileStorageService接口
	fileHeader := &multipart.FileHeader{
		Filename: formData.Avatar.Filename,
		Size:     formData.Avatar.Size,
	}

	// 如果司機已有頭像，先刪除舊文件
	if driverFromToken.AvatarPath != nil && *driverFromToken.AvatarPath != "" {
		if err := c.fileStorageService.DeleteFile(ctx, *driverFromToken.AvatarPath); err != nil {
			c.logger.Warn().
				Err(err).
				Str("old_avatar_path", *driverFromToken.AvatarPath).
				Msg("刪除舊頭像文件失敗，繼續上傳新文件")
		} else {
			c.logger.Info().
				Str("old_avatar_path", *driverFromToken.AvatarPath).
				Msg("成功刪除舊頭像文件")
		}
	}

	// 上傳頭像文件
	result, err := c.fileStorageService.UploadAvatarFile(ctx, formData.Avatar, fileHeader, driverFromToken.ID.Hex(), driverFromToken.CarPlate)
	if err != nil {
		c.logger.Error().
			Err(err).
			Str("driver_id", driverFromToken.ID.Hex()).
			Str("filename", formData.Avatar.Filename).
			Msg("頭像文件上傳失敗")

		return &AvatarUploadResponse{
			Body: struct {
				Success bool `json:"success"`
				Data    struct {
					AvatarURL string `json:"avatar_url" doc:"頭像完整URL"`
					DriverID  string `json:"driver_id" doc:"司機ID"`
				} `json:"data,omitempty"`
				Error *struct {
					Code    string `json:"code"`
					Message string `json:"message"`
				} `json:"error,omitempty"`
			}{
				Success: false,
				Error: &struct {
					Code    string `json:"code"`
					Message string `json:"message"`
				}{
					Code:    "UPLOAD_FAILED",
					Message: fmt.Sprintf("頭像文件上傳失敗: %s", err.Error()),
				},
			},
		}, nil
	}

	// 更新司機的頭像路徑到資料庫
	if err := c.driverService.UpdateDriverAvatarPath(ctx, driverFromToken.ID.Hex(), result.RelativePath); err != nil {
		c.logger.Error().
			Err(err).
			Str("driver_id", driverFromToken.ID.Hex()).
			Str("relative_path", result.RelativePath).
			Msg("更新司機頭像路徑到資料庫失敗")

		// 刪除已上傳的文件
		c.fileStorageService.DeleteFile(ctx, result.RelativePath)

		return &AvatarUploadResponse{
			Body: struct {
				Success bool `json:"success"`
				Data    struct {
					AvatarURL string `json:"avatar_url" doc:"頭像完整URL"`
					DriverID  string `json:"driver_id" doc:"司機ID"`
				} `json:"data,omitempty"`
				Error *struct {
					Code    string `json:"code"`
					Message string `json:"message"`
				} `json:"error,omitempty"`
			}{
				Success: false,
				Error: &struct {
					Code    string `json:"code"`
					Message string `json:"message"`
				}{
					Code:    "DATABASE_UPDATE_FAILED",
					Message: "更新司機頭像路徑失敗",
				},
			},
		}, nil
	}

	// 使用 DriverService 的 GetDriverAvatarURL 方法生成正確的完整URL
	fullURL := c.driverService.GetDriverAvatarURL(&result.RelativePath, c.baseURL)

	c.logger.Info().
		Str("driver_id", driverFromToken.ID.Hex()).
		Str("relative_path", result.RelativePath).
		Str("full_url", fullURL).
		Msg("司機頭像上傳成功")

	return &AvatarUploadResponse{
		Body: struct {
			Success bool `json:"success"`
			Data    struct {
				AvatarURL string `json:"avatar_url" doc:"頭像完整URL"`
				DriverID  string `json:"driver_id" doc:"司機ID"`
			} `json:"data,omitempty"`
			Error *struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error,omitempty"`
		}{
			Success: true,
			Data: struct {
				AvatarURL string `json:"avatar_url" doc:"頭像完整URL"`
				DriverID  string `json:"driver_id" doc:"司機ID"`
			}{
				AvatarURL: fullURL,
				DriverID:  driverFromToken.ID.Hex(),
			},
		},
	}, nil
}
