// adapter.go 는 MCP 원격 도구·프롬프트를 우리 tool.Tool/message.Message로 감싸는 어댑터를 제공한다.
// RemoteTool은 tool.Tool 구현체로, Execute가 원격 호출에 위임한다(rt 미사용).
// mcp는 tool/message/core + MCP SDK + 표준 라이브러리만 import한다(§28-1 규칙).
package mcp

import (
	"context"
	"fmt"

	"github.com/zipkero/langgraph-go/message"
	"github.com/zipkero/langgraph-go/tool"
)

// RemoteTool 은 MCP 원격 도구 하나를 tool.Tool 인터페이스로 감싸는 구현체다.
// Execute 는 주입된 Runtime을 사용하지 않고 원격 호출에 위임한다.
type RemoteTool struct {
	client *Client
	schema tool.Schema
}

// Name 은 원격 도구 이름을 반환한다.
func (r *RemoteTool) Name() string { return r.schema.Name }

// Description 은 원격 도구 설명을 반환한다.
func (r *RemoteTool) Description() string { return r.schema.Description }

// Schema 는 원격 도구 스키마를 반환한다.
func (r *RemoteTool) Schema() tool.Schema { return r.schema }

// Execute 는 입력을 그대로 원격 호출에 위임하고 결과를 반환한다.
// rt(Runtime)는 원격 호출에서 사용하지 않으며, tool.Tool 인터페이스 시그니처 충족을 위해 받는다.
func (r *RemoteTool) Execute(ctx context.Context, input tool.Input, rt tool.Runtime) (tool.Result, error) {
	res, err := r.client.CallTool(ctx, r.schema.Name, input)
	if err != nil {
		return tool.Result{}, fmt.Errorf("mcp: 원격 도구 %q 실행 실패: %w", r.schema.Name, err)
	}
	return res, nil
}

// WrapMCPTool 은 client와 단일 스키마 s를 받아 RemoteTool(tool.Tool 구현체)을 반환한다.
func WrapMCPTool(client *Client, s tool.Schema) tool.Tool {
	return &RemoteTool{
		client: client,
		schema: s,
	}
}

// LoadMCPTools 는 client에서 원격 도구 목록을 가져와 각 스키마를 WrapMCPTool로 감싼
// []tool.Tool 슬라이스를 반환한다.
func LoadMCPTools(ctx context.Context, client *Client) ([]tool.Tool, error) {
	schemas, err := client.ListTools(ctx)
	if err != nil {
		return nil, fmt.Errorf("mcp: 원격 도구 목록 로드 실패: %w", err)
	}

	tools := make([]tool.Tool, 0, len(schemas))
	for _, s := range schemas {
		tools = append(tools, WrapMCPTool(client, s))
	}
	return tools, nil
}

// LoadMCPPrompt 는 client에서 name 프롬프트를 args 인자로 가져와
// 역할별 []message.Message로 반환한다.
func LoadMCPPrompt(ctx context.Context, client *Client, name string, args map[string]string) ([]message.Message, error) {
	msgs, err := client.LoadPrompt(ctx, name, args)
	if err != nil {
		return nil, fmt.Errorf("mcp: 원격 프롬프트 %q 로드 실패: %w", name, err)
	}
	return msgs, nil
}
