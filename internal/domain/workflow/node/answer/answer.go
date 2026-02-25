package answer

import (
	"context"
	"encoding/json"
	"strings"

	"flowweave/internal/domain/workflow/event"
	types "flowweave/internal/domain/workflow/model"
	"flowweave/internal/domain/workflow/node"
)

// AnswerNodeData Answer 节点配置数据
type AnswerNodeData struct {
	Type   string `json:"type"`
	Title  string `json:"title"`
	Answer string `json:"answer"` // 支持变量引用模板 {{#node_id.var#}}
}

// AnswerNode 回答节点，支持流式输出
type AnswerNode struct {
	*node.BaseNode
	data AnswerNodeData
}

func init() {
	node.Register(types.NodeTypeAnswer, NewAnswerNode)
}

// NewAnswerNode 创建 Answer 节点
func NewAnswerNode(id string, rawData json.RawMessage) (node.Node, error) {
	var data AnswerNodeData
	if err := json.Unmarshal(rawData, &data); err != nil {
		return nil, err
	}

	n := &AnswerNode{
		BaseNode: node.NewBaseNode(id, types.NodeTypeAnswer, data.Title, types.NodeExecutionTypeResponse),
		data:     data,
	}
	return n, nil
}

// Run 执行 Answer 节点
func (n *AnswerNode) Run(ctx context.Context) (<-chan event.NodeEvent, error) {
	return node.RunStreamWithEvents(ctx, n, func(ctx context.Context, stream chan<- string) (*node.NodeRunResult, error) {
		answer := n.data.Answer

		// 解析模板中的变量引用
		vp, ok := node.GetVariablePoolFromContext(ctx)
		if ok {
			answer = resolveTemplate(answer, vp)
		}

		// 流式输出
		stream <- answer

		return &node.NodeRunResult{
			Status:  types.NodeExecutionStatusSucceeded,
			Outputs: map[string]interface{}{"answer": answer},
		}, nil
	})
}

// resolveTemplate 解析模板中的 {{#node_id.var_name#}} 引用
func resolveTemplate(template string, vp node.VariablePoolAccessor) string {
	var result strings.Builder
	i := 0
	for i < len(template) {
		if i+3 <= len(template) && template[i:i+3] == "{{#" {
			end := strings.Index(template[i+3:], "#}}")
			if end >= 0 {
				ref := template[i+3 : i+3+end]
				parts := strings.SplitN(ref, ".", 2)
				if len(parts) == 2 {
					val, ok := vp.GetVariable(types.VariableSelector(parts))
					if ok {
						result.WriteString(toString(val))
					}
				}
				i = i + 3 + end + 3
				continue
			}
		}
		result.WriteByte(template[i])
		i++
	}
	return result.String()
}

func toString(v interface{}) string {
	switch val := v.(type) {
	case string:
		return val
	default:
		b, _ := json.Marshal(val)
		return string(b)
	}
}
