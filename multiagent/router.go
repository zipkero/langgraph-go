// router.go 는 수퍼바이저 라우팅 로직을 담는다.
// RouterTool(라우터 도구 생성), SelectNext(tool_calls에서 다음 워커 이름 추출),
// Route(graph.State 위에서 command.Command 분기 반환),
// MergeWorkerResult(WorkerOutput을 상태에 병합)를 제공한다.
// SPEC §5.4, ANALYSIS §2·§5.1·§5.2·§5.5 참조.
package multiagent

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/zipkero/langgraph-go/graph"
	"github.com/zipkero/langgraph-go/graph/command"
	"github.com/zipkero/langgraph-go/message"
	"github.com/zipkero/langgraph-go/structured"
	"github.com/zipkero/langgraph-go/tool"
)

// routerArgs 는 라우터 도구가 받는 입력 구조체다.
// next 필드에 다음 워커 이름을 담는다.
type routerArgs struct {
	// Next 는 다음으로 이동할 워커 이름이다.
	Next string `json:"next" description:"다음으로 이동할 워커 이름"`
}

// RouterTool 은 허용된 워커 이름 목록을 choices로 받아 라우터 tool.Tool을 반환한다.
// 수퍼바이저가 이 도구를 BindTools로 바인딩한 뒤 LLM을 호출하면,
// 모델이 라우터 도구 호출로 next 워커를 선택한다.
// choices가 비어 있으면 enum 제약 없이 임의 문자열을 허용한다.
func RouterTool(choices ...string) tool.Tool {
	// 허용 목록 기반 description 생성
	desc := "다음으로 위임할 워커를 선택한다."
	if len(choices) > 0 {
		desc += " 허용값: " + strings.Join(choices, ", ")
	}

	// tool.WithArgsSchema를 쓰면 구조체 태그에서 스키마를 자동 도출한다.
	// choices 기반 enum 제약은 Schema를 직접 구성해 funcTool로 감싼다.
	return newRouterTool("route_to_worker", desc, choices)
}

// newRouterTool 은 RouterTool의 내부 구현이다.
// choices가 있으면 structured.RouterChoiceSchema로 enum 제약을 반영한다.
func newRouterTool(name, desc string, choices []string) tool.Tool {
	// 스키마 파라미터: next 필드(string 또는 enum)
	params := []tool.Parameter{
		{
			Name:        "next",
			Type:        "string",
			Description: "다음으로 이동할 워커 이름",
			Required:    true,
		},
	}
	schema := tool.Schema{
		Name:        name,
		Description: desc,
		Parameters:  params,
	}

	execFn := func(ctx context.Context, input tool.Input, rt tool.Runtime) (tool.Result, error) {
		var args routerArgs
		if len(input) > 0 {
			if err := json.Unmarshal(input, &args); err != nil {
				return tool.Result{IsError: true, Content: fmt.Sprintf("라우터 도구 인자 파싱 실패: %v", err)}, nil
			}
		}
		// choices 제약 검증
		if len(choices) > 0 && !slices.Contains(choices, args.Next) {
			return tool.Result{IsError: true, Content: fmt.Sprintf("라우터 도구: 허용되지 않은 워커 이름 %q (허용: %s)", args.Next, strings.Join(choices, ", "))}, nil
		}
		// 실행 결과는 라우터가 직접 사용하지 않는다. SelectNext가 tool_calls.Args를 파싱한다.
		return tool.Result{Content: args.Next}, nil
	}

	return &routerToolImpl{schema: schema, fn: execFn}
}

// routerToolImpl 은 RouterTool이 반환하는 Tool 구현체다.
type routerToolImpl struct {
	schema tool.Schema
	fn     func(ctx context.Context, input tool.Input, rt tool.Runtime) (tool.Result, error)
}

func (t *routerToolImpl) Name() string        { return t.schema.Name }
func (t *routerToolImpl) Description() string { return t.schema.Description }
func (t *routerToolImpl) Schema() tool.Schema { return t.schema }
func (t *routerToolImpl) Execute(ctx context.Context, input tool.Input, rt tool.Runtime) (tool.Result, error) {
	return t.fn(ctx, input, rt)
}

// SelectNext 는 st에서 마지막 AI 메시지의 tool_calls[0].Args를
// structured.RouterChoice[string]으로 해석해 next 워커 이름을 반환한다.
// 라우터 도구 호출이 없으면 ("", nil)을 반환한다(Route가 종료 판단).
// args 파싱 실패는 에러로 반환한다.
func SelectNext(_ context.Context, st graph.State) (string, error) {
	msgs := messagesFromState(st)
	lastAI, ok := message.LastAIMessage(msgs)
	if !ok {
		return "", nil
	}

	calls := message.ExtractToolCalls(lastAI)
	if len(calls) == 0 {
		return "", nil
	}

	// tool_calls[0].Args를 RouterChoice로 해석
	var choice structured.RouterChoice[string]
	if err := json.Unmarshal(calls[0].Args, &choice); err != nil {
		return "", fmt.Errorf("multiagent: SelectNext — 라우터 args 파싱 실패: %w", err)
	}

	return choice.Next, nil
}

// Route 는 st에서 SelectNext를 호출해 다음 워커를 결정하고
// 해당 노드로 command.Goto를 반환한다.
// 라우터 도구 호출이 없으면 command.End를 반환한다.
// reg가 nil이 아니면 워커 이름이 등록되지 않은 경우 에러를 반환한다(ANALYSIS §5.2).
func Route(ctx context.Context, st graph.State, reg *WorkerRegistry) (command.Command, error) {
	next, err := SelectNext(ctx, st)
	if err != nil {
		return command.Command{}, err
	}

	// 라우터 미호출: 종료
	if next == "" {
		return command.End(nil), nil
	}

	// 미등록 워커 이름 검증
	if reg != nil {
		if _, ok := reg.GetWorker(next); !ok {
			return command.Command{}, fmt.Errorf("multiagent: Route — 미등록 워커 이름 %q", next)
		}
	}

	return command.Goto(next, nil), nil
}

// MergeWorkerResult 는 out(WorkerOutput)의 메시지를 st의 "messages" 키에 병합해
// 갱신된 graph.State를 반환한다(ANALYSIS §5.5 — 코드 머지 없이 상태 병합만 담당).
// StructuredResponse가 있으면 "structured_response" 키에도 추가한다.
func MergeWorkerResult(st graph.State, out WorkerOutput) graph.State {
	// 기존 상태 복사
	result := make(graph.State, len(st)+2)
	maps.Copy(result, st)

	// 기존 메시지 목록 가져오기
	base := messagesFromState(st)
	// 워커 산출 메시지 병합
	merged := message.AddMessages(base, out.Messages)
	result["messages"] = merged

	if out.StructuredResponse != nil {
		result["structured_response"] = out.StructuredResponse
	}

	return result
}

// messagesFromState 는 st["messages"]를 []message.Message로 추출한다.
// 키가 없거나 타입이 맞지 않으면 빈 슬라이스를 반환한다.
func messagesFromState(st graph.State) []message.Message {
	if raw, ok := st["messages"]; ok {
		if msgs, ok := raw.([]message.Message); ok {
			return msgs
		}
	}
	return []message.Message{}
}
