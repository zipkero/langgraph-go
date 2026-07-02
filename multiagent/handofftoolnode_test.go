// handofftoolnode_test.go 는 HandoffToolNode 의 Command 전파(단일 ToParent·복수 Fanout)와
// 비핸드오프 도구의 일반 ToolMessage 병합을 검증한다.
package multiagent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/zipkero/langgraph-go/graph"
	"github.com/zipkero/langgraph-go/graph/command"
	"github.com/zipkero/langgraph-go/message"
	"github.com/zipkero/langgraph-go/tool"
)

// plainTool 은 Command 를 도출하지 않는 일반 도구다.
type plainTool struct{}

func (plainTool) Name() string        { return "plain" }
func (plainTool) Description() string { return "일반 도구" }
func (plainTool) Schema() tool.Schema {
	return tool.Schema{Name: "plain", Description: "일반 도구"}
}
func (plainTool) Execute(_ context.Context, _ tool.Input, _ tool.Runtime) (tool.Result, error) {
	return tool.Result{Content: "plain-ok"}, nil
}

// handoffState 는 지정한 tool_calls 를 가진 마지막 AI 메시지를 담은 상태를 만든다.
func handoffState(calls ...message.ToolCall) graph.State {
	aiMsg := message.NewAssistantToolCalls(calls)
	return graph.State{"messages": []message.Message{message.NewUserMessage("hi"), aiMsg}}
}

// TestHandoffToolNode_SingleCall_PropagatesToParent 는 단일 핸드오프 호출이
// ToParent Command(부모 그래프, Goto=워커 이름)로 전파되고 ToolMessage 가 Update 에 실리는지 검증한다.
func TestHandoffToolNode_SingleCall_PropagatesToParent(t *testing.T) {
	reg := tool.NewRegistry()
	ht := CreateHandoffTool("worker-a", "워커 A에 위임")
	if err := reg.Register(ht); err != nil {
		t.Fatalf("Register 실패: %v", err)
	}

	node := HandoffToolNode(reg)
	raw, err := node(context.Background(), handoffState(message.ToolCall{
		ID: "call-1", Name: ht.Name(), Args: json.RawMessage(`{"agent_name":"worker-a","query":"조사해줘"}`),
	}))
	if err != nil {
		t.Fatalf("노드 실행 실패: %v", err)
	}

	cmd, ok := raw.(command.Command)
	if !ok {
		t.Fatalf("반환 타입 기대 command.Command, 실제 %T", raw)
	}
	if cmd.Goto != "worker-a" {
		t.Errorf("Goto 기대 worker-a, 실제 %q", cmd.Goto)
	}
	if !cmd.IsParent() {
		t.Error("부모 그래프 대상(TargetParent) Command 여야 한다")
	}

	msgs, ok := cmd.Update["messages"].([]message.Message)
	if !ok || len(msgs) == 0 {
		t.Fatalf("Update[messages] 에 병합 메시지가 없다: %+v", cmd.Update)
	}
	last := msgs[len(msgs)-1]
	if last.Role != message.RoleTool || last.ToolCallID != "call-1" {
		t.Errorf("마지막 메시지 기대 ToolMessage(call-1), 실제 %+v", last)
	}
}

// TestHandoffToolNode_MultipleCalls_PropagatesFanout 은 복수 핸드오프 호출이
// Fanout(Send 목록) Command 로 전파되는지 검증한다.
func TestHandoffToolNode_MultipleCalls_PropagatesFanout(t *testing.T) {
	reg := tool.NewRegistry()
	ht := CreateHandoffTool("worker-a", "워커에 위임")
	if err := reg.Register(ht); err != nil {
		t.Fatalf("Register 실패: %v", err)
	}

	node := HandoffToolNode(reg)
	raw, err := node(context.Background(), handoffState(
		message.ToolCall{ID: "call-1", Name: ht.Name(), Args: json.RawMessage(`{"agent_name":"worker-a","query":"A 조사"}`)},
		message.ToolCall{ID: "call-2", Name: ht.Name(), Args: json.RawMessage(`{"agent_name":"worker-b","query":"B 조사"}`)},
	))
	if err != nil {
		t.Fatalf("노드 실행 실패: %v", err)
	}

	cmd, ok := raw.(command.Command)
	if !ok {
		t.Fatalf("반환 타입 기대 command.Command, 실제 %T", raw)
	}
	if len(cmd.Sends) != 2 {
		t.Fatalf("Fanout Send 수 기대 2, 실제 %d", len(cmd.Sends))
	}
	if cmd.Sends[0].Target != "worker-a" || cmd.Sends[1].Target != "worker-b" {
		t.Errorf("Send 대상 기대 [worker-a worker-b], 실제 [%s %s]", cmd.Sends[0].Target, cmd.Sends[1].Target)
	}
}

// TestHandoffToolNode_PlainTool_ReturnsStateUpdate 는 핸드오프 도구가 없으면
// Command 없이 ToolMessage 병합 StateUpdate 만 반환하는지 검증한다.
func TestHandoffToolNode_PlainTool_ReturnsStateUpdate(t *testing.T) {
	reg := tool.NewRegistry()
	if err := reg.Register(plainTool{}); err != nil {
		t.Fatalf("Register 실패: %v", err)
	}

	node := HandoffToolNode(reg)
	raw, err := node(context.Background(), handoffState(message.ToolCall{
		ID: "call-1", Name: "plain", Args: json.RawMessage(`{}`),
	}))
	if err != nil {
		t.Fatalf("노드 실행 실패: %v", err)
	}

	update, ok := raw.(graph.StateUpdate)
	if !ok {
		t.Fatalf("반환 타입 기대 graph.StateUpdate, 실제 %T", raw)
	}
	msgs, ok := update["messages"].([]message.Message)
	if !ok || len(msgs) == 0 {
		t.Fatalf("messages 병합 결과가 없다: %+v", update)
	}
	last := msgs[len(msgs)-1]
	if last.Role != message.RoleTool || last.Content != "plain-ok" {
		t.Errorf("마지막 메시지 기대 ToolMessage(plain-ok), 실제 %+v", last)
	}
}
