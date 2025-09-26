package controller

import (
	"context"
	"right-backend/data-models/auth"
	"right-backend/infra"
	"right-backend/model"
	"right-backend/service"

	"github.com/danielgtaylor/huma/v2"
	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel/trace"
)

type AuthController struct {
	logger        zerolog.Logger
	userService   *service.UserService
	driverService *service.DriverService
}

func NewAuthController(logger zerolog.Logger, userService *service.UserService, driverService *service.DriverService) *AuthController {
	return &AuthController{
		logger:        logger.With().Str("module", "auth_controller").Logger(),
		userService:   userService,
		driverService: driverService,
	}
}

func (c *AuthController) RegisterRoutes(api huma.API) {
	// 用戶登入
	huma.Register(api, huma.Operation{
		OperationID: "user-login",
		Method:      "POST",
		Path:        "/auth/login",
		Summary:     "用戶登入",
		Tags:        []string{"auth"},
	}, func(ctx context.Context, input *auth.LoginInput) (*auth.UserLoginResponse, error) {
		// 獲取當前 span
		span := trace.SpanFromContext(ctx)

		// 創建業務 span
		authCtx, authSpan := infra.StartSpan(ctx, "user_login_controller",
			infra.AttrOperation("user_login"),
			infra.AttrString("auth.account", input.Body.Account),
		)
		defer authSpan.End()

		// 添加登入開始事件
		infra.AddEvent(authSpan, "user_login_started",
			infra.AttrString("account", input.Body.Account),
		)

		user, token, err := c.userService.Login(authCtx, input.Body.Account, input.Body.Password)
		if err != nil {
			// 記錄錯誤到 span
			infra.RecordError(authSpan, err, "User login failed",
				infra.AttrString("account", input.Body.Account),
				infra.AttrString("error", err.Error()),
			)

			infra.AddEvent(authSpan, "user_login_failed",
				infra.AttrString("account", input.Body.Account),
				infra.AttrString("error", err.Error()),
			)

			c.logger.Warn().
				Str("用戶帳號", input.Body.Account).
				Str("錯誤原因", err.Error()).
				Str("trace_id", span.SpanContext().TraceID().String()).
				Str("span_id", authSpan.SpanContext().SpanID().String()).
				Msg("用戶登入失敗 - 帳號或密碼錯誤")
			return nil, huma.Error401Unauthorized("帳號或密碼錯誤", err)
		}

		// 添加成功事件
		infra.AddEvent(authSpan, "user_login_success",
			infra.AttrUserID(user.ID.Hex()),
			infra.AttrString("account", user.Account),
			infra.AttrString("role", string(user.Role)),
		)

		// 設置成功的 span 屬性
		infra.MarkSuccess(authSpan,
			infra.AttrUserID(user.ID.Hex()),
			infra.AttrString("user.role", string(user.Role)),
		)

		c.logger.Info().
			Str("用戶編號", user.ID.Hex()).
			Str("用戶帳號", user.Account).
			Str("用戶角色", string(user.Role)).
			Str("trace_id", span.SpanContext().TraceID().String()).
			Str("span_id", authSpan.SpanContext().SpanID().String()).
			Msg("用戶登入成功")

		resp := &auth.UserLoginResponse{}
		resp.Body.User = user
		resp.Body.Token = token
		resp.Body.Message = "登入成功"

		return resp, nil
	})

	// 司機登入
	huma.Register(api, huma.Operation{
		OperationID: "driver-login",
		Method:      "POST",
		Path:        "/auth/driver-login",
		Summary:     "司機登入",
		Tags:        []string{"auth"},
	}, func(ctx context.Context, input *auth.DriverLoginInput) (*auth.DriverLoginResponse, error) {
		// 獲取當前 span
		span := trace.SpanFromContext(ctx)

		// 創建業務 span
		authCtx, authSpan := infra.StartSpan(ctx, "driver_login_controller",
			infra.AttrOperation("driver_login"),
			infra.AttrDriverAccount(input.Body.Account),
			infra.AttrString("device.model", input.Body.DeviceModelName),
			infra.AttrString("device.brand", input.Body.DeviceBrand),
			infra.AttrString("app.version", input.Body.DeviceAppVersion),
		)
		defer authSpan.End()

		// 添加登入開始事件
		infra.AddEvent(authSpan, "driver_login_started",
			infra.AttrDriverAccount(input.Body.Account),
			infra.AttrString("device_model", input.Body.DeviceModelName),
		)

		driver, token, err := c.driverService.Login(authCtx, input.Body.Account, input.Body.Password, input.Body.DeviceModelName, input.Body.DeviceDeviceName, input.Body.DeviceBrand, input.Body.DeviceManufacturer, input.Body.DeviceAppVersion)
		if err != nil {
			// 記錄錯誤到 span
			infra.RecordError(authSpan, err, "Driver login failed",
				infra.AttrDriverAccount(input.Body.Account),
				infra.AttrString("error", err.Error()),
			)

			infra.AddEvent(authSpan, "driver_login_failed",
				infra.AttrDriverAccount(input.Body.Account),
				infra.AttrString("error", err.Error()),
			)

			c.logger.Debug().
				Str("司機帳號", input.Body.Account).
				Str("錯誤原因", err.Error()).
				Str("trace_id", span.SpanContext().TraceID().String()).
				Str("span_id", authSpan.SpanContext().SpanID().String()).
				Msg("司機登入失敗")

			switch err.Error() {
			case "帳號不存在":
				infra.SetAttributes(authSpan, infra.AttrErrorType("account_not_found"))
				return nil, huma.Error401Unauthorized("帳號不存在", err)
			case "密碼錯誤":
				infra.SetAttributes(authSpan, infra.AttrErrorType("wrong_password"))
				return nil, huma.Error401Unauthorized("密碼錯誤", err)
			case "帳號未啟用":
				infra.SetAttributes(authSpan, infra.AttrErrorType("account_disabled"))
				return nil, huma.Error403Forbidden("您的帳號尚未啟用，請聯絡管理員", err)
			default:
				infra.SetAttributes(authSpan, infra.AttrErrorType("system_error"))
				return nil, huma.Error500InternalServerError("系統錯誤，請稍後再試", err)
			}
		}

		// 添加認證成功事件
		infra.AddEvent(authSpan, "driver_credentials_verified",
			infra.AttrDriverID(driver.ID.Hex()),
			infra.AttrDriverAccount(driver.Account),
		)

		// 檢查帳號是否已通過審核
		infra.AddEvent(authSpan, "checking_driver_approval")
		if !driver.IsApproved {
			infra.AddEvent(authSpan, "driver_approval_check_failed",
				infra.AttrDriverID(driver.ID.Hex()),
				infra.AttrBool("is_approved", driver.IsApproved),
			)
			infra.SetAttributes(authSpan,
				infra.AttrErrorType("not_approved"),
				infra.AttrBool("driver.is_approved", false),
			)

			c.logger.Debug().
				Str("司機編號", driver.ID.Hex()).
				Str("司機帳號", driver.Account).
				Str("司機姓名", driver.Name).
				Str("trace_id", span.SpanContext().TraceID().String()).
				Str("span_id", authSpan.SpanContext().SpanID().String()).
				Msg("司機登入失敗 - 帳號尚未審核通過")
			return nil, huma.Error403Forbidden("您的帳號正在等待管理員審核中！")
		}

		// 添加最終成功事件
		infra.AddEvent(authSpan, "driver_login_success",
			infra.AttrDriverID(driver.ID.Hex()),
			infra.AttrDriverAccount(driver.Account),
			infra.AttrString("name", driver.Name),
			infra.AttrString("fleet", string(driver.Fleet)),
			infra.AttrString("car_plate", driver.CarPlate),
		)

		// 設置成功的 span 屬性
		infra.MarkSuccess(authSpan,
			infra.AttrDriverID(driver.ID.Hex()),
			infra.AttrString("driver.name", driver.Name),
			infra.AttrString("fleet", string(driver.Fleet)),
			infra.AttrString("car_plate", driver.CarPlate),
			infra.AttrBool("driver.is_approved", true),
		)

		c.logger.Debug().
			Str("司機編號", driver.ID.Hex()).
			Str("司機帳號", driver.Account).
			Str("司機姓名", driver.Name).
			Str("車隊名稱", string(driver.Fleet)).
			Str("車牌號碼", driver.CarPlate).
			Str("trace_id", span.SpanContext().TraceID().String()).
			Str("span_id", authSpan.SpanContext().SpanID().String()).
			Msg("司機登入成功")

		resp := &auth.DriverLoginResponse{}
		resp.Body.Driver = driver
		resp.Body.Token = token
		resp.Body.Message = "登入成功"

		return resp, nil
	})

	// 司機註冊
	huma.Register(api, huma.Operation{
		OperationID: "driver-register",
		Method:      "POST",
		Path:        "/auth/driver-register",
		Summary:     "司機註冊",
		Tags:        []string{"auth"},
	}, func(ctx context.Context, input *auth.DriverRegisterInput) (*auth.DriverRegisterResponse, error) {
		// 獲取當前 span
		span := trace.SpanFromContext(ctx)

		// 創建業務 span
		authCtx, authSpan := infra.StartSpan(ctx, "driver_register_controller",
			infra.AttrOperation("driver_register"),
			infra.AttrDriverAccount(input.Body.Account),
			infra.AttrString("driver.name", input.Body.Name),
			infra.AttrString("driver.fleet", input.Body.Fleet),
			infra.AttrString("driver.car_plate", input.Body.CarPlate),
		)
		defer authSpan.End()

		// 添加註冊開始事件
		infra.AddEvent(authSpan, "driver_register_started",
			infra.AttrDriverAccount(input.Body.Account),
			infra.AttrString("name", input.Body.Name),
			infra.AttrString("fleet", input.Body.Fleet),
		)

		// 檢查帳號是否已存在
		infra.AddEvent(authSpan, "checking_existing_account")
		existingDriver, _ := c.driverService.GetDriverByAccount(authCtx, input.Body.Account)
		if existingDriver != nil {
			infra.AddEvent(authSpan, "account_already_exists",
				infra.AttrString("account", input.Body.Account),
				infra.AttrString("existing_driver_id", existingDriver.ID.Hex()),
			)
			infra.SetAttributes(authSpan,
				infra.AttrString("auth.failure_reason", "account_exists"),
				infra.AttrBool("auth.success", false),
			)

			c.logger.Warn().
				Str("司機帳號", input.Body.Account).
				Str("車隊名稱", input.Body.Fleet).
				Str("車牌號碼", input.Body.CarPlate).
				Str("trace_id", span.SpanContext().TraceID().String()).
				Str("span_id", authSpan.SpanContext().SpanID().String()).
				Msg("司機註冊失敗 - 帳號已存在")
			return nil, huma.Error400BadRequest("此帳號已被註冊")
		}

		infra.AddEvent(authSpan, "account_available")

		// 建立新司機資料
		infra.AddEvent(authSpan, "creating_driver_data")
		newDriver := &model.DriverInfo{
			Name:       input.Body.Name,
			Nickname:   input.Body.Nickname,
			DriverNo:   input.Body.DriverNo,
			Account:    input.Body.Account,
			Password:   input.Body.Password,
			CarPlate:   input.Body.CarPlate,
			Fleet:      model.FleetType(input.Body.Fleet),
			CarModel:   input.Body.CarModel,
			CarAge:     input.Body.CarAge,
			CarColor:   input.Body.CarColor,
			Referrer:   input.Body.Referrer,
			JkoAccount: input.Body.JkoAccount,
			IsApproved: false, // 預設未審核
			IsActive:   false, // 預設未啟用，需等待審核通過
			IsOnline:   false,
			Status:     model.DriverStatusInactive,
		}

		// 建立司機
		infra.AddEvent(authSpan, "saving_driver_to_database")
		createdDriver, err := c.driverService.CreateDriver(authCtx, newDriver)
		if err != nil {
			// 記錄錯誤到 span
			infra.RecordError(authSpan, err, "Operation failed")
			infra.AddEvent(authSpan, "driver_creation_failed",
				infra.AttrString("account", input.Body.Account),
				infra.AttrString("error", err.Error()),
			)
			infra.SetAttributes(authSpan,
				infra.AttrString("auth.failure_reason", "database_error"),
				infra.AttrBool("auth.success", false),
			)

			c.logger.Error().
				Str("司機帳號", input.Body.Account).
				Str("司機姓名", input.Body.Name).
				Str("車隊名稱", input.Body.Fleet).
				Str("車牌號碼", input.Body.CarPlate).
				Str("錯誤原因", err.Error()).
				Str("trace_id", span.SpanContext().TraceID().String()).
				Str("span_id", authSpan.SpanContext().SpanID().String()).
				Msg("司機註冊失敗 - 數據庫錯誤")
			return nil, huma.Error500InternalServerError("註冊失敗", err)
		}

		// 添加成功事件
		infra.AddEvent(authSpan, "driver_register_success",
			infra.AttrString("driver_id", createdDriver.ID.Hex()),
			infra.AttrString("account", createdDriver.Account),
			infra.AttrString("name", createdDriver.Name),
			infra.AttrString("fleet", string(createdDriver.Fleet)),
			infra.AttrString("car_plate", createdDriver.CarPlate),
		)

		// 設置成功的 span 屬性
		infra.SetAttributes(authSpan,
			infra.AttrString("auth.driver_id", createdDriver.ID.Hex()),
			infra.AttrString("auth.driver_name", createdDriver.Name),
			infra.AttrString("auth.fleet", string(createdDriver.Fleet)),
			infra.AttrString("auth.car_plate", createdDriver.CarPlate),
			infra.AttrBool("auth.success", true),
			infra.AttrBool("driver.is_approved", false),
			infra.AttrBool("driver.is_active", false),
		)

		c.logger.Info().
			Str("司機編號", createdDriver.ID.Hex()).
			Str("司機帳號", createdDriver.Account).
			Str("司機姓名", createdDriver.Name).
			Str("車隊名稱", string(createdDriver.Fleet)).
			Str("車牌號碼", createdDriver.CarPlate).
			Str("trace_id", span.SpanContext().TraceID().String()).
			Str("span_id", authSpan.SpanContext().SpanID().String()).
			Msg("司機註冊成功 - 等待審核")

		resp := &auth.DriverRegisterResponse{}
		resp.Body.Driver = createdDriver
		resp.Body.Message = "註冊成功，等待審核"

		return resp, nil
	})
}
