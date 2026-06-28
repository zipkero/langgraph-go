// agent 패키지는 ReAct 루프를 직접 구현하는 통합 에이전트를 담당한다.
// message·llm·tool·structured·middleware·checkpoint·core·config에 의존하며,
// graph·command·streaming(Phase 2) 패키지는 참조하지 않는다(§28-1 규칙1, D2).
package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/zipkero/langgraph-go/checkpoint"
	"github.com/zipkero/langgraph-go/config"
	"github.com/zipkero/langgraph-go/core"
	"github.com/zipkero/langgraph-go/llm"
	"github.com/zipkero/langgraph-go/message"
	"github.com/zipkero/langgraph-go/middleware"
	"github.com/zipkero/langgraph-go/structured"
	"github.com/zipkero/langgraph-go/tool"
)

// defaultMaxSteps 는 MaxSteps 미지정 시 적용되는 기본 최대 스텝 수다.
const defaultMaxSteps = 25

// Decision 은 shouldContinue 가 반환하는 루프 분기 결정이다.
type Decision string

const (
	// DecisionContinue 는 미처리 tool_calls 가 있으므로 도구를 실행하고 루프를 계속한다.
	DecisionContinue Decision = "continue"
	// DecisionRespond 는 ResponseFormat 이 지정됐고 structured_response 가 아직 비어 있으므로
	// 구조화 출력을 생성하고 종료한다.
	DecisionRespond Decision = "respond"
	// DecisionEnd 는 정상 종료한다.
	DecisionEnd Decision = "end"
)

// State 는 에이전트 실행 중 누적되는 내부 상태다.
// core.State(map[string]any)를 기반으로 "messages" 키에 메시지 목록을,
// "structured_response" 키에 구조화 응답을 담는다.
type State struct {
	// Messages 는 현재까지 누적된 대화 메시지 목록이다.
	Messages []message.Message
	// StructuredResponse 는 applyResponseFormat 이 생성한 구조화 응답이다.
	// ResponseFormat 이 미지정이거나 아직 생성 전이면 nil 이다.
	StructuredResponse any
}

// toCoreState 는 State 를 core.State 맵으로 변환한다.
func (s State) toCoreState() core.State {
	return core.State{
		"messages":            s.Messages,
		"structured_response": s.StructuredResponse,
	}
}

// stateFromCore 는 core.State 맵에서 State 를 복원한다.
func stateFromCore(cs core.State) State {
	var st State
	if msgs, ok := cs["messages"]; ok {
		if msgSlice, ok := msgs.([]message.Message); ok {
			st.Messages = msgSlice
		}
	}
	if sr, ok := cs["structured_response"]; ok {
		st.StructuredResponse = sr
	}
	return st
}

// Input 은 Invoke/Stream 에 전달하는 입력이다.
type Input struct {
	// Messages 는 이번 호출에 추가할 메시지 목록이다.
	Messages []message.Message
}

// Result 는 Invoke 가 반환하는 실행 결과다.
type Result struct {
	// Messages 는 루프 종료 후 누적된 전체 대화 메시지 목록이다.
	Messages []message.Message
	// StructuredResponse 는 WithResponseFormat 지정 시 종료 직전 생성된 구조화 응답이다.
	// ResponseFormat 이 미지정이면 nil 이다.
	StructuredResponse any
}

// AgentEvent 는 Stream 이 방출하는 진행 이벤트다.
type AgentEvent struct {
	// IsTaskComplete 는 루프가 완전히 종료됐음을 나타낸다.
	IsTaskComplete bool
	// RequireUserInput 은 에이전트가 사용자 입력을 기다리고 있음을 나타낸다.
	// Phase 1에서는 인터럽트/resume 없이 이벤트 노출만 한다.
	RequireUserInput bool
	// Content 는 이 이벤트의 텍스트 내용이다(토큰·업데이트 등).
	Content string
	// Node 는 이 이벤트를 방출한 노드 이름이다.
	Node string
	// Token 은 토큰 단위 스트리밍 이벤트에서 방출된 텍스트 조각이다.
	Token string
	// Update 는 상태 업데이트 이벤트에서 변경된 상태 맵이다.
	Update core.StateUpdate
	// Error 는 루프 실행 중 발생한 에러다.
	Error error
}

// Config 는 에이전트 생성 및 실행에 필요한 설정이다.
type Config struct {
	// Model 은 에이전트가 사용하는 LLM 클라이언트다.
	Model llm.Client
	// Tools 는 에이전트에 바인딩된 도구 목록이다.
	Tools []tool.Tool
	// SystemPrompt 는 에이전트의 기본 시스템 프롬프트다.
	SystemPrompt string
	// Middleware 는 모델 호출에 적용할 미들웨어 목록이다.
	Middleware []middleware.Middleware
	// Checkpointer 는 스레드 단위 상태 영속화에 사용할 체크포인터다.
	// nil 이면 체크포인트를 저장하지 않는다.
	Checkpointer checkpoint.Checkpointer
	// Store 는 도구 실행 시 주입할 스토어 인터페이스다.
	Store tool.Store
	// ResponseFormat 은 종료 직전 구조화 응답을 생성하기 위한 스키마다.
	// nil 이면 구조화 출력을 생성하지 않는다.
	ResponseFormat *structured.Schema
	// MaxSteps 는 루프 최대 반복 수다. 0이면 defaultMaxSteps(25)를 사용한다.
	MaxSteps int
}

// Agent 는 ReAct 루프를 직접 실행하는 에이전트다.
// 그래프 엔진 없이 runModel/runTools/shouldContinue 직접 루프로 동작한다(D2).
type Agent struct {
	cfg      Config
	registry *tool.Registry
	executor *tool.Executor
	// boundModel 은 도구 스키마가 바인딩된 llm.Client 다.
	boundModel llm.Client
	// middlewareChain 은 합성된 미들웨어 체인이다.
	middlewareChain *middleware.Chain
}

// Option 은 Agent 생성에 전달하는 옵션 함수 타입이다.
type Option func(*Config)

// WithSystemPrompt 는 시스템 프롬프트를 지정하는 옵션이다.
func WithSystemPrompt(prompt string) Option {
	return func(c *Config) {
		c.SystemPrompt = prompt
	}
}

// WithMiddleware 는 미들웨어를 추가하는 옵션이다.
func WithMiddleware(mws ...middleware.Middleware) Option {
	return func(c *Config) {
		c.Middleware = append(c.Middleware, mws...)
	}
}

// WithCheckpointer 는 체크포인터를 지정하는 옵션이다.
func WithCheckpointer(cp checkpoint.Checkpointer) Option {
	return func(c *Config) {
		c.Checkpointer = cp
	}
}

// WithStore 는 도구 스토어를 지정하는 옵션이다.
func WithStore(s tool.Store) Option {
	return func(c *Config) {
		c.Store = s
	}
}

// WithResponseFormat 은 구조화 응답 스키마를 지정하는 옵션이다.
// 지정하면 루프 종료 직전 해당 스키마로 llm.Structured 를 호출해 structured_response 를 채운다.
func WithResponseFormat(schema structured.Schema) Option {
	return func(c *Config) {
		c.ResponseFormat = &schema
	}
}

// WithMaxSteps 는 루프 최대 반복 수를 지정하는 옵션이다.
// 0 이하이면 defaultMaxSteps(25)를 사용한다.
func WithMaxSteps(n int) Option {
	return func(c *Config) {
		if n > 0 {
			c.MaxSteps = n
		}
	}
}

// Create 는 model 과 tools 로 새 Agent 를 생성한다.
// tools 는 레지스트리에 등록되고, model 에 도구 스키마가 바인딩된다.
func Create(model llm.Client, tools []tool.Tool, opts ...Option) (*Agent, error) {
	if model == nil {
		return nil, fmt.Errorf("agent: model 은 nil 일 수 없습니다")
	}

	cfg := Config{
		Model: model,
		Tools: tools,
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.MaxSteps <= 0 {
		cfg.MaxSteps = defaultMaxSteps
	}

	// 도구 레지스트리 구성
	reg := tool.NewRegistry()
	for _, t := range tools {
		if err := reg.Register(t); err != nil {
			return nil, fmt.Errorf("agent: 도구 등록 실패: %w", err)
		}
	}
	exec := tool.NewExecutor(reg)

	// 도구 스키마를 모델에 바인딩
	var boundModel llm.Client
	if len(tools) > 0 {
		boundModel = model.BindTools(reg.Schemas())
	} else {
		boundModel = model
	}

	// 미들웨어 체인 구성
	chain := middleware.NewChain(cfg.Middleware...)

	return &Agent{
		cfg:             cfg,
		registry:        reg,
		executor:        exec,
		boundModel:      boundModel,
		middlewareChain: chain,
	}, nil
}

// Invoke 는 in 을 입력으로 ReAct 루프를 실행하고 Result 를 반환한다.
// 루프는 shouldContinue 가 DecisionEnd/DecisionRespond 를 반환할 때까지 반복한다.
// MaxSteps 초과 시 강제 종료한다.
func (a *Agent) Invoke(ctx context.Context, in Input, cfg config.RunConfig) (Result, error) {
	// 체크포인터에서 이전 상태 복원
	st, err := a.loadState(ctx, cfg)
	if err != nil {
		return Result{}, err
	}

	// 입력 메시지 누적
	st.Messages = message.AddMessages(st.Messages, in.Messages)

	// ReAct 루프
	for step := 0; step < a.cfg.MaxSteps; step++ {
		// 모델 호출
		aiMsg, err := a.runModel(ctx, st, cfg)
		if err != nil {
			return Result{}, fmt.Errorf("agent: runModel 실패(step=%d): %w", step, err)
		}
		st.Messages = append(st.Messages, aiMsg)

		// 루프 계속 여부 판정
		switch a.shouldContinue(st) {
		case DecisionContinue:
			// 도구 실행
			toolMsgs, err := a.runTools(ctx, st, cfg)
			if err != nil {
				return Result{}, fmt.Errorf("agent: runTools 실패(step=%d): %w", step, err)
			}
			st.Messages = append(st.Messages, toolMsgs...)

		case DecisionRespond:
			// 구조화 출력 생성
			structured, err := a.applyResponseFormat(ctx, st, cfg)
			if err != nil {
				return Result{}, fmt.Errorf("agent: applyResponseFormat 실패: %w", err)
			}
			st.StructuredResponse = structured
			// 체크포인트 저장 후 종료
			if err := a.saveState(ctx, cfg, st); err != nil {
				return Result{}, err
			}
			return Result{
				Messages:           st.Messages,
				StructuredResponse: st.StructuredResponse,
			}, nil

		case DecisionEnd:
			// 체크포인트 저장 후 종료
			if err := a.saveState(ctx, cfg, st); err != nil {
				return Result{}, err
			}
			return Result{
				Messages:           st.Messages,
				StructuredResponse: st.StructuredResponse,
			}, nil
		}
	}

	// MaxSteps 초과 — 강제 종료
	if err := a.saveState(ctx, cfg, st); err != nil {
		return Result{}, err
	}
	return Result{
		Messages:           st.Messages,
		StructuredResponse: st.StructuredResponse,
	}, nil
}

// Stream 은 ReAct 루프를 실행하면서 AgentEvent 를 채널로 방출한다.
// mode 는 core.Mode 이며(ModeMessages/ModeValues/ModeUpdates/ModeDebug) Phase 1에서는
// 채널 방출 형식에 반영된다.
// 루프 완료 시 IsTaskComplete=true 인 이벤트를 방출하고 채널을 닫는다.
func (a *Agent) Stream(ctx context.Context, in Input, cfg config.RunConfig, mode core.Mode) (<-chan AgentEvent, error) {
	ch := make(chan AgentEvent, 32)

	go func() {
		defer close(ch)

		// 체크포인터에서 이전 상태 복원
		st, err := a.loadState(ctx, cfg)
		if err != nil {
			ch <- AgentEvent{Error: err}
			return
		}

		// 입력 메시지 누적
		st.Messages = message.AddMessages(st.Messages, in.Messages)

		for step := 0; step < a.cfg.MaxSteps; step++ {
			// 모델 스트림 호출
			aiMsg, tokens, err := a.runModelStream(ctx, st, cfg, ch, mode)
			if err != nil {
				ch <- AgentEvent{Error: fmt.Errorf("agent: Stream runModel 실패(step=%d): %w", step, err)}
				return
			}
			st.Messages = append(st.Messages, aiMsg)

			// 업데이트 이벤트 방출
			if mode == core.ModeUpdates || mode == core.ModeDebug {
				ch <- AgentEvent{
					Node:    "agent",
					Content: fmt.Sprintf("step=%d tokens=%d", step, tokens),
					Update:  core.StateUpdate{"messages": st.Messages},
				}
			}

			switch a.shouldContinue(st) {
			case DecisionContinue:
				toolMsgs, err := a.runTools(ctx, st, cfg)
				if err != nil {
					ch <- AgentEvent{Error: fmt.Errorf("agent: Stream runTools 실패(step=%d): %w", step, err)}
					return
				}
				st.Messages = append(st.Messages, toolMsgs...)
				// 도구 실행 업데이트 이벤트
				ch <- AgentEvent{
					Node:    "tools",
					Content: fmt.Sprintf("도구 %d 개 실행", len(toolMsgs)),
					Update:  core.StateUpdate{"messages": st.Messages},
				}

			case DecisionRespond:
				sr, err := a.applyResponseFormat(ctx, st, cfg)
				if err != nil {
					ch <- AgentEvent{Error: fmt.Errorf("agent: Stream applyResponseFormat 실패: %w", err)}
					return
				}
				st.StructuredResponse = sr
				_ = a.saveState(ctx, cfg, st)
				ch <- AgentEvent{
					IsTaskComplete: true,
					Node:           "agent",
					Update:         core.StateUpdate{"structured_response": sr},
				}
				return

			case DecisionEnd:
				_ = a.saveState(ctx, cfg, st)
				ch <- AgentEvent{
					IsTaskComplete: true,
					Node:           "agent",
					Content:        aiMsg.Content,
				}
				return
			}
		}

		// MaxSteps 초과 — 강제 종료
		_ = a.saveState(ctx, cfg, st)
		ch <- AgentEvent{
			IsTaskComplete: true,
			Node:           "agent",
			Content:        "MaxSteps 초과로 강제 종료",
		}
	}()

	return ch, nil
}

// GetState 는 cfg 에서 thread_id 를 뽑아 현재 상태 스냅샷을 반환한다.
// Checkpointer 가 없거나 thread_id 가 없으면 빈 스냅샷을 반환한다.
func (a *Agent) GetState(cfg config.RunConfig) (core.StateSnapshot, error) {
	if a.cfg.Checkpointer == nil {
		return core.StateSnapshot{
			Values:    core.State{},
			Config:    cfg,
			CreatedAt: time.Now(),
		}, nil
	}

	threadID, err := checkpoint.ThreadIDFromConfig(cfg)
	if err != nil {
		return core.StateSnapshot{
			Values:    core.State{},
			Config:    cfg,
			CreatedAt: time.Now(),
		}, nil
	}

	cp, ok, err := a.cfg.Checkpointer.Get(context.Background(), threadID)
	if err != nil {
		return core.StateSnapshot{}, fmt.Errorf("agent: GetState 조회 실패: %w", err)
	}
	if !ok {
		return core.StateSnapshot{
			Values:    core.State{},
			Config:    cfg,
			CreatedAt: time.Now(),
		}, nil
	}

	return core.StateSnapshot{
		Values:    cp.Values,
		Next:      cp.Next,
		Config:    cfg,
		Metadata:  cp.Metadata,
		CreatedAt: cp.CreatedAt,
	}, nil
}

// runModel 은 미들웨어 체인을 거쳐 llm.Client.Chat 을 호출하고 AI 메시지를 반환한다.
// 미들웨어가 없으면 직접 Chat 을 호출한다.
func (a *Agent) runModel(ctx context.Context, st State, cfg config.RunConfig) (message.Message, error) {
	// 미들웨어 체인의 터미널 핸들러: 실제 모델 호출
	terminal := func(ctx context.Context, req middleware.ModelRequest) (middleware.ModelResponse, error) {
		model := req.Model
		if model == nil {
			model = a.boundModel
		}

		// 메시지 구성: 시스템 프롬프트가 있으면 앞에 삽입
		msgs := a.buildMessages(st.Messages, req.SystemPrompt)

		resp, err := model.Chat(ctx, llm.ChatRequest{
			Messages: msgs,
		})
		if err != nil {
			return middleware.ModelResponse{}, err
		}
		return middleware.ModelResponse{Response: resp}, nil
	}

	req := middleware.ModelRequest{
		State:        st.toCoreState(),
		Model:        a.boundModel,
		SystemPrompt: a.cfg.SystemPrompt,
	}

	handler := a.middlewareChain.Handler(terminal)
	resp, err := handler(ctx, req)
	if err != nil {
		return message.Message{}, err
	}

	// tool_calls 가 있으면 ToolCalls 필드를 포함한 메시지로 구성
	aiMsg := resp.Response.Message
	if len(resp.Response.ToolCalls) > 0 {
		aiMsg = message.NewAssistantToolCalls(resp.Response.ToolCalls)
		aiMsg.Content = resp.Response.Message.Content
	}

	return aiMsg, nil
}

// runModelStream 은 스트리밍 모드에서 모델을 호출하고 토큰 이벤트를 ch 에 방출하며
// 완성된 AI 메시지와 토큰 수를 반환한다.
func (a *Agent) runModelStream(ctx context.Context, st State, cfg config.RunConfig, ch chan<- AgentEvent, mode core.Mode) (message.Message, int, error) {
	msgs := a.buildMessages(st.Messages, a.cfg.SystemPrompt)

	events, err := a.boundModel.ChatStream(ctx, llm.ChatRequest{
		Messages: msgs,
	})
	if err != nil {
		return message.Message{}, 0, err
	}

	var aiMsg message.Message
	tokenCount := 0
	for ev := range events {
		switch ev.Type {
		case llm.ChatEventToken:
			tokenCount++
			if mode == core.ModeMessages || mode == core.ModeDebug {
				ch <- AgentEvent{
					Node:  "agent",
					Token: ev.Token,
				}
			}
		case llm.ChatEventMessage:
			if ev.Message != nil {
				aiMsg = *ev.Message
			}
		case llm.ChatEventDone:
			if ev.Response != nil && len(ev.Response.ToolCalls) > 0 {
				aiMsg = message.NewAssistantToolCalls(ev.Response.ToolCalls)
				aiMsg.Content = ev.Response.Message.Content
			}
		}
	}

	return aiMsg, tokenCount, nil
}

// runTools 는 마지막 AI 메시지의 tool_calls 를 tool.Executor 로 디스패치해
// ToolMessage 목록을 반환한다.
func (a *Agent) runTools(ctx context.Context, st State, cfg config.RunConfig) ([]message.Message, error) {
	lastAI, ok := message.LastAIMessage(st.Messages)
	if !ok || !message.HasToolCalls(lastAI) {
		return nil, nil
	}

	calls := message.ExtractToolCalls(lastAI)
	rt := tool.NewRuntime(st.toCoreState(), "", cfg, a.cfg.Store, nil)
	toolMsgs, err := a.executor.ExecuteMany(ctx, calls, rt)
	if err != nil {
		return nil, err
	}
	return toolMsgs, nil
}

// shouldContinue 는 현재 상태를 보고 루프 분기 결정을 반환한다.
// - 마지막 AI 메시지에 미처리 tool_calls 가 있으면 DecisionContinue
// - tool_calls 가 없고 ResponseFormat 이 지정됐으며 structured_response 가 비어 있으면 DecisionRespond
// - 그 외는 DecisionEnd
func (a *Agent) shouldContinue(st State) Decision {
	lastAI, ok := message.LastAIMessage(st.Messages)
	if ok && message.HasToolCalls(lastAI) {
		return DecisionContinue
	}

	if a.cfg.ResponseFormat != nil && st.StructuredResponse == nil {
		return DecisionRespond
	}

	return DecisionEnd
}

// applyResponseFormat 은 현재 대화 컨텍스트로 llm.Structured 를 호출해
// structured_response 값을 생성한다.
func (a *Agent) applyResponseFormat(ctx context.Context, st State, cfg config.RunConfig) (any, error) {
	if a.cfg.ResponseFormat == nil {
		return nil, nil
	}

	msgs := a.buildMessages(st.Messages, a.cfg.SystemPrompt)
	result, err := a.cfg.Model.Structured(ctx, llm.ChatRequest{
		Messages: msgs,
	}, *a.cfg.ResponseFormat)
	if err != nil {
		return nil, fmt.Errorf("agent: Structured 호출 실패: %w", err)
	}
	return result, nil
}

// buildMessages 는 systemPrompt 가 비어 있지 않으면 앞에 SystemMessage 를 추가해 반환한다.
func (a *Agent) buildMessages(msgs []message.Message, systemPrompt string) []message.Message {
	if systemPrompt == "" {
		return msgs
	}
	// 이미 맨 앞이 system 메시지이면 교체하지 않고 그 앞에 삽입하지 않는다
	// (동일 시스템 프롬프트 중복 방지는 호출자 책임으로 한다)
	result := make([]message.Message, 0, len(msgs)+1)
	result = append(result, message.NewSystemMessage(systemPrompt))
	result = append(result, msgs...)
	return result
}

// loadState 는 Checkpointer 에서 이전 상태를 복원한다.
// Checkpointer 가 없거나 thread_id 가 없으면 빈 상태를 반환한다.
func (a *Agent) loadState(ctx context.Context, cfg config.RunConfig) (State, error) {
	if a.cfg.Checkpointer == nil {
		return State{}, nil
	}

	saver, ok := a.cfg.Checkpointer.(*checkpoint.InMemorySaver)
	if !ok {
		// 일반 Checkpointer 인터페이스에서 LoadState 를 직접 지원하지 않으므로
		// Get 으로 조회 후 변환한다.
		threadID, err := checkpoint.ThreadIDFromConfig(cfg)
		if err != nil {
			// thread_id 가 없으면 빈 상태로 시작
			return State{}, nil
		}
		cp, found, err := a.cfg.Checkpointer.Get(ctx, threadID)
		if err != nil {
			return State{}, fmt.Errorf("agent: 상태 복원 실패: %w", err)
		}
		if !found {
			return State{}, nil
		}
		return stateFromCore(cp.Values), nil
	}

	coreState, found, err := saver.LoadState(ctx, cfg)
	if err != nil {
		if err == checkpoint.ErrNoThreadID {
			return State{}, nil
		}
		return State{}, fmt.Errorf("agent: 상태 복원 실패: %w", err)
	}
	if !found {
		return State{}, nil
	}
	return stateFromCore(coreState), nil
}

// saveState 는 현재 상태를 Checkpointer 에 저장한다.
// Checkpointer 가 없거나 thread_id 가 없으면 아무것도 하지 않는다.
func (a *Agent) saveState(ctx context.Context, cfg config.RunConfig, st State) error {
	if a.cfg.Checkpointer == nil {
		return nil
	}

	saver, ok := a.cfg.Checkpointer.(*checkpoint.InMemorySaver)
	if !ok {
		threadID, err := checkpoint.ThreadIDFromConfig(cfg)
		if err != nil {
			return nil
		}
		return a.cfg.Checkpointer.Put(ctx, threadID, checkpoint.Checkpoint{
			ThreadID:  threadID,
			Values:    st.toCoreState(),
			CreatedAt: time.Now(),
		})
	}

	if err := saver.SaveState(ctx, cfg, st.toCoreState()); err != nil {
		if err == checkpoint.ErrNoThreadID {
			return nil
		}
		return fmt.Errorf("agent: 상태 저장 실패: %w", err)
	}
	return nil
}
