package service

import (
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

type FileStorageService struct {
	logger     zerolog.Logger
	uploadPath string
	baseURL    string
}

type FileUploadResult struct {
	URL           string
	FilePath      string
	RelativePath  string // 相對路徑，用於存儲到資料庫
	Size          int64
	AudioDuration *int // 音頻時長（秒）
}

func NewFileStorageService(logger zerolog.Logger, uploadPath, baseURL string) *FileStorageService {
	return &FileStorageService{
		logger:     logger.With().Str("module", "file_storage_service").Logger(),
		uploadPath: uploadPath,
		baseURL:    baseURL,
	}
}

// UploadFile 上傳文件到本地存儲
func (fs *FileStorageService) UploadFile(ctx context.Context, file multipart.File, header *multipart.FileHeader, subDir, prefix string) (*FileUploadResult, error) {
	// 創建目標目錄
	targetDir := filepath.Join(fs.uploadPath, subDir)
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return nil, fmt.Errorf("創建目錄失敗: %w", err)
	}

	// 生成唯一文件名
	ext := filepath.Ext(header.Filename)
	uniqueID := uuid.New().String()
	timestamp := time.Now().Unix()
	filename := fmt.Sprintf("%s_%s_%d%s", prefix, uniqueID[:8], timestamp, ext)

	filePath := filepath.Join(targetDir, filename)

	// 創建目標文件
	dst, err := os.Create(filePath)
	if err != nil {
		return nil, fmt.Errorf("創建文件失敗: %w", err)
	}
	defer dst.Close()

	// 復制文件內容
	size, err := io.Copy(dst, file)
	if err != nil {
		// 如果復制失敗，清理已創建的文件
		os.Remove(filePath)
		return nil, fmt.Errorf("保存文件失敗: %w", err)
	}

	// 生成訪問URL (baseURL 已經包含 uploads 前綴)
	relativeURL := filepath.Join(subDir, filename)
	// 將 Windows 路徑分隔符替換為 URL 分隔符
	relativeURL = strings.ReplaceAll(relativeURL, "\\", "/")
	url := fmt.Sprintf("%s/%s", strings.TrimSuffix(fs.baseURL, "/"), relativeURL)

	fs.logger.Info().
		Str("filename", filename).
		Str("path", filePath).
		Int64("size", size).
		Str("url", url).
		Msg("文件上傳成功")

	return &FileUploadResult{
		URL:           url,
		FilePath:      filePath,
		RelativePath:  relativeURL,
		Size:          size,
		AudioDuration: nil,
	}, nil
}

// UploadAudioFile 上傳音頻文件
func (fs *FileStorageService) UploadAudioFile(ctx context.Context, file multipart.File, header *multipart.FileHeader, orderID, messageID string) (*FileUploadResult, error) {
	// 驗證音頻文件類型
	if !fs.isValidAudioFile(header.Filename) {
		return nil, fmt.Errorf("不支持的音頻文件格式: %s", filepath.Ext(header.Filename))
	}

	// 驗證文件大小 (10MB 限制)
	if header.Size > 10*1024*1024 {
		return nil, fmt.Errorf("音頻文件太大: %d bytes (最大 10MB)", header.Size)
	}

	// 使用 chat/{orderID} 作為子目錄，存儲音頻文件
	subDir := filepath.Join("chat", orderID)
	prefix := fmt.Sprintf("chat_audio_%s", messageID)
	return fs.UploadFile(ctx, file, header, subDir, prefix)
}

// UploadImageFile 上傳圖片文件
func (fs *FileStorageService) UploadImageFile(ctx context.Context, file multipart.File, header *multipart.FileHeader, orderID, messageID string) (*FileUploadResult, error) {
	// 驗證圖片文件類型
	if !fs.isValidImageFile(header.Filename) {
		return nil, fmt.Errorf("不支持的圖片文件格式: %s", filepath.Ext(header.Filename))
	}

	// 驗證文件大小 (5MB 限制)
	if header.Size > 5*1024*1024 {
		return nil, fmt.Errorf("圖片文件太大: %d bytes (最大 5MB)", header.Size)
	}

	// 使用 chat/{orderID} 作為子目錄，存儲圖片文件
	subDir := filepath.Join("chat", orderID)
	prefix := fmt.Sprintf("chat_image_%s", messageID)
	return fs.UploadFile(ctx, file, header, subDir, prefix)
}

// UploadAvatarFile 上傳頭像文件
func (fs *FileStorageService) UploadAvatarFile(ctx context.Context, file multipart.File, header *multipart.FileHeader, driverID, carPlate string) (*FileUploadResult, error) {
	// 驗證圖片文件類型
	if !fs.isValidImageFile(header.Filename) {
		return nil, fmt.Errorf("不支持的頭像文件格式: %s", filepath.Ext(header.Filename))
	}

	// 驗證文件大小 (2MB 限制)
	if header.Size > 2*1024*1024 {
		return nil, fmt.Errorf("頭像文件太大: %d bytes (最大 2MB)", header.Size)
	}

	// 使用司機車牌作為子目錄，存儲頭像文件
	subDir := filepath.Join("avatars", carPlate)
	prefix := fmt.Sprintf("driver_%s_avatar", driverID)
	return fs.UploadFile(ctx, file, header, subDir, prefix)
}

// UploadPickupCertificateFile 上傳抵達證明文件
func (fs *FileStorageService) UploadPickupCertificateFile(ctx context.Context, file multipart.File, header *multipart.FileHeader, orderID, carPlate string) (*FileUploadResult, error) {
	// 驗證圖片文件類型
	if !fs.isValidImageFile(header.Filename) {
		return nil, fmt.Errorf("不支持的證明文件格式: %s", filepath.Ext(header.Filename))
	}

	// 使用訂單編號作為子目錄，存儲證明文件
	subDir := filepath.Join("certificate", orderID)
	prefix := fmt.Sprintf("pickup_certificate_%s", carPlate)

	return fs.UploadFile(ctx, file, header, subDir, prefix)
}

// DeleteFile 刪除文件
func (fs *FileStorageService) DeleteFile(ctx context.Context, filePath string) error {
	fullPath := filepath.Join(fs.uploadPath, filePath)

	if err := os.Remove(fullPath); err != nil {
		if os.IsNotExist(err) {
			fs.logger.Warn().Str("file_path", fullPath).Msg("要刪除的文件不存在")
			return nil // 文件不存在也視為成功
		}
		return fmt.Errorf("刪除文件失敗: %w", err)
	}

	fs.logger.Info().Str("file_path", fullPath).Msg("文件已刪除")
	return nil
}

// GetFileInfo 獲取文件信息
func (fs *FileStorageService) GetFileInfo(ctx context.Context, filePath string) (os.FileInfo, error) {
	fullPath := filepath.Join(fs.uploadPath, filePath)
	return os.Stat(fullPath)
}

// 驗證音頻文件類型
func (fs *FileStorageService) isValidAudioFile(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	validExts := []string{".mp3", ".wav", ".m4a", ".aac", ".ogg", ".flac"}

	for _, validExt := range validExts {
		if ext == validExt {
			return true
		}
	}
	return false
}

// 驗證圖片文件類型
func (fs *FileStorageService) isValidImageFile(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	validExts := []string{".jpg", ".jpeg", ".png", ".gif", ".webp", ".bmp"}

	for _, validExt := range validExts {
		if ext == validExt {
			return true
		}
	}
	return false
}

// InitializeUploadDirectories 初始化上傳目錄
func (fs *FileStorageService) InitializeUploadDirectories() error {
	dirs := []string{"audio", "images", "avatars", "certificate"}

	for _, dir := range dirs {
		fullDir := filepath.Join(fs.uploadPath, dir)
		if err := os.MkdirAll(fullDir, 0755); err != nil {
			return fmt.Errorf("創建目錄 %s 失敗: %w", fullDir, err)
		}
		fs.logger.Info().Str("directory", fullDir).Msg("上傳目錄已創建")
	}

	return nil
}
