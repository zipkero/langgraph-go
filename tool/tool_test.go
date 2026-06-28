package tool_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/zipkero/langgraph-go/config"
	"github.com/zipkero/langgraph-go/message"
	"github.com/zipkero/langgraph-go/tool"
)

// --- 테스트용 스텁 Store ---

// stubStore 는 테스트에서 tool.Store 인터페이스를 충족하는 스텁 구현체다.
type stubStore struct {
	data map[string]map[string]any
}

func newStubStore() *stubStore {
	return &stubStore{data: make(map[string]map[string]any)}
}

func (s *stubStore) Get(_ context.Context, namespace []string, key string) (map[string]any, bool, error) {
	nsKey := nsKey(namespace, key)
	v, ok := s.data[nsKey]
	return v, ok, nil
}

func (s *stubStore) Put(_ context.Context, namespace []string, key string, value map[string]any) error {
	s.data[nsKey(namespace, key)] = value
	return nil
}

func (s *stubStore) Search(_ context.Context, _ []string, _ string, _ int) ([]map[string]any, error) {
	return nil, nil
}

func nsKey(namespace []string, key string) string {
	ns := ""
	for _, n := range namespace {
		ns += n + "/"
	}
	return ns + key
}

// --- 테스트용 입력 구조체 ---

// calcInput 은 덧셈 도구의 입력 구조체다.
type calcInput struct {
	A int    `json:"a" description:"첫 번째 피연산자"`
	B int    `json:"b" description:"두 번째 피연산자"`
	Op string `json:"op,omitempty" description:"연산 종류(add/sub)"`
}

// --- 1. FromFunc 로 만든 Tool 에 ToolCall 을 디스패치하면 ToolMessage 가 산출된다 ---

func TestFromFuncDispatch(t *testing.T) {
	// 덧셈 도구 생성
	addTool := tool.FromFunc("add", "두 수를 더한다", func(ctx context.Context, args calcInput, rt tool.Runtime) (tool.Result, error) {
		return tool.Result{Content: "42"}, nil
	})

	reg := tool.NewRegistry()
	if err := reg.Register(addTool); err != nil {
		t.Fatalf("Register 실패: %v", err)
	}

	exec := tool.NewExecutor(reg)
	call := message.ToolCall{
		ID:   "call-1",
		Name: "add",
		Args: json.RawMessage(`{"a":20,"b":22}`),
	}

	rt := tool.NewRuntime(nil, call.ID, config.RunConfig{}, newStubStore(), nil)
	res, err := exec.Execute(context.Background(), call, rt)
	if err != nil {
		t.Fatalf("Execute 실패: %v", err)
	}
	if res.Content != "42" {
		t.Errorf("Content 기대 '42', 받음 %q", res.Content)
	}
	if res.IsError {
		t.Error("IsError 는 false 이어야 합니다")
	}

	// BuildToolMessage 로 ToolMessage 생성 확인
	msg := exec.BuildToolMessage(call, res)
	if msg.Role != message.RoleTool {
		t.Errorf("Role 기대 %q, 받음 %q", message.RoleTool, msg.Role)
	}
	if msg.ToolCallID != call.ID {
		t.Errorf("ToolCallID 기대 %q, 받음 %q", call.ID, msg.ToolCallID)
	}
	if msg.Content != "42" {
		t.Errorf("Content 기대 '42', 받음 %q", msg.Content)
	}
}

// --- 2. WithArgsSchema 로 만든 Tool 에 ToolCall 을 디스패치하면 ToolMessage 가 산출된다 ---

func TestWithArgsSchemaDispatch(t *testing.T) {
	greetTool := tool.WithArgsSchema[calcInput]("calc", "계산 도구", func(ctx context.Context, args calcInput, rt tool.Runtime) (tool.Result, error) {
		result := args.A + args.B
		return tool.Result{Content: string(rune('0' + result%10))}, nil
	})

	if greetTool.Name() != "calc" {
		t.Errorf("Name 기대 'calc', 받음 %q", greetTool.Name())
	}
	if greetTool.Description() != "계산 도구" {
		t.Errorf("Description 기대 '계산 도구', 받음 %q", greetTool.Description())
	}

	// 스키마에 파라미터가 있어야 함
	schema := greetTool.Schema()
	if len(schema.Parameters) == 0 {
		t.Error("Parameters 가 비어 있으면 안 됩니다")
	}

	// 파라미터 이름 확인
	paramNames := make(map[string]bool)
	for _, p := range schema.Parameters {
		paramNames[p.Name] = true
	}
	for _, expected := range []string{"a", "b"} {
		if !paramNames[expected] {
			t.Errorf("파라미터 %q 가 스키마에 없습니다", expected)
		}
	}

	// 실행 확인
	rt := tool.NewRuntime(nil, "call-2", config.RunConfig{}, newStubStore(), nil)
	res, err := greetTool.Execute(context.Background(), json.RawMessage(`{"a":3,"b":4}`), rt)
	if err != nil {
		t.Fatalf("Execute 실패: %v", err)
	}
	_ = res // 결과 내용 자체보다 에러 없음을 확인
}

// --- 3. Registry 등록/조회/Schemas 동작 ---

func TestRegistry(t *testing.T) {
	reg := tool.NewRegistry()

	toolA := tool.WithArgsSchema[calcInput]("toolA", "도구 A", func(ctx context.Context, args calcInput, rt tool.Runtime) (tool.Result, error) {
		return tool.Result{Content: "A"}, nil
	})
	toolB := tool.WithArgsSchema[calcInput]("toolB", "도구 B", func(ctx context.Context, args calcInput, rt tool.Runtime) (tool.Result, error) {
		return tool.Result{Content: "B"}, nil
	})

	if err := reg.Register(toolA); err != nil {
		t.Fatalf("toolA Register 실패: %v", err)
	}
	if err := reg.Register(toolB); err != nil {
		t.Fatalf("toolB Register 실패: %v", err)
	}

	// 중복 등록 에러
	if err := reg.Register(toolA); err == nil {
		t.Error("중복 등록 시 에러가 발생해야 합니다")
	}

	// Get 조회
	got, ok := reg.Get("toolA")
	if !ok {
		t.Fatal("toolA 를 찾을 수 없습니다")
	}
	if got.Name() != "toolA" {
		t.Errorf("Name 기대 'toolA', 받음 %q", got.Name())
	}

	// 미등록 도구 조회
	_, ok = reg.Get("nonexistent")
	if ok {
		t.Error("미등록 도구 조회는 false 이어야 합니다")
	}

	// List
	list := reg.List()
	if len(list) != 2 {
		t.Errorf("List 길이 기대 2, 받음 %d", len(list))
	}

	// Schemas
	schemas := reg.Schemas()
	if len(schemas) != 2 {
		t.Errorf("Schemas 길이 기대 2, 받음 %d", len(schemas))
	}
	if schemas[0].Name != "toolA" || schemas[1].Name != "toolB" {
		t.Errorf("Schemas 순서 또는 이름 불일치: %v", schemas)
	}
}

// --- 4. 미등록 도구 호출 시 UnknownToolError ---

func TestUnknownToolError(t *testing.T) {
	reg := tool.NewRegistry()
	exec := tool.NewExecutor(reg)

	call := message.ToolCall{
		ID:   "call-unknown",
		Name: "ghost_tool",
		Args: json.RawMessage(`{}`),
	}
	rt := tool.NewRuntime(nil, call.ID, config.RunConfig{}, newStubStore(), nil)
	_, err := exec.Execute(context.Background(), call, rt)
	if err == nil {
		t.Fatal("미등록 도구 호출 시 에러가 발생해야 합니다")
	}
	if !tool.IsUnknownToolError(err) {
		t.Errorf("UnknownToolError 가 아닙니다: %v", err)
	}

	// 에러 메시지에 도구 이름이 포함되어야 함
	if !contains(err.Error(), "ghost_tool") {
		t.Errorf("에러 메시지에 도구 이름 'ghost_tool' 이 없습니다: %v", err)
	}
}

// --- 5. ValidateArgs 인자 검증 ---

func TestValidateArgs(t *testing.T) {
	schema := tool.Schema{
		Name: "myTool",
		Parameters: []tool.Parameter{
			{Name: "x", Type: "integer", Required: true},
			{Name: "y", Type: "integer", Required: false},
		},
	}

	// 유효한 인자
	if err := tool.ValidateArgs(schema, json.RawMessage(`{"x":1}`)); err != nil {
		t.Errorf("유효한 인자에 에러: %v", err)
	}

	// 필수 파라미터 누락
	if err := tool.ValidateArgs(schema, json.RawMessage(`{"y":2}`)); err == nil {
		t.Error("필수 파라미터 누락 시 에러가 발생해야 합니다")
	}

	// 빈 인자
	if err := tool.ValidateArgs(schema, json.RawMessage(nil)); err == nil {
		t.Error("필수 파라미터가 있는데 nil 인자를 주면 에러가 발생해야 합니다")
	}

	// 필수 파라미터가 없는 스키마에서 빈 인자는 유효
	emptySchema := tool.Schema{Name: "empty"}
	if err := tool.ValidateArgs(emptySchema, json.RawMessage(nil)); err != nil {
		t.Errorf("필수 파라미터 없는 스키마에 nil 인자: %v", err)
	}
}

// --- 6. DecodeArgs 인자 디코딩 ---

func TestDecodeArgs(t *testing.T) {
	type myArgs struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}

	raw := json.RawMessage(`{"name":"hello","count":3}`)
	args, err := tool.DecodeArgs[myArgs](raw)
	if err != nil {
		t.Fatalf("DecodeArgs 실패: %v", err)
	}
	if args.Name != "hello" {
		t.Errorf("Name 기대 'hello', 받음 %q", args.Name)
	}
	if args.Count != 3 {
		t.Errorf("Count 기대 3, 받음 %d", args.Count)
	}

	// 유효하지 않은 JSON
	_, err = tool.DecodeArgs[myArgs](json.RawMessage(`{invalid}`))
	if err == nil {
		t.Error("유효하지 않은 JSON 디코딩 시 에러가 발생해야 합니다")
	}
}

// --- 7. Runtime 접근 — 스텁 Store/Event 주입 ---

func TestRuntimeAccess(t *testing.T) {
	state := map[string]any{"key": "value"}
	toolCallID := "rt-call-1"
	cfg := config.RunConfig{Configurable: map[string]any{"thread_id": "t1"}}
	store := newStubStore()

	var emittedEvents []tool.Event
	emitFn := func(ev tool.Event) {
		emittedEvents = append(emittedEvents, ev)
	}

	rt := tool.NewRuntime(state, toolCallID, cfg, store, emitFn)

	// State 접근
	stateMap, ok := rt.State().(map[string]any)
	if !ok {
		t.Fatal("State() 타입 단언 실패")
	}
	if stateMap["key"] != "value" {
		t.Errorf("State 값 기대 'value', 받음 %v", stateMap["key"])
	}

	// ToolCallID 접근
	if rt.ToolCallID() != toolCallID {
		t.Errorf("ToolCallID 기대 %q, 받음 %q", toolCallID, rt.ToolCallID())
	}

	// Config 접근
	gotCfg := rt.Config()
	threadID := config.GetThreadID(gotCfg)
	if threadID != "t1" {
		t.Errorf("Config().Configurable thread_id 기대 't1', 받음 %q", threadID)
	}

	// Store 접근
	if rt.Store() == nil {
		t.Error("Store() 는 nil 이면 안 됩니다")
	}

	// Store Put/Get 동작
	putCtx := context.Background()
	if err := rt.Store().Put(putCtx, []string{"ns"}, "k", map[string]any{"v": 1}); err != nil {
		t.Fatalf("Store.Put 실패: %v", err)
	}
	val, found, err := rt.Store().Get(putCtx, []string{"ns"}, "k")
	if err != nil {
		t.Fatalf("Store.Get 실패: %v", err)
	}
	if !found {
		t.Error("저장된 값을 찾을 수 없습니다")
	}
	if val["v"] != 1 {
		t.Errorf("Store.Get 값 기대 1, 받음 %v", val["v"])
	}

	// Emit 동작
	ev := tool.Event{ToolName: "testTool", ToolCallID: toolCallID}
	rt.Emit(ev)
	if len(emittedEvents) != 1 {
		t.Fatalf("방출된 이벤트 수 기대 1, 받음 %d", len(emittedEvents))
	}
	if emittedEvents[0].ToolName != "testTool" {
		t.Errorf("이벤트 ToolName 기대 'testTool', 받음 %q", emittedEvents[0].ToolName)
	}
}

// --- 8. Tool 이 Runtime 에서 State/ToolCallID/Config/Store/Emit 에 접근하는 통합 ---

func TestToolWithRuntimeAccess(t *testing.T) {
	type echoInput struct {
		Msg string `json:"msg" description:"에코할 메시지"`
	}

	var capturedID string
	var capturedEvents []tool.Event

	echoTool := tool.WithArgsSchema[echoInput]("echo", "메시지를 에코한다", func(ctx context.Context, args echoInput, rt tool.Runtime) (tool.Result, error) {
		capturedID = rt.ToolCallID()
		rt.Emit(tool.Event{ToolName: "echo", ToolCallID: rt.ToolCallID()})
		return tool.Result{Content: args.Msg}, nil
	})

	reg := tool.NewRegistry()
	if err := reg.Register(echoTool); err != nil {
		t.Fatalf("Register 실패: %v", err)
	}
	exec := tool.NewExecutor(reg)

	call := message.ToolCall{
		ID:   "echo-call",
		Name: "echo",
		Args: json.RawMessage(`{"msg":"hello"}`),
	}

	rt := tool.NewRuntime(nil, call.ID, config.RunConfig{}, newStubStore(), func(ev tool.Event) {
		capturedEvents = append(capturedEvents, ev)
	})

	res, err := exec.Execute(context.Background(), call, rt)
	if err != nil {
		t.Fatalf("Execute 실패: %v", err)
	}
	if res.Content != "hello" {
		t.Errorf("Content 기대 'hello', 받음 %q", res.Content)
	}
	if capturedID != "echo-call" {
		t.Errorf("ToolCallID 기대 'echo-call', 받음 %q", capturedID)
	}
	if len(capturedEvents) != 1 {
		t.Fatalf("이벤트 수 기대 1, 받음 %d", len(capturedEvents))
	}
}

// --- 9. ExecuteMany —— 복수 ToolCall 처리 및 미등록 도구 포함 ---

func TestExecuteMany(t *testing.T) {
	sayTool := tool.FromFunc("say", "말하기", func(ctx context.Context, args struct {
		Word string `json:"word"`
	}, rt tool.Runtime) (tool.Result, error) {
		return tool.Result{Content: args.Word}, nil
	})

	reg := tool.NewRegistry()
	_ = reg.Register(sayTool)
	exec := tool.NewExecutor(reg)

	calls := []message.ToolCall{
		{ID: "c1", Name: "say", Args: json.RawMessage(`{"word":"hi"}`)},
		{ID: "c2", Name: "unknown_tool", Args: json.RawMessage(`{}`)},
	}

	rt := tool.NewRuntime(nil, "", config.RunConfig{}, newStubStore(), nil)
	msgs, err := exec.ExecuteMany(context.Background(), calls, rt)
	// ExecuteMany 는 개별 에러를 메시지에 포함하고 전체 에러는 nil
	if err != nil {
		t.Fatalf("ExecuteMany 는 nil 에러를 반환해야 합니다: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("메시지 수 기대 2, 받음 %d", len(msgs))
	}
	// 첫 번째 메시지는 정상 결과
	if msgs[0].Content != "hi" {
		t.Errorf("msgs[0].Content 기대 'hi', 받음 %q", msgs[0].Content)
	}
	// 두 번째는 UnknownToolError 내용을 담은 메시지
	if !contains(msgs[1].Content, "unknown_tool") {
		t.Errorf("msgs[1].Content 에 'unknown_tool' 이 없습니다: %q", msgs[1].Content)
	}
}

// --- 10. FromFunc 스키마 자동 도출 검증 ---

func TestFromFuncSchemaDerivation(t *testing.T) {
	type searchInput struct {
		Query  string `json:"query" description:"검색 질의"`
		Limit  int    `json:"limit,omitempty" description:"결과 최대 개수"`
		Filter string `json:"filter" description:"필터 조건"`
	}

	searchTool := tool.FromFunc("search", "검색 도구", func(ctx context.Context, args searchInput, rt tool.Runtime) (tool.Result, error) {
		return tool.Result{Content: args.Query}, nil
	})

	schema := searchTool.Schema()
	if schema.Name != "search" {
		t.Errorf("Schema.Name 기대 'search', 받음 %q", schema.Name)
	}

	paramMap := make(map[string]tool.Parameter)
	for _, p := range schema.Parameters {
		paramMap[p.Name] = p
	}

	// query 파라미터 검증
	qp, ok := paramMap["query"]
	if !ok {
		t.Fatal("'query' 파라미터가 없습니다")
	}
	if qp.Type != "string" {
		t.Errorf("query 타입 기대 'string', 받음 %q", qp.Type)
	}
	if qp.Description != "검색 질의" {
		t.Errorf("query 설명 기대 '검색 질의', 받음 %q", qp.Description)
	}
	if !qp.Required {
		t.Error("query 는 Required=true 이어야 합니다")
	}

	// limit 파라미터는 omitempty 이므로 Required=false
	lp, ok := paramMap["limit"]
	if !ok {
		t.Fatal("'limit' 파라미터가 없습니다")
	}
	if lp.Required {
		t.Error("limit 는 Required=false 이어야 합니다")
	}

	// filter 파라미터
	fp, ok := paramMap["filter"]
	if !ok {
		t.Fatal("'filter' 파라미터가 없습니다")
	}
	if !fp.Required {
		t.Error("filter 는 Required=true 이어야 합니다")
	}
}

// --- 11. RegisterMany ---

func TestRegisterMany(t *testing.T) {
	reg := tool.NewRegistry()

	tools := []tool.Tool{
		tool.WithArgsSchema[calcInput]("t1", "도구1", func(ctx context.Context, args calcInput, rt tool.Runtime) (tool.Result, error) {
			return tool.Result{}, nil
		}),
		tool.WithArgsSchema[calcInput]("t2", "도구2", func(ctx context.Context, args calcInput, rt tool.Runtime) (tool.Result, error) {
			return tool.Result{}, nil
		}),
	}

	if err := reg.RegisterMany(tools...); err != nil {
		t.Fatalf("RegisterMany 실패: %v", err)
	}
	if len(reg.List()) != 2 {
		t.Errorf("List 길이 기대 2, 받음 %d", len(reg.List()))
	}
}

// --- 12. ExecuteWithTimeout 타임아웃 테스트 ---

func TestExecuteWithTimeout(t *testing.T) {
	// 즉시 반환하는 도구
	fastTool := tool.WithArgsSchema[calcInput]("fast", "빠른 도구", func(ctx context.Context, args calcInput, rt tool.Runtime) (tool.Result, error) {
		return tool.Result{Content: "done"}, nil
	})

	reg := tool.NewRegistry()
	_ = reg.Register(fastTool)
	exec := tool.NewExecutor(reg)

	call := message.ToolCall{
		ID:   "timeout-call",
		Name: "fast",
		Args: json.RawMessage(`{"a":1,"b":2}`),
	}

	rt := tool.NewRuntime(nil, call.ID, config.RunConfig{}, newStubStore(), nil)

	// 충분한 타임아웃으로 성공해야 함
	res, err := exec.ExecuteWithTimeout(context.Background(), call, rt, 5_000_000_000) // 5초
	if err != nil {
		t.Fatalf("ExecuteWithTimeout 실패: %v", err)
	}
	if res.Content != "done" {
		t.Errorf("Content 기대 'done', 받음 %q", res.Content)
	}
}

// --- helpers ---

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(substr); i++ {
				if s[i:i+len(substr)] == substr {
					return true
				}
			}
			return false
		}())
}
