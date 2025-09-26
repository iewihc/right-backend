package common

// PaginationInput 分頁輸入介面，定義分頁參數的獲取方法
type PaginationInput interface {
	GetPageNum() int
	GetPageSize() int
}

// BasePaginationInput 基礎分頁輸入結構，供其他結構嵌入使用
type BasePaginationInput struct {
	PageNum  int `query:"pageNum" default:"1" doc:"當前頁碼（從 1 開始計數）"`
	PageSize int `query:"pageSize" default:"10" doc:"每頁返回的數據條數"`
}

// GetPageNum 實現 PaginationInput 介面
func (p *BasePaginationInput) GetPageNum() int {
	if p.PageNum <= 0 {
		return 1
	}
	return p.PageNum
}

// GetPageSize 實現 PaginationInput 介面
func (p *BasePaginationInput) GetPageSize() int {
	if p.PageSize <= 0 {
		return 10
	}
	return p.PageSize
}

// BaseSearchPaginationInput 帶搜尋功能的基礎分頁輸入結構
type BaseSearchPaginationInput struct {
	BasePaginationInput
	SearchKeyword string `query:"search" doc:"搜尋關鍵字"`
}

// GetSearchKeyword 獲取搜尋關鍵字
func (p *BaseSearchPaginationInput) GetSearchKeyword() string {
	return p.SearchKeyword
}

// PaginationInfo 分頁資訊結構，供全專案共用
type PaginationInfo struct {
	CurrentPage int   `json:"currentPage" doc:"當前頁碼"`
	PageSize    int   `json:"pageSize" doc:"每頁數據條數"`
	TotalItems  int64 `json:"totalItems" doc:"總數據條數"`
	TotalPages  int   `json:"totalPages" doc:"總頁數"`
}

// NewPaginationInfo 創建分頁資訊
func NewPaginationInfo(pageNum, pageSize int, totalItems int64) PaginationInfo {
	totalPages := int((totalItems + int64(pageSize) - 1) / int64(pageSize))
	return PaginationInfo{
		CurrentPage: pageNum,
		PageSize:    pageSize,
		TotalItems:  totalItems,
		TotalPages:  totalPages,
	}
}

// CalculateOffset 計算跳過的記錄數量
func CalculateOffset(pageNum, pageSize int) int {
	return (pageNum - 1) * pageSize
}

// PaginationService 通用分頁服務介面
type PaginationService interface {
	// 計算分頁資訊並返回 offset 和 limit
	GetPaginationParams(input PaginationInput) (offset, limit int)
	// 創建分頁響應
	CreatePaginationInfo(input PaginationInput, totalItems int64) PaginationInfo
}

// DefaultPaginationService 默認分頁服務實現
type DefaultPaginationService struct{}

// GetPaginationParams 計算分頁參數
func (s *DefaultPaginationService) GetPaginationParams(input PaginationInput) (offset, limit int) {
	pageNum := input.GetPageNum()
	pageSize := input.GetPageSize()
	return CalculateOffset(pageNum, pageSize), pageSize
}

// CreatePaginationInfo 創建分頁資訊
func (s *DefaultPaginationService) CreatePaginationInfo(input PaginationInput, totalItems int64) PaginationInfo {
	return NewPaginationInfo(input.GetPageNum(), input.GetPageSize(), totalItems)
}
