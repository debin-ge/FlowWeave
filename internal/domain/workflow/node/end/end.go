package end

import (
	"context"
	"encoding/json"

	"flowweave/internal/domain/workflow/event"
	types "flowweave/internal/domain/workflow/model"
	"flowweave/internal/domain/workflow/node"
)

// EndNodeData End 节点配置数据
type EndNodeData struct {
	Type    string           `json:"type"`
	Title   string           `json:"title"`
	Outputs []OutputVariable `json:"outputs"`
}

// OutputVariable 输出变量定义
type OutputVariable struct {
	Variable      string                 `json:"variable"`
	ValueSelector types.VariableSelector `json:"value_selector"`
}

// EndNode 工作流结束节点
type EndNode struct {
	*node.BaseNode
	data EndNodeData
}

func init() {
	node.Register(types.NodeTypeEnd, NewEndNode)
}

// NewEndNode 创建 End 节点
func NewEndNode(id string, rawData json.RawMessage) (node.Node, error) {
	var data EndNodeData
	if err := json.Unmarshal(rawData, &data); err != nil {
		return nil, err
	}

	n := &EndNode{
		BaseNode: node.NewBaseNode(id, types.NodeTypeEnd, data.Title, types.NodeExecutionTypeResponse),
		data:     data,
	}
	return n, nil
}

// Run 执行 End 节点
// End 节点从变量池中收集所有声明的输出变量
func (n *EndNode) Run(ctx context.Context) (<-chan event.NodeEvent, error) {
	return node.RunWithEvents(ctx, n, func(ctx context.Context) (*node.NodeRunResult, error) {
		outputs := make(map[string]interface{})

		vp, ok := node.GetVariablePoolFromContext(ctx)
		if ok {
			for _, out := range n.data.Outputs {
				val, exists := vp.GetVariable(out.ValueSelector)
				if exists {
					outputs[out.Variable] = val
				}
			}
		}

		return &node.NodeRunResult{
			Status:  types.NodeExecutionStatusSucceeded,
			Outputs: outputs,
		}, nil
	})
}
