// exec.go 는 그래프 실행 루프의 핵심 내부 함수를 담는다.
// runNode, applyReducers, resolveNext 를 정의하며,
// 이후 task에서 maxSteps, 조건엣지, command 제어 흐름, 스키마 필터링이 단계적으로 추가된다.
package graph

import (
	"context"
	"fmt"

	"github.com/zipkero/langgraph-go/checkpoint"
	"github.com/zipkero/langgraph-go/config"
	"github.com/zipkero/langgraph-go/graph/command"
)

// runNode 는 지정된 노드를 실행하고 any 반환값을 NodeResult로 정규화한다(§2.2 D2).
//
// 타입 스위치 규칙:
//   - command.Command  → NodeResult{Update: cmd.Update, Control: &cmd}
//   - core.StateUpdate → NodeResult{Update: u}
//   - nil              → NodeResult{Update: nil}
//   - 그 외            → error("지원하지 않는 노드 반환 타입: …")
func runNode(ctx context.Context, name string, nodes map[string]nodeEntry, state State) (NodeResult, error) {
	entry, ok := nodes[name]
	if !ok {
		return NodeResult{}, fmt.Errorf("graph: 노드 %q가 존재하지 않습니다", name)
	}

	raw, err := entry.fn(ctx, state)
	if err != nil {
		return NodeResult{}, fmt.Errorf("graph: 노드 %q 실행 중 오류: %w", name, err)
	}

	switch v := raw.(type) {
	case command.Command:
		return NodeResult{Update: v.Update, Control: &v}, nil
	case StateUpdate:
		return NodeResult{Update: v}, nil
	case nil:
		return NodeResult{Update: nil}, nil
	default:
		return NodeResult{}, fmt.Errorf("graph: 노드 %q가 지원하지 않는 반환 타입을 반환했습니다: %T", name, raw)
	}
}

// applyReducers 는 update 의 각 키를 스키마에 등록된 리듀서로 병합한다(§2.4 D4).
// 리듀서가 없는 필드는 last-write-wins(덮어쓰기)로 처리한다.
// update 가 nil 이면 state 를 그대로 반환한다.
func applyReducers(state State, update StateUpdate, schema StateSchema) State {
	if update == nil {
		return state
	}

	// 기존 상태를 얕은 복사해 새 맵을 만든다(원본 불변성 보장).
	next := make(State, len(state)+len(update))
	for k, v := range state {
		next[k] = v
	}

	for k, upd := range update {
		if reducer, ok := schema.Reducers[k]; ok {
			// 리듀서가 등록된 필드: reducer(현재값, 업데이트값)
			next[k] = reducer(next[k], upd)
		} else {
			// 미등록 필드: last-write-wins
			next[k] = upd
		}
	}

	return next
}

// nextResult 는 resolveNext 가 반환하는 다음 실행 결정을 담는다.
// 단일 노드(Single)·종료(End)·다중 분기(Fanout) 세 가지 경우를 표현한다.
type nextResult struct {
	// target 은 이동할 단일 노드 이름이다. 빈 문자열이면 터미널(End)과 같다.
	target string
	// sends 는 Fanout 분기 목록이다. nil 이면 Fanout이 아니다.
	sends []sendEntry
}

// sendEntry 는 Fanout 분기 하나를 실행하기 위한 내부 레코드다.
type sendEntry struct {
	target string
	state  State
}

// resolveNext 는 현재 노드와 NodeResult를 받아 다음 실행 결정을 반환한다(§2.3 D3).
//
// 우선순위:
//  1. res.Control이 Command인 경우: End → 종료, Goto → target 이동(WithDestinations 검증),
//     Fanout → 다중 분기(sendEntry 목록 반환).
//  2. AddConditionalEdges가 있으면 router(ctx, state) 키를 mapping으로 노드 이름에 매핑.
//  3. AddEdge의 정적 엣지.
//  4. 위 모두 해당 없으면 빈 target(터미널).
func resolveNext(ctx context.Context, name string, res NodeResult, state State, edges []edgeEntry, condEdges []conditionalEdgeEntry, nodes map[string]nodeEntry) (nextResult, error) {
	// 1단계: Command 제어 흐름 해석 (§2.3, §5.5 D5)
	if res.Control != nil {
		ctrl := res.Control

		// End: 그래프 즉시 종료
		if ctrl.IsEnd() {
			return nextResult{}, nil
		}

		// Fanout: 다중 분기 — 각 Send를 sendEntry로 변환
		if len(ctrl.Sends) > 0 {
			entries := make([]sendEntry, 0, len(ctrl.Sends))
			for _, s := range ctrl.Sends {
				// 부모 그래프 대상(TargetParent)은 task-009(서브그래프)에서 처리한다.
				// 현재 그래프 맥락에서는 current 대상만 허용한다.
				if s.Graph == command.TargetParent {
					return nextResult{}, fmt.Errorf("graph: Fanout Send의 Graph=parent는 서브그래프 맥락(task-009)에서만 유효합니다")
				}
				branchState, err := coerceSendState(s.State, state)
				if err != nil {
					return nextResult{}, fmt.Errorf("graph: Fanout Send(target=%q) 상태 변환 실패: %w", s.Target, err)
				}
				entries = append(entries, sendEntry{target: s.Target, state: branchState})
			}
			return nextResult{sends: entries}, nil
		}

		// Goto: 대상 노드로 이동 — WithDestinations 선언에 대해 런타임 검증
		if ctrl.Goto != "" {
			if err := validateGotoTarget(name, ctrl.Goto, nodes); err != nil {
				return nextResult{}, err
			}
			return nextResult{target: ctrl.Goto}, nil
		}

		// Goto가 빈 문자열이고 IsEnd도 아닌 경우 → 터미널로 처리
		return nextResult{}, nil
	}

	// 2단계: 조건 엣지 — from == name 인 첫 조건 엣지의 라우터를 실행해 다음 노드를 결정한다.
	for _, ce := range condEdges {
		if ce.from == name {
			key := ce.router(ctx, state)
			if target, ok := ce.mapping[key]; ok {
				return nextResult{target: target}, nil
			}
			// mapping에 없는 키 → 터미널로 처리(라우터가 알 수 없는 값을 반환한 경우)
			return nextResult{}, nil
		}
	}

	// 3단계: 정적 엣지에서 from == name 인 첫 엣지의 to를 반환
	for _, e := range edges {
		if e.from == name {
			return nextResult{target: e.to}, nil
		}
	}

	// 4단계: 해당 없음 → 터미널
	return nextResult{}, nil
}

// validateGotoTarget 은 Goto 대상이 해당 노드의 WithDestinations 선언에 포함되는지 확인한다(§2.3 D5).
// destinations가 비어 있으면(WithDestinations 미선언) 제한 없이 허용한다.
func validateGotoTarget(from, target string, nodes map[string]nodeEntry) error {
	entry, ok := nodes[from]
	if !ok {
		return fmt.Errorf("graph: Goto 검증 중 노드 %q를 찾을 수 없습니다", from)
	}
	if len(entry.destinations) == 0 {
		// WithDestinations 미선언 → 런타임 제한 없음
		return nil
	}
	for _, d := range entry.destinations {
		if d == target {
			return nil
		}
	}
	return fmt.Errorf("graph: 노드 %q의 Goto 대상 %q가 WithDestinations 선언에 없습니다(허용 목록: %v)",
		from, target, entry.destinations)
}

// coerceSendState 는 Send.State(any)를 core.State(map[string]any)로 변환한다.
// Send.State가 nil 이면 현재 그래프 state를 그대로 사용한다.
// Send.State가 map[string]any(= core.State) 이면 그대로 사용한다.
// 그 외 타입은 error를 반환한다.
func coerceSendState(raw any, currentState State) (State, error) {
	if raw == nil {
		// nil이면 현재 상태를 분기에 그대로 전달
		copied := make(State, len(currentState))
		for k, v := range currentState {
			copied[k] = v
		}
		return copied, nil
	}
	switch v := raw.(type) {
	case State: // map[string]any alias
		return v, nil
	default:
		return nil, fmt.Errorf("지원하지 않는 Send.State 타입: %T (core.State 또는 nil 필요)", raw)
	}
}

// branchResult 는 runFromNodeCollect 의 반환값이다.
// finalState 는 분기 실행 후 최종 상태, updates 는 분기 내 각 노드가 반환한
// 원본 업데이트 목록이다(ANALYSIS §2.2).
// 외부에서 updates 를 순서대로 applyReducers 로 적용하면 이중 누적 없이 병합된다.
type branchResult struct {
	finalState State
	updates    []StateUpdate
}

// runFromNodeCollect 는 runFromNode 와 동일하게 분기를 실행하되,
// 분기 내 각 노드가 반환한 원본 업데이트 목록(updates)도 함께 반환한다.
//
// updates 목록을 외부 state 에 순서대로 applyReducers 로 적용하면
// 분기가 공유 state 에 기여한 변경만 리듀서로 누적되므로 이중 누적이 없다(ANALYSIS §2.2).
func runFromNodeCollect(ctx context.Context, compiled *Compiled, startNode string, initState State, stepOffset int, cfg config.RunConfig) (branchResult, error) {
	state := make(State, len(initState))
	for k, v := range initState {
		state[k] = v
	}

	var updates []StateUpdate

	current := startNode
	for step := stepOffset; current != ""; step++ {
		if err := checkMaxSteps(step, compiled.maxSteps); err != nil {
			return branchResult{}, err
		}

		res, err := runNode(ctx, current, compiled.nodes, state)
		if err != nil {
			return branchResult{}, err
		}

		// 분기 내부 state를 갱신하고 원본 update를 목록에 추가한다.
		state = applyReducers(state, res.Update, compiled.schema)
		if res.Update != nil {
			updates = append(updates, res.Update)
		}

		next, err := resolveNext(ctx, current, res, state, compiled.edges, compiled.condEdges, compiled.nodes)
		if err != nil {
			return branchResult{}, err
		}

		// 분기 내부에서 Fanout이 다시 발생하는 경우도 재귀 처리한다.
		if len(next.sends) > 0 {
			for _, se := range next.sends {
				inner, innerErr := runFromNodeCollect(ctx, compiled, se.target, se.state, step+1, cfg)
				if innerErr != nil {
					return branchResult{}, fmt.Errorf("graph: 중첩 Fanout 분기 %q 실행 실패: %w", se.target, innerErr)
				}
				// 내부 분기의 state 변경을 현재 분기 state에 반영하고
				// 내부 분기 updates도 이 분기 updates 목록에 합산한다.
				for _, u := range inner.updates {
					state = applyReducers(state, u, compiled.schema)
				}
				updates = append(updates, inner.updates...)
			}
			break
		}

		current = next.target
	}

	return branchResult{finalState: state, updates: updates}, nil
}

// runFromNode 는 지정된 startNode부터 실행 루프를 진행하는 내부 헬퍼다.
// Fanout 분기가 Send.Target 노드부터 독립 실행될 때 호출한다.
// stepOffset 은 현재 루프의 스텝 카운트를 이어받아 maxSteps 검사에 반영한다.
//
// 이 함수는 최종 state만 반환한다. 병합에는 runFromNodeCollect 를 사용한다.
func runFromNode(ctx context.Context, compiled *Compiled, startNode string, initState State, stepOffset int, cfg config.RunConfig) (State, error) {
	res, err := runFromNodeCollect(ctx, compiled, startNode, initState, stepOffset, cfg)
	if err != nil {
		return nil, err
	}
	return res.finalState, nil
}

// checkMaxSteps 는 step이 maxSteps 이상이면 순환 무한 실행 차단 error를 반환한다(§5.10).
// step은 0-기반이므로 step >= maxSteps 조건을 사용한다.
func checkMaxSteps(step, maxSteps int) error {
	if step >= maxSteps {
		return fmt.Errorf("graph: 최대 스텝(%d)을 초과했습니다 — 순환 그래프가 종료되지 않을 수 있습니다", maxSteps)
	}
	return nil
}

// filterBySchema 는 src에서 fields에 속하는 키만 추출해 새 State를 반환한다.
// fields가 nil 또는 비어 있으면 src를 얕은 복사해 그대로 반환한다.
func filterBySchema(src State, fields []string) State {
	if len(fields) == 0 {
		// 스키마 미설정: 전체 state 얕은 복사
		dst := make(State, len(src))
		for k, v := range src {
			dst[k] = v
		}
		return dst
	}
	dst := make(State, len(fields))
	for _, f := range fields {
		if v, ok := src[f]; ok {
			dst[f] = v
		}
	}
	return dst
}

// checkpointSave 는 체크포인터가 설정된 경우 현재 state를 thread_id에 저장하는 헬퍼다.
// 체크포인터가 nil이거나 thread_id가 없으면 아무 것도 하지 않는다.
func checkpointSave(ctx context.Context, compiled *Compiled, cfg config.RunConfig, state State) {
	if compiled.checkpointer == nil {
		return
	}
	threadID, err := checkpoint.ThreadIDFromConfig(cfg)
	if err != nil {
		// thread_id 없음 → 체크포인트 생략
		return
	}
	cp := checkpoint.Checkpoint{
		ThreadID: threadID,
		Values:   state,
	}
	// 저장 실패는 무시한다(영속 실패가 실행을 중단시키지 않도록).
	_ = compiled.checkpointer.Put(ctx, threadID, cp)
}

// invokeLoop 는 Invoke의 실행 루프 본체다.
// 진입점(조건 진입점 포함)부터 엣지를 따라 노드를 순차 실행하고, 최종 State를 반환한다.
//
// 체크포인터 처리(§2.1, SPEC §5.9):
//   - 루프 시작 전: WithCheckpointer 지정 시 thread_id 기존 상태를 LoadState로 로드해 input과 병합한다.
//   - 각 스텝 후: applyReducers 직후 현재 state를 thread_id에 SaveState한다.
//
// 입출력 스키마 처리(§2.1, SPEC §5.6):
//   - 루프 시작 전: WithInputSchema가 지정된 경우 input을 입력 스키마 필드로 필터링한다.
//   - 루프 종료 후: WithOutputSchema가 지정된 경우 최종 state를 출력 스키마 필드로 추출해 반환한다.
func invokeLoop(ctx context.Context, compiled *Compiled, input State, cfg config.RunConfig) (State, error) {
	// 체크포인터 로드: thread_id에 저장된 기존 상태를 로드해 input과 병합한다(SPEC §5.9, §2.1).
	// 기존 상태가 있으면 병합(기존 상태 기반에 input 덮어쓰기), 없으면 input만 사용한다.
	if compiled.checkpointer != nil {
		threadID, err := checkpoint.ThreadIDFromConfig(cfg)
		if err == nil && threadID != "" {
			cp, ok, loadErr := compiled.checkpointer.Get(ctx, threadID)
			if loadErr == nil && ok && cp.Values != nil {
				// 기존 상태를 기반으로 하고 이번 input을 덮어쓴다.
				merged := make(State, len(cp.Values)+len(input))
				for k, v := range cp.Values {
					merged[k] = v
				}
				for k, v := range input {
					merged[k] = v
				}
				input = merged
			}
		}
	}

	// 입력 스키마 필터링: WithInputSchema가 지정된 경우 입력 필드를 스키마 필드로 제한한다(§2.1).
	filtered := filterBySchema(input, compiled.schemaOpts.inputSchema)
	state := make(State, len(filtered))
	for k, v := range filtered {
		state[k] = v
	}

	// 진입점 결정: 조건 진입점이 설정된 경우 라우터+mapping으로 첫 노드를 선택한다(§2.1, §5.4).
	var current string
	if compiled.condEntry != nil {
		key := compiled.condEntry.router(ctx, state)
		target, ok := compiled.condEntry.mapping[key]
		if !ok {
			return nil, fmt.Errorf("graph: 조건 진입점 라우터가 반환한 키 %q가 mapping에 없습니다", key)
		}
		current = target
	} else {
		current = compiled.entryPoint
	}

	for step := 0; current != ""; step++ {
		// 매 스텝마다 한도 초과 여부를 검사한다(§5.10, §2.1 checkMaxSteps).
		if err := checkMaxSteps(step, compiled.maxSteps); err != nil {
			return nil, err
		}

		res, err := runNode(ctx, current, compiled.nodes, state)
		if err != nil {
			return nil, err
		}

		state = applyReducers(state, res.Update, compiled.schema)

		// 체크포인터 저장: applyReducers 직후 현재 state를 thread_id에 저장한다(SPEC §5.9, §2.1).
		checkpointSave(ctx, compiled, cfg, state)

		next, err := resolveNext(ctx, current, res, state, compiled.edges, compiled.condEdges, compiled.nodes)
		if err != nil {
			return nil, err
		}

		// Fanout: 각 분기를 순차 실행하고 분기 내 원본 업데이트들을 리듀서로 누적한다
		// (§2.3 D5, SPEC §5.1, §5.2, ANALYSIS §2.2).
		// 각 분기는 Send.Target 노드부터 Send.State로 독립 실행된다.
		// runFromNodeCollect 가 반환하는 updates 는 분기 내 노드들의 원본 업데이트 목록이므로,
		// 순서대로 applyReducers 로 적용해도 base 키 이중 누적이 없다.
		// task-009(서브그래프) 이전까지는 current-graph 분기만 지원한다.
		if len(next.sends) > 0 {
			for _, se := range next.sends {
				br, branchErr := runFromNodeCollect(ctx, compiled, se.target, se.state, step+1, cfg)
				if branchErr != nil {
					return nil, fmt.Errorf("graph: Fanout 분기 %q 실행 실패: %w", se.target, branchErr)
				}
				// 분기 내 원본 업데이트를 순서대로 공유 state에 병합한다(ANALYSIS §2.2).
				for _, u := range br.updates {
					state = applyReducers(state, u, compiled.schema)
				}
			}
			// Fanout 이후 루프 종료(분기가 내부 루프를 각자 완전 실행했으므로).
			break
		}

		current = next.target
	}

	// 출력 스키마 추출: WithOutputSchema가 지정된 경우 출력 필드를 스키마 필드로 제한한다(§2.1, SPEC §5.6).
	return filterBySchema(state, compiled.schemaOpts.outputSchema), nil
}
