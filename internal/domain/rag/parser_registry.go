package rag

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
)

// ParserRegistry 文档解析器注册表
type ParserRegistry struct {
	mu      sync.RWMutex
	parsers map[string]Parser // key = ".ext"
}

// NewParserRegistry 创建解析器注册表并注册内置解析器
func NewParserRegistry() *ParserRegistry {
	r := &ParserRegistry{
		parsers: make(map[string]Parser),
	}

	// 注册内置解析器
	r.Register(&MarkdownParser{})
	r.Register(&PlainTextParser{})
	r.Register(&PDFParser{})
	r.Register(&DOCXParser{})

	return r
}

// Register 注册解析器
func (r *ParserRegistry) Register(p Parser) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, ext := range p.SupportedTypes() {
		r.parsers[strings.ToLower(ext)] = p
	}
}

// Get 根据文件名获取解析器
func (r *ParserRegistry) Get(filename string) (Parser, error) {
	ext := strings.ToLower(filepath.Ext(filename))
	if ext == "" {
		return nil, fmt.Errorf("no file extension in filename: %s", filename)
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	p, ok := r.parsers[ext]
	if !ok {
		return nil, fmt.Errorf("unsupported file type: %s (supported: %s)", ext, r.SupportedTypes())
	}
	return p, nil
}

// SupportedTypes 返回所有支持的文件扩展名
func (r *ParserRegistry) SupportedTypes() string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	seen := make(map[string]bool)
	var types []string
	for ext := range r.parsers {
		if !seen[ext] {
			seen[ext] = true
			types = append(types, ext)
		}
	}
	return strings.Join(types, ", ")
}
