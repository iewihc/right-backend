package infra

import (
	"context"
	"encoding/json"
	"fmt"
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
