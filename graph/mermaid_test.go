// mermaid_test.go 는 task-012 검증 조건 중 DrawMermaid 출력을 검증하는 단위 테스트를 담는다.
// 모든 테스트는 stub 노드를 사용하며 네트워크·API 키 없이 수행된다(D11).
package graph_test

import (
	"context"
	"strings"
	"testing"

	"github.com/zipkero/langgraph-go/graph"
)

// buildStubGraph 는 DrawMermaid 테스트용 stub 그래프를 빌드해 반환한다.
//
// 구성:
//
//	A → B (정적 엣지)
//	B →|go_c| C, |go_end| C (조건 엣지)
//	진입점: A
func buildStubGraph(t *testing.T) *graph.Compiled {
	t.Helper()
	schema := graph.StateSchema{Reducers: map[string]graph.ReducerFunc{}}
	b := graph.NewStateGraph(schema)

	noop := func(_ context.Context, st graph.State) (any, error) { return nil, nil }

	if err := b.AddNode("A", noop); err != nil {
		t.Fatalf("AddNode A: %v", err)
	}
	if err := b.AddNode("B", noop); err != nil {
		t.Fatalf("AddNode B: %v", err)
	}
	if err := b.AddNode("C", noop); err != nil {
		t.Fatalf("AddNode C: %v", err)
	}
	if err := b.AddEdge("A", "B"); err != nil {
		t.Fatalf("AddEdge A→B: %v", err)
	}
	router := func(_ context.Context, st graph.State) string {
		if v, ok := st["go"]; ok && v == "c" {
			return "go_c"
		}
		return "go_end"
	}
	if err := b.AddConditionalEdges("B", router, map[string]string{
		"go_c":   "C",
		"go_end": "C",
	}); err != nil {
		t.Fatalf("AddConditionalEdges B: %v", err)
	}
	if err := b.SetEntryPoint("A"); err != nil {
		t.Fatalf("SetEntryPoint: %v", err)
	}

	compiled, err := b.Compile()
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	return compiled
}

// TestDrawMermaid_헤더 는 DrawMermaid 결과가 "flowchart TD" 헤더로 시작함을 검증한다.
func TestDrawMermaid_헤더(t *testing.T) {
	compiled := buildStubGraph(t)
	output := compiled.DrawMermaid()
	if !strings.HasPrefix(output, "flowchart TD\n") {
		t.Errorf("DrawMermaid 출력이 'flowchart TD'로 시작하지 않음:\n%s", output)
	}
}

// TestDrawMermaid_노드포함 는 DrawMermaid 결과에 그래프의 모든 노드가 포함됨을 검증한다.
func TestDrawMermaid_노드포함(t *testing.T) {
	compiled := buildStubGraph(t)
	output := compiled.DrawMermaid()

	for _, node := range []string{"A", "B", "C"} {
		if !strings.Contains(output, node) {
			t.Errorf("DrawMermaid 출력에 노드 %q가 없음:\n%s", node, output)
		}
	}
}

// TestDrawMermaid_정적엣지포함 는 DrawMermaid 결과에 정적 엣지(A → B)가 포함됨을 검증한다.
func TestDrawMermaid_정적엣지포함(t *testing.T) {
	compiled := buildStubGraph(t)
	output := compiled.DrawMermaid()

	// 정적 엣지: A --> B
	if !strings.Contains(output, "A --> B") {
		t.Errorf("DrawMermaid 출력에 정적 엣지 'A --> B'가 없음:\n%s", output)
	}
}

// TestDrawMermaid_진입점엣지포함 는 DrawMermaid 결과에 __START__ → A 진입점 엣지가 포함됨을 검증한다.
func TestDrawMermaid_진입점엣지포함(t *testing.T) {
	compiled := buildStubGraph(t)
	output := compiled.DrawMermaid()

	if !strings.Contains(output, "__START__ --> A") {
		t.Errorf("DrawMermaid 출력에 진입점 엣지 '__START__ --> A'가 없음:\n%s", output)
	}
}

// TestDrawMermaid_조건엣지포함 는 DrawMermaid 결과에 조건 엣지 레이블(go_c, go_end)이 포함됨을 검증한다.
func TestDrawMermaid_조건엣지포함(t *testing.T) {
	compiled := buildStubGraph(t)
	output := compiled.DrawMermaid()

	// 조건 엣지: B -->|go_c| C, B -->|go_end| C
	for _, label := range []string{"go_c", "go_end"} {
		if !strings.Contains(output, label) {
			t.Errorf("DrawMermaid 출력에 조건 엣지 레이블 %q가 없음:\n%s", label, output)
		}
	}
	if !strings.Contains(output, "B -->") {
		t.Errorf("DrawMermaid 출력에 조건 엣지 출발 노드 'B -->'가 없음:\n%s", output)
	}
}

// TestDrawMermaid_조건진입점 은 조건 진입점이 설정된 그래프에서 DrawMermaid가
// __START__ -->|key| node 형식의 엣지를 포함함을 검증한다.
func TestDrawMermaid_조건진입점(t *testing.T) {
	schema := graph.StateSchema{Reducers: map[string]graph.ReducerFunc{}}
	b := graph.NewStateGraph(schema)

	noop := func(_ context.Context, st graph.State) (any, error) { return nil, nil }
	if err := b.AddNode("X", noop); err != nil {
		t.Fatalf("AddNode X: %v", err)
	}
	if err := b.AddNode("Y", noop); err != nil {
		t.Fatalf("AddNode Y: %v", err)
	}
	router := func(_ context.Context, st graph.State) string { return "go_x" }
	if err := b.SetConditionalEntryPoint(router, map[string]string{
		"go_x": "X",
		"go_y": "Y",
	}); err != nil {
		t.Fatalf("SetConditionalEntryPoint: %v", err)
	}

	compiled, err := b.Compile()
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	output := compiled.DrawMermaid()
	if !strings.Contains(output, "__START__") {
		t.Errorf("DrawMermaid 출력에 '__START__'가 없음:\n%s", output)
	}
	for _, label := range []string{"go_x", "go_y"} {
		if !strings.Contains(output, label) {
			t.Errorf("DrawMermaid 출력에 조건 진입점 레이블 %q가 없음:\n%s", label, output)
		}
	}
}

// TestDrawMermaid_결정론적출력 는 같은 그래프에서 DrawMermaid를 두 번 호출해도 같은 출력이 나옴을 검증한다.
func TestDrawMermaid_결정론적출력(t *testing.T) {
	compiled := buildStubGraph(t)
	first := compiled.DrawMermaid()
	second := compiled.DrawMermaid()
	if first != second {
		t.Errorf("DrawMermaid 출력이 두 번 호출에서 다름:\nfirst=%q\nsecond=%q", first, second)
	}
}
