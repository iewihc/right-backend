package service

import (
	"context"
	"fmt"
	"mime/multipart"
	"right-backend/model"
	"strings"
	"time"

	websocketModels "right-backend/data-models/websocket"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type ChatService struct {
	logger             zerolog.Logger
	db                 *mongo.Database
	orderService       *OrderService
	driverService      *DriverService
	fileStorageService *FileStorageService
	discordService     *DiscordService
	baseURL            string
}

func NewChatService(logger zerolog.Logger, db *mongo.Database, orderService *OrderService, driverService *DriverService, fileStorageService *FileStorageService, discordService *DiscordService, baseURL string) *ChatService {
	return &ChatService{
		logger:             logger.With().Str("module", "chat_service").Logger(),
		db:                 db,
		orderService:       orderService,
		driverService:      driverService,
		fileStorageService: fileStorageService,
		discordService:     discordService,
		baseURL:            baseURL,
	}
}

// 聊天房間相關方法

// CreateOrGetChatRoom 創建或獲取聊天房間
func (cs *ChatService) CreateOrGetChatRoom(ctx context.Context, orderID, driverID string) (*model.OrderChat, error) {
	// 檢查訂單是否存在
	order, err := cs.orderService.GetOrderByID(ctx, orderID)
	if err != nil {
		return nil, fmt.Errorf("訂單不存在: %w", err)
	}

	// 檢查司機是否存在
	_, err = cs.driverService.GetDriverByID(ctx, driverID)
	if err != nil {
		return nil, fmt.Errorf("司機不存在: %w", err)
	}

	collection := cs.db.Collection("order_chats")

	// 嘗試獲取現有聊天房間
	var chat model.OrderChat
	err = collection.FindOne(ctx, bson.M{"orderId": orderID}).Decode(&chat)
	if err == nil {
		return &chat, nil
	}

	if err != mongo.ErrNoDocuments {
		return nil, fmt.Errorf("查詢聊天房間失敗: %w", err)
	}

	// 創建新的聊天房間
	now := time.Now()
	orderInfo := model.ChatOrderInfo{
		OrderID:             order.ID.Hex(),
		ShortID:             order.ShortID,
		OriText:             order.OriText,
		Status:              model.OrderChatStatus(order.Status),
		PickupAddress:       order.Customer.PickupAddress,
		DestinationAddress:  order.Customer.DestAddress,
		PassengerName:       order.Driver.Name, // 目前 Customer 結構沒有 Name 字段，暫用司機名稱
		PassengerPhone:      "",                // Customer 結構沒有電話字段，暫時留空
		EstimatedPickupTime: nil,               // Driver.EstPickupTime 是 string，需要轉換，暫時留空
		CreatedAt:           *order.CreatedAt,
	}

	chat = model.OrderChat{
		OrderID:   orderID,
		DriverID:  driverID,
		OrderInfo: orderInfo,
		IsActive:  true,
		CreatedAt: now,
		UpdatedAt: now,
	}

	result, err := collection.InsertOne(ctx, chat)
	if err != nil {
		return nil, fmt.Errorf("創建聊天房間失敗: %w", err)
	}

	chat.ID = result.InsertedID.(primitive.ObjectID)
	cs.logger.Info().Str("order_id", orderID).Str("driver_id", driverID).Msg("已創建新的聊天房間")

	return &chat, nil
}

// GetChatRoomsByDriverID 獲取司機的所有聊天房間
func (cs *ChatService) GetChatRoomsByDriverID(ctx context.Context, driverID string) ([]model.OrderChat, error) {
	collection := cs.db.Collection("order_chats")

	cursor, err := collection.Find(ctx, bson.M{
		"driverId": driverID,
		"isActive": true,
	}, options.Find().SetSort(bson.D{{Key: "updatedAt", Value: -1}}))

	if err != nil {
		return nil, fmt.Errorf("獲取聊天房間失敗: %w", err)
	}
	defer cursor.Close(ctx)

	var chatRooms []model.OrderChat
	if err = cursor.All(ctx, &chatRooms); err != nil {
		return nil, fmt.Errorf("解析聊天房間數據失敗: %w", err)
	}

	return chatRooms, nil
}

// 消息相關方法

// SendMessage 發送消息
func (cs *ChatService) SendMessage(ctx context.Context, orderID, senderID string, senderType model.SenderType, msgType model.MessageType, content, audioURL, imageURL *string, audioDuration *int, tempID *string) (*model.ChatMessage, error) {
	cs.logger.Info().
		Str("order_id", orderID).
		Str("sender_id", senderID).
		Str("sender_type", string(senderType)).
		Str("message_type", string(msgType)).
		Msg("開始發送聊天消息")

	// 驗證輸入參數
	if orderID == "" {
		cs.logger.Error().Msg("訂單ID不能為空")
		return nil, fmt.Errorf("訂單ID不能為空")
	}
	if senderID == "" {
		cs.logger.Error().Msg("發送者ID不能為空")
		return nil, fmt.Errorf("發送者ID不能為空")
	}

	// 驗證消息內容
	if msgType == model.MessageTypeText && (content == nil || *content == "") {
		cs.logger.Error().Str("message_type", string(msgType)).Msg("文字消息內容不能為空")
		return nil, fmt.Errorf("文字消息內容不能為空")
	}

	// 驗證權限
	if err := cs.validateMessagePermission(ctx, orderID, senderID, senderType); err != nil {
		cs.logger.Error().
			Err(err).
			Str("order_id", orderID).
			Str("sender_id", senderID).
			Msg("消息權限驗證失敗")
		return nil, err
	}

	now := time.Now()
	message := model.ChatMessage{
		OrderID:       orderID,
		Type:          msgType,
		Sender:        senderType,
		SenderID:      senderID,
		Content:       content,
		AudioURL:      audioURL,
		AudioDuration: audioDuration,
		ImageURL:      imageURL,
		Status:        model.MessageStatusSent,
		TempID:        tempID,
		ReadBy:        []model.ReadStatus{},
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	collection := cs.db.Collection("chat_messages")
	result, err := collection.InsertOne(ctx, message)
	if err != nil {
		return nil, fmt.Errorf("發送消息失敗: %w", err)
	}

	message.ID = result.InsertedID.(primitive.ObjectID)

	// 更新聊天房間活動時間
	cs.updateChatRoomActivity(ctx, orderID)

	// 更新接收方未讀數量
	cs.updateUnreadCount(ctx, orderID, senderID, senderType, 1)

	cs.logger.Info().
		Str("order_id", orderID).
		Str("sender_id", senderID).
		Str("message_type", string(msgType)).
		Msg("消息已發送")

	// 如果是司機發送的消息，發送Discord通知
	if senderType == model.SenderTypeDriver {
		go cs.sendDiscordChatNotification(context.Background(), orderID, senderID, msgType, content, imageURL)
	}

	return &message, nil
}

// GetChatHistory 獲取聊天歷史
func (cs *ChatService) GetChatHistory(ctx context.Context, orderID, userID string, userType model.SenderType, limit, offset int, beforeMessageID *string) ([]model.ChatMessage, int, bool, error) {
	// 驗證權限
	if err := cs.validateMessagePermission(ctx, orderID, userID, userType); err != nil {
		return nil, 0, false, err
	}

	collection := cs.db.Collection("chat_messages")

	// 構建查詢條件
	filter := bson.M{"orderId": orderID}
	if beforeMessageID != nil {
		objID, err := primitive.ObjectIDFromHex(*beforeMessageID)
		if err == nil {
			filter["_id"] = bson.M{"$lt": objID}
		}
	}

	// 獲取總數
	total, err := collection.CountDocuments(ctx, bson.M{"orderId": orderID})
	if err != nil {
		return nil, 0, false, fmt.Errorf("獲取消息總數失敗: %w", err)
	}

	// 獲取消息
	opts := options.Find().
		SetSort(bson.D{{Key: "createdAt", Value: -1}}).
		SetSkip(int64(offset)).
		SetLimit(int64(limit))

	cursor, err := collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, 0, false, fmt.Errorf("獲取聊天歷史失敗: %w", err)
	}
	defer cursor.Close(ctx)

	var messages []model.ChatMessage
	if err = cursor.All(ctx, &messages); err != nil {
		return nil, 0, false, fmt.Errorf("解析聊天歷史失敗: %w", err)
	}

	// 轉換文件URL為完整URL
	messages = cs.ConvertMessageURLs(messages)

	hasMore := int(total) > offset+len(messages)

	return messages, int(total), hasMore, nil
}

// MarkAsRead 標記為已讀
func (cs *ChatService) MarkAsRead(ctx context.Context, orderID, userID string, userType model.SenderType) error {
	// 驗證權限
	if err := cs.validateMessagePermission(ctx, orderID, userID, userType); err != nil {
		return err
	}

	// 獲取最新消息
	collection := cs.db.Collection("chat_messages")
	var lastMessage model.ChatMessage
	err := collection.FindOne(ctx, bson.M{"orderId": orderID}, options.FindOne().SetSort(bson.D{{Key: "createdAt", Value: -1}})).Decode(&lastMessage)
	if err == mongo.ErrNoDocuments {
		return nil // 沒有消息不需要標記
	}
	if err != nil {
		return fmt.Errorf("獲取最新消息失敗: %w", err)
	}

	// 更新未讀數量為0
	cs.updateUnreadCount(ctx, orderID, userID, userType, -999) // 使用負數重置為0

	cs.logger.Info().
		Str("order_id", orderID).
		Str("user_id", userID).
		Msg("已標記聊天為已讀")

	return nil
}

// RecallMessage 收回訊息
func (cs *ChatService) RecallMessage(ctx context.Context, messageID, userID string, userType model.SenderType) error {
	cs.logger.Info().
		Str("message_id", messageID).
		Str("user_id", userID).
		Str("user_type", string(userType)).
		Msg("開始收回聊天訊息")

	// 驗證訊息ID格式
	objID, err := primitive.ObjectIDFromHex(messageID)
	if err != nil {
		return fmt.Errorf("無效的訊息ID格式: %w", err)
	}

	collection := cs.db.Collection("chat_messages")

	// 獲取訊息
	var message model.ChatMessage
	err = collection.FindOne(ctx, bson.M{"_id": objID}).Decode(&message)
	if err == mongo.ErrNoDocuments {
		return fmt.Errorf("訊息不存在")
	}
	if err != nil {
		return fmt.Errorf("獲取訊息失敗: %w", err)
	}

	// 驗證權限：只能收回自己的訊息
	if message.SenderID != userID {
		return fmt.Errorf("沒有權限收回此訊息")
	}

	// 檢查是否已被收回
	if message.IsRecalled {
		return fmt.Errorf("訊息已被收回")
	}

	// 檢查收回時間限制（24小時內）
	if time.Since(message.CreatedAt) > 24*time.Hour {
		return fmt.Errorf("收回時間已超過24小時")
	}

	// 標記訊息為已收回
	now := time.Now()
	update := bson.M{
		"$set": bson.M{
			"isRecalled": true,
			"recalledAt": &now,
			"recalledBy": &userID,
			"updatedAt":  now,
		},
	}

	_, err = collection.UpdateOne(ctx, bson.M{"_id": objID}, update)
	if err != nil {
		return fmt.Errorf("標記訊息為已收回失敗: %w", err)
	}

	cs.logger.Info().
		Str("message_id", messageID).
		Str("user_id", userID).
		Str("order_id", message.OrderID).
		Msg("訊息已成功收回")

	return nil
}

// UpdateMessageStatus 更新消息狀態
func (cs *ChatService) UpdateMessageStatus(ctx context.Context, messageID string, status model.MessageStatus) error {
	objID, err := primitive.ObjectIDFromHex(messageID)
	if err != nil {
		return fmt.Errorf("無效的消息ID: %w", err)
	}

	collection := cs.db.Collection("chat_messages")
	_, err = collection.UpdateOne(ctx,
		bson.M{"_id": objID},
		bson.M{"$set": bson.M{
			"status":    status,
			"updatedAt": time.Now(),
		}})

	if err != nil {
		return fmt.Errorf("更新消息狀態失敗: %w", err)
	}

	return nil
}

// 文件上傳相關方法

// UploadAudioFile 上傳音頻文件
func (cs *ChatService) UploadAudioFile(ctx context.Context, orderID, messageID string, file multipart.File, header *multipart.FileHeader) (string, error) {
	// 使用文件存儲服務上傳文件
	result, err := cs.fileStorageService.UploadAudioFile(ctx, file, header, orderID, messageID)
	if err != nil {
		cs.logger.Error().
			Err(err).
			Str("order_id", orderID).
			Str("message_id", messageID).
			Str("filename", header.Filename).
			Msg("音頻文件上傳失敗")
		return "", fmt.Errorf("音頻文件上傳失敗: %w", err)
	}

	cs.logger.Info().
		Str("order_id", orderID).
		Str("message_id", messageID).
		Str("filename", header.Filename).
		Str("url", result.URL).
		Str("relative_path", result.RelativePath).
		Int64("size", result.Size).
		Msg("音頻文件上傳成功")

	// 返回相對路徑，儲存到資料庫使用
	return result.RelativePath, nil
}

// UploadImageFile 上傳圖片文件
func (cs *ChatService) UploadImageFile(ctx context.Context, orderID, messageID string, file multipart.File, header *multipart.FileHeader) (string, error) {
	// 使用文件存儲服務上傳文件
	result, err := cs.fileStorageService.UploadImageFile(ctx, file, header, orderID, messageID)
	if err != nil {
		cs.logger.Error().
			Err(err).
			Str("order_id", orderID).
			Str("message_id", messageID).
			Str("filename", header.Filename).
			Msg("圖片文件上傳失敗")
		return "", fmt.Errorf("圖片文件上傳失敗: %w", err)
	}

	cs.logger.Info().
		Str("order_id", orderID).
		Str("message_id", messageID).
		Str("filename", header.Filename).
		Str("url", result.URL).
		Str("relative_path", result.RelativePath).
		Int64("size", result.Size).
		Msg("圖片文件上傳成功")

	// 返回相對路徑，儲存到資料庫使用
	return result.RelativePath, nil
}

// GetFileURL 將相對路徑轉換為完整URL
func (cs *ChatService) GetFileURL(relativePath string) string {
	if relativePath == "" {
		return ""
	}

	baseURL := strings.TrimSuffix(cs.baseURL, "/")
	cleanRelativePath := strings.TrimPrefix(relativePath, "/")

	// 確保 relativePath 以 uploads/ 開頭
	if !strings.HasPrefix(cleanRelativePath, "uploads/") {
		cleanRelativePath = "uploads/" + cleanRelativePath
	}

	fullURL := fmt.Sprintf("%s/%s", baseURL, cleanRelativePath)

	// 記錄生成的 URL 用於除錯
	cs.logger.Info().
		Str("relative_path", relativePath).
		Str("clean_relative_path", cleanRelativePath).
		Str("base_url", baseURL).
		Str("full_url", fullURL).
		Msg("生成檔案完整URL")

	return fullURL
}

// ConvertMessageURLs 轉換消息中的文件URL為完整URL
func (cs *ChatService) ConvertMessageURLs(messages []model.ChatMessage) []model.ChatMessage {
	for i := range messages {
		if messages[i].AudioURL != nil {
			fullURL := cs.GetFileURL(*messages[i].AudioURL)
			messages[i].AudioURL = &fullURL
		}
		if messages[i].ImageURL != nil {
			fullURL := cs.GetFileURL(*messages[i].ImageURL)
			messages[i].ImageURL = &fullURL
		}
	}
	return messages
}

// 輔助方法

// validateMessagePermission 驗證消息權限
func (cs *ChatService) validateMessagePermission(ctx context.Context, orderID, userID string, userType model.SenderType) error {
	if userType == model.SenderTypeDriver {
		// 驗證司機是否有權限訪問此訂單
		driver, err := cs.driverService.GetDriverByID(ctx, userID)
		if err != nil {
			return fmt.Errorf("司機不存在: %w", err)
		}

		// 檢查司機是否被分配到此訂單
		order, err := cs.orderService.GetOrderByID(ctx, orderID)
		if err != nil {
			return fmt.Errorf("訂單不存在: %w", err)
		}

		if order.Driver.AssignedDriver != driver.ID.Hex() {
			return fmt.Errorf("無權限訪問此訂單聊天")
		}
	} else if userType == model.SenderTypeSupport {
		// 對於客服用戶，允許訪問所有聊天
		// 特別處理 discord_support 這個特殊的客服ID
		if userID == "discord_support" {
			return nil // Discord客服總是有權限
		}
		// 其他客服也暫時允許訪問所有聊天
		return nil
	}

	return nil
}

// updateChatRoomActivity 更新聊天房間活動時間
func (cs *ChatService) updateChatRoomActivity(ctx context.Context, orderID string) {
	collection := cs.db.Collection("order_chats")
	collection.UpdateOne(ctx,
		bson.M{"orderId": orderID},
		bson.M{"$set": bson.M{"updatedAt": time.Now()}})
}

// updateUnreadCount 更新未讀數量
func (cs *ChatService) updateUnreadCount(ctx context.Context, orderID, senderID string, senderType model.SenderType, delta int) {
	collection := cs.db.Collection("chat_unread_counts")

	// 確定接收方
	var receiverType model.SenderType
	if senderType == model.SenderTypeDriver {
		receiverType = model.SenderTypeSupport
	} else {
		receiverType = model.SenderTypeDriver
	}

	// 獲取或創建未讀計數記錄
	filter := bson.M{
		"orderId":  orderID,
		"userType": receiverType,
	}

	if delta < 0 { // 重置為0
		collection.UpdateOne(ctx, filter, bson.M{
			"$set": bson.M{
				"count":     0,
				"updatedAt": time.Now(),
			},
		}, options.Update().SetUpsert(true))
	} else { // 增加計數
		collection.UpdateOne(ctx, filter, bson.M{
			"$inc": bson.M{"count": delta},
			"$set": bson.M{"updatedAt": time.Now()},
		}, options.Update().SetUpsert(true))
	}
}

// 文件相關的輔助方法已移至 FileStorageService

// GetUnreadCount 獲取未讀數量
func (cs *ChatService) GetUnreadCount(ctx context.Context, orderID, userID string, userType model.SenderType) (int, error) {
	collection := cs.db.Collection("chat_unread_counts")

	var unreadCount model.ChatUnreadCount
	err := collection.FindOne(ctx, bson.M{
		"orderId":  orderID,
		"userId":   userID,
		"userType": userType,
	}).Decode(&unreadCount)

	if err == mongo.ErrNoDocuments {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("獲取未讀數量失敗: %w", err)
	}

	return unreadCount.Count, nil
}

// GenerateTempMessageID 生成臨時消息ID
func (cs *ChatService) GenerateTempMessageID() string {
	return uuid.New().String()
}

// 聊天統計相關方法

// GetChatStatistics 獲取聊天統計數據
func (cs *ChatService) GetChatStatistics(ctx context.Context, driverID string) (*ChatStatistics, error) {
	cs.logger.Info().Str("driver_id", driverID).Msg("獲取聊天統計數據")

	stats := &ChatStatistics{
		DriverID:  driverID,
		QueryTime: time.Now(),
	}

	// 統計聊天房間數量
	roomCount, err := cs.getChatRoomCount(ctx, driverID)
	if err != nil {
		cs.logger.Error().Err(err).Str("driver_id", driverID).Msg("獲取聊天房間數量失敗")
		return nil, err
	}
	stats.TotalChatRooms = roomCount

	// 統計消息數量
	messageCount, err := cs.getTotalMessageCount(ctx, driverID)
	if err != nil {
		cs.logger.Error().Err(err).Str("driver_id", driverID).Msg("獲取消息數量失敗")
		return nil, err
	}
	stats.TotalMessages = messageCount

	// 統計未讀消息數量
	unreadCount, err := cs.getTotalUnreadCount(ctx, driverID)
	if err != nil {
		cs.logger.Error().Err(err).Str("driver_id", driverID).Msg("獲取未讀消息數量失敗")
		return nil, err
	}
	stats.UnreadMessages = unreadCount

	// 統計活躍聊天房間數量
	activeRoomCount, err := cs.getActiveChatRoomCount(ctx, driverID)
	if err != nil {
		cs.logger.Error().Err(err).Str("driver_id", driverID).Msg("獲取活躍聊天房間數量失敗")
		return nil, err
	}
	stats.ActiveChatRooms = activeRoomCount

	cs.logger.Info().
		Str("driver_id", driverID).
		Int("total_rooms", stats.TotalChatRooms).
		Int("total_messages", stats.TotalMessages).
		Int("unread_messages", stats.UnreadMessages).
		Msg("聊天統計數據獲取完成")

	return stats, nil
}

// getChatRoomCount 獲取聊天房間數量
func (cs *ChatService) getChatRoomCount(ctx context.Context, driverID string) (int, error) {
	collection := cs.db.Collection("order_chats")
	count, err := collection.CountDocuments(ctx, bson.M{"driverId": driverID})
	return int(count), err
}

// getTotalMessageCount 獲取總消息數量
func (cs *ChatService) getTotalMessageCount(ctx context.Context, driverID string) (int, error) {
	collection := cs.db.Collection("chat_messages")
	count, err := collection.CountDocuments(ctx, bson.M{"senderId": driverID})
	return int(count), err
}

// getTotalUnreadCount 獲取總未讀消息數量
func (cs *ChatService) getTotalUnreadCount(ctx context.Context, driverID string) (int, error) {
	collection := cs.db.Collection("chat_unread_counts")

	cursor, err := collection.Find(ctx, bson.M{
		"userId":   driverID,
		"userType": model.SenderTypeDriver,
	})
	if err != nil {
		return 0, err
	}
	defer cursor.Close(ctx)

	totalUnread := 0
	for cursor.Next(ctx) {
		var unreadCount model.ChatUnreadCount
		if err := cursor.Decode(&unreadCount); err == nil {
			totalUnread += unreadCount.Count
		}
	}

	return totalUnread, nil
}

// getActiveChatRoomCount 獲取活躍聊天房間數量
func (cs *ChatService) getActiveChatRoomCount(ctx context.Context, driverID string) (int, error) {
	collection := cs.db.Collection("order_chats")
	count, err := collection.CountDocuments(ctx, bson.M{
		"driverId": driverID,
		"isActive": true,
	})
	return int(count), err
}

// GetSystemChatStatistics 獲取系統聊天統計（管理員用）
func (cs *ChatService) GetSystemChatStatistics(ctx context.Context) (*SystemChatStatistics, error) {
	cs.logger.Info().Msg("獲取系統聊天統計數據")

	stats := &SystemChatStatistics{
		QueryTime: time.Now(),
	}

	// 統計總聊天房間數
	roomCollection := cs.db.Collection("order_chats")
	totalRooms, err := roomCollection.CountDocuments(ctx, bson.M{})
	if err != nil {
		return nil, fmt.Errorf("獲取總聊天房間數失敗: %w", err)
	}
	stats.TotalChatRooms = int(totalRooms)

	// 統計活躍聊天房間數
	activeRooms, err := roomCollection.CountDocuments(ctx, bson.M{"isActive": true})
	if err != nil {
		return nil, fmt.Errorf("獲取活躍聊天房間數失敗: %w", err)
	}
	stats.ActiveChatRooms = int(activeRooms)

	// 統計總消息數
	messageCollection := cs.db.Collection("chat_messages")
	totalMessages, err := messageCollection.CountDocuments(ctx, bson.M{})
	if err != nil {
		return nil, fmt.Errorf("獲取總消息數失敗: %w", err)
	}
	stats.TotalMessages = int(totalMessages)

	// 統計今日消息數
	today := time.Now().Truncate(24 * time.Hour)
	todayMessages, err := messageCollection.CountDocuments(ctx, bson.M{
		"createdAt": bson.M{"$gte": today},
	})
	if err != nil {
		return nil, fmt.Errorf("獲取今日消息數失敗: %w", err)
	}
	stats.TodayMessages = int(todayMessages)

	cs.logger.Info().
		Int("total_rooms", stats.TotalChatRooms).
		Int("active_rooms", stats.ActiveChatRooms).
		Int("total_messages", stats.TotalMessages).
		Int("today_messages", stats.TodayMessages).
		Msg("系統聊天統計數據獲取完成")

	return stats, nil
}

// ChatStatistics 聊天統計數據
type ChatStatistics struct {
	DriverID        string    `json:"driverId"`
	TotalChatRooms  int       `json:"totalChatRooms"`
	ActiveChatRooms int       `json:"activeChatRooms"`
	TotalMessages   int       `json:"totalMessages"`
	UnreadMessages  int       `json:"unreadMessages"`
	QueryTime       time.Time `json:"queryTime"`
}

// SystemChatStatistics 系統聊天統計數據
type SystemChatStatistics struct {
	TotalChatRooms  int       `json:"totalChatRooms"`
	ActiveChatRooms int       `json:"activeChatRooms"`
	TotalMessages   int       `json:"totalMessages"`
	TodayMessages   int       `json:"todayMessages"`
	QueryTime       time.Time `json:"queryTime"`
}

// GetUserRecentChats 查詢用戶最近聊天記錄
func (cs *ChatService) GetUserRecentChats(ctx context.Context, userID string, limit int) ([]websocketModels.ChatRoomInfo, error) {
	cs.logger.Info().
		Str("user_id", userID).
		Int("limit", limit).
		Msg("查詢用戶最近聊天記錄")

	// 查詢用戶參與的聊天房間，按最後更新時間排序
	roomCollection := cs.db.Collection("order_chats")

	// 查詢條件：用戶作為客服參與的聊天房間
	filter := bson.M{
		"supportUserId": userID,
		"isActive":      true,
	}

	// 按最後更新時間降序排序
	opts := options.Find().
		SetSort(bson.D{{Key: "updatedAt", Value: -1}}).
		SetLimit(int64(limit))

	cursor, err := roomCollection.Find(ctx, filter, opts)
	if err != nil {
		cs.logger.Error().
			Err(err).
			Str("user_id", userID).
			Msg("查詢聊天房間失敗")
		return nil, fmt.Errorf("查詢聊天房間失敗: %w", err)
	}
	defer cursor.Close(ctx)

	var chatRooms []model.OrderChat
	if err := cursor.All(ctx, &chatRooms); err != nil {
		cs.logger.Error().
			Err(err).
			Str("user_id", userID).
			Msg("解析聊天房間數據失敗")
		return nil, fmt.Errorf("解析聊天房間數據失敗: %w", err)
	}

	// 轉換為回應格式並獲取額外信息
	var result []websocketModels.ChatRoomInfo
	for _, room := range chatRooms {
		// 獲取最新消息
		lastMessage, err := cs.getLastMessageForRoom(ctx, room.OrderID)
		if err != nil {
			cs.logger.Warn().
				Err(err).
				Str("order_id", room.OrderID).
				Msg("獲取最新消息失敗，跳過")
			continue
		}

		// 獲取未讀數量
		unreadCount, err := cs.getUnreadCountForUser(ctx, room.OrderID, userID, model.SenderTypeUser)
		if err != nil {
			cs.logger.Warn().
				Err(err).
				Str("order_id", room.OrderID).
				Str("user_id", userID).
				Msg("獲取未讀數量失敗，設為0")
			unreadCount = 0
		}

		roomInfo := websocketModels.ChatRoomInfo{
			OrderID:     room.OrderID,
			DriverID:    room.DriverID,
			OrderInfo:   convertToChatOrderInfo(room.OrderInfo),
			LastMessage: lastMessage,
			UnreadCount: unreadCount,
			IsActive:    room.IsActive,
			CreatedAt:   room.CreatedAt,
			UpdatedAt:   room.UpdatedAt,
		}

		result = append(result, roomInfo)
	}

	cs.logger.Info().
		Str("user_id", userID).
		Int("requested_limit", limit).
		Int("found_rooms", len(result)).
		Msg("用戶最近聊天記錄查詢完成")

	return result, nil
}

// getLastMessageForRoom 獲取聊天房間的最新消息
func (cs *ChatService) getLastMessageForRoom(ctx context.Context, orderID string) (*websocketModels.ChatMessageSummary, error) {
	messageCollection := cs.db.Collection("chat_messages")

	// 查詢最新的一條消息
	opts := options.FindOne().SetSort(bson.D{{Key: "createdAt", Value: -1}})

	var message model.ChatMessage
	err := messageCollection.FindOne(ctx, bson.M{"orderId": orderID}, opts).Decode(&message)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil // 沒有消息是正常情況
		}
		return nil, err
	}

	return &websocketModels.ChatMessageSummary{
		ID:        message.ID.Hex(),
		Type:      message.Type,
		Sender:    message.Sender,
		Content:   message.Content,
		Timestamp: message.CreatedAt,
	}, nil
}

// getUnreadCountForUser 獲取用戶的未讀消息數量
func (cs *ChatService) getUnreadCountForUser(ctx context.Context, orderID, userID string, userType model.SenderType) (int, error) {
	unreadCollection := cs.db.Collection("chat_unread_counts")

	var unreadCount model.ChatUnreadCount
	err := unreadCollection.FindOne(ctx, bson.M{
		"orderId":  orderID,
		"userId":   userID,
		"userType": userType,
	}).Decode(&unreadCount)

	if err != nil {
		if err == mongo.ErrNoDocuments {
			return 0, nil // 沒有未讀記錄表示未讀數為0
		}
		return 0, err
	}

	return unreadCount.Count, nil
}

// convertToChatOrderInfo 轉換訂單信息格式
func convertToChatOrderInfo(orderInfo model.ChatOrderInfo) websocketModels.ChatOrderInfo {
	return websocketModels.ChatOrderInfo{
		OrderID:             orderInfo.OrderID,
		ShortID:             orderInfo.ShortID,
		OriText:             orderInfo.OriText,
		Status:              string(orderInfo.Status),
		PickupAddress:       orderInfo.PickupAddress,
		DestinationAddress:  orderInfo.DestinationAddress,
		PassengerName:       orderInfo.PassengerName,
		PassengerPhone:      orderInfo.PassengerPhone,
		EstimatedPickupTime: orderInfo.EstimatedPickupTime,
		CreatedAt:           orderInfo.CreatedAt,
	}
}

// sendDiscordChatNotification 發送司機聊天消息的Discord通知
func (cs *ChatService) sendDiscordChatNotification(ctx context.Context, orderID, senderID string, msgType model.MessageType, content, imageURL *string) {
	if cs.discordService == nil {
		cs.logger.Warn().Msg("Discord服務未初始化，跳過聊天通知")
		return
	}

	// 獲取訂單信息
	order, err := cs.orderService.GetOrderByID(ctx, orderID)
	if err != nil {
		cs.logger.Error().Err(err).Str("order_id", orderID).Msg("獲取訂單信息失敗，無法發送Discord聊天通知")
		return
	}

	// 檢查訂單是否有Discord消息資訊
	if order.DiscordChannelID == "" || order.DiscordMessageID == "" {
		cs.logger.Debug().Str("order_id", orderID).Msg("訂單沒有Discord消息資訊，跳過聊天通知")
		return
	}

	// 獲取司機信息
	driver, err := cs.driverService.GetDriverByID(ctx, senderID)
	if err != nil {
		cs.logger.Error().Err(err).Str("driver_id", senderID).Msg("獲取司機信息失敗，無法發送Discord聊天通知")
		return
	}

	// 格式化消息內容
	var messageContent string
	switch msgType {
	case model.MessageTypeText:
		if content != nil {
			messageContent = *content
		} else {
			messageContent = "(空消息)"
		}
	case model.MessageTypeImage:
		if imageURL != nil {
			// 檢查 imageURL 是否已經是完整 URL
			var fullImageURL string
			if strings.HasPrefix(*imageURL, "http://") || strings.HasPrefix(*imageURL, "https://") {
				// 已經是完整 URL，直接使用
				fullImageURL = *imageURL
			} else {
				// 是相對路徑，需要轉換為完整 URL
				fullImageURL = cs.GetFileURL(*imageURL)
			}
			messageContent = fmt.Sprintf("圖片: %s", fullImageURL)
		} else {
			messageContent = "圖片"
		}
	case model.MessageTypeAudio:
		messageContent = "語音消息"
	default:
		messageContent = string(msgType)
	}

	// 格式化Discord回覆消息
	// 格式：【WEI#7a2b| 料理鼠王(WEI測)(ABC-5808(黑))－Chat】: 訊息內容
	var carInfo string
	if driver.CarColor != "" {
		carInfo = fmt.Sprintf("%s(%s)", driver.CarPlate, driver.CarColor)
	} else {
		carInfo = driver.CarPlate
	}

	replyText := fmt.Sprintf("【%s%s| %s(%s)－Chat】: %s",
		string(order.Fleet),
		order.ShortID,
		driver.Name,
		carInfo,
		messageContent)

	// 除錯日誌：檢查格式化的值
	cs.logger.Debug().
		Str("fleet", string(order.Fleet)).
		Str("short_id", order.ShortID).
		Str("driver_name", driver.Name).
		Str("car_info", carInfo).
		Str("reply_text", replyText).
		Msg("Discord 回覆文字格式化")

	// 除錯日誌：檢查圖片條件
	cs.logger.Info().
		Str("order_id", orderID).
		Str("message_type", string(msgType)).
		Bool("is_image_type", msgType == model.MessageTypeImage).
		Bool("has_image_url", imageURL != nil).
		Interface("image_url_value", imageURL).
		Msg("檢查Discord圖片回覆條件")

	// 發送回覆到Discord訂單卡片（簡潔方案：使用footer嵌入orderID）
	if msgType == model.MessageTypeImage && imageURL != nil {
		// 檢查 imageURL 是否已經是完整 URL
		var fullImageURL string
		if strings.HasPrefix(*imageURL, "http://") || strings.HasPrefix(*imageURL, "https://") {
			// 已經是完整 URL，直接使用
			fullImageURL = *imageURL
		} else {
			// 是相對路徑，需要轉換為完整 URL
			fullImageURL = cs.GetFileURL(*imageURL)
		}

		cs.logger.Info().
			Str("order_id", orderID).
			Str("original_image_url", *imageURL).
			Str("full_image_url", fullImageURL).
			Str("base_url", cs.baseURL).
			Msg("準備發送包含圖片的Discord回覆")

		// 圖片回覆，使用footer嵌入orderID和聊天顏色
		chatColor := cs.discordService.GetEventColor("chat")
		_, err = cs.discordService.SendImageReplyWithOrderIDAndColor(order.DiscordChannelID, order.DiscordMessageID, replyText, fullImageURL, orderID, chatColor)
	} else {
		// 文字回覆，使用footer嵌入orderID和聊天顏色
		chatColor := cs.discordService.GetEventColor("chat")
		_, err = cs.discordService.ReplyToMessageWithOrderIDAndColor(order.DiscordChannelID, order.DiscordMessageID, replyText, orderID, chatColor)
	}

	if err != nil {
		cs.logger.Error().Err(err).
			Str("order_id", orderID).
			Str("driver_id", senderID).
			Str("channel_id", order.DiscordChannelID).
			Str("message_id", order.DiscordMessageID).
			Msg("Discord聊天通知發送失敗")
		return
	}

	cs.logger.Info().
		Str("order_id", orderID).
		Str("driver_id", senderID).
		Str("message_type", string(msgType)).
		Str("reply_text", replyText).
		Msg("Discord聊天通知發送成功")
}
