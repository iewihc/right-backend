package controller

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

// SSEController 管理所有 SSE 連接和事件推送
type SSEController struct {
	logger    zerolog.Logger
	clients   map[string]*SSEClient
	clientsMu sync.RWMutex
}

// SSEClient 代表一個SSE連接
type SSEClient struct {
	ID        string
	Writer    http.ResponseWriter
	Flusher   http.Flusher
	Request   *http.Request
	Events    chan SSEEvent
	Done      chan struct{}
	closeOnce sync.Once
}

// SSEEvent SSE事件結構
type SSEEvent struct {
	Event string      `json:"event"`
	Data  interface{} `json:"data"`
}

// PageUpdateEvent 頁面更新事件
type PageUpdateEvent struct {
	EventName string      `json:"event_name"`
	Pages     []string    `json:"pages"`
	Data      interface{} `json:"data,omitempty"`
}

// NewSSEController 建立新的 SSE 控制器
func NewSSEController(logger zerolog.Logger) *SSEController {
	sse := &SSEController{
		logger:  logger.With().Str("module", "sse_controller").Logger(),
		clients: make(map[string]*SSEClient),
	}

	// 啟動定期清理無效連接
	go sse.cleanup()

	return sse
}

// handleSSE 處理 SSE 連接
func (sse *SSEController) handleSSE(w http.ResponseWriter, r *http.Request) {
	// 設置 SSE 標頭
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Cache-Control")

	flusher, ok := w.(http.Flusher)
	if !ok {
		sse.logger.Error().Msg("Streaming unsupported")
		http.Error(w, "Streaming unsupported!", http.StatusInternalServerError)
		return
	}

	// 生成客戶端ID
	clientID := fmt.Sprintf("client_%d_%s", time.Now().UnixNano(), r.RemoteAddr)

	client := &SSEClient{
		ID:      clientID,
		Writer:  w,
		Flusher: flusher,
		Request: r,
		Events:  make(chan SSEEvent, 100),
		Done:    make(chan struct{}),
	}

	// 註冊客戶端
	sse.registerClient(client)
	defer sse.unregisterClient(client)

	// 發送初始連接確認訊息
	sse.sendEvent(client, SSEEvent{
		Event: "connected",
		Data: map[string]interface{}{
			"client_id": clientID,
			"timestamp": time.Now().Format("15:04"),
			"message":   "SSE 連接建立成功",
		},
	})

	sse.logger.Debug().
		Str("客戶端ID/client_id", clientID).
		Msg("SSE 客戶端已連接/SSE client connected")

	// 監聽客戶端斷開和事件發送
	for {
		select {
		case event := <-client.Events:
			if !sse.sendEvent(client, event) {
				return
			}
		case <-client.Done:
			return
		case <-r.Context().Done():
			return
		}
	}
}

// registerClient 註冊新的SSE客戶端
func (sse *SSEController) registerClient(client *SSEClient) {
	sse.clientsMu.Lock()
	defer sse.clientsMu.Unlock()
	sse.clients[client.ID] = client
}

// unregisterClient 註銷SSE客戶端
func (sse *SSEController) unregisterClient(client *SSEClient) {
	sse.clientsMu.Lock()
	defer sse.clientsMu.Unlock()

	if _, exists := sse.clients[client.ID]; exists {
		delete(sse.clients, client.ID)
		client.closeOnce.Do(func() {
			close(client.Done)
			close(client.Events)
		})
		sse.logger.Debug().
			Str("客戶端ID/client_id", client.ID).
			Msg("SSE 客戶端已斷開連接/SSE client disconnected")
	}
}

// sendEvent 發送事件給單一客戶端
func (sse *SSEController) sendEvent(client *SSEClient, event SSEEvent) bool {
	data, err := json.Marshal(event.Data)
	if err != nil {
		sse.logger.Error().
			Err(err).
			Msg("序列化事件資料失敗/Failed to serialize event data")
		return false
	}

	// 格式化 SSE 訊息
	message := fmt.Sprintf("event: %s\ndata: %s\n\n", event.Event, string(data))

	if _, err := client.Writer.Write([]byte(message)); err != nil {
		sse.logger.Error().
			Err(err).
			Str("客戶端ID/client_id", client.ID).
			Msg("發送 SSE 事件失敗/Failed to send SSE event")
		return false
	}

	client.Flusher.Flush()
	return true
}

// BroadcastPageUpdate 廣播頁面更新事件給所有連接的客戶端
func (sse *SSEController) BroadcastPageUpdate(eventName string, pages []string, data interface{}) {
	event := SSEEvent{
		Event: "page_update",
		Data: PageUpdateEvent{
			EventName: eventName,
			Pages:     pages,
			Data:      data,
		},
	}

	sse.clientsMu.RLock()
	clients := make([]*SSEClient, 0, len(sse.clients))
	for _, client := range sse.clients {
		clients = append(clients, client)
	}
	sse.clientsMu.RUnlock()

	//sse.logger.Info().
	//	Str("事件名稱/event_name", eventName).
	//	Strs("頁面/pages", pages).
	//	Int("客戶端數量/client_count", len(clients)).
	//	Msg("廣播頁面更新事件/Broadcasting page update event")

	for _, client := range clients {
		select {
		case client.Events <- event:
			// 事件發送成功
		default:
			// 客戶端事件隊列已滿，跳過該客戶端
			sse.logger.Warn().
				Str("客戶端ID/client_id", client.ID).
				Msg("跳過客戶端，事件隊列已滿/Skipping client, event queue is full")
		}
	}
}

// BroadcastCustomEvent 廣播自定義事件
func (sse *SSEController) BroadcastCustomEvent(eventType string, data interface{}) {
	event := SSEEvent{
		Event: eventType,
		Data:  data,
	}

	sse.clientsMu.RLock()
	clients := make([]*SSEClient, 0, len(sse.clients))
	for _, client := range sse.clients {
		clients = append(clients, client)
	}
	sse.clientsMu.RUnlock()

	sse.logger.Info().
		Str("事件類型/event_type", eventType).
		Int("客戶端數量/client_count", len(clients)).
		Msg("廣播自定義事件/Broadcasting custom event")

	for _, client := range clients {
		select {
		case client.Events <- event:
			// 事件發送成功
		default:
			sse.logger.Warn().
				Str("客戶端ID/client_id", client.ID).
				Msg("跳過客戶端，事件隊列已滿/Skipping client, event queue is full")
		}
	}
}

// GetStats 獲取SSE連接統計資訊
func (sse *SSEController) GetStats() map[string]interface{} {
	sse.clientsMu.RLock()
	connectedClients := len(sse.clients)
	sse.clientsMu.RUnlock()

	return map[string]interface{}{
		"connected_clients": connectedClients,
	}
}

// cleanup 定期清理無效連接
func (sse *SSEController) cleanup() {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		sse.clientsMu.Lock()
		var toRemove []string

		for clientID, client := range sse.clients {
			// 檢查客戶端連接是否仍然有效
			select {
			case <-client.Done:
				toRemove = append(toRemove, clientID)
			default:
				// 連接仍然有效
			}
		}

		// 移除無效連接
		for _, clientID := range toRemove {
			if client, exists := sse.clients[clientID]; exists {
				delete(sse.clients, clientID)
				client.closeOnce.Do(func() {
					close(client.Done)
					close(client.Events)
				})
				sse.logger.Info().
					Str("客戶端ID/client_id", clientID).
					Msg("清理無效的 SSE 客戶端/Cleaning up invalid SSE client")
			}
		}

		sse.clientsMu.Unlock()
	}
}

// GetSSEHandler 返回 SSE 處理函數，用於在 Chi 路由器上註冊
func (sse *SSEController) GetSSEHandler() http.HandlerFunc {
	return sse.handleSSE
}
