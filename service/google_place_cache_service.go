package service

import (
	"context"
	"fmt"
	"right-backend/infra"
	"right-backend/model"
	"time"

	"github.com/rs/zerolog"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type GooglePlaceCacheService struct {
	logger  zerolog.Logger
	MongoDB *infra.MongoDB
}

func NewGooglePlaceCacheService(logger zerolog.Logger, mongo *infra.MongoDB) *GooglePlaceCacheService {
	return &GooglePlaceCacheService{
		logger:  logger.With().Str("module", "google_place_cache_service").Logger(),
		MongoDB: mongo,
	}
}

// CreateGooglePlaceCache 創建 Google Place Cache
func (s *GooglePlaceCacheService) CreateGooglePlaceCache(ctx context.Context, cache *model.GooglePlaceCache) (*model.GooglePlaceCache, error) {
	coll := s.MongoDB.GetCollection("google_place_cache")

	// 1. 去除同一記錄內的重複標籤
	if len(cache.Tags) > 0 {
		uniqueTags := make([]string, 0, len(cache.Tags))
		tagMap := make(map[string]bool)
		for _, tag := range cache.Tags {
			if !tagMap[tag] {
				tagMap[tag] = true
				uniqueTags = append(uniqueTags, tag)
			}
		}

		if len(uniqueTags) != len(cache.Tags) {
			s.logger.Info().Int("original_count", len(cache.Tags)).Int("unique_count", len(uniqueTags)).Msg("已移除重複的標籤 (Removed duplicate tags)")
		}
		cache.Tags = uniqueTags
	}

	// 2. 檢查標籤是否已被其他記錄使用（全域唯一性檢查）
	if len(cache.Tags) > 0 {
		for _, tag := range cache.Tags {
			existingFilter := bson.M{"tags": tag}
			count, err := coll.CountDocuments(ctx, existingFilter)
			if err != nil {
				s.logger.Error().Str("tag", tag).Err(err).Msg("檢查標籤唯一性失敗 (Failed to check tag uniqueness)")
				return nil, fmt.Errorf("檢查標籤唯一性失敗 (Failed to check tag uniqueness): %v", err)
			}
			if count > 0 {
				s.logger.Warn().Str("tag", tag).Msg("標籤已存在於其他記錄中 (Tag already exists in another record)")
				return nil, fmt.Errorf("標籤 '%s' 已被其他地點使用，每個標籤只能屬於一個地點 (Tag '%s' is already used by another location, each tag can only belong to one location)", tag, tag)
			}
		}
	}

	// 3. 如果所有標籤都沒有重複，才進行創建
	cache.CreatedAt = time.Now()
	cache.UpdatedAt = time.Now()

	result, err := coll.InsertOne(ctx, cache)
	if err != nil {
		s.logger.Error().Err(err).Msg("建立Google地點快取失敗 (Failed to create Google place cache)")
		return nil, err
	}

	cache.ID = result.InsertedID.(primitive.ObjectID)
	s.logger.Info().Strs("tags", cache.Tags).Str("id", cache.ID.Hex()).Msg("成功創建Google地點快取 (Successfully created Google place cache)")
	return cache, nil
}

// GetGooglePlaceCache 取得單筆 Google Place Cache
func (s *GooglePlaceCacheService) GetGooglePlaceCache(ctx context.Context, id string) (*model.GooglePlaceCache, error) {
	coll := s.MongoDB.GetCollection("google_place_cache")

	objectID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		s.logger.Error().Str("id", id).Err(err).Msg("無效的ID格式 (Invalid ID format)")
		return nil, fmt.Errorf("無效的ID格式 (Invalid ID format): %v", err)
	}

	var cache model.GooglePlaceCache
	err = coll.FindOne(ctx, bson.M{"_id": objectID}).Decode(&cache)
	if err != nil {
		s.logger.Error().Str("id", id).Err(err).Msg("查詢Google地點快取失敗 (Failed to get Google place cache)")
		return nil, err
	}

	return &cache, nil
}

// UpdateGooglePlaceCache 更新 Google Place Cache
func (s *GooglePlaceCacheService) UpdateGooglePlaceCache(ctx context.Context, id string, updateData bson.M) (*model.GooglePlaceCache, error) {
	coll := s.MongoDB.GetCollection("google_place_cache")

	objectID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		s.logger.Error().Str("id", id).Err(err).Msg("無效的ID格式 (Invalid ID format)")
		return nil, fmt.Errorf("無效的ID格式 (Invalid ID format): %v", err)
	}

	updateData["updated_at"] = time.Now()

	// 如果更新了 tags，需要檢查標籤唯一性
	if newTags, exists := updateData["tags"]; exists {
		if tagsSlice, ok := newTags.([]string); ok {
			// 檢查標籤是否已被其他記錄使用（全域唯一性檢查）
			for _, tag := range tagsSlice {
				// 排除當前記錄本身
				existingFilter := bson.M{
					"tags": tag,
					"_id":  bson.M{"$ne": objectID},
				}
				count, err := coll.CountDocuments(ctx, existingFilter)
				if err != nil {
					s.logger.Error().Str("tag", tag).Str("id", id).Err(err).Msg("檢查標籤唯一性失敗 (Failed to check tag uniqueness)")
					return nil, fmt.Errorf("檢查標籤唯一性失敗 (Failed to check tag uniqueness): %v", err)
				}
				if count > 0 {
					s.logger.Warn().Str("tag", tag).Str("id", id).Msg("標籤已存在於其他記錄中 (Tag already exists in another record)")
					return nil, fmt.Errorf("標籤 '%s' 已被其他地點使用，每個標籤只能屬於一個地點 (Tag '%s' is already used by another location, each tag can only belong to one location)", tag, tag)
				}
			}

			// 去除重複標籤（在同一個記錄內）
			uniqueTags := make([]string, 0, len(tagsSlice))
			tagMap := make(map[string]bool)
			for _, tag := range tagsSlice {
				if !tagMap[tag] {
					tagMap[tag] = true
					uniqueTags = append(uniqueTags, tag)
				}
			}
			updateData["tags"] = uniqueTags

			if len(uniqueTags) != len(tagsSlice) {
				s.logger.Info().Str("id", id).Int("original_count", len(tagsSlice)).Int("unique_count", len(uniqueTags)).Msg("已移除重複的標籤 (Removed duplicate tags)")
			}
		}
	}

	// 如果更新了主記錄的 address，同時更新 candidates 中的 formatted_address 以保持一致性
	if newAddress, exists := updateData["address"]; exists {
		// 先獲取當前記錄以檢查是否有 candidates
		var currentCache model.GooglePlaceCache
		if err := coll.FindOne(ctx, bson.M{"_id": objectID}).Decode(&currentCache); err == nil {
			if len(currentCache.Candidates) > 0 {
				// 更新第一個候選項目的 Address 欄位
				updateData["candidates.0.formatted_address"] = newAddress
				s.logger.Info().
					Str("id", id).
					Interface("new_address", newAddress).
					Msg("同時更新candidates中的formatted_address以保持一致性")
			}
		}
	}

	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)
	var cache model.GooglePlaceCache
	err = coll.FindOneAndUpdate(ctx, bson.M{"_id": objectID}, bson.M{"$set": updateData}, opts).Decode(&cache)
	if err != nil {
		s.logger.Error().Str("id", id).Err(err).Msg("更新Google地點快取失敗 (Failed to update Google place cache)")
		return nil, err
	}

	return &cache, nil
}

// ListGooglePlaceCache 列出 Google Place Cache
func (s *GooglePlaceCacheService) ListGooglePlaceCache(ctx context.Context, keyword string, limit, offset int) ([]model.GooglePlaceCache, int, error) {
	coll := s.MongoDB.GetCollection("google_place_cache")

	// 構建過濾條件
	filter := bson.M{}
	if keyword != "" {
		filter["$or"] = []bson.M{
			{"tags": bson.M{"$regex": keyword, "$options": "i"}},    // 先搜尋標籤
			{"query": bson.M{"$regex": keyword, "$options": "i"}},   // 再搜尋 query
			{"name": bson.M{"$regex": keyword, "$options": "i"}},    // 再搜尋名稱
			{"address": bson.M{"$regex": keyword, "$options": "i"}}, // 最後搜尋地址
		}
	}

	// 計算總數
	total, err := coll.CountDocuments(ctx, filter)
	if err != nil {
		s.logger.Error().Str("keyword", keyword).Err(err).Msg("計算Google地點快取總數失敗 (Failed to count Google place cache)")
		return nil, 0, err
	}

	// 設定預設值
	if limit <= 0 {
		limit = 10
	}
	if offset < 0 {
		offset = 0
	}

	// 查詢結果
	opts := options.Find().SetLimit(int64(limit)).SetSkip(int64(offset)).SetSort(bson.M{"updated_at": -1})
	cursor, err := coll.Find(ctx, filter, opts)
	if err != nil {
		s.logger.Error().Str("keyword", keyword).Err(err).Msg("查詢Google地點快取清單失敗 (Failed to find Google place cache list)")
		return nil, 0, err
	}
	defer cursor.Close(ctx)

	var caches []model.GooglePlaceCache
	if err = cursor.All(ctx, &caches); err != nil {
		s.logger.Error().Str("keyword", keyword).Err(err).Msg("讀取Google地點快取結果失敗 (Failed to read Google place cache results)")
		return nil, 0, err
	}

	return caches, int(total), nil
}

// DeleteGooglePlaceCache 刪除 Google Place Cache
func (s *GooglePlaceCacheService) DeleteGooglePlaceCache(ctx context.Context, id string) error {
	coll := s.MongoDB.GetCollection("google_place_cache")

	objectID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return fmt.Errorf("invalid id format: %v", err)
	}

	result, err := coll.DeleteOne(ctx, bson.M{"_id": objectID})
	if err != nil {
		s.logger.Error().Str("id", id).Err(err).Msg("刪除Google地點快取失敗 (Failed to delete Google place cache)")
		return err
	}

	if result.DeletedCount == 0 {
		s.logger.Warn().Str("id", id).Msg("Google地點快取記錄不存在 (Google place cache record not found)")
		return fmt.Errorf("記錄不存在 (Record not found)")
	}

	return nil
}

// AddTags 為 Google Place Cache 添加標籤
func (s *GooglePlaceCacheService) AddTags(ctx context.Context, id string, tags []string) (*model.GooglePlaceCache, error) {
	coll := s.MongoDB.GetCollection("google_place_cache")

	objectID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		s.logger.Error().Str("id", id).Err(err).Msg("無效的ID格式 (Invalid ID format)")
		return nil, fmt.Errorf("無效的ID格式 (Invalid ID format): %v", err)
	}

	// 1. 檢查標籤是否已被其他記錄使用（全域唯一性檢查）
	for _, tag := range tags {
		existingFilter := bson.M{
			"tags": tag,
			"_id":  bson.M{"$ne": objectID}, // 排除當前記錄
		}
		count, err := coll.CountDocuments(ctx, existingFilter)
		if err != nil {
			s.logger.Error().Str("tag", tag).Err(err).Msg("檢查標籤唯一性失敗 (Failed to check tag uniqueness)")
			return nil, fmt.Errorf("檢查標籤唯一性失敗 (Failed to check tag uniqueness): %v", err)
		}
		if count > 0 {
			s.logger.Warn().Str("tag", tag).Str("id", id).Msg("標籤已存在於其他記錄中 (Tag already exists in another record)")
			return nil, fmt.Errorf("標籤 '%s' 已被其他地點使用，每個標籤只能屬於一個地點 (Tag '%s' is already used by another location, each tag can only belong to one location)", tag, tag)
		}
	}

	// 2. 如果所有標籤都沒有重複，才進行添加
	update := bson.M{
		"$addToSet": bson.M{"tags": bson.M{"$each": tags}},
		"$set":      bson.M{"updated_at": time.Now()},
	}

	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)
	var cache model.GooglePlaceCache
	err = coll.FindOneAndUpdate(ctx, bson.M{"_id": objectID}, update, opts).Decode(&cache)
	if err != nil {
		s.logger.Error().Str("id", id).Strs("tags", tags).Err(err).Msg("添加Google地點快取標籤失敗 (Failed to add Google place cache tags)")
		return nil, err
	}

	s.logger.Info().Str("id", id).Strs("tags", tags).Msg("成功添加標籤到Google地點快取 (Successfully added tags to Google place cache)")
	return &cache, nil
}

// RemoveTags 移除 Google Place Cache 標籤
func (s *GooglePlaceCacheService) RemoveTags(ctx context.Context, id string, tags []string) (*model.GooglePlaceCache, error) {
	coll := s.MongoDB.GetCollection("google_place_cache")

	objectID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		s.logger.Error().Str("id", id).Err(err).Msg("無效的ID格式 (Invalid ID format)")
		return nil, fmt.Errorf("無效的ID格式 (Invalid ID format): %v", err)
	}

	update := bson.M{
		"$pullAll": bson.M{"tags": tags},
		"$set":     bson.M{"updated_at": time.Now()},
	}

	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)
	var cache model.GooglePlaceCache
	err = coll.FindOneAndUpdate(ctx, bson.M{"_id": objectID}, update, opts).Decode(&cache)
	if err != nil {
		s.logger.Error().Str("id", id).Strs("tags", tags).Err(err).Msg("移除Google地點快取標籤失敗 (Failed to remove Google place cache tags)")
		return nil, err
	}

	return &cache, nil
}

// SearchByTags 根據標籤搜尋 Google Place Cache
func (s *GooglePlaceCacheService) SearchByTags(ctx context.Context, tags []string) ([]model.GooglePlaceCache, error) {
	coll := s.MongoDB.GetCollection("google_place_cache")

	filter := bson.M{"tags": bson.M{"$in": tags}}

	cursor, err := coll.Find(ctx, filter)
	if err != nil {
		s.logger.Error().Strs("tags", tags).Err(err).Msg("根據標籤搜尋Google地點快取失敗 (Failed to search Google place cache by tags)")
		return nil, err
	}
	defer cursor.Close(ctx)

	var caches []model.GooglePlaceCache
	if err = cursor.All(ctx, &caches); err != nil {
		s.logger.Error().Strs("tags", tags).Err(err).Msg("讀取標籤搜尋結果失敗 (Failed to read tag search results)")
		return nil, err
	}

	return caches, nil
}

// FindByTagsOrQuery 根據標籤或查詢字串搜尋 Google Place Cache，優先順序：tags -> query
func (s *GooglePlaceCacheService) FindByTagsOrQuery(ctx context.Context, input string) ([]model.GooglePlaceCache, error) {
	coll := s.MongoDB.GetCollection("google_place_cache")

	// 1. 首先在 tags 欄位中搜尋關鍵字
	tagsFilter := bson.M{
		"tags": bson.M{"$in": []string{input}},
	}

	cursor, err := coll.Find(ctx, tagsFilter)
	if err == nil {
		var cachedResults []model.GooglePlaceCache
		if err := cursor.All(ctx, &cachedResults); err == nil && len(cachedResults) > 0 {
			s.logger.Info().Str("input", input).Int("count", len(cachedResults)).Msg("從 MongoDB tags 欄位找到 Find Place 結果 (Found Find Place results from MongoDB tags field)")
			return cachedResults, nil
		}
	}

	// 2. 如果 tags 中沒找到，再在 query 欄位中搜尋
	queryFilter := bson.M{
		"query": input,
	}

	cursor, err = coll.Find(ctx, queryFilter)
	if err == nil {
		var cachedResults []model.GooglePlaceCache
		if err := cursor.All(ctx, &cachedResults); err == nil && len(cachedResults) > 0 {
			//s.logger.Info().Str("input", input).Int("count", len(cachedResults)).Msg("從 MongoDB query 欄位找到 Find Place 結果 (Found Find Place results from MongoDB query field)")
			return cachedResults, nil
		}
	}

	// 沒找到任何結果
	return nil, nil
}

// FindByPlaceID 根據 place_id 查詢 Google Place Cache
func (s *GooglePlaceCacheService) FindByPlaceID(ctx context.Context, placeID string) (*model.GooglePlaceCache, error) {
	coll := s.MongoDB.GetCollection("google_place_cache")

	filter := bson.M{
		"place_id": placeID,
	}

	var cached model.GooglePlaceCache
	err := coll.FindOne(ctx, filter).Decode(&cached)
	if err != nil {
		return nil, err
	}

	//s.logger.Info().Str("place_id", placeID).Msg("從 MongoDB 快取找到 Place Details 結果 (Found Place Details results from MongoDB cache)")
	return &cached, nil
}

// CacheGooglePlaceBatch 批次快取 Find Place 的所有結果到 MongoDB，避免重複查詢產生多筆記錄
func (s *GooglePlaceCacheService) CacheGooglePlaceBatch(ctx context.Context, query string, candidates []interface{}) {
	if len(candidates) == 0 {
		return
	}

	coll := s.MongoDB.GetCollection("google_place_cache")

	// 轉換所有候選結果
	var candidateList []model.GooglePlaceCandidate
	for _, candidate := range candidates {
		candidateMap, ok := candidate.(map[string]interface{})
		if !ok {
			continue
		}

		candidateData := model.GooglePlaceCandidate{}

		if placeID, ok := candidateMap["place_id"].(string); ok {
			candidateData.PlaceID = placeID
		}
		if name, ok := candidateMap["name"].(string); ok {
			candidateData.Name = name
		}
		if address, ok := candidateMap["formatted_address"].(string); ok {
			candidateData.Address = address
		}
		if geometry, ok := candidateMap["geometry"].(map[string]interface{}); ok {
			if location, ok := geometry["location"].(map[string]interface{}); ok {
				if lat, ok := location["lat"].(float64); ok {
					candidateData.Lat = lat
				}
				if lng, ok := location["lng"].(float64); ok {
					candidateData.Lng = lng
				}
			}
		}
		if rating, ok := candidateMap["rating"].(float64); ok {
			candidateData.Rating = rating
		}

		candidateList = append(candidateList, candidateData)
	}

	cache := model.GooglePlaceCache{
		Query:      query,
		Candidates: candidateList,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}

	// 如果只有一個候選結果，將其數據也填入外層欄位
	if len(candidateList) == 1 {
		firstCandidate := candidateList[0]
		cache.PlaceID = firstCandidate.PlaceID
		cache.Name = firstCandidate.Name
		cache.Address = firstCandidate.Address
		cache.Lat = firstCandidate.Lat
		cache.Lng = firstCandidate.Lng
		cache.Rating = firstCandidate.Rating
	}

	// 使用只有 query 的過濾器，避免為同一查詢創建多筆記錄
	filter := bson.M{"query": query}
	update := bson.M{"$set": cache}
	opts := options.Update().SetUpsert(true)
	coll.UpdateOne(ctx, filter, update, opts)
}

// CacheGooglePlace 快取 Find Place 的結果到 MongoDB
func (s *GooglePlaceCacheService) CacheGooglePlace(ctx context.Context, query string, candidate map[string]interface{}) {
	coll := s.MongoDB.GetCollection("google_place_cache")

	cache := model.GooglePlaceCache{
		Query:     query,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if placeID, ok := candidate["place_id"].(string); ok {
		cache.PlaceID = placeID
	}
	if name, ok := candidate["name"].(string); ok {
		cache.Name = name
	}
	if address, ok := candidate["formatted_address"].(string); ok {
		cache.Address = address
	}
	if geometry, ok := candidate["geometry"].(map[string]interface{}); ok {
		if location, ok := geometry["location"].(map[string]interface{}); ok {
			if lat, ok := location["lat"].(float64); ok {
				cache.Lat = lat
			}
			if lng, ok := location["lng"].(float64); ok {
				cache.Lng = lng
			}
		}
	}
	if rating, ok := candidate["rating"].(float64); ok {
		cache.Rating = rating
	}

	// 使用 upsert 更新或插入
	filter := bson.M{"query": query, "place_id": cache.PlaceID}
	update := bson.M{"$set": cache}
	opts := options.Update().SetUpsert(true)
	coll.UpdateOne(ctx, filter, update, opts)
}

// CacheGooglePlaceDetails 快取 Place Details 的結果到 MongoDB
func (s *GooglePlaceCacheService) CacheGooglePlaceDetails(ctx context.Context, placeResult map[string]interface{}) {
	coll := s.MongoDB.GetCollection("google_place_cache")

	cache := model.GooglePlaceCache{
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if placeID, ok := placeResult["place_id"].(string); ok {
		cache.PlaceID = placeID
	}
	if name, ok := placeResult["name"].(string); ok {
		cache.Name = name
	}
	if address, ok := placeResult["formatted_address"].(string); ok {
		cache.Address = address
	}
	if geometry, ok := placeResult["geometry"].(map[string]interface{}); ok {
		if location, ok := geometry["location"].(map[string]interface{}); ok {
			if lat, ok := location["lat"].(float64); ok {
				cache.Lat = lat
			}
			if lng, ok := location["lng"].(float64); ok {
				cache.Lng = lng
			}
		}
	}
	if website, ok := placeResult["website"].(string); ok {
		cache.Website = website
	}
	if types, ok := placeResult["types"].([]interface{}); ok {
		cache.Types = make([]string, len(types))
		for i, t := range types {
			if typeStr, ok := t.(string); ok {
				cache.Types[i] = typeStr
			}
		}
	}
	if rating, ok := placeResult["rating"].(float64); ok {
		cache.Rating = rating
	}

	// 使用 upsert 更新或插入
	filter := bson.M{"place_id": cache.PlaceID}
	update := bson.M{"$set": cache}
	opts := options.Update().SetUpsert(true)
	coll.UpdateOne(ctx, filter, update, opts)
}

// BuildCandidatesFromCache 從快取結果建構候選結果
func (s *GooglePlaceCacheService) BuildCandidatesFromCache(cachedResults []model.GooglePlaceCache) []interface{} {
	var candidates []interface{}
	for _, cached := range cachedResults {
		// 優先使用主記錄的 address、lat、lng，而不是 candidates 中的資料
		// 這確保了地址的一致性，特別是在編輯後
		mainAddress := cached.Address
		mainLat := cached.Lat
		mainLng := cached.Lng
		mainName := cached.Name
		mainPlaceID := cached.PlaceID
		mainRating := cached.Rating

		// 如果主記錄沒有地址資訊，才使用第一個候選項目作為備用
		if mainAddress == "" && len(cached.Candidates) > 0 {
			firstCandidate := cached.Candidates[0]
			mainAddress = firstCandidate.Address
			mainLat = firstCandidate.Lat
			mainLng = firstCandidate.Lng
			if mainName == "" {
				mainName = firstCandidate.Name
			}
			if mainPlaceID == "" {
				mainPlaceID = firstCandidate.PlaceID
			}
			if mainRating == 0 {
				mainRating = firstCandidate.Rating
			}
		}

		// 建立候選結果，優先使用主記錄資料
		candidate := map[string]interface{}{
			"place_id":          mainPlaceID,
			"name":              mainName,
			"formatted_address": mainAddress,
			"geometry": map[string]interface{}{
				"location": map[string]interface{}{
					"lat": mainLat,
					"lng": mainLng,
				},
			},
			"fromCache": true,
		}
		if mainRating > 0 {
			candidate["rating"] = mainRating
		}
		if len(cached.Tags) > 0 {
			candidate["tags"] = cached.Tags
		}
		candidates = append(candidates, candidate)
	}
	return candidates
}
