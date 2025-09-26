package websocket

import (
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// ConnectionType 連接類型枚舉
type ConnectionType string

const (
	ConnectionTypeDriver ConnectionType = "driver"
	ConnectionTypeUser   ConnectionType = "user"
)

// Connection 通用的WebSocket連接結構
type Connection struct {
	ID           string           `json:"id"`                  // 用戶ID或司機ID
	Type         ConnectionType   `json:"type"`                // 連接類型
	Conn         *websocket.Conn  `json:"-"`                   // WebSocket連接
	Fleet        string           `json:"fleet"`               // 車隊信息
	LastPing     time.Time        `json:"last_ping"`           // 最後ping時間
	SendChannel  chan []byte      `json:"-"`                   // 發送通道
	CloseChannel chan struct{}    `json:"-"`                   // 關閉通道
	CloseOnce    sync.Once        `json:"-"`                   // 確保只關閉一次
	Status       ConnectionStatus `json:"status"`              // 連接狀態
	UserInfo     interface{}      `json:"user_info,omitempty"` // 用戶額外信息
}

// ConnectionStats 連線統計資訊
type ConnectionStats struct {
	ConnectedDrivers   int                         `json:"connected_drivers"`
	ConnectedUsers     int                         `json:"connected_users"`
	TotalConnections   int                         `json:"total_connections"`
	ConnectionsByFleet map[string]int              `json:"connections_by_fleet"`
	ConnectionsByType  map[string]int              `json:"connections_by_type"`
	LastPingTimes      map[string]time.Time        `json:"last_ping_times,omitempty"`
	ConnectionStatus   map[string]ConnectionStatus `json:"connection_status"`
}

// ConnectionConfig WebSocket 連線設定
type ConnectionConfig struct {
	ReadBufferSize      int           `json:"read_buffer_size"`
	WriteBufferSize     int           `json:"write_buffer_size"`
	HandshakeTimeout    time.Duration `json:"handshake_timeout"`
	ReadTimeout         time.Duration `json:"read_timeout"`
	WriteTimeout        time.Duration `json:"write_timeout"`
	PingInterval        time.Duration `json:"ping_interval"`
	HealthCheckInterval time.Duration `json:"health_check_interval"`
	MaxConnectionAge    time.Duration `json:"max_connection_age"`
}

// DefaultConnectionConfig 預設連線設定
func DefaultConnectionConfig() *ConnectionConfig {
	return &ConnectionConfig{
		ReadBufferSize:      1024,
		WriteBufferSize:     1024,
		HandshakeTimeout:    10 * time.Second,
		ReadTimeout:         60 * time.Second,
		WriteTimeout:        10 * time.Second,
		PingInterval:        10 * time.Second,
		HealthCheckInterval: 10 * time.Second,
		MaxConnectionAge:    60 * time.Second,
	}
}
