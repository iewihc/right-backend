package utils

import (
	"right-backend/model"
)

// GetDriverInfo 直接從 DriverInfo 結構格式化司機資訊
func GetDriverInfo(driver *model.DriverInfo) string {
	if driver == nil {
		return "未知車隊|未知編號|未知司機"
	}

	fleet := string(driver.Fleet)
	if fleet == "" {
		fleet = "未知車隊"
	}
	driverNo := driver.DriverNo
	if driverNo == "" {
		driverNo = "未知編號"
	}
	name := driver.Name
	if name == "" {
		name = "未知司機"
	}

	return fleet + "|" + driverNo + "|" + name
}

// GetDriverInfoWithPlate 格式化司機資訊（包含車牌和顏色，不含車隊）
// 格式：ABC-5808(黑) | 料理鼠王
func GetDriverInfoWithPlate(driver *model.DriverInfo) string {
	if driver == nil {
		return "未知車牌 | 未知司機"
	}

	// 組合車牌和顏色
	carPlate := driver.CarPlate
	if carPlate == "" {
		carPlate = "未知車牌"
	}

	carColor := driver.CarColor
	if carColor != "" {
		carPlate = carPlate + "(" + carColor + ")"
	}

	name := driver.Name
	if name == "" {
		name = "未知司機"
	}

	return carPlate + " | " + name
}

// GetOrderShortID 從完整的訂單 ID 生成短 ID，格式為 # + 最後四碼
// 例如：ObjectID("507f1f77bcf86cd799439011") -> "#9011"
func GetOrderShortID(fullOrderID string) string {
	if fullOrderID == "" {
		return ""
	}

	// 如果長度不足4位，直接加前綴返回
	if len(fullOrderID) < 4 {
		return "#" + fullOrderID
	}

	// 返回最後四位並加上前綴
	return "#" + fullOrderID[len(fullOrderID)-4:]
}
