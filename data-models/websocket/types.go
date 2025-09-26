package websocket

// WebSocket 消息類型常量
const (
	// 請求類型
	MessageTypeCheckNotifyingOrder = "check_notifying_order"
	MessageTypeCheckCancelingOrder = "check_canceling_order"
	MessageTypeLocationUpdate      = "location_update"
	MessageTypePing                = "ping"

	// 聊天相關請求類型
	MessageTypeChatSendMessage   = "chat_send_message"
	MessageTypeChatGetHistory    = "chat_get_history"
	MessageTypeChatMarkAsRead    = "chat_mark_as_read"
	MessageTypeChatRecallMessage = "chat_recall_message"
	MessageTypeChatTypingStart   = "chat_typing_start"
	MessageTypeChatTypingEnd     = "chat_typing_end"

	// 回應類型
	MessageTypeCheckNotifyingOrderResponse = "check_notifying_order_response"
	MessageTypeCheckCancelingOrderResponse = "check_canceling_order_response"
	MessageTypeLocationUpdateResponse      = "location_update_response"
	MessageTypePong                        = "pong"

	// 聊天相關回應類型
	MessageTypeChatSendResponse        = "chat_send_response"
	MessageTypeChatReceiveMessage      = "chat_receive_message"
	MessageTypeChatMessageStatusUpdate = "chat_message_status_update"
	MessageTypeChatHistoryResponse     = "chat_history_response"
	MessageTypeChatUnreadCountUpdate   = "chat_unread_count_update"
	MessageTypeChatMessageRecalled     = "chat_message_recalled"
	MessageTypeChatTypingUpdate        = "chat_typing_update"
	MessageTypeChatError               = "chat_error"

	// 推送類型
	MessageTypeOrderUpdate       = "order_update"
	MessageTypeOrderStatusUpdate = "order_status_update"
)

// WebSocket 連線狀態
type ConnectionStatus string

const (
	ConnectionStatusConnected    ConnectionStatus = "connected"
	ConnectionStatusDisconnected ConnectionStatus = "disconnected"
	ConnectionStatusReconnecting ConnectionStatus = "reconnecting"
)

// WebSocket 錯誤類型
type ErrorType string

const (
	ErrorTypeInvalidToken      ErrorType = "invalid_token"
	ErrorTypeInvalidMessage    ErrorType = "invalid_message"
	ErrorTypeServiceError      ErrorType = "service_error"
	ErrorTypeConnectionTimeout ErrorType = "connection_timeout"
)
