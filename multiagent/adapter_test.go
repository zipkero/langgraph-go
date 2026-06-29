// adapter_test.go 는 워커 구성 어댑터(AgentAsNode/GraphAsNode/AgentAsTool)의 단위 테스트를 담는다.
// llm.StubClient 로 만든 stub 에이전트를 어댑터로 감싸 노드·도구로 실행해 결정적으로 검증한다.
// 네트워크·API 키 없이 수행된다.
package multiagent_test

import (
	"context"
	"testing"

	"github.com/zipkero/langgraph-go/agent"
	"github.com/zipkero/langgraph-go/config"
	"github.com/zipkero/langgraph-go/graph"
	"github.com/zipkero/langgraph-go/llm"
	"github.com/zipkero/langgraph-go/message"
	"github.com/zipkero/langgraph-go/multiagent"
	"github.com/zipkero/langgraph-go/tool"
)

// newStubAgent 는 정해진 응답을 반환하는 stub 에이전트를 생성한다.
func newStubAgent(t *testing.T, content string) *agent.Agent {
	t.Helper()
	stub := llm.NewStubClient("stub-model", llm.StubResponse{
		Message: message.NewAssistantMessage(content),
	})
	a, err := agent.Create(stub, nil)
	if err != nil {
		t.Fatalf("stub 에이전트 생성 실패: %v", err)
	}
	return a
}

// ============================================================
// AgentAsNode 테스트
// ============================================================

// TestAgentAsNode_NodeFunc반환 는 AgentAsNode 가 graph.NodeFunc 를 반환하는지 검증한다.
func TestAgentAsNode_NodeFunc반환(t *testing.T) {
	a := newStubAgent(t, "안녕하세요")
	nodeFn := multiagent.AgentAsNode(a)
	if nodeFn == nil {
		t.Fatal("AgentAsNode 가 nil NodeFunc 를 반환했습니다")
	}
}

// TestAgentAsNode_그래프노드실행 는 AgentAsNode 로 감싼 에이전트를 그래프 노드로 실행했을 때
// 에이전트 결과가 상태에 반영되는지 검증한다.
func TestAgentAsNode_그래프노드실행(t *testing.T) {
	const wantContent = "에이전트 응답입니다"
	a := newStubAgent(t, wantContent)

	nodeFn := multiagent.AgentAsNode(a)

	// 초기 상태에 입력 메시지를 담는다.
	initState := graph.State{
		"messages": []message.Message{
			message.NewUserMessage("안녕"),
		},
	}

	result, err := nodeFn(context.Background(), initState)
	if err != nil {
		t.Fatalf("AgentAsNode 실행 실패: %v", err)
	}

	// 반환값이 StateUpdate 여야 한다.
	update, ok := result.(graph.StateUpdate)
	if !ok {
		t.Fatalf("AgentAsNode 반환 타입이 StateUpdate 가 아닙니다: %T", result)
	}

	// "messages" 키에 에이전트 결과 메시지가 있어야 한다.
	rawMsgs, ok := update["messages"]
	if !ok {
		t.Fatal("StateUpdate 에 'messages' 키가 없습니다")
	}
	msgs, ok := rawMsgs.([]message.Message)
	if !ok {
		t.Fatalf("update['messages'] 타입이 []message.Message 가 아닙니다: %T", rawMsgs)
	}

	// 마지막 AI 메시지 내용이 기대값과 일치해야 한다.
	lastAI, found := message.LastAIMessage(msgs)
	if !found {
		t.Fatal("결과 메시지에서 AI 메시지를 찾지 못했습니다")
	}
	if lastAI.Content != wantContent {
		t.Errorf("AI 메시지 내용 불일치: got=%q, want=%q", lastAI.Content, wantContent)
	}
}

// TestAgentAsNode_그래프통합 는 AgentAsNode 를 실제 그래프 노드로 등록해 Invoke 했을 때
// 에이전트 결과가 최종 상태에 반영됨을 검증한다.
func TestAgentAsNode_그래프통합(t *testing.T) {
	const wantContent = "그래프 에이전트 응답"
	a := newStubAgent(t, wantContent)

	schema := graph.StateSchema{
		Reducers: map[string]graph.ReducerFunc{},
	}
	b := graph.NewStateGraph(schema)

	if err := b.AddNode("agent", multiagent.AgentAsNode(a)); err != nil {
		t.Fatalf("AddNode 실패: %v", err)
	}
	if err := b.SetEntryPoint("agent"); err != nil {
		t.Fatalf("SetEntryPoint 실패: %v", err)
	}

	compiled, err := b.Compile()
	if err != nil {
		t.Fatalf("Compile 실패: %v", err)
	}

	initState := graph.State{
		"messages": []message.Message{
			message.NewUserMessage("테스트 입력"),
		},
	}

	finalState, err := compiled.Invoke(context.Background(), initState, config.RunConfig{})
	if err != nil {
		t.Fatalf("그래프 Invoke 실패: %v", err)
	}

	rawMsgs, ok := finalState["messages"]
	if !ok {
		t.Fatal("최종 상태에 'messages' 키가 없습니다")
	}
	msgs, ok := rawMsgs.([]message.Message)
	if !ok {
		t.Fatalf("최종 상태 'messages' 타입 불일치: %T", rawMsgs)
	}

	lastAI, found := message.LastAIMessage(msgs)
	if !found {
		t.Fatal("최종 상태에서 AI 메시지를 찾지 못했습니다")
	}
	if lastAI.Content != wantContent {
		t.Errorf("AI 메시지 내용 불일치: got=%q, want=%q", lastAI.Content, wantContent)
	}
}

// ============================================================
// GraphAsNode 테스트
// ============================================================

// TestGraphAsNode_NodeFunc반환 는 GraphAsNode 가 graph.NodeFunc 를 반환하는지 검증한다.
func TestGraphAsNode_NodeFunc반환(t *testing.T) {
	// 단순 stub 서브그래프를 만든다.
	schema := graph.StateSchema{Reducers: map[string]graph.ReducerFunc{}}
	b := graph.NewStateGraph(schema)
	_ = b.AddNode("inner", func(ctx context.Context, st graph.State) (any, error) {
		return graph.StateUpdate{"result": "서브그래프 결과"}, nil
	})
	_ = b.SetEntryPoint("inner")
	sub, err := b.Compile()
	if err != nil {
		t.Fatalf("서브그래프 Compile 실패: %v", err)
	}

	nodeFn := multiagent.GraphAsNode(sub)
	if nodeFn == nil {
		t.Fatal("GraphAsNode 가 nil NodeFunc 를 반환했습니다")
	}
}

// TestGraphAsNode_서브그래프결과반영 는 GraphAsNode 로 감싼 서브그래프가 실행됐을 때
// 서브그래프 결과가 상태에 반영됨을 검증한다.
func TestGraphAsNode_서브그래프결과반영(t *testing.T) {
	// 서브그래프: "value" 키에 42를 쓴다.
	schema := graph.StateSchema{Reducers: map[string]graph.ReducerFunc{}}
	b := graph.NewStateGraph(schema)
	_ = b.AddNode("inner", func(ctx context.Context, st graph.State) (any, error) {
		return graph.StateUpdate{"value": 42}, nil
	})
	_ = b.SetEntryPoint("inner")
	sub, err := b.Compile()
	if err != nil {
		t.Fatalf("서브그래프 Compile 실패: %v", err)
	}

	nodeFn := multiagent.GraphAsNode(sub)
	result, err := nodeFn(context.Background(), graph.State{})
	if err != nil {
		t.Fatalf("GraphAsNode 실행 실패: %v", err)
	}

	update, ok := result.(graph.StateUpdate)
	if !ok {
		t.Fatalf("GraphAsNode 반환 타입이 StateUpdate 가 아닙니다: %T", result)
	}
	if update["value"] != 42 {
		t.Errorf("서브그래프 결과 불일치: got=%v, want=42", update["value"])
	}
}

// TestGraphAsNode_그래프통합 는 GraphAsNode 를 부모 그래프 노드로 등록해 Invoke 했을 때
// 서브그래프 결과가 최종 상태에 반영됨을 검증한다.
func TestGraphAsNode_그래프통합(t *testing.T) {
	// 서브그래프
	subSchema := graph.StateSchema{Reducers: map[string]graph.ReducerFunc{}}
	subBuilder := graph.NewStateGraph(subSchema)
	_ = subBuilder.AddNode("worker", func(ctx context.Context, st graph.State) (any, error) {
		return graph.StateUpdate{"output": "서브그래프 완료"}, nil
	})
	_ = subBuilder.SetEntryPoint("worker")
	sub, err := subBuilder.Compile()
	if err != nil {
		t.Fatalf("서브그래프 Compile 실패: %v", err)
	}

	// 부모 그래프
	parentSchema := graph.StateSchema{Reducers: map[string]graph.ReducerFunc{}}
	parentBuilder := graph.NewStateGraph(parentSchema)
	if err := parentBuilder.AddNode("sub", multiagent.GraphAsNode(sub)); err != nil {
		t.Fatalf("부모 그래프 AddNode 실패: %v", err)
	}
	if err := parentBuilder.SetEntryPoint("sub"); err != nil {
		t.Fatalf("SetEntryPoint 실패: %v", err)
	}
	parent, err := parentBuilder.Compile()
	if err != nil {
		t.Fatalf("부모 그래프 Compile 실패: %v", err)
	}

	finalState, err := parent.Invoke(context.Background(), graph.State{}, config.RunConfig{})
	if err != nil {
		t.Fatalf("부모 그래프 Invoke 실패: %v", err)
	}

	if finalState["output"] != "서브그래프 완료" {
		t.Errorf("서브그래프 결과 불일치: got=%v, want='서브그래프 완료'", finalState["output"])
	}
}

// ============================================================
// AgentAsTool 테스트
// ============================================================

// TestAgentAsTool_Tool반환 는 AgentAsTool 이 tool.Tool 을 반환하는지 검증한다.
func TestAgentAsTool_Tool반환(t *testing.T) {
	a := newStubAgent(t, "도구 응답")
	agentTool := multiagent.AgentAsTool(a, "my_agent", "테스트 에이전트 도구")
	if agentTool == nil {
		t.Fatal("AgentAsTool 이 nil Tool 을 반환했습니다")
	}
	if agentTool.Name() != "my_agent" {
		t.Errorf("도구 이름 불일치: got=%q, want=%q", agentTool.Name(), "my_agent")
	}
	if agentTool.Description() != "테스트 에이전트 도구" {
		t.Errorf("도구 설명 불일치: got=%q, want=%q", agentTool.Description(), "테스트 에이전트 도구")
	}
}

// TestAgentAsTool_Execute 는 AgentAsTool 이 반환한 Tool 을 Execute 했을 때
// 감싼 에이전트가 실행되어 결과가 도구 출력으로 나오는지 검증한다.
func TestAgentAsTool_Execute(t *testing.T) {
	const wantContent = "에이전트 도구 응답입니다"
	a := newStubAgent(t, wantContent)
	agentTool := multiagent.AgentAsTool(a, "agent_tool", "에이전트 도구")

	// 입력 JSON 구성
	input := []byte(`{"input": "안녕하세요"}`)
	rt := tool.NewRuntime(nil, "call-001", config.RunConfig{}, nil, nil)

	result, err := agentTool.Execute(context.Background(), input, rt)
	if err != nil {
		t.Fatalf("AgentAsTool Execute 실패: %v", err)
	}
	if result.IsError {
		t.Fatalf("AgentAsTool Execute 가 에러 결과를 반환했습니다: %s", result.Content)
	}
	if result.Content != wantContent {
		t.Errorf("도구 출력 내용 불일치: got=%q, want=%q", result.Content, wantContent)
	}
}

// TestAgentAsTool_Schema 는 AgentAsTool 이 반환한 Tool 의 스키마가 올바른지 검증한다.
func TestAgentAsTool_Schema(t *testing.T) {
	a := newStubAgent(t, "응답")
	agentTool := multiagent.AgentAsTool(a, "schema_tool", "스키마 테스트")

	schema := agentTool.Schema()
	if schema.Name != "schema_tool" {
		t.Errorf("스키마 이름 불일치: got=%q, want=%q", schema.Name, "schema_tool")
	}

	// "input" 파라미터가 있어야 한다.
	found := false
	for _, p := range schema.Parameters {
		if p.Name == "input" {
			found = true
			break
		}
	}
	if !found {
		t.Error("스키마에 'input' 파라미터가 없습니다")
	}
}
