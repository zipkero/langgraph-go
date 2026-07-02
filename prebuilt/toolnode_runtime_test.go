// toolnode_runtime_test.go 는 NewToolNode 의 호출별 Runtime 구성
// (tool_call ID·ctx 주입 RunConfig·옵션 store)과 WithToolEventSink 이벤트 방출을 검증한다.
package prebuilt

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/zipkero/langgraph-go/config"
	"github.com/zipkero/langgraph-go/core"
	"github.com/zipkero/langgraph-go/message"
	"github.com/zipkero/langgraph-go/tool"
)

// runtimeRecorderTool 은 실행 시 Runtime 의 ToolCallID/Config/Store 를 기록하는 테스트용 도구다.
type runtimeRecorderTool struct {
	seenCallIDs   []string
	seenThreadIDs []string
	seenStores    []tool.Store
}

func (r *runtimeRecorderTool) Name() string        { return "recorder" }
func (r *runtimeRecorderTool) Description() string { return "Runtime 값을 기록한다" }
func (r *runtimeRecorderTool) Schema() tool.Schema {
	return tool.Schema{Name: "recorder", Description: "Runtime 값을 기록한다"}
}
func (r *runtimeRecorderTool) Execute(_ context.Context, _ tool.Input, rt tool.Runtime) (tool.Result, error) {
	r.seenCallIDs = append(r.seenCallIDs, rt.ToolCallID())
	r.seenThreadIDs = append(r.seenThreadIDs, config.GetThreadID(rt.Config()))
	r.seenStores = append(r.seenStores, rt.Store())
	return tool.Result{Content: "ok"}, nil
}

// noopStore 는 tool.Store 의 최소 스텁이다. 주입 여부(동일성)만 확인한다.
type noopStore struct{}

func (noopStore) Get(_ context.Context, _ []string, _ string) (map[string]any, bool, error) {
	return nil, false, nil
}
func (noopStore) Put(_ context.Context, _ []string, _ string, _ map[string]any) error { return nil }
func (noopStore) Search(_ context.Context, _ []string, _ string, _ int) ([]map[string]any, error) {
	return nil, nil
}

// stateWithToolCalls 는 recorder 도구를 두 번 호출하는 AI 메시지를 담은 상태를 만든다.
func stateWithToolCalls() core.State {
	aiMsg := message.NewAssistantToolCalls([]message.ToolCall{
		{ID: "call-1", Name: "recorder", Args: json.RawMessage(`{}`)},
		{ID: "call-2", Name: "recorder", Args: json.RawMessage(`{}`)},
	})
	return core.State{"messages": []message.Message{message.NewUserMessage("hi"), aiMsg}}
}

// TestToolNode_PerCallRuntime 은 호출마다 tool_call ID 가 주입되고, 그래프가 ctx 에 주입한
// RunConfig 와 옵션 store 가 도구 Runtime 에 전달되는지 검증한다.
func TestToolNode_PerCallRuntime(t *testing.T) {
	rec := &runtimeRecorderTool{}
	reg := tool.NewRegistry()
	if err := reg.Register(rec); err != nil {
		t.Fatalf("Register 실패: %v", err)
	}

	st := noopStore{}
	node := NewToolNode(reg, WithToolStore(st))

	ctx := config.WithRunConfig(context.Background(),
		config.RunConfig{Configurable: map[string]any{"thread_id": "t-7"}})
	if _, err := node(ctx, stateWithToolCalls()); err != nil {
		t.Fatalf("ToolNode 실행 실패: %v", err)
	}

	if len(rec.seenCallIDs) != 2 || rec.seenCallIDs[0] != "call-1" || rec.seenCallIDs[1] != "call-2" {
		t.Errorf("호출별 ToolCallID 기대 [call-1 call-2], 실제 %v", rec.seenCallIDs)
	}
	for i, tid := range rec.seenThreadIDs {
		if tid != "t-7" {
			t.Errorf("호출 %d의 thread_id 기대 t-7, 실제 %q", i, tid)
		}
	}
	for i, s := range rec.seenStores {
		if s != tool.Store(st) {
			t.Errorf("호출 %d의 store 가 옵션으로 지정한 인스턴스가 아니다", i)
		}
	}
}

// TestToolNode_EventSink 는 싱크가 호출 전(Result nil)·후(Result 채움) 이벤트를
// tool_call 별로 순서대로 수신하는지 검증한다.
func TestToolNode_EventSink(t *testing.T) {
	reg := tool.NewRegistry()
	if err := reg.Register(&runtimeRecorderTool{}); err != nil {
		t.Fatalf("Register 실패: %v", err)
	}

	var events []tool.Event
	node := NewToolNode(reg, WithToolEventSink(func(ev tool.Event) {
		events = append(events, ev)
	}))

	if _, err := node(context.Background(), stateWithToolCalls()); err != nil {
		t.Fatalf("ToolNode 실행 실패: %v", err)
	}

	if len(events) != 4 {
		t.Fatalf("이벤트 수 기대 4(호출 2건 × 전/후), 실제 %d", len(events))
	}
	wantIDs := []string{"call-1", "call-1", "call-2", "call-2"}
	for i, ev := range events {
		if ev.ToolCallID != wantIDs[i] {
			t.Errorf("이벤트 %d ToolCallID 기대 %s, 실제 %s", i, wantIDs[i], ev.ToolCallID)
		}
		wantResult := i%2 == 1
		if (ev.Result != nil) != wantResult {
			t.Errorf("이벤트 %d Result 존재 기대 %v, 실제 %v", i, wantResult, ev.Result != nil)
		}
	}
}
