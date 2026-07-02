// toolevents_test.go 는 runTools 의 호출별 Runtime 구성(tool_call ID·RunConfig 전달)과
// WithToolEventSink 의 호출 전/후 이벤트 방출을 검증한다. 네트워크 호출 없음.
package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/zipkero/langgraph-go/config"
	"github.com/zipkero/langgraph-go/llm"
	"github.com/zipkero/langgraph-go/message"
	"github.com/zipkero/langgraph-go/tool"
)

// rtRecorderTool 은 실행 시 Runtime 의 ToolCallID/Config 를 기록하는 테스트용 도구다.
type rtRecorderTool struct {
	seenCallIDs   []string
	seenThreadIDs []string
}

func (r *rtRecorderTool) Name() string        { return "recorder" }
func (r *rtRecorderTool) Description() string { return "Runtime 값을 기록한다" }
func (r *rtRecorderTool) Schema() tool.Schema {
	return tool.Schema{Name: "recorder", Description: "Runtime 값을 기록한다"}
}
func (r *rtRecorderTool) Execute(_ context.Context, _ tool.Input, rt tool.Runtime) (tool.Result, error) {
	r.seenCallIDs = append(r.seenCallIDs, rt.ToolCallID())
	r.seenThreadIDs = append(r.seenThreadIDs, config.GetThreadID(rt.Config()))
	return tool.Result{Content: "ok"}, nil
}

// TestRunTools_PerCallRuntime_ToolCallIDAndConfig 는 도구가 rt.ToolCallID() 로 자기 호출을
// 식별하고 rt.Config() 로 실행별 설정에 접근할 수 있는지 검증한다.
func TestRunTools_PerCallRuntime_ToolCallIDAndConfig(t *testing.T) {
	rec := &rtRecorderTool{}

	client := newSeqStubClient("stub",
		llm.StubResponse{ToolCalls: []message.ToolCall{
			{ID: "call-1", Name: "recorder", Args: json.RawMessage(`{}`)},
			{ID: "call-2", Name: "recorder", Args: json.RawMessage(`{}`)},
		}},
		llm.StubResponse{Message: message.NewAssistantMessage("done")},
	)

	a, err := Create(client, []tool.Tool{rec})
	if err != nil {
		t.Fatalf("Create 실패: %v", err)
	}

	cfg := config.RunConfig{Configurable: map[string]any{"thread_id": "t-42"}}
	if _, err := a.Invoke(context.Background(), Input{Messages: []message.Message{message.NewUserMessage("hi")}}, cfg); err != nil {
		t.Fatalf("Invoke 실패: %v", err)
	}

	if len(rec.seenCallIDs) != 2 || rec.seenCallIDs[0] != "call-1" || rec.seenCallIDs[1] != "call-2" {
		t.Errorf("호출별 ToolCallID 기대 [call-1 call-2], 실제 %v", rec.seenCallIDs)
	}
	for i, tid := range rec.seenThreadIDs {
		if tid != "t-42" {
			t.Errorf("호출 %d의 thread_id 기대 t-42, 실제 %q", i, tid)
		}
	}
}

// TestWithToolEventSink_EmitsCallAndResultEvents 는 싱크가 도구 호출 전(Result nil)·후(Result 채움)
// 이벤트를 호출 순서대로 수신하는지 검증한다.
func TestWithToolEventSink_EmitsCallAndResultEvents(t *testing.T) {
	client := newSeqStubClient("stub",
		llm.StubResponse{ToolCalls: []message.ToolCall{
			{ID: "call-1", Name: "recorder", Args: json.RawMessage(`{}`)},
		}},
		llm.StubResponse{Message: message.NewAssistantMessage("done")},
	)

	var events []tool.Event
	a, err := Create(client, []tool.Tool{&rtRecorderTool{}}, WithToolEventSink(func(ev tool.Event) {
		events = append(events, ev)
	}))
	if err != nil {
		t.Fatalf("Create 실패: %v", err)
	}

	if _, err := a.Invoke(context.Background(), Input{Messages: []message.Message{message.NewUserMessage("hi")}}, config.RunConfig{}); err != nil {
		t.Fatalf("Invoke 실패: %v", err)
	}

	if len(events) != 2 {
		t.Fatalf("이벤트 수 기대 2(호출 전/후), 실제 %d", len(events))
	}
	call, result := events[0], events[1]
	if call.Result != nil {
		t.Error("첫 이벤트(호출 전)는 Result 가 nil 이어야 한다")
	}
	if result.Result == nil || result.Result.Content != "ok" {
		t.Errorf("둘째 이벤트(호출 후)의 Result 기대 ok, 실제 %+v", result.Result)
	}
	for i, ev := range events {
		if ev.ToolName != "recorder" || ev.ToolCallID != "call-1" {
			t.Errorf("이벤트 %d 식별자 불일치: %+v", i, ev)
		}
	}
}
