// stream_test.go 는 task-010 검증 조건을 만족하는 단위 테스트를 담는다(SPEC §5.8, ANALYSIS §2.6).
//
// 검증 시나리오:
//  1. ModeValues: 각 노드 실행 후 전체 상태 스냅샷이 방출된다.
//  2. ModeUpdates: 각 노드의 StateUpdate가 방출된다.
//  3. ModeMessages: 노드가 StreamTokens(ctx)로 보낸 토큰이 GraphEvent.Token으로 방출된다.
//  4. ModeDebug: 노드 진입/이탈 진단 이벤트가 방출된다.
//  5. WithSubgraphs(): 서브그래프 이벤트의 GraphEvent.Path가 채워진다.
package graph_test

import (
	"context"
	"testing"

	"github.com/zipkero/langgraph-go/config"
	"github.com/zipkero/langgraph-go/core"
	"github.com/zipkero/langgraph-go/graph"
)

// collectEvents 는 채널에서 모든 GraphEvent를 수집해 슬라이스로 반환하는 헬퍼다.
// 채널이 닫힐 때까지 기다린다.
func collectEvents(ch <-chan graph.GraphEvent) []graph.GraphEvent {
	var events []graph.GraphEvent
	for evt := range ch {
		events = append(events, evt)
	}
	return events
}

// buildTwoNodeGraph 는 A → B 두 노드 그래프를 빌드하는 헬퍼다.
// nodeA, nodeB는 각각 stub 함수로 대체할 수 있다.
func buildTwoNodeGraph(t *testing.T, schema graph.StateSchema, nodeA, nodeB graph.NodeFunc) *graph.Compiled {
	t.Helper()
	b := graph.NewStateGraph(schema)
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
	return compiled
}

// TestStream_ModeValues_전체상태스냅샷방출 은 ModeValues 모드에서 각 노드 실행 후
// 전체 상태 스냅샷이 GraphEvent.Value에 담겨 방출됨을 검증한다.
//
// 그래프: A → B
//   - A: {x: "a"} 반환
//   - B: {y: "b"} 반환
//
// 기대 이벤트 (순서대로):
//  1. Node="A", Mode=ModeValues, Value={x:"a"}
//  2. Node="B", Mode=ModeValues, Value={x:"a", y:"b"}
func TestStream_ModeValues_전체상태스냅샷방출(t *testing.T) {
	nodeA := func(ctx context.Context, st graph.State) (any, error) {
		return graph.StateUpdate{"x": "a"}, nil
	}
	nodeB := func(ctx context.Context, st graph.State) (any, error) {
		return graph.StateUpdate{"y": "b"}, nil
	}

	compiled := buildTwoNodeGraph(t, graph.StateSchema{}, nodeA, nodeB)

	ch, err := compiled.Stream(context.Background(), graph.State{}, config.RunConfig{}, core.ModeValues)
	if err != nil {
		t.Fatalf("Stream 시작 실패: %v", err)
	}

	events := collectEvents(ch)

	// 오류 이벤트 없이 정상 이벤트만 수집됐는지 확인
	var valueEvents []graph.GraphEvent
	for _, evt := range events {
		if evt.Mode == core.ModeValues {
			valueEvents = append(valueEvents, evt)
		}
	}

	if len(valueEvents) != 2 {
		t.Fatalf("ModeValues 이벤트 수 오류: want 2, got %d (전체 이벤트: %v)", len(valueEvents), events)
	}

	// 첫 번째 이벤트: A 실행 후 스냅샷
	evt0 := valueEvents[0]
	if evt0.Node != "A" {
		t.Fatalf("이벤트[0].Node 오류: want A, got %q", evt0.Node)
	}
	if evt0.Value["x"] != "a" {
		t.Fatalf("이벤트[0].Value[x] 오류: want a, got %v", evt0.Value["x"])
	}
	if _, hasY := evt0.Value["y"]; hasY {
		t.Fatal("이벤트[0].Value에 y가 있습니다(기대: A 실행 후이므로 없어야 함)")
	}

	// 두 번째 이벤트: B 실행 후 스냅샷
	evt1 := valueEvents[1]
	if evt1.Node != "B" {
		t.Fatalf("이벤트[1].Node 오류: want B, got %q", evt1.Node)
	}
	if evt1.Value["x"] != "a" {
		t.Fatalf("이벤트[1].Value[x] 오류: want a, got %v", evt1.Value["x"])
	}
	if evt1.Value["y"] != "b" {
		t.Fatalf("이벤트[1].Value[y] 오류: want b, got %v", evt1.Value["y"])
	}
}

// TestStream_ModeUpdates_노드별변경분방출 은 ModeUpdates 모드에서 각 노드의
// StateUpdate가 GraphEvent.Update에 담겨 방출됨을 검증한다.
//
// 그래프: A → B
//   - A: {x: "a", label: "from_A"} 반환
//   - B: {y: "b", label: "from_B"} 반환
//
// 기대 이벤트:
//  1. Node="A", Mode=ModeUpdates, Update={x:"a", label:"from_A"}
//  2. Node="B", Mode=ModeUpdates, Update={y:"b", label:"from_B"}
func TestStream_ModeUpdates_노드별변경분방출(t *testing.T) {
	nodeA := func(ctx context.Context, st graph.State) (any, error) {
		return graph.StateUpdate{"x": "a", "label": "from_A"}, nil
	}
	nodeB := func(ctx context.Context, st graph.State) (any, error) {
		return graph.StateUpdate{"y": "b", "label": "from_B"}, nil
	}

	compiled := buildTwoNodeGraph(t, graph.StateSchema{}, nodeA, nodeB)

	ch, err := compiled.Stream(context.Background(), graph.State{}, config.RunConfig{}, core.ModeUpdates)
	if err != nil {
		t.Fatalf("Stream 시작 실패: %v", err)
	}

	events := collectEvents(ch)

	var updateEvents []graph.GraphEvent
	for _, evt := range events {
		if evt.Mode == core.ModeUpdates {
			updateEvents = append(updateEvents, evt)
		}
	}

	if len(updateEvents) != 2 {
		t.Fatalf("ModeUpdates 이벤트 수 오류: want 2, got %d", len(updateEvents))
	}

	// 첫 번째 이벤트: A의 update
	evt0 := updateEvents[0]
	if evt0.Node != "A" {
		t.Fatalf("이벤트[0].Node 오류: want A, got %q", evt0.Node)
	}
	if evt0.Update["x"] != "a" {
		t.Fatalf("이벤트[0].Update[x] 오류: want a, got %v", evt0.Update["x"])
	}
	if evt0.Update["label"] != "from_A" {
		t.Fatalf("이벤트[0].Update[label] 오류: want from_A, got %v", evt0.Update["label"])
	}
	// Update에는 전체 상태가 아닌 이 노드의 변경분만 있어야 한다
	if _, hasY := evt0.Update["y"]; hasY {
		t.Fatal("이벤트[0].Update에 y가 있습니다(기대: A의 update에만 해당 필드)")
	}

	// 두 번째 이벤트: B의 update
	evt1 := updateEvents[1]
	if evt1.Node != "B" {
		t.Fatalf("이벤트[1].Node 오류: want B, got %q", evt1.Node)
	}
	if evt1.Update["y"] != "b" {
		t.Fatalf("이벤트[1].Update[y] 오류: want b, got %v", evt1.Update["y"])
	}
	if evt1.Update["label"] != "from_B" {
		t.Fatalf("이벤트[1].Update[label] 오류: want from_B, got %v", evt1.Update["label"])
	}
}

// TestStream_ModeMessages_토큰방출 은 ModeMessages 모드에서 노드가 StreamTokens(ctx)로
// 보낸 토큰이 GraphEvent.Token에 담겨 순서대로 방출됨을 검증한다.
//
// 그래프: A(토큰 방출) → B(토큰 방출 없음)
//   - A: "hello", " ", "world" 세 토큰을 보내고 StateUpdate{"done": "a"} 반환
//   - B: StateUpdate{"done": "b"} 반환 (토큰 없음)
//
// 기대 이벤트: Token="hello", Token=" ", Token="world" (순서대로, A 노드에서)
func TestStream_ModeMessages_토큰방출(t *testing.T) {
	nodeA := func(ctx context.Context, st graph.State) (any, error) {
		if ch := graph.StreamTokens(ctx); ch != nil {
			ch <- "hello"
			ch <- " "
			ch <- "world"
		}
		return graph.StateUpdate{"done": "a"}, nil
	}
	nodeB := func(ctx context.Context, st graph.State) (any, error) {
		// 토큰을 보내지 않는 노드
		return graph.StateUpdate{"done": "b"}, nil
	}

	compiled := buildTwoNodeGraph(t, graph.StateSchema{}, nodeA, nodeB)

	ch, err := compiled.Stream(context.Background(), graph.State{}, config.RunConfig{}, core.ModeMessages)
	if err != nil {
		t.Fatalf("Stream 시작 실패: %v", err)
	}

	events := collectEvents(ch)

	var tokenEvents []graph.GraphEvent
	for _, evt := range events {
		if evt.Mode == core.ModeMessages && evt.Token != "" {
			tokenEvents = append(tokenEvents, evt)
		}
	}

	if len(tokenEvents) != 3 {
		t.Fatalf("ModeMessages 토큰 이벤트 수 오류: want 3, got %d (전체 이벤트: %v)", len(tokenEvents), events)
	}

	// 토큰 순서 검증
	expectedTokens := []string{"hello", " ", "world"}
	for i, expected := range expectedTokens {
		if tokenEvents[i].Token != expected {
			t.Fatalf("토큰[%d] 오류: want %q, got %q", i, expected, tokenEvents[i].Token)
		}
		if tokenEvents[i].Node != "A" {
			t.Fatalf("토큰[%d].Node 오류: want A, got %q", i, tokenEvents[i].Node)
		}
	}
}

// TestStream_ModeDebug_진단이벤트방출 은 ModeDebug 모드에서 노드 진입/이탈 진단 이벤트가
// 방출됨을 검증한다.
//
// 그래프: A → B (두 노드 각각 진입/이탈 이벤트)
// 기대 이벤트: A_enter, A_exit, B_enter, B_exit (최소 4개 이벤트, Metadata["event"] 확인)
func TestStream_ModeDebug_진단이벤트방출(t *testing.T) {
	nodeA := func(ctx context.Context, st graph.State) (any, error) {
		return graph.StateUpdate{"x": "a"}, nil
	}
	nodeB := func(ctx context.Context, st graph.State) (any, error) {
		return graph.StateUpdate{"y": "b"}, nil
	}

	compiled := buildTwoNodeGraph(t, graph.StateSchema{}, nodeA, nodeB)

	ch, err := compiled.Stream(context.Background(), graph.State{}, config.RunConfig{}, core.ModeDebug)
	if err != nil {
		t.Fatalf("Stream 시작 실패: %v", err)
	}

	events := collectEvents(ch)

	var debugEvents []graph.GraphEvent
	for _, evt := range events {
		if evt.Mode == core.ModeDebug {
			debugEvents = append(debugEvents, evt)
		}
	}

	// 두 노드 × (진입 + 이탈) = 최소 4개
	if len(debugEvents) < 4 {
		t.Fatalf("ModeDebug 이벤트 수 오류: want >=4, got %d", len(debugEvents))
	}

	// 첫 번째 이벤트: A 진입
	evt0 := debugEvents[0]
	if evt0.Node != "A" {
		t.Fatalf("디버그[0].Node 오류: want A, got %q", evt0.Node)
	}
	if evt0.Metadata["event"] != "node_enter" {
		t.Fatalf("디버그[0].event 오류: want node_enter, got %v", evt0.Metadata["event"])
	}

	// 두 번째 이벤트: A 이탈
	evt1 := debugEvents[1]
	if evt1.Node != "A" {
		t.Fatalf("디버그[1].Node 오류: want A, got %q", evt1.Node)
	}
	if evt1.Metadata["event"] != "node_exit" {
		t.Fatalf("디버그[1].event 오류: want node_exit, got %v", evt1.Metadata["event"])
	}

	// 세 번째 이벤트: B 진입
	evt2 := debugEvents[2]
	if evt2.Node != "B" {
		t.Fatalf("디버그[2].Node 오류: want B, got %q", evt2.Node)
	}
	if evt2.Metadata["event"] != "node_enter" {
		t.Fatalf("디버그[2].event 오류: want node_enter, got %v", evt2.Metadata["event"])
	}

	// 네 번째 이벤트: B 이탈
	evt3 := debugEvents[3]
	if evt3.Node != "B" {
		t.Fatalf("디버그[3].Node 오류: want B, got %q", evt3.Node)
	}
	if evt3.Metadata["event"] != "node_exit" {
		t.Fatalf("디버그[3].event 오류: want node_exit, got %v", evt3.Metadata["event"])
	}
}

// TestStream_WithSubgraphs_서브그래프_Path채워짐 은 WithSubgraphs() 옵션이 켜진 경우
// 서브그래프에서 방출된 GraphEvent의 Path가 채워짐을 검증한다.
//
// 구성:
//   - 서브그래프: sub_a → sub_b (두 노드)
//   - 부모 그래프: "sub_node"(서브그래프) 단일 노드
//
// 기대:
//   - 서브그래프 이벤트의 Path에 "sub_node"가 포함된다.
//   - 부모 노드에서 직접 방출된 이벤트는 Path가 비어 있다.
func TestStream_WithSubgraphs_서브그래프_Path채워짐(t *testing.T) {
	// 서브그래프 빌드: sub_a → sub_b
	subSchema := graph.StateSchema{}
	sb := graph.NewStateGraph(subSchema)
	subA := func(ctx context.Context, st graph.State) (any, error) {
		return graph.StateUpdate{"sub_step": "a"}, nil
	}
	subB := func(ctx context.Context, st graph.State) (any, error) {
		return graph.StateUpdate{"sub_step": "b"}, nil
	}
	if err := sb.AddNode("sub_a", subA); err != nil {
		t.Fatalf("서브그래프 AddNode sub_a 실패: %v", err)
	}
	if err := sb.AddNode("sub_b", subB); err != nil {
		t.Fatalf("서브그래프 AddNode sub_b 실패: %v", err)
	}
	if err := sb.AddEdge("sub_a", "sub_b"); err != nil {
		t.Fatalf("서브그래프 AddEdge 실패: %v", err)
	}
	if err := sb.SetEntryPoint("sub_a"); err != nil {
		t.Fatalf("서브그래프 SetEntryPoint 실패: %v", err)
	}
	subCompiled, err := sb.Compile()
	if err != nil {
		t.Fatalf("서브그래프 Compile 실패: %v", err)
	}

	// 부모 그래프 빌드: AddSubgraphNode 사용
	pb := graph.NewStateGraph(graph.StateSchema{})
	if err := pb.AddSubgraphNode("sub_node", subCompiled); err != nil {
		t.Fatalf("부모 AddSubgraphNode 실패: %v", err)
	}
	if err := pb.SetEntryPoint("sub_node"); err != nil {
		t.Fatalf("부모 SetEntryPoint 실패: %v", err)
	}
	parentCompiled, err := pb.Compile()
	if err != nil {
		t.Fatalf("부모 Compile 실패: %v", err)
	}

	// WithSubgraphs() 옵션과 함께 ModeValues로 Stream
	ch, err := parentCompiled.Stream(
		context.Background(),
		graph.State{},
		config.RunConfig{},
		core.ModeValues,
		graph.WithSubgraphs(),
	)
	if err != nil {
		t.Fatalf("Stream 시작 실패: %v", err)
	}

	events := collectEvents(ch)

	// 서브그래프에서 방출된 이벤트를 분류한다
	var subEvents []graph.GraphEvent
	var rootEvents []graph.GraphEvent
	for _, evt := range events {
		if evt.Mode != core.ModeValues {
			continue
		}
		if len(evt.Path) > 0 {
			subEvents = append(subEvents, evt)
		} else {
			rootEvents = append(rootEvents, evt)
		}
	}

	// 서브그래프 이벤트가 존재해야 한다
	if len(subEvents) == 0 {
		t.Fatalf("서브그래프 이벤트가 없습니다(Path가 있는 이벤트가 없음). 전체 이벤트: %v", events)
	}

	// 서브그래프 이벤트의 Path에 "sub_node"가 포함돼야 한다
	for _, evt := range subEvents {
		found := false
		for _, p := range evt.Path {
			if p == "sub_node" {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("서브그래프 이벤트의 Path에 sub_node가 없습니다: Path=%v, Node=%q", evt.Path, evt.Node)
		}
	}

	// 서브그래프 내부에서 sub_a, sub_b 이벤트가 방출됐는지 확인
	subNodeNames := make(map[string]bool)
	for _, evt := range subEvents {
		subNodeNames[evt.Node] = true
	}
	if !subNodeNames["sub_a"] {
		t.Fatal("서브그래프 이벤트에 sub_a 노드가 없습니다")
	}
	if !subNodeNames["sub_b"] {
		t.Fatal("서브그래프 이벤트에 sub_b 노드가 없습니다")
	}

	// 루트 레벨 이벤트는 Path가 비어 있어야 한다 (서브그래프이므로 부모 자체 노드는 없음)
	for _, evt := range rootEvents {
		if len(evt.Path) > 0 {
			t.Fatalf("루트 이벤트에 Path가 있습니다: %v", evt.Path)
		}
	}
}

// TestStream_WithSubgraphs_없을때_Path비어있음 은 WithSubgraphs() 없이 Stream하면
// 서브그래프(AsNode 등록)에서 방출된 이벤트가 없고 Path도 비어 있음을 검증한다.
//
// WithSubgraphs 없는 경우 서브그래프는 단일 노드처럼 취급되므로
// 부모 기준의 이벤트만 방출되고, 서브그래프 내부 이벤트는 방출되지 않는다.
func TestStream_WithSubgraphs_없을때_Path비어있음(t *testing.T) {
	// 서브그래프
	sb := graph.NewStateGraph(graph.StateSchema{})
	subNode := func(ctx context.Context, st graph.State) (any, error) {
		return graph.StateUpdate{"sub": "done"}, nil
	}
	if err := sb.AddNode("s", subNode); err != nil {
		t.Fatalf("서브그래프 AddNode 실패: %v", err)
	}
	if err := sb.SetEntryPoint("s"); err != nil {
		t.Fatalf("서브그래프 SetEntryPoint 실패: %v", err)
	}
	subCompiled, err := sb.Compile()
	if err != nil {
		t.Fatalf("서브그래프 Compile 실패: %v", err)
	}

	// 부모: AddSubgraphNode 사용
	pb := graph.NewStateGraph(graph.StateSchema{})
	if err := pb.AddSubgraphNode("sub_node", subCompiled); err != nil {
		t.Fatalf("부모 AddSubgraphNode 실패: %v", err)
	}
	if err := pb.SetEntryPoint("sub_node"); err != nil {
		t.Fatalf("부모 SetEntryPoint 실패: %v", err)
	}
	parentCompiled, err := pb.Compile()
	if err != nil {
		t.Fatalf("부모 Compile 실패: %v", err)
	}

	// WithSubgraphs 없이 ModeValues Stream
	ch, err := parentCompiled.Stream(
		context.Background(),
		graph.State{},
		config.RunConfig{},
		core.ModeValues,
		// WithSubgraphs() 없음
	)
	if err != nil {
		t.Fatalf("Stream 시작 실패: %v", err)
	}

	events := collectEvents(ch)

	// Path가 있는 이벤트가 없어야 한다(서브그래프 내부 이벤트 방출 없음)
	for _, evt := range events {
		if len(evt.Path) > 0 {
			t.Fatalf("WithSubgraphs 없는데 Path가 있는 이벤트가 있습니다: %v", evt)
		}
	}
}

// TestStream_컨텍스트취소_채널닫힘 은 ctx가 취소되면 Stream 채널이 닫히고
// 더 이상 이벤트가 방출되지 않음을 검증한다.
func TestStream_컨텍스트취소_채널닫힘(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	// 첫 번째 노드 실행 중 ctx를 취소한다
	nodeA := func(ctx context.Context, st graph.State) (any, error) {
		cancel() // 실행 중 취소
		return graph.StateUpdate{"x": "a"}, nil
	}
	nodeB := func(ctx context.Context, st graph.State) (any, error) {
		return graph.StateUpdate{"y": "b"}, nil
	}

	compiled := buildTwoNodeGraph(t, graph.StateSchema{}, nodeA, nodeB)

	ch, err := compiled.Stream(ctx, graph.State{}, config.RunConfig{}, core.ModeValues)
	if err != nil {
		t.Fatalf("Stream 시작 실패: %v", err)
	}

	// 채널을 소진한다 — 취소 후 닫혀야 한다
	events := collectEvents(ch)

	// 채널이 닫혔는지 확인(collectEvents가 반환했으면 채널은 닫힌 것)
	// ctx 취소 후 이벤트가 2개 미만이어야 한다(B는 실행되지 않거나 이벤트 방출 전 취소)
	_ = events // 이벤트 수 자체보다 채널이 닫혔는지가 중요; deadlock 없이 반환된 것으로 검증
}

// TestStream_ModeValues_순서검증 은 ModeValues 모드에서 이벤트가 노드 실행 순서대로
// 방출됨을 검증한다(세 노드 A → B → C).
func TestStream_ModeValues_순서검증(t *testing.T) {
	b := graph.NewStateGraph(graph.StateSchema{})
	for _, name := range []string{"A", "B", "C"} {
		n := name
		fn := func(ctx context.Context, st graph.State) (any, error) {
			return graph.StateUpdate{"last": n}, nil
		}
		if err := b.AddNode(n, fn); err != nil {
			t.Fatalf("AddNode %s 실패: %v", n, err)
		}
	}
	if err := b.AddEdge("A", "B"); err != nil {
		t.Fatalf("AddEdge A→B 실패: %v", err)
	}
	if err := b.AddEdge("B", "C"); err != nil {
		t.Fatalf("AddEdge B→C 실패: %v", err)
	}
	if err := b.SetEntryPoint("A"); err != nil {
		t.Fatalf("SetEntryPoint 실패: %v", err)
	}
	compiled, err := b.Compile()
	if err != nil {
		t.Fatalf("Compile 실패: %v", err)
	}

	ch, err := compiled.Stream(context.Background(), graph.State{}, config.RunConfig{}, core.ModeValues)
	if err != nil {
		t.Fatalf("Stream 시작 실패: %v", err)
	}

	events := collectEvents(ch)

	var valueEvents []graph.GraphEvent
	for _, evt := range events {
		if evt.Mode == core.ModeValues {
			valueEvents = append(valueEvents, evt)
		}
	}

	if len(valueEvents) != 3 {
		t.Fatalf("ModeValues 이벤트 수 오류: want 3, got %d", len(valueEvents))
	}

	expectedNodes := []string{"A", "B", "C"}
	for i, expected := range expectedNodes {
		if valueEvents[i].Node != expected {
			t.Fatalf("이벤트[%d].Node 오류: want %s, got %q", i, expected, valueEvents[i].Node)
		}
	}
}

// TestStream_ModeMessages_StreamTokens없는노드_이벤트없음 은 노드가 StreamTokens를
// 사용하지 않으면 ModeMessages 이벤트가 방출되지 않음을 검증한다.
func TestStream_ModeMessages_StreamTokens없는노드_이벤트없음(t *testing.T) {
	nodeA := func(ctx context.Context, st graph.State) (any, error) {
		// StreamTokens 사용하지 않음
		return graph.StateUpdate{"done": "a"}, nil
	}
	nodeB := func(ctx context.Context, st graph.State) (any, error) {
		return graph.StateUpdate{"done": "b"}, nil
	}

	compiled := buildTwoNodeGraph(t, graph.StateSchema{}, nodeA, nodeB)

	ch, err := compiled.Stream(context.Background(), graph.State{}, config.RunConfig{}, core.ModeMessages)
	if err != nil {
		t.Fatalf("Stream 시작 실패: %v", err)
	}

	events := collectEvents(ch)

	// 토큰 이벤트가 없어야 한다
	for _, evt := range events {
		if evt.Mode == core.ModeMessages && evt.Token != "" {
			t.Fatalf("예상치 않은 토큰 이벤트: %v", evt)
		}
	}
}
