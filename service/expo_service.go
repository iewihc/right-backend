package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"right-backend/service/interfaces"
	"time"

	"github.com/rs/zerolog"
)

type ExpoService struct {
	logger     zerolog.Logger
	Client     *http.Client
	PushAPIURL string
}

// ExpoMessage 單個推送消息
type ExpoMessage struct {
	To         string                 `json:"to"`
	Title      string                 `json:"title,omitempty"`
	Body       string                 `json:"body,omitempty"`
	Data       map[string]interface{} `json:"data,omitempty"`
	Sound      string                 `json:"sound,omitempty"`
	Badge      int                    `json:"badge,omitempty"`
	ChannelID  string                 `json:"channelId,omitempty"`
	TTL        int                    `json:"ttl,omitempty"`
	Expiration int64                  `json:"expiration,omitempty"`
	Priority   string                 `json:"priority,omitempty"`
}

// ExpoPushResponse 推送響應
type ExpoPushResponse struct {
	Data []ExpoPushResult `json:"data"`
}

type ExpoPushResult struct {
	Status  string `json:"status"`
	ID      string `json:"id,omitempty"`
	Message string `json:"message,omitempty"`
	Details struct {
		Error string `json:"error,omitempty"`
	} `json:"details,omitempty"`
}

func NewExpoService(logger zerolog.Logger) *ExpoService {
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	return &ExpoService{
		logger:     logger.With().Str("module", "expo_service").Logger(),
		Client:     client,
		PushAPIURL: "https://exp.host/--/api/v2/push/send",
	}
}

// Send 發送推送通知，保持與FCMService相同的接口
func (e *ExpoService) Send(ctx context.Context, token string, data map[string]interface{}, notification map[string]interface{}) error {
	// 為 data 添加 experienceId
	if data == nil {
		data = make(map[string]interface{})
	}
	data["experienceId"] = "@iewihc/DriverApp"

	// 構建Expo推送消息
	message := ExpoMessage{
		To:       token,
		Data:     data,
		Sound:    "new_order.wav", // 預設值
		Priority: "high",
		TTL:      300, // 5分鐘過期
	}

	// 添加通知內容
	if notification != nil {
		if title, ok := notification["title"].(string); ok {
			message.Title = title
		}
		if body, ok := notification["body"].(string); ok {
			message.Body = body
		}
		if sound, ok := notification["sound"].(string); ok {
			message.Sound = sound
		}
	}

	// 序列化消息
	jsonData, err := json.Marshal(message)
	if err != nil {
		e.logger.Error().Err(err).Msg("序列化Expo推送消息失敗")
		return fmt.Errorf("序列化Expo推送消息失敗: %v", err)
	}

	// 調試：打印發送的 payload
	e.logger.Info().Str("payload", string(jsonData)).Msg("發送給Expo的推送內容")

	// 創建HTTP請求
	req, err := http.NewRequestWithContext(ctx, "POST", e.PushAPIURL, bytes.NewBuffer(jsonData))
	if err != nil {
		e.logger.Error().Err(err).Msg("創建Expo推送請求失敗")
		return fmt.Errorf("創建Expo推送請求失敗: %v", err)
	}

	// 設置請求標頭
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Encoding", "gzip, deflate")

	// 發送請求
	resp, err := e.Client.Do(req)
	if err != nil {
		e.logger.Error().Err(err).Msg("發送Expo推送請求失敗")
		return fmt.Errorf("發送Expo推送請求失敗: %v", err)
	}
	defer resp.Body.Close()

	// 讀取響應
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		e.logger.Error().Err(err).Msg("讀取Expo推送響應失敗")
		return fmt.Errorf("讀取Expo推送響應失敗: %v", err)
	}

	// 檢查HTTP狀態碼
	if resp.StatusCode != http.StatusOK {
		e.logger.Error().Str("status", resp.Status).Str("response_body", string(body)).Msg("Expo推送API返回錯誤")
		return fmt.Errorf("Expo推送API返回錯誤: %s - %s", resp.Status, string(body))
	}

	// 解析響應以檢查推送結果
	// 根據官方文檔，Expo API 總是返回陣列格式，但實際可能有變化
	// 先嘗試標準陣列格式，如果失敗則嘗試物件格式
	var pushResponse ExpoPushResponse
	var result ExpoPushResult

	if err := json.Unmarshal(body, &pushResponse); err == nil && len(pushResponse.Data) > 0 {
		// 標準陣列格式成功
		result = pushResponse.Data[0]
	} else {
		// 嘗試非標準物件格式（用於相容性）
		var singleResponse struct {
			Data ExpoPushResult `json:"data"`
		}
		if err := json.Unmarshal(body, &singleResponse); err != nil {
			e.logger.Error().Err(err).Str("response_body", string(body)).Msg("解析Expo推送響應失敗")
			return fmt.Errorf("解析Expo推送響應失敗: %v", err)
		}
		result = singleResponse.Data
		e.logger.Debug().Str("response_body", string(body)).Msg("收到Expo單個物件響應格式（已正常處理）")
	}

	// 檢查推送結果
	if result.Status == "error" {
		e.logger.Error().Str("error", result.Details.Error).Str("message", result.Message).Msg("Expo推送失敗")
		return fmt.Errorf("Expo推送失敗: %s - %s", result.Message, result.Details.Error)
	}
	e.logger.Info().Str("status", result.Status).Str("id", result.ID).Msg("Expo推送成功")

	return nil
}

// 確保ExpoService實現了FCMService接口
var _ interfaces.FCMService = (*ExpoService)(nil)
