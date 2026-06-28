// prebuilt 패키지는 사전 구성된 노드·조건·요약 헬퍼를 제공한다.
// message, tool, llm, core에 의존하며, graph/command/streaming은 참조하지 않는다(§1-3, D1).
// 노드 함수 타입은 core.State/core.StateUpdate 기반 로컬 타입이다.
package prebuilt

import (
	"context"
	"fmt"
	"strings"

	"github.com/zipkero/langgraph-go/config"
	"github.com/zipkero/langgraph-go/core"
	"github.com/zipkero/langgraph-go/llm"
	"github.com/zipkero/langgraph-go/message"
	"github.com/zipkero/langgraph-go/tool"
)

// NodeFunc 는 prebuilt 노드 함수 타입이다.
// Phase 2가 도입되면 graph.NodeFunc로 흡수·정합될 로컬 타입이다(D1).
type NodeFunc func(ctx context.Context, st core.State) (core.StateUpdate, error)

// messagesKey 는 상태에서 메시지 목록을 읽고 쓸 때 사용하는 관례적 키다.
// agent.State의 메시지 키와 일치해야 한다.
const messagesKey = "messages"

// getMessages 는 상태에서 메시지 목록을 꺼낸다.
// 키가 없거나 타입이 맞지 않으면 빈 슬라이스를 반환한다.
func getMessages(st core.State) []message.Message {
	v, ok := st[messagesKey]
	if !ok {
		return nil
	}
	msgs, ok := v.([]message.Message)
	if !ok {
		return nil
	}
	return msgs
}

// NewToolNode 는 마지막 AI 메시지의 미처리 tool_calls 를 실행하고
// ToolMessage 목록을 상태에 추가하는 NodeFunc 를 반환한다.
// reg 에 등록된 도구를 이름으로 찾아 Executor 로 디스패치한다.
// 상태의 "messages" 키에서 메시지를 읽고, StateUpdate 로 추가된 ToolMessage 를 반환한다.
// 호출자/agent 가 리듀서(AddMessages)로 상태에 병합한다.
func NewToolNode(reg *tool.Registry) NodeFunc {
	exec := tool.NewExecutor(reg)
	return func(ctx context.Context, st core.State) (core.StateUpdate, error) {
		msgs := getMessages(st)

		// 마지막 AI 메시지 조회
		aiMsg, ok := message.LastAIMessage(msgs)
		if !ok {
			return core.StateUpdate{}, nil
		}

		// 미처리 tool_calls 추출
		toolCalls := message.ExtractToolCalls(aiMsg)
		if len(toolCalls) == 0 {
			return core.StateUpdate{}, nil
		}

		// 도구 실행 — Runtime은 nil store·no-op emit으로 최소 주입
		rt := tool.NewRuntime(st, "", config.RunConfig{}, nil, nil)
		toolMsgs, err := exec.ExecuteMany(ctx, toolCalls, rt)
		if err != nil {
			return core.StateUpdate{}, fmt.Errorf("prebuilt.ToolNode: 도구 실행 실패: %w", err)
		}

		// StateUpdate: 기존 메시지에 ToolMessage 들을 append한 전체 목록 반환
		// 호출자가 리듀서로 병합하는 모델이므로, 새 메시지만 담은 슬라이스를 반환한다.
		return core.StateUpdate{
			messagesKey: message.AddMessages(msgs, toolMsgs),
		}, nil
	}
}

// HasPendingToolCalls 는 상태의 마지막 AI 메시지에 미처리 tool_calls 가 있는지 반환한다.
func HasPendingToolCalls(st core.State) bool {
	msgs := getMessages(st)
	aiMsg, ok := message.LastAIMessage(msgs)
	if !ok {
		return false
	}
	return message.HasToolCalls(aiMsg)
}

// ToolsCondition 은 미처리 도구 호출 유무로 라우팅 목적지를 반환하는 조건 함수다.
// 미처리 tool_calls 가 있으면 "tools", 없으면 "END" 를 반환한다.
// Phase 2의 graph.ConditionalEdge 에서 NodeFunc 대신 이 함수를 직접 사용할 수 있다.
func ToolsCondition(_ context.Context, st core.State) string {
	if HasPendingToolCalls(st) {
		return "tools"
	}
	return "END"
}

// SummarizeOptions 는 SummarizationNode 와 ShouldSummarize 의 옵션을 담는다.
type SummarizeOptions struct {
	// MaxMessages 는 요약을 트리거할 최대 메시지 수 임계값이다.
	// 0이면 메시지 수 기준 임계를 사용하지 않는다.
	MaxMessages int
	// MaxTokens 는 요약을 트리거할 최대 토큰 수 임계값이다.
	// 0이면 토큰 기준 임계를 사용하지 않는다.
	MaxTokens int
	// KeepLast 는 요약 후 보존할 최근 메시지 수다.
	// 0이면 요약에 반영된 메시지를 모두 제거한다.
	KeepLast int
	// SummaryKey 는 상태에서 요약 문자열을 저장·조회할 키다.
	// 비어 있으면 기본값 "summary" 를 사용한다.
	SummaryKey string
}

// summaryKey 는 SummarizeOptions 에서 실제 사용할 키를 반환한다.
func (o SummarizeOptions) summaryKey() string {
	if o.SummaryKey != "" {
		return o.SummaryKey
	}
	return "summary"
}

// ShouldSummarize 는 상태의 메시지가 opts 의 임계(메시지 수/토큰)를 초과하는지 판정한다.
// MaxMessages 와 MaxTokens 중 하나라도 초과하면 true 를 반환한다.
// 두 값이 모두 0이면 항상 false 를 반환한다.
func ShouldSummarize(st core.State, opts SummarizeOptions) bool {
	msgs := getMessages(st)
	if len(msgs) == 0 {
		return false
	}
	if opts.MaxMessages > 0 && len(msgs) > opts.MaxMessages {
		return true
	}
	if opts.MaxTokens > 0 && message.CountTokensApprox(msgs) > opts.MaxTokens {
		return true
	}
	return false
}

// InjectSummary 는 저장된 요약을 SystemMessage 로 msgs 앞에 주입한 새 슬라이스를 반환한다.
// summary 가 비어 있으면 msgs 를 그대로 반환한다.
func InjectSummary(msgs []message.Message, summary string) []message.Message {
	if summary == "" {
		return msgs
	}
	sysMsg := message.NewSystemMessage(fmt.Sprintf("[이전 대화 요약]\n%s", summary))
	result := make([]message.Message, 0, 1+len(msgs))
	result = append(result, sysMsg)
	result = append(result, msgs...)
	return result
}

// buildSummaryPrompt 는 msgs 를 요약 요청 프롬프트로 변환한다.
func buildSummaryPrompt(msgs []message.Message, existingSummary string) []message.Message {
	var sb strings.Builder
	if existingSummary != "" {
		sb.WriteString("이전 요약:\n")
		sb.WriteString(existingSummary)
		sb.WriteString("\n\n")
	}
	sb.WriteString("다음 대화를 간결하게 요약해 주세요:\n\n")
	for _, m := range msgs {
		sb.WriteString(message.PrettyPrint(m))
		sb.WriteString("\n")
	}

	return []message.Message{
		message.NewUserMessage(sb.String()),
	}
}

// NewSummarizationNode 는 메시지 임계 초과 시 누적 대화를 LLM 으로 요약하고,
// 요약을 상태에 저장하며, 요약에 반영된 과거 메시지를 제거하는 NodeFunc 를 반환한다.
// opts.KeepLast 만큼 최근 메시지를 보존하고 나머지를 RemoveMessage+ApplyRemovals 로 정리한다.
// 반환하는 StateUpdate 는 호출자/agent 가 리듀서로 상태에 병합한다.
func NewSummarizationNode(model llm.Client, opts SummarizeOptions) NodeFunc {
	return func(ctx context.Context, st core.State) (core.StateUpdate, error) {
		if !ShouldSummarize(st, opts) {
			return core.StateUpdate{}, nil
		}

		msgs := getMessages(st)

		// 기존 요약 조회
		existingSummary := ""
		if v, ok := st[opts.summaryKey()]; ok {
			if s, ok := v.(string); ok {
				existingSummary = s
			}
		}

		// 요약 대상 범위 결정
		// KeepLast 만큼 최근 메시지를 보존하고, 나머지를 요약 대상으로 한다.
		keepLast := opts.KeepLast
		if keepLast < 0 {
			keepLast = 0
		}

		var toSummarize []message.Message
		var toKeep []message.Message
		if keepLast > 0 && len(msgs) > keepLast {
			toSummarize = msgs[:len(msgs)-keepLast]
			toKeep = msgs[len(msgs)-keepLast:]
		} else if keepLast == 0 {
			toSummarize = msgs
			toKeep = nil
		} else {
			// keepLast >= len(msgs): 요약할 메시지 없음
			return core.StateUpdate{}, nil
		}

		if len(toSummarize) == 0 {
			return core.StateUpdate{}, nil
		}

		// LLM 으로 요약 생성
		promptMsgs := buildSummaryPrompt(toSummarize, existingSummary)
		resp, err := model.Chat(ctx, llm.ChatRequest{Messages: promptMsgs})
		if err != nil {
			return core.StateUpdate{}, fmt.Errorf("prebuilt.SummarizationNode: 요약 생성 실패: %w", err)
		}
		newSummary := resp.Message.Content

		// 요약 대상 메시지 제거:
		// - ID 가 있는 메시지: RemoveMessage 마커 + ApplyRemovals 로 제거
		// - ID 없는 메시지: toKeep 슬라이스로 직접 교체
		// 두 방식을 혼합하면 복잡해지므로, toSummarize 에 ID 없는 메시지가 하나라도 있으면
		// 전체를 toKeep 으로 교체하는 단순 전략을 사용한다.
		var newMsgs []message.Message
		if len(toKeep) > 0 {
			hasNoID := false
			for _, m := range toSummarize {
				if m.ID == "" {
					hasNoID = true
					break
				}
			}
			if hasNoID {
				// ID 없는 메시지 포함 → toKeep 으로 직접 교체
				newMsgs = toKeep
			} else {
				// 모두 ID 있음 → RemoveMessage 마커 + ApplyRemovals 로 제거
				withRemovals := make([]message.Message, 0, len(msgs)+len(toSummarize))
				withRemovals = append(withRemovals, msgs...)
				for _, m := range toSummarize {
					withRemovals = append(withRemovals, message.RemoveMessage(m.ID))
				}
				newMsgs = message.ApplyRemovals(withRemovals)
			}
		} else {
			// 전체 제거
			newMsgs = []message.Message{}
		}

		update := core.StateUpdate{
			messagesKey:       newMsgs,
			opts.summaryKey(): newSummary,
		}
		return update, nil
	}
}

