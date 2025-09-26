package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"strings"

	"right-backend/infra"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"gopkg.in/yaml.v3"
)

type Config struct {
	MongoDB struct {
		URI      string `yaml:"uri"`
		Database string `yaml:"database"`
	} `yaml:"mongodb"`
}

func main() {
	// 讀取配置 - 自動尋找配置檔位置
	configPaths := []string{
		"config.yml",       // 當前目錄
		"../config.yml",    // 上層目錄 (cmd/init -> Right-Backend)
		"../../config.yml", // 上上層目錄
	}

	var configData []byte
	var err error
	var usedPath string

	for _, path := range configPaths {
		configData, err = ioutil.ReadFile(path)
		if err == nil {
			usedPath = path
			break
		}
	}

	if err != nil {
		log.Fatalf("❌ 無法找到 config.yml 配置檔，已嘗試路徑: %v", configPaths)
	}

	fmt.Printf("✅ 找到配置檔: %s\n", usedPath)

	var cfg Config
	if err := yaml.Unmarshal(configData, &cfg); err != nil {
		log.Fatalf("❌ 解析 config.yml 失敗: %v", err)
	}

	// 連接 MongoDB
	mongoConfig := infra.MongoConfig{
		URI:      cfg.MongoDB.URI,
		Database: cfg.MongoDB.Database,
	}
	mongoDB, err := infra.NewMongoDB(mongoConfig)
	if err != nil {
		log.Fatalf("❌ 連接 MongoDB 失敗: %v", err)
	}
	defer mongoDB.Close(context.Background())

	ctx := context.Background()

	fmt.Println("🚀 開始優化 MongoDB 索引...")
	fmt.Println("🎯 專為接單、派單系統優化，針對以下場景：")
	fmt.Println("   • dispatcher.findCandidateDrivers() - 司機篩選與排序")
	fmt.Println("   • 事件驅動調度的狀態檢查與更新")
	fmt.Println("   • controller 層的訂單與司機查詢")
	fmt.Println("   • service 層的複合條件過濾")
	fmt.Println()

	// 創建索引
	if err := createOptimizedIndexes(ctx, mongoDB); err != nil {
		log.Fatalf("❌ 創建索引失敗: %v", err)
	}

	// 顯示索引創建結果
	if err := printIndexInfo(ctx, mongoDB); err != nil {
		fmt.Printf("⚠️  顯示索引資訊失敗: %v\n", err)
	}

	fmt.Println("✅ 索引優化完成！接單、派單速度將顯著提升")
}

// createOptimizedIndexes 創建針對接單派單優化的索引
func createOptimizedIndexes(ctx context.Context, mongoDB *infra.MongoDB) error {
	fmt.Println("📝 創建核心索引...")

	// ==================== ORDERS 集合索引 ====================
	ordersCollection := mongoDB.GetCollection("orders")
	fmt.Println("🎯 優化 orders 集合...")

	orderIndexes := []mongo.IndexModel{
		// 【核心調度索引】- 最關鍵的複合索引，支援 dispatcher 的核心邏輯
		{
			Keys: bson.D{
				{Key: "status", Value: 1},      // 第一層：訂單狀態篩選
				{Key: "fleet", Value: 1},       // 第二層：車隊匹配
				{Key: "created_at", Value: -1}, // 第三層：時間排序
			},
			Options: options.Index().SetName("core_dispatch_query"),
		},

		// 【司機訂單查詢】- 支援司機查看自己的訂單
		{
			Keys: bson.D{
				{Key: "driver.assigned_driver", Value: 1},
				{Key: "status", Value: 1},
				{Key: "created_at", Value: -1},
			},
			Options: options.Index().SetName("driver_orders_query"),
		},

		// 【訂單狀態更新】- 支援 CAS 操作和狀態檢查
		{
			Keys: bson.D{
				{Key: "_id", Value: 1},
				{Key: "status", Value: 1},
			},
			Options: options.Index().SetName("order_status_update"),
		},

		// 【管理查詢索引】- 支援後台管理系統的複合查詢
		{
			Keys: bson.D{
				{Key: "status", Value: 1},
				{Key: "created_at", Value: -1},
			},
			Options: options.Index().SetName("admin_orders_query"),
		},

		// 【客群查詢索引】- 支援客戶群組過濾
		{
			Keys: bson.D{
				{Key: "customer_group", Value: 1},
				{Key: "created_at", Value: -1},
			},
			Options: options.Index().SetName("customer_group_query"),
		},

		// 【地理位置索引】- 支援地址相關查詢
		{
			Keys: bson.D{
				{Key: "customer.pickup_lat", Value: 1},
				{Key: "customer.pickup_lng", Value: 1},
			},
			Options: options.Index().SetName("pickup_location_query"),
		},

		// 【時間範圍查詢】- 支援日期範圍篩選
		{
			Keys:    bson.D{{Key: "created_at", Value: -1}},
			Options: options.Index().SetName("time_range_query"),
		},

		// 【訂單統計索引】- 支援各種統計查詢
		{
			Keys: bson.D{
				{Key: "fleet", Value: 1},
				{Key: "status", Value: 1},
				{Key: "created_at", Value: -1},
			},
			Options: options.Index().SetName("order_statistics_query"),
		},
	}

	if err := createIndexesSafely(ctx, ordersCollection, orderIndexes, "orders"); err != nil {
		return err
	}
	fmt.Println("✅ orders 集合索引創建完成")

	// ==================== DRIVERS 集合索引 ====================
	driversCollection := mongoDB.GetCollection("drivers")
	fmt.Println("🎯 優化 drivers 集合...")

	driverIndexes := []mongo.IndexModel{
		// 【帳號唯一索引】- 防止重複帳號
		{
			Keys:    bson.D{{Key: "account", Value: 1}},
			Options: options.Index().SetUnique(true).SetName("unique_account"),
		},

		// 【調度核心索引】- 最關鍵！支援 dispatcher 的司機篩選
		{
			Keys: bson.D{
				{Key: "is_online", Value: 1}, // 第一層：上線狀態
				{Key: "status", Value: 1},    // 第二層：司機狀態 (idle/busy)
				{Key: "fleet", Value: 1},     // 第三層：車隊匹配
				{Key: "is_active", Value: 1}, // 第四層：啟用狀態
			},
			Options: options.Index().SetName("core_driver_dispatch"),
		},

		// 【司機位置索引】- 支援地理位置計算
		{
			Keys: bson.D{
				{Key: "lat", Value: 1},
				{Key: "lng", Value: 1},
			},
			Options: options.Index().SetName("driver_location_query"),
		},

		// 【管理查詢索引】- 支援後台司機管理
		{
			Keys: bson.D{
				{Key: "fleet", Value: 1},
				{Key: "is_approved", Value: 1},
				{Key: "created_at", Value: -1},
			},
			Options: options.Index().SetName("admin_driver_query"),
		},

		// 【司機狀態更新】- 支援頻繁的狀態變更
		{
			Keys: bson.D{
				{Key: "_id", Value: 1},
				{Key: "status", Value: 1},
			},
			Options: options.Index().SetName("driver_status_update"),
		},

		// 【在線司機查詢】- 支援快速查找在線司機
		{
			Keys: bson.D{
				{Key: "is_online", Value: 1},
				{Key: "last_online", Value: -1},
			},
			Options: options.Index().SetName("online_drivers_query"),
		},

		// 【車隊司機查詢】- 支援車隊管理功能
		{
			Keys: bson.D{
				{Key: "fleet", Value: 1},
				{Key: "is_active", Value: 1},
				{Key: "created_at", Value: -1},
			},
			Options: options.Index().SetName("fleet_drivers_query"),
		},
	}

	if err := createIndexesSafely(ctx, driversCollection, driverIndexes, "drivers"); err != nil {
		return err
	}
	fmt.Println("✅ drivers 集合索引創建完成")

	// ==================== 其他支援索引 ====================
	if err := createSupportingIndexes(ctx, mongoDB); err != nil {
		return err
	}

	return nil
}

// createIndexesSafely 安全地創建索引，跳過已存在的索引
func createIndexesSafely(ctx context.Context, collection *mongo.Collection, indexes []mongo.IndexModel, collectionName string) error {
	for _, index := range indexes {
		// 逐一創建索引，避免批量操作因單一衝突而失敗
		_, err := collection.Indexes().CreateOne(ctx, index)
		if err != nil {
			// 檢查是否為索引已存在或重複鍵錯誤
			if strings.Contains(err.Error(), "IndexOptionsConflict") ||
				strings.Contains(err.Error(), "already exists") ||
				strings.Contains(err.Error(), "DuplicateKey") ||
				strings.Contains(err.Error(), "E11000 duplicate key") {
				if name := index.Options.Name; name != nil {
					fmt.Printf("   ⚠️  索引 %s 存在衝突，跳過創建 (可能已存在或資料重複)\n", *name)
				} else {
					fmt.Printf("   ⚠️  索引存在衝突，跳過創建\n")
				}
				continue
			}
			// 其他錯誤則返回
			return fmt.Errorf("創建 %s 索引失敗: %v", collectionName, err)
		} else {
			if name := index.Options.Name; name != nil {
				fmt.Printf("   ✅ 索引 %s 創建成功\n", *name)
			} else {
				fmt.Printf("   ✅ 索引創建成功\n")
			}
		}
	}
	return nil
}

// createSupportingIndexes 創建其他支援性索引
func createSupportingIndexes(ctx context.Context, mongoDB *infra.MongoDB) error {
	fmt.Println("🎯 創建支援性索引...")

	// Users 集合索引 - 支援管理系統
	usersCollection := mongoDB.GetCollection("users")
	userIndexes := []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "account", Value: 1}},
			Options: options.Index().SetUnique(true).SetName("unique_user_account"),
		},
		{
			Keys: bson.D{
				{Key: "role", Value: 1},
				{Key: "is_active", Value: 1},
			},
			Options: options.Index().SetName("user_role_query"),
		},
	}

	if err := createIndexesSafely(ctx, usersCollection, userIndexes, "users"); err != nil {
		fmt.Printf("⚠️  創建 users 索引失敗 (集合可能不存在): %v\n", err)
	} else {
		fmt.Println("✅ users 集合索引創建完成")
	}

	// Order Logs 集合索引 - 支援訂單日誌查詢
	orderLogsCollection := mongoDB.GetCollection("order_logs")
	orderLogIndexes := []mongo.IndexModel{
		{
			Keys: bson.D{
				{Key: "order_id", Value: 1},
				{Key: "created_at", Value: -1},
			},
			Options: options.Index().SetName("order_logs_query"),
		},
		{
			Keys: bson.D{
				{Key: "driver_id", Value: 1},
				{Key: "created_at", Value: -1},
			},
			Options: options.Index().SetName("driver_logs_query"),
		},
		{
			Keys:    bson.D{{Key: "action", Value: 1}},
			Options: options.Index().SetName("log_action_query"),
		},
	}

	if err := createIndexesSafely(ctx, orderLogsCollection, orderLogIndexes, "order_logs"); err != nil {
		fmt.Printf("⚠️  創建 order_logs 索引失敗 (集合可能不存在): %v\n", err)
	} else {
		fmt.Println("✅ order_logs 集合索引創建完成")
	}

	return nil
}

// printIndexInfo 顯示各集合的索引資訊
func printIndexInfo(ctx context.Context, mongoDB *infra.MongoDB) error {
	collections := []string{"orders", "drivers", "users", "order_logs"}

	fmt.Println("\n📊 索引創建報告:")
	fmt.Println(strings.Repeat("=", 60))

	for _, collName := range collections {
		collection := mongoDB.GetCollection(collName)
		cursor, err := collection.Indexes().List(ctx)
		if err != nil {
			continue // 集合可能不存在
		}

		var indexes []bson.M
		if err := cursor.All(ctx, &indexes); err != nil {
			continue
		}

		if len(indexes) > 0 {
			fmt.Printf("📁 %s: %d 個索引\n", collName, len(indexes))
			for i, index := range indexes {
				if name, ok := index["name"].(string); ok {
					if keys, ok := index["key"].(bson.M); ok {
						var keyStrs []string
						for key, direction := range keys {
							dir := "1"
							if d, ok := direction.(int32); ok && d == -1 {
								dir = "-1"
							}
							keyStrs = append(keyStrs, fmt.Sprintf("%s:%s", key, dir))
						}

						// 顯示是否為唯一索引
						unique := ""
						if u, ok := index["unique"].(bool); ok && u {
							unique = " [UNIQUE]"
						}

						fmt.Printf("   %d. %s%s\n", i+1, name, unique)
						fmt.Printf("      └─ %v\n", keyStrs)
					}
				}
			}
			fmt.Println()
		}
	}

	fmt.Println(strings.Repeat("=", 60))
	fmt.Println("🎯 關鍵優化說明:")
	fmt.Println("   • core_dispatch_query: 調度核心查詢，支援狀態+車隊+時間篩選")
	fmt.Println("   • core_driver_dispatch: 司機篩選核心，支援多層條件過濾")
	fmt.Println("   • driver_orders_query: 司機訂單查詢，支援司機端API")
	fmt.Println("   • order_status_update: CAS操作優化，支援原子性更新")
	fmt.Println("   • 所有索引均針對實際查詢模式設計，避免無效索引")
	fmt.Println(strings.Repeat("=", 60))

	return nil
}
