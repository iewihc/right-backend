package service

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

var testService *CrawlerService
var testLogger zerolog.Logger

// TestMain 設置測試環境，只建立一次瀏覽器實例供所有測試使用
func TestMain(m *testing.M) {
	// 設置測試用的 logger
	testLogger = zerolog.New(zerolog.ConsoleWriter{Out: os.Stdout}).With().Timestamp().Logger()
	testLogger.Info().Msg("開始建立測試用 CrawlerService...")

	// 使用測試專用構造函數
	var err error
	testService, err = NewCrawlerServiceForTesting(testLogger, nil)
	if err != nil {
		testLogger.Fatal().Err(err).Msg("建立測試用 CrawlerService 失敗")
	}

	testLogger.Info().Msg("CrawlerService 建立成功，瀏覽器將保持開啟狀態供所有測試使用")

	// 執行所有測試
	exitCode := m.Run()

	// 測試完成後詢問是否關閉瀏覽器
	testLogger.Info().Msg("所有測試完成。按 Enter 鍵關閉瀏覽器並結束...")
	var input string
	_, _ = fmt.Scanln(&input) // 等待用戶按鍵

	// 關閉服務
	if testService != nil {
		testService.Close()
		testLogger.Info().Msg("CrawlerService 已關閉")
	}

	os.Exit(exitCode)
}

// TestGetGoogleMapsDirections 測試基本路線查詢
func TestGetGoogleMapsDirections(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	testCases := []struct {
		name  string
		start string
		end   string
	}{
		{"台北到松山機場", "台北車站", "松山機場"},
		{"信義到大安", "信義區", "大安區"},
		{"板橋到淡水", "板橋車站", "淡水老街"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			testLogger.Info().Str("test", tc.name).Str("start", tc.start).Str("end", tc.end).Msg("開始測試路線查詢")

			routes, err := testService.GetGoogleMapsDirections(ctx, tc.start, tc.end)
			if err != nil {
				t.Fatalf("路線查詢失敗: %v", err)
			}

			if len(routes) == 0 {
				t.Fatal("未找到任何路線")
			}

			// 驗證結果
			for i, route := range routes {
				t.Logf("路線 %d: %s, 時間: %s, 距離: %s, 分鐘數: %d",
					i+1, route.Title, route.Time, route.Distance, route.TimeInMinutes)

				if route.TimeInMinutes <= 0 {
					t.Errorf("路線 %d 時間解析失敗: %d 分鐘", i+1, route.TimeInMinutes)
				}
			}

			testLogger.Info().Str("test", tc.name).Int("routes_count", len(routes)).Msg("路線查詢測試完成")
		})
	}
}

// TestGetAllDirections 測試獲取所有路線
func TestGetAllDirections(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	testCases := []struct {
		name        string
		origin      string
		destination string
	}{
		{"台北到桃園機場", "台北車站", "桃園機場"},
		{"西門町到101", "西門町", "台北101"},
		{"中山到士林", "中山區", "士林夜市"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			testLogger.Info().Str("test", tc.name).Str("origin", tc.origin).Str("destination", tc.destination).Msg("開始測試多路線查詢")

			routes, err := testService.GetAllDirections(ctx, tc.origin, tc.destination)
			if err != nil {
				t.Fatalf("多路線查詢失敗: %v", err)
			}

			if len(routes) == 0 {
				t.Fatal("未找到任何路線")
			}

			t.Logf("找到 %d 條路線", len(routes))
			for i, route := range routes {
				t.Logf("路線 %d: %s, 時間: %s, 距離: %s, 分鐘數: %d, 公里數: %.2f",
					i+1, route.Title, route.Time, route.Distance, route.TimeInMinutes, route.DistanceKm)

				if route.TimeInMinutes <= 0 {
					t.Errorf("路線 %d 時間解析失敗", i+1)
				}
				if route.DistanceKm <= 0 {
					t.Errorf("路線 %d 距離解析失敗", i+1)
				}
			}

			testLogger.Info().Str("test", tc.name).Int("routes_count", len(routes)).Msg("多路線查詢測試完成")
		})
	}
}

// TestDirectionsMatrixInverse 測試司機到客戶路線矩陣
func TestDirectionsMatrixInverse(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	driverLocations := []string{
		"台北車站",
		"西門町",
		"中山區",
	}
	pickupLocation := "信義區"

	testLogger.Info().Strs("driver_locations", driverLocations).Str("pickup_location", pickupLocation).Msg("開始測試司機到客戶路線矩陣")

	routes, err := testService.DirectionsMatrixInverse(ctx, driverLocations, pickupLocation)
	if err != nil {
		t.Fatalf("司機到客戶路線矩陣查詢失敗: %v", err)
	}

	if len(routes) == 0 {
		t.Fatal("未找到任何司機到客戶的路線")
	}

	expectedRoutes := len(driverLocations)
	if len(routes) != expectedRoutes {
		t.Logf("警告: 預期 %d 條路線，實際獲得 %d 條路線", expectedRoutes, len(routes))
	}

	for i, route := range routes {
		t.Logf("司機路線 %d: %s", i+1, route.Title)
		t.Logf("  時間: %s (%d 分鐘)", route.Time, route.TimeInMinutes)
		t.Logf("  距離: %s (%.2f 公里)", route.Distance, route.DistanceKm)

		if route.TimeInMinutes <= 0 {
			t.Errorf("路線 %d 時間解析失敗", i+1)
		}
		if route.DistanceKm <= 0 {
			t.Errorf("路線 %d 距離解析失敗", i+1)
		}
	}

	testLogger.Info().Int("routes_count", len(routes)).Msg("司機到客戶路線矩陣測試完成")
}

// TestCrawlerServiceWithCustomConfig 展示如何使用自定義配置
func TestCrawlerServiceWithCustomConfig(t *testing.T) {
	t.Skip("跳過自定義配置測試，因為已在 TestMain 中建立了共用實例")

	logger := zerolog.New(zerolog.ConsoleWriter{Out: log.Logger}).With().Timestamp().Logger()

	// 自定義配置：無頭模式但跳過預熱
	config := CrawlerConfig{
		IsDebug:      false, // 無頭模式（更快）
		SkipWarmup:   true,  // 跳過預熱（測試用）
		PoolSize:     2,     // 小型頁面池
		DisableCache: true,  // 測試時禁用快取
	}

	service, err := NewCrawlerServiceWithConfig(logger, nil, config)
	if err != nil {
		t.Fatalf("建立自定義配置 CrawlerService 失敗: %v", err)
	}
	defer service.Close()

	// 進行測試...
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	routes, err := service.GetAllDirections(ctx, "信義區", "大安區")
	if err != nil {
		t.Fatalf("多路線查詢失敗: %v", err)
	}

	t.Logf("找到 %d 條路線", len(routes))
}

// TestCrawlerStressTest 壓力測試（可選）
func TestCrawlerStressTest(t *testing.T) {
	if testing.Short() {
		t.Skip("跳過壓力測試（短模式）")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second) // 5分鐘
	defer cancel()

	locations := [][]string{
		{"台北車站", "松山機場"},
		{"信義區", "大安區"},
		{"板橋", "淡水"},
		{"中山", "士林"},
		{"西門町", "101"},
	}

	testLogger.Info().Msg("開始壓力測試 - 連續查詢多個路線")

	for i, loc := range locations {
		testLogger.Info().Int("iteration", i+1).Strs("locations", loc).Msg("壓力測試進行中")

		routes, err := testService.GetGoogleMapsDirections(ctx, loc[0], loc[1])
		if err != nil {
			t.Errorf("壓力測試第 %d 次查詢失敗: %v", i+1, err)
			continue
		}

		if len(routes) == 0 {
			t.Errorf("壓力測試第 %d 次查詢無結果", i+1)
		} else {
			t.Logf("壓力測試第 %d 次查詢成功: 找到 %d 條路線", i+1, len(routes))
		}
	}

	testLogger.Info().Msg("壓力測試完成")
}
