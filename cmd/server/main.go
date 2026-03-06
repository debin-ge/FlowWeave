package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/lib/pq"
	goredis "github.com/redis/go-redis/v9"

	"flowweave/internal/api"
	"flowweave/internal/app/bootstrap"
	"flowweave/internal/app/workflow"
	"flowweave/internal/db/opensearch"
	"flowweave/internal/db/postgres"
	redisdb "flowweave/internal/db/redis"
	"flowweave/internal/domain/memory"
	"flowweave/internal/domain/rag"
	"flowweave/internal/domain/workflow/engine"
	"flowweave/internal/domain/workflow/port"
	"flowweave/internal/platform/config"
	applog "flowweave/internal/platform/log"
)

func main() {
	appCtx, appCancel := context.WithCancel(context.Background())
	defer appCancel()

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Config load failed: %v\n", err)
		os.Exit(1)
	}

	applog.Init(applog.Config{
		Level:  cfg.LogLevel,
		Format: cfg.LogFormat,
	})

	db, err := sql.Open("postgres", cfg.Database.URL)
	if err != nil {
		applog.Fatalf("❌ Failed to connect to database: %v", err)
	}
	defer db.Close()

	db.SetMaxOpenConns(cfg.Database.MaxOpenConns)
	db.SetMaxIdleConns(cfg.Database.MaxIdleConns)
	db.SetConnMaxLifetime(time.Duration(cfg.Database.ConnMaxLifetimeSeconds) * time.Second)

	if err := db.Ping(); err != nil {
		applog.Fatalf("❌ Failed to ping database: %v", err)
	}
	applog.Info("✅ Connected to PostgreSQL")

	var repo port.Repository
	pgRepo := postgres.NewRepository(db)
	repo = pgRepo

	migrateCtx, migrateCancel := context.WithTimeout(context.Background(), time.Duration(cfg.Runtime.MigrationTimeoutSeconds)*time.Second)
	defer migrateCancel()
	if err := pgRepo.EnsureRunColumns(migrateCtx); err != nil {
		applog.Warnf("⚠️  Failed to ensure run columns: %v", err)
	}
	if err := pgRepo.EnsureTracesTable(migrateCtx); err != nil {
		applog.Warnf("⚠️  Failed to ensure conversation_traces table: %v", err)
	} else {
		applog.Info("✅ Conversation traces table ready")
	}
	if err := pgRepo.EnsureNodeExecTable(migrateCtx); err != nil {
		applog.Warnf("⚠️  Failed to ensure node_executions table: %v", err)
	} else {
		applog.Info("✅ Node executions table ready")
	}
	if err := pgRepo.EnsureLLMTracesTable(migrateCtx); err != nil {
		applog.Warnf("⚠️  Failed to ensure llm_call_traces table: %v", err)
	} else {
		applog.Info("✅ LLM call traces table ready")
	}
	if err := pgRepo.EnsureExternalAsyncTaskTable(migrateCtx); err != nil {
		applog.Warnf("⚠️  Failed to ensure external_async_tasks table: %v", err)
	} else {
		applog.Info("✅ External async tasks table ready")
	}
	if err := pgRepo.EnsureTenantTables(migrateCtx); err != nil {
		applog.Warnf("⚠️  Failed to ensure tenant tables: %v", err)
	} else {
		applog.Info("✅ Tenant tables ready (organizations, tenants, conversations)")
	}
	if err := pgRepo.EnsureTenantColumns(migrateCtx); err != nil {
		applog.Warnf("⚠️  Failed to ensure tenant columns: %v", err)
	} else {
		applog.Info("✅ Tenant columns ready on business tables")
	}

	engineConfig := &engine.Config{
		MaxWorkers:   cfg.Engine.MaxWorkers,
		NodeTimeout:  time.Duration(cfg.Engine.NodeTimeoutSeconds) * time.Second,
		MaxNodeSteps: cfg.Engine.MaxNodeSteps,
	}

	initLLMProviders(
		cfg.OpenAI.APIKey,
		cfg.OpenAI.BaseURL,
		cfg.OpenAI.ConnectTimeoutSeconds,
		cfg.OpenAI.TLSHandshakeTimeoutSeconds,
	)
	initASRProviders(cfg)

	memCoord := initMemory(db, cfg)
	runner := workflow.NewWorkflowRunner(engineConfig, memCoord)
	asyncManager := workflow.NewAsyncRunManager(repo, runner, workflow.AsyncRunManagerConfig{
		Workers:      cfg.Runtime.AsyncRunWorkers,
		PollInterval: time.Duration(cfg.Runtime.AsyncRunPollIntervalMs) * time.Millisecond,
		RunTimeout:   time.Duration(cfg.Runtime.AsyncRunTimeoutSeconds) * time.Second,
	})
	asyncManager.Start(appCtx)

	serverConfig := api.DefaultServerConfig()
	serverConfig.Host = cfg.Server.Host
	serverConfig.Port = cfg.Server.Port
	serverConfig.ReadTimeout = time.Duration(cfg.Server.ReadTimeoutSeconds) * time.Second
	serverConfig.WriteTimeout = time.Duration(cfg.Server.WriteTimeoutSeconds) * time.Second
	serverConfig.RunTimeout = time.Duration(cfg.Server.RunTimeoutSeconds) * time.Second
	serverConfig.JWTSecret = cfg.Auth.JWTSecret
	serverConfig.JWTIssuer = cfg.Auth.JWTIssuer
	serverConfig.ASRTempDir = cfg.ASR.TempDir
	serverConfig.ASRMaxAudioMB = cfg.ASR.MaxAudioMB
	server := api.NewServer(serverConfig, repo, runner)

	ragCfg := &cfg.RAG
	if ragCfg.OpenSearchURL != "" {
		rclient := opensearch.NewClient(ragCfg)
		pingCtx, pingCancel := context.WithTimeout(context.Background(), time.Duration(cfg.Runtime.OpenSearchPingTimeoutSeconds)*time.Second)
		err = rclient.Ping(pingCtx)
		pingCancel()
		if err != nil {
			applog.Warnf("⚠️  OpenSearch ping failed: %v (RAG disabled)", err)
		} else {
			applog.Info("✅ Connected to OpenSearch")
			retriever := rag.NewRetriever(rclient, ragCfg)
			indexer := rag.NewIndexer(rclient, ragCfg)

			embeddingDims := 0
			if ragCfg.HasEmbedding() {
				embedder := rag.NewOpenAIEmbedder(rag.OpenAIEmbedderConfig{
					BaseURL:        cfg.OpenAI.BaseURL,
					APIKey:         cfg.OpenAI.APIKey,
					Model:          ragCfg.EmbeddingModel,
					Dims:           ragCfg.EmbeddingDims,
					TimeoutSeconds: ragCfg.EmbeddingHTTPTimeoutSeconds,
					BatchSize:      ragCfg.EmbeddingBatchSize,
				})
				retriever.SetEmbedder(embedder)
				indexer.SetEmbedder(embedder)
				embeddingDims = embedder.Dims()
				applog.Infof("✅ RAG Embedder initialized (model: %s, dims: %d)", ragCfg.EmbeddingModel, embeddingDims)
			}

			if ragCfg.HasRerank() {
				reranker := rag.NewLLMReranker(ragCfg.RerankProvider, ragCfg.RerankModel)
				retriever.SetReranker(reranker)
				applog.Infof("✅ RAG Reranker initialized (provider: %s, model: %s)", ragCfg.RerankProvider, ragCfg.RerankModel)
			}

			if ragCfg.HasCache() {
				if opt, err := goredis.ParseURL(cfg.Redis.URL); err == nil {
					cacheRedis := goredis.NewClient(opt)
					searchCache := redisdb.NewSearchCache(cacheRedis, ragCfg.CacheTTL)
					retriever.SetCache(searchCache)
					indexer.SetCache(searchCache)
					applog.Infof("✅ RAG Search cache initialized (TTL: %ds)", ragCfg.CacheTTL)
				} else {
					applog.Warnf("⚠️  Redis URL invalid, RAG cache disabled: %v", err)
				}
			}

			parsers := rag.NewParserRegistry()
			indexer.SetParsers(parsers)
			applog.Infof("✅ RAG Parser registry initialized (types: %s)", parsers.SupportedTypes())

			server.SetRAG(retriever, indexer, ragCfg.MaxFileSize)
			runner.SetRetriever(retriever)

			if err := rclient.EnsureIndex(context.Background(), embeddingDims); err != nil {
				applog.Warnf("⚠️  Failed to ensure RAG index: %v", err)
			}

			if err := pgRepo.EnsureRAGTables(context.Background()); err != nil {
				applog.Warnf("⚠️  Failed to ensure RAG tables: %v", err)
			} else {
				applog.Info("✅ RAG tables ready (datasets, documents)")
			}
		}
	} else {
		applog.Info("ℹ️  No OPENSEARCH_URL set, RAG disabled")
	}

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh

		applog.Info("🔄 Shutting down...")
		appCancel()
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.Runtime.ShutdownTimeoutSeconds)*time.Second)
		defer cancel()

		if err := server.Stop(ctx); err != nil {
			applog.Errorf("❌ Server shutdown error: %v", err)
		}
	}()

	if err := server.Start(); err != nil && err.Error() != "http: Server closed" {
		applog.Fatalf("❌ Server error: %v", err)
	}
	appCancel()

	applog.Info("👋 Server stopped")
}

func initLLMProviders(apiKey, baseURL string, connectTimeoutSeconds, tlsHandshakeTimeoutSeconds int) {
	bootstrap.RegisterLLMProviders(apiKey, baseURL, connectTimeoutSeconds, tlsHandshakeTimeoutSeconds)
}

func initASRProviders(cfg *config.AppConfig) {
	if cfg == nil {
		return
	}
	bootstrap.RegisterASRProviders(
		cfg.ASR.TempDir,
		cfg.ASR.MaxAudioMB,
		cfg.ASR.MaxBase64Chars,
		cfg.ASR.URLFetchTimeoutMS,
		cfg.ASR.Async.CallbackBaseURL,
		cfg.ASR.Async.PollIntervalMS,
		cfg.ASR.Async.WaitTimeoutMS,
		cfg.ASR.Tencent.AppID,
		cfg.ASR.Tencent.SecretID,
		cfg.ASR.Tencent.SecretKey,
		cfg.ASR.Tencent.EngineType,
		cfg.ASR.TencentRec.SecretID,
		cfg.ASR.TencentRec.SecretKey,
		cfg.ASR.TencentRec.Region,
		cfg.ASR.TencentRec.EngineModelType,
	)
}

func initMemory(db *sql.DB, cfg *config.AppConfig) *memory.Coordinator {
	opt, err := goredis.ParseURL(cfg.Redis.URL)
	if err != nil {
		applog.Fatalf("❌ Invalid REDIS_URL: %v", err)
	}

	redisClient := goredis.NewClient(opt)
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.Runtime.RedisPingTimeoutSeconds)*time.Second)
	defer cancel()
	if err := redisClient.Ping(ctx).Err(); err != nil {
		applog.Fatalf("❌ Redis connection failed: %v", err)
	}
	applog.Info("✅ Connected to Redis for memory store")

	stm := redisdb.NewSTMStore(redisdb.STMStoreConfig{Client: redisClient})
	coordinator := memory.NewCoordinator(stm)

	estimator := &memory.SimpleTokenEstimator{}
	gateway := memory.NewGatewayCompressor(cfg.Gateway.Provider, cfg.Gateway.Model, estimator)
	compressLock := redisdb.NewCompressLock(redisClient)
	coordinator.WithGateway(gateway, compressLock)
	memory.SetGatewayDefaults(
		cfg.Gateway.ContextWindowSize,
		cfg.Gateway.ThresholdRatio,
		cfg.Gateway.MinRecentTurns,
	)

	applog.Infof("✅ Gateway compressor initialized (provider: %s, model: %s, window: %d, threshold: %.2f, min_recent: %d)",
		cfg.Gateway.Provider, cfg.Gateway.Model, cfg.Gateway.ContextWindowSize, cfg.Gateway.ThresholdRatio, cfg.Gateway.MinRecentTurns)

	mtm := postgres.NewMTMStore(postgres.MTMStoreConfig{
		DB:    db,
		Redis: redisClient,
	})

	ctxDB, cancelDB := context.WithTimeout(context.Background(), time.Duration(cfg.Runtime.MTMEnsureTimeoutSeconds)*time.Second)
	defer cancelDB()
	if err := mtm.EnsureTable(ctxDB); err != nil {
		applog.Warnf("⚠️  Failed to create conversation_summaries table: %v", err)
	} else {
		applog.Info("✅ Mid-term memory table ready (conversation_summaries)")
	}

	gen := memory.NewLLMSummaryGenerator(cfg.Summary.Provider, cfg.Summary.Model)
	coordinator.WithMidTerm(mtm, gen)
	applog.Infof("✅ Mid-term memory initialized (provider: %s, model: %s)", cfg.Summary.Provider, cfg.Summary.Model)

	return coordinator
}
