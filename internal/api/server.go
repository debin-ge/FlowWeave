package api

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"flowweave/internal/app/workflow"
	"flowweave/internal/domain/rag"
	"flowweave/internal/domain/workflow/port"
	applog "flowweave/internal/platform/log"
)

// ServerConfig 服务配置
type ServerConfig struct {
	Host         string
	Port         int
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	RunTimeout   time.Duration // 工作流执行超时（同步/流式）
	JWTSecret    string        // JWT 签名密钥，为空则跳过鉴权
	JWTIssuer    string        // JWT 签发者（可选）
}

// DefaultServerConfig 默认配置
func DefaultServerConfig() *ServerConfig {
	return &ServerConfig{
		Host:         "0.0.0.0",
		Port:         8080,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 10 * time.Minute, // SSE 需要较长写超时
		RunTimeout:   5 * time.Minute,
	}
}

// Server HTTP 服务器
type Server struct {
	config    *ServerConfig
	repo      port.Repository
	runner    *workflow.WorkflowRunner
	retriever *rag.Retriever
	indexer   *rag.Indexer
	ragMaxMB  int
	httpSrv   *http.Server
}

// NewServer 创建服务器
func NewServer(config *ServerConfig, repo port.Repository, runner *workflow.WorkflowRunner) *Server {
	if config == nil {
		config = DefaultServerConfig()
	}
	return &Server{
		config: config,
		repo:   repo,
		runner: runner,
	}
}

// SetRAG 设置 RAG 组件（可选，仅在 OpenSearch 配置时启用）
func (s *Server) SetRAG(retriever *rag.Retriever, indexer *rag.Indexer, maxFileMB int) {
	s.retriever = retriever
	s.indexer = indexer
	s.ragMaxMB = maxFileMB
}

// Start 启动服务器
func (s *Server) Start() error {
	r := chi.NewRouter()

	// 基础中间件（必须在所有路由定义之前注册）
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestID)
	r.Use(corsMiddleware)

	// 健康检查（无需鉴权，放在 JWT 中间件之前）
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	// JWT 鉴权中间件（仅当 JWT_SECRET 配置时启用）
	// 使用 Group 包裹需要鉴权的路由，避免 chi 的中间件顺序限制
	if s.config.JWTSecret != "" {
		jwtCfg := &JWTConfig{
			Secret: s.config.JWTSecret,
			Issuer: s.config.JWTIssuer,
		}
		applog.Info("🔐 JWT authentication enabled")

		r.Group(func(r chi.Router) {
			r.Use(authMiddleware(jwtCfg))

			// 注册工作流 API
			handler := NewWorkflowHandler(s.repo, s.runner, s.config.RunTimeout)
			handler.RegisterRoutes(r)

			// 注册租户和组织 API
			orgHandler := NewOrganizationHandler(s.repo)
			orgHandler.RegisterRoutes(r)
			tenantHandler := NewTenantHandler(s.repo)
			tenantHandler.RegisterRoutes(r)

			// 注册 RAG API（仅在配置时启用）
			if s.retriever != nil || s.indexer != nil {
				ragHandler := NewRAGHandler(s.repo, s.retriever, s.indexer, s.ragMaxMB)
				ragHandler.RegisterRoutes(r)
				applog.Info("📚 RAG API enabled")
			}
		})
	} else {
		applog.Warn("⚠️  JWT_SECRET not set, authentication disabled (development mode)")

		// 注册工作流 API
		handler := NewWorkflowHandler(s.repo, s.runner, s.config.RunTimeout)
		handler.RegisterRoutes(r)

		// 注册租户和组织 API
		orgHandler := NewOrganizationHandler(s.repo)
		orgHandler.RegisterRoutes(r)
		tenantHandler := NewTenantHandler(s.repo)
		tenantHandler.RegisterRoutes(r)

		// 注册 RAG API（仅在配置时启用）
		if s.retriever != nil || s.indexer != nil {
			ragHandler := NewRAGHandler(s.repo, s.retriever, s.indexer, s.ragMaxMB)
			ragHandler.RegisterRoutes(r)
			applog.Info("📚 RAG API enabled")
		}
	}

	addr := fmt.Sprintf("%s:%d", s.config.Host, s.config.Port)
	s.httpSrv = &http.Server{
		Addr:         addr,
		Handler:      r,
		ReadTimeout:  s.config.ReadTimeout,
		WriteTimeout: s.config.WriteTimeout,
	}

	applog.Infof("🚀 Workflow API server starting on %s", addr)
	return s.httpSrv.ListenAndServe()
}

// Stop 优雅停机
func (s *Server) Stop(ctx context.Context) error {
	if s.httpSrv != nil {
		return s.httpSrv.Shutdown(ctx)
	}
	return nil
}

// Handler 返回 HTTP Handler（用于测试）
func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	// 测试模式：如果配置了 JWT 也加上
	if s.config.JWTSecret != "" {
		jwtCfg := &JWTConfig{
			Secret: s.config.JWTSecret,
			Issuer: s.config.JWTIssuer,
		}
		r.Use(authMiddleware(jwtCfg))
	}

	handler := NewWorkflowHandler(s.repo, s.runner, s.config.RunTimeout)
	handler.RegisterRoutes(r)

	orgHandler := NewOrganizationHandler(s.repo)
	orgHandler.RegisterRoutes(r)
	tenantHandler := NewTenantHandler(s.repo)
	tenantHandler.RegisterRoutes(r)

	if s.retriever != nil || s.indexer != nil {
		ragHandler := NewRAGHandler(s.repo, s.retriever, s.indexer, s.ragMaxMB)
		ragHandler.RegisterRoutes(r)
	}
	return r
}

// corsMiddleware CORS 中间件
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}
