package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"right-backend/data-models/admin"
	driverModels "right-backend/data-models/driver"
	"right-backend/infra"
	"right-backend/model"
	"right-backend/utils"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/rs/zerolog"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.opentelemetry.io/otel/trace"
)

type DriverService struct {
	logger                 zerolog.Logger
	mongoDB                *infra.MongoDB
	jwtSecretKey           string
	jwtExpiresHours        int
	orderService           *OrderService
	googleService          *GoogleMapService
	crawlerService         *CrawlerService
	trafficUsageLogService *TrafficUsageLogService
	blacklistService       *DriverBlacklistService
	eventManager           *infra.RedisEventManager // 事件管理器
	notificationService    *NotificationService     // 統一通知服務
}

func NewDriverService(
	logger zerolog.Logger,
	mongoDB *infra.MongoDB,
	jwtSecretKey string,
	jwtExpiresHours int,
	orderService *OrderService,
	googleService *GoogleMapService,
	crawlerService *CrawlerService,
	trafficUsageLogService *TrafficUsageLogService,
	blacklistService *DriverBlacklistService,
	eventManager *infra.RedisEventManager, // 事件管理器參數
	notificationService *NotificationService, // 統一通知服務
) *DriverService {
	return &DriverService{
		logger:                 logger.With().Str("module", "driver_service").Logger(),
		mongoDB:                mongoDB,
		jwtSecretKey:           jwtSecretKey,
		jwtExpiresHours:        jwtExpiresHours,
		orderService:           orderService,
		googleService:          googleService,
		crawlerService:         crawlerService,
		trafficUsageLogService: trafficUsageLogService,
		blacklistService:       blacklistService,
		eventManager:           eventManager,
		notificationService:    notificationService,
	}
}

func (s *DriverService) CreateDriver(ctx context.Context, driver *model.DriverInfo) (*model.DriverInfo, error) {
	driver.ID = primitive.NewObjectID()
	now := utils.NowUTC()
	driver.CreatedAt = now
	driver.UpdatedAt = now
	driver.JoinedOn = now

	// 如果未設置審核狀態，預設為未審核
	if !driver.IsApproved {
		driver.Status = model.DriverStatusInactive // 未審核時設為非活躍
		driver.IsActive = false                    // 未審核時不啟用
	} else {
		// 已審核的司機設置初始狀態以便接單
		driver.Status = model.DriverStatusIdle // 狀態設為「閒置」
		driver.IsActive = true                 // 設為啟用
	}
	driver.IsOnline = false // 初始為離線，需司機手動上線

	collection := s.mongoDB.GetCollection("drivers")
	_, err := collection.InsertOne(ctx, driver)
	if err != nil {
		s.logger.Error().
			Str("司機帳號", driver.Account).
			Str("司機姓名", driver.Name).
			Str("車隊名稱", string(driver.Fleet)).
			Str("車牌號碼", driver.CarPlate).
			Str("錯誤原因", err.Error()).
			Msg("建立司機失敗")
		return nil, err
	}

	s.logger.Info().
		Str("司機編號", driver.ID.Hex()).
		Str("司機帳號", driver.Account).
		Str("司機姓名", driver.Name).
		Str("車隊名稱", string(driver.Fleet)).
		Str("車牌號碼", driver.CarPlate).
		Bool("是否審核", driver.IsApproved).
		Msg("司機建立成功")

	return driver, nil
}

func (s *DriverService) GetDriverByID(ctx context.Context, id string) (*model.DriverInfo, error) {
	objectID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		s.logger.Warn().
			Str("無效編號", id).
			Str("錯誤原因", err.Error()).
			Msg("司機編號格式不正確")
		return nil, err
	}

	collection := s.mongoDB.GetCollection("drivers")
	var driver model.DriverInfo
	err = collection.FindOne(ctx, bson.M{"_id": objectID}).Decode(&driver)
	if err != nil {
		s.logger.Warn().
			Str("司機編號", id).
			Str("錯誤原因", err.Error()).
			Msg("司機不存在")
		return nil, err
	}

	return &driver, nil
}

func (s *DriverService) GetDriverByAccount(ctx context.Context, account string) (*model.DriverInfo, error) {
	collection := s.mongoDB.GetCollection("drivers")
	var driver model.DriverInfo
	err := collection.FindOne(ctx, bson.M{"account": account}).Decode(&driver)
	if err != nil {
		s.logger.Warn().
			Str("司機帳號", account).
			Str("錯誤原因", err.Error()).
			Msg("司機帳號不存在")
		return nil, err
	}

	return &driver, nil
}

func (s *DriverService) GetDriverByLineUID(ctx context.Context, lineUID string) (*model.DriverInfo, error) {
	collection := s.mongoDB.GetCollection("drivers")
	var driver model.DriverInfo
	err := collection.FindOne(ctx, bson.M{"line_uid": lineUID}).Decode(&driver)
	if err != nil {
		s.logger.Warn().
			Str("LINE_UID", lineUID).
			Str("錯誤原因", err.Error()).
			Msg("司機 LINE UID 不存在")
		return nil, err
	}

	return &driver, nil
}

func (s *DriverService) UpdateDriverLocation(ctx context.Context, id string, lat, lng string) (*model.DriverInfo, error) {
	var updatedDriver *model.DriverInfo

	err := infra.WithSpan(ctx, "driver_service.update_location", func(ctx context.Context, span trace.Span) error {
		// 設置基本屬性
		infra.SetAttributes(span,
			infra.AttrOperation("update_driver_location"),
			infra.AttrDriverID(id),
			infra.AttrString("location.lat", lat),
			infra.AttrString("location.lng", lng),
		)

		// 驗證司機 ID 格式
		infra.AddEvent(span, "validating_driver_id")
		objectID, err := primitive.ObjectIDFromHex(id)
		if err != nil {
			infra.AddEvent(span, "driver_id_validation_failed")
			infra.SetAttributes(span, infra.AttrErrorType("invalid_driver_id"))
			s.logger.Error().Str("driver_id", id).Err(err).Msg("無效的司機ID格式 (Invalid driver ID format)")
			return err
		}

		infra.AddEvent(span, "driver_id_validated")

		// 準備更新數據
		infra.AddEvent(span, "preparing_update_data")
		collection := s.mongoDB.GetCollection("drivers")
		filter := bson.M{"_id": objectID}
		update := bson.M{
			"$set": bson.M{
				"lat":         lat,
				"lng":         lng,
				"last_online": time.Now(),
				"updated_at":  time.Now(),
			},
		}

		// 執行數據庫更新
		infra.AddEvent(span, "updating_driver_location_in_database")
		var driverInfo model.DriverInfo
		err = collection.FindOneAndUpdate(ctx, filter, update).Decode(&driverInfo)
		if err != nil {
			infra.AddEvent(span, "database_update_failed")
			infra.SetAttributes(span, infra.AttrErrorType("database_error"))
			s.logger.Error().
				Str("司機編號", id).
				Str("緯度", lat).
				Str("經度", lng).
				Str("錯誤原因", err.Error()).
				Msg("更新司機位置失敗")
			return err
		}

		infra.AddEvent(span, "driver_location_updated_successfully")
		infra.SetAttributes(span,
			infra.AttrString("driver.name", driverInfo.Name),
			infra.AttrString("driver.car_plate", driverInfo.CarPlate),
		)

		updatedDriver = &driverInfo
		return nil
	},
		infra.AttrOperation("update_driver_location"),
		infra.AttrDriverID(id),
	)

	if err != nil {
		return nil, err
	}

	return updatedDriver, nil
}

func (s *DriverService) UpdateDriverStatus(ctx context.Context, id string, isOnline bool) (*model.DriverInfo, error) {
	objectID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		s.logger.Error().Str("driver_id", id).Err(err).Msg("無效的司機ID格式 (Invalid driver ID format)")
		return nil, err
	}

	collection := s.mongoDB.GetCollection("drivers")
	filter := bson.M{"_id": objectID}
	updateFields := bson.M{
		"is_online":   isOnline,
		"last_online": time.Now(),
		"updated_at":  time.Now(),
	}

	update := bson.M{"$set": updateFields}

	var updatedDriver model.DriverInfo
	err = collection.FindOneAndUpdate(ctx, filter, update).Decode(&updatedDriver)
	if err != nil {
		s.logger.Error().
			Str("司機編號", id).
			Bool("上線狀態", isOnline).
			Str("錯誤原因", err.Error()).
			Msg("更新司機上線狀態失敗")
		return nil, err
	}

	s.logger.Debug().
		Str("司機編號", updatedDriver.ID.Hex()).
		Str("司機帳號", updatedDriver.Account).
		Str("司機姓名", updatedDriver.Name).
		Bool("上線狀態", isOnline).
		Msg("司機上線狀態更新成功")

	return &updatedDriver, nil
}

// UpdateDriverStatusType 更新司機狀態，可選提供原因和訂單ID
func (s *DriverService) UpdateDriverStatusType(ctx context.Context, id string, status model.DriverStatus, reason ...string) error {
	// 設置默認原因
	updateReason := string(model.DriverReasonSystemUpdate)
	var orderID string

	// 解析可選參數：reason, orderID
	if len(reason) > 0 && reason[0] != "" {
		updateReason = reason[0]
	}
	if len(reason) > 1 {
		orderID = reason[1]
	}
	objectID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		s.logger.Error().Str("driver_id", id).Err(err).Msg("無效的司機ID格式 (Invalid driver ID format)")
		return err
	}

	// 首先獲取司機當前狀態
	collection := s.mongoDB.GetCollection("drivers")
	var currentDriver model.DriverInfo
	err = collection.FindOne(ctx, bson.M{"_id": objectID}).Decode(&currentDriver)
	if err != nil {
		s.logger.Error().
			Str("driver_id", id).
			Err(err).
			Msg("獲取司機當前狀態失敗")
		return err
	}

	oldStatus := currentDriver.Status

	// 如果狀態沒有變化，直接返回
	if oldStatus == status {
		s.logger.Debug().
			Str("driver_id", id).
			Str("status", string(status)).
			Msg("司機狀態無變化，跳過更新")
		return nil
	}

	// 更新司機狀態
	filter := bson.M{"_id": objectID}
	update := bson.M{
		"$set": bson.M{
			"status":     status,
			"updated_at": time.Now(),
		},
	}

	_, err = collection.UpdateOne(ctx, filter, update)
	if err != nil {
		s.logger.Error().
			Str("司機編號", id).
			Str("舊狀態", string(oldStatus)).
			Str("新狀態", string(status)).
			Str("錯誤原因", err.Error()).
			Msg("更新司機狀態類型失敗")
		return err
	}

	// 發布司機狀態變更事件
	if s.eventManager != nil {
		statusEvent := &infra.DriverStatusEvent{
			DriverID:  id,
			OldStatus: string(oldStatus),
			NewStatus: string(status),
			OrderID:   orderID,
			Timestamp: time.Now(),
			Reason:    updateReason,
		}

		if publishErr := s.eventManager.PublishDriverStatusEvent(ctx, statusEvent); publishErr != nil {
			s.logger.Error().Err(publishErr).
				Str("driver_id", id).
				Str("old_status", string(oldStatus)).
				Str("new_status", string(status)).
				Msg("🚨 發送司機狀態變更事件失敗")
			// 不影響主流程，繼續執行
		} else {
			s.logger.Info().
				Str("driver_id", id).
				Str("old_status", string(oldStatus)).
				Str("new_status", string(status)).
				Str("reason", updateReason).
				Msg("司機狀態變更事件已發布")
		}
	} else {
		s.logger.Warn().
			Str("driver_id", id).
			Msg("事件管理器未初始化，無法發送狀態變更通知")
	}

	return nil
}

func (s *DriverService) IncrementAcceptedCount(ctx context.Context, driverID string) error {
	objectID, err := primitive.ObjectIDFromHex(driverID)
	if err != nil {
		s.logger.Error().Str("driver_id", driverID).Err(err).Msg("無效的司機ID格式 (Invalid driver ID format)")
		return err
	}
	collection := s.mongoDB.GetCollection("drivers")
	_, err = collection.UpdateOne(ctx, bson.M{"_id": objectID}, bson.M{"$inc": bson.M{"accepted_count": 1}})
	if err != nil {
		s.logger.Error().Str("driver_id", driverID).Err(err).Msg("增加司機接單計數失敗 (Increment driver accepted count failed)")
	}
	return err
}

func (s *DriverService) IncrementCompletedCount(ctx context.Context, driverID string) error {
	objectID, err := primitive.ObjectIDFromHex(driverID)
	if err != nil {
		s.logger.Error().Str("driver_id", driverID).Err(err).Msg("無效的司機ID格式 (Invalid driver ID format)")
		return err
	}
	collection := s.mongoDB.GetCollection("drivers")
	_, err = collection.UpdateOne(ctx, bson.M{"_id": objectID}, bson.M{"$inc": bson.M{"completed_count": 1}})
	if err != nil {
		s.logger.Error().Str("driver_id", driverID).Err(err).Msg("增加司機完成計數失敗 (Increment driver completed count failed)")
	}
	return err
}

// UpdateDriverCurrentOrderId 更新司機的當前訂單ID
func (s *DriverService) UpdateDriverCurrentOrderId(ctx context.Context, driverID string, orderID string) error {
	objectID, err := primitive.ObjectIDFromHex(driverID)
	if err != nil {
		s.logger.Error().Str("driver_id", driverID).Err(err).Msg("無效的司機ID格式")
		return err
	}

	collection := s.mongoDB.GetCollection("drivers")

	var updateField interface{}
	if orderID == "" {
		updateField = nil // 清空 current_order_id
	} else {
		updateField = orderID
	}

	update := bson.M{
		"$set": bson.M{
			"current_order_id": updateField,
			"updated_at":       time.Now(),
		},
	}

	_, err = collection.UpdateOne(ctx, bson.M{"_id": objectID}, update)
	if err != nil {
		s.logger.Error().Str("driver_id", driverID).Str("order_id", orderID).Err(err).Msg("更新司機CurrentOrderId失敗")
		return err
	}

	return nil
}

func (s *DriverService) IncrementRejectedCount(ctx context.Context, driverID string) error {
	objectID, err := primitive.ObjectIDFromHex(driverID)
	if err != nil {
		s.logger.Error().Str("driver_id", driverID).Err(err).Msg("無效的司機ID格式 (Invalid driver ID format)")
		return err
	}
	collection := s.mongoDB.GetCollection("drivers")
	_, err = collection.UpdateOne(ctx, bson.M{"_id": objectID}, bson.M{"$inc": bson.M{"rejected_count": 1}})
	if err != nil {
		s.logger.Error().Str("driver_id", driverID).Err(err).Msg("增加司機拒絕計數失敗 (Increment driver rejected count failed)")
	}
	return err
}

func (s *DriverService) UpdateFCMToken(ctx context.Context, driverID string, fcmToken string, fcmType string) (*model.DriverInfo, error) {
	objectID, err := primitive.ObjectIDFromHex(driverID)
	if err != nil {
		s.logger.Error().Str("driver_id", driverID).Err(err).Msg("無效的司機ID格式 (Invalid driver ID format)")
		return nil, err
	}

	collection := s.mongoDB.GetCollection("drivers")
	filter := bson.M{"_id": objectID}
	update := bson.M{
		"$set": bson.M{
			"fcm_token":  fcmToken,
			"fcm_type":   fcmType,
			"updated_at": time.Now(),
		},
	}

	var updatedDriver model.DriverInfo
	err = collection.FindOneAndUpdate(ctx, filter, update, nil).Decode(&updatedDriver)
	if err != nil {
		s.logger.Error().Str("driver_id", driverID).Str("fcm_token", fcmToken).Str("fcm_type", fcmType).Err(err).Msg("更新司機FCM令牌失敗 (Update driver FCM token failed)")
		return nil, err
	}

	return &updatedDriver, nil
}

func (s *DriverService) Login(ctx context.Context, account, password, deviceModelName, deviceDeviceName, deviceBrand, deviceManufacturer, deviceAppVersion string) (*model.DriverInfo, string, error) {
	var driver *model.DriverInfo
	var tokenString string

	err := infra.WithSpan(ctx, "driver_service.login", func(ctx context.Context, span trace.Span) error {
		// 設置基本屬性
		infra.SetAttributes(span,
			infra.AttrOperation("driver_login"),
			infra.AttrDriverAccount(account),
			infra.AttrString("device.model", deviceModelName),
			infra.AttrString("device.brand", deviceBrand),
		)

		collection := s.mongoDB.GetCollection("drivers")
		var driverInfo model.DriverInfo

		// 先檢查帳號是否存在
		infra.AddEvent(span, "checking_driver_account")
		err := collection.FindOne(ctx, bson.M{"account": account}).Decode(&driverInfo)
		if err != nil {
			if errors.Is(err, mongo.ErrNoDocuments) {
				infra.AddEvent(span, "driver_account_not_found")
				infra.SetAttributes(span, infra.AttrErrorType("account_not_found"))
				s.logger.Warn().
					Str("司機帳號", account).
					Msg("司機登入失敗 - 帳號不存在")
				return fmt.Errorf("帳號不存在")
			}
			infra.AddEvent(span, "database_query_error")
			infra.SetAttributes(span, infra.AttrErrorType("database_error"))
			s.logger.Error().
				Str("司機帳號", account).
				Str("錯誤原因", err.Error()).
				Msg("司機登入失敗 - 資料庫查詢錯誤")
			return err
		}

		infra.AddEvent(span, "driver_account_found")
		infra.SetAttributes(span,
			infra.AttrDriverID(driverInfo.ID.Hex()),
			infra.AttrString("driver.name", driverInfo.Name),
			infra.AttrBool("driver.is_active", driverInfo.IsActive),
			infra.AttrBool("driver.is_approved", driverInfo.IsApproved),
		)

		// 檢查密碼是否正確
		infra.AddEvent(span, "validating_password")
		if driverInfo.Password != password {
			infra.AddEvent(span, "password_validation_failed")
			infra.SetAttributes(span, infra.AttrErrorType("invalid_password"))
			s.logger.Warn().
				Str("司機帳號", account).
				Msg("司機登入失敗 - 密碼錯誤")
			return fmt.Errorf("密碼錯誤")
		}

		// 檢查帳號是否啟用
		infra.AddEvent(span, "checking_account_status")
		if !driverInfo.IsActive {
			infra.AddEvent(span, "account_not_active")
			infra.SetAttributes(span, infra.AttrErrorType("account_inactive"))
			s.logger.Warn().
				Str("司機帳號", account).
				Msg("司機登入失敗 - 帳號未啟用")
			return fmt.Errorf("帳號未啟用")
		}

		// 生成 JWT token
		infra.AddEvent(span, "generating_jwt_token")
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
			"driver_id": driverInfo.ID.Hex(),
			"account":   driverInfo.Account,
			"name":      driverInfo.Name,
			"car_plate": driverInfo.CarPlate,
			"fleet":     string(driverInfo.Fleet),
			"car_model": driverInfo.CarModel,
			"type":      string(model.TokenTypeDriver),
			"exp":       time.Now().Add(time.Hour * time.Duration(s.jwtExpiresHours)).Unix(),
		})

		var tokenErr error
		tokenString, tokenErr = token.SignedString([]byte(s.jwtSecretKey))
		if tokenErr != nil {
			infra.AddEvent(span, "jwt_generation_failed")
			infra.SetAttributes(span, infra.AttrErrorType("jwt_generation_error"))
			s.logger.Error().
				Str("司機編號", driverInfo.ID.Hex()).
				Str("司機帳號", driverInfo.Account).
				Str("車隊名稱", string(driverInfo.Fleet)).
				Str("車牌號碼", driverInfo.CarPlate).
				Str("錯誤原因", tokenErr.Error()).
				Msg("司機 JWT 令牌生成失敗")
			return tokenErr
		}

		infra.AddEvent(span, "jwt_token_generated")

		// 更新設備資訊（如果提供的話）
		infra.AddEvent(span, "updating_device_info")
		updateFields := bson.M{
			"last_online": time.Now(),
		}

		if deviceModelName != "" {
			updateFields["device_model_name"] = deviceModelName
		}
		if deviceDeviceName != "" {
			updateFields["device_device_name"] = deviceDeviceName
		}
		if deviceBrand != "" {
			updateFields["device_brand"] = deviceBrand
		}
		if deviceManufacturer != "" {
			updateFields["device_manufacturer"] = deviceManufacturer
		}
		if deviceAppVersion != "" {
			updateFields["device_app_version"] = deviceAppVersion
		}

		// 更新司機資料
		_, updateErr := collection.UpdateOne(ctx,
			bson.M{"_id": driverInfo.ID},
			bson.M{"$set": updateFields},
		)
		if updateErr != nil {
			infra.AddEvent(span, "device_info_update_failed")
			s.logger.Warn().
				Str("司機編號", driverInfo.ID.Hex()).
				Err(updateErr).
				Msg("更新司機設備資訊失敗，但登入成功")
		} else {
			infra.AddEvent(span, "device_info_updated")
			// 更新本地 driver 物件的設備資訊
			if deviceModelName != "" {
				driverInfo.DeviceModelName = deviceModelName
			}
			if deviceDeviceName != "" {
				driverInfo.DeviceDeviceName = deviceDeviceName
			}
			if deviceBrand != "" {
				driverInfo.DeviceBrand = deviceBrand
			}
			if deviceManufacturer != "" {
				driverInfo.DeviceManufacturer = deviceManufacturer
			}
			if deviceAppVersion != "" {
				driverInfo.DeviceAppVersion = deviceAppVersion
			}
			driverInfo.LastOnline = time.Now()
		}

		driver = &driverInfo
		return nil
	},
		infra.AttrOperation("driver_login"),
		infra.AttrDriverAccount(account),
	)

	if err != nil {
		return nil, "", err
	}

	s.logger.Debug().
		Str("司機編號", driver.ID.Hex()).
		Str("司機帳號", driver.Account).
		Str("司機姓名", driver.Name).
		Str("車隊名稱", string(driver.Fleet)).
		Str("車牌號碼", driver.CarPlate).
		Msg("司機登入成功 - 服務層驗證完成")

	return driver, tokenString, nil
}

// getPreRedisData 從 Redis 獲取預計算的距離時間數據
func (s *DriverService) getPreRedisData(ctx context.Context, driverID, orderID string) (float64, int, bool) {
	if s.eventManager == nil {
		return 0, 0, false
	}

	notifyingOrderKey := fmt.Sprintf("notifying_order:%s", driverID)
	cachedData, cacheErr := s.eventManager.GetCache(ctx, notifyingOrderKey)

	if cacheErr != nil || cachedData == "" {
		return 0, 0, false
	}

	var redisNotifyingOrder *driverModels.RedisNotifyingOrder
	if unmarshalErr := json.Unmarshal([]byte(cachedData), &redisNotifyingOrder); unmarshalErr != nil {
		return 0, 0, false
	}

	// 檢查是否為同一個訂單
	if redisNotifyingOrder.OrderID != orderID {
		return 0, 0, false
	}

	distanceKm := redisNotifyingOrder.OrderData.EstPickUpDist
	estPickupMins := redisNotifyingOrder.OrderData.EstPickupMins

	s.logger.Info().
		Str("訂單編號", orderID).
		Str("司機編號", driverID).
		Float64("距離_km", distanceKm).
		Int("時間_分鐘", estPickupMins).
		Msg("✅ 重用 Redis 中的距離時間數據，避免重複計算")

	return distanceKm, estPickupMins, true
}

// CalcDistanceAndMins 使用 DirectionsMatrixInverse 計算司機當前位置到客戶上車地點的距離和時間
func (s *DriverService) CalcDistanceAndMins(ctx context.Context, driver *model.DriverInfo, order *model.Order) (float64, int, error) {
	if s.crawlerService == nil {
		return 0, 0, fmt.Errorf("CrawlerService 未初始化")
	}

	// 檢查司機位置
	if driver.Lat == "" || driver.Lng == "" {
		return 0, 0, fmt.Errorf("司機位置資訊不完整")
	}

	// 檢查客戶上車點位置
	if order.Customer.PickupLat == nil || order.Customer.PickupLng == nil {
		return 0, 0, fmt.Errorf("客戶上車點位置資訊不完整")
	}

	// 構建司機位置和客戶上車點坐標
	driverOrigin := fmt.Sprintf("%s,%s", driver.Lat, driver.Lng)
	pickupDestination := fmt.Sprintf("%s,%s", *order.Customer.PickupLat, *order.Customer.PickupLng)

	s.logger.Info().
		Str("driver_id", driver.ID.Hex()).
		Str("order_id", order.ID.Hex()).
		Str("driver_location", driverOrigin).
		Str("pickup_location", pickupDestination).
		Msg("開始計算司機到客戶上車點的距離和時間")

	// 使用 DirectionsMatrixInverse 計算路徑
	routes, err := s.crawlerService.DirectionsMatrixInverse(ctx, []string{driverOrigin}, pickupDestination)
	if err != nil {
		s.logger.Error().
			Err(err).
			Str("driver_id", driver.ID.Hex()).
			Str("order_id", order.ID.Hex()).
			Msg("使用 DirectionsMatrixInverse 計算距離失敗")
		return 0, 0, fmt.Errorf("計算路徑距離失敗: %w", err)
	}

	if len(routes) == 0 {
		s.logger.Warn().
			Str("driver_id", driver.ID.Hex()).
			Str("order_id", order.ID.Hex()).
			Msg("DirectionsMatrixInverse 返回空結果")
		return 0, 0, fmt.Errorf("無法獲取路徑資訊")
	}

	route := routes[0]
	distanceKm := route.DistanceKm
	estPickupMins := route.TimeInMinutes

	s.logger.Info().
		Str("driver_id", driver.ID.Hex()).
		Str("order_id", order.ID.Hex()).
		Float64("distance_km", distanceKm).
		Int("estimated_mins", estPickupMins).
		Str("distance_str", route.Distance).
		Str("time_str", route.Time).
		Msg("✅ 成功計算司機到客戶上車點的距離和時間")

	return distanceKm, estPickupMins, nil
}

// buildDriverObject 構建訂單司機資訊物件
func (s *DriverService) buildDriverObject(driver *model.DriverInfo, adjustMins *int, distanceKm float64, estPickupMins int, estPickupTimeStr string) model.Driver {
	return model.Driver{
		AssignedDriver:  driver.ID.Hex(),
		CarNo:           driver.CarPlate,
		CarColor:        driver.CarColor,
		AdjustMins:      adjustMins,
		Lat:             &driver.Lat,
		Lng:             &driver.Lng,
		LineUserID:      driver.LineUID,
		Name:            driver.Name,
		EstPickupMins:   estPickupMins,
		EstPickupDistKm: distanceKm,
		EstPickupTime:   estPickupTimeStr,
	}
}

// addAcceptOrderLog 新增接單日誌
func (s *DriverService) addAcceptOrderLog(ctx context.Context, orderID string, driver *model.DriverInfo, finalEstPickupMins int, distanceKm float64, currentRounds int) {
	if err := s.orderService.AddOrderLog(ctx, orderID, model.OrderLogActionDriverAccept,
		string(driver.Fleet), driver.Name, driver.CarPlate, driver.ID.Hex(),
		fmt.Sprintf("預估到達時間: %d分鐘 (距離: %.2fkm)", finalEstPickupMins, distanceKm), currentRounds); err != nil {
		s.logger.Error().
			Str("訂單編號", orderID).
			Str("司機編號", driver.ID.Hex()).
			Str("錯誤原因", err.Error()).
			Msg("新增接單記錄失敗")
	}
}

func (s *DriverService) AcceptOrder(ctx context.Context, driver *model.DriverInfo, orderID string, adjustMins *int, requestTime time.Time) (float64, int, string, string, string, error) {
	// 步驟1: 從Redis中獲取計算厚的訂單數據 (
	distanceKm, estPickupMins, foundPreCalc := s.getPreRedisData(ctx, driver.ID.Hex(), orderID)

	// 步驟2: 計算最終預估時間
	finalEstPickupMins := estPickupMins
	if adjustMins != nil {
		finalEstPickupMins = estPickupMins + *adjustMins
	}

	// 轉換為台北時間
	taipeiLocation := time.FixedZone("Asia/Taipei", 8*3600)
	estPickupTime := requestTime.Add(time.Duration(finalEstPickupMins) * time.Minute).In(taipeiLocation)
	estPickupTimeStr := estPickupTime.Format("15:04:05")

	// 步驟3: 構建司機物件
	driverInfoForOrder := s.buildDriverObject(driver, adjustMins, distanceKm, estPickupMins, estPickupTimeStr)

	// 步驟4: 原子性訂單更新（CAS操作）
	matched, err := s.orderService.AcceptOrderAction(ctx, orderID, driverInfoForOrder, model.OrderStatusEnroute, &requestTime)
	if err != nil {
		s.logger.Error().Str("order_id", orderID).Str("driver_id", driver.ID.Hex()).Err(err).Msg("司機接單資料庫操作失敗")
		return 0, 0, "", "", "", fmt.Errorf("接單失敗")
	}

	if !matched {
		return 0, 0, "", "", "", fmt.Errorf("接單失敗")
	}

	// 步驟5: 先同步更新司機狀態和CurrentOrderId，防止dispatcher競爭條件
	if statusErr := s.UpdateDriverStatusType(ctx, driver.ID.Hex(), model.DriverStatusEnroute, string(model.DriverReasonAcceptOrder), orderID); statusErr == nil {
		s.logger.Info().
			Str("order_id", orderID).
			Str("driver_id", driver.ID.Hex()).
			Str("status", string(model.DriverStatusEnroute)).
			Msg("司機狀態已更新為前往上車點")
	} else {
		s.logger.Error().Err(statusErr).
			Str("order_id", orderID).
			Str("driver_id", driver.ID.Hex()).
			Msg("更新司機狀態失敗")
		// 狀態更新失敗但不回滾訂單，記錄錯誤
	}

	// 步驟5.5: 更新司機的 CurrentOrderId
	if updateErr := s.UpdateDriverCurrentOrderId(ctx, driver.ID.Hex(), orderID); updateErr != nil {
		s.logger.Error().Err(updateErr).
			Str("order_id", orderID).
			Str("driver_id", driver.ID.Hex()).
			Msg("更新司機CurrentOrderId失敗")
		// 記錄錯誤但不回滾訂單，繼續執行
	} else {
		s.logger.Info().
			Str("order_id", orderID).
			Str("driver_id", driver.ID.Hex()).
			Msg("司機CurrentOrderId已更新")
	}

	// 步驟6: 同步發布 Redis 事件（在狀態更新之後，確保dispatcher看到正確狀態）
	if s.eventManager != nil {
		acceptResponse := &infra.DriverResponse{
			OrderID:   orderID,
			DriverID:  driver.ID.Hex(),
			Action:    infra.DriverResponseAccept,
			Timestamp: time.Now(),
			Details: map[string]interface{}{
				"distance_km":     distanceKm,
				"est_pickup_mins": finalEstPickupMins,
				"est_pickup_time": estPickupTimeStr,
				"adjust_mins":     adjustMins,
				"driver_status":   string(model.DriverStatusEnroute),
			},
		}

		if publishErr := s.eventManager.PublishDriverResponse(ctx, acceptResponse); publishErr != nil {
			s.logger.Error().Err(publishErr).
				Str("order_id", orderID).
				Str("driver_id", driver.ID.Hex()).
				Msg("發送接單事件通知失敗")
		} else {
			s.logger.Info().
				Str("order_id", orderID).
				Str("driver_id", driver.ID.Hex()).
				Msg("接單事件通知已發送")
		}
	}

	// 步驟7: 異步處理所有通知（使用 NotificationService）
	if s.notificationService != nil {
		go func() {
			if notifyErr := s.notificationService.NotifyOrderAccepted(context.Background(), orderID, driver); notifyErr != nil {
				s.logger.Error().Err(notifyErr).
					Str("order_id", orderID).
					Str("driver_id", driver.ID.Hex()).
					Msg("統一通知服務處理失敗")
			}
		}()
	}

	// 步驟8: 異步處理非關鍵操作
	go func() {
		bgCtx := context.Background()

		// 添加接單日誌（需要查詢 rounds，但這是異步的）
		if order, getErr := s.orderService.GetOrderByID(bgCtx, orderID); getErr == nil {
			currentRounds := 1
			if order.Rounds != nil {
				currentRounds = *order.Rounds
			}
			s.addAcceptOrderLog(bgCtx, orderID, driver, finalEstPickupMins, distanceKm, currentRounds)
		}
	}()

	s.logger.Info().
		Str("司機編號", driver.ID.Hex()).
		Str("司機姓名", driver.Name).
		Str("車牌號碼", driver.CarPlate).
		Str("訂單編號", orderID).
		Float64("距離_km", distanceKm).
		Int("原始預估時間_分鐘", estPickupMins).
		Int("最終預估時間_分鐘", finalEstPickupMins).
		Str("預估到達時間", estPickupTimeStr).
		Bool("使用預計算數據", foundPreCalc).
		Msg("司機成功接受訂單")

	// 返回司機最終狀態和訂單狀態
	finalDriverStatus := string(model.DriverStatusEnroute)
	finalOrderStatus := string(model.OrderStatusEnroute)

	return distanceKm, finalEstPickupMins, estPickupTimeStr, finalDriverStatus, finalOrderStatus, nil
}

// AcceptScheduledOrder 司機接收預約訂單－但尚未激活
func (s *DriverService) AcceptScheduledOrder(ctx context.Context, driver *model.DriverInfo, orderID string, requestTime time.Time) (string, string, string, error) {
	// 步驟1: 驗證訂單存在且為預約訂單
	order, err := s.orderService.GetOrderByID(ctx, orderID)
	if err != nil {
		s.logger.Error().Str("order_id", orderID).Err(err).Msg("查找預約訂單失敗")
		return "", "", "", fmt.Errorf("查找預約訂單失敗")
	}

	// 檢查是否為預約訂單
	if order.Type != model.OrderTypeScheduled {
		return "", "", "", fmt.Errorf("此訂單不是預約訂單")
	}

	// 檢查訂單是否已被接單
	if order.Driver.AssignedDriver != "" {
		return "", "", "", fmt.Errorf("此預約訂單已被其他司機接單")
	}

	// 檢查訂單狀態
	if order.Status != model.OrderStatusWaiting {
		return "", "", "", fmt.Errorf("此預約訂單狀態不正確，無法接單")
	}

	// 步驟2: 檢查司機是否已有預約訂單
	if driver.CurrentOrderScheduleId != nil && *driver.CurrentOrderScheduleId != "" {
		s.logger.Warn().
			Str("driver_id", driver.ID.Hex()).
			Str("current_order_schedule_id", *driver.CurrentOrderScheduleId).
			Msg("司機已有預約訂單，無法接新單")
		return "", "", "", fmt.Errorf("司機已有預約訂單，無法接新單")
	}

	// 步驟3: 更新預約訂單，分配司機並設置為已接受狀態
	driverInfoForOrder := s.buildDriverObjectForScheduled(driver)

	// 預約單接受後設置為預約單已接受狀態，而不是立即進入前往上車點
	matched, err := s.orderService.AcceptScheduledOrderWithCondition(ctx, orderID, driverInfoForOrder, model.OrderStatusWaiting, &requestTime)
	if err != nil {
		s.logger.Error().Str("order_id", orderID).Str("driver_id", driver.ID.Hex()).Err(err).Msg("司機接預約單資料庫操作失敗")
		return "", "", "", fmt.Errorf("接預約單失敗")
	}
	if !matched {
		return "", "", "", fmt.Errorf("接預約單失敗，可能已被其他司機接單")
	}

	// 步驟4: 更新司機的預約狀態
	if err := s.updateDriverScheduleInfo(ctx, driver.ID.Hex(), orderID, order.ScheduledAt); err != nil {
		s.logger.Error().Err(err).Str("driver_id", driver.ID.Hex()).Msg("更新司機預約狀態失敗")
	}

	// 步驟5: 記錄接單日誌
	if err := s.orderService.AddOrderLog(ctx, orderID, model.OrderLogActionDriverAccept,
		string(driver.Fleet), driver.Name, driver.CarPlate, driver.ID.Hex(),
		"司機接受預約訂單", 1); err != nil {
		s.logger.Error().Str("order_id", orderID).Str("driver_id", driver.ID.Hex()).Err(err).Msg("新增預約單接單記錄失敗")
	}

	// 步驟6: 準備回傳資料
	scheduledTimeStr := ""
	if order.ScheduledAt != nil {
		// 格式化為 UTC 時區
		scheduledTimeStr = order.ScheduledAt.Format("2006-01-02 15:04:05")
	}

	pickupAddress := order.Customer.PickupAddress
	if pickupAddress == "" {
		pickupAddress = order.Customer.InputPickupAddress
	}

	s.logger.Info().
		Str("order_id", orderID).
		Str("driver_id", driver.ID.Hex()).
		Str("driver_name", driver.Name).
		Str("scheduled_time", scheduledTimeStr).
		Msg("司機成功接受預約訂單")

	// 步驟7: 發送預約單司機接收通知（尚未激活）
	if s.notificationService != nil {
		if err := s.notificationService.NotifyScheduledOrderAccepted(ctx, orderID, driver); err != nil {
			s.logger.Error().Err(err).
				Str("order_id", orderID).
				Str("driver_id", driver.ID.Hex()).
				Msg("預約單司機接收通知發送失敗")
		} else {
			s.logger.Info().
				Str("order_id", orderID).
				Str("driver_id", driver.ID.Hex()).
				Msg("預約單司機接收通知已發送")
		}
	} else {
		s.logger.Warn().Str("order_id", orderID).Msg("NotificationService 未初始化，跳過預約單司機接收通知")
	}

	return scheduledTimeStr, pickupAddress, "", nil
}

// ActivateScheduledOrder 激活預約單為即時單（司機開始前往客上）
func (s *DriverService) ActivateScheduledOrder(ctx context.Context, driver *model.DriverInfo, orderID string, requestTime time.Time) (*model.Order, error) {
	// 步驟1: 驗證訂單存在且為預約單已接受狀態
	order, err := s.orderService.GetOrderByID(ctx, orderID)
	if err != nil {
		s.logger.Error().Str("order_id", orderID).Err(err).Msg("查找預約訂單失敗")
		return nil, fmt.Errorf("查找預約訂單失敗")
	}

	// 檢查是否為預約單
	if order.Type != model.OrderTypeScheduled {
		return nil, fmt.Errorf("此訂單不是預約訂單")
	}

	// 檢查訂單狀態是否為預約單已接受
	if order.Status != model.OrderStatusScheduleAccepted {
		return nil, fmt.Errorf("此預約訂單狀態不正確，必須是已接受狀態才能激活")
	}

	// 檢查是否由該司機接單
	if order.Driver.AssignedDriver != driver.ID.Hex() {
		return nil, fmt.Errorf("此預約訂單不是由該司機接單")
	}

	// 檢查司機是否有正在進行中的即時單
	currentOrderInfo, err := s.orderService.GetCurrentOrderByDriverID(ctx, driver.ID.Hex())
	if err == nil && currentOrderInfo != nil {
		s.logger.Warn().
			Str("driver_id", driver.ID.Hex()).
			Str("current_order_id", currentOrderInfo.Order.ID.Hex()).
			Str("current_order_status", string(currentOrderInfo.OrderStatus)).
			Str("schedule_order_id", orderID).
			Msg("司機有正在進行中的即時單，無法激活預約單")
		return nil, fmt.Errorf("司機有正在進行中的即時單，無法激活預約單")
	}

	// 步驟2: 計算距離和預計到達時間
	// 直接使用 DirectionsMatrixInverse 實時計算距離和時間
	s.logger.Info().Str("order_id", orderID).Msg("使用 DirectionsMatrixInverse 實時計算距離和時間")
	distanceKm, estPickupMins, calcErr := s.CalcDistanceAndMins(ctx, driver, order)
	if calcErr != nil {
		s.logger.Error().Err(calcErr).Str("order_id", orderID).Msg("實時計算距離時間失敗，使用默認值")
		distanceKm, estPickupMins = 0, 0
	}

	activateTime := requestTime
	// 轉換為台北時間
	taipeiLocation := time.FixedZone("Asia/Taipei", 8*3600)
	estPickupTime := activateTime.Add(time.Duration(estPickupMins) * time.Minute).In(taipeiLocation)
	estPickupTimeStr := estPickupTime.Format("15:04:05")

	// 步驟3: 更新訂單狀態為前往上車點，並更新司機狀態
	driverInfoForOrder := s.buildDriverObject(driver, nil, distanceKm, estPickupMins, estPickupTimeStr)

	matched, err := s.orderService.ActivateScheduledOrderWithCondition(ctx, orderID, driverInfoForOrder, model.OrderStatusScheduleAccepted, &requestTime)
	if err != nil {
		s.logger.Error().Str("order_id", orderID).Str("driver_id", driver.ID.Hex()).Err(err).Msg("激活預約單資料庫操作失敗")
		return nil, fmt.Errorf("激活預約單失敗")
	}
	if !matched {
		return nil, fmt.Errorf("激活預約單失敗，訂單狀態可能已變更")
	}

	// 步驟4: 更新司機狀態為前往上車點
	if statusErr := s.UpdateDriverStatusType(ctx, driver.ID.Hex(), model.DriverStatusEnroute, string(model.DriverReasonActivateSchedule), orderID); statusErr != nil {
		s.logger.Error().Err(statusErr).Str("driver_id", driver.ID.Hex()).Msg("更新司機狀態失敗")
		// 這裡不回滾訂單，因為訂單已經激活了
	}

	// 步驟5: 異步處理所有通知（使用 NotificationService）
	if s.notificationService != nil {
		go func() {
			if notifyErr := s.notificationService.NotifyScheduledOrderActivated(context.Background(), orderID, driver, distanceKm, estPickupMins); notifyErr != nil {
				s.logger.Error().Err(notifyErr).
					Str("order_id", orderID).
					Str("driver_id", driver.ID.Hex()).
					Msg("預約單激活統一通知服務處理失敗")
			}
		}()
	}

	// 步驟6: 異步處理非關鍵操作
	go func() {
		bgCtx := context.Background()

		// 添加激活日誌
		if err := s.orderService.AddOrderLog(bgCtx, orderID, model.OrderLogActionDriverAccept,
			string(driver.Fleet), driver.Name, driver.CarPlate, driver.ID.Hex(),
			fmt.Sprintf("司機激活預約訂單，預估到達時間: %d分鐘 (距離: %.2fkm)", estPickupMins, distanceKm), 1); err != nil {
			s.logger.Error().Str("order_id", orderID).Str("driver_id", driver.ID.Hex()).Err(err).Msg("新增預約單激活記錄失敗")
		}
	}()

	s.logger.Info().
		Str("order_id", orderID).
		Str("driver_id", driver.ID.Hex()).
		Str("driver_name", driver.Name).
		Float64("distance_km", distanceKm).
		Int("est_pickup_mins", estPickupMins).
		Msg("司機成功激活預約訂單")

	// 重新查詢更新後的訂單狀態
	updatedOrder, err := s.orderService.GetOrderByID(ctx, orderID)
	if err != nil {
		s.logger.Error().Str("order_id", orderID).Err(err).Msg("查詢更新後的訂單失敗")
		return nil, fmt.Errorf("查詢更新後的訂單失敗")
	}

	return updatedOrder, nil
}

func (s *DriverService) RejectOrder(ctx context.Context, driver *model.DriverInfo, orderID string) error {
	// 新增：使用 Redis 鎖防止重複拒絕
	var rejectLockRelease func()
	if s.eventManager != nil {
		lockTTL := 10 * time.Second // 拒絕鎖存活10秒，足夠完成拒絕操作
		lockAcquired, releaseLock, lockErr := s.eventManager.AcquireOrderRejectLock(ctx, orderID, driver.ID.Hex(), "manual", lockTTL)

		if lockErr != nil {
			s.logger.Error().Err(lockErr).
				Str("訂單編號", orderID).
				Str("司機編號", driver.ID.Hex()).
				Msg("獲取訂單拒絕鎖失敗")
			// 不阻塞主流程，繼續執行後續邏輯
		} else if !lockAcquired {
			s.logger.Info().
				Str("訂單編號", orderID).
				Str("司機編號", driver.ID.Hex()).
				Str("司機姓名", driver.Name).
				Msg("🔒 訂單拒絕鎖已被持有，避免重複拒絕操作")
			return nil // 直接返回成功，避免重複拒絕
		} else {
			rejectLockRelease = releaseLock
			s.logger.Debug().
				Str("訂單編號", orderID).
				Str("司機編號", driver.ID.Hex()).
				Msg("✅ 訂單拒絕鎖獲取成功，開始處理拒絕邏輯")
		}
	}

	// 確保在函數結束時釋放鎖
	defer func() {
		if rejectLockRelease != nil {
			rejectLockRelease()
		}
	}()

	o, err := s.orderService.GetOrderByID(ctx, orderID)
	if err != nil {
		s.logger.Error().
			Str("訂單編號", orderID).
			Str("司機編號", driver.ID.Hex()).
			Str("錯誤原因", err.Error()).
			Msg("司機拒單失敗 - 訂單不存在")
		s.logger.Error().Str("order_id", orderID).Str("driver_id", driver.ID.Hex()).Err(err).Msg("司機拒單失敗，訂單不存在 (Driver reject order failed, order not found)")
		return fmt.Errorf("訂單不存在 (Order not found): %w", err)
	}

	// 檢查司機是否已經拒絕過這個訂單
	driverIDStr := driver.ID.Hex()
	for _, logEntry := range o.Logs {
		if logEntry.Action == model.OrderLogActionDriverReject && logEntry.DriverID == driverIDStr {
			s.logger.Info().
				Str("訂單編號", orderID).
				Str("司機編號", driverIDStr).
				Str("司機姓名", driver.Name).
				Time("前次拒單時間", logEntry.Timestamp).
				Msg("司機已拒絕過此訂單，直接返回成功無需重複操作")
			return nil // 直接返回成功，不執行任何拒單邏輯
		}
	}

	// 添加到黑名單
	if infra.AppConfig.DriverBlacklist.Enabled && s.blacklistService != nil {
		if err := s.blacklistService.AddDriverToBlacklist(ctx, driver.ID.Hex(), o.Customer.PickupAddress); err != nil {
			s.logger.Error().Err(err).
				Str("訂單編號", orderID).
				Str("司機編號", driver.ID.Hex()).
				Msg("司機拒單失敗 - 添加到Redis黑名單失敗")
			return fmt.Errorf("添加到黑名單失敗 (Add to blacklist failed): %w", err)
		}
	}
	err = s.IncrementRejectedCount(ctx, driver.ID.Hex())
	if err != nil {
		s.logger.Error().
			Str("司機編號", driver.ID.Hex()).
			Str("錯誤原因", err.Error()).
			Msg("司機拒單計數失敗")
	}

	// 發送拒單事件通知調度器
	if s.eventManager != nil {
		rejectResponse := &infra.DriverResponse{
			OrderID:   orderID,
			DriverID:  driver.ID.Hex(),
			Action:    infra.DriverResponseReject,
			Timestamp: time.Now(),
			Details: map[string]interface{}{
				"reason": "司機主動拒單",
			},
		}

		if publishErr := s.eventManager.PublishDriverResponse(ctx, rejectResponse); publishErr != nil {
			s.logger.Error().Err(publishErr).
				Str("order_id", orderID).
				Str("driver_id", driver.ID.Hex()).
				Msg("發送拒單事件通知失敗")
			// 不影響主流程，繼續執行
		} else {
			s.logger.Info().
				Str("order_id", orderID).
				Str("driver_id", driver.ID.Hex()).
				Msg("拒單事件通知已發送")
		}
	} else {
		s.logger.Warn().
			Str("order_id", orderID).
			Str("driver_id", driver.ID.Hex()).
			Msg("事件管理器未初始化，無法發送拒單通知")
	}

	// 添加拒單日誌
	currentRounds := 1
	if o.Rounds != nil {
		currentRounds = *o.Rounds
	}
	if err := s.orderService.AddOrderLog(ctx, orderID, model.OrderLogActionDriverReject,
		string(driver.Fleet), driver.Name, driver.CarPlate, driver.ID.Hex(), "司機拒絕接單", currentRounds); err != nil {
		s.logger.Error().Err(err).Msg("新增拒單記錄失敗")
	}

	// 通知處理 - 使用統一的 NotificationService
	if s.notificationService != nil {
		if err := s.notificationService.NotifyOrderRejected(ctx, orderID, driver); err != nil {
			s.logger.Error().Err(err).
				Str("order_id", orderID).
				Str("driver_id", driver.ID.Hex()).
				Msg("拒單通知發送失敗")
		}
	}

	// 拒絕訂單完成，不重置司機狀態
	s.logger.Info().
		Str("司機編號", driver.ID.Hex()).
		Str("訂單編號", orderID).
		Str("司機姓名", driver.Name).
		Str("車牌號碼", driver.CarPlate).
		Msg("司機拒單成功，保持司機現有狀態不變")

	return nil
}

func (s *DriverService) CompleteOrder(ctx context.Context, driver *model.DriverInfo, orderID string, duration int, requestTime time.Time) (string, string, error) {
	// 步驟1: 獲取訂單並更新完成信息（避免額外查詢）
	order, err := s.orderService.GetOrderByID(ctx, orderID)
	if err != nil {
		s.logger.Error().Str("order_id", orderID).Str("driver_id", driver.ID.Hex()).Err(err).Msg("完成訂單失敗，訂單不存在")
		return "", "", fmt.Errorf("訂單不存在")
	}

	// 步驟2: 原子性更新訂單狀態和完成信息
	order.Driver.Duration = duration
	order.CompletionTime = &requestTime
	_, err = s.orderService.UpdateOrder(ctx, order)
	if err != nil {
		s.logger.Error().Str("order_id", orderID).Str("driver_id", driver.ID.Hex()).Err(err).Msg("更新訂單資訊失敗")
		return "", "", fmt.Errorf("更新訂單失敗")
	}

	// 步驟3: 優先更新司機狀態（關鍵，影響派單邏輯，防止被分派新單）
	if err := s.UpdateDriverStatusType(ctx, driver.ID.Hex(), model.DriverStatusIdle); err != nil {
		s.logger.Error().Err(err).
			Str("order_id", orderID).
			Str("driver_id", driver.ID.Hex()).
			Msg("更新司機狀態失敗")
		return "", "", fmt.Errorf("更新司機狀態失敗: %w", err)
	}

	// 步驟3.2: 根據訂單類型清除對應的訂單ID
	if order.Type == model.OrderTypeScheduled {
		// 預約單：清除 CurrentOrderScheduleId (在步驟3.5中處理)
	} else {
		// 即時單：清除 CurrentOrderId
		if err := s.UpdateDriverCurrentOrderId(ctx, driver.ID.Hex(), ""); err != nil {
			s.logger.Error().Err(err).
				Str("order_id", orderID).
				Str("driver_id", driver.ID.Hex()).
				Msg("清除司機CurrentOrderId失敗")
			// 記錄錯誤但不影響主流程，繼續執行
		} else {
			s.logger.Info().
				Str("order_id", orderID).
				Str("driver_id", driver.ID.Hex()).
				Msg("司機CurrentOrderId已清除")
		}
	}

	// 步驟3.5: 如果完成的是預約單，清理司機的預約狀態
	if order.Type == model.OrderTypeScheduled {
		if err := s.ResetDriverScheduledOrder(ctx, driver.ID.Hex()); err != nil {
			s.logger.Error().Err(err).
				Str("order_id", orderID).
				Str("driver_id", driver.ID.Hex()).
				Msg("清理司機預約狀態失敗")
			// 記錄錯誤但不影響主流程，繼續執行
		} else {
			s.logger.Info().
				Str("order_id", orderID).
				Str("driver_id", driver.ID.Hex()).
				Msg("預約單完成，已清理司機預約狀態")
		}
	}

	// 步驟4: 更新訂單狀態為完成（不會觸發舊通知機制，統一由NotificationService處理）
	_, err = s.orderService.UpdateOrderStatus(ctx, orderID, model.OrderStatusCompleted)
	if err != nil {
		s.logger.Error().Str("order_id", orderID).Str("driver_id", driver.ID.Hex()).Err(err).Msg("更新完成訂單狀態失敗")
		return "", "", fmt.Errorf("更新訂單狀態失敗")
	}

	// 步驟5: 異步更新司機完成訂單數（優化效能，不阻塞主流程）
	go func() {
		if err := s.IncrementCompletedCount(context.Background(), driver.ID.Hex()); err != nil {
			s.logger.Error().Err(err).
				Str("order_id", orderID).
				Str("driver_id", driver.ID.Hex()).
				Msg("更新司機完成訂單數失敗")
		}
	}()

	// 步驟6: 異步處理所有通知（使用 NotificationService）
	if s.notificationService != nil {
		go func() {
			if notifyErr := s.notificationService.NotifyOrderCompleted(context.Background(), orderID, driver); notifyErr != nil {
				s.logger.Error().Err(notifyErr).
					Str("order_id", orderID).
					Str("driver_id", driver.ID.Hex()).
					Msg("統一通知服務處理失敗")
			}
		}()
	}

	// 步驟7: 異步處理非關鍵操作
	go func() {
		bgCtx := context.Background()

		// 添加完成訂單日誌（需要查詢 rounds，但這是異步的）
		currentRounds := 1
		if order.Rounds != nil {
			currentRounds = *order.Rounds
		}
		if err := s.orderService.AddOrderLog(bgCtx, orderID, model.OrderLogActionOrderCompleted,
			string(driver.Fleet), driver.Name, driver.CarPlate, driver.ID.Hex(),
			fmt.Sprintf("訂單完成，用時: %d秒", duration), currentRounds); err != nil {
			s.logger.Error().
				Str("訂單編號", orderID).
				Str("錯誤原因", err.Error()).
				Msg("新增完成訂單記錄失敗")
		}
	}()

	s.logger.Info().
		Str("司機編號", driver.ID.Hex()).
		Str("司機姓名", driver.Name).
		Str("車牌號碼", driver.CarPlate).
		Str("訂單編號", orderID).
		Int("用時_秒", duration).
		Msg("司機已完成訂單")

	// 返回最終狀態值
	driverStatus := string(model.DriverStatusIdle)
	orderStatus := string(model.OrderStatusCompleted)

	return driverStatus, orderStatus, nil
}

func (s *DriverService) PickUpCustomer(ctx context.Context, driver *model.DriverInfo, orderID string, hasMeterJump bool, requestTime time.Time) (string, string, error) {
	// 步驟1: 優先更新司機狀態（關鍵，影響派單邏輯，避免被分派新單）
	if err := s.UpdateDriverStatusType(ctx, driver.ID.Hex(), model.DriverStatusExecuting); err != nil {
		s.logger.Error().Err(err).
			Str("order_id", orderID).
			Str("driver_id", driver.ID.Hex()).
			Msg("更新司機狀態失敗")
		return "", "", fmt.Errorf("更新司機狀態失敗: %w", err)
	}

	s.logger.Info().
		Str("order_id", orderID).
		Str("driver_id", driver.ID.Hex()).
		Str("status", string(model.DriverStatusExecuting)).
		Msg("司機狀態已更新為執行任務")

	// 步驟2: 更新訂單狀態為正在執行
	updatedOrder, err := s.orderService.UpdateOrderStatusWithPickupTime(ctx, orderID, model.OrderStatusExecuting, hasMeterJump, requestTime)
	if err != nil {
		s.logger.Error().Str("order_id", orderID).Str("driver_id", driver.ID.Hex()).Err(err).Msg("更新訂單狀態為執行任務失敗")
		return "", "", fmt.Errorf("更新訂單狀態失敗: %w", err)
	}

	// 步驟3: 異步處理所有通知（使用 NotificationService）
	if s.notificationService != nil {
		go func() {
			if notifyErr := s.notificationService.NotifyCustomerOnBoard(context.Background(), orderID, driver); notifyErr != nil {
				s.logger.Error().Err(notifyErr).
					Str("order_id", orderID).
					Str("driver_id", driver.ID.Hex()).
					Msg("統一通知服務處理失敗")
			}
		}()
	}

	// 步驟4: 異步處理非關鍵操作
	go func() {
		bgCtx := context.Background()

		// 添加客人上車日誌（需要查詢 rounds，但這是異步的）
		currentRounds := 1
		if updatedOrder.Rounds != nil {
			currentRounds = *updatedOrder.Rounds
		}
		if err := s.orderService.AddOrderLog(bgCtx, orderID, model.OrderLogActionCustomerPickup,
			string(driver.Fleet), driver.Name, driver.CarPlate, driver.ID.Hex(), "客人已上車，開始執行任務", currentRounds); err != nil {
			s.logger.Error().
				Str("訂單編號", orderID).
				Str("錯誤原因", err.Error()).
				Msg("新增客人上車記錄失敗")
		}
	}()

	logEvent := s.logger.Info().
		Str("訂單編號", orderID).
		Str("司機編號", driver.ID.Hex()).
		Str("司機姓名", driver.Name).
		Str("訂單狀態", string(updatedOrder.Status)).
		Bool("跳表狀態", updatedOrder.HasMeterJump)

	if updatedOrder.PickUpTime != nil {
		logEvent = logEvent.Str("客人上車時間", updatedOrder.PickUpTime.Format("2006-01-02 15:04:05"))
	}

	logEvent.Msg("客人上車成功")

	// 返回更新後的狀態值
	driverStatus := string(model.DriverStatusExecuting)
	orderStatus := string(updatedOrder.Status)

	return driverStatus, orderStatus, nil
}

func (s *DriverService) GetOrdersByDriverID(ctx context.Context, driverID string, pageNum, pageSize int) ([]*model.Order, int64, error) {
	orders, total, err := s.orderService.GetOrdersByDriverID(ctx, driverID, pageNum, pageSize)
	if err != nil {
		s.logger.Error().Str("driver_id", driverID).Int("page_num", pageNum).Int("page_size", pageSize).Err(err).Msg("獲取司機訂單列表失敗")
		return nil, 0, err
	}

	for _, order := range orders {
		order.Driver.AssignedDriver = ""
	}

	return orders, total, nil
}

func (s *DriverService) UpdateDriverProfile(ctx context.Context, driverID string, updateData *driverModels.UpdateDriverProfileInputBody) (*model.DriverInfo, error) {
	objectID, err := primitive.ObjectIDFromHex(driverID)
	if err != nil {
		s.logger.Error().Str("driver_id", driverID).Err(err).Msg("無效的司機ID格式 (Invalid driver ID format)")
		return nil, fmt.Errorf("無效的司機ID格式 (Invalid driver ID format): %w", err)
	}

	updateFields := bson.M{}
	if updateData.Name != nil {
		updateFields["name"] = *updateData.Name
	}
	if updateData.CarModel != nil {
		updateFields["car_model"] = *updateData.CarModel
	}
	if updateData.CarAge != nil {
		updateFields["car_age"] = *updateData.CarAge
	}
	if updateData.CarPlate != nil {
		updateFields["car_plate"] = *updateData.CarPlate
	}
	if updateData.CarColor != nil {
		updateFields["car_color"] = *updateData.CarColor
	}
	if updateData.NewPassword != nil {
		updateFields["password"] = *updateData.NewPassword
	}

	if len(updateFields) == 0 {
		return s.GetDriverByID(ctx, driverID)
	}

	updateFields["updated_at"] = time.Now()

	collection := s.mongoDB.GetCollection("drivers")
	filter := bson.M{"_id": objectID}
	update := bson.M{"$set": updateFields}

	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)
	var updatedDriver model.DriverInfo
	err = collection.FindOneAndUpdate(ctx, filter, update, opts).Decode(&updatedDriver)
	if err != nil {
		s.logger.Error().Str("driver_id", driverID).Err(err).Msg("更新司機資料失敗 (Update driver profile failed)")
		return nil, fmt.Errorf("更新司機資料失敗 (Update driver profile failed): %w", err)
	}

	return &updatedDriver, nil
}

func (s *DriverService) AdminUpdateDriverProfile(ctx context.Context, driverID string, updateData *admin.UpdateDriverProfileInput) (*model.DriverInfo, error) {
	objectID, err := primitive.ObjectIDFromHex(driverID)
	if err != nil {
		s.logger.Error().Str("driver_id", driverID).Err(err).Msg("無效的司機ID格式 (Invalid driver ID format)")
		return nil, fmt.Errorf("無效的司機ID格式 (Invalid driver ID format): %w", err)
	}

	updateFields := bson.M{}
	if updateData.Body.Name != nil {
		updateFields["name"] = *updateData.Body.Name
	}
	if updateData.Body.CarModel != nil {
		updateFields["car_model"] = *updateData.Body.CarModel
	}
	if updateData.Body.CarAge != nil {
		updateFields["car_age"] = *updateData.Body.CarAge
	}
	if updateData.Body.CarPlate != nil {
		updateFields["car_plate"] = *updateData.Body.CarPlate
	}
	if updateData.Body.CarColor != nil {
		updateFields["car_color"] = *updateData.Body.CarColor
	}
	if updateData.Body.NewPassword != nil {
		updateFields["password"] = *updateData.Body.NewPassword
	}
	if updateData.Body.Fleet != nil {
		updateFields["fleet"] = *updateData.Body.Fleet
	}
	if updateData.Body.IsActive != nil {
		updateFields["is_active"] = *updateData.Body.IsActive
	}
	if updateData.Body.IsApproved != nil {
		updateFields["is_approved"] = *updateData.Body.IsApproved
	}

	if len(updateFields) == 0 {
		return s.GetDriverByID(ctx, driverID)
	}

	updateFields["updated_at"] = time.Now()

	collection := s.mongoDB.GetCollection("drivers")
	filter := bson.M{"_id": objectID}
	update := bson.M{"$set": updateFields}

	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)
	var updatedDriver model.DriverInfo
	err = collection.FindOneAndUpdate(ctx, filter, update, opts).Decode(&updatedDriver)
	if err != nil {
		s.logger.Error().Str("driver_id", driverID).Err(err).Msg("管理員更新司機資料失敗 (Admin update driver profile failed)")
		return nil, fmt.Errorf("管理員更新司機資料失敗 (Admin update driver profile failed): %w", err)
	}

	return &updatedDriver, nil
}

func (s *DriverService) RemoveDriverFromFleet(ctx context.Context, driverID string) (*model.DriverInfo, error) {
	objectID, err := primitive.ObjectIDFromHex(driverID)
	if err != nil {
		s.logger.Error().Str("driver_id", driverID).Err(err).Msg("無效的司機ID格式 (Invalid driver ID format)")
		return nil, fmt.Errorf("無效的司機ID格式 (Invalid driver ID format): %w", err)
	}

	updateFields := bson.M{
		"fleet":      "",
		"updated_at": time.Now(),
	}

	collection := s.mongoDB.GetCollection("drivers")
	filter := bson.M{"_id": objectID}
	update := bson.M{"$set": updateFields}

	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)
	var updatedDriver model.DriverInfo
	err = collection.FindOneAndUpdate(ctx, filter, update, opts).Decode(&updatedDriver)
	if err != nil {
		s.logger.Error().Str("driver_id", driverID).Err(err).Msg("移除司機車隊失敗 (Remove driver from fleet failed)")
		return nil, fmt.Errorf("移除司機車隊失敗 (Remove driver from fleet failed): %w", err)
	}

	return &updatedDriver, nil
}

func (s *DriverService) CreateTrafficUsageLog(ctx context.Context, logEntry *model.TrafficUsageLog) (*model.TrafficUsageLog, error) {
	return s.trafficUsageLogService.CreateTrafficUsageLog(ctx, logEntry)
}

// GetDriversForApproval 獲取司機列表（用於管理審核）
func (s *DriverService) GetDriversForApproval(ctx context.Context, pageNum, pageSize int, fleet, hasFleet, searchKeyword string, isApproved string) ([]*model.DriverInfo, int64, error) {
	collection := s.mongoDB.GetCollection("drivers")

	// 建構查詢條件
	filter := bson.M{}

	// 車隊狀態過濾（優先處理，會覆蓋 fleet 參數）
	if hasFleet != "" {
		if hasFleetBool, err := strconv.ParseBool(hasFleet); err == nil {
			if hasFleetBool {
				// 有車隊：fleet 欄位存在且不為空字串
				filter["fleet"] = bson.M{"$exists": true, "$ne": ""}
			} else {
				// 沒車隊：fleet 欄位不存在或為空字串
				filter["$or"] = []bson.M{
					{"fleet": bson.M{"$exists": false}},
					{"fleet": ""},
				}
			}
		}
	} else if fleet != "" {
		// 只有在沒有 hasFleet 參數時才使用 fleet 過濾
		filter["fleet"] = fleet
	}

	// 審核狀態過濾
	if isApproved != "" {
		if approved, err := strconv.ParseBool(isApproved); err == nil {
			filter["is_approved"] = approved
		}
	}

	// 模糊搜尋（司機姓名、帳號、車牌號碼）
	if searchKeyword != "" {
		filter["$or"] = []bson.M{
			{"name": bson.M{"$regex": searchKeyword, "$options": "i"}},
			{"account": bson.M{"$regex": searchKeyword, "$options": "i"}},
			{"car_plate": bson.M{"$regex": searchKeyword, "$options": "i"}},
		}
	}

	// 計算總數
	totalCount, err := collection.CountDocuments(ctx, filter)
	if err != nil {
		s.logger.Error().Err(err).Msg("計算司機總數失敗")
		return nil, 0, err
	}

	// 分頁查詢
	skip := (pageNum - 1) * pageSize
	opts := options.Find().
		SetSkip(int64(skip)).
		SetLimit(int64(pageSize)).
		SetSort(bson.D{primitive.E{Key: "created_at", Value: -1}}) // 按建立時間倒序排列

	cursor, err := collection.Find(ctx, filter, opts)
	if err != nil {
		s.logger.Error().Err(err).Msg("查詢司機列表失敗")
		return nil, 0, err
	}
	defer func() {
		if err := cursor.Close(ctx); err != nil {
			s.logger.Error().Err(err).Msg("關閉游標失敗")
		}
	}()

	var drivers []*model.DriverInfo
	if err = cursor.All(ctx, &drivers); err != nil {
		s.logger.Error().Err(err).Msg("解析司機資料失敗")
		return nil, 0, err
	}

	return drivers, totalCount, nil
}

// ApproveDriver 審核司機（設定 is_approved 為 true）
func (s *DriverService) ApproveDriver(ctx context.Context, driverID string) (*model.DriverInfo, error) {
	objectID, err := primitive.ObjectIDFromHex(driverID)
	if err != nil {
		s.logger.Error().Str("driver_id", driverID).Err(err).Msg("司機ID格式錯誤")
		return nil, fmt.Errorf("司機ID格式錯誤: %w", err)
	}

	collection := s.mongoDB.GetCollection("drivers")

	// 更新司機審核狀態
	update := bson.M{
		"$set": bson.M{
			"is_approved": true,
			"is_active":   true,                   // 審核通過後啟用司機
			"status":      model.DriverStatusIdle, // 設定為閒置狀態，可以接單
			"updated_at":  time.Now(),
		},
	}

	filter := bson.M{"_id": objectID}

	result, err := collection.UpdateOne(ctx, filter, update)
	if err != nil {
		s.logger.Error().Str("driver_id", driverID).Err(err).Msg("更新司機審核狀態失敗")
		return nil, err
	}

	if result.MatchedCount == 0 {
		s.logger.Error().Str("driver_id", driverID).Msg("司機不存在")
		return nil, fmt.Errorf("司機不存在")
	}

	// 返回更新後的司機資料
	updatedDriver, err := s.GetDriverByID(ctx, driverID)
	if err != nil {
		s.logger.Error().Str("driver_id", driverID).Err(err).Msg("獲取更新後司機資料失敗")
		return nil, err
	}

	s.logger.Info().
		Str("driver_id", driverID).
		Str("司機姓名", updatedDriver.Name).
		Str("車隊", string(updatedDriver.Fleet)).
		Msg("司機審核成功")

	return updatedDriver, nil
}

// GetHistoryOrdersByDriverID 獲取司機歷史訂單（用於新的 driver-get-history-order endpoint）
func (s *DriverService) GetHistoryOrdersByDriverID(ctx context.Context, driverID string, pageNum, pageSize int) ([]*model.Order, int64, error) {
	orders, total, err := s.orderService.GetOrdersByDriverID(ctx, driverID, pageNum, pageSize)
	if err != nil {
		s.logger.Error().Str("driver_id", driverID).Int("page_num", pageNum).Int("page_size", pageSize).Err(err).Msg("獲取司機歷史訂單列表失敗")
		return nil, 0, err
	}
	// 不隱藏 assigned_driver，保持完整資料渲染
	return orders, total, nil
}

// CheckNotifyingOrder 檢查司機的通知中訂單
func (s *DriverService) CheckNotifyingOrder(ctx context.Context, driverID string) (*driverModels.CheckNotifyingOrderData, error) {

	// 從 Redis 查詢 notifying order
	notifyingOrderKey := fmt.Sprintf("notifying_order:%s", driverID)

	// 使用 EventManager 獲取緩存
	cachedData, err := s.eventManager.GetCache(ctx, notifyingOrderKey)
	if err != nil || cachedData == "" {
		// 沒有通知中訂單或查詢失敗
		//s.logger.Debug().Str("driver_id", driverID).Msg("沒有通知中訂單")
		return &driverModels.CheckNotifyingOrderData{
			HasNotifyingOrder: false,
		}, nil
	}

	// 反序列化 Redis 中的資料
	var redisNotifyingOrder driverModels.RedisNotifyingOrder
	if err := json.Unmarshal([]byte(cachedData), &redisNotifyingOrder); err != nil {
		s.logger.Error().Err(err).Str("driver_id", driverID).Msg("解析 notifying order 失敗")
		return nil, fmt.Errorf("解析通知中訂單資料失敗")
	}

	// 計算剩餘時間
	now := time.Now()
	pushTime := time.Unix(redisNotifyingOrder.PushTime, 0)
	elapsed := int(now.Sub(pushTime).Seconds())
	remainingSeconds := redisNotifyingOrder.TimeoutSeconds - elapsed

	// 如果已過期，返回無通知中訂單
	if remainingSeconds <= 0 {
		s.logger.Debug().
			Str("driver_id", driverID).
			Str("order_id", redisNotifyingOrder.OrderID).
			Int("elapsed", elapsed).
			Int("timeout", redisNotifyingOrder.TimeoutSeconds).
			Msg("通知中訂單已過期")

		return &driverModels.CheckNotifyingOrderData{
			HasNotifyingOrder: false,
		}, nil
	}

	// 構建回應資料
	notifyingOrder := &driverModels.NotifyingOrder{
		OrderID:          redisNotifyingOrder.OrderID,
		RemainingSeconds: remainingSeconds,
		OrderData:        redisNotifyingOrder.OrderData,
	}

	s.logger.Debug().
		Str("driver_id", driverID).
		Str("order_id", redisNotifyingOrder.OrderID).
		Int("remaining_seconds", remainingSeconds).
		Msgf("找到通知中訂單: %s", redisNotifyingOrder.OrderData.OriText)

	return &driverModels.CheckNotifyingOrderData{
		HasNotifyingOrder: true,
		NotifyingOrder:    notifyingOrder,
	}, nil
}

// CheckCancelingOrder 檢查司機的取消中訂單
func (s *DriverService) CheckCancelingOrder(ctx context.Context, driverID string) (*driverModels.CheckCancelingOrderData, error) {
	// 從 Redis 查詢 canceling order
	cancelingOrderKey := fmt.Sprintf("canceling_order:%s", driverID)

	// 使用 EventManager 獲取緩存
	cachedData, err := s.eventManager.GetCache(ctx, cancelingOrderKey)
	if err != nil || cachedData == "" {
		// 沒有取消中訂單或查詢失敗
		s.logger.Debug().Str("driver_id", driverID).Msg("沒有取消中訂單")
		return &driverModels.CheckCancelingOrderData{
			HasCancelingOrder: false,
		}, nil
	}

	// 反序列化 Redis 中的資料
	var redisCancelingOrder driverModels.RedisCancelingOrder
	if err := json.Unmarshal([]byte(cachedData), &redisCancelingOrder); err != nil {
		s.logger.Error().Err(err).Str("driver_id", driverID).Msg("解析 canceling order 失敗")
		return nil, fmt.Errorf("解析取消中訂單資料失敗")
	}

	// 計算剩餘時間
	now := time.Now()
	cancelTime := time.Unix(redisCancelingOrder.CancelTime, 0)
	elapsed := int(now.Sub(cancelTime).Seconds())
	remainingSeconds := redisCancelingOrder.TimeoutSeconds - elapsed

	// 如果已過期，返回無取消中訂單
	if remainingSeconds <= 0 {
		s.logger.Debug().
			Str("driver_id", driverID).
			Str("order_id", redisCancelingOrder.OrderID).
			Int("elapsed", elapsed).
			Int("timeout", redisCancelingOrder.TimeoutSeconds).
			Msg("取消中訂單已過期")

		return &driverModels.CheckCancelingOrderData{
			HasCancelingOrder: false,
		}, nil
	}

	// 構建回應資料
	cancelingOrder := &driverModels.CancelingOrder{
		OrderID:          redisCancelingOrder.OrderID,
		RemainingSeconds: remainingSeconds,
		OrderData:        redisCancelingOrder.OrderData,
	}

	s.logger.Debug().
		Str("driver_id", driverID).
		Str("order_id", redisCancelingOrder.OrderID).
		Int("remaining_seconds", remainingSeconds).
		Msg("找到取消中訂單")

	return &driverModels.CheckCancelingOrderData{
		HasCancelingOrder: true,
		CancelingOrder:    cancelingOrder,
	}, nil
}

// UpdateDriverAvatarPath 更新司機頭像路徑
func (s *DriverService) UpdateDriverAvatarPath(ctx context.Context, driverID, avatarPath string) error {
	objID, err := primitive.ObjectIDFromHex(driverID)
	if err != nil {
		return fmt.Errorf("無效的司機ID: %w", err)
	}

	collection := s.mongoDB.Database.Collection("drivers")
	filter := bson.M{"_id": objID}

	update := bson.M{
		"$set": bson.M{
			"avatar_path": avatarPath,
			"updated_at":  time.Now(),
		},
	}

	result, err := collection.UpdateOne(ctx, filter, update)
	if err != nil {
		s.logger.Error().
			Err(err).
			Str("driver_id", driverID).
			Str("avatar_path", avatarPath).
			Msg("更新司機頭像路徑失敗")
		return fmt.Errorf("更新司機頭像路徑失敗: %w", err)
	}

	if result.MatchedCount == 0 {
		return fmt.Errorf("司機不存在")
	}

	s.logger.Info().
		Str("driver_id", driverID).
		Str("avatar_path", avatarPath).
		Msg("成功更新司機頭像路徑")

	return nil
}

// GetDriverAvatarURL 將頭像相對路徑轉換為完整URL
func (s *DriverService) GetDriverAvatarURL(relativePath *string, baseURL string) string {
	if relativePath == nil || *relativePath == "" {
		return ""
	}
	return fmt.Sprintf("%s/%s", strings.TrimSuffix(baseURL, "/"), strings.TrimPrefix(*relativePath, "/"))
}

// UpdateDriverDeviceInfo 更新司機設備資訊
func (s *DriverService) UpdateDriverDeviceInfo(ctx context.Context, driverID, deviceModelName, deviceDeviceName, deviceBrand, deviceManufacturer, deviceAppVersion string) error {
	objectID, err := primitive.ObjectIDFromHex(driverID)
	if err != nil {
		s.logger.Error().Str("driver_id", driverID).Err(err).Msg("無效的司機ID格式")
		return fmt.Errorf("無效的司機ID格式: %w", err)
	}

	collection := s.mongoDB.GetCollection("drivers")
	filter := bson.M{"_id": objectID}

	updateFields := bson.M{
		"updated_at": time.Now(),
	}

	// 只更新非空的字段
	if deviceModelName != "" {
		updateFields["device_model_name"] = deviceModelName
	}
	if deviceDeviceName != "" {
		updateFields["device_device_name"] = deviceDeviceName
	}
	if deviceBrand != "" {
		updateFields["device_brand"] = deviceBrand
	}
	if deviceManufacturer != "" {
		updateFields["device_manufacturer"] = deviceManufacturer
	}
	if deviceAppVersion != "" {
		updateFields["device_app_version"] = deviceAppVersion
	}

	update := bson.M{"$set": updateFields}

	result, err := collection.UpdateOne(ctx, filter, update)
	if err != nil {
		s.logger.Error().
			Str("driver_id", driverID).
			Str("device_model", deviceModelName).
			Str("device_brand", deviceBrand).
			Str("app_version", deviceAppVersion).
			Err(err).
			Msg("更新司機設備資訊失敗")
		return fmt.Errorf("更新司機設備資訊失敗: %w", err)
	}

	if result.MatchedCount == 0 {
		return fmt.Errorf("司機不存在")
	}

	s.logger.Info().
		Str("driver_id", driverID).
		Str("device_model", deviceModelName).
		Str("device_brand", deviceBrand).
		Str("app_version", deviceAppVersion).
		Msg("成功更新司機設備資訊")

	return nil
}

// NotifyDriverArrival 統一處理司機抵達通知
func (s *DriverService) NotifyDriverArrival(ctx context.Context, orderID string, driver *model.DriverInfo, discordOnly bool) error {
	if s.notificationService == nil {
		s.logger.Warn().Str("order_id", orderID).Msg("NotificationService 未初始化，跳過司機抵達通知")
		return nil
	}

	// 使用統一通知服務處理司機抵達事件
	if err := s.notificationService.NotifyDriverArrived(ctx, orderID, driver, discordOnly); err != nil {
		s.logger.Error().
			Err(err).
			Str("order_id", orderID).
			Str("driver_id", driver.ID.Hex()).
			Bool("discord_only", discordOnly).
			Msg("統一司機抵達通知處理失敗")
		return err
	}

	notificationType := "完整通知"
	if discordOnly {
		notificationType = "僅Discord卡片更新"
	}

	s.logger.Info().
		Str("order_id", orderID).
		Str("driver_id", driver.ID.Hex()).
		Str("driver_name", driver.Name).
		Str("notification_type", notificationType).
		Msg("統一司機抵達通知處理成功")

	return nil
}

// NotifyDriverArrivedWithPhoto 司機抵達拍照證明場景通知
func (s *DriverService) NotifyDriverArrivedWithPhoto(ctx context.Context, orderID string, driver *model.DriverInfo) error {
	if s.notificationService == nil {
		s.logger.Warn().Str("order_id", orderID).Msg("NotificationService 未初始化，跳過司機抵達拍照通知")
		return nil
	}

	// 使用統一通知服務處理拍照場景
	if err := s.notificationService.NotifyDriverArrivedWithPhoto(ctx, orderID, driver); err != nil {
		s.logger.Error().
			Err(err).
			Str("order_id", orderID).
			Str("driver_id", driver.ID.Hex()).
			Msg("司機抵達拍照通知處理失敗")
		return err
	}

	s.logger.Info().
		Str("order_id", orderID).
		Str("driver_id", driver.ID.Hex()).
		Str("driver_name", driver.Name).
		Msg("司機抵達拍照通知處理成功")

	return nil
}

// NotifyDriverArrivedSkipPhoto 司機抵達略過拍照場景通知
func (s *DriverService) NotifyDriverArrivedSkipPhoto(ctx context.Context, orderID string, driver *model.DriverInfo) error {
	if s.notificationService == nil {
		s.logger.Warn().Str("order_id", orderID).Msg("NotificationService 未初始化，跳過司機抵達報告通知")
		return nil
	}

	// 使用統一通知服務處理報告場景
	if err := s.notificationService.NotifyArrivedSkipPhoto(ctx, orderID, driver); err != nil {
		s.logger.Error().
			Err(err).
			Str("order_id", orderID).
			Str("driver_id", driver.ID.Hex()).
			Msg("司機抵達報告通知處理失敗")
		return err
	}

	s.logger.Info().
		Str("order_id", orderID).
		Str("driver_id", driver.ID.Hex()).
		Str("driver_name", driver.Name).
		Msg("司機抵達報告通知處理成功")

	return nil
}

// ResetDriverStatusByName 根據司機名稱重置司機狀態為閒置
func (s *DriverService) ResetDriverStatusByName(ctx context.Context, driverName string, operatorName string) error {
	if driverName == "" {
		return fmt.Errorf("司機名稱不能為空")
	}

	s.logger.Info().
		Str("driver_name", driverName).
		Str("operator", operatorName).
		Msg("開始重置司機狀態")

	// 根據司機名稱查找司機
	collection := s.mongoDB.GetCollection("drivers")
	filter := bson.M{"name": driverName}

	var driver model.DriverInfo
	err := collection.FindOne(ctx, filter).Decode(&driver)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			s.logger.Warn().
				Str("driver_name", driverName).
				Msg("找不到指定的司機")
			return fmt.Errorf("找不到司機：%s", driverName)
		}
		s.logger.Error().
			Err(err).
			Str("driver_name", driverName).
			Msg("查詢司機失敗")
		return fmt.Errorf("查詢司機失敗：%w", err)
	}

	// 記錄重置前的狀態
	oldStatus := driver.Status
	s.logger.Info().
		Str("driver_id", driver.ID.Hex()).
		Str("driver_name", driverName).
		Str("old_status", string(oldStatus)).
		Str("operator", operatorName).
		Msg("找到司機，準備重置狀態")

	// 更新司機狀態為閒置
	updateFilter := bson.M{"_id": driver.ID}
	update := bson.M{
		"$set": bson.M{
			"status":          model.DriverStatusIdle,
			"updated_at":      time.Now(),
			"last_reset_time": time.Now(),
			"reset_operator":  operatorName,
		},
	}

	result, err := collection.UpdateOne(ctx, updateFilter, update)
	if err != nil {
		s.logger.Error().
			Err(err).
			Str("driver_id", driver.ID.Hex()).
			Str("driver_name", driverName).
			Msg("更新司機狀態失敗")
		return fmt.Errorf("更新司機狀態失敗：%w", err)
	}

	if result.MatchedCount == 0 {
		s.logger.Warn().
			Str("driver_id", driver.ID.Hex()).
			Str("driver_name", driverName).
			Msg("司機記錄未找到")
		return fmt.Errorf("司機記錄未找到")
	}

	// 記錄成功
	s.logger.Info().
		Str("driver_id", driver.ID.Hex()).
		Str("driver_name", driverName).
		Str("old_status", string(oldStatus)).
		Str("new_status", string(model.DriverStatusIdle)).
		Str("operator", operatorName).
		Msg("成功重置司機狀態")

	// 發布狀態變更事件（如果有 Redis 事件管理器）
	if s.eventManager != nil {
		resetEvent := map[string]interface{}{
			"driver_id":   driver.ID.Hex(),
			"driver_name": driverName,
			"old_status":  string(oldStatus),
			"new_status":  string(model.DriverStatusIdle),
			"operator":    operatorName,
			"reset_time":  time.Now(),
			"action_type": "admin_reset",
		}

		// 可以在這裡添加特定的事件發布邏輯
		s.logger.Info().
			Str("driver_id", driver.ID.Hex()).
			Interface("event", resetEvent).
			Msg("司機狀態重置事件已準備")
	}

	return nil
}

// ResetDriverWithScheduleClear 根據多種識別方式重置司機狀態並清除預約單資訊
func (s *DriverService) ResetDriverWithScheduleClear(ctx context.Context, driverIdentifier string, operatorName string) (*model.DriverInfo, error) {
	if driverIdentifier == "" {
		return nil, fmt.Errorf("司機識別資訊不能為空")
	}

	s.logger.Info().
		Str("driver_identifier", driverIdentifier).
		Str("operator", operatorName).
		Msg("開始重置司機狀態並清除預約單資訊")

	// 嘗試多種方式查找司機：名稱、account、driverNo
	collection := s.mongoDB.GetCollection("drivers")

	// 構建查詢條件 - 支援多種查詢方式
	filter := bson.M{
		"$or": []bson.M{
			{"name": driverIdentifier},      // 司機名稱
			{"account": driverIdentifier},   // 司機account
			{"driver_no": driverIdentifier}, // 司機編號
		},
	}

	var driver model.DriverInfo
	err := collection.FindOne(ctx, filter).Decode(&driver)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			s.logger.Warn().
				Str("driver_identifier", driverIdentifier).
				Msg("找不到指定的司機（已嘗試名稱、account、司機編號）")
			return nil, fmt.Errorf("找不到司機：%s（已嘗試名稱、account、司機編號）", driverIdentifier)
		}
		s.logger.Error().
			Err(err).
			Str("driver_identifier", driverIdentifier).
			Msg("查詢司機失敗")
		return nil, fmt.Errorf("查詢司機失敗：%w", err)
	}

	// 記錄重置前的狀態
	oldStatus := driver.Status
	oldHasSchedule := driver.HasSchedule

	s.logger.Info().
		Str("driver_id", driver.ID.Hex()).
		Str("driver_name", driver.Name).
		Str("driver_account", driver.Account).
		Str("driver_no", driver.DriverNo).
		Str("old_status", string(oldStatus)).
		Bool("old_has_schedule", oldHasSchedule).
		Str("operator", operatorName).
		Msg("找到司機，準備重置狀態並清除預約單資訊")

	// 更新司機狀態為閒置並清除預約單資訊
	updateFilter := bson.M{"_id": driver.ID}
	update := bson.M{
		"$set": bson.M{
			"status":          model.DriverStatusIdle,
			"updated_at":      time.Now(),
			"last_reset_time": time.Now(),
			"reset_operator":  operatorName,
		},
		"$unset": bson.M{
			"has_schedule":              "", // 清除預約單標記
			"scheduled_time":            "", // 清除預約時間
			"current_order_schedule_id": "", // 清除當前預約訂單ID
			"current_order_id":          "", // 清除當前即時訂單ID
		},
	}

	result, err := collection.UpdateOne(ctx, updateFilter, update)
	if err != nil {
		s.logger.Error().
			Err(err).
			Str("driver_id", driver.ID.Hex()).
			Str("driver_name", driver.Name).
			Msg("更新司機狀態失敗")
		return nil, fmt.Errorf("更新司機狀態失敗：%w", err)
	}

	if result.MatchedCount == 0 {
		s.logger.Warn().
			Str("driver_id", driver.ID.Hex()).
			Str("driver_name", driver.Name).
			Msg("司機記錄未找到")
		return nil, fmt.Errorf("司機記錄未找到")
	}

	// 記錄成功
	s.logger.Info().
		Str("driver_id", driver.ID.Hex()).
		Str("driver_name", driver.Name).
		Str("driver_account", driver.Account).
		Str("driver_no", driver.DriverNo).
		Str("old_status", string(oldStatus)).
		Str("new_status", string(model.DriverStatusIdle)).
		Bool("old_has_schedule", oldHasSchedule).
		Bool("new_has_schedule", false).
		Str("operator", operatorName).
		Msg("成功重置司機狀態並清除預約單資訊")

	// 發布狀態變更事件（如果有 Redis 事件管理器）
	if s.eventManager != nil {
		resetEvent := map[string]interface{}{
			"driver_id":        driver.ID.Hex(),
			"driver_name":      driver.Name,
			"driver_account":   driver.Account,
			"driver_no":        driver.DriverNo,
			"old_status":       string(oldStatus),
			"new_status":       string(model.DriverStatusIdle),
			"old_has_schedule": oldHasSchedule,
			"new_has_schedule": false,
			"operator":         operatorName,
			"reset_time":       time.Now(),
			"action_type":      "admin_reset_with_schedule_clear",
		}

		s.logger.Info().
			Str("driver_id", driver.ID.Hex()).
			Interface("event", resetEvent).
			Msg("司機狀態重置事件已準備")
	}

	// 更新driver對象並返回
	driver.Status = model.DriverStatusIdle
	driver.HasSchedule = false
	driver.ScheduledTime = nil
	driver.IsOnline = true
	driver.UpdatedAt = utils.NowUTC()

	return &driver, nil
}

// buildDriverObjectForScheduled 為預約訂單構建司機物件
func (s *DriverService) buildDriverObjectForScheduled(driver *model.DriverInfo) model.Driver {
	return model.Driver{
		AssignedDriver: driver.ID.Hex(),
		CarNo:          driver.CarPlate,
		CarColor:       driver.CarColor,
		Lat:            &driver.Lat,
		Lng:            &driver.Lng,
		LineUserID:     driver.LineUID,
		Name:           driver.Name,
		// 預約訂單不需要預估時間，因為是提前安排的
	}
}

// updateDriverScheduleInfo 更新司機的預約狀態信息
func (s *DriverService) updateDriverScheduleInfo(ctx context.Context, driverID string, orderID string, scheduledTime *time.Time) error {
	objectID, err := primitive.ObjectIDFromHex(driverID)
	if err != nil {
		s.logger.Error().Str("driver_id", driverID).Err(err).Msg("無效的司機ID格式")
		return fmt.Errorf("無效的司機ID格式: %w", err)
	}

	collection := s.mongoDB.GetCollection("drivers")
	filter := bson.M{"_id": objectID}

	update := bson.M{
		"$set": bson.M{
			"has_schedule":              true,
			"scheduled_time":            scheduledTime,
			"current_order_schedule_id": orderID,
			"updated_at":                time.Now(),
		},
	}

	result, err := collection.UpdateOne(ctx, filter, update)
	if err != nil {
		s.logger.Error().
			Str("driver_id", driverID).
			Err(err).
			Msg("更新司機預約狀態失敗")
		return fmt.Errorf("更新司機預約狀態失敗: %w", err)
	}

	if result.MatchedCount == 0 {
		s.logger.Warn().Str("driver_id", driverID).Msg("找不到對應的司機")
		return fmt.Errorf("找不到對應的司機")
	}

	s.logger.Info().
		Str("driver_id", driverID).
		Time("scheduled_time", *scheduledTime).
		Msg("司機預約狀態更新成功")

	return nil
}

// ResetDriverScheduledOrder 重置司機的預約訂單狀態（公開接口）
func (s *DriverService) ResetDriverScheduledOrder(ctx context.Context, driverID string) error {
	objectID, err := primitive.ObjectIDFromHex(driverID)
	if err != nil {
		s.logger.Error().Str("driver_id", driverID).Err(err).Msg("無效的司機ID格式")
		return fmt.Errorf("無效的司機ID格式: %w", err)
	}

	collection := s.mongoDB.GetCollection("drivers")
	filter := bson.M{"_id": objectID}

	update := bson.M{
		"$set": bson.M{
			"has_schedule":              false,
			"scheduled_time":            nil,
			"current_order_schedule_id": nil,
			"updated_at":                time.Now(),
		},
	}

	result, err := collection.UpdateOne(ctx, filter, update)
	if err != nil {
		s.logger.Error().
			Str("driver_id", driverID).
			Err(err).
			Msg("清除司機預約狀態失敗")
		return fmt.Errorf("清除司機預約狀態失敗: %w", err)
	}

	if result.MatchedCount == 0 {
		s.logger.Warn().Str("driver_id", driverID).Msg("找不到對應的司機")
		return fmt.Errorf("找不到對應的司機")
	}

	s.logger.Info().
		Str("driver_id", driverID).
		Msg("司機預約狀態清除成功")

	return nil
}

// GetAvailableScheduledOrdersCount 獲取司機可用的預約單數量
func (s *DriverService) GetAvailableScheduledOrdersCount(ctx context.Context, driverID string) (*driverModels.AvailableScheduledOrdersCountData, error) {
	s.logger.Info().
		Str("driver_id", driverID).
		Msg("開始獲取司機可用預約單數量")

	// 使用現有的 orderService 依賴
	if s.orderService == nil {
		s.logger.Error().
			Str("driver_id", driverID).
			Msg("OrderService 依賴未設置")
		return nil, fmt.Errorf("OrderService 依賴未設置")
	}

	totalCount, availableCount, err := s.orderService.GetAvailableScheduledOrdersCount(ctx, driverID)
	if err != nil {
		s.logger.Error().
			Err(err).
			Str("driver_id", driverID).
			Msg("獲取可用預約單數量失敗")
		return nil, fmt.Errorf("獲取可用預約單數量失敗：%w", err)
	}

	data := &driverModels.AvailableScheduledOrdersCountData{
		TotalCount:     int(totalCount),
		AvailableCount: int(availableCount),
	}

	s.logger.Info().
		Str("driver_id", driverID).
		Int("total_count", data.TotalCount).
		Int("available_count", data.AvailableCount).
		Msg("成功獲取司機可用預約單數量")

	return data, nil
}

// ArrivePickupLocation 處理司機抵達上車點的業務邏輯（僅記錄時間，不更新狀態）
func (s *DriverService) ArrivePickupLocation(ctx context.Context, driver *model.DriverInfo, orderID string, requestTime time.Time) (string, string, int, time.Time, error) {
	// 獲取訂單資料
	order, err := s.orderService.GetOrderByID(ctx, orderID)
	if err != nil {
		return "", "", 0, time.Time{}, fmt.Errorf("獲取訂單失敗: %w", err)
	}

	// 計算司機早到/遲到時間
	deviationSecs := s.CalcArrivalDeviation(order, requestTime)

	// 設置訂單的到達時間和偏差時間
	if err := s.orderService.SetArrivalTime(ctx, orderID, requestTime, deviationSecs); err != nil {
		return "", "", 0, time.Time{}, fmt.Errorf("設置到達時間失敗: %w", err)
	}

	// 非同步添加司機抵達日誌
	go func() {
		currentRounds := 1
		if order.Rounds != nil {
			currentRounds = *order.Rounds
		}
		if err := s.orderService.AddOrderLog(context.Background(), orderID, model.OrderLogActionDriverArrived,
			string(driver.Fleet), driver.Name, driver.CarPlate, driver.ID.Hex(), "司機已抵達上車點", currentRounds); err != nil {
			s.logger.Error().Err(err).Str("order_id", orderID).Msg("添加司機抵達日誌失敗")
		}
	}()

	// 返回當前狀態（不更新狀態）
	currentDriverStatus := string(driver.Status)
	currentOrderStatus := string(order.Status)

	// 計算實際偏差秒數，處理 nil 指針
	actualDeviationSecs := 0
	if deviationSecs != nil {
		actualDeviationSecs = *deviationSecs
	}

	s.logger.Info().
		Str("driver_name", driver.Name).
		Str("car_plate", driver.CarPlate).
		Str("order_id", orderID).
		Str("driver_status", currentDriverStatus).
		Str("order_status", currentOrderStatus).
		Int("deviation_secs", actualDeviationSecs).
		Msg("司機成功回報抵達時間記錄")

	return currentDriverStatus, currentOrderStatus, actualDeviationSecs, requestTime, nil
}

// CalcArrivalDeviation 計算司機抵達時間偏差
func (s *DriverService) CalcArrivalDeviation(order *model.Order, requestTime time.Time) *int {
	// 預約單的時間計算邏輯
	if order.Type == model.OrderTypeScheduled {
		if order.ScheduledAt == nil {
			return nil
		}
		// 預約單：以預約時間為基準，超過預約時間就是遲到
		deviation := int(requestTime.Sub(*order.ScheduledAt).Seconds())
		return &deviation
	}

	// 即時單的時間計算邏輯（原本邏輯）
	if order.AcceptanceTime == nil {
		return nil
	}

	// 計算預估到達時間（分鐘）
	estimatedMins := order.Driver.EstPickupMins
	if order.Driver.AdjustMins != nil {
		estimatedMins += *order.Driver.AdjustMins
	}

	// 計算偏差時間（秒）
	actualDuration := int(requestTime.Sub(*order.AcceptanceTime).Seconds())
	expectedDuration := estimatedMins * 60
	deviation := actualDuration - expectedDuration

	return &deviation
}

// GetOnlineDrivers 獲取所有在線司機列表
func (s *DriverService) GetOnlineDrivers(ctx context.Context) ([]*model.DriverInfo, error) {
	collection := s.mongoDB.Database.Collection("drivers")

	// 查詢條件：IsOnline = true 且 IsActive = true
	filter := bson.M{
		"is_online": true,
		"is_active": true,
	}

	// 排序：按車隊、名稱排序
	opts := options.Find().SetSort(bson.D{
		{Key: "fleet", Value: 1},
		{Key: "name", Value: 1},
	})

	cursor, err := collection.Find(ctx, filter, opts)
	if err != nil {
		s.logger.Error().Err(err).Msg("查詢在線司機失敗")
		return nil, fmt.Errorf("查詢在線司機失敗: %w", err)
	}
	defer cursor.Close(ctx)

	var drivers []*model.DriverInfo
	if err = cursor.All(ctx, &drivers); err != nil {
		s.logger.Error().Err(err).Msg("解析在線司機數據失敗")
		return nil, fmt.Errorf("解析在線司機數據失敗: %w", err)
	}

	s.logger.Info().Int("online_count", len(drivers)).Msg("成功獲取在線司機列表")
	return drivers, nil
}

// calculateArrivalDeviationWithNotification 計算司機抵達時間偏差並處理遲到通知
func (s *DriverService) calculateArrivalDeviationWithNotification(ctx context.Context, driver *model.DriverInfo, order *model.Order, orderID string, requestTime time.Time) {
	if order.AcceptanceTime == nil {
		return
	}

	// 計算預估到達時間（分鐘）
	estimatedMins := order.Driver.EstPickupMins
	if order.Driver.AdjustMins != nil {
		estimatedMins += *order.Driver.AdjustMins
	}

	// 計算偏差時間（秒）
	actualDuration := int(requestTime.Sub(*order.AcceptanceTime).Seconds())
	expectedDuration := estimatedMins * 60
	deviation := actualDuration - expectedDuration

	if deviation > 0 {
		s.logger.Warn().
			Str("driver_name", driver.Name).
			Str("car_plate", driver.CarPlate).
			Str("order_id", orderID).
			Int("late_secs", deviation).
			Int("estimated_mins", estimatedMins).
			Int("actual_duration_secs", actualDuration).
			Msg("司機遲到抵達上車點")

		// 如果遲到，發送Discord通知
		if err := s.orderService.CheckAndNotifyDriverLateness(ctx, orderID, driver.Name, driver.CarPlate); err != nil {
			s.logger.Error().Err(err).Str("order_id", orderID).Msg("發送司機遲到Discord通知失敗")
		}
	} else {
		s.logger.Info().
			Str("driver_name", driver.Name).
			Str("car_plate", driver.CarPlate).
			Str("order_id", orderID).
			Int("early_secs", -deviation).
			Int("estimated_mins", estimatedMins).
			Int("actual_duration_secs", actualDuration).
			Msg("司機提早抵達上車點")
	}
}

// HandlePickupCertificateUpload 處理司機上傳抵達證明的完整業務邏輯
func (s *DriverService) HandlePickupCertificateUpload(ctx context.Context, driver *model.DriverInfo, orderID string, file multipart.File, header *multipart.FileHeader, requestTime time.Time, fileStorageService *FileStorageService) (string, string, error) {
	// 1. 使用 FileStorageService 上傳文件
	uploadResult, err := fileStorageService.UploadPickupCertificateFile(ctx, file, header, orderID, driver.CarPlate)
	if err != nil {
		s.logger.Error().Err(err).Str("driver_id", driver.ID.Hex()).Str("order_id", orderID).Msg("檔案上傳失敗")
		return "", "", fmt.Errorf("檔案上傳失敗: %w", err)
	}

	s.logger.Info().Str("driver_name", driver.Name).Str("car_plate", driver.CarPlate).Str("order_id", orderID).Str("file_url", uploadResult.URL).Msg("司機已成功上傳抵達證明")

	// 2. 使用統一的司機抵達處理邏輯（包含照片URL）
	driverStatus, orderStatus, err := s.HandleDriverArrival(ctx, driver, orderID, uploadResult.URL)
	if err != nil {
		s.logger.Error().Err(err).Str("order_id", orderID).Msg("處理司機抵達報告失敗")

		// 如果業務邏輯失敗，嘗試清理已上傳的文件
		if cleanupErr := fileStorageService.DeleteFile(ctx, uploadResult.RelativePath); cleanupErr != nil {
			s.logger.Error().Err(cleanupErr).Str("file_path", uploadResult.RelativePath).Msg("清理上傳文件失敗")
		}

		return "", "", fmt.Errorf("處理司機抵達報告失敗: %w", err)
	}

	return driverStatus, orderStatus, nil
}

// HandleDriverArrival 處理司機抵達（統一處理有照片和無照片的場景）
func (s *DriverService) HandleDriverArrival(ctx context.Context, driver *model.DriverInfo, orderID string, certificateURL string) (string, string, error) {
	// 1. 獲取訂單（讀取已保存的偏差時間）
	order, err := s.orderService.GetOrderByID(ctx, orderID)
	if err != nil {
		return "", "", fmt.Errorf("獲取訂單失敗: %w", err)
	}

	// 2. 記錄抵達狀態（使用已保存的偏差時間）
	if order.Driver.ArrivalDeviationSecs != nil {
		deviationSecs := *order.Driver.ArrivalDeviationSecs
		if deviationSecs > 0 {
			s.logger.Info().
				Str("driver_name", driver.Name).
				Str("car_plate", driver.CarPlate).
				Str("order_id", orderID).
				Int("late_secs", deviationSecs).
				Msg("司機抵達報告（之前已記錄遲到）")
		} else {
			s.logger.Info().
				Str("driver_name", driver.Name).
				Str("car_plate", driver.CarPlate).
				Str("order_id", orderID).
				Int("early_secs", -deviationSecs).
				Msg("司機抵達報告（之前已記錄準時或早到）")
		}
	}

	// 3. 更新司機狀態為抵達
	if err := s.UpdateDriverStatusType(ctx, driver.ID.Hex(), model.DriverStatusArrived, string(model.DriverReasonDriverArrived), orderID); err != nil {
		s.logger.Error().Err(err).Str("driver_id", driver.ID.Hex()).Str("order_id", orderID).Msg("更新司機狀態失敗")
		return "", "", fmt.Errorf("更新司機狀態失敗: %w", err)
	}

	// 4. 更新訂單狀態（統一處理：狀態、證明照片URL、拍照狀態）
	updatedOrder, err := s.orderService.UpdateOrderStatusAndCertificate(ctx, orderID, model.OrderStatusDriverArrived, certificateURL, true)
	if err != nil {
		s.logger.Error().Err(err).Str("order_id", orderID).Msg("更新訂單狀態失敗")
		return "", "", fmt.Errorf("更新訂單狀態失敗: %w", err)
	}

	// 5. 非同步處理通知（根據是否有照片決定通知類型）
	hasPhoto := certificateURL != ""
	go func() {
		if err := s.HandleArrivalNotification(context.Background(), driver, orderID, hasPhoto); err != nil {
			s.logger.Error().Err(err).Str("order_id", orderID).Msg("處理司機抵達通知失敗")
		}
	}()

	// 6. 直接返回 enum 狀態值（避免額外的 DB 查詢）
	driverStatus := string(model.DriverStatusArrived) // 直接使用 enum 值
	orderStatus := string(updatedOrder.Status)        // 使用更新後的訂單狀態

	s.logger.Info().
		Str("driver_name", driver.Name).
		Str("car_plate", driver.CarPlate).
		Str("order_id", orderID).
		Str("driver_status", driverStatus).
		Str("order_status", orderStatus).
		Bool("has_photo", hasPhoto).
		Msg("司機成功回報抵達訂單")

	return driverStatus, orderStatus, nil
}

// HandleArrivalNotification 統一處理司機抵達通知邏輯
func (s *DriverService) HandleArrivalNotification(ctx context.Context, driver *model.DriverInfo, orderID string, withPhoto bool) error {
	if s.notificationService == nil {
		s.logger.Warn().Str("order_id", orderID).Msg("NotificationService 未初始化，跳過司機抵達通知")
		return nil
	}

	// 發送抵達通知（包含遲到邏輯）
	if withPhoto {
		// 拍照場景：Discord + LINE 卡片更新 + 遲到通知
		if err := s.notificationService.NotifyArrivedWithPhoto(ctx, orderID, driver); err != nil {
			return fmt.Errorf("司機抵達拍照通知失敗: %w", err)
		}
		s.logger.Info().Str("order_id", orderID).Msg("司機抵達拍照通知處理完成")
	} else {
		// 無照片場景：完整通知 + 遲到通知
		if err := s.notificationService.NotifyArrivedSkipPhoto(ctx, orderID, driver); err != nil {
			return fmt.Errorf("司機抵達報告通知失敗: %w", err)
		}
		s.logger.Info().Str("order_id", orderID).Msg("司機抵達報告通知處理完成")
	}

	return nil
}

// GetDriversByFleet 根據車隊獲取司機列表
func (s *DriverService) GetDriversByFleet(ctx context.Context, fleet model.FleetType) ([]*model.DriverInfo, error) {
	collection := s.mongoDB.GetCollection("drivers")

	// 查詢指定車隊的所有司機
	filter := bson.M{
		"fleet": fleet,
	}

	// 排序：按名稱排序
	opts := options.Find().SetSort(bson.D{
		{Key: "name", Value: 1},
	})

	cursor, err := collection.Find(ctx, filter, opts)
	if err != nil {
		s.logger.Error().Err(err).Str("fleet", string(fleet)).Msg("查詢車隊司機失敗")
		return nil, fmt.Errorf("查詢車隊司機失敗: %w", err)
	}
	defer cursor.Close(ctx)

	var drivers []*model.DriverInfo
	if err = cursor.All(ctx, &drivers); err != nil {
		s.logger.Error().Err(err).Str("fleet", string(fleet)).Msg("解析車隊司機數據失敗")
		return nil, fmt.Errorf("解析車隊司機數據失敗: %w", err)
	}

	s.logger.Info().Int("drivers_count", len(drivers)).Str("fleet", string(fleet)).Msg("成功獲取車隊司機列表")
	return drivers, nil
}
