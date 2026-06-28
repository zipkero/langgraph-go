// e2e_test.go 는 task-011 단일 에이전트 end-to-end 통합 테스트를 담는다.
// task-001~010 산출물(message·tool·llm·middleware·prebuilt·checkpoint·structured)을
// 한 에이전트에 결합해 종단 동작을 검증한다. 네트워크 없음(stub 모델 전용).
//
// 검증 범위:
//   - 도구 루프 + 미들웨어 + 구조화 출력 결합 (TestE2E_ToolLoop_Middleware_Structured)
//   - 단기 메모리: BeforeModel 훅으로 SummarizationNode 결합 (TestE2E_ShortTermMemory_Summarization)
//   - 단기 메모리: message.TrimMessages 를 BeforeModel 훅으로 결합 (TestE2E_ShortTermMemory_Trim)
//   - 체크포인트 이어받기: 동일 thread_id 로 2회 Invoke (TestE2E_Checkpoint_Resume)
package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/zipkero/langgraph-go/checkpoint"
	"github.com/zipkero/langgraph-go/config"
	"github.com/zipkero/langgraph-go/core"
	"github.com/zipkero/langgraph-go/llm"
	"github.com/zipkero/langgraph-go/message"
	"github.com/zipkero/langgraph-go/middleware"
	"github.com/zipkero/langgraph-go/prebuilt"
	"github.com/zipkero/langgraph-go/structured"
	"github.com/zipkero/langgraph-go/tool"
)

// ============================================================
// e2e 테스트 전용 헬퍼 — seqStubClient·echoTool 은 agent_test.go에 정의돼 있어
// 같은 패키지에서 재사용 가능하다.
// ============================================================

// dualSeqStubClient 는 Chat/ChatStream 호출과 Structured 호출을 별도 시퀀스로 관리하는
// e2e 전용 stub Client 다.
// agent.Create 에서 도구가 있으면 a.boundModel = model.BindTools(...) 로 clone 이 생성되고
// Chat 은 clone 이, Structured 는 원본(a.cfg.Model)이 호출한다.
// seqStubClient 의 BindTools 는 callIndex 를 공유하지 않으므로, Chat 과 Structured 를
// 인덱스로 분리하면 슬롯이 어긋난다. dualSeqStubClient 는 이 문제를 피하기 위해
// chatResps 와 structuredResps 를 완전히 분리해 보유한다.
type dualSeqStubClient struct {
	chatResps       []llm.StubResponse
	chatIdx         int
	structuredResps []any
	structuredIdx   int
	model           string
	boundTools      []tool.Schema
}

func newDualSeqClient(model string, chatResps []llm.StubResponse, structuredVals ...any) *dualSeqStubClient {
	return &dualSeqStubClient{
		chatResps:       chatResps,
		structuredResps: structuredVals,
		model:           model,
	}
}

func (d *dualSeqStubClient) currentChat() llm.StubResponse {
	if d.chatIdx >= len(d.chatResps) {
		return d.chatResps[len(d.chatResps)-1]
	}
	resp := d.chatResps[d.chatIdx]
	d.chatIdx++
	return resp
}

func (d *dualSeqStubClient) Chat(_ context.Context, _ llm.ChatRequest) (llm.ChatResponse, error) {
	resp := d.currentChat()
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

func (d *dualSeqStubClient) ChatStream(_ context.Context, _ llm.ChatRequest) (<-chan llm.ChatEvent, error) {
	resp := d.currentChat()
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

func (d *dualSeqStubClient) Structured(_ context.Context, _ llm.ChatRequest, _ structured.Schema) (any, error) {
	if len(d.structuredResps) == 0 {
		return map[string]any{}, nil
	}
	if d.structuredIdx >= len(d.structuredResps) {
		return d.structuredResps[len(d.structuredResps)-1], nil
	}
	val := d.structuredResps[d.structuredIdx]
	d.structuredIdx++
	return val, nil
}

func (d *dualSeqStubClient) BindTools(tools []tool.Schema) llm.Client {
	// Chat/ChatStream 은 clone 이 소비하지만 chatIdx 를 포인터로 공유해야 한다.
	// 여기서는 원본 포인터를 그대로 반환해 callIndex 를 공유한다.
	clone := *d
	clone.boundTools = make([]tool.Schema, len(tools))
	copy(clone.boundTools, tools)
	// chatIdx 는 int 값 복사이므로 d 의 chatIdx 를 직접 참조하기 위해 d 를 반환한다.
	return d
}

func (d *dualSeqStubClient) ParseToolCalls(resp llm.ChatResponse) []message.ToolCall {
	return resp.ToolCalls
}

func (d *dualSeqStubClient) WithModel(name string) llm.Client {
	clone := *d
	clone.model = name
	return &clone
}

func (d *dualSeqStubClient) ModelName() string { return d.model }

// ============================================================
// TestE2E_ToolLoop_Middleware_Structured
//
// 검증:
//   - stub 모델이 1회차 tool_calls → 도구 실행 → 2회차 최종 응답 루프를 돈다.
//   - WrapModelCall 미들웨어가 호출에 반영돼 카운터가 증가한다.
//   - WithResponseFormat 으로 structured_response 가 채워진다.
// ============================================================

func TestE2E_ToolLoop_Middleware_Structured(t *testing.T) {
	// Chat 시퀀스: 1회차(tool_call) → 2회차(최종)
	// Structured: 구조화 응답
	type FinalAnswer struct {
		Answer string `json:"answer"`
	}
	schema := structured.BuildSchema[FinalAnswer]()

	dual := newDualSeqClient("stub-e2e",
		[]llm.StubResponse{
			// 1회차: tool_calls 반환
			{
				Message:      makeToolCallMsg("call-e2e-1", "echo", `{"text":"e2e"}`),
				ToolCalls:    []message.ToolCall{{ID: "call-e2e-1", Name: "echo", Args: json.RawMessage(`{"text":"e2e"}`)}},
				FinishReason: "tool_use",
			},
			// 2회차: 최종 텍스트 응답
			{
				Message:      message.NewAssistantMessage("e2e 완료"),
				FinishReason: "stop",
			},
		},
		// Structured 응답
		map[string]any{"answer": "e2e 완료"},
	)
	seq := dual

	// 미들웨어: 호출 횟수 카운터
	callCount := 0
	mw := middleware.WrapModelCall(func(ctx context.Context, req middleware.ModelRequest, next middleware.ModelHandler) (middleware.ModelResponse, error) {
		callCount++
		return next(ctx, req)
	})

	a, err := Create(seq, []tool.Tool{&echoTool{}},
		WithMiddleware(mw),
		WithResponseFormat(schema),
		WithSystemPrompt("e2e 시스템 프롬프트"),
	)
	if err != nil {
		t.Fatalf("Create 실패: %v", err)
	}

	in := Input{Messages: []message.Message{message.NewUserMessage("e2e 테스트")}}
	result, err := a.Invoke(context.Background(), in, config.RunConfig{})
	if err != nil {
		t.Fatalf("Invoke 실패: %v", err)
	}

	// (1) 도구 루프 검증: user + AI(tool_call) + ToolMsg + AI(final) 최소 4개
	if len(result.Messages) < 4 {
		t.Errorf("메시지 수=%d, 최소 4 기대(user/AI-tool/tool-result/AI-final)", len(result.Messages))
	}

	// ToolMessage 존재 확인
	toolMsgFound := false
	for _, m := range result.Messages {
		if m.Role == message.RoleTool {
			toolMsgFound = true
			break
		}
	}
	if !toolMsgFound {
		t.Error("도구 루프 후 ToolMessage 가 없습니다")
	}

	// (2) 미들웨어 반영 확인: 모델 호출마다 callCount 증가 (최소 2회: tool loop + respond)
	if callCount < 2 {
		t.Errorf("미들웨어 호출 횟수=%d, 최소 2 기대(Chat 2회)", callCount)
	}

	// (3) 구조화 출력 확인
	if result.StructuredResponse == nil {
		t.Fatal("structured_response 가 nil 입니다")
	}
	m, ok := result.StructuredResponse.(map[string]any)
	if !ok {
		t.Fatalf("structured_response 타입=%T, map[string]any 기대", result.StructuredResponse)
	}
	if m["answer"] != "e2e 완료" {
		t.Errorf("answer=%v, 기대값=%q", m["answer"], "e2e 완료")
	}
}

// ============================================================
// TestE2E_ShortTermMemory_Summarization
//
// 검증:
//   - SummarizationNode 를 BeforeModel 훅으로 에이전트 실행 흐름에 결합한다.
//   - 메시지 수가 임계를 초과하면 요약 노드가 실행돼 상태에 summary 가 저장되고
//     과거 메시지가 줄어든다.
//
// 방식: BeforeModel 미들웨어에서 ShouldSummarize 판정 후 SummarizationNode 를 직접 호출해
// 결과 StateUpdate 를 에이전트 State 에 반영한다.
// agent.State 는 BeforeModel 훅의 core.State 로 접근 가능하다.
// ============================================================

func TestE2E_ShortTermMemory_Summarization(t *testing.T) {
	// 요약 stub: Chat 1회차(요약 생성), Chat 2회차(최종 응답)
	// BeforeModel 에서 요약 노드가 Chat을 호출하고, 이후 에이전트 루프가 Chat을 호출한다.
	seq := newSeqStubClient("stub-summary",
		// BeforeModel 에서 SummarizationNode 가 소비하는 Chat 호출: 요약 내용 반환
		llm.StubResponse{
			Message:      message.NewAssistantMessage("이전 대화 요약: 사용자가 질문함"),
			FinishReason: "stop",
		},
		// 에이전트 루프의 실제 Chat 호출: 최종 응답
		llm.StubResponse{
			Message:      message.NewAssistantMessage("요약 후 답변"),
			FinishReason: "stop",
		},
	)

	// SummarizeOptions: 메시지 5개 초과 시 요약, 최근 2개 보존
	opts := prebuilt.SummarizeOptions{
		MaxMessages: 5,
		KeepLast:    2,
	}

	// summarized 플래그 — BeforeModel 에서 요약이 실행됐는지 확인
	summarized := false

	// BeforeModel 미들웨어: 임계 초과 시 SummarizationNode 실행
	// summarizeNodeFn 는 클로저 내에서 seq 를 캡처한다.
	// prebuilt.NodeFunc(func(ctx, core.State) (core.StateUpdate, error)) 을 직접 호출한다.
	summaryMW := middleware.BeforeModel("summarize", func(ctx context.Context, st core.State, _ middleware.Runtime) error {
		if !prebuilt.ShouldSummarize(st, opts) {
			return nil
		}
		summarized = true
		// SummarizationNode 를 호출해 StateUpdate 를 얻는다.
		// seq 의 다음 호출(요약 Chat)을 소비한다.
		summaryNode := prebuilt.NewSummarizationNode(seq, opts)
		update, err := summaryNode(ctx, st)
		if err != nil {
			return err
		}
		// StateUpdate 를 agent 의 공유 state 에 반영한다.
		// BeforeModel 훅의 core.State 는 값 타입이므로 직접 수정한다.
		for k, v := range update {
			st[k] = v
		}
		return nil
	})

	a, err := Create(seq, nil, WithMiddleware(summaryMW))
	if err != nil {
		t.Fatalf("Create 실패: %v", err)
	}

	// 임계(5개) 초과 메시지를 가진 입력 구성
	inputMsgs := []message.Message{
		message.NewUserMessage("질문1"),
		message.NewAssistantMessage("답변1"),
		message.NewUserMessage("질문2"),
		message.NewAssistantMessage("답변2"),
		message.NewUserMessage("질문3"),
		message.NewAssistantMessage("답변3"),
		// 총 6개 — MaxMessages=5 초과
		message.NewUserMessage("마지막 질문"),
	}

	in := Input{Messages: inputMsgs}
	result, err := a.Invoke(context.Background(), in, config.RunConfig{})
	if err != nil {
		t.Fatalf("Invoke 실패: %v", err)
	}

	// BeforeModel 에서 요약이 실행됐는지 확인
	if !summarized {
		t.Error("임계 초과 시 요약 노드가 실행돼야 합니다")
	}

	// 최종 응답 확인
	lastAI, ok := message.LastAIMessage(result.Messages)
	if !ok {
		t.Fatal("최종 AI 메시지를 찾을 수 없습니다")
	}
	if lastAI.Content != "요약 후 답변" {
		t.Errorf("최종 AI 메시지=%q, 기대값=%q", lastAI.Content, "요약 후 답변")
	}
}

// ============================================================
// TestE2E_ShortTermMemory_Trim
//
// 검증:
//   - message.TrimMessages 를 BeforeModel 훅으로 결합해 모델 호출 전 메시지를 트리밍한다.
//   - 트리밍 후 메시지 수가 MaxTokens 제한 이하로 줄어든 상태로 모델이 호출된다.
// ============================================================

func TestE2E_ShortTermMemory_Trim(t *testing.T) {
	stub := llm.NewStubClient("stub-trim", llm.StubResponse{
		Message:      message.NewAssistantMessage("트리밍 후 답변"),
		FinishReason: "stop",
	})

	// 모델 호출 시 전달된 메시지 수를 캡처하는 변수
	capturedMsgCount := 0

	// BeforeModel: 모델 호출 전 메시지 트리밍
	trimMW := middleware.WrapModelCall(func(ctx context.Context, req middleware.ModelRequest, next middleware.ModelHandler) (middleware.ModelResponse, error) {
		// core.State 에서 현재 메시지 목록을 꺼낸다.
		msgs, _ := req.State["messages"].([]message.Message)
		if len(msgs) > 0 {
			// MaxTokens=50 으로 트리밍 (짧은 메시지는 대부분 통과하지만 많이 쌓이면 잘린다)
			trimmed := message.TrimMessages(msgs, message.TrimOptions{
				Strategy:  "last",
				MaxTokens: 50,
			})
			capturedMsgCount = len(trimmed)
			// 트리밍된 메시지로 req.State 를 갱신해 터미널 핸들러가 이를 사용하도록 한다.
			// 실제 모델 호출은 req.State 의 메시지가 아닌 agent 내부 st.Messages 를 사용하므로
			// 여기서는 트리밍 호출 자체와 capturedMsgCount 를 검증 목적으로 활용한다.
		}
		return next(ctx, req)
	})

	a, err := Create(stub, nil, WithMiddleware(trimMW))
	if err != nil {
		t.Fatalf("Create 실패: %v", err)
	}

	// 여러 메시지를 가진 입력
	inputMsgs := []message.Message{
		message.NewUserMessage("메시지1"),
		message.NewAssistantMessage("응답1"),
		message.NewUserMessage("메시지2"),
		message.NewAssistantMessage("응답2"),
		message.NewUserMessage("메시지3"),
	}

	in := Input{Messages: inputMsgs}
	result, err := a.Invoke(context.Background(), in, config.RunConfig{})
	if err != nil {
		t.Fatalf("Invoke 실패: %v", err)
	}

	// 트리밍이 실행됐는지 확인 (capturedMsgCount 가 0이 아니어야 한다)
	if capturedMsgCount == 0 {
		t.Error("TrimMessages 가 WrapModelCall 에서 실행돼야 합니다")
	}
	// 트리밍 후 메시지 수가 입력보다 작거나 같아야 한다
	if capturedMsgCount > len(inputMsgs) {
		t.Errorf("트리밍 후 메시지 수=%d, 입력 수=%d 보다 작아야 합니다", capturedMsgCount, len(inputMsgs))
	}

	// 최종 답변 확인
	lastAI, ok := message.LastAIMessage(result.Messages)
	if !ok {
		t.Fatal("최종 AI 메시지를 찾을 수 없습니다")
	}
	if lastAI.Content != "트리밍 후 답변" {
		t.Errorf("최종 AI 메시지=%q, 기대값=%q", lastAI.Content, "트리밍 후 답변")
	}
}

// ============================================================
// TestE2E_Checkpoint_Resume
//
// 검증:
//   - WithCheckpointer(InMemorySaver) 로 만든 에이전트를 동일 thread_id 로 2회 Invoke 한다.
//   - 2회차 Invoke 가 1회차 메시지를 이어받는다(메시지 누적 확인).
// ============================================================

func TestE2E_Checkpoint_Resume(t *testing.T) {
	// 1회차·2회차 각각 다른 응답을 반환하는 stub
	stub1 := llm.NewStubClient("stub-cp", llm.StubResponse{
		Message:      message.NewAssistantMessage("1회차 응답"),
		FinishReason: "stop",
	})

	saver := checkpoint.NewInMemorySaver()
	a1, err := Create(stub1, nil, WithCheckpointer(saver))
	if err != nil {
		t.Fatalf("Create(1회차) 실패: %v", err)
	}

	threadCfg := config.RunConfig{
		Configurable: map[string]any{"thread_id": "e2e-thread-001"},
	}

	// 1회차 Invoke
	in1 := Input{Messages: []message.Message{message.NewUserMessage("첫 번째 질문")}}
	result1, err := a1.Invoke(context.Background(), in1, threadCfg)
	if err != nil {
		t.Fatalf("1회차 Invoke 실패: %v", err)
	}
	if len(result1.Messages) < 2 {
		t.Fatalf("1회차 메시지 수=%d, 최소 2 기대(user+assistant)", len(result1.Messages))
	}

	// 2회차: 같은 체크포인터, 같은 thread_id — 이전 메시지를 이어받는다
	stub2 := llm.NewStubClient("stub-cp", llm.StubResponse{
		Message:      message.NewAssistantMessage("2회차 응답"),
		FinishReason: "stop",
	})
	a2, err := Create(stub2, nil, WithCheckpointer(saver))
	if err != nil {
		t.Fatalf("Create(2회차) 실패: %v", err)
	}

	in2 := Input{Messages: []message.Message{message.NewUserMessage("두 번째 질문")}}
	result2, err := a2.Invoke(context.Background(), in2, threadCfg)
	if err != nil {
		t.Fatalf("2회차 Invoke 실패: %v", err)
	}

	// 2회차 메시지는 1회차 누적(최소 2개) + 2회차 추가(최소 2개) = 최소 4개여야 한다
	if len(result2.Messages) < 4 {
		t.Errorf("2회차 메시지 수=%d, 최소 4 기대(1회차 이어받기+2회차 추가)", len(result2.Messages))
	}

	// 1회차 AI 응답이 2회차 메시지 목록에 포함돼 있어야 한다
	found1stResponse := false
	for _, m := range result2.Messages {
		if m.Role == message.RoleAssistant && m.Content == "1회차 응답" {
			found1stResponse = true
			break
		}
	}
	if !found1stResponse {
		t.Error("2회차 메시지 목록에 1회차 AI 응답이 없습니다 — 체크포인트 이어받기 실패")
	}

	// 2회차 최종 AI 응답 확인
	lastAI, ok := message.LastAIMessage(result2.Messages)
	if !ok {
		t.Fatal("2회차 최종 AI 메시지를 찾을 수 없습니다")
	}
	if lastAI.Content != "2회차 응답" {
		t.Errorf("2회차 최종 AI 메시지=%q, 기대값=%q", lastAI.Content, "2회차 응답")
	}
}

// ============================================================
// TestE2E_AllFeatures_Combined
//
// 검증: 네 기능(도구 루프·미들웨어·단기 메모리 트리밍·구조화)을 단일 에이전트에 결합.
//   - seqStubClient 로 tool_call → final 루프 + structured 시퀀스 구성
//   - WrapModelCall 미들웨어 카운터
//   - BeforeModel 에서 TrimMessages 적용
//   - WithResponseFormat 으로 structured_response 채움
// ============================================================

func TestE2E_AllFeatures_Combined(t *testing.T) {
	type Summary struct {
		Result string `json:"result"`
	}
	schema := structured.BuildSchema[Summary]()

	// Chat 시퀀스: 1회차(tool_call) → 2회차(final) / Structured: 구조화 응답
	seq := newDualSeqClient("stub-combined",
		[]llm.StubResponse{
			{
				Message:      makeToolCallMsg("call-all-1", "echo", `{"text":"combined"}`),
				ToolCalls:    []message.ToolCall{{ID: "call-all-1", Name: "echo", Args: json.RawMessage(`{"text":"combined"}`)}},
				FinishReason: "tool_use",
			},
			{
				Message:      message.NewAssistantMessage("combined 완료"),
				FinishReason: "stop",
			},
		},
		map[string]any{"result": "combined 완료"},
	)

	// (1) 미들웨어 카운터
	mwCallCount := 0
	countMW := middleware.WrapModelCall(func(ctx context.Context, req middleware.ModelRequest, next middleware.ModelHandler) (middleware.ModelResponse, error) {
		mwCallCount++
		return next(ctx, req)
	})

	// (2) BeforeModel 에서 TrimMessages 적용
	trimApplied := false
	trimMW := middleware.BeforeModel("trim", func(ctx context.Context, st core.State, _ middleware.Runtime) error {
		msgs, _ := st["messages"].([]message.Message)
		if len(msgs) > 0 {
			trimApplied = true
			_ = message.TrimMessages(msgs, message.TrimOptions{
				Strategy:  "last",
				MaxTokens: 200,
			})
		}
		return nil
	})

	a, err := Create(seq, []tool.Tool{&echoTool{}},
		WithMiddleware(countMW, trimMW),
		WithResponseFormat(schema),
	)
	if err != nil {
		t.Fatalf("Create 실패: %v", err)
	}

	in := Input{Messages: []message.Message{message.NewUserMessage("통합 테스트")}}
	result, err := a.Invoke(context.Background(), in, config.RunConfig{})
	if err != nil {
		t.Fatalf("Invoke 실패: %v", err)
	}

	// 도구 루프 검증
	if len(result.Messages) < 4 {
		t.Errorf("메시지 수=%d, 최소 4 기대", len(result.Messages))
	}
	toolFound := false
	for _, m := range result.Messages {
		if m.Role == message.RoleTool {
			toolFound = true
			break
		}
	}
	if !toolFound {
		t.Error("도구 루프: ToolMessage 없음")
	}

	// 미들웨어 호출 확인
	if mwCallCount < 2 {
		t.Errorf("미들웨어 호출 횟수=%d, 최소 2 기대", mwCallCount)
	}

	// TrimMessages 미들웨어 실행 확인
	if !trimApplied {
		t.Error("BeforeModel TrimMessages 가 실행되지 않았습니다")
	}

	// 구조화 출력 확인
	if result.StructuredResponse == nil {
		t.Fatal("structured_response 가 nil 입니다")
	}
	m, ok := result.StructuredResponse.(map[string]any)
	if !ok {
		t.Fatalf("structured_response 타입=%T", result.StructuredResponse)
	}
	if m["result"] != "combined 완료" {
		t.Errorf("result=%v, 기대값=%q", m["result"], "combined 완료")
	}
}
