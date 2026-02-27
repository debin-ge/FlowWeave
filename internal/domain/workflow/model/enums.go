package types

// NodeType 表示工作流中节点的类型
type NodeType string

const (
	NodeTypeStart              NodeType = "start"
	NodeTypeEnd                NodeType = "end"
	NodeTypeAnswer             NodeType = "answer"
	NodeTypeLLM                NodeType = "llm"
	NodeTypeIfElse             NodeType = "if-else"
	NodeTypeFunc               NodeType = "func"
	NodeTypeTemplateTransform  NodeType = "template-transform"
	NodeTypeQuestionClassifier NodeType = "question-classifier"
	NodeTypeHTTPRequest        NodeType = "http-request"
	NodeTypeTool               NodeType = "tool"
	NodeTypeVariableAggregator NodeType = "variable-aggregator"
	NodeTypeIteration          NodeType = "iteration"
	NodeTypeIterationStart     NodeType = "iteration-start"
	NodeTypeLoop               NodeType = "loop"
	NodeTypeLoopStart          NodeType = "loop-start"
	NodeTypeLoopEnd            NodeType = "loop-end"
	NodeTypeParameterExtractor NodeType = "parameter-extractor"
	NodeTypeVariableAssigner   NodeType = "assigner"
	NodeTypeDocumentExtractor  NodeType = "document-extractor"
	NodeTypeListOperator       NodeType = "list-operator"
	NodeTypeAgent              NodeType = "agent"
	NodeTypeHumanInput         NodeType = "human-input"
)

// IsStartNode 判断节点类型是否可以作为工作流的入口点
func (nt NodeType) IsStartNode() bool {
	return nt == NodeTypeStart
}

// NodeState 表示节点或边在工作流执行期间的状态
type NodeState string

const (
	NodeStateUnknown NodeState = "unknown"
	NodeStateTaken   NodeState = "taken"
	NodeStateSkipped NodeState = "skipped"
)

// NodeExecutionType 节点的执行类型分类
type NodeExecutionType string

const (
	// NodeExecutionTypeExecutable 常规执行节点，产出输出
	NodeExecutionTypeExecutable NodeExecutionType = "executable"
	// NodeExecutionTypeResponse 响应节点，流式输出 (Answer, End)
	NodeExecutionTypeResponse NodeExecutionType = "response"
	// NodeExecutionTypeBranch 分支节点，选择不同路径 (if-else, question-classifier)
	NodeExecutionTypeBranch NodeExecutionType = "branch"
	// NodeExecutionTypeContainer 容器节点，管理子图 (iteration, loop)
	NodeExecutionTypeContainer NodeExecutionType = "container"
	// NodeExecutionTypeRoot 根节点，作为执行入口 (start)
	NodeExecutionTypeRoot NodeExecutionType = "root"
)

// ErrorStrategy 节点错误处理策略
type ErrorStrategy string

const (
	ErrorStrategyNone         ErrorStrategy = ""
	ErrorStrategyFailBranch   ErrorStrategy = "fail-branch"
	ErrorStrategyDefaultValue ErrorStrategy = "default-value"
	ErrorStrategyRetry        ErrorStrategy = "retry"
)

// CommandType 引擎命令类型
type CommandType string

const (
	CommandAbort  CommandType = "abort"
	CommandPause  CommandType = "pause"
	CommandResume CommandType = "resume"
)

// Command 引擎控制命令
type Command struct {
	Type   CommandType
	Reason string
}

// FailBranchSourceHandle 失败分支的 source handle 标识
type FailBranchSourceHandle string

const (
	FailBranchSourceHandleFailed  FailBranchSourceHandle = "fail-branch"
	FailBranchSourceHandleSuccess FailBranchSourceHandle = "success-branch"
)

// WorkflowType 工作流类型
type WorkflowType string

const (
	WorkflowTypeWorkflow WorkflowType = "workflow"
	WorkflowTypeChat     WorkflowType = "chat"
)

// WorkflowExecutionStatus 工作流执行状态
type WorkflowExecutionStatus string

const (
	WorkflowExecutionStatusScheduled        WorkflowExecutionStatus = "scheduled"
	WorkflowExecutionStatusRunning          WorkflowExecutionStatus = "running"
	WorkflowExecutionStatusSucceeded        WorkflowExecutionStatus = "succeeded"
	WorkflowExecutionStatusFailed           WorkflowExecutionStatus = "failed"
	WorkflowExecutionStatusStopped          WorkflowExecutionStatus = "stopped"
	WorkflowExecutionStatusPartialSucceeded WorkflowExecutionStatus = "partial-succeeded"
	WorkflowExecutionStatusPaused           WorkflowExecutionStatus = "paused"
)

// IsEnded 判断执行状态是否已结束
func (s WorkflowExecutionStatus) IsEnded() bool {
	switch s {
	case WorkflowExecutionStatusSucceeded,
		WorkflowExecutionStatusFailed,
		WorkflowExecutionStatusPartialSucceeded,
		WorkflowExecutionStatusStopped:
		return true
	default:
		return false
	}
}

// NodeExecutionStatus 节点执行状态
type NodeExecutionStatus string

const (
	NodeExecutionStatusPending   NodeExecutionStatus = "pending"
	NodeExecutionStatusRunning   NodeExecutionStatus = "running"
	NodeExecutionStatusSucceeded NodeExecutionStatus = "succeeded"
	NodeExecutionStatusFailed    NodeExecutionStatus = "failed"
	NodeExecutionStatusException NodeExecutionStatus = "exception"
	NodeExecutionStatusStopped   NodeExecutionStatus = "stopped"
	NodeExecutionStatusPaused    NodeExecutionStatus = "paused"
)
