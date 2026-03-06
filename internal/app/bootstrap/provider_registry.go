package bootstrap

import (
	asrprovider "flowweave/internal/adapter/provider/asr"
	tencentasr "flowweave/internal/adapter/provider/asr/tencent"
	"flowweave/internal/adapter/provider/llm"
	"flowweave/internal/adapter/provider/llm/openai"
	applog "flowweave/internal/platform/log"
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
	asyncCallbackBaseURL string,
	asyncPollIntervalMS int,
	asyncWaitTimeoutMS int,
	appID string,
	secretID string,
	secretKey string,
	engineType string,
	recSecretID string,
	recSecretKey string,
	recRegion string,
	recEngineModelType string,
) {
	asrprovider.SetASRRuntimeConfig(asrprovider.ASRRuntimeConfig{
		TempDir:           tempDir,
		MaxAudioMB:        maxAudioMB,
		MaxBase64Chars:    maxBase64Chars,
		URLFetchTimeoutMS: urlFetchTimeoutMS,
	})
	asrprovider.SetASRAsyncRuntimeConfig(asrprovider.ASRAsyncRuntimeConfig{
		CallbackBaseURL:       asyncCallbackBaseURL,
		DefaultPollIntervalMS: asyncPollIntervalMS,
		DefaultWaitTimeoutMS:  asyncWaitTimeoutMS,
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
	asrprovider.RegisterASRProvider(p)
	applog.Infof("✅ Registered ASR provider: %s", p.Name())

	if recSecretID == "" || recSecretKey == "" {
		applog.Warn("⚠️  Tencent RecTask credentials missing, async ASR provider not registered")
		return
	}

	asyncProvider, err := tencentasr.NewRecTaskProvider(tencentasr.RecTaskConfig{
		SecretID:          recSecretID,
		SecretKey:         recSecretKey,
		Region:            recRegion,
		DefaultEngineType: recEngineModelType,
	})
	if err != nil {
		applog.Warnf("⚠️  Failed to init Tencent RecTask ASR provider: %v", err)
		return
	}
	asrprovider.RegisterASRAsyncProvider(asyncProvider)
	applog.Infof("✅ Registered ASR async provider: %s (region: %s)", asyncProvider.Name(), recRegion)
}
