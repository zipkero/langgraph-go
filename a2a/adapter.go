// adapter.go 는 agent.Agent 실행 스트림을 A2A 태스크 수명주기로 매핑하는 어댑터를 구현한다.
// StreamToTaskUpdates 는 에이전트 스트림 이벤트를 TaskUpdater 전이·아티팩트로 변환한다(README §22-4).
// import 방향: a2a → agent·config·structured·message·core + 표준 라이브러리(SPEC §3, README §28-1).
package a2a

import (
	"context"
	"fmt"

	"github.com/zipkero/langgraph-go/agent"
	"github.com/zipkero/langgraph-go/config"
	"github.com/zipkero/langgraph-go/core"
	"github.com/zipkero/langgraph-go/message"
	"github.com/zipkero/langgraph-go/structured"
)

// StreamToTaskUpdates 는 agent.Agent 실행 스트림을 소비해 TaskUpdater로 태스크 수명주기를 매핑한다.
//
// 매핑 규칙(SPEC §5.6, README §22-4):
//
//   - 기본 경로 (AgentEvent 플래그):
//     - 진행 중 이벤트(IsTaskComplete·RequireUserInput 모두 false) → UpdateStatus(working)
//     - RequireUserInput=true → UpdateStatus(input_required, Final(true))
//     - IsTaskComplete=true → Content를 아티팩트로 AddArtifact 후 Complete()
//     - AgentEvent.Error != nil → setFailed (failed, Final=true)
//     - ctx 취소 → Cancel(canceled)
//
//   - 대체 경로 (structured.AgentStatus 단언 가능한 경우):
//     - IsTaskComplete 이벤트에서 StructuredResponse가 AgentStatus로 단언되면 대체 경로 진입
//     - AgentStatus.Status="completed" → AddArtifact(Message) + Complete()
//     - AgentStatus.Status="input_required" → UpdateStatus(input_required, Final(true))
//     - AgentStatus.Status="error" → UpdateStatus(input_required, Final(true))  (error는 input_required로)
//
// 반환: 스트림이 정상 종료되면 nil, 실행기 스트림 시작 실패면 error를 반환한다.
// ctx 취소 시에는 태스크를 canceled로 전이한 뒤 ctx.Err()를 반환한다.
func StreamToTaskUpdates(ctx context.Context, a *agent.Agent, query, sessionID string, u *TaskUpdater) error {
	// query를 사용자 메시지로 감싸고 세션 식별자를 thread_id로 전달한다.
	in := agent.Input{
		Messages: []message.Message{
			message.NewUserMessage(query),
		},
	}
	runCfg := config.RunConfig{
		Configurable: map[string]any{
			"thread_id": sessionID,
		},
	}

	// 에이전트 스트림을 시작한다. 시작 실패 시 즉시 failed로 전이한다.
	ch, err := a.Stream(ctx, in, runCfg, core.ModeMessages)
	if err != nil {
		failMsg := NewAgentTextMessage(fmt.Sprintf("에이전트 스트림 시작 실패: %s", err.Error()), "", "")
		u.UpdateStatus(TaskStateFailed, failMsg, Final(true))
		return err
	}

	// 이벤트 루프: AgentEvent를 소비해 TaskUpdater로 전이·아티팩트를 매핑한다.
	for {
		select {
		case <-ctx.Done():
			// ctx 취소 → canceled 전이
			cancelMsg := NewAgentTextMessage("컨텍스트 취소로 태스크가 중단됐습니다", "", "")
			u.Cancel(cancelMsg)
			return ctx.Err()

		case ev, ok := <-ch:
			if !ok {
				// 채널이 닫혔다 = 스트림 정상 종료(IsTaskComplete 이벤트 처리 후)
				return nil
			}

			if err := handleAgentEvent(ev, u); err != nil {
				return err
			}
		}
	}
}

// handleAgentEvent 는 단일 AgentEvent를 TaskUpdater 전이로 매핑한다.
// 최종 전이(Complete·input_required final·failed)가 발생하면 nil을 반환한다.
// 실행기 예외(ev.Error != nil)인 경우에도 failed 전이 후 nil을 반환한다(호출자가 채널 소비 종료).
func handleAgentEvent(ev agent.AgentEvent, u *TaskUpdater) error {
	// 실행기 예외 → failed 전이
	if ev.Error != nil {
		u.setFailed(ev.Error)
		return nil
	}

	// 완료 이벤트 처리: 기본 경로 또는 대체 경로(structured.AgentStatus)
	if ev.IsTaskComplete {
		return handleCompleteEvent(ev, u)
	}

	// 추가 입력 필요 → input_required(final)
	if ev.RequireUserInput {
		inputMsg := NewAgentTextMessage("추가 입력이 필요합니다", "", "")
		if ev.Content != "" {
			inputMsg = NewAgentTextMessage(ev.Content, "", "")
		}
		u.UpdateStatus(TaskStateInputRequired, inputMsg, Final(true))
		return nil
	}

	// 진행 중 이벤트 → working
	workingMsg := NewAgentTextMessage(ev.Content, "", "")
	u.UpdateStatus(TaskStateWorking, workingMsg)
	return nil
}

// handleCompleteEvent 는 IsTaskComplete=true 이벤트를 기본 경로 또는 대체 경로로 처리한다.
//
// 대체 경로: ev.Update["structured_response"]가 *structured.AgentStatus로 단언되는 경우.
// 기본 경로: 그 외 모든 경우 — Content를 아티팩트로 추가하고 Complete()를 호출한다.
func handleCompleteEvent(ev agent.AgentEvent, u *TaskUpdater) error {
	// 대체 경로 탐색: Update 맵에서 structured_response를 꺼내 AgentStatus로 단언한다.
	if ev.Update != nil {
		if sr, ok := ev.Update["structured_response"]; ok && sr != nil {
			if status, ok := toAgentStatus(sr); ok {
				return handleStructuredStatus(status, ev, u)
			}
		}
	}

	// 기본 경로: Content를 텍스트 아티팩트로 추가하고 Complete()
	parts := []Part{
		{Text: &TextPart{Text: ev.Content}},
	}
	u.AddArtifact(parts, "응답")
	u.Complete()
	return nil
}

// toAgentStatus 는 any 값을 *structured.AgentStatus로 변환 시도한다.
// 직접 단언과 포인터 단언 두 가지를 모두 시도한다.
func toAgentStatus(v any) (*structured.AgentStatus, bool) {
	if s, ok := v.(structured.AgentStatus); ok {
		return &s, true
	}
	if s, ok := v.(*structured.AgentStatus); ok && s != nil {
		return s, true
	}
	return nil, false
}

// handleStructuredStatus 는 structured.AgentStatus 대체 경로를 처리한다.
//
// 매핑(README §22-4, ANALYSIS §5 D5.4):
//   - "completed" → AddArtifact(AgentStatus.Message) + Complete()
//   - "input_required" → UpdateStatus(input_required, Final(true))
//   - "error" → UpdateStatus(input_required, Final(true))  (error는 input_required로 흐른다)
func handleStructuredStatus(status *structured.AgentStatus, ev agent.AgentEvent, u *TaskUpdater) error {
	switch status.Status {
	case "completed":
		// 구조화 응답의 Message 내용을 아티팩트로 추가한다.
		content := status.Message
		if content == "" {
			content = ev.Content
		}
		parts := []Part{
			{Text: &TextPart{Text: content}},
		}
		u.AddArtifact(parts, "응답")
		u.Complete()

	case "input_required", "error":
		// error는 input_required로 흐른다(ANALYSIS §5 D5.4, README §22-4).
		msg := status.Message
		if msg == "" {
			msg = "추가 입력이 필요합니다"
		}
		inputMsg := NewAgentTextMessage(msg, "", "")
		u.UpdateStatus(TaskStateInputRequired, inputMsg, Final(true))

	default:
		// 알 수 없는 상태는 기본 경로로 처리한다.
		parts := []Part{
			{Text: &TextPart{Text: ev.Content}},
		}
		u.AddArtifact(parts, "응답")
		u.Complete()
	}

	return nil
}
