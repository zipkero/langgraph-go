// checkpoint_test.go 는 task-011 검증 조건을 만족하는 단위 테스트를 담는다.
// InMemorySaver를 결합한 stub 그래프로 thread_id 영속·조회·수동 갱신을 검증한다.
// 네트워크·API 키 없이 수행된다(D11).
package graph_test

import (
	"context"
	"testing"

	"github.com/zipkero/langgraph-go/checkpoint"
	"github.com/zipkero/langgraph-go/config"
	"github.com/zipkero/langgraph-go/graph"
)

// runCfg 는 thread_id가 포함된 RunConfig를 생성하는 헬퍼다.
func runCfg(threadID string) config.RunConfig {
	return config.RunConfig{
		Configurable: map[string]any{
			"thread_id": threadID,
		},
	}
}

// buildCheckpointGraph 는 체크포인터를 결합한 단순 선형 그래프를 반환한다.
// 노드 A → 노드 B 순서로 실행하며, 각 노드는 "count" 필드를 누적한다.
func buildCheckpointGraph(t *testing.T, cp checkpoint.Checkpointer) *graph.Compiled {
	t.Helper()

	// "count" 필드를 정수로 누적하는 리듀서를 등록한다.
	schema := graph.StateSchema{
		Reducers: map[string]graph.ReducerFunc{
			"count": func(cur, upd any) any {
				var a, b int
				if cur != nil {
					if v, ok := cur.(int); ok {
						a = v
					}
				}
				if upd != nil {
					if v, ok := upd.(int); ok {
						b = v
					}
				}
				return a + b
			},
		},
	}

	b := graph.NewStateGraph(schema)

	if err := b.AddNode("A", func(ctx context.Context, st graph.State) (any, error) {
		return graph.StateUpdate{"count": 1, "step": "A"}, nil
	}); err != nil {
		t.Fatalf("AddNode A 실패: %v", err)
	}
	if err := b.AddNode("B", func(ctx context.Context, st graph.State) (any, error) {
		return graph.StateUpdate{"count": 1, "step": "B"}, nil
	}); err != nil {
		t.Fatalf("AddNode B 실패: %v", err)
	}
	if err := b.AddEdge("A", "B"); err != nil {
		t.Fatalf("AddEdge 실패: %v", err)
	}
	if err := b.SetEntryPoint("A"); err != nil {
		t.Fatalf("SetEntryPoint 실패: %v", err)
	}

	compiled, err := b.Compile(graph.WithCheckpointer(cp))
	if err != nil {
		t.Fatalf("Compile 실패: %v", err)
	}
	return compiled
}

// TestCheckpointer_동일ThreadID_두번Invoke_상태영속 는 같은 thread_id로 두 번 Invoke하면
// 두 번째 실행이 첫 번째 상태를 이어받음을 검증한다(SPEC §5.9, D8).
func TestCheckpointer_동일ThreadID_두번Invoke_상태영속(t *testing.T) {
	cp := checkpoint.NewInMemorySaver()
	compiled := buildCheckpointGraph(t, cp)

	cfg := runCfg("thread-001")
	ctx := context.Background()

	// 첫 번째 Invoke: count는 A(+1) + B(+1) = 2
	state1, err := compiled.Invoke(ctx, graph.State{}, cfg)
	if err != nil {
		t.Fatalf("첫 번째 Invoke 실패: %v", err)
	}
	if state1["count"] != 2 {
		t.Errorf("첫 번째 Invoke count = %v, 기대 2", state1["count"])
	}

	// 두 번째 Invoke: 기존 count(2)를 이어받아 A(+1) + B(+1) = 4
	state2, err := compiled.Invoke(ctx, graph.State{}, cfg)
	if err != nil {
		t.Fatalf("두 번째 Invoke 실패: %v", err)
	}
	if state2["count"] != 4 {
		t.Errorf("두 번째 Invoke count = %v, 기대 4 (첫 번째 상태 이어받기)", state2["count"])
	}
}

// TestCheckpointer_다른ThreadID_독립상태 는 다른 thread_id 간 상태가 독립적임을 검증한다.
func TestCheckpointer_다른ThreadID_독립상태(t *testing.T) {
	cp := checkpoint.NewInMemorySaver()
	compiled := buildCheckpointGraph(t, cp)

	ctx := context.Background()

	// thread-A 실행
	stateA, err := compiled.Invoke(ctx, graph.State{}, runCfg("thread-A"))
	if err != nil {
		t.Fatalf("thread-A Invoke 실패: %v", err)
	}

	// thread-B 실행: thread-A의 상태를 이어받지 않아야 한다.
	stateB, err := compiled.Invoke(ctx, graph.State{}, runCfg("thread-B"))
	if err != nil {
		t.Fatalf("thread-B Invoke 실패: %v", err)
	}

	if stateA["count"] != 2 {
		t.Errorf("thread-A count = %v, 기대 2", stateA["count"])
	}
	if stateB["count"] != 2 {
		t.Errorf("thread-B count = %v, 기대 2 (독립 상태)", stateB["count"])
	}
}

// TestGetState_스냅샷반환 는 GetState가 최신 StateSnapshot을 반환함을 검증한다(SPEC §5.9).
func TestGetState_스냅샷반환(t *testing.T) {
	cp := checkpoint.NewInMemorySaver()
	compiled := buildCheckpointGraph(t, cp)

	cfg := runCfg("thread-getstate")
	ctx := context.Background()

	// Invoke 전에는 빈 스냅샷이 반환된다.
	snapBefore, err := compiled.GetState(cfg)
	if err != nil {
		t.Fatalf("Invoke 전 GetState 실패: %v", err)
	}
	if snapBefore.Values != nil && len(snapBefore.Values) != 0 {
		t.Errorf("Invoke 전 Values = %v, 기대 nil 또는 빈 맵", snapBefore.Values)
	}

	// Invoke 실행 후 스냅샷 조회
	if _, err := compiled.Invoke(ctx, graph.State{}, cfg); err != nil {
		t.Fatalf("Invoke 실패: %v", err)
	}

	snap, err := compiled.GetState(cfg)
	if err != nil {
		t.Fatalf("GetState 실패: %v", err)
	}
	if snap.Values == nil {
		t.Fatal("GetState Values = nil, 기대 비nil")
	}
	if snap.Values["count"] != 2 {
		t.Errorf("GetState count = %v, 기대 2", snap.Values["count"])
	}
}

// TestGetStateHistory_이력반환 는 GetStateHistory가 최신 순으로 스냅샷 이력을 반환함을 검증한다(SPEC §5.9).
func TestGetStateHistory_이력반환(t *testing.T) {
	cp := checkpoint.NewInMemorySaver()
	compiled := buildCheckpointGraph(t, cp)

	cfg := runCfg("thread-history")
	ctx := context.Background()

	// Invoke를 두 번 실행해 체크포인트를 쌓는다.
	if _, err := compiled.Invoke(ctx, graph.State{}, cfg); err != nil {
		t.Fatalf("첫 번째 Invoke 실패: %v", err)
	}
	if _, err := compiled.Invoke(ctx, graph.State{}, cfg); err != nil {
		t.Fatalf("두 번째 Invoke 실패: %v", err)
	}

	history, err := compiled.GetStateHistory(cfg)
	if err != nil {
		t.Fatalf("GetStateHistory 실패: %v", err)
	}

	// A→B 2노드 × 2회 실행 = 4개 체크포인트(각 스텝마다 1번 저장)
	if len(history) < 2 {
		t.Errorf("이력 개수 = %d, 기대 2 이상", len(history))
	}

	// 최신 항목(index 0)이 가장 큰 count를 가져야 한다.
	if len(history) > 0 && history[0].Values != nil {
		if count, ok := history[0].Values["count"].(int); ok {
			if count < 2 {
				t.Errorf("최신 스냅샷 count = %d, 기대 2 이상", count)
			}
		}
	}
}

// TestUpdateState_수동갱신반영 는 UpdateState 호출 후 GetState에 갱신이 반영됨을 검증한다(SPEC §5.9).
func TestUpdateState_수동갱신반영(t *testing.T) {
	cp := checkpoint.NewInMemorySaver()
	compiled := buildCheckpointGraph(t, cp)

	cfg := runCfg("thread-update")
	ctx := context.Background()

	// 먼저 Invoke로 초기 상태를 만든다.
	if _, err := compiled.Invoke(ctx, graph.State{}, cfg); err != nil {
		t.Fatalf("Invoke 실패: %v", err)
	}

	// UpdateState로 "memo" 필드를 추가한다.
	if err := compiled.UpdateState(cfg, graph.StateUpdate{"memo": "hello"}); err != nil {
		t.Fatalf("UpdateState 실패: %v", err)
	}

	// GetState로 갱신 확인
	snap, err := compiled.GetState(cfg)
	if err != nil {
		t.Fatalf("UpdateState 후 GetState 실패: %v", err)
	}
	if snap.Values["memo"] != "hello" {
		t.Errorf("갱신 후 memo = %v, 기대 hello", snap.Values["memo"])
	}
	// 기존 상태도 보존돼야 한다.
	if snap.Values["count"] != 2 {
		t.Errorf("갱신 후 count = %v, 기대 2 (기존 상태 보존)", snap.Values["count"])
	}
}

// TestGetState_체크포인터없음_에러 는 체크포인터 없이 GetState를 호출하면 error가 반환됨을 검증한다.
func TestGetState_체크포인터없음_에러(t *testing.T) {
	schema := graph.StateSchema{}
	b := graph.NewStateGraph(schema)
	if err := b.AddNode("A", stubNode(graph.StateUpdate{})); err != nil {
		t.Fatalf("AddNode 실패: %v", err)
	}
	if err := b.SetEntryPoint("A"); err != nil {
		t.Fatalf("SetEntryPoint 실패: %v", err)
	}
	compiled, err := b.Compile()
	if err != nil {
		t.Fatalf("Compile 실패: %v", err)
	}

	_, err = compiled.GetState(runCfg("any"))
	if err == nil {
		t.Error("체크포인터 없이 GetState 호출: error가 반환돼야 합니다")
	}
}

// TestUpdateState_체크포인터없음_에러 는 체크포인터 없이 UpdateState를 호출하면 error가 반환됨을 검증한다.
func TestUpdateState_체크포인터없음_에러(t *testing.T) {
	schema := graph.StateSchema{}
	b := graph.NewStateGraph(schema)
	if err := b.AddNode("A", stubNode(graph.StateUpdate{})); err != nil {
		t.Fatalf("AddNode 실패: %v", err)
	}
	if err := b.SetEntryPoint("A"); err != nil {
		t.Fatalf("SetEntryPoint 실패: %v", err)
	}
	compiled, err := b.Compile()
	if err != nil {
		t.Fatalf("Compile 실패: %v", err)
	}

	err = compiled.UpdateState(runCfg("any"), graph.StateUpdate{"x": 1})
	if err == nil {
		t.Error("체크포인터 없이 UpdateState 호출: error가 반환돼야 합니다")
	}
}

// TestCheckpointer_체크포인터없음_Invoke_정상동작 는 체크포인터가 없어도 Invoke가 정상 동작함을 검증한다.
// (task-004~010 비회귀)
func TestCheckpointer_체크포인터없음_Invoke_정상동작(t *testing.T) {
	schema := graph.StateSchema{}
	b := graph.NewStateGraph(schema)
	if err := b.AddNode("A", stubNode(graph.StateUpdate{"x": 42})); err != nil {
		t.Fatalf("AddNode 실패: %v", err)
	}
	if err := b.SetEntryPoint("A"); err != nil {
		t.Fatalf("SetEntryPoint 실패: %v", err)
	}
	compiled, err := b.Compile()
	if err != nil {
		t.Fatalf("Compile 실패: %v", err)
	}

	state, err := compiled.Invoke(context.Background(), graph.State{}, config.RunConfig{})
	if err != nil {
		t.Fatalf("Invoke 실패: %v", err)
	}
	if state["x"] != 42 {
		t.Errorf("x = %v, 기대 42", state["x"])
	}
}
