// prompt 패키지는 프롬프트 템플릿과 체인을 담당한다.
// message·llm·structured 를 소비하며, 플레이스홀더 치환과 모델 호출을 연결한다.
package prompt

import (
	"context"
	"fmt"
	"strings"

	"github.com/zipkero/langgraph-go/llm"
	"github.com/zipkero/langgraph-go/message"
	"github.com/zipkero/langgraph-go/structured"
)

// MessagesPlaceholder 는 변수명으로 메시지 리스트를 삽입하는 플레이스홀더다.
// FromMessages 의 MessageSpec 에 포함되어, Format 호출 시 vars 의 해당 변수에 담긴
// []message.Message 를 그 자리에 펼쳐 삽입한다.
type MessagesPlaceholder struct {
	// VarName 은 vars 에서 조회할 변수 이름이다.
	VarName string
}

// specKind 는 MessageSpec 이 담는 값 종류를 나타낸다.
type specKind int

const (
	specKindText        specKind = iota // 역할+텍스트 템플릿 쌍
	specKindPlaceholder                 // MessagesPlaceholder
)

// MessageSpec 은 FromMessages 에 전달하는 템플릿 입력 단위다.
// 역할+텍스트 쌍 또는 MessagesPlaceholder 를 담는다.
type MessageSpec struct {
	kind        specKind
	role        message.Role
	template    string
	placeholder MessagesPlaceholder
}

// SpecFromRole 은 역할과 텍스트 템플릿 쌍으로 MessageSpec 을 생성한다.
// 텍스트 안의 {varName} 형식 플레이스홀더는 Format 호출 시 vars 로 치환된다.
func SpecFromRole(role message.Role, template string) MessageSpec {
	return MessageSpec{
		kind:     specKindText,
		role:     role,
		template: template,
	}
}

// SpecFromPlaceholder 는 MessagesPlaceholder 로 MessageSpec 을 생성한다.
// Format 호출 시 vars[placeholder.VarName] 의 []message.Message 가 그 자리에 삽입된다.
func SpecFromPlaceholder(p MessagesPlaceholder) MessageSpec {
	return MessageSpec{
		kind:        specKindPlaceholder,
		placeholder: p,
	}
}

// templateEntry 는 PromptTemplate 내부에서 사용하는 항목 단위다.
type templateEntry struct {
	kind        specKind
	role        message.Role
	template    string
	placeholder MessagesPlaceholder
}

// PromptTemplate 은 역할별 템플릿과 메시지 플레이스홀더를 담는 템플릿이다.
// Format 을 호출하면 vars 로 플레이스홀더를 채운 []message.Message 를 반환한다.
type PromptTemplate struct {
	entries []templateEntry
}

// FromMessages 는 MessageSpec 목록으로 PromptTemplate 을 생성한다.
// specs 의 각 항목은 역할+텍스트 쌍 또는 MessagesPlaceholder 다.
func FromMessages(specs []MessageSpec) PromptTemplate {
	entries := make([]templateEntry, len(specs))
	for i, s := range specs {
		entries[i] = templateEntry{
			kind:        s.kind,
			role:        s.role,
			template:    s.template,
			placeholder: s.placeholder,
		}
	}
	return PromptTemplate{entries: entries}
}

// FromTemplate 은 단일 텍스트 템플릿으로 PromptTemplate 을 생성한다.
// 템플릿은 human(user) 역할로 등록된다.
// {varName} 형식 플레이스홀더는 Format 호출 시 vars 로 치환된다.
func FromTemplate(text string) PromptTemplate {
	return PromptTemplate{
		entries: []templateEntry{
			{kind: specKindText, role: message.RoleUser, template: text},
		},
	}
}

// Format 은 vars 로 플레이스홀더를 채운 []message.Message 를 반환한다.
// {varName} 형식의 텍스트 플레이스홀더는 vars[varName] 의 문자열 값으로 치환된다.
// MessagesPlaceholder 는 vars[VarName] 의 []message.Message 로 펼쳐진다.
// 누락된 변수가 있으면 에러를 반환한다.
func (p PromptTemplate) Format(vars map[string]any) ([]message.Message, error) {
	var result []message.Message

	for _, entry := range p.entries {
		switch entry.kind {
		case specKindPlaceholder:
			// MessagesPlaceholder: vars 에서 []message.Message 를 꺼내 삽입
			val, ok := vars[entry.placeholder.VarName]
			if !ok {
				return nil, fmt.Errorf(
					"prompt: MessagesPlaceholder 변수 %q 가 vars 에 없습니다",
					entry.placeholder.VarName,
				)
			}
			msgs, ok := val.([]message.Message)
			if !ok {
				return nil, fmt.Errorf(
					"prompt: MessagesPlaceholder 변수 %q 의 값이 []message.Message 가 아닙니다 (실제 타입: %T)",
					entry.placeholder.VarName,
					val,
				)
			}
			result = append(result, msgs...)

		case specKindText:
			// 텍스트 템플릿: {varName} 플레이스홀더를 vars 로 치환
			text, err := applyTextVars(entry.template, vars)
			if err != nil {
				return nil, err
			}
			result = append(result, message.Message{
				Role:    entry.role,
				Content: text,
			})
		}
	}

	return result, nil
}

// applyTextVars 는 template 내의 {varName} 패턴을 vars 의 값으로 치환한다.
// 변수가 vars 에 없으면 에러를 반환한다.
func applyTextVars(template string, vars map[string]any) (string, error) {
	result := template

	// vars 를 순회하며 {key} 패턴을 값으로 치환
	for key, val := range vars {
		placeholder := "{" + key + "}"
		if strings.Contains(result, placeholder) {
			result = strings.ReplaceAll(result, placeholder, fmt.Sprintf("%v", val))
		}
	}

	// 치환 후 남은 {varName} 패턴 검사 — 누락 변수 탐지
	start := strings.Index(result, "{")
	for start != -1 {
		end := strings.Index(result[start:], "}")
		if end == -1 {
			break
		}
		varName := result[start+1 : start+end]
		// 중괄호가 중첩되거나 내부에 중괄호가 있으면 건너뜀
		if !strings.Contains(varName, "{") && varName != "" {
			return "", fmt.Errorf("prompt: 텍스트 템플릿 플레이스홀더 {%s} 에 대한 vars 값이 없습니다", varName)
		}
		start = strings.Index(result[start+end+1:], "{")
		if start != -1 {
			start += (start + end + 1)
		}
	}

	return result, nil
}

// Chain 은 PromptTemplate → llm.Client(→ structured 파서) 흐름을 담는 호출 단위다.
// Pipe 로 생성하며 Invoke 로 실행한다.
type Chain struct {
	template PromptTemplate
	model    llm.Client
	schema   *structured.Schema
}

// Pipe 는 template 과 model 을 연결한 Chain 을 반환한다.
// Chain.Invoke 호출 시 template.Format → model.Chat(또는 model.Structured) 흐름으로 실행된다.
func Pipe(template PromptTemplate, model llm.Client) Chain {
	return Chain{
		template: template,
		model:    model,
	}
}

// WithStructuredOutput 은 schema 를 강제하는 구조화 출력 Chain 을 반환한다.
// 반환된 Chain.Invoke 는 model.Structured 경로를 사용해 구조화 결과를 반환한다.
// 원본 Chain 은 변경하지 않는다(불변 빌더 패턴).
func (c Chain) WithStructuredOutput(schema structured.Schema) Chain {
	clone := c
	clone.schema = &schema
	return clone
}

// Invoke 는 vars 로 템플릿을 포매팅하고 모델을 호출해 결과를 반환한다.
// WithStructuredOutput 이 설정되어 있으면 model.Structured 경로를 사용하고
// 구조화 값(any)을 반환한다. 그렇지 않으면 model.Chat 으로 ChatResponse 를 반환한다.
func (c Chain) Invoke(ctx context.Context, vars map[string]any) (any, error) {
	// 템플릿 포매팅
	msgs, err := c.template.Format(vars)
	if err != nil {
		return nil, fmt.Errorf("prompt: 템플릿 포매팅 실패: %w", err)
	}

	req := llm.ChatRequest{
		Messages: msgs,
	}

	if c.schema != nil {
		// 구조화 출력 경로
		result, err := c.model.Structured(ctx, req, *c.schema)
		if err != nil {
			return nil, fmt.Errorf("prompt: 구조화 출력 호출 실패: %w", err)
		}
		return result, nil
	}

	// 일반 챗 경로
	resp, err := c.model.Chat(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("prompt: 모델 호출 실패: %w", err)
	}
	return resp, nil
}
