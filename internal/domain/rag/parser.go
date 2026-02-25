package rag

import (
	applog "flowweave/internal/platform/log"
	"bufio"
	"bytes"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ledongthuc/pdf"
	"github.com/nguyenthenguyen/docx"
)

// ── Parser 接口 ───────────────────────────────────────────────

// ParseResult 文档解析结果
type ParseResult struct {
	Content  string            `json:"content"`
	Metadata map[string]string `json:"metadata,omitempty"`
	Pages    int               `json:"pages,omitempty"`
}

// Parser 文档解析器接口
type Parser interface {
	// Parse 解析文档，返回纯文本内容
	Parse(reader io.Reader, filename string) (*ParseResult, error)
	// SupportedTypes 支持的文件扩展名
	SupportedTypes() []string
}

// ── Markdown Parser ──────────────────────────────────────────

// MarkdownParser 去除 Markdown 格式标记
type MarkdownParser struct{}

var (
	reMarkdownHeader = regexp.MustCompile(`(?m)^#{1,6}\s+`)
	reMarkdownBold   = regexp.MustCompile(`\*\*(.+?)\*\*`)
	reMarkdownItalic = regexp.MustCompile(`\*(.+?)\*`)
	reMarkdownCode   = regexp.MustCompile("```[\\s\\S]*?```")
	reMarkdownInline = regexp.MustCompile("`([^`]+)`")
	reMarkdownLink   = regexp.MustCompile(`\[([^\]]+)\]\([^)]+\)`)
	reMarkdownImage  = regexp.MustCompile(`!\[([^\]]*)\]\([^)]+\)`)
	reMarkdownHTML   = regexp.MustCompile(`<[^>]+>`)
)

func (p *MarkdownParser) SupportedTypes() []string {
	return []string{".md", ".markdown"}
}

func (p *MarkdownParser) Parse(reader io.Reader, filename string) (*ParseResult, error) {
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("read markdown: %w", err)
	}

	text := string(data)

	// 提取标题（第一个 # 标题）
	title := ""
	lines := strings.SplitN(text, "\n", 10)
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# ") {
			title = strings.TrimPrefix(line, "# ")
			break
		}
	}

	// 去除代码块
	text = reMarkdownCode.ReplaceAllStringFunc(text, func(s string) string {
		// 保留代码内容，去除 ``` 标记
		s = strings.TrimPrefix(s, "```")
		idx := strings.Index(s, "\n")
		if idx >= 0 {
			s = s[idx+1:]
		}
		s = strings.TrimSuffix(s, "```")
		return strings.TrimSpace(s)
	})

	// 去除格式标记
	text = reMarkdownImage.ReplaceAllString(text, "$1")
	text = reMarkdownLink.ReplaceAllString(text, "$1")
	text = reMarkdownBold.ReplaceAllString(text, "$1")
	text = reMarkdownItalic.ReplaceAllString(text, "$1")
	text = reMarkdownInline.ReplaceAllString(text, "$1")
	text = reMarkdownHeader.ReplaceAllString(text, "")
	text = reMarkdownHTML.ReplaceAllString(text, "")

	// 清理多余空行
	text = cleanExtraNewlines(text)

	meta := map[string]string{"format": "markdown"}
	if title != "" {
		meta["title"] = title
	}

	return &ParseResult{
		Content:  strings.TrimSpace(text),
		Metadata: meta,
	}, nil
}

// ── Plain Text Parser ────────────────────────────────────────

// PlainTextParser 纯文本/CSV 解析
type PlainTextParser struct{}

func (p *PlainTextParser) SupportedTypes() []string {
	return []string{".txt", ".text", ".csv", ".log", ".json", ".xml", ".yaml", ".yml"}
}

func (p *PlainTextParser) Parse(reader io.Reader, filename string) (*ParseResult, error) {
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("read text: %w", err)
	}

	ext := strings.ToLower(filepath.Ext(filename))
	return &ParseResult{
		Content:  strings.TrimSpace(string(data)),
		Metadata: map[string]string{"format": ext},
	}, nil
}

// ── PDF Parser ───────────────────────────────────────────────

// PDFParser 提取 PDF 文本
type PDFParser struct{}

func (p *PDFParser) SupportedTypes() []string {
	return []string{".pdf"}
}

func (p *PDFParser) Parse(reader io.Reader, filename string) (*ParseResult, error) {
	// pdf 库需要 io.ReaderAt + size，先读到内存
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("read pdf data: %w", err)
	}

	r, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("open pdf: %w", err)
	}

	pages := r.NumPage()
	var sb strings.Builder

	for i := 1; i <= pages; i++ {
		page := r.Page(i)
		if page.V.IsNull() {
			continue
		}
		text, err := page.GetPlainText(nil)
		if err != nil {
			applog.Warn("[RAG/PDF] Failed to extract page text", "page", i, "error", err)
			continue
		}
		if text = strings.TrimSpace(text); text != "" {
			sb.WriteString(text)
			sb.WriteString("\n\n")
		}
	}

	content := cleanExtraNewlines(sb.String())

	return &ParseResult{
		Content: strings.TrimSpace(content),
		Pages:   pages,
		Metadata: map[string]string{
			"format": "pdf",
			"pages":  fmt.Sprintf("%d", pages),
		},
	}, nil
}

// ── DOCX Parser ──────────────────────────────────────────────

// DOCXParser 提取 Word 文档文本
type DOCXParser struct{}

func (p *DOCXParser) SupportedTypes() []string {
	return []string{".docx"}
}

func (p *DOCXParser) Parse(reader io.Reader, filename string) (*ParseResult, error) {
	// docx 库需要文件路径或 io.ReaderAt；先写入临时文件
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("read docx data: %w", err)
	}

	// 使用 ReadDocxFromMemory
	r, err := docx.ReadDocxFromMemory(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("open docx: %w", err)
	}
	defer r.Close()

	// 提取所有段落文本
	var sb strings.Builder
	editable := r.Editable()
	content := editable.GetContent()

	// docx 返回 XML，需要提取纯文本
	// 简单方式：扫描 <w:t> 标签内容
	scanner := bufio.NewScanner(strings.NewReader(content))
	scanner.Split(bufio.ScanLines)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			sb.WriteString(line)
			sb.WriteString("\n")
		}
	}

	text := cleanExtraNewlines(sb.String())

	return &ParseResult{
		Content:  strings.TrimSpace(text),
		Metadata: map[string]string{"format": "docx"},
	}, nil
}

// ── 辅助函数 ─────────────────────────────────────────────────

var reMultiNewlines = regexp.MustCompile(`\n{3,}`)

func cleanExtraNewlines(text string) string {
	return reMultiNewlines.ReplaceAllString(text, "\n\n")
}
