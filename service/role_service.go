package service

import (
	"context"
	"errors"
	"right-backend/data-models/common"
	"right-backend/data-models/role"
	"right-backend/infra"
	"right-backend/model"
	"time"

	"github.com/rs/zerolog"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type RoleService struct {
	logger  zerolog.Logger
	mongoDB *infra.MongoDB
}

func NewRoleService(logger zerolog.Logger, mongoDB *infra.MongoDB) *RoleService {
	return &RoleService{
		logger:  logger.With().Str("module", "role_service").Logger(),
		mongoDB: mongoDB,
	}
}

// CreateRole 創建角色
func (s *RoleService) CreateRole(ctx context.Context, input *role.CreateRoleInput) (*model.Role, error) {
	// 檢查角色名稱是否已存在
	existingRole, err := s.GetRoleByName(ctx, string(model.UserRole(input.Body.Name)))
	if err == nil && existingRole != nil {
		s.logger.Warn().
			Str("角色名稱", input.Body.Name).
			Msg("角色名稱已存在")
		return nil, errors.New("角色名稱已存在")
	}

	newRole := &model.Role{
		ID:          primitive.NewObjectID(),
		Name:        model.UserRole(input.Body.Name),
		TagColor:    input.Body.TagColor,
		FleetAccess: input.Body.FleetAccess,
		Permissions: input.Body.Permissions,
		IsSystem:    false, // 用戶創建的角色都不是系統角色
		IsActive:    true,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	collection := s.mongoDB.GetCollection("roles")
	_, err = collection.InsertOne(ctx, newRole)
	if err != nil {
		s.logger.Error().
			Str("角色名稱", input.Body.Name).
			Str("錯誤原因", err.Error()).
			Msg("創建角色失敗")
		return nil, err
	}

	s.logger.Info().
		Str("角色ID", newRole.ID.Hex()).
		Str("角色名稱", input.Body.Name).
		Msg("角色創建成功")

	return newRole, nil
}

// GetRoleByID 根據ID獲取角色
func (s *RoleService) GetRoleByID(ctx context.Context, id string) (*model.Role, error) {
	objectID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		s.logger.Warn().
			Str("無效ID", id).
			Str("錯誤原因", err.Error()).
			Msg("角色ID格式不正確")
		return nil, err
	}

	collection := s.mongoDB.GetCollection("roles")
	var role model.Role
	err = collection.FindOne(ctx, bson.M{"_id": objectID}).Decode(&role)
	if err != nil {
		s.logger.Warn().
			Str("角色ID", id).
			Str("錯誤原因", err.Error()).
			Msg("角色不存在")
		return nil, err
	}

	return &role, nil
}

// GetRoleByName 根據名稱獲取角色
func (s *RoleService) GetRoleByName(ctx context.Context, name string) (*model.Role, error) {
	collection := s.mongoDB.GetCollection("roles")
	var role model.Role
	err := collection.FindOne(ctx, bson.M{"name": name}).Decode(&role)
	if err != nil {
		s.logger.Debug().
			Str("角色名稱", name).
			Str("錯誤原因", err.Error()).
			Msg("角色不存在")
		return nil, err
	}

	return &role, nil
}

// GetRolesWithPagination 獲取分頁角色列表
func (s *RoleService) GetRolesWithPagination(ctx context.Context, pageNum, pageSize int, includeSystem bool, search string, fleet string) ([]*model.Role, *common.PaginationInfo, error) {
	collection := s.mongoDB.GetCollection("roles")

	// 建構查詢條件
	filter := bson.M{"is_active": true}

	// 是否排除系統角色（預設包含系統角色）
	// 只有明確傳入 include_system=false 時才排除系統角色
	// 注意：這裡的邏輯可能需要根據業務需求調整

	// 模糊搜尋角色名稱
	if search != "" {
		filter["name"] = bson.M{"$regex": search, "$options": "i"}
	}

	// 根據車隊過濾角色
	if fleet != "" {
		// 當指定車隊時，返回以下角色：
		// 1. fleet_access 為 "全部" 的角色（適用於所有車隊）
		// 2. fleet 欄位匹配指定車隊的角色（專屬角色）
		// 3. fleet 欄位不存在或為 null 的角色（通用角色）
		filter["$or"] = []bson.M{
			{"fleet_access": "全部"},              // 適用於所有車隊的角色
			{"fleet": fleet},                    // 專屬於指定車隊的角色
			{"fleet": bson.M{"$exists": false}}, // fleet 欄位不存在
			{"fleet": nil},                      // fleet 欄位為 null
		}
	}

	// 計算總數量
	totalItems, err := collection.CountDocuments(ctx, filter)
	if err != nil {
		s.logger.Error().
			Str("錯誤原因", err.Error()).
			Msg("獲取角色總數量失敗")
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
			Bool("包含系統角色", includeSystem).
			Str("搜尋關鍵字", search).
			Str("錯誤原因", err.Error()).
			Msg("查詢角色列表失敗")
		return nil, nil, err
	}
	defer cursor.Close(ctx)

	var roles []*model.Role
	for cursor.Next(ctx) {
		var r model.Role
		if err := cursor.Decode(&r); err != nil {
			s.logger.Error().
				Str("錯誤原因", err.Error()).
				Msg("解析角色資料失敗")
			return nil, nil, err
		}
		roles = append(roles, &r)
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
		Bool("包含系統角色", includeSystem).
		Str("搜尋關鍵字", search).
		Int64("總筆數", totalItems).
		Int("回傳筆數", len(roles)).
		Msg("獲取角色列表成功")

	return roles, pagination, nil
}

// UpdateRole 更新角色
func (s *RoleService) UpdateRole(ctx context.Context, input *role.UpdateRoleInput) (*model.Role, error) {
	objectID, err := primitive.ObjectIDFromHex(input.ID)
	if err != nil {
		s.logger.Warn().
			Str("無效ID", input.ID).
			Str("錯誤原因", err.Error()).
			Msg("角色ID格式不正確")
		return nil, err
	}

	// 檢查角色是否存在
	existingRole, err := s.GetRoleByID(ctx, input.ID)
	if err != nil {
		return nil, errors.New("角色不存在")
	}

	// 檢查是否為系統角色
	if existingRole.IsSystem {
		s.logger.Warn().
			Str("角色ID", input.ID).
			Str("角色名稱", string(existingRole.Name)).
			Msg("嘗試更新系統角色")
		return nil, errors.New("不能修改系統角色")
	}

	// 建構更新資料
	updates := bson.M{"updated_at": time.Now()}

	if input.Body.Name != "" {
		// 檢查新名稱是否已被使用
		if string(existingRole.Name) != input.Body.Name {
			conflictRole, err := s.GetRoleByName(ctx, input.Body.Name)
			if err == nil && conflictRole != nil {
				return nil, errors.New("角色名稱已存在")
			}
		}
		updates["name"] = input.Body.Name
	}

	if input.Body.TagColor != "" {
		updates["tag_color"] = input.Body.TagColor
	}

	if input.Body.FleetAccess != "" {
		updates["fleet_access"] = input.Body.FleetAccess
	}

	if input.Body.Permissions != nil {
		updates["permissions"] = input.Body.Permissions
	}

	collection := s.mongoDB.GetCollection("roles")
	filter := bson.M{"_id": objectID}
	update := bson.M{"$set": updates}

	var updatedRole model.Role
	err = collection.FindOneAndUpdate(ctx, filter, update, options.FindOneAndUpdate().SetReturnDocument(options.After)).Decode(&updatedRole)
	if err != nil {
		s.logger.Error().
			Str("角色ID", input.ID).
			Str("錯誤原因", err.Error()).
			Msg("更新角色失敗")
		return nil, err
	}

	s.logger.Info().
		Str("角色ID", updatedRole.ID.Hex()).
		Str("角色名稱", string(updatedRole.Name)).
		Msg("角色更新成功")

	return &updatedRole, nil
}

// DeleteRole 刪除角色
func (s *RoleService) DeleteRole(ctx context.Context, roleID string) error {
	objectID, err := primitive.ObjectIDFromHex(roleID)
	if err != nil {
		s.logger.Warn().
			Str("無效ID", roleID).
			Str("錯誤原因", err.Error()).
			Msg("角色ID格式不正確")
		return err
	}

	// 檢查角色是否存在
	existingRole, err := s.GetRoleByID(ctx, roleID)
	if err != nil {
		return errors.New("角色不存在")
	}

	// 檢查是否為系統角色
	if existingRole.IsSystem {
		s.logger.Warn().
			Str("角色ID", roleID).
			Str("角色名稱", string(existingRole.Name)).
			Msg("嘗試刪除系統角色")
		return errors.New("不能刪除系統角色")
	}

	// 檢查是否有用戶使用該角色
	userCollection := s.mongoDB.GetCollection("users")
	userCount, err := userCollection.CountDocuments(ctx, bson.M{"role": existingRole.Name})
	if err != nil {
		s.logger.Error().
			Str("角色ID", roleID).
			Str("錯誤原因", err.Error()).
			Msg("檢查角色使用情況失敗")
		return err
	}

	if userCount > 0 {
		s.logger.Warn().
			Str("角色ID", roleID).
			Str("角色名稱", string(existingRole.Name)).
			Int64("使用人數", userCount).
			Msg("角色仍有用戶使用，無法刪除")
		return errors.New("該角色仍有用戶使用，無法刪除")
	}

	// 軟刪除角色
	collection := s.mongoDB.GetCollection("roles")
	filter := bson.M{"_id": objectID}
	update := bson.M{"$set": bson.M{
		"is_active":  false,
		"updated_at": time.Now(),
	}}

	result, err := collection.UpdateOne(ctx, filter, update)
	if err != nil {
		s.logger.Error().
			Str("角色ID", roleID).
			Str("錯誤原因", err.Error()).
			Msg("刪除角色失敗")
		return err
	}

	if result.MatchedCount == 0 {
		return errors.New("找不到要刪除的角色")
	}

	s.logger.Info().
		Str("角色ID", roleID).
		Str("角色名稱", string(existingRole.Name)).
		Msg("角色刪除成功")

	return nil
}

// InitializeSystemRoles 初始化系統角色 (使用 upsert)
func (s *RoleService) InitializeSystemRoles(ctx context.Context) error {
	collection := s.mongoDB.GetCollection("roles")

	systemRoles := model.GetSystemRoles()
	for _, roleName := range systemRoles {
		systemRole := model.CreateSystemRole(roleName)

		// 使用 upsert 方式：如果角色名稱存在則更新，不存在則創建
		filter := bson.M{"name": roleName}
		update := bson.M{
			"$set": bson.M{
				"name":         systemRole.Name,
				"tag_color":    systemRole.TagColor,
				"fleet_access": systemRole.FleetAccess,
				"permissions":  systemRole.Permissions,
				"is_system":    systemRole.IsSystem,
				"is_active":    systemRole.IsActive,
				"updated_at":   time.Now(),
			},
			"$setOnInsert": bson.M{
				"_id":        systemRole.ID,
				"created_at": systemRole.CreatedAt,
			},
		}

		opts := options.Update().SetUpsert(true)
		result, err := collection.UpdateOne(ctx, filter, update, opts)
		if err != nil {
			s.logger.Error().
				Str("角色名稱", string(roleName)).
				Str("錯誤原因", err.Error()).
				Msg("初始化系統角色失敗")
			return err
		}

		if result.UpsertedCount > 0 {
			s.logger.Info().
				Str("角色名稱", string(roleName)).
				Str("角色ID", systemRole.ID.Hex()).
				Msg("系統角色創建成功")
		} else if result.ModifiedCount > 0 {
			s.logger.Info().
				Str("角色名稱", string(roleName)).
				Msg("系統角色更新成功")
		} else {
			s.logger.Debug().
				Str("角色名稱", string(roleName)).
				Msg("系統角色無變更")
		}
	}

	s.logger.Info().
		Int("角色數量", len(systemRoles)).
		Msg("系統角色初始化完成")

	return nil
}
