package infra

import (
	"os"

	"gopkg.in/yaml.v3"
)

// LineConfigYAML 代表 config.yml 中的 LINE 配置結構
type LineConfigYAML struct {
	ID            string   `yaml:"id"`                      // 配置 ID
	Name          string   `yaml:"name"`                    // 配置名稱
	ChannelSecret string   `yaml:"channel_secret"`          // LINE Channel Secret
	ChannelToken  string   `yaml:"channel_token"`           // LINE Channel Access Token
	Enabled       bool     `yaml:"enabled"`                 // 是否啟用此配置
	PushTriggers  []string `yaml:"push_triggers,omitempty"` // 推送觸發器
}

type Config struct {
	App struct {
		IsCrawler  bool   `yaml:"is_crawler"`
		AppVersion string `yaml:"app_version"`
	} `yaml:"app"`
	DriverBlacklist struct {
		Enabled       bool `yaml:"enabled"`
		ExpiryMinutes int  `yaml:"expiry_minutes"`
	} `yaml:"driver_blacklist"`
	MongoDB struct {
		URI      string `yaml:"uri"`
		Database string `yaml:"database"`
	} `yaml:"mongodb"`
	Redis struct {
		Addr     string `yaml:"addr"`
		Password string `yaml:"password"`
		DB       int    `yaml:"db"`
	} `yaml:"redis"`
	RabbitMQ struct {
		URL string `yaml:"url"`
	} `yaml:"rabbitmq"`
	Google struct {
		APIKey string `yaml:"api_key"`
	} `yaml:"google"`
	FCM struct {
		ServerKey string `yaml:"server_key"`
	} `yaml:"fcm"`
	JWT struct {
		SecretKey    string `yaml:"secret_key"`
		ExpiresHours int    `yaml:"expires_hours"`
	} `yaml:"jwt"`
	Discord struct {
		BotToken string `yaml:"bot_token"`
	} `yaml:"discord"`
	LINE struct {
		Enabled bool             `yaml:"enabled"` // 全域開關
		Configs []LineConfigYAML `yaml:"configs"`
	} `yaml:"line"`
	CertBaseURL string `yaml:"cert_base_url"`
}

var AppConfig Config

func LoadConfig() error {
	f, err := os.Open("config.yml")
	if err != nil {
		return err
	}
	defer f.Close()

	decoder := yaml.NewDecoder(f)
	err = decoder.Decode(&AppConfig)
	if err != nil {
		return err
	}
	return nil
}
