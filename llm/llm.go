// llm 패키지는 챗 모델 추상화, 도구 바인딩, 구조화/JSON 출력, 스트리밍을 담당한다.
// message, tool, structured, core에 의존하며, Anthropic SDK 의존은 task-005에서 추가된다.
// 임베딩 타입·팩토리(EmbeddingClient/InitEmbeddings/Embed/EmbedQuery)는 Phase 3 범위다.
package llm

import (
	"context"
	"fmt"
	"strings"

	"github.com/zipkero/langgraph-go/message"
	"github.com/zipkero/langgraph-go/structured"
	"github.com/zipkero/langgraph-go/tool"
)

// TokenUsage 는 단일 요청에서 소비된 토큰 수를 담는다.
type TokenUsage struct {
	// InputTokens 는 입력(프롬프트) 토큰 수다.
	InputTokens int
	// OutputTokens 는 출력(완성) 토큰 수다.
	OutputTokens int
	// TotalTokens 는 전체 토큰 수다.
	TotalTokens int
}

// ChatRequest 는 챗 모델에 전달하는 요청이다.
type ChatRequest struct {
	// Messages 는 대화 메시지 목록이다.
	Messages []message.Message
	// Tools 는 도구 호출에 사용할 도구 스키마 목록이다.
	Tools []tool.Schema
	// ToolChoice 는 도구 선택 전략이다("auto", "none", 특정 도구 이름 등).
	ToolChoice string
	// Model 은 이 요청에 사용할 모델 이름이다. 비어 있으면 클라이언트 기본 모델을 쓴다.
	Model string
	// Temperature 는 샘플링 온도다. 0이면 기본값을 사용한다.
	// claude-opus-4-8 등 미지원 모델에는 어댑터가 전송하지 않는다(D4).
	Temperature float64
}

// ChatResponse 는 챗 모델의 응답이다.
type ChatResponse struct {
	// Message 는 어시스턴트 응답 메시지다.
	Message message.Message
	// ToolCalls 는 어시스턴트가 요청한 도구 호출 목록이다.
	// BindTools 로 도구 바인딩이 활성화됐을 때 ParseToolCalls 로 추출한 값이 채워진다.
	ToolCalls []message.ToolCall
	// Usage 는 이 요청의 토큰 사용량이다.
	Usage TokenUsage
	// FinishReason 은 응답 종료 이유다("stop", "tool_use", "max_tokens" 등).
	FinishReason string
}

// ChatEventType 은 스트림 이벤트 종류를 나타내는 명명된 문자열 타입이다.
type ChatEventType string

const (
	// ChatEventToken 은 토큰 단위 텍스트 스트리밍 이벤트다.
	ChatEventToken ChatEventType = "token"
	// ChatEventMessage 는 완성된 메시지 이벤트다.
	ChatEventMessage ChatEventType = "message"
	// ChatEventDone 은 스트림 종료 이벤트다.
	ChatEventDone ChatEventType = "done"
)

// ChatEvent 는 ChatStream 이 방출하는 스트리밍 이벤트다.
type ChatEvent struct {
	// Type 은 이벤트 종류다(token/message/done).
	Type ChatEventType
	// Token 은 ChatEventToken 이벤트에서 방출된 텍스트 토큰이다.
	Token string
	// Message 는 ChatEventMessage 이벤트에서 완성된 메시지다.
	Message *message.Message
	// Response 는 ChatEventDone 이벤트에서 최종 응답이다.
	Response *ChatResponse
}

// Option 은 InitChatModel 에 전달하는 옵션 타입이다.
type Option func(*clientOptions)

// clientOptions 는 클라이언트 생성 옵션을 담는 내부 타입이다.
type clientOptions struct {
	// apiKey 는 프로바이더 API 키다.
	apiKey string
	// defaultModel 은 기본 모델 이름이다.
	defaultModel string
}

// WithAPIKey 는 API 키를 지정하는 옵션이다.
func WithAPIKey(key string) Option {
	return func(o *clientOptions) {
		o.apiKey = key
	}
}

// WithDefaultModel 은 기본 모델을 지정하는 옵션이다.
func WithDefaultModel(model string) Option {
	return func(o *clientOptions) {
		o.defaultModel = model
	}
}

// Client 는 챗 모델 호출, 도구 바인딩, 구조화 출력, 스트리밍의 계약 인터페이스다.
// 프로바이더 중립 계약으로, Anthropic 어댑터(task-005)와 테스트용 stub 이 이를 구현한다.
type Client interface {
	// Chat 은 단일 챗 요청을 실행하고 응답을 반환한다.
	Chat(ctx context.Context, req ChatRequest) (ChatResponse, error)

	// ChatStream 은 스트리밍 챗 요청을 실행하고 이벤트 채널을 반환한다.
	ChatStream(ctx context.Context, req ChatRequest) (<-chan ChatEvent, error)

	// Structured 는 스키마 강제 출력을 실행하고 파싱된 값을 반환한다.
	// structured.Schema 에 맞는 값을 any 로 반환한다.
	Structured(ctx context.Context, req ChatRequest, schema structured.Schema) (any, error)

	// BindTools 는 주어진 도구 스키마 목록을 바인딩해 응답의 tool_calls 파싱을 활성화한
	// 새 Client 를 반환한다. 원본 Client 는 변경하지 않는다(불변 빌더 패턴).
	BindTools(tools []tool.Schema) Client

	// ParseToolCalls 는 ChatResponse 에서 도구 호출 목록을 추출해 반환한다.
	// BindTools 로 도구 바인딩이 활성화된 경우에만 유효한 결과를 돌려준다.
	ParseToolCalls(resp ChatResponse) []message.ToolCall

	// WithModel 은 지정한 모델 이름을 사용하는 새 Client 를 반환한다.
	WithModel(name string) Client

	// ModelName 은 이 Client 가 사용하는 모델 이름을 반환한다.
	ModelName() string
}

// providerSpec 은 InitChatModel 에서 파싱된 프로바이더·모델 정보를 담는다.
type providerSpec struct {
	// provider 는 프로바이더 이름이다(anthropic 등).
	provider string
	// model 은 모델 이름이다(claude-opus-4-8 등).
	model string
}

// parseProviderSpec 은 "provider:model" 형식 문자열을 파싱한다.
// 형식이 잘못됐거나 provider/model 이 비어 있으면 에러를 반환한다.
func parseProviderSpec(spec string) (providerSpec, error) {
	parts := strings.SplitN(spec, ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return providerSpec{}, fmt.Errorf(
			"llm: 잘못된 모델 식별자 %q — \"provider:model\" 형식이어야 합니다(예: anthropic:claude-opus-4-8)",
			spec,
		)
	}
	return providerSpec{provider: parts[0], model: parts[1]}, nil
}

// InitChatModel 은 "provider:model" 형식 식별자로 Client 를 생성한다.
// provider=anthropic 이면 Anthropic 어댑터를(task-005 에서 완성), 그 외는 에러를 반환한다.
// 빌드가 통과하도록 anthropic 분기는 newAnthropicClient 를 호출하며,
// 해당 함수는 anthropic_adapter.go(task-005)에서 구현된다.
func InitChatModel(spec string, opts ...Option) (Client, error) {
	ps, err := parseProviderSpec(spec)
	if err != nil {
		return nil, err
	}

	o := &clientOptions{}
	for _, opt := range opts {
		opt(o)
	}
	if o.defaultModel == "" {
		o.defaultModel = ps.model
	}

	switch ps.provider {
	case "anthropic":
		return newAnthropicClient(ps.model, o)
	case "openai":
		return newOpenAIClient(ps.model, o)
	default:
		return nil, fmt.Errorf(
			"llm: 지원하지 않는 프로바이더 %q — 현재는 \"anthropic\", \"openai\" 만 지원합니다",
			ps.provider,
		)
	}
}
