// client.go 는 단일 MCP 서버 클라이언트 래퍼를 제공한다.
// SDK *Client/*ClientSession을 비공개 필드로 보관하고, 우리 타입 기반 API만 노출한다.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zipkero/langgraph-go/message"
	"github.com/zipkero/langgraph-go/tool"
)

// Client 는 단일 MCP 서버에 대한 연결 래퍼다.
// SDK의 *Client와 *ClientSession을 비공개로 감추고 우리 API만 노출한다.
type Client struct {
	cfg     ServerConfig
	sdkCli  *sdkmcp.Client
	session *sdkmcp.ClientSession
}

// NewClient 는 cfg 설정으로 새 Client를 생성한다.
// 연결은 Connect 호출 시 수행된다.
func NewClient(cfg ServerConfig) *Client {
	sdkCli := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "langgraph-go-client"}, nil)
	return &Client{
		cfg:    cfg,
		sdkCli: sdkCli,
	}
}

// Connect 는 ServerConfig의 전송 종류에 따라 MCP 서버에 연결·초기화한다.
// SDK가 Connect 시 MCP 핸드셰이크를 수행하므로 별도 Initialize 호출이 필요 없다.
func (c *Client) Connect(ctx context.Context) error {
	if c.session != nil {
		return nil // 이미 연결됨
	}

	var transport sdkmcp.Transport
	switch c.cfg.Transport {
	case TransportStdio:
		if c.cfg.Command == "" {
			return fmt.Errorf("mcp: stdio 전송에는 Command가 필요합니다")
		}
		transport = &sdkmcp.CommandTransport{
			Command: exec.Command(c.cfg.Command, c.cfg.Args...),
		}
	case TransportStreamableHTTP:
		if c.cfg.URL == "" {
			return fmt.Errorf("mcp: streamable_http 전송에는 URL이 필요합니다")
		}
		transport = &sdkmcp.StreamableClientTransport{
			Endpoint: c.cfg.URL,
		}
	default:
		return fmt.Errorf("mcp: 지원하지 않는 전송 종류: %q", c.cfg.Transport)
	}

	cs, err := c.sdkCli.Connect(ctx, transport, nil)
	if err != nil {
		return fmt.Errorf("mcp: 서버 연결 실패: %w", err)
	}
	c.session = cs
	return nil
}

// Initialize 는 Connect에서 SDK가 핸드셰이크를 이미 수행하므로 no-op이다.
// API 표면 호환을 위해 메서드를 노출한다.
func (c *Client) Initialize(_ context.Context) error {
	if c.session == nil {
		return fmt.Errorf("mcp: 연결되지 않음 — Connect를 먼저 호출하세요")
	}
	return nil
}

// ListTools 는 원격 MCP 서버의 도구 목록을 우리 tool.Schema 목록으로 반환한다.
func (c *Client) ListTools(ctx context.Context) ([]tool.Schema, error) {
	if c.session == nil {
		return nil, fmt.Errorf("mcp: 연결되지 않음 — Connect를 먼저 호출하세요")
	}

	res, err := c.session.ListTools(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("mcp: ListTools 실패: %w", err)
	}

	schemas := make([]tool.Schema, 0, len(res.Tools))
	for _, t := range res.Tools {
		schemas = append(schemas, sdkToolToSchema(t))
	}
	return schemas, nil
}

// CallTool 은 name 도구를 args 인자로 호출하고 실행 결과를 tool.Result로 반환한다.
func (c *Client) CallTool(ctx context.Context, name string, args json.RawMessage) (tool.Result, error) {
	if c.session == nil {
		return tool.Result{}, fmt.Errorf("mcp: 연결되지 않음 — Connect를 먼저 호출하세요")
	}

	// json.RawMessage를 map[string]any로 언마샬해 Arguments에 싣는다.
	var arguments any
	if len(args) > 0 {
		var m map[string]any
		if err := json.Unmarshal(args, &m); err != nil {
			return tool.Result{}, fmt.Errorf("mcp: 인자 언마샬 실패: %w", err)
		}
		arguments = m
	}

	res, err := c.session.CallTool(ctx, &sdkmcp.CallToolParams{
		Name:      name,
		Arguments: arguments,
	})
	if err != nil {
		return tool.Result{}, fmt.Errorf("mcp: CallTool 실패: %w", err)
	}

	return sdkCallResultToToolResult(res), nil
}

// LoadPrompt 는 name 프롬프트를 args 인자로 가져와 역할별 message.Message 목록으로 반환한다.
func (c *Client) LoadPrompt(ctx context.Context, name string, args map[string]string) ([]message.Message, error) {
	if c.session == nil {
		return nil, fmt.Errorf("mcp: 연결되지 않음 — Connect를 먼저 호출하세요")
	}

	res, err := c.session.GetPrompt(ctx, &sdkmcp.GetPromptParams{
		Name:      name,
		Arguments: args,
	})
	if err != nil {
		return nil, fmt.Errorf("mcp: GetPrompt 실패: %w", err)
	}

	return sdkPromptMessagesToMessages(res.Messages), nil
}

// Close 는 MCP 서버와의 연결을 정리한다.
func (c *Client) Close() error {
	if c.session == nil {
		return nil
	}
	err := c.session.Close()
	c.session = nil
	return err
}

// sdkToolToSchema 는 SDK *Tool을 우리 tool.Schema로 변환한다.
// InputSchema(any, 클라이언트에서는 map[string]any로 수신)의 properties/required를 평탄 순회해
// []tool.Parameter로 옮긴다.
func sdkToolToSchema(t *sdkmcp.Tool) tool.Schema {
	schema := tool.Schema{
		Name:        t.Name,
		Description: t.Description,
	}
	schema.Parameters = extractParameters(t.InputSchema)
	return schema
}

// extractParameters 는 JSON Schema(map[string]any 형태)에서 properties/required를 추출해
// []tool.Parameter로 평탄 변환한다.
func extractParameters(inputSchema any) []tool.Parameter {
	if inputSchema == nil {
		return nil
	}

	schemaMap, ok := inputSchema.(map[string]any)
	if !ok {
		return nil
	}

	propsRaw, ok := schemaMap["properties"]
	if !ok {
		return nil
	}
	propsMap, ok := propsRaw.(map[string]any)
	if !ok {
		return nil
	}

	// required 목록을 집합으로 구성한다.
	requiredSet := make(map[string]bool)
	if reqRaw, ok := schemaMap["required"]; ok {
		switch rv := reqRaw.(type) {
		case []string:
			for _, r := range rv {
				requiredSet[r] = true
			}
		case []any:
			for _, r := range rv {
				if s, ok := r.(string); ok {
					requiredSet[s] = true
				}
			}
		}
	}

	params := make([]tool.Parameter, 0, len(propsMap))
	for name, propRaw := range propsMap {
		propMap, ok := propRaw.(map[string]any)
		if !ok {
			continue
		}
		p := tool.Parameter{
			Name:     name,
			Required: requiredSet[name],
		}
		if typ, ok := propMap["type"].(string); ok {
			p.Type = typ
		}
		if desc, ok := propMap["description"].(string); ok {
			p.Description = desc
		}
		params = append(params, p)
	}
	return params
}

// sdkCallResultToToolResult 는 SDK *CallToolResult를 우리 tool.Result로 변환한다.
// TextContent.Text를 이어 붙여 Content로, IsError를 그대로 옮긴다.
func sdkCallResultToToolResult(res *sdkmcp.CallToolResult) tool.Result {
	var sb strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*sdkmcp.TextContent); ok {
			sb.WriteString(tc.Text)
		}
	}
	return tool.Result{
		Content: sb.String(),
		IsError: res.IsError,
	}
}

// sdkPromptMessagesToMessages 는 SDK []*PromptMessage를 우리 []message.Message로 변환한다.
// role은 user/assistant만 지원하며, 그 외 역할은 건너뛴다.
func sdkPromptMessagesToMessages(pms []*sdkmcp.PromptMessage) []message.Message {
	msgs := make([]message.Message, 0, len(pms))
	for _, pm := range pms {
		var text string
		if tc, ok := pm.Content.(*sdkmcp.TextContent); ok {
			text = tc.Text
		}

		switch pm.Role {
		case "user":
			msgs = append(msgs, message.NewUserMessage(text))
		case "assistant":
			msgs = append(msgs, message.NewAssistantMessage(text))
		// 그 외 역할은 MCP 프롬프트 범위 밖이므로 건너뛴다.
		}
	}
	return msgs
}
