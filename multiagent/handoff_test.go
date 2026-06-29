// handoff_test.go 는 CreateHandoffTool/HandoffBackMessages 의 동작을
// stub Runtime/메시지로 결정적으로 검증한다. 네트워크·LLM 호출이 없다.
// SPEC §5.5, ANALYSIS §2·§5.3·§5.4 참조.
package multiagent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/zipkero/langgraph-go/config"
	"github.com/zipkero/langgraph-go/graph/command"
	"github.com/zipkero/langgraph-go/message"
	"github.com/zipkero/langgraph-go/tool"
)

// stubRuntime 은 테스트용 tool.Runtime 구현체다.
// State() 에 임의 상태를 주입할 수 있으며 다른 메서드는 no-op 이다.
type stubRuntime struct {
	state      any
	toolCallID string
}

func (r *stubRuntime) State() any                { return r.state }
func (r *stubRuntime) ToolCallID() string        { return r.toolCallID }
func (r *stubRuntime) Config() config.RunConfig  { return config.RunConfig{} }
func (r *stubRuntime) Store() tool.Store         { return nil }
func (r *stubRuntime) Emit(_ tool.Event)         {}

// makeStateWithToolCalls 는 단일 또는 복수 tool_calls 를 가진 AI 메시지를 담은
// map[string]any 상태를 생성해 반환한다.
func makeStateWithToolCalls(calls []message.ToolCall) map[string]any {
	msg := message.NewAssistantToolCalls(calls)
	return map[string]any{
		"messages": []message.Message{msg},
	}
}

// makeHandoffCall 은 agentName 과 query 를 담은 ToolCall 을 생성한다.
func makeHandoffCall(id, agentName, query string) message.ToolCall {
	args, _ := json.Marshal(handoffArgs{AgentName: agentName, Query: query})
	return message.ToolCall{
		ID:   id,
		Name: "handoff_to_" + agentName,
		Args: args,
	}
}

// ---- CreateHandoffTool 기본 동작 ----

// TestCreateHandoffTool_ReturnsToolWithCorrectName 은 CreateHandoffTool 이 올바른 이름의 Tool 을 반환하는지 검증한다.
func TestCreateHandoffTool_ReturnsToolWithCorrectName(t *testing.T) {
	ht := CreateHandoffTool("worker-a", "워커 A에 위임")
	wantName := "handoff_to_worker-a"
	if ht.Name() != wantName {
		t.Errorf("Name(): 기대 %q, 실제 %q", wantName, ht.Name())
	}
	if ht.Description() == "" {
		t.Error("Description() 이 비어 있음")
	}
}

// TestCreateHandoffTool_ImplementsToolInterface 는 CreateHandoffTool 반환값이 tool.Tool 을 충족하는지 검증한다.
func TestCreateHandoffTool_ImplementsToolInterface(t *testing.T) {
	var _ tool.Tool = CreateHandoffTool("worker-a", "설명")
}

// TestCreateHandoffTool_ImplementsHandoffToolInterface 는 CreateHandoffTool 반환값이 HandoffTool 을 충족하는지 검증한다.
func TestCreateHandoffTool_ImplementsHandoffToolInterface(t *testing.T) {
	var _ HandoffTool = CreateHandoffTool("worker-a", "설명")
}

// TestCreateHandoffTool_Execute_ReturnsResult 는 Execute 가 성공 Result 를 반환하는지 검증한다.
func TestCreateHandoffTool_Execute_ReturnsResult(t *testing.T) {
	ht := CreateHandoffTool("worker-a", "워커 A에 위임")

	args, _ := json.Marshal(handoffArgs{AgentName: "worker-a", Query: "작업 수행"})
	res, err := ht.Execute(context.Background(), args, nil)
	if err != nil {
		t.Fatalf("Execute 에러: %v", err)
	}
	if res.IsError {
		t.Errorf("Execute: IsError=true, Content=%q", res.Content)
	}
	if res.Content == "" {
		t.Error("Execute: Content 가 비어 있음")
	}
}

// ---- 단일 위임: Command.ToParent ----

// TestHandoffTool_Command_SingleCall_ReturnsToParent 는 tool_calls 가 1개일 때
// Command 가 ToParent(IsParent=true) 를 반환하는지 검증한다.
func TestHandoffTool_Command_SingleCall_ReturnsToParent(t *testing.T) {
	ht := CreateHandoffTool("worker-a", "워커 A에 위임")

	singleCall := makeHandoffCall("call-001", "worker-a", "질문 처리")
	rt := &stubRuntime{
		state: makeStateWithToolCalls([]message.ToolCall{singleCall}),
	}

	cmd, err := ht.Command(rt)
	if err != nil {
		t.Fatalf("Command 에러: %v", err)
	}

	// 단일 위임: IsParent=true, IsEnd=false
	if !cmd.IsParent() {
		t.Errorf("단일 위임: IsParent() 기대 true, 실제 false (Graph=%q)", cmd.Graph)
	}
	if cmd.IsEnd() {
		t.Error("단일 위임: IsEnd() 기대 false, 실제 true")
	}
	// Sends 는 비어 있어야 한다
	if len(cmd.Sends) != 0 {
		t.Errorf("단일 위임: Sends 기대 0개, 실제 %d개", len(cmd.Sends))
	}
}

// TestHandoffTool_Command_SingleCall_GotoIsAgentName 은 단일 위임 시 Goto 가 agentName 인지 검증한다.
func TestHandoffTool_Command_SingleCall_GotoIsAgentName(t *testing.T) {
	ht := CreateHandoffTool("worker-a", "워커 A에 위임")

	singleCall := makeHandoffCall("call-001", "worker-a", "질문")
	rt := &stubRuntime{state: makeStateWithToolCalls([]message.ToolCall{singleCall})}

	cmd, err := ht.Command(rt)
	if err != nil {
		t.Fatalf("Command 에러: %v", err)
	}

	if cmd.Goto != "worker-a" {
		t.Errorf("단일 위임 Goto: 기대 %q, 실제 %q", "worker-a", cmd.Goto)
	}
}

// TestHandoffTool_Command_NilRuntime_ReturnsToParent 는 Runtime 이 nil 일 때
// 단일 위임(ToParent) 으로 fallback 하는지 검증한다.
func TestHandoffTool_Command_NilRuntime_ReturnsToParent(t *testing.T) {
	ht := CreateHandoffTool("worker-a", "워커 A에 위임")

	cmd, err := ht.Command(nil)
	if err != nil {
		t.Fatalf("Command(nil rt) 에러: %v", err)
	}
	if !cmd.IsParent() {
		t.Errorf("nil Runtime: IsParent() 기대 true, 실제 false")
	}
}

// ---- 복수 위임: Command.Fanout ----

// TestHandoffTool_Command_MultipleCalls_ReturnsFanout 은 tool_calls 가 2개 이상이면
// Command 가 Fanout([]Send) 를 반환하는지 검증한다.
func TestHandoffTool_Command_MultipleCalls_ReturnsFanout(t *testing.T) {
	ht := CreateHandoffTool("worker-a", "워커 A에 위임")

	calls := []message.ToolCall{
		makeHandoffCall("call-001", "worker-a", "질문 A"),
		makeHandoffCall("call-002", "worker-b", "질문 B"),
	}
	rt := &stubRuntime{state: makeStateWithToolCalls(calls)}

	cmd, err := ht.Command(rt)
	if err != nil {
		t.Fatalf("Command 에러: %v", err)
	}

	// Fanout: Sends 가 2개, IsEnd=false
	if len(cmd.Sends) != 2 {
		t.Fatalf("Fanout: Sends 기대 2개, 실제 %d개", len(cmd.Sends))
	}
	if cmd.IsEnd() {
		t.Error("Fanout: IsEnd() 기대 false, 실제 true")
	}
}

// TestHandoffTool_Command_MultipleCalls_EachSendTargetsParent 는 Fanout 의 각 Send 가
// TargetParent 대상인지 검증한다.
func TestHandoffTool_Command_MultipleCalls_EachSendTargetsParent(t *testing.T) {
	ht := CreateHandoffTool("worker-a", "워커 A에 위임")

	calls := []message.ToolCall{
		makeHandoffCall("call-001", "worker-a", "질문 A"),
		makeHandoffCall("call-002", "worker-b", "질문 B"),
	}
	rt := &stubRuntime{state: makeStateWithToolCalls(calls)}

	cmd, err := ht.Command(rt)
	if err != nil {
		t.Fatalf("Command 에러: %v", err)
	}

	for i, s := range cmd.Sends {
		if s.Graph != command.TargetParent {
			t.Errorf("Send[%d].Graph: 기대 TargetParent, 실제 %q", i, s.Graph)
		}
	}
}

// TestHandoffTool_Command_MultipleCalls_DifferentInputPerWorker 는 Fanout 의 각 Send 가
// 워커마다 다른 query 를 담은 State 를 가지는지 검증한다.
func TestHandoffTool_Command_MultipleCalls_DifferentInputPerWorker(t *testing.T) {
	ht := CreateHandoffTool("worker-a", "위임")

	calls := []message.ToolCall{
		makeHandoffCall("call-001", "worker-a", "질문 A"),
		makeHandoffCall("call-002", "worker-b", "질문 B"),
	}
	rt := &stubRuntime{state: makeStateWithToolCalls(calls)}

	cmd, err := ht.Command(rt)
	if err != nil {
		t.Fatalf("Command 에러: %v", err)
	}

	if len(cmd.Sends) != 2 {
		t.Fatalf("Sends 개수 기대 2, 실제 %d", len(cmd.Sends))
	}

	// 각 Send 의 State 에서 messages 첫 번째 메시지의 Content 가 query 와 일치하는지 검증한다.
	for i, s := range cmd.Sends {
		stMap, ok := s.State.(map[string]any)
		if !ok {
			t.Fatalf("Send[%d].State 가 map[string]any 가 아님: %T", i, s.State)
		}
		msgs, ok := stMap["messages"].([]message.Message)
		if !ok || len(msgs) == 0 {
			t.Fatalf("Send[%d].State[messages] 가 비어 있거나 타입 불일치", i)
		}
		expectedQueries := []string{"질문 A", "질문 B"}
		if msgs[0].Content != expectedQueries[i] {
			t.Errorf("Send[%d] query: 기대 %q, 실제 %q", i, expectedQueries[i], msgs[0].Content)
		}
	}
}

// TestHandoffTool_Command_MultipleCalls_DifferentTargetsPerSend 는 Fanout 의 각 Send 가
// 다른 워커 이름을 Target 으로 가지는지 검증한다.
func TestHandoffTool_Command_MultipleCalls_DifferentTargetsPerSend(t *testing.T) {
	ht := CreateHandoffTool("worker-a", "위임")

	calls := []message.ToolCall{
		makeHandoffCall("call-001", "worker-a", "질문 A"),
		makeHandoffCall("call-002", "worker-b", "질문 B"),
	}
	rt := &stubRuntime{state: makeStateWithToolCalls(calls)}

	cmd, err := ht.Command(rt)
	if err != nil {
		t.Fatalf("Command 에러: %v", err)
	}

	targets := map[string]bool{}
	for _, s := range cmd.Sends {
		targets[s.Target] = true
	}

	if !targets["worker-a"] {
		t.Error("Fanout: worker-a 가 Send Target 에 없음")
	}
	if !targets["worker-b"] {
		t.Error("Fanout: worker-b 가 Send Target 에 없음")
	}
}

// ---- HandoffBackMessages ----

// TestHandoffBackMessages_ReturnsTwoMessages 는 HandoffBackMessages 가 2개 메시지를 반환하는지 검증한다.
func TestHandoffBackMessages_ReturnsTwoMessages(t *testing.T) {
	msgs := HandoffBackMessages("worker-a", "call-001", "워커 A 완료 결과")
	if len(msgs) != 2 {
		t.Fatalf("HandoffBackMessages: 2개 메시지 기대, 실제 %d개", len(msgs))
	}
}

// TestHandoffBackMessages_FirstIsAssistantWithToolCalls 는 첫 번째 메시지가 AI tool_calls 메시지인지 검증한다.
func TestHandoffBackMessages_FirstIsAssistantWithToolCalls(t *testing.T) {
	msgs := HandoffBackMessages("worker-a", "call-001", "결과")

	aiMsg := msgs[0]
	if aiMsg.Role != message.RoleAssistant {
		t.Errorf("msgs[0].Role: 기대 assistant, 실제 %q", aiMsg.Role)
	}
	if len(aiMsg.ToolCalls) == 0 {
		t.Error("msgs[0]: ToolCalls 가 비어 있음")
	}
}

// TestHandoffBackMessages_SecondIsToolMessage 는 두 번째 메시지가 Tool 역할 메시지인지 검증한다.
func TestHandoffBackMessages_SecondIsToolMessage(t *testing.T) {
	msgs := HandoffBackMessages("worker-a", "call-001", "결과")

	toolMsg := msgs[1]
	if toolMsg.Role != message.RoleTool {
		t.Errorf("msgs[1].Role: 기대 tool, 실제 %q", toolMsg.Role)
	}
}

// TestHandoffBackMessages_ToolCallIDPaired 는 AI 메시지의 ToolCalls[0].ID 와
// Tool 메시지의 ToolCallID 가 동일한지 검증한다.
func TestHandoffBackMessages_ToolCallIDPaired(t *testing.T) {
	wantID := "call-xyz-001"
	msgs := HandoffBackMessages("worker-a", wantID, "결과")

	aiMsg := msgs[0]
	toolMsg := msgs[1]

	if len(aiMsg.ToolCalls) == 0 {
		t.Fatal("aiMsg.ToolCalls 가 비어 있음")
	}
	aiCallID := aiMsg.ToolCalls[0].ID
	if aiCallID != wantID {
		t.Errorf("AI ToolCalls[0].ID: 기대 %q, 실제 %q", wantID, aiCallID)
	}
	if toolMsg.ToolCallID != wantID {
		t.Errorf("Tool ToolCallID: 기대 %q, 실제 %q", wantID, toolMsg.ToolCallID)
	}
}

// TestHandoffBackMessages_AgentNameInMessages 는 AI/Tool 메시지에 agentName 이 반영되는지 검증한다.
func TestHandoffBackMessages_AgentNameInMessages(t *testing.T) {
	agentName := "worker-a"
	msgs := HandoffBackMessages(agentName, "call-001", "결과")

	aiMsg := msgs[0]
	toolMsg := msgs[1]

	if len(aiMsg.ToolCalls) == 0 {
		t.Fatal("aiMsg.ToolCalls 가 비어 있음")
	}
	if aiMsg.ToolCalls[0].Name != agentName {
		t.Errorf("AI ToolCalls[0].Name: 기대 %q, 실제 %q", agentName, aiMsg.ToolCalls[0].Name)
	}
	if toolMsg.Name != agentName {
		t.Errorf("Tool.Name: 기대 %q, 실제 %q", agentName, toolMsg.Name)
	}
}

// TestHandoffBackMessages_ResultInToolContent 는 Tool 메시지의 Content 가 result 와 일치하는지 검증한다.
func TestHandoffBackMessages_ResultInToolContent(t *testing.T) {
	wantResult := "워커 A가 생성한 최종 답변"
	msgs := HandoffBackMessages("worker-a", "call-001", wantResult)

	toolMsg := msgs[1]
	if toolMsg.Content != wantResult {
		t.Errorf("Tool.Content: 기대 %q, 실제 %q", wantResult, toolMsg.Content)
	}
}

// TestHandoffBackMessages_MultipleCallsProduceSeparatePairs 는 서로 다른 toolCallID 로
// HandoffBackMessages 를 두 번 호출하면 ID 가 각각 짝지어지는지 검증한다.
func TestHandoffBackMessages_MultipleCallsProduceSeparatePairs(t *testing.T) {
	msgs1 := HandoffBackMessages("worker-a", "id-001", "결과1")
	msgs2 := HandoffBackMessages("worker-b", "id-002", "결과2")

	// 각 쌍 내에서 ID 가 짝지어지는지 확인
	if msgs1[0].ToolCalls[0].ID != "id-001" || msgs1[1].ToolCallID != "id-001" {
		t.Error("msgs1 쌍: ToolCallID 불일치")
	}
	if msgs2[0].ToolCalls[0].ID != "id-002" || msgs2[1].ToolCallID != "id-002" {
		t.Error("msgs2 쌍: ToolCallID 불일치")
	}

	// 두 쌍의 ID 가 서로 다른지 확인
	if msgs1[0].ToolCalls[0].ID == msgs2[0].ToolCalls[0].ID {
		t.Error("두 쌍의 ToolCallID 가 같음 — 분리돼야 함")
	}
}
