package iteration

import (
	"context"
	"encoding/json"
	"fmt"

	"flowweave/internal/domain/workflow/event"
	types "flowweave/internal/domain/workflow/model"
	"flowweave/internal/domain/workflow/node"
)

func init() {
	node.Register(types.NodeTypeIteration, NewIterationNode)
	node.Register(types.NodeTypeIterationStart, NewIterationStartNode)
}

// IterationConfig 迭代节点配置
type IterationConfig struct {
	Type          string                 `json:"type"`
	Title         string                 `json:"title"`
	IteratorVar   types.VariableSelector `json:"iterator"`                 // 待迭代的列表变量引用
	OutputVar     string                 `json:"output_variable"`          // 聚合输出的变量名
	MaxIterations int                    `json:"max_iterations,omitempty"` // 最大迭代次数（安全阀）
}

// IterationNode 迭代节点 —— 对列表中的每个元素重复执行下游子图
// 简化实现：在当前节点内展开迭代，将每个元素及其索引写入变量池
type IterationNode struct {
	*node.BaseNode
	config IterationConfig
}

// NewIterationNode 创建迭代节点
func NewIterationNode(id string, data json.RawMessage) (node.Node, error) {
	var config IterationConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse iteration node config: %w", err)
	}

	if config.MaxIterations <= 0 {
		config.MaxIterations = 100
	}
	if config.OutputVar == "" {
		config.OutputVar = "items"
	}

	n := &IterationNode{
		BaseNode: node.NewBaseNode(id, types.NodeTypeIteration, config.Title, types.NodeExecutionTypeContainer),
		config:   config,
	}
	return n, nil
}

// Run 执行迭代
func (n *IterationNode) Run(ctx context.Context) (<-chan event.NodeEvent, error) {
	return node.RunWithEvents(ctx, n, func(ctx context.Context) (*node.NodeRunResult, error) {
		// 获取变量池
		vp, ok := node.GetVariablePoolFromContext(ctx)
		if !ok {
			return nil, fmt.Errorf("variable pool not found in context")
		}

		// 获取待迭代的列表
		listValue, ok := vp.GetVariable(n.config.IteratorVar)
		if !ok {
			return &node.NodeRunResult{
				Status:  types.NodeExecutionStatusSucceeded,
				Outputs: map[string]interface{}{n.config.OutputVar: []interface{}{}},
				Metadata: map[string]interface{}{
					"iteration_count": 0,
					"reason":          "iterator variable not found",
				},
			}, nil
		}

		// 转换为列表
		list, err := toSlice(listValue)
		if err != nil {
			return nil, fmt.Errorf("iterator value is not iterable: %w", err)
		}

		// 限制迭代次数
		iterCount := len(list)
		if iterCount > n.config.MaxIterations {
			iterCount = n.config.MaxIterations
		}

		// 执行迭代：将每个元素及索引写入变量池
		results := make([]interface{}, 0, iterCount)
		for i := 0; i < iterCount; i++ {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
			}

			// 写入当前迭代变量
			vp.SetVariable(n.ID(), "index", i)
			vp.SetVariable(n.ID(), "item", list[i])
			vp.SetVariable(n.ID(), "is_last", i == iterCount-1)

			results = append(results, list[i])
		}

		return &node.NodeRunResult{
			Status: types.NodeExecutionStatusSucceeded,
			Outputs: map[string]interface{}{
				n.config.OutputVar: results,
				"index":            iterCount - 1,
				"count":            iterCount,
			},
			Metadata: map[string]interface{}{
				"iteration_count": iterCount,
			},
		}, nil
	})
}

// --- Iteration Start Node ---

// IterationStartNode 迭代起始节点（容器内部入口）
type IterationStartNode struct {
	*node.BaseNode
}

// NewIterationStartNode 创建迭代起始节点
func NewIterationStartNode(id string, data json.RawMessage) (node.Node, error) {
	var nd types.NodeData
	if err := json.Unmarshal(data, &nd); err != nil {
		return nil, err
	}
	return &IterationStartNode{
		BaseNode: node.NewBaseNode(id, types.NodeTypeIterationStart, nd.Title, types.NodeExecutionTypeRoot),
	}, nil
}

// Run 直接传递
func (n *IterationStartNode) Run(ctx context.Context) (<-chan event.NodeEvent, error) {
	return node.RunWithEvents(ctx, n, func(ctx context.Context) (*node.NodeRunResult, error) {
		return &node.NodeRunResult{
			Status:  types.NodeExecutionStatusSucceeded,
			Outputs: map[string]interface{}{},
		}, nil
	})
}

// --- 辅助函数 ---

// toSlice 将任意值转换为 []interface{}
func toSlice(v interface{}) ([]interface{}, error) {
	switch val := v.(type) {
	case []interface{}:
		return val, nil
	case []string:
		result := make([]interface{}, len(val))
		for i, s := range val {
			result[i] = s
		}
		return result, nil
	case []int:
		result := make([]interface{}, len(val))
		for i, n := range val {
			result[i] = n
		}
		return result, nil
	case []float64:
		result := make([]interface{}, len(val))
		for i, n := range val {
			result[i] = n
		}
		return result, nil
	case []map[string]interface{}:
		result := make([]interface{}, len(val))
		for i, m := range val {
			result[i] = m
		}
		return result, nil
	default:
		return nil, fmt.Errorf("cannot convert %T to slice", v)
	}
}
