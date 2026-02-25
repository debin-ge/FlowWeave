package bootstrap

import (
	"flowweave/internal/adapter/provider/llm/openai"
	applog "flowweave/internal/platform/log"
	"flowweave/internal/provider"
)

// RegisterLLMProviders registers configured LLM providers.
func RegisterLLMProviders(apiKey, baseURL string) {
	if apiKey == "" {
		applog.Warn("⚠️  No OPENAI_API_KEY set, LLM nodes will not work")
		return
	}

	p := openai.New(openai.Config{
		APIKey:  apiKey,
		BaseURL: baseURL,
	})
	provider.RegisterProvider(p)
	applog.Infof("✅ Registered LLM provider: %s (base: %s)", p.Name(), baseURL)
}
