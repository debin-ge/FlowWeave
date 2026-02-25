package ifelse

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"flowweave/internal/domain/workflow/event"
	types "flowweave/internal/domain/workflow/model"
	"flowweave/internal/domain/workflow/node"
)

// IfElseNodeData IF/ELSE 节点配置数据
type IfElseNodeData struct {
	Type       string      `json:"type"`
	Title      string      `json:"title"`
	Conditions []Condition `json:"conditions"`
}

// Condition 条件分支
type Condition struct {
	ID          string       `json:"id"`
	LogicalOp   string       `json:"logical_operator"` // "and" | "or"
	Comparisons []Comparison `json:"conditions"`
}

// Comparison 单个比较操作
type Comparison struct {
	VariableSelector types.VariableSelector `json:"variable_selector"`
	Operator         string                 `json:"comparison_operator"`
	Value            string                 `json:"value"`
}

// IfElseNode 条件分支节点
type IfElseNode struct {
	*node.BaseNode
	data IfElseNodeData
}

func init() {
	node.Register(types.NodeTypeIfElse, NewIfElseNode)
}

// NewIfElseNode 创建 IF/ELSE 节点
func NewIfElseNode(id string, rawData json.RawMessage) (node.Node, error) {
	var data IfElseNodeData
	if err := json.Unmarshal(rawData, &data); err != nil {
		return nil, err
	}

	n := &IfElseNode{
		BaseNode: node.NewBaseNode(id, types.NodeTypeIfElse, data.Title, types.NodeExecutionTypeBranch),
		data:     data,
	}
	return n, nil
}

// Run 执行 IF/ELSE 节点
func (n *IfElseNode) Run(ctx context.Context) (<-chan event.NodeEvent, error) {
	return node.RunWithEvents(ctx, n, func(ctx context.Context) (*node.NodeRunResult, error) {
		vp, _ := node.GetVariablePoolFromContext(ctx)

		// 评估每个条件分支
		for _, cond := range n.data.Conditions {
			matched := evaluateCondition(cond, vp)
			if matched {
				return &node.NodeRunResult{
					Status: types.NodeExecutionStatusSucceeded,
					Outputs: map[string]interface{}{
						"__branch__": cond.ID,
						"result":     true,
					},
				}, nil
			}
		}

		// 没有任何条件匹配，走 else 分支
		return &node.NodeRunResult{
			Status: types.NodeExecutionStatusSucceeded,
			Outputs: map[string]interface{}{
				"__branch__": "false",
				"result":     false,
			},
		}, nil
	})
}

// evaluateCondition 评估一个条件分支
func evaluateCondition(cond Condition, vp node.VariablePoolAccessor) bool {
	if len(cond.Comparisons) == 0 {
		return false
	}

	isAnd := strings.ToLower(cond.LogicalOp) == "and"

	for _, comp := range cond.Comparisons {
		matched := evaluateComparison(comp, vp)
		if isAnd && !matched {
			return false
		}
		if !isAnd && matched { // OR
			return true
		}
	}

	return isAnd
}

// evaluateComparison 评估单个比较操作
func evaluateComparison(comp Comparison, vp node.VariablePoolAccessor) bool {
	if vp == nil {
		return false
	}

	val, ok := vp.GetVariable(comp.VariableSelector)
	if !ok {
		// 变量不存在时根据操作符特殊处理
		switch comp.Operator {
		case "is-empty", "is-null", "not-exist":
			return true
		case "is-not-empty", "is-not-null", "exist":
			return false
		default:
			return false
		}
	}

	strVal := fmt.Sprintf("%v", val)
	expected := comp.Value

	switch comp.Operator {
	// 字符串比较
	case "contains":
		return strings.Contains(strVal, expected)
	case "not-contains":
		return !strings.Contains(strVal, expected)
	case "start-with":
		return strings.HasPrefix(strVal, expected)
	case "end-with":
		return strings.HasSuffix(strVal, expected)
	case "is", "equal":
		return strVal == expected
	case "is-not", "not-equal":
		return strVal != expected
	case "is-empty":
		return strVal == ""
	case "is-not-empty":
		return strVal != ""

	// 存在性检查
	case "is-null", "not-exist":
		return val == nil
	case "is-not-null", "exist":
		return val != nil

	// 数值比较
	case "gt", ">":
		return toFloat(val) > toFloat(expected)
	case "lt", "<":
		return toFloat(val) < toFloat(expected)
	case "gte", "ge", ">=":
		return toFloat(val) >= toFloat(expected)
	case "lte", "le", "<=":
		return toFloat(val) <= toFloat(expected)
	case "eq", "==":
		return toFloat(val) == toFloat(expected)
	case "ne", "!=":
		return toFloat(val) != toFloat(expected)

	default:
		return false
	}
}

func toFloat(v interface{}) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case float32:
		return float64(val)
	case int:
		return float64(val)
	case int64:
		return float64(val)
	case string:
		var f float64
		fmt.Sscanf(val, "%f", &f)
		return f
	default:
		return 0
	}
}
