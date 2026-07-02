// agentasnode_ctx_test.go 는 AgentAsNode 의 실행별 설정(ctx 주입 RunConfig) 전파와
// ModeMessages 스트리밍 시 토큰 포워딩을 검증한다. 네트워크·API 키 없이 수행된다.
package multiagent_test

import (
	"context"
	"testing"

	"github.com/zipkero/langgraph-go/agent"
	"github.com/zipkero/langgraph-go/checkpoint"
	"github.com/zipkero/langgraph-go/config"
	"github.com/zipkero/langgraph-go/core"
	"github.com/zipkero/langgraph-go/graph"
	"github.com/zipkero/langgraph-go/llm"
	"github.com/zipkero/langgraph-go/message"
	"github.com/zipkero/langgraph-go/multiagent"
)

// buildSingleAgentGraph 는 AgentAsNode 하나만 가진 그래프를 컴파일해 반환한다.
func buildSingleAgentGraph(t *testing.T, a *agent.Agent) *graph.Compiled {
	t.Helper()
	b := graph.NewStateGraph(graph.StateSchema{Reducers: map[string]graph.ReducerFunc{}})
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
	return compiled
}

// TestAgentAsNode_RunConfig전파 는 그래프 Invoke 에 전달한 RunConfig(thread_id)가
// ctx 를 통해 내부 에이전트까지 흘러 체크포인터 저장에 사용되는지 검증한다.
func TestAgentAsNode_RunConfig전파(t *testing.T) {
	stub := llm.NewStubClient("stub-model", llm.StubResponse{
		Message: message.NewAssistantMessage("설정 전파 확인"),
	})
	saver := checkpoint.NewInMemorySaver()
	a, err := agent.Create(stub, nil, agent.WithCheckpointer(saver))
	if err != nil {
		t.Fatalf("에이전트 생성 실패: %v", err)
	}

	compiled := buildSingleAgentGraph(t, a)

	cfg := config.RunConfig{Configurable: map[string]any{"thread_id": "th-전파"}}
	initState := graph.State{"messages": []message.Message{message.NewUserMessage("안녕")}}
	if _, err := compiled.Invoke(context.Background(), initState, cfg); err != nil {
		t.Fatalf("그래프 Invoke 실패: %v", err)
	}

	// 내부 에이전트가 같은 thread_id 로 상태를 저장했어야 한다.
	snap, err := a.GetState(cfg)
	if err != nil {
		t.Fatalf("GetState 실패: %v", err)
	}
	if len(snap.Values) == 0 {
		t.Fatal("thread_id 로 저장된 에이전트 상태가 없다 — RunConfig 가 내부 에이전트에 전파되지 않았다")
	}
}

// TestAgentAsNode_토큰포워딩 은 그래프 ModeMessages 스트리밍에서 내부 에이전트의
// 토큰이 GraphEvent.Token 으로 방출되는지 검증한다.
func TestAgentAsNode_토큰포워딩(t *testing.T) {
	const wantToken = "토큰 응답입니다"
	a := newStubAgent(t, wantToken)

	compiled := buildSingleAgentGraph(t, a)

	initState := graph.State{"messages": []message.Message{message.NewUserMessage("안녕")}}
	events, err := compiled.Stream(context.Background(), initState, config.RunConfig{}, core.ModeMessages)
	if err != nil {
		t.Fatalf("그래프 Stream 실패: %v", err)
	}

	var tokens []string
	for ev := range events {
		if ev.Token != "" {
			tokens = append(tokens, ev.Token)
		}
	}

	if len(tokens) == 0 {
		t.Fatal("ModeMessages 스트림에서 토큰 이벤트가 방출되지 않았다")
	}
	joined := ""
	for _, tk := range tokens {
		joined += tk
	}
	if joined != wantToken {
		t.Errorf("토큰 연결 결과 기대 %q, 실제 %q", wantToken, joined)
	}
}
