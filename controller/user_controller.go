package controller

import (
	"context"
	"right-backend/auth"
	"right-backend/data-models/user"
	"right-backend/middleware"
	"right-backend/model"
	"right-backend/service"

	"github.com/danielgtaylor/huma/v2"
	"github.com/rs/zerolog"
	"go.mongodb.org/mongo-driver/bson"
)

type UserController struct {
	logger         zerolog.Logger
	userService    *service.UserService
	orderService   *service.OrderService
	authMiddleware *middleware.UserAuthMiddleware
}

func NewUserController(logger zerolog.Logger, userService *service.UserService, orderService *service.OrderService, authMiddleware *middleware.UserAuthMiddleware) *UserController {
	return &UserController{
		logger:         logger.With().Str("module", "user_controller").Logger(),
		userService:    userService,
		orderService:   orderService,
		authMiddleware: authMiddleware,
	}
}

func (c *UserController) RegisterRoutes(api huma.API) {
	// 創建用戶
	huma.Register(api, huma.Operation{
		OperationID: "create-user",
		Method:      "POST",
		Path:        "/users",
		Summary:     "創建新用戶",
		Description: "創建新用戶，需要認證，根據當前用戶角色決定可以創建的用戶角色",
		Tags:        []string{"users"},
		Middlewares: huma.Middlewares{c.authMiddleware.Auth()},
		Security: []map[string][]string{
			{"bearerAuth": {}},
		},
	}, func(ctx context.Context, input *user.CreateUserInput) (*user.CreateUserResponse, error) {
		// 從 JWT token 中獲取當前用戶資訊
		userFromToken, err := auth.GetUserFromContext(ctx)
		if err != nil {
			c.logger.Error().Err(err).Msg("無法從token中獲取用戶資訊")
			return nil, huma.Error500InternalServerError("無法從token中獲取用戶資訊")
		}

		c.logger.Info().
			Str("當前用戶ID", userFromToken.ID.Hex()).
			Str("當前用戶帳號", userFromToken.Account).
			Str("當前用戶角色", string(userFromToken.Role)).
			Str("嘗試創建帳號", input.Body.Account).
			Str("嘗試創建角色", string(input.Body.Role)).
			Msg("用戶嘗試創建新用戶")

		// 使用帶驗證的創建方法
		createdUser, err := c.userService.CreateUserWithValidation(ctx, input, userFromToken.Role)
		if err != nil {
			c.logger.Error().
				Str("當前用戶角色", string(userFromToken.Role)).
				Str("嘗試創建角色", string(input.Body.Role)).
				Str("嘗試創建帳號", input.Body.Account).
				Err(err).
				Msg("建立用戶失敗")
			return nil, huma.Error400BadRequest("建立用戶失敗: " + err.Error())
		}

		c.logger.Info().
			Str("創建者ID", userFromToken.ID.Hex()).
			Str("創建者帳號", userFromToken.Account).
			Str("新用戶ID", createdUser.ID.Hex()).
			Str("新用戶帳號", createdUser.Account).
			Str("新用戶角色", string(createdUser.Role)).
			Msg("用戶創建成功")

		return &user.CreateUserResponse{Body: createdUser}, nil
	})

	// 獲取所有用戶（分頁）
	huma.Register(api, huma.Operation{
		OperationID: "get-users",
		Method:      "GET",
		Path:        "/users",
		Summary:     "獲取管理員列表（分頁）",
		Description: "獲取分頁的用戶列表，支援頁碼、每頁數量、車隊篩選和模糊搜尋參數，不返回密碼",
		Tags:        []string{"users"},
	}, func(ctx context.Context, input *user.GetUsersInput) (*user.PaginatedUsersResponse, error) {
		users, pagination, err := c.userService.GetUsersWithFilterAndPagination(ctx, input.GetPageNum(), input.GetPageSize(), input.Fleet, input.Search)
		if err != nil {
			c.logger.Error().
				Str("車隊篩選", input.Fleet).
				Str("搜尋關鍵字", input.Search).
				Err(err).
				Msg("獲取用戶列表失敗")
			return nil, huma.Error500InternalServerError("獲取用戶列表失敗", err)
		}

		response := &user.PaginatedUsersResponse{}
		response.Body.Users = users
		response.Body.Pagination = *pagination

		c.logger.Info().
			Int("頁碼", input.GetPageNum()).
			Int("每頁數量", input.GetPageSize()).
			Str("車隊篩選", input.Fleet).
			Str("搜尋關鍵字", input.Search).
			Int64("總筆數", pagination.TotalItems).
			Int("回傳筆數", len(users)).
			Msg("成功獲取用戶列表")

		return response, nil
	})

	// 移除用戶角色（軟刪除）
	huma.Register(api, huma.Operation{
		OperationID: "remove-user",
		Method:      "DELETE",
		Path:        "/users/{id}",
		Summary:     "移除用戶角色",
		Description: "移除指定用戶的角色，將角色設為'無'並停用帳號，需要認證，根據角色層級決定可移除的用戶",
		Tags:        []string{"users"},
		Middlewares: huma.Middlewares{c.authMiddleware.Auth()},
		Security: []map[string][]string{
			{"bearerAuth": {}},
		},
	}, func(ctx context.Context, input *user.RemoveUserInput) (*user.RemoveUserResponse, error) {
		// 從 JWT token 中獲取當前用戶資訊
		userFromToken, err := auth.GetUserFromContext(ctx)
		if err != nil {
			c.logger.Error().Err(err).Msg("無法從token中獲取用戶資訊")
			return nil, huma.Error500InternalServerError("無法從token中獲取用戶資訊")
		}

		// 防止用戶移除自己的角色
		if userFromToken.ID.Hex() == input.ID {
			c.logger.Warn().
				Str("用戶ID", userFromToken.ID.Hex()).
				Str("用戶帳號", userFromToken.Account).
				Msg("用戶嘗試移除自己的角色")
			return nil, huma.Error400BadRequest("不能移除自己的角色")
		}

		c.logger.Info().
			Str("當前用戶ID", userFromToken.ID.Hex()).
			Str("當前用戶帳號", userFromToken.Account).
			Str("當前用戶角色", string(userFromToken.Role)).
			Str("目標用戶ID", input.ID).
			Msg("用戶嘗試移除用戶角色")

		// 執行角色移除
		err = c.userService.RemoveUserRole(ctx, input.ID, userFromToken.Role)
		if err != nil {
			c.logger.Error().
				Str("當前用戶角色", string(userFromToken.Role)).
				Str("目標用戶ID", input.ID).
				Err(err).
				Msg("移除用戶角色失敗")
			return nil, huma.Error400BadRequest("移除用戶角色失敗: " + err.Error())
		}

		c.logger.Info().
			Str("操作者ID", userFromToken.ID.Hex()).
			Str("操作者帳號", userFromToken.Account).
			Str("目標用戶ID", input.ID).
			Msg("用戶角色移除成功")

		response := &user.RemoveUserResponse{}
		response.Body.Message = "用戶角色已成功移除"
		response.Body.UserID = input.ID

		return response, nil
	})

	// 根據ID獲取用戶
	huma.Register(api, huma.Operation{
		OperationID: "get-user-by-id",
		Method:      "GET",
		Path:        "/users/{id}",
		Summary:     "根據ID獲取用戶",
		Tags:        []string{"users"},
	}, func(ctx context.Context, input *user.UserIDInput) (*user.UserResponse, error) {
		u, err := c.userService.GetUserByID(ctx, input.ID)
		if err != nil {
			c.logger.Error().Err(err).Str("user_id", input.ID).Msg("用戶不存在")
			return nil, huma.Error404NotFound("用戶不存在", err)
		}

		return &user.UserResponse{Body: u}, nil
	})

	// 修改用戶密碼
	huma.Register(api, huma.Operation{
		OperationID: "change-user-password",
		Method:      "PUT",
		Path:        "/users/{id}/password",
		Summary:     "修改用戶密碼",
		Description: "修改指定用戶的密碼，需要提供舊密碼和新密碼",
		Tags:        []string{"users"},
	}, func(ctx context.Context, input *user.ChangePasswordInput) (*user.ChangePasswordResponse, error) {
		err := c.userService.ChangePassword(ctx, input.ID, input.Body.OldPassword, input.Body.NewPassword)
		if err != nil {
			c.logger.Error().
				Str("user_id", input.ID).
				Err(err).
				Msg("修改密碼失敗")
			return nil, huma.Error400BadRequest("修改密碼失敗，請檢查舊密碼是否正確", err)
		}

		response := &user.ChangePasswordResponse{}
		response.Body.Message = "密碼修改成功"
		response.Body.NewPassword = input.Body.NewPassword

		c.logger.Info().
			Str("user_id", input.ID).
			Msg("用戶密碼修改成功")

		return response, nil
	})

	// 獲取當前用戶資料（需要認證）
	huma.Register(api, huma.Operation{
		OperationID: "get-current-user-profile",
		Method:      "GET",
		Path:        "/users/me",
		Summary:     "獲取當前登入用戶的個人資料",
		Description: "根據JWT Bearer token獲取當前登入用戶的完整資料。",
		Tags:        []string{"users"},
		Middlewares: huma.Middlewares{c.authMiddleware.Auth()},
		Security: []map[string][]string{
			{"bearerAuth": {}},
		},
	}, func(ctx context.Context, input *struct{}) (*user.UserResponse, error) {
		userFromToken, err := auth.GetUserFromContext(ctx)
		if err != nil {
			c.logger.Error().Err(err).Msg("無法從token中獲取用戶資訊")
			return nil, huma.Error500InternalServerError("無法從token中獲取用戶資訊")
		}

		userID := userFromToken.ID.Hex()
		c.logger.Info().Str("user_id", userID).Str("user_account", userFromToken.Account).Msg("用戶獲取個人資料")

		// 從資料庫獲取最新的用戶資料
		u, err := c.userService.GetUserByID(ctx, userID)
		if err != nil {
			c.logger.Warn().Err(err).Str("user_id", userID).Msg("獲取當前用戶資料失敗")
			return nil, huma.Error404NotFound("用戶不存在", err)
		}

		return &user.UserResponse{Body: u}, nil
	})

	// 更新當前用戶資料（需要認證）
	huma.Register(api, huma.Operation{
		OperationID: "update-current-user-profile",
		Method:      "PUT",
		Path:        "/users/me/profile",
		Summary:     "更新當前用戶的個人資料",
		Description: "更新當前登入用戶的個人資料，如姓名等。用戶ID會從JWT Bearer token中自動獲取。",
		Tags:        []string{"users"},
		Middlewares: huma.Middlewares{c.authMiddleware.Auth()},
		Security: []map[string][]string{
			{"bearerAuth": {}},
		},
	}, func(ctx context.Context, input *user.UpdateUserProfileInput) (*user.UserResponse, error) {
		userFromToken, err := auth.GetUserFromContext(ctx)
		if err != nil {
			c.logger.Error().Err(err).Msg("無法從token中獲取用戶資訊")
			return nil, huma.Error500InternalServerError("無法從token中獲取用戶資訊")
		}

		userID := userFromToken.ID.Hex()
		c.logger.Info().Str("user_id", userID).Str("user_account", userFromToken.Account).Msg("用戶正在更新個人資料")

		// 構建更新資料
		updates := bson.M{}
		hasUpdates := false

		if input.Body.Name != "" {
			updates["name"] = input.Body.Name
			hasUpdates = true
		}
		if input.Body.Account != "" {
			updates["account"] = input.Body.Account
			hasUpdates = true
		}
		if input.Body.Role != "" {
			updates["role"] = input.Body.Role
			hasUpdates = true
		}
		if input.Body.Fleet != "" {
			updates["fleet"] = input.Body.Fleet
			hasUpdates = true
		}

		// 處理密碼更新
		if input.Body.Password != "" {
			updates["password"] = input.Body.Password
			hasUpdates = true
			c.logger.Info().Str("user_id", userID).Msg("用戶正在更新密碼")
		}

		var u *model.User

		if hasUpdates {
			// 更新用戶資料
			u, err = c.userService.UpdateUser(ctx, userID, updates)
			if err != nil {
				c.logger.Error().Err(err).Str("user_id", userID).Msg("更新用戶資料失敗")
				return nil, huma.Error400BadRequest("更新用戶資料失敗", err)
			}
			c.logger.Info().Str("user_id", userID).Str("user_account", u.Account).Msg("用戶資料更新成功")
		} else {
			// 沒有更新，只返回當前用戶資料
			u, err = c.userService.GetUserByID(ctx, userID)
			if err != nil {
				c.logger.Error().Err(err).Str("user_id", userID).Msg("獲取用戶資料失敗")
				return nil, huma.Error400BadRequest("獲取用戶資料失敗", err)
			}
		}

		return &user.UserResponse{Body: u}, nil
	})

	// 更新指定用戶資料（管理員專用，需要認證）
	huma.Register(api, huma.Operation{
		OperationID: "update-user-profile-by-id",
		Method:      "PUT",
		Path:        "/users/{id}/profile",
		Summary:     "更新指定用戶的個人資料（管理員專用）",
		Description: "管理員可以更新指定用戶的個人資料，包括帳號、角色、車隊和密碼。需要管理員權限。",
		Tags:        []string{"users"},
		Middlewares: huma.Middlewares{c.authMiddleware.Auth()},
		Security: []map[string][]string{
			{"bearerAuth": {}},
		},
	}, func(ctx context.Context, input *user.UpdateUserProfileByIdInput) (*user.UserResponse, error) {
		userFromToken, err := auth.GetUserFromContext(ctx)
		if err != nil {
			c.logger.Error().Err(err).Msg("無法從token中獲取用戶資訊")
			return nil, huma.Error500InternalServerError("無法從token中獲取用戶資訊")
		}

		// 檢查權限 - 只有系統管理員和版主可以修改其他用戶資料
		if userFromToken.Role != model.RoleSystemAdmin && userFromToken.Role != model.RoleModerator {
			c.logger.Warn().
				Str("user_id", userFromToken.ID.Hex()).
				Str("user_role", string(userFromToken.Role)).
				Str("target_user_id", input.ID).
				Msg("用戶嘗試修改其他用戶資料但權限不足")
			return nil, huma.Error403Forbidden("權限不足，只有系統管理員和版主可以修改用戶資料")
		}

		c.logger.Info().
			Str("admin_id", userFromToken.ID.Hex()).
			Str("admin_account", userFromToken.Account).
			Str("target_user_id", input.ID).
			Msg("管理員正在更新指定用戶資料")

		// 構建更新資料
		updates := bson.M{}
		hasUpdates := false

		if input.Body.Name != "" {
			updates["name"] = input.Body.Name
			hasUpdates = true
		}
		if input.Body.Account != "" {
			updates["account"] = input.Body.Account
			hasUpdates = true
		}
		if input.Body.Role != "" {
			updates["role"] = input.Body.Role
			hasUpdates = true
		}
		if input.Body.Fleet != "" {
			updates["fleet"] = input.Body.Fleet
			hasUpdates = true
		}

		// 處理密碼更新
		if input.Body.Password != "" {
			updates["password"] = input.Body.Password
			hasUpdates = true
			c.logger.Info().
				Str("admin_id", userFromToken.ID.Hex()).
				Str("target_user_id", input.ID).
				Msg("管理員正在為指定用戶更新密碼")
		}

		var u *model.User

		if hasUpdates {
			// 更新用戶資料
			u, err = c.userService.UpdateUser(ctx, input.ID, updates)
			if err != nil {
				c.logger.Error().
					Err(err).
					Str("admin_id", userFromToken.ID.Hex()).
					Str("target_user_id", input.ID).
					Msg("管理員更新用戶資料失敗")
				return nil, huma.Error400BadRequest("更新用戶資料失敗", err)
			}
			c.logger.Info().
				Str("admin_id", userFromToken.ID.Hex()).
				Str("admin_account", userFromToken.Account).
				Str("target_user_id", input.ID).
				Str("target_user_account", u.Account).
				Msg("管理員成功更新用戶資料")
		} else {
			// 沒有更新，只返回當前用戶資料
			u, err = c.userService.GetUserByID(ctx, input.ID)
			if err != nil {
				c.logger.Error().
					Err(err).
					Str("target_user_id", input.ID).
					Msg("獲取目標用戶資料失敗")
				return nil, huma.Error400BadRequest("獲取用戶資料失敗", err)
			}
		}

		return &user.UserResponse{Body: u}, nil
	})

}
