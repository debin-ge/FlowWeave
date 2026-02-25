package rag

import "context"

// SearchClient defines storage/search operations required by Retriever/Indexer.
type SearchClient interface {
	Ping(ctx context.Context) error
	EnsureIndex(ctx context.Context, dims int) error
	BulkIndex(ctx context.Context, docs []ChunkDocument) error
	SearchBM25(ctx context.Context, req *SearchRequest) (*SearchResult, error)
	SearchKNN(ctx context.Context, vector []float32, req *SearchRequest) (*SearchResult, error)
	SearchHybrid(ctx context.Context, vector []float32, req *SearchRequest) (*SearchResult, error)
	DeleteByDocID(ctx context.Context, docID string) error
}

// SearchCacheStore defines cache operations required by Retriever/Indexer.
type SearchCacheStore interface {
	Get(ctx context.Context, req *SearchRequest) (*SearchResult, bool)
	Set(ctx context.Context, req *SearchRequest, result *SearchResult)
	InvalidateByDataset(ctx context.Context, datasetID string)
}
