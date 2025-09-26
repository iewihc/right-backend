package utils

import "time"

// GetTaipeiLocation 取得台北時區
func GetTaipeiLocation() *time.Location {
	return time.FixedZone("Asia/Taipei", 8*3600)
}

// GetTaipeiTimeString 取得台北時間的時:分格式字串 (HH:mm)
func GetTaipeiTimeString() string {
	return time.Now().In(GetTaipeiLocation()).Format("15:04")
}

// GetTaipeiTimeFromTime 將指定時間轉換為台北時間的時:分格式字串 (HH:mm)
func GetTaipeiTimeFromTime(t time.Time) string {
	return t.In(GetTaipeiLocation()).Format("15:04")
}

// FormatTaipeiTime 將指定時間轉換為台北時間的月日時分格式字串 (MM/dd HH:mm)
func FormatTaipeiTime(t time.Time) string {
	return t.In(GetTaipeiLocation()).Format("01/02 15:04")
}

// FormatTimeInTaipeiHMS 將時間轉換為台北時區的時分秒格式
func FormatTimeInTaipeiHMS(t time.Time) string {
	return t.In(GetTaipeiLocation()).Format("15:04:05")
}

// NowInTaipei 取得當前台北時間
func NowInTaipei() time.Time {
	return time.Now().In(GetTaipeiLocation())
}

// NowUTC 取得當前 UTC 時間（用於存儲到 MongoDB）
func NowUTC() time.Time {
	return time.Now().UTC()
}

// ToTaipeiTime 將任意時間轉換為台北時間
func ToTaipeiTime(t time.Time) time.Time {
	return t.In(GetTaipeiLocation())
}

// FormatTaipeiDateTime 將時間轉換為台北時間的完整日期時間格式 (YYYY-MM-DD HH:mm:ss)
func FormatTaipeiDateTime(t time.Time) string {
	return t.In(GetTaipeiLocation()).Format("2006-01-02 15:04:05")
}

// ParseTaipeiTime 解析台北時區的時間字串
func ParseTaipeiTime(layout, value string) (time.Time, error) {
	return time.ParseInLocation(layout, value, GetTaipeiLocation())
}
