// store_agent_integration_test.go 는 store.InMemoryStore 를 agent.WithStore 로 주입하는
// 통합 테스트를 담는다. package store_test (외부 테스트 패키지)로 선언해 store→agent 역방향
// import 없이, agent(상위)→store(하위) 정방향으로만 참조한다(§28-1 규칙2·4).
//
// 이 파일이 컴파일되는 것 자체가 *store.InMemoryStore 가 tool.Store 를 구조적으로 충족함의
// 검증 지점이다(analysis.md §3 비고, §5 D1).
package store_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/zipkero/langgraph-go/agent"
	"github.com/zipkero/langgraph-go/config"
	"github.com/zipkero/langgraph-go/llm"
	"github.com/zipkero/langgraph-go/message"
	"github.com/zipkero/langgraph-go/store"
	"github.com/zipkero/langgraph-go/structured"
	"github.com/zipkero/langgraph-go/tool"
)

// storeTool 은 rt.Store() 로 받은 store 에 Put/Get/Search 를 수행하고
// 그 결과를 도구 출력(Content)에 담아 반환하는 테스트용 도구다.
//
// 흐름:
//  1. Put: ns=["integration","test"], key="item-1", value={"data":"hello"}
//  2. Get: 같은 네임스페이스·키로 조회 → "hello" 포함 여부 확인
//  3. Search: limit=5 로 검색 → 결과 수를 Content 에 포함
//
// 결과는 Content 문자열에 저장값·조회값·검색 결과 수를 담아 반환한다.
type storeTool struct{}

func (s *storeTool) Name() string        { return "store_op" }
func (s *storeTool) Description() string { return "store에 Put/Get/Search를 수행하는 테스트 도구" }
func (s *storeTool) Schema() tool.Schema {
	return tool.Schema{
		Name:        "store_op",
		Description: "store에 Put/Get/Search를 수행하는 테스트 도구",
		Parameters: []tool.Parameter{
			{Name: "action", Type: "string", Description: "수행할 액션", Required: true},
		},
	}
}

func (s *storeTool) Execute(ctx context.Context, input tool.Input, rt tool.Runtime) (tool.Result, error) {
	st := rt.Store()
	if st == nil {
		return tool.Result{Content: "store=nil", IsError: true}, nil
	}

	ns := []string{"integration", "test"}

	// 1. Put: 값을 저장한다.
	putVal := map[string]any{"data": "hello"}
	if err := st.Put(ctx, ns, "item-1", putVal); err != nil {
		return tool.Result{Content: "put 실패: " + err.Error(), IsError: true}, nil
	}

	// 2. Get: 저장한 값을 조회한다.
	got, found, err := st.Get(ctx, ns, "item-1")
	if err != nil {
		return tool.Result{Content: "get 실패: " + err.Error(), IsError: true}, nil
	}
	if !found {
		return tool.Result{Content: "get: not-found", IsError: true}, nil
	}

	// 3. Search: 검색해 결과 수를 확인한다.
	results, err := st.Search(ctx, ns, "hello", 5)
	if err != nil {
		return tool.Result{Content: "search 실패: " + err.Error(), IsError: true}, nil
	}

	// 결과를 JSON 으로 직렬화해 Content 에 담는다.
	out := map[string]any{
		"put_key":      "item-1",
		"get_data":     got["data"],
		"search_count": len(results),
	}
	b, _ := json.Marshal(out)
	return tool.Result{Content: string(b)}, nil
}

// ============================================================
// 통합 테스트
// ============================================================

// TestWithStore_AgentInject 는 store.NewInMemoryStore() 를 agent.WithStore 로 주입하고,
// store_op 도구를 통해 Put/Get/Search 가 정상 동작함을 검증한다.
//
// 검증 조건(task-006):
//   - agent.WithStore 에 *store.InMemoryStore 를 넘겨 컴파일 → tool.Store 구조적 충족 검증.
//   - 도구 실행 후 도구 출력(ToolMessage.Content)에 저장·조회·검색 결과가 반영됨.
//   - get_data == "hello" (Put 한 값을 Get 으로 회수).
//   - search_count >= 1 (인덱스 미설정 폴백으로 저장 항목 반환).
func TestWithStore_AgentInject(t *testing.T) {
	// store 구현체 생성 (인덱스 미설정 — 폴백 검색 사용)
	s := store.NewInMemoryStore()

	// stub 클라이언트: 1회차 — store_op tool_call, 2회차 — 최종 답변
	seq := newSeqStub(
		llm.StubResponse{
			Message: message.NewAssistantToolCalls([]message.ToolCall{
				{
					ID:   "call-store-1",
					Name: "store_op",
					Args: json.RawMessage(`{"action":"run"}`),
				},
			}),
			ToolCalls: []message.ToolCall{
				{
					ID:   "call-store-1",
					Name: "store_op",
					Args: json.RawMessage(`{"action":"run"}`),
				},
			},
			FinishReason: "tool_use",
		},
		llm.StubResponse{
			Message:      message.NewAssistantMessage("store 연동 완료"),
			FinishReason: "stop",
		},
	)

	// agent 생성 — WithStore 로 InMemoryStore 주입.
	// 이 호출이 컴파일되는 것이 tool.Store 구조적 충족의 검증 지점이다.
	a, err := agent.Create(seq, []tool.Tool{&storeTool{}},
		agent.WithStore(s),
	)
	if err != nil {
		t.Fatalf("agent.Create 실패: %v", err)
	}

	in := agent.Input{
		Messages: []message.Message{message.NewUserMessage("store 연동 테스트")},
	}
	result, err := a.Invoke(context.Background(), in, config.RunConfig{})
	if err != nil {
		t.Fatalf("Invoke 실패: %v", err)
	}

	// 메시지 구조: user → AI(tool_call) → tool(result) → AI(최종)
	if len(result.Messages) < 4 {
		t.Errorf("메시지 수=%d, 최소 4 기대(user/AI-tool/tool-result/AI-final)", len(result.Messages))
	}

	// ToolMessage 에서 도구 출력을 찾아 검증한다.
	var toolContent string
	for _, m := range result.Messages {
		if m.Role == message.RoleTool {
			toolContent = m.Content
			break
		}
	}
	if toolContent == "" {
		t.Fatal("ToolMessage.Content 가 비어 있습니다")
	}

	// 도구 출력 JSON 파싱
	var out map[string]any
	if err := json.Unmarshal([]byte(toolContent), &out); err != nil {
		t.Fatalf("ToolMessage.Content JSON 파싱 실패: %v, content=%q", err, toolContent)
	}

	// get_data == "hello": Put 한 값이 Get 으로 회수됐는지 확인
	if out["get_data"] != "hello" {
		t.Errorf("get_data=%v, 기대값=%q", out["get_data"], "hello")
	}

	// search_count >= 1: 인덱스 미설정 폴백으로 저장 항목이 반환됐는지 확인
	searchCount, ok := out["search_count"].(float64)
	if !ok {
		t.Fatalf("search_count 타입=%T, float64 기대", out["search_count"])
	}
	if int(searchCount) < 1 {
		t.Errorf("search_count=%d, 최소 1 기대", int(searchCount))
	}

	// 최종 AI 메시지 확인
	lastAI, ok2 := message.LastAIMessage(result.Messages)
	if !ok2 {
		t.Fatal("마지막 AI 메시지를 찾을 수 없습니다")
	}
	if lastAI.Content != "store 연동 완료" {
		t.Errorf("마지막 AI 메시지=%q, 기대값=%q", lastAI.Content, "store 연동 완료")
	}
}

// TestWithStore_PutGetSearchDirect 는 store 를 직접(agent 없이) 구성해 도구 함수가
// store 메서드를 정상 호출하는지, 그 결과가 기대와 일치하는지 추가 검증한다.
// agent 주입 경로와 별개로 store 구현의 기능 정합을 단정한다.
func TestWithStore_PutGetSearchDirect(t *testing.T) {
	s := store.NewInMemoryStore()
	ctx := context.Background()
	ns := []string{"direct", "verify"}

	// Put
	if err := s.Put(ctx, ns, "k1", map[string]any{"val": "world"}); err != nil {
		t.Fatalf("Put 실패: %v", err)
	}

	// Get
	got, found, err := s.Get(ctx, ns, "k1")
	if err != nil || !found {
		t.Fatalf("Get 실패: err=%v found=%v", err, found)
	}
	if got["val"] != "world" {
		t.Errorf("val=%v, 기대값=%q", got["val"], "world")
	}

	// Search (인덱스 미설정 폴백 — 키 정렬, limit=10)
	results, err := s.Search(ctx, ns, "any", 10)
	if err != nil {
		t.Fatalf("Search 실패: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("Search 결과 수=%d, 기대값=1", len(results))
	}
}

// ============================================================
// 테스트 내부 stub — seqStub (agent_test 의 seqStubClient 와 동일 패턴,
// package store_test 에서 재구현 — 다른 패키지이므로 공유 불가)
// ============================================================

type seqStub struct {
	responses []llm.StubResponse
	idx       int
}

func newSeqStub(resps ...llm.StubResponse) *seqStub {
	return &seqStub{responses: resps}
}

func (s *seqStub) cur() llm.StubResponse {
	if s.idx >= len(s.responses) {
		return s.responses[len(s.responses)-1]
	}
	r := s.responses[s.idx]
	s.idx++
	return r
}

func (s *seqStub) Chat(_ context.Context, _ llm.ChatRequest) (llm.ChatResponse, error) {
	r := s.cur()
	if r.Err != nil {
		return llm.ChatResponse{}, r.Err
	}
	return llm.ChatResponse{
		Message:      r.Message,
		ToolCalls:    r.ToolCalls,
		Usage:        r.Usage,
		FinishReason: r.FinishReason,
	}, nil
}

func (s *seqStub) ChatStream(_ context.Context, _ llm.ChatRequest) (<-chan llm.ChatEvent, error) {
	r := s.cur()
	if r.Err != nil {
		return nil, r.Err
	}
	ch := make(chan llm.ChatEvent, 3)
	go func() {
		defer close(ch)
		if r.Message.Content != "" {
			ch <- llm.ChatEvent{Type: llm.ChatEventToken, Token: r.Message.Content}
		}
		msg := r.Message
		ch <- llm.ChatEvent{Type: llm.ChatEventMessage, Message: &msg}
		resp := llm.ChatResponse{
			Message:      r.Message,
			ToolCalls:    r.ToolCalls,
			FinishReason: r.FinishReason,
		}
		ch <- llm.ChatEvent{Type: llm.ChatEventDone, Response: &resp}
	}()
	return ch, nil
}

func (s *seqStub) Structured(_ context.Context, _ llm.ChatRequest, _ structured.Schema) (any, error) {
	r := s.cur()
	if r.Err != nil {
		return nil, r.Err
	}
	return r.StructuredValue, nil
}

func (s *seqStub) BindTools(_ []tool.Schema) llm.Client {
	return s // callIndex 공유 필요 — 같은 포인터 반환
}

func (s *seqStub) ParseToolCalls(resp llm.ChatResponse) []message.ToolCall {
	return resp.ToolCalls
}

func (s *seqStub) WithModel(_ string) llm.Client { return s }
func (s *seqStub) ModelName() string             { return "stub-seq" }
