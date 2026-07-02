package trace

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/zipkero/langgraph-go/graph"
	"github.com/zipkero/langgraph-go/llm"
	"github.com/zipkero/langgraph-go/message"
	"github.com/zipkero/langgraph-go/tool"
)

// TestRecordAllKinds 는 각 Record* 호출이 대응 이벤트 종류로 기록되고,
// Events() 가 호출 순서를 보존하는지 검증한다.
func TestRecordAllKinds(t *testing.T) {
	tr := New()

	tr.StartRun("run-1")
	tr.RecordNodeStart("node-a", graph.State{"x": 1})
	tr.RecordNodeEnd("node-a", graph.StateUpdate{"x": 2})
	tr.RecordToolCall(message.ToolCall{ID: "call-1", Name: "search"})
	tr.RecordToolResult(tool.Result{Content: "ok"})
	tr.RecordLLMRequest(llm.ChatRequest{Model: "test-model"})
	tr.RecordLLMResponse(llm.ChatResponse{FinishReason: "stop"})
	tr.RecordError(errors.New("boom"))
	tr.EndRun("run-1")

	events := tr.Events()
	if len(events) != 7 {
		t.Fatalf("이벤트 개수 = %d, want 7", len(events))
	}

	// 순번이 호출 순서대로 1부터 증가하는지 확인
	for i, ev := range events {
		if ev.Seq != i+1 {
			t.Errorf("events[%d].Seq = %d, want %d", i, ev.Seq, i+1)
		}
	}

	// 종류·페이로드 확인
	ev := events[0]
	if ev.Kind != KindNode || ev.Node == nil || ev.Node.Phase != NodePhaseStart {
		t.Errorf("events[0] = %+v, want KindNode/NodePhaseStart", ev)
	}
	if ev.Node.Node != "node-a" || ev.Node.State["x"] != 1 {
		t.Errorf("events[0].Node = %+v, want node-a with state x=1", ev.Node)
	}

	ev = events[1]
	if ev.Kind != KindNode || ev.Node == nil || ev.Node.Phase != NodePhaseEnd {
		t.Errorf("events[1] = %+v, want KindNode/NodePhaseEnd", ev)
	}
	if ev.Node.Update["x"] != 2 {
		t.Errorf("events[1].Node.Update = %+v, want x=2", ev.Node.Update)
	}

	ev = events[2]
	if ev.Kind != KindTool || ev.Tool == nil || ev.Tool.Phase != ToolPhaseCall {
		t.Errorf("events[2] = %+v, want KindTool/ToolPhaseCall", ev)
	}
	if ev.Tool.Call == nil || ev.Tool.Call.Name != "search" {
		t.Errorf("events[2].Tool.Call = %+v, want name=search", ev.Tool.Call)
	}

	ev = events[3]
	if ev.Kind != KindTool || ev.Tool == nil || ev.Tool.Phase != ToolPhaseResult {
		t.Errorf("events[3] = %+v, want KindTool/ToolPhaseResult", ev)
	}
	if ev.Tool.Result == nil || ev.Tool.Result.Content != "ok" {
		t.Errorf("events[3].Tool.Result = %+v, want content=ok", ev.Tool.Result)
	}

	ev = events[4]
	if ev.Kind != KindLLM || ev.LLM == nil || ev.LLM.Phase != LLMPhaseRequest {
		t.Errorf("events[4] = %+v, want KindLLM/LLMPhaseRequest", ev)
	}
	if ev.LLM.Request == nil || ev.LLM.Request.Model != "test-model" {
		t.Errorf("events[4].LLM.Request = %+v, want model=test-model", ev.LLM.Request)
	}

	ev = events[5]
	if ev.Kind != KindLLM || ev.LLM == nil || ev.LLM.Phase != LLMPhaseResponse {
		t.Errorf("events[5] = %+v, want KindLLM/LLMPhaseResponse", ev)
	}
	if ev.LLM.Response == nil || ev.LLM.Response.FinishReason != "stop" {
		t.Errorf("events[5].LLM.Response = %+v, want finish_reason=stop", ev.LLM.Response)
	}

	ev = events[6]
	if ev.Kind != KindError || ev.Error == nil || ev.Error.Message != "boom" {
		t.Errorf("events[6] = %+v, want KindError with message=boom", ev)
	}
}

// TestEventsAppendSafe 는 Events() 가 반환한 슬라이스에 append 해도
// Trace 내부 누적 상태(길이)에 영향을 주지 않는지 확인한다.
func TestEventsAppendSafe(t *testing.T) {
	tr := New()
	tr.RecordError(errors.New("first"))

	events := tr.Events()
	events = append(events, Event{Kind: KindError, Error: &ErrorTrace{Message: "extra"}})
	if len(events) != 2 {
		t.Fatalf("len(events) = %d, want 2", len(events))
	}

	again := tr.Events()
	if len(again) != 1 {
		t.Fatalf("Events() 반환 슬라이스에 append 한 결과가 내부 상태에 반영되면 안 된다: len = %d, want 1", len(again))
	}
}

// TestExportJSONRoundTrip 은 네 종류 이벤트를 기록한 뒤 ExportJSON 으로 내보내고
// 다시 역직렬화하면 종류·필드 값이 보존되는지 확인한다. 값은 표준 JSON 규칙(숫자가
// float64 로 복원되는 등) 안에서 구성한다.
func TestExportJSONRoundTrip(t *testing.T) {
	tr := New()

	tr.StartRun("run-1")
	tr.RecordNodeStart("node-a", graph.State{"x": float64(1)})
	tr.RecordNodeEnd("node-a", graph.StateUpdate{"x": float64(2)})
	tr.RecordToolCall(message.ToolCall{ID: "call-1", Name: "search"})
	tr.RecordToolResult(tool.Result{Content: "ok"})
	tr.RecordLLMRequest(llm.ChatRequest{Model: "test-model"})
	tr.RecordLLMResponse(llm.ChatResponse{FinishReason: "stop"})
	tr.RecordError(errors.New("boom"))
	tr.EndRun("run-1")

	data, err := tr.ExportJSON()
	if err != nil {
		t.Fatalf("ExportJSON() error = %v, want nil", err)
	}

	var got []Event
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal(ExportJSON()) error = %v, want nil", err)
	}

	want := tr.Events()
	if len(got) != len(want) {
		t.Fatalf("len(got) = %d, want %d", len(got), len(want))
	}

	for i := range want {
		g, w := got[i], want[i]
		if g.Seq != w.Seq || g.Kind != w.Kind {
			t.Errorf("events[%d]: Seq/Kind = %d/%s, want %d/%s", i, g.Seq, g.Kind, w.Seq, w.Kind)
		}
	}

	// events[0]: NodeTrace 진입 — 이름·페이즈·상태 값 보존
	if got[0].Kind != KindNode || got[0].Node == nil || got[0].Node.Phase != NodePhaseStart {
		t.Fatalf("got[0] = %+v, want KindNode/NodePhaseStart", got[0])
	}
	if got[0].Node.Node != "node-a" || got[0].Node.State["x"] != float64(1) {
		t.Errorf("got[0].Node = %+v, want node-a with state x=1", got[0].Node)
	}
	if got[0].Node.Update != nil {
		t.Errorf("got[0].Node.Update = %+v, want nil (omitempty)", got[0].Node.Update)
	}

	// events[1]: NodeTrace 종료 — 업데이트 값 보존
	if got[1].Kind != KindNode || got[1].Node == nil || got[1].Node.Phase != NodePhaseEnd {
		t.Fatalf("got[1] = %+v, want KindNode/NodePhaseEnd", got[1])
	}
	if got[1].Node.Update["x"] != float64(2) {
		t.Errorf("got[1].Node.Update = %+v, want x=2", got[1].Node.Update)
	}
	if got[1].Node.State != nil {
		t.Errorf("got[1].Node.State = %+v, want nil (omitempty)", got[1].Node.State)
	}

	// events[2]/[3]: ToolTrace 호출/결과 — Call/Result 값 보존
	if got[2].Kind != KindTool || got[2].Tool == nil || got[2].Tool.Phase != ToolPhaseCall {
		t.Fatalf("got[2] = %+v, want KindTool/ToolPhaseCall", got[2])
	}
	if got[2].Tool.Call == nil || got[2].Tool.Call.Name != "search" || got[2].Tool.Call.ID != "call-1" {
		t.Errorf("got[2].Tool.Call = %+v, want id=call-1 name=search", got[2].Tool.Call)
	}
	if got[2].Tool.Result != nil {
		t.Errorf("got[2].Tool.Result = %+v, want nil (omitempty)", got[2].Tool.Result)
	}

	if got[3].Kind != KindTool || got[3].Tool == nil || got[3].Tool.Phase != ToolPhaseResult {
		t.Fatalf("got[3] = %+v, want KindTool/ToolPhaseResult", got[3])
	}
	if got[3].Tool.Result == nil || got[3].Tool.Result.Content != "ok" {
		t.Errorf("got[3].Tool.Result = %+v, want content=ok", got[3].Tool.Result)
	}

	// events[4]/[5]: LLMTrace 요청/응답 — Request/Response 값 보존
	if got[4].Kind != KindLLM || got[4].LLM == nil || got[4].LLM.Phase != LLMPhaseRequest {
		t.Fatalf("got[4] = %+v, want KindLLM/LLMPhaseRequest", got[4])
	}
	if got[4].LLM.Request == nil || got[4].LLM.Request.Model != "test-model" {
		t.Errorf("got[4].LLM.Request = %+v, want model=test-model", got[4].LLM.Request)
	}
	if got[4].LLM.Response != nil {
		t.Errorf("got[4].LLM.Response = %+v, want nil (omitempty)", got[4].LLM.Response)
	}

	if got[5].Kind != KindLLM || got[5].LLM == nil || got[5].LLM.Phase != LLMPhaseResponse {
		t.Fatalf("got[5] = %+v, want KindLLM/LLMPhaseResponse", got[5])
	}
	if got[5].LLM.Response == nil || got[5].LLM.Response.FinishReason != "stop" {
		t.Errorf("got[5].LLM.Response = %+v, want finish_reason=stop", got[5].LLM.Response)
	}

	// events[6]: ErrorTrace — 메시지 문자열 보존
	if got[6].Kind != KindError || got[6].Error == nil || got[6].Error.Message != "boom" {
		t.Errorf("got[6] = %+v, want KindError with message=boom", got[6])
	}
	if got[6].Node != nil || got[6].Tool != nil || got[6].LLM != nil {
		t.Errorf("got[6] non-error payloads should be nil (omitempty): %+v", got[6])
	}
}
