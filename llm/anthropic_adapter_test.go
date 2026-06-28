// anthropic_adapter_test.go 는 Anthropic 어댑터의 단위 테스트와 라이브 스모크 테스트를 담는다.
//
// 테스트 두 종류:
//  1. message↔content-block 변환 단위 테스트 — 네트워크 불필요, 항상 실행
//  2. 라이브 스모크 테스트 — ANTHROPIC_API_KEY 환경변수 없으면 skip(D9)
package llm_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/zipkero/langgraph-go/llm"
	"github.com/zipkero/langgraph-go/message"
	"github.com/zipkero/langgraph-go/tool"
)

// ─── 헬퍼 ────────────────────────────────────────────────────────────────────

// apiKey 는 환경변수에서 ANTHROPIC_API_KEY 를 읽어 반환한다.
func apiKey() string {
	return os.Getenv("ANTHROPIC_API_KEY")
}

// skipIfNoKey 는 API 키가 없으면 테스트를 skip 한다(D9).
func skipIfNoKey(t *testing.T) {
	t.Helper()
	if apiKey() == "" {
		t.Skip("ANTHROPIC_API_KEY 가 없으므로 라이브 스모크 테스트를 건너뜁니다")
	}
}

// newLiveClient 는 라이브 테스트용 Anthropic 클라이언트를 생성한다.
func newLiveClient(t *testing.T) llm.Client {
	t.Helper()
	client, err := llm.InitChatModel("anthropic:claude-opus-4-8")
	if err != nil {
		t.Fatalf("InitChatModel 실패: %v", err)
	}
	return client
}

// ─── 변환 단위 테스트 (네트워크 불필요) ──────────────────────────────────────

// TestAnthropicAdapter_InitChatModel_Success 는 anthropic 프로바이더로 클라이언트가
// 생성되는지 검증한다(task-004 테스트 보강).
func TestAnthropicAdapter_InitChatModel_Success(t *testing.T) {
	client, err := llm.InitChatModel("anthropic:claude-opus-4-8")
	if err != nil {
		t.Fatalf("InitChatModel 이 에러를 반환함: %v", err)
	}
	if client == nil {
		t.Fatal("클라이언트가 nil 을 반환함")
	}
	if client.ModelName() != "claude-opus-4-8" {
		t.Errorf("ModelName 불일치: got %q, want %q", client.ModelName(), "claude-opus-4-8")
	}
}

// TestAnthropicAdapter_ModelName 은 WithModel 이 모델 이름을 변경하는지 검증한다.
func TestAnthropicAdapter_ModelName(t *testing.T) {
	client, err := llm.InitChatModel("anthropic:claude-opus-4-8")
	if err != nil {
		t.Fatalf("InitChatModel 실패: %v", err)
	}

	newClient := client.WithModel("claude-opus-4-7")
	if newClient.ModelName() != "claude-opus-4-7" {
		t.Errorf("WithModel 후 ModelName 불일치: got %q, want %q",
			newClient.ModelName(), "claude-opus-4-7")
	}
	// 원본은 변경되지 않아야 한다.
	if client.ModelName() != "claude-opus-4-8" {
		t.Errorf("원본 ModelName 이 변경됨: got %q", client.ModelName())
	}
}

// TestAnthropicAdapter_BindTools_Immutable 은 BindTools 가 원본을 변경하지 않는지 검증한다.
func TestAnthropicAdapter_BindTools_Immutable(t *testing.T) {
	client, err := llm.InitChatModel("anthropic:claude-opus-4-8")
	if err != nil {
		t.Fatalf("InitChatModel 실패: %v", err)
	}

	schemas := []tool.Schema{
		{Name: "test_tool", Description: "테스트 도구"},
	}
	bound := client.BindTools(schemas)

	// bound 는 다른 인스턴스여야 한다.
	if bound == client {
		t.Error("BindTools 가 원본과 동일한 인스턴스를 반환함")
	}
}

// TestAnthropicAdapter_ParseToolCalls_Empty 는 ToolCalls 없는 응답에서
// ParseToolCalls 가 빈 슬라이스를 반환하는지 검증한다.
func TestAnthropicAdapter_ParseToolCalls_Empty(t *testing.T) {
	client, err := llm.InitChatModel("anthropic:claude-opus-4-8")
	if err != nil {
		t.Fatalf("InitChatModel 실패: %v", err)
	}

	resp := llm.ChatResponse{
		Message: message.NewAssistantMessage("단순 텍스트 응답"),
	}
	calls := client.ParseToolCalls(resp)
	if calls == nil {
		t.Fatal("ParseToolCalls 가 nil 을 반환함 — 빈 슬라이스여야 함")
	}
	if len(calls) != 0 {
		t.Errorf("ParseToolCalls 결과 수 불일치: got %d, want 0", len(calls))
	}
}

// TestAnthropicAdapter_ParseToolCalls_WithCalls 는 ToolCalls 가 있는 응답에서
// ParseToolCalls 가 올바른 슬라이스를 반환하는지 검증한다.
func TestAnthropicAdapter_ParseToolCalls_WithCalls(t *testing.T) {
	client, err := llm.InitChatModel("anthropic:claude-opus-4-8")
	if err != nil {
		t.Fatalf("InitChatModel 실패: %v", err)
	}

	args, _ := json.Marshal(map[string]any{"query": "Go 언어"})
	resp := llm.ChatResponse{
		Message: message.NewAssistantMessage(""),
		ToolCalls: []message.ToolCall{
			{ID: "call-1", Name: "search", Args: json.RawMessage(args)},
		},
	}
	calls := client.ParseToolCalls(resp)
	if len(calls) != 1 {
		t.Fatalf("ParseToolCalls 결과 수 불일치: got %d, want 1", len(calls))
	}
	if calls[0].Name != "search" {
		t.Errorf("도구 이름 불일치: got %q, want %q", calls[0].Name, "search")
	}
	if calls[0].ID != "call-1" {
		t.Errorf("도구 ID 불일치: got %q, want %q", calls[0].ID, "call-1")
	}
}

// ─── 라이브 스모크 테스트 (ANTHROPIC_API_KEY 게이트) ─────────────────────────

// TestAnthropicAdapter_LiveChat 는 실제 Anthropic API 를 호출해 ChatResponse 를 받는지 검증한다.
// ANTHROPIC_API_KEY 없으면 skip 된다(D9).
func TestAnthropicAdapter_LiveChat(t *testing.T) {
	skipIfNoKey(t)
	client := newLiveClient(t)

	resp, err := client.Chat(context.Background(), llm.ChatRequest{
		Messages: []message.Message{
			message.NewUserMessage("Reply with exactly: OK"),
		},
	})
	if err != nil {
		t.Fatalf("라이브 Chat 실패: %v", err)
	}
	if resp.Message.Content == "" {
		t.Error("ChatResponse.Message.Content 가 비어 있음")
	}
	if resp.FinishReason == "" {
		t.Error("ChatResponse.FinishReason 이 비어 있음")
	}
	if resp.Usage.InputTokens <= 0 {
		t.Errorf("InputTokens 가 0 이하: %d", resp.Usage.InputTokens)
	}
	if resp.Usage.OutputTokens <= 0 {
		t.Errorf("OutputTokens 가 0 이하: %d", resp.Usage.OutputTokens)
	}
	t.Logf("라이브 Chat 응답: content=%q finish=%q tokens(in=%d out=%d)",
		resp.Message.Content, resp.FinishReason,
		resp.Usage.InputTokens, resp.Usage.OutputTokens)
}

// TestAnthropicAdapter_LiveChat_ToolBinding 는 도구 바인딩 후 라이브 호출 시
// 응답에서 도구 호출이 노출되는지 검증한다.
// ANTHROPIC_API_KEY 없으면 skip 된다(D9).
func TestAnthropicAdapter_LiveChat_ToolBinding(t *testing.T) {
	skipIfNoKey(t)
	client := newLiveClient(t)

	// 도구 스키마 정의: 도시 이름을 받아 날씨를 반환하는 도구
	schemas := []tool.Schema{
		{
			Name:        "get_weather",
			Description: "Get the current weather for a city",
			Parameters: []tool.Parameter{
				{
					Name:        "city",
					Type:        "string",
					Description: "The city name",
					Required:    true,
				},
			},
		},
	}
	bound := client.BindTools(schemas)

	resp, err := bound.Chat(context.Background(), llm.ChatRequest{
		Messages: []message.Message{
			message.NewUserMessage("What is the weather in Seoul? Use the get_weather tool."),
		},
	})
	if err != nil {
		t.Fatalf("라이브 ToolBinding Chat 실패: %v", err)
	}

	// 도구 호출 파싱
	toolCalls := bound.ParseToolCalls(resp)
	if len(toolCalls) == 0 {
		t.Logf("도구 호출이 반환되지 않음 (finish_reason=%q, content=%q)",
			resp.FinishReason, resp.Message.Content)
		// 모델이 도구를 사용하지 않고 텍스트로 답할 수도 있으므로
		// finish_reason 이 tool_use 인 경우에만 필수로 요구한다.
		if resp.FinishReason == "tool_use" {
			t.Error("FinishReason 이 tool_use 인데 도구 호출이 없음")
		}
		return
	}

	// 도구 호출이 있으면 get_weather 가 호출됐는지 검증한다.
	found := false
	for _, tc := range toolCalls {
		if tc.Name == "get_weather" {
			found = true
			t.Logf("도구 호출 확인: id=%q name=%q args=%s", tc.ID, tc.Name, string(tc.Args))
		}
	}
	if !found {
		t.Errorf("get_weather 도구 호출이 없음. 호출된 도구: %v",
			func() []string {
				names := make([]string, len(toolCalls))
				for i, tc := range toolCalls {
					names[i] = tc.Name
				}
				return names
			}())
	}
}

// TestAnthropicAdapter_LiveChatStream 는 스트리밍 호출이 토큰 이벤트를 방출하는지 검증한다.
// ANTHROPIC_API_KEY 없으면 skip 된다(D9).
func TestAnthropicAdapter_LiveChatStream(t *testing.T) {
	skipIfNoKey(t)
	client := newLiveClient(t)

	ch, err := client.ChatStream(context.Background(), llm.ChatRequest{
		Messages: []message.Message{
			message.NewUserMessage("Say 'hello' and nothing else."),
		},
	})
	if err != nil {
		t.Fatalf("라이브 ChatStream 호출 실패: %v", err)
	}

	var tokenCount int
	var gotDone bool
	for event := range ch {
		switch event.Type {
		case llm.ChatEventToken:
			tokenCount++
		case llm.ChatEventDone:
			gotDone = true
			if event.Response == nil {
				t.Error("ChatEventDone 의 Response 가 nil 임")
			}
		}
	}

	if tokenCount == 0 {
		t.Error("스트리밍에서 토큰 이벤트가 한 건도 없음")
	}
	if !gotDone {
		t.Error("ChatEventDone 이벤트가 없음")
	}
	t.Logf("스트리밍 토큰 이벤트 수: %d", tokenCount)
}
