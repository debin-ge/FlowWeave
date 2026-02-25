package rag

import (
	applog "flowweave/internal/platform/log"
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// Retriever 检索引擎
type Retriever struct {
	client   SearchClient
	config   *Config
	embedder Embedder         // 可选
	reranker Reranker         // 可选
	cache    SearchCacheStore // 可选
}

// NewRetriever 创建检索引擎
func NewRetriever(client SearchClient, config *Config) *Retriever {
	return &Retriever{
		client: client,
		config: config,
	}
}

// SetEmbedder 设置 Embedder（启用向量检索）
func (r *Retriever) SetEmbedder(e Embedder) {
	r.embedder = e
}

// SetReranker 设置 Reranker（启用重排序）
func (r *Retriever) SetReranker(rr Reranker) {
	r.reranker = rr
}

// HasEmbedder 是否配置了 Embedder
func (r *Retriever) HasEmbedder() bool {
	return r.embedder != nil
}

// SetCache 设置检索缓存
func (r *Retriever) SetCache(c SearchCacheStore) {
	r.cache = c
}

// Search 执行知识检索
func (r *Retriever) Search(ctx context.Context, req *SearchRequest) (*SearchResult, error) {
	// 填充默认值
	if req.Mode == "" {
		req.Mode = r.config.DefaultMode
	}
	if req.TopK <= 0 {
		req.TopK = r.config.DefaultTopK
	}

	// AUTO 模式：根据 query 特征 + 能力自动选择
	if req.Mode == RetrievalModeAuto {
		req.Mode = r.autoSelectMode(req.Query)
	}

	// 如果请求 HYBRID 但没有 Embedder，降级为 BM25
	if req.Mode == RetrievalModeHybrid && r.embedder == nil {
		applog.Warn("[RAG] Hybrid mode requested but no embedder configured, falling back to BM25")
		req.Mode = RetrievalModeBM25
	}

	applog.Info("[RAG] Search",
		"query", req.Query,
		"mode", req.Mode,
		"top_k", req.TopK,
		"tenant_id", req.TenantID,
		"has_embedder", r.embedder != nil,
		"has_reranker", r.reranker != nil,
		"has_cache", r.cache != nil,
	)

	// 查询缓存
	if r.cache != nil {
		if cached, ok := r.cache.Get(ctx, req); ok {
			return cached, nil
		}
	}

	var result *SearchResult
	var err error

	switch req.Mode {
	case RetrievalModeBM25:
		result, err = r.client.SearchBM25(ctx, req)
	case RetrievalModeHybrid:
		result, err = r.searchHybrid(ctx, req)
	default:
		return nil, fmt.Errorf("unknown retrieval mode: %s", req.Mode)
	}

	if err != nil {
		return nil, err
	}

	// 写入缓存
	if r.cache != nil && result != nil {
		cacheReq := cloneSearchRequest(req)
		cacheResult := cloneSearchResult(result)
		go func() {
			cacheCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			r.cache.Set(cacheCtx, cacheReq, cacheResult)
		}()
	}

	return result, nil
}

// searchHybrid Hybrid 检索流程：Embed → BM25 + kNN → RRF → Rerank
func (r *Retriever) searchHybrid(ctx context.Context, req *SearchRequest) (*SearchResult, error) {
	start := time.Now()

	// 1. Embed query
	vectors, err := r.embedder.Embed(ctx, []string{req.Query})
	if err != nil {
		applog.Warn("[RAG] Embedding failed, falling back to BM25", "error", err)
		return r.client.SearchBM25(ctx, req)
	}
	queryVector := vectors[0]

	// 2. 并行执行 BM25 和 kNN
	type searchResult struct {
		result *SearchResult
		err    error
	}

	var wg sync.WaitGroup
	bm25Ch := make(chan searchResult, 1)
	knnCh := make(chan searchResult, 1)

	// 多取候选用于融合
	hybridReq := *req
	hybridReq.TopK = req.TopK * 3
	if hybridReq.TopK < 20 {
		hybridReq.TopK = 20
	}

	wg.Add(2)
	go func() {
		defer wg.Done()
		result, err := r.client.SearchBM25(ctx, &hybridReq)
		bm25Ch <- searchResult{result, err}
	}()
	go func() {
		defer wg.Done()
		result, err := r.client.SearchKNN(ctx, queryVector, &hybridReq)
		knnCh <- searchResult{result, err}
	}()

	wg.Wait()
	close(bm25Ch)
	close(knnCh)

	bm25Res := <-bm25Ch
	knnRes := <-knnCh

	// 3. RRF 融合
	var bm25Docs, knnDocs []ResultDocument
	if bm25Res.err == nil && bm25Res.result != nil {
		bm25Docs = bm25Res.result.Documents
	}
	if knnRes.err == nil && knnRes.result != nil {
		knnDocs = knnRes.result.Documents
	}

	if len(bm25Docs) == 0 && len(knnDocs) == 0 {
		return &SearchResult{
			Documents: nil,
			Mode:      RetrievalModeHybrid,
			ElapsedMs: time.Since(start).Milliseconds(),
		}, nil
	}

	// 如果某一路完全失败，使用另一路结果
	if len(bm25Docs) == 0 {
		bm25Docs = nil
	}
	if len(knnDocs) == 0 {
		knnDocs = nil
	}

	merged := rrfMerge(bm25Docs, knnDocs, req.TopK)

	applog.Info("[RAG] Hybrid search merged",
		"bm25_count", len(bm25Docs),
		"knn_count", len(knnDocs),
		"merged_count", len(merged),
	)

	// 4. 可选 Rerank
	if r.reranker != nil && r.config.EnableRerank && len(merged) > 0 {
		reranked, err := r.reranker.Rerank(ctx, req.Query, merged, req.TopK)
		if err != nil {
			applog.Warn("[RAG] Rerank failed, using RRF order", "error", err)
		} else {
			merged = reranked
		}
	}

	// 截断到 TopK
	if len(merged) > req.TopK {
		merged = merged[:req.TopK]
	}

	return &SearchResult{
		Documents: merged,
		Mode:      RetrievalModeHybrid,
		ElapsedMs: time.Since(start).Milliseconds(),
	}, nil
}

// rrfMerge Reciprocal Rank Fusion 融合排序
// 公式: score(d) = Σ 1/(k + rank_i(d)), k=60 (标准参数)
func rrfMerge(list1, list2 []ResultDocument, topK int) []ResultDocument {
	const k = 60.0

	type scored struct {
		doc   ResultDocument
		score float64
	}

	scoreMap := make(map[string]*scored) // key = ChunkID

	// 遍历 BM25 结果
	for rank, doc := range list1 {
		key := doc.ChunkID
		if key == "" {
			key = fmt.Sprintf("bm25_%d", rank)
		}
		if _, ok := scoreMap[key]; !ok {
			scoreMap[key] = &scored{doc: doc}
		}
		scoreMap[key].score += 1.0 / (k + float64(rank+1))
	}

	// 遍历 kNN 结果
	for rank, doc := range list2 {
		key := doc.ChunkID
		if key == "" {
			key = fmt.Sprintf("knn_%d", rank)
		}
		if _, ok := scoreMap[key]; !ok {
			scoreMap[key] = &scored{doc: doc}
		}
		scoreMap[key].score += 1.0 / (k + float64(rank+1))
	}

	// 排序
	results := make([]scored, 0, len(scoreMap))
	for _, s := range scoreMap {
		results = append(results, *s)
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].score > results[j].score
	})

	// 输出
	if topK <= 0 || topK > len(results) {
		topK = len(results)
	}
	docs := make([]ResultDocument, topK)
	for i := 0; i < topK; i++ {
		docs[i] = results[i].doc
		docs[i].Score = results[i].score
	}

	return docs
}

func cloneSearchRequest(req *SearchRequest) *SearchRequest {
	if req == nil {
		return nil
	}
	cloned := *req
	if len(req.DatasetIDs) > 0 {
		cloned.DatasetIDs = append([]string(nil), req.DatasetIDs...)
	}
	if len(req.Filters) > 0 {
		cloned.Filters = make(map[string]string, len(req.Filters))
		for k, v := range req.Filters {
			cloned.Filters[k] = v
		}
	}
	return &cloned
}

func cloneSearchResult(result *SearchResult) *SearchResult {
	if result == nil {
		return nil
	}
	cloned := *result
	if len(result.Documents) > 0 {
		cloned.Documents = append([]ResultDocument(nil), result.Documents...)
	}
	return &cloned
}

// autoSelectMode 根据 query 特征自动选择检索模式
func (r *Retriever) autoSelectMode(query string) RetrievalMode {
	runes := []rune(query)
	// 短关键词 → BM25
	if len(runes) <= 8 {
		return RetrievalModeBM25
	}
	// 有 Embedder 且是自然语言（长查询）→ HYBRID
	if r.embedder != nil {
		return RetrievalModeHybrid
	}
	return RetrievalModeBM25
}

// FormatContext 将检索结果格式化为 LLM 上下文文本
func FormatContext(result *SearchResult) string {
	if result == nil || len(result.Documents) == 0 {
		return ""
	}

	var sb strings.Builder
	for i, doc := range result.Documents {
		sb.WriteString(fmt.Sprintf("[%d] ", i+1))
		if doc.Title != "" {
			sb.WriteString(doc.Title)
			sb.WriteString("\n")
		}
		sb.WriteString(doc.Content)
		if doc.Source != "" {
			sb.WriteString(fmt.Sprintf("\n(来源: %s", doc.Source))
			if doc.Page > 0 {
				sb.WriteString(fmt.Sprintf(", 第%d页", doc.Page))
			}
			sb.WriteString(")")
		}
		sb.WriteString("\n\n")
	}
	return strings.TrimSpace(sb.String())
}

// ── Indexer 入库 Pipeline ─────────────────────────────────────

// Indexer 文档入库 Pipeline
type Indexer struct {
	client   SearchClient
	chunker  *Chunker
	embedder Embedder         // 可选：入库时生成向量
	cache    SearchCacheStore // 可选：入库后清缓存
	parsers  *ParserRegistry
}

// NewIndexer 创建入库 Pipeline
func NewIndexer(client SearchClient, cfg *Config) *Indexer {
	return &Indexer{
		client:  client,
		chunker: NewChunker(cfg.ChunkSize, cfg.ChunkOverlap),
	}
}

// SetEmbedder 设置 Embedder（入库时自动生成向量）
func (idx *Indexer) SetEmbedder(e Embedder) {
	idx.embedder = e
}

// SetCache 设置缓存（入库后自动清除）
func (idx *Indexer) SetCache(c SearchCacheStore) {
	idx.cache = c
}

// SetParsers 设置文档解析器注册表
func (idx *Indexer) SetParsers(p *ParserRegistry) {
	idx.parsers = p
}

// Parsers 返回解析器注册表
func (idx *Indexer) Parsers() *ParserRegistry {
	return idx.parsers
}

// IndexDocument 入库单个文档
func (idx *Indexer) IndexDocument(ctx context.Context, req *IndexRequest) (*IndexResult, error) {
	start := time.Now()

	// 1. 分块
	chunks, err := idx.chunker.Chunk(req)
	if err != nil {
		return nil, fmt.Errorf("chunk document: %w", err)
	}

	// 2. 可选：批量 Embedding
	if idx.embedder != nil && len(chunks) > 0 {
		texts := make([]string, len(chunks))
		for i, c := range chunks {
			texts[i] = c.Content
		}

		vectors, err := idx.embedder.Embed(ctx, texts)
		if err != nil {
			applog.Warn("[RAG/Indexer] Embedding failed, indexing without vectors", "error", err)
		} else if len(vectors) == len(chunks) {
			for i := range chunks {
				chunks[i].Vector = vectors[i]
			}
			applog.Info("[RAG/Indexer] Chunks embedded", "count", len(chunks), "dims", idx.embedder.Dims())
		}
	}

	// 3. 写入 OpenSearch
	if err := idx.client.BulkIndex(ctx, chunks); err != nil {
		return nil, fmt.Errorf("bulk index: %w", err)
	}

	docID := ""
	if len(chunks) > 0 {
		docID = chunks[0].DocID
	}

	applog.Info("[RAG] Document indexed",
		"doc_id", docID,
		"chunks", len(chunks),
		"has_vectors", idx.embedder != nil,
		"elapsed_ms", time.Since(start).Milliseconds(),
	)

	// 4. 主动清除相关缓存
	if idx.cache != nil && req.DatasetID != "" {
		idx.cache.InvalidateByDataset(ctx, req.DatasetID)
	}

	return &IndexResult{
		DocID:      docID,
		ChunkCount: len(chunks),
	}, nil
}
