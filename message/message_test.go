package message

import (
	"encoding/json"
	"testing"
)

// --- 생성자 테스트 ---

func TestNewSystemMessage(t *testing.T) {
	m := NewSystemMessage("system content")
	if m.Role != RoleSystem {
		t.Errorf("역할 기대=%s, 실제=%s", RoleSystem, m.Role)
	}
	if m.Content != "system content" {
		t.Errorf("내용 불일치: %s", m.Content)
	}
}

func TestNewUserMessage(t *testing.T) {
	m := NewUserMessage("hello")
	if m.Role != RoleUser {
		t.Errorf("역할 기대=%s, 실제=%s", RoleUser, m.Role)
	}
}

func TestNewAssistantMessage(t *testing.T) {
	m := NewAssistantMessage("reply")
	if m.Role != RoleAssistant {
		t.Errorf("역할 기대=%s, 실제=%s", RoleAssistant, m.Role)
	}
}

func TestNewToolMessage(t *testing.T) {
	m := NewToolMessage("call-1", "search", "result text")
	if m.Role != RoleTool {
		t.Errorf("역할 기대=%s, 실제=%s", RoleTool, m.Role)
	}
	if m.ToolCallID != "call-1" {
		t.Errorf("ToolCallID 불일치: %s", m.ToolCallID)
	}
	if m.Name != "search" {
		t.Errorf("Name 불일치: %s", m.Name)
	}
	if m.Content != "result text" {
		t.Errorf("Content 불일치: %s", m.Content)
	}
}

func TestNewAssistantToolCalls(t *testing.T) {
	calls := []ToolCall{
		{ID: "c1", Name: "tool_a", Args: json.RawMessage(`{"key":"val"}`)},
	}
	m := NewAssistantToolCalls(calls)
	if m.Role != RoleAssistant {
		t.Errorf("역할 기대=%s, 실제=%s", RoleAssistant, m.Role)
	}
	if len(m.ToolCalls) != 1 {
		t.Errorf("ToolCalls 길이 기대=1, 실제=%d", len(m.ToolCalls))
	}
}

func TestWithName(t *testing.T) {
	m := NewUserMessage("hello")
	named := WithName(m, "alice")
	if named.Name != "alice" {
		t.Errorf("Name 기대=alice, 실제=%s", named.Name)
	}
	// 원본은 변경되지 않아야 한다
	if m.Name != "" {
		t.Errorf("원본 Name이 변경됨: %s", m.Name)
	}
}

// --- 조회 테스트 ---

func TestLastMessage(t *testing.T) {
	msgs := []Message{
		NewUserMessage("first"),
		NewAssistantMessage("second"),
	}
	last, ok := LastMessage(msgs)
	if !ok {
		t.Fatal("LastMessage가 false 반환")
	}
	if last.Content != "second" {
		t.Errorf("마지막 메시지 내용 불일치: %s", last.Content)
	}

	// 빈 슬라이스
	_, ok = LastMessage(nil)
	if ok {
		t.Error("빈 슬라이스에서 ok=true")
	}
}

func TestLastAIMessage(t *testing.T) {
	msgs := []Message{
		NewUserMessage("hi"),
		NewAssistantMessage("hello"),
		NewUserMessage("ok"),
	}
	m, ok := LastAIMessage(msgs)
	if !ok {
		t.Fatal("LastAIMessage가 false 반환")
	}
	if m.Content != "hello" {
		t.Errorf("마지막 AI 메시지 불일치: %s", m.Content)
	}

	// AI 메시지 없음
	noAI := []Message{NewUserMessage("user only")}
	_, ok = LastAIMessage(noAI)
	if ok {
		t.Error("AI 메시지 없는 경우에서 ok=true")
	}
}

func TestHasToolCalls(t *testing.T) {
	m := NewAssistantMessage("no tools")
	if HasToolCalls(m) {
		t.Error("도구 호출 없는 메시지에서 true")
	}

	withTools := NewAssistantToolCalls([]ToolCall{{ID: "c1", Name: "fn", Args: json.RawMessage(`{}`)}})
	if !HasToolCalls(withTools) {
		t.Error("도구 호출 있는 메시지에서 false")
	}
}

func TestExtractToolCalls(t *testing.T) {
	calls := []ToolCall{
		{ID: "c1", Name: "fn1", Args: json.RawMessage(`{"x":1}`)},
		{ID: "c2", Name: "fn2", Args: json.RawMessage(`{"y":2}`)},
	}
	m := NewAssistantToolCalls(calls)
	extracted := ExtractToolCalls(m)
	if len(extracted) != 2 {
		t.Errorf("추출된 도구 호출 수 기대=2, 실제=%d", len(extracted))
	}
	if extracted[0].ID != "c1" || extracted[1].ID != "c2" {
		t.Errorf("도구 호출 ID 불일치: %+v", extracted)
	}

	// 도구 호출 없음
	empty := ExtractToolCalls(NewUserMessage("hello"))
	if len(empty) != 0 {
		t.Errorf("빈 도구 호출이 비어야 하는데 길이=%d", len(empty))
	}
}

func TestFilterByName(t *testing.T) {
	msgs := []Message{
		WithName(NewUserMessage("a"), "alice"),
		WithName(NewUserMessage("b"), "bob"),
		WithName(NewUserMessage("c"), "alice"),
	}
	result := FilterByName(msgs, "alice")
	if len(result) != 2 {
		t.Errorf("FilterByName 결과 기대=2, 실제=%d", len(result))
	}
	for _, m := range result {
		if m.Name != "alice" {
			t.Errorf("예상치 않은 Name: %s", m.Name)
		}
	}

	// 없는 이름
	none := FilterByName(msgs, "nobody")
	if len(none) != 0 {
		t.Errorf("없는 이름 결과 길이 기대=0, 실제=%d", len(none))
	}
}

// --- AddMessages 리듀서 테스트 ---

func TestAddMessages_Append(t *testing.T) {
	base := []Message{
		{Role: RoleUser, Content: "hello", ID: "m1"},
	}
	incoming := []Message{
		{Role: RoleAssistant, Content: "hi", ID: "m2"},
	}
	result := AddMessages(base, incoming)
	if len(result) != 2 {
		t.Errorf("길이 기대=2, 실제=%d", len(result))
	}
	if result[1].ID != "m2" {
		t.Errorf("추가된 메시지 ID 불일치: %s", result[1].ID)
	}
}

func TestAddMessages_Upsert(t *testing.T) {
	base := []Message{
		{Role: RoleUser, Content: "original", ID: "m1"},
		{Role: RoleAssistant, Content: "response", ID: "m2"},
	}
	incoming := []Message{
		{Role: RoleUser, Content: "updated", ID: "m1"},
	}
	result := AddMessages(base, incoming)
	// 길이는 변하지 않아야 한다
	if len(result) != 2 {
		t.Errorf("upsert 후 길이 기대=2, 실제=%d", len(result))
	}
	// m1의 내용이 업데이트되어야 한다
	if result[0].Content != "updated" {
		t.Errorf("upsert 후 내용 기대=updated, 실제=%s", result[0].Content)
	}
}

func TestAddMessages_NoID_AlwaysAppend(t *testing.T) {
	base := []Message{
		NewUserMessage("first"),
	}
	incoming := []Message{
		NewUserMessage("second"),
		NewUserMessage("third"),
	}
	result := AddMessages(base, incoming)
	if len(result) != 3 {
		t.Errorf("ID 없는 메시지 append 후 길이 기대=3, 실제=%d", len(result))
	}
}

func TestAddMessages_RemoveAllSentinel(t *testing.T) {
	base := []Message{
		{Role: RoleUser, Content: "old", ID: "m1"},
		{Role: RoleAssistant, Content: "old reply", ID: "m2"},
	}
	// 센티널로 전체 삭제 후 새 메시지만 포함
	incoming := []Message{
		{ID: RemoveAllSentinel},
		{Role: RoleUser, Content: "fresh", ID: "m3"},
	}
	result := AddMessages(base, incoming)
	if len(result) != 1 {
		t.Errorf("센티널 후 길이 기대=1, 실제=%d", len(result))
	}
	if result[0].Content != "fresh" {
		t.Errorf("센티널 후 내용 기대=fresh, 실제=%s", result[0].Content)
	}
}

// --- RemoveMessage + ApplyRemovals 테스트 ---

func TestRemoveMessage_SingleRemoval(t *testing.T) {
	msgs := []Message{
		{Role: RoleUser, Content: "keep", ID: "m1"},
		{Role: RoleAssistant, Content: "remove me", ID: "m2"},
		{Role: RoleUser, Content: "keep too", ID: "m3"},
	}
	// 삭제 마커를 붙여 ApplyRemovals 로 제거
	msgs = append(msgs, RemoveMessage("m2"))
	result := ApplyRemovals(msgs)
	if len(result) != 2 {
		t.Errorf("단건 삭제 후 길이 기대=2, 실제=%d", len(result))
	}
	for _, m := range result {
		if m.ID == "m2" {
			t.Error("m2가 제거되지 않음")
		}
	}
}

func TestApplyRemovals_RemoveAllSentinel(t *testing.T) {
	msgs := []Message{
		{Role: RoleUser, Content: "a", ID: "m1"},
		{Role: RoleUser, Content: "b", ID: "m2"},
		{ID: RemoveAllSentinel},
	}
	result := ApplyRemovals(msgs)
	if len(result) != 0 {
		t.Errorf("전체 삭제 후 길이 기대=0, 실제=%d", len(result))
	}
}

func TestApplyRemovals_NoMarkers(t *testing.T) {
	msgs := []Message{
		{Role: RoleUser, Content: "a", ID: "m1"},
		{Role: RoleAssistant, Content: "b", ID: "m2"},
	}
	result := ApplyRemovals(msgs)
	if len(result) != 2 {
		t.Errorf("마커 없음 후 길이 기대=2, 실제=%d", len(result))
	}
}

// --- TrimMessages 테스트 ---

func TestTrimMessages_MaxTokens(t *testing.T) {
	msgs := []Message{
		NewSystemMessage("system prompt that is quite long for testing purposes"),
		NewUserMessage("user message content for testing"),
		NewAssistantMessage("assistant response for testing purposes"),
		NewUserMessage("another user message here"),
		NewAssistantMessage("final assistant response"),
	}

	// 매우 작은 max_tokens으로 마지막 몇 개만 남겨야 한다
	opts := TrimOptions{
		Strategy:  "last",
		MaxTokens: 20,
	}
	result := TrimMessages(msgs, opts)
	// 트리밍 후 토큰이 max_tokens 이하여야 한다
	if CountTokensApprox(result) > opts.MaxTokens {
		t.Errorf("트리밍 후 토큰=%d, max=%d 초과", CountTokensApprox(result), opts.MaxTokens)
	}
	// 빈 결과가 아니어야 한다(적어도 마지막 메시지는 포함)
	if len(result) == 0 {
		t.Error("트리밍 결과가 비어 있음")
	}
}

func TestTrimMessages_NoLimit(t *testing.T) {
	msgs := []Message{
		NewUserMessage("a"),
		NewUserMessage("b"),
		NewAssistantMessage("c"),
	}
	opts := TrimOptions{Strategy: "last"} // MaxTokens=0이면 제한 없음
	result := TrimMessages(msgs, opts)
	if len(result) != len(msgs) {
		t.Errorf("제한 없음 시 길이 기대=%d, 실제=%d", len(msgs), len(result))
	}
}

func TestTrimMessages_StartOn(t *testing.T) {
	msgs := []Message{
		NewSystemMessage("system"),
		NewUserMessage("user first"),
		NewAssistantMessage("assistant"),
		NewUserMessage("user second"),
	}
	opts := TrimOptions{StartOn: RoleUser}
	result := TrimMessages(msgs, opts)
	// 첫 번째 user 메시지부터 시작해야 한다
	if len(result) == 0 || result[0].Role != RoleUser {
		t.Errorf("StartOn 후 첫 역할 기대=%s, 실제=%s", RoleUser, result[0].Role)
	}
	// system 메시지는 포함되면 안 된다
	if result[0].Content == "system" {
		t.Error("StartOn 후 system 메시지가 포함됨")
	}
}

func TestTrimMessages_EndOn(t *testing.T) {
	msgs := []Message{
		NewUserMessage("user first"),
		NewAssistantMessage("assistant"),
		NewUserMessage("user second"),
		NewAssistantMessage("last assistant"),
	}
	opts := TrimOptions{EndOn: RoleUser}
	result := TrimMessages(msgs, opts)
	// 마지막 user 메시지로 끝나야 한다
	if len(result) == 0 || result[len(result)-1].Role != RoleUser {
		t.Errorf("EndOn 후 마지막 역할 기대=%s, 실제=%s", RoleUser, result[len(result)-1].Role)
	}
}

func TestTrimMessages_WindowBoundary(t *testing.T) {
	// 토큰이 딱 맞는 경우
	msgs := []Message{
		{Role: RoleUser, Content: "aaaa", ID: "m1"},  // 약 5토큰(1+4)
		{Role: RoleUser, Content: "bbbb", ID: "m2"},  // 약 5토큰
	}
	allTokens := CountTokensApprox(msgs)
	opts := TrimOptions{Strategy: "last", MaxTokens: allTokens}
	result := TrimMessages(msgs, opts)
	// 모두 포함되어야 한다
	if len(result) != 2 {
		t.Errorf("정확한 토큰 경계에서 길이 기대=2, 실제=%d", len(result))
	}
}

func TestTrimMessages_Empty(t *testing.T) {
	result := TrimMessages(nil, TrimOptions{Strategy: "last", MaxTokens: 100})
	if len(result) != 0 {
		t.Errorf("빈 입력에서 결과가 비어야 함, 실제=%d", len(result))
	}
}

// --- CountTokensApprox 테스트 ---

func TestCountTokensApprox(t *testing.T) {
	msgs := []Message{
		NewUserMessage("hello world"),
		NewAssistantMessage("this is a longer response to the user"),
	}
	count := CountTokensApprox(msgs)
	if count <= 0 {
		t.Errorf("토큰 수 기대=양수, 실제=%d", count)
	}
}

func TestCountTokensApprox_Empty(t *testing.T) {
	count := CountTokensApprox(nil)
	if count != 0 {
		t.Errorf("빈 메시지 토큰 기대=0, 실제=%d", count)
	}
}

func TestCountTokensApprox_WithToolCalls(t *testing.T) {
	m := NewAssistantToolCalls([]ToolCall{
		{ID: "c1", Name: "search_tool", Args: json.RawMessage(`{"query":"what is go programming"}`)},
	})
	count := CountTokensApprox([]Message{m})
	if count <= 0 {
		t.Errorf("도구 호출 메시지 토큰 기대=양수, 실제=%d", count)
	}
}

// --- PrettyPrint 테스트 ---

func TestPrettyPrint(t *testing.T) {
	m := Message{
		Role:    RoleUser,
		Content: "hello",
		ID:      "m1",
		Name:    "alice",
	}
	s := PrettyPrint(m)
	if s == "" {
		t.Error("PrettyPrint 결과가 비어 있음")
	}
	// 역할 포함 여부 확인
	if !contains(s, "USER") {
		t.Errorf("PrettyPrint에 역할(USER)이 포함되지 않음: %s", s)
	}
}

func TestPrettyPrintMessages(t *testing.T) {
	msgs := []Message{
		NewUserMessage("hi"),
		NewAssistantMessage("hello"),
	}
	s := PrettyPrintMessages(msgs)
	if s == "" {
		t.Error("PrettyPrintMessages 결과가 비어 있음")
	}
}

// contains 는 테스트용 문자열 포함 여부 helper다.
func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}

// --- 통합: AddMessages upsert/append 혼합 시나리오 ---

func TestAddMessages_MixedUpsertAppend(t *testing.T) {
	base := []Message{
		{Role: RoleUser, Content: "original user", ID: "u1"},
		{Role: RoleAssistant, Content: "original assistant", ID: "a1"},
	}
	incoming := []Message{
		{Role: RoleUser, Content: "updated user", ID: "u1"},    // upsert
		{Role: RoleAssistant, Content: "new assistant", ID: "a2"}, // append
		NewUserMessage("no id message"),                          // append (ID 없음)
	}
	result := AddMessages(base, incoming)
	if len(result) != 4 {
		t.Errorf("혼합 후 길이 기대=4, 실제=%d", len(result))
	}
	// u1이 업데이트되어야 한다
	if result[0].Content != "updated user" {
		t.Errorf("u1 upsert 확인: 기대=updated user, 실제=%s", result[0].Content)
	}
	// a1은 그대로
	if result[1].Content != "original assistant" {
		t.Errorf("a1 유지 확인: 기대=original assistant, 실제=%s", result[1].Content)
	}
	// a2 append
	if result[2].ID != "a2" {
		t.Errorf("a2 append 확인: 실제ID=%s", result[2].ID)
	}
}

// --- 통합: 도구 호출 추출 시나리오 ---

func TestToolCallWorkflow(t *testing.T) {
	// 1. 어시스턴트가 도구 호출을 포함한 메시지 생성
	calls := []ToolCall{
		{ID: "call-1", Name: "search", Args: json.RawMessage(`{"query":"langgraph"}`)},
	}
	assistantMsg := NewAssistantToolCalls(calls)

	// 2. HasToolCalls 확인
	if !HasToolCalls(assistantMsg) {
		t.Error("도구 호출 메시지에서 HasToolCalls=false")
	}

	// 3. ExtractToolCalls 로 호출 추출
	extracted := ExtractToolCalls(assistantMsg)
	if len(extracted) != 1 {
		t.Fatalf("추출된 도구 호출 수 기대=1, 실제=%d", len(extracted))
	}
	if extracted[0].Name != "search" {
		t.Errorf("도구 이름 기대=search, 실제=%s", extracted[0].Name)
	}

	// 4. 도구 결과 메시지 생성
	toolResult := NewToolMessage("call-1", "search", "langgraph search result")

	// 5. LastAIMessage 가 어시스턴트 메시지를 찾는지 확인
	msgs := []Message{
		NewUserMessage("what is langgraph?"),
		assistantMsg,
		toolResult,
	}
	last, ok := LastAIMessage(msgs)
	if !ok {
		t.Fatal("LastAIMessage가 false")
	}
	if !HasToolCalls(last) {
		t.Error("마지막 AI 메시지가 도구 호출을 포함하지 않음")
	}
}
