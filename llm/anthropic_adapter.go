// anthropic_adapter.go 는 Anthropic 공식 Go SDK 기반 Client 구현체를 담는다.
// message↔content-block 변환, 도구 바인딩, 구조화 출력(도구 강제 방식),
// 스트리밍, 샘플링 파라미터 필터링(D4), max_tokens 기본값 보장(D5)을 담당한다.
// SDK 타입은 이 파일 내부에서만 사용하며 공개 API에 노출하지 않는다.
package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/zipkero/langgraph-go/message"
	"github.com/zipkero/langgraph-go/structured"
	"github.com/zipkero/langgraph-go/tool"
)

// defaultMaxTokens 는 ChatRequest.MaxTokens(또는 Temperature 등)가 지정되지 않을 때 사용할 기본값이다(D5).
const defaultMaxTokens int64 = 16000

// samplingParamUnsupportedModels 는 temperature/top_p/top_k 미지원 모델 접두사 목록이다(D4).
// 이 접두사로 시작하는 모델에는 샘플링 파라미터를 전송하지 않는다.
var samplingParamUnsupportedModels = []string{
	"claude-opus-4",
}

// anthropicClient 는 Anthropic SDK 기반 Client 구현체다.
type anthropicClient struct {
	// client 는 Anthropic SDK 클라이언트다.
	client anthropic.Client
	// model 은 현재 클라이언트가 사용하는 모델 이름이다.
	model string
	// boundTools 는 BindTools 로 바인딩된 도구 스키마 목록이다.
	boundTools []tool.Schema
}

// newAnthropicClient 는 Anthropic SDK 기반 Client 를 생성한다.
func newAnthropicClient(model string, opts *clientOptions) (Client, error) {
	// 기본 모델 설정
	if model == "" {
		model = string(anthropic.ModelClaudeOpus4_8)
	}

	// SDK 클라이언트 옵션 구성
	var sdkOpts []option.RequestOption
	if opts != nil && opts.apiKey != "" {
		sdkOpts = append(sdkOpts, option.WithAPIKey(opts.apiKey))
	}
	// API 키가 없으면 ANTHROPIC_API_KEY 환경변수를 자동으로 사용한다.

	c := anthropic.NewClient(sdkOpts...)
	return &anthropicClient{
		client: c,
		model:  model,
	}, nil
}

// isSamplingParamUnsupported 는 modelName 이 샘플링 파라미터 미지원 모델인지 판정한다(D4).
func isSamplingParamUnsupported(modelName string) bool {
	for _, prefix := range samplingParamUnsupportedModels {
		if strings.HasPrefix(modelName, prefix) {
			return true
		}
	}
	return false
}

// buildMessages 는 []message.Message 를 Anthropic MessageParam 목록으로 변환한다.
// system 역할 메시지는 별도 반환(Anthropic API 의 System 필드 전용)하고,
// user/assistant/tool 은 MessageParam 목록으로 변환한다.
func buildMessages(msgs []message.Message) (systemBlocks []anthropic.TextBlockParam, msgParams []anthropic.MessageParam) {
	for _, m := range msgs {
		switch m.Role {
		case message.RoleSystem:
			// system 역할은 Anthropic API 의 System 필드로 전달한다.
			systemBlocks = append(systemBlocks, anthropic.TextBlockParam{Text: m.Content})

		case message.RoleUser:
			// user 메시지는 텍스트 블록으로 변환한다.
			msgParams = append(msgParams, anthropic.NewUserMessage(anthropic.NewTextBlock(m.Content)))

		case message.RoleAssistant:
			// assistant 메시지는 텍스트 블록과 tool_use 블록으로 변환한다.
			var blocks []anthropic.ContentBlockParamUnion
			if m.Content != "" {
				blocks = append(blocks, anthropic.NewTextBlock(m.Content))
			}
			for _, tc := range m.ToolCalls {
				// tool_use 블록: Args(json.RawMessage)를 any 로 변환한다.
				var inputAny any
				if len(tc.Args) > 0 {
					if err := json.Unmarshal(tc.Args, &inputAny); err != nil {
						inputAny = map[string]any{}
					}
				} else {
					inputAny = map[string]any{}
				}
				blocks = append(blocks, anthropic.NewToolUseBlock(tc.ID, inputAny, tc.Name))
			}
			if len(blocks) > 0 {
				msgParams = append(msgParams, anthropic.MessageParam{
					Role:    anthropic.MessageParamRoleAssistant,
					Content: blocks,
				})
			}

		case message.RoleTool:
			// tool role 메시지는 user 메시지의 tool_result 블록으로 변환한다(Anthropic 프로토콜).
			block := anthropic.NewToolResultBlock(m.ToolCallID, m.Content, false)
			msgParams = append(msgParams, anthropic.NewUserMessage(block))
		}
	}
	return systemBlocks, msgParams
}

// buildTools 는 []tool.Schema 를 Anthropic ToolUnionParam 목록으로 변환한다.
func buildTools(schemas []tool.Schema) []anthropic.ToolUnionParam {
	tools := make([]anthropic.ToolUnionParam, 0, len(schemas))
	for _, s := range schemas {
		// 파라미터 목록에서 properties 와 required 목록을 구성한다.
		properties := make(map[string]any, len(s.Parameters))
		var required []string
		for _, p := range s.Parameters {
			prop := map[string]any{
				"type": p.Type,
			}
			if p.Description != "" {
				prop["description"] = p.Description
			}
			properties[p.Name] = prop
			if p.Required {
				required = append(required, p.Name)
			}
		}

		tp := &anthropic.ToolParam{
			Name:        s.Name,
			Description: anthropic.String(s.Description),
			InputSchema: anthropic.ToolInputSchemaParam{
				Properties: properties,
				Required:   required,
			},
		}
		tools = append(tools, anthropic.ToolUnionParam{OfTool: tp})
	}
	return tools
}

// buildResponse 는 Anthropic Message 응답을 ChatResponse 로 변환한다.
func buildResponse(resp *anthropic.Message) ChatResponse {
	var textParts []string
	var toolCalls []message.ToolCall

	for _, block := range resp.Content {
		switch v := block.AsAny().(type) {
		case anthropic.TextBlock:
			textParts = append(textParts, v.Text)
		case anthropic.ToolUseBlock:
			// ToolUseBlock.Input 은 json.RawMessage 타입이다.
			toolCalls = append(toolCalls, message.ToolCall{
				ID:   v.ID,
				Name: v.Name,
				Args: json.RawMessage(v.Input),
			})
		}
	}

	content := strings.Join(textParts, "")
	msg := message.Message{
		Role:      message.RoleAssistant,
		Content:   content,
		ToolCalls: toolCalls,
	}

	return ChatResponse{
		Message:      msg,
		ToolCalls:    toolCalls,
		FinishReason: string(resp.StopReason),
		Usage: TokenUsage{
			InputTokens:  int(resp.Usage.InputTokens),
			OutputTokens: int(resp.Usage.OutputTokens),
			TotalTokens:  int(resp.Usage.InputTokens + resp.Usage.OutputTokens),
		},
	}
}

// buildParams 는 ChatRequest 에서 Anthropic MessageNewParams 를 구성한다.
func (c *anthropicClient) buildParams(req ChatRequest) anthropic.MessageNewParams {
	// 모델 결정: 요청에 지정된 모델 우선, 없으면 클라이언트 기본 모델
	modelName := c.model
	if req.Model != "" {
		modelName = req.Model
	}

	// max_tokens 기본값 보장(D5)
	maxTokens := defaultMaxTokens
	// ChatRequest 에 MaxTokens 필드가 없으므로 항상 defaultMaxTokens 사용

	// 메시지 변환
	systemBlocks, msgParams := buildMessages(req.Messages)

	// 도구 목록 구성: req.Tools 와 c.boundTools 를 합산한다.
	var allTools []tool.Schema
	allTools = append(allTools, req.Tools...)
	allTools = append(allTools, c.boundTools...)

	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(modelName),
		MaxTokens: maxTokens,
		Messages:  msgParams,
		System:    systemBlocks,
	}

	// 도구 바인딩
	if len(allTools) > 0 {
		params.Tools = buildTools(allTools)
	}

	// ToolChoice 처리
	if req.ToolChoice != "" {
		switch req.ToolChoice {
		case "auto":
			params.ToolChoice = anthropic.ToolChoiceUnionParam{
				OfAuto: &anthropic.ToolChoiceAutoParam{},
			}
		case "any":
			params.ToolChoice = anthropic.ToolChoiceUnionParam{
				OfAny: &anthropic.ToolChoiceAnyParam{},
			}
		case "none":
			// none 은 ToolChoice 를 설정하지 않는 것과 같다(도구 사용 안 함)
		default:
			// 특정 도구 이름으로 강제
			params.ToolChoice = anthropic.ToolChoiceUnionParam{
				OfTool: &anthropic.ToolChoiceToolParam{Name: req.ToolChoice},
			}
		}
	}

	// 샘플링 파라미터: 미지원 모델에는 전송하지 않는다(D4).
	if !isSamplingParamUnsupported(modelName) && req.Temperature != 0 {
		params.Temperature = anthropic.Float(req.Temperature)
	}

	return params
}

// Chat 은 단일 챗 요청을 Anthropic API 로 실행하고 ChatResponse 를 반환한다.
func (c *anthropicClient) Chat(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	params := c.buildParams(req)
	resp, err := c.client.Messages.New(ctx, params)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("llm: Anthropic 챗 요청 실패: %w", err)
	}
	return buildResponse(resp), nil
}

// ChatStream 은 스트리밍 챗 요청을 실행하고 ChatEvent 채널을 반환한다.
func (c *anthropicClient) ChatStream(ctx context.Context, req ChatRequest) (<-chan ChatEvent, error) {
	params := c.buildParams(req)
	stream := c.client.Messages.NewStreaming(ctx, params)

	ch := make(chan ChatEvent, 16)
	go func() {
		defer close(ch)

		var accMsg anthropic.Message
		for stream.Next() {
			event := stream.Current()
			// 메시지 누적
			if err := accMsg.Accumulate(event); err != nil {
				// 누적 오류는 무시하고 계속 진행
				continue
			}
			// content_block_delta 이벤트에서 텍스트 델타를 토큰으로 방출한다.
			// MessageStreamEventUnionDelta 는 Text 필드를 직접 가지며 type="text_delta" 일 때 유효하다.
			if event.Type == "content_block_delta" {
				delta := event.Delta
				if delta.Type == "text_delta" && delta.Text != "" {
					ch <- ChatEvent{Type: ChatEventToken, Token: delta.Text}
				}
			}
		}

		if err := stream.Err(); err != nil {
			// 스트림 오류 시 에러 포함 done 이벤트를 방출한다.
			errResp := ChatResponse{}
			ch <- ChatEvent{Type: ChatEventDone, Response: &errResp}
			return
		}

		// 최종 응답 조립
		finalResp := buildResponse(&accMsg)
		msg := finalResp.Message
		ch <- ChatEvent{Type: ChatEventMessage, Message: &msg}
		ch <- ChatEvent{Type: ChatEventDone, Response: &finalResp}
	}()

	return ch, nil
}

// Structured 는 스키마 강제 출력을 실행한다.
// 구조화 방식: output_config.format(json_schema)를 사용해 스키마를 강제한다(D3).
// SDK 에서 OutputConfigParam.Format 이 지원되므로 이 경로를 사용한다.
func (c *anthropicClient) Structured(ctx context.Context, req ChatRequest, schema structured.Schema) (any, error) {
	params := c.buildParams(req)

	// JSON 스키마 변환: structured.Schema → map[string]any
	schemaMap := buildSchemaMap(schema)

	// output_config.format = json_schema 로 구조화 출력 강제
	params.OutputConfig = anthropic.OutputConfigParam{
		Format: anthropic.JSONOutputFormatParam{
			Schema: schemaMap,
		},
	}

	resp, err := c.client.Messages.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("llm: Anthropic 구조화 출력 요청 실패: %w", err)
	}

	// 응답에서 텍스트 콘텐츠 추출
	var textParts []string
	for _, block := range resp.Content {
		if tb, ok := block.AsAny().(anthropic.TextBlock); ok {
			textParts = append(textParts, tb.Text)
		}
	}

	rawJSON := strings.Join(textParts, "")
	if rawJSON == "" {
		return nil, fmt.Errorf("llm: Anthropic 구조화 출력 응답이 비어 있습니다")
	}

	// JSON 파싱 후 스키마 검증
	if err := structured.Validate(rawJSON, schema); err != nil {
		return nil, fmt.Errorf("llm: 구조화 출력 스키마 검증 실패: %w", err)
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(rawJSON), &result); err != nil {
		return nil, fmt.Errorf("llm: 구조화 출력 JSON 파싱 실패: %w", err)
	}
	return result, nil
}

// buildSchemaMap 은 structured.Schema 를 Anthropic JSON 스키마 map 으로 변환한다.
// structured.Schema.JSONSchema 를 그대로 사용한다.
func buildSchemaMap(schema structured.Schema) map[string]any {
	if len(schema.JSONSchema) > 0 {
		return schema.JSONSchema
	}
	// JSONSchema 가 비어 있으면 최소 스키마를 반환한다.
	return map[string]any{
		"type": "object",
	}
}

// BindTools 는 도구 스키마를 바인딩해 응답의 tool_calls 파싱을 활성화한 새 Client 를 반환한다.
// 원본 Client 는 변경하지 않는다(불변 빌더 패턴).
func (c *anthropicClient) BindTools(tools []tool.Schema) Client {
	clone := *c
	clone.boundTools = make([]tool.Schema, len(tools))
	copy(clone.boundTools, tools)
	return &clone
}

// ParseToolCalls 는 ChatResponse 에서 도구 호출 목록을 추출해 반환한다.
func (c *anthropicClient) ParseToolCalls(resp ChatResponse) []message.ToolCall {
	if len(resp.ToolCalls) > 0 {
		result := make([]message.ToolCall, len(resp.ToolCalls))
		copy(result, resp.ToolCalls)
		return result
	}
	return []message.ToolCall{}
}

// WithModel 은 지정한 모델 이름을 사용하는 새 Client 를 반환한다.
func (c *anthropicClient) WithModel(name string) Client {
	clone := *c
	clone.model = name
	return &clone
}

// ModelName 은 이 Client 가 사용하는 모델 이름을 반환한다.
func (c *anthropicClient) ModelName() string {
	return c.model
}
