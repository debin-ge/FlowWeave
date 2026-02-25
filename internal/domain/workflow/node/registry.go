package node

import (
	"encoding/json"
	"fmt"
	"sync"

	types "flowweave/internal/domain/workflow/model"
)

// NodeConstructor 节点构造函数类型
type NodeConstructor func(id string, data json.RawMessage) (Node, error)

// Registry 节点类型注册表
// 所有节点类型在 init() 中注册自身的构造函数
type Registry struct {
	mu           sync.RWMutex
	constructors map[types.NodeType]NodeConstructor
}

// globalRegistry 全局节点注册表实例
var globalRegistry = &Registry{
	constructors: make(map[types.NodeType]NodeConstructor),
}

// Register 注册节点类型的构造函数
func Register(nodeType types.NodeType, constructor NodeConstructor) {
	globalRegistry.mu.Lock()
	defer globalRegistry.mu.Unlock()
	globalRegistry.constructors[nodeType] = constructor
}

// GetRegistry 获取全局注册表
func GetRegistry() *Registry {
	return globalRegistry
}

// Get 根据节点类型获取构造函数
func (r *Registry) Get(nodeType types.NodeType) (NodeConstructor, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.constructors[nodeType]
	return c, ok
}

// RegisteredTypes 返回所有已注册的节点类型
func (r *Registry) RegisteredTypes() []types.NodeType {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]types.NodeType, 0, len(r.constructors))
	for nt := range r.constructors {
		result = append(result, nt)
	}
	return result
}

// Factory 节点工厂，通过注册表创建节点实例
type Factory struct {
	registry *Registry
}

// NewFactory 创建节点工厂
func NewFactory() *Factory {
	return &Factory{registry: globalRegistry}
}

// NewFactoryWithRegistry 使用指定注册表创建工厂
func NewFactoryWithRegistry(registry *Registry) *Factory {
	return &Factory{registry: registry}
}

// CreateNode 根据配置创建节点实例
func (f *Factory) CreateNode(config types.NodeConfig) (Node, error) {
	// 从 data 中解析节点类型
	var nodeData types.NodeData
	if err := json.Unmarshal(config.Data, &nodeData); err != nil {
		return nil, fmt.Errorf("failed to parse node data for node %s: %w", config.ID, err)
	}

	nodeType := types.NodeType(nodeData.Type)

	constructor, ok := f.registry.Get(nodeType)
	if !ok {
		return nil, fmt.Errorf("unknown node type: %s (node id: %s)", nodeType, config.ID)
	}

	n, err := constructor(config.ID, config.Data)
	if err != nil {
		return nil, fmt.Errorf("failed to create node %s (type: %s): %w", config.ID, nodeType, err)
	}

	// 应用错误策略配置
	if nodeData.ErrorStrategy != types.ErrorStrategyNone {
		type errorStrategySetter interface {
			SetErrorStrategy(types.ErrorStrategy)
			SetDefaultValue(map[string]interface{})
			SetRetryConfig(*types.RetryConfig)
		}
		if setter, ok := n.(errorStrategySetter); ok {
			setter.SetErrorStrategy(nodeData.ErrorStrategy)
			if nodeData.DefaultValue != nil {
				setter.SetDefaultValue(nodeData.DefaultValue)
			}
			if nodeData.Retry != nil {
				setter.SetRetryConfig(nodeData.Retry)
			}
		}
	}

	return n, nil
}
