package service

import (
	"right-backend/model"
)

// SSEEventManager 專門管理 SSE 事件的格式化和推送
// 提供高層級的 API 來統一處理各種訂單相關事件
type SSEEventManager struct {
	sseService *SSEService
}

// NewSSEEventManager 建立新的 SSE 事件管理器
func NewSSEEventManager(sseService *SSEService) *SSEEventManager {
	return &SSEEventManager{
		sseService: sseService,
	}
}

// PushDriverAcceptedOrder 推送司機接單事件
func (em *SSEEventManager) PushDriverAcceptedOrder(
	order *model.Order,
	driver *model.DriverInfo,
	distanceKm float64,
	estimatedMins int) {

	em.sseService.PushOrderEventWithDriverInfo(
		SSEPages.OrderRelated,
		EventDriverAcceptedOrder,
		order.ID.Hex(),
		string(order.Fleet),
		order.OriText,
		order.ShortID,
		driver,
		distanceKm,
		estimatedMins,
		nil,
	)

	// Discord 回覆訊息現在統一由 NotificationService 處理，不在此處重複發送
}

// PushDriverRejectedOrder 推送司機拒單事件
func (em *SSEEventManager) PushDriverRejectedOrder(
	order *model.Order,
	driver *model.DriverInfo,
	distanceKm float64,
	estimatedMins int) {

	em.sseService.PushOrderEventWithDriverInfo(
		SSEPages.OrderRelated,
		EventDriverRejectedOrder,
		order.ID.Hex(),
		string(order.Fleet),
		order.OriText,
		order.ShortID,
		driver,
		distanceKm,
		estimatedMins,
		nil,
	)

	// 註解：司機拒單不顯示 Discord 回覆訊息
	// 同時回覆 Discord 消息到第一張訂單卡片
	// if em.discordEventHandler != nil {
	// 	em.discordEventHandler.ReplyToOrderMessage(
	// 		context.Background(),
	// 		order,
	// 		string(EventDriverRejectedOrder),
	// 		string(order.Fleet),
	// 		driver.Name,
	// 		driver.CarPlate,
	// 		driver.CarColor,
	// 		distanceKm,
	// 		estimatedMins,
	// 	)
	// }
}

// PushDriverTimeoutOrder 推送司機逾時事件
func (em *SSEEventManager) PushDriverTimeoutOrder(
	order *model.Order,
	driver *model.DriverInfo,
	distanceKm float64,
	estimatedMins int) {

	em.sseService.PushOrderEventWithDriverInfo(
		SSEPages.OrderRelated,
		EventDriverTimeoutOrder,
		order.ID.Hex(),
		string(order.Fleet),
		order.OriText,
		order.ShortID,
		driver,
		distanceKm,
		estimatedMins,
		nil,
	)

	// 註解：暫時不顯示司機逾時的 Discord 回覆訊息
	// 同時回覆 Discord 消息到第一張訂單卡片
	// if em.discordEventHandler != nil {
	// 	em.discordEventHandler.ReplyToOrderMessage(
	// 		context.Background(),
	// 		order,
	// 		string(EventDriverTimeoutOrder),
	// 		string(order.Fleet),
	// 		driver.Name,
	// 		driver.CarPlate,
	// 		driver.CarColor,
	// 		distanceKm,
	// 		estimatedMins,
	// 	)
	// }
}

// PushDriverArrived 推送司機抵達事件
func (em *SSEEventManager) PushDriverArrived(
	order *model.Order,
	driver *model.DriverInfo,
	distanceKm float64,
	estimatedMins int) {

	em.sseService.PushOrderEventWithDriverInfo(
		SSEPages.OrderRelated,
		EventDriverArrived,
		order.ID.Hex(),
		string(order.Fleet),
		order.OriText,
		order.ShortID,
		driver,
		distanceKm,
		estimatedMins,
		nil,
	)

	// Discord 回覆訊息現在統一由 NotificationService 處理，不在此處重複發送
}

// PushCustomerOnBoard 推送客人上車事件
func (em *SSEEventManager) PushCustomerOnBoard(
	order *model.Order,
	driver *model.DriverInfo,
	distanceKm float64,
	estimatedMins int) {

	em.sseService.PushOrderEventWithDriverInfo(
		SSEPages.OrderRelated,
		EventCustomerOnBoard,
		order.ID.Hex(),
		string(order.Fleet),
		order.OriText,
		order.ShortID,
		driver,
		distanceKm,
		estimatedMins,
		nil,
	)

	// Discord 回覆訊息現在統一由 NotificationService 處理，不在此處重複發送
}

// PushOrderCompleted 推送訂單完成事件
func (em *SSEEventManager) PushOrderCompleted(
	order *model.Order,
	driver *model.DriverInfo,
	distanceKm float64,
	estimatedMins int) {

	em.sseService.PushOrderEventWithDriverInfo(
		SSEPages.OrderRelated,
		EventOrderCompleted,
		order.ID.Hex(),
		string(order.Fleet),
		order.OriText,
		order.ShortID,
		driver,
		distanceKm,
		estimatedMins,
		nil,
	)

	// Discord 回覆訊息現在統一由 NotificationService 處理，不在此處重複發送
}

// PushOrderFailed 推送訂單失敗事件
// 訂單失敗事件通常不涉及特定司機，所以使用簡化版本
func (em *SSEEventManager) PushOrderFailed(
	order *model.Order,
	reason string) {

	additionalData := map[string]interface{}{
		"reason":  reason,
		"status":  "failed",
		"message": "訂單流單",
	}

	em.sseService.PushOrderEventWithDetails(
		SSEPages.OrderRelated,
		EventOrderFailed,
		order.ID.Hex(),
		string(order.Fleet),
		order.OriText,
		order.ShortID,
		additionalData,
	)

	// Discord 回覆訊息現在統一由 NotificationService 處理，不在此處重複發送
}

// PushOrderCancelled 推送訂單取消事件
func (em *SSEEventManager) PushOrderCancelled(
	order *model.Order,
	previousStatus string,
	cancelReason string,
	cancelledByUserName string) {

	// 格式化取消信息，顯示是哪個調度取消了訂單
	cancelInfo := ""
	if cancelledByUserName != "" {
		cancelInfo = "調度" + cancelledByUserName + "取消"
	} else {
		cancelInfo = cancelReason
	}

	additionalData := map[string]interface{}{
		"previous_status":        previousStatus,
		"cancel_reason":          cancelReason,
		"cancelled_by_user_name": cancelledByUserName,
		"cancel_info":            cancelInfo,
		"message":                "訂單已取消",
	}

	em.sseService.PushOrderEventWithDetails(
		SSEPages.OrderRelated,
		EventOrderCancelled,
		order.ID.Hex(),
		string(order.Fleet),
		order.OriText,
		order.ShortID,
		additionalData,
	)
}
