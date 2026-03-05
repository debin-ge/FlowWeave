package bootstrap

import (
	"flowweave/internal/adapter/provider/llm/openai"
	applog "flowweave/internal/platform/log"
	"flowweave/internal/provider"
	tencentasr "flowweave/internal/provider/asr/tencent"
)

// RegisterLLMProviders registers configured LLM providers.
func RegisterLLMProviders(apiKey, baseURL string, connectTimeoutSeconds, tlsHandshakeTimeoutSeconds int) {
	if apiKey == "" {
		applog.Warn("⚠️  No OPENAI_API_KEY set, LLM nodes will not work")
		return
	}

	p := openai.New(openai.Config{
		APIKey:                     apiKey,
		BaseURL:                    baseURL,
		ConnectTimeoutSeconds:      connectTimeoutSeconds,
		TLSHandshakeTimeoutSeconds: tlsHandshakeTimeoutSeconds,
	})
	provider.RegisterProvider(p)
	applog.Infof("✅ Registered LLM provider: %s (base: %s)", p.Name(), baseURL)
}

// RegisterASRProviders registers configured ASR providers and runtime limits.
func RegisterASRProviders(
	tempDir string,
	maxAudioMB int,
	maxBase64Chars int,
	urlFetchTimeoutMS int,
	appID string,
	secretID string,
	secretKey string,
	engineType string,
) {
	provider.SetASRRuntimeConfig(provider.ASRRuntimeConfig{
		TempDir:           tempDir,
		MaxAudioMB:        maxAudioMB,
		MaxBase64Chars:    maxBase64Chars,
		URLFetchTimeoutMS: urlFetchTimeoutMS,
	})

	if appID == "" || secretID == "" || secretKey == "" {
		applog.Warn("⚠️  Tencent ASR credentials missing, ASR nodes will not work")
		return
	}

	p, err := tencentasr.NewFlashProvider(tencentasr.Config{
		AppID:             appID,
		SecretID:          secretID,
		SecretKey:         secretKey,
		DefaultEngineType: engineType,
	})
	if err != nil {
		applog.Warnf("⚠️  Failed to init Tencent ASR provider: %v", err)
		return
	}
	provider.RegisterASRProvider(p)
	applog.Infof("✅ Registered ASR provider: %s", p.Name())
}
