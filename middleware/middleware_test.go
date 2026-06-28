// middleware_test.go 는 middleware 패키지의 단위 테스트다.
// WrapModelCall·BeforeModel·DynamicPrompt 합성·ModelRequest 조작을
// stub llm.Client 로 관찰한다(D9).
package middleware

import (
	"context"
	"errors"
	"testing"

	"github.com/zipkero/langgraph-go/core"
	"github.com/zipkero/langgraph-go/llm"
	"github.com/zipkero/langgraph-go/message"
)

// newTestStub 은 테스트에 쓰는 StubClient 를 생성하는 헬퍼다.
func newTestStub(modelName string, content string) *llm.StubClient {
	return llm.NewStubClient(modelName, llm.StubResponse{
		Message: message.NewAssistantMessage(content),
	})
}

// echoHandler 는 테스트에서 수신된 ModelRequest 를 기록하는 터미널 핸들러다.
// captured 포인터가 nil 이 아니면 수신된 요청을 저장한다.
func echoHandler(captured *ModelRequest) ModelHandler {
	return func(_ context.Context, req ModelRequest) (ModelResponse, error) {
		if captured != nil {
			*captured = req
		}
		resp := llm.ChatResponse{
			Message: message.NewAssistantMessage("ok"),
		}
		return ModelResponse{Response: resp}, nil
	}
}

// TestWrapModelCall_기본_래핑 은 WrapModelCall 이 핸들러를 감싸
// 요청과 응답을 가공하는 것을 확인한다.
func TestWrapModelCall_기본_래핑(t *testing.T) {
	called := false
	var gotReq ModelRequest

	wrap := WrapModelCall(func(ctx context.Context, req ModelRequest, next ModelHandler) (ModelResponse, error) {
		called = true
		// 요청 가공: 시스템 프롬프트 추가
		req = req.SetSystemPrompt("래핑된 프롬프트")
		gotReq = req
		resp, err := next(ctx, req)
		if err != nil {
			return resp, err
		}
		// 응답 가공: FinishReason 덮어쓰기
		resp.Response.FinishReason = "wrapped"
		return resp, nil
	})

	terminal := echoHandler(nil)
	chain := NewChain(wrap)
	handler := chain.Handler(terminal)

	ctx := context.Background()
	req := ModelRequest{State: core.State{"key": "val"}}
	resp, err := handler(ctx, req)

	if err != nil {
		t.Fatalf("예상치 못한 에러: %v", err)
	}
	if !called {
		t.Error("WrapModelCall 함수가 호출되지 않았습니다")
	}
	if gotReq.SystemPrompt != "래핑된 프롬프트" {
		t.Errorf("시스템 프롬프트 가공 실패: got %q", gotReq.SystemPrompt)
	}
	if resp.Response.FinishReason != "wrapped" {
		t.Errorf("응답 가공 실패: FinishReason=%q", resp.Response.FinishReason)
	}
}

// TestWrapModelCall_체인_순서 는 여러 WrapModelCall 이 바깥→안쪽 순서로 실행됨을 확인한다.
func TestWrapModelCall_체인_순서(t *testing.T) {
	order := make([]string, 0, 4)

	outer := WrapModelCall(func(ctx context.Context, req ModelRequest, next ModelHandler) (ModelResponse, error) {
		order = append(order, "outer-before")
		resp, err := next(ctx, req)
		order = append(order, "outer-after")
		return resp, err
	})
	inner := WrapModelCall(func(ctx context.Context, req ModelRequest, next ModelHandler) (ModelResponse, error) {
		order = append(order, "inner-before")
		resp, err := next(ctx, req)
		order = append(order, "inner-after")
		return resp, err
	})

	chain := NewChain(outer, inner)
	handler := chain.Handler(echoHandler(nil))

	_, err := handler(context.Background(), ModelRequest{})
	if err != nil {
		t.Fatalf("예상치 못한 에러: %v", err)
	}

	want := []string{"outer-before", "inner-before", "inner-after", "outer-after"}
	if len(order) != len(want) {
		t.Fatalf("실행 순서 항목 수: got %d, want %d", len(order), len(want))
	}
	for i, v := range want {
		if order[i] != v {
			t.Errorf("순서[%d]: got %q, want %q", i, order[i], v)
		}
	}
}

// TestBeforeModel_통과 는 BeforeModel 훅이 에러 없이 통과해 다음 핸들러가 실행됨을 확인한다.
func TestBeforeModel_통과(t *testing.T) {
	var capturedState core.State
	hookCalled := false

	mw := BeforeModel("test-hook", func(_ context.Context, st core.State, rt Runtime) error {
		hookCalled = true
		capturedState = st
		return nil
	})

	var capturedReq ModelRequest
	handler := mw.Apply(echoHandler(&capturedReq))

	state := core.State{"user": "alice", "count": 42}
	req := ModelRequest{State: state}
	_, err := handler(context.Background(), req)

	if err != nil {
		t.Fatalf("예상치 못한 에러: %v", err)
	}
	if !hookCalled {
		t.Error("BeforeModel 훅이 호출되지 않았습니다")
	}
	if capturedState["user"] != "alice" {
		t.Errorf("상태 접근 실패: user=%v", capturedState["user"])
	}
	if capturedState["count"] != 42 {
		t.Errorf("상태 접근 실패: count=%v", capturedState["count"])
	}
}

// TestBeforeModel_차단 은 BeforeModel 훅이 에러를 반환하면 모델 호출이 차단됨을 확인한다.
func TestBeforeModel_차단(t *testing.T) {
	blockErr := errors.New("차단됨")
	terminalCalled := false

	mw := BeforeModel("blocker", func(_ context.Context, _ core.State, _ Runtime) error {
		return blockErr
	})

	terminal := func(_ context.Context, _ ModelRequest) (ModelResponse, error) {
		terminalCalled = true
		return ModelResponse{}, nil
	}
	handler := mw.Apply(terminal)

	_, err := handler(context.Background(), ModelRequest{})

	if err == nil {
		t.Fatal("에러가 반환되어야 하는데 nil 입니다")
	}
	if !errors.Is(err, blockErr) {
		t.Errorf("에러가 blockErr 를 포함하지 않습니다: %v", err)
	}
	if terminalCalled {
		t.Error("터미널 핸들러가 호출되면 안 되는데 호출되었습니다")
	}
}

// TestBeforeModel_Runtime_ModelName 은 BeforeModel 훅에 전달된 Runtime 이
// 올바른 모델 이름을 반환하는지 확인한다.
func TestBeforeModel_Runtime_ModelName(t *testing.T) {
	stub := newTestStub("claude-opus-4-8", "응답")
	var gotModelName string

	mw := BeforeModel("model-check", func(_ context.Context, _ core.State, rt Runtime) error {
		gotModelName = rt.ModelName()
		return nil
	})

	handler := mw.Apply(echoHandler(nil))
	req := ModelRequest{Model: stub}
	_, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("예상치 못한 에러: %v", err)
	}
	if gotModelName != "claude-opus-4-8" {
		t.Errorf("Runtime.ModelName(): got %q, want %q", gotModelName, "claude-opus-4-8")
	}
}

// TestDynamicPrompt_치환 은 DynamicPrompt 가 호출마다 시스템 프롬프트를 치환함을 확인한다.
func TestDynamicPrompt_치환(t *testing.T) {
	callCount := 0
	mw := DynamicPrompt(func(_ context.Context, req ModelRequest) (string, error) {
		callCount++
		return "동적 프롬프트", nil
	})

	var captured ModelRequest
	handler := mw.Apply(echoHandler(&captured))

	req := ModelRequest{SystemPrompt: "원본 프롬프트"}
	_, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("예상치 못한 에러: %v", err)
	}
	if captured.SystemPrompt != "동적 프롬프트" {
		t.Errorf("DynamicPrompt 치환 실패: got %q", captured.SystemPrompt)
	}
	if callCount != 1 {
		t.Errorf("DynamicPrompt 함수 호출 횟수: got %d, want 1", callCount)
	}
}

// TestDynamicPrompt_에러 는 DynamicPrompt 의 fn 이 에러를 반환하면 호출이 중단됨을 확인한다.
func TestDynamicPrompt_에러(t *testing.T) {
	promptErr := errors.New("프롬프트 생성 실패")
	mw := DynamicPrompt(func(_ context.Context, _ ModelRequest) (string, error) {
		return "", promptErr
	})

	terminalCalled := false
	terminal := func(_ context.Context, _ ModelRequest) (ModelResponse, error) {
		terminalCalled = true
		return ModelResponse{}, nil
	}
	handler := mw.Apply(terminal)

	_, err := handler(context.Background(), ModelRequest{})
	if err == nil {
		t.Fatal("에러가 반환되어야 하는데 nil 입니다")
	}
	if !errors.Is(err, promptErr) {
		t.Errorf("에러가 promptErr 를 포함하지 않습니다: %v", err)
	}
	if terminalCalled {
		t.Error("터미널 핸들러가 호출되면 안 됩니다")
	}
}

// TestModelRequest_Override 는 Override 가 모델을 교체한 새 요청을 반환하고
// 원본을 변경하지 않음을 확인한다.
func TestModelRequest_Override(t *testing.T) {
	stub1 := newTestStub("model-1", "응답1")
	stub2 := newTestStub("model-2", "응답2")

	orig := ModelRequest{Model: stub1}
	overridden := orig.Override(stub2)

	if orig.Model.ModelName() != "model-1" {
		t.Errorf("원본 Model 이 변경됨: %q", orig.Model.ModelName())
	}
	if overridden.Model.ModelName() != "model-2" {
		t.Errorf("Override 실패: got %q, want %q", overridden.Model.ModelName(), "model-2")
	}
}

// TestModelRequest_SetSystemPrompt 는 SetSystemPrompt 가 프롬프트를 치환한 새 요청을 반환하고
// 원본을 변경하지 않음을 확인한다.
func TestModelRequest_SetSystemPrompt(t *testing.T) {
	orig := ModelRequest{SystemPrompt: "원본"}
	updated := orig.SetSystemPrompt("새 프롬프트")

	if orig.SystemPrompt != "원본" {
		t.Errorf("원본 SystemPrompt 가 변경됨: %q", orig.SystemPrompt)
	}
	if updated.SystemPrompt != "새 프롬프트" {
		t.Errorf("SetSystemPrompt 실패: got %q", updated.SystemPrompt)
	}
}

// TestModelRequest_StateValue 는 StateValue 가 공유 상태에서 키 값을 올바로 반환함을 확인한다.
func TestModelRequest_StateValue(t *testing.T) {
	state := core.State{
		"name":  "alice",
		"count": 99,
	}
	req := ModelRequest{State: state}

	if v := req.StateValue("name"); v != "alice" {
		t.Errorf("StateValue(\"name\"): got %v, want \"alice\"", v)
	}
	if v := req.StateValue("count"); v != 99 {
		t.Errorf("StateValue(\"count\"): got %v, want 99", v)
	}
	if v := req.StateValue("없는키"); v != nil {
		t.Errorf("StateValue(없는키): got %v, want nil", v)
	}

	// nil State 도 안전하게 처리해야 한다
	nilReq := ModelRequest{}
	if v := nilReq.StateValue("key"); v != nil {
		t.Errorf("nil State 에서 StateValue: got %v, want nil", v)
	}
}

// TestChain_복합 은 WrapModelCall·BeforeModel·DynamicPrompt 를 조합했을 때
// 모두 실행되고 효과가 누적됨을 확인한다.
func TestChain_복합(t *testing.T) {
	var execOrder []string
	var capturedReq ModelRequest

	wrap := WrapModelCall(func(ctx context.Context, req ModelRequest, next ModelHandler) (ModelResponse, error) {
		execOrder = append(execOrder, "wrap")
		return next(ctx, req)
	})
	before := BeforeModel("check", func(_ context.Context, st core.State, _ Runtime) error {
		execOrder = append(execOrder, "before")
		return nil
	})
	dynPrompt := DynamicPrompt(func(_ context.Context, req ModelRequest) (string, error) {
		execOrder = append(execOrder, "dynamic")
		return "동적", nil
	})

	chain := NewChain(wrap, before, dynPrompt)
	handler := chain.Handler(echoHandler(&capturedReq))

	_, err := handler(context.Background(), ModelRequest{State: core.State{"x": 1}})
	if err != nil {
		t.Fatalf("예상치 못한 에러: %v", err)
	}

	wantOrder := []string{"wrap", "before", "dynamic"}
	if len(execOrder) != len(wantOrder) {
		t.Fatalf("실행 순서 항목 수: got %d, want %d — %v", len(execOrder), len(wantOrder), execOrder)
	}
	for i, v := range wantOrder {
		if execOrder[i] != v {
			t.Errorf("실행 순서[%d]: got %q, want %q", i, execOrder[i], v)
		}
	}
	if capturedReq.SystemPrompt != "동적" {
		t.Errorf("DynamicPrompt 치환 효과 미반영: got %q", capturedReq.SystemPrompt)
	}
}

// TestOverride_stub_모델_호출_반영 은 Override 로 교체된 모델이
// WrapModelCall 체인 내에서 실제 호출에 반영됨을 stub 으로 확인한다.
func TestOverride_stub_모델_호출_반영(t *testing.T) {
	stub := newTestStub("override-model", "override-응답")
	var capturedModel llm.Client

	wrap := WrapModelCall(func(ctx context.Context, req ModelRequest, next ModelHandler) (ModelResponse, error) {
		// Override 로 모델 교체
		req = req.Override(stub)
		capturedModel = req.Model
		return next(ctx, req)
	})

	var capturedReq ModelRequest
	chain := NewChain(wrap)
	handler := chain.Handler(echoHandler(&capturedReq))

	origStub := newTestStub("original-model", "원본-응답")
	req := ModelRequest{Model: origStub}
	_, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("예상치 못한 에러: %v", err)
	}
	if capturedModel == nil {
		t.Fatal("capturedModel 이 nil 입니다")
	}
	if capturedModel.ModelName() != "override-model" {
		t.Errorf("Override 모델이 반영되지 않음: got %q", capturedModel.ModelName())
	}
	if capturedReq.Model.ModelName() != "override-model" {
		t.Errorf("터미널에서 Override 모델 미반영: got %q", capturedReq.Model.ModelName())
	}
}

// TestChain_Then_추가 는 Then 이 미들웨어를 덧붙인 새 Chain 을 반환함을 확인한다.
func TestChain_Then_추가(t *testing.T) {
	var order []string

	m1 := MiddlewareFunc(func(next ModelHandler) ModelHandler {
		return func(ctx context.Context, req ModelRequest) (ModelResponse, error) {
			order = append(order, "m1")
			return next(ctx, req)
		}
	})
	m2 := MiddlewareFunc(func(next ModelHandler) ModelHandler {
		return func(ctx context.Context, req ModelRequest) (ModelResponse, error) {
			order = append(order, "m2")
			return next(ctx, req)
		}
	})

	chain := NewChain(m1).Then(m2)
	handler := chain.Handler(echoHandler(nil))

	_, err := handler(context.Background(), ModelRequest{})
	if err != nil {
		t.Fatalf("예상치 못한 에러: %v", err)
	}
	if len(order) != 2 || order[0] != "m1" || order[1] != "m2" {
		t.Errorf("Then 이후 체인 순서 오류: %v", order)
	}
}
