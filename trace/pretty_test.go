package trace

import (
	"errors"
	"strings"
	"testing"

	"github.com/zipkero/langgraph-go/graph"
	"github.com/zipkero/langgraph-go/llm"
	"github.com/zipkero/langgraph-go/message"
	"github.com/zipkero/langgraph-go/tool"
)

// TestPrettyIncludesRunBoundaryAndAllKinds 는 run 경계와 노드·도구·LLM·에러
// 네 종류 이벤트가 모두 Pretty() 출력에 식별 가능한 형태로 나타나는지 확인한다.
func TestPrettyIncludesRunBoundaryAndAllKinds(t *testing.T) {
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

	out := tr.Pretty()

	// run 구간 식별
	startIdx := strings.Index(out, "run-1")
	if startIdx == -1 || !strings.Contains(out, "RUN START") || !strings.Contains(out, "RUN END") {
		t.Fatalf("Pretty() 출력에 run 경계(run-1, RUN START/END)가 없다: %q", out)
	}

	wantSubstrings := []string{
		"node-a",     // 노드 이벤트
		"search",     // 도구 호출 이벤트
		"ok",         // 도구 결과 이벤트
		"test-model", // LLM 요청 이벤트
		"stop",       // LLM 응답 이벤트
		"boom",       // 에러 이벤트
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(out, want) {
			t.Errorf("Pretty() 출력에 %q 가 없다: %q", want, out)
		}
	}

	// run 시작이 첫 이벤트보다 앞서고, run 종료가 마지막 이벤트보다 뒤에 오는지 확인
	runStartPos := strings.Index(out, "RUN START")
	firstEventPos := strings.Index(out, "node-a")
	runEndPos := strings.LastIndex(out, "RUN END")
	lastEventPos := strings.LastIndex(out, "boom")
	if runStartPos == -1 || firstEventPos == -1 || runStartPos > firstEventPos {
		t.Errorf("RUN START 가 첫 이벤트보다 앞서지 않는다: runStartPos=%d firstEventPos=%d", runStartPos, firstEventPos)
	}
	if runEndPos == -1 || lastEventPos == -1 || runEndPos < lastEventPos {
		t.Errorf("RUN END 가 마지막 이벤트보다 뒤에 오지 않는다: runEndPos=%d lastEventPos=%d", runEndPos, lastEventPos)
	}
}

// TestPrettyEmptyRun 은 이벤트가 없는 run(StartRun 직후 바로 EndRun)도
// 시작/종료 경계가 함께 출력되는지 확인한다.
func TestPrettyEmptyRun(t *testing.T) {
	tr := New()
	tr.StartRun("empty-run")
	tr.EndRun("empty-run")

	out := tr.Pretty()
	if !strings.Contains(out, "empty-run") || !strings.Contains(out, "RUN START") || !strings.Contains(out, "RUN END") {
		t.Errorf("빈 run 경계가 Pretty() 출력에 없다: %q", out)
	}
}

// TestPrettyOpenRun 은 EndRun 이 호출되지 않은 run 도 식별 가능하게 나타나는지 확인한다.
func TestPrettyOpenRun(t *testing.T) {
	tr := New()
	tr.StartRun("open-run")
	tr.RecordError(errors.New("mid"))

	out := tr.Pretty()
	if !strings.Contains(out, "open-run") {
		t.Errorf("열린 run 이 Pretty() 출력에 없다: %q", out)
	}
}
