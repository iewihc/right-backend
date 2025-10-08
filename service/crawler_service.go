package service

import (
	"context"
	"encoding/json"
	"fmt"
	"right-backend/infra"
	"right-backend/utils"
	"strings"
	"sync"
	"time"

	"github.com/playwright-community/playwright-go"
	"github.com/rs/zerolog"
)

// isDebug 模式開關：設定為 true 以顯示瀏覽器進行除錯，設定為 false 則在背景執行以獲得最佳效能。
const isDebug = false
const redisKeyPrefix = "crawler:v1:gmaps" // Redis 快取鍵前綴，方便版本管理

// CrawlerConfig 爬蟲服務配置
type CrawlerConfig struct {
	IsDebug      bool // 是否顯示瀏覽器
	SkipWarmup   bool // 是否跳過預熱
	PoolSize     int  // 頁面池大小
	DisableCache bool // 是否禁用快取
}

// 預熱頁面池
type PagePool struct {
	searchPages     chan playwright.Page
	detailsPages    chan playwright.Page
	directionsPages chan playwright.Page
	browser         playwright.Browser
	mu              sync.Mutex
	isWarming       bool
}

// CrawlerService 負責執行爬蟲相關的腳本，並持有一個共用的瀏覽器實例以提高效能
type CrawlerService struct {
	logger      zerolog.Logger
	pw          *playwright.Playwright
	browser     playwright.Browser
	redisClient *infra.Redis
	pagePool    *PagePool
	config      CrawlerConfig
}

// NewCrawlerService 建立一個新的 CrawlerService，並初始化一個長期的 Playwright 和瀏覽器實例
func NewCrawlerService(logger zerolog.Logger, redisClient *infra.Redis) (*CrawlerService, error) {
	// 使用默認生產配置
	config := CrawlerConfig{
		IsDebug:      isDebug,
		SkipWarmup:   false,
		PoolSize:     2,
		DisableCache: false,
	}
	return NewCrawlerServiceWithConfig(logger, redisClient, config)
}

// NewCrawlerServiceForTesting 創建適合測試的 CrawlerService
// 特點：顯示瀏覽器、跳過預熱、禁用快取、單一頁面池
func NewCrawlerServiceForTesting(logger zerolog.Logger, redisClient *infra.Redis) (*CrawlerService, error) {
	config := CrawlerConfig{
		IsDebug:      true, // 顯示瀏覽器
		SkipWarmup:   true, // 跳過預熱
		PoolSize:     1,    // 單一頁面
		DisableCache: true, // 禁用快取
	}
	return NewCrawlerServiceWithConfig(logger, redisClient, config)
}

// NewCrawlerServiceWithConfig 使用自定義配置建立 CrawlerService（適合測試使用）
func NewCrawlerServiceWithConfig(logger zerolog.Logger, redisClient *infra.Redis, config CrawlerConfig) (*CrawlerService, error) {
	pw, err := playwright.Run()
	if err != nil {
		return nil, err
	}

	// 極致優化瀏覽器啟動選項
	browser, err := pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
		Headless: playwright.Bool(!config.IsDebug),
		Args: []string{
			"--disable-blink-features=AutomationControlled",
			"--disable-web-security",
			"--disable-features=VizDisplayCompositor",
			"--disable-extensions",
			"--disable-plugins",
			"--disable-images",
			"--disable-background-timer-throttling",
			"--disable-backgrounding-occluded-windows",
			"--disable-renderer-backgrounding",
			"--disable-hang-monitor",
			"--disable-prompt-on-repost",
			"--disable-sync",
			"--disable-translate",
			"--disable-ipc-flooding-protection",
			"--no-first-run",
			"--no-default-browser-check",
			"--no-sandbox",
			"--disable-dev-shm-usage",
			"--disable-gpu",
			"--aggressive-cache-discard",
			"--memory-pressure-off",
			"--disable-automation",
			"--disable-infobars",
			"--disable-features=TranslateUI",
			"--disable-save-password-bubble",
			"--disable-component-extensions-with-background-pages",
			"--disable-default-apps",
			"--disable-domain-reliability",
			"--disable-features=VizDisplayCompositor",
			"--run-all-compositor-stages-before-draw",
			"--disable-features=TranslateUI",
			"--disable-ipc-flooding-protection",
		},
	})
	if err != nil {
		return nil, err
	}

	// 根據配置設定頁面池大小
	poolSize := config.PoolSize
	if config.IsDebug && poolSize > 1 {
		poolSize = 1 // Debug 模式下強制使用單一頁面
	}

	// 初始化頁面池
	pagePool := &PagePool{
		searchPages:     make(chan playwright.Page, poolSize),
		detailsPages:    make(chan playwright.Page, poolSize),
		directionsPages: make(chan playwright.Page, poolSize),
		browser:         browser,
	}

	service := &CrawlerService{
		logger:      logger.With().Str("module", "crawler_service").Logger(),
		pw:          pw,
		browser:     browser,
		redisClient: redisClient,
		pagePool:    pagePool,
		config:      config,
	}

	// 根據配置決定是否預熱頁面
	if !config.SkipWarmup {
		go service.warmupPages()
		service.logger.Info().Msg("Playwright瀏覽器實例已成功啟動並開始預熱頁面")
	} else {
		service.logger.Info().Msg("Playwright瀏覽器實例已成功啟動（跳過預熱）")
	}

	return service, nil
}

// warmupPages 預熱頁面池
func (s *CrawlerService) warmupPages() {
	s.pagePool.mu.Lock()
	if s.pagePool.isWarming {
		s.pagePool.mu.Unlock()
		return
	}
	s.pagePool.isWarming = true
	s.pagePool.mu.Unlock()

	defer func() {
		s.pagePool.mu.Lock()
		s.pagePool.isWarming = false
		s.pagePool.mu.Unlock()
	}()

	if s.config.IsDebug {
		s.logger.Debug().Msg("Debug模式：只預熱一個搜尋頁面")
		if page, err := s.createPrewarmedSearchPage(); err == nil {
			select {
			case s.pagePool.searchPages <- page:
			default:
				page.Close()
			}
		}
		s.logger.Debug().Msg("單一頁面預熱完成")
		return
	}

	// 正常模式：預熱所有頁面
	poolSize := 3
	var wg sync.WaitGroup
	wg.Add(3)

	// 預熱搜尋頁面
	go func() {
		defer wg.Done()
		for i := 0; i < poolSize; i++ {
			if page, err := s.createPrewarmedSearchPage(); err == nil {
				select {
				case s.pagePool.searchPages <- page:
				default:
					page.Close()
				}
			}
		}
	}()

	// 預熱詳情頁面
	go func() {
		defer wg.Done()
		for i := 0; i < poolSize; i++ {
			if page, err := s.createPrewarmedDetailsPage(); err == nil {
				select {
				case s.pagePool.detailsPages <- page:
				default:
					page.Close()
				}
			}
		}
	}()

	// 預熱路線頁面
	go func() {
		defer wg.Done()
		for i := 0; i < poolSize; i++ {
			if page, err := s.createPrewarmedDirectionsPage(); err == nil {
				select {
				case s.pagePool.directionsPages <- page:
				default:
					page.Close()
				}
			}
		}
	}()

	wg.Wait()
	s.logger.Info().Msg("Crawler所有頁面預熱完成")
}

// createPrewarmedSearchPage 創建預熱的搜尋頁面
func (s *CrawlerService) createPrewarmedSearchPage() (playwright.Page, error) {
	page, err := s.createOptimizedPage()
	if err != nil {
		return nil, err
	}

	// 預載入 Google Maps 首頁
	page.Goto("https://www.google.com/maps/", playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateNetworkidle,
	})

	return page, nil
}

// createPrewarmedDetailsPage 創建預熱的詳情頁面
func (s *CrawlerService) createPrewarmedDetailsPage() (playwright.Page, error) {
	page, err := s.createOptimizedPage()
	if err != nil {
		return nil, err
	}

	// 預載入一個示例搜尋
	page.Goto("https://www.google.com/maps/search/台北車站", playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateNetworkidle,
	})

	return page, nil
}

// createPrewarmedDirectionsPage 創建預熱的路線頁面
func (s *CrawlerService) createPrewarmedDirectionsPage() (playwright.Page, error) {
	page, err := s.createOptimizedPage()
	if err != nil {
		return nil, err
	}

	// 預載入路線規劃頁面
	page.Goto("https://www.google.com/maps/dir/", playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateNetworkidle,
	})

	return page, nil
}

// getPageFromPool 從池中獲取頁面
func (s *CrawlerService) getPageFromPool(pageType string) playwright.Page {
	var pageChan chan playwright.Page
	var createFunc func() (playwright.Page, error)

	switch pageType {
	case "search":
		pageChan = s.pagePool.searchPages
		createFunc = s.createPrewarmedSearchPage
	case "details":
		pageChan = s.pagePool.detailsPages
		createFunc = s.createPrewarmedDetailsPage
	case "directions":
		pageChan = s.pagePool.directionsPages
		createFunc = s.createPrewarmedDirectionsPage
	default:
		return nil
	}

	// 嘗試從池中獲取
	select {
	case page := <-pageChan:
		// 異步補充頁面池
		go func() {
			if newPage, err := createFunc(); err == nil {
				select {
				case pageChan <- newPage:
				default:
					newPage.Close()
				}
			}
		}()
		return page
	default:
		// 池為空，創建新頁面
		if page, err := createFunc(); err == nil {
			return page
		}
		return nil
	}
}

// returnPageToPool 歸還頁面到池中
func (s *CrawlerService) returnPageToPool(page playwright.Page, pageType string) {
	var pageChan chan playwright.Page

	switch pageType {
	case "search":
		pageChan = s.pagePool.searchPages
	case "details":
		pageChan = s.pagePool.detailsPages
	case "directions":
		pageChan = s.pagePool.directionsPages
	default:
		page.Close()
		return
	}

	select {
	case pageChan <- page:
	// 成功歸還
	default:
		// 池已滿，關閉頁面
		page.Close()
	}
}

// Close 用於在應用程式關閉時，優雅地關閉瀏覽器和 Playwright
func (s *CrawlerService) Close() error {
	// 關閉所有池中的頁面
	for {
		select {
		case page := <-s.pagePool.searchPages:
			page.Close()
		case page := <-s.pagePool.detailsPages:
			page.Close()
		case page := <-s.pagePool.directionsPages:
			page.Close()
		default:
			goto cleanup
		}
	}

cleanup:
	if s.browser != nil {
		if err := s.browser.Close(); err != nil {
			s.logger.Error().Err(err).Msg("關閉瀏覽器失敗")
			return err
		}
	}
	if s.pw != nil {
		if err := s.pw.Stop(); err != nil {
			s.logger.Error().Err(err).Msg("停止 Playwright 失敗")
			return err
		}
	}
	s.logger.Info().Msg("Playwright瀏覽器實例已關閉")
	return nil
}

// LocationDetails 存儲從 Google Maps 抓取的詳細地點資訊
type LocationDetails struct {
	Query             string            `json:"query"`
	ClickedSuggestion string            `json:"clicked_suggestion,omitempty"`
	Address           string            `json:"address"`
	Website           string            `json:"website,omitempty"`
	Phone             string            `json:"phone,omitempty"`
	OpeningHours      map[string]string `json:"opening_hours,omitempty"`
	Lat               string            `json:"lat"`
	Lng               string            `json:"lng"`
	URL               string            `json:"url"`
}

// Suggestion 存儲單條搜尋建議的資訊
type Suggestion struct {
	Title    string `json:"title"`
	Subtitle string `json:"subtitle"`
	FullText string `json:"full_text"`
}

// SuggestionV2 存儲搜尋建議的名稱和地址資訊
type SuggestionV2 struct {
	Name    string `json:"name"`
	Address string `json:"address"`
}

// GeoPoint 存儲地理座標點
type GeoPoint struct {
	Lat string `json:"lat"`
	Lng string `json:"lng"`
}

// LocationDetailsV3 存儲 V3 版本的地點資訊
type LocationDetailsV3 struct {
	Address string `json:"address"`
	Lat     string `json:"lat"`
	Lng     string `json:"lng"`
}

// RouteInfo 存儲從 Google Maps 抓取的單條路線資訊
type RouteInfo struct {
	Title         string  `json:"title"`
	Time          string  `json:"time"`
	Distance      string  `json:"distance"`
	TrafficInfo   string  `json:"traffic_info,omitempty"`
	TollInfo      string  `json:"toll_info,omitempty"`
	TimeInMinutes int     `json:"time_in_minutes"`
	DistanceKm    float64 `json:"distance_km,omitempty"`
}

// createOptimizedPage 創建優化配置的頁面
func (s *CrawlerService) createOptimizedPage() (playwright.Page, error) {
	page, err := s.browser.NewPage(playwright.BrowserNewPageOptions{
		Locale:     playwright.String("zh-TW"),
		TimezoneId: playwright.String("Asia/Taipei"),
		UserAgent:  playwright.String("Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"),
	})
	if err != nil {
		return nil, err
	}

	// 設置額外的標頭來模擬真實瀏覽器
	page.SetExtraHTTPHeaders(map[string]string{
		"Accept":                    "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.9",
		"Accept-Encoding":           "gzip, deflate, br",
		"Accept-Language":           "zh-TW,zh;q=0.9,en;q=0.8",
		"Cache-Control":             "no-cache",
		"DNT":                       "1",
		"Pragma":                    "no-cache",
		"Sec-Fetch-Dest":            "document",
		"Sec-Fetch-Mode":            "navigate",
		"Sec-Fetch-Site":            "none",
		"Sec-Fetch-User":            "?1",
		"Upgrade-Insecure-Requests": "1",
	})

	// 極致阻止載入不必要的資源
	page.Route("**/*.{png,jpg,jpeg,gif,svg,woff,woff2,ttf,eot,css,ico}", func(route playwright.Route) {
		route.Abort()
	})

	// 阻止 Google Analytics 和其他追蹤腳本
	page.Route("**/analytics.js", func(route playwright.Route) {
		route.Abort()
	})
	page.Route("**/gtag/**", func(route playwright.Route) {
		route.Abort()
	})

	return page, nil
}

// GetGoogleMapsDirections 規劃兩點之間的開車路線 - 極速版本
func (s *CrawlerService) GetGoogleMapsDirections(ctx context.Context, start string, end string) ([]RouteInfo, error) {
	startTime := time.Now()
	defer func() {
		elapsed := time.Since(startTime)
		s.logger.Debug().Str("start", start).Str("end", end).Dur("elapsed", elapsed).Msg("GetGoogleMapsDirections總執行時間")
	}()

	cacheKey := fmt.Sprintf("%s:directions:%s-%s", redisKeyPrefix, start, end)
	if !s.config.DisableCache && s.redisClient != nil {
		cachedResult, err := s.redisClient.Client.Get(ctx, cacheKey).Result()
		if err == nil {
			s.logger.Debug().Str("start", start).Str("end", end).Msg("Cache HIT for directions")
			var routes []RouteInfo
			if jsonErr := json.Unmarshal([]byte(cachedResult), &routes); jsonErr == nil {
				return routes, nil
			}
		}
	}

	s.logger.Debug().Str("start", start).Str("end", end).Msg("Cache MISS for directions, running crawler")

	page := s.getPageFromPool("directions")
	if page == nil {
		s.logger.Error().Msg("無法獲取頁面")
		return nil, fmt.Errorf("無法獲取頁面")
	}
	defer s.returnPageToPool(page, "directions")

	directionsURL := fmt.Sprintf("https://www.google.com/maps/dir/%s/%s",
		strings.ReplaceAll(start, " ", "+"),
		strings.ReplaceAll(end, " ", "+"))

	page.Goto(directionsURL)

	go func() {
		page.Locator(`button[data-tooltip="開車"]`).Click()
	}()

	var routes []RouteInfo

	for i := 0; i < 20; i++ {
		routeElements, err := page.Locator("#section-directions-trip-0").All()
		if err == nil && len(routeElements) > 0 {

			for _, el := range routeElements {
				title, _ := el.Locator("h1").TextContent()
				timeText, _ := el.Locator(".Fk3sm").First().TextContent()
				distText, _ := el.Locator(".ivN21e").First().TextContent()

				if timeText != "" && distText != "" {
					routes = append(routes, RouteInfo{
						Title:         title,
						Time:          timeText,
						Distance:      distText,
						TimeInMinutes: utils.ParseTimeToMinutes(timeText),
					})
				}
			}

			if len(routes) > 0 {
				break
			}
		}
		time.Sleep(100 * time.Millisecond)
	}

	if !s.config.DisableCache && s.redisClient != nil && len(routes) > 0 {
		if jsonData, err := json.Marshal(routes); err == nil {
			s.redisClient.Client.Set(ctx, cacheKey, jsonData, 24*time.Hour)
			s.logger.Debug().Str("start", start).Str("end", end).Msg("Cache SET for directions")
		}
	}

	return routes, nil
}

// DirectionsMatrixInverse 抓取從多個司機位置到客戶上車地點的開車路線資訊
func (s *CrawlerService) DirectionsMatrixInverse(ctx context.Context, driverLocations []string, pickupLocation string) ([]RouteInfo, error) {
	// 添加 service 層的 tracing
	ctx, span := infra.StartSpan(ctx, "crawler_service_directions_matrix_inverse",
		infra.AttrOperation("directions_matrix_inverse"),
		infra.AttrInt("driver_locations_count", len(driverLocations)),
		infra.AttrString("pickup_location", pickupLocation),
		infra.AttrString("service_layer", "crawler_service"),
	)
	defer span.End()

	startTime := time.Now()
	defer func() {
		elapsed := time.Since(startTime)
		s.logger.Debug().Int("driver_count", len(driverLocations)).Str("pickup_location", pickupLocation).Dur("elapsed", elapsed).Msg("DirectionsMatrixInverse總執行時間")
	}()

	infra.AddEvent(span, "directions_matrix_inverse_service_started",
		infra.AttrInt("driver_locations_count", len(driverLocations)),
		infra.AttrString("pickup_location", pickupLocation),
	)

	s.logger.Debug().Int("driver_count", len(driverLocations)).Str("pickup_location", pickupLocation).Msg("Crawler計算司機到客戶上車地點的路線")

	page := s.getPageFromPool("directions")
	if page == nil {
		infra.RecordError(span, fmt.Errorf("無法獲取頁面"), "無法獲取crawler頁面",
			infra.AttrString("error_type", "page_unavailable"),
		)
		s.logger.Error().Msg("無法獲取頁面")
		return nil, fmt.Errorf("無法獲取頁面")
	}
	defer s.returnPageToPool(page, "directions")

	// 強制重新載入頁面，確保使用最新的交通資訊
	currentTime := time.Now()
	refreshURL := fmt.Sprintf("https://www.google.com/maps?t=%d", currentTime.Unix())
	s.logger.Debug().Str("refresh_url", refreshURL).Msg("重新載入Google Maps以獲取最新交通資訊")

	if _, err := page.Goto(refreshURL, playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateNetworkidle,
		Timeout:   playwright.Float(10000),
	}); err != nil {
		s.logger.Error().Err(err).Msg("無法重新載入 Google Maps")
		return nil, err
	}

	if err := page.Locator(`button[aria-label="路線"]`).Click(); err != nil {
		s.logger.Error().Err(err).Msg("點擊『路線』按鈕失敗")
		return nil, err
	}

	if err := page.Locator(`div[aria-label="開車"]`).Click(playwright.LocatorClickOptions{Timeout: playwright.Float(5000)}); err != nil {
		s.logger.Debug().Err(err).Msg("未找到開車模式按鈕或點擊失敗，可能已是預設模式")
	}

	// 固定目的地為客戶上車地點
	destinationInput := page.Locator(`input[aria-label*="目的地"]`).First()
	if err := destinationInput.Fill(pickupLocation); err != nil {
		s.logger.Error().Err(err).Str("pickup_location", pickupLocation).Msg("填寫客戶上車地點失敗")
		return nil, err
	}
	page.WaitForTimeout(500)

	var results []RouteInfo
	for i, driverLocation := range driverLocations {
		s.logger.Debug().Int("current", i+1).Int("total", len(driverLocations)).Str("driver_location", driverLocation).Str("pickup_location", pickupLocation).Msg("Crawler正在處理司機位置")

		// 填寫司機位置作為起點
		originInput := page.Locator(`input[aria-label*="起點"]`).First()
		if err := originInput.Fill(driverLocation); err != nil {
			s.logger.Warn().Err(err).Str("driver_location", driverLocation).Msg("填寫司機位置失敗")
			continue
		}

		// 先清空起點輸入框，強制重新計算
		if err := originInput.SelectText(); err == nil {
			originInput.Press("Delete")
		}
		page.WaitForTimeout(200)

		// 重新填寫司機位置
		if err := originInput.Fill(driverLocation); err != nil {
			s.logger.Warn().Err(err).Str("driver_location", driverLocation).Msg("重新填寫司機位置失敗")
			continue
		}

		// 在目的地輸入框按 Enter 觸發路線計算
		if err := destinationInput.Press("Enter"); err != nil {
			s.logger.Warn().Err(err).Str("driver_location", driverLocation).Str("pickup_location", pickupLocation).Msg("觸發路線計算失敗")
			continue
		}

		// 等待舊結果消失，新結果出現
		page.WaitForTimeout(800)

		// 等待路線結果出現
		firstRouteContainer := page.Locator(`div[id^="section-directions-trip-"]`).First()
		if err := firstRouteContainer.WaitFor(playwright.LocatorWaitForOptions{State: playwright.WaitForSelectorStateVisible, Timeout: playwright.Float(20000)}); err != nil {
			s.logger.Warn().Err(err).Str("driver_location", driverLocation).Msg("等待司機到客戶上車點的路線結果超時")
			continue
		}

		// 提取路線資訊
		durationStr, _ := firstRouteContainer.Locator(".Fk3sm").First().TextContent()
		distanceStr, _ := firstRouteContainer.Locator(".ivN21e").First().TextContent()

		minutes := utils.ParseTimeToMinutes(durationStr)
		kilometers := utils.ParseDistanceToKm(distanceStr)

		if minutes > 0 && kilometers > 0 {
			// 明確標示這是司機到客戶的路線
			routeTitle := fmt.Sprintf("司機位置: %s → 客戶上車點: %s", driverLocation, pickupLocation)
			results = append(results, RouteInfo{
				Title:         routeTitle,
				Time:          durationStr,
				Distance:      distanceStr,
				TimeInMinutes: minutes,
				DistanceKm:    kilometers,
			})
			s.logger.Info().Str("driver_location", driverLocation).Str("duration", durationStr).Str("distance", distanceStr).Msg("Crawler成功計算司機到客戶上車點的路線")
		} else {
			s.logger.Warn().Str("driver_location", driverLocation).Str("duration", durationStr).Str("distance", distanceStr).Msg("無法解析司機到客戶上車點的路線數據")
		}
	}

	if len(results) == 0 {
		infra.RecordError(span, fmt.Errorf("未能抓取到任何有效的路線資訊"), "路線抓取失敗",
			infra.AttrInt("driver_locations_count", len(driverLocations)),
			infra.AttrString("pickup_location", pickupLocation),
			infra.AttrString("error_type", "no_valid_routes"),
		)
		s.logger.Error().Msg("未能抓取到任何有效的路線資訊")
		return nil, fmt.Errorf("未能抓取到任何有效的路線資訊")
	}

	infra.AddEvent(span, "directions_matrix_inverse_service_completed",
		infra.AttrInt("result_count", len(results)),
		infra.AttrFloat64("total_elapsed_ms", float64(time.Since(startTime).Nanoseconds())/1e6),
	)
	infra.MarkSuccess(span,
		infra.AttrInt("driver_locations_count", len(driverLocations)),
		infra.AttrString("pickup_location", pickupLocation),
		infra.AttrInt("result_count", len(results)),
		infra.AttrString("service_layer", "crawler_service"),
	)

	s.logger.Info().Int("result_count", len(results)).Dur("total_elapsed", time.Since(startTime)).Msg("Crawler DirectionsMatrixInverse執行完畢，成功計算司機到客戶上車點的路線")
	return results, nil
}

// GetAllDirections 抓取兩點之間的所有開車路線資訊（不只第一條）
func (s *CrawlerService) GetAllDirections(ctx context.Context, origin, destination string) ([]RouteInfo, error) {
	startTime := time.Now()
	defer func() {
		elapsed := time.Since(startTime)
		s.logger.Debug().Str("origin", origin).Str("destination", destination).Dur("elapsed", elapsed).Msg("GetAllDirections總執行時間")
	}()

	// 組成 Redis key 並查詢快取
	cacheKey := fmt.Sprintf("%s:all-directions:%s-%s", redisKeyPrefix, origin, destination)
	if !s.config.DisableCache && s.redisClient != nil {
		cachedResult, err := s.redisClient.Client.Get(ctx, cacheKey).Result()
		if err == nil {
			s.logger.Debug().Str("origin", origin).Str("destination", destination).Msg("Cache HIT for all directions")
			var routes []RouteInfo
			if jsonErr := json.Unmarshal([]byte(cachedResult), &routes); jsonErr == nil {
				return routes, nil
			}
		}
	}

	s.logger.Debug().Str("origin", origin).Str("destination", destination).Msg("Cache MISS for all directions, running crawler")

	page := s.getPageFromPool("directions")
	if page == nil {
		s.logger.Error().Msg("無法獲取頁面")
		return nil, fmt.Errorf("無法獲取頁面")
	}
	defer s.returnPageToPool(page, "directions")

	if _, err := page.Goto("https://www.google.com/maps"); err != nil {
		s.logger.Error().Err(err).Msg("無法導航到 Google Maps")
		return nil, err
	}

	if err := page.Locator(`button[aria-label="路線"]`).Click(); err != nil {
		s.logger.Error().Err(err).Msg("點擊『路線』按鈕失敗")
		return nil, err
	}

	if err := page.Locator(`div[aria-label="開車"]`).Click(playwright.LocatorClickOptions{Timeout: playwright.Float(5000)}); err != nil {
		s.logger.Debug().Err(err).Msg("未找到開車模式按鈕或點擊失敗，可能已是預設模式")
	}

	originInput := page.Locator(`input[aria-label*="起點"]`).First()
	destinationInput := page.Locator(`input[aria-label*="目的地"]`).First()

	if err := originInput.Fill(origin); err != nil {
		s.logger.Error().Err(err).Msg("填寫起點失敗")
		return nil, err
	}
	if err := destinationInput.Fill(destination); err != nil {
		s.logger.Error().Err(err).Msg("填寫目的地失敗")
		return nil, err
	}

	if err := page.Locator(`button[aria-label="搜尋"]`).Last().Click(); err != nil {
		s.logger.Error().Err(err).Msg("點擊『搜尋』按鈕失敗")
		return nil, err
	}

	// 等待第一條路線結果出現
	routesContainer := page.Locator(`div[id^="section-directions-trip-"]`).First()
	if err := routesContainer.WaitFor(playwright.LocatorWaitForOptions{State: playwright.WaitForSelectorStateVisible, Timeout: playwright.Float(15000)}); err != nil {
		s.logger.Error().Err(err).Msg("等待路線結果容器超時")
		return nil, err
	}

	// 獲取所有路線
	routeLocators, err := page.Locator(`div[id^="section-directions-trip-"]`).All()
	if err != nil {
		s.logger.Error().Err(err).Msg("無法定位所有路線")
		return nil, err
	}

	var results []RouteInfo
	for i, routeLocator := range routeLocators {
		h1, _ := routeLocator.Locator("h1").TextContent()
		durationStr, _ := routeLocator.Locator(".Fk3sm").First().TextContent()
		distanceStr, _ := routeLocator.Locator(".ivN21e").First().TextContent()

		minutes := utils.ParseTimeToMinutes(durationStr)
		kilometers := utils.ParseDistanceToKm(distanceStr)

		if minutes > 0 && kilometers > 0 {
			results = append(results, RouteInfo{
				Title:         h1,
				Time:          durationStr,
				Distance:      distanceStr,
				TimeInMinutes: minutes,
				DistanceKm:    kilometers,
			})
			s.logger.Debug().Int("route_index", i+1).Str("route_title", h1).Msg("成功抓取路線")
		} else {
			s.logger.Warn().Str("route_title", h1).Str("duration", durationStr).Str("distance", distanceStr).Msg("無法解析路線數據")
		}
	}

	if !s.config.DisableCache && s.redisClient != nil && len(results) > 0 {
		if jsonData, err := json.Marshal(results); err == nil {
			s.redisClient.Client.Set(ctx, cacheKey, jsonData, 24*time.Hour)
			s.logger.Debug().Str("origin", origin).Str("destination", destination).Msg("Cache SET for all directions")
		}
	}

	if len(results) == 0 {
		s.logger.Error().Msg("未能抓取到任何有效的路線資訊")
		return nil, fmt.Errorf("未能抓取到任何有效的路線資訊")
	}

	s.logger.Info().Int("route_count", len(results)).Msg("GetAllDirections執行完畢，成功抓取路線")
	return results, nil
}
