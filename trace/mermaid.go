package trace

import (
	"fmt"
	"strings"
)

// Mermaid 는 기록된 NodeTrace 이벤트(노드 진입/종료)만 골라 진입/종료 순서대로
// mermaid flowchart 노드·엣지 텍스트로 렌더링한다. 도구·LLM·에러 이벤트는 흐름에
// 포함하지 않는다(ANALYSIS Decision (c)). error 를 반환하지 않는 순수 문자열
// 생성이며, 외부 렌더링 라이브러리·네트워크를 쓰지 않는다.
//
// 출력 구조:
//
//	flowchart TD
//	  n1["node-a (start)"]
//	  n2["node-a (end)"]
//	  n1 --> n2
//
// 각 NodeTrace 이벤트는 자신의 기록 순번(Seq)을 딴 고유 노드로 선언되고, 기록
// 순서대로 인접한 두 노드 사이에 엣지가 그려진다. 같은 노드 이름을 여러 번 방문해도
// 방문마다 별개의 mermaid 노드로 나타나 진입/종료 순서를 그대로 보존한다.
func (t *Trace) Mermaid() string {
	t.mu.Lock()
	defer t.mu.Unlock()

	var nodeEvents []Event
	for _, ev := range t.events {
		if ev.Kind == KindNode {
			nodeEvents = append(nodeEvents, ev)
		}
	}

	var b strings.Builder
	b.WriteString("flowchart TD\n")

	ids := make([]string, len(nodeEvents))
	for i, ev := range nodeEvents {
		id := mermaidStepID(ev.Seq)
		ids[i] = id
		fmt.Fprintf(&b, "  %s[\"%s\"]\n", id, mermaidStepLabel(ev.Node))
	}
	for i := 1; i < len(ids); i++ {
		fmt.Fprintf(&b, "  %s --> %s\n", ids[i-1], ids[i])
	}

	return b.String()
}

// mermaidStepID 는 이벤트 순번(Seq)으로부터 mermaid 노드 ID를 만든다.
func mermaidStepID(seq int) string {
	return fmt.Sprintf("n%d", seq)
}

// mermaidStepLabel 은 NodeTrace 를 "노드이름 (phase)" 형태의 mermaid 라벨 문자열로 만든다.
func mermaidStepLabel(n *NodeTrace) string {
	return fmt.Sprintf("%s (%s)", n.Node, n.Phase)
}
