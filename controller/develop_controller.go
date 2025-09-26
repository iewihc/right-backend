package controller

import (
	"context"
	"right-backend/data-models/common"
	"right-backend/data-models/order"
	"right-backend/model"
	"right-backend/service"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/rs/zerolog"
)

type DevelopController struct {
	logger       zerolog.Logger
	orderService *service.OrderService
}

type PingResponse struct {
	Body struct {
		Message string `json:"message" example:"pong" doc:"回應訊息"`
	}
}

func NewDevelopController(logger zerolog.Logger, orderService *service.OrderService) *DevelopController {
	return &DevelopController{
		logger:       logger.With().Str("module", "develop_controller").Logger(),
		orderService: orderService,
	}
}

func (c *DevelopController) RegisterRoutes(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "develop-ping",
		Method:      "GET",
		Path:        "/develop/ping",
		Summary:     "開發環境健康檢查",
		Description: "用於檢查 API 服務是否正常運行，包含日誌整合測試",
		Tags:        []string{"develop"},
	}, c.ping)

	huma.Register(api, huma.Operation{
		OperationID: "develop-error-test",
		Method:      "GET",
		Path:        "/develop/error",
		Summary:     "錯誤日誌測試",
		Description: "用於測試錯誤日誌的顯示效果",
		Tags:        []string{"develop"},
	}, c.errorTest)

	// Simple Create Order for development
	huma.Register(api, huma.Operation{
		OperationID: "develop-simple-create-order",
		Method:      "POST",
		Path:        "/develop/simple-create",
		Summary:     "開發環境創建新訂單(純文字)",
		Description: "開發環境用的創建新訂單API，與正式版功能相同",
		Tags:        []string{"develop"},
	}, c.simpleCreateOrder)
}

func (c *DevelopController) ping(ctx context.Context, input *struct{}) (*PingResponse, error) {
	// 簡單的業務邏輯，日誌記錄由中間件自動處理
	c.logger.Info().Msg("Processing ping request")

	return &PingResponse{
		Body: struct {
			Message string `json:"message" example:"pong" doc:"回應訊息"`
		}{
			Message: "pong",
		},
	}, nil
}

func (c *DevelopController) errorTest(ctx context.Context, input *struct{}) (*PingResponse, error) {
	// 注意：這裡的 ctx 是 Go 的 context.Context，不是 Huma 的 Context
	// 在真實使用中，你需要從 Huma operation 的 context 獲取 Huma Context
	// 這裡示範如果有 Huma Context 的話，如何使用 middleware 提供的 helper 函數

	c.logger.Info().Msg("Processing error test request")

	// 模擬一些業務邏輯和自定義日誌

	// 發送一些本地日誌用於測試
	c.logger.Error().
		Str("error_code", "TEST_ERROR").
		Str("module", "develop_controller").
		Msg("模擬錯誤日誌")

	c.logger.Warn().
		Str("issue", "performance_degradation").
		Str("threshold", "500ms").
		Str("actual_time", "750ms").
		Msg("模擬性能警告")

	c.logger.Debug().
		Str("debug_info", "detailed_execution_path").
		Interface("variables", map[string]interface{}{
			"userId":    "12345",
			"sessionId": "sess_abc123",
			"features":  []string{"feature_a", "feature_b"},
		}).
		Msg("模擬調試信息")

	return &PingResponse{
		Body: struct {
			Message string `json:"message" example:"pong" doc:"回應訊息"`
		}{
			Message: "error test completed - check logs for various levels",
		},
	}, nil
}

func (c *DevelopController) simpleCreateOrder(ctx context.Context, input *order.SimpleCreateOrderInput) (*model.SimpleOrderResponse, error) {
	result, err := c.orderService.SimpleCreateOrder(ctx, input.Body.OriText, input.Body.Fleet, model.CreatedBySystem)
	if err != nil {
		// 檢查是否為地址建議錯誤
		if suggestionErr, ok := err.(*order.AddressSuggestionError); ok {
			// 直接返回建議列表，使用統一的 APIResponse 格式
			suggestionData := &model.AddressSuggestionData{
				Message:     suggestionErr.Message,
				Suggestions: suggestionErr.Suggestions,
			}
			return &model.SimpleOrderResponse{
				Body: *common.SuccessResponse("需要選擇地址", suggestionData),
			}, nil
		}
		c.logger.Error().Err(err).Str("text", input.Body.OriText).Msg("建立訂單失敗")
		return nil, huma.Error400BadRequest("建立訂單失敗", err)
	}

	createdOrder := result.Order

	// 建立簡化的回應資料
	orderIDStr := createdOrder.ID.Hex()
	simpleData := &model.SimpleOrderData{
		OrderID:   orderIDStr,
		ShortID:   "#" + orderIDStr[len(orderIDStr)-5:], // 取最後5碼作為短ID
		Type:      string(createdOrder.Type),
		CreatedAt: createdOrder.CreatedAt.Format(time.RFC3339),
		OriText:   input.Body.OriText,
	}

	return &model.SimpleOrderResponse{
		Body: *common.SuccessResponse("訂單創建成功", simpleData),
	}, nil
}
