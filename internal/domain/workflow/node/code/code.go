package code

import (
	"context"
	"encoding/json"
	"fmt"

	"flowweave/internal/domain/workflow/event"
	types "flowweave/internal/domain/workflow/model"
	"flowweave/internal/domain/workflow/node"
)

// CodeNodeData 代码执行节点配置
type CodeNodeData struct {
	Type         string               `json:"type"`
	Title        string               `json:"title"`
	CodeLanguage string               `json:"code_language"` // javascript, python3
	Code         string               `json:"code"`
	Variables    []InputVariable      `json:"variables"`
	Outputs      map[string]OutputDef `json:"outputs"`
}

// InputVariable 输入变量绑定
type InputVariable struct {
	Variable      string                 `json:"variable"`
	ValueSelector types.VariableSelector `json:"value_selector"`
}

// OutputDef 输出变量定义
type OutputDef struct {
	Type string `json:"type"` // string, number, object, array[string], etc.
}

// CodeExecutor 代码执行器接口
// 具体实现可以是嵌入式 JS (goja) 或外部沙箱
type CodeExecutor interface {
	Execute(ctx context.Context, language, code string, inputs map[string]interface{}) (map[string]interface{}, error)
}

// defaultExecutor 默认代码执行器（简单 JSON 映射，用于开发/测试）
type defaultExecutor struct{}

func (e *defaultExecutor) Execute(ctx context.Context, language, code string, inputs map[string]interface{}) (map[string]interface{}, error) {
	// 默认实现：将输入直接作为输出传递
	// 生产环境应替换为 goja (JS) 或 Docker 沙箱 (Python)
	return map[string]interface{}{
		"result": fmt.Sprintf("[code:%s] executed with %d inputs", language, len(inputs)),
	}, nil
}

var globalCodeExecutor CodeExecutor = &defaultExecutor{}

// SetCodeExecutor 设置全局代码执行器
func SetCodeExecutor(executor CodeExecutor) {
	globalCodeExecutor = executor
}

// GetCodeExecutor 获取当前代码执行器
func GetCodeExecutor() CodeExecutor {
	return globalCodeExecutor
}

// CodeNode 代码执行节点
type CodeNode struct {
	*node.BaseNode
	data CodeNodeData
}

func init() {
	node.Register(types.NodeTypeCode, NewCodeNode)
}

// NewCodeNode 创建 Code 节点
func NewCodeNode(id string, rawData json.RawMessage) (node.Node, error) {
	var data CodeNodeData
	if err := json.Unmarshal(rawData, &data); err != nil {
		return nil, err
	}

	n := &CodeNode{
		BaseNode: node.NewBaseNode(id, types.NodeTypeCode, data.Title, types.NodeExecutionTypeExecutable),
		data:     data,
	}
	return n, nil
}

// Run 执行 Code 节点
func (n *CodeNode) Run(ctx context.Context) (<-chan event.NodeEvent, error) {
	return node.RunWithEvents(ctx, n, func(ctx context.Context) (*node.NodeRunResult, error) {
		// 1. 从变量池收集输入
		inputs := make(map[string]interface{})
		vp, ok := node.GetVariablePoolFromContext(ctx)
		if ok {
			for _, v := range n.data.Variables {
				val, exists := vp.GetVariable(v.ValueSelector)
				if exists {
					inputs[v.Variable] = val
				}
			}
		}

		// 2. 执行代码
		outputs, err := globalCodeExecutor.Execute(ctx, n.data.CodeLanguage, n.data.Code, inputs)
		if err != nil {
			return &node.NodeRunResult{
				Status: types.NodeExecutionStatusFailed,
				Error:  fmt.Sprintf("code execution failed: %s", err.Error()),
			}, nil
		}

		return &node.NodeRunResult{
			Status:  types.NodeExecutionStatusSucceeded,
			Outputs: outputs,
			Metadata: map[string]interface{}{
				"language": n.data.CodeLanguage,
			},
		}, nil
	})
}
