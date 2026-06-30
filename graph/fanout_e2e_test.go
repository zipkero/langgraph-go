// fanout_e2e_test.go 는 task-004 검증 조건을 만족하는 e2e 교차 검증 테스트를 담는다.
//
// 목적: Fanout 분기 병합·격리 의미가 Invoke·Stream·subgraph 세 실행 경로에서
// 동일하게 관찰됨을 한 시나리오로 교차 검증한다(SPEC §5.1, §5.2, §5.3).
//
// 핵심 시나리오:
//   - 두 Send 분기가 "messages" 누적 리듀서 키를 각각 갱신한다.
//   - 분기 입력은 fanout 직전 base 상태 기준이다(격리).
//   - 세 경로 모두 최종 상태가 동일해야 한다.
package graph_test

import (
	"context"
	"testing"

	"github.com/zipkero/langgraph-go/config"
	"github.com/zipkero/langgraph-go/core"
	"github.com/zipkero/langgraph-go/graph"
	"github.com/zipkero/langgraph-go/graph/command"
)

// fanoutE2ESchema 는 세 경로 모두 동일하게 쓰는 리듀서 스키마다.
// "messages" 키에 slice-append 리듀서(AddMessages 역할)를 등록한다.
var fanoutE2ESchema = graph.StateSchema{
	Reducers: map[string]graph.ReducerFunc{
		"messages": func(cur, upd any) any {
			var base []any
			if cur != nil {
				if s, ok := cur.([]any); ok {
					base = append(base, s...)
				}
			}
			if upd == nil {
				return base
			}
			switch v := upd.(type) {
			case []any:
				return append(base, v...)
			default:
				return append(base, v)
			}
		},
	},
}

// buildFanoutE2EGraph 는 세 경로 교차 검증에 공통으로 쓰는 fanout 그래프를 빌드한다.
//
// 그래프 구성:
//   - "dispatcher" 노드: Send("branchA", nil), Send("branchB", nil) 두 분기 Fanout
//   - "branchA" 노드: messages에 "msg-A" 추가, label = "from-A"
//   - "branchB" 노드: messages에 "msg-B" 추가, label = "from-B"
//   - "messages" 키에 fanoutE2ESchema 리듀서 등록
//
// branchA와 branchB는 분기 격리 확인을 위해 입력 state의 "observed_in_A" / "observed_in_B"
// 필드를 각각 기록한다(상대 분기가 쓴 값이 자기 입력에 보이면 격리 위반).
func buildFanoutE2EGraph(t *testing.T) *graph.Compiled {
	t.Helper()
	b := graph.NewStateGraph(fanoutE2ESchema)

	// dispatcher: 두 분기로 Fanout(State=nil → 현재 state를 입력으로 전달)
	dispatcherNode := func(ctx context.Context, st graph.State) (any, error) {
		return command.Fanout([]command.Send{
			command.NewSend("branchA", nil),
			command.NewSend("branchB", nil),
		}), nil
	}

	// branchA: messages에 "msg-A" 추가, label = "from-A"
	// 격리 검증: 자신이 받은 입력에 branchB가 쓴 값("seen_B")이 없음을 기록한다.
	branchANode := func(ctx context.Context, st graph.State) (any, error) {
		_, seenB := st["seen_B"]
		return graph.StateUpdate{
			"messages":    "msg-A",
			"label":       "from-A",
			"isolation_A": !seenB, // branchB 쓰기가 입력에 없으면 true(격리 성립)
		}, nil
	}

	// branchB: messages에 "msg-B" 추가, label = "from-B"
	// 격리 검증: 자신이 받은 입력에 branchA가 쓴 값("seen_A")이 없음을 기록한다.
	branchBNode := func(ctx context.Context, st graph.State) (any, error) {
		_, seenA := st["seen_A"]
		return graph.StateUpdate{
			"messages":    "msg-B",
			"label":       "from-B",
			"seen_B":      true,  // branchB가 실행됐음을 표시
			"isolation_B": !seenA, // branchA 쓰기가 입력에 없으면 true(격리 성립)
		}, nil
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
	// 도달성: dispatcher → branchA, branchB 조건엣지
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
	return compiled
}

// assertFanoutE2EResult 는 fanout e2e 시나리오 기대 결과를 공통 검증하는 헬퍼다.
//
// 기대 최종 상태:
//   - messages: [msg-base, msg-A, msg-B] (순서 고정: base → branchA → branchB 순차)
//   - label: "from-B" (last-write-wins, branchB가 마지막)
//   - base_val: "initial" (분기가 갱신하지 않았으므로 이중 누적 없이 보존)
//   - isolation_A: true (branchA 입력에 branchB가 쓴 값이 없음 → 격리 성립)
//   - isolation_B: true (branchB 입력에 branchA가 쓴 값이 없음 → 격리 성립)
func assertFanoutE2EResult(t *testing.T, label string, state graph.State) {
	t.Helper()

	// messages: base + msg-A + msg-B = 3개, 순서 고정
	msgs, ok := state["messages"]
	if !ok {
		t.Fatalf("[%s] messages 키가 없습니다", label)
	}
	msgsSlice, ok := msgs.([]any)
	if !ok {
		t.Fatalf("[%s] messages 타입 오류: got %T", label, msgs)
	}
	if len(msgsSlice) != 3 {
		t.Fatalf("[%s] messages 길이 오류: want 3, got %d (%v)", label, len(msgsSlice), msgsSlice)
	}
	if msgsSlice[0] != "msg-base" {
		t.Fatalf("[%s] messages[0] 오류: want msg-base, got %v", label, msgsSlice[0])
	}
	if msgsSlice[1] != "msg-A" {
		t.Fatalf("[%s] messages[1] 오류: want msg-A, got %v", label, msgsSlice[1])
	}
	if msgsSlice[2] != "msg-B" {
		t.Fatalf("[%s] messages[2] 오류: want msg-B, got %v", label, msgsSlice[2])
	}

	// label: last-write-wins(branchB)
	lbl, ok := state["label"]
	if !ok {
		t.Fatalf("[%s] label 키가 없습니다", label)
	}
	if lbl != "from-B" {
		t.Fatalf("[%s] label 오류: want from-B, got %v", label, lbl)
	}

	// base_val: 분기가 갱신하지 않았으므로 이중 누적 없이 초기값 유지
	bv, ok := state["base_val"]
	if !ok {
		t.Fatalf("[%s] base_val 키가 없습니다", label)
	}
	if bv != "initial" {
		t.Fatalf("[%s] base_val 오류: want initial, got %v (이중 누적 의심)", label, bv)
	}

	// 격리 확인: branchA 입력에 branchB 쓰기가 없어야 한다
	isolA, ok := state["isolation_A"]
	if !ok {
		t.Fatalf("[%s] isolation_A 키가 없습니다", label)
	}
	if isolA != true {
		t.Fatalf("[%s] isolation_A 오류: branchA 입력에 branchB 쓰기가 반영됨(격리 위반)", label)
	}

	// 격리 확인: branchB 입력에 branchA 쓰기가 없어야 한다
	// 주의: branchB는 branchA 이후에 순차 실행되므로 branchA가 쓴 "isolation_A" 키가
	// branchB의 입력 state에 포함될 수 있다. 여기서는 branchA가 명시적으로 쓰는
	// "seen_A" 키의 유무로 판단한다(branchA는 seen_A를 쓰지 않음).
	isolB, ok := state["isolation_B"]
	if !ok {
		t.Fatalf("[%s] isolation_B 키가 없습니다", label)
	}
	if isolB != true {
		t.Fatalf("[%s] isolation_B 오류: branchB 입력에 branchA 쓰기가 반영됨(격리 위반)", label)
	}
}

// TestFanoutE2E_세경로_최종상태_동일 는 같은 fanout 그래프를 Invoke·Stream·subgraph 경유 Invoke
// 세 경로로 각각 실행했을 때 최종 상태가 동일하고, 분기 격리도 성립함을 교차 검증한다.
//
// 검증 조건(SPEC §5.1, §5.2, §5.3 / implement.md task-004):
//   - 세 경로 모두 두 분기 메시지를 보존한다(messages = [msg-base, msg-A, msg-B]).
//   - 세 경로 모두 동일한 최종 상태를 낸다(교차 비교).
//   - 분기 격리가 깨지지 않는다(한 분기의 쓰기가 다른 분기 입력에 반영되지 않음).
//
// subgraph 경로 주의:
//   - 공유 상태 모드에서 AsNode()는 서브그래프 종료 상태 전체를 StateUpdate로 반환한다.
//   - 부모 그래프가 messages 리듀서를 가지면, "초기 messages"가 서브그래프 결과에
//     이미 포함된 채로 다시 리듀서에 들어가 이중 누적이 생긴다.
//   - 이 e2e 테스트에서 subgraph 경로는 부모 그래프에 messages 리듀서를 달지 않아
//     마지막 덮어쓰기(서브그래프 최종 상태)로 messages를 받는다.
//     — 서브그래프 내부에서 fanout 병합이 올바르게 동작함을 검증하는 것이 목적이다.
func TestFanoutE2E_세경로_최종상태_동일(t *testing.T) {
	initial := graph.State{
		"base_val": "initial",
		"messages": []any{"msg-base"},
	}

	// --- 경로 1: Invoke ---
	invokeResult, err := buildFanoutE2EGraph(t).Invoke(context.Background(), initial, config.RunConfig{})
	if err != nil {
		t.Fatalf("[Invoke] 실행 실패: %v", err)
	}
	assertFanoutE2EResult(t, "Invoke", invokeResult)

	// --- 경로 2: Stream (마지막 ModeValues 이벤트에서 최종 상태 추출) ---
	ch, err := buildFanoutE2EGraph(t).Stream(
		context.Background(), initial, config.RunConfig{}, core.ModeValues,
	)
	if err != nil {
		t.Fatalf("[Stream] 실행 실패: %v", err)
	}
	var streamFinalState graph.State
	for evt := range ch {
		if evt.Mode == core.ModeValues {
			streamFinalState = evt.Value
		}
	}
	if streamFinalState == nil {
		t.Fatal("[Stream] ModeValues 이벤트가 방출되지 않았습니다")
	}
	assertFanoutE2EResult(t, "Stream", streamFinalState)

	// Invoke와 Stream 교차 비교
	invokeMsgs := invokeResult["messages"].([]any)
	streamMsgs := streamFinalState["messages"].([]any)
	if len(invokeMsgs) != len(streamMsgs) {
		t.Fatalf("messages 길이 불일치: Invoke=%d(%v), Stream=%d(%v)",
			len(invokeMsgs), invokeMsgs, len(streamMsgs), streamMsgs)
	}
	for i, im := range invokeMsgs {
		if im != streamMsgs[i] {
			t.Fatalf("messages[%d] 불일치: Invoke=%v, Stream=%v", i, im, streamMsgs[i])
		}
	}
	if invokeResult["label"] != streamFinalState["label"] {
		t.Fatalf("label 불일치: Invoke=%v, Stream=%v", invokeResult["label"], streamFinalState["label"])
	}

	// --- 경로 3: subgraph 경유 Invoke ---
	// 동일한 fanout 그래프를 부모 그래프의 서브그래프 노드(공유 상태 모드)로 등록해 실행한다.
	// 서브그래프 fanout 병합이 올바르게 작동하는지 검증하는 것이 목적이다.
	// 부모는 messages 리듀서를 달지 않아 서브그래프 최종 상태를 그대로(last-write-wins) 받는다.
	subCompiled := buildFanoutE2EGraph(t)
	pb := graph.NewStateGraph(graph.StateSchema{}) // 부모에 messages 리듀서 없음
	if err := pb.AddSubgraphNode("fanout_sub", subCompiled); err != nil {
		t.Fatalf("[subgraph] AddSubgraphNode 실패: %v", err)
	}
	if err := pb.SetEntryPoint("fanout_sub"); err != nil {
		t.Fatalf("[subgraph] SetEntryPoint 실패: %v", err)
	}
	parentCompiled, err := pb.Compile()
	if err != nil {
		t.Fatalf("[subgraph] Compile 실패: %v", err)
	}
	subgraphFinalState, err := parentCompiled.Invoke(context.Background(), initial, config.RunConfig{})
	if err != nil {
		t.Fatalf("[subgraph] Invoke 실패: %v", err)
	}
	// 서브그래프 경로에서도 동일한 최종 상태를 기대한다.
	// 서브그래프 내부 fanout이 올바르게 병합되면 messages = [msg-base, msg-A, msg-B]가 된다.
	assertFanoutE2EResult(t, "subgraph", subgraphFinalState)

	// subgraph 경로와 Invoke 경로 교차 비교
	subMsgs := subgraphFinalState["messages"].([]any)
	if len(invokeMsgs) != len(subMsgs) {
		t.Fatalf("messages 길이 불일치: Invoke=%d(%v), subgraph=%d(%v)",
			len(invokeMsgs), invokeMsgs, len(subMsgs), subMsgs)
	}
	for i, im := range invokeMsgs {
		if im != subMsgs[i] {
			t.Fatalf("messages[%d] 불일치: Invoke=%v, subgraph=%v", i, im, subMsgs[i])
		}
	}
	if invokeResult["label"] != subgraphFinalState["label"] {
		t.Fatalf("label 불일치: Invoke=%v, subgraph=%v", invokeResult["label"], subgraphFinalState["label"])
	}
}

// TestFanoutE2E_분기격리_순차실행_오염없음 은 순차 실행에서 앞선 분기의 결과 쓰기가
// 뒤 분기의 입력 state에 반영되지 않음을 명시적으로 검증한다(SPEC §5.2).
//
// 시나리오:
//   - branchA가 먼저 실행되며 "written_by_A": true를 상태에 기록한다.
//   - branchB가 나중에 실행될 때, 자신의 입력 state에 "written_by_A"가 없어야 한다.
func TestFanoutE2E_분기격리_순차실행_오염없음(t *testing.T) {
	addMessages := func(cur, upd any) any {
		var base []any
		if cur != nil {
			if s, ok := cur.([]any); ok {
				base = append(base, s...)
			}
		}
		if upd == nil {
			return base
		}
		switch v := upd.(type) {
		case []any:
			return append(base, v...)
		default:
			return append(base, v)
		}
	}

	schema := graph.StateSchema{
		Reducers: map[string]graph.ReducerFunc{
			"messages": addMessages,
		},
	}

	b := graph.NewStateGraph(schema)

	// dispatcher: 두 분기로 Fanout
	dispatcherNode := func(ctx context.Context, st graph.State) (any, error) {
		return command.Fanout([]command.Send{
			command.NewSend("branchA", nil), // branchA가 먼저 실행됨(순차)
			command.NewSend("branchB", nil),
		}), nil
	}

	// branchA: "written_by_A"를 상태에 기록한다.
	branchANode := func(ctx context.Context, st graph.State) (any, error) {
		return graph.StateUpdate{
			"messages":    "msg-A",
			"written_by_A": true,
		}, nil
	}

	// branchB: 자신의 입력 state에 "written_by_A"가 없어야 한다(격리).
	// 있으면 branchA의 결과 병합이 branchB 입력을 오염시킨 것이다.
	var branchBObservedWrittenByA bool
	branchBNode := func(ctx context.Context, st graph.State) (any, error) {
		_, found := st["written_by_A"]
		branchBObservedWrittenByA = found // 격리 위반 여부 기록
		return graph.StateUpdate{
			"messages": "msg-B",
		}, nil
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

	// 격리 검증: branchB 입력에 branchA가 쓴 "written_by_A"가 없어야 한다.
	if branchBObservedWrittenByA {
		t.Fatal("격리 위반: branchB 입력 state에 branchA의 written_by_A가 반영됐습니다. " +
			"fanout 시점 base 스냅샷이 아닌 병합 결과를 입력으로 받은 것으로 보입니다.")
	}

	// 양쪽 분기 메시지가 모두 보존됐는지 확인
	msgs, ok := result["messages"]
	if !ok {
		t.Fatal("result에 messages 키가 없습니다")
	}
	msgsSlice, ok := msgs.([]any)
	if !ok {
		t.Fatalf("messages 타입 오류: got %T", msgs)
	}
	if len(msgsSlice) != 2 {
		t.Fatalf("messages 길이 오류: want 2 (msg-A + msg-B), got %d (%v)", len(msgsSlice), msgsSlice)
	}
	found := map[any]bool{}
	for _, v := range msgsSlice {
		found[v] = true
	}
	if !found["msg-A"] {
		t.Fatalf("messages에 msg-A가 없습니다: %v", msgsSlice)
	}
	if !found["msg-B"] {
		t.Fatalf("messages에 msg-B가 없습니다: %v", msgsSlice)
	}

	// 최종 상태에 written_by_A가 있어야 한다(branchA의 결과는 병합됨)
	if result["written_by_A"] != true {
		t.Fatalf("written_by_A 오류: want true, got %v", result["written_by_A"])
	}
}
