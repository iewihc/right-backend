package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"right-backend/background"
	"right-backend/controller"
	"right-backend/infra"
	"right-backend/metrics"
	authMiddleware "right-backend/middleware"
	otelMiddleware "right-backend/middleware"
	"right-backend/service"
	"syscall"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/danielgtaylor/huma/v2/humacli"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/rs/zerolog/log"
)

type Options struct {
	Port        int    `help:"服務監聽端口" short:"p" default:"8090"`
	MongoURI    string `help:"MongoDB連接URI" default:"mongodb://localhost:27017"`
	MongoDB     string `help:"MongoDB資料庫名稱" default:"taxi_db"`
	RedisAddr   string `help:"Redis地址" default:"localhost:6379"`
	RabbitMQURL string `help:"RabbitMQ連接URL" default:"amqp://guest:guest@localhost:5672/"`
}

type AppServices struct {
	MongoDB  *infra.MongoDB
	Redis    *infra.Redis
	RabbitMQ *infra.RabbitMQ
}

// 全局變量用於存儲 OpenTelemetry cleanup 函數
var otelCleanup func()

func main() {
	cli := humacli.New(func(hooks humacli.Hooks, options *Options) {
		// 載入設定檔
		if err := infra.LoadConfig(); err != nil {
			log.Fatal().
				Err(err).
				Msg("讀取 config.yml 失敗")
		}

		// 初始化 logger（在載入配置後）
		infra.InitLogger()

		// 初始化 OpenTelemetry
		// 從環境變數取得 OTEL endpoint，預設為 localhost:4318
		otelEndpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
		if otelEndpoint == "" {
			otelEndpoint = "localhost:4318"
		}

		otelConfig := otelMiddleware.OtelConfig{
			ServiceName:     "right-backend",
			ServiceVersion:  "1.0.0",
			Environment:     "development",
			OTLPEndpoint:    otelEndpoint,
			TracesEnabled:   true,
			MetricsEnabled:  true,
			Enabled:         true,
			DevelopmentMode: false, // 使用 OTLP exporter
		}

		var err error
		otelCleanup, err = otelMiddleware.InitOpenTelemetry(otelConfig, log.Logger)
		if err != nil {
			log.Fatal().
				Err(err).
				Msg("OpenTelemetry 初始化失敗")
		}

		// 初始化全局 tracer
		infra.InitTracer()

		// 初始化 Prometheus metrics
		if err := otelMiddleware.InitPrometheusMetrics(log.Logger); err != nil {
			log.Error().
				Err(err).
				Msg("Prometheus metrics 初始化失敗，將繼續運行")
		}

		// 初始化 Service 層 metrics
		if err := metrics.InitServiceMetrics(otelMiddleware.GetPrometheusRegistry()); err != nil {
			log.Error().
				Err(err).
				Msg("Service metrics 初始化失敗，將繼續運行")
		}

		log.Info().
			Int("port", options.Port).
			Msg("啟動 Right Backend API服務")

		services, err := initializeServices(options)
		if err != nil {
			log.Fatal().
				Err(err).
				Msg("初始化服務失敗")
		}

		// 清除所有 Redis 快取（應用重啟時）
		if services.Redis != nil {
			ctx := context.Background()
			if flushErr := services.Redis.Client.FlushAll(ctx).Err(); flushErr != nil {
				log.Error().
					Err(flushErr).
					Msg("清除 Redis 快取失敗")
			} else {
				log.Info().Msg("已清除所有 Redis 快取")
			}
		}

		router := chi.NewRouter()
		router.Use(middleware.Logger)
		router.Use(middleware.Recoverer)
		router.Use(middleware.RequestID)
		router.Use(middleware.Heartbeat("/ping"))

		// CORS 設定 - 允許所有來源
		router.Use(cors.Handler(cors.Options{
			AllowedOrigins:   []string{"*"},
			AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
			AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
			ExposedHeaders:   []string{"Link"},
			AllowCredentials: false,
			MaxAge:           300, // Maximum value not ignored by any of major browsers
		}))

		apiConfig := huma.DefaultConfig("Right Admin API", "1.0.0")
		apiConfig.Info.Description = "基於Huma框架的Right Admin API"

		// 設定服務器 URL - 使用 config.yml 中的 cert_base_url
		serverURL := fmt.Sprintf("http://localhost:%d", options.Port)
		if infra.AppConfig.CertBaseURL != "" {
			serverURL = infra.AppConfig.CertBaseURL
		}
		apiConfig.Servers = []*huma.Server{
			{URL: serverURL},
		}

		// 配置 JWT Bearer 認證
		apiConfig.Components.SecuritySchemes = map[string]*huma.SecurityScheme{
			"bearerAuth": {
				Type:         "http",
				Scheme:       "bearer",
				BearerFormat: "JWT",
				Description:  "JWT Bearer Token 認證",
			},
		}

		api := humachi.New(router, apiConfig)

		// 添加 OpenTelemetry 中間件到 API
		api.UseMiddleware(otelMiddleware.OpenTelemetryMiddleware(otelConfig, log.Logger))

		// 添加 Prometheus metrics 中間件
		api.UseMiddleware(otelMiddleware.PrometheusMiddleware(log.Logger))

		googleClient := infra.NewGoogleClient(infra.GoogleConfig{APIKey: infra.AppConfig.Google.APIKey})

		// 創建 GooglePlaceCacheService
		googlePlaceCacheService := service.NewGooglePlaceCacheService(log.Logger, services.MongoDB)

		googleService := service.NewGoogleMapService(log.Logger, googleClient, services.MongoDB, services.Redis.Client, googlePlaceCacheService)

		// 創建 FCM 服務
		fcmService := service.NewExpoService(log.Logger)

		var crawlerService *service.CrawlerService
		if infra.AppConfig.App.IsCrawler {
			log.Info().Msg("CrawlerService is ENABLED via config.yml")
			var crawlerErr error
			crawlerService, crawlerErr = service.NewCrawlerService(log.Logger, services.Redis)
			if crawlerErr != nil {
				log.Error().
					Err(crawlerErr).
					Msg("初始化 CrawlerService 失敗，繼續運行其他服務")
				crawlerService = nil
			}
		} else {
			log.Info().Msg("CrawlerService is DISABLED via config.yml")
		}

		// 相互依賴的服務初始化
		var orderService *service.OrderService
		var discordService *service.DiscordService
		var discordEventHandler *service.DiscordEventHandler
		var lineService *service.LineService
		var lineEventHandler *service.LineEventHandler

		// 創建 Redis 事件管理器（需要在 OrderService 之前）
		eventManager := infra.NewRedisEventManager(services.Redis.Client, log.Logger)

		// 1. 初始化 DiscordService (暫時不傳入 orderService)
		if infra.AppConfig.Discord.BotToken != "" && infra.AppConfig.Discord.BotToken != "YOUR_DISCORD_BOT_TOKEN" {
			var err error
			discordService, err = service.NewDiscordService(log.Logger, infra.AppConfig.Discord.BotToken, nil) // 簡潔方案：只需要基本參數
			if err != nil {
				log.Fatal().
					Err(err).
					Msg("初始化 DiscordService 失敗")
			}
		} else {
			log.Info().Msg("☑️ DiscordService is DISABLED via config.yml (missing bot_token)")
		}

		// 2. 初始化 SSE 控制器和服務 (需要在通知服務之前初始化)
		sseController := controller.NewSSEController(log.Logger)
		sseService := service.NewSSEService(log.Logger, sseController)

		// 3. 初始化 OrderService
		orderService = service.NewOrderService(log.Logger, services.MongoDB, services.RabbitMQ, googleService, crawlerService, eventManager)

		// 4. 創建統一的通知服務（Worker Pool 模式）
		// 設定 3 個 worker，隊列大小 100
		// 注意：discordEventHandler 和 lineEventHandler 會在稍後初始化
		notificationService := service.NewNotificationService(
			log.Logger,
			orderService,
			nil,                                    // Discord 事件處理器（稍後設定）
			nil,                                    // LINE 事件處理器（稍後設定）
			service.NewSSEEventManager(sseService), // SSE 事件管理器
			eventManager,                           // Redis 事件管理器
			3,                                      // worker 數量
			100,                                    // 隊列大小
		)

		// 5. 將 orderService 注入回 discordService
		if discordService != nil {
			discordService.SetOrderService(orderService)
		}

		// 6. 初始化 Discord 事件處理器
		if discordService != nil && services.Redis != nil {
			discordEventHandler = service.NewDiscordEventHandler(log.Logger, eventManager, discordService, orderService)
			// 將 Discord 事件處理器設定到通知服務
			notificationService.SetDiscordEventHandler(discordEventHandler)
			log.Info().Msg("Discord 事件處理器已初始化並設定到通知服務")
		}

		// 7. 初始化 LINE Bot 服務
		log.Info().
			Bool("line_enabled", infra.AppConfig.LINE.Enabled).
			Int("line_configs_count", len(infra.AppConfig.LINE.Configs)).
			Msg("檢查 LINE 配置")

		if infra.AppConfig.LINE.Enabled && len(infra.AppConfig.LINE.Configs) > 0 {
			// 轉換配置格式
			var lineConfigs []*service.LineConfig
			for _, configYAML := range infra.AppConfig.LINE.Configs {
				log.Info().
					Str("config_id", configYAML.ID).
					Str("config_name", configYAML.Name).
					Bool("config_enabled", configYAML.Enabled).
					Msg("處理 LINE 配置")

				if configYAML.Enabled {
					lineConfig := &service.LineConfig{
						ID:            configYAML.ID,
						Name:          configYAML.Name,
						ChannelSecret: configYAML.ChannelSecret,
						ChannelToken:  configYAML.ChannelToken,
						Enabled:       configYAML.Enabled,
						PushTriggers:  configYAML.PushTriggers,
					}
					lineConfigs = append(lineConfigs, lineConfig)
					log.Info().
						Str("config_id", configYAML.ID).
						Msg("已添加到 LineService 配置")
				}
			}

			if len(lineConfigs) > 0 {
				var err error
				lineService, err = service.NewLineService(log.Logger, lineConfigs, orderService)
				if err != nil {
					log.Error().
						Err(err).
						Msg("初始化 LINE 服務失敗")
				} else {
					log.Info().
						Int("configs_count", len(lineConfigs)).
						Msg("LINE Bot 服務已初始化")

					// 8. 初始化 LINE 事件處理器
					if services.Redis != nil {
						lineEventHandler = service.NewLineEventHandler(log.Logger, eventManager, lineService, orderService)
						// 將 LINE 事件處理器設定到通知服務
						notificationService.SetLineEventHandler(lineEventHandler)
						log.Info().Msg("LINE 事件處理器已初始化並設定到通知服務")
					}
				}
			}
		} else {
			log.Info().Msg("☑️ LINE Bot 服務 is DISABLED via config.yml")
		}

		trafficUsageLogService := service.NewTrafficUsageLogService(log.Logger, services.MongoDB)
		userService := service.NewUserService(log.Logger, services.MongoDB, infra.AppConfig.JWT.SecretKey, infra.AppConfig.JWT.ExpiresHours)
		roleService := service.NewRoleService(log.Logger, services.MongoDB)

		// 初始化系統角色
		if err := roleService.InitializeSystemRoles(context.Background()); err != nil {
			log.Error().
				Err(err).
				Msg("初始化系統角色失敗")
		} else {
			log.Info().Msg("系統角色初始化完成")
		}

		// 初始化聊天系統 MongoDB 集合
		if err := infra.InitializeChatCollections(log.Logger, services.MongoDB.Database); err != nil {
			log.Error().
				Err(err).
				Msg("初始化聊天系統失敗")
		} else {
			log.Info().Msg("聊天系統 MongoDB 集合初始化完成")
		}

		// 初始化司機黑名單服務
		blacklistService := service.NewDriverBlacklistService(log.Logger, services.Redis.Client)

		driverService := service.NewDriverService(log.Logger, services.MongoDB, infra.AppConfig.JWT.SecretKey, infra.AppConfig.JWT.ExpiresHours, orderService, googleService, crawlerService, trafficUsageLogService, blacklistService, eventManager, notificationService)

		// 設定司機服務依賴到訂單服務（避免循環依賴）
		orderService.SetDriverService(driverService)
		// 設定 FCM 服務依賴到訂單服務
		orderService.SetFCMService(fcmService)
		// 設定司機服務依賴到 Discord 服務（支援 reset-driver 指令）
		if discordService != nil {
			discordService.SetDriverService(driverService)
		}

		// 文件存儲服務
		uploadPath := "./uploads"                                         // 可以從配置文件讀取
		baseURL := fmt.Sprintf("%s/uploads", infra.AppConfig.CertBaseURL) // 使用配置文件中的域名
		fileStorageService := service.NewFileStorageService(log.Logger, uploadPath, baseURL)

		// 初始化上傳目錄
		if err := fileStorageService.InitializeUploadDirectories(); err != nil {
			log.Error().
				Err(err).
				Msg("初始化文件上傳目錄失敗")
		} else {
			log.Info().Msg("文件上傳目錄初始化完成")
		}

		// 聊天服務和控制器
		chatService := service.NewChatService(log.Logger, services.MongoDB.Database, orderService, driverService, fileStorageService, discordService, infra.AppConfig.CertBaseURL)
		chatController := controller.NewChatController(log.Logger, chatService)

		// 設置 Discord 服務的依賴（避免循環依賴）
		discordService.SetChatService(chatService)
		discordService.SetFCMService(fcmService)
		discordService.SetNotificationService(notificationService)

		// WebSocket控制器
		webSocketController := controller.NewWebSocketController(log.Logger, driverService, userService, chatController, infra.AppConfig.JWT.SecretKey)

		// Auth Middleware
		driverAuthMiddleware := authMiddleware.NewDriverAuthMiddleware(driverService, infra.AppConfig.JWT.SecretKey)
		userAuthMiddleware := authMiddleware.NewUserAuthMiddleware(userService, infra.AppConfig.JWT.SecretKey)

		orderController := controller.NewOrderController(log.Logger, orderService, driverService, userAuthMiddleware, notificationService)

		// 創建 OrderScheduleService 和 Controller
		orderScheduleService := service.NewOrderScheduleService(log.Logger, orderService, driverService)
		orderScheduleController := controller.NewOrderScheduleController(log.Logger, orderScheduleService, driverAuthMiddleware, userAuthMiddleware)

		// 創建 OrderSummaryService
		orderSummaryService := service.NewOrderSummaryService(log.Logger, services.MongoDB)
		orderSummaryController := controller.NewOrderSummaryController(log.Logger, orderSummaryService, userAuthMiddleware)
		driverController := controller.NewDriverController(log.Logger, driverService, orderService, orderScheduleService, driverAuthMiddleware, fileStorageService, baseURL)
		userController := controller.NewUserController(log.Logger, userService, orderService, userAuthMiddleware)
		authController := controller.NewAuthController(log.Logger, userService, driverService)
		crawlerController := controller.NewCrawlerController(log.Logger, crawlerService)
		roleController := controller.NewRoleController(log.Logger, roleService, userAuthMiddleware)

		// 建立 LINE Controller 配置 (只有當 LINE 服務啟用時)
		var lineController *controller.LineController
		if infra.AppConfig.LINE.Enabled && len(infra.AppConfig.LINE.Configs) > 0 {
			var lineControllerConfigs []*controller.LineConfig
			for _, configYAML := range infra.AppConfig.LINE.Configs {
				if configYAML.Enabled { // 只載入啟用的配置
					lineControllerConfig := &controller.LineConfig{
						ID:            configYAML.ID,
						Name:          configYAML.Name,
						ChannelSecret: configYAML.ChannelSecret,
						ChannelToken:  configYAML.ChannelToken,
					}
					lineControllerConfigs = append(lineControllerConfigs, lineControllerConfig)
				}
			}

			if len(lineControllerConfigs) > 0 {
				lineController = controller.NewLineController(log.Logger, driverService, lineService, orderService, notificationService, lineControllerConfigs)
				log.Info().
					Int("controller_configs_count", len(lineControllerConfigs)).
					Msg("LINE Controller 已初始化")
			}
		}

		// === TrafficUsageLog Controller ===
		trafficUsageLogController := controller.NewTrafficUsageLogController(log.Logger, trafficUsageLogService)
		trafficUsageLogController.RegisterRoutes(api)

		orderController.RegisterRoutes(api)
		orderScheduleController.RegisterRoutes(api)
		orderSummaryController.RegisterRoutes(api)

		driverController.RegisterRoutes(api)
		userController.RegisterRoutes(api)
		authController.RegisterRoutes(api)
		crawlerController.RegisterRoutes(api)
		roleController.RegisterRoutes(api)

		// 只在 LINE Controller 存在時註冊路由
		if lineController != nil {
			lineController.RegisterRoutes(api)
		}

		chatController.RegisterRoutes(api)

		// 註冊WebSocket路由
		webSocketController.RegisterRoutes(api)

		// 直接在Chi路由器上註冊WebSocket端點
		router.HandleFunc("/ws/driver", webSocketController.GetWebSocketHandler())

		// 註冊SSE端點
		router.HandleFunc("/sse/events", sseController.GetSSEHandler())

		// 註冊 Prometheus metrics 端點（使用標準 Prometheus client）
		router.Handle("/metrics", otelMiddleware.GetStandardPrometheusHandler())

		// 設定靜態檔案服務 (舊路徑已遷移到 uploads/certificate/)
		// fs := http.FileServer(http.Dir("./download-pickup-certificate"))
		// router.Handle("/files/*", http.StripPrefix("/files/", fs))

		// 設定聊天文件上傳的靜態檔案服務
		uploadFS := http.FileServer(http.Dir(uploadPath))
		router.Handle("/uploads/*", http.StripPrefix("/uploads/", uploadFS))

		// === Google Controller ===
		googleController := controller.NewGoogleMapController(log.Logger, googleService)
		googleController.RegisterRoutes(api)

		// === Google Place Cache Controller ===
		// googlePlaceCacheService 已在上面創建，直接使用
		googlePlaceCacheController := controller.NewGooglePlaceCacheController(log.Logger, googlePlaceCacheService)
		googlePlaceCacheController.RegisterRoutes(api)

		// === Dashboard Controller ===
		dashboardService := service.NewDashboardService(log.Logger, services.MongoDB, driverService)
		dashboardController := controller.NewDashboardController(log.Logger, dashboardService)
		dashboardController.RegisterRoutes(api)

		// === Develop Controller ===
		developController := controller.NewDevelopController(log.Logger, orderService)
		developController.RegisterRoutes(api)

		// === Admin Controller ===
		adminController := controller.NewAdminController(log.Logger, driverService)
		adminController.RegisterRoutes(api)

		// 啟動 Dispatcher（包含事件驅動調度和黑名單服務）
		bgDispatcher := background.NewDispatcher(log.Logger, services.MongoDB, services.RabbitMQ, crawlerService, orderService, fcmService, trafficUsageLogService, blacklistService, services.Redis.Client, notificationService)

		// 初始化 ScheduledDispatcher（預約單派送器）
		scheduledDispatcher := background.NewScheduledDispatcher(
			log.Logger,
			services.MongoDB,
			services.RabbitMQ,
			crawlerService,
			orderService,
			fcmService,
			trafficUsageLogService,
			blacklistService,
			notificationService,
		)

		log.Info().Msg("ScheduledDispatcher 已初始化")

		// 設置 Discord 事件處理器到 NotificationService
		if discordEventHandler != nil && notificationService != nil {
			notificationService.SetDiscordEventHandler(discordEventHandler)
			log.Info().Msg("Discord 事件處理器已設置到 NotificationService")
		}

		go bgDispatcher.Start(context.Background())
		go scheduledDispatcher.Start(context.Background())

		// 啟動 Redis 自動清理監聽器
		if bgDispatcher.EventManager != nil {
			go bgDispatcher.EventManager.StartCleanupWatcher(context.Background())
			log.Info().Msg("Redis 自動清理監聽器已啟動")
		}

		// 啟動 Discord 事件處理器
		if discordEventHandler != nil {
			discordEventHandler.Start()
			log.Info().Msg("Discord 事件處理器已啟動")
		}

		// 啟動 LINE 事件處理器
		if lineEventHandler != nil {
			lineEventHandler.Start()
			log.Info().Msg("LINE 事件處理器已啟動")
		}

		// 啟動統一通知服務
		if notificationService != nil {
			notificationService.Start()
			log.Info().Msg("統一通知服務已啟動")
		}

		// 啟動 metrics 更新器
		go func() {
			ticker := time.NewTicker(30 * time.Second) // 每30秒更新一次
			defer ticker.Stop()

			for range ticker.C {
				// 更新 WebSocket 連接統計
				wsStats := webSocketController.GetStats()
				if wsStats != nil {
					otelMiddleware.UpdateWebSocketConnections(
						wsStats.ConnectionsByType,
						wsStats.ConnectionsByFleet,
					)
				}

				// 更新在線司機統計
				fleetCounts, err := dashboardService.GetOnlineDriverStatsByFleet(context.Background())
				if err == nil && fleetCounts != nil {
					otelMiddleware.UpdateOnlineDrivers(fleetCounts)
				} else {
					log.Error().Err(err).Msg("獲取車隊線上司機統計失敗")
				}

				// 檢查基礎設施健康狀態
				// MongoDB 健康檢查
				mongoStart := time.Now()
				mongoErr := services.MongoDB.Client.Ping(context.Background(), nil)
				mongoLatency := float64(time.Since(mongoStart).Nanoseconds()) / 1e6
				otelMiddleware.UpdateInfrastructureHealth("database", "mongodb", mongoErr == nil, mongoLatency)

				// Redis 健康檢查
				redisStart := time.Now()
				redisErr := services.Redis.Client.Ping(context.Background()).Err()
				redisLatency := float64(time.Since(redisStart).Nanoseconds()) / 1e6
				otelMiddleware.UpdateInfrastructureHealth("cache", "redis", redisErr == nil, redisLatency)

				// RabbitMQ 健康檢查
				rabbitStart := time.Now()
				// 簡單檢查連接是否還活著
				rabbitHealthy := services.RabbitMQ.Connection != nil && !services.RabbitMQ.Connection.IsClosed()
				rabbitLatency := float64(time.Since(rabbitStart).Nanoseconds()) / 1e6
				otelMiddleware.UpdateInfrastructureHealth("queue", "rabbitmq", rabbitHealthy, rabbitLatency)
			}
		}()
		log.Info().Msg("Metrics 更新器已啟動")

		huma.Register(api, huma.Operation{
			OperationID: "health-check",
			Method:      "GET",
			Path:        "/health",
			Summary:     "健康檢查",
			Tags:        []string{"system"},
		}, func(ctx context.Context, input *struct{}) (*struct {
			Body struct {
				Status  string `json:"status" example:"ok"`
				Message string `json:"message" example:"服務運行正常"`
			}
		}, error) {
			resp := &struct {
				Body struct {
					Status  string `json:"status" example:"ok"`
					Message string `json:"message" example:"服務運行正常"`
				}
			}{}
			resp.Body.Status = "ok"
			resp.Body.Message = "Right API服務運行正常"
			return resp, nil
		})

		// MongoDB 監控端點
		huma.Register(api, huma.Operation{
			OperationID: "mongodb-monitoring",
			Method:      "GET",
			Path:        "/api/monitoring/mongodb",
			Summary:     "MongoDB 健康狀態監控",
			Tags:        []string{"monitoring"},
		}, func(ctx context.Context, input *struct{}) (*struct {
			Body struct {
				Status  string  `json:"status" example:"healthy"`
				Latency float64 `json:"latency" example:"1.23"`
				Message string  `json:"message" example:"MongoDB 連接正常"`
			}
		}, error) {
			start := time.Now()
			err := services.MongoDB.Client.Ping(ctx, nil)
			latency := float64(time.Since(start).Nanoseconds()) / 1e6

			resp := &struct {
				Body struct {
					Status  string  `json:"status" example:"healthy"`
					Latency float64 `json:"latency" example:"1.23"`
					Message string  `json:"message" example:"MongoDB 連接正常"`
				}
			}{}

			if err != nil {
				resp.Body.Status = "unhealthy"
				resp.Body.Latency = latency
				resp.Body.Message = fmt.Sprintf("MongoDB 連接失敗: %v", err)
			} else {
				resp.Body.Status = "healthy"
				resp.Body.Latency = latency
				resp.Body.Message = "MongoDB 連接正常"
			}
			return resp, nil
		})

		// Redis 監控端點
		huma.Register(api, huma.Operation{
			OperationID: "redis-monitoring",
			Method:      "GET",
			Path:        "/api/monitoring/redis",
			Summary:     "Redis 健康狀態監控",
			Tags:        []string{"monitoring"},
		}, func(ctx context.Context, input *struct{}) (*struct {
			Body struct {
				Status  string  `json:"status" example:"healthy"`
				Latency float64 `json:"latency" example:"0.45"`
				Message string  `json:"message" example:"Redis 連接正常"`
			}
		}, error) {
			start := time.Now()
			var err error
			if services.Redis != nil {
				err = services.Redis.Client.Ping(ctx).Err()
			} else {
				err = fmt.Errorf("Redis 服務未啟用")
			}
			latency := float64(time.Since(start).Nanoseconds()) / 1e6

			resp := &struct {
				Body struct {
					Status  string  `json:"status" example:"healthy"`
					Latency float64 `json:"latency" example:"0.45"`
					Message string  `json:"message" example:"Redis 連接正常"`
				}
			}{}

			if err != nil {
				resp.Body.Status = "unhealthy"
				resp.Body.Latency = latency
				resp.Body.Message = fmt.Sprintf("Redis 連接失敗: %v", err)
			} else {
				resp.Body.Status = "healthy"
				resp.Body.Latency = latency
				resp.Body.Message = "Redis 連接正常"
			}
			return resp, nil
		})

		// RabbitMQ 監控端點
		huma.Register(api, huma.Operation{
			OperationID: "rabbitmq-monitoring",
			Method:      "GET",
			Path:        "/api/monitoring/rabbitmq",
			Summary:     "RabbitMQ 健康狀態監控",
			Tags:        []string{"monitoring"},
		}, func(ctx context.Context, input *struct{}) (*struct {
			Body struct {
				Status  string  `json:"status" example:"healthy"`
				Latency float64 `json:"latency" example:"2.1"`
				Message string  `json:"message" example:"RabbitMQ 連接正常"`
			}
		}, error) {
			start := time.Now()
			var healthy bool
			var err error

			if services.RabbitMQ != nil && services.RabbitMQ.Connection != nil {
				healthy = !services.RabbitMQ.Connection.IsClosed()
				if !healthy {
					err = fmt.Errorf("RabbitMQ 連接已關閉")
				}
			} else {
				err = fmt.Errorf("RabbitMQ 服務未啟用或未連接")
			}
			latency := float64(time.Since(start).Nanoseconds()) / 1e6

			resp := &struct {
				Body struct {
					Status  string  `json:"status" example:"healthy"`
					Latency float64 `json:"latency" example:"2.1"`
					Message string  `json:"message" example:"RabbitMQ 連接正常"`
				}
			}{}

			if err != nil {
				resp.Body.Status = "unhealthy"
				resp.Body.Latency = latency
				resp.Body.Message = fmt.Sprintf("RabbitMQ 連接失敗: %v", err)
			} else {
				resp.Body.Status = "healthy"
				resp.Body.Latency = latency
				resp.Body.Message = "RabbitMQ 連接正常"
			}
			return resp, nil
		})

		hooks.OnStart(func() {
			log.Info().
				Int("port", options.Port).
				Str("docs_url", fmt.Sprintf("%s/docs", serverURL)).
				Msg("API文檔已啟用")
			log.Info().
				Int("port", options.Port).
				Str("openapi_url", fmt.Sprintf("%s/openapi.json", serverURL)).
				Msg("OpenAPI規格已啟用")
			server := &http.Server{
				Addr:    fmt.Sprintf(":%d", options.Port),
				Handler: router,
			}
			go func() {
				if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
					log.Fatal().
						Err(err).
						Msg("服務器啟動失敗")
				}
			}()
			quit := make(chan os.Signal, 1)
			signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
			<-quit
			log.Info().Msg("正在關閉服務器...")
			ctx, cancel := context.WithTimeout(context.Background(), 30)
			defer cancel()
			if err := server.Shutdown(ctx); err != nil {
				log.Error().
					Err(err).
					Msg("服務器關閉錯誤")
			}
			if crawlerService != nil {
				if err := crawlerService.Close(); err != nil {
					log.Error().
						Err(err).
						Msg("關閉 CrawlerService 失敗")
				}
			}
			if discordEventHandler != nil {
				log.Info().Msg("正在停止 Discord 事件處理器...")
				discordEventHandler.Stop()
			}
			if discordService != nil {
				log.Info().Msg("正在關閉 Discord 服務...")
				discordService.Close()
			}
			if lineEventHandler != nil {
				log.Info().Msg("正在停止 LINE 事件處理器...")
				lineEventHandler.Stop()
			}
			if lineService != nil {
				log.Info().Msg("正在關閉 LINE 服務...")
				lineService.Close()
			}
			if notificationService != nil {
				log.Info().Msg("正在停止統一通知服務...")
				notificationService.Stop()
			}
			// 清理 OpenTelemetry resources
			if otelCleanup != nil {
				log.Info().Msg("正在關閉 OpenTelemetry...")
				otelCleanup()
			}
			cleanupServices(services)
			log.Info().Msg("服務器已關閉")
		})
	})
	cli.Run()
}

func initializeServices(options *Options) (*AppServices, error) {
	mongoConfig := infra.MongoConfig{
		URI:      infra.AppConfig.MongoDB.URI,
		Database: infra.AppConfig.MongoDB.Database,
	}
	mongoDB, err := infra.NewMongoDB(mongoConfig)
	if err != nil {
		return nil, fmt.Errorf("MongoDB初始化失敗: %w", err)
	}

	redisConfig := infra.RedisConfig{
		Addr:     infra.AppConfig.Redis.Addr,
		Password: infra.AppConfig.Redis.Password,
		DB:       infra.AppConfig.Redis.DB,
	}
	redisClient, err := infra.NewRedis(redisConfig)
	if err != nil {
		log.Error().
			Err(err).
			Msg("Redis連接失敗 (繼續運行)")
		redisClient = nil
	}

	rabbitConfig := infra.RabbitMQConfig{
		URL: infra.AppConfig.RabbitMQ.URL,
	}
	rabbitMQ, err := infra.NewRabbitMQ(rabbitConfig)
	if err != nil {
		log.Error().
			Err(err).
			Msg("RabbitMQ連接失敗 (繼續運行)")
		rabbitMQ = nil
	}

	return &AppServices{
		MongoDB:  mongoDB,
		Redis:    redisClient,
		RabbitMQ: rabbitMQ,
	}, nil
}

func cleanupServices(services *AppServices) {
	if services.MongoDB != nil {
		ctx := context.Background()
		if err := services.MongoDB.Close(ctx); err != nil {
			log.Error().
				Err(err).
				Msg("MongoDB關閉錯誤")
		}
	}

	if services.Redis != nil {
		if err := services.Redis.Close(); err != nil {
			log.Error().
				Err(err).
				Msg("Redis關閉錯誤")
		}
	}

	if services.RabbitMQ != nil {
		if err := services.RabbitMQ.Close(); err != nil {
			log.Error().
				Err(err).
				Msg("RabbitMQ關閉錯誤")
		}
	}
}
