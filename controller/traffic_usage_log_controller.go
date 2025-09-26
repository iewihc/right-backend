package controller

import (
	"context"
	"right-backend/data-models/traffic_usage_log"
	"right-backend/service"

	"github.com/danielgtaylor/huma/v2"
	"github.com/rs/zerolog"
)

type TrafficUsageLogController struct {
	logger                 zerolog.Logger
	trafficUsageLogService *service.TrafficUsageLogService
}

func NewTrafficUsageLogController(logger zerolog.Logger, trafficUsageLogService *service.TrafficUsageLogService) *TrafficUsageLogController {
	return &TrafficUsageLogController{
		logger:                 logger.With().Str("module", "traffic_usage_log_controller").Logger(),
		trafficUsageLogService: trafficUsageLogService,
	}
}

func (c *TrafficUsageLogController) RegisterRoutes(api huma.API) {
	// 獲取 Traffic Usage Log 統計信息
	huma.Register(api, huma.Operation{
		OperationID: "get-traffic-usage-stats",
		Method:      "GET",
		Path:        "/traffic-usage-logs/stats",
		Summary:     "獲取 Traffic Usage Log 統計信息",
		Tags:        []string{"Traffic Usage Log"},
	}, func(ctx context.Context, input *traffic_usage_log.GetTrafficUsageStatsInput) (*traffic_usage_log.TrafficUsageStatsResponse, error) {
		stats, err := c.trafficUsageLogService.GetTrafficUsageStats(ctx, input.GroupBy)
		if err != nil {
			c.logger.Error().Err(err).Str("group_by", input.GroupBy).Msg("獲取統計信息失敗")
			return nil, huma.Error500InternalServerError("獲取統計信息失敗", err)
		}

		return &traffic_usage_log.TrafficUsageStatsResponse{Body: stats}, nil
	})
}
