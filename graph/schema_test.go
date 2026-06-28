// schema_test.go 는 task-008 검증 조건을 만족하는 단위 테스트를 담는다.
// WithInputSchema/WithOutputSchema를 지정하면 Invoke 입력 필터링과 출력 추출이
// 노드 관찰 입력·최종 반환 State에서 관찰되는지 확인한다(SPEC §5.6, ANALYSIS §2.1, D9).
// 모든 테스트는 stub 노드(정해진 StateUpdate를 반환)를 사용하며 네트워크·API 키 없이 수행된다(D11).
package graph_test

import (
	"context"
	"testing"

	"github.com/zipkero/langgraph-go/config"
	"github.com/zipkero/langgraph-go/graph"
)

// TestInvoke_입력스키마필터링_노드관찰 은 WithInputSchema로 입력 필드를 제한하면
// 노드가 스키마 밖 필드를 관찰하지 못함을 검증한다.
//
// 입력: {allowed: "yes", extra: "hidden"}
// WithInputSchema("allowed") → 노드에 전달되는 state는 allowed만 포함해야 한다.
// 노드는 관찰한 state 키 목록을 seenKeys 채널에 기록한다.
func TestInvoke_입력스키마필터링_노드관찰(t *testing.T) {
	schema := graph.StateSchema{}

	// 노드가 관찰한 state 키를 기록하기 위한 슬라이스(단일 goroutine이므로 채널 불필요)
	var seenKeys []string

	observeNode := func(ctx context.Context, st graph.State) (any, error) {
		for k := range st {
			seenKeys = append(seenKeys, k)
		}
		return graph.StateUpdate{"result": "ok"}, nil
	}

	b := graph.NewStateGraph(schema, graph.WithInputSchema("allowed"))

	if err := b.AddNode("observe", observeNode); err != nil {
		t.Fatalf("AddNode 실패: %v", err)
	}
	if err := b.SetEntryPoint("observe"); err != nil {
		t.Fatalf("SetEntryPoint 실패: %v", err)
	}

	compiled, err := b.Compile()
	if err != nil {
		t.Fatalf("Compile 실패: %v", err)
	}

	input := graph.State{
		"allowed": "yes",
		"extra":   "hidden",
	}
	_, err = compiled.Invoke(context.Background(), input, config.RunConfig{})
	if err != nil {
		t.Fatalf("Invoke 실패: %v", err)
	}

	// 노드가 본 키 집합에 "extra"가 없어야 한다.
	for _, k := range seenKeys {
		if k == "extra" {
			t.Errorf("노드가 입력 스키마 밖 필드 %q를 관찰했습니다", k)
		}
	}

	// 노드가 본 키 집합에 "allowed"가 있어야 한다.
	found := false
	for _, k := range seenKeys {
		if k == "allowed" {
			found = true
			break
		}
	}
	if !found {
		t.Error("노드가 입력 스키마 안 필드 \"allowed\"를 관찰하지 못했습니다")
	}
}

// TestInvoke_출력스키마추출_반환키집합 은 WithOutputSchema로 출력 필드를 제한하면
// 최종 반환 State에 스키마 밖 필드가 포함되지 않음을 검증한다.
//
// 노드: {out_field: "keep", internal: "drop"} 반환
// WithOutputSchema("out_field") → 반환 State는 out_field만 포함해야 한다.
func TestInvoke_출력스키마추출_반환키집합(t *testing.T) {
	schema := graph.StateSchema{}

	produceNode := func(ctx context.Context, st graph.State) (any, error) {
		return graph.StateUpdate{
			"out_field": "keep",
			"internal":  "drop",
		}, nil
	}

	b := graph.NewStateGraph(schema, graph.WithOutputSchema("out_field"))

	if err := b.AddNode("produce", produceNode); err != nil {
		t.Fatalf("AddNode 실패: %v", err)
	}
	if err := b.SetEntryPoint("produce"); err != nil {
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

	// "internal" 키가 최종 반환에 없어야 한다.
	if _, ok := result["internal"]; ok {
		t.Error("최종 반환 State에 출력 스키마 밖 필드 \"internal\"이 포함됐습니다")
	}

	// "out_field" 키가 최종 반환에 있어야 한다.
	v, ok := result["out_field"]
	if !ok {
		t.Fatal("최종 반환 State에 출력 스키마 안 필드 \"out_field\"가 없습니다")
	}
	if v != "keep" {
		t.Fatalf("out_field 값 오류: want keep, got %v", v)
	}
}

// TestInvoke_입출력스키마동시_필터링과추출 은 WithInputSchema와 WithOutputSchema를 동시에
// 지정했을 때 입력 필터링과 출력 추출이 모두 올바르게 동작함을 검증한다.
//
// 입력: {a: "in_a", b: "in_b", noise: "dropped_at_input"}
// WithInputSchema("a", "b") → 노드는 a·b만 본다.
// 노드: {a: "out_a", b: "out_b", extra: "dropped_at_output"} 반환
// WithOutputSchema("a") → 최종 반환은 a만 포함한다.
func TestInvoke_입출력스키마동시_필터링과추출(t *testing.T) {
	schema := graph.StateSchema{}

	var seenKeys []string

	processNode := func(ctx context.Context, st graph.State) (any, error) {
		for k := range st {
			seenKeys = append(seenKeys, k)
		}
		return graph.StateUpdate{
			"a":     "out_a",
			"b":     "out_b",
			"extra": "dropped_at_output",
		}, nil
	}

	b := graph.NewStateGraph(schema,
		graph.WithInputSchema("a", "b"),
		graph.WithOutputSchema("a"),
	)

	if err := b.AddNode("process", processNode); err != nil {
		t.Fatalf("AddNode 실패: %v", err)
	}
	if err := b.SetEntryPoint("process"); err != nil {
		t.Fatalf("SetEntryPoint 실패: %v", err)
	}

	compiled, err := b.Compile()
	if err != nil {
		t.Fatalf("Compile 실패: %v", err)
	}

	input := graph.State{
		"a":     "in_a",
		"b":     "in_b",
		"noise": "dropped_at_input",
	}
	result, err := compiled.Invoke(context.Background(), input, config.RunConfig{})
	if err != nil {
		t.Fatalf("Invoke 실패: %v", err)
	}

	// 노드가 "noise"를 관찰하지 않아야 한다(입력 필터링).
	for _, k := range seenKeys {
		if k == "noise" {
			t.Errorf("노드가 입력 스키마 밖 필드 %q를 관찰했습니다", k)
		}
	}

	// 최종 반환에 "b"·"extra"가 없어야 한다(출력 추출).
	for _, unwanted := range []string{"b", "extra"} {
		if _, ok := result[unwanted]; ok {
			t.Errorf("최종 반환 State에 출력 스키마 밖 필드 %q가 포함됐습니다", unwanted)
		}
	}

	// 최종 반환에 "a"가 있어야 한다.
	v, ok := result["a"]
	if !ok {
		t.Fatal("최종 반환 State에 출력 스키마 안 필드 \"a\"가 없습니다")
	}
	if v != "out_a" {
		t.Fatalf("a 값 오류: want out_a, got %v", v)
	}
}

// TestInvoke_스키마미설정_전체필드유지 는 WithInputSchema/WithOutputSchema를 지정하지 않으면
// 입출력이 모두 그대로 통과됨을 검증한다(기존 동작 회귀 방지).
func TestInvoke_스키마미설정_전체필드유지(t *testing.T) {
	schema := graph.StateSchema{}

	nodeA := func(ctx context.Context, st graph.State) (any, error) {
		return graph.StateUpdate{"x": "x_val", "y": "y_val"}, nil
	}

	// 스키마 옵션 없이 NewStateGraph 호출
	b := graph.NewStateGraph(schema)

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

	input := graph.State{"init": "present"}
	result, err := compiled.Invoke(context.Background(), input, config.RunConfig{})
	if err != nil {
		t.Fatalf("Invoke 실패: %v", err)
	}

	// 스키마 미설정이므로 노드 update의 모든 필드가 결과에 있어야 한다.
	for _, want := range []string{"x", "y"} {
		if _, ok := result[want]; !ok {
			t.Errorf("스키마 미설정 시 필드 %q가 결과에 없습니다", want)
		}
	}
	// 초기 입력 필드도 그대로 유지돼야 한다.
	if _, ok := result["init"]; !ok {
		t.Error("스키마 미설정 시 입력 필드 \"init\"이 결과에 없습니다")
	}
}
