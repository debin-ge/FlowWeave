package rag

import "context"

// ── Scope context 注入（轻量 Scope，避免 import cycle）──────────

// ScopeInfo 多租户作用域（RAG 内部使用，避免循环依赖 api 包）
type ScopeInfo struct {
	OrgID    string
	TenantID string
}

type scopeContextKey struct{}

// WithScopeInfo 注入 ScopeInfo 到 context
func WithScopeInfo(ctx context.Context, s *ScopeInfo) context.Context {
	return context.WithValue(ctx, scopeContextKey{}, s)
}

// GetScopeFromContext 从 context 提取 ScopeInfo
func GetScopeFromContext(ctx context.Context) *ScopeInfo {
	val, _ := ctx.Value(scopeContextKey{}).(*ScopeInfo)
	return val
}
