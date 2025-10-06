package controller

import (
	"context"
	"right-backend/auth"
	"right-backend/data-models/common"
	"right-backend/data-models/order"
	"right-backend/infra"
	"right-backend/middleware"
	"right-backend/model"
	"right-backend/service"

	"github.com/danielgtaylor/huma/v2"
	"github.com/rs/zerolog"
)

type OrderController struct {
	logger              zerolog.Logger
	orderService        *service.OrderService
	driverService       *service.DriverService
	authMiddleware      *middleware.UserAuthMiddleware
	notificationService *service.NotificationService
}

func NewOrderController(logger zerolog.Logger, orderService *service.OrderService, driverService *service.DriverService, authMiddleware *middleware.UserAuthMiddleware, notificationService *service.NotificationService) *OrderController {
	return &OrderController{
		logger:              logger.With().Str("module", "order_controller").Logger(),
		orderService:        orderService,
		driverService:       driverService,
		authMiddleware:      authMiddleware,
		notificationService: notificationService,
	}
}

func (c *OrderController) RegisterRoutes(api huma.API) {
	// 創建訂單
	huma.Register(api, huma.Operation{
		OperationID: "create-order",
		Method:      "POST",
		Path:        "/orders",
		Summary:     "創建新訂單(輸入版)",
		Tags:        []string{"orders"},
	}, func(ctx context.Context, input *order.CreateOrderInput) (*order.OrderResponse, error) {
		o, err := c.orderService.CreateOrder(ctx, &input.Body)
		if err != nil {
			c.logger.Error().Err(err).Msg("建立訂單失敗")
			return nil, huma.Error400BadRequest("建立訂單失敗", err)
		}

		return &order.OrderResponse{Body: o}, nil
	})

	// 獲取所有訂單
	huma.Register(api, huma.Operation{
		OperationID: "get-orders",
		Method:      "GET",
		Path:        "/orders",
		Summary:     "獲取所有訂單",
		Tags:        []string{"orders"},
	}, func(ctx context.Context, input *order.GetOrdersInput) (*order.OrdersResponse, error) {
		orders, err := c.orderService.GetOrders(ctx, input.Limit, input.Skip)
		if err != nil {
			c.logger.Error().Err(err).Msg("獲取訂單列表失敗")
			return nil, huma.Error500InternalServerError("獲取訂單列表失敗", err)
		}

		return &order.OrdersResponse{Body: orders}, nil
	})

	// 獲取調度訂單（分頁）
	huma.Register(api, huma.Operation{
		OperationID: "get-dispatch-orders",
		Method:      "GET",
		Path:        "/orders-dispatch",
		Summary:     "獲取調度訂單",
		Tags:        []string{"orders"},
	}, func(ctx context.Context, input *order.GetDispatchOrdersInput) (*order.GetDispatchOrdersResponse, error) {
		orders, total, err := c.orderService.GetDispatchOrdersFromInput(ctx, input)
		if err != nil {
			c.logger.Error().Err(err).Msg("獲取調度訂單列表失敗")
			return nil, huma.Error500InternalServerError("獲取調度訂單列表失敗", err)
		}

		pagination := common.NewPaginationInfo(input.GetPageNum(), input.GetPageSize(), total)

		response := &order.GetDispatchOrdersResponse{}
		response.Body.Data = orders
		response.Body.Pagination = pagination

		return response, nil
	})

	// 根據ID獲取訂單
	huma.Register(api, huma.Operation{
		OperationID: "get-order-by-id",
		Method:      "GET",
		Path:        "/orders/{id}",
		Summary:     "根據ID獲取訂單",
		Tags:        []string{"orders"},
	}, func(ctx context.Context, input *order.OrderIDInput) (*order.OrderResponse, error) {
		o, err := c.orderService.GetOrderByID(ctx, input.ID)
		if err != nil {
			c.logger.Error().Err(err).Str("order_id", input.ID).Msg("訂單不存在")
			return nil, huma.Error404NotFound("訂單不存在", err)
		}

		return &order.OrderResponse{Body: o}, nil
	})

	// 更新訂單狀態
	huma.Register(api, huma.Operation{
		OperationID: "update-order-status",
		Method:      "PUT",
		Path:        "/orders/{id}/status",
		Summary:     "更新訂單狀態",
		Tags:        []string{"orders"},
	}, func(ctx context.Context, input *order.UpdateOrderStatusInput) (*order.OrderResponse, error) {
		o, err := c.orderService.UpdateOrderStatus(ctx, input.ID, input.Body.Status)
		if err != nil {
			c.logger.Error().Err(err).Str("order_id", input.ID).Str("status", string(input.Body.Status)).Msg("更新訂單狀態失敗")
			return nil, huma.Error400BadRequest("更新訂單狀態失敗", err)
		}

		// 對於特定狀態變更，觸發通知
		if c.notificationService != nil && o.Driver.AssignedDriver != "" {
			// 構建司機資訊
			driver := &model.DriverInfo{}
			driver.Name = o.Driver.Name
			driver.CarPlate = o.Driver.CarNo

			// 根據新狀態決定是否需要通知
			shouldNotify := input.Body.Status == model.OrderStatusCancelled

			if shouldNotify {
				go func() {
					var err error
					switch input.Body.Status {
					case model.OrderStatusCancelled:
						err = c.notificationService.NotifyOrderCancelled(context.Background(), input.ID, driver)
					}

					if err != nil {
						c.logger.Error().Err(err).
							Str("order_id", input.ID).
							Str("status", string(input.Body.Status)).
							Msg("管理員訂單狀態更新通知失敗")
					}
				}()
			}
		}

		return &order.OrderResponse{Body: o}, nil
	})

	// Simple Create Order
	huma.Register(api, huma.Operation{
		OperationID: "simple-create-order",
		Method:      "POST",
		Path:        "/orders/simple-create",
		Summary:     "創建新訂單(純文字)",
		Tags:        []string{"orders"},
		Middlewares: huma.Middlewares{c.authMiddleware.Auth()},
		Security: []map[string][]string{
			{"bearerAuth": {}},
		},
	}, func(ctx context.Context, input *order.SimpleCreateOrderInput) (*model.CreateOrderResponse, error) {
		ctx, span := infra.StartOrderControllerSpan(ctx, "simple_create_order",
			infra.AttrString("ori_text", input.Body.OriText),
			infra.AttrString("fleet", string(input.Body.Fleet)),
		)
		defer span.End()

		infra.AddEvent(span, "simple_create_order_started",
			infra.AttrString("ori_text", input.Body.OriText),
			infra.AttrString("fleet", string(input.Body.Fleet)),
		)

		// 從 JWT token 中獲取當前用戶資訊
		userFromToken, err := auth.GetUserFromContext(ctx)
		if err != nil {
			infra.RecordError(span, err, "無法從token中獲取用戶資訊",
				infra.AttrString("error", err.Error()),
			)
			c.logger.Error().Err(err).Msg("無法從token中獲取用戶資訊")
			return nil, huma.Error500InternalServerError("無法從token中獲取用戶資訊")
		}

		// 使用 SimpleCreateOrder 方法
		result, err := c.orderService.SimpleCreateOrder(ctx, input.Body.OriText, input.Body.Fleet, model.CreatedBySystem, userFromToken.Name)
		if err != nil {
			// 檢查是否為地址建議錯誤
			if suggestionErr, ok := err.(*order.AddressSuggestionError); ok {
				// 直接返回建議列表，使用統一的 APIResponse 格式
				suggestionData := &model.AddressSuggestionData{
					Message:     suggestionErr.Message,
					Suggestions: suggestionErr.Suggestions,
				}
				return &model.CreateOrderResponse{
					Type:    "suggestion",
					Data:    suggestionData,
					Message: "需要選擇地址",
				}, nil
			}
			infra.RecordError(span, err, "建立訂單失敗",
				infra.AttrString("ori_text", input.Body.OriText),
				infra.AttrString("error", err.Error()),
			)
			c.logger.Error().Err(err).Str("text", input.Body.OriText).Msg("建立訂單失敗")
			return nil, huma.Error400BadRequest("建立訂單失敗", err)
		}

		if result.IsScheduled {
			infra.AddEvent(span, "scheduled_order_created")
			infra.MarkSuccess(span,
				infra.AttrString("result.type", "schedule"),
				infra.AttrBool("is_scheduled", true),
			)
			// 返回排程訂單
			return &model.CreateOrderResponse{
				Type:    "schedule",
				Data:    result.Schedule,
				Message: result.Message,
			}, nil
		} else {
			infra.AddEvent(span, "instant_order_created")
			if result.Order != nil {
				infra.MarkSuccess(span,
					infra.AttrString("result.type", "order"),
					infra.AttrOrderID(result.Order.ID.Hex()),
					infra.AttrString("order.status", string(result.Order.Status)),
					infra.AttrBool("is_scheduled", false),
				)
			} else {
				infra.MarkSuccess(span,
					infra.AttrString("result.type", "order"),
					infra.AttrBool("is_scheduled", false),
				)
			}
			// 返回立即創建的訂單
			return &model.CreateOrderResponse{
				Type:    "order",
				Data:    result.Order,
				Message: result.Message,
			}, nil
		}
	})

	// 更新調度訂單狀態 (hasCopy, hasNotify)
	huma.Register(api, huma.Operation{
		OperationID: "update-dispatch-order-status",
		Method:      "PUT",
		Path:        "/orders/{id}/dispatch-status",
		Summary:     "更新調度訂單狀態 (hasCopy, hasNotify)",
		Tags:        []string{"orders"},
	}, func(ctx context.Context, input *order.UpdateDispatchOrderStatusInput) (*order.OrderResponse, error) {
		o, err := c.orderService.UpdateDispatchOrderStatus(ctx, input.ID, input.Body.HasCopy, input.Body.HasNotify)
		if err != nil {
			c.logger.Error().Err(err).Str("order_id", input.ID).Interface("has_copy", input.Body.HasCopy).Interface("has_notify", input.Body.HasNotify).Msg("更新調度訂單狀態失敗")
			return nil, huma.Error400BadRequest("更新調度訂單狀態失敗", err)
		}

		return &order.OrderResponse{Body: o}, nil
	})

	// 重新派送訂單
	huma.Register(api, huma.Operation{
		OperationID: "redispatch-order",
		Method:      "POST",
		Path:        "/orders/{id}/redispatch",
		Summary:     "重新派送訂單",
		Tags:        []string{"orders"},
	}, func(ctx context.Context, input *order.OrderIDInput) (*order.OrderResponse, error) {
		ctx, span := infra.StartOrderControllerSpan(ctx, "redispatch_order",
			infra.AttrOrderID(input.ID),
		)
		defer span.End()

		infra.AddEvent(span, "redispatch_order_started",
			infra.AttrOrderID(input.ID),
		)

		o, err := c.orderService.RedispatchOrder(ctx, input.ID)
		if err != nil {
			infra.RecordOrderControllerError(span, err, input.ID, "重新派送訂單失敗")
			c.logger.Error().Err(err).Str("order_id", input.ID).Msg("重新派送訂單失敗")
			return nil, huma.Error400BadRequest("重新派送訂單失敗", err)
		}

		infra.RecordOrderControllerSuccess(span, input.ID,
			infra.AttrString("result.status", string(o.Status)),
		)

		return &order.OrderResponse{Body: o}, nil
	})

	// 刪除流單訂單
	huma.Register(api, huma.Operation{
		OperationID: "delete-failed-order",
		Method:      "DELETE",
		Path:        "/orders/{id}",
		Summary:     "刪除流單訂單",
		Tags:        []string{"orders"},
	}, func(ctx context.Context, input *order.OrderIDInput) (*struct{}, error) {
		if err := c.orderService.DeleteFailedOrder(ctx, input.ID); err != nil {
			c.logger.Error().Err(err).Str("order_id", input.ID).Msg("刪除流單訂單失敗")
			return nil, huma.Error400BadRequest("刪除流單訂單失敗", err)
		}

		return &struct{}{}, nil
	})

	// 調度取消訂單
	huma.Register(api, huma.Operation{
		OperationID: "dispatch-cancel-order",
		Method:      "POST",
		Path:        "/orders/{orderID}/dispatch-cancel",
		Summary:     "調度取消訂單",
		Description: "取消正在派送或司機已抵達的訂單，通知司機訂單已取消",
		Tags:        []string{"orders"},
		Middlewares: huma.Middlewares{c.authMiddleware.Auth()},
		Security: []map[string][]string{
			{"bearerAuth": {}},
		},
	}, func(ctx context.Context, input *order.DispatchCancelInput) (*order.OrderResponse, error) {
		ctx, span := infra.StartOrderControllerSpan(ctx, "dispatch_cancel_order",
			infra.AttrOrderID(input.OrderID),
		)
		defer span.End()

		infra.AddEvent(span, "dispatch_cancel_order_started",
			infra.AttrOrderID(input.OrderID),
		)

		// 0. 獲取當前操作用戶信息，如果無法獲取則使用預設值
		var userName string = "調度中心"
		if userFromToken, err := auth.GetUserFromContext(ctx); err != nil {
			infra.AddEvent(span, "using_default_user_name",
				infra.AttrString("default_user", userName),
			)
			c.logger.Warn().Err(err).Msg("無法從token中獲取用戶資訊，使用預設用戶名稱")
		} else {
			userName = userFromToken.Name
			infra.AddEvent(span, "user_authenticated",
				infra.AttrString("user_name", userName),
			)
		}

		// 使用統一的取消服務（包含所有驗證邏輯）
		updatedOrder, err := c.orderService.CancelOrder(ctx, input.OrderID, "網頁取消", userName)
		if err != nil {
			infra.RecordOrderControllerError(span, err, input.OrderID, "取消訂單失敗")
			c.logger.Error().Err(err).Str("order_id", input.OrderID).Msg("取消訂單失敗")
			return nil, huma.Error500InternalServerError("取消訂單失敗", err)
		}

		infra.AddEvent(span, "order_cancelled_successfully")
		infra.RecordOrderControllerSuccess(span, input.OrderID,
			infra.AttrString("result.status", string(updatedOrder.Status)),
			infra.AttrString("cancelled_by", userName),
			infra.AttrString("cancel_reason", "網頁取消"),
		)

		return &order.OrderResponse{Body: updatedOrder}, nil
	})

}
