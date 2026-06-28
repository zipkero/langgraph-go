// prompt_test.go 는 PromptTemplate 포매팅, Chain Invoke, 구조화 출력 경로를
// stub llm.Client 로 검증하는 단위 테스트다.
package prompt

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/zipkero/langgraph-go/llm"
	"github.com/zipkero/langgraph-go/message"
	"github.com/zipkero/langgraph-go/structured"
)

// ── 헬퍼 ────────────────────────────────────────────────────────────────────

// mustMarshal 은 v 를 JSON 문자열로 변환한다.
func mustMarshal(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("JSON 직렬화 실패: %v", err)
	}
	return string(b)
}

// ── FromTemplate ─────────────────────────────────────────────────────────────

// TestFromTemplate_Format 은 단일 텍스트 템플릿의 플레이스홀더 치환을 검증한다.
func TestFromTemplate_Format(t *testing.T) {
	tmpl := FromTemplate("안녕하세요, {name}! 오늘은 {day} 입니다.")
	msgs, err := tmpl.Format(map[string]any{
		"name": "철수",
		"day":  "월요일",
	})
	if err != nil {
		t.Fatalf("Format 실패: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("메시지 수 기대 1, 실제 %d", len(msgs))
	}
	want := "안녕하세요, 철수! 오늘은 월요일 입니다."
	if msgs[0].Content != want {
		t.Errorf("Content 기대 %q, 실제 %q", want, msgs[0].Content)
	}
	if msgs[0].Role != message.RoleUser {
		t.Errorf("Role 기대 %q, 실제 %q", message.RoleUser, msgs[0].Role)
	}
}

// TestFromTemplate_Format_NoPlaceholder 는 플레이스홀더가 없는 순수 텍스트를 검증한다.
func TestFromTemplate_Format_NoPlaceholder(t *testing.T) {
	tmpl := FromTemplate("고정 메시지입니다.")
	msgs, err := tmpl.Format(map[string]any{})
	if err != nil {
		t.Fatalf("Format 실패: %v", err)
	}
	if len(msgs) != 1 || msgs[0].Content != "고정 메시지입니다." {
		t.Errorf("기대하지 않은 결과: %v", msgs)
	}
}

// TestFromTemplate_Format_MissingVar 는 누락 변수에서 에러가 반환됨을 검증한다.
func TestFromTemplate_Format_MissingVar(t *testing.T) {
	tmpl := FromTemplate("안녕하세요, {name}!")
	_, err := tmpl.Format(map[string]any{})
	if err == nil {
		t.Fatal("누락 변수에서 에러가 반환되어야 합니다")
	}
}

// ── FromMessages ─────────────────────────────────────────────────────────────

// TestFromMessages_Format_MultiRole 은 시스템+사용자 역할 멀티 스펙을 검증한다.
func TestFromMessages_Format_MultiRole(t *testing.T) {
	specs := []MessageSpec{
		SpecFromRole(message.RoleSystem, "당신은 {persona} 입니다."),
		SpecFromRole(message.RoleUser, "{question}"),
	}
	tmpl := FromMessages(specs)
	msgs, err := tmpl.Format(map[string]any{
		"persona":  "유능한 비서",
		"question": "날씨를 알려주세요.",
	})
	if err != nil {
		t.Fatalf("Format 실패: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("메시지 수 기대 2, 실제 %d", len(msgs))
	}
	if msgs[0].Role != message.RoleSystem || msgs[0].Content != "당신은 유능한 비서 입니다." {
		t.Errorf("시스템 메시지 불일치: %+v", msgs[0])
	}
	if msgs[1].Role != message.RoleUser || msgs[1].Content != "날씨를 알려주세요." {
		t.Errorf("사용자 메시지 불일치: %+v", msgs[1])
	}
}

// ── MessagesPlaceholder ───────────────────────────────────────────────────────

// TestFromMessages_Format_MessagesPlaceholder 는 MessagesPlaceholder 로 메시지 목록이
// 삽입됨을 검증한다.
func TestFromMessages_Format_MessagesPlaceholder(t *testing.T) {
	history := []message.Message{
		message.NewUserMessage("이전 질문입니다."),
		message.NewAssistantMessage("이전 답변입니다."),
	}

	specs := []MessageSpec{
		SpecFromRole(message.RoleSystem, "당신은 도움이 되는 AI 입니다."),
		SpecFromPlaceholder(MessagesPlaceholder{VarName: "history"}),
		SpecFromRole(message.RoleUser, "새 질문입니다."),
	}
	tmpl := FromMessages(specs)
	msgs, err := tmpl.Format(map[string]any{
		"history": history,
	})
	if err != nil {
		t.Fatalf("Format 실패: %v", err)
	}
	// 시스템 1 + 히스토리 2 + 사용자 1 = 4개
	if len(msgs) != 4 {
		t.Fatalf("메시지 수 기대 4, 실제 %d", len(msgs))
	}
	if msgs[0].Role != message.RoleSystem {
		t.Errorf("msgs[0] 역할 기대 system, 실제 %q", msgs[0].Role)
	}
	if msgs[1].Role != message.RoleUser || msgs[1].Content != "이전 질문입니다." {
		t.Errorf("msgs[1] 불일치: %+v", msgs[1])
	}
	if msgs[2].Role != message.RoleAssistant || msgs[2].Content != "이전 답변입니다." {
		t.Errorf("msgs[2] 불일치: %+v", msgs[2])
	}
	if msgs[3].Role != message.RoleUser || msgs[3].Content != "새 질문입니다." {
		t.Errorf("msgs[3] 불일치: %+v", msgs[3])
	}
}

// TestFromMessages_Format_MessagesPlaceholder_Empty 는 빈 히스토리 삽입을 검증한다.
func TestFromMessages_Format_MessagesPlaceholder_Empty(t *testing.T) {
	specs := []MessageSpec{
		SpecFromRole(message.RoleSystem, "시스템입니다."),
		SpecFromPlaceholder(MessagesPlaceholder{VarName: "history"}),
		SpecFromRole(message.RoleUser, "질문입니다."),
	}
	tmpl := FromMessages(specs)
	msgs, err := tmpl.Format(map[string]any{
		"history": []message.Message{},
	})
	if err != nil {
		t.Fatalf("Format 실패: %v", err)
	}
	// 시스템 1 + 빈 히스토리 0 + 사용자 1 = 2개
	if len(msgs) != 2 {
		t.Fatalf("메시지 수 기대 2, 실제 %d", len(msgs))
	}
}

// TestFromMessages_Format_MessagesPlaceholder_MissingVar 는 MessagesPlaceholder 변수가
// vars 에 없을 때 에러가 반환됨을 검증한다.
func TestFromMessages_Format_MessagesPlaceholder_MissingVar(t *testing.T) {
	specs := []MessageSpec{
		SpecFromPlaceholder(MessagesPlaceholder{VarName: "history"}),
	}
	tmpl := FromMessages(specs)
	_, err := tmpl.Format(map[string]any{})
	if err == nil {
		t.Fatal("누락 MessagesPlaceholder 변수에서 에러가 반환되어야 합니다")
	}
}

// TestFromMessages_Format_MessagesPlaceholder_WrongType 은 변수가 []message.Message 가
// 아닐 때 에러가 반환됨을 검증한다.
func TestFromMessages_Format_MessagesPlaceholder_WrongType(t *testing.T) {
	specs := []MessageSpec{
		SpecFromPlaceholder(MessagesPlaceholder{VarName: "history"}),
	}
	tmpl := FromMessages(specs)
	_, err := tmpl.Format(map[string]any{
		"history": "잘못된 타입",
	})
	if err == nil {
		t.Fatal("잘못된 타입에서 에러가 반환되어야 합니다")
	}
}

// ── Chain.Invoke — 일반 챗 경로 ──────────────────────────────────────────────

// TestChain_Invoke_Chat 은 Pipe 로 만든 Chain.Invoke 가 stub 모델을 호출함을 검증한다.
func TestChain_Invoke_Chat(t *testing.T) {
	stubResp := llm.StubResponse{
		Message:      message.NewAssistantMessage("안녕하세요!"),
		FinishReason: "stop",
	}
	stub := llm.NewStubClient("stub-model", stubResp)

	tmpl := FromTemplate("사용자 입력: {input}")
	chain := Pipe(tmpl, stub)

	result, err := chain.Invoke(context.Background(), map[string]any{
		"input": "테스트 메시지",
	})
	if err != nil {
		t.Fatalf("Invoke 실패: %v", err)
	}

	resp, ok := result.(llm.ChatResponse)
	if !ok {
		t.Fatalf("반환 타입이 llm.ChatResponse 여야 합니다, 실제: %T", result)
	}
	if resp.Message.Content != "안녕하세요!" {
		t.Errorf("Content 기대 %q, 실제 %q", "안녕하세요!", resp.Message.Content)
	}
}

// TestChain_Invoke_Chat_WithHistory 는 MessagesPlaceholder 를 포함한 체인 Invoke 를 검증한다.
func TestChain_Invoke_Chat_WithHistory(t *testing.T) {
	stubResp := llm.StubResponse{
		Message:      message.NewAssistantMessage("잘 받았습니다."),
		FinishReason: "stop",
	}
	stub := llm.NewStubClient("stub-model", stubResp)

	history := []message.Message{
		message.NewUserMessage("이전 질문"),
		message.NewAssistantMessage("이전 답변"),
	}

	specs := []MessageSpec{
		SpecFromRole(message.RoleSystem, "도움이 되는 AI 입니다."),
		SpecFromPlaceholder(MessagesPlaceholder{VarName: "chat_history"}),
		SpecFromRole(message.RoleUser, "{input}"),
	}
	tmpl := FromMessages(specs)
	chain := Pipe(tmpl, stub)

	result, err := chain.Invoke(context.Background(), map[string]any{
		"chat_history": history,
		"input":        "현재 질문",
	})
	if err != nil {
		t.Fatalf("Invoke 실패: %v", err)
	}
	resp, ok := result.(llm.ChatResponse)
	if !ok {
		t.Fatalf("반환 타입이 llm.ChatResponse 여야 합니다, 실제: %T", result)
	}
	if resp.Message.Content != "잘 받았습니다." {
		t.Errorf("Content 기대 %q, 실제 %q", "잘 받았습니다.", resp.Message.Content)
	}
}

// ── Chain.WithStructuredOutput ────────────────────────────────────────────────

// TestChain_WithStructuredOutput 은 구조화 출력 경로가 스키마에 맞는 값을 반환함을 검증한다.
func TestChain_WithStructuredOutput(t *testing.T) {
	// 구조화 출력 타입 정의
	type SentimentResult struct {
		Sentiment string `json:"sentiment" description:"감정 분류 결과"`
		Score     int    `json:"score"     description:"0~100 신뢰도 점수"`
	}

	schema := structured.BuildSchema[SentimentResult]()

	// StructuredValue 로 직접 결과 지정
	stubResp := llm.StubResponse{
		StructuredValue: SentimentResult{Sentiment: "positive", Score: 90},
	}
	stub := llm.NewStubClient("stub-model", stubResp)

	tmpl := FromTemplate("다음 문장의 감정을 분석하세요: {text}")
	chain := Pipe(tmpl, stub).WithStructuredOutput(schema)

	result, err := chain.Invoke(context.Background(), map[string]any{
		"text": "정말 좋은 날씨입니다!",
	})
	if err != nil {
		t.Fatalf("WithStructuredOutput Invoke 실패: %v", err)
	}

	sr, ok := result.(SentimentResult)
	if !ok {
		t.Fatalf("반환 타입이 SentimentResult 여야 합니다, 실제: %T", result)
	}
	if sr.Sentiment != "positive" {
		t.Errorf("Sentiment 기대 %q, 실제 %q", "positive", sr.Sentiment)
	}
	if sr.Score != 90 {
		t.Errorf("Score 기대 90, 실제 %d", sr.Score)
	}
}

// TestChain_WithStructuredOutput_RawJSON 은 stub 이 StructuredValue 대신 JSON 문자열을
// 반환하는 경우 structured.Validate 경로를 검증한다.
func TestChain_WithStructuredOutput_RawJSON(t *testing.T) {
	type RouterOut struct {
		Next string `json:"next" description:"다음 노드 이름"`
	}

	schema := structured.BuildSchema[RouterOut](
		structured.EnumField("next", "search", "answer", "end"),
	)

	raw := mustMarshal(t, map[string]any{"next": "search"})
	stubResp := llm.StubResponse{
		Message: message.NewAssistantMessage(raw),
	}
	stub := llm.NewStubClient("stub-model", stubResp)

	tmpl := FromTemplate("다음 노드를 결정하세요.")
	chain := Pipe(tmpl, stub).WithStructuredOutput(schema)

	result, err := chain.Invoke(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("WithStructuredOutput(raw JSON) Invoke 실패: %v", err)
	}
	// Structured 경로가 map[string]any 를 반환하는지 확인
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("반환 타입이 map[string]any 여야 합니다, 실제: %T", result)
	}
	if m["next"] != "search" {
		t.Errorf("next 기대 %q, 실제 %v", "search", m["next"])
	}
}

// TestChain_WithStructuredOutput_NotMutable 은 WithStructuredOutput 이 원본 Chain 을
// 변경하지 않음을 검증한다.
func TestChain_WithStructuredOutput_NotMutable(t *testing.T) {
	type Out struct {
		Value string `json:"value"`
	}
	schema := structured.BuildSchema[Out]()

	stub := llm.NewStubClient("stub-model", llm.StubResponse{
		Message: message.NewAssistantMessage("원본 체인 응답"),
	})

	tmpl := FromTemplate("질문: {q}")
	original := Pipe(tmpl, stub)
	withStructured := original.WithStructuredOutput(schema)

	// 원본은 schema 가 nil 이어야 함
	if original.schema != nil {
		t.Error("WithStructuredOutput 이 원본 Chain 을 변경해서는 안 됩니다")
	}
	// 구조화 버전은 schema 가 설정됨
	if withStructured.schema == nil {
		t.Error("withStructured.schema 가 설정되어 있어야 합니다")
	}
}

// TestChain_Invoke_FormatError 는 템플릿 포매팅 실패 시 에러가 전파됨을 검증한다.
func TestChain_Invoke_FormatError(t *testing.T) {
	stub := llm.NewStubClient("stub-model", llm.StubResponse{
		Message: message.NewAssistantMessage("응답"),
	})

	// {missing} 변수가 vars 에 없음
	tmpl := FromTemplate("값: {missing}")
	chain := Pipe(tmpl, stub)

	_, err := chain.Invoke(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("포매팅 실패 시 에러가 반환되어야 합니다")
	}
}
