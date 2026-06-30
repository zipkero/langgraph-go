// multi_server.go 는 여러 MCP 서버를 묶어 도구·프롬프트를 통합 조회하는 MultiServerClient를 제공한다.
// mcp는 tool/message/core + MCP SDK + 표준 라이브러리만 import한다(§28-1 규칙).
package mcp

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/zipkero/langgraph-go/message"
	"github.com/zipkero/langgraph-go/tool"
)

// ErrServerNotFound 는 요청한 서버 이름이 MultiServerClient에 등록되지 않았을 때 반환하는 센티널 에러다.
// IsServerNotFound(err)로 판별할 수 있다.
var ErrServerNotFound = errors.New("mcp: 등록되지 않은 서버 이름")

// serverNotFoundError 는 어떤 서버 이름이 없는지를 포함하는 not-found 에러다.
// errors.Is(err, ErrServerNotFound)로 판정 가능하다.
type serverNotFoundError struct {
	name string
}

func (e *serverNotFoundError) Error() string {
	return fmt.Sprintf("mcp: 등록되지 않은 서버 이름 %q", e.name)
}

// Unwrap 은 ErrServerNotFound를 래핑해 errors.Is 판정이 동작하게 한다.
func (e *serverNotFoundError) Unwrap() error {
	return ErrServerNotFound
}

// IsServerNotFound 는 err가 ErrServerNotFound 계열인지 판별한다.
func IsServerNotFound(err error) bool {
	return errors.Is(err, ErrServerNotFound)
}

// MultiServerClient 는 여러 MCP 서버 클라이언트를 이름으로 묶어 통합 도구 조회·서버별 조회·프롬프트 조회를 제공한다.
// clients 맵과 order 슬라이스는 비공개로 보관하며, 외부에 SDK 타입을 노출하지 않는다.
type MultiServerClient struct {
	clients map[string]*Client // 서버 이름 → *Client 맵
	order   []string           // 등록 순서(이름 sort 기준으로 결정적)
}

// NewMultiServerClient 는 servers 맵의 각 ServerConfig로 Client를 만들고 Connect를 수행한 뒤
// MultiServerClient를 반환한다. 서버 이름은 sort해 통합 조회 순서를 결정적으로 만든다.
// 어느 서버의 Connect라도 실패하면 에러를 반환한다.
func NewMultiServerClient(ctx context.Context, servers map[string]ServerConfig) (*MultiServerClient, error) {
	order := make([]string, 0, len(servers))
	for name := range servers {
		order = append(order, name)
	}
	sort.Strings(order)

	clients := make(map[string]*Client, len(servers))
	for _, name := range order {
		cfg := servers[name]
		c := NewClient(cfg)
		if err := c.Connect(ctx); err != nil {
			return nil, fmt.Errorf("mcp: 서버 %q 연결 실패: %w", name, err)
		}
		clients[name] = c
	}

	return &MultiServerClient{
		clients: clients,
		order:   order,
	}, nil
}

// GetTools 는 등록 순서(이름 sort 기준)대로 각 서버의 도구를 이어 붙여 통합 []tool.Tool을 반환한다.
// 어느 서버의 도구 조회라도 실패하면 에러를 반환한다.
func (m *MultiServerClient) GetTools(ctx context.Context) ([]tool.Tool, error) {
	var result []tool.Tool
	for _, name := range m.order {
		c := m.clients[name]
		tools, err := LoadMCPTools(ctx, c)
		if err != nil {
			return nil, fmt.Errorf("mcp: 서버 %q 도구 조회 실패: %w", name, err)
		}
		result = append(result, tools...)
	}
	return result, nil
}

// GetToolsByServer 는 server 이름에 해당하는 서버의 도구만 []tool.Tool로 반환한다.
// 미등록 서버 이름이면 IsServerNotFound(err)가 true인 에러를 반환한다.
func (m *MultiServerClient) GetToolsByServer(ctx context.Context, server string) ([]tool.Tool, error) {
	c, ok := m.clients[server]
	if !ok {
		return nil, &serverNotFoundError{name: server}
	}
	tools, err := LoadMCPTools(ctx, c)
	if err != nil {
		return nil, fmt.Errorf("mcp: 서버 %q 도구 조회 실패: %w", server, err)
	}
	return tools, nil
}

// GetPrompt 는 server 이름에 해당하는 서버에서 name 프롬프트를 args 인자로 가져와
// 역할별 []message.Message로 반환한다.
// 미등록 서버 이름이면 IsServerNotFound(err)가 true인 에러를 반환한다.
func (m *MultiServerClient) GetPrompt(ctx context.Context, server, name string, args map[string]string) ([]message.Message, error) {
	c, ok := m.clients[server]
	if !ok {
		return nil, &serverNotFoundError{name: server}
	}
	msgs, err := c.LoadPrompt(ctx, name, args)
	if err != nil {
		return nil, fmt.Errorf("mcp: 서버 %q 프롬프트 %q 조회 실패: %w", server, name, err)
	}
	return msgs, nil
}
