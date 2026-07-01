// command_test.go 는 task-007 검증 조건을 만족하는 단위 테스트를 담는다.
// Goto/End/Fanout 각각을 반환하는 stub 노드 그래프를 Invoke해
// 이동·종료·분기별 상태를 검증한다(§5.5, ANALYSIS §2.2, §2.3, §5 D5).
package graph_test

import (
	"context"
	"testing"

	"github.com/zipkero/langgraph-go/config"
	"github.com/zipkero/langgraph-go/graph"
	"github.com/zipkero/langgraph-go/graph/command"
)

// TestInvoke_Goto_대상노드이동_update적용 은 노드가 command.Goto를 반환하면
// 지정 대상 노드로 이동하며 update가 적용됨을 검증한다.
//
// 그래프 구성:
//
//	start → [Goto("target", {"step": "goto_applied"})] → target → (터미널)
//
// 검증 포인트:
//   - start가 Goto("target", ...)를 반환하면 target이 실행됨
//   - Goto의 update(step="goto_applied")가 상태에 반영됨
//   - target 노드도 실행되어 result["visited"] == "target"이 됨
func TestInvoke_Goto_대상노드이동_update적용(t *testing.T) {
	schema := graph.StateSchema{}
	b := graph.NewStateGraph(schema)

	// start 노드: Goto("target")으로 이동 + update 적용
	startNode := func(ctx context.Context, st graph.State) (any, error) {
		return command.Goto("target", graph.StateUpdate{"step": "goto_applied"}), nil
	}
	// target 노드: 자신이 실행됐음을 표시
	targetNode := func(ctx context.Context, st graph.State) (any, error) {
		return graph.StateUpdate{"visited": "target"}, nil
	}

	if err := b.AddNode("start", startNode, graph.WithDestinations("target")); err != nil {
		t.Fatalf("AddNode start 실패: %v", err)
	}
	if err := b.AddNode("target", targetNode); err != nil {
		t.Fatalf("AddNode target 실패: %v", err)
	}
	// start에서 target으로의 조건엣지/정적엣지 없음 — WithDestinations 선언만으로
	// 도달성 검사를 통과하고, 실제 이동은 Goto가 담당한다.
	if err := b.SetEntryPoint("start"); err != nil {
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

	// Goto update가 적용됐는지 확인
	if result["step"] != "goto_applied" {
		t.Fatalf("step 값 오류: want goto_applied, got %v", result["step"])
	}
	// target 노드가 실행됐는지 확인
	if result["visited"] != "target" {
		t.Fatalf("visited 값 오류: want target, got %v", result["visited"])
	}
}

// TestInvoke_End_즉시종료 는 노드가 command.End를 반환하면 그래프가 즉시 종료됨을 검증한다.
//
// 그래프 구성:
//
//	start → [End({"finished": true})] → (종료, B는 실행되지 않음)
//
// 검증 포인트:
//   - start가 End를 반환하면 뒤의 B 노드가 실행되지 않음
//   - End의 update(finished=true)가 최종 상태에 반영됨
func TestInvoke_End_즉시종료(t *testing.T) {
	schema := graph.StateSchema{}
	b := graph.NewStateGraph(schema)

	// start 노드: End 반환 (finished=true update 포함)
	startNode := func(ctx context.Context, st graph.State) (any, error) {
		return command.End(graph.StateUpdate{"finished": true}), nil
	}
	// B 노드: 실행되면 안 됨
	nodeB := func(ctx context.Context, st graph.State) (any, error) {
		return graph.StateUpdate{"visited_b": true}, nil
	}

	if err := b.AddNode("start", startNode); err != nil {
		t.Fatalf("AddNode start 실패: %v", err)
	}
	if err := b.AddNode("B", nodeB); err != nil {
		t.Fatalf("AddNode B 실패: %v", err)
	}
	if err := b.AddEdge("start", "B"); err != nil {
		t.Fatalf("AddEdge 실패: %v", err)
	}
	if err := b.SetEntryPoint("start"); err != nil {
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

	// End update가 적용됐는지 확인
	if result["finished"] != true {
		t.Fatalf("finished 값 오류: want true, got %v", result["finished"])
	}
	// B 노드가 실행되지 않았는지 확인
	if _, ok := result["visited_b"]; ok {
		t.Fatalf("B 노드가 실행됐습니다(기대: End 이후 실행 없음)")
	}
}

// TestInvoke_End_update없음_즉시종료 는 nil update를 가진 End가 상태 변경 없이 즉시 종료함을 검증한다.
func TestInvoke_End_update없음_즉시종료(t *testing.T) {
	schema := graph.StateSchema{}
	b := graph.NewStateGraph(schema)

	startNode := func(ctx context.Context, st graph.State) (any, error) {
		return command.End(nil), nil
	}
	nodeB := func(ctx context.Context, st graph.State) (any, error) {
		return graph.StateUpdate{"visited": "B"}, nil
	}

	if err := b.AddNode("start", startNode); err != nil {
		t.Fatalf("AddNode start 실패: %v", err)
	}
	if err := b.AddNode("B", nodeB); err != nil {
		t.Fatalf("AddNode B 실패: %v", err)
	}
	if err := b.AddEdge("start", "B"); err != nil {
		t.Fatalf("AddEdge 실패: %v", err)
	}
	if err := b.SetEntryPoint("start"); err != nil {
		t.Fatalf("SetEntryPoint 실패: %v", err)
	}

	compiled, err := b.Compile()
	if err != nil {
		t.Fatalf("Compile 실패: %v", err)
	}

	// 초기 상태
	initial := graph.State{"x": "initial"}
	result, err := compiled.Invoke(context.Background(), initial, config.RunConfig{})
	if err != nil {
		t.Fatalf("Invoke 실패: %v", err)
	}

	// 초기 상태가 보존됐는지 확인
	if result["x"] != "initial" {
		t.Fatalf("x 값 오류: want initial, got %v", result["x"])
	}
	// B 노드가 실행되지 않았는지 확인
	if _, ok := result["visited"]; ok {
		t.Fatalf("B 노드가 실행됐습니다(기대: End 이후 실행 없음)")
	}
}

// TestInvoke_Fanout_각분기_독립상태실행 은 Fanout이 각 Send target을
// 해당 Send.State로 독립 실행함을 검증한다.
//
// 그래프 구성:
//
//	dispatcher → [Fanout([Send("branchA", {role: "A"}), Send("branchB", {role: "B"})])]
//	  - branchA: state["role"] == "A"인 상태를 받아 visited_a=true 설정
//	  - branchB: state["role"] == "B"인 상태를 받아 visited_b=true 설정
//
// 검증 포인트:
//   - branchA가 Send.State({role:"A"})를 받아 실행됨
//   - branchB가 Send.State({role:"B"})를 받아 실행됨
//   - 두 분기 결과가 최종 상태에 반영됨
func TestInvoke_Fanout_각분기_독립상태실행(t *testing.T) {
	schema := graph.StateSchema{}
	b := graph.NewStateGraph(schema)

	// dispatcher 노드: 두 분기로 Fanout
	dispatcherNode := func(ctx context.Context, st graph.State) (any, error) {
		return command.Fanout([]command.Send{
			command.NewSend("branchA", graph.State{"role": "A"}),
			command.NewSend("branchB", graph.State{"role": "B"}),
		}), nil
	}

	// branchA 노드: role=="A"인 상태로 실행됐는지 확인
	branchANode := func(ctx context.Context, st graph.State) (any, error) {
		role, _ := st["role"].(string)
		return graph.StateUpdate{"visited_a": true, "role_a": role}, nil
	}

	// branchB 노드: role=="B"인 상태로 실행됐는지 확인
	branchBNode := func(ctx context.Context, st graph.State) (any, error) {
		role, _ := st["role"].(string)
		return graph.StateUpdate{"visited_b": true, "role_b": role}, nil
	}

	if err := b.AddNode("dispatcher", dispatcherNode); err != nil {
		t.Fatalf("AddNode dispatcher 실패: %v", err)
	}
	if err := b.AddNode("branchA", branchANode); err != nil {
		t.Fatalf("AddNode branchA 실패: %v", err)
	}
	if err := b.AddNode("branchB", branchBNode); err != nil {
		t.Fatalf("AddNode branchB 실패: %v", err)
	}
	// Compile 도달 가능성 검사를 위해 dispatcher → branchA, dispatcher → branchB 조건 엣지 추가
	router := func(ctx context.Context, st graph.State) string { return "a" }
	if err := b.AddConditionalEdges("dispatcher", router, map[string]string{
		"a": "branchA",
		"b": "branchB",
	}); err != nil {
		t.Fatalf("AddConditionalEdges 실패: %v", err)
	}
	if err := b.SetEntryPoint("dispatcher"); err != nil {
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

	// branchA가 실행됐는지 확인
	if result["visited_a"] != true {
		t.Fatalf("visited_a 값 오류: want true, got %v", result["visited_a"])
	}
	// branchA가 role=="A"인 상태로 실행됐는지 확인
	if result["role_a"] != "A" {
		t.Fatalf("role_a 값 오류: want A, got %v", result["role_a"])
	}
	// branchB가 실행됐는지 확인
	if result["visited_b"] != true {
		t.Fatalf("visited_b 값 오류: want true, got %v", result["visited_b"])
	}
	// branchB가 role=="B"인 상태로 실행됐는지 확인
	if result["role_b"] != "B" {
		t.Fatalf("role_b 값 오류: want B, got %v", result["role_b"])
	}
}

// TestInvoke_Goto_WithDestinations_미선언_허용 은 WithDestinations를 선언하지 않은 노드가
// Goto를 사용할 때 destinations 제한 없이 이동함을 검증한다.
func TestInvoke_Goto_WithDestinations_미선언_허용(t *testing.T) {
	schema := graph.StateSchema{}
	b := graph.NewStateGraph(schema)

	// start 노드: WithDestinations 없이 Goto("anywhere") 반환
	startNode := func(ctx context.Context, st graph.State) (any, error) {
		return command.Goto("anywhere", nil), nil
	}
	anywhereNode := func(ctx context.Context, st graph.State) (any, error) {
		return graph.StateUpdate{"reached": "anywhere"}, nil
	}

	if err := b.AddNode("start", startNode); err != nil {
		t.Fatalf("AddNode start 실패: %v", err)
	}
	if err := b.AddNode("anywhere", anywhereNode); err != nil {
		t.Fatalf("AddNode anywhere 실패: %v", err)
	}
	// Compile 도달 가능성을 위해 정적 엣지 추가
	if err := b.AddEdge("start", "anywhere"); err != nil {
		t.Fatalf("AddEdge 실패: %v", err)
	}
	if err := b.SetEntryPoint("start"); err != nil {
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

	if result["reached"] != "anywhere" {
		t.Fatalf("reached 값 오류: want anywhere, got %v", result["reached"])
	}
}

// TestInvoke_Goto_WithDestinations_선언외대상_에러 는 WithDestinations에 선언되지 않은
// 대상으로 Goto하면 런타임 error가 반환됨을 검증한다(§2.3 D5).
func TestInvoke_Goto_WithDestinations_선언외대상_에러(t *testing.T) {
	schema := graph.StateSchema{}
	b := graph.NewStateGraph(schema)

	// start 노드: WithDestinations("allowed")만 선언했지만 "forbidden"으로 Goto
	startNode := func(ctx context.Context, st graph.State) (any, error) {
		return command.Goto("forbidden", nil), nil
	}
	allowedNode := func(ctx context.Context, st graph.State) (any, error) {
		return nil, nil
	}
	forbiddenNode := func(ctx context.Context, st graph.State) (any, error) {
		return nil, nil
	}

	// WithDestinations("allowed")만 선언
	if err := b.AddNode("start", startNode, graph.WithDestinations("allowed")); err != nil {
		t.Fatalf("AddNode start 실패: %v", err)
	}
	if err := b.AddNode("allowed", allowedNode); err != nil {
		t.Fatalf("AddNode allowed 실패: %v", err)
	}
	if err := b.AddNode("forbidden", forbiddenNode); err != nil {
		t.Fatalf("AddNode forbidden 실패: %v", err)
	}
	// Compile 도달 가능성을 위해 엣지 추가
	if err := b.AddEdge("start", "allowed"); err != nil {
		t.Fatalf("AddEdge start→allowed 실패: %v", err)
	}
	if err := b.AddEdge("start", "forbidden"); err != nil {
		t.Fatalf("AddEdge start→forbidden 실패: %v", err)
	}
	if err := b.SetEntryPoint("start"); err != nil {
		t.Fatalf("SetEntryPoint 실패: %v", err)
	}

	compiled, err := b.Compile()
	if err != nil {
		t.Fatalf("Compile 실패: %v", err)
	}

	_, err = compiled.Invoke(context.Background(), graph.State{}, config.RunConfig{})
	if err == nil {
		t.Fatal("Invoke가 성공했습니다(기대: WithDestinations 미선언 대상 error)")
	}
}

// TestInvoke_Goto_WithDestinations_선언대상_정상 은 WithDestinations에 선언된 대상으로
// Goto하면 정상 이동함을 검증한다.
func TestInvoke_Goto_WithDestinations_선언대상_정상(t *testing.T) {
	schema := graph.StateSchema{}
	b := graph.NewStateGraph(schema)

	startNode := func(ctx context.Context, st graph.State) (any, error) {
		// WithDestinations("allowed")에 포함된 대상으로 이동
		return command.Goto("allowed", graph.StateUpdate{"moved": true}), nil
	}
	allowedNode := func(ctx context.Context, st graph.State) (any, error) {
		return graph.StateUpdate{"executed": "allowed"}, nil
	}

	if err := b.AddNode("start", startNode, graph.WithDestinations("allowed")); err != nil {
		t.Fatalf("AddNode start 실패: %v", err)
	}
	if err := b.AddNode("allowed", allowedNode); err != nil {
		t.Fatalf("AddNode allowed 실패: %v", err)
	}
	if err := b.AddEdge("start", "allowed"); err != nil {
		t.Fatalf("AddEdge 실패: %v", err)
	}
	if err := b.SetEntryPoint("start"); err != nil {
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

	if result["moved"] != true {
		t.Fatalf("moved 값 오류: want true, got %v", result["moved"])
	}
	if result["executed"] != "allowed" {
		t.Fatalf("executed 값 오류: want allowed, got %v", result["executed"])
	}
}

// TestInvoke_Fanout_Send상태_nil이면현재상태전달 은 Send.State가 nil이면
// 현재 그래프 상태가 분기에 전달됨을 검증한다.
func TestInvoke_Fanout_Send상태_nil이면현재상태전달(t *testing.T) {
	schema := graph.StateSchema{}
	b := graph.NewStateGraph(schema)

	dispatcherNode := func(ctx context.Context, st graph.State) (any, error) {
		// Send.State == nil → 현재 상태가 전달됨
		return command.Fanout([]command.Send{
			{Target: "branchA", State: nil, Graph: command.TargetCurrent},
		}), nil
	}

	branchANode := func(ctx context.Context, st graph.State) (any, error) {
		// 현재 상태에서 "base" 필드를 읽어 확인
		base, _ := st["base"].(string)
		return graph.StateUpdate{"received_base": base}, nil
	}

	if err := b.AddNode("dispatcher", dispatcherNode); err != nil {
		t.Fatalf("AddNode dispatcher 실패: %v", err)
	}
	if err := b.AddNode("branchA", branchANode); err != nil {
		t.Fatalf("AddNode branchA 실패: %v", err)
	}
	router := func(ctx context.Context, st graph.State) string { return "a" }
	if err := b.AddConditionalEdges("dispatcher", router, map[string]string{"a": "branchA"}); err != nil {
		t.Fatalf("AddConditionalEdges 실패: %v", err)
	}
	if err := b.SetEntryPoint("dispatcher"); err != nil {
		t.Fatalf("SetEntryPoint 실패: %v", err)
	}

	compiled, err := b.Compile()
	if err != nil {
		t.Fatalf("Compile 실패: %v", err)
	}

	// 초기 상태에 base 필드 포함
	result, err := compiled.Invoke(context.Background(), graph.State{"base": "inherited"}, config.RunConfig{})
	if err != nil {
		t.Fatalf("Invoke 실패: %v", err)
	}

	if result["received_base"] != "inherited" {
		t.Fatalf("received_base 값 오류: want inherited, got %v", result["received_base"])
	}
}
