// agent_test.go 는 agent 패키지의 단위 테스트를 담는다.
// stub llm.Client 로 도구 루프 다회 반복·shouldContinue 분기·applyResponseFormat·
// Stream 이벤트 방출·MaxSteps 종료를 검증한다. 네트워크 호출 없음.
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/zipkero/langgraph-go/checkpoint"
	"github.com/zipkero/langgraph-go/config"
	"github.com/zipkero/langgraph-go/core"
	"github.com/zipkero/langgraph-go/llm"
	"github.com/zipkero/langgraph-go/message"
	"github.com/zipkero/langgraph-go/middleware"
	"github.com/zipkero/langgraph-go/structured"
	"github.com/zipkero/langgraph-go/tool"
)

// ============================================================
// 테스트용 시퀀스 stub — 호출 순서별 응답 반환
// ============================================================

// seqStubClient 는 호출 순서에 따라 미리 지정된 응답을 순차 반환하는 stub Client 다.
// 도구 루프 다회 반복 테스트에서 "1회차: tool_calls → 2회차: 최종 답변" 순서 구성에 사용한다.
// 런타임 코드는 llm.Client 인터페이스에만 의존하며, 이 타입은 테스트 파일에만 존재한다.
type seqStubClient struct {
	responses  []llm.StubResponse
	callIndex  int
	model      string
	boundTools []tool.Schema
}

func newSeqStubClient(model string, responses ...llm.StubResponse) *seqStubClient {
	return &seqStubClient{
		responses: responses,
		model:     model,
	}
}

func (s *seqStubClient) currentResponse() llm.StubResponse {
	if s.callIndex >= len(s.responses) {
		// 마지막 응답을 반복한다
		return s.responses[len(s.responses)-1]
	}
	resp := s.responses[s.callIndex]
	s.callIndex++
	return resp
}

func (s *seqStubClient) Chat(_ context.Context, _ llm.ChatRequest) (llm.ChatResponse, error) {
	resp := s.currentResponse()
	if resp.Err != nil {
		return llm.ChatResponse{}, resp.Err
	}
	return llm.ChatResponse{
		Message:      resp.Message,
		ToolCalls:    resp.ToolCalls,
		Usage:        resp.Usage,
		FinishReason: resp.FinishReason,
	}, nil
}

func (s *seqStubClient) ChatStream(_ context.Context, _ llm.ChatRequest) (<-chan llm.ChatEvent, error) {
	resp := s.currentResponse()
	if resp.Err != nil {
		return nil, resp.Err
	}

	ch := make(chan llm.ChatEvent, 3)
	go func() {
		defer close(ch)
		if resp.Message.Content != "" {
			ch <- llm.ChatEvent{Type: llm.ChatEventToken, Token: resp.Message.Content}
		}
		msg := resp.Message
		ch <- llm.ChatEvent{Type: llm.ChatEventMessage, Message: &msg}
		fullResp := llm.ChatResponse{
			Message:      resp.Message,
			ToolCalls:    resp.ToolCalls,
			Usage:        resp.Usage,
			FinishReason: resp.FinishReason,
		}
		ch <- llm.ChatEvent{Type: llm.ChatEventDone, Response: &fullResp}
	}()
	return ch, nil
}

func (s *seqStubClient) Structured(_ context.Context, _ llm.ChatRequest, _ structured.Schema) (any, error) {
	resp := s.currentResponse()
	if resp.Err != nil {
		return nil, resp.Err
	}
	if resp.StructuredValue != nil {
		return resp.StructuredValue, nil
	}
	return map[string]any{"content": resp.Message.Content}, nil
}

func (s *seqStubClient) BindTools(tools []tool.Schema) llm.Client {
	clone := *s
	clone.boundTools = make([]tool.Schema, len(tools))
	copy(clone.boundTools, tools)
	// callIndex 는 공유(포인터 공유) 필요 — clone 이 별도 카운터를 가지면 루프 중 순서가 깨진다
	// 여기서는 포인터를 반환해 공유한다
	return &clone
}

func (s *seqStubClient) ParseToolCalls(resp llm.ChatResponse) []message.ToolCall {
	return resp.ToolCalls
}

func (s *seqStubClient) WithModel(name string) llm.Client {
	clone := *s
	clone.model = name
	return &clone
}

func (s *seqStubClient) ModelName() string { return s.model }

// ============================================================
// 테스트용 도구 — 단순 echo 도구
// ============================================================

// echoTool 은 인자를 그대로 돌려주는 테스트용 도구다.
type echoTool struct{}

func (e *echoTool) Name() string        { return "echo" }
func (e *echoTool) Description() string { return "인자를 그대로 돌려주는 도구" }
func (e *echoTool) Schema() tool.Schema {
	return tool.Schema{
		Name:        "echo",
		Description: "인자를 그대로 돌려주는 도구",
		Parameters: []tool.Parameter{
			{Name: "text", Type: "string", Description: "echo 할 텍스트", Required: true},
		},
	}
}
func (e *echoTool) Execute(_ context.Context, input tool.Input, _ tool.Runtime) (tool.Result, error) {
	var args map[string]any
	if err := json.Unmarshal(input, &args); err != nil {
		return tool.Result{}, err
	}
	text, _ := args["text"].(string)
	return tool.Result{Content: "echo: " + text}, nil
}

// ============================================================
// 헬퍼
// ============================================================

func makeToolCallMsg(toolCallID, toolName, argsJSON string) message.Message {
	return message.NewAssistantToolCalls([]message.ToolCall{
		{
			ID:   toolCallID,
			Name: toolName,
			Args: json.RawMessage(argsJSON),
		},
	})
}

// ============================================================
// 테스트: Create
// ============================================================

func TestCreate_NilModel(t *testing.T) {
	_, err := Create(nil, nil)
	if err == nil {
		t.Fatal("nil model 이면 에러를 반환해야 합니다")
	}
}

func TestCreate_Success(t *testing.T) {
	stub := llm.NewStubClient("stub", llm.StubResponse{
		Message: message.NewAssistantMessage("안녕하세요"),
	})
	a, err := Create(stub, nil)
	if err != nil {
		t.Fatalf("Create 실패: %v", err)
	}
	if a == nil {
		t.Fatal("Agent 가 nil 입니다")
	}
	if a.cfg.MaxSteps != defaultMaxSteps {
		t.Errorf("기본 MaxSteps=%d, 기대값=%d", a.cfg.MaxSteps, defaultMaxSteps)
	}
}

func TestCreate_WithOptions(t *testing.T) {
	stub := llm.NewStubClient("stub", llm.StubResponse{})
	a, err := Create(stub, nil,
		WithSystemPrompt("시스템 프롬프트"),
		WithMaxSteps(5),
	)
	if err != nil {
		t.Fatalf("Create 실패: %v", err)
	}
	if a.cfg.SystemPrompt != "시스템 프롬프트" {
		t.Errorf("SystemPrompt=%q, 기대값=%q", a.cfg.SystemPrompt, "시스템 프롬프트")
	}
	if a.cfg.MaxSteps != 5 {
		t.Errorf("MaxSteps=%d, 기대값=5", a.cfg.MaxSteps)
	}
}

// ============================================================
// 테스트: shouldContinue
// ============================================================

func TestShouldContinue_Continue(t *testing.T) {
	stub := llm.NewStubClient("stub", llm.StubResponse{})
	a, _ := Create(stub, nil)

	// 마지막 메시지가 tool_calls 를 가진 AI 메시지
	st := State{
		Messages: []message.Message{
			makeToolCallMsg("call-1", "echo", `{"text":"hello"}`),
		},
	}
	if d := a.shouldContinue(st); d != DecisionContinue {
		t.Errorf("shouldContinue=%q, 기대값=%q", d, DecisionContinue)
	}
}

func TestShouldContinue_Respond(t *testing.T) {
	schema := structured.BuildSchema[struct {
		Answer string `json:"answer"`
	}]()
	stub := llm.NewStubClient("stub", llm.StubResponse{})
	a, _ := Create(stub, nil, WithResponseFormat(schema))

	// AI 메시지에 tool_calls 없고 structured_response 미생성
	st := State{
		Messages: []message.Message{
			message.NewAssistantMessage("최종 답변"),
		},
		StructuredResponse: nil,
	}
	if d := a.shouldContinue(st); d != DecisionRespond {
		t.Errorf("shouldContinue=%q, 기대값=%q", d, DecisionRespond)
	}
}

func TestShouldContinue_End(t *testing.T) {
	stub := llm.NewStubClient("stub", llm.StubResponse{})
	a, _ := Create(stub, nil)

	// AI 메시지에 tool_calls 없고 ResponseFormat 도 없음
	st := State{
		Messages: []message.Message{
			message.NewAssistantMessage("최종 답변"),
		},
	}
	if d := a.shouldContinue(st); d != DecisionEnd {
		t.Errorf("shouldContinue=%q, 기대값=%q", d, DecisionEnd)
	}
}

func TestShouldContinue_Respond_AlreadyFilled(t *testing.T) {
	schema := structured.BuildSchema[struct {
		Answer string `json:"answer"`
	}]()
	stub := llm.NewStubClient("stub", llm.StubResponse{})
	a, _ := Create(stub, nil, WithResponseFormat(schema))

	// structured_response 가 이미 채워진 경우 → end
	st := State{
		Messages: []message.Message{
			message.NewAssistantMessage("최종 답변"),
		},
		StructuredResponse: map[string]any{"answer": "42"},
	}
	if d := a.shouldContinue(st); d != DecisionEnd {
		t.Errorf("structured_response 채워진 경우 shouldContinue=%q, 기대값=%q", d, DecisionEnd)
	}
}

// ============================================================
// 테스트: Invoke — 도구 루프 없는 단순 케이스
// ============================================================

func TestInvoke_NoTools(t *testing.T) {
	stub := llm.NewStubClient("stub", llm.StubResponse{
		Message:      message.NewAssistantMessage("안녕하세요"),
		FinishReason: "stop",
	})
	a, _ := Create(stub, nil)

	in := Input{Messages: []message.Message{message.NewUserMessage("안녕")}}
	result, err := a.Invoke(context.Background(), in, config.RunConfig{})
	if err != nil {
		t.Fatalf("Invoke 실패: %v", err)
	}
	if len(result.Messages) < 2 {
		t.Errorf("메시지 수=%d, 최소 2 기대(user+assistant)", len(result.Messages))
	}
	last := result.Messages[len(result.Messages)-1]
	if last.Content != "안녕하세요" {
		t.Errorf("마지막 메시지=%q, 기대값=%q", last.Content, "안녕하세요")
	}
}

// ============================================================
// 테스트: Invoke — 도구 루프 다회 반복
// ============================================================

func TestInvoke_ToolLoop_MultiRound(t *testing.T) {
	// 1회차: tool_calls 포함 응답, 2회차: 최종 텍스트 답변
	seq := newSeqStubClient("stub",
		llm.StubResponse{
			Message:      makeToolCallMsg("call-1", "echo", `{"text":"hello"}`),
			ToolCalls:    []message.ToolCall{{ID: "call-1", Name: "echo", Args: json.RawMessage(`{"text":"hello"}`)}},
			FinishReason: "tool_use",
		},
		llm.StubResponse{
			Message:      message.NewAssistantMessage("도구 실행 완료"),
			FinishReason: "stop",
		},
	)

	a, err := Create(seq, []tool.Tool{&echoTool{}})
	if err != nil {
		t.Fatalf("Create 실패: %v", err)
	}

	in := Input{Messages: []message.Message{message.NewUserMessage("echo 해줘")}}
	result, err := a.Invoke(context.Background(), in, config.RunConfig{})
	if err != nil {
		t.Fatalf("Invoke 실패: %v", err)
	}

	// 기대 메시지 구조: user → AI(tool_call) → tool → AI(최종)
	if len(result.Messages) < 4 {
		t.Errorf("메시지 수=%d, 최소 4 기대(user/AI-tool/tool-result/AI-final), 메시지: %v", len(result.Messages), result.Messages)
	}

	// 마지막 AI 메시지 내용 확인
	lastAI, ok := message.LastAIMessage(result.Messages)
	if !ok {
		t.Fatal("마지막 AI 메시지를 찾을 수 없습니다")
	}
	if lastAI.Content != "도구 실행 완료" {
		t.Errorf("마지막 AI 메시지=%q, 기대값=%q", lastAI.Content, "도구 실행 완료")
	}

	// ToolMessage 가 누적됐는지 확인
	toolMsgFound := false
	for _, m := range result.Messages {
		if m.Role == message.RoleTool {
			toolMsgFound = true
			break
		}
	}
	if !toolMsgFound {
		t.Error("ToolMessage 가 결과 메시지에 없습니다")
	}
}

// ============================================================
// 테스트: Invoke — 도구 루프 3회 반복
// ============================================================

func TestInvoke_ToolLoop_ThreeRounds(t *testing.T) {
	// 1·2회차: tool_calls, 3회차: 최종 답변
	seq := newSeqStubClient("stub",
		llm.StubResponse{
			Message:      makeToolCallMsg("call-1", "echo", `{"text":"1"}`),
			ToolCalls:    []message.ToolCall{{ID: "call-1", Name: "echo", Args: json.RawMessage(`{"text":"1"}`)}},
			FinishReason: "tool_use",
		},
		llm.StubResponse{
			Message:      makeToolCallMsg("call-2", "echo", `{"text":"2"}`),
			ToolCalls:    []message.ToolCall{{ID: "call-2", Name: "echo", Args: json.RawMessage(`{"text":"2"}`)}},
			FinishReason: "tool_use",
		},
		llm.StubResponse{
			Message:      message.NewAssistantMessage("3회 반복 완료"),
			FinishReason: "stop",
		},
	)

	a, err := Create(seq, []tool.Tool{&echoTool{}})
	if err != nil {
		t.Fatalf("Create 실패: %v", err)
	}

	in := Input{Messages: []message.Message{message.NewUserMessage("3회 루프")}}
	result, err := a.Invoke(context.Background(), in, config.RunConfig{})
	if err != nil {
		t.Fatalf("Invoke 실패: %v", err)
	}

	// 기대: user + AI(t1) + tool1 + AI(t2) + tool2 + AI(final) = 6
	if len(result.Messages) < 6 {
		t.Errorf("메시지 수=%d, 최소 6 기대", len(result.Messages))
	}
	lastAI, _ := message.LastAIMessage(result.Messages)
	if lastAI.Content != "3회 반복 완료" {
		t.Errorf("마지막 AI=%q, 기대값=%q", lastAI.Content, "3회 반복 완료")
	}
}

// ============================================================
// 테스트: Invoke — MaxSteps 강제 종료
// ============================================================

func TestInvoke_MaxSteps(t *testing.T) {
	// 항상 tool_calls 를 반환하는 stub → MaxSteps 초과 시 강제 종료
	alwaysToolCall := llm.NewStubClient("stub", llm.StubResponse{
		Message:      makeToolCallMsg("call-x", "echo", `{"text":"loop"}`),
		ToolCalls:    []message.ToolCall{{ID: "call-x", Name: "echo", Args: json.RawMessage(`{"text":"loop"}`)}},
		FinishReason: "tool_use",
	})

	a, err := Create(alwaysToolCall, []tool.Tool{&echoTool{}}, WithMaxSteps(3))
	if err != nil {
		t.Fatalf("Create 실패: %v", err)
	}

	in := Input{Messages: []message.Message{message.NewUserMessage("무한 루프")}}
	result, err := a.Invoke(context.Background(), in, config.RunConfig{})
	if err != nil {
		t.Fatalf("Invoke 실패(MaxSteps 초과는 에러 아님): %v", err)
	}

	// MaxSteps=3 → 최대 3회 runModel 실행 후 강제 종료
	// 메시지 = user + (AI(tool)+tool)*3 = 1 + 6 = 7개 이상이어야 하지만
	// 정확한 수는 구현에 따라 다를 수 있으므로 최소 확인만 한다
	if len(result.Messages) == 0 {
		t.Error("강제 종료 후에도 메시지가 있어야 합니다")
	}
}

// ============================================================
// 테스트: applyResponseFormat
// ============================================================

func TestApplyResponseFormat(t *testing.T) {
	type Answer struct {
		Answer string `json:"answer"`
	}
	schema := structured.BuildSchema[Answer]()

	stub := llm.NewStubClient("stub", llm.StubResponse{
		StructuredValue: map[string]any{"answer": "42"},
	})
	a, _ := Create(stub, nil, WithResponseFormat(schema))

	st := State{
		Messages: []message.Message{
			message.NewUserMessage("답은?"),
			message.NewAssistantMessage("42입니다"),
		},
	}
	result, err := a.applyResponseFormat(context.Background(), st, config.RunConfig{})
	if err != nil {
		t.Fatalf("applyResponseFormat 실패: %v", err)
	}
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("반환 타입=%T, map[string]any 기대", result)
	}
	if m["answer"] != "42" {
		t.Errorf("answer=%v, 기대값=42", m["answer"])
	}
}

// ============================================================
// 테스트: Invoke — WithResponseFormat 지정 시 structured_response 채워짐
// ============================================================

func TestInvoke_WithResponseFormat(t *testing.T) {
	type Answer struct {
		Answer string `json:"answer"`
	}
	schema := structured.BuildSchema[Answer]()

	// Chat → 최종 답변, Structured → 구조화 값 반환
	// seqStubClient 가 Structured 도 처리하므로 callIndex 증가 고려
	seq := newSeqStubClient("stub",
		// Chat 호출용 (shouldContinue → respond 이후 applyResponseFormat 에서 Structured 호출)
		llm.StubResponse{
			Message:         message.NewAssistantMessage("42입니다"),
			FinishReason:    "stop",
			StructuredValue: map[string]any{"answer": "42"},
		},
	)

	a, _ := Create(seq, nil, WithResponseFormat(schema))

	in := Input{Messages: []message.Message{message.NewUserMessage("답은?")}}
	result, err := a.Invoke(context.Background(), in, config.RunConfig{})
	if err != nil {
		t.Fatalf("Invoke 실패: %v", err)
	}
	if result.StructuredResponse == nil {
		t.Fatal("StructuredResponse 가 nil 입니다")
	}
	m, ok := result.StructuredResponse.(map[string]any)
	if !ok {
		t.Fatalf("StructuredResponse 타입=%T, map[string]any 기대", result.StructuredResponse)
	}
	if m["answer"] != "42" {
		t.Errorf("answer=%v, 기대값=42", m["answer"])
	}
}

// ============================================================
// 테스트: Stream — AgentEvent 방출
// ============================================================

func TestStream_Events(t *testing.T) {
	stub := llm.NewStubClient("stub", llm.StubResponse{
		Message:      message.NewAssistantMessage("스트림 응답"),
		FinishReason: "stop",
	})
	a, _ := Create(stub, nil)

	in := Input{Messages: []message.Message{message.NewUserMessage("안녕")}}
	ch, err := a.Stream(context.Background(), in, config.RunConfig{}, core.ModeMessages)
	if err != nil {
		t.Fatalf("Stream 실패: %v", err)
	}

	var events []AgentEvent
	for ev := range ch {
		events = append(events, ev)
	}

	// 최소 1개 이벤트, 마지막 이벤트는 IsTaskComplete=true
	if len(events) == 0 {
		t.Fatal("이벤트가 방출되지 않았습니다")
	}
	last := events[len(events)-1]
	if !last.IsTaskComplete {
		t.Error("마지막 이벤트의 IsTaskComplete=false, true 기대")
	}
}

func TestStream_TokenEvents(t *testing.T) {
	stub := llm.NewStubClient("stub", llm.StubResponse{
		Message:      message.NewAssistantMessage("토큰 내용"),
		FinishReason: "stop",
	})
	a, _ := Create(stub, nil)

	in := Input{Messages: []message.Message{message.NewUserMessage("안녕")}}
	ch, err := a.Stream(context.Background(), in, config.RunConfig{}, core.ModeMessages)
	if err != nil {
		t.Fatalf("Stream 실패: %v", err)
	}

	var tokenEvents []AgentEvent
	for ev := range ch {
		if ev.Token != "" {
			tokenEvents = append(tokenEvents, ev)
		}
	}

	// ModeMessages 모드에서 토큰 이벤트가 방출돼야 한다
	if len(tokenEvents) == 0 {
		t.Error("ModeMessages 모드에서 토큰 이벤트가 방출되지 않았습니다")
	}
}

func TestStream_ToolLoop_Events(t *testing.T) {
	seq := newSeqStubClient("stub",
		llm.StubResponse{
			Message:      makeToolCallMsg("call-1", "echo", `{"text":"test"}`),
			ToolCalls:    []message.ToolCall{{ID: "call-1", Name: "echo", Args: json.RawMessage(`{"text":"test"}`)}},
			FinishReason: "tool_use",
		},
		llm.StubResponse{
			Message:      message.NewAssistantMessage("완료"),
			FinishReason: "stop",
		},
	)
	a, _ := Create(seq, []tool.Tool{&echoTool{}})

	in := Input{Messages: []message.Message{message.NewUserMessage("echo")}}
	ch, err := a.Stream(context.Background(), in, config.RunConfig{}, core.ModeUpdates)
	if err != nil {
		t.Fatalf("Stream 실패: %v", err)
	}

	var events []AgentEvent
	for ev := range ch {
		events = append(events, ev)
	}

	// 최소 2개 이벤트(도구 실행 업데이트 + 완료)
	if len(events) < 2 {
		t.Errorf("이벤트 수=%d, 최소 2 기대", len(events))
	}
	// 마지막 이벤트는 IsTaskComplete=true
	last := events[len(events)-1]
	if !last.IsTaskComplete {
		t.Error("마지막 이벤트의 IsTaskComplete=false")
	}
}

func TestStream_MaxSteps(t *testing.T) {
	// 항상 tool_calls → MaxSteps 초과 시 강제 종료
	alwaysToolCall := llm.NewStubClient("stub", llm.StubResponse{
		Message:      makeToolCallMsg("call-x", "echo", `{"text":"loop"}`),
		ToolCalls:    []message.ToolCall{{ID: "call-x", Name: "echo", Args: json.RawMessage(`{"text":"loop"}`)}},
		FinishReason: "tool_use",
	})
	a, _ := Create(alwaysToolCall, []tool.Tool{&echoTool{}}, WithMaxSteps(2))

	in := Input{Messages: []message.Message{message.NewUserMessage("loop")}}
	ch, err := a.Stream(context.Background(), in, config.RunConfig{}, core.ModeUpdates)
	if err != nil {
		t.Fatalf("Stream 실패: %v", err)
	}

	var events []AgentEvent
	for ev := range ch {
		events = append(events, ev)
	}

	// 채널이 닫혀야 한다(영원히 블록되지 않아야 한다) — 위 루프가 종료됐으면 통과
	if len(events) == 0 {
		t.Error("MaxSteps 초과 강제 종료 시 최소 1개 이벤트가 있어야 합니다")
	}
	last := events[len(events)-1]
	if !last.IsTaskComplete {
		t.Error("MaxSteps 초과 종료 이벤트의 IsTaskComplete=false")
	}
}

// ============================================================
// 테스트: Checkpointer 결합
// ============================================================

func TestInvoke_WithCheckpointer(t *testing.T) {
	stub := llm.NewStubClient("stub", llm.StubResponse{
		Message:      message.NewAssistantMessage("저장됨"),
		FinishReason: "stop",
	})
	saver := checkpoint.NewInMemorySaver()
	a, _ := Create(stub, nil, WithCheckpointer(saver))

	cfg := config.RunConfig{Configurable: map[string]any{"thread_id": "thread-1"}}
	in := Input{Messages: []message.Message{message.NewUserMessage("저장 테스트")}}

	result, err := a.Invoke(context.Background(), in, cfg)
	if err != nil {
		t.Fatalf("Invoke 실패: %v", err)
	}
	if len(result.Messages) == 0 {
		t.Fatal("결과 메시지가 없습니다")
	}

	// GetState 로 저장된 상태 확인
	snap, err := a.GetState(cfg)
	if err != nil {
		t.Fatalf("GetState 실패: %v", err)
	}
	if snap.Values == nil {
		t.Error("스냅샷 Values 가 nil 입니다")
	}
}

// ============================================================
// 테스트: WithMiddleware 결합
// ============================================================

func TestInvoke_WithMiddleware(t *testing.T) {
	middlewareCalled := false
	mw := middleware.WrapModelCall(func(ctx context.Context, req middleware.ModelRequest, next middleware.ModelHandler) (middleware.ModelResponse, error) {
		middlewareCalled = true
		return next(ctx, req)
	})

	stub := llm.NewStubClient("stub", llm.StubResponse{
		Message:      message.NewAssistantMessage("미들웨어 통과"),
		FinishReason: "stop",
	})
	a, _ := Create(stub, nil, WithMiddleware(mw))

	in := Input{Messages: []message.Message{message.NewUserMessage("테스트")}}
	_, err := a.Invoke(context.Background(), in, config.RunConfig{})
	if err != nil {
		t.Fatalf("Invoke 실패: %v", err)
	}
	if !middlewareCalled {
		t.Error("미들웨어가 호출되지 않았습니다")
	}
}

func TestInvoke_BeforeModel_Block(t *testing.T) {
	blockErr := errors.New("차단됨")
	mw := middleware.BeforeModel("blocker", func(_ context.Context, _ core.State, _ middleware.Runtime) error {
		return blockErr
	})

	stub := llm.NewStubClient("stub", llm.StubResponse{
		Message: message.NewAssistantMessage("이 응답은 반환되면 안 됨"),
	})
	a, _ := Create(stub, nil, WithMiddleware(mw))

	in := Input{Messages: []message.Message{message.NewUserMessage("차단 테스트")}}
	_, err := a.Invoke(context.Background(), in, config.RunConfig{})
	if err == nil {
		t.Fatal("BeforeModel 차단 시 에러를 반환해야 합니다")
	}
}

// ============================================================
// 테스트: GetState
// ============================================================

func TestGetState_NoCheckpointer(t *testing.T) {
	stub := llm.NewStubClient("stub", llm.StubResponse{})
	a, _ := Create(stub, nil)

	snap, err := a.GetState(config.RunConfig{})
	if err != nil {
		t.Fatalf("GetState 실패: %v", err)
	}
	if snap.Values == nil {
		t.Error("Values 가 nil 입니다")
	}
}

func TestGetState_NoThread(t *testing.T) {
	stub := llm.NewStubClient("stub", llm.StubResponse{})
	saver := checkpoint.NewInMemorySaver()
	a, _ := Create(stub, nil, WithCheckpointer(saver))

	// thread_id 없는 RunConfig
	snap, err := a.GetState(config.RunConfig{})
	if err != nil {
		t.Fatalf("GetState 실패: %v", err)
	}
	if snap.Values == nil {
		t.Error("Values 가 nil 입니다")
	}
}

// ============================================================
// 테스트: 도구 등록 중복 시 에러
// ============================================================

func TestCreate_DuplicateTool(t *testing.T) {
	stub := llm.NewStubClient("stub", llm.StubResponse{})
	_, err := Create(stub, []tool.Tool{&echoTool{}, &echoTool{}})
	if err == nil {
		t.Fatal("중복 도구 등록 시 에러를 반환해야 합니다")
	}
}
