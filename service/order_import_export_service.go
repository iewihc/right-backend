package service

import (
	"context"
	"fmt"
	"mime/multipart"
	orderModels "right-backend/data-models/order"
	"right-backend/infra"
	"right-backend/model"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"github.com/xuri/excelize/v2"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type OrderImportExportService struct {
	logger  zerolog.Logger
	mongoDB *infra.MongoDB
}

func NewOrderImportExportService(logger zerolog.Logger, mongoDB *infra.MongoDB) *OrderImportExportService {
	return &OrderImportExportService{
		logger:  logger.With().Str("module", "order_import_export_service").Logger(),
		mongoDB: mongoDB,
	}
}

// CheckImportOrders 檢查匯入訂單並返回預覽資訊
func (s *OrderImportExportService) CheckImportOrders(ctx context.Context, fleet string, hasHeader bool, file multipart.File) (int, int, error) {
	// 讀取 Excel 檔案
	f, err := excelize.OpenReader(file)
	if err != nil {
		return 0, 0, fmt.Errorf("無法開啟 Excel 檔案: %w", err)
	}
	defer f.Close()

	// 獲取第一個工作表
	sheets := f.GetSheetList()
	if len(sheets) == 0 {
		return 0, 0, fmt.Errorf("Excel 檔案中沒有工作表")
	}

	// 讀取所有行
	rows, err := f.GetRows(sheets[0])
	if err != nil {
		return 0, 0, fmt.Errorf("無法讀取 Excel 行: %w", err)
	}

	// 計算要匯入的訂單數量
	importCount := 0
	startRow := 0
	if hasHeader {
		startRow = 1 // 跳過標題行
	}

	for i := startRow; i < len(rows); i++ {
		row := rows[i]
		// 檢查該行是否為空白行（所有欄位都是空的）
		isEmpty := true
		for _, cell := range row {
			if strings.TrimSpace(cell) != "" {
				isEmpty = false
				break
			}
		}
		if !isEmpty {
			importCount++
		}
	}

	// 獲取當前訂單總數
	collection := s.mongoDB.GetCollection("orders")
	filter := bson.M{"fleet": fleet}
	currentCount, err := collection.CountDocuments(ctx, filter)
	if err != nil {
		return 0, 0, fmt.Errorf("無法查詢當前訂單數量: %w", err)
	}

	return int(currentCount), importCount, nil
}

// ImportOrders 執行訂單匯入
func (s *OrderImportExportService) ImportOrders(ctx context.Context, fleet string, hasHeader bool, file multipart.File) (int, int, []string, error) {
	// 讀取 Excel 檔案
	f, err := excelize.OpenReader(file)
	if err != nil {
		return 0, 0, nil, fmt.Errorf("無法開啟 Excel 檔案: %w", err)
	}
	defer f.Close()

	// 獲取第一個工作表
	sheets := f.GetSheetList()
	if len(sheets) == 0 {
		return 0, 0, nil, fmt.Errorf("Excel 檔案中沒有工作表")
	}

	// 讀取所有行
	rows, err := f.GetRows(sheets[0])
	if err != nil {
		return 0, 0, nil, fmt.Errorf("無法讀取 Excel 行: %w", err)
	}

	collection := s.mongoDB.GetCollection("orders")
	successCount := 0
	failedCount := 0
	errors := []string{}

	startRow := 0
	if hasHeader {
		startRow = 1 // 跳過標題行
	}

	for i := startRow; i < len(rows); i++ {
		row := rows[i]

		// 檢查是否為空白行
		isEmpty := true
		for _, cell := range row {
			if strings.TrimSpace(cell) != "" {
				isEmpty = false
				break
			}
		}
		if isEmpty {
			continue // 跳過空白行
		}

		// 解析 Excel 行
		excelRow, err := s.parseExcelRow(row)
		if err != nil {
			errors = append(errors, fmt.Sprintf("第 %d 行: %v", i+1, err))
			failedCount++
			continue
		}

		// 檢查該訂單是否已存在（透過系統編號）
		var existingOrder *model.Order
		if excelRow.SystemID != "" {
			objectID, err := primitive.ObjectIDFromHex(excelRow.SystemID)
			if err == nil {
				filter := bson.M{"_id": objectID}
				err = collection.FindOne(ctx, filter).Decode(&existingOrder)
				if err == nil {
					// 訂單已存在，執行更新
					if err := s.updateExistingOrder(ctx, collection, objectID, excelRow); err != nil {
						errors = append(errors, fmt.Sprintf("第 %d 行: 更新失敗 - %v", i+1, err))
						failedCount++
						continue
					}
					successCount++
					continue
				}
			}
		}

		// 建立新訂單
		if err := s.createNewOrder(ctx, collection, fleet, excelRow); err != nil {
			errors = append(errors, fmt.Sprintf("第 %d 行: 建立失敗 - %v", i+1, err))
			failedCount++
			continue
		}
		successCount++
	}

	return successCount, failedCount, errors, nil
}

// ExportOrders 匯出訂單到 Excel
func (s *OrderImportExportService) ExportOrders(ctx context.Context, fleet, startDate, endDate string, hasHeader bool) (*excelize.File, error) {
	collection := s.mongoDB.GetCollection("orders")

	// 建立查詢條件
	filter := bson.M{}

	// 車隊過濾
	if fleet != "" {
		filter["fleet"] = fleet
	}

	// 日期過濾
	if startDate != "" || endDate != "" {
		dateFilter := bson.M{}

		if startDate != "" {
			// 解析開始日期並設定為 00:00:00
			startTime, err := time.Parse("2006-01-02", startDate)
			if err != nil {
				return nil, fmt.Errorf("開始日期格式錯誤: %w", err)
			}
			startTime = time.Date(startTime.Year(), startTime.Month(), startTime.Day(), 0, 0, 0, 0, time.UTC)
			dateFilter["$gte"] = startTime
		}

		if endDate != "" {
			// 解析結束日期並設定為 23:59:59
			endTime, err := time.Parse("2006-01-02", endDate)
			if err != nil {
				return nil, fmt.Errorf("結束日期格式錯誤: %w", err)
			}
			endTime = time.Date(endTime.Year(), endTime.Month(), endTime.Day(), 23, 59, 59, 999999999, time.UTC)
			dateFilter["$lte"] = endTime
		}

		filter["created_at"] = dateFilter
	}

	// 按建立時間排序
	opts := options.Find().SetSort(bson.D{{Key: "created_at", Value: 1}})

	// 查詢訂單
	cursor, err := collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, fmt.Errorf("查詢訂單失敗: %w", err)
	}
	defer cursor.Close(ctx)

	// 建立 Excel 檔案
	f := excelize.NewFile()
	sheetName := "訂單資料"
	index, err := f.NewSheet(sheetName)
	if err != nil {
		return nil, fmt.Errorf("建立工作表失敗: %w", err)
	}
	f.SetActiveSheet(index)

	// 寫入標題行
	rowNum := 1
	if hasHeader {
		headers := []string{"編號", "訂單日期", "時間", "客群", "乘客姓名/編號", "上車地點", "承接司機", "車隊/取消", "支出", "收入", "備註", "系統編號"}
		for col, header := range headers {
			cell, _ := excelize.CoordinatesToCellName(col+1, rowNum)
			f.SetCellValue(sheetName, cell, header)
		}
		rowNum++
	}

	// 寫入資料
	var order model.Order
	for cursor.Next(ctx) {
		if err := cursor.Decode(&order); err != nil {
			s.logger.Error().Err(err).Msg("解碼訂單失敗")
			continue
		}

		// 轉換為 Excel 行資料
		excelRow := s.convertOrderToExcelRow(&order)

		// 寫入各欄位
		cells := []interface{}{
			excelRow.ShortID,
			excelRow.OrderDate,
			excelRow.OrderTime,
			excelRow.CustomerGroup,
			excelRow.PassengerID,
			excelRow.OriText,
			excelRow.DriverName,
			excelRow.Fleet,
			excelRow.Expense,
			excelRow.Income,
			excelRow.AmountNote,
			excelRow.SystemID,
		}

		for col, value := range cells {
			cell, _ := excelize.CoordinatesToCellName(col+1, rowNum)
			f.SetCellValue(sheetName, cell, value)
		}
		rowNum++
	}

	// 刪除預設的 Sheet1
	if f.GetSheetName(0) == "Sheet1" {
		f.DeleteSheet("Sheet1")
	}

	return f, nil
}

// parseExcelRow 解析 Excel 行資料
func (s *OrderImportExportService) parseExcelRow(row []string) (*orderModels.ExcelOrderRow, error) {
	// 確保至少有必要的欄位
	if len(row) < 6 {
		return nil, fmt.Errorf("資料欄位不足")
	}

	// 解析支出和收入
	expense := 0
	if len(row) > 8 && row[8] != "" {
		val, err := strconv.Atoi(strings.TrimSpace(row[8]))
		if err == nil {
			expense = val
		}
	}

	income := 0
	if len(row) > 9 && row[9] != "" {
		val, err := strconv.Atoi(strings.TrimSpace(row[9]))
		if err == nil {
			income = val
		}
	}

	excelRow := &orderModels.ExcelOrderRow{
		ShortID:      getValue(row, 0),
		OrderDate:    getValue(row, 1),
		OrderTime:    getValue(row, 2),
		CustomerGroup: getValue(row, 3),
		PassengerID:  getValue(row, 4),
		OriText:      getValue(row, 5),
		DriverName:   getValue(row, 6),
		Fleet:        getValue(row, 7),
		Expense:      expense,
		Income:       income,
		AmountNote:   getValue(row, 10),
		SystemID:     getValue(row, 11),
	}

	return excelRow, nil
}

// updateExistingOrder 更新現有訂單
func (s *OrderImportExportService) updateExistingOrder(ctx context.Context, collection *mongo.Collection, orderID primitive.ObjectID, excelRow *orderModels.ExcelOrderRow) error {
	updateFields := bson.M{
		"updated_at": time.Now(),
	}

	// 更新建立時間（如果有提供日期和時間）
	if excelRow.OrderDate != "" && excelRow.OrderTime != "" {
		createdAt, err := s.parseDateTime(excelRow.OrderDate, excelRow.OrderTime)
		if err == nil {
			updateFields["created_at"] = createdAt
		}
	}

	// 更新其他欄位
	if excelRow.CustomerGroup != "" {
		updateFields["customer_group"] = excelRow.CustomerGroup
	}
	if excelRow.PassengerID != "" {
		updateFields["passenger_id"] = excelRow.PassengerID
	}
	if excelRow.OriText != "" {
		updateFields["ori_text"] = excelRow.OriText
	}
	if excelRow.DriverName != "" {
		updateFields["driver.name"] = excelRow.DriverName
	}
	if excelRow.Fleet != "" {
		updateFields["fleet"] = excelRow.Fleet
	}
	updateFields["expense"] = excelRow.Expense
	updateFields["income"] = excelRow.Income
	if excelRow.AmountNote != "" {
		updateFields["amount_note"] = excelRow.AmountNote
	}

	filter := bson.M{"_id": orderID}
	update := bson.M{"$set": updateFields}

	_, err := collection.UpdateOne(ctx, filter, update)
	return err
}

// createNewOrder 建立新訂單
func (s *OrderImportExportService) createNewOrder(ctx context.Context, collection *mongo.Collection, fleet string, excelRow *orderModels.ExcelOrderRow) error {
	now := time.Now()

	// 解析建立時間
	createdAt := now
	if excelRow.OrderDate != "" && excelRow.OrderTime != "" {
		parsedTime, err := s.parseDateTime(excelRow.OrderDate, excelRow.OrderTime)
		if err == nil {
			createdAt = parsedTime
		}
	}

	// 生成 short_id（如果沒有提供）
	shortID := excelRow.ShortID
	if shortID == "" {
		shortID = s.generateShortID()
	}

	// 建立訂單
	order := &model.Order{
		ShortID:       shortID,
		Type:          model.OrderTypeInstant,
		Status:        model.OrderStatusCompleted, // 預設為已完成
		CustomerGroup: excelRow.CustomerGroup,
		PassengerID:   excelRow.PassengerID,
		OriText:       excelRow.OriText,
		Fleet:         model.FleetType(excelRow.Fleet),
		AmountNote:    excelRow.AmountNote,
		Income:        &excelRow.Income,
		Expense:       &excelRow.Expense,
		CreatedAt:     &createdAt,
		UpdatedAt:     &now,
		CreatedBy:     "import",
		CreatedType:   "system",
		Driver: model.Driver{
			Name: excelRow.DriverName,
		},
		Customer: model.Customer{},
	}

	_, err := collection.InsertOne(ctx, order)
	return err
}

// convertOrderToExcelRow 將訂單轉換為 Excel 行資料
func (s *OrderImportExportService) convertOrderToExcelRow(order *model.Order) *orderModels.ExcelOrderRow {
	excelRow := &orderModels.ExcelOrderRow{
		ShortID:       order.ShortID,
		CustomerGroup: order.CustomerGroup,
		PassengerID:   order.PassengerID,
		OriText:       order.OriText,
		DriverName:    order.Driver.Name,
		Fleet:         string(order.Fleet),
		AmountNote:    order.AmountNote,
	}

	// 處理建立時間
	if order.CreatedAt != nil {
		// 台北時區 (UTC+8)
		taipei := time.FixedZone("Asia/Taipei", 8*3600)
		localTime := order.CreatedAt.In(taipei)
		excelRow.OrderDate = localTime.Format("2006-01-02")
		excelRow.OrderTime = localTime.Format("15:04")
	}

	// 處理支出和收入
	if order.Expense != nil {
		excelRow.Expense = *order.Expense
	}
	if order.Income != nil {
		excelRow.Income = *order.Income
	}

	// 處理系統編號
	if order.ID != nil {
		excelRow.SystemID = order.ID.Hex()
	}

	return excelRow
}

// parseDateTime 解析日期和時間字串
func (s *OrderImportExportService) parseDateTime(dateStr, timeStr string) (time.Time, error) {
	// 組合日期和時間字串
	datetimeStr := fmt.Sprintf("%s %s", dateStr, timeStr)

	// 嘗試多種格式
	formats := []string{
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
		"2006/01/02 15:04:05",
		"2006/01/02 15:04",
	}

	var parsedTime time.Time
	var err error

	for _, format := range formats {
		parsedTime, err = time.Parse(format, datetimeStr)
		if err == nil {
			// 轉換為 UTC+8（台北時區）
			taipei := time.FixedZone("Asia/Taipei", 8*3600)
			return time.Date(
				parsedTime.Year(),
				parsedTime.Month(),
				parsedTime.Day(),
				parsedTime.Hour(),
				parsedTime.Minute(),
				parsedTime.Second(),
				0,
				taipei,
			), nil
		}
	}

	return time.Time{}, fmt.Errorf("無法解析日期時間: %s", datetimeStr)
}

// generateShortID 生成訂單短 ID
func (s *OrderImportExportService) generateShortID() string {
	// 使用當前時間戳的後 4 位數字
	timestamp := time.Now().Unix()
	return fmt.Sprintf("#%d", timestamp%10000)
}

// getValue 安全地從切片中獲取值
func getValue(slice []string, index int) string {
	if index < len(slice) {
		return strings.TrimSpace(slice[index])
	}
	return ""
}
