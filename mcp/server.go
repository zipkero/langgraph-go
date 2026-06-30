// mcp 패키지는 MCP(Model Context Protocol) 서버·클라이언트·어댑터를 제공한다.
// tool/message/core + MCP SDK + 표준 라이브러리만 import하며,
// agent/graph 등 상위 패키지를 import하지 않는다(§28-1 규칙).
package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zipkero/langgraph-go/config"
	"github.com/zipkero/langgraph-go/message"
	"github.com/zipkero/langgraph-go/tool"
)

// Transport 는 MCP 서버 전송 종류를 나타내는 명명된 문자열 타입이다.
type Transport string

const (
	// TransportStdio 는 stdio 기반 전송을 나타낸다.
	TransportStdio Transport = "stdio"
)

// ServerConfig 는 MCP 서버 연결 설정을 담는다.
type ServerConfig struct {
	// Transport 는 서버 전송 종류다(현재 stdio만 지원).
	Transport Transport
	// Command 는 stdio 전송에서 실행할 서버 프로세스 명령이다.
	Command string
	// Args 는 서버 프로세스 명령 인자 목록이다.
	Args []string
	// URL 은 HTTP 전송에서 사용할 서버 URL이다(향후 지원 예정).
	URL string
}

// Server 는 MCP 서버를 감싸는 래퍼다.
// SDK의 *mcp.Server를 비공개 필드로 보관하고, 우리 타입 기반 API만 노출한다.
type Server struct {
	sdkServer *sdkmcp.Server
}

// NewServer 는 name을 이름으로 하는 새 MCP 서버 래퍼를 생성한다.
func NewServer(name string) *Server {
	impl := &sdkmcp.Implementation{Name: name}
	s := sdkmcp.NewServer(impl, nil)
	return &Server{sdkServer: s}
}

// RegisterTool 은 tool.Tool 하나를 MCP 서버에 등록한다.
// 도구의 Schema를 SDK Tool.InputSchema로 역변환하고, Execute를 호출하는 핸들러를 합성한다.
func (s *Server) RegisterTool(t tool.Tool) error {
	schema := t.Schema()
	sdkTool := &sdkmcp.Tool{
		Name:        schema.Name,
		Description: schema.Description,
		InputSchema: toolSchemaToInputSchema(schema),
	}

	// no-op 런타임: 서버 측 도구 실행에는 상태·스토어 없는 최소 런타임을 사용한다.
	rt := tool.NewRuntime(nil, "", config.RunConfig{}, nil, nil)

	handler := func(ctx context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		// 들어온 Arguments를 json.RawMessage로 직렬화해 tool.Execute에 위임한다.
		var rawArgs json.RawMessage
		if req.Params != nil && req.Params.Arguments != nil {
			b, err := json.Marshal(req.Params.Arguments)
			if err != nil {
				return nil, fmt.Errorf("mcp: 인자 직렬화 실패: %w", err)
			}
			rawArgs = b
		}
		res, err := t.Execute(ctx, rawArgs, rt)
		if err != nil {
			// Execute 에러는 프로토콜 에러가 아니라 도구 에러로 처리한다.
			return &sdkmcp.CallToolResult{
				Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: err.Error()}},
				IsError: true,
			}, nil
		}
		return &sdkmcp.CallToolResult{
			Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: res.Content}},
			IsError: res.IsError,
		}, nil
	}

	s.sdkServer.AddTool(sdkTool, handler)
	return nil
}

// RegisterTools 는 tool.Registry의 모든 도구를 MCP 서버에 등록한다.
func (s *Server) RegisterTools(reg *tool.Registry) error {
	for _, t := range reg.List() {
		if err := s.RegisterTool(t); err != nil {
			return err
		}
	}
	return nil
}

// RegisterPrompt 는 name 이름의 정적 프롬프트를 MCP 서버에 등록한다.
// msgs 목록을 역할별 PromptMessage로 변환해 GetPromptResult를 반환하는 핸들러를 합성한다.
func (s *Server) RegisterPrompt(name string, msgs []message.Message) error {
	prompt := &sdkmcp.Prompt{Name: name}

	handler := func(ctx context.Context, req *sdkmcp.GetPromptRequest) (*sdkmcp.GetPromptResult, error) {
		pmMsgs, err := messagesToPromptMessages(msgs)
		if err != nil {
			return nil, err
		}
		return &sdkmcp.GetPromptResult{
			Messages: pmMsgs,
		}, nil
	}

	s.sdkServer.AddPrompt(prompt, handler)
	return nil
}

// ServeStdio 는 stdio 전송으로 MCP 서버를 시작한다. ctx가 취소되거나 stdin이 닫힐 때까지 블로킹한다.
func (s *Server) ServeStdio(ctx context.Context) error {
	return s.sdkServer.Run(ctx, &sdkmcp.StdioTransport{})
}

// toolSchemaToInputSchema 는 tool.Schema의 Parameters를 JSON Schema(map[string]any)로 변환한다.
// Tool.InputSchema 필드가 any 타입이므로 map[string]any로 직접 구성한다.
func toolSchemaToInputSchema(schema tool.Schema) map[string]any {
	properties := make(map[string]any, len(schema.Parameters))
	required := make([]string, 0)

	for _, p := range schema.Parameters {
		prop := map[string]any{
			"type": p.Type,
		}
		if p.Description != "" {
			prop["description"] = p.Description
		}
		properties[p.Name] = prop
		if p.Required {
			required = append(required, p.Name)
		}
	}

	result := map[string]any{
		"type":       "object",
		"properties": properties,
	}
	if len(required) > 0 {
		result["required"] = required
	}
	return result
}

// messagesToPromptMessages 는 []message.Message를 SDK []*PromptMessage로 변환한다.
// MCP 프롬프트 역할은 user/assistant만 지원하며, system/tool 역할은 건너뛴다.
func messagesToPromptMessages(msgs []message.Message) ([]*sdkmcp.PromptMessage, error) {
	result := make([]*sdkmcp.PromptMessage, 0, len(msgs))
	for _, m := range msgs {
		var role sdkmcp.Role
		switch m.Role {
		case message.RoleUser:
			role = "user"
		case message.RoleAssistant:
			role = "assistant"
		default:
			// system/tool 역할은 MCP 프롬프트 범위 밖이므로 건너뛴다.
			continue
		}
		pm := &sdkmcp.PromptMessage{
			Role:    role,
			Content: &sdkmcp.TextContent{Text: m.Content},
		}
		result = append(result, pm)
	}
	return result, nil
}
