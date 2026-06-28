// stub.go 는 테스트·검증용 stub Client 구현을 담는다.
// 네트워크 없이 정해진 응답/도구 호출을 반환해, 호출자가 Client 인터페이스 계약을
// 단위 테스트로 검증할 수 있게 한다(D9).
// 런타임 코드는 이 stub 에 의존하지 않는다 — 테스트 헬퍼 또는 외부 노출 목적에만 쓴다.
package llm

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/zipkero/langgraph-go/message"
	"github.com/zipkero/langgraph-go/structured"
	"github.com/zipkero/langgraph-go/tool"
)

// StubResponse 는 StubClient 가 반환할 응답을 미리 지정하는 타입이다.
type StubResponse struct {
	// Message 는 Chat/ChatStream 응답 메시지다.
	Message message.Message
	// ToolCalls 는 Chat/ChatStream 응답에 포함할 도구 호출 목록이다.
	ToolCalls []message.ToolCall
	// Usage 는 반환할 토큰 사용량이다.
	Usage TokenUsage
	// FinishReason 은 반환할 종료 이유다.
	FinishReason string
	// StructuredValue 는 Structured 호출 시 반환할 값이다.
	StructuredValue any
	// Err 는 반환할 에러다. nil 이 아니면 모든 호출이 이 에러를 반환한다.
	Err error
}

// StubClient 는 Client 인터페이스의 테스트용 stub 구현체다.
// 정해진 StubResponse 를 반환하며 실제 네트워크 호출을 하지 않는다.
type StubClient struct {
	// response 는 반환할 응답이다.
	response StubResponse
	// model 은 이 클라이언트의 모델 이름이다.
	model string
	// boundTools 는 BindTools 로 바인딩된 도구 스키마 목록이다.
	boundTools []tool.Schema
}

// NewStubClient 는 지정된 응답을 반환하는 StubClient 를 생성한다.
func NewStubClient(model string, resp StubResponse) *StubClient {
	return &StubClient{
		response: resp,
		model:    model,
	}
}

// Chat 은 미리 지정된 ChatResponse 를 반환한다.
func (s *StubClient) Chat(_ context.Context, _ ChatRequest) (ChatResponse, error) {
	if s.response.Err != nil {
		return ChatResponse{}, s.response.Err
	}
	return ChatResponse{
		Message:      s.response.Message,
		ToolCalls:    s.response.ToolCalls,
		Usage:        s.response.Usage,
		FinishReason: s.response.FinishReason,
	}, nil
}

// ChatStream 은 미리 지정된 메시지를 토큰 단위 이벤트로 방출한 뒤 완료 이벤트를 방출한다.
func (s *StubClient) ChatStream(_ context.Context, _ ChatRequest) (<-chan ChatEvent, error) {
	if s.response.Err != nil {
		return nil, s.response.Err
	}

	ch := make(chan ChatEvent, 3)
	go func() {
		defer close(ch)
		content := s.response.Message.Content
		// 내용이 있으면 토큰 이벤트로 방출
		if content != "" {
			ch <- ChatEvent{Type: ChatEventToken, Token: content}
		}
		// 메시지 완성 이벤트 방출
		msg := s.response.Message
		ch <- ChatEvent{Type: ChatEventMessage, Message: &msg}
		// 종료 이벤트 방출
		resp := ChatResponse{
			Message:      s.response.Message,
			ToolCalls:    s.response.ToolCalls,
			Usage:        s.response.Usage,
			FinishReason: s.response.FinishReason,
		}
		ch <- ChatEvent{Type: ChatEventDone, Response: &resp}
	}()

	return ch, nil
}

// Structured 는 미리 지정된 StructuredValue 를 반환한다.
// StructuredValue 가 nil 이고 응답 Message.Content 가 있으면 raw JSON 으로 파싱을 시도한다.
func (s *StubClient) Structured(_ context.Context, _ ChatRequest, schema structured.Schema) (any, error) {
	if s.response.Err != nil {
		return nil, s.response.Err
	}
	if s.response.StructuredValue != nil {
		return s.response.StructuredValue, nil
	}
	// 응답 메시지 내용을 raw JSON 으로 파싱해 스키마 검증 후 반환
	raw := s.response.Message.Content
	if raw == "" {
		return nil, fmt.Errorf("llm: stub Structured 호출 — StructuredValue 와 Message.Content 가 모두 비어 있습니다")
	}
	if err := structured.Validate(raw, schema); err != nil {
		return nil, fmt.Errorf("llm: stub Structured 스키마 검증 실패: %w", err)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return nil, fmt.Errorf("llm: stub Structured JSON 파싱 실패: %w", err)
	}
	return result, nil
}

// BindTools 는 도구 스키마를 바인딩한 새 StubClient 를 반환한다.
// 원본 StubClient 는 변경하지 않는다(불변 빌더 패턴).
func (s *StubClient) BindTools(tools []tool.Schema) Client {
	clone := *s
	clone.boundTools = make([]tool.Schema, len(tools))
	copy(clone.boundTools, tools)
	return &clone
}

// ParseToolCalls 는 resp.ToolCalls 를 반환한다.
// BindTools 가 활성화된 경우 응답에 포함된 도구 호출을 그대로 돌려준다.
func (s *StubClient) ParseToolCalls(resp ChatResponse) []message.ToolCall {
	if len(resp.ToolCalls) > 0 {
		result := make([]message.ToolCall, len(resp.ToolCalls))
		copy(result, resp.ToolCalls)
		return result
	}
	return []message.ToolCall{}
}

// WithModel 은 지정 모델 이름을 사용하는 새 StubClient 를 반환한다.
func (s *StubClient) WithModel(name string) Client {
	clone := *s
	clone.model = name
	return &clone
}

// ModelName 은 이 StubClient 의 모델 이름을 반환한다.
func (s *StubClient) ModelName() string {
	return s.model
}
