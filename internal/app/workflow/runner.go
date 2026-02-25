package workflow

import (
	applog "flowweave/internal/platform/log"
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"flowweave/internal/domain/memory"
	"flowweave/internal/domain/rag"
	"flowweave/internal/domain/workflow/engine"
	"flowweave/internal/domain/workflow/event"
	"flowweave/internal/domain/workflow/graph"
	types "flowweave/internal/domain/workflow/model"
	"flowweave/internal/domain/workflow/node"
	"flowweave/internal/domain/workflow/port"
	"flowweave/internal/domain/workflow/runtime"
	"flowweave/internal/tool"
	ragtool "flowweave/internal/tool/rag"

	// 自动注册所有节点
	_ "flowweave/internal/app/bootstrap"
)

// WorkflowRunner 工作流运行器，提供简化的执行入口
type WorkflowRunner struct {
	engineConfig *engine.Config
	memoryCoord  *memory.Coordinator
	retriever    *rag.Retriever
}

// NewWorkflowRunner 创建工作流运行器
func NewWorkflowRunner(config *engine.Config, memCoord *memory.Coordinator) *WorkflowRunner {
	if config == nil {
		config = engine.DefaultConfig()
	}
	return &WorkflowRunner{
		engineConfig: config,
		memoryCoord:  memCoord,
	}
}

// SetRetriever 设置 RAG Retriever（可选）
func (r *WorkflowRunner) SetRetriever(retriever *rag.Retriever) {
	r.retriever = retriever
}

// RunOptions 执行选项
type RunOptions struct {
	ConversationID string // 会话 ID（用于记忆管理）
	UserID         string // 用户 ID（预留长期记忆）
	OrgID          string // 组织 ID（用于 RAG 多租户隔离）
	TenantID       string // 租户 ID（用于 RAG 多租户隔离）
}

// RunResult 同步执行结果
type RunResult struct {
	Outputs        map[string]interface{} `json:"outputs,omitempty"`
	NodeExecutions []port.NodeExecution   `json:"node_executions,omitempty"`
}

// RunFromDSL 从 DSL JSON 执行工作流
func (r *WorkflowRunner) RunFromDSL(ctx context.Context, dslJSON []byte, inputs map[string]interface{}, opts *RunOptions) (<-chan event.GraphEvent, error) {
	// 1. 解析 DSL
	var config types.GraphConfig
	if err := json.Unmarshal(dslJSON, &config); err != nil {
		return nil, fmt.Errorf("failed to parse workflow DSL: %w", err)
	}

	return r.RunFromConfig(ctx, &config, inputs, opts)
}

// RunFromConfig 从 GraphConfig 执行工作流
func (r *WorkflowRunner) RunFromConfig(ctx context.Context, config *types.GraphConfig, inputs map[string]interface{}, opts *RunOptions) (<-chan event.GraphEvent, error) {
	// 1. 构建图
	factory := node.NewFactory()
	g, err := graph.Init(config, factory)
	if err != nil {
		return nil, fmt.Errorf("failed to build graph: %w", err)
	}

	// 2. 初始化运行时状态
	vp := runtime.NewVariablePool()

	// 设置系统变量（工作流输入）
	for k, v := range inputs {
		vp.SetSystem(k, v)
	}

	state := runtime.NewGraphRuntimeState(vp)

	// 3. 注入记忆管理到 context
	if r.memoryCoord != nil && opts != nil {
		ctx = memory.WithCoordinator(ctx, r.memoryCoord)
		if opts.ConversationID != "" {
			ctx = memory.WithConversationID(ctx, opts.ConversationID)
		}
		if opts.UserID != "" {
			ctx = memory.WithUserID(ctx, opts.UserID)
		}
	}

	// 4. 注入 RAG 多租户 scope（供 Agent Tool/RAG 使用）
	if opts != nil && (opts.OrgID != "" || opts.TenantID != "") {
		ctx = rag.WithScopeInfo(ctx, &rag.ScopeInfo{
			OrgID:    opts.OrgID,
			TenantID: opts.TenantID,
		})
	}

	// 5. 根据 DSL tools 配置注入工具注册表（Agent Tool Calling）
	toolNames := collectToolNamesFromDSL(config)
	if len(toolNames) > 0 {
		toolReg := tool.NewRegistry()
		registeredCount := 0
		for _, name := range toolNames {
			switch name {
			case "knowledge_search":
				if r.retriever == nil {
					applog.Warn("[WorkflowRunner] Tool requested but retriever is not configured",
						"tool", name,
					)
					continue
				}
				toolReg.Register(ragtool.NewRAGTool(r.retriever, nil))
				registeredCount++
			default:
				applog.Warn("[WorkflowRunner] Unknown tool requested in DSL, skip registration",
					"tool", name,
				)
			}
		}
		if registeredCount > 0 {
			ctx = tool.WithRegistry(ctx, toolReg)
		}
	}

	// 6. 创建引擎并执行
	eng := engine.New(g, state, r.engineConfig)
	return eng.Run(ctx), nil
}

// RunSync 同步执行工作流并返回结果（含节点执行明细）
func (r *WorkflowRunner) RunSync(ctx context.Context, dslJSON []byte, inputs map[string]interface{}, opts *RunOptions) (*RunResult, error) {
	eventCh, err := r.RunFromDSL(ctx, dslJSON, inputs, opts)
	if err != nil {
		return nil, err
	}

	result := &RunResult{}
	var lastError string

	for evt := range eventCh {
		switch evt.Type {
		case event.EventTypeGraphRunSucceeded:
			result.Outputs = evt.Outputs
			result.NodeExecutions = evt.NodeExecutions
		case event.EventTypeGraphRunFailed:
			lastError = evt.Error
			result.NodeExecutions = evt.NodeExecutions
		case event.EventTypeGraphRunAborted:
			lastError = evt.Error
			result.NodeExecutions = evt.NodeExecutions
		}
	}

	if lastError != "" {
		return result, fmt.Errorf("workflow failed: %s", lastError)
	}

	return result, nil
}

type dslToolBinding struct {
	Name string `json:"name"`
}

type dslNodeData struct {
	Tools []dslToolBinding `json:"tools,omitempty"`
}

func collectToolNamesFromDSL(config *types.GraphConfig) []string {
	if config == nil || len(config.Nodes) == 0 {
		return nil
	}

	namesSet := make(map[string]struct{})
	for _, n := range config.Nodes {
		var data dslNodeData
		if err := json.Unmarshal(n.Data, &data); err != nil {
			continue
		}

		for _, tb := range data.Tools {
			if tb.Name == "" {
				continue
			}
			namesSet[tb.Name] = struct{}{}
		}
	}

	if len(namesSet) == 0 {
		return nil
	}

	names := make([]string, 0, len(namesSet))
	for name := range namesSet {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
