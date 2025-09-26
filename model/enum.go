package model

// OrderStatus 訂單狀態
type OrderStatus string

const (
	OrderStatusWaiting          OrderStatus = "等待接單"   // 等待接單
	OrderStatusScheduleAccepted OrderStatus = "預約單被接受" // 預約單被接受（尚未激活）
	OrderStatusEnroute          OrderStatus = "前往上車點"  // 前往上車點
	OrderStatusDriverArrived    OrderStatus = "司機抵達"   // 司機抵達
	OrderStatusExecuting        OrderStatus = "執行任務"   // 執行任務
	OrderStatusCompleted        OrderStatus = "完成"     // 完成
	OrderStatusCancelled        OrderStatus = "乘客取消"   // 乘客取消
	OrderStatusFailed           OrderStatus = "流單"     // 流單
	OrderStatusSystemFailed     OrderStatus = "系統失敗"   // 系統失敗
)

// DriverStatus 司機狀態
type DriverStatus string

const (
	DriverStatusIdle      DriverStatus = "閒置"
	DriverStatusEnroute   DriverStatus = "前往上車點"
	DriverStatusArrived   DriverStatus = "司機抵達"
	DriverStatusExecuting DriverStatus = "執行任務"
	DriverStatusInactive  DriverStatus = "停用中" // 保留用於啟用/停用司機
)

// NotifyType 通知類型
type NotifyType string

const (
	NotifyTypeNewOrder            NotifyType = "new_order"             // 新訂單通知
	NotifyTypeCancelOrder         NotifyType = "cancel_order"          // 取消訂單通知
	NotifyTypeCancelScheduleOrder NotifyType = "cancel_schedule_order" // 取消預約訂單通知
	NotifyTypeScheduleOrderNotify NotifyType = "schedule_order_notify" // 預約訂單提醒通知
	NotifyTypeChat                NotifyType = "chat"                  // 聊天消息通知
)

// TokenType JWT token 類型
type TokenType string

const (
	TokenTypeDriver TokenType = "driver" // 司機 token
	TokenTypeUser   TokenType = "user"   // 用戶 token
)

// FleetType 車隊類型
type FleetType string

const (
	FleetTypeRSK FleetType = "RSK" // RSK 車隊
	FleetTypeKD  FleetType = "KD"  // KD 車隊
	FleetTypeWEI FleetType = "WEI" // WEI 車隊
)

// UserRole 用戶角色
type UserRole string

const (
	RoleSystemAdmin UserRole = "系統管理員"
	RoleModerator   UserRole = "版主"
	RoleAdmin       UserRole = "管理員"
	RoleDispatcher  UserRole = "調度"
	RoleNone        UserRole = "無"
)

// NotificationTaskType 通知任務類型
type NotificationTaskType string

const (
	NotificationDiscord NotificationTaskType = "discord" // Discord 通知
	NotificationLine    NotificationTaskType = "line"    // LINE 通知
	NotificationSSE     NotificationTaskType = "sse"     // SSE 通知
)

// EventType Discord事件類型
type EventType string

const (
	EventDriverAccepted     EventType = "driver_accepted_order" // 司機接單
	EventScheduledAccepted  EventType = "scheduled_accepted"    // 預約單接受
	EventScheduledActivated EventType = "scheduled_activated"   // 預約單激活
	EventDriverRejected     EventType = "driver_rejected_order" // 司機拒單
	EventDriverTimeout      EventType = "driver_timeout_order"  // 司機逾時
	EventDriverArrived      EventType = "driver_arrived"        // 司機抵達
	EventCustomerOnBoard    EventType = "customer_on_board"     // 客人上車
	EventOrderCompleted     EventType = "order_completed"       // 訂單完成
	EventOrderFailed        EventType = "order_failed"          // 訂單失敗
	EventOrderCancelled     EventType = "order_cancelled"       // 訂單取消
	EventConversionMessage  EventType = "conversion_message"    // 轉換說明訊息
	EventScheduledWaiting   EventType = "scheduled_waiting"     // 預約單等待接單
	EventOrderConverted     EventType = "order_converted"       // 預約單轉換為即時單
	EventChat               EventType = "chat"                  // 聊天消息
)

// CreatedBy 訂單建立者類型
type CreatedBy string

const (
	CreatedByDiscord CreatedBy = "discord" // Discord 機器人建立
	CreatedByLine    CreatedBy = "line"    // LINE 機器人建立
	CreatedBySystem  CreatedBy = "system"  // 系統建立
)

// DiscordColor Discord embed顏色常量
type DiscordColor int

const (
	ColorSuccess   DiscordColor = 0x00FF00 // 綠色 - 成功事件
	ColorInfo      DiscordColor = 0x0080FF // 藍色 - 信息事件
	ColorWarning   DiscordColor = 0xFF9800 // 琥珀色 - 警告事件
	ColorError     DiscordColor = 0xF44336 // 紅色 - 錯誤事件
	ColorProgress  DiscordColor = 0xFFA500 // 橙色 - 進行中事件
	ColorComplete  DiscordColor = 0x00C851 // 深綠色 - 完成事件
	ColorRejected  DiscordColor = 0xFF6B35 // 橙紅色 - 拒絕事件
	ColorCancelled DiscordColor = 0x9E9E9E // 灰色 - 取消事件
	ColorChat      DiscordColor = 0x9C27B0 // 紫色 - 聊天事件
	ColorDefault   DiscordColor = 0x2F3136 // 深灰色 - 默認
)

// DriverStatusChangeReason 司機狀態變更原因
type DriverStatusChangeReason string

const (
	DriverReasonSystemUpdate     DriverStatusChangeReason = "系統更新"
	DriverReasonAcceptOrder      DriverStatusChangeReason = "接受訂單"
	DriverReasonActivateSchedule DriverStatusChangeReason = "激活預約訂單"
	DriverReasonLineCancellation DriverStatusChangeReason = "訂單被LINE取消，重置為閒置"
	DriverReasonWebCancellation  DriverStatusChangeReason = "訂單被網頁用戶取消，重置為閒置"
	DriverReasonDiscordCancel    DriverStatusChangeReason = "訂單被Discord用戶取消，重置為閒置"
	DriverReasonOrderComplete    DriverStatusChangeReason = "訂單完成"
	DriverReasonDriverArrived    DriverStatusChangeReason = "司機抵達"
	DriverReasonPickupCustomer   DriverStatusChangeReason = "客人上車"
)

// SlashCommand Discord slash指令類型
type SlashCommand string

const (
	SlashCommandPing                   SlashCommand = "ping"                       // ping指令
	SlashCommandResetDriver            SlashCommand = "reset-driver"               // 重置司機狀態
	SlashCommandCleanFailedOrders      SlashCommand = "clean-failed-orders"        // 清理流單
	SlashCommandSearchScheduled        SlashCommand = "search-scheduled-orders"    // 查詢預約單
	SlashCommandSearchOnlineDrivers    SlashCommand = "search-online-drivers"      // 查詢在線司機
	SlashCommandWeiEmptyOrderAndDriver SlashCommand = "wei-empty-order-and-driver" // WEI車隊清空訂單和司機狀態
	SlashCommandWeiCreateExampleOrder  SlashCommand = "wei-create-example-order"   // WEI車隊建立測試訂單
)
