package bootstrap

import (
	// 核心节点注册
	_ "flowweave/internal/domain/workflow/node/answer"
	_ "flowweave/internal/domain/workflow/node/code"
	_ "flowweave/internal/domain/workflow/node/end"
	_ "flowweave/internal/domain/workflow/node/httprequest"
	_ "flowweave/internal/domain/workflow/node/ifelse"
	_ "flowweave/internal/domain/workflow/node/iteration"
	_ "flowweave/internal/domain/workflow/node/llm"
	_ "flowweave/internal/domain/workflow/node/start"
	_ "flowweave/internal/domain/workflow/node/template"
)
