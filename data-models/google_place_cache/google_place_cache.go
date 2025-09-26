package google_place_cache

import "right-backend/data-models/common"

// CreateGooglePlaceCacheInput 創建 Google Place Cache
type CreateGooglePlaceCacheInput struct {
	Body struct {
		Query   string   `json:"query" example:"光田綜合醫院" doc:"搜尋關鍵字"`
		Name    string   `json:"name" example:"光田綜合醫院向上院區" doc:"地點名稱"`
		Address string   `json:"address" example:"台中市沙鹿區向上路七段125號" doc:"完整地址"`
		Lat     float64  `json:"lat" example:"24.2266" doc:"緯度"`
		Lng     float64  `json:"lng" example:"120.5689" doc:"經度"`
		Tags    []string `json:"tags,omitempty" example:"[\"XC\",\"X Cube\",\"Cube\",\"XCube\"]" doc:"自定義標籤"`
	} `json:"body"`
}

// UpdateGooglePlaceCacheInput 更新 Google Place Cache
type UpdateGooglePlaceCacheInput struct {
	ID   string `path:"id" doc:"記錄 ID"`
	Body struct {
		Query   string   `json:"query,omitempty" example:"光田綜合醫院" doc:"搜尋關鍵字"`
		Name    string   `json:"name,omitempty" example:"光田綜合醫院向上院區" doc:"地點名稱"`
		Address string   `json:"address,omitempty" example:"台中市沙鹿區向上路七段125號" doc:"完整地址"`
		Lat     *float64 `json:"lat,omitempty" example:"24.2266" doc:"緯度"`
		Lng     *float64 `json:"lng,omitempty" example:"120.5689" doc:"經度"`
		Tags    []string `json:"tags,omitempty" example:"[\"XC\",\"X Cube\",\"Cube\",\"XCube\"]" doc:"自定義標籤"`
	} `json:"body"`
}

// GetGooglePlaceCacheInput 取得單筆 Google Place Cache
type GetGooglePlaceCacheInput struct {
	ID string `path:"id" doc:"記錄 ID"`
}

// ListGooglePlaceCacheInput 列出 Google Place Cache
type ListGooglePlaceCacheInput struct {
	common.BasePaginationInput
	Keyword string `query:"keyword,omitempty" example:"光田" doc:"關鍵字搜尋（會先搜尋標籤再搜尋名稱、地址等）"`
}

// DeleteGooglePlaceCacheInput 刪除 Google Place Cache
type DeleteGooglePlaceCacheInput struct {
	ID string `path:"id" doc:"記錄 ID"`
}

// AddTagsInput 為 Google Place Cache 添加標籤
type AddTagsInput struct {
	ID   string `path:"id" doc:"記錄 ID"`
	Body struct {
		Tags []string `json:"tags" example:"[\"hospital\",\"medical\"]" doc:"要添加的標籤"`
	} `json:"body"`
}

// RemoveTagsInput 移除 Google Place Cache 標籤
type RemoveTagsInput struct {
	ID   string `path:"id" doc:"記錄 ID"`
	Body struct {
		Tags []string `json:"tags" example:"[\"hospital\"]" doc:"要移除的標籤"`
	} `json:"body"`
}

// GooglePlaceCacheOutput 輸出格式
type GooglePlaceCacheOutput struct {
	ID         string                 `json:"id"`
	Query      string                 `json:"query"`
	Candidates []GooglePlaceCandidate `json:"candidates,omitempty"`
	Name       string                 `json:"name,omitempty"`
	Address    string                 `json:"address,omitempty"`
	Lat        float64                `json:"lat,omitempty"`
	Lng        float64                `json:"lng,omitempty"`
	Tags       []string               `json:"tags,omitempty"`
	CreatedAt  string                 `json:"created_at"`
	UpdatedAt  string                 `json:"updated_at"`
}

// GooglePlaceCandidate 候選結果
type GooglePlaceCandidate struct {
	Name    string  `json:"name"`
	Address string  `json:"formatted_address"`
	Lat     float64 `json:"lat"`
	Lng     float64 `json:"lng"`
}

// 回應格式
type CreateGooglePlaceCacheResponse struct {
	Body GooglePlaceCacheOutput `json:"body"`
}

type GetGooglePlaceCacheResponse struct {
	Body GooglePlaceCacheOutput `json:"body"`
}

type UpdateGooglePlaceCacheResponse struct {
	Body GooglePlaceCacheOutput `json:"body"`
}

type ListGooglePlaceCacheResponse struct {
	Body struct {
		Data       []GooglePlaceCacheOutput `json:"data" doc:"Google Place Cache 列表"`
		Pagination common.PaginationInfo    `json:"pagination" doc:"分頁資訊"`
	} `json:"body"`
}

type DeleteGooglePlaceCacheResponse struct {
	Body struct {
		Message string `json:"message" example:"記錄已刪除"`
	} `json:"body"`
}

type AddTagsResponse struct {
	Body GooglePlaceCacheOutput `json:"body"`
}

type RemoveTagsResponse struct {
	Body GooglePlaceCacheOutput `json:"body"`
}
