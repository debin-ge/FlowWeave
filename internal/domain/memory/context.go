package memory

import "context"

type contextKey int

const (
	coordinatorKey contextKey = iota
	conversationIDKey
	userIDKey
)

// WithCoordinator 将 MemoryCoordinator 注入 context
func WithCoordinator(ctx context.Context, mc *Coordinator) context.Context {
	return context.WithValue(ctx, coordinatorKey, mc)
}

// CoordinatorFromContext 从 context 获取 MemoryCoordinator
func CoordinatorFromContext(ctx context.Context) (*Coordinator, bool) {
	mc, ok := ctx.Value(coordinatorKey).(*Coordinator)
	return mc, ok
}

// WithConversationID 将 conversation_id 注入 context
func WithConversationID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, conversationIDKey, id)
}

// ConversationIDFromContext 从 context 获取 conversation_id
func ConversationIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(conversationIDKey).(string)
	return id
}

// WithUserID 将 user_id 注入 context
func WithUserID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, userIDKey, id)
}

// UserIDFromContext 从 context 获取 user_id
func UserIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(userIDKey).(string)
	return id
}
