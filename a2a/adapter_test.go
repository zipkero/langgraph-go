// adapter_test.go 는 StreamToTaskUpdates 어댑터의 단위 테스트를 담는다.
// stub TaskUpdater와 내부 handleAgentEvent 함수를 활용해 외부 LLM·원격 에이전트 없이
// 기본 경로·대체 경로 각 전이를 결정적으로 검증한다(SPEC §5.6, implement.md task-004).
package a2a

import (
	"errors"
	"testing"

	"github.com/zipkero/langgraph-go/agent"
	"github.com/zipkero/langgraph-go/core"
	"github.com/zipkero/langgraph-go/structured"
)

// ─── stub 헬퍼 ───────────────────────────────────────────────────────────────

// stubTaskUpdater 는 테스트 전용 TaskUpdater 대체다.
// 실제 TaskUpdater는 버퍼드 채널과 TaskStore를 요구하므로,
// 전이 기록만 남기는 stub을 별도로 구성한다.
//
// 단, handleAgentEvent/handleCompleteEvent/handleStructuredStatus 는
// *TaskUpdater를 인자로 받으므로 실제 TaskUpdater를 사용해야 한다.
// 여기서는 newBufferedEventQueue + InMemoryTaskStore + newTaskUpdater 조합으로
// 실제 TaskUpdater를 생성하고 결과를 TaskStore에서 읽어 단정한다.
func newTestUpdater() (*TaskUpdater, *InMemoryTaskStore, *bufferedEventQueue) {
	store := NewInMemoryTaskStore()
	queue := newBufferedEventQueue(64)
	task := NewTask(Message{
		Role:  RoleUser,
		Parts: []Part{{Text: &TextPart{Text: "테스트 질의"}}},
	})
	store.SaveTask(task)
	updater := newTaskUpdater(task, queue, store)
	return updater, store, queue
}

// drainQueue 는 버퍼드 큐에 쌓인 이벤트를 모두 꺼내 슬라이스로 반환한다.
// 큐를 닫지 않으므로 채널을 직접 비운다.
func drainQueue(q *bufferedEventQueue) []Event {
	var evs []Event
	for {
		select {
		case ev, ok := <-q.ch:
			if !ok {
				return evs
			}
			evs = append(evs, ev)
		default:
			return evs
		}
	}
}

// ─── 기본 경로: 진행 중 이벤트 → working ─────────────────────────────────────

func TestHandleAgentEvent_Working(t *testing.T) {
	u, store, q := newTestUpdater()

	// IsTaskComplete·RequireUserInput 모두 false인 진행 중 이벤트
	ev := agent.AgentEvent{
		Content: "처리 중...",
	}

	if err := handleAgentEvent(ev, u); err != nil {
		t.Fatalf("handleAgentEvent 반환 오류: %v", err)
	}

	// 큐에 working 상태 갱신 이벤트가 있어야 한다
	evs := drainQueue(q)
	if len(evs) != 1 {
		t.Fatalf("이벤트 수 기대=1, 실제=%d", len(evs))
	}
	if evs[0].StatusUpdate == nil {
		t.Fatal("StatusUpdate 이벤트가 없습니다")
	}
	if evs[0].StatusUpdate.Status.State != TaskStateWorking {
		t.Errorf("상태 기대=%s, 실제=%s", TaskStateWorking, evs[0].StatusUpdate.Status.State)
	}
	if evs[0].StatusUpdate.Final {
		t.Error("working 이벤트는 Final이 false여야 합니다")
	}

	// 저장소에도 working 상태가 반영돼야 한다
	task, ok := store.GetTask(u.task.ID)
	if !ok {
		t.Fatal("태스크를 찾을 수 없습니다")
	}
	if task.Status.State != TaskStateWorking {
		t.Errorf("저장소 상태 기대=%s, 실제=%s", TaskStateWorking, task.Status.State)
	}
}

// ─── 기본 경로: 추가 입력 필요 → input_required(final) ────────────────────────

func TestHandleAgentEvent_RequireUserInput(t *testing.T) {
	u, store, q := newTestUpdater()

	ev := agent.AgentEvent{
		RequireUserInput: true,
		Content:          "사용자 확인이 필요합니다",
	}

	if err := handleAgentEvent(ev, u); err != nil {
		t.Fatalf("handleAgentEvent 반환 오류: %v", err)
	}

	evs := drainQueue(q)
	if len(evs) != 1 {
		t.Fatalf("이벤트 수 기대=1, 실제=%d", len(evs))
	}
	su := evs[0].StatusUpdate
	if su == nil {
		t.Fatal("StatusUpdate 이벤트가 없습니다")
	}
	if su.Status.State != TaskStateInputRequired {
		t.Errorf("상태 기대=%s, 실제=%s", TaskStateInputRequired, su.Status.State)
	}
	if !su.Final {
		t.Error("input_required 이벤트는 Final=true여야 합니다")
	}

	task, ok := store.GetTask(u.task.ID)
	if !ok {
		t.Fatal("태스크를 찾을 수 없습니다")
	}
	if task.Status.State != TaskStateInputRequired {
		t.Errorf("저장소 상태 기대=%s, 실제=%s", TaskStateInputRequired, task.Status.State)
	}
}

// ─── 기본 경로: 완료 → AddArtifact + Complete ─────────────────────────────────

func TestHandleAgentEvent_Complete(t *testing.T) {
	u, store, q := newTestUpdater()

	ev := agent.AgentEvent{
		IsTaskComplete: true,
		Content:        "작업이 완료됐습니다",
	}

	if err := handleAgentEvent(ev, u); err != nil {
		t.Fatalf("handleAgentEvent 반환 오류: %v", err)
	}

	evs := drainQueue(q)
	// 아티팩트 갱신 이벤트 + 완료 상태 갱신 이벤트 두 개가 있어야 한다
	if len(evs) != 2 {
		t.Fatalf("이벤트 수 기대=2 (아티팩트+완료), 실제=%d", len(evs))
	}

	// 첫 번째: 아티팩트 갱신
	if evs[0].ArtifactUpdate == nil {
		t.Error("첫 번째 이벤트가 ArtifactUpdate여야 합니다")
	} else {
		// 아티팩트에 Content가 담겨야 한다
		txt, ok := ArtifactText(evs[0].ArtifactUpdate.Artifact)
		if !ok {
			t.Error("아티팩트에 텍스트 파트가 없습니다")
		} else if txt != "작업이 완료됐습니다" {
			t.Errorf("아티팩트 텍스트 기대=%q, 실제=%q", "작업이 완료됐습니다", txt)
		}
	}

	// 두 번째: completed 상태 (Final=true)
	if evs[1].StatusUpdate == nil {
		t.Error("두 번째 이벤트가 StatusUpdate여야 합니다")
	} else {
		su := evs[1].StatusUpdate
		if su.Status.State != TaskStateCompleted {
			t.Errorf("상태 기대=%s, 실제=%s", TaskStateCompleted, su.Status.State)
		}
		if !su.Final {
			t.Error("completed 이벤트는 Final=true여야 합니다")
		}
	}

	// 저장소 완료 상태 확인
	task, ok := store.GetTask(u.task.ID)
	if !ok {
		t.Fatal("태스크를 찾을 수 없습니다")
	}
	if task.Status.State != TaskStateCompleted {
		t.Errorf("저장소 상태 기대=%s, 실제=%s", TaskStateCompleted, task.Status.State)
	}
	if len(task.Artifacts) != 1 {
		t.Errorf("아티팩트 수 기대=1, 실제=%d", len(task.Artifacts))
	}
}

// ─── 기본 경로: 실행기 예외 → failed ─────────────────────────────────────────

func TestHandleAgentEvent_Error_Failed(t *testing.T) {
	u, store, q := newTestUpdater()

	execErr := errors.New("실행기 내부 오류")
	ev := agent.AgentEvent{
		Error: execErr,
	}

	if err := handleAgentEvent(ev, u); err != nil {
		t.Fatalf("handleAgentEvent 반환 오류: %v (setFailed는 오류를 전파하지 않음)", err)
	}

	evs := drainQueue(q)
	if len(evs) != 1 {
		t.Fatalf("이벤트 수 기대=1, 실제=%d", len(evs))
	}
	su := evs[0].StatusUpdate
	if su == nil {
		t.Fatal("StatusUpdate 이벤트가 없습니다")
	}
	if su.Status.State != TaskStateFailed {
		t.Errorf("상태 기대=%s, 실제=%s", TaskStateFailed, su.Status.State)
	}
	if !su.Final {
		t.Error("failed 이벤트는 Final=true여야 합니다")
	}

	task, ok := store.GetTask(u.task.ID)
	if !ok {
		t.Fatal("태스크를 찾을 수 없습니다")
	}
	if task.Status.State != TaskStateFailed {
		t.Errorf("저장소 상태 기대=%s, 실제=%s", TaskStateFailed, task.Status.State)
	}
}

// ─── 기본 경로: 취소(Cancel) ──────────────────────────────────────────────────
// ctx 취소 경로는 StreamToTaskUpdates 내부에서 처리되므로
// Cancel 메서드의 동작을 직접 검증한다.

func TestTaskUpdater_Cancel(t *testing.T) {
	u, store, q := newTestUpdater()

	cancelMsg := NewAgentTextMessage("컨텍스트 취소로 태스크가 중단됐습니다", "", "")
	u.Cancel(cancelMsg)

	evs := drainQueue(q)
	if len(evs) != 1 {
		t.Fatalf("이벤트 수 기대=1, 실제=%d", len(evs))
	}
	su := evs[0].StatusUpdate
	if su == nil {
		t.Fatal("StatusUpdate 이벤트가 없습니다")
	}
	if su.Status.State != TaskStateCanceled {
		t.Errorf("상태 기대=%s, 실제=%s", TaskStateCanceled, su.Status.State)
	}
	if !su.Final {
		t.Error("canceled 이벤트는 Final=true여야 합니다")
	}

	task, ok := store.GetTask(u.task.ID)
	if !ok {
		t.Fatal("태스크를 찾을 수 없습니다")
	}
	if task.Status.State != TaskStateCanceled {
		t.Errorf("저장소 상태 기대=%s, 실제=%s", TaskStateCanceled, task.Status.State)
	}
}

// ─── 대체 경로: structured.AgentStatus="completed" ────────────────────────────

func TestHandleAgentEvent_StructuredStatus_Completed(t *testing.T) {
	u, store, q := newTestUpdater()

	status := structured.AgentStatus{
		Status:  "completed",
		Message: "구조화 완료 응답입니다",
	}
	ev := agent.AgentEvent{
		IsTaskComplete: true,
		Content:        "기본 경로 내용",
		Update: core.StateUpdate{
			"structured_response": status,
		},
	}

	if err := handleAgentEvent(ev, u); err != nil {
		t.Fatalf("handleAgentEvent 반환 오류: %v", err)
	}

	evs := drainQueue(q)
	if len(evs) != 2 {
		t.Fatalf("이벤트 수 기대=2 (아티팩트+완료), 실제=%d", len(evs))
	}

	// 아티팩트에 AgentStatus.Message가 담겨야 한다
	if evs[0].ArtifactUpdate == nil {
		t.Error("첫 번째 이벤트가 ArtifactUpdate여야 합니다")
	} else {
		txt, ok := ArtifactText(evs[0].ArtifactUpdate.Artifact)
		if !ok {
			t.Error("아티팩트에 텍스트 파트가 없습니다")
		} else if txt != "구조화 완료 응답입니다" {
			t.Errorf("아티팩트 텍스트 기대=%q, 실제=%q", "구조화 완료 응답입니다", txt)
		}
	}

	// 완료 상태 확인
	if evs[1].StatusUpdate == nil || evs[1].StatusUpdate.Status.State != TaskStateCompleted {
		t.Errorf("두 번째 이벤트 기대=completed StatusUpdate, 실제=%+v", evs[1])
	}

	task, ok := store.GetTask(u.task.ID)
	if !ok {
		t.Fatal("태스크를 찾을 수 없습니다")
	}
	if task.Status.State != TaskStateCompleted {
		t.Errorf("저장소 상태 기대=%s, 실제=%s", TaskStateCompleted, task.Status.State)
	}
}

// ─── 대체 경로: structured.AgentStatus="input_required" ──────────────────────

func TestHandleAgentEvent_StructuredStatus_InputRequired(t *testing.T) {
	u, store, q := newTestUpdater()

	status := structured.AgentStatus{
		Status:  "input_required",
		Message: "추가 정보를 입력해 주세요",
	}
	ev := agent.AgentEvent{
		IsTaskComplete: true,
		Update: core.StateUpdate{
			"structured_response": status,
		},
	}

	if err := handleAgentEvent(ev, u); err != nil {
		t.Fatalf("handleAgentEvent 반환 오류: %v", err)
	}

	evs := drainQueue(q)
	if len(evs) != 1 {
		t.Fatalf("이벤트 수 기대=1, 실제=%d", len(evs))
	}
	su := evs[0].StatusUpdate
	if su == nil {
		t.Fatal("StatusUpdate 이벤트가 없습니다")
	}
	if su.Status.State != TaskStateInputRequired {
		t.Errorf("상태 기대=%s, 실제=%s", TaskStateInputRequired, su.Status.State)
	}
	if !su.Final {
		t.Error("input_required 이벤트는 Final=true여야 합니다")
	}

	task, ok := store.GetTask(u.task.ID)
	if !ok {
		t.Fatal("태스크를 찾을 수 없습니다")
	}
	if task.Status.State != TaskStateInputRequired {
		t.Errorf("저장소 상태 기대=%s, 실제=%s", TaskStateInputRequired, task.Status.State)
	}
}

// ─── 대체 경로: structured.AgentStatus="error" → input_required ───────────────

func TestHandleAgentEvent_StructuredStatus_ErrorToInputRequired(t *testing.T) {
	u, store, q := newTestUpdater()

	// error 상태는 input_required로 흘러야 한다(ANALYSIS §5 D5.4, README §22-4).
	status := structured.AgentStatus{
		Status:  "error",
		Message: "처리 중 오류가 발생했습니다. 다시 시도해 주세요.",
	}
	ev := agent.AgentEvent{
		IsTaskComplete: true,
		Update: core.StateUpdate{
			"structured_response": status,
		},
	}

	if err := handleAgentEvent(ev, u); err != nil {
		t.Fatalf("handleAgentEvent 반환 오류: %v", err)
	}

	evs := drainQueue(q)
	if len(evs) != 1 {
		t.Fatalf("이벤트 수 기대=1, 실제=%d", len(evs))
	}
	su := evs[0].StatusUpdate
	if su == nil {
		t.Fatal("StatusUpdate 이벤트가 없습니다")
	}
	// error는 input_required로 흘러야 한다
	if su.Status.State != TaskStateInputRequired {
		t.Errorf("error 상태가 input_required로 흘러야 함: 기대=%s, 실제=%s",
			TaskStateInputRequired, su.Status.State)
	}
	if !su.Final {
		t.Error("input_required(from error) 이벤트는 Final=true여야 합니다")
	}

	task, ok := store.GetTask(u.task.ID)
	if !ok {
		t.Fatal("태스크를 찾을 수 없습니다")
	}
	// 저장소도 input_required여야 한다
	if task.Status.State != TaskStateInputRequired {
		t.Errorf("저장소 상태 기대=%s, 실제=%s", TaskStateInputRequired, task.Status.State)
	}
}

// ─── 대체 경로: *structured.AgentStatus 포인터 단언 ──────────────────────────

func TestHandleAgentEvent_StructuredStatus_PointerAssertion(t *testing.T) {
	u, _, q := newTestUpdater()

	// 포인터 형태로 structured_response에 담긴 경우도 단언돼야 한다.
	status := &structured.AgentStatus{
		Status:  "completed",
		Message: "포인터 단언 경로",
	}
	ev := agent.AgentEvent{
		IsTaskComplete: true,
		Content:        "기본 경로 내용",
		Update: core.StateUpdate{
			"structured_response": status,
		},
	}

	if err := handleAgentEvent(ev, u); err != nil {
		t.Fatalf("handleAgentEvent 반환 오류: %v", err)
	}

	evs := drainQueue(q)
	if len(evs) != 2 {
		t.Fatalf("이벤트 수 기대=2, 실제=%d", len(evs))
	}
	if evs[0].ArtifactUpdate == nil {
		t.Error("첫 번째 이벤트가 ArtifactUpdate여야 합니다")
	}
	if evs[1].StatusUpdate == nil || evs[1].StatusUpdate.Status.State != TaskStateCompleted {
		t.Error("두 번째 이벤트가 completed StatusUpdate여야 합니다")
	}
}

// ─── 대체 경로: structured_response가 없으면 기본 경로 유지 ───────────────────

func TestHandleAgentEvent_Complete_FallsBackToDefaultPath(t *testing.T) {
	u, _, q := newTestUpdater()

	// Update가 있지만 structured_response 키가 없는 경우 → 기본 경로
	ev := agent.AgentEvent{
		IsTaskComplete: true,
		Content:        "기본 경로로 완료",
		Update: core.StateUpdate{
			"messages": []string{"some", "messages"},
		},
	}

	if err := handleAgentEvent(ev, u); err != nil {
		t.Fatalf("handleAgentEvent 반환 오류: %v", err)
	}

	evs := drainQueue(q)
	if len(evs) != 2 {
		t.Fatalf("이벤트 수 기대=2 (아티팩트+완료), 실제=%d", len(evs))
	}
	if evs[0].ArtifactUpdate == nil {
		t.Error("첫 번째 이벤트가 ArtifactUpdate여야 합니다")
	}
	txt, _ := ArtifactText(evs[0].ArtifactUpdate.Artifact)
	if txt != "기본 경로로 완료" {
		t.Errorf("아티팩트 텍스트 기대=%q, 실제=%q", "기본 경로로 완료", txt)
	}
	if evs[1].StatusUpdate == nil || evs[1].StatusUpdate.Status.State != TaskStateCompleted {
		t.Error("두 번째 이벤트가 completed StatusUpdate여야 합니다")
	}
}
