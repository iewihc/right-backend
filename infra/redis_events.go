package infra

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

// DriverResponseType 司機回應類型
type DriverResponseType string

const (
	DriverResponseAccept DriverResponseType = "accept"
	DriverResponseReject DriverResponseType = "reject"
)

// DriverEventType 司機事件類型
type DriverEventType string

const (
	DriverEventStatusChange DriverEventType = "status_change"
)

// OrderEventType 訂單事件類型
type OrderEventType string

const (
	OrderEventStatusChange OrderEventType = "status_change"
	OrderEventAccepted     OrderEventType = "accepted"
	OrderEventFailed       OrderEventType = "failed"
	OrderEventCompleted    OrderEventType = "completed"
	OrderEventCancelled    OrderEventType = "cancelled"
)

// DriverResponse 司機回應事件
type DriverResponse struct {
	OrderID   string                 `json:"order_id"`
	DriverID  string                 `json:"driver_id"`
	Action    DriverResponseType     `json:"action"`
	Timestamp time.Time              `json:"timestamp"`
	Details   map[string]interface{} `json:"details,omitempty"`
}

// ToJSON 轉換為 JSON 字串
func (dr *DriverResponse) ToJSON() string {
	data, _ := json.Marshal(dr)
	return string(data)
}

// ParseDriverResponse 解析司機回應
func ParseDriverResponse(payload string) (*DriverResponse, error) {
	var response DriverResponse
	err := json.Unmarshal([]byte(payload), &response)
	return &response, err
}

// DriverStatusEvent 司機狀態變更事件
type DriverStatusEvent struct {
	DriverID  string    `json:"driver_id"`
	OldStatus string    `json:"old_status"`
	NewStatus string    `json:"new_status"`
	OrderID   string    `json:"order_id,omitempty"` // 相關訂單ID（如果有）
	Timestamp time.Time `json:"timestamp"`
	Reason    string    `json:"reason"`
}

// ToJSON 轉換為 JSON 字串
func (dse *DriverStatusEvent) ToJSON() string {
	data, _ := json.Marshal(dse)
	return string(data)
}

// ParseDriverStatusEvent 解析司機狀態事件
func ParseDriverStatusEvent(payload string) (*DriverStatusEvent, error) {
	var event DriverStatusEvent
	err := json.Unmarshal([]byte(payload), &event)
	return &event, err
}

// OrderStatusEvent 訂單狀態變更事件
type OrderStatusEvent struct {
	OrderID   string                 `json:"order_id"`
	OldStatus string                 `json:"old_status"`
	NewStatus string                 `json:"new_status"`
	DriverID  string                 `json:"driver_id,omitempty"` // 相關司機ID（如果有）
	Timestamp time.Time              `json:"timestamp"`
	Reason    string                 `json:"reason"`
	EventType OrderEventType         `json:"event_type"`
	Details   map[string]interface{} `json:"details,omitempty"`
}

// ToJSON 轉換為 JSON 字串
func (ose *OrderStatusEvent) ToJSON() string {
	data, _ := json.Marshal(ose)
	return string(data)
}

// ParseOrderStatusEvent 解析訂單狀態事件
func ParseOrderStatusEvent(payload string) (*OrderStatusEvent, error) {
	var event OrderStatusEvent
	err := json.Unmarshal([]byte(payload), &event)
	return &event, err
}

// RedisEventManager Redis 事件管理器
type RedisEventManager struct {
	client *redis.Client
	logger zerolog.Logger
}

// NewRedisEventManager 建立 Redis 事件管理器
func NewRedisEventManager(client *redis.Client, logger zerolog.Logger) *RedisEventManager {
	return &RedisEventManager{
		client: client,
		logger: logger.With().Str("module", "redis_events").Logger(),
	}
}

// PublishDriverResponse 發布司機回應事件
func (rem *RedisEventManager) PublishDriverResponse(ctx context.Context, response *DriverResponse) error {
	channel := fmt.Sprintf("order_response:%s", response.OrderID)
	payload := response.ToJSON()

	err := rem.client.Publish(ctx, channel, payload).Err()
	if err != nil {
		rem.logger.Error().Err(err).
			Str("channel", channel).
			Str("order_id", response.OrderID).
			Str("driver_id", response.DriverID).
			Str("action", string(response.Action)).
			Msg("發布司機回應事件失敗")
		return err
	}

	rem.logger.Info().
		Str("channel", channel).
		Str("order_id", response.OrderID).
		Str("driver_id", response.DriverID).
		Str("action", string(response.Action)).
		Msg("司機回應事件已發布")

	return nil
}

// SubscribeOrderResponses 訂閱訂單回應事件
func (rem *RedisEventManager) SubscribeOrderResponses(ctx context.Context, orderID string) *redis.PubSub {
	channel := fmt.Sprintf("order_response:%s", orderID)
	pubsub := rem.client.Subscribe(ctx, channel)

	//rem.logger.Info().
	//	Str("channel", channel).
	//	Str("order_id", orderID).
	//	Msg("開始訂閱訂單回應事件")

	return pubsub
}

// AcquireDispatchLock 獲取調度鎖
func (rem *RedisEventManager) AcquireDispatchLock(ctx context.Context, orderID string, dispatcherID string, ttl time.Duration) (bool, string, func(), error) {
	lockKey := fmt.Sprintf("dispatch_lock:%s", orderID)
	lockValue := fmt.Sprintf("%s:%d", dispatcherID, time.Now().Unix())

	// 嘗試獲取分散式鎖
	success, err := rem.client.SetNX(ctx, lockKey, lockValue, ttl).Result()
	if err != nil {
		rem.logger.Error().Err(err).
			Str("lock_key", lockKey).
			Str("order_id", orderID).
			Msg("獲取調度鎖失敗")
		return false, "", nil, err
	}

	if !success {
		rem.logger.Warn().
			Str("lock_key", lockKey).
			Str("order_id", orderID).
			Msg("調度鎖已被其他流程持有")
		return false, "", nil, nil
	}

	//rem.logger.Info().
	//	Str("lock_key", lockKey).
	//	Str("order_id", orderID).
	//	Str("dispatcher_id", dispatcherID).
	//	Dur("ttl", ttl).
	//	Msg("調度鎖獲取成功")

	// 返回釋放鎖的函數
	releaseLock := func() {
		// 使用 Lua 腳本確保安全釋放（只有鎖持有者才能釋放）
		script := `
			if redis.call("GET", KEYS[1]) == ARGV[1] then
				return redis.call("DEL", KEYS[1])
			else
				return 0
			end
		`
		result, scriptErr := rem.client.Eval(ctx, script, []string{lockKey}, lockValue).Result()
		if scriptErr != nil {
			rem.logger.Error().Err(scriptErr).
				Str("lock_key", lockKey).
				Msg("釋放調度鎖失敗")
		} else if result.(int64) == 1 {
			//rem.logger.Info().
			//	Str("lock_key", lockKey).
			//	Str("order_id", orderID).
			//	Msg("調度鎖已釋放")
		} else {
			rem.logger.Warn().
				Str("lock_key", lockKey).
				Str("order_id", orderID).
				Msg("調度鎖釋放失敗 - 鎖可能已被其他流程持有")
		}
	}

	return true, lockValue, releaseLock, nil
}

// ExtendDispatchLock 延長調度鎖
func (rem *RedisEventManager) ExtendDispatchLock(ctx context.Context, orderID string, lockValue string, ttl time.Duration) error {
	lockKey := fmt.Sprintf("dispatch_lock:%s", orderID)

	script := `
		if redis.call("GET", KEYS[1]) == ARGV[1] then
			return redis.call("EXPIRE", KEYS[1], ARGV[2])
		else
			return 0
		end
	`

	result, err := rem.client.Eval(ctx, script, []string{lockKey}, lockValue, int64(ttl.Seconds())).Result()
	if err != nil {
		rem.logger.Error().Err(err).
			Str("lock_key", lockKey).
			Msg("延長調度鎖失敗")
		return err
	}

	if result.(int64) == 1 {
		//rem.logger.Debug().
		//	Str("lock_key", lockKey).
		//	Str("order_id", orderID).
		//	Dur("ttl", ttl).
		//	Msg("調度鎖已延長")
		return nil
	}

	return fmt.Errorf("無法延長調度鎖，鎖可能已被其他流程持有")
}

// PublishDriverStatusEvent 發布司機狀態變更事件
func (rem *RedisEventManager) PublishDriverStatusEvent(ctx context.Context, event *DriverStatusEvent) error {
	channel := "driver_status_changes"
	payload := event.ToJSON()

	err := rem.client.Publish(ctx, channel, payload).Err()
	if err != nil {
		rem.logger.Error().Err(err).
			Str("channel", channel).
			Str("driver_id", event.DriverID).
			Str("old_status", event.OldStatus).
			Str("new_status", event.NewStatus).
			Msg("發布司機狀態變更事件失敗")
		return err
	}

	rem.logger.Info().
		Str("channel", channel).
		Str("driver_id", event.DriverID).
		Str("old_status", event.OldStatus).
		Str("new_status", event.NewStatus).
		Str("reason", event.Reason).
		Msg("司機狀態變更事件已發布")

	return nil
}

// SubscribeDriverStatusChanges 訂閱司機狀態變更事件
func (rem *RedisEventManager) SubscribeDriverStatusChanges(ctx context.Context) *redis.PubSub {
	channel := "driver_status_changes"
	pubsub := rem.client.Subscribe(ctx, channel)

	//rem.logger.Info().
	//	Str("channel", channel).
	//	Msg("開始訂閱司機狀態變更事件")

	return pubsub
}

// PublishOrderStatusEvent 發布訂單狀態變更事件
func (rem *RedisEventManager) PublishOrderStatusEvent(ctx context.Context, event *OrderStatusEvent) error {
	channel := "order_status_changes"
	payload := event.ToJSON()

	err := rem.client.Publish(ctx, channel, payload).Err()
	if err != nil {
		rem.logger.Error().Err(err).
			Str("channel", channel).
			Str("order_id", event.OrderID).
			Str("old_status", event.OldStatus).
			Str("new_status", event.NewStatus).
			Msg("發布訂單狀態變更事件失敗")
		return err
	}

	//rem.logger.Info().
	//	Str("channel", channel).
	//	Str("order_id", event.OrderID).
	//	Str("old_status", event.OldStatus).
	//	Str("new_status", event.NewStatus).
	//	Str("event_type", string(event.EventType)).
	//	Str("reason", event.Reason).
	//	Msg("訂單狀態變更事件已發布")

	return nil
}

// SubscribeOrderStatusChanges 訂閱訂單狀態變更事件
func (rem *RedisEventManager) SubscribeOrderStatusChanges(ctx context.Context) *redis.PubSub {
	channel := "order_status_changes"
	pubsub := rem.client.Subscribe(ctx, channel)

	//rem.logger.Info().
	//	Str("channel", channel).
	//	Msg("開始訂閱訂單狀態變更事件")

	return pubsub
}

// AcquireDriverNotificationLock 獲取司機通知鎖
func (rem *RedisEventManager) AcquireDriverNotificationLock(ctx context.Context, driverID string, orderID string, dispatcherID string, ttl time.Duration) (bool, func(), error) {
	lockKey := fmt.Sprintf("driver_notification_lock:%s", driverID)
	lockValue := fmt.Sprintf("%s:%s:%d", dispatcherID, orderID, time.Now().Unix())

	// 嘗試獲取司機通知鎖
	success, err := rem.client.SetNX(ctx, lockKey, lockValue, ttl).Result()
	if err != nil {
		rem.logger.Error().Err(err).
			Str("lock_key", lockKey).
			Str("driver_id", driverID).
			Str("order_id", orderID).
			Msg("獲取司機通知鎖失敗")
		return false, nil, err
	}

	if !success {
		rem.logger.Warn().
			Str("lock_key", lockKey).
			Str("driver_id", driverID).
			Str("order_id", orderID).
			Msg("司機通知鎖已被其他訂單持有 - 司機正在處理其他通知")
		return false, nil, nil
	}

	//rem.logger.Info().
	//	Str("lock_key", lockKey).
	//	Str("driver_id", driverID).
	//	Str("order_id", orderID).
	//	Str("dispatcher_id", dispatcherID).
	//	Dur("ttl", ttl).
	//	Msg("司機通知鎖獲取成功")

	// 返回釋放鎖的函數
	releaseLock := func() {
		// 使用 Lua 腳本確保安全釋放
		script := `
			if redis.call("GET", KEYS[1]) == ARGV[1] then
				return redis.call("DEL", KEYS[1])
			else
				return 0
			end
		`
		result, scriptErr := rem.client.Eval(ctx, script, []string{lockKey}, lockValue).Result()
		if scriptErr != nil {
			rem.logger.Error().Err(scriptErr).
				Str("lock_key", lockKey).
				Str("driver_id", driverID).
				Str("order_id", orderID).
				Msg("釋放司機通知鎖失敗")
		} else if result.(int64) == 1 {
			//rem.logger.Info().
			//	Str("lock_key", lockKey).
			//	Str("driver_id", driverID).
			//	Str("order_id", orderID).
			//	Msg("司機通知鎖已釋放")
		} else {
			rem.logger.Warn().
				Str("lock_key", lockKey).
				Str("driver_id", driverID).
				Str("order_id", orderID).
				Msg("司機通知鎖釋放失敗 - 鎖可能已被其他流程持有或超時")
		}
	}

	return true, releaseLock, nil
}

// SetCache 設置 Redis 緩存
func (rem *RedisEventManager) SetCache(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	return rem.client.Set(ctx, key, value, ttl).Err()
}

// GetCache 獲取 Redis 緩存
func (rem *RedisEventManager) GetCache(ctx context.Context, key string) (string, error) {
	return rem.client.Get(ctx, key).Result()
}

// DiscordEventType Discord 事件類型
type DiscordEventType string

const (
	DiscordEventUpdateMessage DiscordEventType = "update_message"
)

// LineEventType LINE 事件類型
type LineEventType string

const (
	LineEventUpdateMessage LineEventType = "update_message"
)

// DiscordUpdateEvent Discord 消息更新事件
type DiscordUpdateEvent struct {
	OrderID        string           `json:"order_id"`
	ChannelID      string           `json:"channel_id"`
	MessageID      string           `json:"message_id"`
	EventType      DiscordEventType `json:"event_type"`
	Timestamp      time.Time        `json:"timestamp"`
	RetryCount     int              `json:"retry_count,omitempty"`
	MaxRetries     int              `json:"max_retries,omitempty"`
	RetryBackoffMs int              `json:"retry_backoff_ms,omitempty"`
}

// ToJSON 轉換為 JSON 字串
func (due *DiscordUpdateEvent) ToJSON() string {
	data, _ := json.Marshal(due)
	return string(data)
}

// ParseDiscordUpdateEvent 解析 Discord 更新事件
func ParseDiscordUpdateEvent(payload string) (*DiscordUpdateEvent, error) {
	var event DiscordUpdateEvent
	err := json.Unmarshal([]byte(payload), &event)
	return &event, err
}

// LineUpdateEvent LINE 消息更新事件
type LineUpdateEvent struct {
	OrderID        string        `json:"order_id"`
	ConfigID       string        `json:"config_id"` // LINE Bot 配置 ID
	UserID         string        `json:"user_id"`   // LINE 用戶 ID
	EventType      LineEventType `json:"event_type"`
	Timestamp      time.Time     `json:"timestamp"`
	RetryCount     int           `json:"retry_count,omitempty"`
	MaxRetries     int           `json:"max_retries,omitempty"`
	RetryBackoffMs int           `json:"retry_backoff_ms,omitempty"`
}

// ToJSON 轉換為 JSON 字串
func (lue *LineUpdateEvent) ToJSON() string {
	data, _ := json.Marshal(lue)
	return string(data)
}

// ParseLineUpdateEvent 解析 LINE 更新事件
func ParseLineUpdateEvent(payload string) (*LineUpdateEvent, error) {
	var event LineUpdateEvent
	err := json.Unmarshal([]byte(payload), &event)
	return &event, err
}

// PublishDiscordUpdateEvent 發布 Discord 消息更新事件
func (rem *RedisEventManager) PublishDiscordUpdateEvent(ctx context.Context, event *DiscordUpdateEvent) error {
	channel := "discord_message_updates"
	payload := event.ToJSON()

	err := rem.client.Publish(ctx, channel, payload).Err()
	if err != nil {
		rem.logger.Error().Err(err).
			Str("channel", channel).
			Str("order_id", event.OrderID).
			Str("channel_id", event.ChannelID).
			Str("message_id", event.MessageID).
			Msg("發布 Discord 消息更新事件失敗")
		return err
	}

	//rem.logger.Debug().
	//	Str("channel", channel).
	//	Str("order_id", event.OrderID).
	//	Str("channel_id", event.ChannelID).
	//	Str("message_id", event.MessageID).
	//	Str("event_type", string(event.EventType)).
	//	Msg("Discord 消息更新事件已發布")

	return nil
}

// SubscribeDiscordUpdateEvents 訂閱 Discord 消息更新事件
func (rem *RedisEventManager) SubscribeDiscordUpdateEvents(ctx context.Context) *redis.PubSub {
	channel := "discord_message_updates"
	pubsub := rem.client.Subscribe(ctx, channel)

	rem.logger.Info().
		Str("channel", channel).
		Msg("開始訂閱 Discord 消息更新事件")

	return pubsub
}

// PublishLineUpdateEvent 發布 LINE 消息更新事件
func (rem *RedisEventManager) PublishLineUpdateEvent(ctx context.Context, event *LineUpdateEvent) error {
	channel := "line_message_updates"
	payload := event.ToJSON()

	err := rem.client.Publish(ctx, channel, payload).Err()
	if err != nil {
		rem.logger.Error().Err(err).
			Str("channel", channel).
			Str("order_id", event.OrderID).
			Str("config_id", event.ConfigID).
			Str("user_id", event.UserID).
			Msg("發布 LINE 消息更新事件失敗")
		return err
	}

	rem.logger.Debug().
		Str("channel", channel).
		Str("order_id", event.OrderID).
		Str("config_id", event.ConfigID).
		Str("user_id", event.UserID).
		Msg("成功發布 LINE 消息更新事件")

	return nil
}

// SubscribeLineUpdateEvents 訂閱 LINE 消息更新事件
func (rem *RedisEventManager) SubscribeLineUpdateEvents(ctx context.Context) *redis.PubSub {
	channel := "line_message_updates"
	pubsub := rem.client.Subscribe(ctx, channel)

	rem.logger.Info().
		Str("channel", channel).
		Msg("開始訂閱 LINE 消息更新事件")

	return pubsub
}

// AcquireOrderRejectLock 獲取訂單拒絕鎖，防止重複拒絕記錄
// orderID: 訂單ID
// driverID: 司機ID
// source: 拒絕來源 ("manual"=手動拒絕, "timeout"=超時拒絕)
// ttl: 鎖的存活時間
func (rem *RedisEventManager) AcquireOrderRejectLock(ctx context.Context, orderID string, driverID string, source string, ttl time.Duration) (bool, func(), error) {
	lockKey := fmt.Sprintf("order_reject_lock:%s:%s", orderID, driverID)
	lockValue := fmt.Sprintf("%s:%d", source, time.Now().Unix())

	// 嘗試獲取訂單拒絕鎖
	success, err := rem.client.SetNX(ctx, lockKey, lockValue, ttl).Result()
	if err != nil {
		rem.logger.Error().Err(err).
			Str("lock_key", lockKey).
			Str("order_id", orderID).
			Str("driver_id", driverID).
			Str("source", source).
			Msg("獲取訂單拒絕鎖失敗")
		return false, nil, err
	}

	if !success {
		// 檢查是否已經有其他來源拒絕了這個訂單
		existingValue, getErr := rem.client.Get(ctx, lockKey).Result()
		if getErr == nil {
			rem.logger.Info().
				Str("lock_key", lockKey).
				Str("order_id", orderID).
				Str("driver_id", driverID).
				Str("current_source", source).
				Str("existing_source", existingValue).
				Msg("🔒 訂單拒絕鎖已被持有，避免重複拒絕記錄")
		}
		return false, nil, nil
	}

	rem.logger.Info().
		Str("lock_key", lockKey).
		Str("order_id", orderID).
		Str("driver_id", driverID).
		Str("source", source).
		Dur("ttl", ttl).
		Msg("✅ 訂單拒絕鎖獲取成功")

	// 返回釋放鎖的函數
	releaseLock := func() {
		// 使用 Lua 腳本確保安全釋放
		script := `
			if redis.call("GET", KEYS[1]) == ARGV[1] then
				return redis.call("DEL", KEYS[1])
			else
				return 0
			end
		`
		result, scriptErr := rem.client.Eval(ctx, script, []string{lockKey}, lockValue).Result()
		if scriptErr != nil {
			rem.logger.Error().Err(scriptErr).
				Str("lock_key", lockKey).
				Str("order_id", orderID).
				Str("driver_id", driverID).
				Str("source", source).
				Msg("釋放訂單拒絕鎖失敗")
		} else if result.(int64) == 1 {
			rem.logger.Debug().
				Str("lock_key", lockKey).
				Str("order_id", orderID).
				Str("driver_id", driverID).
				Str("source", source).
				Msg("訂單拒絕鎖已釋放")
		} else {
			rem.logger.Warn().
				Str("lock_key", lockKey).
				Str("order_id", orderID).
				Str("driver_id", driverID).
				Str("source", source).
				Msg("訂單拒絕鎖釋放失敗 - 鎖可能已被其他流程持有或超時")
		}
	}

	return true, releaseLock, nil
}

// AtomicNotifyDriver 原子性檢查並通知司機
// 一次性檢查司機狀態、訂單狀態並設置鎖定，防止競爭狀態
func (rem *RedisEventManager) AtomicNotifyDriver(ctx context.Context, driverID, orderID, dispatcherID string, ttl time.Duration) (success bool, reason string, err error) {
	script := `
		local driver_lock_key = "driver_notification_lock:" .. ARGV[1]
		local driver_state_key = "driver_state:" .. ARGV[1]
		local order_claim_key = "order_claimed:" .. ARGV[2]
		local dispatcher_id = ARGV[3]
		local ttl = tonumber(ARGV[4])
		local start_time = ARGV[5]

		-- 檢查司機是否已被鎖定（正在處理其他訂單通知）
		if redis.call("EXISTS", driver_lock_key) == 1 then
			local existing_dispatcher = redis.call("GET", driver_lock_key)
			return {0, "driver_locked_by:" .. existing_dispatcher}
		end

		-- 檢查司機當前狀態
		local current_status = redis.call("HGET", driver_state_key, "status")
		local current_order = redis.call("HGET", driver_state_key, "current_order_id")

		-- 如果司機已有訂單或正在處理訂單，拒絕
		if current_order and current_order ~= "" then
			return {0, "driver_has_order:" .. current_order}
		end

		if current_status == "busy" or current_status == "processing" then
			return {0, "driver_busy:" .. current_status}
		end

		-- 檢查訂單是否已被其他調度器聲明
		local order_claimer = redis.call("GET", order_claim_key)
		if order_claimer and order_claimer ~= dispatcher_id then
			return {0, "order_claimed_by:" .. order_claimer}
		end

		-- 原子性設置所有狀態
		redis.call("SETEX", driver_lock_key, ttl, dispatcher_id)
		redis.call("SETEX", order_claim_key, ttl, dispatcher_id)
		redis.call("HSET", driver_state_key,
			"status", "receiving_notification",
			"notification_order_id", ARGV[2],
			"notification_dispatcher", dispatcher_id,
			"notification_start", start_time
		)
		redis.call("EXPIRE", driver_state_key, ttl)

		return {1, "success"}
	`

	startTime := fmt.Sprintf("%d", time.Now().Unix())
	result, err := rem.client.Eval(ctx, script, []string{},
		driverID, orderID, dispatcherID, int(ttl.Seconds()), startTime).Result()

	if err != nil {
		rem.logger.Error().Err(err).
			Str("driver_id", driverID).
			Str("order_id", orderID).
			Str("dispatcher_id", dispatcherID).
			Msg("原子性司機通知檢查失敗")
		return false, "redis_error", err
	}

	resultSlice := result.([]interface{})
	success = resultSlice[0].(int64) == 1
	reason = resultSlice[1].(string)

	if success {
		rem.logger.Info().
			Str("driver_id", driverID).
			Str("order_id", orderID).
			Str("dispatcher_id", dispatcherID).
			Dur("ttl", ttl).
			Msg("✅ 原子性司機通知檢查成功，司機和訂單已鎖定")
	} else {
		rem.logger.Debug().
			Str("driver_id", driverID).
			Str("order_id", orderID).
			Str("dispatcher_id", dispatcherID).
			Str("reason", reason).
			Msg("原子性司機通知檢查失敗")
	}

	return success, reason, nil
}

// ReleaseDriverNotification 釋放司機通知狀態
func (rem *RedisEventManager) ReleaseDriverNotification(ctx context.Context, driverID, orderID, dispatcherID string) error {
	script := `
		local driver_lock_key = "driver_notification_lock:" .. ARGV[1]
		local driver_state_key = "driver_state:" .. ARGV[1]
		local order_claim_key = "order_claimed:" .. ARGV[2]
		local dispatcher_id = ARGV[3]

		-- 只有原調度器可以釋放
		local current_locker = redis.call("GET", driver_lock_key)
		if current_locker == dispatcher_id then
			redis.call("DEL", driver_lock_key, order_claim_key)
			redis.call("HDEL", driver_state_key,
				"notification_order_id",
				"notification_dispatcher",
				"notification_start"
			)
			-- 重置司機狀態為閒置（如果沒有當前訂單）
			local current_order = redis.call("HGET", driver_state_key, "current_order_id")
			if not current_order or current_order == "" then
				redis.call("HSET", driver_state_key, "status", "idle")
			end
			return "released"
		end

		return "not_owner"
	`

	result, err := rem.client.Eval(ctx, script, []string{},
		driverID, orderID, dispatcherID).Result()

	if err != nil {
		rem.logger.Error().Err(err).
			Str("driver_id", driverID).
			Str("order_id", orderID).
			Str("dispatcher_id", dispatcherID).
			Msg("釋放司機通知狀態失敗")
		return err
	}

	if result.(string) == "released" {
		rem.logger.Debug().
			Str("driver_id", driverID).
			Str("order_id", orderID).
			Str("dispatcher_id", dispatcherID).
			Msg("司機通知狀態已釋放")
	} else {
		rem.logger.Warn().
			Str("driver_id", driverID).
			Str("order_id", orderID).
			Str("dispatcher_id", dispatcherID).
			Msg("無法釋放司機通知狀態 - 非鎖持有者")
	}

	return nil
}

// AtomicAcceptOrder 原子性接單檢查
// 供司機接單 API 使用，確保司機只能接受正在通知的訂單
func (rem *RedisEventManager) AtomicAcceptOrder(ctx context.Context, driverID, orderID string) (success bool, reason string, err error) {
	script := `
		local driver_state_key = "driver_state:" .. ARGV[1]
		local driver_lock_key = "driver_notification_lock:" .. ARGV[1]
		local order_claim_key = "order_claimed:" .. ARGV[2]
		local accept_time = ARGV[3]

		-- 檢查司機是否正在接收這個訂單的通知
		local expected_order = redis.call("HGET", driver_state_key, "notification_order_id")
		if expected_order ~= ARGV[2] then
			return {0, "not_expecting_this_order"}
		end

		-- 檢查司機當前是否有其他訂單
		local current_order = redis.call("HGET", driver_state_key, "current_order_id")
		if current_order and current_order ~= "" and current_order ~= ARGV[2] then
			return {0, "already_has_order:" .. current_order}
		end

		-- 原子性設置司機接單狀態
		redis.call("HSET", driver_state_key,
			"status", "busy",
			"current_order_id", ARGV[2],
			"order_accepted_at", accept_time
		)

		-- 清除通知狀態但保留訂單聲明（由調度器處理）
		redis.call("HDEL", driver_state_key,
			"notification_order_id",
			"notification_dispatcher",
			"notification_start"
		)
		redis.call("DEL", driver_lock_key)

		return {1, "accepted"}
	`

	timestamp := fmt.Sprintf("%d", time.Now().Unix())
	result, err := rem.client.Eval(ctx, script, []string{},
		driverID, orderID, timestamp).Result()

	if err != nil {
		rem.logger.Error().Err(err).
			Str("driver_id", driverID).
			Str("order_id", orderID).
			Msg("原子性接單檢查失敗")
		return false, "redis_error", err
	}

	resultSlice := result.([]interface{})
	success = resultSlice[0].(int64) == 1
	reason = resultSlice[1].(string)

	if success {
		rem.logger.Info().
			Str("driver_id", driverID).
			Str("order_id", orderID).
			Msg("✅ 司機接單成功")
	} else {
		rem.logger.Warn().
			Str("driver_id", driverID).
			Str("order_id", orderID).
			Str("reason", reason).
			Msg("司機接單失敗")
	}

	return success, reason, nil
}

// StartCleanupWatcher 啟動自動清理監聽器
// 監聽 Redis 過期事件並自動清理相關狀態
func (rem *RedisEventManager) StartCleanupWatcher(ctx context.Context) {
	// 開啟鍵空間通知
	rem.client.ConfigSet(ctx, "notify-keyspace-events", "Ex")

	// 監聽過期事件
	pubsub := rem.client.PSubscribe(ctx, "__keyevent@*__:expired")
	defer pubsub.Close()

	rem.logger.Info().Msg("Redis 自動清理監聽器已啟動")

	for msg := range pubsub.Channel() {
		expiredKey := msg.Payload

		// 處理司機通知鎖過期
		if strings.HasPrefix(expiredKey, "driver_notification_lock:") {
			driverID := strings.TrimPrefix(expiredKey, "driver_notification_lock:")

			// 清理相關的狀態
			cleanupScript := `
				local driver_state_key = "driver_state:" .. ARGV[1]
				local notifying_order_key = "notifying_order:" .. ARGV[1]

				-- 檢查司機是否沒有當前訂單，如果沒有則重置為閒置
				local current_order = redis.call("HGET", driver_state_key, "current_order_id")
				if not current_order or current_order == "" then
					redis.call("HSET", driver_state_key, "status", "idle")
				end

				-- 清除通知相關狀態
				redis.call("HDEL", driver_state_key,
					"notification_order_id",
					"notification_dispatcher",
					"notification_start"
				)
				redis.call("DEL", notifying_order_key)

				return "cleaned"
			`

			_, err := rem.client.Eval(ctx, cleanupScript, []string{}, driverID).Result()
			if err != nil {
				rem.logger.Error().Err(err).
					Str("driver_id", driverID).
					Msg("自動清理司機狀態失敗")
			} else {
				rem.logger.Debug().
					Str("driver_id", driverID).
					Msg("🧹 自動清理過期的司機通知狀態")
			}
		}

		// 處理訂單聲明過期
		if strings.HasPrefix(expiredKey, "order_claimed:") {
			orderID := strings.TrimPrefix(expiredKey, "order_claimed:")
			rem.logger.Debug().
				Str("order_id", orderID).
				Msg("🧹 訂單聲明已過期，可被其他調度器處理")
		}
	}
}

// UpdateDriverStateAfterAccept 更新 Redis driver_state 在司機接單後
// 確保 driver_state 與 MongoDB 狀態同步，防止重複派單
func (rem *RedisEventManager) UpdateDriverStateAfterAccept(ctx context.Context, driverID, orderID string) error {
	driverStateKey := fmt.Sprintf("driver_state:%s", driverID)
	script := `
		redis.call("HSET", KEYS[1],
			"status", "busy",
			"current_order_id", ARGV[1],
			"order_accepted_at", ARGV[2]
		)
		redis.call("EXPIRE", KEYS[1], 86400)
		return 1
	`
	timestamp := fmt.Sprintf("%d", time.Now().Unix())

	_, err := rem.client.Eval(ctx, script, []string{driverStateKey}, orderID, timestamp).Result()
	if err != nil {
		rem.logger.Error().Err(err).
			Str("driver_id", driverID).
			Str("order_id", orderID).
			Msg("更新 Redis driver_state 失敗")
		return err
	}

	rem.logger.Info().
		Str("driver_id", driverID).
		Str("order_id", orderID).
		Msg("✅ Redis driver_state 已更新 (接單後同步)")

	return nil
}

// ClearDriverStateAfterComplete 清除 Redis driver_state 在訂單完成後
// 確保 driver_state 與 MongoDB 狀態同步，讓司機可以接新單
func (rem *RedisEventManager) ClearDriverStateAfterComplete(ctx context.Context, driverID string) error {
	driverStateKey := fmt.Sprintf("driver_state:%s", driverID)
	script := `
		redis.call("HSET", KEYS[1],
			"status", "idle",
			"current_order_id", "",
			"order_completed_at", ARGV[1]
		)
		redis.call("HDEL", KEYS[1], "order_accepted_at")
		redis.call("EXPIRE", KEYS[1], 86400)
		return 1
	`
	timestamp := fmt.Sprintf("%d", time.Now().Unix())

	_, err := rem.client.Eval(ctx, script, []string{driverStateKey}, timestamp).Result()
	if err != nil {
		rem.logger.Error().Err(err).
			Str("driver_id", driverID).
			Msg("清除 Redis driver_state 失敗")
		return err
	}

	rem.logger.Info().
		Str("driver_id", driverID).
		Msg("✅ Redis driver_state 已清除 (訂單完成後同步)")

	return nil
}
