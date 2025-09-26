package controller

import (
	"context"
	"right-backend/data-models/admin"
	"right-backend/data-models/common"
	"right-backend/service"

	"github.com/danielgtaylor/huma/v2"
	"github.com/rs/zerolog"
)

type AdminController struct {
	logger        zerolog.Logger
	driverService *service.DriverService
}

func NewAdminController(logger zerolog.Logger, driverService *service.DriverService) *AdminController {
	return &AdminController{
		logger:        logger.With().Str("module", "admin_controller").Logger(),
		driverService: driverService,
	}
}

func (c *AdminController) RegisterRoutes(api huma.API) {
	// 獲取司機審核列表
	huma.Register(api, huma.Operation{
		OperationID: "get-approval-driver",
		Method:      "GET",
		Path:        "/admin/get-approval-driver",
		Summary:     "獲取司機審核列表",
		Description: "獲取司機列表，支援車隊過濾、模糊搜尋和審核狀態過濾，帶分頁功能",
		Tags:        []string{"admin"},
	}, func(ctx context.Context, input *admin.GetApprovalDriverInput) (*admin.PaginatedDriversResponse, error) {
		drivers, totalCount, err := c.driverService.GetDriversForApproval(
			ctx,
			input.GetPageNum(),
			input.GetPageSize(),
			input.Fleet,
			input.HasFleet,
			input.GetSearchKeyword(),
			input.IsApproved,
		)
		if err != nil {
			c.logger.Error().
				Err(err).
				Str("fleet", input.Fleet).
				Str("has_fleet", input.HasFleet).
				Str("search_keyword", input.GetSearchKeyword()).
				Msg("獲取司機審核列表失敗")
			return nil, huma.Error500InternalServerError("獲取司機審核列表失敗", err)
		}

		response := &admin.PaginatedDriversResponse{}
		response.Body.Drivers = drivers
		response.Body.Pagination = common.NewPaginationInfo(input.GetPageNum(), input.GetPageSize(), totalCount)

		return response, nil
	})

	// 審核司機
	huma.Register(api, huma.Operation{
		OperationID: "approve-driver",
		Method:      "PUT",
		Path:        "/admin/approve-driver/{driverId}",
		Summary:     "審核司機",
		Description: "將指定司機的審核狀態設為已審核（is_approved = true），並啟用司機帳號",
		Tags:        []string{"admin"},
	}, func(ctx context.Context, input *admin.ApproveDriverInput) (*admin.SimpleResponse, error) {
		updatedDriver, err := c.driverService.ApproveDriver(ctx, input.DriverID)
		if err != nil {
			c.logger.Error().
				Err(err).
				Str("driver_id", input.DriverID).
				Msg("審核司機失敗")
			return nil, huma.Error400BadRequest("審核司機失敗", err)
		}

		response := &admin.SimpleResponse{}
		response.Body.Success = true
		response.Body.Message = "司機審核成功，司機 " + updatedDriver.Name + " 已通過審核並啟用"

		c.logger.Info().
			Str("driver_id", input.DriverID).
			Str("司機姓名", updatedDriver.Name).
			Str("車隊", string(updatedDriver.Fleet)).
			Msg("司機審核成功")

		return response, nil
	})

	// 更新司機個人資料
	huma.Register(api, huma.Operation{
		OperationID: "admin-update-driver-profile",
		Method:      "PUT",
		Path:        "/admin/drivers/{driverId}/profile",
		Summary:     "管理員更新司機個人資料",
		Description: "管理員更新指定司機的個人資料，包含基本資料、車輛資訊、車隊、狀態等",
		Tags:        []string{"admin"},
	}, func(ctx context.Context, input *admin.UpdateDriverProfileInput) (*admin.UpdateDriverProfileResponse, error) {
		updatedDriver, err := c.driverService.AdminUpdateDriverProfile(ctx, input.DriverID, input)
		if err != nil {
			c.logger.Error().
				Err(err).
				Str("driver_id", input.DriverID).
				Msg("管理員更新司機個人資料失敗")
			return nil, huma.Error400BadRequest("管理員更新司機個人資料失敗", err)
		}

		response := &admin.UpdateDriverProfileResponse{}
		response.Body = updatedDriver

		c.logger.Info().
			Str("driver_id", input.DriverID).
			Str("司機姓名", updatedDriver.Name).
			Str("車隊", string(updatedDriver.Fleet)).
			Msg("管理員更新司機個人資料成功")

		return response, nil
	})

	// 司機退出車隊
	huma.Register(api, huma.Operation{
		OperationID: "admin-driver-leave-fleet",
		Method:      "PUT",
		Path:        "/admin/drivers/{driverId}/leave-fleet",
		Summary:     "管理員讓司機退出車隊",
		Description: "將指定司機從車隊中移除，設定車隊欄位為空白",
		Tags:        []string{"admin"},
	}, func(ctx context.Context, input *admin.RemoveDriverFromFleetInput) (*admin.UpdateDriverProfileResponse, error) {
		updatedDriver, err := c.driverService.RemoveDriverFromFleet(ctx, input.DriverID)

		if err != nil {
			return nil, huma.Error400BadRequest("管理員移除司機車隊失敗", err)
		}

		response := &admin.UpdateDriverProfileResponse{}
		response.Body = updatedDriver

		c.logger.Info().
			Str("driver_id", input.DriverID).
			Str("司機姓名", updatedDriver.Name).
			Msg("管理員移除司機車隊成功")

		return response, nil
	})
}
