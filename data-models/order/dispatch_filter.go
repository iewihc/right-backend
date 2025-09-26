package order

// DispatchOrdersFilter 調度訂單過濾器
type DispatchOrdersFilter struct {
	StartDate     string   `json:"start_date" doc:"開始日期"`
	EndDate       string   `json:"end_date" doc:"結束日期"`
	Fleet         string   `json:"fleet" doc:"車隊"`
	CustomerGroup string   `json:"customer_group" doc:"客戶群組"`
	Status        []string `json:"status" doc:"訂單狀態列表"`
	PickupAddress string   `json:"pickup_address" doc:"上車地點"`
	OrderID       string   `json:"order_id" doc:"訂單ID"`
	Driver        string   `json:"driver" doc:"司機"`
	PassengerID   string   `json:"passenger_id" doc:"乘客ID"`
}
