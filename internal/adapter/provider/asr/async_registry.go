package asr

import (
	"fmt"
	"sync"
)

type asrAsyncRegistry struct {
	mu        sync.RWMutex
	providers map[string]ASRAsyncProvider
}

var globalASRAsyncRegistry = &asrAsyncRegistry{providers: make(map[string]ASRAsyncProvider)}

func RegisterASRAsyncProvider(p ASRAsyncProvider) {
	if p == nil {
		return
	}
	globalASRAsyncRegistry.mu.Lock()
	defer globalASRAsyncRegistry.mu.Unlock()
	globalASRAsyncRegistry.providers[p.Name()] = p
}

func GetASRAsyncProvider(name string) (ASRAsyncProvider, error) {
	globalASRAsyncRegistry.mu.RLock()
	defer globalASRAsyncRegistry.mu.RUnlock()
	p, ok := globalASRAsyncRegistry.providers[name]
	if !ok {
		return nil, fmt.Errorf("ASR async provider not found: %s", name)
	}
	return p, nil
}
