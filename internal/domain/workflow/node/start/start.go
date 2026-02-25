package start

import (
	"context"
	"encoding/json"

	"flowweave/internal/domain/workflow/event"
	types "flowweave/internal/domain/workflow/model"
	"flowweave/internal/domain/workflow/node"
)

// StartNodeData Start 节点的配置数据
type StartNodeData struct {
	Type      string         `json:"type"`
	Title     string         `json:"title"`
	Variables []VariableDecl `json:"variables"`
}

// VariableDecl 变量声明
type VariableDecl struct {
	Variable string      `json:"variable"`
	Label    string      `json:"label"`
	Type     string      `json:"type"`
	Required bool        `json:"required"`
	Default  interface{} `json:"default,omitempty"`
}

// StartNode 工作流起始节点
type StartNode struct {
	*node.BaseNode
	data StartNodeData
}

func init() {
	node.Register(types.NodeTypeStart, NewStartNode)
}

// NewStartNode 创建 Start 节点
func NewStartNode(id string, rawData json.RawMessage) (node.Node, error) {
	var data StartNodeData
	if err := json.Unmarshal(rawData, &data); err != nil {
		return nil, err
	}

	n := &StartNode{
		BaseNode: node.NewBaseNode(id, types.NodeTypeStart, data.Title, types.NodeExecutionTypeRoot),
		data:     data,
	}
	return n, nil
}

// Run 执行 Start 节点
// Start 节点从变量池中读取输入参数并设置为自己的输出
func (n *StartNode) Run(ctx context.Context) (<-chan event.NodeEvent, error) {
	return node.RunWithEvents(ctx, n, func(ctx context.Context) (*node.NodeRunResult, error) {
		outputs := make(map[string]interface{})

		// 从变量池读取输入变量
		vp, ok := node.GetVariablePoolFromContext(ctx)
		if ok {
			for _, v := range n.data.Variables {
				val, exists := vp.GetVariable(types.VariableSelector{"sys", v.Variable})
				if exists {
					outputs[v.Variable] = val
				} else if v.Default != nil {
					outputs[v.Variable] = v.Default
				}
			}
		}

		return &node.NodeRunResult{
			Status:  types.NodeExecutionStatusSucceeded,
			Outputs: outputs,
		}, nil
	})
}
