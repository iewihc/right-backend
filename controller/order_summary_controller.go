package controller

import (
	"context"
	"right-backend/data-models/common"
	"right-backend/data-models/order"
	"right-backend/middleware"
	"right-backend/service"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/rs/zerolog"
)

type OrderSummaryController struct {
	logger              zerolog.Logger
	orderSummaryService *service.OrderSummaryService
	authMiddleware      *middleware.UserAuthMiddleware
}

func NewOrderSummaryController(logger zerolog.Logger, orderSummaryService *service.OrderSummaryService, authMiddleware *middleware.UserAuthMiddleware) *OrderSummaryController {
	return &OrderSummaryController{
		logger:              logger.With().Str("module", "order_summary_controller").Logger(),
		orderSummaryService: orderSummaryService,
		authMiddleware:      authMiddleware,
	}
}

func (c *OrderSummaryController) RegisterRoutes(api huma.API) {
	// 獲取訂單報表列表
	huma.Register(api, huma.Operation{
		OperationID: "get-order-summary",
		Method:      "GET",
		Path:        "/order-summary",
		Summary:     "獲取訂單報表列表",
		Tags:        []string{"order-summary"},
		Middlewares: huma.Middlewares{c.authMiddleware.Auth()},
		Security: []map[string][]string{
			{"bearerAuth": {}},
		},
	}, func(ctx context.Context, input *order.GetOrderSummaryInput) (*order.GetOrderSummaryResponse, error) {
		// 建立過濾器
		var filter *service.OrderSummaryFilter
		if input.StartDate != "" || input.EndDate != "" || input.Fleet != "" ||
			input.CustomerGroup != "" || input.Status != "" || input.PickupAddress != "" ||
			input.OrderID != "" || input.Driver != "" || input.PassengerID != "" ||
			input.ShortID != "" || input.Remarks != "" || input.AmountNote != "" || input.Keyword != "" {

			// 處理多個狀態值 (以逗號分隔)
			var statusList []string
			if input.Status != "" {
				statusList = strings.Split(input.Status, ",")
				// 去除空白字元
				for i, status := range statusList {
					statusList[i] = strings.TrimSpace(status)
				}
			}

			filter = &service.OrderSummaryFilter{
				StartDate:     input.StartDate,
				EndDate:       input.EndDate,
				Fleet:         input.Fleet,
				CustomerGroup: input.CustomerGroup,
				Status:        statusList,
				PickupAddress: input.PickupAddress,
				OrderID:       input.OrderID,
				Driver:        input.Driver,
				PassengerID:   input.PassengerID,
				ShortID:       input.ShortID,
				Remarks:       input.Remarks,
				AmountNote:    input.AmountNote,
				Keyword:       input.Keyword,
			}
		} else {
			// 如果沒有其他過濾條件，至少要有車隊過濾
			filter = &service.OrderSummaryFilter{
				Fleet: input.Fleet,
			}
		}

		// 解析排序參數
		var sortField, sortOrder string = "created_at", "desc" // 預設排序
		if input.Sort != "" {
			parts := strings.Split(input.Sort, ":")
			if len(parts) == 2 {
				sortField = parts[0]
				sortOrder = parts[1]
			}
		}

		orders, total, err := c.orderSummaryService.GetOrderSummary(ctx, input.GetPageNum(), input.GetPageSize(), filter, sortField, sortOrder)
		if err != nil {
			c.logger.Error().Err(err).Msg("獲取訂單報表列表失敗")
			return nil, huma.Error500InternalServerError("獲取訂單報表列表失敗", err)
		}

		pagination := common.NewPaginationInfo(input.GetPageNum(), input.GetPageSize(), total)

		response := &order.GetOrderSummaryResponse{}
		response.Body.Data = orders
		response.Body.Pagination = pagination

		return response, nil
	})

	// 根據ID獲取訂單詳情
	huma.Register(api, huma.Operation{
		OperationID: "get-order-summary-by-id",
		Method:      "GET",
		Path:        "/order-summary/{id}",
		Summary:     "根據ID獲取訂單詳情",
		Tags:        []string{"order-summary"},
		Middlewares: huma.Middlewares{c.authMiddleware.Auth()},
		Security: []map[string][]string{
			{"bearerAuth": {}},
		},
	}, func(ctx context.Context, input *order.OrderSummaryIDInput) (*order.OrderSummaryResponse, error) {
		o, err := c.orderSummaryService.GetOrderByID(ctx, input.ID)
		if err != nil {
			c.logger.Error().Err(err).Str("order_id", input.ID).Msg("訂單不存在")
			return nil, huma.Error404NotFound("訂單不存在", err)
		}

		return &order.OrderSummaryResponse{Body: o}, nil
	})

	// 創建訂單
	huma.Register(api, huma.Operation{
		OperationID: "create-order-summary",
		Method:      "POST",
		Path:        "/order-summary",
		Summary:     "創建新訂單",
		Tags:        []string{"order-summary"},
		Middlewares: huma.Middlewares{c.authMiddleware.Auth()},
		Security: []map[string][]string{
			{"bearerAuth": {}},
		},
	}, func(ctx context.Context, input *order.CreateOrderSummaryInput) (*order.OrderSummaryResponse, error) {
		o, err := c.orderSummaryService.CreateOrder(ctx, &input.Body)
		if err != nil {
			c.logger.Error().Err(err).Msg("建立訂單失敗")
			return nil, huma.Error400BadRequest("建立訂單失敗", err)
		}

		return &order.OrderSummaryResponse{Body: o}, nil
	})

	// 更新訂單
	huma.Register(api, huma.Operation{
		OperationID: "update-order-summary",
		Method:      "PUT",
		Path:        "/order-summary/{id}",
		Summary:     "更新訂單",
		Tags:        []string{"order-summary"},
		Middlewares: huma.Middlewares{c.authMiddleware.Auth()},
		Security: []map[string][]string{
			{"bearerAuth": {}},
		},
	}, func(ctx context.Context, input *order.UpdateOrderSummaryInput) (*order.OrderSummaryResponse, error) {
		o, err := c.orderSummaryService.UpdateOrderSummary(ctx, input.ID, &input.Body)
		if err != nil {
			c.logger.Error().Err(err).Str("order_id", input.ID).Msg("更新訂單失敗")
			return nil, huma.Error400BadRequest("更新訂單失敗", err)
		}

		return &order.OrderSummaryResponse{Body: o}, nil
	})

	// 刪除訂單
	huma.Register(api, huma.Operation{
		OperationID: "delete-order-summary",
		Method:      "DELETE",
		Path:        "/order-summary/{id}",
		Summary:     "刪除訂單",
		Tags:        []string{"order-summary"},
		Middlewares: huma.Middlewares{c.authMiddleware.Auth()},
		Security: []map[string][]string{
			{"bearerAuth": {}},
		},
	}, func(ctx context.Context, input *order.OrderSummaryIDInput) (*struct{}, error) {
		if err := c.orderSummaryService.DeleteOrder(ctx, input.ID); err != nil {
			c.logger.Error().Err(err).Str("order_id", input.ID).Msg("刪除訂單失敗")
			return nil, huma.Error400BadRequest("刪除訂單失敗", err)
		}

		return &struct{}{}, nil
	})

	// 批量編輯訂單
	huma.Register(api, huma.Operation{
		OperationID: "batch-edit-order-summary",
		Method:      "PUT",
		Path:        "/order-summary/batch-edit",
		Summary:     "批量編輯訂單",
		Tags:        []string{"order-summary"},
	}, func(ctx context.Context, input *order.BatchEditOrderInput) (*order.BatchEditOrderResponse, error) {
		// 驗證訂單數量
		if len(input.Body.Orders) == 0 {
			c.logger.Error().Msg("訂單列表不能為空")
			return nil, huma.Error400BadRequest("訂單列表不能為空")
		}

		if len(input.Body.Orders) > 100 {
			c.logger.Error().Msg("一次最多只能編輯100筆訂單")
			return nil, huma.Error400BadRequest("一次最多只能編輯100筆訂單")
		}

		// 執行批量編輯
		results := c.orderSummaryService.BatchEditOrders(ctx, input.Body.Orders)

		// 統計成功和失敗數量
		successCount := 0
		failCount := 0
		for _, result := range results {
			if result.Success {
				successCount++
			} else {
				failCount++
			}
		}

		response := &order.BatchEditOrderResponse{}
		response.Body.Results = results
		response.Body.SuccessCount = successCount
		response.Body.FailCount = failCount

		c.logger.Info().
			Int("success_count", successCount).
			Int("fail_count", failCount).
			Int("total", len(input.Body.Orders)).
			Msg("批量編輯訂單完成")

		return response, nil
	})
}
