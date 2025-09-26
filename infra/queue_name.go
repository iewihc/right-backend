package infra

// QueueName 定義 RabbitMQ 隊列名稱的枚舉類型
type QueueName string

const (
	// QueueNameOrders 即時單隊列
	QueueNameOrders QueueName = "orders_queue"

	// QueueNameOrdersSchedule 預約單隊列
	QueueNameOrdersSchedule QueueName = "orders_schedule_queue"
)

// String 實現 Stringer 接口，返回隊列名稱字符串
func (qn QueueName) String() string {
	return string(qn)
}

// GetAllQueueNames 返回所有定義的隊列名稱
func GetAllQueueNames() []QueueName {
	return []QueueName{
		QueueNameOrders,
		QueueNameOrdersSchedule,
	}
}
