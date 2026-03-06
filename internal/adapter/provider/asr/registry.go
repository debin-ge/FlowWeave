package asr

import (
	"fmt"
	"sync"
)

type asrRegistry struct {
	mu        sync.RWMutex
	providers map[string]ASRProvider
}

var globalASRRegistry = &asrRegistry{providers: make(map[string]ASRProvider)}

// RegisterASRProvider registers an ASR provider.
func RegisterASRProvider(p ASRProvider) {
	if p == nil {
		return
	}
	globalASRRegistry.mu.Lock()
	defer globalASRRegistry.mu.Unlock()
	globalASRRegistry.providers[p.Name()] = p
}

// GetASRProvider looks up provider by name.
func GetASRProvider(name string) (ASRProvider, error) {
	globalASRRegistry.mu.RLock()
	defer globalASRRegistry.mu.RUnlock()
	p, ok := globalASRRegistry.providers[name]
	if !ok {
		return nil, fmt.Errorf("ASR provider not found: %s", name)
	}
	return p, nil
}

// HasASRProvider checks whether a sync ASR provider is registered.
func HasASRProvider(name string) bool {
	globalASRRegistry.mu.RLock()
	defer globalASRRegistry.mu.RUnlock()
	_, ok := globalASRRegistry.providers[name]
	return ok
}
