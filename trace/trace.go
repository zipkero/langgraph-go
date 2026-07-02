// trace 패키지는 그래프·에이전트 실행을 시간순으로 기록하는 선택 모듈이다.
// graph·message·llm·tool·core·config에 의존하며 하위 패키지는 trace를 역참조하지 않는다(§28-1).
// 자동 계측은 제공하지 않는다 — Record* 메소드와 tool.Event 싱크는 수신 표면일 뿐, 그래프·에이전트
// 실행 경로에 스스로 끼어들지 않는다.
package trace

import (
	"encoding/json"
	"sync"

	"github.com/zipkero/langgraph-go/graph"
	"github.com/zipkero/langgraph-go/llm"
	"github.com/zipkero/langgraph-go/message"
	"github.com/zipkero/langgraph-go/tool"
)

// Kind 는 Event 가 담는 페이로드의 종류를 나타내는 명명된 문자열 타입이다.
type Kind string

const (
	// KindNode 는 NodeTrace 페이로드를 담은 이벤트다.
	KindNode Kind = "node"
	// KindTool 은 ToolTrace 페이로드를 담은 이벤트다.
	KindTool Kind = "tool"
	// KindLLM 은 LLMTrace 페이로드를 담은 이벤트다.
	KindLLM Kind = "llm"
	// KindError 는 ErrorTrace 페이로드를 담은 이벤트다.
	KindError Kind = "error"
)

// NodePhase 는 NodeTrace 가 노드 진입인지 종료인지를 나타낸다.
type NodePhase string

const (
	// NodePhaseStart 는 노드 진입을 나타낸다.
	NodePhaseStart NodePhase = "start"
	// NodePhaseEnd 는 노드 종료를 나타낸다.
	NodePhaseEnd NodePhase = "end"
)

// NodeTrace 는 노드 진입/종료 한 번을 기록한 페이로드다.
type NodeTrace struct {
	// Node 는 노드 이름이다.
	Node string
	// Phase 는 진입/종료 구분이다.
	Phase NodePhase
	// State 는 진입 시점의 상태다(NodePhaseStart 에서만 채워진다).
	State graph.State `json:",omitempty"`
	// Update 는 종료 시점에 노드가 반환한 상태 변경분이다(NodePhaseEnd 에서만 채워진다).
	Update graph.StateUpdate `json:",omitempty"`
}

// ToolPhase 는 ToolTrace 가 도구 호출인지 결과인지를 나타낸다.
type ToolPhase string

const (
	// ToolPhaseCall 은 도구 호출 요청을 나타낸다.
	ToolPhaseCall ToolPhase = "call"
	// ToolPhaseResult 는 도구 실행 결과를 나타낸다.
	ToolPhaseResult ToolPhase = "result"
)

// ToolTrace 는 도구 호출 또는 결과 한 번을 기록한 페이로드다.
type ToolTrace struct {
	// Phase 는 호출/결과 구분이다.
	Phase ToolPhase
	// Call 은 도구 호출 내용이다(ToolPhaseCall 에서만 채워진다).
	Call *message.ToolCall `json:",omitempty"`
	// Result 는 도구 실행 결과다(ToolPhaseResult 에서만 채워진다).
	Result *tool.Result `json:",omitempty"`
	// Err 는 tool.Event 싱크(ToolEventSink)로 유입된 오류 메시지다. 직접
	// RecordToolCall/RecordToolResult 경로에서는 채워지지 않는다(task-005).
	Err string `json:",omitempty"`
}

// LLMPhase 는 LLMTrace 가 요청인지 응답인지를 나타낸다.
type LLMPhase string

const (
	// LLMPhaseRequest 는 LLM 요청을 나타낸다.
	LLMPhaseRequest LLMPhase = "request"
	// LLMPhaseResponse 는 LLM 응답을 나타낸다.
	LLMPhaseResponse LLMPhase = "response"
)

// LLMTrace 는 LLM 요청 또는 응답 한 번을 기록한 페이로드다.
type LLMTrace struct {
	// Phase 는 요청/응답 구분이다.
	Phase LLMPhase
	// Request 는 LLM 요청 내용이다(LLMPhaseRequest 에서만 채워진다).
	Request *llm.ChatRequest `json:",omitempty"`
	// Response 는 LLM 응답 내용이다(LLMPhaseResponse 에서만 채워진다).
	Response *llm.ChatResponse `json:",omitempty"`
}

// ErrorTrace 는 기록된 에러 한 번을 담은 페이로드다.
// error 값 자체는 표준 JSON으로 직렬화되지 않으므로 메시지 문자열로 보관한다.
type ErrorTrace struct {
	// Message 는 에러 메시지다.
	Message string
}

// Event 는 시간순 기록의 한 항목이다.
// Seq 로 기록 순서를, Kind 로 어느 페이로드 필드가 채워졌는지 판별한다.
type Event struct {
	// Seq 는 1부터 증가하는 기록 순번이다.
	Seq int
	// Kind 는 이 이벤트가 담는 페이로드의 종류다.
	Kind Kind
	// Node 는 KindNode 이벤트의 페이로드다.
	Node *NodeTrace `json:",omitempty"`
	// Tool 은 KindTool 이벤트의 페이로드다.
	Tool *ToolTrace `json:",omitempty"`
	// LLM 은 KindLLM 이벤트의 페이로드다.
	LLM *LLMTrace `json:",omitempty"`
	// Error 는 KindError 이벤트의 페이로드다.
	Error *ErrorTrace `json:",omitempty"`
}

// runSpan 은 StartRun/EndRun 으로 표시된 run 구간 하나를 나타내는 내부 상태다.
type runSpan struct {
	runID string
	// startSeq 는 run 시작 시점의 다음 이벤트 순번이다.
	startSeq int
	// endSeq 는 run 종료 시점의 마지막 이벤트 순번이다. 종료 전이면 0이다.
	endSeq int
	// closed 는 EndRun 이 호출되어 이 run 이 닫혔는지를 나타낸다. endSeq 는 이벤트가
	// 하나도 없는 상태로 닫힌 run 에서도 0일 수 있으므로, "닫힘"과 "미종료"를
	// 구분하려면 endSeq==0 이 아니라 이 필드를 봐야 한다(Pretty() 의 run 경계 렌더링용).
	closed bool
}

// Trace 는 한 run 단위로 이벤트를 시간순 누적하는 인메모리 추적기다.
// 기록(Record*)과 조회(Events)를 mutex로 직렬화해 동시 호출에 안전하다.
type Trace struct {
	mu     sync.Mutex
	seq    int
	events []Event
	runs   []runSpan
}

// New 는 빈 Trace 를 생성한다.
func New() *Trace {
	return &Trace{}
}

// StartRun 은 runID 로 식별되는 run 구간을 연다.
func (t *Trace) StartRun(runID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.runs = append(t.runs, runSpan{runID: runID, startSeq: t.seq + 1})
}

// EndRun 은 runID 로 식별되는, 아직 열려 있는 가장 최근 run 구간을 닫는다.
func (t *Trace) EndRun(runID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for i := len(t.runs) - 1; i >= 0; i-- {
		if t.runs[i].runID == runID && !t.runs[i].closed {
			t.runs[i].endSeq = t.seq
			t.runs[i].closed = true
			return
		}
	}
}

// append 는 잠금을 쥔 상태에서 순번을 매겨 이벤트를 누적한다.
func (t *Trace) append(ev Event) {
	t.seq++
	ev.Seq = t.seq
	t.events = append(t.events, ev)
}

// RecordNodeStart 는 node 의 진입을 st 상태와 함께 기록한다.
func (t *Trace) RecordNodeStart(node string, st graph.State) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.append(Event{Kind: KindNode, Node: &NodeTrace{Node: node, Phase: NodePhaseStart, State: st}})
}

// RecordNodeEnd 는 node 의 종료를 update 변경분과 함께 기록한다.
func (t *Trace) RecordNodeEnd(node string, update graph.StateUpdate) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.append(Event{Kind: KindNode, Node: &NodeTrace{Node: node, Phase: NodePhaseEnd, Update: update}})
}

// RecordToolCall 은 도구 호출 call 을 기록한다.
func (t *Trace) RecordToolCall(call message.ToolCall) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.append(Event{Kind: KindTool, Tool: &ToolTrace{Phase: ToolPhaseCall, Call: &call}})
}

// RecordToolResult 는 도구 실행 결과 res 를 기록한다.
func (t *Trace) RecordToolResult(res tool.Result) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.append(Event{Kind: KindTool, Tool: &ToolTrace{Phase: ToolPhaseResult, Result: &res}})
}

// RecordLLMRequest 는 LLM 요청 req 를 기록한다.
func (t *Trace) RecordLLMRequest(req llm.ChatRequest) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.append(Event{Kind: KindLLM, LLM: &LLMTrace{Phase: LLMPhaseRequest, Request: &req}})
}

// RecordLLMResponse 는 LLM 응답 resp 를 기록한다.
func (t *Trace) RecordLLMResponse(resp llm.ChatResponse) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.append(Event{Kind: KindLLM, LLM: &LLMTrace{Phase: LLMPhaseResponse, Response: &resp}})
}

// RecordError 는 err 를 기록한다.
func (t *Trace) RecordError(err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.append(Event{Kind: KindError, Error: &ErrorTrace{Message: err.Error()}})
}

// ToolEventSink 는 tool.Event 를 받아 ToolTrace 로 매핑·누적하는 클로저를 반환한다
// (ANALYSIS Decision (e)). 반환값은 tool.NewRuntime(..., emit func(tool.Event)) 의
// emit 인자에 그대로 대입할 수 있어, 도구가 이벤트를 방출하면 별도 수작업
// RecordToolCall/RecordToolResult 호출 없이 그 호출/결과가 자동으로 기록된다.
//
// ev.Result 가 nil 이면 도구 호출(ToolPhaseCall)로, 아니면 결과(ToolPhaseResult)로
// 매핑한다. ev.Err 가 있으면 ToolTrace.Err 에 메시지 문자열로 반영한다. 누적은
// task-001의 mutex 보호 append 경로를 그대로 공유한다.
func (t *Trace) ToolEventSink() func(tool.Event) {
	return func(ev tool.Event) {
		tt := &ToolTrace{}
		if ev.Result != nil {
			tt.Phase = ToolPhaseResult
			tt.Result = ev.Result
		} else {
			tt.Phase = ToolPhaseCall
			tt.Call = &message.ToolCall{ID: ev.ToolCallID, Name: ev.ToolName, Args: ev.Input}
		}
		if ev.Err != nil {
			tt.Err = ev.Err.Error()
		}

		t.mu.Lock()
		defer t.mu.Unlock()
		t.append(Event{Kind: KindTool, Tool: tt})
	}
}

// Events 는 누적된 이벤트를 기록(호출) 순서대로 반환한다.
func (t *Trace) Events() []Event {
	t.mu.Lock()
	defer t.mu.Unlock()
	result := make([]Event, len(t.events))
	copy(result, t.events)
	return result
}

// ExportJSON 은 누적된 이벤트를 JSON 바이트로 직렬화해 반환한다.
// 커스텀 마샬러 없이 표준 encoding/json 만 사용하며, Event 의 Kind 판별 필드와
// omitempty 페이로드 필드 덕분에 역직렬화 시 종류·필드 값이 보존된다(round-trip).
// Events() 와 동일한 잠금 경로(스냅샷)를 거치므로 직렬화 도중 동시 기록과 경합하지 않는다.
func (t *Trace) ExportJSON() ([]byte, error) {
	events := t.Events()
	b, err := json.Marshal(events)
	if err != nil {
		return nil, err
	}
	return b, nil
}
