package template

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"flowweave/internal/domain/workflow/event"
	types "flowweave/internal/domain/workflow/model"
	"flowweave/internal/domain/workflow/node"
)

// TemplateNodeData 模板转换节点配置
type TemplateNodeData struct {
	Type      string          `json:"type"`
	Title     string          `json:"title"`
	Template  string          `json:"template"` // Jinja2 风格模板（简化实现）
	Variables []InputVariable `json:"variables"`
}

// InputVariable 输入变量绑定
type InputVariable struct {
	Variable      string                 `json:"variable"`
	ValueSelector types.VariableSelector `json:"value_selector"`
}

// TemplateNode 模板转换节点
type TemplateNode struct {
	*node.BaseNode
	data TemplateNodeData
}

func init() {
	node.Register(types.NodeTypeTemplateTransform, NewTemplateNode)
}

// NewTemplateNode 创建 Template 节点
func NewTemplateNode(id string, rawData json.RawMessage) (node.Node, error) {
	var data TemplateNodeData
	if err := json.Unmarshal(rawData, &data); err != nil {
		return nil, err
	}

	n := &TemplateNode{
		BaseNode: node.NewBaseNode(id, types.NodeTypeTemplateTransform, data.Title, types.NodeExecutionTypeExecutable),
		data:     data,
	}
	return n, nil
}

// Run 执行模板转换
func (n *TemplateNode) Run(ctx context.Context) (<-chan event.NodeEvent, error) {
	return node.RunWithEvents(ctx, n, func(ctx context.Context) (*node.NodeRunResult, error) {
		// 1. 从变量池收集输入变量
		vars := make(map[string]interface{})
		vp, ok := node.GetVariablePoolFromContext(ctx)
		if ok {
			for _, v := range n.data.Variables {
				val, exists := vp.GetVariable(v.ValueSelector)
				if exists {
					vars[v.Variable] = val
				}
			}
		}

		// 2. 渲染模板
		output := renderTemplate(n.data.Template, vars)

		return &node.NodeRunResult{
			Status: types.NodeExecutionStatusSucceeded,
			Outputs: map[string]interface{}{
				"output": output,
			},
		}, nil
	})
}

// renderTemplate 简化版模板渲染
// 支持:
//   - {{ variable }}  — 变量替换
//   - {{#node_id.var_name#}} — 变量池引用（带 VariablePoolAccessor）
func renderTemplate(tmpl string, vars map[string]interface{}) string {
	var result strings.Builder
	i := 0
	for i < len(tmpl) {
		// 检查 {{ 开始标记
		if i+2 <= len(tmpl) && tmpl[i:i+2] == "{{" {
			// 区分 {{# (变量池引用) 和 {{ (局部变量)
			if i+3 <= len(tmpl) && tmpl[i+2] == '#' {
				// {{#...#}} 格式由调用方处理，这里跳过
				end := strings.Index(tmpl[i+3:], "#}}")
				if end >= 0 {
					// 原样保留（或作为变量池引用）
					result.WriteString(tmpl[i : i+3+end+3])
					i = i + 3 + end + 3
					continue
				}
			}

			// {{ variable }} 格式
			end := strings.Index(tmpl[i+2:], "}}")
			if end >= 0 {
				varName := strings.TrimSpace(tmpl[i+2 : i+2+end])
				if val, ok := vars[varName]; ok {
					result.WriteString(toString(val))
				}
				i = i + 2 + end + 2
				continue
			}
		}
		result.WriteByte(tmpl[i])
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

// RenderWithVariablePool 使用变量池渲染模板（同时支持局部变量和变量池引用）
func RenderWithVariablePool(tmpl string, localVars map[string]interface{}, vp node.VariablePoolAccessor) string {
	// 先处理 {{#...#}} 变量池引用
	var step1 strings.Builder
	i := 0
	for i < len(tmpl) {
		if i+3 <= len(tmpl) && tmpl[i:i+3] == "{{#" {
			end := strings.Index(tmpl[i+3:], "#}}")
			if end >= 0 {
				ref := tmpl[i+3 : i+3+end]
				parts := strings.SplitN(ref, ".", 2)
				if len(parts) == 2 && vp != nil {
					val, ok := vp.GetVariable(types.VariableSelector(parts))
					if ok {
						step1.WriteString(fmt.Sprintf("%v", val))
					}
				}
				i = i + 3 + end + 3
				continue
			}
		}
		step1.WriteByte(tmpl[i])
		i++
	}

	// 再处理 {{ }} 局部变量
	return renderTemplate(step1.String(), localVars)
}
