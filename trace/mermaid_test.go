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

// TestMermaidRendersNodeFlowInOrder 는 여러 노드의 진입/종료가 기록 순서대로
// mermaid 노드·엣지로 나타나는지 확인한다.
func TestMermaidRendersNodeFlowInOrder(t *testing.T) {
	tr := New()

	tr.StartRun("run-1")
	tr.RecordNodeStart("node-a", graph.State{"x": 1})
	tr.RecordNodeEnd("node-a", graph.StateUpdate{"x": 2})
	tr.RecordNodeStart("node-b", graph.State{"x": 2})
	tr.RecordNodeEnd("node-b", graph.StateUpdate{"x": 3})
	tr.EndRun("run-1")

	out := tr.Mermaid()

	if !strings.HasPrefix(out, "flowchart TD\n") {
		t.Fatalf("Mermaid() 출력이 flowchart 선언으로 시작하지 않는다: %q", out)
	}

	// 노드 선언 순서: node-a start, node-a end, node-b start, node-b end
	idxAStart := strings.Index(out, "node-a (start)")
	idxAEnd := strings.Index(out, "node-a (end)")
	idxBStart := strings.Index(out, "node-b (start)")
	idxBEnd := strings.Index(out, "node-b (end)")
	if idxAStart == -1 || idxAEnd == -1 || idxBStart == -1 || idxBEnd == -1 {
		t.Fatalf("Mermaid() 출력에 노드 진입/종료 라벨이 모두 있어야 한다: %q", out)
	}
	if !(idxAStart < idxAEnd && idxAEnd < idxBStart && idxBStart < idxBEnd) {
		t.Errorf("노드 진입/종료 순서가 기록 순서와 일치하지 않는다: %q", out)
	}

	// 순번 기반 노드 ID(n1..n4)가 순서대로 --> 로 연결돼야 한다
	wantEdges := []string{"n1 --> n2", "n2 --> n3", "n3 --> n4"}
	for _, want := range wantEdges {
		if !strings.Contains(out, want) {
			t.Errorf("Mermaid() 출력에 엣지 %q 가 없다: %q", want, out)
		}
	}
}

// TestMermaidExcludesNonNodeEvents 는 도구·LLM·에러 이벤트가 mermaid 노드 흐름에서
// 제외되는지 확인한다.
func TestMermaidExcludesNonNodeEvents(t *testing.T) {
	tr := New()

	tr.StartRun("run-1")
	tr.RecordNodeStart("node-a", graph.State{"x": 1})
	tr.RecordToolCall(message.ToolCall{ID: "call-1", Name: "search-tool"})
	tr.RecordToolResult(tool.Result{Content: "tool-output"})
	tr.RecordLLMRequest(llm.ChatRequest{Model: "test-model"})
	tr.RecordLLMResponse(llm.ChatResponse{FinishReason: "stop-reason"})
	tr.RecordError(errors.New("boom-error"))
	tr.RecordNodeEnd("node-a", graph.StateUpdate{"x": 2})
	tr.EndRun("run-1")

	out := tr.Mermaid()

	// 노드 이벤트만 두 개(start/end) 존재해야 하고, 바로 연결돼야 한다.
	if !strings.Contains(out, "node-a (start)") || !strings.Contains(out, "node-a (end)") {
		t.Fatalf("Mermaid() 출력에 노드 진입/종료가 있어야 한다: %q", out)
	}
	if !strings.Contains(out, "n1 --> n7") {
		t.Errorf("비노드 이벤트를 건너뛰고 노드 진입/종료가 직접 연결돼야 한다: %q", out)
	}

	// 도구·LLM·에러 관련 내용은 mermaid 흐름 출력에 나타나지 않아야 한다.
	excluded := []string{"search-tool", "tool-output", "test-model", "stop-reason", "boom-error"}
	for _, s := range excluded {
		if strings.Contains(out, s) {
			t.Errorf("Mermaid() 출력에 비노드 이벤트 내용 %q 가 포함되면 안 된다: %q", s, out)
		}
	}
}
