package controller

import (
	"context"
	"right-backend/data-models/common"
	"right-backend/data-models/google_place_cache"
	"right-backend/model"
	"right-backend/service"

	"github.com/danielgtaylor/huma/v2"
	"github.com/rs/zerolog"
	"go.mongodb.org/mongo-driver/bson"
)

type GooglePlaceCacheController struct {
	logger  zerolog.Logger
	Service *service.GooglePlaceCacheService
}

func NewGooglePlaceCacheController(logger zerolog.Logger, svc *service.GooglePlaceCacheService) *GooglePlaceCacheController {
	return &GooglePlaceCacheController{
		logger:  logger.With().Str("module", "google_place_cache_controller").Logger(),
		Service: svc,
	}
}

func (c *GooglePlaceCacheController) RegisterRoutes(api huma.API) {
	// 1. 創建 Google Place Cache
	huma.Register(api, huma.Operation{
		OperationID: "create-google-place-cache",
		Tags:        []string{"google-place-cache"},
		Summary:     "創建 Google Place Cache",
		Method:      "POST",
		Path:        "/google-place-cache",
	}, func(ctx context.Context, input *google_place_cache.CreateGooglePlaceCacheInput) (*google_place_cache.CreateGooglePlaceCacheResponse, error) {
		cache := &model.GooglePlaceCache{
			Query:   input.Body.Query,
			Name:    input.Body.Name,
			Address: input.Body.Address,
			Lat:     input.Body.Lat,
			Lng:     input.Body.Lng,
			Tags:    input.Body.Tags,
		}

		result, err := c.Service.CreateGooglePlaceCache(ctx, cache)
		if err != nil {
			c.logger.Error().Err(err).Str("query", input.Body.Query).Msg("創建失敗")
			return nil, huma.Error400BadRequest("創建失敗", err)
		}

		return &google_place_cache.CreateGooglePlaceCacheResponse{
			Body: c.convertToOutput(result),
		}, nil
	})

	// 2. 取得單筆 Google Place Cache
	huma.Register(api, huma.Operation{
		OperationID: "get-google-place-cache",
		Tags:        []string{"google-place-cache"},
		Summary:     "取得單筆 Google Place Cache",
		Method:      "GET",
		Path:        "/google-place-cache/{id}",
	}, func(ctx context.Context, input *google_place_cache.GetGooglePlaceCacheInput) (*google_place_cache.GetGooglePlaceCacheResponse, error) {
		result, err := c.Service.GetGooglePlaceCache(ctx, input.ID)
		if err != nil {
			c.logger.Error().Err(err).Str("id", input.ID).Msg("記錄不存在")
			return nil, huma.Error404NotFound("記錄不存在", err)
		}

		return &google_place_cache.GetGooglePlaceCacheResponse{
			Body: c.convertToOutput(result),
		}, nil
	})

	// 3. 更新 Google Place Cache
	huma.Register(api, huma.Operation{
		OperationID: "update-google-place-cache",
		Tags:        []string{"google-place-cache"},
		Summary:     "更新 Google Place Cache",
		Method:      "PUT",
		Path:        "/google-place-cache/{id}",
	}, func(ctx context.Context, input *google_place_cache.UpdateGooglePlaceCacheInput) (*google_place_cache.UpdateGooglePlaceCacheResponse, error) {
		updateData := bson.M{}

		if input.Body.Query != "" {
			updateData["query"] = input.Body.Query
		}
		if input.Body.Name != "" {
			updateData["name"] = input.Body.Name
		}
		if input.Body.Address != "" {
			updateData["address"] = input.Body.Address
		}
		if input.Body.Lat != nil {
			updateData["lat"] = *input.Body.Lat
		}
		if input.Body.Lng != nil {
			updateData["lng"] = *input.Body.Lng
		}
		if input.Body.Tags != nil {
			updateData["tags"] = input.Body.Tags
		}

		result, err := c.Service.UpdateGooglePlaceCache(ctx, input.ID, updateData)
		if err != nil {
			c.logger.Error().Err(err).Str("id", input.ID).Msg("更新失敗")
			return nil, huma.Error400BadRequest("更新失敗", err)
		}

		return &google_place_cache.UpdateGooglePlaceCacheResponse{
			Body: c.convertToOutput(result),
		}, nil
	})

	// 4. 列出 Google Place Cache
	huma.Register(api, huma.Operation{
		OperationID: "list-google-place-cache",
		Tags:        []string{"google-place-cache"},
		Summary:     "列出 Google Place Cache",
		Method:      "GET",
		Path:        "/google-place-cache",
	}, func(ctx context.Context, input *google_place_cache.ListGooglePlaceCacheInput) (*google_place_cache.ListGooglePlaceCacheResponse, error) {
		// 使用統一介面獲取分頁參數
		pageNum := input.GetPageNum()
		pageSize := input.GetPageSize()

		// 計算 offset
		offset := (pageNum - 1) * pageSize

		caches, total, err := c.Service.ListGooglePlaceCache(ctx, input.Keyword, pageSize, offset)
		if err != nil {
			c.logger.Error().Err(err).Str("keyword", input.Keyword).Msg("查詢失敗")
			return nil, huma.Error400BadRequest("查詢失敗", err)
		}

		data := make([]google_place_cache.GooglePlaceCacheOutput, len(caches))
		for i, cache := range caches {
			data[i] = c.convertToOutput(&cache)
		}

		response := &google_place_cache.ListGooglePlaceCacheResponse{}
		response.Body.Data = data
		response.Body.Pagination = common.NewPaginationInfo(pageNum, pageSize, int64(total))

		return response, nil
	})

	// 5. 刪除 Google Place Cache
	huma.Register(api, huma.Operation{
		OperationID: "delete-google-place-cache",
		Tags:        []string{"google-place-cache"},
		Summary:     "刪除 Google Place Cache",
		Method:      "DELETE",
		Path:        "/google-place-cache/{id}",
	}, func(ctx context.Context, input *google_place_cache.DeleteGooglePlaceCacheInput) (*google_place_cache.DeleteGooglePlaceCacheResponse, error) {
		err := c.Service.DeleteGooglePlaceCache(ctx, input.ID)
		if err != nil {
			c.logger.Error().Err(err).Str("id", input.ID).Msg("記錄不存在")
			return nil, huma.Error404NotFound("記錄不存在", err)
		}

		return &google_place_cache.DeleteGooglePlaceCacheResponse{
			Body: struct {
				Message string `json:"message" example:"記錄已刪除"`
			}{
				Message: "記錄已刪除",
			},
		}, nil
	})

	// 6. 添加標籤
	huma.Register(api, huma.Operation{
		OperationID: "add-tags-to-google-place-cache",
		Tags:        []string{"google-place-cache"},
		Summary:     "為 Google Place Cache 添加標籤",
		Method:      "POST",
		Path:        "/google-place-cache/{id}/tags",
	}, func(ctx context.Context, input *google_place_cache.AddTagsInput) (*google_place_cache.AddTagsResponse, error) {
		result, err := c.Service.AddTags(ctx, input.ID, input.Body.Tags)
		if err != nil {
			c.logger.Error().Err(err).Str("id", input.ID).Strs("tags", input.Body.Tags).Msg("添加標籤失敗")
			return nil, huma.Error400BadRequest("添加標籤失敗", err)
		}

		return &google_place_cache.AddTagsResponse{
			Body: c.convertToOutput(result),
		}, nil
	})

	// 7. 移除標籤
	huma.Register(api, huma.Operation{
		OperationID: "remove-tags-from-google-place-cache",
		Tags:        []string{"google-place-cache"},
		Summary:     "移除 Google Place Cache 標籤",
		Method:      "DELETE",
		Path:        "/google-place-cache/{id}/tags",
	}, func(ctx context.Context, input *google_place_cache.RemoveTagsInput) (*google_place_cache.RemoveTagsResponse, error) {
		result, err := c.Service.RemoveTags(ctx, input.ID, input.Body.Tags)
		if err != nil {
			c.logger.Error().Err(err).Str("id", input.ID).Strs("tags", input.Body.Tags).Msg("移除標籤失敗")
			return nil, huma.Error400BadRequest("移除標籤失敗", err)
		}

		return &google_place_cache.RemoveTagsResponse{
			Body: c.convertToOutput(result),
		}, nil
	})
}

// convertToOutput 轉換為輸出格式
func (c *GooglePlaceCacheController) convertToOutput(cache *model.GooglePlaceCache) google_place_cache.GooglePlaceCacheOutput {
	output := google_place_cache.GooglePlaceCacheOutput{
		ID:        cache.ID.Hex(),
		Query:     cache.Query,
		Name:      cache.Name,
		Address:   cache.Address,
		Lat:       cache.Lat,
		Lng:       cache.Lng,
		Tags:      cache.Tags,
		CreatedAt: cache.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt: cache.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}

	// 轉換候選結果
	if len(cache.Candidates) > 0 {
		output.Candidates = make([]google_place_cache.GooglePlaceCandidate, len(cache.Candidates))
		for i, candidate := range cache.Candidates {
			output.Candidates[i] = google_place_cache.GooglePlaceCandidate{
				Name:    candidate.Name,
				Address: candidate.Address,
				Lat:     candidate.Lat,
				Lng:     candidate.Lng,
			}
		}
	}

	return output
}
