package service

import (
	"context"
	"crypto/md5"
	"fmt"
	"right-backend/infra"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

const (
	BLACKLIST_KEY_PREFIX = "driver_blacklist" // Redis key 前綴
)

type DriverBlacklistService struct {
	logger      zerolog.Logger
	redisClient *redis.Client
}

func NewDriverBlacklistService(logger zerolog.Logger, redisClient *redis.Client) *DriverBlacklistService {
	return &DriverBlacklistService{
		logger:      logger.With().Str("module", "driver_blacklist_service").Logger(),
		redisClient: redisClient,
	}
}

// generateBlacklistKey 生成Redis黑名單key
// 格式: "driver_blacklist:<driverID>:<pickup_address_hash>"
func (s *DriverBlacklistService) generateBlacklistKey(driverID, pickupAddress string) string {
	// 對pickup address進行MD5雜湊，避免特殊字符和長度問題
	hasher := md5.New()
	hasher.Write([]byte(pickupAddress))
	addressHash := fmt.Sprintf("%x", hasher.Sum(nil))[:8] // 取前8位

	return fmt.Sprintf("%s:%s:%s", BLACKLIST_KEY_PREFIX, driverID, addressHash)
}

// AddDriverToBlacklist 添加司機到黑名單
func (s *DriverBlacklistService) AddDriverToBlacklist(ctx context.Context, driverID, pickupAddress string) error {
	key := s.generateBlacklistKey(driverID, pickupAddress)

	// 從配置獲取過期時間
	expiryMinutes := infra.AppConfig.DriverBlacklist.ExpiryMinutes
	if expiryMinutes <= 0 {
		expiryMinutes = 10 // 默認值
	}

	err := s.redisClient.Set(ctx, key, "1", time.Duration(expiryMinutes)*time.Minute).Err()
	if err != nil {
		s.logger.Error().Err(err).
			Str("driver_id", driverID).
			Str("pickup_address", pickupAddress).
			Str("redis_key", key).
			Msg("添加司機到黑名單失敗")
		return fmt.Errorf("添加司機到黑名單失敗: %w", err)
	}

	s.logger.Info().
		Str("driver_id", driverID).
		Str("pickup_address", pickupAddress).
		Str("redis_key", key).
		Int("expiry_minutes", expiryMinutes).
		Msg("成功添加司機到黑名單")

	return nil
}

// IsDriverBlacklisted 檢查司機是否在黑名單中
func (s *DriverBlacklistService) IsDriverBlacklisted(ctx context.Context, driverID, pickupAddress string) (bool, error) {
	key := s.generateBlacklistKey(driverID, pickupAddress)

	result, err := s.redisClient.Get(ctx, key).Result()
	if err != nil {
		if err == redis.Nil {
			// Key不存在，司機不在黑名單中
			return false, nil
		}
		s.logger.Error().Err(err).
			Str("driver_id", driverID).
			Str("pickup_address", pickupAddress).
			Str("redis_key", key).
			Msg("檢查司機黑名單狀態失敗")
		return false, fmt.Errorf("檢查司機黑名單狀態失敗: %w", err)
	}

	// Key存在且有值，司機在黑名單中
	isBlacklisted := result != ""

	if isBlacklisted {
		s.logger.Debug().
			Str("driver_id", driverID).
			Str("pickup_address", pickupAddress).
			Str("redis_key", key).
			Msg("司機在黑名單中")
	}

	return isBlacklisted, nil
}

// RemoveDriverFromBlacklist 從黑名單中移除司機（可選方法，用於測試或手動操作）
func (s *DriverBlacklistService) RemoveDriverFromBlacklist(ctx context.Context, driverID, pickupAddress string) error {
	key := s.generateBlacklistKey(driverID, pickupAddress)

	err := s.redisClient.Del(ctx, key).Err()
	if err != nil {
		s.logger.Error().Err(err).
			Str("driver_id", driverID).
			Str("pickup_address", pickupAddress).
			Str("redis_key", key).
			Msg("從黑名單中移除司機失敗")
		return fmt.Errorf("從黑名單中移除司機失敗: %w", err)
	}

	s.logger.Info().
		Str("driver_id", driverID).
		Str("pickup_address", pickupAddress).
		Str("redis_key", key).
		Msg("成功從黑名單中移除司機")

	return nil
}

// GetBlacklistTTL 獲取黑名單剩餘過期時間（可選方法，用於調試）
func (s *DriverBlacklistService) GetBlacklistTTL(ctx context.Context, driverID, pickupAddress string) (time.Duration, error) {
	key := s.generateBlacklistKey(driverID, pickupAddress)

	ttl, err := s.redisClient.TTL(ctx, key).Result()
	if err != nil {
		s.logger.Error().Err(err).
			Str("driver_id", driverID).
			Str("pickup_address", pickupAddress).
			Str("redis_key", key).
			Msg("獲取黑名單TTL失敗")
		return 0, fmt.Errorf("獲取黑名單TTL失敗: %w", err)
	}

	return ttl, nil
}
