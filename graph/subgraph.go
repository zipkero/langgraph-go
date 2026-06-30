// subgraph.go 는 서브그래프 지원을 구현한다(task-009, SPEC §5.7, ANALYSIS §2.5 D6).
//
// 핵심 설계:
//   - Compiled.AsNode()가 NodeFunc 어댑터를 반환해 서브그래프를 부모 그래프 노드로 등록한다.
//   - 공유 상태 모드: WithInputSchema/WithOutputSchema 미설정 시 부모 상태를 그대로 넘기고
//     서브그래프 종료 상태를 부모 update로 반환한다.
//   - 독립 상태 모드: WithInputSchema/WithOutputSchema 설정 시 입력 필터·출력 추출 경계만 넘긴다
//     (task-008 filterBySchema 재사용).
//   - ToParent/parent-Send: 서브그래프 노드가 이를 반환하면 부모 루프에 전달해 부모 노드로 라우팅한다.
package graph

import (
	"context"
	"fmt"

	"github.com/zipkero/langgraph-go/config"
	"github.com/zipkero/langgraph-go/graph/command"
)

// subgraphResult 는 서브그래프 실행의 결과를 담는 내부 타입이다.
// 정상 종료이면 parentCmd가 nil이고, ToParent/parent-Send 명령을 만나면
// parentCmd에 해당 Command를 담아 반환한다.
type subgraphResult struct {
	// state 는 서브그래프 종료 후 부모에 반영할 상태다.
	// parentCmd가 nil이 아닌 경우에도 Update 필드가 있으면 state에 반영해야 한다.
	state State
	// parentCmd 가 nil이 아니면 이 명령을 부모 루프로 전달한다.
	parentCmd *command.Command
	// updates 는 Fanout 분기 실행 중 각 노드가 반환한 원본 업데이트 목록이다.
	// invokeSubgraphFromNodeCollect 가 채워 반환하며, 호출자가 순서대로
	// applyReducers 로 적용해야 이중 누적 없이 병합된다(ANALYSIS §2.2).
	updates []StateUpdate
}

// invokeSubgraph 는 서브그래프 전용 실행 루프다.
// invokeLoop와 동일하게 동작하지만, 노드가 ToParent 또는 parent-Send 명령을 반환하면
// 즉시 subgraphResult{parentCmd: ...}를 반환해 부모 루프에 전달한다.
//
// 입출력 스키마 처리는 AsNode에서 이미 처리하므로 여기서는 하지 않는다.
func invokeSubgraph(ctx context.Context, compiled *Compiled, input State, cfg config.RunConfig) (subgraphResult, error) {
	state := make(State, len(input))
	for k, v := range input {
		state[k] = v
	}

	// 진입점 결정
	var current string
	if compiled.condEntry != nil {
		key := compiled.condEntry.router(ctx, state)
		target, ok := compiled.condEntry.mapping[key]
		if !ok {
			return subgraphResult{}, fmt.Errorf("graph: 서브그래프 조건 진입점 라우터가 반환한 키 %q가 mapping에 없습니다", key)
		}
		current = target
	} else {
		current = compiled.entryPoint
	}

	for step := 0; current != ""; step++ {
		if err := checkMaxSteps(step, compiled.maxSteps); err != nil {
			return subgraphResult{}, err
		}

		res, err := runNode(ctx, current, compiled.nodes, state)
		if err != nil {
			return subgraphResult{}, err
		}

		// ToParent/parent-Send 명령 감지: 부모 루프로 즉시 전달한다.
		// Update가 있으면 현재 state에 반영한 뒤 parentCmd와 함께 반환한다.
		if res.Control != nil {
			ctrl := res.Control

			// ToParent: 부모 그래프의 target 노드로 라우팅
			if ctrl.IsParent() && !ctrl.IsEnd() && len(ctrl.Sends) == 0 {
				// Update가 있으면 state에 반영
				if ctrl.Update != nil {
					state = applyReducers(state, ctrl.Update, compiled.schema)
				}
				return subgraphResult{state: state, parentCmd: ctrl}, nil
			}

			// parent-Send: Fanout 중 부모 대상 Send가 있는 경우
			if len(ctrl.Sends) > 0 {
				hasParent := false
				for _, s := range ctrl.Sends {
					if s.Graph == command.TargetParent {
						hasParent = true
						break
					}
				}
				if hasParent {
					// Update가 있으면 state에 반영
					if ctrl.Update != nil {
						state = applyReducers(state, ctrl.Update, compiled.schema)
					}
					return subgraphResult{state: state, parentCmd: ctrl}, nil
				}
			}
		}

		state = applyReducers(state, res.Update, compiled.schema)

		next, err := resolveNext(ctx, current, res, state, compiled.edges, compiled.condEdges, compiled.nodes)
		if err != nil {
			return subgraphResult{}, err
		}

		// Fanout 처리 (current-graph 대상만)
		if len(next.sends) > 0 {
			for _, se := range next.sends {
				br, branchErr := invokeSubgraphFromNodeCollect(ctx, compiled, se.target, se.state, step+1, cfg)
				if branchErr != nil {
					return subgraphResult{}, fmt.Errorf("graph: 서브그래프 Fanout 분기 %q 실행 실패: %w", se.target, branchErr)
				}
				// 분기에서 parent 명령이 발생하면 즉시 전파
				if br.parentCmd != nil {
					return br, nil
				}
				// 분기 내 원본 업데이트를 순서대로 리듀서로 누적한다(ANALYSIS §2.2).
				// raw 덮어쓰기 대신 applyReducers를 경유해 리듀서 등록 키가 모두 보존된다.
				for _, u := range br.updates {
					state = applyReducers(state, u, compiled.schema)
				}
			}
			break
		}

		current = next.target
	}

	return subgraphResult{state: state}, nil
}

// invokeSubgraphFromNodeCollect 는 서브그래프 Fanout 분기를 실행하되,
// 분기 내 각 노드가 반환한 원본 업데이트 목록(updates)도 함께 반환한다.
//
// updates 를 외부 state 에 순서대로 applyReducers 로 적용하면
// 이중 누적 없이 리듀서가 등록된 키를 올바르게 병합할 수 있다(ANALYSIS §2.2).
// parent 명령이 발생하면 parentCmd를 채워 반환하고 병합은 호출자가 하지 않는다.
func invokeSubgraphFromNodeCollect(ctx context.Context, compiled *Compiled, startNode string, initState State, stepOffset int, cfg config.RunConfig) (subgraphResult, error) {
	state := make(State, len(initState))
	for k, v := range initState {
		state[k] = v
	}

	var updates []StateUpdate

	current := startNode
	for step := stepOffset; current != ""; step++ {
		if err := checkMaxSteps(step, compiled.maxSteps); err != nil {
			return subgraphResult{}, err
		}

		res, err := runNode(ctx, current, compiled.nodes, state)
		if err != nil {
			return subgraphResult{}, err
		}

		// ToParent/parent-Send 감지: parent 명령 발생 시 즉시 전파
		if res.Control != nil {
			ctrl := res.Control
			if ctrl.IsParent() && !ctrl.IsEnd() && len(ctrl.Sends) == 0 {
				if ctrl.Update != nil {
					state = applyReducers(state, ctrl.Update, compiled.schema)
				}
				return subgraphResult{state: state, parentCmd: ctrl}, nil
			}
			if len(ctrl.Sends) > 0 {
				for _, s := range ctrl.Sends {
					if s.Graph == command.TargetParent {
						if ctrl.Update != nil {
							state = applyReducers(state, ctrl.Update, compiled.schema)
						}
						return subgraphResult{state: state, parentCmd: ctrl}, nil
					}
				}
			}
		}

		// 분기 내부 state를 갱신하고 원본 update를 목록에 추가한다.
		state = applyReducers(state, res.Update, compiled.schema)
		if res.Update != nil {
			updates = append(updates, res.Update)
		}

		next, err := resolveNext(ctx, current, res, state, compiled.edges, compiled.condEdges, compiled.nodes)
		if err != nil {
			return subgraphResult{}, err
		}

		// 분기 내부 중첩 Fanout도 재귀 처리한다.
		if len(next.sends) > 0 {
			for _, se := range next.sends {
				inner, innerErr := invokeSubgraphFromNodeCollect(ctx, compiled, se.target, se.state, step+1, cfg)
				if innerErr != nil {
					return subgraphResult{}, fmt.Errorf("graph: 중첩 서브그래프 Fanout 분기 %q 실행 실패: %w", se.target, innerErr)
				}
				if inner.parentCmd != nil {
					return inner, nil
				}
				// 내부 분기의 updates를 현재 분기 state에 반영하고 목록에 합산한다.
				for _, u := range inner.updates {
					state = applyReducers(state, u, compiled.schema)
				}
				updates = append(updates, inner.updates...)
			}
			break
		}

		current = next.target
	}

	return subgraphResult{state: state, updates: updates}, nil
}

// invokeSubgraphFromNode 는 invokeSubgraphFromNodeCollect 의 래퍼로,
// 최종 state만 필요한 호출자를 위해 존재한다.
func invokeSubgraphFromNode(ctx context.Context, compiled *Compiled, startNode string, initState State, stepOffset int, cfg config.RunConfig) (subgraphResult, error) {
	return invokeSubgraphFromNodeCollect(ctx, compiled, startNode, initState, stepOffset, cfg)
}

// AsNode 는 이 Compiled 그래프를 부모 그래프의 NodeFunc 어댑터로 반환한다(ANALYSIS §2.5 D6).
//
// 동작 모드:
//   - 공유 상태 모드 (WithInputSchema/WithOutputSchema 미설정):
//     부모 상태를 그대로 서브그래프에 전달하고, 서브그래프 종료 상태 전체를 부모 update로 반환한다.
//   - 독립 상태 모드 (WithInputSchema 또는 WithOutputSchema 설정):
//     부모 상태를 입력 스키마로 필터링해 서브그래프에 전달하고,
//     서브그래프 종료 상태를 출력 스키마로 추출해 부모 update로 반환한다.
//   - ToParent/parent-Send:
//     서브그래프 노드가 이를 반환하면 해당 command.Command를 그대로 부모 루프에 반환한다.
//     부모 루프는 기존 resolveNext로 이 Command를 처리한다(IsParent() == true).
func (c *Compiled) AsNode() NodeFunc {
	return func(ctx context.Context, parentState State) (any, error) {
		// 입력 준비: 독립 모드면 입력 스키마로 필터링, 공유 모드면 전체 상태 전달
		subInput := filterBySchema(parentState, c.schemaOpts.inputSchema)

		// 서브그래프 실행
		cfg := config.RunConfig{}
		result, err := invokeSubgraph(ctx, c, subInput, cfg)
		if err != nil {
			return nil, fmt.Errorf("graph: 서브그래프 실행 실패: %w", err)
		}

		// ToParent/parent-Send: 부모 루프에 Command를 그대로 전달한다.
		// 부모의 resolveNext가 IsParent()를 보고 부모 노드로 라우팅한다.
		if result.parentCmd != nil {
			return *result.parentCmd, nil
		}

		// 정상 종료: 출력 스키마로 추출 (독립 모드) 또는 전체 상태 반환 (공유 모드)
		outputState := filterBySchema(result.state, c.schemaOpts.outputSchema)
		return StateUpdate(outputState), nil
	}
}
