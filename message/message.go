// message 패키지는 메시지 모델과 누적 리듀서를 정의한다.
// core만 의존하며, 다른 Phase 1 패키지를 import하지 않는 가장 하단 노드다.
package message

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Role 은 메시지 발신자 역할을 나타내는 명명된 문자열 타입이다.
type Role string

const (
	// RoleSystem 은 시스템 프롬프트 역할이다.
	RoleSystem Role = "system"
	// RoleUser 는 사용자 메시지 역할이다.
	RoleUser Role = "user"
	// RoleAssistant 는 AI 어시스턴트 메시지 역할이다.
	RoleAssistant Role = "assistant"
	// RoleTool 은 도구 실행 결과 메시지 역할이다.
	RoleTool Role = "tool"
)

// MessageID 는 메시지를 식별하는 타입이다.
type MessageID = string

// ToolCallID 는 도구 호출을 식별하는 타입이다.
type ToolCallID = string

// MessageName 은 메시지에 부여된 이름 타입이다.
type MessageName = string

// RemoveAllSentinel 은 전체 삭제를 나타내는 센티널 ID 상수다.
// AddMessages/ApplyRemovals 에서 이 ID를 가진 메시지를 감지하면 전체를 비운다.
const RemoveAllSentinel MessageID = "__remove_all__"

// ToolCall 은 AI 어시스턴트가 요청한 도구 호출 단위다.
type ToolCall struct {
	// ID 는 도구 호출 식별자다.
	ID ToolCallID
	// Name 은 호출할 도구 이름이다.
	Name string
	// Args 는 도구에 전달할 인자(JSON 형식)다.
	Args json.RawMessage
}

// ToolResult 는 도구 실행 결과를 담는 타입이다.
type ToolResult struct {
	// ToolCallID 는 대응하는 도구 호출의 ID다.
	ToolCallID ToolCallID
	// Name 은 실행된 도구 이름이다.
	Name string
	// Content 는 도구 실행 결과 텍스트다.
	Content string
	// IsError 는 도구 실행이 오류로 끝났는지를 나타낸다.
	IsError bool
}

// Message 는 대화의 단일 메시지 단위다.
// 역할(Role)과 내용(Content)을 기본으로 하며, 도구 호출(ToolCalls)이나
// 도구 결과 참조(ToolCallID)를 함께 담을 수 있다.
type Message struct {
	// Role 은 메시지 발신자 역할이다.
	Role Role
	// Content 는 메시지 텍스트 내용이다.
	Content string
	// Name 은 메시지에 부여된 선택적 이름이다.
	Name MessageName
	// ID 는 메시지 식별자다. 비어 있으면 ID 없는 메시지다.
	ID MessageID
	// ToolCalls 는 AI 어시스턴트가 요청한 도구 호출 목록이다.
	ToolCalls []ToolCall
	// ToolCallID 는 이 메시지가 응답하는 도구 호출의 ID다(RoleTool 메시지에 사용).
	ToolCallID ToolCallID
}

// isRemoveMarker 는 메시지가 삭제 마커인지 판정한다.
// 삭제 마커는 내용 없이 ID만 갖는 메시지다.
func isRemoveMarker(m Message) bool {
	return m.ID != "" && m.Content == "" && len(m.ToolCalls) == 0 && m.ToolCallID == ""
}

// NewSystemMessage 는 system 역할의 메시지를 생성한다.
func NewSystemMessage(content string) Message {
	return Message{Role: RoleSystem, Content: content}
}

// NewUserMessage 는 user 역할의 메시지를 생성한다.
func NewUserMessage(content string) Message {
	return Message{Role: RoleUser, Content: content}
}

// NewAssistantMessage 는 assistant 역할의 텍스트 메시지를 생성한다.
func NewAssistantMessage(content string) Message {
	return Message{Role: RoleAssistant, Content: content}
}

// NewToolMessage 는 tool 역할의 메시지를 생성한다.
// toolCallID 는 대응하는 도구 호출 ID, name 은 도구 이름, content 는 실행 결과다.
func NewToolMessage(toolCallID, name, content string) Message {
	return Message{
		Role:       RoleTool,
		Content:    content,
		Name:       name,
		ToolCallID: toolCallID,
	}
}

// NewAssistantToolCalls 는 도구 호출 목록을 담은 assistant 역할 메시지를 생성한다.
func NewAssistantToolCalls(calls []ToolCall) Message {
	return Message{Role: RoleAssistant, ToolCalls: calls}
}

// WithName 은 메시지에 이름을 부여한 새 메시지를 반환한다.
func WithName(m Message, name string) Message {
	m.Name = name
	return m
}

// LastMessage 는 msgs 에서 마지막 메시지를 반환한다.
// msgs 가 비어 있으면 (Message{}, false)를 반환한다.
func LastMessage(msgs []Message) (Message, bool) {
	if len(msgs) == 0 {
		return Message{}, false
	}
	return msgs[len(msgs)-1], true
}

// LastAIMessage 는 msgs 에서 마지막 assistant 역할 메시지를 반환한다.
// 해당하는 메시지가 없으면 (Message{}, false)를 반환한다.
func LastAIMessage(msgs []Message) (Message, bool) {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == RoleAssistant {
			return msgs[i], true
		}
	}
	return Message{}, false
}

// HasToolCalls 는 메시지에 도구 호출이 포함되어 있는지 반환한다.
func HasToolCalls(m Message) bool {
	return len(m.ToolCalls) > 0
}

// ExtractToolCalls 는 메시지에서 도구 호출 목록을 추출해 반환한다.
// 도구 호출이 없으면 빈 슬라이스를 반환한다.
func ExtractToolCalls(m Message) []ToolCall {
	if len(m.ToolCalls) == 0 {
		return []ToolCall{}
	}
	result := make([]ToolCall, len(m.ToolCalls))
	copy(result, m.ToolCalls)
	return result
}

// FilterByName 은 msgs 에서 Name 이 name 과 일치하는 메시지만 필터링해 반환한다.
func FilterByName(msgs []Message, name string) []Message {
	var result []Message
	for _, m := range msgs {
		if m.Name == name {
			result = append(result, m)
		}
	}
	return result
}

// AddMessages 는 base 메시지 목록에 incoming 메시지를 병합한다.
// 동일 ID(비어 있지 않은 ID)를 가진 메시지는 upsert(덮어쓰기), 신규 ID 또는 ID 없는 메시지는 뒤에 append한다.
// incoming 에 RemoveAllSentinel ID 가 포함되면 전체를 비운다.
func AddMessages(base, incoming []Message) []Message {
	// RemoveAllSentinel 확인: incoming 에 센티널이 있으면 전체 삭제 후 이후 메시지만 포함
	for i, m := range incoming {
		if m.ID == RemoveAllSentinel {
			// 센티널 이후 메시지만 새 base로 시작
			rest := incoming[i+1:]
			result := make([]Message, 0, len(rest))
			for _, r := range rest {
				if r.ID != RemoveAllSentinel {
					result = append(result, r)
				}
			}
			return result
		}
	}

	// base의 ID → index 맵 구축
	idIndex := make(map[MessageID]int, len(base))
	for i, m := range base {
		if m.ID != "" {
			idIndex[m.ID] = i
		}
	}

	result := make([]Message, len(base))
	copy(result, base)

	for _, m := range incoming {
		if m.ID != "" {
			if idx, exists := idIndex[m.ID]; exists {
				// 동일 ID 존재 → upsert
				result[idx] = m
			} else {
				// 신규 ID → append
				idIndex[m.ID] = len(result)
				result = append(result, m)
			}
		} else {
			// ID 없는 메시지 → append
			result = append(result, m)
		}
	}
	return result
}

// RemoveMessage 는 지정 id 를 가진 삭제 마커 메시지를 생성해 반환한다.
// 이 마커를 AddMessages 로 병합하거나 ApplyRemovals 에 전달하면 해당 메시지가 제거된다.
func RemoveMessage(id MessageID) Message {
	return Message{ID: id}
}

// ApplyRemovals 는 msgs 에서 삭제 마커와 마커가 가리키는 메시지를 모두 제거한 결과를 반환한다.
// RemoveAllSentinel ID를 가진 마커가 있으면 전체를 비운다.
func ApplyRemovals(msgs []Message) []Message {
	// RemoveAllSentinel 확인
	for _, m := range msgs {
		if m.ID == RemoveAllSentinel {
			return []Message{}
		}
	}

	// 삭제할 ID 집합 수집
	removeIDs := make(map[MessageID]bool)
	for _, m := range msgs {
		if isRemoveMarker(m) {
			removeIDs[m.ID] = true
		}
	}

	if len(removeIDs) == 0 {
		return msgs
	}

	result := make([]Message, 0, len(msgs))
	for _, m := range msgs {
		// 삭제 마커 자신도 제거, 대상 ID 메시지도 제거
		if removeIDs[m.ID] {
			continue
		}
		result = append(result, m)
	}
	return result
}

// TrimOptions 는 TrimMessages 에 전달하는 윈도 트리밍 옵션이다.
type TrimOptions struct {
	// Strategy 는 트리밍 전략이다. 현재 "last"(최신 메시지 우선 보존)를 지원한다.
	Strategy string
	// StartOn 은 윈도 시작 역할 필터다. 지정하면 윈도 앞쪽을 해당 역할 메시지부터 시작한다.
	StartOn Role
	// EndOn 은 윈도 끝 역할 필터다. 지정하면 윈도 뒤쪽을 해당 역할 메시지로 끝낸다.
	EndOn Role
	// MaxTokens 는 허용 최대 토큰 수다. 0이면 토큰 제한 없이 전체를 반환한다.
	MaxTokens int
}

// TrimMessages 는 opts 에 따라 msgs 를 트리밍해 윈도 내 메시지만 반환한다.
// strategy="last": 최신 메시지부터 역순으로 토큰을 누적해 max_tokens 이하로 자른다.
// start_on: 윈도 앞쪽을 해당 역할 메시지에서 시작하도록 잘라낸다.
// end_on: 윈도 뒤쪽을 해당 역할 메시지로 끝내도록 잘라낸다.
// MaxTokens 가 0이면 토큰 제한 없이 start_on/end_on 필터만 적용한다.
func TrimMessages(msgs []Message, opts TrimOptions) []Message {
	if len(msgs) == 0 {
		return msgs
	}

	result := msgs

	// strategy 기본값은 "last"
	strategy := opts.Strategy
	if strategy == "" {
		strategy = "last"
	}

	if opts.MaxTokens > 0 && strategy == "last" {
		// 뒤에서부터 누적해 max_tokens 초과 전까지 포함
		total := 0
		startIdx := len(result)
		for i := len(result) - 1; i >= 0; i-- {
			t := countTokensApproxOne(result[i])
			if total+t > opts.MaxTokens {
				break
			}
			total += t
			startIdx = i
		}
		result = result[startIdx:]
	}

	// start_on: 윈도 앞쪽을 해당 역할 메시지부터 시작
	if opts.StartOn != "" {
		for i, m := range result {
			if m.Role == opts.StartOn {
				result = result[i:]
				break
			}
		}
	}

	// end_on: 윈도 뒤쪽을 해당 역할 메시지로 끝냄
	if opts.EndOn != "" {
		for i := len(result) - 1; i >= 0; i-- {
			if result[i].Role == opts.EndOn {
				result = result[:i+1]
				break
			}
		}
	}

	return result
}

// countTokensApproxOne 은 단일 메시지의 근사 토큰 수를 계산한다.
// 내부 helper로, CountTokensApprox 와 TrimMessages 가 함께 사용한다.
func countTokensApproxOne(m Message) int {
	// 근사 공식: 내용 문자 수 / 4 (영문 기준 ~4자/토큰) + 역할 오버헤드 4토큰
	// 한글·유니코드는 rune 수로 계산해 동일 공식 적용
	total := utf8.RuneCountInString(m.Content) / 4
	if total == 0 && len(m.Content) > 0 {
		total = 1
	}
	// 도구 호출 인자도 근사 토큰에 포함
	for _, tc := range m.ToolCalls {
		total += utf8.RuneCountInString(tc.Name)/4 + 1
		total += len(tc.Args)/4 + 1
	}
	// 역할 오버헤드
	total += 4
	return total
}

// CountTokensApprox 는 msgs 의 전체 근사 토큰 수를 반환한다.
// 외부 토크나이저(tiktoken 등) 없이 표준 라이브러리 기반 근사로 계산한다.
// 내용 문자 수를 4로 나눈 값에 메시지별 오버헤드를 더하는 휴리스틱을 사용한다.
func CountTokensApprox(msgs []Message) int {
	total := 0
	for _, m := range msgs {
		total += countTokensApproxOne(m)
	}
	return total
}

// PrettyPrint 는 단일 메시지를 가독성 있는 형태로 포맷해 문자열로 반환한다.
func PrettyPrint(m Message) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("[%s]", strings.ToUpper(string(m.Role))))
	if m.Name != "" {
		sb.WriteString(fmt.Sprintf("(%s)", m.Name))
	}
	if m.ID != "" {
		sb.WriteString(fmt.Sprintf(" id=%s", m.ID))
	}
	if m.Content != "" {
		sb.WriteString(": ")
		sb.WriteString(m.Content)
	}
	if len(m.ToolCalls) > 0 {
		sb.WriteString(" tool_calls=[")
		for i, tc := range m.ToolCalls {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(fmt.Sprintf("{id:%s name:%s args:%s}", tc.ID, tc.Name, string(tc.Args)))
		}
		sb.WriteString("]")
	}
	if m.ToolCallID != "" {
		sb.WriteString(fmt.Sprintf(" tool_call_id=%s", m.ToolCallID))
	}
	return sb.String()
}

// PrettyPrintMessages 는 메시지 목록을 가독성 있는 형태로 포맷해 문자열로 반환한다.
func PrettyPrintMessages(msgs []Message) string {
	lines := make([]string, 0, len(msgs))
	for _, m := range msgs {
		lines = append(lines, PrettyPrint(m))
	}
	return strings.Join(lines, "\n")
}
