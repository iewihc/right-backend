package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"right-backend/infra"
	"right-backend/model"
	"sort"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type GoogleMapService struct {
	logger            zerolog.Logger
	GoogleClient      *infra.GoogleClient
	MongoDB           *infra.MongoDB
	Redis             *redis.Client
	placeCacheService *GooglePlaceCacheService
}

func NewGoogleMapService(logger zerolog.Logger, gc *infra.GoogleClient, mongo *infra.MongoDB, redisClient *redis.Client, placeCacheService *GooglePlaceCacheService) *GoogleMapService {
	return &GoogleMapService{
		logger:            logger.With().Str("module", "google_map_service").Logger(),
		GoogleClient:      gc,
		MongoDB:           mongo,
		Redis:             redisClient,
		placeCacheService: placeCacheService,
	}
}

func (s *GoogleMapService) logUsage(ctx context.Context, api string, params map[string]string, options ...string) {
	var fleet, createdBy string
	if len(options) > 0 {
		fleet = options[0]
	}
	if len(options) > 1 {
		createdBy = options[1]
	}

	// 記錄使用日誌
	b, _ := json.Marshal(params)
	s.logger.Info().Str("api", api).Str("fleet", fleet).Str("created_by", createdBy).RawJSON("params", b).Msg("Google API 使用記錄 (Google API usage)")

	if s.MongoDB == nil {
		return // 測試環境下不記錄日誌
	}

	coll := s.MongoDB.GetCollection("traffic_usage_log")

	// 為特定 API 設置 Elements 數量
	elements := 0
	if api == "find-place-from-text" {
		elements = 1
	}

	log := model.TrafficUsageLog{
		Service:   "google",
		API:       api,
		Params:    string(b),
		Fleet:     fleet,
		CreatedBy: createdBy,
		Elements:  elements,
		CreatedAt: time.Now(),
	}
	coll.InsertOne(ctx, log)
}

func (s *GoogleMapService) GeocodeLatLng(ctx context.Context, lat, lng string) (string, error) {
	params := map[string]string{"latlng": lat + "," + lng, "language": "zh-TW"}
	url := s.GoogleClient.BuildURL("https://maps.googleapis.com/maps/api/geocode/json", params)
	s.logUsage(ctx, "geocode-latlng", params)
	resp, err := s.GoogleClient.HTTPClient.Get(url)
	if err != nil {
		s.logger.Error().Str("lat", lat).Str("lng", lng).Err(err).Msg("地理編碼 HTTP 請求失敗 (Geocoding HTTP request failed)")
		return "", err
	}
	defer resp.Body.Close()
	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	if results, ok := result["results"].([]interface{}); ok && len(results) > 0 {
		if first, ok := results[0].(map[string]interface{}); ok {
			return first["formatted_address"].(string), nil
		}
	}
	s.logger.Warn().Str("lat", lat).Str("lng", lng).Msg("找不到地址 (No address found)")
	return "", fmt.Errorf("找不到地址 (No address found)")
}

func (s *GoogleMapService) GeocodeAddress(ctx context.Context, address string, options ...string) (lat, lng string, err error) {
	cacheKey := "geo:address:" + address
	if s.Redis != nil {
		cached, err := s.Redis.Get(ctx, cacheKey).Result()
		if err == nil && cached != "" {
			var result [2]string
			json.Unmarshal([]byte(cached), &result)
			return result[0], result[1], nil
		}
	}
	params := map[string]string{"address": url.QueryEscape(address), "language": "zh-TW"}
	url := s.GoogleClient.BuildURL("https://maps.googleapis.com/maps/api/geocode/json", params)
	s.logUsage(ctx, "geocode-address", params, options...)
	resp, err := s.GoogleClient.HTTPClient.Get(url)
	if err != nil {
		s.logger.Error().Str("address", address).Err(err).Msg("地址地理編碼 HTTP 請求失敗 (Address geocoding HTTP request failed)")
		return
	}
	defer resp.Body.Close()
	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	if results, ok := result["results"].([]interface{}); ok && len(results) > 0 {
		if first, ok := results[0].(map[string]interface{}); ok {
			if geometry, ok := first["geometry"].(map[string]interface{}); ok {
				if location, ok := geometry["location"].(map[string]interface{}); ok {
					lat = fmt.Sprintf("%v", location["lat"])
					lng = fmt.Sprintf("%v", location["lng"])
					if s.Redis != nil {
						b, _ := json.Marshal([2]string{lat, lng})
						s.Redis.Set(ctx, cacheKey, b, 24*time.Hour)
					}
					return
				}
			}
		}
	}
	s.logger.Warn().Str("address", address).Msg("找不到位置 (No location found)")
	err = fmt.Errorf("找不到位置 (No location found)")
	return
}

func (s *GoogleMapService) DistanceMatrix(ctx context.Context, origin, destination string, options ...string) (distanceKm float64, durationMins int, err error) {
	params := map[string]string{"origins": origin, "destinations": destination, "language": "zh-TW"}
	url := s.GoogleClient.BuildURL("https://maps.googleapis.com/maps/api/distancematrix/json", params)
	s.logUsage(ctx, "distance-matrix", params, options...)
	resp, err := s.GoogleClient.HTTPClient.Get(url)
	if err != nil {
		s.logger.Error().Str("origin", origin).Str("destination", destination).Err(err).Msg("距離矩陣 HTTP 請求失敗 (Distance matrix HTTP request failed)")
		return
	}
	defer resp.Body.Close()
	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	if rows, ok := result["rows"].([]interface{}); ok && len(rows) > 0 {
		if elements, ok := rows[0].(map[string]interface{})["elements"].([]interface{}); ok && len(elements) > 0 {
			el := elements[0].(map[string]interface{})
			if dist, ok := el["distance"].(map[string]interface{}); ok {
				if distVal, ok := dist["value"].(float64); ok {
					distanceKm = distVal / 1000 // 轉換米為公里
				}
			}
			if dur, ok := el["duration"].(map[string]interface{}); ok {
				if durVal, ok := dur["value"].(float64); ok {
					durationMins = int(durVal / 60) // 轉換秒為分鐘
				}
			}
			return
		}
	}
	s.logger.Warn().Str("origin", origin).Str("destination", destination).Msg("找不到距離資訊 (No distance info found)")
	err = fmt.Errorf("找不到距離資訊 (No distance info found)")
	return
}

func (s *GoogleMapService) PlacesAutocomplete(ctx context.Context, input string) ([]string, error) {
	// 記錄搜索關鍵詞
	s.logger.Debug().Str("input", input).Msg("地點自動完成搜索 (Places autocomplete search)")

	// 1. 從Redis緩存中查找
	cacheKey := fmt.Sprintf("google:places_autocomplete:%s", input)
	if s.Redis != nil {
		cached, err := s.Redis.Get(ctx, cacheKey).Result()
		if err == nil && cached != "" {
			var cachedData []string
			if json.Unmarshal([]byte(cached), &cachedData) == nil {
				s.logger.Info().Str("input", input).Msg("從 Redis 緩存找到 Google Places 結果 (Found Google Places results from Redis cache)")
				return cachedData, nil
			}
		}
	}

	// 2. 調用實際的Google Places API
	params := map[string]string{"input": url.QueryEscape(input), "language": "zh-TW"}
	url := s.GoogleClient.BuildURL("https://maps.googleapis.com/maps/api/place/autocomplete/json", params)
	s.logUsage(ctx, "places-autocomplete", params)
	resp, err := s.GoogleClient.HTTPClient.Get(url)
	if err != nil {
		s.logger.Error().Str("input", input).Err(err).Msg("地點自動完成 HTTP 請求失敗 (Places autocomplete HTTP request failed)")
		return nil, err
	}
	defer resp.Body.Close()
	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	predictions := []string{}
	if preds, ok := result["predictions"].([]interface{}); ok {
		for _, p := range preds {
			if pred, ok := p.(map[string]interface{}); ok {
				if desc, ok := pred["description"].(string); ok {
					predictions = append(predictions, desc)
				}
			}
		}
	}

	// 緩存結果到Redis（1小時過期）
	if s.Redis != nil && len(predictions) > 0 {
		resultBytes, _ := json.Marshal(predictions)
		s.Redis.Set(ctx, cacheKey, resultBytes, 1*time.Hour)
	}

	return predictions, nil
}

func (s *GoogleMapService) FindNearestDriver(ctx context.Context, driverCoords []string, passengerCoord string) (nearestIdx int, minDistance string, minDuration string, err error) {
	if len(driverCoords) == 0 || len(driverCoords) > 25 {
		s.logger.Error().Int("driver_count", len(driverCoords)).Msg("司機座標數量無效 (Invalid driver coordinates count)")
		err = fmt.Errorf("司機座標數量需介於 1~25 之間 (Driver coordinates count must be between 1~25)")
		return
	}
	origins := driverCoords[0]
	for i := 1; i < len(driverCoords); i++ {
		origins += "|" + driverCoords[i]
	}
	params := map[string]string{
		"origins":      origins,
		"destinations": passengerCoord,
		"language":     "zh-TW",
	}
	url := s.GoogleClient.BuildURL("https://maps.googleapis.com/maps/api/distancematrix/json", params)
	s.logUsage(ctx, "find-nearest-driver", params)
	resp, err := s.GoogleClient.HTTPClient.Get(url)
	if err != nil {
		s.logger.Error().Int("driver_count", len(driverCoords)).Str("passenger_coord", passengerCoord).Err(err).Msg("尋找最近司機 HTTP 請求失敗 (Find nearest driver HTTP request failed)")
		return
	}
	defer resp.Body.Close()
	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	minVal := int64(1<<63 - 1)
	for i, row := range result["rows"].([]interface{}) {
		if elements, ok := row.(map[string]interface{})["elements"].([]interface{}); ok && len(elements) > 0 {
			el := elements[0].(map[string]interface{})
			if dist, ok := el["distance"].(map[string]interface{}); ok {
				if val, ok := dist["value"].(float64); ok {
					if int64(val) < minVal {
						minVal = int64(val)
						nearestIdx = i
						minDistance = dist["text"].(string)
						if dur, ok := el["duration"].(map[string]interface{}); ok {
							minDuration = dur["text"].(string)
						}
					}
				}
			}
		}
	}
	if minVal == int64(1<<63-1) {
		s.logger.Warn().Int("driver_count", len(driverCoords)).Str("passenger_coord", passengerCoord).Msg("找不到最近司機 (No nearest driver found)")
		err = fmt.Errorf("找不到最近司機 (No nearest driver found)")
	}
	return
}

func (s *GoogleMapService) TopNDriversByDuration(ctx context.Context, driverCoords []string, passengerCoord string, n int, options ...string) (indexes []int, dists []string, durs []string, err error) {
	if len(driverCoords) == 0 || len(driverCoords) > 25 {
		s.logger.Error().Int("driver_count", len(driverCoords)).Int("requested_n", n).Msg("司機座標數量無效 (Invalid driver coordinates count)")
		err = fmt.Errorf("司機座標數量需介於 1~25 之間 (Driver coordinates count must be between 1~25)")
		return
	}
	origins := driverCoords[0]
	for i := 1; i < len(driverCoords); i++ {
		origins += "|" + driverCoords[i]
	}
	params := map[string]string{
		"origins":      origins,
		"destinations": passengerCoord,
		"language":     "zh-TW",
	}
	url := s.GoogleClient.BuildURL("https://maps.googleapis.com/maps/api/distancematrix/json", params)
	s.logUsage(ctx, "top-n-drivers-by-duration", params, options...)
	resp, err := s.GoogleClient.HTTPClient.Get(url)
	if err != nil {
		s.logger.Error().Int("driver_count", len(driverCoords)).Str("passenger_coord", passengerCoord).Int("requested_n", n).Err(err).Msg("獲取前 N 名司機 HTTP 請求失敗 (Get top N drivers HTTP request failed)")
		return
	}
	defer resp.Body.Close()
	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)

	type driverResult struct {
		Idx      int
		DistText string
		DurText  string
		DurVal   float64
	}
	var results []driverResult
	for i, row := range result["rows"].([]interface{}) {
		if elements, ok := row.(map[string]interface{})["elements"].([]interface{}); ok && len(elements) > 0 {
			el := elements[0].(map[string]interface{})
			if dist, ok := el["distance"].(map[string]interface{}); ok {
				if dur, ok := el["duration"].(map[string]interface{}); ok {
					if durVal, ok := dur["value"].(float64); ok {
						results = append(results, driverResult{
							Idx:      i,
							DistText: dist["text"].(string),
							DurText:  dur["text"].(string),
							DurVal:   durVal,
						})
					}
				}
			}
		}
	}
	sort.Slice(results, func(i, j int) bool { return results[i].DurVal < results[j].DurVal })
	for i := 0; i < n && i < len(results); i++ {
		indexes = append(indexes, results[i].Idx)
		dists = append(dists, results[i].DistText)
		durs = append(durs, results[i].DurText)
	}
	return
}

// FindPlaceFromText 根據文字搜尋地點，優先順序：tags -> query -> Google API
func (s *GoogleMapService) FindPlaceFromText(ctx context.Context, input string, options ...string) ([]interface{}, error) {
	// 1. 先檢查快取
	if s.placeCacheService != nil {
		cachedResults, err := s.placeCacheService.FindByTagsOrQuery(ctx, input)
		if err == nil && len(cachedResults) > 0 {
			return s.placeCacheService.BuildCandidatesFromCache(cachedResults), nil
		}
	}

	// 2. 如果都沒找到，最後調用 Google Places API Find Place from Text
	params := map[string]string{
		"input":     url.QueryEscape(input),
		"inputtype": "textquery",
		"fields":    "place_id,name,formatted_address,geometry,rating",
		"language":  "zh-TW",
		"region":    "tw",
		"locationbias": "circle:25000@24.1477,120.6736", // 台中市中心半徑 25 公里範圍
	}

	apiURL := s.GoogleClient.BuildURL("https://maps.googleapis.com/maps/api/place/findplacefromtext/json", params)
	s.logUsage(ctx, "find-place-from-text", params, options...)

	resp, err := s.GoogleClient.HTTPClient.Get(apiURL)
	if err != nil {
		s.logger.Error().Str("input", input).Err(err).Msg("文字尋找地點 HTTP 請求失敗 (Find place from text HTTP request failed)")
		return nil, err
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		s.logger.Error().Str("input", input).Err(err).Msg("解析 Find Place 響應失敗 (Failed to decode Find Place response)")
		return nil, err
	}

	candidates, ok := result["candidates"].([]interface{})
	if !ok || len(candidates) == 0 {
		s.logger.Warn().Str("input", input).Msg("沒有找到候選地點 (No candidates found)")
		return nil, fmt.Errorf("沒有找到候選地點 (No candidates found)")
	}

	// 3. 只有在單一結果時才快取到 MongoDB（避免多個建議時也被快取）
	if s.placeCacheService != nil && len(candidates) == 1 {
		s.placeCacheService.CacheGooglePlaceBatch(ctx, input, candidates)
		s.logger.Debug().Str("input", input).Msg("單一結果已快取到 MongoDB")
	} else if len(candidates) > 1 {
		s.logger.Debug().Str("input", input).Int("candidate_count", len(candidates)).Msg("多個候選結果，跳過快取避免地址建議問題")
	}

	// 4. 為所有候選結果添加 fromCache 標記
	for _, candidate := range candidates {
		if candidateMap, ok := candidate.(map[string]interface{}); ok {
			candidateMap["fromCache"] = false
		}
	}

	return candidates, nil
}

// GetPlaceDetails 根據 place_id 獲取地點詳細資訊，首先檢查 MongoDB 快取
func (s *GoogleMapService) GetPlaceDetails(ctx context.Context, placeID string, options ...string) (map[string]interface{}, error) {
	// 1. 先檢查 MongoDB 快取
	if s.placeCacheService != nil {
		cached, err := s.placeCacheService.FindByPlaceID(ctx, placeID)
		if err == nil {
			// 找到快取，返回詳細結果
			result := map[string]interface{}{
				"place_id":          cached.PlaceID,
				"name":              cached.Name,
				"formatted_address": cached.Address,
				"geometry": map[string]interface{}{
					"location": map[string]interface{}{
						"lat": cached.Lat,
						"lng": cached.Lng,
					},
				},
				"website":   cached.Website,
				"types":     cached.Types,
				"rating":    cached.Rating,
				"fromCache": true,
			}
			if len(cached.Tags) > 0 {
				result["tags"] = cached.Tags
			}
			return result, nil
		}
	}

	// 2. 調用 Google Places API Place Details
	params := map[string]string{
		"place_id": placeID,
		"fields":   "place_id,name,formatted_address,geometry,website,types,rating",
		"language": "zh-TW",
	}

	apiURL := s.GoogleClient.BuildURL("https://maps.googleapis.com/maps/api/place/details/json", params)
	s.logUsage(ctx, "place-details", params, options...)

	resp, err := s.GoogleClient.HTTPClient.Get(apiURL)
	if err != nil {
		s.logger.Error().Str("place_id", placeID).Err(err).Msg("地點詳細資訊 HTTP 請求失敗 (Place details HTTP request failed)")
		return nil, err
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		s.logger.Error().Str("place_id", placeID).Err(err).Msg("解析 Place Details 響應失敗 (Failed to decode Place Details response)")
		return nil, err
	}

	placeResult, ok := result["result"].(map[string]interface{})
	if !ok {
		s.logger.Warn().Str("place_id", placeID).Msg("沒有找到地點詳細資訊 (No place details found)")
		return nil, fmt.Errorf("沒有找到地點詳細資訊 (No place details found)")
	}

	// 3. 快取結果到 MongoDB（永久保存）
	if s.placeCacheService != nil {
		s.placeCacheService.CacheGooglePlaceDetails(ctx, placeResult)
	}

	// 4. 添加 fromCache 標記
	placeResult["fromCache"] = false

	return placeResult, nil
}

// ImprovedGeocodeAddress 改進的地址轉經緯度，先檢查 MongoDB 快取
func (s *GoogleMapService) ImprovedGeocodeAddress(ctx context.Context, address string, options ...string) (lat, lng string, err error) {
	// 1. 先檢查 MongoDB 快取
	if s.MongoDB != nil {
		coll := s.MongoDB.GetCollection("geocode_cache")
		var cached model.GeocodeCache

		filter := bson.M{
			"query":      address,
			"query_type": "address_to_latlng",
		}

		err := coll.FindOne(ctx, filter).Decode(&cached)
		if err == nil {
			// 找到快取，返回結果
			//s.logger.Info().Str("address", address).Msg("從 MongoDB 快取找到 Geocode 結果 (Found Geocode results from MongoDB cache)")
			return strconv.FormatFloat(cached.Lat, 'f', -1, 64),
				strconv.FormatFloat(cached.Lng, 'f', -1, 64), nil
		}
	}

	// 2. 先檢查 Redis 快取（保持原有邏輯）
	cacheKey := "geo:address:" + address
	if s.Redis != nil {
		cached, err := s.Redis.Get(ctx, cacheKey).Result()
		if err == nil && cached != "" {
			var result [2]string
			json.Unmarshal([]byte(cached), &result)
			return result[0], result[1], nil
		}
	}

	// 3. 調用 Google Geocoding API
	params := map[string]string{"address": url.QueryEscape(address), "language": "zh-TW"}
	url := s.GoogleClient.BuildURL("https://maps.googleapis.com/maps/api/geocode/json", params)
	s.logUsage(ctx, "geocode-address", params, options...)

	resp, err := s.GoogleClient.HTTPClient.Get(url)
	if err != nil {
		s.logger.Error().Str("address", address).Err(err).Msg("改進地址地理編碼 HTTP 請求失敗 (Improved address geocoding HTTP request failed)")
		return
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)

	if results, ok := result["results"].([]interface{}); ok && len(results) > 0 {
		if first, ok := results[0].(map[string]interface{}); ok {
			if geometry, ok := first["geometry"].(map[string]interface{}); ok {
				if location, ok := geometry["location"].(map[string]interface{}); ok {
					lat = fmt.Sprintf("%v", location["lat"])
					lng = fmt.Sprintf("%v", location["lng"])

					// 4. 快取到 Redis（保持原有邏輯）
					if s.Redis != nil {
						b, _ := json.Marshal([2]string{lat, lng})
						s.Redis.Set(ctx, cacheKey, b, 24*time.Hour)
					}

					// 5. 快取到 MongoDB（永久保存）
					if s.MongoDB != nil {
						s.cacheGeocodeResult(ctx, address, "address_to_latlng", first["formatted_address"].(string), location["lat"].(float64), location["lng"].(float64))
					}

					return
				}
			}
		}
	}

	s.logger.Warn().Str("address", address).Msg("找不到位置 (No location found)")
	err = fmt.Errorf("找不到位置 (No location found)")
	return
}

// cacheGeocodeResult 快取 Geocoding 結果到 MongoDB
func (s *GoogleMapService) cacheGeocodeResult(ctx context.Context, query, queryType, address string, lat, lng float64) {
	coll := s.MongoDB.GetCollection("geocode_cache")

	cache := model.GeocodeCache{
		Query:     query,
		QueryType: queryType,
		Address:   address,
		Lat:       lat,
		Lng:       lng,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	// 使用 upsert 更新或插入
	filter := bson.M{"query": query, "query_type": queryType}
	update := bson.M{"$set": cache}
	opts := options.Update().SetUpsert(true)
	coll.UpdateOne(ctx, filter, update, opts)
}
