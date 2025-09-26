package utils

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// 提取客群、地址、備註、時間和跑腿標記
// 新格式: "CustomerGroup / 地址 備註" 或 "CustomerGroup / 地址 備註 hh:mm"
func ExOriText(input string) (customerGroup, address, remarks string, scheduledTime *time.Time, isErrand bool) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", "", "", nil, false
	}

	// 檢查是否包含"跑腿"
	isErrand = strings.Contains(input, "跑腿")

	// 分割客群和詳細內容
	parts := strings.SplitN(input, "/", 2)
	if len(parts) != 2 {
		// 如果格式不對，將整個輸入視為地址，客群為空
		return "", input, "", nil, isErrand
	}

	customerGroup = strings.ToUpper(strings.TrimSpace(parts[0]))
	details := strings.TrimSpace(parts[1])

	// 解析詳細內容中的地址、備註和時間
	address, remarks, scheduledTime = parseDetailsInternal(details)

	return customerGroup, address, remarks, scheduledTime, isErrand
}

// parseDetailsInternal 解析詳細內容中的地址、備註和時間
// 格式: "地址 備註" 或 "地址 備註 hh:mm"
func parseDetailsInternal(input string) (address, remarks string, scheduledTime *time.Time) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", "", nil
	}

	// 先檢查整個輸入中是否包含時間格式 hh:mm
	timeRegex := regexp.MustCompile(`\b(\d{1,2}:\d{2})\b`)
	timeMatch := timeRegex.FindStringSubmatch(input)

	var timeStr string

	if len(timeMatch) == 2 {
		timeStr = timeMatch[1]

		// 解析時間
		taipeiLocation := GetTaipeiLocation()
		now := time.Now().In(taipeiLocation)

		// 在台北時區解析時間
		parsed, err := time.ParseInLocation("2006-01-02 15:04", fmt.Sprintf("%s %s", now.Format("2006-01-02"), timeStr), taipeiLocation)
		if err == nil {
			// 如果解析的時間已經過去，設為明天
			if parsed.Before(now) {
				parsed = parsed.Add(24 * time.Hour)
			}
			// 轉換為 UTC 時間存儲
			utcTime := parsed.UTC()
			scheduledTime = &utcTime
		}
	}

	// 分割地址和備註：找到第一個空格（不移除時間，保留原始備註）
	spaceIndex := strings.Index(input, " ")
	if spaceIndex == -1 {
		// 沒有空白，整個都是地址
		return input, "", scheduledTime
	}

	address = strings.TrimSpace(input[:spaceIndex])
	remarks = strings.TrimSpace(input[spaceIndex+1:])

	return address, remarks, scheduledTime
}
