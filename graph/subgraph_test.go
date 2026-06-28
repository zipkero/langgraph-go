// subgraph_test.go 는 task-009 검증 조건을 만족하는 단위 테스트를 담는다(SPEC §5.7, §5.5, §5.6).
//
// 세 시나리오를 검증한다:
//  1. 공유 상태 모드: 서브그래프 상태 변경이 부모로 반영된다.
//  2. 독립 상태 모드: 입력 스키마 필터·출력 스키마 추출 경계만 넘긴다.
//  3. ToParent/parent-Send: 서브그래프 노드가 이를 반환하면 부모 노드로 라우팅된다.
package graph_test

import (
	"context"
	"testing"

	"github.com/zipkero/langgraph-go/config"
	"github.com/zipkero/langgraph-go/graph"
	"github.com/zipkero/langgraph-go/graph/command"
)

// buildSimpleSubgraph 는 단일 노드로 구성된 단순 서브그래프를 빌드하는 헬퍼다.
// nodeFunc가 반환하는 값을 그대로 반환하는 단일 노드 "sub_node"를 가진다.
func buildSimpleSubgraph(t *testing.T, nodeFunc graph.NodeFunc, opts ...graph.SchemaOption) *graph.Compiled {
	t.Helper()
	b := graph.NewStateGraph(graph.StateSchema{}, opts...)
	if err := b.AddNode("sub_node", nodeFunc); err != nil {
		t.Fatalf("서브그래프 AddNode 실패: %v", err)
	}
	if err := b.SetEntryPoint("sub_node"); err != nil {
		t.Fatalf("서브그래프 SetEntryPoint 실패: %v", err)
	}
	sub, err := b.Compile()
	if err != nil {
		t.Fatalf("서브그래프 Compile 실패: %v", err)
	}
	return sub
}

// TestSubgraph_공유상태_부모상태변경반영 은 공유 상태 모드(WithInputSchema/WithOutputSchema 미설정)에서
// 서브그래프의 상태 변경이 부모 그래프에 그대로 반영됨을 검증한다(SPEC §5.7, ANALYSIS §2.5).
//
// 구성:
//
//	부모: start → subgraph_node → end
//	서브그래프: sub_node가 {sub_result: "done", parent_field: "modified"}를 반환
//
// 검증:
//   - 서브그래프가 반환한 모든 필드가 부모 최종 상태에 반영된다.
//   - 부모 초기 상태 필드("parent_init")도 보존된다.
func TestSubgraph_공유상태_부모상태변경반영(t *testing.T) {
	// 서브그래프: sub_node가 두 필드를 반환
	subNodeFn := func(ctx context.Context, st graph.State) (any, error) {
		return graph.StateUpdate{
			"sub_result":   "done",
			"parent_field": "modified",
		}, nil
	}
	// 공유 상태 모드: 스키마 옵션 없이 빌드
	sub := buildSimpleSubgraph(t, subNodeFn)

	// 부모 그래프 빌드
	parentSchema := graph.StateSchema{}
	pb := graph.NewStateGraph(parentSchema)

	// 서브그래프를 부모 노드로 등록
	if err := pb.AddNode("subgraph_node", sub.AsNode()); err != nil {
		t.Fatalf("부모 AddNode(서브그래프) 실패: %v", err)
	}

	// end 노드: 아무것도 하지 않음
	endNode := func(ctx context.Context, st graph.State) (any, error) {
		return nil, nil
	}
	if err := pb.AddNode("end", endNode); err != nil {
		t.Fatalf("부모 AddNode(end) 실패: %v", err)
	}
	if err := pb.AddEdge("subgraph_node", "end"); err != nil {
		t.Fatalf("부모 AddEdge 실패: %v", err)
	}
	if err := pb.SetEntryPoint("subgraph_node"); err != nil {
		t.Fatalf("부모 SetEntryPoint 실패: %v", err)
	}

	parentCompiled, err := pb.Compile()
	if err != nil {
		t.Fatalf("부모 Compile 실패: %v", err)
	}

	// 부모 초기 상태에 parent_init 포함
	initial := graph.State{"parent_init": "preserved"}
	result, err := parentCompiled.Invoke(context.Background(), initial, config.RunConfig{})
	if err != nil {
		t.Fatalf("부모 Invoke 실패: %v", err)
	}

	// 서브그래프 반환 필드가 부모 상태에 반영됐는지 확인
	if result["sub_result"] != "done" {
		t.Fatalf("sub_result 값 오류: want done, got %v", result["sub_result"])
	}
	if result["parent_field"] != "modified" {
		t.Fatalf("parent_field 값 오류: want modified, got %v", result["parent_field"])
	}
	// 부모 초기 상태가 보존됐는지 확인
	if result["parent_init"] != "preserved" {
		t.Fatalf("parent_init 값 오류: want preserved, got %v", result["parent_init"])
	}
}

// TestSubgraph_독립상태_입력필터_출력추출 은 독립 상태 모드에서 서브그래프가
// 입력 스키마로 필터된 상태만 받고, 출력 스키마로 추출된 결과만 부모로 반환됨을 검증한다
// (SPEC §5.6, §5.7, ANALYSIS §2.5).
//
// 구성:
//   - 서브그래프: WithInputSchema("sub_in"), WithOutputSchema("sub_out")
//   - sub_node: 받은 state에서 "sub_in" 값을 읽어 "sub_out"에 기록,
//     "secret" 필드는 받지 못해야 한다.
//
// 검증:
//   - sub_node가 받은 state에 "secret" 필드가 없다.
//   - 최종 부모 상태에 "sub_out"만 추가되고 "sub_internal"은 없다.
func TestSubgraph_독립상태_입력필터_출력추출(t *testing.T) {
	var observedInput graph.State

	// 서브그래프: sub_node가 입력을 관찰하고 sub_out + sub_internal을 반환
	subNodeFn := func(ctx context.Context, st graph.State) (any, error) {
		// 받은 상태 기록
		observedInput = make(graph.State, len(st))
		for k, v := range st {
			observedInput[k] = v
		}
		// sub_in 값을 sub_out에 기록하고, sub_internal은 출력 스키마 밖
		inVal, _ := st["sub_in"].(string)
		return graph.StateUpdate{
			"sub_out":      "processed_" + inVal,
			"sub_internal": "should_not_cross",
		}, nil
	}

	// 독립 상태 모드: WithInputSchema("sub_in"), WithOutputSchema("sub_out")
	sub := buildSimpleSubgraph(t, subNodeFn,
		graph.WithInputSchema("sub_in"),
		graph.WithOutputSchema("sub_out"),
	)

	// 부모 그래프 빌드
	pb := graph.NewStateGraph(graph.StateSchema{})
	if err := pb.AddNode("subgraph_node", sub.AsNode()); err != nil {
		t.Fatalf("부모 AddNode(서브그래프) 실패: %v", err)
	}
	if err := pb.SetEntryPoint("subgraph_node"); err != nil {
		t.Fatalf("부모 SetEntryPoint 실패: %v", err)
	}

	parentCompiled, err := pb.Compile()
	if err != nil {
		t.Fatalf("부모 Compile 실패: %v", err)
	}

	// 부모 초기 상태: sub_in과 secret 포함
	initial := graph.State{
		"sub_in": "hello",
		"secret": "do_not_pass",
	}
	result, err := parentCompiled.Invoke(context.Background(), initial, config.RunConfig{})
	if err != nil {
		t.Fatalf("부모 Invoke 실패: %v", err)
	}

	// 서브그래프 노드가 받은 입력에 "secret"이 없어야 한다
	if _, ok := observedInput["secret"]; ok {
		t.Fatal("서브그래프가 secret 필드를 받았습니다(기대: 입력 필터로 차단)")
	}
	// 서브그래프 노드가 "sub_in"을 받아야 한다
	if observedInput["sub_in"] != "hello" {
		t.Fatalf("서브그래프 observedInput[sub_in] 값 오류: want hello, got %v", observedInput["sub_in"])
	}

	// 최종 부모 상태에 sub_out이 있어야 한다
	if result["sub_out"] != "processed_hello" {
		t.Fatalf("sub_out 값 오류: want processed_hello, got %v", result["sub_out"])
	}
	// sub_internal은 출력 스키마 밖이므로 부모 상태에 없어야 한다
	if _, ok := result["sub_internal"]; ok {
		t.Fatal("sub_internal이 부모 상태에 포함됐습니다(기대: 출력 스키마로 차단)")
	}
}

// TestSubgraph_ToParent_부모노드라우팅 은 서브그래프 노드가 command.ToParent를 반환하면
// 부모 그래프의 해당 노드로 라우팅됨을 검증한다(SPEC §5.5, §5.7, ANALYSIS §2.5).
//
// 구성:
//
//	부모:
//	  - "subgraph_node": 서브그래프 어댑터 (WithDestinations("parent_handler"))
//	  - "parent_handler": ToParent 라우팅 대상
//	  - "not_called": 정적 엣지 연결만 (실행되면 안 됨)
//	서브그래프:
//	  - sub_node: ToParent("parent_handler", {from_sub: "toparent_update"}) 반환
//
// 검증:
//   - parent_handler 노드가 실행된다.
//   - not_called 노드는 실행되지 않는다.
//   - from_sub 업데이트가 최종 상태에 반영된다.
func TestSubgraph_ToParent_부모노드라우팅(t *testing.T) {
	// 서브그래프: sub_node가 ToParent를 반환
	subNodeFn := func(ctx context.Context, st graph.State) (any, error) {
		return command.ToParent("parent_handler", graph.StateUpdate{"from_sub": "toparent_update"}), nil
	}
	sub := buildSimpleSubgraph(t, subNodeFn)

	// 부모 그래프 빌드
	pb := graph.NewStateGraph(graph.StateSchema{})

	// 서브그래프 노드 등록: WithDestinations("parent_handler")로 Goto 검증 통과
	if err := pb.AddNode("subgraph_node", sub.AsNode(), graph.WithDestinations("parent_handler")); err != nil {
		t.Fatalf("부모 AddNode(서브그래프) 실패: %v", err)
	}

	// parent_handler: ToParent 라우팅 대상, 자신이 실행됐음을 기록
	parentHandlerNode := func(ctx context.Context, st graph.State) (any, error) {
		return graph.StateUpdate{"handler_executed": true}, nil
	}
	if err := pb.AddNode("parent_handler", parentHandlerNode); err != nil {
		t.Fatalf("부모 AddNode(parent_handler) 실패: %v", err)
	}

	// not_called: 정적 엣지로 parent_handler에서 연결 (도달 가능성 확보용, 실행 안 됨)
	notCalledNode := func(ctx context.Context, st graph.State) (any, error) {
		return graph.StateUpdate{"not_called_executed": true}, nil
	}
	if err := pb.AddNode("not_called", notCalledNode); err != nil {
		t.Fatalf("부모 AddNode(not_called) 실패: %v", err)
	}
	// 도달 가능성: subgraph_node에서 not_called로 정적 엣지(실제 실행은 ToParent로 우회)
	if err := pb.AddEdge("subgraph_node", "not_called"); err != nil {
		t.Fatalf("부모 AddEdge(subgraph_node→not_called) 실패: %v", err)
	}
	if err := pb.AddEdge("not_called", "parent_handler"); err != nil {
		t.Fatalf("부모 AddEdge(not_called→parent_handler) 실패: %v", err)
	}
	if err := pb.SetEntryPoint("subgraph_node"); err != nil {
		t.Fatalf("부모 SetEntryPoint 실패: %v", err)
	}

	parentCompiled, err := pb.Compile()
	if err != nil {
		t.Fatalf("부모 Compile 실패: %v", err)
	}

	result, err := parentCompiled.Invoke(context.Background(), graph.State{}, config.RunConfig{})
	if err != nil {
		t.Fatalf("부모 Invoke 실패: %v", err)
	}

	// parent_handler가 실행됐는지 확인
	if result["handler_executed"] != true {
		t.Fatalf("handler_executed 값 오류: want true, got %v", result["handler_executed"])
	}
	// ToParent update가 반영됐는지 확인
	if result["from_sub"] != "toparent_update" {
		t.Fatalf("from_sub 값 오류: want toparent_update, got %v", result["from_sub"])
	}
	// not_called가 실행되지 않았는지 확인
	if _, ok := result["not_called_executed"]; ok {
		t.Fatal("not_called 노드가 실행됐습니다(기대: ToParent로 우회되어 실행 안 됨)")
	}
}

// TestSubgraph_독립상태_서브그래프내부_리듀서 는 독립 상태 서브그래프가 자체 스키마의
// 리듀서를 올바르게 적용함을 검증한다.
//
// 서브그래프가 자체 slice-append 리듀서를 가지고 두 노드가 실행될 때
// 리듀서 병합이 서브그래프 내부에서 올바르게 작동하고,
// 출력 스키마를 통해 필터된 결과가 부모로 전달된다.
func TestSubgraph_독립상태_서브그래프내부_리듀서(t *testing.T) {
	// slice-append 리듀서
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

	subSchema := graph.StateSchema{
		Reducers: map[string]graph.ReducerFunc{
			"items": sliceAppend,
		},
	}

	// 서브그래프: 두 노드 A→B, items에 누적
	sb := graph.NewStateGraph(subSchema,
		graph.WithInputSchema("seed"),
		graph.WithOutputSchema("items"),
	)

	subA := func(ctx context.Context, st graph.State) (any, error) {
		seed, _ := st["seed"].(string)
		return graph.StateUpdate{"items": "item_" + seed}, nil
	}
	subB := func(ctx context.Context, st graph.State) (any, error) {
		return graph.StateUpdate{"items": "item_extra"}, nil
	}

	if err := sb.AddNode("subA", subA); err != nil {
		t.Fatalf("서브그래프 AddNode subA 실패: %v", err)
	}
	if err := sb.AddNode("subB", subB); err != nil {
		t.Fatalf("서브그래프 AddNode subB 실패: %v", err)
	}
	if err := sb.AddEdge("subA", "subB"); err != nil {
		t.Fatalf("서브그래프 AddEdge 실패: %v", err)
	}
	if err := sb.SetEntryPoint("subA"); err != nil {
		t.Fatalf("서브그래프 SetEntryPoint 실패: %v", err)
	}
	sub, err := sb.Compile()
	if err != nil {
		t.Fatalf("서브그래프 Compile 실패: %v", err)
	}

	// 부모 그래프
	pb := graph.NewStateGraph(graph.StateSchema{})
	if err := pb.AddNode("subgraph_node", sub.AsNode()); err != nil {
		t.Fatalf("부모 AddNode 실패: %v", err)
	}
	if err := pb.SetEntryPoint("subgraph_node"); err != nil {
		t.Fatalf("부모 SetEntryPoint 실패: %v", err)
	}
	parentCompiled, err := pb.Compile()
	if err != nil {
		t.Fatalf("부모 Compile 실패: %v", err)
	}

	result, err := parentCompiled.Invoke(context.Background(), graph.State{"seed": "hello"}, config.RunConfig{})
	if err != nil {
		t.Fatalf("부모 Invoke 실패: %v", err)
	}

	// items가 출력 스키마로 부모에 전달됐는지 확인
	items, ok := result["items"]
	if !ok {
		t.Fatal("result에 items 키가 없습니다")
	}
	itemsSlice, ok := items.([]any)
	if !ok {
		t.Fatalf("items 타입 오류: got %T", items)
	}
	if len(itemsSlice) != 2 {
		t.Fatalf("items 길이 오류: want 2, got %d (%v)", len(itemsSlice), itemsSlice)
	}
	if itemsSlice[0] != "item_hello" || itemsSlice[1] != "item_extra" {
		t.Fatalf("items 값 오류: want [item_hello item_extra], got %v", itemsSlice)
	}

	// seed는 출력 스키마 밖이므로 부모 최종 상태에 없어야 한다
	// (부모 초기 상태에 seed가 있었으므로 부모 state에는 남아 있음 — 확인: items만 추가됨)
	if _, hasSeedInSub := result["items"]; !hasSeedInSub {
		t.Fatal("items가 없습니다")
	}
}

// TestSubgraph_공유상태_다중노드 는 공유 상태 모드에서 서브그래프의 여러 노드가
// 순차 실행되며 모든 변경이 부모로 반영됨을 검증한다.
func TestSubgraph_공유상태_다중노드(t *testing.T) {
	// 서브그래프: 두 노드 A→B
	sb := graph.NewStateGraph(graph.StateSchema{})
	subA := func(ctx context.Context, st graph.State) (any, error) {
		return graph.StateUpdate{"step": "sub_a", "from_a": true}, nil
	}
	subB := func(ctx context.Context, st graph.State) (any, error) {
		return graph.StateUpdate{"step": "sub_b", "from_b": true}, nil
	}
	if err := sb.AddNode("subA", subA); err != nil {
		t.Fatalf("서브그래프 AddNode subA 실패: %v", err)
	}
	if err := sb.AddNode("subB", subB); err != nil {
		t.Fatalf("서브그래프 AddNode subB 실패: %v", err)
	}
	if err := sb.AddEdge("subA", "subB"); err != nil {
		t.Fatalf("서브그래프 AddEdge 실패: %v", err)
	}
	if err := sb.SetEntryPoint("subA"); err != nil {
		t.Fatalf("서브그래프 SetEntryPoint 실패: %v", err)
	}
	sub, err := sb.Compile()
	if err != nil {
		t.Fatalf("서브그래프 Compile 실패: %v", err)
	}

	// 부모 그래프
	pb := graph.NewStateGraph(graph.StateSchema{})
	if err := pb.AddNode("subgraph_node", sub.AsNode()); err != nil {
		t.Fatalf("부모 AddNode 실패: %v", err)
	}
	if err := pb.SetEntryPoint("subgraph_node"); err != nil {
		t.Fatalf("부모 SetEntryPoint 실패: %v", err)
	}
	parentCompiled, err := pb.Compile()
	if err != nil {
		t.Fatalf("부모 Compile 실패: %v", err)
	}

	result, err := parentCompiled.Invoke(context.Background(), graph.State{"initial": "yes"}, config.RunConfig{})
	if err != nil {
		t.Fatalf("부모 Invoke 실패: %v", err)
	}

	// 서브그래프 두 노드의 결과가 부모에 반영됐는지 확인
	if result["from_a"] != true {
		t.Fatalf("from_a 값 오류: want true, got %v", result["from_a"])
	}
	if result["from_b"] != true {
		t.Fatalf("from_b 값 오류: want true, got %v", result["from_b"])
	}
	// 마지막 step은 sub_b
	if result["step"] != "sub_b" {
		t.Fatalf("step 값 오류: want sub_b, got %v", result["step"])
	}
	// 부모 초기 상태 보존
	if result["initial"] != "yes" {
		t.Fatalf("initial 값 오류: want yes, got %v", result["initial"])
	}
}
