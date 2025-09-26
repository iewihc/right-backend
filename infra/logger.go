package infra

import (
	"io"
	"os"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// InitLogger 初始化 zerolog
func InitLogger() {
	// 設定時間格式
	zerolog.TimeFieldFormat = time.RFC3339

	// 預設輸出到 console
	var writers []io.Writer

	// Console 輸出（開發環境）
	if os.Getenv("ENV") != "production" {
		consoleWriter := zerolog.ConsoleWriter{
			Out:        os.Stdout,
			TimeFormat: "2006-01-02 15:04:05",
		}
		writers = append(writers, consoleWriter)
	} else {
		// Production 環境使用 JSON 格式
		writers = append(writers, os.Stdout)
	}

	// 設定多個輸出
	multi := zerolog.MultiLevelWriter(writers...)

	// 設定全域 logger
	log.Logger = zerolog.New(multi).
		With().
		Timestamp().
		Str("service", "right-backend").
		Str("environment", getEnvironment()).
		Str("hostname", getHostname()).
		Logger()

	// 設定日誌級別
	setLogLevel()
}

// getEnvironment 獲取環境名稱
func getEnvironment() string {
	env := os.Getenv("ENV")
	if env == "" {
		return "development"
	}
	return env
}

// getHostname 獲取主機名稱
func getHostname() string {
	hostname, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return hostname
}

// setLogLevel 設定日誌級別
func setLogLevel() {
	levelStr := os.Getenv("LOG_LEVEL")
	if levelStr == "" {
		levelStr = "info" // 預設級別為 info
	}

	level, err := zerolog.ParseLevel(levelStr)
	if err != nil {
		level = zerolog.InfoLevel // 錯誤時的預設級別
	}

	zerolog.SetGlobalLevel(level)
}

// GetLogger 獲取特定模組的 logger
func GetLogger(module string) zerolog.Logger {
	return log.With().Str("module", module).Logger()
}
