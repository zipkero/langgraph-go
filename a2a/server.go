// server.go 는 A2A 서버 계층을 구현한다.
// AgentExecutor, RequestContext, EventQueue, TaskUpdater, TaskStore/InMemoryTaskStore,
// RequestHandler/DefaultRequestHandler, Server(NewServer/Run)와 헬퍼를 제공한다.
// JSON-RPC 2.0 over HTTP(+SSE)를 net/http와 encoding/json으로 직접 구현한다(SPEC §3).
package a2a

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"
)

// ─── 헬퍼 ────────────────────────────────────────────────────────────────────

// newUUID 는 표준 라이브러리 crypto/rand를 사용해 UUID v4 문자열을 생성한다.
// 외부 패키지 없이 구현한다(SPEC §3).
func newUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	// UUID v4: version 비트와 variant 비트를 설정한다.
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return hex.EncodeToString(b[:4]) + "-" +
		hex.EncodeToString(b[4:6]) + "-" +
		hex.EncodeToString(b[6:8]) + "-" +
		hex.EncodeToString(b[8:10]) + "-" +
		hex.EncodeToString(b[10:])
}

// NewTask 는 메시지를 초기 입력으로 가지는 새 태스크를 생성한다.
// ID는 자동 생성되며, 상태는 TaskStateWorking으로 초기화된다(README §22-2).
func NewTask(msg Message) Task {
	return Task{
		ID:      newUUID(),
		Status:  TaskStatus{State: TaskStateWorking},
		History: []Message{msg},
	}
}

// NewAgentTextMessage 는 에이전트 역할의 텍스트 메시지를 생성한다.
// contextID·taskID는 옵션이며 빈 문자열로 전달하면 생략된다(README §22-2).
func NewAgentTextMessage(text, contextID, taskID string) Message {
	return Message{
		Role:      RoleAgent,
		MessageID: newUUID(),
		Parts: []Part{
			{Text: &TextPart{Text: text}},
		},
	}
}

// ─── EventQueue ───────────────────────────────────────────────────────────────

// EventQueue 는 태스크 수명주기 이벤트를 전달하는 큐다.
// 실행기(AgentExecutor)가 이벤트를 push하고 HTTP 핸들러가 소비한다(README §22-2).
type EventQueue interface {
	// EnqueueEvent 는 이벤트를 큐에 추가한다.
	// 큐가 가득 찬 경우 호출이 블로킹될 수 있다.
	EnqueueEvent(e Event)
}

// bufferedEventQueue 는 버퍼드 채널 기반 EventQueue 구현이다(ANALYSIS §5 D5.3).
type bufferedEventQueue struct {
	ch chan Event
}

// newBufferedEventQueue 는 size 버퍼를 가진 이벤트 큐를 생성한다.
func newBufferedEventQueue(size int) *bufferedEventQueue {
	return &bufferedEventQueue{ch: make(chan Event, size)}
}

// EnqueueEvent 는 이벤트를 채널에 push한다.
func (q *bufferedEventQueue) EnqueueEvent(e Event) {
	q.ch <- e
}

// close 는 이벤트 채널을 닫아 소비자에게 스트림 종료를 알린다.
func (q *bufferedEventQueue) close() {
	close(q.ch)
}

// events 는 이벤트 채널을 반환한다.
func (q *bufferedEventQueue) events() <-chan Event {
	return q.ch
}

// ─── TaskStore / InMemoryTaskStore ───────────────────────────────────────────

// TaskStore 는 태스크 저장·조회 인터페이스다(README §22-2).
type TaskStore interface {
	// SaveTask 는 태스크를 저장한다.
	SaveTask(t Task)
	// GetTask 는 ID로 태스크를 조회한다. 존재하지 않으면 (Task{}, false)를 반환한다.
	GetTask(id string) (Task, bool)
}

// InMemoryTaskStore 는 mutex+map 기반 메모리 내 태스크 저장소다.
// 동시 접근에서 안전하다(SPEC §5.4, ANALYSIS §5 D5.3).
type InMemoryTaskStore struct {
	mu    sync.RWMutex
	tasks map[string]Task
}

// NewInMemoryTaskStore 는 새 InMemoryTaskStore를 생성한다.
func NewInMemoryTaskStore() *InMemoryTaskStore {
	return &InMemoryTaskStore{tasks: make(map[string]Task)}
}

// SaveTask 는 태스크를 저장소에 저장한다.
func (s *InMemoryTaskStore) SaveTask(t Task) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tasks[t.ID] = t
}

// GetTask 는 ID로 태스크를 조회한다.
func (s *InMemoryTaskStore) GetTask(id string) (Task, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.tasks[id]
	return t, ok
}

// ─── RequestContext ───────────────────────────────────────────────────────────

// RequestContext 는 실행기로 전달되는 요청 컨텍스트다(README §22-2).
type RequestContext struct {
	// message 는 수신된 A2A 메시지다.
	message Message
	// currentTask 는 현재 태스크다(없으면 nil).
	currentTask *Task
}

// GetUserInput 은 사용자 입력 메시지를 반환한다.
func (rc RequestContext) GetUserInput() Message {
	return rc.message
}

// CurrentTask 는 현재 태스크를 반환한다. 없으면 nil을 반환한다.
func (rc RequestContext) CurrentTask() *Task {
	return rc.currentTask
}

// Message 는 수신된 메시지를 반환한다.
func (rc RequestContext) Message() Message {
	return rc.message
}

// ─── UpdaterOption / TaskUpdater ─────────────────────────────────────────────

// updaterOption 은 TaskUpdater 옵션을 나타내는 내부 구조체다.
type updaterOption struct {
	final bool
}

// UpdaterOption 은 TaskUpdater.UpdateStatus의 옵션 인터페이스다(README §22-2).
type UpdaterOption func(*updaterOption)

// Final 은 이 상태 전이가 최종 상태임을 나타내는 옵션이다.
// Final(true)로 전달하면 스트리밍 종료 신호가 된다(ANALYSIS §5 D5.3).
func Final(v bool) UpdaterOption {
	return func(o *updaterOption) {
		o.final = v
	}
}

// TaskUpdater 는 태스크 상태 전이·아티팩트 추가·완료·취소를 담당한다.
// EventQueue push와 TaskStore 갱신을 한 호출에서 함께 수행한다(README §22-2, ANALYSIS §5 D5.3).
type TaskUpdater struct {
	task  Task
	queue *bufferedEventQueue
	store TaskStore
	mu    sync.Mutex
}

// newTaskUpdater 는 새 TaskUpdater를 생성한다.
func newTaskUpdater(task Task, queue *bufferedEventQueue, store TaskStore) *TaskUpdater {
	return &TaskUpdater{
		task:  task,
		queue: queue,
		store: store,
	}
}

// UpdateStatus 는 태스크 상태를 갱신하고 이벤트를 큐에 push한다.
// opts에 Final(true)를 주면 이벤트의 Final 필드가 true로 설정된다(README §22-2).
func (u *TaskUpdater) UpdateStatus(state TaskState, msg Message, opts ...UpdaterOption) {
	u.mu.Lock()
	defer u.mu.Unlock()

	opt := &updaterOption{}
	for _, o := range opts {
		o(opt)
	}

	u.task.Status = TaskStatus{State: state, Message: &msg}
	u.store.SaveTask(u.task)

	ev := TaskStatusUpdateEvent{
		TaskID: u.task.ID,
		Status: u.task.Status,
		Final:  opt.final,
	}
	u.queue.EnqueueEvent(Event{StatusUpdate: &ev})
}

// AddArtifact 는 아티팩트를 태스크에 추가하고 이벤트를 큐에 push한다(README §22-2).
func (u *TaskUpdater) AddArtifact(parts []Part, name string) {
	u.mu.Lock()
	defer u.mu.Unlock()

	artifact := Artifact{
		ArtifactID: newUUID(),
		Name:       name,
		Parts:      parts,
	}
	u.task.Artifacts = append(u.task.Artifacts, artifact)
	u.store.SaveTask(u.task)

	ev := TaskArtifactUpdateEvent{
		TaskID:   u.task.ID,
		Artifact: artifact,
	}
	u.queue.EnqueueEvent(Event{ArtifactUpdate: &ev})
}

// Complete 는 태스크를 완료 상태로 전이한다.
// 완료 이벤트를 Final=true로 큐에 push하고 태스크를 저장한다(README §22-2).
func (u *TaskUpdater) Complete() {
	u.mu.Lock()
	defer u.mu.Unlock()

	u.task.Status = TaskStatus{State: TaskStateCompleted}
	u.store.SaveTask(u.task)

	ev := TaskStatusUpdateEvent{
		TaskID: u.task.ID,
		Status: u.task.Status,
		Final:  true,
	}
	u.queue.EnqueueEvent(Event{StatusUpdate: &ev})
}

// Cancel 은 태스크를 취소 상태로 전이한다.
// 취소 이벤트를 Final=true로 큐에 push하고 태스크를 저장한다(README §22-2).
func (u *TaskUpdater) Cancel(msg Message) {
	u.mu.Lock()
	defer u.mu.Unlock()

	u.task.Status = TaskStatus{State: TaskStateCanceled, Message: &msg}
	u.store.SaveTask(u.task)

	ev := TaskStatusUpdateEvent{
		TaskID: u.task.ID,
		Status: u.task.Status,
		Final:  true,
	}
	u.queue.EnqueueEvent(Event{StatusUpdate: &ev})
}

// GetTask 는 현재 태스크 스냅샷을 반환한다.
func (u *TaskUpdater) GetTask() Task {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.task
}

// setFailed 는 태스크를 failed 상태로 전이하고 Final 이벤트를 큐에 push한다.
// 실행기 예외 처리에서 내부적으로 호출된다(ANALYSIS §2.1, §2.2).
func (u *TaskUpdater) setFailed(err error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	failMsg := NewAgentTextMessage(err.Error(), "", u.task.ID)
	u.task.Status = TaskStatus{State: TaskStateFailed, Message: &failMsg}
	u.store.SaveTask(u.task)
	ev := TaskStatusUpdateEvent{
		TaskID: u.task.ID,
		Status: u.task.Status,
		Final:  true,
	}
	u.queue.EnqueueEvent(Event{StatusUpdate: &ev})
}

// ─── AgentExecutor ───────────────────────────────────────────────────────────

// AgentExecutor 는 A2A 실행기 인터페이스다.
// 응용이 이 인터페이스를 구현해 DefaultRequestHandler에 전달한다(README §22-2).
// Execute의 세 번째 인자로 *TaskUpdater를 받아 상태 전이·아티팩트 추가를 수행한다.
// EventQueue는 내부에서 TaskUpdater가 감싸 사용하며, 실행기는 TaskUpdater API만 쓴다.
type AgentExecutor interface {
	// Execute 는 요청 컨텍스트를 받아 실행을 수행한다.
	// u를 통해 상태 전이·아티팩트 추가·완료·취소를 호출한다.
	Execute(ctx context.Context, rc RequestContext, u *TaskUpdater) error
	// Cancel 은 실행 취소를 요청한다.
	Cancel(ctx context.Context, rc RequestContext, u *TaskUpdater) error
}

// ─── JSON-RPC 2.0 구조체 ─────────────────────────────────────────────────────

// jsonRPCRequest 는 JSON-RPC 2.0 요청 envelope다.
type jsonRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// jsonRPCResponse 는 JSON-RPC 2.0 응답 envelope다.
type jsonRPCResponse struct {
	JSONRPC string         `json:"jsonrpc"`
	ID      any            `json:"id,omitempty"`
	Result  any            `json:"result,omitempty"`
	Error   *jsonRPCError  `json:"error,omitempty"`
}

// jsonRPCError 는 JSON-RPC 2.0 오류 객체다.
type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// JSON-RPC 2.0 메서드 문자열 상수(ANALYSIS §5 D5.2).
const (
	// methodMessageSend 는 비스트리밍 메시지 전송 메서드 문자열이다.
	methodMessageSend = "message/send"
	// methodMessageStream 는 스트리밍 메시지 전송 메서드 문자열이다.
	methodMessageStream = "message/stream"
)

// JSON-RPC 2.0 오류 코드.
const (
	errCodeParseError     = -32700
	errCodeInvalidRequest = -32600
	errCodeMethodNotFound = -32601
	errCodeInternalError  = -32603
)

// ─── RequestHandler / DefaultRequestHandler ──────────────────────────────────

// RequestHandler 는 JSON-RPC 요청을 처리하는 인터페이스다.
// DefaultRequestHandler가 구현하며, NewServer가 수신한다(README §22-2).
type RequestHandler interface {
	// ServeHTTP 는 HTTP 요청을 처리한다.
	ServeHTTP(w http.ResponseWriter, r *http.Request)
}

// DefaultRequestHandler 는 AgentExecutor와 TaskStore를 받아 JSON-RPC 디스패치를 구현한다.
// message/send(비스트리밍)와 message/stream(스트리밍)을 처리한다(README §22-2).
type DefaultRequestHandler struct {
	executor AgentExecutor
	store    TaskStore
}

// NewDefaultRequestHandler 는 DefaultRequestHandler를 생성한다.
func NewDefaultRequestHandler(executor AgentExecutor, store TaskStore) *DefaultRequestHandler {
	return &DefaultRequestHandler{
		executor: executor,
		store:    store,
	}
}

// ServeHTTP 는 JSON-RPC 요청을 파싱해 method에 따라 디스패치한다.
func (h *DefaultRequestHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST 메서드만 허용됩니다", http.StatusMethodNotAllowed)
		return
	}

	var req jsonRPCRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONRPCError(w, nil, errCodeParseError, "JSON-RPC 파싱 실패: "+err.Error())
		return
	}

	switch req.Method {
	case methodMessageSend:
		h.handleMessageSend(w, r, req)
	case methodMessageStream:
		h.handleMessageStream(w, r, req)
	default:
		writeJSONRPCError(w, req.ID, errCodeMethodNotFound, "알 수 없는 메서드: "+req.Method)
	}
}

// parseMessageSendParams 는 JSON-RPC params를 MessageSendParams로 역직렬화한다.
func parseMessageSendParams(raw json.RawMessage) (MessageSendParams, error) {
	var params MessageSendParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return MessageSendParams{}, fmt.Errorf("params 역직렬화 실패: %w", err)
	}
	return params, nil
}

// buildRequestContext 는 MessageSendParams에서 RequestContext를 구성한다.
func buildRequestContext(params MessageSendParams, store TaskStore) (RequestContext, Task) {
	rc := RequestContext{message: params.Message}
	// 기존 태스크가 있으면 가져오고, 없으면 새 태스크를 생성한다.
	if params.TaskID != "" {
		if t, ok := store.GetTask(params.TaskID); ok {
			rc.currentTask = &t
			return rc, t
		}
	}
	task := NewTask(params.Message)
	if params.ContextID != "" {
		task.ContextID = params.ContextID
	}
	rc.currentTask = &task
	return rc, task
}

// handleMessageSend 는 비스트리밍 message/send 요청을 처리한다.
// 실행기를 호출하고 큐 이벤트를 최종 Task로 수합해 JSON-RPC result로 응답한다(ANALYSIS §2.1).
func (h *DefaultRequestHandler) handleMessageSend(w http.ResponseWriter, r *http.Request, req jsonRPCRequest) {
	params, err := parseMessageSendParams(req.Params)
	if err != nil {
		writeJSONRPCError(w, req.ID, errCodeInvalidRequest, err.Error())
		return
	}

	rc, task := buildRequestContext(params, h.store)
	h.store.SaveTask(task)

	// 이벤트 큐를 생성하고 실행기를 goroutine으로 실행한다.
	queue := newBufferedEventQueue(64)
	updater := newTaskUpdater(task, queue, h.store)

	go func() {
		defer queue.close()
		if execErr := h.executor.Execute(r.Context(), rc, updater); execErr != nil {
			// 실행기 예외 → failed 상태로 전이한다(ANALYSIS §2.1).
			updater.setFailed(execErr)
		}
	}()

	// 큐 이벤트를 소비해 최종 태스크를 수합한다.
	finalTask := h.collectFinalTask(queue, updater)

	writeJSONRPCResult(w, req.ID, finalTask)
}

// handleMessageStream 는 스트리밍 message/stream 요청을 처리한다.
// text/event-stream 응답을 열고 이벤트별 SSE 프레임으로 전송한다(ANALYSIS §2.2).
func (h *DefaultRequestHandler) handleMessageStream(w http.ResponseWriter, r *http.Request, req jsonRPCRequest) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSONRPCError(w, req.ID, errCodeInternalError, "SSE를 지원하지 않는 클라이언트입니다")
		return
	}

	params, err := parseMessageSendParams(req.Params)
	if err != nil {
		writeJSONRPCError(w, req.ID, errCodeInvalidRequest, err.Error())
		return
	}

	rc, task := buildRequestContext(params, h.store)
	h.store.SaveTask(task)

	// SSE 헤더를 설정한다.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	// 초기 태스크 이벤트를 방출한다.
	sendSSEEvent(w, flusher, Event{Task: &task})

	queue := newBufferedEventQueue(64)
	updater := newTaskUpdater(task, queue, h.store)

	go func() {
		defer queue.close()
		if execErr := h.executor.Execute(r.Context(), rc, updater); execErr != nil {
			// 실행기 예외 → failed 상태로 전이한다(ANALYSIS §2.2).
			updater.setFailed(execErr)
		}
	}()

	// 이벤트를 순차적으로 SSE 프레임으로 전송한다.
	// final 상태 이벤트 이후 스트림을 종료한다(ANALYSIS §2.2).
	for ev := range queue.events() {
		sendSSEEvent(w, flusher, ev)
		if isEventFinal(ev) {
			break
		}
	}
}

// collectFinalTask 는 큐 이벤트를 소비해 최종 태스크를 반환한다.
// final 이벤트 도달 또는 채널 종료 시 현재 태스크 스냅샷을 반환한다.
func (h *DefaultRequestHandler) collectFinalTask(queue *bufferedEventQueue, updater *TaskUpdater) Task {
	for ev := range queue.events() {
		if isEventFinal(ev) {
			break
		}
	}
	return updater.GetTask()
}

// isEventFinal 은 이벤트가 최종 상태를 나타내는지 판별한다.
func isEventFinal(ev Event) bool {
	if ev.StatusUpdate != nil && ev.StatusUpdate.Final {
		return true
	}
	return false
}

// sendSSEEvent 는 이벤트를 SSE data 프레임으로 직렬화해 전송한다.
func sendSSEEvent(w http.ResponseWriter, flusher http.Flusher, ev Event) {
	data, err := json.Marshal(ev)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "data: %s\n\n", data)
	flusher.Flush()
}

// writeJSONRPCResult 는 JSON-RPC 성공 응답을 작성한다.
func writeJSONRPCResult(w http.ResponseWriter, id any, result any) {
	resp := jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// writeJSONRPCError 는 JSON-RPC 오류 응답을 작성한다.
func writeJSONRPCError(w http.ResponseWriter, id any, code int, msg string) {
	resp := jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &jsonRPCError{Code: code, Message: msg},
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// ─── Server / NewServer / Run ─────────────────────────────────────────────────

// Server 는 A2A HTTP 서버다.
// AgentCard를 /.well-known/agent-card.json에 노출하고 JSON-RPC 요청을 처리한다(README §22-2).
type Server struct {
	card    AgentCard
	handler RequestHandler
	mux     *http.ServeMux
}

// NewServer 는 AgentCard와 RequestHandler를 받아 서버를 구성한다.
// AgentCard는 /.well-known/agent-card.json 경로에 노출된다(README §22-2, A2A 스펙).
func NewServer(card AgentCard, handler RequestHandler) *Server {
	mux := http.NewServeMux()
	s := &Server{
		card:    card,
		handler: handler,
		mux:     mux,
	}

	// AgentCard를 well-known 경로에 노출한다.
	mux.HandleFunc("/.well-known/agent-card.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(card)
	})

	// JSON-RPC 엔드포인트를 등록한다.
	mux.Handle("/", handler)

	return s
}

// Handler 는 서버의 http.Handler를 반환한다.
// net/http/httptest에서 서버를 직접 테스트할 때 사용한다.
func (s *Server) Handler() http.Handler {
	return s.mux
}

// Run 은 host:port에서 HTTP 서버를 기동한다.
// ctx 취소 시 서버를 graceful shutdown한다(README §22-2).
func (s *Server) Run(ctx context.Context, host string, port int) error {
	addr := fmt.Sprintf("%s:%d", host, port)
	srv := &http.Server{
		Addr:    addr,
		Handler: s.mux,
		BaseContext: func(l net.Listener) context.Context {
			return ctx
		},
	}

	// ctx 취소 시 서버를 graceful shutdown한다.
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("a2a: 서버 기동 실패: %w", err)
	}
	return nil
}
