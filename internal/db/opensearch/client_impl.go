package opensearch

import (
	applog "flowweave/internal/platform/log"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	domainrag "flowweave/internal/domain/rag"
)

// Client OpenSearch HTTP 客户端
type Client struct {
	baseURL    string
	username   string
	password   string
	httpClient *http.Client
	indexName  string
}

// NewClient 创建 OpenSearch 客户端
func NewClient(cfg *domainrag.Config) *Client {
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // 开发环境
	}
	return &Client{
		baseURL:  strings.TrimRight(cfg.OpenSearchURL, "/"),
		username: cfg.OpenSearchUsername,
		password: cfg.OpenSearchPassword,
		httpClient: &http.Client{
			Timeout:   30 * time.Second,
			Transport: transport,
		},
		indexName: cfg.ChunkIndexName(),
	}
}

// EnsureIndex 确保索引存在，如不存在则创建
func (c *Client) EnsureIndex(ctx context.Context, dims int) error {
	// 检查索引是否存在
	resp, err := c.doRequest(ctx, "HEAD", "/"+c.indexName, nil)
	if err != nil {
		return fmt.Errorf("check index existence: %w", err)
	}
	resp.Body.Close()

	if resp.StatusCode == 200 {
		applog.Info("[RAG] Index already exists", "index", c.indexName)
		return nil
	}

	// 创建索引
	settings := map[string]interface{}{
		"analysis": map[string]interface{}{
			"analyzer": map[string]interface{}{
				"ik_max": map[string]interface{}{
					"type":      "custom",
					"tokenizer": "ik_max_word",
				},
			},
		},
	}

	// 如果启用向量检索（dims > 0），添加 knn 设置
	if dims > 0 {
		settings["index.knn"] = true
	}

	properties := map[string]interface{}{
		"doc_id":     map[string]string{"type": "keyword"},
		"chunk_id":   map[string]string{"type": "keyword"},
		"dataset_id": map[string]string{"type": "keyword"},
		"org_id":     map[string]string{"type": "keyword"},
		"tenant_id":  map[string]string{"type": "keyword"},
		"title": map[string]string{
			"type":     "text",
			"analyzer": "ik_max",
		},
		"content": map[string]string{
			"type":     "text",
			"analyzer": "ik_max",
		},
		"tags":       map[string]string{"type": "keyword"},
		"source":     map[string]string{"type": "keyword"},
		"page":       map[string]string{"type": "integer"},
		"created_at": map[string]string{"type": "date"},
	}

	// 仅在启用向量检索时添加 vector 字段（OpenSearch 使用 knn_vector 而非 dense_vector）
	if dims > 0 {
		properties["vector"] = map[string]interface{}{
			"type":      "knn_vector",
			"dimension": dims,
			"method": map[string]interface{}{
				"name":       "hnsw",
				"space_type": "cosinesimil",
				"engine":     "lucene",
			},
		}
	}

	mapping := map[string]interface{}{
		"settings": settings,
		"mappings": map[string]interface{}{
			"properties": properties,
		},
	}

	body, _ := json.Marshal(mapping)
	resp, err = c.doRequest(ctx, "PUT", "/"+c.indexName, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create index: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("create index failed (%d): %s", resp.StatusCode, string(respBody))
	}

	applog.Info("[RAG] Index created", "index", c.indexName)
	return nil
}

// BulkIndex 批量写入文档
func (c *Client) BulkIndex(ctx context.Context, docs []domainrag.ChunkDocument) error {
	if len(docs) == 0 {
		return nil
	}

	var buf bytes.Buffer
	for _, doc := range docs {
		action := map[string]interface{}{
			"index": map[string]interface{}{
				"_index": c.indexName,
				"_id":    doc.ChunkID,
			},
		}
		actionLine, _ := json.Marshal(action)
		buf.Write(actionLine)
		buf.WriteByte('\n')

		docLine, _ := json.Marshal(doc)
		buf.Write(docLine)
		buf.WriteByte('\n')
	}

	resp, err := c.doRequest(ctx, "POST", "/_bulk", &buf)
	if err != nil {
		return fmt.Errorf("bulk index: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("bulk index failed (%d): %s", resp.StatusCode, string(respBody))
	}

	applog.Info("[RAG] Bulk indexed", "count", len(docs))
	return nil
}

// SearchBM25 BM25 全文检索
func (c *Client) SearchBM25(ctx context.Context, req *domainrag.SearchRequest) (*domainrag.SearchResult, error) {
	start := time.Now()

	// 构建查询
	must := []interface{}{
		map[string]interface{}{
			"multi_match": map[string]interface{}{
				"query":  req.Query,
				"fields": []string{"title^2", "content"},
			},
		},
	}

	// 构建过滤条件
	var filters []interface{}
	if req.OrgID != "" {
		filters = append(filters, map[string]interface{}{
			"term": map[string]string{"org_id": req.OrgID},
		})
	}
	if req.TenantID != "" {
		filters = append(filters, map[string]interface{}{
			"term": map[string]string{"tenant_id": req.TenantID},
		})
	}
	if len(req.DatasetIDs) > 0 {
		filters = append(filters, map[string]interface{}{
			"terms": map[string]interface{}{"dataset_id": req.DatasetIDs},
		})
	}

	topK := req.TopK
	if topK <= 0 {
		topK = 5
	}

	query := map[string]interface{}{
		"size": topK,
		"query": map[string]interface{}{
			"bool": map[string]interface{}{
				"must":   must,
				"filter": filters,
			},
		},
	}

	return c.executeSearch(ctx, query, start, domainrag.RetrievalModeBM25, req.ScoreThreshold)
}

// SearchKNN kNN 向量检索
func (c *Client) SearchKNN(ctx context.Context, vector []float32, req *domainrag.SearchRequest) (*domainrag.SearchResult, error) {
	start := time.Now()

	topK := req.TopK
	if topK <= 0 {
		topK = 5
	}

	// 构建过滤条件
	var filters []interface{}
	if req.OrgID != "" {
		filters = append(filters, map[string]interface{}{
			"term": map[string]string{"org_id": req.OrgID},
		})
	}
	if req.TenantID != "" {
		filters = append(filters, map[string]interface{}{
			"term": map[string]string{"tenant_id": req.TenantID},
		})
	}
	if len(req.DatasetIDs) > 0 {
		filters = append(filters, map[string]interface{}{
			"terms": map[string]interface{}{"dataset_id": req.DatasetIDs},
		})
	}

	// OpenSearch kNN 查询
	knnQuery := map[string]interface{}{
		"vector": map[string]interface{}{
			"vector": vector,
			"k":      topK,
		},
	}
	if len(filters) > 0 {
		knnQuery["vector"].(map[string]interface{})["filter"] = map[string]interface{}{
			"bool": map[string]interface{}{
				"filter": filters,
			},
		}
	}

	query := map[string]interface{}{
		"size": topK,
		"query": map[string]interface{}{
			"knn": knnQuery,
		},
	}

	return c.executeSearch(ctx, query, start, domainrag.RetrievalModeHybrid, req.ScoreThreshold)
}

// SearchHybrid BM25 + kNN 混合检索（使用 bool should 融合）
func (c *Client) SearchHybrid(ctx context.Context, vector []float32, req *domainrag.SearchRequest) (*domainrag.SearchResult, error) {
	start := time.Now()

	topK := req.TopK
	if topK <= 0 {
		topK = 5
	}

	// 构建过滤条件
	var filters []interface{}
	if req.OrgID != "" {
		filters = append(filters, map[string]interface{}{
			"term": map[string]string{"org_id": req.OrgID},
		})
	}
	if req.TenantID != "" {
		filters = append(filters, map[string]interface{}{
			"term": map[string]string{"tenant_id": req.TenantID},
		})
	}
	if len(req.DatasetIDs) > 0 {
		filters = append(filters, map[string]interface{}{
			"terms": map[string]interface{}{"dataset_id": req.DatasetIDs},
		})
	}

	// 拉取更多候选以便 RRF 融合
	fetchSize := topK * 3
	if fetchSize < 20 {
		fetchSize = 20
	}

	// BM25 查询子句
	bm25Clause := map[string]interface{}{
		"multi_match": map[string]interface{}{
			"query":  req.Query,
			"fields": []string{"title^2", "content"},
		},
	}

	// kNN 查询子句（script_score 方式实现余弦相似度）
	knnClause := map[string]interface{}{
		"script_score": map[string]interface{}{
			"query": map[string]interface{}{
				"match_all": map[string]interface{}{},
			},
			"script": map[string]interface{}{
				"source": "knn_score",
				"lang":   "knn",
				"params": map[string]interface{}{
					"field":       "vector",
					"query_value": vector,
					"space_type":  "cosinesimil",
				},
			},
		},
	}

	// bool should 融合 BM25 + kNN
	boolQuery := map[string]interface{}{
		"should": []interface{}{bm25Clause, knnClause},
	}
	if len(filters) > 0 {
		boolQuery["filter"] = filters
	}

	query := map[string]interface{}{
		"size": fetchSize,
		"query": map[string]interface{}{
			"bool": boolQuery,
		},
	}

	return c.executeSearch(ctx, query, start, domainrag.RetrievalModeHybrid, req.ScoreThreshold)
}

// DeleteByDocID 按 doc_id 删除所有 chunk
func (c *Client) DeleteByDocID(ctx context.Context, docID string) error {
	query := map[string]interface{}{
		"query": map[string]interface{}{
			"term": map[string]string{"doc_id": docID},
		},
	}
	body, _ := json.Marshal(query)

	resp, err := c.doRequest(ctx, "POST", "/"+c.indexName+"/_delete_by_query", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("delete by doc_id: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("delete failed (%d): %s", resp.StatusCode, string(respBody))
	}
	return nil
}

// executeSearch 执行 OpenSearch 查询并解析结果
func (c *Client) executeSearch(ctx context.Context, query map[string]interface{}, start time.Time, mode domainrag.RetrievalMode, scoreThreshold float64) (*domainrag.SearchResult, error) {
	body, _ := json.Marshal(query)
	resp, err := c.doRequest(ctx, "POST", "/"+c.indexName+"/_search", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("search failed (%d): %s", resp.StatusCode, string(respBody))
	}

	// 解析响应
	var osResp struct {
		Hits struct {
			Hits []struct {
				ID     string          `json:"_id"`
				Score  float64         `json:"_score"`
				Source json.RawMessage `json:"_source"`
			} `json:"hits"`
		} `json:"hits"`
	}
	if err := json.Unmarshal(respBody, &osResp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	var docs []domainrag.ResultDocument
	for _, hit := range osResp.Hits.Hits {
		if scoreThreshold > 0 && hit.Score < scoreThreshold {
			continue
		}

		var src domainrag.ChunkDocument
		if err := json.Unmarshal(hit.Source, &src); err != nil {
			applog.Warn("[RAG] Failed to parse hit source", "id", hit.ID, "error", err)
			continue
		}

		docs = append(docs, domainrag.ResultDocument{
			ChunkID:   src.ChunkID,
			Content:   src.Content,
			Score:     hit.Score,
			Source:    src.Source,
			Page:      src.Page,
			Title:     src.Title,
			DatasetID: src.DatasetID,
		})
	}

	return &domainrag.SearchResult{
		Documents: docs,
		Mode:      mode,
		ElapsedMs: time.Since(start).Milliseconds(),
	}, nil
}

// Ping 检查 OpenSearch 连通性
func (c *Client) Ping(ctx context.Context) error {
	resp, err := c.doRequest(ctx, "GET", "/", nil)
	if err != nil {
		return fmt.Errorf("ping opensearch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("opensearch returned status %d", resp.StatusCode)
	}
	return nil
}

// doRequest 执行 HTTP 请求
func (c *Client) doRequest(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	url := c.baseURL + path
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	if c.username != "" {
		req.SetBasicAuth(c.username, c.password)
	}

	return c.httpClient.Do(req)
}
