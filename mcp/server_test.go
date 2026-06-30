// server_test.go 는 task-001 검증 조건을 단정하는 단위 테스트를 담는다.
// 인메모리 전송으로 서버와 클라이언트를 같은 프로세스에서 연결해 외부 프로세스·네트워크 없이 검증한다.
// 내부 패키지 테스트로 sdkServer 필드에 직접 접근한다.
package mcp

import (
	"context"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zipkero/langgraph-go/message"
	"github.com/zipkero/langgraph-go/tool"
)

// makeEchoTool 은 msg 필드를 그대로 Content로 반환하는 에코 도구를 생성한다.
func makeEchoTool(name, desc string) tool.Tool {
	type echoArgs struct {
		Msg string `json:"msg" description:"에코할 메시지"`
	}
	return tool.WithArgsSchema(name, desc, func(ctx context.Context, args echoArgs, rt tool.Runtime) (tool.Result, error) {
		return tool.Result{Content: args.Msg, IsError: false}, nil
	})
}

// makeErrorTool 은 항상 IsError=true 결과를 반환하는 도구를 생성한다.
func makeErrorTool(name, desc string) tool.Tool {
	type errorArgs struct {
		Msg string `json:"msg" description:"에러 메시지"`
	}
	return tool.WithArgsSchema(name, desc, func(ctx context.Context, args errorArgs, rt tool.Runtime) (tool.Result, error) {
		return tool.Result{Content: "에러: " + args.Msg, IsError: true}, nil
	})
}

// connectTestClient 는 서버와 인메모리 클라이언트 세션을 연결해 반환한다.
// 서버는 별도 고루틴에서 실행되며, cancel을 호출하면 서버가 종료된다.
func connectTestClient(t *testing.T, srv *Server) (*sdkmcp.ClientSession, context.CancelFunc) {
	t.Helper()
	serverTransport, clientTransport := sdkmcp.NewInMemoryTransports()

	ctx, cancel := context.WithCancel(context.Background())

	// 서버를 별도 고루틴에서 실행한다. Run은 ctx 취소 시 반환한다.
	go func() {
		_ = srv.sdkServer.Run(ctx, serverTransport)
	}()

	sdkClient := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "test-client"}, nil)
	cs, err := sdkClient.Connect(ctx, clientTransport, nil)
	if err != nil {
		cancel()
		t.Fatalf("클라이언트 연결 실패: %v", err)
	}
	return cs, cancel
}

// TestRegisterTool_도구가_목록에_노출된다 는 RegisterTool로 등록한 도구가
// ListTools 결과에 이름·설명·스키마와 함께 포함되는지 검증한다.
func TestRegisterTool_도구가_목록에_노출된다(t *testing.T) {
	srv := NewServer("test-srv")
	if err := srv.RegisterTool(makeEchoTool("echo", "에코 도구")); err != nil {
		t.Fatalf("RegisterTool 실패: %v", err)
	}

	cs, cancel := connectTestClient(t, srv)
	defer cancel()
	defer cs.Close()

	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools 실패: %v", err)
	}

	found := false
	for _, tl := range res.Tools {
		if tl.Name == "echo" {
			found = true
			if tl.Description != "에코 도구" {
				t.Errorf("설명 불일치: 기대 %q, 실제 %q", "에코 도구", tl.Description)
			}
			if tl.InputSchema == nil {
				t.Error("InputSchema가 nil임")
			}
		}
	}
	if !found {
		t.Errorf("등록한 도구 %q를 목록에서 찾지 못함", "echo")
	}
}

// TestRegisterTool_정상_호출_결과 는 CallTool이 tool.Execute 결과의 Content와
// IsError=false를 그대로 돌려주는지 검증한다.
func TestRegisterTool_정상_호출_결과(t *testing.T) {
	srv := NewServer("test-srv")
	if err := srv.RegisterTool(makeEchoTool("echo", "에코")); err != nil {
		t.Fatalf("RegisterTool 실패: %v", err)
	}

	cs, cancel := connectTestClient(t, srv)
	defer cancel()
	defer cs.Close()

	res, err := cs.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name:      "echo",
		Arguments: map[string]any{"msg": "hello mcp"},
	})
	if err != nil {
		t.Fatalf("CallTool 실패: %v", err)
	}
	if res.IsError {
		t.Error("IsError가 true여야 하지 않음")
	}
	if len(res.Content) == 0 {
		t.Fatal("Content가 비어있음")
	}
	tc, ok := res.Content[0].(*sdkmcp.TextContent)
	if !ok {
		t.Fatalf("Content[0]가 *TextContent가 아님: %T", res.Content[0])
	}
	if tc.Text != "hello mcp" {
		t.Errorf("Content 불일치: 기대 %q, 실제 %q", "hello mcp", tc.Text)
	}
}

// TestRegisterTool_에러_호출_결과 는 IsError=true를 반환하는 도구를 호출했을 때
// CallToolResult.IsError가 true이고 Content에 에러 텍스트가 담기는지 검증한다.
func TestRegisterTool_에러_호출_결과(t *testing.T) {
	srv := NewServer("test-srv")
	if err := srv.RegisterTool(makeErrorTool("fail-tool", "에러 도구")); err != nil {
		t.Fatalf("RegisterTool 실패: %v", err)
	}

	cs, cancel := connectTestClient(t, srv)
	defer cancel()
	defer cs.Close()

	res, err := cs.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name:      "fail-tool",
		Arguments: map[string]any{"msg": "something"},
	})
	if err != nil {
		t.Fatalf("CallTool 실패: %v", err)
	}
	if !res.IsError {
		t.Error("IsError가 true여야 함")
	}
	if len(res.Content) == 0 {
		t.Fatal("Content가 비어있음")
	}
	tc, ok := res.Content[0].(*sdkmcp.TextContent)
	if !ok {
		t.Fatalf("Content[0]가 *TextContent가 아님: %T", res.Content[0])
	}
	if tc.Text != "에러: something" {
		t.Errorf("Content 불일치: 기대 %q, 실제 %q", "에러: something", tc.Text)
	}
}

// TestRegisterTools_레지스트리_도구_모두_노출 는 RegisterTools로 레지스트리의
// 모든 도구가 ListTools에 나타나는지 검증한다.
func TestRegisterTools_레지스트리_도구_모두_노출(t *testing.T) {
	reg := tool.NewRegistry()
	if err := reg.Register(makeEchoTool("tool-a", "A 도구")); err != nil {
		t.Fatalf("Register 실패: %v", err)
	}
	if err := reg.Register(makeEchoTool("tool-b", "B 도구")); err != nil {
		t.Fatalf("Register 실패: %v", err)
	}

	srv := NewServer("test-srv")
	if err := srv.RegisterTools(reg); err != nil {
		t.Fatalf("RegisterTools 실패: %v", err)
	}

	cs, cancel := connectTestClient(t, srv)
	defer cancel()
	defer cs.Close()

	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools 실패: %v", err)
	}

	names := make(map[string]bool)
	for _, tl := range res.Tools {
		names[tl.Name] = true
	}
	for _, expected := range []string{"tool-a", "tool-b"} {
		if !names[expected] {
			t.Errorf("도구 %q가 목록에 없음", expected)
		}
	}
}

// TestRegisterPrompt_역할별_메시지 는 RegisterPrompt로 등록한 프롬프트가
// GetPrompt에서 역할별 메시지로 반환되는지 검증한다.
func TestRegisterPrompt_역할별_메시지(t *testing.T) {
	msgs := []message.Message{
		message.NewUserMessage("안녕하세요"),
		message.NewAssistantMessage("무엇을 도와드릴까요?"),
	}

	srv := NewServer("test-srv")
	if err := srv.RegisterPrompt("greeting", msgs); err != nil {
		t.Fatalf("RegisterPrompt 실패: %v", err)
	}

	cs, cancel := connectTestClient(t, srv)
	defer cancel()
	defer cs.Close()

	res, err := cs.GetPrompt(context.Background(), &sdkmcp.GetPromptParams{
		Name: "greeting",
	})
	if err != nil {
		t.Fatalf("GetPrompt 실패: %v", err)
	}

	if len(res.Messages) != 2 {
		t.Fatalf("메시지 수 불일치: 기대 2, 실제 %d", len(res.Messages))
	}

	// 첫 번째: user 역할
	if res.Messages[0].Role != "user" {
		t.Errorf("Messages[0].Role 불일치: 기대 %q, 실제 %q", "user", res.Messages[0].Role)
	}
	tc0, ok := res.Messages[0].Content.(*sdkmcp.TextContent)
	if !ok {
		t.Fatalf("Messages[0].Content가 *TextContent가 아님: %T", res.Messages[0].Content)
	}
	if tc0.Text != "안녕하세요" {
		t.Errorf("Messages[0].Content 불일치: 기대 %q, 실제 %q", "안녕하세요", tc0.Text)
	}

	// 두 번째: assistant 역할
	if res.Messages[1].Role != "assistant" {
		t.Errorf("Messages[1].Role 불일치: 기대 %q, 실제 %q", "assistant", res.Messages[1].Role)
	}
	tc1, ok := res.Messages[1].Content.(*sdkmcp.TextContent)
	if !ok {
		t.Fatalf("Messages[1].Content가 *TextContent가 아님: %T", res.Messages[1].Content)
	}
	if tc1.Text != "무엇을 도와드릴까요?" {
		t.Errorf("Messages[1].Content 불일치: 기대 %q, 실제 %q", "무엇을 도와드릴까요?", tc1.Text)
	}
}

// TestRegisterPrompt_시스템_역할_건너뜀 은 system 역할 메시지가 MCP 프롬프트에서
// 제외되는지 검증한다(MCP 프롬프트는 user/assistant만 지원).
func TestRegisterPrompt_시스템_역할_건너뜀(t *testing.T) {
	msgs := []message.Message{
		message.NewSystemMessage("시스템 프롬프트"),
		message.NewUserMessage("사용자 질문"),
	}

	srv := NewServer("test-srv")
	if err := srv.RegisterPrompt("mixed", msgs); err != nil {
		t.Fatalf("RegisterPrompt 실패: %v", err)
	}

	cs, cancel := connectTestClient(t, srv)
	defer cancel()
	defer cs.Close()

	res, err := cs.GetPrompt(context.Background(), &sdkmcp.GetPromptParams{
		Name: "mixed",
	})
	if err != nil {
		t.Fatalf("GetPrompt 실패: %v", err)
	}

	// system 메시지는 건너뛰어지므로 user 메시지 하나만 남아야 한다.
	if len(res.Messages) != 1 {
		t.Fatalf("메시지 수 불일치: 기대 1, 실제 %d", len(res.Messages))
	}
	if res.Messages[0].Role != "user" {
		t.Errorf("Messages[0].Role 불일치: 기대 %q, 실제 %q", "user", res.Messages[0].Role)
	}
}
