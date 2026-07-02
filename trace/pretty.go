package trace

import (
	"fmt"
	"strings"
)

// Pretty 는 누적된 이벤트를 사람이 읽는 텍스트로 렌더링한다.
// run 시작/종료 경계를 표시하고, 누적 이벤트를 순서대로 순회하며 노드·도구·LLM·에러
// 각 종류를 식별 가능한 한 줄 형태로 렌더링한다. error 를 반환하지 않는 순수 문자열
// 생성이며(ANALYSIS Decision (c)), 외부 렌더링 라이브러리·네트워크를 쓰지 않는다.
func (t *Trace) Pretty() string {
	t.mu.Lock()
	defer t.mu.Unlock()

	// seq -> 그 시점에 시작/종료하는 run 목록. Seq 는 append 에서 1부터 증가해
	// t.events 인덱스(0-based)와 1:1 대응하므로 events[seq-1] 로 바로 찾을 수 있다.
	starts := make(map[int][]runSpan)
	ends := make(map[int][]runSpan)
	for _, r := range t.runs {
		starts[r.startSeq] = append(starts[r.startSeq], r)
		if r.closed {
			ends[r.endSeq] = append(ends[r.endSeq], r)
		}
	}

	var b strings.Builder
	n := t.seq

	// 이벤트가 하나도 없는 사이(예: StartRun 직후 EndRun)에 걸린 run 은 startSeq >
	// endSeq 형태로 나타난다. 그 지점에서 시작/종료를 붙여 출력한다.
	emitStartsAt := func(seq int) {
		for _, r := range starts[seq] {
			if r.closed && r.endSeq < r.startSeq {
				fmt.Fprintf(&b, "=== RUN START %s ===\n", r.runID)
				fmt.Fprintf(&b, "=== RUN END %s ===\n", r.runID)
				continue
			}
			fmt.Fprintf(&b, "=== RUN START %s ===\n", r.runID)
		}
	}
	emitEndsAt := func(seq int) {
		for _, r := range ends[seq] {
			if r.endSeq < r.startSeq {
				continue // emitStartsAt 에서 이미 함께 출력했다
			}
			fmt.Fprintf(&b, "=== RUN END %s ===\n", r.runID)
		}
	}

	for seq := 1; seq <= n; seq++ {
		emitStartsAt(seq)
		b.WriteString(formatEvent(t.events[seq-1]))
		b.WriteString("\n")
		emitEndsAt(seq)
	}
	// 마지막 기록 이후에 시작(또는 시작+종료)하는 run(이벤트가 하나도 없는 run)을 출력한다.
	emitStartsAt(n + 1)
	// 아직 닫히지 않은 run 을 표시한다.
	for _, r := range t.runs {
		if !r.closed {
			fmt.Fprintf(&b, "=== RUN %s (open) ===\n", r.runID)
		}
	}

	return b.String()
}

// formatEvent 는 이벤트 하나를 종류별로 식별 가능한 한 줄 텍스트로 렌더링한다.
func formatEvent(ev Event) string {
	switch ev.Kind {
	case KindNode:
		n := ev.Node
		switch n.Phase {
		case NodePhaseStart:
			return fmt.Sprintf("[%d] NODE start %s state=%v", ev.Seq, n.Node, n.State)
		case NodePhaseEnd:
			return fmt.Sprintf("[%d] NODE end %s update=%v", ev.Seq, n.Node, n.Update)
		default:
			return fmt.Sprintf("[%d] NODE %s %s", ev.Seq, n.Phase, n.Node)
		}
	case KindTool:
		tt := ev.Tool
		switch tt.Phase {
		case ToolPhaseCall:
			if tt.Call != nil {
				return fmt.Sprintf("[%d] TOOL call id=%s name=%s args=%s", ev.Seq, tt.Call.ID, tt.Call.Name, tt.Call.Args)
			}
			return fmt.Sprintf("[%d] TOOL call", ev.Seq)
		case ToolPhaseResult:
			if tt.Result != nil {
				return fmt.Sprintf("[%d] TOOL result content=%s is_error=%v", ev.Seq, tt.Result.Content, tt.Result.IsError)
			}
			return fmt.Sprintf("[%d] TOOL result", ev.Seq)
		default:
			return fmt.Sprintf("[%d] TOOL %s", ev.Seq, tt.Phase)
		}
	case KindLLM:
		lt := ev.LLM
		switch lt.Phase {
		case LLMPhaseRequest:
			if lt.Request != nil {
				return fmt.Sprintf("[%d] LLM request model=%s", ev.Seq, lt.Request.Model)
			}
			return fmt.Sprintf("[%d] LLM request", ev.Seq)
		case LLMPhaseResponse:
			if lt.Response != nil {
				return fmt.Sprintf("[%d] LLM response finish_reason=%s", ev.Seq, lt.Response.FinishReason)
			}
			return fmt.Sprintf("[%d] LLM response", ev.Seq)
		default:
			return fmt.Sprintf("[%d] LLM %s", ev.Seq, lt.Phase)
		}
	case KindError:
		return fmt.Sprintf("[%d] ERROR %s", ev.Seq, ev.Error.Message)
	default:
		return fmt.Sprintf("[%d] UNKNOWN", ev.Seq)
	}
}
