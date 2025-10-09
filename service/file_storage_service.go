package service

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"
	"time"

	"sync"

	"github.com/chai2010/webp"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
	_ "golang.org/x/image/webp"
)

type FileStorageService struct {
	logger         zerolog.Logger
	uploadPath     string
	baseURL        string
	watermarkFont  font.Face // 緩存浮水印字體
	fontLoadOnce   sync.Once
	fontLoadError  error
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

// loadWatermarkFont 載入浮水印字體（只執行一次）
func (fs *FileStorageService) loadWatermarkFont() font.Face {
	fs.fontLoadOnce.Do(func() {
		// 嘗試載入常見的中文字體路徑（Ubuntu/Debian）
		fontPaths := []string{
			// Noto Sans CJK (優先，完整支援繁體中文 UTF-8)
			"/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc",
			"/usr/share/fonts/truetype/noto/NotoSansCJK-Regular.ttc",
			"/usr/share/fonts/google-noto-cjk/NotoSansCJK-Regular.ttc",
			// WQY 文泉驛字體（備用）
			"/usr/share/fonts/truetype/wqy/wqy-zenhei.ttc",
			"/usr/share/fonts/truetype/wqy/wqy-microhei.ttc",
		}

		var fontData []byte
		var fontPath string
		for _, path := range fontPaths {
			data, err := os.ReadFile(path)
			if err == nil {
				fontData = data
				fontPath = path
				fs.logger.Info().
					Str("font_path", fontPath).
					Int("font_size_bytes", len(fontData)).
					Msg("成功載入中文字體檔案")
				break
			}
		}

		if fontData != nil {
			// 嘗試解析 TTC 字體
			ttc, err := opentype.ParseCollection(fontData)
			if err == nil && ttc.NumFonts() > 0 {
				fs.logger.Info().Int("num_fonts", ttc.NumFonts()).Msg("成功解析 TTC 字體集合")
				// Font(0) 返回 (*sfnt.Font, error)
				firstFont, fontErr := ttc.Font(0)
				if fontErr != nil {
					fs.logger.Warn().Err(fontErr).Msg("無法取得 TTC 第一個字體")
				} else {
					fs.watermarkFont, fs.fontLoadError = opentype.NewFace(firstFont, &opentype.FaceOptions{
						Size:    24,
						DPI:     72,
						Hinting: font.HintingFull,
					})
					if fs.fontLoadError == nil {
						fs.logger.Info().Msg("✓ 成功創建中文字體 Face")
						return
					}
					fs.logger.Warn().Err(fs.fontLoadError).Msg("創建 TTC 字體 Face 失敗")
				}
			}

			// 嘗試解析為單一 TTF
			ttf, err := opentype.Parse(fontData)
			if err == nil {
				fs.watermarkFont, fs.fontLoadError = opentype.NewFace(ttf, &opentype.FaceOptions{
					Size:    24,
					DPI:     72,
					Hinting: font.HintingFull,
				})
				if fs.fontLoadError == nil {
					fs.logger.Info().Msg("✓ 成功創建 TTF 字體 Face")
					return
				}
			}
		}

		// 使用 Go 內建字體作為後備
		fs.logger.Warn().Msg("未能載入中文字體，使用 Go 內建字體")
		ttf, _ := opentype.Parse(goregular.TTF)
		fs.watermarkFont, _ = opentype.NewFace(ttf, &opentype.FaceOptions{
			Size:    24,
			DPI:     72,
			Hinting: font.HintingFull,
		})
	})

	return fs.watermarkFont
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

// bytesReaderWithCloser wraps bytes.Reader to implement multipart.File
type bytesReaderWithCloser struct {
	*bytes.Reader
}

func (b *bytesReaderWithCloser) Close() error {
	return nil
}

// addWatermarkToImage 為圖片添加浮水印
func (fs *FileStorageService) addWatermarkToImage(imgData image.Image, watermarkText string, oriText string) (image.Image, error) {
	bounds := imgData.Bounds()
	rgba := image.NewRGBA(bounds)
	draw.Draw(rgba, bounds, imgData, bounds.Min, draw.Src)

	// 設置字體顏色（純白色，不透明）
	col := color.RGBA{255, 255, 255, 255}

	// 使用緩存的字體（第一次呼叫時會載入）
	fontFace := fs.loadWatermarkFont()

	drawer := &font.Drawer{
		Dst:  rgba,
		Src:  image.NewUniform(col),
		Face: fontFace,
		Dot:  fixed.Point26_6{},
	}

	// 準備要顯示的文字行
	lines := []string{watermarkText}
	if oriText != "" {
		lines = append(lines, oriText)
	}

	// 計算所有行的最大寬度
	maxWidth := 0
	for _, line := range lines {
		lineWidth := drawer.MeasureString(line).Ceil()
		if lineWidth > maxWidth {
			maxWidth = lineWidth
		}
	}

	// 行高設定
	lineHeight := 30

	// 設置位置：右上角，留15像素邊距
	startX := bounds.Max.X - maxWidth - 15
	startY := 35 // 第一行從頂部開始35像素

	// 計算背景矩形大小
	bgHeight := len(lines)*lineHeight + 8
	bgRect := image.Rect(startX-8, startY-28, startX+maxWidth+8, startY-28+bgHeight)

	// 繪製背景（黑色半透明矩形）
	bgColor := color.RGBA{0, 0, 0, 180}
	draw.Draw(rgba, bgRect, &image.Uniform{bgColor}, image.Point{}, draw.Over)

	// 繪製每一行文字
	for i, line := range lines {
		y := startY + (i * lineHeight)
		drawer.Dot = fixed.Point26_6{
			X: fixed.I(startX),
			Y: fixed.I(y),
		}
		drawer.DrawString(line)
	}

	return rgba, nil
}

// processImageWithWatermark 處理圖片並添加浮水印
func (fs *FileStorageService) processImageWithWatermark(file multipart.File, header *multipart.FileHeader, watermarkTime time.Time, oriText string) (multipart.File, *multipart.FileHeader, error) {
	// 讀取圖片
	imgData, format, err := image.Decode(file)
	if err != nil {
		return nil, nil, fmt.Errorf("解碼圖片失敗: %w", err)
	}

	// 添加浮水印（使用傳入的時間和 ori_text）
	timestamp := watermarkTime.Format("2006-01-02 15:04:05")
	watermarkedImg, err := fs.addWatermarkToImage(imgData, timestamp, oriText)
	if err != nil {
		return nil, nil, fmt.Errorf("添加浮水印失敗: %w", err)
	}

	// 將處理後的圖片編碼回 bytes，盡量保持原格式
	var buf bytes.Buffer
	var outputFormat string
	switch format {
	case "jpeg", "jpg":
		if err := jpeg.Encode(&buf, watermarkedImg, &jpeg.Options{Quality: 90}); err != nil {
			return nil, nil, fmt.Errorf("編碼JPEG失敗: %w", err)
		}
		outputFormat = "jpeg"
	case "png":
		if err := png.Encode(&buf, watermarkedImg); err != nil {
			return nil, nil, fmt.Errorf("編碼PNG失敗: %w", err)
		}
		outputFormat = "png"
	case "webp":
		// 保持 webp 格式輸出
		if err := webp.Encode(&buf, watermarkedImg, &webp.Options{Lossless: false, Quality: 90}); err != nil {
			return nil, nil, fmt.Errorf("編碼WebP失敗: %w", err)
		}
		outputFormat = "webp"
	case "gif", "bmp":
		// gif, bmp 等格式轉換為 PNG（無損）
		if err := png.Encode(&buf, watermarkedImg); err != nil {
			return nil, nil, fmt.Errorf("編碼PNG失敗: %w", err)
		}
		outputFormat = "png"
		fs.logger.Info().
			Str("original_format", format).
			Str("output_format", "png").
			Msg("圖片格式已轉換為PNG")
	default:
		// 其他格式轉為 JPEG
		if err := jpeg.Encode(&buf, watermarkedImg, &jpeg.Options{Quality: 90}); err != nil {
			return nil, nil, fmt.Errorf("編碼JPEG失敗: %w", err)
		}
		outputFormat = "jpeg"
		fs.logger.Warn().
			Str("original_format", format).
			Str("output_format", "jpeg").
			Msg("未知圖片格式，已轉換為JPEG")
	}

	// 創建新的 multipart.File
	reader := bytes.NewReader(buf.Bytes())
	newFile := &bytesReaderWithCloser{Reader: reader}

	// 創建新的 header
	newHeader := &multipart.FileHeader{
		Filename: header.Filename,
		Size:     int64(buf.Len()),
	}

	fs.logger.Info().
		Str("filename", header.Filename).
		Str("input_format", format).
		Str("output_format", outputFormat).
		Int("original_size", int(header.Size)).
		Int("new_size", buf.Len()).
		Str("timestamp", timestamp).
		Msg("圖片浮水印添加成功")

	return newFile, newHeader, nil
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
func (fs *FileStorageService) UploadPickupCertificateFile(ctx context.Context, file multipart.File, header *multipart.FileHeader, orderID, carPlate string, arrivalTime *time.Time, oriText string) (*FileUploadResult, error) {
	// 驗證圖片文件類型
	if !fs.isValidImageFile(header.Filename) {
		return nil, fmt.Errorf("不支持的證明文件格式: %s", filepath.Ext(header.Filename))
	}

	// 決定浮水印時間：優先使用 arrivalTime，否則使用當前時間
	watermarkTime := time.Now()
	if arrivalTime != nil {
		watermarkTime = *arrivalTime
	}

	// 轉換為台北時間 (UTC+8)
	taipeiLocation := time.FixedZone("Asia/Taipei", 8*60*60)
	watermarkTime = watermarkTime.In(taipeiLocation)

	fs.logger.Info().
		Str("filename", header.Filename).
		Str("order_id", orderID).
		Str("car_plate", carPlate).
		Time("watermark_time", watermarkTime).
		Str("ori_text", oriText).
		Msg("開始處理抵達證明文件，準備添加浮水印")

	// 添加浮水印
	processedFile, processedHeader, err := fs.processImageWithWatermark(file, header, watermarkTime, oriText)
	if err != nil {
		fs.logger.Error().Err(err).
			Str("filename", header.Filename).
			Str("order_id", orderID).
			Msg("添加浮水印失敗，使用原始圖片")

		// 如果浮水印添加失敗，需要重置文件讀取位置
		if seeker, ok := file.(io.Seeker); ok {
			seeker.Seek(0, io.SeekStart)
		}
		processedFile = file
		processedHeader = header
	} else {
		fs.logger.Info().
			Str("filename", header.Filename).
			Str("order_id", orderID).
			Msg("浮水印添加成功")
	}

	// 使用訂單編號作為子目錄，存儲證明文件
	subDir := filepath.Join("certificate", orderID)
	prefix := fmt.Sprintf("pickup_certificate_%s", carPlate)

	return fs.UploadFile(ctx, processedFile, processedHeader, subDir, prefix)
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
