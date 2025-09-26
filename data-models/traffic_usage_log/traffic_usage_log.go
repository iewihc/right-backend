package traffic_usage_log

import "right-backend/service"

type GetTrafficUsageStatsInput struct {
	GroupBy string `query:"group_by" enum:"fleet,created_by" default:"fleet" doc:"Group by 'fleet' or 'created_by'"`
}

type TrafficUsageStatsResponse struct {
	Body []service.StatsResult `json:"stats"`
}
