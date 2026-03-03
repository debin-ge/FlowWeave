package textsemanticsplit

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"flowweave/internal/domain/workflow/node/code"
)

const (
	defaultChunkSize = 800
	defaultOverlap   = 60
)

type function struct{}

func (f *function) Name() string {
	return "text.semantic_split.v1"
}

func (f *function) Execute(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error) {
	args, err := parseArgs(input)
	if err != nil {
		return nil, err
	}

	text, err := extractText(args)
	if err != nil {
		return nil, err
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, fmt.Errorf("args.text must not be empty")
	}

	chunkSize := getInt(args, "chunk_size", defaultChunkSize)
	overlap := getInt(args, "overlap", defaultOverlap)

	if chunkSize < 200 {
		chunkSize = defaultChunkSize
	}
	if overlap < 0 || overlap >= chunkSize {
		overlap = chunkSize / 4
	}

	chunks := splitSemantically(text, chunkSize, overlap)
	if len(chunks) == 0 {
		chunks = []string{text}
	}

	maxLen := 0
	minLen := 0
	if len(chunks) > 0 {
		minLen = utf8.RuneCountInString(chunks[0])
	}
	for _, c := range chunks {
		l := utf8.RuneCountInString(c)
		if l > maxLen {
			maxLen = l
		}
		if l < minLen {
			minLen = l
		}
	}

	return map[string]interface{}{
		"chunks":         chunks,
		"chunk_count":    len(chunks),
		"split_strategy": "paragraph_sentence_overlap",
		"stats": map[string]interface{}{
			"chunk_size": chunkSize,
			"overlap":    overlap,
			"max_len":    maxLen,
			"min_len":    minLen,
		},
	}, nil
}

func parseArgs(input map[string]interface{}) (map[string]interface{}, error) {
	raw, ok := input["args"]
	if !ok {
		return nil, fmt.Errorf("missing required input: args")
	}
	args, ok := raw.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("input args must be object, got %T", raw)
	}
	return args, nil
}

func getRequiredString(args map[string]interface{}, key string) (string, error) {
	raw, ok := args[key]
	if !ok {
		return "", fmt.Errorf("missing required args field: %s", key)
	}
	val, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("args.%s must be string, got %T", key, raw)
	}
	return val, nil
}

func extractText(args map[string]interface{}) (string, error) {
	if text, err := getRequiredString(args, "text"); err == nil {
		return text, nil
	}

	raw, ok := args["current_text"]
	if !ok {
		return "", fmt.Errorf("missing required args field: text (or current_text)")
	}

	switch v := raw.(type) {
	case string:
		return v, nil
	case map[string]interface{}:
		if s, ok := v["text"].(string); ok {
			return s, nil
		}
		return "", fmt.Errorf("args.current_text object must contain string field 'text'")
	default:
		return "", fmt.Errorf("args.current_text must be string or object, got %T", raw)
	}
}

func getInt(args map[string]interface{}, key string, def int) int {
	raw, ok := args[key]
	if !ok {
		return def
	}
	switch v := raw.(type) {
	case int:
		return v
	case int8:
		return int(v)
	case int16:
		return int(v)
	case int32:
		return int(v)
	case int64:
		return int(v)
	case uint:
		return int(v)
	case uint8:
		return int(v)
	case uint16:
		return int(v)
	case uint32:
		return int(v)
	case uint64:
		return int(v)
	case float32:
		return int(v)
	case float64:
		return int(v)
	default:
		return def
	}
}

func splitSemantically(text string, chunkSize, overlap int) []string {
	paragraphs := splitParagraphs(text)
	if len(paragraphs) == 0 {
		return nil
	}

	units := make([]string, 0, len(paragraphs))
	for _, p := range paragraphs {
		if utf8.RuneCountInString(p) > chunkSize {
			units = append(units, splitSentences(p)...)
			continue
		}
		units = append(units, p)
	}

	return mergeUnits(units, chunkSize, overlap)
}

func splitParagraphs(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	lines := strings.Split(text, "\n")
	parts := make([]string, 0, len(lines))
	var cur strings.Builder
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if trim == "" {
			if cur.Len() > 0 {
				parts = append(parts, strings.TrimSpace(cur.String()))
				cur.Reset()
			}
			continue
		}
		if cur.Len() > 0 {
			cur.WriteString("\n")
		}
		cur.WriteString(trim)
	}
	if cur.Len() > 0 {
		parts = append(parts, strings.TrimSpace(cur.String()))
	}
	return parts
}

func splitSentences(text string) []string {
	var out []string
	var cur strings.Builder
	for _, r := range text {
		cur.WriteRune(r)
		if isSentencePunctuation(r) {
			s := strings.TrimSpace(cur.String())
			if s != "" {
				out = append(out, s)
			}
			cur.Reset()
		}
	}
	if cur.Len() > 0 {
		s := strings.TrimSpace(cur.String())
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func isSentencePunctuation(r rune) bool {
	switch r {
	case '。', '！', '？', '；', '.', '!', '?', ';':
		return true
	default:
		return false
	}
}

func mergeUnits(units []string, chunkSize, overlap int) []string {
	chunks := make([]string, 0, len(units))
	var cur strings.Builder

	for _, unit := range units {
		unit = strings.TrimSpace(unit)
		if unit == "" {
			continue
		}
		curLen := utf8.RuneCountInString(cur.String())
		uLen := utf8.RuneCountInString(unit)

		if curLen == 0 && uLen > chunkSize {
			chunks = append(chunks, splitHard(unit, chunkSize, overlap)...)
			continue
		}

		if curLen+uLen+1 > chunkSize {
			if cur.Len() > 0 {
				committed := strings.TrimSpace(cur.String())
				if committed != "" {
					chunks = append(chunks, committed)
				}
			}
			prevTail := tailRunes(cur.String(), overlap)
			cur.Reset()
			if prevTail != "" {
				cur.WriteString(prevTail)
				cur.WriteString("\n")
			}
		}

		if cur.Len() > 0 {
			cur.WriteString("\n")
		}
		cur.WriteString(unit)
	}

	if cur.Len() > 0 {
		committed := strings.TrimSpace(cur.String())
		if committed != "" {
			chunks = append(chunks, committed)
		}
	}
	return chunks
}

func splitHard(text string, chunkSize, overlap int) []string {
	runes := []rune(text)
	step := chunkSize - overlap
	if step < 1 {
		step = chunkSize
	}
	out := make([]string, 0, (len(runes)/chunkSize)+1)
	for i := 0; i < len(runes); i += step {
		end := i + chunkSize
		if end > len(runes) {
			end = len(runes)
		}
		out = append(out, string(runes[i:end]))
		if end == len(runes) {
			break
		}
	}
	return out
}

func tailRunes(text string, n int) string {
	if n <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= n {
		return strings.TrimSpace(text)
	}
	return strings.TrimSpace(string(runes[len(runes)-n:]))
}

func init() {
	code.MustRegisterFunction(&function{})
}
