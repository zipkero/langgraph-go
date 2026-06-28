// prebuilt_test.go 는 ToolNode, ToolsCondition, SummarizationNode 의 단위 테스트다.
// stub llm.Client(llm.StubClient)와 stub tool.Registry 를 사용하며, 네트워크 호출은 없다.
package prebuilt

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/zipkero/langgraph-go/core"
	"github.com/zipkero/langgraph-go/llm"
	"github.com/zipkero/langgraph-go/message"
	"github.com/zipkero/langgraph-go/tool"
)

// --- 헬퍼 ---

// makeState 는 메시지 목록으로 core.State 를 생성하는 헬퍼다.
func makeState(msgs []message.Message) core.State {
	return core.State{messagesKey: msgs}
}

// makeStateWithSummary 는 메시지 목록과 요약 문자열로 core.State 를 생성하는 헬퍼다.
func makeStateWithSummary(msgs []message.Message, summaryKey, summary string) core.State {
	return core.State{
		messagesKey: msgs,
		summaryKey:  summary,
	}
}

// newEchoTool 은 입력 JSON 을 그대로 돌려주는 echo 도구를 생성한다.
func newEchoTool(name string) tool.Tool {
	type args struct {
		Input string `json:"input" description:"입력 문자열"`
	}
	return tool.WithArgsSchema(name, "입력을 그대로 반환하는 도구", func(ctx context.Context, a args, rt tool.Runtime) (tool.Result, error) {
		return tool.Result{Content: "echo: " + a.Input}, nil
	})
}

// --- ToolNode 테스트 ---

// TestToolNode_ExecutesPendingToolCalls 는 ToolNode 가 미처리 tool_calls 를 실행하고
// ToolMessage 를 상태에 추가하는지 검증한다.
func TestToolNode_ExecutesPendingToolCalls(t *testing.T) {
	reg := tool.NewRegistry()
	if err := reg.Register(newEchoTool("echo")); err != nil {
		t.Fatalf("도구 등록 실패: %v", err)
	}

	argsJSON, _ := json.Marshal(map[string]string{"input": "hello"})
	aiMsg := message.NewAssistantToolCalls([]message.ToolCall{
		{ID: "call-1", Name: "echo", Args: argsJSON},
	})
	st := makeState([]message.Message{
		message.NewUserMessage("안녕"),
		aiMsg,
	})

	node := NewToolNode(reg)
	update, err := node(context.Background(), st)
	if err != nil {
		t.Fatalf("ToolNode 실행 실패: %v", err)
	}

	// StateUpdate 에 messages 키가 있어야 한다
	rawMsgs, ok := update[messagesKey]
	if !ok {
		t.Fatal("StateUpdate 에 'messages' 키가 없음")
	}
	msgs, ok := rawMsgs.([]message.Message)
	if !ok {
		t.Fatal("StateUpdate 의 messages 가 []message.Message 타입이 아님")
	}

	// 마지막 메시지가 ToolMessage 여야 한다
	if len(msgs) == 0 {
		t.Fatal("업데이트된 메시지 목록이 비어 있음")
	}
	last := msgs[len(msgs)-1]
	if last.Role != message.RoleTool {
		t.Errorf("마지막 메시지 역할 = %q, 기대값 = %q", last.Role, message.RoleTool)
	}
	if last.ToolCallID != "call-1" {
		t.Errorf("ToolCallID = %q, 기대값 = %q", last.ToolCallID, "call-1")
	}
	if last.Content != "echo: hello" {
		t.Errorf("Content = %q, 기대값 = %q", last.Content, "echo: hello")
	}
}

// TestToolNode_NoPendingToolCalls 는 미처리 tool_calls 가 없으면 빈 StateUpdate 를 반환하는지 검증한다.
func TestToolNode_NoPendingToolCalls(t *testing.T) {
	reg := tool.NewRegistry()
	st := makeState([]message.Message{
		message.NewUserMessage("안녕"),
		message.NewAssistantMessage("안녕하세요"),
	})

	node := NewToolNode(reg)
	update, err := node(context.Background(), st)
	if err != nil {
		t.Fatalf("ToolNode 실행 실패: %v", err)
	}
	if len(update) != 0 {
		t.Errorf("빈 StateUpdate 가 기대되지만 %d 개 키가 있음", len(update))
	}
}

// TestToolNode_MultipleToolCalls 는 여러 tool_calls 를 모두 실행하는지 검증한다.
func TestToolNode_MultipleToolCalls(t *testing.T) {
	reg := tool.NewRegistry()
	if err := reg.RegisterMany(newEchoTool("echo1"), newEchoTool("echo2")); err != nil {
		t.Fatalf("도구 등록 실패: %v", err)
	}

	args1, _ := json.Marshal(map[string]string{"input": "a"})
	args2, _ := json.Marshal(map[string]string{"input": "b"})
	aiMsg := message.NewAssistantToolCalls([]message.ToolCall{
		{ID: "c1", Name: "echo1", Args: args1},
		{ID: "c2", Name: "echo2", Args: args2},
	})
	st := makeState([]message.Message{aiMsg})

	node := NewToolNode(reg)
	update, err := node(context.Background(), st)
	if err != nil {
		t.Fatalf("ToolNode 실행 실패: %v", err)
	}

	msgs := update[messagesKey].([]message.Message)
	// 원본 1개 + 도구 결과 2개 = 3개
	if len(msgs) != 3 {
		t.Errorf("메시지 수 = %d, 기대값 = 3", len(msgs))
	}
}

// TestToolNode_UnknownTool 은 미등록 도구 호출 시 에러 ToolMessage 가 추가되는지 검증한다.
func TestToolNode_UnknownTool(t *testing.T) {
	reg := tool.NewRegistry()

	argsJSON, _ := json.Marshal(map[string]string{"input": "x"})
	aiMsg := message.NewAssistantToolCalls([]message.ToolCall{
		{ID: "c1", Name: "unknown_tool", Args: argsJSON},
	})
	st := makeState([]message.Message{aiMsg})

	node := NewToolNode(reg)
	update, err := node(context.Background(), st)
	// ExecuteMany 는 알 수 없는 도구도 에러 ToolMessage 로 포함하므로 err == nil 이어야 한다
	if err != nil {
		t.Fatalf("ToolNode 가 예기치 않은 에러 반환: %v", err)
	}

	msgs := update[messagesKey].([]message.Message)
	last := msgs[len(msgs)-1]
	if last.Role != message.RoleTool {
		t.Errorf("미등록 도구 결과 메시지 역할 = %q, 기대값 = %q", last.Role, message.RoleTool)
	}
}

// --- ToolsCondition / HasPendingToolCalls 테스트 ---

// TestToolsCondition_WithPendingCalls 는 tool_calls 가 있을 때 "tools" 를 반환하는지 검증한다.
func TestToolsCondition_WithPendingCalls(t *testing.T) {
	argsJSON, _ := json.Marshal(map[string]string{"input": "x"})
	aiMsg := message.NewAssistantToolCalls([]message.ToolCall{
		{ID: "c1", Name: "tool", Args: argsJSON},
	})
	st := makeState([]message.Message{aiMsg})

	got := ToolsCondition(context.Background(), st)
	if got != "tools" {
		t.Errorf("ToolsCondition = %q, 기대값 = %q", got, "tools")
	}
}

// TestToolsCondition_WithoutPendingCalls 는 tool_calls 가 없을 때 "END" 를 반환하는지 검증한다.
func TestToolsCondition_WithoutPendingCalls(t *testing.T) {
	st := makeState([]message.Message{
		message.NewAssistantMessage("완료"),
	})

	got := ToolsCondition(context.Background(), st)
	if got != "END" {
		t.Errorf("ToolsCondition = %q, 기대값 = %q", got, "END")
	}
}

// TestHasPendingToolCalls 는 HasPendingToolCalls 의 참/거짓 케이스를 검증한다.
func TestHasPendingToolCalls(t *testing.T) {
	argsJSON, _ := json.Marshal(map[string]string{"input": "x"})

	t.Run("tool_calls 있음", func(t *testing.T) {
		st := makeState([]message.Message{
			message.NewAssistantToolCalls([]message.ToolCall{
				{ID: "c1", Name: "tool", Args: argsJSON},
			}),
		})
		if !HasPendingToolCalls(st) {
			t.Error("HasPendingToolCalls = false, 기대값 = true")
		}
	})

	t.Run("tool_calls 없음", func(t *testing.T) {
		st := makeState([]message.Message{
			message.NewAssistantMessage("텍스트 응답"),
		})
		if HasPendingToolCalls(st) {
			t.Error("HasPendingToolCalls = true, 기대값 = false")
		}
	})

	t.Run("메시지 없음", func(t *testing.T) {
		st := makeState([]message.Message{})
		if HasPendingToolCalls(st) {
			t.Error("HasPendingToolCalls = true, 기대값 = false")
		}
	})
}

// --- ShouldSummarize 테스트 ---

// TestShouldSummarize_MessageThreshold 는 메시지 수 임계 초과를 판정하는지 검증한다.
func TestShouldSummarize_MessageThreshold(t *testing.T) {
	msgs := []message.Message{
		message.NewUserMessage("msg1"),
		message.NewAssistantMessage("resp1"),
		message.NewUserMessage("msg2"),
		message.NewAssistantMessage("resp2"),
		message.NewUserMessage("msg3"),
	}
	st := makeState(msgs)
	opts := SummarizeOptions{MaxMessages: 4}

	if !ShouldSummarize(st, opts) {
		t.Error("ShouldSummarize = false, 기대값 = true (5개 메시지 > 임계 4)")
	}
}

// TestShouldSummarize_BelowThreshold 는 임계 미만이면 false 를 반환하는지 검증한다.
func TestShouldSummarize_BelowThreshold(t *testing.T) {
	msgs := []message.Message{
		message.NewUserMessage("msg1"),
		message.NewAssistantMessage("resp1"),
	}
	st := makeState(msgs)
	opts := SummarizeOptions{MaxMessages: 10}

	if ShouldSummarize(st, opts) {
		t.Error("ShouldSummarize = true, 기대값 = false (2개 메시지 <= 임계 10)")
	}
}

// TestShouldSummarize_TokenThreshold 는 토큰 수 임계 초과를 판정하는지 검증한다.
func TestShouldSummarize_TokenThreshold(t *testing.T) {
	// 긴 내용으로 토큰 수를 높인다
	longContent := "이것은 매우 긴 메시지입니다. " + string(make([]byte, 200))
	msgs := []message.Message{
		message.NewUserMessage(longContent),
		message.NewAssistantMessage(longContent),
	}
	st := makeState(msgs)
	// 실제 토큰 수보다 낮은 임계를 설정
	opts := SummarizeOptions{MaxTokens: 1}

	if !ShouldSummarize(st, opts) {
		t.Error("ShouldSummarize = false, 기대값 = true (토큰 수 > 임계 1)")
	}
}

// TestShouldSummarize_BothZero 는 MaxMessages=0, MaxTokens=0 이면 false 를 반환하는지 검증한다.
func TestShouldSummarize_BothZero(t *testing.T) {
	msgs := []message.Message{
		message.NewUserMessage("msg1"),
	}
	st := makeState(msgs)
	opts := SummarizeOptions{}

	if ShouldSummarize(st, opts) {
		t.Error("ShouldSummarize = true, 기대값 = false (임계 미설정)")
	}
}

// --- InjectSummary 테스트 ---

// TestInjectSummary_PrependsSummaryAsSystemMessage 는 요약이 SystemMessage 로 앞에 주입되는지 검증한다.
func TestInjectSummary_PrependsSummaryAsSystemMessage(t *testing.T) {
	msgs := []message.Message{
		message.NewUserMessage("안녕"),
		message.NewAssistantMessage("안녕하세요"),
	}
	summary := "이전 대화에서 사용자는 인사를 나눴다."

	result := InjectSummary(msgs, summary)

	if len(result) != 3 {
		t.Fatalf("메시지 수 = %d, 기대값 = 3", len(result))
	}
	if result[0].Role != message.RoleSystem {
		t.Errorf("첫 번째 메시지 역할 = %q, 기대값 = %q", result[0].Role, message.RoleSystem)
	}
	if result[1].Role != message.RoleUser {
		t.Errorf("두 번째 메시지 역할 = %q, 기대값 = %q", result[1].Role, message.RoleUser)
	}
}

// TestInjectSummary_EmptySummary 는 요약이 빈 문자열이면 msgs 를 그대로 반환하는지 검증한다.
func TestInjectSummary_EmptySummary(t *testing.T) {
	msgs := []message.Message{
		message.NewUserMessage("안녕"),
	}

	result := InjectSummary(msgs, "")

	if len(result) != 1 {
		t.Errorf("메시지 수 = %d, 기대값 = 1", len(result))
	}
}

// --- SummarizationNode 테스트 ---

// TestSummarizationNode_SummarizesAndRemovesOldMessages 는
// 임계 초과 시 LLM 으로 요약하고 과거 메시지를 제거하며 summary 를 저장하는지 검증한다.
func TestSummarizationNode_SummarizesAndRemovesOldMessages(t *testing.T) {
	summaryText := "이전 대화 요약: 사용자와 어시스턴트가 대화했다."
	stubModel := llm.NewStubClient("stub", llm.StubResponse{
		Message: message.NewAssistantMessage(summaryText),
	})

	msgs := []message.Message{
		message.NewUserMessage("msg1"),
		message.NewAssistantMessage("resp1"),
		message.NewUserMessage("msg2"),
		message.NewAssistantMessage("resp2"),
		message.NewUserMessage("msg3"),
	}
	st := makeState(msgs)
	opts := SummarizeOptions{
		MaxMessages: 4,
		KeepLast:    2,
		SummaryKey:  "summary",
	}

	node := NewSummarizationNode(stubModel, opts)
	update, err := node(context.Background(), st)
	if err != nil {
		t.Fatalf("SummarizationNode 실행 실패: %v", err)
	}

	// summary 키에 요약 문자열이 저장되어야 한다
	rawSummary, ok := update["summary"]
	if !ok {
		t.Fatal("StateUpdate 에 'summary' 키가 없음")
	}
	gotSummary, ok := rawSummary.(string)
	if !ok {
		t.Fatal("summary 가 string 타입이 아님")
	}
	if gotSummary != summaryText {
		t.Errorf("summary = %q, 기대값 = %q", gotSummary, summaryText)
	}

	// messages 는 KeepLast=2 만큼만 보존되어야 한다
	rawMsgs, ok := update[messagesKey]
	if !ok {
		t.Fatal("StateUpdate 에 'messages' 키가 없음")
	}
	newMsgs, ok := rawMsgs.([]message.Message)
	if !ok {
		t.Fatal("messages 가 []message.Message 타입이 아님")
	}
	if len(newMsgs) != 2 {
		t.Errorf("남은 메시지 수 = %d, 기대값 = 2 (KeepLast=2)", len(newMsgs))
	}
}

// TestSummarizationNode_BelowThreshold 는 임계 미만이면 StateUpdate 가 비어 있는지 검증한다.
func TestSummarizationNode_BelowThreshold(t *testing.T) {
	stubModel := llm.NewStubClient("stub", llm.StubResponse{
		Message: message.NewAssistantMessage("요약"),
	})

	msgs := []message.Message{
		message.NewUserMessage("msg1"),
		message.NewAssistantMessage("resp1"),
	}
	st := makeState(msgs)
	opts := SummarizeOptions{MaxMessages: 10}

	node := NewSummarizationNode(stubModel, opts)
	update, err := node(context.Background(), st)
	if err != nil {
		t.Fatalf("SummarizationNode 실행 실패: %v", err)
	}
	if len(update) != 0 {
		t.Errorf("임계 미만에서 빈 StateUpdate 가 기대되지만 %d 개 키가 있음", len(update))
	}
}

// TestSummarizationNode_RemovesAllWhenKeepLastZero 는 KeepLast=0 이면 모든 메시지를 제거하는지 검증한다.
func TestSummarizationNode_RemovesAllWhenKeepLastZero(t *testing.T) {
	summaryText := "전체 요약"
	stubModel := llm.NewStubClient("stub", llm.StubResponse{
		Message: message.NewAssistantMessage(summaryText),
	})

	msgs := []message.Message{
		message.NewUserMessage("msg1"),
		message.NewAssistantMessage("resp1"),
		message.NewUserMessage("msg2"),
	}
	st := makeState(msgs)
	opts := SummarizeOptions{
		MaxMessages: 2,
		KeepLast:    0,
	}

	node := NewSummarizationNode(stubModel, opts)
	update, err := node(context.Background(), st)
	if err != nil {
		t.Fatalf("SummarizationNode 실행 실패: %v", err)
	}

	rawMsgs, ok := update[messagesKey]
	if !ok {
		t.Fatal("StateUpdate 에 'messages' 키가 없음")
	}
	newMsgs := rawMsgs.([]message.Message)
	if len(newMsgs) != 0 {
		t.Errorf("KeepLast=0 일 때 남은 메시지 수 = %d, 기대값 = 0", len(newMsgs))
	}
}

// TestSummarizationNode_DefaultSummaryKey 는 SummaryKey 가 비어 있으면 "summary" 키를 사용하는지 검증한다.
func TestSummarizationNode_DefaultSummaryKey(t *testing.T) {
	stubModel := llm.NewStubClient("stub", llm.StubResponse{
		Message: message.NewAssistantMessage("기본 키 요약"),
	})

	msgs := make([]message.Message, 5)
	for i := range msgs {
		msgs[i] = message.NewUserMessage("msg")
	}
	st := makeState(msgs)
	opts := SummarizeOptions{MaxMessages: 4} // SummaryKey 비워둠

	node := NewSummarizationNode(stubModel, opts)
	update, err := node(context.Background(), st)
	if err != nil {
		t.Fatalf("SummarizationNode 실행 실패: %v", err)
	}

	if _, ok := update["summary"]; !ok {
		t.Error("기본 키 'summary' 가 StateUpdate 에 없음")
	}
}

// TestSummarizationNode_WithIDMessages 는 ID 있는 메시지를 RemoveMessage+ApplyRemovals 로 제거하는지 검증한다.
func TestSummarizationNode_WithIDMessages(t *testing.T) {
	summaryText := "ID 메시지 요약"
	stubModel := llm.NewStubClient("stub", llm.StubResponse{
		Message: message.NewAssistantMessage(summaryText),
	})

	msgs := []message.Message{
		{Role: message.RoleUser, Content: "msg1", ID: "id-1"},
		{Role: message.RoleAssistant, Content: "resp1", ID: "id-2"},
		{Role: message.RoleUser, Content: "msg2", ID: "id-3"},
		{Role: message.RoleAssistant, Content: "resp2", ID: "id-4"},
		{Role: message.RoleUser, Content: "msg3", ID: "id-5"},
	}
	st := makeState(msgs)
	opts := SummarizeOptions{
		MaxMessages: 4,
		KeepLast:    2,
		SummaryKey:  "summary",
	}

	node := NewSummarizationNode(stubModel, opts)
	update, err := node(context.Background(), st)
	if err != nil {
		t.Fatalf("SummarizationNode 실행 실패: %v", err)
	}

	rawMsgs, ok := update[messagesKey]
	if !ok {
		t.Fatal("StateUpdate 에 'messages' 키가 없음")
	}
	newMsgs := rawMsgs.([]message.Message)
	// KeepLast=2: 마지막 2개(id-4, id-5)만 남아야 한다
	if len(newMsgs) != 2 {
		t.Errorf("남은 메시지 수 = %d, 기대값 = 2", len(newMsgs))
	}
	if newMsgs[0].ID != "id-4" {
		t.Errorf("첫 번째 남은 메시지 ID = %q, 기대값 = %q", newMsgs[0].ID, "id-4")
	}
}

// TestInjectSummary_ContentContainsSummary 는 주입된 SystemMessage 내용에 요약이 포함되는지 검증한다.
func TestInjectSummary_ContentContainsSummary(t *testing.T) {
	summary := "핵심 요약 내용"
	result := InjectSummary(nil, summary)

	if len(result) != 1 {
		t.Fatalf("메시지 수 = %d, 기대값 = 1", len(result))
	}
	if result[0].Role != message.RoleSystem {
		t.Errorf("역할 = %q, 기대값 = %q", result[0].Role, message.RoleSystem)
	}
	// 요약 내용이 SystemMessage Content 에 포함되어야 한다
	if !containsString(result[0].Content, summary) {
		t.Errorf("SystemMessage Content 에 요약 %q 가 포함되지 않음: %q", summary, result[0].Content)
	}
}

// containsString 은 s 에 substr 이 포함되는지 반환하는 헬퍼다.
func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(substr); i++ {
				if s[i:i+len(substr)] == substr {
					return true
				}
			}
			return false
		}())
}
