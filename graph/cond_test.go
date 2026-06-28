// cond_test.go 는 task-006 검증 조건을 만족하는 단위 테스트를 담는다.
// 조건 엣지 라우팅과 조건 진입점을 stub 노드로 검증한다(D11).
package graph_test

import (
	"context"
	"testing"

	"github.com/zipkero/langgraph-go/config"
	"github.com/zipkero/langgraph-go/graph"
)

// TestInvoke_조건엣지_분기선택 은 라우터 반환 키에 따라 서로 다른 분기 노드가 실행됨을 검증한다.
//
// 그래프 구성:
//
//	start → 조건 엣지(라우터 반환 키: "go_b" or "go_c") → B 또는 C
//
// 검증 포인트:
//   - 라우터가 "go_b"를 반환하면 B가 실행되고 result["visited"] == "B"
//   - 라우터가 "go_c"를 반환하면 C가 실행되고 result["visited"] == "C"
func TestInvoke_조건엣지_분기선택(t *testing.T) {
	tests := []struct {
		name        string
		routerKey   string
		wantVisited string
	}{
		{"라우터_go_b_B분기실행", "go_b", "B"},
		{"라우터_go_c_C분기실행", "go_c", "C"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			schema := graph.StateSchema{}
			b := graph.NewStateGraph(schema)

			// start 노드: 상태 변경 없이 라우팅만 위임
			startNode := func(ctx context.Context, st graph.State) (any, error) {
				return graph.StateUpdate{"from": "start"}, nil
			}
			// B 노드: 자신이 실행됐음을 표시
			nodeB := func(ctx context.Context, st graph.State) (any, error) {
				return graph.StateUpdate{"visited": "B"}, nil
			}
			// C 노드: 자신이 실행됐음을 표시
			nodeC := func(ctx context.Context, st graph.State) (any, error) {
				return graph.StateUpdate{"visited": "C"}, nil
			}

			if err := b.AddNode("start", startNode); err != nil {
				t.Fatalf("AddNode start 실패: %v", err)
			}
			if err := b.AddNode("B", nodeB); err != nil {
				t.Fatalf("AddNode B 실패: %v", err)
			}
			if err := b.AddNode("C", nodeC); err != nil {
				t.Fatalf("AddNode C 실패: %v", err)
			}

			// 라우터 반환 키를 tc.routerKey로 고정
			routerKey := tc.routerKey
			router := func(ctx context.Context, st graph.State) string {
				return routerKey
			}
			if err := b.AddConditionalEdges("start", router, map[string]string{
				"go_b": "B",
				"go_c": "C",
			}); err != nil {
				t.Fatalf("AddConditionalEdges 실패: %v", err)
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

			visited, ok := result["visited"]
			if !ok {
				t.Fatal("result에 visited 키가 없습니다")
			}
			if visited != tc.wantVisited {
				t.Fatalf("visited 값 오류: want %q, got %v", tc.wantVisited, visited)
			}
		})
	}
}

// TestInvoke_조건엣지_다단계경로 는 조건 엣지로 선택된 분기 노드에서 정적 엣지로 계속 진행함을 검증한다.
//
// 그래프 구성:
//
//	start → 조건 엣지("go_b" → B) → B → C (정적 엣지)
//
// 검증 포인트:
//   - 실행 순서가 start → B → C이고 최종 result["path"] == "start→B→C"
func TestInvoke_조건엣지_다단계경로(t *testing.T) {
	schema := graph.StateSchema{
		Reducers: map[string]graph.ReducerFunc{
			// path 필드: 문자열 연결 리듀서
			"path": func(cur, upd any) any {
				s, _ := cur.(string)
				a, _ := upd.(string)
				if s == "" {
					return a
				}
				return s + "→" + a
			},
		},
	}
	b := graph.NewStateGraph(schema)

	startNode := func(ctx context.Context, st graph.State) (any, error) {
		return graph.StateUpdate{"path": "start"}, nil
	}
	nodeB := func(ctx context.Context, st graph.State) (any, error) {
		return graph.StateUpdate{"path": "B"}, nil
	}
	nodeC := func(ctx context.Context, st graph.State) (any, error) {
		return graph.StateUpdate{"path": "C"}, nil
	}

	if err := b.AddNode("start", startNode); err != nil {
		t.Fatalf("AddNode start 실패: %v", err)
	}
	if err := b.AddNode("B", nodeB); err != nil {
		t.Fatalf("AddNode B 실패: %v", err)
	}
	if err := b.AddNode("C", nodeC); err != nil {
		t.Fatalf("AddNode C 실패: %v", err)
	}
	if err := b.AddConditionalEdges("start", func(_ context.Context, _ graph.State) string {
		return "go_b"
	}, map[string]string{"go_b": "B"}); err != nil {
		t.Fatalf("AddConditionalEdges 실패: %v", err)
	}
	if err := b.AddEdge("B", "C"); err != nil {
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

	wantPath := "start→B→C"
	if result["path"] != wantPath {
		t.Fatalf("path 값 오류: want %q, got %v", wantPath, result["path"])
	}
}

// TestInvoke_조건진입점_상태에따라다른첫노드 는 조건 진입점이 초기 상태에 따라 다른 첫 노드로 진입함을 검증한다.
//
// 그래프 구성: 조건 진입점(라우터: state["mode"] 값 기반) → A 또는 B
//
// 검증 포인트:
//   - 초기 상태 state["mode"] == "a" 이면 A가 첫 노드로 실행 → result["visited"] == "A"
//   - 초기 상태 state["mode"] == "b" 이면 B가 첫 노드로 실행 → result["visited"] == "B"
func TestInvoke_조건진입점_상태에따라다른첫노드(t *testing.T) {
	tests := []struct {
		name        string
		initMode    string
		wantVisited string
	}{
		{"mode_a_A진입", "a", "A"},
		{"mode_b_B진입", "b", "B"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			schema := graph.StateSchema{}
			b := graph.NewStateGraph(schema)

			nodeA := func(ctx context.Context, st graph.State) (any, error) {
				return graph.StateUpdate{"visited": "A"}, nil
			}
			nodeB := func(ctx context.Context, st graph.State) (any, error) {
				return graph.StateUpdate{"visited": "B"}, nil
			}

			if err := b.AddNode("A", nodeA); err != nil {
				t.Fatalf("AddNode A 실패: %v", err)
			}
			if err := b.AddNode("B", nodeB); err != nil {
				t.Fatalf("AddNode B 실패: %v", err)
			}

			// 라우터: state["mode"] 값에 따라 "go_a" 또는 "go_b" 반환
			router := func(ctx context.Context, st graph.State) string {
				mode, _ := st["mode"].(string)
				if mode == "a" {
					return "go_a"
				}
				return "go_b"
			}
			if err := b.SetConditionalEntryPoint(router, map[string]string{
				"go_a": "A",
				"go_b": "B",
			}); err != nil {
				t.Fatalf("SetConditionalEntryPoint 실패: %v", err)
			}

			compiled, err := b.Compile()
			if err != nil {
				t.Fatalf("Compile 실패: %v", err)
			}

			// 초기 상태에 mode 필드 포함
			initial := graph.State{"mode": tc.initMode}
			result, err := compiled.Invoke(context.Background(), initial, config.RunConfig{})
			if err != nil {
				t.Fatalf("Invoke 실패: %v", err)
			}

			visited, ok := result["visited"]
			if !ok {
				t.Fatal("result에 visited 키가 없습니다")
			}
			if visited != tc.wantVisited {
				t.Fatalf("visited 값 오류: want %q, got %v", tc.wantVisited, visited)
			}
		})
	}
}

// TestInvoke_조건진입점_알수없는키_에러 는 조건 진입점 라우터가 mapping에 없는 키를 반환하면
// Invoke가 error를 반환함을 검증한다.
func TestInvoke_조건진입점_알수없는키_에러(t *testing.T) {
	schema := graph.StateSchema{}
	b := graph.NewStateGraph(schema)

	nodeA := func(ctx context.Context, st graph.State) (any, error) {
		return nil, nil
	}
	if err := b.AddNode("A", nodeA); err != nil {
		t.Fatalf("AddNode A 실패: %v", err)
	}

	// 라우터: 항상 mapping에 없는 키 반환
	router := func(ctx context.Context, st graph.State) string {
		return "unknown_key"
	}
	if err := b.SetConditionalEntryPoint(router, map[string]string{
		"go_a": "A",
	}); err != nil {
		t.Fatalf("SetConditionalEntryPoint 실패: %v", err)
	}

	compiled, err := b.Compile()
	if err != nil {
		t.Fatalf("Compile 실패: %v", err)
	}

	_, err = compiled.Invoke(context.Background(), graph.State{}, config.RunConfig{})
	if err == nil {
		t.Fatal("Invoke가 성공했습니다(기대: 알 수 없는 키 error)")
	}
}
