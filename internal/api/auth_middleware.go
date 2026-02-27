package api

import (
	"context"
	"database/sql"
	"flowweave/internal/domain/workflow/port"
	applog "flowweave/internal/platform/log"
	"fmt"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

// JWTConfig JWT 鉴权配置
type JWTConfig struct {
	Secret string // HMAC 签名密钥
	Issuer string // 可选签发者校验
}

// authMiddleware JWT 鉴权中间件
// 验证 Authorization: Bearer <token> 的有效性
func authMiddleware(cfg *JWTConfig, repo port.Repository) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// 从 Authorization 头中提取 token
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				writeErrorCode(w, http.StatusUnauthorized, "unauthorized", "Missing Authorization header")
				return
			}

			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
				writeErrorCode(w, http.StatusUnauthorized, "unauthorized", "Invalid Authorization header format")
				return
			}
			tokenStr := parts[1]

			// 解析并验证 JWT
			parserOpts := []jwt.ParserOption{jwt.WithValidMethods([]string{"HS256", "HS384", "HS512"})}
			if cfg.Issuer != "" {
				parserOpts = append(parserOpts, jwt.WithIssuer(cfg.Issuer))
			}

			token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
				if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
					return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
				}
				return []byte(cfg.Secret), nil
			}, parserOpts...)

			if err != nil || !token.Valid {
				applog.Warn("[Auth] Invalid JWT token", "error", err)
				writeErrorCode(w, http.StatusUnauthorized, "unauthorized", "Invalid or expired token")
				return
			}

			claims, ok := token.Claims.(jwt.MapClaims)
			if !ok {
				writeErrorCode(w, http.StatusUnauthorized, "unauthorized", "Invalid token claims")
				return
			}

			// 提取 scope 信息
			orgID, _ := claims["org_id"].(string)
			tenantID, _ := claims["tenant_id"].(string)
			subject, _ := claims["sub"].(string)

			if orgID == "" || tenantID == "" {
				writeErrorCode(w, http.StatusForbidden, "forbidden_scope", "Missing org_id or tenant_id in token")
				return
			}
			if err := validateScopeClaims(r.Context(), repo, orgID, tenantID); err != nil {
				if errorsIsScopeInvalid(err) {
					writeErrorCode(w, http.StatusForbidden, "forbidden_scope", err.Error())
					return
				}
				applog.Error("[Auth] Scope validation failed", "error", err)
				writeErrorCode(w, http.StatusInternalServerError, "scope_validation_failed", "Failed to validate auth scope")
				return
			}

			// 提取 roles
			var roles []string
			if rolesRaw, ok := claims["roles"].([]interface{}); ok {
				for _, r := range rolesRaw {
					if s, ok := r.(string); ok {
						roles = append(roles, s)
					}
				}
			}

			// 注入 Scope 到 context
			scope := &Scope{
				OrgID:    orgID,
				TenantID: tenantID,
				Subject:  subject,
				Roles:    roles,
			}
			ctx := WithScope(r.Context(), scope)

			applog.Debug("[Auth] Scope injected",
				"org_id", orgID,
				"tenant_id", tenantID,
				"subject", subject,
			)

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func validateScopeClaims(ctx context.Context, repo port.Repository, orgID, tenantID string) error {
	if repo == nil {
		return fmt.Errorf("scope repository is not configured")
	}

	org, err := repo.GetOrganization(ctx, orgID)
	if err != nil {
		return err
	}
	if org == nil {
		return fmt.Errorf("invalid org_id in token")
	}
	if org.Status != "" && !strings.EqualFold(org.Status, "active") {
		return fmt.Errorf("organization is not active")
	}

	tenant, err := repo.GetTenant(ctx, tenantID)
	if err != nil {
		return err
	}
	if tenant == nil {
		return fmt.Errorf("invalid tenant_id in token")
	}
	if tenant.Status != "" && !strings.EqualFold(tenant.Status, "active") {
		return fmt.Errorf("tenant is not active")
	}
	if tenant.OrgID != orgID {
		return fmt.Errorf("tenant does not belong to org")
	}

	return nil
}

func errorsIsScopeInvalid(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	if strings.Contains(msg, "invalid org_id") ||
		strings.Contains(msg, "invalid tenant_id") ||
		strings.Contains(msg, "not active") ||
		strings.Contains(msg, "does not belong to org") {
		return true
	}
	// allow SQL no rows from custom repos as invalid scope
	return err == sql.ErrNoRows
}

// writeErrorCode 带错误码的统一错误响应
func writeErrorCode(w http.ResponseWriter, status int, code string, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	fmt.Fprintf(w, `{"code":%d,"error":"%s","message":"%s"}`, status, code, message)
}
