package api

import (
	"encoding/json"
	applog "flowweave/internal/platform/log"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"time"

	"flowweave/internal/domain/rag"
	"flowweave/internal/domain/workflow/port"

	"github.com/go-chi/chi/v5"
)

// RAGHandler RAG 检索与知识库管理 API
type RAGHandler struct {
	repo      port.Repository
	retriever *rag.Retriever
	indexer   *rag.Indexer
	maxFileMB int
}

// NewRAGHandler 创建 RAG 处理器
func NewRAGHandler(repo port.Repository, retriever *rag.Retriever, indexer *rag.Indexer, maxFileMB int) *RAGHandler {
	if maxFileMB <= 0 {
		maxFileMB = 50
	}
	return &RAGHandler{
		repo:      repo,
		retriever: retriever,
		indexer:   indexer,
		maxFileMB: maxFileMB,
	}
}

// RegisterRoutes 注册 RAG 路由
func (h *RAGHandler) RegisterRoutes(r chi.Router) {
	r.Route("/rag", func(r chi.Router) {
		// 知识检索
		r.Post("/search", h.Search)

		// 文档管理
		r.Route("/datasets", func(r chi.Router) {
			r.Post("/", h.CreateDataset)
			r.Get("/", h.ListDatasets)
			r.Get("/{id}", h.GetDataset)
			r.Put("/{id}", h.UpdateDataset)
			r.Delete("/{id}", h.DeleteDataset)

			// 文档管理
			r.Route("/{datasetID}/documents", func(r chi.Router) {
				r.Post("/", h.IndexDocument)
				r.Post("/upload", h.UploadDocument)
				r.Post("/qa", h.IndexQAPairs)
				r.Get("/", h.ListDocuments)
				r.Get("/{docID}", h.GetDocument)
				r.Delete("/{docID}", h.DeleteDocument)
			})
		})
	})
}

// --- 知识检索 ---

func (h *RAGHandler) Search(w http.ResponseWriter, r *http.Request) {
	if h.retriever == nil {
		writeError(w, http.StatusServiceUnavailable, "RAG retriever not configured")
		return
	}

	var req rag.SearchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Query == "" {
		writeError(w, http.StatusBadRequest, "query is required")
		return
	}

	// 从 Scope 注入多租户
	if scope, err := ScopeFrom(r.Context()); err == nil {
		req.OrgID = scope.OrgID
		req.TenantID = scope.TenantID
	}

	result, err := h.retriever.Search(r.Context(), &req)
	if err != nil {
		applog.Error("[RAG] Search failed", "error", err)
		writeError(w, http.StatusInternalServerError, "search failed")
		return
	}

	writeJSON(w, http.StatusOK, result)
}

// --- 知识库 CRUD ---

func (h *RAGHandler) CreateDataset(w http.ResponseWriter, r *http.Request) {
	var ds port.Dataset
	if err := json.NewDecoder(r.Body).Decode(&ds); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if ds.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	// 注入 scope
	if scope, err := ScopeFrom(r.Context()); err == nil {
		ds.OrgID = scope.OrgID
		ds.TenantID = scope.TenantID
	}

	if err := h.repo.CreateDataset(r.Context(), &ds); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create dataset")
		return
	}
	writeJSON(w, http.StatusCreated, ds)
}

func (h *RAGHandler) GetDataset(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	ds, err := h.repo.GetDataset(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get dataset")
		return
	}
	if ds == nil {
		writeError(w, http.StatusNotFound, "dataset not found")
		return
	}
	writeJSON(w, http.StatusOK, ds)
}

func (h *RAGHandler) ListDatasets(w http.ResponseWriter, r *http.Request) {
	orgID := ""
	tenantID := ""
	if scope, err := ScopeFrom(r.Context()); err == nil {
		orgID = scope.OrgID
		tenantID = scope.TenantID
	}

	datasets, err := h.repo.ListDatasets(r.Context(), orgID, tenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list datasets")
		return
	}
	writeJSON(w, http.StatusOK, datasets)
}

func (h *RAGHandler) UpdateDataset(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var ds port.Dataset
	if err := json.NewDecoder(r.Body).Decode(&ds); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	ds.ID = id
	if err := h.repo.UpdateDataset(r.Context(), &ds); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update dataset")
		return
	}
	writeJSON(w, http.StatusOK, ds)
}

func (h *RAGHandler) DeleteDataset(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.repo.DeleteDataset(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete dataset")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// --- 文档管理 ---

type indexDocumentRequest struct {
	Title   string   `json:"title"`
	Content string   `json:"content"`
	Source  string   `json:"source,omitempty"`
	Tags    []string `json:"tags,omitempty"`
}

func (h *RAGHandler) IndexDocument(w http.ResponseWriter, r *http.Request) {
	if h.indexer == nil {
		writeError(w, http.StatusServiceUnavailable, "RAG indexer not configured")
		return
	}

	datasetID := chi.URLParam(r, "datasetID")

	var req indexDocumentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Content == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}
	if req.Title == "" {
		req.Title = "Untitled"
	}

	// 获取 scope
	orgID, tenantID := "", ""
	if scope, err := ScopeFrom(r.Context()); err == nil {
		orgID = scope.OrgID
		tenantID = scope.TenantID
	}

	start := time.Now()

	// 1. 入库 OpenSearch
	indexReq := &rag.IndexRequest{
		DatasetID: datasetID,
		OrgID:     orgID,
		TenantID:  tenantID,
		Title:     req.Title,
		Content:   req.Content,
		Source:    req.Source,
		Tags:      req.Tags,
	}

	result, err := h.indexer.IndexDocument(r.Context(), indexReq)
	if err != nil {
		applog.Error("[RAG] IndexDocument failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to index document")
		return
	}

	// 2. 保存文档元数据到 PostgreSQL
	doc := &port.Document{
		ID:         result.DocID,
		DatasetID:  datasetID,
		OrgID:      orgID,
		TenantID:   tenantID,
		Name:       req.Title,
		Source:     req.Source,
		ChunkCount: result.ChunkCount,
		Status:     "completed",
	}
	if err := h.repo.CreateDocument(r.Context(), doc); err != nil {
		applog.Warn("[RAG] Failed to save document metadata", "error", err)
	}

	// 3. 更新 dataset doc_count
	ds, _ := h.repo.GetDataset(r.Context(), datasetID)
	if ds != nil {
		ds.DocCount++
		_ = h.repo.UpdateDataset(r.Context(), ds)
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"doc_id":      result.DocID,
		"chunk_count": result.ChunkCount,
		"elapsed_ms":  time.Since(start).Milliseconds(),
	})
}

func (h *RAGHandler) ListDocuments(w http.ResponseWriter, r *http.Request) {
	datasetID := chi.URLParam(r, "datasetID")
	docs, err := h.repo.ListDocuments(r.Context(), datasetID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list documents")
		return
	}
	writeJSON(w, http.StatusOK, docs)
}

func (h *RAGHandler) GetDocument(w http.ResponseWriter, r *http.Request) {
	docID := chi.URLParam(r, "docID")
	doc, err := h.repo.GetDocument(r.Context(), docID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get document")
		return
	}
	if doc == nil {
		writeError(w, http.StatusNotFound, "document not found")
		return
	}
	writeJSON(w, http.StatusOK, doc)
}

func (h *RAGHandler) DeleteDocument(w http.ResponseWriter, r *http.Request) {
	docID := chi.URLParam(r, "docID")
	if err := h.repo.DeleteDocument(r.Context(), docID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete document")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// --- 文件上传 ---

// UploadDocument 文件上传入库（multipart/form-data）
func (h *RAGHandler) UploadDocument(w http.ResponseWriter, r *http.Request) {
	if h.indexer == nil {
		writeError(w, http.StatusServiceUnavailable, "RAG indexer not configured")
		return
	}

	datasetID := chi.URLParam(r, "datasetID")

	limitBytes := int64(h.maxFileMB) << 20

	// 解析 multipart（限制 maxFileMB MB）
	if err := r.ParseMultipartForm(limitBytes); err != nil {
		writeError(w, http.StatusBadRequest, "failed to parse multipart form")
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "file field is required")
		return
	}
	defer file.Close()

	if header.Size > 0 && header.Size > limitBytes {
		writeError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("file size exceeds limit (%dMB)", h.maxFileMB))
		return
	}

	filename := header.Filename
	title := r.FormValue("title")
	if title == "" {
		title = filename
	}
	source := r.FormValue("source")
	if source == "" {
		source = filename
	}

	// 获取解析器
	parsers := h.indexer.Parsers()
	if parsers == nil {
		// 无解析器，直接读取为纯文本
		data, err := io.ReadAll(file)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read file")
			return
		}
		h.indexAndRespond(w, r, datasetID, title, string(data), source, nil)
		return
	}

	parser, err := parsers.Get(filename)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("unsupported file type: %s (supported: %s)", filepath.Ext(filename), parsers.SupportedTypes()))
		return
	}

	// 解析文档
	result, err := parser.Parse(file, filename)
	if err != nil {
		applog.Error("[RAG] File parse failed", "filename", filename, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to parse file")
		return
	}

	if result.Content == "" {
		writeError(w, http.StatusBadRequest, "no text content extracted from file")
		return
	}

	h.indexAndRespond(w, r, datasetID, title, result.Content, source, result.Metadata)
}

// IndexQAPairs QA 对批量入库
func (h *RAGHandler) IndexQAPairs(w http.ResponseWriter, r *http.Request) {
	if h.indexer == nil {
		writeError(w, http.StatusServiceUnavailable, "RAG indexer not configured")
		return
	}

	datasetID := chi.URLParam(r, "datasetID")

	var req struct {
		Title   string       `json:"title"`
		QAPairs []rag.QAPair `json:"qa_pairs"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.QAPairs) == 0 {
		writeError(w, http.StatusBadRequest, "qa_pairs is required")
		return
	}
	if req.Title == "" {
		req.Title = "QA Pairs"
	}

	orgID, tenantID := "", ""
	if scope, err := ScopeFrom(r.Context()); err == nil {
		orgID = scope.OrgID
		tenantID = scope.TenantID
	}

	start := time.Now()

	indexReq := &rag.IndexRequest{
		DatasetID: datasetID,
		OrgID:     orgID,
		TenantID:  tenantID,
		Title:     req.Title,
		QAPairs:   req.QAPairs,
		Source:    "qa_import",
	}

	result, err := h.indexer.IndexDocument(r.Context(), indexReq)
	if err != nil {
		applog.Error("[RAG] IndexQAPairs failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to index QA pairs")
		return
	}

	// 保存文档元数据
	doc := &port.Document{
		ID:         result.DocID,
		DatasetID:  datasetID,
		OrgID:      orgID,
		TenantID:   tenantID,
		Name:       req.Title,
		Source:     "qa_import",
		ChunkCount: result.ChunkCount,
		Status:     "completed",
	}
	if err := h.repo.CreateDocument(r.Context(), doc); err != nil {
		applog.Warn("[RAG] Failed to save QA document metadata", "error", err)
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"doc_id":      result.DocID,
		"qa_count":    len(req.QAPairs),
		"chunk_count": result.ChunkCount,
		"elapsed_ms":  time.Since(start).Milliseconds(),
	})
}

// indexAndRespond 通用入库+响应逻辑
func (h *RAGHandler) indexAndRespond(w http.ResponseWriter, r *http.Request, datasetID, title, content, source string, metadata map[string]string) {
	orgID, tenantID := "", ""
	if scope, err := ScopeFrom(r.Context()); err == nil {
		orgID = scope.OrgID
		tenantID = scope.TenantID
	}

	start := time.Now()

	indexReq := &rag.IndexRequest{
		DatasetID: datasetID,
		OrgID:     orgID,
		TenantID:  tenantID,
		Title:     title,
		Content:   content,
		Source:    source,
	}

	result, err := h.indexer.IndexDocument(r.Context(), indexReq)
	if err != nil {
		applog.Error("[RAG] IndexDocument failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to index document")
		return
	}

	// 保存元数据
	doc := &port.Document{
		ID:         result.DocID,
		DatasetID:  datasetID,
		OrgID:      orgID,
		TenantID:   tenantID,
		Name:       title,
		Source:     source,
		ChunkCount: result.ChunkCount,
		Status:     "completed",
	}
	if err := h.repo.CreateDocument(r.Context(), doc); err != nil {
		applog.Warn("[RAG] Failed to save document metadata", "error", err)
	}

	ds, _ := h.repo.GetDataset(r.Context(), datasetID)
	if ds != nil {
		ds.DocCount++
		_ = h.repo.UpdateDataset(r.Context(), ds)
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"doc_id":      result.DocID,
		"chunk_count": result.ChunkCount,
		"elapsed_ms":  time.Since(start).Milliseconds(),
	})
}
