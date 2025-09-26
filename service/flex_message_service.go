package service

import (
	"fmt"
	"net/url"
	"right-backend/model"
	"time"

	"github.com/line/line-bot-sdk-go/v8/linebot/messaging_api"
	"github.com/rs/zerolog"
)

// FlexMessageService 處理 Flex Message 建立
type FlexMessageService struct {
	logger zerolog.Logger
}

// NewFlexMessageService 建立新的 Flex Message 服務
func NewFlexMessageService(logger zerolog.Logger) *FlexMessageService {
	return &FlexMessageService{
		logger: logger.With().Str("service", "flex_message").Logger(),
	}
}

// GetFlexMessage 根據訂單狀態取得對應的 Flex Message
func (s *FlexMessageService) GetFlexMessage(order *model.Order) messaging_api.MessageInterface {
	switch order.Status {
	case model.OrderStatusWaiting:
		return s.createWaitingMessage(order)
	case model.OrderStatusEnroute:
		return s.createEnrouteMessage(order)
	case model.OrderStatusDriverArrived:
		return s.createDriverArrivedMessage(order)
	case model.OrderStatusExecuting:
		return s.createExecutingMessage(order)
	case model.OrderStatusCompleted:
		return s.createCompletedMessage(order)
	case model.OrderStatusFailed:
		return s.createFailedMessage(order)
	case model.OrderStatusCancelled:
		return s.createCancelledMessage(order)
	default:
		return s.createWaitingMessage(order)
	}
}

// GetCreatingFlexMessage 取得建立中的 Flex Message
func (s *FlexMessageService) GetCreatingFlexMessage() messaging_api.MessageInterface {
	return s.createCreatingMessage()
}

// createWaitingMessage 建立等待中的 Flex Message
func (s *FlexMessageService) createWaitingMessage(order *model.Order) *messaging_api.FlexMessage {
	remarks := order.Customer.Remarks
	if remarks == "" {
		remarks = "無"
	}

	return &messaging_api.FlexMessage{
		AltText: "⏳ 等待司機接單",
		Contents: &messaging_api.FlexBubble{
			Size: "kilo",
			Header: &messaging_api.FlexBox{
				Layout:          "horizontal",
				BackgroundColor: "#6366F1",
				PaddingAll:      "16px",
				Spacing:         "lg",
				AlignItems:      "center",
				Contents: []messaging_api.FlexComponentInterface{
					&messaging_api.FlexText{
						Text:  "⏳",
						Size:  "lg",
						Flex:  0,
						Color: "#FFFFFF",
					},
					&messaging_api.FlexText{
						Text:   "等待接單",
						Weight: "bold",
						Size:   "lg",
						Color:  "#FFFFFF",
						Flex:   4,
					},
					&messaging_api.FlexText{
						Text:  order.ShortID,
						Size:  "md",
						Color: "#E0E7FF",
						Align: "end",
						Flex:  2,
					},
				},
			},
			Body:   s.createOrderInfoBody(order.ShortID, "", order.OriText, remarks, "", "", order.Customer.PickupAddress),
			Footer: s.createCompletedOrderFooter(order.OriText, order.Customer.PickupAddress),
		},
	}
}

// createEnrouteMessage 建立司機前往中的 Flex Message
func (s *FlexMessageService) createEnrouteMessage(order *model.Order) *messaging_api.FlexMessage {
	remarks := order.Customer.Remarks
	if remarks == "" {
		remarks = "無"
	}

	driverInfo := ""
	estimatedArrival := ""
	if order.Driver.Name != "" {
		driverInfo = fmt.Sprintf("%s (%s)", order.Driver.Name, order.Driver.CarNo)
		displayMins := order.Driver.EstPickupMins
		if order.Driver.AdjustMins != nil {
			displayMins += *order.Driver.AdjustMins
		}
		arrivalTime := time.Now().Add(time.Minute * time.Duration(displayMins))
		estimatedArrival = fmt.Sprintf("%d 分鐘（%s）", displayMins, arrivalTime.Format("15:04"))
	}

	return &messaging_api.FlexMessage{
		AltText: "🚗 司機前往上車點",
		Contents: &messaging_api.FlexBubble{
			Size: "kilo",
			Header: &messaging_api.FlexBox{
				Layout:          "horizontal",
				BackgroundColor: "#6366F1",
				PaddingAll:      "16px",
				Spacing:         "lg",
				AlignItems:      "center",
				Contents: []messaging_api.FlexComponentInterface{
					&messaging_api.FlexText{
						Text:  "🚗",
						Size:  "lg",
						Flex:  0,
						Color: "#FFFFFF",
					},
					&messaging_api.FlexText{
						Text:   "前往客上",
						Weight: "bold",
						Size:   "lg",
						Color:  "#FFFFFF",
						Flex:   4,
					},
					&messaging_api.FlexText{
						Text:  order.ShortID,
						Size:  "md",
						Color: "#E0E7FF",
						Align: "end",
						Flex:  2,
					},
				},
			},
			Body:   s.createOrderInfoBody(order.ShortID, "", order.OriText, remarks, driverInfo, estimatedArrival, order.Customer.PickupAddress),
			Footer: s.createActionButtonsFooter(order.OriText, order.Customer.PickupAddress, order.ID.Hex(), true),
		},
	}
}

// createFailedMessage 建立派單失敗的 Flex Message
func (s *FlexMessageService) createFailedMessage(order *model.Order) *messaging_api.FlexMessage {
	remarks := order.Customer.Remarks
	if remarks == "" {
		remarks = "無"
	}

	return &messaging_api.FlexMessage{
		AltText: "❌ 派單失敗",
		Contents: &messaging_api.FlexBubble{
			Size: "kilo",
			Header: &messaging_api.FlexBox{
				Layout:          "horizontal",
				BackgroundColor: "#6366F1",
				PaddingAll:      "16px",
				Spacing:         "lg",
				AlignItems:      "center",
				Contents: []messaging_api.FlexComponentInterface{
					&messaging_api.FlexText{
						Text:  "❌",
						Size:  "lg",
						Flex:  0,
						Color: "#FFFFFF",
					},
					&messaging_api.FlexText{
						Text:   "派單失敗",
						Weight: "bold",
						Size:   "lg",
						Color:  "#FFFFFF",
						Flex:   4,
					},
					&messaging_api.FlexText{
						Text:  order.ShortID,
						Size:  "md",
						Color: "#E0E7FF",
						Align: "end",
						Flex:  2,
					},
				},
			},
			Body:   s.createFailedBody(order.ShortID, "", order.OriText, remarks),
			Footer: s.createActionButtonsFooter(order.OriText, order.Customer.PickupAddress, order.ID.Hex(), false),
		},
	}
}

// createDriverArrivedMessage 建立司機到達的 Flex Message
func (s *FlexMessageService) createDriverArrivedMessage(order *model.Order) *messaging_api.FlexMessage {
	remarks := order.Customer.Remarks
	if remarks == "" {
		remarks = "無"
	}

	driverInfo := ""
	estimatedArrival := ""
	if order.Driver.Name != "" {
		driverInfo = fmt.Sprintf("%s (%s)", order.Driver.Name, order.Driver.CarNo)
		displayMins := order.Driver.EstPickupMins
		if order.Driver.AdjustMins != nil {
			displayMins += *order.Driver.AdjustMins
		}
		arrivalTime := time.Now().Add(time.Minute * time.Duration(displayMins))
		estimatedArrival = fmt.Sprintf("%d 分鐘 (%s)", displayMins, arrivalTime.Format("15:04"))
	}

	return &messaging_api.FlexMessage{
		AltText: "📍 司機抵達",
		Contents: &messaging_api.FlexBubble{
			Size: "kilo",
			Header: &messaging_api.FlexBox{
				Layout:          "horizontal",
				BackgroundColor: "#6366F1",
				PaddingAll:      "16px",
				Spacing:         "lg",
				AlignItems:      "center",
				Contents: []messaging_api.FlexComponentInterface{
					&messaging_api.FlexText{
						Text:  "📍",
						Size:  "lg",
						Flex:  0,
						Color: "#FFFFFF",
					},
					&messaging_api.FlexText{
						Text:   "司機抵達",
						Weight: "bold",
						Size:   "lg",
						Color:  "#FFFFFF",
						Flex:   4,
					},
					&messaging_api.FlexText{
						Text:  order.ShortID,
						Size:  "md",
						Color: "#E0E7FF",
						Align: "end",
						Flex:  2,
					},
				},
			},
			Body:   s.createDriverArrivedInfoBody(order.ShortID, "", order.OriText, remarks, driverInfo, estimatedArrival, order.PickupCertificateURL, order.Customer.PickupAddress),
			Footer: s.createActionButtonsFooter(order.OriText, order.Customer.PickupAddress, order.ID.Hex(), true),
		},
	}

}

// createExecutingMessage 建立乘客已上車的 Flex Message
func (s *FlexMessageService) createExecutingMessage(order *model.Order) *messaging_api.FlexMessage {
	remarks := order.Customer.Remarks
	if remarks == "" {
		remarks = "無"
	}

	driverInfo := ""
	estimatedArrival := ""
	if order.Driver.Name != "" {
		driverInfo = fmt.Sprintf("%s (%s)", order.Driver.Name, order.Driver.CarNo)
		displayMins := order.Driver.EstPickupMins
		if order.Driver.AdjustMins != nil {
			displayMins += *order.Driver.AdjustMins
		}
		arrivalTime := time.Now().Add(time.Minute * time.Duration(displayMins))
		estimatedArrival = fmt.Sprintf("%d 分鐘（%s）", displayMins, arrivalTime.Format("15:04"))
	}

	return &messaging_api.FlexMessage{
		AltText: "🟢 客上",
		Contents: &messaging_api.FlexBubble{
			Size: "kilo",
			Header: &messaging_api.FlexBox{
				Layout:          "horizontal",
				BackgroundColor: "#6366F1",
				PaddingAll:      "16px",
				Spacing:         "lg",
				AlignItems:      "center",
				Contents: []messaging_api.FlexComponentInterface{
					&messaging_api.FlexText{
						Text:  "🟢",
						Size:  "lg",
						Flex:  0,
						Color: "#FFFFFF",
					},
					&messaging_api.FlexText{
						Text:   "客上",
						Weight: "bold",
						Size:   "lg",
						Color:  "#FFFFFF",
						Flex:   4,
					},
					&messaging_api.FlexText{
						Text:  order.ShortID,
						Size:  "md",
						Color: "#E0E7FF",
						Align: "end",
						Flex:  2,
					},
				},
			},
			Body:   s.createOrderInfoBody(order.ShortID, "", order.OriText, remarks, driverInfo, estimatedArrival, order.Customer.PickupAddress),
			Footer: s.createActionButtonsFooter(order.OriText, order.Customer.PickupAddress, order.ID.Hex(), true),
		},
	}
}

// createCompletedMessage 建立訂單完成的 Flex Message
func (s *FlexMessageService) createCompletedMessage(order *model.Order) *messaging_api.FlexMessage {
	remarks := order.Customer.Remarks
	if remarks == "" {
		remarks = "無"
	}

	driverInfo := ""
	if order.Driver.Name != "" {
		driverInfo = fmt.Sprintf("%s (%s)", order.Driver.Name, order.Driver.CarNo)
	}

	return &messaging_api.FlexMessage{
		AltText: "🟢 訂單完成",
		Contents: &messaging_api.FlexBubble{
			Size: "kilo",
			Header: &messaging_api.FlexBox{
				Layout:          "horizontal",
				BackgroundColor: "#6366F1",
				PaddingAll:      "16px",
				Spacing:         "lg",
				AlignItems:      "center",
				Contents: []messaging_api.FlexComponentInterface{
					&messaging_api.FlexText{
						Text:  "🟢",
						Size:  "lg",
						Flex:  0,
						Color: "#FFFFFF",
					},
					&messaging_api.FlexText{
						Text:   "訂單完成",
						Weight: "bold",
						Size:   "lg",
						Color:  "#FFFFFF",
						Flex:   4,
					},
					&messaging_api.FlexText{
						Text:  order.ShortID,
						Size:  "md",
						Color: "#E0E7FF",
						Align: "end",
						Flex:  2,
					},
				},
			},
			Body:   s.createOrderInfoBody(order.ShortID, "", order.OriText, remarks, driverInfo, "", order.Customer.PickupAddress),
			Footer: s.createCompletedOrderFooter(order.OriText, order.Customer.PickupAddress),
		},
	}
}

// createCancelledMessage 建立訂單取消的 Flex Message
func (s *FlexMessageService) createCancelledMessage(order *model.Order) *messaging_api.FlexMessage {
	return &messaging_api.FlexMessage{
		AltText: "🟤 訂單取消",
		Contents: &messaging_api.FlexBubble{
			Size: "kilo",
			Header: &messaging_api.FlexBox{
				Layout:          "horizontal",
				BackgroundColor: "#6366F1",
				PaddingAll:      "16px",
				Spacing:         "lg",
				AlignItems:      "center",
				Contents: []messaging_api.FlexComponentInterface{
					&messaging_api.FlexText{
						Text:  "🟤",
						Size:  "lg",
						Flex:  0,
						Color: "#FFFFFF",
					},
					&messaging_api.FlexText{
						Text:   "訂單取消",
						Weight: "bold",
						Size:   "lg",
						Color:  "#FFFFFF",
						Flex:   4,
					},
					&messaging_api.FlexText{
						Text:  order.ShortID,
						Size:  "md",
						Color: "#E0E7FF",
						Align: "end",
						Flex:  2,
					},
				},
			},
			Body:   s.createCancelledBody(order.ShortID, "", order.OriText),
			Footer: s.createCompletedOrderFooter(order.OriText, order.Customer.PickupAddress),
		},
	}
}

// createCreatingMessage 建立訂單創建中的 Flex Message
func (s *FlexMessageService) createCreatingMessage() *messaging_api.FlexMessage {
	return &messaging_api.FlexMessage{
		AltText: "⏳ 建立訂單",
		Contents: &messaging_api.FlexBubble{
			Size: "kilo",
			Header: &messaging_api.FlexBox{
				Layout:          "horizontal",
				BackgroundColor: "#6366F1",
				PaddingAll:      "16px",
				Spacing:         "lg",
				AlignItems:      "center",
				Contents: []messaging_api.FlexComponentInterface{
					&messaging_api.FlexText{
						Text:  "⏳",
						Size:  "lg",
						Flex:  0,
						Color: "#FFFFFF",
					},
					&messaging_api.FlexText{
						Text:   "建立訂單",
						Weight: "bold",
						Size:   "lg",
						Color:  "#FFFFFF",
						Flex:   4,
					},
				},
			},
			Body: &messaging_api.FlexBox{
				Layout: "vertical",
				Contents: []messaging_api.FlexComponentInterface{
					&messaging_api.FlexBox{
						Layout: "vertical",
						Margin: "lg",
						Contents: []messaging_api.FlexComponentInterface{
							&messaging_api.FlexText{
								Text:  "正在為您處理訂單資訊...",
								Color: "#94a1b2",
								Size:  "sm",
								Wrap:  true,
								Align: "center",
							},
						},
					},
				},
			},
		},
	}
}

// 輔助方法：創建基本訂單資訊 Body
func (s *FlexMessageService) createOrderInfoBody(shortID, customerGroup, oriText, remarks, driverInfo, estimatedArrival, pickupAddress string) *messaging_api.FlexBox {
	contents := []messaging_api.FlexComponentInterface{
		s.createOrderNumberRow(oriText),
	}

	// 如果有司機資訊，添加司機資訊行
	if driverInfo != "" {
		contents = append(contents,
			&messaging_api.FlexBox{
				Layout: "vertical",
				Margin: "lg",
				Contents: []messaging_api.FlexComponentInterface{
					s.createInfoRow("司機", driverInfo, false),
				},
			})
	}

	// 如果有預計到達時間，添加到達時間行
	if estimatedArrival != "" {
		contents = append(contents, s.createInfoRow("預計到達", estimatedArrival, true))
	}

	// 添加地址行
	contents = append(contents, s.createAddressRow(pickupAddress))

	return &messaging_api.FlexBox{
		Layout:   "vertical",
		Spacing:  "md",
		Contents: contents,
	}
}

// 創建司機到達專用的 Body
func (s *FlexMessageService) createDriverArrivedBody(shortID, customerGroup, oriText, remarks, driverInfo string) *messaging_api.FlexBox {
	contents := []messaging_api.FlexComponentInterface{
		s.createInfoRow("司機", driverInfo, true),
		s.createOrderNumberRow(oriText),
	}

	return &messaging_api.FlexBox{
		Layout: "vertical",
		Contents: []messaging_api.FlexComponentInterface{
			&messaging_api.FlexBox{
				Layout:   "vertical",
				Margin:   "lg",
				Contents: contents,
			},
		},
	}
}

// 創建司機到達資訊專用的 Body（包含預計到達時間和證明照片）
func (s *FlexMessageService) createDriverArrivedInfoBody(shortID, customerGroup, oriText, remarks, driverInfo, estimatedArrival, pickupCertificateURL, pickupAddress string) *messaging_api.FlexBox {
	contents := []messaging_api.FlexComponentInterface{
		s.createOrderNumberRow(oriText),
	}

	// 如果有司機資訊，添加司機資訊行
	if driverInfo != "" {
		contents = append(contents,
			&messaging_api.FlexBox{
				Layout: "vertical",
				Margin: "lg",
				Contents: []messaging_api.FlexComponentInterface{
					s.createInfoRow("司機", driverInfo, false),
				},
			})
	}

	// 如果有預計到達時間，添加到達時間行
	if estimatedArrival != "" {
		contents = append(contents, s.createInfoRow("預計到達", estimatedArrival, true))
	}

	// 添加地址行
	contents = append(contents, s.createAddressRow(pickupAddress))

	// 如果有接送證明照片，添加照片區塊
	if pickupCertificateURL != "" {
		photoSection := &messaging_api.FlexBox{
			Layout: "vertical",
			Margin: "lg",
			Contents: []messaging_api.FlexComponentInterface{
				// 分隔線
				&messaging_api.FlexSeparator{
					Margin: "md",
				},
				// 照片標題
				&messaging_api.FlexText{
					Text:   "司機抵達證明",
					Weight: "bold",
					Size:   "sm",
					Color:  "#6B7280",
					Margin: "md",
				},
				// 照片
				&messaging_api.FlexImage{
					Url:         pickupCertificateURL,
					Size:        "full",
					AspectRatio: "20:13",
					AspectMode:  "cover",
					Margin:      "xs",
					Action: &messaging_api.UriAction{
						Uri: pickupCertificateURL,
					},
				},
			},
		}
		contents = append(contents, photoSection)
	}

	return &messaging_api.FlexBox{
		Layout:   "vertical",
		Spacing:  "md",
		Contents: contents,
	}
}

// 創建失敗狀態專用的 Body（包含提示）
func (s *FlexMessageService) createFailedBody(shortID, customerGroup, oriText, remarks string) *messaging_api.FlexBox {
	contents := []messaging_api.FlexComponentInterface{
		s.createOrderNumberRow(oriText),
		&messaging_api.FlexBox{
			Layout: "vertical",
			Margin: "lg",
			Contents: []messaging_api.FlexComponentInterface{
				&messaging_api.FlexText{
					Text:  "💡 可以嘗試重新派單",
					Color: "#94a1b2",
					Size:  "xs",
					Wrap:  true,
				},
			},
		},
	}

	return &messaging_api.FlexBox{
		Layout: "vertical",
		Contents: []messaging_api.FlexComponentInterface{
			&messaging_api.FlexBox{
				Layout:   "vertical",
				Margin:   "lg",
				Contents: contents,
			},
		},
	}
}

// 創建取消狀態專用的 Body
func (s *FlexMessageService) createCancelledBody(shortID, customerGroup, oriText string) *messaging_api.FlexBox {
	contents := []messaging_api.FlexComponentInterface{
		s.createOrderNumberRow(oriText),
		&messaging_api.FlexBox{
			Layout: "vertical",
			Margin: "lg",
			Contents: []messaging_api.FlexComponentInterface{
				&messaging_api.FlexText{
					Text:  "💡 如需重新預約，請重新輸入行程",
					Color: "#94a1b2",
					Size:  "xs",
					Wrap:  true,
				},
			},
		},
	}

	return &messaging_api.FlexBox{
		Layout: "vertical",
		Contents: []messaging_api.FlexComponentInterface{
			&messaging_api.FlexBox{
				Layout:   "vertical",
				Margin:   "lg",
				Contents: contents,
			},
		},
	}
}

// 創建單號行（可點擊複製）
func (s *FlexMessageService) createOrderNumberRow(oriText string) *messaging_api.FlexBox {
	return &messaging_api.FlexBox{
		Layout:  "horizontal",
		Spacing: "md",
		Contents: []messaging_api.FlexComponentInterface{
			&messaging_api.FlexText{
				Text:  "單號",
				Color: "#6B7280",
				Size:  "md",
				Flex:  2,
			},
			&messaging_api.FlexText{
				Text:  oriText,
				Color: "#3B82F6",
				Size:  "md",
				Flex:  5,
				Wrap:  true,
				Action: &messaging_api.ClipboardAction{
					Label:         "複製單號",
					ClipboardText: oriText,
				},
			},
		},
	}
}

// 創建資訊行
func (s *FlexMessageService) createInfoRow(label, value string, isBold bool) *messaging_api.FlexBox {
	var weight messaging_api.FlexTextWEIGHT
	labelColor := "#6B7280"
	valueColor := "#111827"

	if isBold {
		weight = messaging_api.FlexTextWEIGHT_BOLD
	} else {
		weight = messaging_api.FlexTextWEIGHT_REGULAR
	}

	return &messaging_api.FlexBox{
		Layout:  "horizontal",
		Spacing: "md",
		Contents: []messaging_api.FlexComponentInterface{
			&messaging_api.FlexText{
				Text:  label,
				Color: labelColor,
				Size:  "md",
				Flex:  2,
			},
			&messaging_api.FlexText{
				Text:   value,
				Wrap:   true,
				Color:  valueColor,
				Size:   "md",
				Flex:   5,
				Weight: weight,
			},
		},
	}
}

// 創建複製單號按鈕
func (s *FlexMessageService) createCopyAddressButton(address string) *messaging_api.FlexButton {
	return &messaging_api.FlexButton{
		Style:  "primary",
		Height: "md",
		Action: &messaging_api.ClipboardAction{
			Label:         "複製單號",
			ClipboardText: address,
		},
		Color: "#6366F1",
	}
}

// 創建取消訂單按鈕
func (s *FlexMessageService) createCancelOrderButton(orderID string) *messaging_api.FlexButton {
	return &messaging_api.FlexButton{
		Style:  "secondary",
		Height: "md",
		Action: &messaging_api.MessageAction{
			Label: "取消訂單",
			Text:  fmt.Sprintf("取消 %s", orderID),
		},
		Color: "#EFEFEF",
	}
}

// 創建重派訂單按鈕
func (s *FlexMessageService) createRedispatchButton(orderID string) *messaging_api.FlexButton {
	return &messaging_api.FlexButton{
		Style:  "primary",
		Height: "md",
		Action: &messaging_api.MessageAction{
			Label: "重派",
			Text:  fmt.Sprintf("重派 %s", orderID),
		},
		Color: "#6366F1",
	}
}

// 創建動作按鈕Footer
func (s *FlexMessageService) createActionButtonsFooter(oriText, pickupAddress, orderID string, showCancel bool) *messaging_api.FlexBox {
	contents := []messaging_api.FlexComponentInterface{}

	// 取消/重派按鈕
	if showCancel {
		contents = append(contents, s.createCancelOrderButton(orderID))
	} else {
		// 顯示重派按鈕（用於失敗狀態）
		contents = append(contents, s.createRedispatchButton(orderID))
	}

	return &messaging_api.FlexBox{
		Layout:   "vertical",
		Spacing:  "sm",
		Contents: contents,
	}
}

// 創建完成訂單專用的Footer（不顯示任何按鈕）
func (s *FlexMessageService) createCompletedOrderFooter(oriText, pickupAddress string) *messaging_api.FlexBox {
	return nil
}

// addCertificatePhoto 在 Flex Bubble 中加入證明照片區塊
func (s *FlexMessageService) addCertificatePhoto(bubble *messaging_api.FlexBubble, photoURL string) *messaging_api.FlexBubble {
	// 創建照片區塊
	photoSection := &messaging_api.FlexBox{
		Layout: "vertical",
		Margin: "lg",
		Contents: []messaging_api.FlexComponentInterface{
			// 分隔線
			&messaging_api.FlexSeparator{
				Margin: "md",
			},
			// 照片標題
			&messaging_api.FlexText{
				Text:   "司機抵達證明",
				Weight: "bold",
				Size:   "sm",
				Color:  "#6B7280",
				Margin: "md",
			},
			// 照片
			&messaging_api.FlexImage{
				Url:         photoURL,
				Size:        "full",
				AspectRatio: "20:13",
				AspectMode:  "cover",
				Margin:      "xs",
				Action: &messaging_api.UriAction{
					Uri: photoURL,
				},
			},
		},
	}

	// 將照片區塊加到 Body 的末尾
	if bubble.Body != nil {
		bubble.Body.Contents = append(bubble.Body.Contents, photoSection)
	}

	return bubble
}

// createAddressRow 創建地址行（參考設計的樣式）
func (s *FlexMessageService) createAddressRow(address string) *messaging_api.FlexBox {
	// URL 編碼地址並創建 Google Maps 導航連結
	encodedAddress := url.QueryEscape(address)
	googleMapsURL := fmt.Sprintf("https://www.google.com/maps/search/?api=1&query=%s", encodedAddress)

	return &messaging_api.FlexBox{
		Layout:  "horizontal",
		Spacing: "md",
		Contents: []messaging_api.FlexComponentInterface{
			&messaging_api.FlexText{
				Text:  "地址",
				Color: "#6B7280",
				Size:  "md",
				Flex:  2,
			},
			&messaging_api.FlexText{
				Text:  "開啟 Google Map",
				Color: "#3B82F6",
				Size:  "md",
				Flex:  5,
				Action: &messaging_api.UriAction{
					Label: "action",
					Uri:   googleMapsURL,
				},
			},
		},
	}
}
