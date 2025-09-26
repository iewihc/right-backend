package service

import (
	"context"
	"errors"
	"right-backend/infra"
	"right-backend/model"
	"time"

	"github.com/rs/zerolog"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

type TrafficUsageLogService struct {
	logger  zerolog.Logger
	MongoDB *infra.MongoDB
}

func NewTrafficUsageLogService(logger zerolog.Logger, mongo *infra.MongoDB) *TrafficUsageLogService {
	return &TrafficUsageLogService{
		logger:  logger.With().Str("module", "traffic_usage_log_service").Logger(),
		MongoDB: mongo,
	}
}

// CreateTrafficUsageLog creates a new traffic usage log
func (s *TrafficUsageLogService) CreateTrafficUsageLog(ctx context.Context, log *model.TrafficUsageLog) (*model.TrafficUsageLog, error) {
	coll := s.MongoDB.GetCollection("traffic_usage_log")
	log.CreatedAt = time.Now()
	_, err := coll.InsertOne(ctx, log)
	if err != nil {
		s.logger.Error().Err(err).Msg("建立流量使用日誌失敗 (Failed to create traffic usage log)")
		return nil, err
	}
	return log, nil
}

// StatsResult represents the result of a stats query
type StatsResult struct {
	ID    string `bson:"_id" json:"id"`
	Count int    `bson:"count" json:"count"`
}

// GetTrafficUsageStats returns aggregated statistics for traffic usage logs
func (s *TrafficUsageLogService) GetTrafficUsageStats(ctx context.Context, groupBy string) ([]StatsResult, error) {
	if groupBy != "fleet" && groupBy != "created_by" {
		s.logger.Warn().Str("group_by", groupBy).Msg("無效的群組欄位 (Invalid group_by field)")
		return nil, errors.New("無效的群組欄位，必須為 'fleet' 或 'created_by' (Invalid group_by field, must be 'fleet' or 'created_by')")
	}

	coll := s.MongoDB.GetCollection("traffic_usage_log")

	pipeline := mongo.Pipeline{
		{primitive.E{Key: "$group", Value: bson.D{
			primitive.E{Key: "_id", Value: "$" + groupBy},
			primitive.E{Key: "count", Value: bson.D{primitive.E{Key: "$sum", Value: 1}}},
		}}},
		{primitive.E{Key: "$sort", Value: bson.D{primitive.E{Key: "count", Value: -1}}}},
	}

	cursor, err := coll.Aggregate(ctx, pipeline)
	if err != nil {
		s.logger.Error().Str("group_by", groupBy).Err(err).Msg("聚合查詢流量統計失敗 (Failed to aggregate traffic usage stats)")
		return nil, err
	}
	defer cursor.Close(ctx)

	var results []StatsResult
	if err = cursor.All(ctx, &results); err != nil {
		s.logger.Error().Str("group_by", groupBy).Err(err).Msg("讀取流量統計結果失敗 (Failed to read traffic usage stats results)")
		return nil, err
	}

	return results, nil
}
