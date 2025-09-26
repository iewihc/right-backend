package middleware

import (
	"context"
	"fmt"
	"net/http"
	"right-backend/model"
	"right-backend/service"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/golang-jwt/jwt/v5"
)

type DriverAuthMiddleware struct {
	driverService *service.DriverService
	jwtSecretKey  string
}

func NewDriverAuthMiddleware(driverService *service.DriverService, jwtSecretKey string) *DriverAuthMiddleware {
	return &DriverAuthMiddleware{
		driverService: driverService,
		jwtSecretKey:  jwtSecretKey,
	}
}

func (m *DriverAuthMiddleware) Auth() func(huma.Context, func(huma.Context)) {
	return func(ctx huma.Context, next func(huma.Context)) {
		// 從 Authorization header 中獲取 token
		authHeader := ctx.Header("Authorization")
		if authHeader == "" {
			ctx.SetStatus(http.StatusUnauthorized)
			ctx.SetHeader("Content-Type", "application/json")
			ctx.BodyWriter().Write([]byte(`{"message":"缺少授權標頭","detail":"missing authorization header"}`))
			return
		}

		// 檢查 Bearer 前綴
		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || parts[0] != "Bearer" {
			ctx.SetStatus(http.StatusUnauthorized)
			ctx.SetHeader("Content-Type", "application/json")
			ctx.BodyWriter().Write([]byte(`{"message":"無效的授權格式","detail":"invalid authorization format"}`))
			return
		}

		tokenString := parts[1]

		// 解析 JWT token
		token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
			// 驗證簽名方法
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
			}
			return []byte(m.jwtSecretKey), nil
		})

		if err != nil {
			ctx.SetStatus(http.StatusUnauthorized)
			ctx.SetHeader("Content-Type", "application/json")
			ctx.BodyWriter().Write([]byte(fmt.Sprintf(`{"code":401,"message":"無效的token","detail":"%s"}`, err.Error())))
			return
		}

		if !token.Valid {
			ctx.SetStatus(http.StatusUnauthorized)
			ctx.SetHeader("Content-Type", "application/json")
			ctx.BodyWriter().Write([]byte(`{"code":401,"message":"token已過期或無效","detail":"invalid token"}`))
			return
		}

		// 提取 claims
		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			ctx.SetStatus(http.StatusUnauthorized)
			ctx.SetHeader("Content-Type", "application/json")
			ctx.BodyWriter().Write([]byte(`{"code":401,"message":"無法解析token claims","detail":"invalid token claims"}`))
			return
		}

		// 檢查 token 類型
		tokenType, ok := claims["type"].(string)
		if !ok || tokenType != string(model.TokenTypeDriver) {
			ctx.SetStatus(http.StatusUnauthorized)
			ctx.SetHeader("Content-Type", "application/json")
			ctx.BodyWriter().Write([]byte(`{"code":401,"message":"無效的token類型","detail":"invalid token type"}`))
			return
		}

		// 提取司機ID
		driverID, ok := claims["driver_id"].(string)
		if !ok {
			ctx.SetStatus(http.StatusUnauthorized)
			ctx.SetHeader("Content-Type", "application/json")
			ctx.BodyWriter().Write([]byte(`{"message":"token中缺少司機ID","detail":"missing driver_id in token"}`))
			return
		}

		// 從資料庫獲取司機資訊
		driver, err := m.driverService.GetDriverByID(context.Background(), driverID)
		if err != nil {
			ctx.SetStatus(http.StatusUnauthorized)
			ctx.SetHeader("Content-Type", "application/json")
			ctx.BodyWriter().Write([]byte(fmt.Sprintf(`{"message":"司機不存在","detail":"%s"}`, err.Error())))
			return
		}

		// 將司機資訊添加到 context 中，讓後續的 handler 可以使用
		ctx = huma.WithValue(ctx, "driver", driver)
		ctx = huma.WithValue(ctx, "driver_id", driverID)
		ctx = huma.WithValue(ctx, "account", claims["account"])

		// 繼續到下一個中間件或 handler
		next(ctx)
	}
}
