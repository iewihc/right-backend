package model

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// MessageType 消息類型
type MessageType string

const (
	MessageTypeText  MessageType = "text"
	MessageTypeAudio MessageType = "audio"
	MessageTypeImage MessageType = "image"
)

// SenderType 發送者類型
type SenderType string

const (
	SenderTypeDriver  SenderType = "driver"
	SenderTypeSupport SenderType = "support"
	SenderTypeUser    SenderType = "user"
)

// MessageStatus 消息狀態
type MessageStatus string

const (
	MessageStatusSending   MessageStatus = "sending"
	MessageStatusSent      MessageStatus = "sent"
	MessageStatusDelivered MessageStatus = "delivered"
	MessageStatusRead      MessageStatus = "read"
	MessageStatusFailed    MessageStatus = "failed"
)

// OrderStatus 訂單狀態（用於聊天房間）
type OrderChatStatus string

const (
	OrderChatStatusWaiting           OrderChatStatus = "waiting"
	OrderChatStatusAccepted          OrderChatStatus = "accepted"
	OrderChatStatusDriverArrived     OrderChatStatus = "driver_arrived"
	OrderChatStatusPassengerPickedUp OrderChatStatus = "passenger_picked_up"
	OrderChatStatusTripCompleted     OrderChatStatus = "trip_completed"
	OrderChatStatusTripCancelled     OrderChatStatus = "trip_cancelled"
)

// ReadStatus 已讀狀態
type ReadStatus struct {
	UserID string    `bson:"userId" json:"userId"`
	ReadAt time.Time `bson:"readAt" json:"readAt"`
}

// ChatOrderInfo 聊天中的訂單信息快照
type ChatOrderInfo struct {
	OrderID             string          `bson:"orderId" json:"orderId"`
	ShortID             string          `bson:"shortId" json:"shortId"`
	OriText             string          `bson:"oriText" json:"oriText"`
	Status              OrderChatStatus `bson:"status" json:"status"`
	PickupAddress       string          `bson:"pickupAddress" json:"pickupAddress"`
	DestinationAddress  string          `bson:"destinationAddress" json:"destinationAddress"`
	PassengerName       string          `bson:"passengerName" json:"passengerName"`
	PassengerPhone      string          `bson:"passengerPhone" json:"passengerPhone"`
	EstimatedPickupTime *time.Time      `bson:"estimatedPickupTime,omitempty" json:"estimatedPickupTime,omitempty"`
	CreatedAt           time.Time       `bson:"createdAt" json:"createdAt"`
}

// ChatMessage 聊天消息
type ChatMessage struct {
	ID            primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	OrderID       string             `bson:"orderId" json:"orderId"`
	Type          MessageType        `bson:"type" json:"type"`
	Sender        SenderType         `bson:"sender" json:"sender"`
	SenderID      string             `bson:"senderId" json:"senderId"`
	Content       *string            `bson:"content,omitempty" json:"content,omitempty"`
	AudioURL      *string            `bson:"audioUrl,omitempty" json:"audioUrl,omitempty"`
	AudioDuration *int               `bson:"audioDuration,omitempty" json:"audioDuration,omitempty"` // 音頻時長（秒）
	ImageURL      *string            `bson:"imageUrl,omitempty" json:"imageUrl,omitempty"`
	Status        MessageStatus      `bson:"status" json:"status"`
	TempID        *string            `bson:"tempId,omitempty" json:"tempId,omitempty"`
	ReadBy        []ReadStatus       `bson:"readBy" json:"readBy"`
	IsRecalled    bool               `bson:"isRecalled" json:"isRecalled"`
	RecalledAt    *time.Time         `bson:"recalledAt,omitempty" json:"recalledAt,omitempty"`
	RecalledBy    *string            `bson:"recalledBy,omitempty" json:"recalledBy,omitempty"`
	CreatedAt     time.Time          `bson:"createdAt" json:"createdAt"`
	UpdatedAt     time.Time          `bson:"updatedAt" json:"updatedAt"`
}

// OrderChat 聊天房間
type OrderChat struct {
	ID            primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	OrderID       string             `bson:"orderId" json:"orderId"`
	DriverID      string             `bson:"driverId" json:"driverId"`
	SupportUserID *string            `bson:"supportUserId,omitempty" json:"supportUserId,omitempty"`
	OrderInfo     ChatOrderInfo      `bson:"orderInfo" json:"orderInfo"`
	IsActive      bool               `bson:"isActive" json:"isActive"`
	CreatedAt     time.Time          `bson:"createdAt" json:"createdAt"`
	UpdatedAt     time.Time          `bson:"updatedAt" json:"updatedAt"`
}

// ChatUnreadCount 未讀數量
type ChatUnreadCount struct {
	ID                primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	OrderID           string             `bson:"orderId" json:"orderId"`
	UserID            string             `bson:"userId" json:"userId"`
	UserType          SenderType         `bson:"userType" json:"userType"`
	Count             int                `bson:"count" json:"count"`
	LastReadMessageID *string            `bson:"lastReadMessageId,omitempty" json:"lastReadMessageId,omitempty"`
	UpdatedAt         time.Time          `bson:"updatedAt" json:"updatedAt"`
}
