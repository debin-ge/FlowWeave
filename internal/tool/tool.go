package tool

import (
	"context"
	"fmt"
	"sync"

	"flowweave/internal/provider"
)

// Tool 工具接口
type Tool interface {
	// Name 工具名称（唯一标识）
	Name() string

	// Description 工具描述（传给 LLM 作为 function description）
	Description() string

	// Parameters 参数的 JSON Schema（传给 LLM 作为 function parameters）
	Parameters() interface{}

	// Execute 执行工具，arguments 为 LLM 传入的 JSON string
	// 返回结果文本，将作为 tool message 回传给 LLM
	Execute(ctx context.Context, arguments string) (string, error)
}

// Registry 工具注册表
type Registry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

// NewRegistry 创建工具注册表
func NewRegistry() *Registry {
	return &Registry{
		tools: make(map[string]Tool),
	}
}

// Register 注册工具
func (r *Registry) Register(t Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[t.Name()] = t
}

// Get 获取工具
func (r *Registry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

// Has 检查工具是否存在
func (r *Registry) Has(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.tools[name]
	return ok
}

// Definitions 将注册的工具转为 Provider ToolDefinition 列表
// names 为空时返回所有工具；否则只返回指定名称的工具
func (r *Registry) Definitions(names ...string) []provider.ToolDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var defs []provider.ToolDefinition

	if len(names) == 0 {
		// 返回所有工具
		for _, t := range r.tools {
			defs = append(defs, provider.ToolDefinition{
				Type: "function",
				Function: provider.ToolFunction{
					Name:        t.Name(),
					Description: t.Description(),
					Parameters:  t.Parameters(),
				},
			})
		}
	} else {
		// 只返回指定的工具
		for _, name := range names {
			if t, ok := r.tools[name]; ok {
				defs = append(defs, provider.ToolDefinition{
					Type: "function",
					Function: provider.ToolFunction{
						Name:        t.Name(),
						Description: t.Description(),
						Parameters:  t.Parameters(),
					},
				})
			}
		}
	}

	return defs
}

// Execute 执行指定名称的工具
func (r *Registry) Execute(ctx context.Context, name string, arguments string) (string, error) {
	t, ok := r.Get(name)
	if !ok {
		return "", fmt.Errorf("tool not found: %s", name)
	}
	return t.Execute(ctx, arguments)
}

// --- Context 注入 ---

type contextKey string

const registryContextKey contextKey = "tool_registry"

// WithRegistry 将 ToolRegistry 注入到 context 中
func WithRegistry(ctx context.Context, reg *Registry) context.Context {
	return context.WithValue(ctx, registryContextKey, reg)
}

// RegistryFromContext 从 context 获取 ToolRegistry
func RegistryFromContext(ctx context.Context) (*Registry, bool) {
	reg, ok := ctx.Value(registryContextKey).(*Registry)
	return reg, ok
}
