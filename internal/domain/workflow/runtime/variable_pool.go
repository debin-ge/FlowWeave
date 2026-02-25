package runtime

import (
	"fmt"
	"sync"

	types "flowweave/internal/domain/workflow/model"
)

// VariablePool 变量池，存储工作流执行期间所有节点的输入输出变量
// 变量通过 [node_id, variable_name] 格式的选择器引用
type VariablePool struct {
	mu        sync.RWMutex
	variables map[string]map[string]interface{} // node_id -> var_name -> value
	system    map[string]interface{}            // 系统变量
}

// NewVariablePool 创建新的变量池
func NewVariablePool() *VariablePool {
	return &VariablePool{
		variables: make(map[string]map[string]interface{}),
		system:    make(map[string]interface{}),
	}
}

// Get 通过变量选择器获取变量值
// selector: [node_id, variable_name]
func (vp *VariablePool) Get(selector types.VariableSelector) (interface{}, bool) {
	if len(selector) < 2 {
		return nil, false
	}

	nodeID := selector.NodeID()
	varName := selector.VarName()

	// 检查是否为系统变量
	if nodeID == "sys" {
		return vp.GetSystem(varName)
	}

	vp.mu.RLock()
	defer vp.mu.RUnlock()

	nodeVars, ok := vp.variables[nodeID]
	if !ok {
		return nil, false
	}

	val, ok := nodeVars[varName]
	return val, ok
}

// GetVariable 通过 node_id 和 variable_name 获取变量值
func (vp *VariablePool) GetVariable(selector types.VariableSelector) (interface{}, bool) {
	return vp.Get(selector)
}

// Set 设置节点变量
func (vp *VariablePool) Set(nodeID, varName string, value interface{}) {
	vp.mu.Lock()
	defer vp.mu.Unlock()

	if _, ok := vp.variables[nodeID]; !ok {
		vp.variables[nodeID] = make(map[string]interface{})
	}
	vp.variables[nodeID][varName] = value
}

// SetVariable 设置节点变量（实现 VariablePoolAccessor 接口）
func (vp *VariablePool) SetVariable(nodeID, varName string, value interface{}) {
	vp.Set(nodeID, varName, value)
}

// SetNodeOutputs 批量设置节点的所有输出变量
func (vp *VariablePool) SetNodeOutputs(nodeID string, outputs map[string]interface{}) {
	vp.mu.Lock()
	defer vp.mu.Unlock()

	if _, ok := vp.variables[nodeID]; !ok {
		vp.variables[nodeID] = make(map[string]interface{})
	}
	for k, v := range outputs {
		vp.variables[nodeID][k] = v
	}
}

// GetNodeOutputs 获取节点的所有输出变量
func (vp *VariablePool) GetNodeOutputs(nodeID string) (map[string]interface{}, bool) {
	vp.mu.RLock()
	defer vp.mu.RUnlock()

	nodeVars, ok := vp.variables[nodeID]
	if !ok {
		return nil, false
	}

	// 返回副本
	result := make(map[string]interface{}, len(nodeVars))
	for k, v := range nodeVars {
		result[k] = v
	}
	return result, true
}

// SetSystem 设置系统变量
func (vp *VariablePool) SetSystem(key string, value interface{}) {
	vp.mu.Lock()
	defer vp.mu.Unlock()
	vp.system[key] = value
}

// GetSystem 获取系统变量
func (vp *VariablePool) GetSystem(key string) (interface{}, bool) {
	vp.mu.RLock()
	defer vp.mu.RUnlock()
	val, ok := vp.system[key]
	return val, ok
}

// Dump 导出所有变量（用于快照）
func (vp *VariablePool) Dump() map[string]interface{} {
	vp.mu.RLock()
	defer vp.mu.RUnlock()

	result := make(map[string]interface{})
	for nodeID, vars := range vp.variables {
		nodeVarsCopy := make(map[string]interface{}, len(vars))
		for k, v := range vars {
			nodeVarsCopy[k] = v
		}
		result[nodeID] = nodeVarsCopy
	}
	result["__system__"] = vp.system
	return result
}

// ResolveTemplate 解析模板字符串中的变量引用
// 格式: {{#node_id.variable_name#}}
func (vp *VariablePool) ResolveTemplate(template string) string {
	// 简化实现：逐字符解析 {{# ... #}}
	result := []byte{}
	i := 0
	for i < len(template) {
		// 检查 {{# 开始标记
		if i+3 <= len(template) && template[i:i+3] == "{{#" {
			// 找到 #}} 结束标记
			end := i + 3
			for end+3 <= len(template) {
				if template[end:end+3] == "#}}" {
					// 解析变量引用
					ref := template[i+3 : end]
					val := vp.resolveRef(ref)
					result = append(result, []byte(val)...)
					i = end + 3
					goto continueOuter
				}
				end++
			}
		}
		result = append(result, template[i])
		i++
	continueOuter:
	}
	return string(result)
}

// resolveRef 解析类似 "node_id.variable_name" 的引用
func (vp *VariablePool) resolveRef(ref string) string {
	// 分割为 node_id 和 variable_name
	dotIdx := -1
	for i, c := range ref {
		if c == '.' {
			dotIdx = i
			break
		}
	}

	if dotIdx < 0 {
		return ""
	}

	nodeID := ref[:dotIdx]
	varName := ref[dotIdx+1:]

	val, ok := vp.Get(types.VariableSelector{nodeID, varName})
	if !ok {
		return ""
	}

	return fmt.Sprintf("%v", val)
}
