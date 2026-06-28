// exec_test.go 는 task-004 검증 조건을 만족하는 단위 테스트를 담는다.
// 모든 테스트는 stub 노드(정해진 StateUpdate/Command를 반환)를 사용하며
// 네트워크·API 키 없이 수행된다(D11).
package graph_test

import (
	"context"
	"errors"
	"testing"

	"github.com/zipkero/langgraph-go/config"
	"github.com/zipkero/langgraph-go/graph"
)

// TestInvoke_리듀서필드누적_덮어쓰기 는 등록된 리듀서 필드가 누적 병합되고,
// 미등록 필드는 last-write-wins로 처리됨을 검증한다.
//
// 그래프 구성: A → B (정적 엣지)
//   - 리듀서 필드 "items": slice append 리듀서
//   - 비리듀서 필드 "label": 마지막 값으로 덮어쓰기
//
// 기대 최종 State:
//
//	items = []any{"a", "b"}   (두 노드의 값이 누적됨)
//	label = "from_B"           (B의 값이 마지막 값으로 덮어씀)
func TestInvoke_리듀서필드누적_덮어쓰기(t *testing.T) {
	// items 필드에 등록할 slice-append 리듀서
	sliceAppend := func(cur, upd any) any {
		var base []any
		if cur != nil {
			if s, ok := cur.([]any); ok {
				base = s
			}
		}
		if upd == nil {
			return base
		}
		return append(base, upd)
	}

	schema := graph.StateSchema{
		Reducers: map[string]graph.ReducerFunc{
			"items": sliceAppend,
		},
	}
	b := graph.NewStateGraph(schema)

	nodeA := func(ctx context.Context, st graph.State) (any, error) {
		return graph.StateUpdate{"items": "a", "label": "from_A"}, nil
	}
	nodeB := func(ctx context.Context, st graph.State) (any, error) {
		return graph.StateUpdate{"items": "b", "label": "from_B"}, nil
	}

	if err := b.AddNode("A", nodeA); err != nil {
		t.Fatalf("AddNode A 실패: %v", err)
	}
	if err := b.AddNode("B", nodeB); err != nil {
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
		t.Fatalf("Compile 실패: %v", err)
	}

	result, err := compiled.Invoke(context.Background(), graph.State{}, config.RunConfig{})
	if err != nil {
		t.Fatalf("Invoke 실패: %v", err)
	}

	// items 필드: 리듀서로 누적 병합
	items, ok := result["items"]
	if !ok {
		t.Fatal("result에 items 키가 없습니다")
	}
	itemsSlice, ok := items.([]any)
	if !ok {
		t.Fatalf("items 타입 오류: got %T", items)
	}
	if len(itemsSlice) != 2 {
		t.Fatalf("items 길이 오류: want 2, got %d", len(itemsSlice))
	}
	if itemsSlice[0] != "a" || itemsSlice[1] != "b" {
		t.Fatalf("items 값 오류: want [a b], got %v", itemsSlice)
	}

	// label 필드: last-write-wins (B의 값)
	label, ok := result["label"]
	if !ok {
		t.Fatal("result에 label 키가 없습니다")
	}
	if label != "from_B" {
		t.Fatalf("label 값 오류: want from_B, got %v", label)
	}
}

// TestInvoke_지원하지않는반환타입_에러 는 노드가 지원하지 않는 타입을 반환하면
// Invoke가 error를 반환함을 검증한다.
func TestInvoke_지원하지않는반환타입_에러(t *testing.T) {
	schema := graph.StateSchema{}
	b := graph.NewStateGraph(schema)

	// 지원하지 않는 타입(int)을 반환하는 stub 노드
	badNode := func(ctx context.Context, st graph.State) (any, error) {
		return 42, nil // int는 지원 타입이 아님
	}

	if err := b.AddNode("bad", badNode); err != nil {
		t.Fatalf("AddNode 실패: %v", err)
	}
	if err := b.SetEntryPoint("bad"); err != nil {
		t.Fatalf("SetEntryPoint 실패: %v", err)
	}

	compiled, err := b.Compile()
	if err != nil {
		t.Fatalf("Compile 실패: %v", err)
	}

	_, err = compiled.Invoke(context.Background(), graph.State{}, config.RunConfig{})
	if err == nil {
		t.Fatal("Invoke가 성공했습니다(기대: 지원하지 않는 반환 타입 error)")
	}
}

// TestInvoke_nil반환_정상종료 는 노드가 nil을 반환하면 상태 변경 없이 정상 진행됨을 검증한다.
func TestInvoke_nil반환_정상종료(t *testing.T) {
	schema := graph.StateSchema{}
	b := graph.NewStateGraph(schema)

	// nil을 반환하는 노드
	nilNode := func(ctx context.Context, st graph.State) (any, error) {
		return nil, nil
	}

	if err := b.AddNode("A", nilNode); err != nil {
		t.Fatalf("AddNode 실패: %v", err)
	}
	if err := b.SetEntryPoint("A"); err != nil {
		t.Fatalf("SetEntryPoint 실패: %v", err)
	}

	compiled, err := b.Compile()
	if err != nil {
		t.Fatalf("Compile 실패: %v", err)
	}

	initialState := graph.State{"x": 1}
	result, err := compiled.Invoke(context.Background(), initialState, config.RunConfig{})
	if err != nil {
		t.Fatalf("Invoke 실패: %v", err)
	}

	// nil 반환이므로 초기 상태 그대로
	if result["x"] != 1 {
		t.Fatalf("state 값 오류: want 1, got %v", result["x"])
	}
}

// TestInvoke_노드에러전파 는 노드가 error를 반환하면 Invoke가 해당 error를 래핑해 반환함을 검증한다.
func TestInvoke_노드에러전파(t *testing.T) {
	schema := graph.StateSchema{}
	b := graph.NewStateGraph(schema)

	sentinel := errors.New("노드 실행 실패")
	errNode := func(ctx context.Context, st graph.State) (any, error) {
		return nil, sentinel
	}

	if err := b.AddNode("A", errNode); err != nil {
		t.Fatalf("AddNode 실패: %v", err)
	}
	if err := b.SetEntryPoint("A"); err != nil {
		t.Fatalf("SetEntryPoint 실패: %v", err)
	}

	compiled, err := b.Compile()
	if err != nil {
		t.Fatalf("Compile 실패: %v", err)
	}

	_, err = compiled.Invoke(context.Background(), graph.State{}, config.RunConfig{})
	if err == nil {
		t.Fatal("Invoke가 성공했습니다(기대: 노드 error 전파)")
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("에러 타입 오류: want sentinel error, got %v", err)
	}
}

// TestInvoke_순환그래프_maxSteps초과에러 는 자기 자신으로 되돌아가는(self-loop) 순환 그래프를
// Invoke하면 maxSteps 초과 error가 반환됨을 검증한다(§5.10, ANALYSIS §2.1).
//
// 그래프 구성: loop → loop (자기 자신으로의 정적 엣지, 종료 없음)
// WithMaxSteps(3)을 지정해 3스텝 이내에 차단됨을 확인한다.
func TestInvoke_순환그래프_maxSteps초과에러(t *testing.T) {
	schema := graph.StateSchema{}
	b := graph.NewStateGraph(schema)

	// 상태를 변경하지 않고 항상 정상 반환하는 순환 노드
	loopNode := func(ctx context.Context, st graph.State) (any, error) {
		return graph.StateUpdate{"count": 1}, nil
	}

	if err := b.AddNode("loop", loopNode); err != nil {
		t.Fatalf("AddNode 실패: %v", err)
	}
	// loop → loop 자기 자신으로의 정적 엣지
	if err := b.AddEdge("loop", "loop"); err != nil {
		t.Fatalf("AddEdge 실패: %v", err)
	}
	if err := b.SetEntryPoint("loop"); err != nil {
		t.Fatalf("SetEntryPoint 실패: %v", err)
	}

	// maxSteps를 3으로 제한해 빠르게 차단 확인
	compiled, err := b.Compile(graph.WithMaxSteps(3))
	if err != nil {
		t.Fatalf("Compile 실패: %v", err)
	}

	_, err = compiled.Invoke(context.Background(), graph.State{}, config.RunConfig{})
	if err == nil {
		t.Fatal("Invoke가 성공했습니다(기대: maxSteps 초과 error)")
	}
}

// TestInvoke_한도내그래프_정상종료 는 maxSteps 한도 안에서 완료되는 그래프가
// 정상적으로 종료함을 검증한다(§5.10, ANALYSIS §2.1).
//
// 그래프 구성: A → B (2스텝, 정적 엣지로 종료)
// WithMaxSteps(5)를 지정해도 2스텝 만에 정상 종료됨을 확인한다.
func TestInvoke_한도내그래프_정상종료(t *testing.T) {
	schema := graph.StateSchema{}
	b := graph.NewStateGraph(schema)

	nodeA := func(ctx context.Context, st graph.State) (any, error) {
		return graph.StateUpdate{"step": "a"}, nil
	}
	nodeB := func(ctx context.Context, st graph.State) (any, error) {
		return graph.StateUpdate{"step": "b"}, nil
	}

	if err := b.AddNode("A", nodeA); err != nil {
		t.Fatalf("AddNode A 실패: %v", err)
	}
	if err := b.AddNode("B", nodeB); err != nil {
		t.Fatalf("AddNode B 실패: %v", err)
	}
	if err := b.AddEdge("A", "B"); err != nil {
		t.Fatalf("AddEdge 실패: %v", err)
	}
	if err := b.SetEntryPoint("A"); err != nil {
		t.Fatalf("SetEntryPoint 실패: %v", err)
	}

	// maxSteps를 5로 지정하되 2스텝 그래프이므로 정상 종료되어야 한다
	compiled, err := b.Compile(graph.WithMaxSteps(5))
	if err != nil {
		t.Fatalf("Compile 실패: %v", err)
	}

	result, err := compiled.Invoke(context.Background(), graph.State{}, config.RunConfig{})
	if err != nil {
		t.Fatalf("Invoke 실패: %v", err)
	}

	if result["step"] != "b" {
		t.Fatalf("step 값 오류: want b, got %v", result["step"])
	}
}

// TestInvoke_초기상태병합 는 input으로 전달된 초기 상태가 노드 실행 후 update와 병합됨을 검증한다.
func TestInvoke_초기상태병합(t *testing.T) {
	schema := graph.StateSchema{}
	b := graph.NewStateGraph(schema)

	// x 필드를 "updated"로 덮어쓰고 y는 건드리지 않는 노드
	nodeA := func(ctx context.Context, st graph.State) (any, error) {
		return graph.StateUpdate{"x": "updated"}, nil
	}

	if err := b.AddNode("A", nodeA); err != nil {
		t.Fatalf("AddNode 실패: %v", err)
	}
	if err := b.SetEntryPoint("A"); err != nil {
		t.Fatalf("SetEntryPoint 실패: %v", err)
	}

	compiled, err := b.Compile()
	if err != nil {
		t.Fatalf("Compile 실패: %v", err)
	}

	// 초기 상태에 x=original, y=preserved 포함
	initial := graph.State{"x": "original", "y": "preserved"}
	result, err := compiled.Invoke(context.Background(), initial, config.RunConfig{})
	if err != nil {
		t.Fatalf("Invoke 실패: %v", err)
	}

	if result["x"] != "updated" {
		t.Fatalf("x 값 오류: want updated, got %v", result["x"])
	}
	if result["y"] != "preserved" {
		t.Fatalf("y 값 오류: want preserved, got %v", result["y"])
	}
}
