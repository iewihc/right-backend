package auth

import (
	"right-backend/model"
)

type DriverLoginInput struct {
	Body struct {
		Account            string `json:"account" doc:"帳號" example:"driver001@taxi.com"`
		Password           string `json:"password" doc:"密碼" example:"123456"`
		DeviceModelName    string `json:"device_model_name,omitempty" doc:"設備型號名稱（選填）" example:"iPhone15,4"`
		DeviceDeviceName   string `json:"device_device_name,omitempty" doc:"設備名稱（選填）" example:"My iPhone"`
		DeviceBrand        string `json:"device_brand,omitempty" doc:"設備品牌（選填）" example:"Apple"`
		DeviceManufacturer string `json:"device_manufacturer,omitempty" doc:"設備製造商（選填）" example:"Apple Inc."`
		DeviceAppVersion   string `json:"device_app_version,omitempty" doc:"應用程式版本（選填）" example:"1.0.0"`
	} `json:"body"`
}

type LoginInput struct {
	Body struct {
		Account  string `json:"account" doc:"帳號" example:"user001@gmail.com"`
		Password string `json:"password" doc:"密碼" example:"123456"`
	} `json:"body"`
}

type UserLoginResponse struct {
	Body struct {
		User    *model.User `json:"user"`
		Token   string      `json:"token" example:"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."`
		Message string      `json:"message" example:"登入成功"`
	} `json:"body"`
}

type DriverLoginResponse struct {
	Body struct {
		Driver  *model.DriverInfo `json:"driver"`
		Token   string            `json:"token" example:"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."`
		Message string            `json:"message" example:"登入成功"`
	} `json:"body"`
}

type DriverRegisterInput struct {
	Body struct {
		Name       string `json:"name" doc:"司機本名" example:"王小明"`
		Nickname   string `json:"nickname" doc:"司機暱稱" example:"小明"`
		DriverNo   string `json:"driver_no" doc:"司機編號" example:"D001"`
		Account    string `json:"account" doc:"司機帳號" example:"driver001@taxi.com"`
		Password   string `json:"password" doc:"密碼" example:"driver123"`
		CarPlate   string `json:"car_plate" doc:"車牌號碼" example:"6793黑"`
		Fleet      string `json:"fleet" doc:"所屬車隊" example:"RSK"`
		CarModel   string `json:"car_model" doc:"車型" example:"納智捷S5"`
		CarAge     int    `json:"car_age" doc:"車齡" example:"3"`
		CarColor   string `json:"car_color" doc:"車輛顏色" example:"白色"`
		Referrer   string `json:"referrer,omitempty" doc:"推薦人（選填）" example:"推薦人A"`
		JkoAccount string `json:"jko_account,omitempty" doc:"JKO帳號（選填）" example:"jko001"`
	} `json:"body"`
}

type DriverRegisterResponse struct {
	Body struct {
		Driver  *model.DriverInfo `json:"driver"`
		Message string            `json:"message" example:"註冊成功，等待審核"`
	} `json:"body"`
}
