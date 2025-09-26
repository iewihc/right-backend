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

type UserAuthMiddleware struct {
	userService  *service.UserService
	jwtSecretKey string
}

func NewUserAuthMiddleware(userService *service.UserService, jwtSecretKey string) *UserAuthMiddleware {
	return &UserAuthMiddleware{
		userService:  userService,
		jwtSecretKey: jwtSecretKey,
	}
}

func (m *UserAuthMiddleware) Auth() func(huma.Context, func(huma.Context)) {
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
		if !ok || tokenType != string(model.TokenTypeUser) {
			ctx.SetStatus(http.StatusUnauthorized)
			ctx.SetHeader("Content-Type", "application/json")
			ctx.BodyWriter().Write([]byte(`{"code":401,"message":"無效的token類型","detail":"invalid token type"}`))
			return
		}

		// 提取用戶ID
		userID, ok := claims["user_id"].(string)
		if !ok {
			ctx.SetStatus(http.StatusUnauthorized)
			ctx.SetHeader("Content-Type", "application/json")
			ctx.BodyWriter().Write([]byte(`{"message":"token中缺少用戶ID","detail":"missing user_id in token"}`))
			return
		}

		// 從資料庫獲取用戶資訊
		user, err := m.userService.GetUserByID(context.Background(), userID)
		if err != nil {
			ctx.SetStatus(http.StatusUnauthorized)
			ctx.SetHeader("Content-Type", "application/json")
			ctx.BodyWriter().Write([]byte(fmt.Sprintf(`{"message":"用戶不存在","detail":"%s"}`, err.Error())))
			return
		}

		// 打印用戶操作
		m.PrintUserOperation(ctx, user)

		// 將用戶資訊添加到 context 中，讓後續的 handler 可以使用
		ctx = huma.WithValue(ctx, "user", user)
		ctx = huma.WithValue(ctx, "user_id", userID)
		ctx = huma.WithValue(ctx, "account", claims["account"])

		// 繼續到下一個中間件或 handler
		next(ctx)
	}
}

// 從 context 中獲取用戶資訊
func (m *UserAuthMiddleware) GetUserFromContext(ctx huma.Context) (*model.User, bool) {
	user, ok := ctx.Context().Value("user").(*model.User)
	return user, ok
}

// 從 context 中獲取用戶ID
func (m *UserAuthMiddleware) GetUserIDFromContext(ctx huma.Context) (string, bool) {
	userID, ok := ctx.Context().Value("user_id").(string)
	return userID, ok
}

// 打印用戶操作日誌
func (m *UserAuthMiddleware) PrintUserOperation(ctx huma.Context, user *model.User) {
	fmt.Printf("🔑 JWT Token 驗證成功！\n")
	fmt.Printf("👤 用戶資料:\n")
	fmt.Printf("  - ID: %s\n", user.ID.Hex())
	fmt.Printf("  - 帳號: %s\n", user.Account)
	fmt.Printf("  - 角色: %s\n", user.Role)
	fmt.Printf("  - 所屬車隊: %s\n", user.Fleet)
	fmt.Printf("  - 權限: %v\n", user.Permissions)
	fmt.Printf("  - 車隊檢視權限: %s\n", user.FleetAccess)
	fmt.Printf("  - 是否啟用: %t\n", user.IsActive)
	fmt.Printf("  - 建立時間: %v\n", user.CreatedAt)
	fmt.Printf("  - 更新時間: %v\n", user.UpdatedAt)
}

// 通用的打印用戶操作函數
func PrintUserOperation(ctx huma.Context, operation string, details string) {
	if user, ok := ctx.Context().Value("user").(*model.User); ok {
		fmt.Printf("👤 用戶操作: %s - 用戶: %s (帳號: %s) - %s\n", operation, user.Account, user.Role, details)
	}
}
