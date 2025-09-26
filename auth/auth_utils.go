package auth

import (
	"context"
	"errors"
	"right-backend/model"

	"github.com/golang-jwt/jwt/v5"
)

var (
	ErrDriverNotFound = errors.New("driver not found in context")
	ErrInvalidDriver  = errors.New("invalid driver type in context")
	ErrUserNotFound   = errors.New("user not found in context")
	ErrInvalidUser    = errors.New("invalid user type in context")
)

func GetDriverFromContext(ctx context.Context) (*model.DriverInfo, error) {
	driverValue := ctx.Value("driver")
	if driverValue == nil {
		return nil, ErrDriverNotFound
	}

	driver, ok := driverValue.(*model.DriverInfo)
	if !ok {
		return nil, ErrInvalidDriver
	}

	return driver, nil
}

func GetUserFromContext(ctx context.Context) (*model.User, error) {
	userValue := ctx.Value("user")
	if userValue == nil {
		return nil, ErrUserNotFound
	}

	user, ok := userValue.(*model.User)
	if !ok {
		return nil, ErrInvalidUser
	}

	return user, nil
}

// JWT 驗證相關的通用錯誤
var (
	ErrInvalidToken            = errors.New("invalid token")
	ErrTokenExpired            = errors.New("token expired")
	ErrInvalidTokenType        = errors.New("invalid token type")
	ErrMissingDriverID         = errors.New("missing driver_id in token")
	ErrMissingUserID           = errors.New("missing user_id in token")
	ErrUnexpectedSigningMethod = errors.New("unexpected signing method")
)

// ValidateJWTToken 通用的 JWT token 驗證函數
func ValidateJWTToken(tokenString string, jwtSecretKey string) (map[string]interface{}, error) {
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ErrUnexpectedSigningMethod
		}
		return []byte(jwtSecretKey), nil
	})

	if err != nil {
		return nil, err
	}

	if !token.Valid {
		return nil, ErrInvalidToken
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, errors.New("invalid token claims")
	}

	// 將 claims 轉換為 map[string]interface{} 方便使用
	result := make(map[string]interface{})
	for key, value := range claims {
		result[key] = value
	}

	return result, nil
}
