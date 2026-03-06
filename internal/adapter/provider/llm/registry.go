package provider

import (
	"fmt"
	"sync"
)

// Registry LLM 供应商注册表
type Registry struct {
	mu        sync.RWMutex
	providers map[string]LLMProvider
}

var globalProviderRegistry = &Registry{
	providers: make(map[string]LLMProvider),
}

// RegisterProvider 注册 LLM 供应商
func RegisterProvider(provider LLMProvider) {
	globalProviderRegistry.mu.Lock()
	defer globalProviderRegistry.mu.Unlock()
	globalProviderRegistry.providers[provider.Name()] = provider
}

// GetProvider 获取 LLM 供应商
func GetProvider(name string) (LLMProvider, error) {
	globalProviderRegistry.mu.RLock()
	defer globalProviderRegistry.mu.RUnlock()
	p, ok := globalProviderRegistry.providers[name]
	if !ok {
		return nil, fmt.Errorf("LLM provider not found: %s", name)
	}
	return p, nil
}

// ListProviders 列出所有供应商
func ListProviders() []string {
	globalProviderRegistry.mu.RLock()
	defer globalProviderRegistry.mu.RUnlock()
	names := make([]string, 0, len(globalProviderRegistry.providers))
	for name := range globalProviderRegistry.providers {
		names = append(names, name)
	}
	return names
}
