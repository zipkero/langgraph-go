package trace

import (
	"errors"
	"testing"

	"github.com/zipkero/langgraph-go/config"
	"github.com/zipkero/langgraph-go/tool"
)

// TestToolEventSinkMapsCallAndResult 는 ToolEventSink() 가 반환한 함수에
// tool.Event 를 직접 방출하면 그 호출/결과가 ToolTrace 로 기록되어
// Events() 에 나타나는지 확인한다.
func TestToolEventSinkMapsCallAndResult(t *testing.T) {
	tr := New()
	sink := tr.ToolEventSink()

	sink(tool.Event{ToolName: "search", ToolCallID: "call-1", Input: []byte(`{"q":"x"}`)})
	sink(tool.Event{ToolName: "search", ToolCallID: "call-1", Result: &tool.Result{Content: "ok"}})

	events := tr.Events()
	if len(events) != 2 {
		t.Fatalf("이벤트 개수 = %d, want 2", len(events))
	}

	ev := events[0]
	if ev.Kind != KindTool || ev.Tool == nil || ev.Tool.Phase != ToolPhaseCall {
		t.Fatalf("events[0] = %+v, want KindTool/ToolPhaseCall", ev)
	}
	if ev.Tool.Call == nil || ev.Tool.Call.Name != "search" || ev.Tool.Call.ID != "call-1" {
		t.Errorf("events[0].Tool.Call = %+v, want id=call-1 name=search", ev.Tool.Call)
	}
	if ev.Tool.Result != nil {
		t.Errorf("events[0].Tool.Result = %+v, want nil", ev.Tool.Result)
	}

	ev = events[1]
	if ev.Kind != KindTool || ev.Tool == nil || ev.Tool.Phase != ToolPhaseResult {
		t.Fatalf("events[1] = %+v, want KindTool/ToolPhaseResult", ev)
	}
	if ev.Tool.Result == nil || ev.Tool.Result.Content != "ok" {
		t.Errorf("events[1].Tool.Result = %+v, want content=ok", ev.Tool.Result)
	}
}

// TestToolEventSinkMapsErr 는 tool.Event.Err 가 있으면 그 메시지가
// ToolTrace.Err 에 반영되는지 확인한다.
func TestToolEventSinkMapsErr(t *testing.T) {
	tr := New()
	sink := tr.ToolEventSink()

	sink(tool.Event{
		ToolName:   "search",
		ToolCallID: "call-1",
		Result:     &tool.Result{Content: "fail", IsError: true},
		Err:        errors.New("boom"),
	})

	events := tr.Events()
	if len(events) != 1 {
		t.Fatalf("이벤트 개수 = %d, want 1", len(events))
	}
	if events[0].Tool.Err != "boom" {
		t.Errorf("events[0].Tool.Err = %q, want %q", events[0].Tool.Err, "boom")
	}
}

// TestToolEventSinkWiredToRuntime 은 ToolEventSink() 가 반환한 함수가
// tool.NewRuntime(..., emit func(tool.Event)) 의 emit 인자에 그대로 대입 가능하며,
// Runtime.Emit 을 통해 방출된 tool.Event 가 ToolTrace 로 기록되는지 확인한다
// (시그니처 호환을 컴파일로도 확인하는 케이스).
func TestToolEventSinkWiredToRuntime(t *testing.T) {
	tr := New()

	rt := tool.NewRuntime(nil, "call-1", config.RunConfig{}, nil, tr.ToolEventSink())
	rt.Emit(tool.Event{ToolName: "echo", ToolCallID: rt.ToolCallID(), Input: []byte(`{}`)})
	rt.Emit(tool.Event{ToolName: "echo", ToolCallID: rt.ToolCallID(), Result: &tool.Result{Content: "hi"}})

	events := tr.Events()
	if len(events) != 2 {
		t.Fatalf("이벤트 개수 = %d, want 2", len(events))
	}
	if events[0].Kind != KindTool || events[0].Tool.Phase != ToolPhaseCall || events[0].Tool.Call.Name != "echo" {
		t.Errorf("events[0] = %+v, want KindTool/ToolPhaseCall name=echo", events[0])
	}
	if events[1].Kind != KindTool || events[1].Tool.Phase != ToolPhaseResult || events[1].Tool.Result.Content != "hi" {
		t.Errorf("events[1] = %+v, want KindTool/ToolPhaseResult content=hi", events[1])
	}
}
