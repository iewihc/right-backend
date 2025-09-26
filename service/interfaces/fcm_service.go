package interfaces

import "context"

// FCMService FCM推送服務接口
type FCMService interface {
	Send(ctx context.Context, token string, data map[string]interface{}, notification map[string]interface{}) error
}
