package model

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// GooglePlaceCandidate 單一候選結果
type GooglePlaceCandidate struct {
	PlaceID string  `bson:"place_id" json:"place_id"`
	Name    string  `bson:"name" json:"name"`
	Address string  `bson:"formatted_address" json:"formatted_address"`
	Lat     float64 `bson:"lat" json:"lat"`
	Lng     float64 `bson:"lng" json:"lng"`
	Rating  float64 `bson:"rating,omitempty" json:"rating,omitempty"`
}

// GooglePlaceCache 用於快取 Google Places API 的結果
type GooglePlaceCache struct {
	ID         primitive.ObjectID     `bson:"_id,omitempty" json:"id"`
	Query      string                 `bson:"query" json:"query"`                               // 搜尋關鍵字
	Candidates []GooglePlaceCandidate `bson:"candidates,omitempty" json:"candidates,omitempty"` // 批次結果的候選列表
	PlaceID    string                 `bson:"place_id,omitempty" json:"place_id,omitempty"`     // Google Place ID (單一結果用)
	Name       string                 `bson:"name,omitempty" json:"name,omitempty"`             // 地點名稱 (單一結果用)
	Address    string                 `bson:"address,omitempty" json:"address,omitempty"`       // 完整地址 (單一結果用)
	Lat        float64                `bson:"lat,omitempty" json:"lat,omitempty"`               // 緯度 (單一結果用)
	Lng        float64                `bson:"lng,omitempty" json:"lng,omitempty"`               // 經度 (單一結果用)
	Website    string                 `bson:"website,omitempty" json:"website,omitempty"`       // 網站
	Types      []string               `bson:"types,omitempty" json:"types,omitempty"`           // 地點類型
	Rating     float64                `bson:"rating,omitempty" json:"rating,omitempty"`         // 評分 (單一結果用)
	Tags       []string               `bson:"tags,omitempty" json:"tags,omitempty"`             // 自定義標籤
	CreatedAt  time.Time              `bson:"created_at" json:"created_at"`                     // 快取建立時間
	UpdatedAt  time.Time              `bson:"updated_at" json:"updated_at"`                     // 快取更新時間
}

// GeocodeCache 用於快取 Geocoding API 的結果
type GeocodeCache struct {
	ID        primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	Query     string             `bson:"query" json:"query"`           // 搜尋關鍵字（地址或經緯度）
	QueryType string             `bson:"query_type" json:"query_type"` // "address_to_latlng" 或 "latlng_to_address"
	Address   string             `bson:"address" json:"address"`       // 地址
	Lat       float64            `bson:"lat" json:"lat"`               // 緯度
	Lng       float64            `bson:"lng" json:"lng"`               // 經度
	CreatedAt time.Time          `bson:"created_at" json:"created_at"` // 快取建立時間
	UpdatedAt time.Time          `bson:"updated_at" json:"updated_at"` // 快取更新時間
}
