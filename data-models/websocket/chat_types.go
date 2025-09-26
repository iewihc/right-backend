package websocket

import (
	"right-backend/model"
	"time"
)

// 聊天相關的常量（消息類型常量已在 types.go 中定義）

// 聊天錯誤類型
type ChatErrorType string

const (
	ChatErrorPermissionDenied   ChatErrorType = "PERMISSION_DENIED"
	ChatErrorOrderNotFound      ChatErrorType = "ORDER_NOT_FOUND"
	ChatErrorInvalidMessage     ChatErrorType = "INVALID_MESSAGE"
	ChatErrorUploadFailed       ChatErrorType = "UPLOAD_FAILED"
	ChatErrorRecallFailed       ChatErrorType = "RECALL_FAILED"
	ChatErrorMessageNotFound    ChatErrorType = "MESSAGE_NOT_FOUND"
	ChatErrorRecallTimeExceeded ChatErrorType = "RECALL_TIME_EXCEEDED"
)

// 聊天消息請求
type ChatMessage struct {
	OrderID   string            `json:"orderId"`
	Type      model.MessageType `json:"type"`
	Content   *string           `json:"content,omitempty"`
	AudioData *string           `json:"audioData,omitempty"`
	ImageData *string           `json:"imageData,omitempty"`
	TempID    string            `json:"tempId"`
}

// 發送聊天消息請求
type ChatSendMessageRequest struct {
	OrderID string      `json:"orderId"`
	Message ChatMessage `json:"message"`
}

// 獲取聊天歷史請求
type ChatHistoryRequest struct {
	OrderID         string  `json:"orderId"`
	Limit           *int    `json:"limit,omitempty"`
	Offset          *int    `json:"offset,omitempty"`
	BeforeMessageID *string `json:"beforeMessageId,omitempty"`
}

type ChatGetHistoryRequest struct {
	OrderID        string             `json:"orderId"`
	HistoryRequest ChatHistoryRequest `json:"historyRequest"`
}

// 標記已讀請求
type ChatMarkAsReadRequest struct {
	OrderID string `json:"orderId"`
}

// 輸入狀態請求
type ChatTypingRequest struct {
	OrderID string `json:"orderId"`
}

// 收回訊息請求
type ChatRecallMessageRequest struct {
	OrderID   string `json:"orderId"`
	MessageID string `json:"messageId"`
}

// 聊天消息回應
type ChatMessageResponse struct {
	ID        string              `json:"id"`
	OrderID   string              `json:"orderId"`
	Type      model.MessageType   `json:"type"`
	Sender    model.SenderType    `json:"sender"`
	Content   *string             `json:"content,omitempty"`
	AudioURL  *string             `json:"audioUrl,omitempty"`
	ImageURL  *string             `json:"imageUrl,omitempty"`
	Timestamp time.Time           `json:"timestamp"`
	Status    model.MessageStatus `json:"status"`
}

// 接收聊天消息事件
type ChatReceiveMessageEvent struct {
	OrderID string              `json:"orderId"`
	Message ChatMessageResponse `json:"message"`
}

// 消息狀態更新事件
type ChatMessageStatusUpdateEvent struct {
	MessageID string              `json:"messageId"`
	OrderID   string              `json:"orderId"`
	Status    model.MessageStatus `json:"status"`
}

// 聊天歷史回應
type ChatHistoryResponse struct {
	OrderID  string                `json:"orderId"`
	Messages []ChatMessageResponse `json:"messages"`
	Total    int                   `json:"total"`
	HasMore  bool                  `json:"hasMore"`
}

// 未讀數量更新事件
type ChatUnreadCountUpdateEvent struct {
	OrderID     string `json:"orderId"`
	UnreadCount int    `json:"unreadCount"`
}

// 輸入狀態更新事件
type ChatTypingUpdateEvent struct {
	OrderID  string `json:"orderId"`
	UserID   string `json:"userId"`
	IsTyping bool   `json:"isTyping"`
}

// 訊息收回通知事件
type ChatMessageRecalledEvent struct {
	OrderID    string           `json:"orderId"`
	MessageID  string           `json:"messageId"`
	RecalledBy model.SenderType `json:"recalledBy"`
	RecalledAt time.Time        `json:"recalledAt"`
}

// 聊天錯誤事件
type ChatErrorEvent struct {
	Code    ChatErrorType `json:"code"`
	Message string        `json:"message"`
	OrderID *string       `json:"orderId,omitempty"`
}

// 文件上傳回應
type FileUploadResponse struct {
	Success bool            `json:"success"`
	Data    *FileUploadData `json:"data,omitempty"`
	Error   *ErrorResponse  `json:"error,omitempty"`
}

type FileUploadData struct {
	URL           string `json:"url"`
	MessageID     string `json:"messageId"`
	AudioDuration *int   `json:"audioDuration,omitempty"` // 音頻時長(秒)，僅音頻文件有值
}

type ErrorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// 聊天房間信息
type ChatRoomInfo struct {
	OrderID     string              `json:"orderId"`
	DriverID    string              `json:"driverId"`
	OrderInfo   ChatOrderInfo       `json:"orderInfo"`
	UnreadCount int                 `json:"unreadCount"`
	LastMessage *ChatMessageSummary `json:"lastMessage,omitempty"`
	IsActive    bool                `json:"isActive"`
	CreatedAt   time.Time           `json:"createdAt"`
	UpdatedAt   time.Time           `json:"updatedAt"`
}

// 聊天消息摘要（用於最新消息顯示）
type ChatMessageSummary struct {
	ID        string            `json:"id"`
	Type      model.MessageType `json:"type"`
	Sender    model.SenderType  `json:"sender"`
	Content   *string           `json:"content,omitempty"`
	Timestamp time.Time         `json:"timestamp"`
}

// 聊天訂單信息（簡化版本）
type ChatOrderInfo struct {
	OrderID             string     `json:"orderId"`
	ShortID             string     `json:"shortId"`
	OriText             string     `json:"oriText"`
	Status              string     `json:"status"`
	PickupAddress       string     `json:"pickupAddress"`
	DestinationAddress  string     `json:"destinationAddress"`
	PassengerName       string     `json:"passengerName"`
	PassengerPhone      string     `json:"passengerPhone"`
	EstimatedPickupTime *time.Time `json:"estimatedPickupTime,omitempty"`
	CreatedAt           time.Time  `json:"createdAt"`
}

// 聊天房間列表回應
type ChatRoomsResponse struct {
	Success bool           `json:"success"`
	Data    []ChatRoomInfo `json:"data,omitempty"`
	Error   *ErrorResponse `json:"error,omitempty"`
}
