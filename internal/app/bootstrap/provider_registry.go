package bootstrap

import (
	asrprovider "flowweave/internal/adapter/provider/asr"
	azureasr "flowweave/internal/adapter/provider/asr/azure"
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
	azureEndpoint string,
	azureRegion string,
	azureSubscriptionKey string,
	azureAPIVersion string,
	azureLocale string,
	azureHTTPTimeoutMS int,
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
		applog.Warn("⚠️  Tencent ASR credentials missing, tencent_flash provider not registered")
	} else {
		p, err := tencentasr.NewFlashProvider(tencentasr.Config{
			AppID:             appID,
			SecretID:          secretID,
			SecretKey:         secretKey,
			DefaultEngineType: engineType,
		})
		if err != nil {
			applog.Warnf("⚠️  Failed to init Tencent ASR provider: %v", err)
		} else {
			asrprovider.RegisterASRProvider(p)
			applog.Infof("✅ Registered ASR provider: %s", p.Name())
		}
	}

	if recSecretID == "" || recSecretKey == "" {
		applog.Warn("⚠️  Tencent RecTask credentials missing, async ASR provider not registered")
	} else {
		asyncProvider, err := tencentasr.NewRecTaskProvider(tencentasr.RecTaskConfig{
			SecretID:          recSecretID,
			SecretKey:         recSecretKey,
			Region:            recRegion,
			DefaultEngineType: recEngineModelType,
		})
		if err != nil {
			applog.Warnf("⚠️  Failed to init Tencent RecTask ASR provider: %v", err)
		} else {
			asrprovider.RegisterASRAsyncProvider(asyncProvider)
			applog.Infof("✅ Registered ASR async provider: %s (region: %s)", asyncProvider.Name(), recRegion)
		}
	}

	if azureSubscriptionKey == "" {
		applog.Warn("⚠️  Azure Speech subscription key missing, azure_batch provider not registered")
	} else {
		azProvider, err := azureasr.NewBatchProvider(azureasr.BatchConfig{
			Endpoint:        azureEndpoint,
			Region:          azureRegion,
			SubscriptionKey: azureSubscriptionKey,
			APIVersion:      azureAPIVersion,
			DefaultLocale:   azureLocale,
			HTTPTimeoutMS:   azureHTTPTimeoutMS,
		})
		if err != nil {
			applog.Warnf("⚠️  Failed to init Azure batch ASR provider: %v", err)
		} else {
			asrprovider.RegisterASRAsyncProvider(azProvider)
			applog.Infof("✅ Registered ASR async provider: %s", azProvider.Name())
		}
	}
}
