// adapter.go 는 에이전트와 서브그래프를 그래프 노드·도구로 노출하는 얇은 어댑터를 담는다.
// agent의 Invoke/Stream과 graph의 AsNode를 호출만 하며 재배치 리팩터링을 하지 않는다(SPEC §3·§4).
package multiagent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/zipkero/langgraph-go/agent"
	"github.com/zipkero/langgraph-go/config"
	"github.com/zipkero/langgraph-go/graph"
	"github.com/zipkero/langgraph-go/message"
	"github.com/zipkero/langgraph-go/tool"
)

// AgentAsNode 는 *agent.Agent 를 graph.NodeFunc 로 감싸 반환한다.
// 노드가 실행되면 현재 상태에서 "messages" 키를 꺼내 에이전트에 전달하고,
// 에이전트 Invoke 결과를 "messages"·"structured_response" 업데이트로 반환한다.
func AgentAsNode(a *agent.Agent) graph.NodeFunc {
	return func(ctx context.Context, st graph.State) (any, error) {
		// 현재 상태에서 메시지 추출
		var msgs []message.Message
		if raw, ok := st["messages"]; ok {
			if m, ok := raw.([]message.Message); ok {
				msgs = m
			}
		}

		result, err := a.Invoke(ctx, agent.Input{Messages: msgs}, config.RunConfig{})
		if err != nil {
			return nil, fmt.Errorf("multiagent: AgentAsNode Invoke 실패: %w", err)
		}

		update := graph.StateUpdate{
			"messages": result.Messages,
		}
		if result.StructuredResponse != nil {
			update["structured_response"] = result.StructuredResponse
		}
		return update, nil
	}
}

// GraphAsNode 는 *graph.Compiled 를 graph.NodeFunc 로 감싸 반환한다.
// 내부적으로 Compiled.AsNode() 를 호출하는 얇은 래퍼이며,
// 재배치 리팩터링 없이 기존 서브그래프 어댑터 기능을 위임한다.
func GraphAsNode(g *graph.Compiled) graph.NodeFunc {
	return g.AsNode()
}

// agentToolArgs 는 AgentAsTool 도구가 받는 입력 구조다.
type agentToolArgs struct {
	// Input 은 에이전트에 전달할 사용자 메시지 텍스트다.
	Input string `json:"input" description:"에이전트에 전달할 입력 텍스트"`
}

// AgentAsTool 은 *agent.Agent 를 tool.Tool 로 감싸 반환한다.
// Execute 시 Input 필드를 UserMessage 로 변환해 에이전트를 Invoke 하고,
// 마지막 AI 메시지 내용을 도구 결과로 반환한다.
func AgentAsTool(a *agent.Agent, name, desc string) tool.Tool {
	return tool.WithArgsSchema(name, desc, func(ctx context.Context, args agentToolArgs, rt tool.Runtime) (tool.Result, error) {
		userMsg := message.NewUserMessage(args.Input)

		result, err := a.Invoke(ctx, agent.Input{Messages: []message.Message{userMsg}}, config.RunConfig{})
		if err != nil {
			return tool.Result{IsError: true, Content: fmt.Sprintf("에이전트 실행 실패: %v", err)}, nil
		}

		// 마지막 AI 메시지 내용을 도구 출력으로 반환한다.
		lastAI, ok := message.LastAIMessage(result.Messages)
		if !ok {
			// AI 메시지가 없으면 전체 메시지를 JSON 직렬화해 반환한다.
			raw, _ := json.Marshal(result.Messages)
			return tool.Result{Content: string(raw)}, nil
		}
		return tool.Result{Content: lastAI.Content}, nil
	})
}
