// network.go 는 워커들을 노드로 묶어 워커 간 command.Goto 동적 라우팅 그래프를
// 컴파일하고, 메시지에서 종료 신호를 판별하는 BuildNetwork/IsFinalAnswer를 담는다.
// SPEC §5.6, ANALYSIS §1·§2 참조.
package multiagent

import (
	"context"
	"fmt"

	"github.com/zipkero/langgraph-go/agent"
	"github.com/zipkero/langgraph-go/config"
	"github.com/zipkero/langgraph-go/graph"
	"github.com/zipkero/langgraph-go/graph/command"
	"github.com/zipkero/langgraph-go/message"
)

// BuildNetwork 는 workers 를 노드로 등록하고 command.Goto 동적 라우팅으로
// 연결한 *graph.Compiled 를 반환한다(SPEC §5.6, ANALYSIS §2).
//
// 구성 방식:
//   - 각 워커를 NodeFunc 으로 감싸 Builder 에 등록한다.
//     노드 등록 시 WithDestinations 를 써서 모든 워커 이름을 Goto 허용 목적지로 선언한다.
//   - NodeFunc 은 워커를 Invoke 하고 결과 메시지를 상태에 병합한 뒤,
//     SelectNext(tool_calls 기반)로 다음 워커를 결정해 command.Goto 또는 command.End 를 반환한다.
//   - 첫 번째 워커가 진입점이 된다.
//   - 빈 워커 목록은 진입점 미설정으로 graph.Compile validate 에서 에러로 드러난다.
//
// 반환된 그래프는 Invoke/Stream 으로 실행하며, 초기 상태의 "messages" 키에
// 사용자 입력 메시지를 담아 전달한다.
func BuildNetwork(workers []Worker) (*graph.Compiled, error) {
	// 워커 이름 목록 수집(WithDestinations 선언에 사용)
	names := make([]string, len(workers))
	for i, w := range workers {
		names[i] = w.Name()
	}

	// StateSchema: messages 필드를 message.AddMessages 리듀서로 누적한다.
	schema := graph.StateSchema{
		Reducers: map[string]graph.ReducerFunc{
			"messages": func(cur, upd any) any {
				var base []message.Message
				if cur != nil {
					if m, ok := cur.([]message.Message); ok {
						base = m
					}
				}
				var incoming []message.Message
				if upd != nil {
					if m, ok := upd.([]message.Message); ok {
						incoming = m
					}
				}
				return message.AddMessages(base, incoming)
			},
		},
	}

	b := graph.NewStateGraph(schema)

	// 각 워커를 노드로 등록한다.
	// WithDestinations 에 모든 워커 이름을 선언해 Goto 동적 라우팅을 허용한다.
	for _, w := range workers {
		// 루프 변수 캡처
		wk := w
		nodeFn := buildWorkerNodeFunc(wk)
		if err := b.AddNode(wk.Name(), nodeFn, graph.WithDestinations(names...)); err != nil {
			return nil, fmt.Errorf("multiagent: BuildNetwork — 노드 등록 실패(%q): %w", wk.Name(), err)
		}
	}

	// validate 의 BFS 도달 가능성 검사를 통과하기 위해, 각 워커 노드에서
	// 다른 모든 워커 노드로 조건 엣지를 추가한다.
	// 실제 실행에서는 NodeFunc 이 command.Goto(또는 End)를 반환하므로
	// resolveNext 가 Command 처리(1단계)에서 반환하며, 조건 엣지는 실행되지 않는다.
	// 이 조건 엣지는 validate 의 도달 가능성 검사를 통과하기 위한 구조 선언이다.
	if len(workers) > 1 {
		// 모든 라우터 반환값을 동일한 이름의 노드로 매핑하는 더미 라우터.
		// NodeFunc 이 항상 Command 를 반환하므로 이 라우터는 실제로 호출되지 않는다.
		// validate 의 BFS 도달 가능성 검사를 통과하기 위한 구조 선언이다.
		dummyRouter := func(_ context.Context, _ graph.State) string { return "" }

		for _, w := range workers {
			// 자기 자신을 제외한 다른 워커들로의 조건 엣지를 추가해 BFS 가 도달 가능으로 판단하게 한다.
			otherMapping := make(map[string]string, len(names)-1)
			for _, n := range names {
				if n != w.Name() {
					otherMapping[n] = n
				}
			}
			if len(otherMapping) == 0 {
				continue
			}
			if err := b.AddConditionalEdges(w.Name(), dummyRouter, otherMapping); err != nil {
				return nil, fmt.Errorf("multiagent: BuildNetwork — 조건 엣지 등록 실패(%q): %w", w.Name(), err)
			}
		}
	}

	// 첫 번째 워커를 진입점으로 설정한다.
	// 빈 목록인 경우 SetEntryPoint 를 호출하지 않으므로
	// Compile validate 에서 "진입점이 설정되지 않았습니다" 에러가 발생한다.
	if len(workers) > 0 {
		if err := b.SetEntryPoint(workers[0].Name()); err != nil {
			return nil, fmt.Errorf("multiagent: BuildNetwork — 진입점 설정 실패: %w", err)
		}
	}

	return b.Compile(graph.WithMaxSteps(100))
}

// buildWorkerNodeFunc 은 wk 를 실행하고 SelectNext 기반으로 다음 노드를 결정하는
// graph.NodeFunc 을 반환하는 내부 헬퍼다.
func buildWorkerNodeFunc(wk Worker) graph.NodeFunc {
	return func(ctx context.Context, st graph.State) (any, error) {
		// 현재 상태에서 메시지 추출
		var msgs []message.Message
		if raw, ok := st["messages"]; ok {
			if m, ok := raw.([]message.Message); ok {
				msgs = m
			}
		}

		// 워커 실행
		out, err := wk.Invoke(ctx, agent.Input{Messages: msgs}, config.RunConfig{})
		if err != nil {
			return nil, fmt.Errorf("multiagent: 워커 %q 실행 실패: %w", wk.Name(), err)
		}

		// 워커 산출 메시지를 상태에 병합할 Update 구성
		update := graph.StateUpdate{
			"messages": out.Messages,
		}
		if out.StructuredResponse != nil {
			update["structured_response"] = out.StructuredResponse
		}

		// 워커 출력 메시지를 포함한 전체 메시지 목록을 구성해 SelectNext 에 전달한다.
		allMsgs := message.AddMessages(msgs, out.Messages)
		nextSt := graph.State{"messages": allMsgs}

		next, err := SelectNext(ctx, nextSt)
		if err != nil {
			return nil, fmt.Errorf("multiagent: 워커 %q 실행 후 SelectNext 실패: %w", wk.Name(), err)
		}

		// 종료 판별: SelectNext 가 다음 워커를 반환하지 않으면 End 로 종료한다.
		if next == "" {
			return command.End(update), nil
		}

		// 다음 워커로 Goto(Update 도 함께 적용)
		return command.Command{
			Goto:   next,
			Update: update,
			Graph:  command.TargetCurrent,
		}, nil
	}
}

// IsFinalAnswer 는 메시지 m 이 종료 신호인지 판별한다(SPEC §5.6).
//
// 판별 규칙:
//   - 역할이 assistant(AI 메시지)이고 tool_calls 가 비어 있고 내용이 있으면 최종 답변 신호다.
//     수퍼바이저가 라우터 도구를 호출하지 않은 경우와 동일한 패턴이다(ANALYSIS §2).
//   - tool_calls 가 있는 AI 메시지는 아직 라우팅이 필요한 상태이므로 false 를 반환한다.
//   - AI 메시지가 아닌 경우(user/system/tool)는 false 를 반환한다.
func IsFinalAnswer(m message.Message) bool {
	return m.Role == message.RoleAssistant && len(m.ToolCalls) == 0 && m.Content != ""
}
