// middleware 패키지는 모델 호출 훅(WrapModelCall·BeforeModel·DynamicPrompt)과
// 요청 조작 메서드(Override·SetSystemPrompt·StateValue)를 정의한다.
// message·llm·core에 의존하며 agent를 import하지 않는다.
// agent가 middleware를 단방향으로 import하므로 역참조하면 순환 import가 된다(§28-1 규칙4).
package middleware

import (
	"context"
	"fmt"

	"github.com/zipkero/langgraph-go/core"
	"github.com/zipkero/langgraph-go/llm"
)

// ModelResponse 는 미들웨어 체인이 주고받는 모델 응답 래퍼다.
// llm.ChatResponse 를 감싸 미들웨어가 응답을 가공할 수 있게 한다.
type ModelResponse struct {
	// Response 는 실제 llm.ChatResponse 다.
	Response llm.ChatResponse
}

// ModelRequest 는 미들웨어 체인이 주고받는 모델 요청이다.
// 상태 인자는 core.State 이고 agent 를 참조하지 않는다(D1).
type ModelRequest struct {
	// State 는 에이전트 실행 중 공유되는 범용 상태 맵이다.
	State core.State
	// Model 은 이 요청에 사용할 llm.Client 다. nil 이면 에이전트 기본 클라이언트를 사용한다.
	Model llm.Client
	// SystemPrompt 는 이 요청의 시스템 프롬프트다.
	SystemPrompt string
	// Messages 는 이 요청의 대화 메시지 목록이다.
	Messages []string
}

// Override 는 이 요청에 한해 다른 llm.Client 를 사용하도록 교체된 새 ModelRequest 를 반환한다.
// 원본 ModelRequest 는 변경하지 않는다(값 복사 의미론).
func (r ModelRequest) Override(model llm.Client) ModelRequest {
	r.Model = model
	return r
}

// SetSystemPrompt 는 시스템 프롬프트를 치환한 새 ModelRequest 를 반환한다.
// 원본 ModelRequest 는 변경하지 않는다(값 복사 의미론).
func (r ModelRequest) SetSystemPrompt(p string) ModelRequest {
	r.SystemPrompt = p
	return r
}

// StateValue 는 공유 상태(core.State)에서 key 에 해당하는 값을 반환한다.
// 키가 없거나 State 가 nil 이면 nil 을 반환한다.
func (r ModelRequest) StateValue(key string) any {
	if r.State == nil {
		return nil
	}
	return r.State[key]
}

// ModelHandler 는 ModelRequest 를 받아 ModelResponse 를 반환하는 핸들러 함수 타입이다.
// WrapModelCall 미들웨어는 이 핸들러를 감싸 요청·응답을 가공한다.
type ModelHandler func(ctx context.Context, req ModelRequest) (ModelResponse, error)

// Runtime 은 미들웨어가 실행 컨텍스트에 접근하기 위한 인터페이스다.
// BeforeModel 훅이 에이전트 설정·이벤트 방출 등을 위해 Runtime 을 인자로 받는다.
type Runtime interface {
	// ModelName 은 현재 에이전트가 사용하는 기본 모델 이름을 반환한다.
	ModelName() string
}

// Middleware 는 미들웨어 훅의 계약 인터페이스다.
// Apply 는 주어진 ModelHandler 를 감싸 새 ModelHandler 를 반환한다.
// 미들웨어들은 체인 형태로 합성되며 바깥에서 안쪽 순서로 적용된다.
type Middleware interface {
	// Apply 는 next 핸들러를 감싸는 새 ModelHandler 를 반환한다.
	Apply(next ModelHandler) ModelHandler
}

// MiddlewareFunc 는 함수를 Middleware 인터페이스로 감싸는 어댑터 타입이다.
type MiddlewareFunc func(next ModelHandler) ModelHandler

// Apply 는 MiddlewareFunc 를 Middleware 인터페이스로 구현한다.
func (f MiddlewareFunc) Apply(next ModelHandler) ModelHandler {
	return f(next)
}

// wrapModelCallMiddleware 는 WrapModelCall 로 생성되는 미들웨어 구현체다.
type wrapModelCallMiddleware struct {
	fn func(ctx context.Context, req ModelRequest, next ModelHandler) (ModelResponse, error)
}

// Apply 는 fn 으로 next 핸들러를 감싸는 새 ModelHandler 를 반환한다.
func (w *wrapModelCallMiddleware) Apply(next ModelHandler) ModelHandler {
	return func(ctx context.Context, req ModelRequest) (ModelResponse, error) {
		return w.fn(ctx, req, next)
	}
}

// WrapModelCall 은 모델 호출을 감싸는 미들웨어를 생성한다.
// fn 은 요청·응답을 가공하고 next 를 호출해 실제 모델 호출을 위임할 수 있다.
// 노드를 추가하지 않으며, 순수하게 모델 호출 전후를 가공하는 용도다.
func WrapModelCall(fn func(ctx context.Context, req ModelRequest, next ModelHandler) (ModelResponse, error)) Middleware {
	return &wrapModelCallMiddleware{fn: fn}
}

// beforeModelMiddleware 는 BeforeModel 로 생성되는 미들웨어 구현체다.
type beforeModelMiddleware struct {
	name string
	fn   func(ctx context.Context, state core.State, rt Runtime) error
}

// Apply 는 모델 호출 전에 fn 을 실행한다.
// fn 이 에러를 반환하면 모델 호출을 차단하고 에러를 전파한다.
func (b *beforeModelMiddleware) Apply(next ModelHandler) ModelHandler {
	return func(ctx context.Context, req ModelRequest) (ModelResponse, error) {
		// Runtime 은 ModelRequest 에서 구성한다
		rt := &modelRequestRuntime{req: req}
		if err := b.fn(ctx, req.State, rt); err != nil {
			return ModelResponse{}, fmt.Errorf("middleware %q: BeforeModel 훅 실패: %w", b.name, err)
		}
		return next(ctx, req)
	}
}

// BeforeModel 은 모델 호출 전 실행되는 훅 미들웨어를 생성한다.
// name 은 훅 식별자이며, fn 은 core.State 에 접근하거나 에러를 반환해 호출을 차단할 수 있다.
func BeforeModel(name string, fn func(ctx context.Context, state core.State, rt Runtime) error) Middleware {
	return &beforeModelMiddleware{name: name, fn: fn}
}

// dynamicPromptMiddleware 는 DynamicPrompt 로 생성되는 미들웨어 구현체다.
type dynamicPromptMiddleware struct {
	fn func(ctx context.Context, req ModelRequest) (string, error)
}

// Apply 는 호출마다 fn 으로 시스템 프롬프트를 생성해 ModelRequest 에 치환한다.
func (d *dynamicPromptMiddleware) Apply(next ModelHandler) ModelHandler {
	return func(ctx context.Context, req ModelRequest) (ModelResponse, error) {
		prompt, err := d.fn(ctx, req)
		if err != nil {
			return ModelResponse{}, fmt.Errorf("middleware: DynamicPrompt 생성 실패: %w", err)
		}
		req = req.SetSystemPrompt(prompt)
		return next(ctx, req)
	}
}

// DynamicPrompt 는 호출마다 시스템 프롬프트를 동적으로 생성하는 미들웨어를 생성한다.
// fn 은 ModelRequest 를 받아 이 호출의 시스템 프롬프트를 반환한다.
func DynamicPrompt(fn func(ctx context.Context, req ModelRequest) (string, error)) Middleware {
	return &dynamicPromptMiddleware{fn: fn}
}

// modelRequestRuntime 은 ModelRequest 로부터 Runtime 인터페이스를 구현하는 내부 타입이다.
type modelRequestRuntime struct {
	req ModelRequest
}

// ModelName 은 요청에 바인딩된 모델 이름을 반환한다.
// Model 이 nil 이면 빈 문자열을 반환한다.
func (r *modelRequestRuntime) ModelName() string {
	if r.req.Model == nil {
		return ""
	}
	return r.req.Model.ModelName()
}

// Chain 은 여러 Middleware 를 체인으로 합성해 단일 ModelHandler 를 만드는 빌더다.
// middlewares 는 바깥에서 안쪽 순서로 적용된다.
// 즉, middlewares[0] 이 가장 바깥에서 실행되고 terminal 이 가장 안쪽(실제 모델 호출)이다.
type Chain struct {
	middlewares []Middleware
}

// NewChain 은 주어진 미들웨어 목록으로 Chain 을 생성한다.
func NewChain(middlewares ...Middleware) *Chain {
	return &Chain{middlewares: middlewares}
}

// Then 은 추가 미들웨어를 체인 끝에 덧붙인 새 Chain 을 반환한다.
func (c *Chain) Then(m ...Middleware) *Chain {
	next := make([]Middleware, len(c.middlewares)+len(m))
	copy(next, c.middlewares)
	copy(next[len(c.middlewares):], m)
	return &Chain{middlewares: next}
}

// Handler 는 terminal 핸들러를 체인의 끝에 두고 모든 미들웨어를 합성한 최종 ModelHandler 를 반환한다.
// middlewares[0] 이 가장 바깥 래퍼가 된다.
func (c *Chain) Handler(terminal ModelHandler) ModelHandler {
	h := terminal
	// 역순으로 적용해 middlewares[0] 이 가장 바깥에 오도록 한다
	for i := len(c.middlewares) - 1; i >= 0; i-- {
		h = c.middlewares[i].Apply(h)
	}
	return h
}

// Apply 는 Chain 자체가 Middleware 인터페이스를 구현하도록 한다.
// 체인을 다른 체인에 중첩할 때 사용한다.
func (c *Chain) Apply(next ModelHandler) ModelHandler {
	return c.Handler(next)
}
