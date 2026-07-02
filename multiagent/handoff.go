// handoff.go 는 도구 기반 위임(핸드오프) 로직을 담는다.
// CreateHandoffTool 은 부모 그래프로 위임하는 tool.Tool 을 반환하고,
// HandoffBackMessages 는 워커→수퍼바이저 복귀 시 상태에 남길 AI/Tool 메시지 쌍을 만든다.
// 단일 위임은 command.ToParent, 복수 tool_calls 는 command.Fanout([]Send{TargetParent}) 를 사용한다.
// SPEC §5.5, ANALYSIS §2·§5.3·§5.4 참조.
package multiagent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/zipkero/langgraph-go/config"
	"github.com/zipkero/langgraph-go/graph"
	"github.com/zipkero/langgraph-go/graph/command"
	"github.com/zipkero/langgraph-go/message"
	"github.com/zipkero/langgraph-go/tool"
)

// HandoffTool 은 tool.Tool 을 구현하면서 command.Command 를 도출하는 핸드오프 전용 인터페이스다.
// CreateHandoffTool 이 반환하는 값은 이 인터페이스를 충족한다.
// NodeFunc 등에서 type assertion 으로 Command 메서드를 호출해 제어 흐름을 얻는다.
type HandoffTool interface {
	tool.Tool
	// Command 는 Runtime 컨텍스트를 받아 핸드오프 command.Command 를 반환한다.
	// state 의 마지막 AI 메시지 tool_calls 수로 단일/복수를 구분한다.
	//   - tool_calls 가 1개이면 command.ToParent(agentName, nil) 반환
	//   - tool_calls 가 2개 이상이면 command.Fanout([]Send{TargetParent, 워커마다 다른 query}) 반환
	Command(rt tool.Runtime) (command.Command, error)
}

// handoffArgs 는 핸드오프 도구가 받는 입력 구조체다.
// agentName 은 위임 대상 워커 이름, query 는 해당 워커에 전달할 요청이다.
type handoffArgs struct {
	// AgentName 은 위임할 워커/에이전트 이름이다.
	AgentName string `json:"agent_name" description:"위임할 워커(에이전트) 이름"`
	// Query 는 해당 워커에 전달할 요청 텍스트다.
	Query string `json:"query" description:"워커에 전달할 요청 텍스트"`
}

// handoffToolImpl 은 HandoffTool 인터페이스를 구현하는 핸드오프 도구 구현체다.
type handoffToolImpl struct {
	agentName string
	schema    tool.Schema
}

// CreateHandoffTool 은 agentName 을 위임 대상으로 하는 HandoffTool 을 반환한다.
// Execute 는 도구 실행 결과(tool.Result)를 반환하며,
// Command 는 Runtime 의 State 에서 마지막 AI 메시지 tool_calls 수를 보고
// 단일이면 command.ToParent, 복수이면 command.Fanout 을 반환한다.
func CreateHandoffTool(agentName, description string) HandoffTool {
	schema := tool.Schema{
		Name:        fmt.Sprintf("handoff_to_%s", agentName),
		Description: description,
		Parameters: []tool.Parameter{
			{
				Name:        "agent_name",
				Type:        "string",
				Description: "위임할 워커(에이전트) 이름",
				Required:    true,
			},
			{
				Name:        "query",
				Type:        "string",
				Description: "워커에 전달할 요청 텍스트",
				Required:    true,
			},
		},
	}
	return &handoffToolImpl{
		agentName: agentName,
		schema:    schema,
	}
}

func (h *handoffToolImpl) Name() string        { return h.schema.Name }
func (h *handoffToolImpl) Description() string { return h.schema.Description }
func (h *handoffToolImpl) Schema() tool.Schema { return h.schema }

// Execute 는 핸드오프 도구를 실행하고 위임 결과 텍스트를 반환한다.
// 실제 제어 흐름 변경(Command 생성)은 Command 메서드로 분리돼 있으므로,
// Execute 의 Result 는 도구 호출 응답 메시지에 포함되는 완료 텍스트만 담는다.
func (h *handoffToolImpl) Execute(_ context.Context, input tool.Input, _ tool.Runtime) (tool.Result, error) {
	var args handoffArgs
	if len(input) > 0 {
		if err := json.Unmarshal(input, &args); err != nil {
			return tool.Result{IsError: true, Content: fmt.Sprintf("핸드오프 도구 인자 파싱 실패: %v", err)}, nil
		}
	}
	targetAgent := args.AgentName
	if targetAgent == "" {
		targetAgent = h.agentName
	}
	return tool.Result{Content: fmt.Sprintf("%s 워커에 위임됩니다.", targetAgent)}, nil
}

// Command 는 rt.State() 에서 마지막 AI 메시지 tool_calls 수를 확인하고
// 단일 위임이면 command.ToParent, 복수이면 command.Fanout([]Send{TargetParent}) 를 반환한다.
// rt 가 nil 이거나 State() 가 []message.Message 를 담지 않으면 단일 위임으로 처리한다.
func (h *handoffToolImpl) Command(rt tool.Runtime) (command.Command, error) {
	// state 에서 마지막 AI 메시지 tool_calls 추출
	var toolCalls []message.ToolCall
	if rt != nil {
		toolCalls = extractToolCallsFromRuntime(rt)
	}

	// tool_calls 가 2개 이상이면 Fanout — 각 Send 가 TargetParent 대상, 워커마다 다른 query 분배
	if len(toolCalls) > 1 {
		sends := make([]command.Send, 0, len(toolCalls))
		for _, tc := range toolCalls {
			// 각 tool_call 의 args 에서 워커 이름과 query 를 추출해 Send.State 에 담는다.
			var args handoffArgs
			if len(tc.Args) > 0 {
				_ = json.Unmarshal(tc.Args, &args)
			}
			targetAgent := args.AgentName
			if targetAgent == "" {
				targetAgent = h.agentName
			}
			query := args.Query

			sends = append(sends, command.Send{
				Target: targetAgent,
				State: map[string]any{
					"messages": []message.Message{
						message.NewUserMessage(query),
					},
				},
				Graph: command.TargetParent,
			})
		}
		return command.Fanout(sends), nil
	}

	// tool_calls 가 0개 또는 1개이면 단일 위임 — ToParent
	return command.ToParent(h.agentName, nil), nil
}

// extractToolCallsFromRuntime 은 rt.State() 에서 "messages" 키를 꺼내
// 마지막 AI 메시지의 tool_calls 를 반환한다.
// 추출 실패 시 빈 슬라이스를 반환한다.
func extractToolCallsFromRuntime(rt tool.Runtime) []message.ToolCall {
	st := rt.State()
	if st == nil {
		return nil
	}
	stMap, ok := st.(map[string]any)
	if !ok {
		return nil
	}
	raw, ok := stMap["messages"]
	if !ok {
		return nil
	}
	msgs, ok := raw.([]message.Message)
	if !ok {
		return nil
	}
	lastAI, ok := message.LastAIMessage(msgs)
	if !ok {
		return nil
	}
	return message.ExtractToolCalls(lastAI)
}

// HandoffBackMessages 는 워커가 수퍼바이저로 복귀할 때 상태에 남길 AI/Tool 메시지 쌍을 만든다.
// 반환된 메시지를 상태에 추가하면 수퍼바이저가 워커 완료를 컨텍스트로 인지한다.
//
// 생성 규칙(SPEC §5.5):
//   - AI 메시지: agentName 도구를 호출한 tool_calls 를 담은 assistant 메시지.
//     toolCallID 는 쌍을 식별하는 임의 문자열이다(호출자가 지정).
//   - Tool 메시지: toolCallID, agentName, result 로 구성된 tool 역할 메시지.
//
// 두 메시지는 동일한 toolCallID 로 짝지어진다.
func HandoffBackMessages(agentName, toolCallID, result string) []message.Message {
	// AI 메시지: 워커 도구를 호출한 tool_calls
	aiMsg := message.NewAssistantToolCalls([]message.ToolCall{
		{
			ID:   toolCallID,
			Name: agentName,
			Args: json.RawMessage(`{}`),
		},
	})

	// Tool 메시지: 워커 실행 결과
	toolMsg := message.NewToolMessage(toolCallID, agentName, result)

	return []message.Message{aiMsg, toolMsg}
}

// HandoffToolNode 는 마지막 AI 메시지의 tool_calls 를 실행하고, 호출된 도구 중
// HandoffTool 이 있으면 그 Command(부모 그래프 위임)를 그래프 엔진에 전파하는
// graph.NodeFunc 를 반환한다. 핸드오프 도구가 없으면 ToolMessage 만 상태에 병합한다.
// 도구가 command 를 도출하는 create_agent 내부 ToolNode 패턴(LangGraph 파이썬)의 Go 대응이며,
// prebuilt.ToolNode 는 설계상 graph/command 를 참조하지 않으므로 Command 전파는 이 노드가 담당한다.
// 도구 Runtime 은 호출마다 tool_call ID 와 그래프 실행이 ctx 에 주입한 RunConfig 로 구성한다.
func HandoffToolNode(reg *tool.Registry) graph.NodeFunc {
	exec := tool.NewExecutor(reg)
	return func(ctx context.Context, st graph.State) (any, error) {
		msgs := messagesFromState(st)
		lastAI, ok := message.LastAIMessage(msgs)
		if !ok {
			return graph.StateUpdate{}, nil
		}
		calls := message.ExtractToolCalls(lastAI)
		if len(calls) == 0 {
			return graph.StateUpdate{}, nil
		}

		runCfg, _ := config.RunConfigFromContext(ctx)

		// 도구 실행 — 실행 오류를 에러 ToolMessage 로 변환하는 규칙은 ExecuteMany 와 동일.
		// 핸드오프 도구는 실행과 별개로 Command 도출용으로 수집한다(첫 번째 것 하나만 —
		// 복수 tool_calls 의 병렬 위임은 HandoffTool.Command 가 state 의 tool_calls 수로 감지해
		// Fanout 을 반환하므로 여기서 복수 Command 를 합치지 않는다).
		var handoff HandoffTool
		var handoffRT tool.Runtime
		toolMsgs := make([]message.Message, 0, len(calls))
		for _, call := range calls {
			rt := tool.NewRuntime(map[string]any(st), string(call.ID), runCfg, nil, nil)

			res, execErr := exec.Execute(ctx, call, rt)
			if execErr != nil {
				res = tool.Result{Content: execErr.Error(), IsError: true}
			}
			toolMsgs = append(toolMsgs, exec.BuildToolMessage(call, res))

			if handoff == nil {
				if t, found := reg.Get(call.Name); found {
					if ht, isHandoff := t.(HandoffTool); isHandoff {
						handoff = ht
						handoffRT = rt
					}
				}
			}
		}

		merged := message.AddMessages(msgs, toolMsgs)

		// 핸드오프 도구가 없으면 일반 ToolNode 와 동일하게 메시지 병합만 반환한다.
		if handoff == nil {
			return graph.StateUpdate{"messages": merged}, nil
		}

		cmd, err := handoff.Command(handoffRT)
		if err != nil {
			return nil, fmt.Errorf("multiagent: HandoffToolNode — Command 도출 실패: %w", err)
		}
		// 도구 호출/응답 쌍이 부모 상태에 남도록 Update 에 병합 메시지를 싣는다.
		// Command 가 자체 messages 업데이트를 지정했으면 그것을 우선한다.
		if cmd.Update == nil {
			cmd.Update = graph.StateUpdate{}
		}
		if _, exists := cmd.Update["messages"]; !exists {
			cmd.Update["messages"] = merged
		}
		return cmd, nil
	}
}
