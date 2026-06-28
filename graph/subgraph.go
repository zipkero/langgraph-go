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
				branchResult, branchErr := invokeSubgraphFromNode(ctx, compiled, se.target, se.state, step+1, cfg)
				if branchErr != nil {
					return subgraphResult{}, fmt.Errorf("graph: 서브그래프 Fanout 분기 %q 실행 실패: %w", se.target, branchErr)
				}
				// 분기에서 parent 명령이 발생하면 즉시 전파
				if branchResult.parentCmd != nil {
					return branchResult, nil
				}
				// 분기 결과를 공유 state에 병합한다(last-write-wins)
				for k, v := range branchResult.state {
					state[k] = v
				}
			}
			break
		}

		current = next.target
	}

	return subgraphResult{state: state}, nil
}

// invokeSubgraphFromNode 는 서브그래프 Fanout 분기를 위한 내부 헬퍼다.
// runFromNode의 서브그래프 버전으로 parent 명령을 감지해 반환한다.
func invokeSubgraphFromNode(ctx context.Context, compiled *Compiled, startNode string, initState State, stepOffset int, cfg config.RunConfig) (subgraphResult, error) {
	state := make(State, len(initState))
	for k, v := range initState {
		state[k] = v
	}

	current := startNode
	for step := stepOffset; current != ""; step++ {
		if err := checkMaxSteps(step, compiled.maxSteps); err != nil {
			return subgraphResult{}, err
		}

		res, err := runNode(ctx, current, compiled.nodes, state)
		if err != nil {
			return subgraphResult{}, err
		}

		// ToParent/parent-Send 감지
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

		state = applyReducers(state, res.Update, compiled.schema)

		next, err := resolveNext(ctx, current, res, state, compiled.edges, compiled.condEdges, compiled.nodes)
		if err != nil {
			return subgraphResult{}, err
		}

		if len(next.sends) > 0 {
			for _, se := range next.sends {
				branchResult, branchErr := invokeSubgraphFromNode(ctx, compiled, se.target, se.state, step+1, cfg)
				if branchErr != nil {
					return subgraphResult{}, fmt.Errorf("graph: 중첩 서브그래프 Fanout 분기 %q 실행 실패: %w", se.target, branchErr)
				}
				if branchResult.parentCmd != nil {
					return branchResult, nil
				}
				for k, v := range branchResult.state {
					state[k] = v
				}
			}
			break
		}

		current = next.target
	}

	return subgraphResult{state: state}, nil
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
