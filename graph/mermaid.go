// mermaid.go 는 Compiled 그래프의 mermaid flowchart 텍스트 렌더 로직을 담는다(SPEC §5.12, D10).
// DrawMermaidPNG는 정의하지 않는다(SPEC §4).
package graph

import (
	"fmt"
	"sort"
	"strings"
)

// drawMermaid 는 컴파일된 그래프의 노드·엣지·조건엣지·진입점을
// mermaid flowchart TD 텍스트로 렌더해 반환한다.
//
// 출력 구조:
//
//	flowchart TD
//	  __START__ --> 진입점노드
//	  노드A --> 노드B   (정적 엣지)
//	  노드A -->|키| 노드B   (조건엣지 mapping)
//
// 노드는 이름을 알파벳 순으로 정렬해 결정론적 출력을 보장한다.
func drawMermaid(c *Compiled) string {
	var sb strings.Builder
	sb.WriteString("flowchart TD\n")

	// 노드 선언: 정렬된 순서로 출력한다.
	nodeNames := make([]string, 0, len(c.nodes))
	for name := range c.nodes {
		nodeNames = append(nodeNames, name)
	}
	sort.Strings(nodeNames)
	for _, name := range nodeNames {
		sb.WriteString(fmt.Sprintf("  %s\n", mermaidNodeID(name)))
	}

	// 진입점 엣지: __START__ → 첫 노드
	if c.condEntry != nil {
		// 조건 진입점: mapping의 각 대상 노드로 엣지를 그린다.
		keys := sortedKeys(c.condEntry.mapping)
		for _, key := range keys {
			target := c.condEntry.mapping[key]
			sb.WriteString(fmt.Sprintf("  __START__ -->|%s| %s\n", key, mermaidNodeID(target)))
		}
	} else if c.entryPoint != "" {
		sb.WriteString(fmt.Sprintf("  __START__ --> %s\n", mermaidNodeID(c.entryPoint)))
	}

	// 정적 엣지
	for _, e := range c.edges {
		sb.WriteString(fmt.Sprintf("  %s --> %s\n", mermaidNodeID(e.from), mermaidNodeID(e.to)))
	}

	// 조건 엣지: mapping의 각 키를 레이블로 표시한다.
	for _, ce := range c.condEdges {
		keys := sortedKeys(ce.mapping)
		for _, key := range keys {
			target := ce.mapping[key]
			sb.WriteString(fmt.Sprintf("  %s -->|%s| %s\n", mermaidNodeID(ce.from), key, mermaidNodeID(target)))
		}
	}

	return sb.String()
}

// mermaidNodeID 는 노드 이름을 mermaid 노드 ID 표기로 변환한다.
// 공백·특수 문자는 밑줄로 치환한다.
func mermaidNodeID(name string) string {
	r := strings.NewReplacer(
		" ", "_",
		"-", "_",
		".", "_",
		"/", "_",
	)
	return r.Replace(name)
}

// sortedKeys 는 map[string]string의 키를 정렬된 슬라이스로 반환한다.
// 결정론적 출력을 보장하기 위해 사용한다.
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
