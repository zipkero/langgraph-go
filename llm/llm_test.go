// llm_test.go 는 llm 패키지의 계약 메서드·도구 파싱·구조화·InitChatModel 파싱을
// stub Client 기반 단위 테스트로 검증한다(D9). 실제 네트워크 호출은 하지 않는다.
package llm_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/zipkero/langgraph-go/llm"
	"github.com/zipkero/langgraph-go/message"
	"github.com/zipkero/langgraph-go/structured"
	"github.com/zipkero/langgraph-go/tool"
)

// ─── 헬퍼 ────────────────────────────────────────────────────────────────────

// makeToolCall 은 테스트용 ToolCall 을 생성한다.
func makeToolCall(id, name string, args map[string]any) message.ToolCall {
	raw, _ := json.Marshal(args)
	return message.ToolCall{
		ID:   id,
		Name: name,
		Args: json.RawMessage(raw),
	}
}

// ─── Chat 계약 테스트 ─────────────────────────────────────────────────────────

// TestStubClient_Chat 은 stub Chat 이 미리 지정된 응답을 반환하는지 검증한다.
func TestStubClient_Chat(t *testing.T) {
	want := "안녕하세요, 저는 어시스턴트입니다."
	stub := llm.NewStubClient("test-model", llm.StubResponse{
		Message:      message.NewAssistantMessage(want),
		FinishReason: "stop",
	})

	resp, err := stub.Chat(context.Background(), llm.ChatRequest{
		Messages: []message.Message{message.NewUserMessage("안녕")},
	})
	if err != nil {
		t.Fatalf("Chat 호출 실패: %v", err)
	}
	if resp.Message.Content != want {
		t.Errorf("Chat 응답 Content 불일치: got %q, want %q", resp.Message.Content, want)
	}
	if resp.FinishReason != "stop" {
		t.Errorf("Chat FinishReason 불일치: got %q, want %q", resp.FinishReason, "stop")
	}
}

// TestStubClient_Chat_Error 는 Err 가 지정됐을 때 Chat 이 에러를 반환하는지 검증한다.
func TestStubClient_Chat_Error(t *testing.T) {
	stub := llm.NewStubClient("test-model", llm.StubResponse{
		Err: fmt.Errorf("테스트 에러"),
	})
	_, err := stub.Chat(context.Background(), llm.ChatRequest{})
	if err == nil {
		t.Fatal("에러가 반환되어야 하는데 nil 반환")
	}
}

// ─── ChatStream 계약 테스트 ───────────────────────────────────────────────────

// TestStubClient_ChatStream 은 stub ChatStream 이 토큰·메시지·완료 이벤트를 순서대로
// 방출하는지 검증한다.
func TestStubClient_ChatStream(t *testing.T) {
	content := "스트리밍 응답"
	stub := llm.NewStubClient("test-model", llm.StubResponse{
		Message:      message.NewAssistantMessage(content),
		FinishReason: "stop",
	})

	ch, err := stub.ChatStream(context.Background(), llm.ChatRequest{})
	if err != nil {
		t.Fatalf("ChatStream 호출 실패: %v", err)
	}

	var events []llm.ChatEvent
	for ev := range ch {
		events = append(events, ev)
	}

	// 이벤트 순서: token → message → done
	if len(events) < 3 {
		t.Fatalf("이벤트 수 부족: got %d, want >= 3", len(events))
	}
	if events[0].Type != llm.ChatEventToken {
		t.Errorf("첫 이벤트 타입 불일치: got %q, want %q", events[0].Type, llm.ChatEventToken)
	}
	if events[0].Token != content {
		t.Errorf("토큰 이벤트 내용 불일치: got %q, want %q", events[0].Token, content)
	}
	if events[1].Type != llm.ChatEventMessage {
		t.Errorf("두 번째 이벤트 타입 불일치: got %q, want %q", events[1].Type, llm.ChatEventMessage)
	}
	if events[2].Type != llm.ChatEventDone {
		t.Errorf("세 번째 이벤트 타입 불일치: got %q, want %q", events[2].Type, llm.ChatEventDone)
	}
	if events[2].Response == nil {
		t.Fatal("완료 이벤트의 Response 가 nil")
	}
	if events[2].Response.FinishReason != "stop" {
		t.Errorf("완료 이벤트 FinishReason 불일치: got %q, want %q",
			events[2].Response.FinishReason, "stop")
	}
}

// ─── BindTools / ParseToolCalls 테스트 ───────────────────────────────────────

// TestStubClient_BindTools_ParseToolCalls 는 BindTools 후 ParseToolCalls 가
// 올바른 []message.ToolCall 을 반환하는지 검증한다.
func TestStubClient_BindTools_ParseToolCalls(t *testing.T) {
	tc := makeToolCall("call-1", "search", map[string]any{"query": "Go 언어"})
	stub := llm.NewStubClient("test-model", llm.StubResponse{
		Message: message.NewAssistantToolCalls([]message.ToolCall{tc}),
		ToolCalls: []message.ToolCall{tc},
		FinishReason: "tool_use",
	})

	// 도구 스키마 바인딩
	schemas := []tool.Schema{
		{
			Name:        "search",
			Description: "웹 검색 도구",
			Parameters: []tool.Parameter{
				{Name: "query", Type: "string", Required: true},
			},
		},
	}
	bound := stub.BindTools(schemas)

	// Chat 호출
	resp, err := bound.Chat(context.Background(), llm.ChatRequest{
		Messages: []message.Message{message.NewUserMessage("검색해줘")},
	})
	if err != nil {
		t.Fatalf("Chat 호출 실패: %v", err)
	}

	// ParseToolCalls 검증
	calls := bound.ParseToolCalls(resp)
	if len(calls) != 1 {
		t.Fatalf("ParseToolCalls 결과 개수 불일치: got %d, want 1", len(calls))
	}
	if calls[0].Name != "search" {
		t.Errorf("도구 이름 불일치: got %q, want %q", calls[0].Name, "search")
	}
	if calls[0].ID != "call-1" {
		t.Errorf("도구 호출 ID 불일치: got %q, want %q", calls[0].ID, "call-1")
	}
}

// TestStubClient_BindTools_Immutable 은 BindTools 가 원본을 변경하지 않는지 검증한다.
func TestStubClient_BindTools_Immutable(t *testing.T) {
	stub := llm.NewStubClient("test-model", llm.StubResponse{
		Message: message.NewAssistantMessage("응답"),
	})

	schemas := []tool.Schema{{Name: "calc", Description: "계산기"}}
	bound := stub.BindTools(schemas)

	// 원본 ParseToolCalls — 빈 응답은 빈 슬라이스를 반환해야 한다
	emptyResp := llm.ChatResponse{}
	original := stub.ParseToolCalls(emptyResp)
	if len(original) != 0 {
		t.Errorf("원본 ParseToolCalls 가 비어 있어야 함: got %d", len(original))
	}

	// bound 는 다른 인스턴스여야 한다
	if bound == (llm.Client)(stub) {
		t.Error("BindTools 가 원본과 동일한 인스턴스를 반환함")
	}
}

// ─── Structured / ResponseFormat 테스트 ──────────────────────────────────────

// testOutput 은 구조화 출력 테스트용 타입이다.
type testOutput struct {
	Answer string `json:"answer" description:"답변"`
	Score  int    `json:"score,omitempty" description:"점수"`
}

// TestStubClient_Structured_WithValue 는 StructuredValue 지정 시 Structured 가
// 해당 값을 반환하는지 검증한다.
func TestStubClient_Structured_WithValue(t *testing.T) {
	want := testOutput{Answer: "42", Score: 100}
	stub := llm.NewStubClient("test-model", llm.StubResponse{
		StructuredValue: want,
	})

	schema := structured.BuildSchema[testOutput]()
	val, err := stub.Structured(context.Background(), llm.ChatRequest{}, schema)
	if err != nil {
		t.Fatalf("Structured 호출 실패: %v", err)
	}
	got, ok := val.(testOutput)
	if !ok {
		t.Fatalf("반환 타입 불일치: got %T, want testOutput", val)
	}
	if got.Answer != want.Answer {
		t.Errorf("Answer 불일치: got %q, want %q", got.Answer, want.Answer)
	}
}

// TestStubClient_Structured_WithJSON 은 Message.Content 에 JSON 을 넣으면 Structured 가
// 스키마 검증 후 map[string]any 를 반환하는지 검증한다.
func TestStubClient_Structured_WithJSON(t *testing.T) {
	jsonContent := `{"answer":"yes"}`
	stub := llm.NewStubClient("test-model", llm.StubResponse{
		Message: message.NewAssistantMessage(jsonContent),
	})

	// BinaryScore 스키마 사용
	type BinaryScore struct {
		Answer string `json:"answer" description:"yes 또는 no"`
	}
	schema := structured.BuildSchema[BinaryScore]()
	val, err := stub.Structured(context.Background(), llm.ChatRequest{}, schema)
	if err != nil {
		t.Fatalf("Structured 호출 실패: %v", err)
	}
	result, ok := val.(map[string]any)
	if !ok {
		t.Fatalf("반환 타입 불일치: got %T, want map[string]any", val)
	}
	if result["answer"] != "yes" {
		t.Errorf("answer 필드 불일치: got %v, want \"yes\"", result["answer"])
	}
}

// TestStubClient_Structured_SchemaViolation 은 스키마에 맞지 않는 JSON 이 에러를 반환하는지 검증한다.
func TestStubClient_Structured_SchemaViolation(t *testing.T) {
	// 필수 필드 answer 가 누락된 JSON
	jsonContent := `{"score":42}`
	stub := llm.NewStubClient("test-model", llm.StubResponse{
		Message: message.NewAssistantMessage(jsonContent),
	})

	type RequiredOutput struct {
		Answer string `json:"answer" description:"필수 답변"`
	}
	schema := structured.BuildSchema[RequiredOutput]()
	_, err := stub.Structured(context.Background(), llm.ChatRequest{}, schema)
	if err == nil {
		t.Fatal("스키마 위반에도 에러가 반환되지 않음")
	}
}

// ─── WithModel / ModelName 테스트 ─────────────────────────────────────────────

// TestStubClient_WithModel 은 WithModel 이 지정한 모델 이름을 갖는 새 Client 를
// 반환하고 원본을 변경하지 않는지 검증한다.
func TestStubClient_WithModel(t *testing.T) {
	stub := llm.NewStubClient("original-model", llm.StubResponse{})

	updated := stub.WithModel("new-model")
	if updated.ModelName() != "new-model" {
		t.Errorf("WithModel 후 ModelName 불일치: got %q, want %q",
			updated.ModelName(), "new-model")
	}
	if stub.ModelName() != "original-model" {
		t.Errorf("원본 ModelName 이 변경됨: got %q, want %q",
			stub.ModelName(), "original-model")
	}
}

// ─── InitChatModel 파싱 테스트 ────────────────────────────────────────────────

// TestInitChatModel_AnthropicProvider 는 anthropic provider 가 파싱돼
// 에러 없이(현재 미구현 에러 제외) 반환되는지 검증한다.
// task-004 에서 InitChatModel 은 anthropic 분기에서 newAnthropicClient 를 호출하고,
// newAnthropicClient 는 "미구현" 에러를 반환하므로, 여기서는 provider 파싱 성공을
// "미지원 provider" 에러가 아닌 것으로 검증한다.
func TestInitChatModel_AnthropicProvider(t *testing.T) {
	_, err := llm.InitChatModel("anthropic:claude-opus-4-8")
	if err == nil {
		// task-005 이후 실제 어댑터가 있으면 이 경로가 정상
		return
	}
	// task-004: newAnthropicClient 가 미구현 에러를 반환하는 것은 허용 —
	// 단, "지원하지 않는 프로바이더" 에러가 아니어야 한다
	if isUnsupportedProviderError(err) {
		t.Errorf("anthropic 는 지원 provider 인데 미지원 에러 반환: %v", err)
	}
}

// TestInitChatModel_UnsupportedProvider 는 미지원 provider 가 에러를 반환하는지 검증한다.
func TestInitChatModel_UnsupportedProvider(t *testing.T) {
	_, err := llm.InitChatModel("openai:gpt-4o")
	if err == nil {
		t.Fatal("미지원 provider 에 에러가 반환되지 않음")
	}
}

// TestInitChatModel_InvalidFormat 은 잘못된 형식의 식별자가 에러를 반환하는지 검증한다.
func TestInitChatModel_InvalidFormat(t *testing.T) {
	cases := []string{
		"anthropic",        // 콜론 없음
		":claude-opus-4-8", // provider 비어 있음
		"anthropic:",       // model 비어 있음
		"",                 // 빈 문자열
	}
	for _, spec := range cases {
		_, err := llm.InitChatModel(spec)
		if err == nil {
			t.Errorf("잘못된 형식 %q 에 에러가 반환되지 않음", spec)
		}
	}
}

// TestInitChatModel_MultipleProviders 는 다양한 미지원 provider 가 에러를 반환하는지 검증한다.
func TestInitChatModel_MultipleProviders(t *testing.T) {
	unsupported := []string{
		"openai:gpt-4o",
		"gemini:gemini-pro",
		"cohere:command-r",
		"ollama:llama3",
	}
	for _, spec := range unsupported {
		_, err := llm.InitChatModel(spec)
		if err == nil {
			t.Errorf("미지원 provider %q 에 에러가 반환되지 않음", spec)
		}
	}
}

// ─── ParseToolCalls 빈 응답 테스트 ───────────────────────────────────────────

// TestStubClient_ParseToolCalls_Empty 는 ToolCalls 가 없는 응답에서
// ParseToolCalls 가 빈 슬라이스를 반환하는지 검증한다.
func TestStubClient_ParseToolCalls_Empty(t *testing.T) {
	stub := llm.NewStubClient("test-model", llm.StubResponse{
		Message: message.NewAssistantMessage("단순 응답"),
	})
	resp := llm.ChatResponse{Message: message.NewAssistantMessage("단순 응답")}
	calls := stub.ParseToolCalls(resp)
	if calls == nil {
		t.Fatal("ParseToolCalls 가 nil 을 반환함 — 빈 슬라이스여야 함")
	}
	if len(calls) != 0 {
		t.Errorf("ParseToolCalls 결과 개수 불일치: got %d, want 0", len(calls))
	}
}

// ─── 다중 도구 호출 테스트 ────────────────────────────────────────────────────

// TestStubClient_MultipleToolCalls 는 복수 도구 호출이 올바르게 파싱되는지 검증한다.
func TestStubClient_MultipleToolCalls(t *testing.T) {
	calls := []message.ToolCall{
		makeToolCall("call-1", "search", map[string]any{"query": "Go 언어"}),
		makeToolCall("call-2", "calc", map[string]any{"expr": "1+1"}),
	}
	stub := llm.NewStubClient("test-model", llm.StubResponse{
		Message:      message.NewAssistantToolCalls(calls),
		ToolCalls:    calls,
		FinishReason: "tool_use",
	})

	schemas := []tool.Schema{
		{Name: "search", Description: "검색"},
		{Name: "calc", Description: "계산"},
	}
	bound := stub.BindTools(schemas)
	resp, err := bound.Chat(context.Background(), llm.ChatRequest{})
	if err != nil {
		t.Fatalf("Chat 호출 실패: %v", err)
	}

	parsed := bound.ParseToolCalls(resp)
	if len(parsed) != 2 {
		t.Fatalf("도구 호출 수 불일치: got %d, want 2", len(parsed))
	}
	if parsed[0].Name != "search" {
		t.Errorf("첫 번째 도구 이름 불일치: got %q, want %q", parsed[0].Name, "search")
	}
	if parsed[1].Name != "calc" {
		t.Errorf("두 번째 도구 이름 불일치: got %q, want %q", parsed[1].Name, "calc")
	}
}

// ─── 헬퍼 함수 ───────────────────────────────────────────────────────────────

// isUnsupportedProviderError 는 err 가 "지원하지 않는 프로바이더" 에러인지 판별한다.
func isUnsupportedProviderError(err error) bool {
	if err == nil {
		return false
	}
	return contains(err.Error(), "지원하지 않는 프로바이더")
}

// contains 는 s 에 sub 가 포함되면 true 를 반환한다.
func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		findSubstring(s, sub))
}

func findSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
