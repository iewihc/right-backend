package controller

import (
	"context"
	"right-backend/auth"
	"right-backend/data-models/role"
	"right-backend/middleware"
	"right-backend/service"

	"github.com/danielgtaylor/huma/v2"
	"github.com/rs/zerolog"
)

type RoleController struct {
	logger         zerolog.Logger
	roleService    *service.RoleService
	authMiddleware *middleware.UserAuthMiddleware
}

func NewRoleController(logger zerolog.Logger, roleService *service.RoleService, authMiddleware *middleware.UserAuthMiddleware) *RoleController {
	return &RoleController{
		logger:         logger.With().Str("module", "role_controller").Logger(),
		roleService:    roleService,
		authMiddleware: authMiddleware,
	}
}

func (c *RoleController) RegisterRoutes(api huma.API) {
	// 獲取角色列表（分頁）
	huma.Register(api, huma.Operation{
		OperationID: "get-roles",
		Method:      "GET",
		Path:        "/roles",
		Summary:     "獲取角色列表（分頁）",
		Description: "獲取分頁的角色列表，支援頁碼、每頁數量、是否包含系統角色和模糊搜尋參數",
		Tags:        []string{"roles"},
		Middlewares: huma.Middlewares{c.authMiddleware.Auth()},
		Security: []map[string][]string{
			{"bearerAuth": {}},
		},
	}, func(ctx context.Context, input *role.GetRolesInput) (*role.PaginatedRolesResponse, error) {
		userFromToken, err := auth.GetUserFromContext(ctx)
		if err != nil {
			c.logger.Error().Err(err).Msg("無法從token中獲取用戶資訊")
			return nil, huma.Error500InternalServerError("無法從token中獲取用戶資訊")
		}

		roles, pagination, err := c.roleService.GetRolesWithPagination(ctx, input.GetPageNum(), input.GetPageSize(), input.IncludeSystem, input.Search, input.Fleet)
		if err != nil {
			c.logger.Error().
				Bool("包含系統角色", input.IncludeSystem).
				Str("搜尋關鍵字", input.Search).
				Str("車隊過濾", input.Fleet).
				Err(err).
				Msg("獲取角色列表失敗")
			return nil, huma.Error500InternalServerError("獲取角色列表失敗", err)
		}

		response := &role.PaginatedRolesResponse{}
		response.Body.Roles = roles
		response.Body.Pagination = *pagination

		c.logger.Info().
			Str("用戶ID", userFromToken.ID.Hex()).
			Int("頁碼", input.GetPageNum()).
			Int("每頁數量", input.GetPageSize()).
			Bool("包含系統角色", input.IncludeSystem).
			Str("搜尋關鍵字", input.Search).
			Str("車隊過濾", input.Fleet).
			Int64("總筆數", pagination.TotalItems).
			Int("回傳筆數", len(roles)).
			Msg("成功獲取角色列表")

		return response, nil
	})

	// 創建角色
	huma.Register(api, huma.Operation{
		OperationID: "create-role",
		Method:      "POST",
		Path:        "/roles",
		Summary:     "創建新角色",
		Description: "創建新的自定義角色，需要認證",
		Tags:        []string{"roles"},
		Middlewares: huma.Middlewares{c.authMiddleware.Auth()},
		Security: []map[string][]string{
			{"bearerAuth": {}},
		},
	}, func(ctx context.Context, input *role.CreateRoleInput) (*role.RoleResponse, error) {
		userFromToken, err := auth.GetUserFromContext(ctx)
		if err != nil {
			c.logger.Error().Err(err).Msg("無法從token中獲取用戶資訊")
			return nil, huma.Error500InternalServerError("無法從token中獲取用戶資訊")
		}

		c.logger.Info().
			Str("用戶ID", userFromToken.ID.Hex()).
			Str("用戶帳號", userFromToken.Account).
			Str("角色名稱", input.Body.Name).
			Msg("用戶嘗試創建新角色")

		createdRole, err := c.roleService.CreateRole(ctx, input)
		if err != nil {
			c.logger.Error().
				Str("用戶ID", userFromToken.ID.Hex()).
				Str("角色名稱", input.Body.Name).
				Err(err).
				Msg("創建角色失敗")
			return nil, huma.Error400BadRequest("創建角色失敗: " + err.Error())
		}

		c.logger.Info().
			Str("創建者ID", userFromToken.ID.Hex()).
			Str("角色ID", createdRole.ID.Hex()).
			Str("角色名稱", string(createdRole.Name)).
			Msg("角色創建成功")

		return &role.RoleResponse{Body: createdRole}, nil
	})

	// 根據ID獲取角色
	huma.Register(api, huma.Operation{
		OperationID: "get-role-by-id",
		Method:      "GET",
		Path:        "/roles/{id}",
		Summary:     "根據ID獲取角色",
		Tags:        []string{"roles"},
		Middlewares: huma.Middlewares{c.authMiddleware.Auth()},
		Security: []map[string][]string{
			{"bearerAuth": {}},
		},
	}, func(ctx context.Context, input *role.RoleIDInput) (*role.RoleResponse, error) {
		r, err := c.roleService.GetRoleByID(ctx, input.ID)
		if err != nil {
			c.logger.Error().Err(err).Str("角色ID", input.ID).Msg("角色不存在")
			return nil, huma.Error404NotFound("角色不存在", err)
		}

		return &role.RoleResponse{Body: r}, nil
	})

	// 更新角色
	huma.Register(api, huma.Operation{
		OperationID: "update-role",
		Method:      "PUT",
		Path:        "/roles/{id}",
		Summary:     "更新角色",
		Description: "更新指定角色的資訊，系統角色不能被修改",
		Tags:        []string{"roles"},
		Middlewares: huma.Middlewares{c.authMiddleware.Auth()},
		Security: []map[string][]string{
			{"bearerAuth": {}},
		},
	}, func(ctx context.Context, input *role.UpdateRoleInput) (*role.RoleResponse, error) {
		userFromToken, err := auth.GetUserFromContext(ctx)
		if err != nil {
			c.logger.Error().Err(err).Msg("無法從token中獲取用戶資訊")
			return nil, huma.Error500InternalServerError("無法從token中獲取用戶資訊")
		}

		c.logger.Info().
			Str("用戶ID", userFromToken.ID.Hex()).
			Str("角色ID", input.ID).
			Msg("用戶嘗試更新角色")

		updatedRole, err := c.roleService.UpdateRole(ctx, input)
		if err != nil {
			c.logger.Error().
				Str("用戶ID", userFromToken.ID.Hex()).
				Str("角色ID", input.ID).
				Err(err).
				Msg("更新角色失敗")
			return nil, huma.Error400BadRequest("更新角色失敗: " + err.Error())
		}

		c.logger.Info().
			Str("操作者ID", userFromToken.ID.Hex()).
			Str("角色ID", updatedRole.ID.Hex()).
			Str("角色名稱", string(updatedRole.Name)).
			Msg("角色更新成功")

		return &role.RoleResponse{Body: updatedRole}, nil
	})

	// 刪除角色
	huma.Register(api, huma.Operation{
		OperationID: "delete-role",
		Method:      "DELETE",
		Path:        "/roles/{id}",
		Summary:     "刪除角色",
		Description: "刪除指定角色，系統角色不能被刪除，有用戶使用的角色也不能被刪除",
		Tags:        []string{"roles"},
		Middlewares: huma.Middlewares{c.authMiddleware.Auth()},
		Security: []map[string][]string{
			{"bearerAuth": {}},
		},
	}, func(ctx context.Context, input *role.DeleteRoleInput) (*role.DeleteRoleResponse, error) {
		userFromToken, err := auth.GetUserFromContext(ctx)
		if err != nil {
			c.logger.Error().Err(err).Msg("無法從token中獲取用戶資訊")
			return nil, huma.Error500InternalServerError("無法從token中獲取用戶資訊")
		}

		c.logger.Info().
			Str("用戶ID", userFromToken.ID.Hex()).
			Str("角色ID", input.ID).
			Msg("用戶嘗試刪除角色")

		err = c.roleService.DeleteRole(ctx, input.ID)
		if err != nil {
			c.logger.Error().
				Str("用戶ID", userFromToken.ID.Hex()).
				Str("角色ID", input.ID).
				Err(err).
				Msg("刪除角色失敗")
			return nil, huma.Error400BadRequest("刪除角色失敗: " + err.Error())
		}

		c.logger.Info().
			Str("操作者ID", userFromToken.ID.Hex()).
			Str("角色ID", input.ID).
			Msg("角色刪除成功")

		response := &role.DeleteRoleResponse{}
		response.Body.Message = "角色已成功刪除"
		response.Body.RoleID = input.ID

		return response, nil
	})
}
