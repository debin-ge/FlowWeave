package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
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
	Host          string
	Port          int
	ReadTimeout   time.Duration
	WriteTimeout  time.Duration
	RunTimeout    time.Duration // 工作流执行超时（同步/流式）
	JWTSecret     string        // JWT 签名密钥（必填）
	JWTIssuer     string        // JWT 签发者（可选）
	ASRTempDir    string        // ASR multipart 文件暂存目录
	ASRMaxAudioMB int           // ASR 上传文件大小上限
}

// DefaultServerConfig 默认配置
func DefaultServerConfig() *ServerConfig {
	return &ServerConfig{
		Host:          "0.0.0.0",
		Port:          8080,
		ReadTimeout:   30 * time.Second,
		WriteTimeout:  10 * time.Minute, // SSE 需要较长写超时
		RunTimeout:    5 * time.Minute,
		ASRTempDir:    "/tmp/flowweave-asr",
		ASRMaxAudioMB: 50,
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
	r, err := s.buildRouter()
	if err != nil {
		return err
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
	r, err := s.buildRouter()
	if err != nil {
		panic(err)
	}
	return r
}

func (s *Server) buildRouter() (http.Handler, error) {
	if strings.TrimSpace(s.config.JWTSecret) == "" {
		return nil, fmt.Errorf("JWT_SECRET is required")
	}

	r := chi.NewRouter()

	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestID)
	r.Use(corsMiddleware)

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	workflowHandler := NewWorkflowHandler(s.repo, s.runner, s.config.RunTimeout, RunInputConfig{
		ASRTempDir:    s.config.ASRTempDir,
		ASRMaxAudioMB: s.config.ASRMaxAudioMB,
	})
	orgHandler := NewOrganizationHandler(s.repo)
	tenantHandler := NewTenantHandler(s.repo)
	ragEnabled := s.retriever != nil || s.indexer != nil

	jwtCfg := &JWTConfig{
		Secret: s.config.JWTSecret,
		Issuer: s.config.JWTIssuer,
	}
	authMW := authMiddleware(jwtCfg, s.repo)

	s.registerPublicRoutes(r, orgHandler, tenantHandler)
	s.registerProtectedRoutes(r, authMW, workflowHandler, orgHandler, tenantHandler, ragEnabled)
	return r, nil
}

func (s *Server) registerPublicRoutes(r chi.Router, orgHandler *OrganizationHandler, tenantHandler *TenantHandler) {
	orgHandler.RegisterPublicRoutes(r)
	tenantHandler.RegisterPublicRoutes(r)
}

func (s *Server) registerProtectedRoutes(
	r chi.Router,
	authMW func(http.Handler) http.Handler,
	workflowHandler *WorkflowHandler,
	orgHandler *OrganizationHandler,
	tenantHandler *TenantHandler,
	ragEnabled bool,
) {
	orgHandler.RegisterProtectedRoutesWithMiddleware(r, authMW)
	tenantHandler.RegisterProtectedRoutesWithMiddleware(r, authMW)

	r.Group(func(r chi.Router) {
		r.Use(authMW)
		workflowHandler.RegisterRoutes(r)
		if ragEnabled {
			ragHandler := NewRAGHandler(s.repo, s.retriever, s.indexer, s.ragMaxMB)
			ragHandler.RegisterRoutes(r)
			applog.Info("📚 RAG API enabled")
		}
	})
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
