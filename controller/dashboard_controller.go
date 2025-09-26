package controller

import (
	"context"
	"right-backend/data-models/common"
	"right-backend/data-models/dashboard"
	"right-backend/model"
	"right-backend/service"

	"github.com/danielgtaylor/huma/v2"
	"github.com/rs/zerolog"
)

type DashboardController struct {
	logger           zerolog.Logger
	dashboardService *service.DashboardService
}

func NewDashboardController(logger zerolog.Logger, dashboardService *service.DashboardService) *DashboardController {
	return &DashboardController{
		logger:           logger.With().Str("module", "dashboard_controller").Logger(),
		dashboardService: dashboardService,
	}
}

func (c *DashboardController) RegisterRoutes(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "get-dashboard-stats",
		Method:      "GET",
		Path:        "/dashboard/stats",
		Summary:     "獲取儀表板統計資料",
		Description: "獲取包含訂單、司機狀態、任務執行等各項統計資料",
		Tags:        []string{"dashboard"},
	}, func(ctx context.Context, input *struct{}) (*dashboard.DashboardStatsResponse, error) {
		stats, err := c.dashboardService.GetDashboardStats(ctx)
		if err != nil {
			c.logger.Error().Err(err).Msg("獲取統計資料失敗")
			return nil, huma.Error500InternalServerError("獲取統計資料失敗", err)
		}

		return &dashboard.DashboardStatsResponse{Body: stats}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-all-drivers",
		Method:      "GET",
		Path:        "/dashboard/drivers",
		Summary:     "獲取所有司機資料",
		Description: "獲取系統中所有司機的詳細資料列表",
		Tags:        []string{"dashboard"},
	}, func(ctx context.Context, input *struct{}) (*dashboard.GetAllDriversResponse, error) {
		drivers, totalCount, err := c.dashboardService.GetAllDrivers(ctx)
		if err != nil {
			c.logger.Error().Err(err).Msg("獲取司機資料失敗")
			return nil, huma.Error500InternalServerError("獲取司機資料失敗", err)
		}

		return &dashboard.GetAllDriversResponse{
			Body: struct {
				Drivers    []model.DriverInfo `json:"drivers"`
				TotalCount int                `json:"totalCount"`
			}{
				Drivers:    drivers,
				TotalCount: totalCount,
			},
		}, nil
	})

	// 測試車隊統計 API
	huma.Register(api, huma.Operation{
		OperationID: "get-fleet-stats",
		Method:      "GET",
		Path:        "/dashboard/fleet-stats",
		Summary:     "獲取車隊統計資料",
		Description: "獲取按車隊分組的線上司機統計",
		Tags:        []string{"dashboard"},
	}, func(ctx context.Context, input *struct{}) (*struct {
		Body map[string]int `json:"fleetStats"`
	}, error) {
		fleetStats, err := c.dashboardService.GetOnlineDriverStatsByFleet(ctx)
		if err != nil {
			c.logger.Error().Err(err).Msg("獲取車隊統計失敗")
			return nil, huma.Error500InternalServerError("獲取車隊統計失敗", err)
		}

		return &struct {
			Body map[string]int `json:"fleetStats"`
		}{
			Body: fleetStats,
		}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-driver-orders",
		Method:      "GET",
		Path:        "/dashboard/driver-orders",
		Summary:     "獲取司機訂單列表",
		Description: "以司機為主找訂單，支援狀態過濾和模糊搜尋，帶分頁功能",
		Tags:        []string{"dashboard"},
	}, func(ctx context.Context, input *dashboard.GetDriverOrdersInput) (*dashboard.GetDriverOrdersResponse, error) {
		orders, pagination, err := c.dashboardService.GetDriverOrders(ctx, input.GetPageNum(), input.GetPageSize(), input.DriverStatus, input.GetSearchKeyword())
		if err != nil {
			c.logger.Error().Err(err).Str("driver_status", input.DriverStatus).Str("search_keyword", input.GetSearchKeyword()).Msg("獲取司機訂單列表失敗")
			return nil, huma.Error500InternalServerError("獲取司機訂單列表失敗", err)
		}

		response := &dashboard.GetDriverOrdersResponse{}
		response.Body.Data = orders
		response.Body.Pagination = pagination
		return response, nil
	})

	// 獲取司機週接單排行榜
	huma.Register(api, huma.Operation{
		OperationID: "get-driver-weekly-order-ranks",
		Method:      "GET",
		Path:        "/dashboard/driver-weekly-order-ranks",
		Summary:     "獲取司機週接單排行榜",
		Description: "根據週數偏移獲取司機接單數量排行榜，不包含流單",
		Tags:        []string{"dashboard"},
	}, func(ctx context.Context, input *dashboard.GetDriverWeeklyOrderRanksInput) (*dashboard.GetDriverWeeklyOrderRanksResponse, error) {
		ranks, totalCount, weekStart, weekEnd, err := c.dashboardService.GetDriverWeeklyOrderRanks(
			ctx,
			input.WeekOffset,
			input.GetPageNum(),
			input.GetPageSize(),
		)
		if err != nil {
			c.logger.Error().Err(err).Int("week_offset", input.WeekOffset).Msg("獲取司機週接單排行榜失敗")
			return nil, huma.Error500InternalServerError("獲取司機週接單排行榜失敗", err)
		}

		resp := &dashboard.GetDriverWeeklyOrderRanksResponse{}
		resp.Body.Ranks = ranks
		resp.Body.WeekStart = weekStart
		resp.Body.WeekEnd = weekEnd
		resp.Body.Pagination = common.NewPaginationInfo(input.GetPageNum(), input.GetPageSize(), totalCount)

		return resp, nil
	})
}
