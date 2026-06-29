// router_test.go 는 RouterTool/SelectNext/Route/MergeWorkerResult 의 동작을
// stub LLM 응답으로 결정적으로 검증한다. 네트워크·LLM 호출이 없다.
// SPEC §5.4, ANALYSIS §2·§5.1·§5.2·§5.5 참조.
package multiagent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/zipkero/langgraph-go/graph"
	"github.com/zipkero/langgraph-go/message"
)

// makeRouterState 는 테스트용 graph.State를 생성한다.
// toolCallName이 비어 있으면 tool_calls 없는 AI 메시지를 넣는다.
// toolCallName이 있으면 해당 이름의 라우터 도구 호출을 가진 AI 메시지를 넣는다.
func makeRouterState(toolCallName, workerName string) graph.State {
	if toolCallName == "" {
		// 라우터 도구 미호출: 일반 AI 텍스트 메시지
		msgs := []message.Message{
			message.NewAssistantMessage("작업 완료"),
		}
		return graph.State{"messages": msgs}
	}

	// 라우터 도구 호출: tool_calls가 있는 AI 메시지
	args, _ := json.Marshal(map[string]string{"next": workerName})
	calls := []message.ToolCall{
		{ID: "call-001", Name: toolCallName, Args: args},
	}
	msgs := []message.Message{
		message.NewAssistantToolCalls(calls),
	}
	return graph.State{"messages": msgs}
}

// TestRouterTool_Create 는 RouterTool이 올바른 이름을 가진 Tool을 반환하는지 검증한다.
func TestRouterTool_Create(t *testing.T) {
	rt := RouterTool("worker-a", "worker-b")
	if rt.Name() != "route_to_worker" {
		t.Errorf("RouterTool 이름: 기대 route_to_worker, 실제 %q", rt.Name())
	}
	if rt.Description() == "" {
		t.Error("RouterTool Description이 비어 있음")
	}
}

// TestRouterTool_Execute_ValidChoice 는 허용된 워커 이름으로 실행 시 성공을 반환하는지 검증한다.
func TestRouterTool_Execute_ValidChoice(t *testing.T) {
	rt := RouterTool("worker-a", "worker-b")

	args, _ := json.Marshal(routerArgs{Next: "worker-a"})
	res, err := rt.Execute(context.Background(), args, nil)
	if err != nil {
		t.Fatalf("Execute 에러: %v", err)
	}
	if res.IsError {
		t.Errorf("허용된 워커인데 IsError=true: %s", res.Content)
	}
	if res.Content != "worker-a" {
		t.Errorf("Content: 기대 worker-a, 실제 %q", res.Content)
	}
}

// TestRouterTool_Execute_InvalidChoice 는 허용되지 않은 워커 이름 시 IsError를 반환하는지 검증한다.
func TestRouterTool_Execute_InvalidChoice(t *testing.T) {
	rt := RouterTool("worker-a", "worker-b")

	args, _ := json.Marshal(routerArgs{Next: "unknown-worker"})
	res, err := rt.Execute(context.Background(), args, nil)
	if err != nil {
		t.Fatalf("Execute 에러: %v", err)
	}
	if !res.IsError {
		t.Error("허용되지 않은 워커인데 IsError=false")
	}
}

// TestRouterTool_NoChoices 는 choices 없이 생성된 RouterTool이 임의 이름을 수락하는지 검증한다.
func TestRouterTool_NoChoices(t *testing.T) {
	rt := RouterTool() // choices 없음 → enum 제약 없음

	args, _ := json.Marshal(routerArgs{Next: "any-worker"})
	res, err := rt.Execute(context.Background(), args, nil)
	if err != nil {
		t.Fatalf("Execute 에러: %v", err)
	}
	if res.IsError {
		t.Errorf("choices 없는 RouterTool인데 IsError=true: %s", res.Content)
	}
}

// TestSelectNext_WithRouterCall 은 라우터 도구 호출 AI 메시지에서 next 이름을 뽑는지 검증한다.
func TestSelectNext_WithRouterCall(t *testing.T) {
	st := makeRouterState("route_to_worker", "worker-a")

	next, err := SelectNext(context.Background(), st)
	if err != nil {
		t.Fatalf("SelectNext 에러: %v", err)
	}
	if next != "worker-a" {
		t.Errorf("SelectNext: 기대 worker-a, 실제 %q", next)
	}
}

// TestSelectNext_WithoutRouterCall 은 라우터 도구 미호출 시 빈 문자열을 반환하는지 검증한다.
func TestSelectNext_WithoutRouterCall(t *testing.T) {
	st := makeRouterState("", "") // tool_calls 없는 AI 메시지

	next, err := SelectNext(context.Background(), st)
	if err != nil {
		t.Fatalf("SelectNext 에러: %v", err)
	}
	if next != "" {
		t.Errorf("SelectNext: 미호출이면 빈 문자열 기대, 실제 %q", next)
	}
}

// TestSelectNext_EmptyState 는 메시지가 없는 상태에서 빈 문자열을 반환하는지 검증한다.
func TestSelectNext_EmptyState(t *testing.T) {
	st := graph.State{}

	next, err := SelectNext(context.Background(), st)
	if err != nil {
		t.Fatalf("SelectNext 에러: %v", err)
	}
	if next != "" {
		t.Errorf("SelectNext: 빈 상태이면 빈 문자열 기대, 실제 %q", next)
	}
}

// TestRoute_GotoOnRouterCall 은 라우터 호출 시 Goto 커맨드를 반환하는지 검증한다.
func TestRoute_GotoOnRouterCall(t *testing.T) {
	reg := NewWorkerRegistry()
	_ = reg.RegisterWorker(&stubWorker{name: "worker-a", desc: "워커 A"})

	st := makeRouterState("route_to_worker", "worker-a")

	cmd, err := Route(context.Background(), st, reg)
	if err != nil {
		t.Fatalf("Route 에러: %v", err)
	}
	if cmd.IsEnd() {
		t.Error("Route: 라우터 호출인데 End 반환")
	}
	if cmd.Goto != "worker-a" {
		t.Errorf("Route Goto: 기대 worker-a, 실제 %q", cmd.Goto)
	}
}

// TestRoute_EndOnNoRouterCall 은 라우터 미호출 시 End 커맨드를 반환하는지 검증한다.
func TestRoute_EndOnNoRouterCall(t *testing.T) {
	reg := NewWorkerRegistry()

	st := makeRouterState("", "") // tool_calls 없음

	cmd, err := Route(context.Background(), st, reg)
	if err != nil {
		t.Fatalf("Route 에러: %v", err)
	}
	if !cmd.IsEnd() {
		t.Errorf("Route: 미호출이면 End 기대, Goto=%q", cmd.Goto)
	}
}

// TestRoute_ErrorOnUnregisteredWorker 는 미등록 워커 이름 시 에러를 반환하는지 검증한다.
func TestRoute_ErrorOnUnregisteredWorker(t *testing.T) {
	reg := NewWorkerRegistry()
	// worker-a 는 등록하지 않음

	st := makeRouterState("route_to_worker", "worker-a")

	_, err := Route(context.Background(), st, reg)
	if err == nil {
		t.Error("미등록 워커: 에러 기대, 그런데 nil 반환")
	}
}

// TestRoute_NilRegistry 는 reg가 nil일 때 워커 검증 없이 Goto를 반환하는지 검증한다.
func TestRoute_NilRegistry(t *testing.T) {
	st := makeRouterState("route_to_worker", "any-worker")

	cmd, err := Route(context.Background(), st, nil)
	if err != nil {
		t.Fatalf("Route(nil reg) 에러: %v", err)
	}
	if cmd.IsEnd() {
		t.Error("Route(nil reg): 라우터 호출인데 End 반환")
	}
	if cmd.Goto != "any-worker" {
		t.Errorf("Route(nil reg) Goto: 기대 any-worker, 실제 %q", cmd.Goto)
	}
}

// TestMergeWorkerResult_MessagesAppended 는 MergeWorkerResult 후 워커 메시지가 상태에 존재하는지 검증한다.
func TestMergeWorkerResult_MessagesAppended(t *testing.T) {
	existing := []message.Message{
		message.NewUserMessage("초기 메시지"),
	}
	st := graph.State{"messages": existing}

	workerMsgs := []message.Message{
		message.NewAssistantMessage("워커 응답"),
	}
	out := WorkerOutput{Messages: workerMsgs}

	merged := MergeWorkerResult(st, out)

	msgs, ok := merged["messages"].([]message.Message)
	if !ok {
		t.Fatal("MergeWorkerResult: messages 키가 []message.Message가 아님")
	}
	if len(msgs) != 2 {
		t.Errorf("MergeWorkerResult: 메시지 수 기대 2, 실제 %d", len(msgs))
	}
	if msgs[1].Content != "워커 응답" {
		t.Errorf("MergeWorkerResult: 마지막 메시지 내용 기대 '워커 응답', 실제 %q", msgs[1].Content)
	}
}

// TestMergeWorkerResult_StructuredResponse 는 StructuredResponse가 상태에 추가되는지 검증한다.
func TestMergeWorkerResult_StructuredResponse(t *testing.T) {
	st := graph.State{}
	out := WorkerOutput{
		Messages:           []message.Message{message.NewAssistantMessage("결과")},
		StructuredResponse: map[string]string{"key": "value"},
	}

	merged := MergeWorkerResult(st, out)

	if sr, ok := merged["structured_response"]; !ok || sr == nil {
		t.Error("MergeWorkerResult: structured_response가 상태에 없음")
	}
}

// TestMergeWorkerResult_EmptyWorkerOutput 은 빈 WorkerOutput 병합 시 상태가 유지되는지 검증한다.
func TestMergeWorkerResult_EmptyWorkerOutput(t *testing.T) {
	existing := []message.Message{message.NewUserMessage("원본")}
	st := graph.State{"messages": existing}

	out := WorkerOutput{} // 빈 출력
	merged := MergeWorkerResult(st, out)

	msgs, ok := merged["messages"].([]message.Message)
	if !ok {
		t.Fatal("MergeWorkerResult: messages 키가 []message.Message가 아님")
	}
	if len(msgs) != 1 {
		t.Errorf("MergeWorkerResult: 원본 메시지 1개 유지 기대, 실제 %d", len(msgs))
	}
}

// TestMergeWorkerResult_OriginalStateUnchanged 는 MergeWorkerResult가 원본 상태를 변경하지 않는지 검증한다.
func TestMergeWorkerResult_OriginalStateUnchanged(t *testing.T) {
	existing := []message.Message{message.NewUserMessage("원본")}
	st := graph.State{"messages": existing}

	out := WorkerOutput{Messages: []message.Message{message.NewAssistantMessage("추가")}}
	_ = MergeWorkerResult(st, out)

	// 원본 st는 변경되지 않아야 한다
	origMsgs, ok := st["messages"].([]message.Message)
	if !ok {
		t.Fatal("원본 상태 messages 타입 확인 실패")
	}
	if len(origMsgs) != 1 {
		t.Errorf("원본 상태 변경됨: 기대 1개, 실제 %d개", len(origMsgs))
	}
}
