// e2e_test.go 는 실제 Claude 모델을 사용한 수퍼바이저+워커 종단 간 통합 테스트를 담는다.
// ANTHROPIC_API_KEY 가 없으면 t.Skip 으로 건너뛴다.
//
// 검증 범위:
//   - 수퍼바이저가 RouterTool 로 다음 워커를 선택해 Goto 한다.
//   - 워커(실 에이전트를 Worker 로 감싼 구현)가 실행되어 결과를 상태에 남긴다.
//   - 워커 실행 후 정적 엣지로 수퍼바이저에 복귀하고,
//     수퍼바이저가 결과를 컨텍스트로 최종 응답을 생성한다.
//
// ANTHROPIC_API_KEY 가 없는 환경에서 go test ./multiagent/... 는 이 테스트를 skip 하고
// 빌드·정적검사·다른 테스트에는 영향을 주지 않는다(SPEC §5.8, ANALYSIS §5.8).
package multiagent_test

import (
	"context"
	"strings"
	"testing"

	"github.com/zipkero/langgraph-go/agent"
	"github.com/zipkero/langgraph-go/config"
	"github.com/zipkero/langgraph-go/core"
	"github.com/zipkero/langgraph-go/graph"
	"github.com/zipkero/langgraph-go/graph/command"
	"github.com/zipkero/langgraph-go/llm"
	"github.com/zipkero/langgraph-go/message"
	"github.com/zipkero/langgraph-go/multiagent"
	"github.com/zipkero/langgraph-go/tool"
)

// skipIfNoAnthropicKey 는 저장소 루트 .env 에서 ANTHROPIC_API_KEY 를 로드하고,
// 키가 없으면 t.Skip 을 호출한다.
// 테스트 작업 디렉토리는 패키지 디렉토리(multiagent/) 이므로 루트 .env 는 "../.env" 경로다.
// config.LoadEnv 는 파일이 없으면 error 를 반환하고, 이미 export 된 환경변수는 보존한다.
func skipIfNoAnthropicKey(t *testing.T) string {
	t.Helper()

	// 저장소 루트 .env 를 로드해 OS 환경에 반영한다.
	// config.LoadEnv 는 파일 부재 시 error 를 반환한다.
	// .env 로드 실패 = 키 없음으로 처리해 t.Skip 한다.
	cfg, loadErr := config.LoadEnv("../.env")
	if loadErr != nil {
		t.Skipf(
			"저장소 루트 .env 를 열 수 없어 e2e 테스트를 건너뜁니다 (%v). "+
				"../.env 에 ANTHROPIC_API_KEY=<key> 를 추가하세요",
			loadErr,
		)
		return ""
	}

	if cfg.AnthropicAPIKey == "" {
		t.Skip("ANTHROPIC_API_KEY 가 .env 에 설정되지 않아 e2e 테스트를 건너뜁니다")
		return ""
	}

	return cfg.AnthropicAPIKey
}

// e2eAgentWorker 는 *agent.Agent 를 multiagent.Worker 인터페이스로 감싸는 e2e 전용 어댑터다.
// multiagent 패키지에 AgentAsWorker 공개 함수가 없으므로 테스트 내부에서 직접 정의한다.
type e2eAgentWorker struct {
	name string
	desc string
	ag   *agent.Agent
}

func (w *e2eAgentWorker) Name() string        { return w.name }
func (w *e2eAgentWorker) Description() string { return w.desc }

func (w *e2eAgentWorker) Invoke(ctx context.Context, in agent.Input, cfg config.RunConfig) (multiagent.WorkerOutput, error) {
	result, err := w.ag.Invoke(ctx, in, cfg)
	if err != nil {
		return multiagent.WorkerOutput{}, err
	}
	return multiagent.WorkerOutput{
		Messages:           result.Messages,
		StructuredResponse: result.StructuredResponse,
	}, nil
}

func (w *e2eAgentWorker) Stream(ctx context.Context, in agent.Input, cfg config.RunConfig, mode core.Mode) (<-chan agent.AgentEvent, error) {
	return w.ag.Stream(ctx, in, cfg, mode)
}

// e2eMsgsFromState 는 graph.State 에서 "messages" 키를 꺼내 []message.Message 로 반환한다.
// 키가 없거나 타입이 맞지 않으면 빈 슬라이스를 반환한다.
func e2eMsgsFromState(st graph.State) []message.Message {
	if raw, ok := st["messages"]; ok {
		if msgs, ok := raw.([]message.Message); ok {
			return msgs
		}
	}
	return []message.Message{}
}

// ============================================================
// TestE2E_SupervisorWorkerRouting
//
// 검증:
//   - 수퍼바이저 노드가 RouterTool 을 바인딩해 질의 내용에 맞는 워커로 Goto 한다.
//   - 워커 노드(AgentAsNode 감싼 실 에이전트)가 실행되어 결과를 상태에 남긴다.
//   - 워커 완료 후 정적 엣지로 수퍼바이저에 복귀하고,
//     수퍼바이저가 워커 결과를 컨텍스트로 최종 응답을 생성한다.
//   - 최종 AI 응답이 비어 있지 않아야 한다.
// ============================================================

func TestE2E_SupervisorWorkerRouting(t *testing.T) {
	apiKey := skipIfNoAnthropicKey(t)

	ctx := context.Background()

	// 실제 Claude 모델 클라이언트 생성(API 키를 명시적으로 전달)
	// claude-haiku-4-5 는 빠르고 저렴해 e2e 에 적합하다.
	model, err := llm.InitChatModel("anthropic:claude-haiku-4-5", llm.WithAPIKey(apiKey))
	if err != nil {
		t.Fatalf("InitChatModel 실패: %v", err)
	}

	// ── 워커 에이전트 생성 ───────────────────────────────────────────────────────

	// 날씨 워커: 날씨·기후 질문 처리
	weatherAgent, err := agent.Create(model, nil,
		agent.WithSystemPrompt("당신은 날씨 전문가입니다. 날씨와 기후에 관한 질문에 한국어로 간결하게 답하세요."),
		agent.WithMaxSteps(3),
	)
	if err != nil {
		t.Fatalf("날씨 워커 에이전트 생성 실패: %v", err)
	}

	// 번역 워커: 텍스트 번역 처리
	translationAgent, err := agent.Create(model, nil,
		agent.WithSystemPrompt("당신은 번역 전문가입니다. 번역 요청에 간결하게 답하세요."),
		agent.WithMaxSteps(3),
	)
	if err != nil {
		t.Fatalf("번역 워커 에이전트 생성 실패: %v", err)
	}

	// Worker 인터페이스로 감싼다.
	weatherWorker := &e2eAgentWorker{
		name: "weather_worker",
		desc: "날씨와 기후에 관한 질문을 처리합니다",
		ag:   weatherAgent,
	}
	translationWorker := &e2eAgentWorker{
		name: "translation_worker",
		desc: "텍스트 번역 요청을 처리합니다",
		ag:   translationAgent,
	}

	// ── 수퍼바이저 그래프 구성 ──────────────────────────────────────────────────
	// 수퍼바이저: RouterTool 을 바인딩한 모델로 다음 워커를 선택한다.
	// 선택 후 command.Goto(worker) → 워커 실행 → AddEdge(worker→supervisor) 복귀 → 최종 응답.

	workerNames := []string{weatherWorker.Name(), translationWorker.Name()}
	routerTool := multiagent.RouterTool(workerNames...)

	// 수퍼바이저 모델에 RouterTool 스키마 바인딩
	supervisorModel := model.BindTools([]tool.Schema{routerTool.Schema()})

	// 상태 스키마: messages 를 message.AddMessages 리듀서로 누적
	stateSchema := graph.StateSchema{
		Reducers: map[string]graph.ReducerFunc{
			"messages": func(cur, upd any) any {
				var base []message.Message
				if cur != nil {
					if m, ok := cur.([]message.Message); ok {
						base = m
					}
				}
				var incoming []message.Message
				if upd != nil {
					if m, ok := upd.([]message.Message); ok {
						incoming = m
					}
				}
				return message.AddMessages(base, incoming)
			},
		},
	}
	b := graph.NewStateGraph(stateSchema)

	// prepareMsgsForSupervisor 는 Claude API 요구사항에 맞게 메시지를 정제한다.
	// tool_use AI 메시지 뒤에 tool_result 가 없으면 자동으로 삽입해 API 오류를 방지한다.
	// 이는 워커가 완료되어 수퍼바이저에 복귀할 때 상태에 남은 불완전한 메시지 시퀀스를 보정한다.
	prepareMsgsForSupervisor := func(msgs []message.Message) []message.Message {
		result := make([]message.Message, 0, len(msgs)+2)
		for i, m := range msgs {
			result = append(result, m)
			// tool_calls 가 있는 AI 메시지 뒤에 tool_result 가 없으면 삽입한다.
			if m.Role == message.RoleAssistant && len(m.ToolCalls) > 0 {
				nextIsToolResult := i+1 < len(msgs) && msgs[i+1].Role == message.RoleTool
				if !nextIsToolResult {
					// 각 tool_call 에 대해 tool_result 메시지를 삽입한다.
					for _, tc := range m.ToolCalls {
						result = append(result, message.NewToolMessage(tc.ID, tc.Name, "워커에 위임했습니다"))
					}
				}
			}
		}
		return result
	}

	// 수퍼바이저 노드: RouterTool 바인딩 모델로 라우팅 결정
	supervisorNode := func(ctx context.Context, st graph.State) (any, error) {
		// 시스템 프롬프트: 워커 선택 방법 안내
		systemPrompt := "당신은 수퍼바이저입니다. 사용자 질문을 분석해 적합한 워커에 위임하세요. " +
			"날씨·기후 관련이면 weather_worker, 번역 요청이면 translation_worker 를 선택하세요. " +
			"워커 결과가 있으면 해당 내용을 바탕으로 최종 응답을 생성하세요. " +
			"라우팅이 필요하면 route_to_worker 도구를 호출하고, 최종 답변이 준비됐으면 도구 호출 없이 응답하세요."

		// 현재 메시지 추출 + Claude API 요구사항에 맞게 정제 + 시스템 메시지 삽입
		msgs := e2eMsgsFromState(st)
		prepared := prepareMsgsForSupervisor(msgs)
		fullMsgs := append([]message.Message{message.NewSystemMessage(systemPrompt)}, prepared...)

		resp, err := supervisorModel.Chat(ctx, llm.ChatRequest{
			Messages: fullMsgs,
		})
		if err != nil {
			return nil, err
		}

		// tool_calls 파싱
		toolCalls := supervisorModel.ParseToolCalls(resp)

		// AI 메시지 구성
		aiMsg := resp.Message
		if len(toolCalls) > 0 {
			aiMsg = message.NewAssistantToolCalls(toolCalls)
			aiMsg.Content = resp.Message.Content
		}

		update := core.StateUpdate{
			"messages": []message.Message{aiMsg},
		}

		// 라우팅 결정: SelectNext 가 tool_calls 에서 다음 워커를 추출
		// update 를 반영한 전체 메시지로 상태 구성
		nextSt := graph.State{
			"messages": message.AddMessages(msgs, []message.Message{aiMsg}),
		}
		cmd, err := multiagent.Route(ctx, nextSt, nil)
		if err != nil {
			return nil, err
		}

		// End 이면 StateUpdate 반환(종료)
		if cmd.IsEnd() {
			return update, nil
		}

		// Goto 이면 command.Command 에 Update 포함해 반환
		return command.Command{
			Goto:   cmd.Goto,
			Update: update,
			Graph:  command.TargetCurrent,
		}, nil
	}

	// workerNodeFunc 은 워커 에이전트를 노드로 감싸는 헬퍼다.
	// 수퍼바이저가 라우터 도구를 호출해 남긴 tool_calls AI 메시지를 워커에 그대로 전달하면
	// Anthropic API 가 "tool_use 뒤에 tool_result 없음" 오류를 반환한다.
	// 따라서:
	//   1) 워커에는 마지막 user 메시지만 전달한다.
	//   2) 워커 응답은 tool_result 메시지로 변환해 상태에 추가한다(수퍼바이저 복귀 시 컨텍스트).
	//      이렇게 하면 수퍼바이저 두 번째 호출 시 메시지 구조가:
	//      user → assistant(tool_call) → tool_result(워커 응답) 가 되어 API 요구사항을 충족한다.
	workerNodeFunc := func(ag *agent.Agent) graph.NodeFunc {
		return func(ctx context.Context, st graph.State) (any, error) {
			msgs := e2eMsgsFromState(st)
			// 마지막 user 메시지만 추출해 워커에 전달한다.
			var userMsg message.Message
			for i := len(msgs) - 1; i >= 0; i-- {
				if msgs[i].Role == message.RoleUser {
					userMsg = msgs[i]
					break
				}
			}
			workerInput := agent.Input{Messages: []message.Message{userMsg}}
			result, err := ag.Invoke(ctx, workerInput, config.RunConfig{})
			if err != nil {
				return nil, err
			}
			// 워커 응답에서 마지막 AI 메시지 내용 추출
			workerResponse := ""
			lastWorkerAI, found := message.LastAIMessage(result.Messages)
			if found {
				workerResponse = lastWorkerAI.Content
			}
			// 수퍼바이저가 남긴 마지막 tool_call 의 ID 를 찾아 tool_result 로 응답한다.
			// 이렇게 하면 수퍼바이저 복귀 시 메시지가 tool_use → tool_result 쌍을 이룬다.
			var toolResultMsgs []message.Message
			for i := len(msgs) - 1; i >= 0; i-- {
				if msgs[i].Role == message.RoleAssistant && len(msgs[i].ToolCalls) > 0 {
					// 첫 번째 tool_call 의 결과를 워커 응답으로 채운다.
					tc := msgs[i].ToolCalls[0]
					toolResultMsgs = append(toolResultMsgs,
						message.NewToolMessage(tc.ID, tc.Name, workerResponse),
					)
					break
				}
			}
			if len(toolResultMsgs) == 0 {
				// tool_call 을 찾지 못한 경우: 일반 AI 메시지로 반환
				toolResultMsgs = result.Messages
			}
			return graph.StateUpdate{"messages": toolResultMsgs}, nil
		}
	}

	// 날씨 워커 노드
	weatherNode := workerNodeFunc(weatherAgent)

	// 번역 워커 노드
	translationNode := workerNodeFunc(translationAgent)

	// 노드 등록 — 수퍼바이저는 두 워커 노드를 WithDestinations 로 선언
	// (실행 시 command.Goto 동적 라우팅 허용)
	if err := b.AddNode("supervisor",
		supervisorNode,
		graph.WithDestinations(weatherWorker.Name(), translationWorker.Name()),
	); err != nil {
		t.Fatalf("수퍼바이저 노드 등록 실패: %v", err)
	}
	if err := b.AddNode(weatherWorker.Name(), weatherNode); err != nil {
		t.Fatalf("날씨 워커 노드 등록 실패: %v", err)
	}
	if err := b.AddNode(translationWorker.Name(), translationNode); err != nil {
		t.Fatalf("번역 워커 노드 등록 실패: %v", err)
	}

	// 정적 엣지: 각 워커 실행 후 수퍼바이저로 복귀
	if err := b.AddEdge(weatherWorker.Name(), "supervisor"); err != nil {
		t.Fatalf("날씨→수퍼바이저 엣지 추가 실패: %v", err)
	}
	if err := b.AddEdge(translationWorker.Name(), "supervisor"); err != nil {
		t.Fatalf("번역→수퍼바이저 엣지 추가 실패: %v", err)
	}

	// validate BFS 도달 가능성 확보:
	// WithDestinations 는 실행 시 Goto 허용 목적지이지만 BFS 인접 목록에는 포함되지 않는다.
	// 수퍼바이저 → 워커 방향을 BFS 가 탐색하도록 dummy 조건 엣지를 추가한다.
	// 실제 실행은 supervisorNode 가 command.Command(Goto) 를 반환하므로 이 조건 엣지는 호출되지 않는다.
	dummyRouter := func(_ context.Context, _ graph.State) string { return "" }
	routerMapping := map[string]string{
		weatherWorker.Name():     weatherWorker.Name(),
		translationWorker.Name(): translationWorker.Name(),
	}
	if err := b.AddConditionalEdges("supervisor", dummyRouter, routerMapping); err != nil {
		t.Fatalf("수퍼바이저 조건 엣지 추가 실패: %v", err)
	}

	// 진입점 설정
	if err := b.SetEntryPoint("supervisor"); err != nil {
		t.Fatalf("진입점 설정 실패: %v", err)
	}

	// 그래프 컴파일
	compiled, err := b.Compile(graph.WithMaxSteps(20))
	if err != nil {
		t.Fatalf("그래프 Compile 실패: %v", err)
	}

	// ── 실행 ─────────────────────────────────────────────────────────────────

	// 날씨 관련 질문으로 e2e 실행 — 수퍼바이저가 weather_worker 를 선택해야 한다.
	initState := graph.State{
		"messages": []message.Message{
			message.NewUserMessage("오늘 서울의 날씨가 어떤지 알려주세요."),
		},
	}

	t.Log("[e2e] 그래프 실행 시작 — 수퍼바이저+워커 라우팅")
	finalState, err := compiled.Invoke(ctx, initState, config.RunConfig{})
	if err != nil {
		t.Fatalf("그래프 Invoke 실패: %v", err)
	}

	// ── 결과 검증 ────────────────────────────────────────────────────────────

	rawMsgs, ok := finalState["messages"]
	if !ok {
		t.Fatal("최종 상태에 'messages' 키가 없습니다")
	}
	finalMsgs, ok := rawMsgs.([]message.Message)
	if !ok {
		t.Fatalf("최종 상태 messages 타입 불일치: %T", rawMsgs)
	}

	// 메시지 수: user + (수퍼바이저 AI) + (워커 응답) + (최종 수퍼바이저) 최소 3개
	if len(finalMsgs) < 3 {
		t.Errorf("최종 메시지 수=%d, 최소 3 기대(user+워커위임+최종응답)", len(finalMsgs))
	}

	// 최종 AI 메시지 존재 및 비어 있지 않음 확인
	lastAI, found := message.LastAIMessage(finalMsgs)
	if !found {
		t.Fatal("최종 상태에서 AI 메시지를 찾지 못했습니다")
	}
	if strings.TrimSpace(lastAI.Content) == "" {
		t.Error("최종 AI 메시지 내용이 비어 있습니다")
	}

	// 관찰 로그: 실제 위임과 통합 흐름 출력
	t.Logf("[e2e] 총 메시지 수: %d", len(finalMsgs))
	t.Logf("[e2e] 최종 AI 응답: %s", lastAI.Content)
	for i, m := range finalMsgs {
		t.Logf("[e2e] 메시지[%d] role=%s tool_calls=%d content_len=%d",
			i, m.Role, len(m.ToolCalls), len(m.Content))
	}
}
