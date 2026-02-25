package api

import (
	"context"
	"fmt"
)

// Scope 租户作用域（注入到 context）
type Scope struct {
	OrgID    string   `json:"org_id"`
	TenantID string   `json:"tenant_id"`
	Subject  string   `json:"subject"`
	Roles    []string `json:"roles,omitempty"`
}

type scopeContextKey struct{}

// WithScope 注入 Scope 到 context
func WithScope(ctx context.Context, scope *Scope) context.Context {
	return context.WithValue(ctx, scopeContextKey{}, scope)
}

// ScopeFrom 从 context 提取 Scope
func ScopeFrom(ctx context.Context) (*Scope, error) {
	scope, ok := ctx.Value(scopeContextKey{}).(*Scope)
	if !ok || scope == nil {
		return nil, fmt.Errorf("scope not found in context")
	}
	return scope, nil
}

// MustScopeFrom 从 context 提取 Scope，panic if missing（仅用于已鉴权路由）
func MustScopeFrom(ctx context.Context) *Scope {
	scope, err := ScopeFrom(ctx)
	if err != nil {
		panic("scope missing from context: middleware not applied?")
	}
	return scope
}
