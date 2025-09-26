package utils

import (
	"math"
	"regexp"
	"strconv"
	"strings"
)

// Haversine calculates the great-circle distance between two points on the earth (specified in decimal degrees).
func Haversine(lat1, lng1, lat2, lng2 float64) float64 {
	const R = 6371e3 // 地球半徑（公尺）
	phi1 := lat1 * math.Pi / 180
	phi2 := lat2 * math.Pi / 180
	deltaPhi := (lat2 - lat1) * math.Pi / 180
	deltaLambda := (lng2 - lng1) * math.Pi / 180

	a := math.Sin(deltaPhi/2)*math.Sin(deltaPhi/2) + math.Cos(phi1)*math.Cos(phi2)*math.Sin(deltaLambda/2)*math.Sin(deltaLambda/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return R * c / 1000 // 公里
}

// ParseTimeToMinutes 將時間字符串轉換為總分鐘數
// 支援多種格式: "1 小時 13 分鐘", "37 分", "1h 13m", "25 mins", "2:30" 等
func ParseTimeToMinutes(timeStr string) int {
	if timeStr == "" {
		return 0
	}

	timeStr = strings.TrimSpace(timeStr)
	totalMinutes := 0

	// 1. 匹配 "2:30" 或 "1:13:45" 時間格式
	timeColonRegex := regexp.MustCompile(`^(\d+):(\d+)(?::(\d+))?$`)
	if timeMatch := timeColonRegex.FindStringSubmatch(timeStr); len(timeMatch) >= 3 {
		if hours, err := strconv.Atoi(timeMatch[1]); err == nil {
			totalMinutes += hours * 60
		}
		if minutes, err := strconv.Atoi(timeMatch[2]); err == nil {
			totalMinutes += minutes
		}
		return totalMinutes
	}

	// 2. 匹配中文小時格式
	hourRegex := regexp.MustCompile(`(\d+)\s*小時`)
	if hourMatch := hourRegex.FindStringSubmatch(timeStr); len(hourMatch) > 1 {
		if hours, err := strconv.Atoi(hourMatch[1]); err == nil {
			totalMinutes += hours * 60
		}
	}

	// 3. 匹配中文分鐘格式 (支援 "分" 和 "分鐘")
	minuteRegex := regexp.MustCompile(`(\d+)\s*分鐘?`)
	if minuteMatch := minuteRegex.FindStringSubmatch(timeStr); len(minuteMatch) > 1 {
		if minutes, err := strconv.Atoi(minuteMatch[1]); err == nil {
			totalMinutes += minutes
		}
	}

	// 4. 匹配英文格式
	lowerTimeStr := strings.ToLower(timeStr)

	// 匹配小時 "1h", "1 hour", "1 hours"
	hourRegexEn := regexp.MustCompile(`(\d+)\s*(?:h|hour|hours)\b`)
	if hourMatch := hourRegexEn.FindStringSubmatch(lowerTimeStr); len(hourMatch) > 1 {
		if hours, err := strconv.Atoi(hourMatch[1]); err == nil {
			totalMinutes += hours * 60
		}
	}

	// 匹配分鐘 "30m", "30 min", "30 mins", "30 minute", "30 minutes"
	minuteRegexEn := regexp.MustCompile(`(\d+)\s*(?:m|min|mins|minute|minutes)\b`)
	if minuteMatch := minuteRegexEn.FindStringSubmatch(lowerTimeStr); len(minuteMatch) > 1 {
		if minutes, err := strconv.Atoi(minuteMatch[1]); err == nil {
			totalMinutes += minutes
		}
	}

	// 5. 如果還是沒有匹配到，嘗試純數字格式 (假設為分鐘)
	if totalMinutes == 0 {
		numRegex := regexp.MustCompile(`^\d+$`)
		if numRegex.MatchString(strings.TrimSpace(timeStr)) {
			if minutes, err := strconv.Atoi(strings.TrimSpace(timeStr)); err == nil {
				totalMinutes = minutes
			}
		}
	}

	return totalMinutes
}

// ParseDistanceToKm 從距離字串中解析出公里數
// 支援多種格式: "1.9 km", "550 m", "8.6 公里", "500 公尺"
func ParseDistanceToKm(distStr string) float64 {
	if distStr == "" {
		return 0
	}

	distStr = strings.ToLower(strings.TrimSpace(distStr))

	// 匹配中文格式：5.2 公里
	kmRegex := regexp.MustCompile(`(\d+\.?\d*)\s*公里`)
	if kmMatch := kmRegex.FindStringSubmatch(distStr); len(kmMatch) > 1 {
		if km, err := strconv.ParseFloat(kmMatch[1], 64); err == nil {
			return km
		}
	}

	// 匹配英文格式：5.2 km 或 5.2 kilometers
	kmRegexEn := regexp.MustCompile(`(\d+\.?\d*)\s*km`)
	if kmMatch := kmRegexEn.FindStringSubmatch(distStr); len(kmMatch) > 1 {
		if km, err := strconv.ParseFloat(kmMatch[1], 64); err == nil {
			return km
		}
	}

	// 匹配公尺格式並轉換為公里：1500 公尺 -> 1.5
	mRegex := regexp.MustCompile(`(\d+\.?\d*)\s*公尺`)
	if mMatch := mRegex.FindStringSubmatch(distStr); len(mMatch) > 1 {
		if meters, err := strconv.ParseFloat(mMatch[1], 64); err == nil {
			return meters / 1000 // 轉換為公里
		}
	}

	// 匹配英文公尺格式：1500 m -> 1.5
	mRegexEn := regexp.MustCompile(`(\d+\.?\d*)\s*m\b`)
	if mMatch := mRegexEn.FindStringSubmatch(distStr); len(mMatch) > 1 {
		if meters, err := strconv.ParseFloat(mMatch[1], 64); err == nil {
			return meters / 1000 // 轉換為公里
		}
	}

	// 通用數字提取作為後備方案
	re := regexp.MustCompile(`[0-9\.]+`)
	numStr := re.FindString(distStr)
	if dist, err := strconv.ParseFloat(numStr, 64); err == nil {
		// 如果包含 m 但不是 km，視為公尺
		if strings.Contains(distStr, "m") && !strings.Contains(distStr, "km") {
			return dist / 1000
		}
		return dist
	}

	return 0
}
