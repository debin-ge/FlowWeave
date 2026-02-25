package rag

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// Chunker 文档分块器
type Chunker struct {
	chunkSize int // 每块最大字符数
	overlap   int // 块间重叠字符数
}

// NewChunker 创建分块器
func NewChunker(chunkSize, overlap int) *Chunker {
	if chunkSize <= 0 {
		chunkSize = 512
	}
	if overlap < 0 || overlap >= chunkSize {
		overlap = chunkSize / 4
	}
	return &Chunker{
		chunkSize: chunkSize,
		overlap:   overlap,
	}
}

// Chunk 将文本切分为多个 ChunkDocument
func (c *Chunker) Chunk(req *IndexRequest) ([]ChunkDocument, error) {
	if req.Content == "" && len(req.QAPairs) == 0 {
		return nil, fmt.Errorf("content and qa_pairs are both empty")
	}

	docID := uuid.New().String()
	var docs []ChunkDocument
	chunkIdx := 0

	// 1. 文本分块
	if req.Content != "" {
		paragraphs := splitParagraphs(req.Content)
		chunks := c.mergeParagraphs(paragraphs)

		for _, chunk := range chunks {
			docs = append(docs, ChunkDocument{
				DocID:     docID,
				ChunkID:   fmt.Sprintf("%s_chunk_%d", docID, chunkIdx),
				DatasetID: req.DatasetID,
				OrgID:     req.OrgID,
				TenantID:  req.TenantID,
				Title:     req.Title,
				Content:   chunk,
				Tags:      req.Tags,
				Source:    req.Source,
				Metadata:  map[string]string{"type": "text"},
				CreatedAt: time.Now(),
			})
			chunkIdx++
		}
	}

	// 2. QA 对分块（每对独立成 chunk）
	for _, qa := range req.QAPairs {
		if qa.Question == "" {
			continue
		}
		content := fmt.Sprintf("Q: %s\nA: %s", qa.Question, qa.Answer)
		docs = append(docs, ChunkDocument{
			DocID:     docID,
			ChunkID:   fmt.Sprintf("%s_qa_%d", docID, chunkIdx),
			DatasetID: req.DatasetID,
			OrgID:     req.OrgID,
			TenantID:  req.TenantID,
			Title:     qa.Question,
			Content:   content,
			Tags:      req.Tags,
			Source:    req.Source,
			Metadata:  map[string]string{"type": "qa"},
			CreatedAt: time.Now(),
		})
		chunkIdx++
	}

	return docs, nil
}

// splitParagraphs 按段落/句子分割文本，优先中文句号和换行
func splitParagraphs(text string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}

	// 按换行分段
	rawParts := strings.Split(text, "\n")
	var parts []string
	for _, p := range rawParts {
		p = strings.TrimSpace(p)
		if p != "" {
			parts = append(parts, p)
		}
	}
	return parts
}

// mergeParagraphs 将段落合并为不超过 chunkSize 的块，带 overlap
func (c *Chunker) mergeParagraphs(paragraphs []string) []string {
	if len(paragraphs) == 0 {
		return nil
	}

	var chunks []string
	var current strings.Builder

	for _, para := range paragraphs {
		paraLen := utf8.RuneCountInString(para)

		// 段落本身就超过 chunkSize，需要硬切分
		if paraLen > c.chunkSize {
			// 先把 current 存起来
			if current.Len() > 0 {
				chunks = append(chunks, current.String())
				current.Reset()
			}
			// 硬切分大段落
			runes := []rune(para)
			for i := 0; i < len(runes); i += c.chunkSize - c.overlap {
				end := i + c.chunkSize
				if end > len(runes) {
					end = len(runes)
				}
				chunks = append(chunks, string(runes[i:end]))
				if end >= len(runes) {
					break
				}
			}
			continue
		}

		currentLen := utf8.RuneCountInString(current.String())
		if currentLen+paraLen+1 > c.chunkSize {
			// 当前块已满，保存并开始新块
			chunks = append(chunks, current.String())
			// Overlap：取前一块的尾部
			prev := current.String()
			current.Reset()
			if c.overlap > 0 {
				prevRunes := []rune(prev)
				if len(prevRunes) > c.overlap {
					current.WriteString(string(prevRunes[len(prevRunes)-c.overlap:]))
					current.WriteString("\n")
				}
			}
		}

		if current.Len() > 0 {
			current.WriteString("\n")
		}
		current.WriteString(para)
	}

	if current.Len() > 0 {
		chunks = append(chunks, current.String())
	}
	return chunks
}
