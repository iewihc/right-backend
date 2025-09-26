package service

import (
	"context"
	"errors"
	"right-backend/data-models/common"
	"right-backend/data-models/user"
	"right-backend/infra"
	"right-backend/model"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/rs/zerolog"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type UserService struct {
	logger          zerolog.Logger
	mongoDB         *infra.MongoDB
	jwtSecretKey    string
	jwtExpiresHours int
}

func NewUserService(logger zerolog.Logger, mongoDB *infra.MongoDB, jwtSecretKey string, jwtExpiresHours int) *UserService {
	return &UserService{
		logger:          logger.With().Str("module", "user_service").Logger(),
		mongoDB:         mongoDB,
		jwtSecretKey:    jwtSecretKey,
		jwtExpiresHours: jwtExpiresHours,
	}
}

func (s *UserService) CreateUser(ctx context.Context, user *model.User) (*model.User, error) {
	user.ID = primitive.NewObjectID()
	user.CreatedAt = time.Now()
	user.UpdatedAt = time.Now()

	// 設置默認權限和車隊存取權限
	user.Permissions = model.GetDefaultPermissions(user.Role)
	user.FleetAccess = model.GetDefaultFleetAccess(user.Role)
	user.IsActive = true

	collection := s.mongoDB.GetCollection("users")
	_, err := collection.InsertOne(ctx, user)
	if err != nil {
		s.logger.Error().
			Str("用戶帳號", user.Account).
			Str("用戶角色", string(user.Role)).
			Str("錯誤原因", err.Error()).
			Msg("建立用戶失敗")
		return nil, err
	}

	s.logger.Info().
		Str("用戶編號", user.ID.Hex()).
		Str("用戶帳號", user.Account).
		Str("用戶角色", string(user.Role)).
		Msg("用戶建立成功")

	return user, nil
}

func (s *UserService) CreateUserWithValidation(ctx context.Context, createData *user.CreateUserInput, currentUserRole model.UserRole) (*user.UserWithoutPassword, error) {
	// 驗證是否有權限創建該角色的用戶
	if !model.CanCreateRole(currentUserRole, createData.Body.Role) {
		s.logger.Warn().
			Str("當前用戶角色", string(currentUserRole)).
			Str("嘗試創建角色", string(createData.Body.Role)).
			Msg("無權限創建該角色的用戶")
		return nil, errors.New("無權限創建該角色的用戶")
	}

	// 檢查帳號是否已存在
	existingUser, err := s.GetUserByAccount(ctx, createData.Body.Account)
	if err == nil && existingUser != nil {
		s.logger.Warn().
			Str("帳號", createData.Body.Account).
			Msg("帳號已存在")
		return nil, errors.New("帳號已存在")
	}

	// 創建用戶模型
	newUser := &model.User{
		Name:     createData.Body.Name,
		Account:  createData.Body.Account,
		Password: createData.Body.Password,
		Role:     createData.Body.Role,
		Fleet:    createData.Body.Fleet,
	}

	// 創建用戶
	createdUser, err := s.CreateUser(ctx, newUser)
	if err != nil {
		return nil, err
	}

	// 轉換為不包含密碼的回應模型
	userWithoutPassword := &user.UserWithoutPassword{
		ID:          createdUser.ID,
		Name:        createdUser.Name,
		Account:     createdUser.Account,
		Role:        createdUser.Role,
		Fleet:       createdUser.Fleet,
		Permissions: createdUser.Permissions,
		FleetAccess: createdUser.FleetAccess,
		IsActive:    createdUser.IsActive,
		CreatedAt:   createdUser.CreatedAt,
		UpdatedAt:   createdUser.UpdatedAt,
	}

	return userWithoutPassword, nil
}

func (s *UserService) RemoveUserRole(ctx context.Context, targetUserID string, currentUserRole model.UserRole) error {
	// 先獲取目標用戶資訊
	targetUser, err := s.GetUserByID(ctx, targetUserID)
	if err != nil {
		s.logger.Warn().
			Str("目標用戶ID", targetUserID).
			Str("錯誤原因", err.Error()).
			Msg("目標用戶不存在")
		return errors.New("目標用戶不存在")
	}

	// 檢查是否有權限移除該用戶的角色
	if !model.CanRemoveRole(currentUserRole, targetUser.Role) {
		s.logger.Warn().
			Str("當前用戶角色", string(currentUserRole)).
			Str("目標用戶角色", string(targetUser.Role)).
			Str("目標用戶ID", targetUserID).
			Msg("無權限移除該用戶的角色")
		return errors.New("無權限移除該用戶的角色")
	}

	// 防止自己移除自己的角色
	// 注意：這裡需要額外的邏輯來檢查當前用戶ID，但目前我們只有角色信息
	// 可以考慮在controller層面檢查

	// 將用戶角色設為"無"，清空權限，停用帳號
	objectID, err := primitive.ObjectIDFromHex(targetUserID)
	if err != nil {
		s.logger.Warn().
			Str("無效編號", targetUserID).
			Str("錯誤原因", err.Error()).
			Msg("用戶編號格式不正確")
		return err
	}

	collection := s.mongoDB.GetCollection("users")
	filter := bson.M{"_id": objectID}
	update := bson.M{"$set": bson.M{
		"role":        model.RoleNone,
		"permissions": []model.Permission{}, // 清空權限
		"is_active":   false,                // 停用帳號
		"updated_at":  time.Now(),
	}}

	result, err := collection.UpdateOne(ctx, filter, update)
	if err != nil {
		s.logger.Error().
			Str("目標用戶ID", targetUserID).
			Str("錯誤原因", err.Error()).
			Msg("移除用戶角色失敗")
		return err
	}

	if result.MatchedCount == 0 {
		s.logger.Warn().
			Str("目標用戶ID", targetUserID).
			Msg("找不到要更新的用戶")
		return errors.New("找不到要更新的用戶")
	}

	s.logger.Info().
		Str("目標用戶ID", targetUserID).
		Str("目標用戶帳號", targetUser.Account).
		Str("原角色", string(targetUser.Role)).
		Str("當前操作者角色", string(currentUserRole)).
		Msg("用戶角色移除成功")

	return nil
}

func (s *UserService) GetUserByID(ctx context.Context, id string) (*model.User, error) {
	objectID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		s.logger.Warn().
			Str("無效編號", id).
			Str("錯誤原因", err.Error()).
			Msg("用戶編號格式不正確")
		return nil, err
	}

	collection := s.mongoDB.GetCollection("users")
	var user model.User
	err = collection.FindOne(ctx, bson.M{"_id": objectID}).Decode(&user)
	if err != nil {
		s.logger.Warn().
			Str("用戶編號", id).
			Str("錯誤原因", err.Error()).
			Msg("用戶不存在")
		return nil, err
	}

	return &user, nil
}

func (s *UserService) GetUserByAccount(ctx context.Context, account string) (*model.User, error) {
	collection := s.mongoDB.GetCollection("users")
	var user model.User
	err := collection.FindOne(ctx, bson.M{"account": account}).Decode(&user)
	if err != nil {
		s.logger.Warn().
			Str("用戶帳號", account).
			Str("錯誤原因", err.Error()).
			Msg("用戶帳號不存在")
		return nil, err
	}

	return &user, nil
}

func (s *UserService) GetUsers(ctx context.Context, limit, skip int64) ([]*model.User, error) {
	collection := s.mongoDB.GetCollection("users")

	findOptions := options.Find()
	findOptions.SetLimit(limit)
	findOptions.SetSkip(skip)
	findOptions.SetSort(bson.D{primitive.E{Key: "created_at", Value: -1}}) // 按創建時間降序排列

	cursor, err := collection.Find(ctx, bson.M{}, findOptions)
	if err != nil {
		s.logger.Error().
			Int64("限制筆數", limit).
			Int64("跳過筆數", skip).
			Str("錯誤原因", err.Error()).
			Msg("查詢用戶列表失敗")
		return nil, err
	}
	defer cursor.Close(ctx)

	var users []*model.User
	for cursor.Next(ctx) {
		var user model.User
		if err := cursor.Decode(&user); err != nil {
			s.logger.Error().
				Str("錯誤原因", err.Error()).
				Msg("解析用戶資料失敗")
			return nil, err
		}
		users = append(users, &user)
	}

	return users, nil
}

// GetUsersWithPagination 獲取分頁用戶列表
func (s *UserService) GetUsersWithPagination(ctx context.Context, pageNum, pageSize int) ([]*model.User, *common.PaginationInfo, error) {
	collection := s.mongoDB.GetCollection("users")

	// 計算總數量
	totalItems, err := collection.CountDocuments(ctx, bson.M{})
	if err != nil {
		s.logger.Error().
			Str("錯誤原因", err.Error()).
			Msg("獲取用戶總數量失敗")
		return nil, nil, err
	}

	// 計算總頁數
	totalPages := int(totalItems) / pageSize
	if int(totalItems)%pageSize > 0 {
		totalPages++
	}

	// 計算跳過的文檔數量
	skip := int64((pageNum - 1) * pageSize)

	// 獲取分頁資料
	users, err := s.GetUsers(ctx, int64(pageSize), skip)
	if err != nil {
		return nil, nil, err
	}

	pagination := &common.PaginationInfo{
		CurrentPage: pageNum,
		PageSize:    pageSize,
		TotalItems:  totalItems,
		TotalPages:  totalPages,
	}

	return users, pagination, nil
}

// GetUsersWithFilterAndPagination 獲取帶篩選條件的分頁用戶列表（不包含密碼）
func (s *UserService) GetUsersWithFilterAndPagination(ctx context.Context, pageNum, pageSize int, fleet, search string) ([]*user.UserWithoutPassword, *common.PaginationInfo, error) {
	collection := s.mongoDB.GetCollection("users")

	// 建構查詢條件
	filter := bson.M{}

	// 車隊篩選
	if fleet != "" && fleet != "全部" {
		switch fleet {
		case "RSK":
			filter["fleet"] = model.FleetTypeRSK
		case "KD":
			filter["fleet"] = model.FleetTypeKD
		case "WEI":
			filter["fleet"] = model.FleetTypeWEI
		}
	}

	// 模糊搜尋（姓名或帳號）
	if search != "" {
		filter["$or"] = []bson.M{
			{"name": bson.M{"$regex": search, "$options": "i"}},
			{"account": bson.M{"$regex": search, "$options": "i"}},
		}
	}

	// 計算總數量
	totalItems, err := collection.CountDocuments(ctx, filter)
	if err != nil {
		s.logger.Error().
			Str("錯誤原因", err.Error()).
			Msg("獲取用戶總數量失敗")
		return nil, nil, err
	}

	// 計算總頁數
	totalPages := int(totalItems) / pageSize
	if int(totalItems)%pageSize > 0 {
		totalPages++
	}

	// 計算跳過的文檔數量
	skip := int64((pageNum - 1) * pageSize)

	// 獲取分頁資料
	findOptions := options.Find()
	findOptions.SetLimit(int64(pageSize))
	findOptions.SetSkip(skip)
	findOptions.SetSort(bson.D{primitive.E{Key: "created_at", Value: -1}}) // 按創建時間降序排列

	cursor, err := collection.Find(ctx, filter, findOptions)
	if err != nil {
		s.logger.Error().
			Int("頁碼", pageNum).
			Int("每頁數量", pageSize).
			Str("車隊篩選", fleet).
			Str("搜尋關鍵字", search).
			Str("錯誤原因", err.Error()).
			Msg("查詢用戶列表失敗")
		return nil, nil, err
	}
	defer cursor.Close(ctx)

	var users []*user.UserWithoutPassword
	for cursor.Next(ctx) {
		var u model.User
		if err := cursor.Decode(&u); err != nil {
			s.logger.Error().
				Str("錯誤原因", err.Error()).
				Msg("解析用戶資料失敗")
			return nil, nil, err
		}

		// 轉換為不包含密碼的模型
		userWithoutPassword := &user.UserWithoutPassword{
			ID:          u.ID,
			Name:        u.Name,
			Account:     u.Account,
			Role:        u.Role,
			Fleet:       u.Fleet,
			Permissions: u.Permissions,
			FleetAccess: u.FleetAccess,
			IsActive:    u.IsActive,
			CreatedAt:   u.CreatedAt,
			UpdatedAt:   u.UpdatedAt,
		}
		users = append(users, userWithoutPassword)
	}

	pagination := &common.PaginationInfo{
		CurrentPage: pageNum,
		PageSize:    pageSize,
		TotalItems:  totalItems,
		TotalPages:  totalPages,
	}

	s.logger.Info().
		Int("頁碼", pageNum).
		Int("每頁數量", pageSize).
		Str("車隊篩選", fleet).
		Str("搜尋關鍵字", search).
		Int64("總筆數", totalItems).
		Int("回傳筆數", len(users)).
		Msg("獲取用戶列表成功")

	return users, pagination, nil
}

func (s *UserService) UpdateUser(ctx context.Context, id string, updates bson.M) (*model.User, error) {
	objectID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		s.logger.Warn().
			Str("無效編號", id).
			Str("錯誤原因", err.Error()).
			Msg("用戶編號格式不正確")
		return nil, err
	}

	updates["updated_at"] = time.Now()

	collection := s.mongoDB.GetCollection("users")
	filter := bson.M{"_id": objectID}
	update := bson.M{"$set": updates}

	var updatedUser model.User
	err = collection.FindOneAndUpdate(ctx, filter, update).Decode(&updatedUser)
	if err != nil {
		s.logger.Error().
			Str("用戶編號", id).
			Str("錯誤原因", err.Error()).
			Msg("更新用戶資料失敗")
		return nil, err
	}

	s.logger.Info().
		Str("用戶編號", updatedUser.ID.Hex()).
		Str("用戶帳號", updatedUser.Account).
		Msg("用戶資料更新成功")

	return &updatedUser, nil
}

func (s *UserService) Login(ctx context.Context, account, password string) (*model.User, string, error) {
	collection := s.mongoDB.GetCollection("users")
	var user model.User
	err := collection.FindOne(ctx, bson.M{
		"account":   account,
		"password":  password,
		"is_active": true,
	}).Decode(&user)
	if err != nil {
		s.logger.Warn().
			Str("用戶帳號", account).
			Str("錯誤原因", err.Error()).
			Msg("用戶登入失敗 - 帳號或密碼錯誤或帳號未啟用")
		return nil, "", err
	}

	// 生成 JWT token，包含完整的用戶資訊以減少資料庫查詢
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id": user.ID.Hex(),
		"account": user.Account,
		"role":    user.Role,
		"fleet":   string(user.Fleet),
		"type":    string(model.TokenTypeUser),
		"exp":     time.Now().Add(time.Hour * time.Duration(s.jwtExpiresHours)).Unix(),
	})

	tokenString, err := token.SignedString([]byte(s.jwtSecretKey)) // 使用配置的密鑰
	if err != nil {
		s.logger.Error().
			Str("用戶編號", user.ID.Hex()).
			Str("用戶帳號", user.Account).
			Str("用戶角色", string(user.Role)).
			Str("錯誤原因", err.Error()).
			Msg("用戶 JWT 令牌生成失敗")
		return nil, "", err
	}

	s.logger.Debug().
		Str("用戶編號", user.ID.Hex()).
		Str("用戶帳號", user.Account).
		Str("用戶角色", string(user.Role)).
		Msg("用戶登入成功 - 服務層驗證完成")

	return &user, tokenString, nil
}

func (s *UserService) ChangePassword(ctx context.Context, userID, oldPassword, newPassword string) error {
	objectID, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		s.logger.Warn().
			Str("無效編號", userID).
			Str("錯誤原因", err.Error()).
			Msg("用戶編號格式不正確")
		return err
	}

	collection := s.mongoDB.GetCollection("users")

	// 先驗證舊密碼是否正確
	var user model.User
	err = collection.FindOne(ctx, bson.M{
		"_id":      objectID,
		"password": oldPassword,
	}).Decode(&user)
	if err != nil {
		s.logger.Warn().
			Str("用戶編號", userID).
			Str("錯誤原因", err.Error()).
			Msg("舊密碼驗證失敗")
		return err
	}

	// 更新密碼
	filter := bson.M{"_id": objectID}
	update := bson.M{"$set": bson.M{
		"password":   newPassword,
		"updated_at": time.Now(),
	}}

	_, err = collection.UpdateOne(ctx, filter, update)
	if err != nil {
		s.logger.Error().
			Str("用戶編號", userID).
			Str("錯誤原因", err.Error()).
			Msg("更新密碼失敗")
		return err
	}

	s.logger.Info().
		Str("用戶編號", userID).
		Str("用戶帳號", user.Account).
		Msg("用戶密碼修改成功")

	return nil
}
