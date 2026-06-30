// server_test.go 는 task-002 검증 조건을 단정하는 단위 테스트를 담는다.
// net/http/httptest 테스트 서버로 직접 호출해 외부 네트워크 없이 검증한다(SPEC §5.3, §5.4).
package a2a

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// ─── 테스트용 실행기 ──────────────────────────────────────────────────────────

// echoExecutor 는 텍스트 메시지를 받아 working→completed 전이와 아티팩트를 방출하는
// 테스트용 실행기다.
type echoExecutor struct {
	// extraSteps 는 Complete 이전에 추가로 수행할 UpdateStatus 횟수다.
	extraSteps int
}

// Execute 는 working 상태로 갱신한 뒤 아티팩트를 추가하고 완료한다.
func (e *echoExecutor) Execute(ctx context.Context, rc RequestContext, u *TaskUpdater) error {
	workingMsg := NewAgentTextMessage("처리 중", "", "")
	u.UpdateStatus(TaskStateWorking, workingMsg)

	for i := 0; i < e.extraSteps; i++ {
		u.UpdateStatus(TaskStateWorking, workingMsg)
	}

	// 사용자 입력 텍스트를 아티팩트로 echo한다.
	input := rc.GetUserInput()
	var text string
	for _, p := range input.Parts {
		if p.Text != nil {
			text = p.Text.Text
			break
		}
	}
	u.AddArtifact([]Part{{Text: &TextPart{Text: "에코: " + text}}}, "echo")
	u.Complete()
	return nil
}

// Cancel 은 취소를 처리한다.
func (e *echoExecutor) Cancel(ctx context.Context, rc RequestContext, u *TaskUpdater) error {
	return nil
}

// failExecutor 는 항상 에러를 반환하는 테스트용 실행기다.
type failExecutor struct{}

func (f *failExecutor) Execute(ctx context.Context, rc RequestContext, u *TaskUpdater) error {
	return fmt.Errorf("실행기 오류 발생")
}

func (f *failExecutor) Cancel(ctx context.Context, rc RequestContext, u *TaskUpdater) error {
	return nil
}

// inputRequiredExecutor 는 input_required(Final=true)를 방출하는 실행기다.
type inputRequiredExecutor struct{}

func (e *inputRequiredExecutor) Execute(ctx context.Context, rc RequestContext, u *TaskUpdater) error {
	msg := NewAgentTextMessage("추가 입력이 필요합니다", "", "")
	u.UpdateStatus(TaskStateInputRequired, msg, Final(true))
	return nil
}

func (e *inputRequiredExecutor) Cancel(ctx context.Context, rc RequestContext, u *TaskUpdater) error {
	return nil
}

// ─── 테스트 서버 구성 헬퍼 ───────────────────────────────────────────────────

// newTestServer 는 테스트용 httptest.Server를 구성해 반환한다.
func newTestServer(t *testing.T, executor AgentExecutor) (*httptest.Server, TaskStore) {
	t.Helper()
	store := NewInMemoryTaskStore()
	card := AgentCard{
		Name:        "테스트에이전트",
		Description: "단위 테스트용 에이전트",
		URL:         "http://localhost",
		Version:     "1.0.0",
		Capabilities: AgentCapabilities{
			Streaming: true,
		},
	}
	handler := NewDefaultRequestHandler(executor, store)
	srv := NewServer(card, handler)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts, store
}

// postJSONRPC 는 baseURL에 JSON-RPC 요청을 POST하고 응답 바이트를 반환한다.
func postJSONRPC(t *testing.T, baseURL, method string, params any) []byte {
	t.Helper()
	paramsBytes, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("params 직렬화 실패: %v", err)
	}
	reqBody := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  method,
		Params:  paramsBytes,
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		t.Fatalf("요청 직렬화 실패: %v", err)
	}
	resp, err := http.Post(baseURL+"/", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("HTTP POST 실패: %v", err)
	}
	defer resp.Body.Close()
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(resp.Body)
	return buf.Bytes()
}

// ─── 검증 테스트 ─────────────────────────────────────────────────────────────

// TestWellKnownAgentCard_카드가_조회된다 는 /.well-known/agent-card.json 경로에서
// AgentCard가 반환되는지 검증한다(SPEC §5.3).
func TestWellKnownAgentCard_카드가_조회된다(t *testing.T) {
	ts, _ := newTestServer(t, &echoExecutor{})

	resp, err := http.Get(ts.URL + "/.well-known/agent-card.json")
	if err != nil {
		t.Fatalf("카드 GET 실패: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("예상 상태코드 200, 실제: %d", resp.StatusCode)
	}

	var card AgentCard
	if err := json.NewDecoder(resp.Body).Decode(&card); err != nil {
		t.Fatalf("카드 역직렬화 실패: %v", err)
	}
	if card.Name != "테스트에이전트" {
		t.Errorf("카드 이름 불일치: 예상 '테스트에이전트', 실제 %q", card.Name)
	}
	if !card.Capabilities.Streaming {
		t.Error("카드에 스트리밍 능력이 표시되어야 한다")
	}
}

// TestMessageSend_실행기가_호출되고_태스크가_응답으로_온다 는 message/send 요청이
// 실행기를 호출해 상태·아티팩트를 담은 Task를 응답으로 돌려주는지 검증한다(SPEC §5.3, §5.4).
func TestMessageSend_실행기가_호출되고_태스크가_응답으로_온다(t *testing.T) {
	ts, store := newTestServer(t, &echoExecutor{})

	params := MessageSendParams{
		Message: Message{
			Role: RoleUser,
			Parts: []Part{
				{Text: &TextPart{Text: "안녕하세요"}},
			},
		},
	}

	respBytes := postJSONRPC(t, ts.URL, methodMessageSend, params)

	var rpcResp jsonRPCResponse
	if err := json.Unmarshal(respBytes, &rpcResp); err != nil {
		t.Fatalf("응답 역직렬화 실패: %v", err)
	}
	if rpcResp.Error != nil {
		t.Fatalf("JSON-RPC 오류 응답: %+v", rpcResp.Error)
	}
	if rpcResp.Result == nil {
		t.Fatal("result가 nil이다")
	}

	// result를 Task로 역직렬화한다.
	resultBytes, err := json.Marshal(rpcResp.Result)
	if err != nil {
		t.Fatalf("result 재직렬화 실패: %v", err)
	}
	var task Task
	if err := json.Unmarshal(resultBytes, &task); err != nil {
		t.Fatalf("Task 역직렬화 실패: %v", err)
	}

	if task.ID == "" {
		t.Error("태스크 ID가 비어 있다")
	}
	if task.Status.State != TaskStateCompleted {
		t.Errorf("예상 상태 completed, 실제: %q", task.Status.State)
	}
	if len(task.Artifacts) == 0 {
		t.Error("아티팩트가 없다")
	}
	artifact := task.Artifacts[0]
	if len(artifact.Parts) == 0 {
		t.Error("아티팩트에 파트가 없다")
	}
	if artifact.Parts[0].Text == nil {
		t.Error("아티팩트 파트가 텍스트가 아니다")
	}
	if !strings.Contains(artifact.Parts[0].Text.Text, "안녕하세요") {
		t.Errorf("아티팩트 텍스트에 입력이 포함되지 않았다: %q", artifact.Parts[0].Text.Text)
	}

	// 저장소에도 태스크가 저장되어 있어야 한다.
	stored, ok := store.GetTask(task.ID)
	if !ok {
		t.Error("저장소에 태스크가 없다")
	}
	if stored.Status.State != TaskStateCompleted {
		t.Errorf("저장된 태스크 상태 불일치: %q", stored.Status.State)
	}
}

// TestMessageStream_이벤트가_순차로_방출되고_최종_상태로_종료된다 는 message/stream 요청에서
// 상태 갱신·아티팩트 갱신 이벤트가 최종 상태로 종료될 때까지 순차 방출되는지 검증한다(SPEC §5.4).
func TestMessageStream_이벤트가_순차로_방출되고_최종_상태로_종료된다(t *testing.T) {
	ts, _ := newTestServer(t, &echoExecutor{extraSteps: 1})

	params := MessageSendParams{
		Message: Message{
			Role: RoleUser,
			Parts: []Part{
				{Text: &TextPart{Text: "스트림 테스트"}},
			},
		},
	}
	paramsBytes, _ := json.Marshal(params)
	reqBody := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      2,
		Method:  methodMessageStream,
		Params:  paramsBytes,
	}
	body, _ := json.Marshal(reqBody)

	resp, err := http.Post(ts.URL+"/", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("HTTP POST 실패: %v", err)
	}
	defer resp.Body.Close()

	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Errorf("Content-Type 불일치: 예상 text/event-stream, 실제 %q", ct)
	}

	// SSE 프레임을 순차 수집한다.
	var events []Event
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		var ev Event
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			t.Fatalf("이벤트 역직렬화 실패: %v", err)
		}
		events = append(events, ev)
		// 최종 이벤트 도달 시 읽기를 종료한다.
		if isEventFinal(ev) {
			break
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("SSE 스캔 실패: %v", err)
	}

	if len(events) == 0 {
		t.Fatal("이벤트가 하나도 오지 않았다")
	}

	// 첫 이벤트는 초기 태스크여야 한다.
	first := events[0]
	if first.Task == nil {
		t.Error("첫 번째 이벤트는 Task 이벤트여야 한다")
	}

	// 마지막 이벤트는 Final=true 상태 갱신이어야 한다.
	last := events[len(events)-1]
	if last.StatusUpdate == nil {
		t.Error("마지막 이벤트는 StatusUpdate여야 한다")
	} else if !last.StatusUpdate.Final {
		t.Error("마지막 이벤트의 Final 필드가 true여야 한다")
	} else if last.StatusUpdate.Status.State != TaskStateCompleted {
		t.Errorf("마지막 이벤트 상태 불일치: 예상 completed, 실제 %q", last.StatusUpdate.Status.State)
	}

	// 아티팩트 갱신 이벤트가 최소 하나 있어야 한다.
	hasArtifact := false
	for _, ev := range events {
		if ev.ArtifactUpdate != nil {
			hasArtifact = true
			break
		}
	}
	if !hasArtifact {
		t.Error("아티팩트 갱신 이벤트가 없다")
	}
}

// TestMessageSend_실행기_오류_시_failed_상태가_반환된다 는 실행기가 에러를 반환할 때
// 태스크 상태가 failed로 전이되는지 검증한다(SPEC §5.4).
func TestMessageSend_실행기_오류_시_failed_상태가_반환된다(t *testing.T) {
	ts, _ := newTestServer(t, &failExecutor{})

	params := MessageSendParams{
		Message: Message{
			Role:  RoleUser,
			Parts: []Part{{Text: &TextPart{Text: "실패 테스트"}}},
		},
	}
	respBytes := postJSONRPC(t, ts.URL, methodMessageSend, params)

	var rpcResp jsonRPCResponse
	_ = json.Unmarshal(respBytes, &rpcResp)

	resultBytes, _ := json.Marshal(rpcResp.Result)
	var task Task
	_ = json.Unmarshal(resultBytes, &task)

	if task.Status.State != TaskStateFailed {
		t.Errorf("예상 상태 failed, 실제: %q", task.Status.State)
	}
}

// TestMessageSend_input_required_상태_전이 는 input_required(Final=true) 전이가
// 태스크 응답에 반영되는지 검증한다(SPEC §5.4).
func TestMessageSend_input_required_상태_전이(t *testing.T) {
	ts, _ := newTestServer(t, &inputRequiredExecutor{})

	params := MessageSendParams{
		Message: Message{
			Role:  RoleUser,
			Parts: []Part{{Text: &TextPart{Text: "입력 필요 테스트"}}},
		},
	}
	respBytes := postJSONRPC(t, ts.URL, methodMessageSend, params)

	var rpcResp jsonRPCResponse
	_ = json.Unmarshal(respBytes, &rpcResp)
	resultBytes, _ := json.Marshal(rpcResp.Result)
	var task Task
	_ = json.Unmarshal(resultBytes, &task)

	if task.Status.State != TaskStateInputRequired {
		t.Errorf("예상 상태 input_required, 실제: %q", task.Status.State)
	}
}

// TestInMemoryTaskStore_동시접근_일관성 은 InMemoryTaskStore가 동시 접근에서
// 데이터 일관성을 유지하는지 검증한다(SPEC §5.4, ANALYSIS §5 D5.3).
func TestInMemoryTaskStore_동시접근_일관성(t *testing.T) {
	store := NewInMemoryTaskStore()
	const goroutines = 20
	const tasksPerGoroutine = 10

	var wg sync.WaitGroup
	// 여러 goroutine에서 동시 저장한다.
	for range goroutines {
		wg.Go(func() {
			for range tasksPerGoroutine {
				task := NewTask(Message{
					Role:  RoleUser,
					Parts: []Part{{Text: &TextPart{Text: "동시성 테스트"}}},
				})
				store.SaveTask(task)
				// 저장 직후 조회해서 일관성을 확인한다.
				stored, ok := store.GetTask(task.ID)
				if !ok {
					t.Errorf("저장한 태스크를 조회하지 못했다: %s", task.ID)
				}
				if stored.ID != task.ID {
					t.Errorf("태스크 ID 불일치: 예상 %q, 실제 %q", task.ID, stored.ID)
				}
			}
		})
	}
	wg.Wait()
}

// TestTaskUpdater_상태전이와_아티팩트가_태스크에_반영된다 는 TaskUpdater의 UpdateStatus/
// AddArtifact/Complete 호출이 태스크 상태와 이벤트에 반영되는지 검증한다(SPEC §5.4).
func TestTaskUpdater_상태전이와_아티팩트가_태스크에_반영된다(t *testing.T) {
	store := NewInMemoryTaskStore()
	task := NewTask(Message{Role: RoleUser, Parts: []Part{{Text: &TextPart{Text: "테스트"}}}})
	store.SaveTask(task)

	queue := newBufferedEventQueue(32)
	updater := newTaskUpdater(task, queue, store)

	// Working 상태로 갱신한다.
	workingMsg := NewAgentTextMessage("작업 중", "", "")
	updater.UpdateStatus(TaskStateWorking, workingMsg)

	// 아티팩트를 추가한다.
	updater.AddArtifact([]Part{{Text: &TextPart{Text: "결과"}}}, "결과물")

	// 완료 상태로 전이한다.
	updater.Complete()

	// 큐를 닫아 수집한다.
	queue.close()

	var events []Event
	for ev := range queue.events() {
		events = append(events, ev)
	}

	// 이벤트 순서: WorkingStatusUpdate → ArtifactUpdate → CompletedStatusUpdate(Final).
	if len(events) < 3 {
		t.Fatalf("이벤트 수 부족: %d", len(events))
	}

	if events[0].StatusUpdate == nil || events[0].StatusUpdate.Status.State != TaskStateWorking {
		t.Errorf("첫 번째 이벤트는 working 상태여야 한다")
	}
	if events[1].ArtifactUpdate == nil {
		t.Errorf("두 번째 이벤트는 아티팩트 갱신이어야 한다")
	}
	last := events[len(events)-1]
	if last.StatusUpdate == nil || !last.StatusUpdate.Final {
		t.Errorf("마지막 이벤트는 Final 상태 갱신이어야 한다")
	}
	if last.StatusUpdate.Status.State != TaskStateCompleted {
		t.Errorf("마지막 이벤트 상태: 예상 completed, 실제 %q", last.StatusUpdate.Status.State)
	}

	// 저장소의 태스크 상태도 completed여야 한다.
	stored, ok := store.GetTask(task.ID)
	if !ok {
		t.Fatal("저장된 태스크를 찾지 못했다")
	}
	if stored.Status.State != TaskStateCompleted {
		t.Errorf("저장된 태스크 상태: 예상 completed, 실제 %q", stored.Status.State)
	}
	if len(stored.Artifacts) == 0 {
		t.Error("저장된 태스크에 아티팩트가 없다")
	}
}

// TestTaskUpdater_Cancel_취소_상태로_전이한다 는 Cancel 호출이 태스크를 canceled로
// 전이하는지 검증한다(SPEC §5.4).
func TestTaskUpdater_Cancel_취소_상태로_전이한다(t *testing.T) {
	store := NewInMemoryTaskStore()
	task := NewTask(Message{Role: RoleUser, Parts: []Part{{Text: &TextPart{Text: "취소 테스트"}}}})
	store.SaveTask(task)

	queue := newBufferedEventQueue(8)
	updater := newTaskUpdater(task, queue, store)

	cancelMsg := NewAgentTextMessage("취소됨", "", "")
	updater.Cancel(cancelMsg)
	queue.close()

	var events []Event
	for ev := range queue.events() {
		events = append(events, ev)
	}

	if len(events) != 1 {
		t.Fatalf("이벤트 수 불일치: 예상 1, 실제 %d", len(events))
	}
	ev := events[0]
	if ev.StatusUpdate == nil {
		t.Fatal("이벤트가 StatusUpdate여야 한다")
	}
	if ev.StatusUpdate.Status.State != TaskStateCanceled {
		t.Errorf("예상 canceled, 실제 %q", ev.StatusUpdate.Status.State)
	}
	if !ev.StatusUpdate.Final {
		t.Error("Cancel 이벤트는 Final이어야 한다")
	}
}

// TestUnknownMethod_오류_응답이_온다 는 알 수 없는 JSON-RPC 메서드에 오류 응답을
// 반환하는지 검증한다.
func TestUnknownMethod_오류_응답이_온다(t *testing.T) {
	ts, _ := newTestServer(t, &echoExecutor{})

	respBytes := postJSONRPC(t, ts.URL, "unknown/method", nil)

	var rpcResp jsonRPCResponse
	if err := json.Unmarshal(respBytes, &rpcResp); err != nil {
		t.Fatalf("응답 역직렬화 실패: %v", err)
	}
	if rpcResp.Error == nil {
		t.Fatal("오류 응답이 기대되었으나 nil이다")
	}
	if rpcResp.Error.Code != errCodeMethodNotFound {
		t.Errorf("오류 코드 불일치: 예상 %d, 실제 %d", errCodeMethodNotFound, rpcResp.Error.Code)
	}
}
