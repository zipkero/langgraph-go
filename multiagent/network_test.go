// network_test.go 는 BuildNetwork/IsFinalAnswer 의 동작을 stub 워커로 결정적으로 검증한다.
// 네트워크·LLM 호출이 없다.
// SPEC §5.6, ANALYSIS §1·§2 참조.
package multiagent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/zipkero/langgraph-go/agent"
	"github.com/zipkero/langgraph-go/config"
	"github.com/zipkero/langgraph-go/core"
	"github.com/zipkero/langgraph-go/graph"
	"github.com/zipkero/langgraph-go/message"
)

// routingWorker 는 Invoke 호출 시 미리 정의된 메시지 목록을 반환하는 테스트용 Worker 다.
// tool_calls 가 있는 메시지를 반환하면 SelectNext 가 Goto 를 선택하고,
// tool_calls 가 없으면 End 로 종료한다.
type routingWorker struct {
	name     string
	desc     string
	response []message.Message // Invoke 가 반환할 메시지 목록
}

func (r *routingWorker) Name() string        { return r.name }
func (r *routingWorker) Description() string { return r.desc }
func (r *routingWorker) Invoke(_ context.Context, _ agent.Input, _ config.RunConfig) (WorkerOutput, error) {
	return WorkerOutput{Messages: r.response}, nil
}
func (r *routingWorker) Stream(_ context.Context, _ agent.Input, _ config.RunConfig, _ core.Mode) (<-chan agent.AgentEvent, error) {
	ch := make(chan agent.AgentEvent)
	close(ch)
	return ch, nil
}

// makeGotoMessage 는 SelectNext 가 next 워커로 Goto 를 선택하도록
// tool_calls 가 있는 AI 메시지를 생성한다.
// SelectNext 는 tool_calls[0].Args 를 {"next": "<workerName>"} 으로 해석한다.
func makeGotoMessage(nextWorkerName string) message.Message {
	args, _ := json.Marshal(map[string]string{"next": nextWorkerName})
	return message.NewAssistantToolCalls([]message.ToolCall{
		{ID: "call-1", Name: "route_to_worker", Args: args},
	})
}

// makeFinalMessage 는 IsFinalAnswer 가 true 를 반환하는 AI 텍스트 메시지를 생성한다.
func makeFinalMessage(content string) message.Message {
	return message.NewAssistantMessage(content)
}

// TestBuildNetwork_EmptyWorkers 는 빈 워커 목록으로 BuildNetwork 를 호출하면
// 에러(진입점 미설정)를 반환하는지 검증한다(SPEC §5.6 에러 경로).
func TestBuildNetwork_EmptyWorkers(t *testing.T) {
	_, err := BuildNetwork([]Worker{})
	if err == nil {
		t.Fatal("빈 워커 목록: 에러 기대, 그런데 nil 반환")
	}
	t.Logf("빈 워커 에러(예상): %v", err)
}

// TestBuildNetwork_SingleWorker 는 단일 워커로 네트워크를 구성하고
// 종료 메시지로 정상 종료되는지 검증한다.
func TestBuildNetwork_SingleWorker(t *testing.T) {
	// 단일 워커: 최종 답변 메시지를 반환해 바로 종료
	w := &routingWorker{
		name: "worker-a",
		desc: "알파 워커",
		response: []message.Message{
			makeFinalMessage("최종 답변입니다"),
		},
	}

	net, err := BuildNetwork([]Worker{w})
	if err != nil {
		t.Fatalf("BuildNetwork 실패: %v", err)
	}

	initState := graph.State{
		"messages": []message.Message{message.NewUserMessage("질문")},
	}
	finalState, err := net.Invoke(context.Background(), initState, config.RunConfig{})
	if err != nil {
		t.Fatalf("Invoke 실패: %v", err)
	}

	msgs, ok := finalState["messages"].([]message.Message)
	if !ok {
		t.Fatal("최종 상태에 messages 가 없거나 타입이 다름")
	}
	// 사용자 메시지 + 워커 응답 메시지
	if len(msgs) < 2 {
		t.Errorf("messages 개수: 최소 2개 기대, 실제 %d", len(msgs))
	}
}

// TestBuildNetwork_MultiWorker_GotoAndEnd 는 두 워커로 네트워크를 구성하고
// worker-a 가 Goto 로 worker-b 로 이동한 뒤 worker-b 가 종료하는 왕복을 검증한다(SPEC §5.6).
func TestBuildNetwork_MultiWorker_GotoAndEnd(t *testing.T) {
	// worker-a: Goto("worker-b") 를 반환하는 메시지를 출력
	workerA := &routingWorker{
		name: "worker-a",
		desc: "알파 워커",
		response: []message.Message{
			makeGotoMessage("worker-b"),
		},
	}

	// worker-b: 최종 답변 메시지를 출력해 종료
	workerB := &routingWorker{
		name: "worker-b",
		desc: "베타 워커",
		response: []message.Message{
			makeFinalMessage("베타 워커의 최종 답변"),
		},
	}

	net, err := BuildNetwork([]Worker{workerA, workerB})
	if err != nil {
		t.Fatalf("BuildNetwork 실패: %v", err)
	}

	initState := graph.State{
		"messages": []message.Message{message.NewUserMessage("두 워커 테스트")},
	}
	finalState, err := net.Invoke(context.Background(), initState, config.RunConfig{})
	if err != nil {
		t.Fatalf("Invoke 실패: %v", err)
	}

	msgs, ok := finalState["messages"].([]message.Message)
	if !ok {
		t.Fatal("최종 상태에 messages 가 없거나 타입이 다름")
	}

	// 최소 3개: 사용자 메시지 + worker-a 응답(Goto) + worker-b 응답(최종)
	if len(msgs) < 3 {
		t.Errorf("messages 개수: 최소 3개 기대, 실제 %d", len(msgs))
	}

	// 마지막 메시지가 worker-b 의 최종 답변인지 확인
	last := msgs[len(msgs)-1]
	if last.Content != "베타 워커의 최종 답변" {
		t.Errorf("마지막 메시지 내용: 기대 '베타 워커의 최종 답변', 실제 %q", last.Content)
	}
}

// TestBuildNetwork_ThreeWorkers_GotoRoundTrip 은 세 워커로 네트워크를 구성하고
// A→B→C 순으로 Goto 왕복 후 C 에서 종료되는 시나리오를 검증한다.
func TestBuildNetwork_ThreeWorkers_GotoRoundTrip(t *testing.T) {
	workerA := &routingWorker{
		name:     "worker-a",
		desc:     "알파",
		response: []message.Message{makeGotoMessage("worker-b")},
	}
	workerB := &routingWorker{
		name:     "worker-b",
		desc:     "베타",
		response: []message.Message{makeGotoMessage("worker-c")},
	}
	workerC := &routingWorker{
		name:     "worker-c",
		desc:     "감마",
		response: []message.Message{makeFinalMessage("감마 최종 답변")},
	}

	net, err := BuildNetwork([]Worker{workerA, workerB, workerC})
	if err != nil {
		t.Fatalf("BuildNetwork 실패: %v", err)
	}

	initState := graph.State{
		"messages": []message.Message{message.NewUserMessage("세 워커 테스트")},
	}
	finalState, err := net.Invoke(context.Background(), initState, config.RunConfig{})
	if err != nil {
		t.Fatalf("Invoke 실패: %v", err)
	}

	msgs, ok := finalState["messages"].([]message.Message)
	if !ok {
		t.Fatal("최종 상태에 messages 가 없거나 타입이 다름")
	}

	// 최소 4개: 사용자 + A + B + C
	if len(msgs) < 4 {
		t.Errorf("messages 개수: 최소 4개 기대, 실제 %d", len(msgs))
	}

	last := msgs[len(msgs)-1]
	if last.Content != "감마 최종 답변" {
		t.Errorf("마지막 메시지: 기대 '감마 최종 답변', 실제 %q", last.Content)
	}
}

// TestIsFinalAnswer 는 IsFinalAnswer 가 메시지 유형에 따라 올바른 값을 반환하는지 검증한다.
func TestIsFinalAnswer(t *testing.T) {
	cases := []struct {
		name string
		msg  message.Message
		want bool
	}{
		{
			name: "AI 텍스트 메시지(tool_calls 없음) — 최종 답변",
			msg:  message.NewAssistantMessage("최종 답변"),
			want: true,
		},
		{
			name: "AI 메시지(tool_calls 있음) — 라우팅 중",
			msg: func() message.Message {
				args, _ := json.Marshal(map[string]string{"next": "worker-b"})
				return message.NewAssistantToolCalls([]message.ToolCall{
					{ID: "c1", Name: "route_to_worker", Args: args},
				})
			}(),
			want: false,
		},
		{
			name: "사용자 메시지 — false",
			msg:  message.NewUserMessage("질문"),
			want: false,
		},
		{
			name: "시스템 메시지 — false",
			msg:  message.NewSystemMessage("시스템"),
			want: false,
		},
		{
			name: "Tool 메시지 — false",
			msg:  message.NewToolMessage("id1", "tool", "결과"),
			want: false,
		},
		{
			name: "내용 없는 AI 메시지 — false",
			msg:  message.Message{Role: message.RoleAssistant, Content: ""},
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := IsFinalAnswer(tc.msg)
			if got != tc.want {
				t.Errorf("IsFinalAnswer: 기대 %v, 실제 %v (메시지: %+v)", tc.want, got, tc.msg)
			}
		})
	}
}

// TestBuildNetwork_CompiledGraphNotNil 은 유효한 워커 목록으로 BuildNetwork 를 호출하면
// nil 이 아닌 *graph.Compiled 를 반환하는지 검증한다.
func TestBuildNetwork_CompiledGraphNotNil(t *testing.T) {
	w := &routingWorker{
		name:     "only-worker",
		desc:     "단독 워커",
		response: []message.Message{makeFinalMessage("완료")},
	}

	net, err := BuildNetwork([]Worker{w})
	if err != nil {
		t.Fatalf("BuildNetwork 실패: %v", err)
	}
	if net == nil {
		t.Fatal("BuildNetwork: nil Compiled 반환")
	}
}
