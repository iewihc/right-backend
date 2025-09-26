package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"right-backend/model"
	"right-backend/service"
	"sync"
	"time"

	websocketModels "right-backend/data-models/websocket"

	"github.com/danielgtaylor/huma/v2"
	"github.com/rs/zerolog"
)

type ChatController struct {
	logger      zerolog.Logger
	chatService *service.ChatService
	// 簡單的內存緩存，用於關聯 tempID 和文件 URL
	fileURLCache map[string]string // key: orderID:tempID:fileType, value: fileURL
	cacheMutex   sync.RWMutex
}

func NewChatController(logger zerolog.Logger, chatService *service.ChatService) *ChatController {
	return &ChatController{
		logger:       logger.With().Str("module", "chat_controller").Logger(),
		chatService:  chatService,
		fileURLCache: make(map[string]string),
	}
}

// 文件上傳相關的請求和回應結構體

type UploadFileRequest struct {
	RawBody huma.MultipartFormFiles[struct {
		OrderID       string        `form:"orderId" required:"true" doc:"訂單ID"`
		MessageID     string        `form:"messageId" required:"true" doc:"消息ID"`
		FileType      string        `form:"fileType" required:"true" doc:"文件類型 (audio/image)"`
		File          huma.FormFile `form:"file" required:"true" doc:"上傳的文件"`
		AudioDuration int           `form:"audioDuration" doc:"音頻時長(秒)，僅音頻文件需要"`
	}]
}

type FileUploadResponse struct {
	Body websocketModels.FileUploadResponse
}

// 聊天房間相關的請求和回應結構體

type GetChatRoomsRequest struct {
	UserID string `query:"userId" validate:"required" doc:"用戶ID"`
}

type GetChatRoomsResponse struct {
	Body websocketModels.ChatRoomsResponse
}

// 聊天統計相關的請求和回應結構體

type GetChatStatisticsRequest struct {
	UserID string `query:"userId" validate:"required" doc:"用戶ID"`
}

type GetChatStatisticsResponse struct {
	Body struct {
		Success bool                           `json:"success"`
		Data    *service.ChatStatistics        `json:"data,omitempty"`
		Error   *websocketModels.ErrorResponse `json:"error,omitempty"`
	}
}

type GetSystemChatStatisticsRequest struct {
	// 系統統計不需要額外參數
}

type GetSystemChatStatisticsResponse struct {
	Body struct {
		Success bool                           `json:"success"`
		Data    *service.SystemChatStatistics  `json:"data,omitempty"`
		Error   *websocketModels.ErrorResponse `json:"error,omitempty"`
	}
}

// RegisterRoutes 註冊路由
// GetUserRecentChatsRequest 查詢用戶最近聊天記錄請求
type GetUserRecentChatsRequest struct {
	UserID string `path:"userId" doc:"用戶ID"`
	Limit  int    `query:"limit" default:"10" doc:"返回記錄數量限制，默認10"`
}

// GetUserRecentChatsResponse 查詢用戶最近聊天記錄回應
type GetUserRecentChatsResponse struct {
	Body struct {
		ChatRooms []websocketModels.ChatRoomInfo `json:"chatRooms" doc:"聊天房間列表"`
		Count     int                            `json:"count" doc:"總數量"`
	}
}

func (cc *ChatController) RegisterRoutes(api huma.API) {
	// 新增用戶最近聊天記錄查詢端點
	huma.Register(api, huma.Operation{
		OperationID:   "getUserRecentChats",
		Method:        http.MethodGet,
		Path:          "/api/v1/users/{userId}/chats/recent",
		Summary:       "查詢用戶最近10筆聊天記錄",
		Description:   "查詢指定用戶的最近聊天記錄，用於客服人員查看歷史聊天",
		Tags:          []string{"Chat"},
		DefaultStatus: http.StatusOK,
	}, cc.GetUserRecentChats)

	// 統一文件上傳端點
	huma.Register(api, huma.Operation{
		OperationID:   "upload-chat-file",
		Method:        http.MethodPost,
		Path:          "/api/chat/upload/file",
		Summary:       "上傳聊天文件",
		Description:   "上傳聊天音頻或圖片文件，支援 multipart/form-data",
		Tags:          []string{"chat"},
		DefaultStatus: http.StatusOK,
	}, cc.handleUploadFile)

	// 聊天房間管理端點
	huma.Register(api, huma.Operation{
		OperationID: "get-chat-rooms",
		Method:      http.MethodGet,
		Path:        "/api/chat/rooms",
		Summary:     "獲取聊天房間列表",
		Description: "獲取用戶的所有聊天房間",
		Tags:        []string{"chat"},
	}, cc.handleGetChatRooms)

	// 聊天統計端點
	huma.Register(api, huma.Operation{
		OperationID: "get-chat-statistics",
		Method:      http.MethodGet,
		Path:        "/api/chat/statistics",
		Summary:     "獲取聊天統計數據",
		Description: "獲取司機的聊天統計信息",
		Tags:        []string{"chat"},
	}, cc.handleGetChatStatistics)

	huma.Register(api, huma.Operation{
		OperationID: "get-system-chat-statistics",
		Method:      http.MethodGet,
		Path:        "/api/chat/statistics/system",
		Summary:     "獲取系統聊天統計數據",
		Description: "獲取系統整體聊天統計信息（管理員用）",
		Tags:        []string{"chat"},
	}, cc.handleGetSystemChatStatistics)
}

// handleUploadFile 處理統一文件上傳
func (cc *ChatController) handleUploadFile(ctx context.Context, req *UploadFileRequest) (*FileUploadResponse, error) {
	// 獲取表單數據
	formData := req.RawBody.Data()

	cc.logger.Info().
		Str("order_id", formData.OrderID).
		Str("message_id", formData.MessageID).
		Str("file_type", formData.FileType).
		Interface("audio_duration", formData.AudioDuration).
		Msg("收到文件上傳請求")

	// 驗證文件類型參數
	if formData.FileType != "audio" && formData.FileType != "image" {
		cc.logger.Error().Str("file_type", formData.FileType).Msg("無效的文件類型")
		return &FileUploadResponse{
			Body: websocketModels.FileUploadResponse{
				Success: false,
				Error: &websocketModels.ErrorResponse{
					Code:    "INVALID_FILE_TYPE",
					Message: "文件類型必須是 audio 或 image",
				},
			},
		}, nil
	}

	// 如果是音頻文件，驗證是否提供了時長
	if formData.FileType == "audio" && formData.AudioDuration <= 0 {
		cc.logger.Error().
			Str("file_type", formData.FileType).
			Int("audio_duration", formData.AudioDuration).
			Msg("音頻文件缺少有效的時長參數")
		return &FileUploadResponse{
			Body: websocketModels.FileUploadResponse{
				Success: false,
				Error: &websocketModels.ErrorResponse{
					Code:    "MISSING_AUDIO_DURATION",
					Message: "音頻文件必須提供有效的時長參數(audioDuration)",
				},
			},
		}, nil
	}

	// 檢查文件是否存在
	if !formData.File.IsSet {
		cc.logger.Error().Msg("沒有上傳文件")
		return &FileUploadResponse{
			Body: websocketModels.FileUploadResponse{
				Success: false,
				Error: &websocketModels.ErrorResponse{
					Code:    "FILE_NOT_FOUND",
					Message: "沒有上傳文件",
				},
			},
		}, nil
	}

	// 獲取文件信息
	cc.logger.Info().
		Str("filename", formData.File.Filename).
		Int64("size", formData.File.Size).
		Str("content_type", formData.File.ContentType).
		Msg("開始處理文件上傳")

	// formData.File 已經是一個 multipart.File，直接使用
	defer formData.File.Close()

	// 創建一個虛擬的FileHeader來兼容現有的ChatService接口
	fileHeader := &multipart.FileHeader{
		Filename: formData.File.Filename,
		Size:     formData.File.Size,
	}

	// 根據文件類型調用對應的上傳方法
	var fileURL string
	var err error
	switch formData.FileType {
	case "audio":
		fileURL, err = cc.chatService.UploadAudioFile(ctx, formData.OrderID, formData.MessageID, formData.File, fileHeader)
	case "image":
		fileURL, err = cc.chatService.UploadImageFile(ctx, formData.OrderID, formData.MessageID, formData.File, fileHeader)
	}

	if err != nil {
		cc.logger.Error().
			Err(err).
			Str("file_type", formData.FileType).
			Str("filename", formData.File.Filename).
			Msg("文件上傳失敗")

		return &FileUploadResponse{
			Body: websocketModels.FileUploadResponse{
				Success: false,
				Error: &websocketModels.ErrorResponse{
					Code:    "UPLOAD_FAILED",
					Message: fmt.Sprintf("文件上傳失敗: %s", err.Error()),
				},
			},
		}, nil
	}

	// 將相對路徑轉換為完整URL用於回應
	fullURL := cc.chatService.GetFileURL(fileURL)

	cc.logger.Info().
		Str("order_id", formData.OrderID).
		Str("message_id", formData.MessageID).
		Str("file_type", formData.FileType).
		Str("relative_path", fileURL).
		Str("full_url", fullURL).
		Int("audio_duration", formData.AudioDuration).
		Msg("文件上傳成功")

	// 準備回應數據，使用完整URL
	responseData := &websocketModels.FileUploadData{
		URL:       fullURL,
		MessageID: formData.MessageID,
	}

	// 將 tempID (messageID) 與文件 URL 的關聯存儲到緩存中，供後續聊天消息使用
	cc.cacheFileURLForTempID(formData.OrderID, formData.MessageID, fullURL, formData.FileType)

	// 如果是音頻文件，添加時長信息
	if formData.FileType == "audio" && formData.AudioDuration > 0 {
		responseData.AudioDuration = &formData.AudioDuration
	}

	return &FileUploadResponse{
		Body: websocketModels.FileUploadResponse{
			Success: true,
			Data:    responseData,
		},
	}, nil
}

// handleGetChatRooms 處理獲取聊天房間列表
func (cc *ChatController) handleGetChatRooms(ctx context.Context, req *GetChatRoomsRequest) (*GetChatRoomsResponse, error) {
	// 獲取聊天房間
	chatRooms, err := cc.chatService.GetChatRoomsByDriverID(ctx, req.UserID)
	if err != nil {
		cc.logger.Error().Err(err).Str("user_id", req.UserID).Msg("獲取聊天房間失敗")
		return &GetChatRoomsResponse{
			Body: websocketModels.ChatRoomsResponse{
				Success: false,
				Error: &websocketModels.ErrorResponse{
					Code:    "GET_ROOMS_FAILED",
					Message: "獲取聊天房間失敗",
				},
			},
		}, nil
	}

	// 轉換為回應格式
	var roomInfos []websocketModels.ChatRoomInfo
	for _, room := range chatRooms {
		// 獲取最後一條消息
		messages, _, _, err := cc.chatService.GetChatHistory(ctx, room.OrderID, req.UserID, model.SenderTypeDriver, 1, 0, nil)
		var lastMessage *websocketModels.ChatMessageResponse
		if err == nil && len(messages) > 0 {
			msg := messages[0]
			lastMessage = &websocketModels.ChatMessageResponse{
				ID:        msg.ID.Hex(),
				OrderID:   msg.OrderID,
				Type:      msg.Type,
				Sender:    msg.Sender,
				Content:   msg.Content,
				AudioURL:  msg.AudioURL,
				ImageURL:  msg.ImageURL,
				Timestamp: msg.CreatedAt,
				Status:    msg.Status,
			}
		}

		// 獲取未讀數量
		unreadCount, _ := cc.chatService.GetUnreadCount(ctx, room.OrderID, req.UserID, model.SenderTypeDriver)

		// 轉換OrderInfo類型
		orderInfo := websocketModels.ChatOrderInfo{
			OrderID:             room.OrderInfo.OrderID,
			ShortID:             room.OrderInfo.ShortID,
			OriText:             room.OrderInfo.OriText,
			Status:              string(room.OrderInfo.Status),
			PickupAddress:       room.OrderInfo.PickupAddress,
			DestinationAddress:  room.OrderInfo.DestinationAddress,
			PassengerName:       room.OrderInfo.PassengerName,
			PassengerPhone:      room.OrderInfo.PassengerPhone,
			EstimatedPickupTime: room.OrderInfo.EstimatedPickupTime,
			CreatedAt:           room.OrderInfo.CreatedAt,
		}

		// 轉換LastMessage類型
		var messageSummary *websocketModels.ChatMessageSummary
		if lastMessage != nil {
			messageSummary = &websocketModels.ChatMessageSummary{
				ID:        lastMessage.ID,
				Type:      lastMessage.Type,
				Sender:    lastMessage.Sender,
				Content:   lastMessage.Content,
				Timestamp: lastMessage.Timestamp,
			}
		}

		roomInfo := websocketModels.ChatRoomInfo{
			OrderID:     room.OrderID,
			DriverID:    room.DriverID,
			OrderInfo:   orderInfo,
			UnreadCount: unreadCount,
			LastMessage: messageSummary,
			IsActive:    room.IsActive,
			CreatedAt:   room.CreatedAt,
			UpdatedAt:   room.UpdatedAt,
		}
		roomInfos = append(roomInfos, roomInfo)
	}

	return &GetChatRoomsResponse{
		Body: websocketModels.ChatRoomsResponse{
			Success: true,
			Data:    roomInfos,
		},
	}, nil
}

// handleGetChatStatistics 處理獲取聊天統計數據
func (cc *ChatController) handleGetChatStatistics(ctx context.Context, req *GetChatStatisticsRequest) (*GetChatStatisticsResponse, error) {
	cc.logger.Info().
		Str("user_id", req.UserID).
		Msg("獲取聊天統計數據請求")

	// 獲取聊天統計
	statistics, err := cc.chatService.GetChatStatistics(ctx, req.UserID)
	if err != nil {
		cc.logger.Error().
			Err(err).
			Str("user_id", req.UserID).
			Msg("獲取聊天統計數據失敗")

		return &GetChatStatisticsResponse{
			Body: struct {
				Success bool                           `json:"success"`
				Data    *service.ChatStatistics        `json:"data,omitempty"`
				Error   *websocketModels.ErrorResponse `json:"error,omitempty"`
			}{
				Success: false,
				Error: &websocketModels.ErrorResponse{
					Code:    "GET_STATISTICS_FAILED",
					Message: "獲取聊天統計數據失敗",
				},
			},
		}, nil
	}

	cc.logger.Info().
		Str("user_id", req.UserID).
		Int("total_rooms", statistics.TotalChatRooms).
		Int("total_messages", statistics.TotalMessages).
		Msg("聊天統計數據獲取成功")

	return &GetChatStatisticsResponse{
		Body: struct {
			Success bool                           `json:"success"`
			Data    *service.ChatStatistics        `json:"data,omitempty"`
			Error   *websocketModels.ErrorResponse `json:"error,omitempty"`
		}{
			Success: true,
			Data:    statistics,
		},
	}, nil
}

// handleGetSystemChatStatistics 處理獲取系統聊天統計數據
func (cc *ChatController) handleGetSystemChatStatistics(ctx context.Context, req *GetSystemChatStatisticsRequest) (*GetSystemChatStatisticsResponse, error) {
	cc.logger.Info().Msg("獲取系統聊天統計數據請求")

	// 獲取系統聊天統計
	statistics, err := cc.chatService.GetSystemChatStatistics(ctx)
	if err != nil {
		cc.logger.Error().
			Err(err).
			Msg("獲取系統聊天統計數據失敗")

		return &GetSystemChatStatisticsResponse{
			Body: struct {
				Success bool                           `json:"success"`
				Data    *service.SystemChatStatistics  `json:"data,omitempty"`
				Error   *websocketModels.ErrorResponse `json:"error,omitempty"`
			}{
				Success: false,
				Error: &websocketModels.ErrorResponse{
					Code:    "GET_SYSTEM_STATISTICS_FAILED",
					Message: "獲取系統聊天統計數據失敗",
				},
			},
		}, nil
	}

	cc.logger.Info().
		Int("total_rooms", statistics.TotalChatRooms).
		Int("total_messages", statistics.TotalMessages).
		Int("today_messages", statistics.TodayMessages).
		Msg("系統聊天統計數據獲取成功")

	return &GetSystemChatStatisticsResponse{
		Body: struct {
			Success bool                           `json:"success"`
			Data    *service.SystemChatStatistics  `json:"data,omitempty"`
			Error   *websocketModels.ErrorResponse `json:"error,omitempty"`
		}{
			Success: true,
			Data:    statistics,
		},
	}, nil
}

// WebSocket相關的聊天處理方法

// HandleChatSendMessage 處理發送聊天消息
func (cc *ChatController) HandleChatSendMessage(ctx context.Context, senderID string, senderType model.SenderType, data interface{}) (*websocketModels.ChatReceiveMessageEvent, error) {
	// 更安全的類型轉換
	var request websocketModels.ChatSendMessageRequest
	if err := cc.parseWebSocketData(data, &request); err != nil {
		cc.logger.Error().
			Err(err).
			Str("sender_id", senderID).
			Str("sender_type", string(senderType)).
			Msg("解析聊天消息請求失敗")
		return nil, fmt.Errorf("解析聊天消息請求失敗: %w", err)
	}

	// 驗證請求數據
	if request.OrderID == "" {
		return nil, fmt.Errorf("訂單ID不能為空")
	}
	if request.Message.TempID == "" {
		return nil, fmt.Errorf("臨時消息ID不能為空")
	}

	// 處理圖片和音頻 URL
	var imageURL, audioURL *string

	// 如果是圖片或音頻消息且沒有提供 URL，嘗試根據 tempID 查找已上傳的文件
	if request.Message.Type == model.MessageTypeImage {
		if request.Message.ImageData != nil && *request.Message.ImageData != "" {
			imageURL = request.Message.ImageData
		} else {
			// 根據 tempID 和 orderID 查找已上傳的圖片
			foundImageURL := cc.findUploadedFileByTempID(ctx, request.OrderID, request.Message.TempID, "image")
			if foundImageURL != "" {
				imageURL = &foundImageURL
				cc.logger.Info().
					Str("order_id", request.OrderID).
					Str("temp_id", request.Message.TempID).
					Str("found_image_url", foundImageURL).
					Msg("根據 tempID 找到圖片 URL")
			}
		}
	} else if request.Message.Type == model.MessageTypeAudio {
		if request.Message.AudioData != nil && *request.Message.AudioData != "" {
			audioURL = request.Message.AudioData
		} else {
			// 根據 tempID 和 orderID 查找已上傳的音頻
			foundAudioURL := cc.findUploadedFileByTempID(ctx, request.OrderID, request.Message.TempID, "audio")
			if foundAudioURL != "" {
				audioURL = &foundAudioURL
				cc.logger.Info().
					Str("order_id", request.OrderID).
					Str("temp_id", request.Message.TempID).
					Str("found_audio_url", foundAudioURL).
					Msg("根據 tempID 找到音頻 URL")
			}
		}
	}

	// 發送消息
	message, err := cc.chatService.SendMessage(
		ctx,
		request.OrderID,
		senderID,
		senderType,
		request.Message.Type,
		request.Message.Content,
		audioURL,
		imageURL,
		nil, // audioDuration 前端提供
		&request.Message.TempID,
	)
	if err != nil {
		return nil, fmt.Errorf("發送消息失敗: %w", err)
	}

	// 構建回應
	response := &websocketModels.ChatReceiveMessageEvent{
		OrderID: message.OrderID,
		Message: websocketModels.ChatMessageResponse{
			ID:        message.ID.Hex(),
			OrderID:   message.OrderID,
			Type:      message.Type,
			Sender:    message.Sender,
			Content:   message.Content,
			AudioURL:  message.AudioURL,
			ImageURL:  message.ImageURL,
			Timestamp: message.CreatedAt,
			Status:    message.Status,
		},
	}

	return response, nil
}

// HandleChatGetHistory 處理獲取聊天歷史
func (cc *ChatController) HandleChatGetHistory(ctx context.Context, userID string, userType model.SenderType, data interface{}) (*websocketModels.ChatHistoryResponse, error) {
	// 解析請求數據
	var request websocketModels.ChatGetHistoryRequest
	if err := cc.parseWebSocketData(data, &request); err != nil {
		cc.logger.Error().
			Err(err).
			Str("user_id", userID).
			Str("user_type", string(userType)).
			Msg("解析聊天歷史請求失敗")
		return nil, fmt.Errorf("解析聊天歷史請求失敗: %w", err)
	}

	// 驗證請求數據
	if request.OrderID == "" {
		return nil, fmt.Errorf("訂單ID不能為空")
	}

	// 設置默認值
	limit := 50
	offset := 0
	if request.HistoryRequest.Limit != nil {
		limit = *request.HistoryRequest.Limit
	}
	if request.HistoryRequest.Offset != nil {
		offset = *request.HistoryRequest.Offset
	}

	// 獲取聊天歷史
	messages, total, hasMore, err := cc.chatService.GetChatHistory(
		ctx,
		request.OrderID,
		userID,
		userType,
		limit,
		offset,
		request.HistoryRequest.BeforeMessageID,
	)
	if err != nil {
		return nil, fmt.Errorf("獲取聊天歷史失敗: %w", err)
	}

	// 轉換為回應格式
	var responseMessages []websocketModels.ChatMessageResponse
	for _, msg := range messages {
		responseMessages = append(responseMessages, websocketModels.ChatMessageResponse{
			ID:        msg.ID.Hex(),
			OrderID:   msg.OrderID,
			Type:      msg.Type,
			Sender:    msg.Sender,
			Content:   msg.Content,
			AudioURL:  msg.AudioURL,
			ImageURL:  msg.ImageURL,
			Timestamp: msg.CreatedAt,
			Status:    msg.Status,
		})
	}

	response := &websocketModels.ChatHistoryResponse{
		OrderID:  request.OrderID,
		Messages: responseMessages,
		Total:    total,
		HasMore:  hasMore,
	}

	return response, nil
}

// HandleChatMarkAsRead 處理標記為已讀
func (cc *ChatController) HandleChatMarkAsRead(ctx context.Context, userID string, userType model.SenderType, data interface{}) error {
	// 解析請求數據
	var request websocketModels.ChatMarkAsReadRequest
	if err := cc.parseWebSocketData(data, &request); err != nil {
		cc.logger.Error().
			Err(err).
			Str("user_id", userID).
			Str("user_type", string(userType)).
			Msg("解析標記已讀請求失敗")
		return fmt.Errorf("解析標記已讀請求失敗: %w", err)
	}

	// 驗證請求數據
	if request.OrderID == "" {
		return fmt.Errorf("訂單ID不能為空")
	}

	// 標記為已讀
	return cc.chatService.MarkAsRead(ctx, request.OrderID, userID, userType)
}

// HandleChatRecallMessage 處理收回訊息
func (cc *ChatController) HandleChatRecallMessage(ctx context.Context, userID string, userType model.SenderType, data interface{}) (*websocketModels.ChatMessageRecalledEvent, error) {
	// 解析請求數據
	var request websocketModels.ChatRecallMessageRequest
	if err := cc.parseWebSocketData(data, &request); err != nil {
		cc.logger.Error().
			Err(err).
			Str("user_id", userID).
			Str("user_type", string(userType)).
			Msg("解析收回訊息請求失敗")
		return nil, fmt.Errorf("解析收回訊息請求失敗: %w", err)
	}

	// 驗證請求數據
	if request.OrderID == "" {
		return nil, fmt.Errorf("訂單ID不能為空")
	}
	if request.MessageID == "" {
		return nil, fmt.Errorf("訊息ID不能為空")
	}

	// 收回訊息
	err := cc.chatService.RecallMessage(ctx, request.MessageID, userID, userType)
	if err != nil {
		return nil, fmt.Errorf("收回訊息失敗: %w", err)
	}

	// 構建回應事件
	response := &websocketModels.ChatMessageRecalledEvent{
		OrderID:    request.OrderID,
		MessageID:  request.MessageID,
		RecalledBy: userType,
		RecalledAt: time.Now(),
	}

	cc.logger.Info().
		Str("user_id", userID).
		Str("user_type", string(userType)).
		Str("order_id", request.OrderID).
		Str("message_id", request.MessageID).
		Msg("訊息收回成功")

	return response, nil
}

// 輔助函數

// parseWebSocketData 安全地解析 WebSocket 數據
// GetUserRecentChats 查詢用戶最近聊天記錄
func (cc *ChatController) GetUserRecentChats(ctx context.Context, req *GetUserRecentChatsRequest) (*GetUserRecentChatsResponse, error) {
	// 設置限制範圍
	limit := req.Limit
	if limit <= 0 {
		limit = 10 // 預設值
	}
	if limit > 50 { // 防止查詢過多數據
		limit = 50
	}

	// 調用服務獲取用戶最近聊天記錄
	chatRooms, err := cc.chatService.GetUserRecentChats(ctx, req.UserID, limit)
	if err != nil {
		cc.logger.Error().
			Err(err).
			Str("user_id", req.UserID).
			Int("limit", limit).
			Msg("查詢用戶最近聊天記錄失敗")
		return nil, fmt.Errorf("查詢用戶最近聊天記錄失敗: %w", err)
	}

	cc.logger.Info().
		Str("user_id", req.UserID).
		Int("limit", limit).
		Int("count", len(chatRooms)).
		Msg("成功查詢用戶最近聊天記錄")

	return &GetUserRecentChatsResponse{
		Body: struct {
			ChatRooms []websocketModels.ChatRoomInfo `json:"chatRooms" doc:"聊天房間列表"`
			Count     int                            `json:"count" doc:"總數量"`
		}{
			ChatRooms: chatRooms,
			Count:     len(chatRooms),
		},
	}, nil
}

func (cc *ChatController) parseWebSocketData(data interface{}, target interface{}) error {
	// 先嘗試直接類型轉換（如果是 map[string]interface{}）
	if dataMap, ok := data.(map[string]interface{}); ok {
		// 序列化為 JSON 再反序列化到目標結構
		jsonBytes, err := json.Marshal(dataMap)
		if err != nil {
			return fmt.Errorf("序列化數據失敗: %w", err)
		}
		if err := json.Unmarshal(jsonBytes, target); err != nil {
			return fmt.Errorf("反序列化到目標結構失敗: %w", err)
		}
		return nil
	}

	// 如果是其他類型，嘗試直接序列化
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("序列化數據失敗: %w", err)
	}

	if err := json.Unmarshal(jsonBytes, target); err != nil {
		return fmt.Errorf("反序列化到目標結構失敗: %w", err)
	}

	return nil
}

func marshalInterface(data interface{}) ([]byte, error) {
	return json.Marshal(data)
}

func unmarshalBytes(data []byte, v interface{}) error {
	return json.Unmarshal(data, v)
}

// cacheFileURLForTempID 將 tempID 與文件 URL 的關聯存儲到緩存中
func (cc *ChatController) cacheFileURLForTempID(orderID, tempID, fileURL, fileType string) {
	key := fmt.Sprintf("%s:%s:%s", orderID, tempID, fileType)

	cc.cacheMutex.Lock()
	defer cc.cacheMutex.Unlock()

	cc.fileURLCache[key] = fileURL

	cc.logger.Info().
		Str("order_id", orderID).
		Str("temp_id", tempID).
		Str("file_type", fileType).
		Str("file_url", fileURL).
		Str("cache_key", key).
		Msg("已緩存文件 URL")
}

// findUploadedFileByTempID 根據 tempID 查找已上傳的文件 URL
func (cc *ChatController) findUploadedFileByTempID(ctx context.Context, orderID, tempID, fileType string) string {
	key := fmt.Sprintf("%s:%s:%s", orderID, tempID, fileType)

	cc.cacheMutex.RLock()
	defer cc.cacheMutex.RUnlock()

	if fileURL, exists := cc.fileURLCache[key]; exists {
		cc.logger.Info().
			Str("order_id", orderID).
			Str("temp_id", tempID).
			Str("file_type", fileType).
			Str("cache_key", key).
			Str("found_url", fileURL).
			Msg("從緩存中找到文件 URL")
		return fileURL
	}

	cc.logger.Warn().
		Str("order_id", orderID).
		Str("temp_id", tempID).
		Str("file_type", fileType).
		Str("cache_key", key).
		Msg("未在緩存中找到文件 URL")

	return ""
}
