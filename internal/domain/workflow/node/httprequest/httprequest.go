package httprequest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"flowweave/internal/domain/workflow/event"
	types "flowweave/internal/domain/workflow/model"
	"flowweave/internal/domain/workflow/node"
)

// HTTPNodeData HTTP 请求节点配置
type HTTPNodeData struct {
	Type          string            `json:"type"`
	Title         string            `json:"title"`
	Method        string            `json:"method"` // GET, POST, PUT, DELETE, PATCH
	URL           string            `json:"url"`    // 支持模板变量
	Headers       map[string]string `json:"headers,omitempty"`
	Params        map[string]string `json:"params,omitempty"` // Query 参数
	Body          *BodyConfig       `json:"body,omitempty"`
	Authorization *AuthConfig       `json:"authorization,omitempty"`
	Timeout       int               `json:"timeout,omitempty"` // 秒
	MaxRetries    int               `json:"max_retries,omitempty"`
}

// BodyConfig 请求体配置
type BodyConfig struct {
	Type string `json:"type"` // none, form-data, x-www-form-urlencoded, raw-text, json
	Data string `json:"data"` // 支持模板变量
}

// AuthConfig 鉴权配置
type AuthConfig struct {
	Type   string            `json:"type"` // api-key, basic, bearer
	Config map[string]string `json:"config"`
}

// HTTPNode HTTP 请求节点
type HTTPNode struct {
	*node.BaseNode
	data   HTTPNodeData
	client *http.Client
}

func init() {
	node.Register(types.NodeTypeHTTPRequest, NewHTTPNode)
}

// NewHTTPNode 创建 HTTP 请求节点
func NewHTTPNode(id string, rawData json.RawMessage) (node.Node, error) {
	var data HTTPNodeData
	if err := json.Unmarshal(rawData, &data); err != nil {
		return nil, err
	}

	timeout := 30 * time.Second
	if data.Timeout > 0 {
		timeout = time.Duration(data.Timeout) * time.Second
	}

	n := &HTTPNode{
		BaseNode: node.NewBaseNode(id, types.NodeTypeHTTPRequest, data.Title, types.NodeExecutionTypeExecutable),
		data:     data,
		client:   &http.Client{Timeout: timeout},
	}
	return n, nil
}

// Run 执行 HTTP 请求
func (n *HTTPNode) Run(ctx context.Context) (<-chan event.NodeEvent, error) {
	return node.RunWithEvents(ctx, n, func(ctx context.Context) (*node.NodeRunResult, error) {
		vp, _ := node.GetVariablePoolFromContext(ctx)

		// 1. 解析 URL 中的模板变量
		url := n.data.URL
		if vp != nil {
			url = resolveTemplate(url, vp)
		}

		// 2. 添加 Query 参数
		if len(n.data.Params) > 0 {
			sep := "?"
			if strings.Contains(url, "?") {
				sep = "&"
			}
			for k, v := range n.data.Params {
				if vp != nil {
					v = resolveTemplate(v, vp)
				}
				url += sep + k + "=" + v
				sep = "&"
			}
		}

		// 3. 构建请求体
		var bodyReader io.Reader
		if n.data.Body != nil && n.data.Body.Type != "none" {
			bodyData := n.data.Body.Data
			if vp != nil {
				bodyData = resolveTemplate(bodyData, vp)
			}
			bodyReader = bytes.NewBufferString(bodyData)
		}

		// 4. 创建 HTTP 请求
		method := strings.ToUpper(n.data.Method)
		if method == "" {
			method = "GET"
		}

		req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
		if err != nil {
			return nil, fmt.Errorf("create request: %w", err)
		}

		// 5. 设置 Headers
		for k, v := range n.data.Headers {
			if vp != nil {
				v = resolveTemplate(v, vp)
			}
			req.Header.Set(k, v)
		}

		// 默认 Content-Type
		if req.Header.Get("Content-Type") == "" && bodyReader != nil {
			if n.data.Body != nil {
				switch n.data.Body.Type {
				case "json":
					req.Header.Set("Content-Type", "application/json")
				case "x-www-form-urlencoded":
					req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				default:
					req.Header.Set("Content-Type", "text/plain")
				}
			}
		}

		// 6. 设置鉴权
		if n.data.Authorization != nil {
			n.applyAuth(req)
		}

		// 7. 发送请求（带重试）
		var resp *http.Response
		maxRetries := n.data.MaxRetries
		if maxRetries <= 0 {
			maxRetries = 1
		}

		for attempt := 0; attempt < maxRetries; attempt++ {
			resp, err = n.client.Do(req)
			if err == nil {
				break
			}
			if attempt < maxRetries-1 {
				time.Sleep(time.Duration(attempt+1) * time.Second)
			}
		}
		if err != nil {
			return nil, fmt.Errorf("HTTP request failed after %d attempts: %w", maxRetries, err)
		}
		defer resp.Body.Close()

		// 8. 读取响应
		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("read response: %w", err)
		}

		// 9. 构建输出
		outputs := map[string]interface{}{
			"status_code": resp.StatusCode,
			"body":        string(respBody),
			"headers":     headerToMap(resp.Header),
		}

		// 尝试解析 JSON 响应
		var jsonBody interface{}
		if err := json.Unmarshal(respBody, &jsonBody); err == nil {
			outputs["json"] = jsonBody
		}

		status := types.NodeExecutionStatusSucceeded
		if resp.StatusCode >= 400 {
			return &node.NodeRunResult{
				Status:  types.NodeExecutionStatusFailed,
				Error:   fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(respBody)),
				Outputs: outputs,
			}, nil
		}

		return &node.NodeRunResult{
			Status:  status,
			Outputs: outputs,
		}, nil
	})
}

func (n *HTTPNode) applyAuth(req *http.Request) {
	if n.data.Authorization == nil {
		return
	}
	switch n.data.Authorization.Type {
	case "bearer":
		token := n.data.Authorization.Config["token"]
		req.Header.Set("Authorization", "Bearer "+token)
	case "basic":
		user := n.data.Authorization.Config["username"]
		pass := n.data.Authorization.Config["password"]
		req.SetBasicAuth(user, pass)
	case "api-key":
		key := n.data.Authorization.Config["key"]
		value := n.data.Authorization.Config["value"]
		headerName := n.data.Authorization.Config["header"]
		if headerName == "" {
			headerName = key
		}
		req.Header.Set(headerName, value)
	}
}

func headerToMap(h http.Header) map[string]string {
	result := make(map[string]string, len(h))
	for k := range h {
		result[k] = h.Get(k)
	}
	return result
}

// resolveTemplate 解析模板中的 {{#node_id.var_name#}} 引用
func resolveTemplate(template string, vp node.VariablePoolAccessor) string {
	var result strings.Builder
	i := 0
	for i < len(template) {
		if i+3 <= len(template) && template[i:i+3] == "{{#" {
			end := strings.Index(template[i+3:], "#}}")
			if end >= 0 {
				ref := template[i+3 : i+3+end]
				parts := strings.SplitN(ref, ".", 2)
				if len(parts) == 2 {
					val, ok := vp.GetVariable(types.VariableSelector(parts))
					if ok {
						result.WriteString(fmt.Sprintf("%v", val))
					}
				}
				i = i + 3 + end + 3
				continue
			}
		}
		result.WriteByte(template[i])
		i++
	}
	return result.String()
}
