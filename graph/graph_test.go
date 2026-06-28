// graph_test.go 는 task-003 검증 조건을 만족하는 단위 테스트를 담는다.
// 모든 테스트는 stub 노드(정해진 StateUpdate/nil을 반환)를 사용하며
// 네트워크·API 키 없이 수행된다(D11).
package graph_test

import (
	"context"
	"testing"

	"github.com/zipkero/langgraph-go/graph"
)

// stubNode 는 미리 정해진 StateUpdate를 반환하는 stub 노드 함수다.
func stubNode(update graph.StateUpdate) graph.NodeFunc {
	return func(ctx context.Context, st graph.State) (any, error) {
		return update, nil
	}
}

// TestCompile_정상그래프 는 올바르게 구성된 그래프가 Compile에 성공함을 검증한다.
func TestCompile_정상그래프(t *testing.T) {
	schema := graph.StateSchema{
		Reducers: map[string]graph.ReducerFunc{},
	}
	b := graph.NewStateGraph(schema)

	if err := b.AddNode("A", stubNode(graph.StateUpdate{"x": 1})); err != nil {
		t.Fatalf("AddNode A 실패: %v", err)
	}
	if err := b.AddNode("B", stubNode(graph.StateUpdate{"x": 2})); err != nil {
		t.Fatalf("AddNode B 실패: %v", err)
	}
	if err := b.AddEdge("A", "B"); err != nil {
		t.Fatalf("AddEdge 실패: %v", err)
	}
	if err := b.SetEntryPoint("A"); err != nil {
		t.Fatalf("SetEntryPoint 실패: %v", err)
	}

	compiled, err := b.Compile()
	if err != nil {
		t.Fatalf("Compile 실패(기대: 성공): %v", err)
	}
	if compiled == nil {
		t.Fatal("Compile이 nil을 반환했습니다")
	}
}

// TestCompile_미정의노드엣지 는 미정의 노드를 가리키는 엣지가 있을 때 Compile이 error를 반환함을 검증한다.
func TestCompile_미정의노드엣지(t *testing.T) {
	schema := graph.StateSchema{}
	b := graph.NewStateGraph(schema)

	if err := b.AddNode("A", stubNode(nil)); err != nil {
		t.Fatalf("AddNode 실패: %v", err)
	}
	// B는 정의되지 않았으나 엣지에서 참조한다
	if err := b.AddEdge("A", "UNDEFINED_NODE"); err != nil {
		t.Fatalf("AddEdge 실패: %v", err)
	}
	if err := b.SetEntryPoint("A"); err != nil {
		t.Fatalf("SetEntryPoint 실패: %v", err)
	}

	_, err := b.Compile()
	if err == nil {
		t.Fatal("Compile이 성공했습니다(기대: 미정의 노드 error)")
	}
}

// TestCompile_도달불가노드 는 진입점에서 도달할 수 없는 노드가 있을 때 Compile이 error를 반환함을 검증한다.
func TestCompile_도달불가노드(t *testing.T) {
	schema := graph.StateSchema{}
	b := graph.NewStateGraph(schema)

	if err := b.AddNode("A", stubNode(nil)); err != nil {
		t.Fatalf("AddNode A 실패: %v", err)
	}
	// B는 정의됐지만 A에서 도달하는 엣지가 없다
	if err := b.AddNode("UNREACHABLE", stubNode(nil)); err != nil {
		t.Fatalf("AddNode UNREACHABLE 실패: %v", err)
	}
	if err := b.SetEntryPoint("A"); err != nil {
		t.Fatalf("SetEntryPoint 실패: %v", err)
	}

	_, err := b.Compile()
	if err == nil {
		t.Fatal("Compile이 성공했습니다(기대: 도달 불가 노드 error)")
	}
}

// TestCompile_조건엣지_정상 은 조건 엣지가 있는 정상 그래프가 Compile에 성공함을 검증한다.
func TestCompile_조건엣지_정상(t *testing.T) {
	schema := graph.StateSchema{}
	b := graph.NewStateGraph(schema)

	if err := b.AddNode("A", stubNode(nil)); err != nil {
		t.Fatalf("AddNode A 실패: %v", err)
	}
	if err := b.AddNode("B", stubNode(nil)); err != nil {
		t.Fatalf("AddNode B 실패: %v", err)
	}
	if err := b.AddNode("C", stubNode(nil)); err != nil {
		t.Fatalf("AddNode C 실패: %v", err)
	}

	router := func(ctx context.Context, st graph.State) string {
		return "branch_b"
	}
	if err := b.AddConditionalEdges("A", router, map[string]string{
		"branch_b": "B",
		"branch_c": "C",
	}); err != nil {
		t.Fatalf("AddConditionalEdges 실패: %v", err)
	}
	if err := b.SetEntryPoint("A"); err != nil {
		t.Fatalf("SetEntryPoint 실패: %v", err)
	}

	compiled, err := b.Compile()
	if err != nil {
		t.Fatalf("Compile 실패(기대: 성공): %v", err)
	}
	if compiled == nil {
		t.Fatal("Compile이 nil을 반환했습니다")
	}
}

// TestCompile_조건엣지_미정의노드 는 조건 엣지 mapping이 미정의 노드를 가리킬 때 Compile이 error를 반환함을 검증한다.
func TestCompile_조건엣지_미정의노드(t *testing.T) {
	schema := graph.StateSchema{}
	b := graph.NewStateGraph(schema)

	if err := b.AddNode("A", stubNode(nil)); err != nil {
		t.Fatalf("AddNode A 실패: %v", err)
	}

	router := func(ctx context.Context, st graph.State) string { return "ok" }
	if err := b.AddConditionalEdges("A", router, map[string]string{
		"ok": "UNDEFINED",
	}); err != nil {
		t.Fatalf("AddConditionalEdges 실패: %v", err)
	}
	if err := b.SetEntryPoint("A"); err != nil {
		t.Fatalf("SetEntryPoint 실패: %v", err)
	}

	_, err := b.Compile()
	if err == nil {
		t.Fatal("Compile이 성공했습니다(기대: 미정의 노드 error)")
	}
}

// TestCompile_진입점미설정 은 진입점이 없으면 Compile이 error를 반환함을 검증한다.
func TestCompile_진입점미설정(t *testing.T) {
	schema := graph.StateSchema{}
	b := graph.NewStateGraph(schema)

	if err := b.AddNode("A", stubNode(nil)); err != nil {
		t.Fatalf("AddNode 실패: %v", err)
	}

	_, err := b.Compile()
	if err == nil {
		t.Fatal("Compile이 성공했습니다(기대: 진입점 미설정 error)")
	}
}

// TestCompile_WithInputOutputSchema 는 WithInputSchema/WithOutputSchema 옵션으로 빌드된 정상 그래프가 Compile에 성공함을 검증한다.
func TestCompile_WithInputOutputSchema(t *testing.T) {
	schema := graph.StateSchema{}
	b := graph.NewStateGraph(schema,
		graph.WithInputSchema("input_field"),
		graph.WithOutputSchema("output_field"),
	)

	if err := b.AddNode("A", stubNode(graph.StateUpdate{"output_field": "result"})); err != nil {
		t.Fatalf("AddNode 실패: %v", err)
	}
	if err := b.SetEntryPoint("A"); err != nil {
		t.Fatalf("SetEntryPoint 실패: %v", err)
	}

	compiled, err := b.Compile()
	if err != nil {
		t.Fatalf("Compile 실패: %v", err)
	}
	if compiled == nil {
		t.Fatal("Compile이 nil을 반환했습니다")
	}
}

// TestCompile_조건진입점 은 조건 진입점으로 설정된 정상 그래프가 Compile에 성공함을 검증한다.
func TestCompile_조건진입점(t *testing.T) {
	schema := graph.StateSchema{}
	b := graph.NewStateGraph(schema)

	if err := b.AddNode("A", stubNode(nil)); err != nil {
		t.Fatalf("AddNode A 실패: %v", err)
	}
	if err := b.AddNode("B", stubNode(nil)); err != nil {
		t.Fatalf("AddNode B 실패: %v", err)
	}

	router := func(ctx context.Context, st graph.State) string { return "go_a" }
	if err := b.SetConditionalEntryPoint(router, map[string]string{
		"go_a": "A",
		"go_b": "B",
	}); err != nil {
		t.Fatalf("SetConditionalEntryPoint 실패: %v", err)
	}

	compiled, err := b.Compile()
	if err != nil {
		t.Fatalf("Compile 실패(기대: 성공): %v", err)
	}
	if compiled == nil {
		t.Fatal("Compile이 nil을 반환했습니다")
	}
}
