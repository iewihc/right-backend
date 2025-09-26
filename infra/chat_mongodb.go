package infra

import (
	"context"
	"time"

	"github.com/rs/zerolog"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// InitializeChatCollections 初始化聊天相關的MongoDB集合和索引
func InitializeChatCollections(logger zerolog.Logger, db *mongo.Database) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 初始化 order_chats 集合
	if err := initOrderChatsCollection(ctx, logger, db); err != nil {
		return err
	}

	// 初始化 chat_messages 集合
	if err := initChatMessagesCollection(ctx, logger, db); err != nil {
		return err
	}

	// 初始化 chat_unread_counts 集合
	if err := initChatUnreadCountsCollection(ctx, logger, db); err != nil {
		return err
	}

	logger.Info().Msg("聊天系統MongoDB集合初始化完成")
	return nil
}

// initOrderChatsCollection 初始化 order_chats 集合
func initOrderChatsCollection(ctx context.Context, logger zerolog.Logger, db *mongo.Database) error {
	collection := db.Collection("order_chats")

	// 創建索引
	indexes := []mongo.IndexModel{
		{
			Keys: bson.D{{Key: "orderId", Value: 1}},
			Options: options.Index().
				SetUnique(true).
				SetName("orderId_unique"),
		},
		{
			Keys:    bson.D{{Key: "driverId", Value: 1}},
			Options: options.Index().SetName("driverId_index"),
		},
		{
			Keys:    bson.D{{Key: "isActive", Value: 1}},
			Options: options.Index().SetName("isActive_index"),
		},
		{
			Keys:    bson.D{{Key: "updatedAt", Value: -1}},
			Options: options.Index().SetName("updatedAt_desc_index"),
		},
		{
			Keys: bson.D{
				{Key: "driverId", Value: 1},
				{Key: "isActive", Value: 1},
				{Key: "updatedAt", Value: -1},
			},
			Options: options.Index().SetName("driver_active_updated_index"),
		},
	}

	_, err := collection.Indexes().CreateMany(ctx, indexes)
	if err != nil {
		logger.Error().Err(err).Msg("創建 order_chats 集合索引失敗")
		return err
	}

	logger.Info().Msg("order_chats 集合索引創建完成")
	return nil
}

// initChatMessagesCollection 初始化 chat_messages 集合
func initChatMessagesCollection(ctx context.Context, logger zerolog.Logger, db *mongo.Database) error {
	collection := db.Collection("chat_messages")

	// 創建索引
	indexes := []mongo.IndexModel{
		{
			Keys: bson.D{
				{Key: "orderId", Value: 1},
				{Key: "createdAt", Value: -1},
			},
			Options: options.Index().SetName("orderId_createdAt_desc_index"),
		},
		{
			Keys:    bson.D{{Key: "senderId", Value: 1}},
			Options: options.Index().SetName("senderId_index"),
		},
		{
			Keys:    bson.D{{Key: "status", Value: 1}},
			Options: options.Index().SetName("status_index"),
		},
		{
			Keys:    bson.D{{Key: "createdAt", Value: -1}},
			Options: options.Index().SetName("createdAt_desc_index"),
		},
		{
			Keys: bson.D{{Key: "tempId", Value: 1}},
			Options: options.Index().
				SetSparse(true).
				SetName("tempId_sparse_index"),
		},
		{
			Keys: bson.D{
				{Key: "orderId", Value: 1},
				{Key: "sender", Value: 1},
				{Key: "createdAt", Value: -1},
			},
			Options: options.Index().SetName("orderId_sender_createdAt_index"),
		},
	}

	_, err := collection.Indexes().CreateMany(ctx, indexes)
	if err != nil {
		logger.Error().Err(err).Msg("創建 chat_messages 集合索引失敗")
		return err
	}

	// 設置TTL索引，90天後自動清理聊天記錄
	ttlIndex := mongo.IndexModel{
		Keys: bson.D{{Key: "createdAt", Value: 1}},
		Options: options.Index().
			SetExpireAfterSeconds(90 * 24 * 3600). // 90天
			SetName("createdAt_ttl_index"),
	}

	_, err = collection.Indexes().CreateOne(ctx, ttlIndex)
	if err != nil {
		logger.Error().Err(err).Msg("創建 chat_messages TTL索引失敗")
		return err
	}

	logger.Info().Msg("chat_messages 集合索引創建完成")
	return nil
}

// initChatUnreadCountsCollection 初始化 chat_unread_counts 集合
func initChatUnreadCountsCollection(ctx context.Context, logger zerolog.Logger, db *mongo.Database) error {
	collection := db.Collection("chat_unread_counts")

	// 創建索引
	indexes := []mongo.IndexModel{
		{
			Keys: bson.D{
				{Key: "orderId", Value: 1},
				{Key: "userId", Value: 1},
			},
			Options: options.Index().
				SetUnique(true).
				SetName("orderId_userId_unique"),
		},
		{
			Keys: bson.D{
				{Key: "userId", Value: 1},
				{Key: "userType", Value: 1},
			},
			Options: options.Index().SetName("userId_userType_index"),
		},
		{
			Keys:    bson.D{{Key: "updatedAt", Value: -1}},
			Options: options.Index().SetName("updatedAt_desc_index"),
		},
		{
			Keys: bson.D{
				{Key: "userId", Value: 1},
				{Key: "userType", Value: 1},
				{Key: "count", Value: -1},
			},
			Options: options.Index().SetName("userId_userType_count_index"),
		},
	}

	_, err := collection.Indexes().CreateMany(ctx, indexes)
	if err != nil {
		logger.Error().Err(err).Msg("創建 chat_unread_counts 集合索引失敗")
		return err
	}

	// 設置TTL索引，30天後自動清理未讀計數記錄
	ttlIndex := mongo.IndexModel{
		Keys: bson.D{{Key: "updatedAt", Value: 1}},
		Options: options.Index().
			SetExpireAfterSeconds(30 * 24 * 3600). // 30天
			SetName("updatedAt_ttl_index"),
	}

	_, err = collection.Indexes().CreateOne(ctx, ttlIndex)
	if err != nil {
		logger.Error().Err(err).Msg("創建 chat_unread_counts TTL索引失敗")
		return err
	}

	logger.Info().Msg("chat_unread_counts 集合索引創建完成")
	return nil
}

// DropChatCollections 刪除所有聊天相關的集合（開發/測試用）
func DropChatCollections(logger zerolog.Logger, db *mongo.Database) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	collections := []string{"order_chats", "chat_messages", "chat_unread_counts"}

	for _, collName := range collections {
		err := db.Collection(collName).Drop(ctx)
		if err != nil {
			logger.Error().Err(err).Str("collection", collName).Msg("刪除集合失敗")
			return err
		}
		logger.Info().Str("collection", collName).Msg("已刪除集合")
	}

	logger.Info().Msg("所有聊天集合已刪除")
	return nil
}

// GetChatCollectionStats 獲取聊天集合統計信息
func GetChatCollectionStats(logger zerolog.Logger, db *mongo.Database) (map[string]interface{}, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	stats := make(map[string]interface{})
	collections := []string{"order_chats", "chat_messages", "chat_unread_counts"}

	for _, collName := range collections {
		var result bson.M
		err := db.RunCommand(ctx, bson.D{
			{Key: "collStats", Value: collName},
		}).Decode(&result)

		if err != nil {
			logger.Error().Err(err).Str("collection", collName).Msg("獲取集合統計失敗")
			continue
		}

		stats[collName] = map[string]interface{}{
			"count":       result["count"],
			"size":        result["size"],
			"storageSize": result["storageSize"],
			"indexCount":  result["nindexes"],
		}
	}

	return stats, nil
}
