package bootstrap

import (
	_ "flowweave/internal/adapter/function/builtin/azure_translate"
	// Built-in local functions registration
	_ "flowweave/internal/adapter/function/builtin/echo"
	_ "flowweave/internal/adapter/function/builtin/prime_judge"
	_ "flowweave/internal/adapter/function/builtin/summary_judge"
	_ "flowweave/internal/adapter/function/builtin/text_replace_beijing"
	_ "flowweave/internal/adapter/function/builtin/text_semantic_split"
)
