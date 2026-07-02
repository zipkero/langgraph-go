// openai_adapter.go 는 OpenAI 공식 Go SDK 기반 Client 구현체를 담는다.
// message↔ChatCompletion 메시지 변환, 도구 바인딩, 구조화 출력(response_format: json_schema),
// SSE 스트리밍을 담당한다.
// SDK 타입은 이 파일 내부에서만 사용하며 공개 API에 노출하지 않는다(anthropic_adapter.go와 동일 패턴, D-a).
package llm

import (
	"context"
	"encoding/json"
	"fmt"

	openai "github.com/openai/openai-go"
	"github.com/openai/openai-go/option"

	"github.com/zipkero/langgraph-go/message"
	"github.com/zipkero/langgraph-go/structured"
	"github.com/zipkero/langgraph-go/tool"
)

// defaultOpenAIChatModel 은 모델이 지정되지 않았을 때 사용할 기본 챗 모델이다.
const defaultOpenAIChatModel = "gpt-5.4-nano-2026-03-17"

// openaiClient 는 OpenAI SDK 기반 Client 구현체다.
type openaiClient struct {
	// client 는 OpenAI SDK 클라이언트다.
	client openai.Client
	// model 은 현재 클라이언트가 사용하는 모델 이름이다.
	model string
	// boundTools 는 BindTools 로 바인딩된 도구 스키마 목록이다.
	boundTools []tool.Schema
}

// newOpenAIClient 는 OpenAI SDK 기반 Client 를 생성한다.
func newOpenAIClient(model string, opts *clientOptions) (Client, error) {
	// 기본 모델 설정
	if model == "" {
		model = defaultOpenAIChatModel
	}

	// SDK 클라이언트 옵션 구성
	var sdkOpts []option.RequestOption
	if opts != nil && opts.apiKey != "" {
		sdkOpts = append(sdkOpts, option.WithAPIKey(opts.apiKey))
	}
	// API 키가 없으면 SDK 가 OPENAI_API_KEY 환경변수를 자동으로 사용한다(anthropic 어댑터와 동일 규약).

	c := openai.NewClient(sdkOpts...)
	return &openaiClient{
		client: c,
		model:  model,
	}, nil
}

// buildOpenAIMessages 는 []message.Message 를 OpenAI ChatCompletionMessageParamUnion 목록으로 변환한다.
func buildOpenAIMessages(msgs []message.Message) []openai.ChatCompletionMessageParamUnion {
	result := make([]openai.ChatCompletionMessageParamUnion, 0, len(msgs))
	for _, m := range msgs {
		switch m.Role {
		case message.RoleSystem:
			result = append(result, openai.SystemMessage(m.Content))

		case message.RoleUser:
			result = append(result, openai.UserMessage(m.Content))

		case message.RoleAssistant:
			if len(m.ToolCalls) > 0 {
				// 도구 호출이 있는 assistant 메시지는 ToolCalls 를 채운 구조체로 직접 구성한다.
				toolCalls := make([]openai.ChatCompletionMessageToolCallParam, 0, len(m.ToolCalls))
				for _, tc := range m.ToolCalls {
					args := string(tc.Args)
					if args == "" {
						args = "{}"
					}
					toolCalls = append(toolCalls, openai.ChatCompletionMessageToolCallParam{
						ID: tc.ID,
						Function: openai.ChatCompletionMessageToolCallFunctionParam{
							Name:      tc.Name,
							Arguments: args,
						},
					})
				}
				assistantParam := openai.ChatCompletionAssistantMessageParam{
					ToolCalls: toolCalls,
				}
				if m.Content != "" {
					assistantParam.Content = openai.ChatCompletionAssistantMessageParamContentUnion{
						OfString: openai.String(m.Content),
					}
				}
				result = append(result, openai.ChatCompletionMessageParamUnion{OfAssistant: &assistantParam})
			} else {
				result = append(result, openai.AssistantMessage(m.Content))
			}

		case message.RoleTool:
			// tool role 메시지는 대응하는 도구 호출 ID 를 참조하는 tool 메시지로 변환한다.
			result = append(result, openai.ToolMessage(m.Content, m.ToolCallID))
		}
	}
	return result
}

// buildOpenAITools 는 []tool.Schema 를 OpenAI ChatCompletionToolParam 목록으로 변환한다.
func buildOpenAITools(schemas []tool.Schema) []openai.ChatCompletionToolParam {
	tools := make([]openai.ChatCompletionToolParam, 0, len(schemas))
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

		params := openai.FunctionParameters{
			"type":       "object",
			"properties": properties,
		}
		if len(required) > 0 {
			params["required"] = required
		}

		tools = append(tools, openai.ChatCompletionToolParam{
			Function: openai.FunctionDefinitionParam{
				Name:        s.Name,
				Description: openai.String(s.Description),
				Parameters:  params,
			},
		})
	}
	return tools
}

// buildOpenAIResponse 는 OpenAI ChatCompletion 응답을 ChatResponse 로 변환한다.
func buildOpenAIResponse(resp *openai.ChatCompletion) ChatResponse {
	if len(resp.Choices) == 0 {
		return ChatResponse{}
	}
	choice := resp.Choices[0]

	var toolCalls []message.ToolCall
	for _, tc := range choice.Message.ToolCalls {
		args := tc.Function.Arguments
		if args == "" {
			args = "{}"
		}
		toolCalls = append(toolCalls, message.ToolCall{
			ID:   tc.ID,
			Name: tc.Function.Name,
			Args: json.RawMessage(args),
		})
	}

	msg := message.Message{
		Role:      message.RoleAssistant,
		Content:   choice.Message.Content,
		ToolCalls: toolCalls,
	}

	return ChatResponse{
		Message:      msg,
		ToolCalls:    toolCalls,
		FinishReason: choice.FinishReason,
		Usage: TokenUsage{
			InputTokens:  int(resp.Usage.PromptTokens),
			OutputTokens: int(resp.Usage.CompletionTokens),
			TotalTokens:  int(resp.Usage.TotalTokens),
		},
	}
}

// buildParams 는 ChatRequest 에서 OpenAI ChatCompletionNewParams 를 구성한다.
func (c *openaiClient) buildParams(req ChatRequest) openai.ChatCompletionNewParams {
	// 모델 결정: 요청에 지정된 모델 우선, 없으면 클라이언트 기본 모델
	modelName := c.model
	if req.Model != "" {
		modelName = req.Model
	}

	// 메시지 변환
	msgs := buildOpenAIMessages(req.Messages)

	// 도구 목록 구성: req.Tools 와 c.boundTools 를 합산한다.
	var allTools []tool.Schema
	allTools = append(allTools, req.Tools...)
	allTools = append(allTools, c.boundTools...)

	params := openai.ChatCompletionNewParams{
		Model:    modelName,
		Messages: msgs,
	}

	// 도구 바인딩
	if len(allTools) > 0 {
		params.Tools = buildOpenAITools(allTools)
	}

	// ToolChoice 처리
	if req.ToolChoice != "" {
		switch req.ToolChoice {
		case "auto":
			params.ToolChoice = openai.ChatCompletionToolChoiceOptionUnionParam{OfAuto: openai.String("auto")}
		case "none":
			params.ToolChoice = openai.ChatCompletionToolChoiceOptionUnionParam{OfAuto: openai.String("none")}
		case "any":
			// OpenAI 는 "임의 도구 강제 호출"을 "required" 값으로 표현한다.
			params.ToolChoice = openai.ChatCompletionToolChoiceOptionUnionParam{OfAuto: openai.String("required")}
		default:
			// 특정 도구 이름으로 강제
			params.ToolChoice = openai.ChatCompletionToolChoiceOptionUnionParam{
				OfChatCompletionNamedToolChoice: &openai.ChatCompletionNamedToolChoiceParam{
					Function: openai.ChatCompletionNamedToolChoiceFunctionParam{Name: req.ToolChoice},
				},
			}
		}
	}

	// 샘플링 파라미터
	if req.Temperature != 0 {
		params.Temperature = openai.Float(req.Temperature)
	}

	return params
}

// Chat 은 단일 챗 요청을 OpenAI API 로 실행하고 ChatResponse 를 반환한다.
func (c *openaiClient) Chat(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	params := c.buildParams(req)
	resp, err := c.client.Chat.Completions.New(ctx, params)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("llm: OpenAI 챗 요청 실패: %w", err)
	}
	return buildOpenAIResponse(resp), nil
}

// ChatStream 은 스트리밍 챗 요청을 실행하고 ChatEvent 채널을 반환한다.
// SSE 델타는 ChatEventToken 으로, 완성 메시지는 ChatEventMessage, 종료는 ChatEventDone 으로 방출한다.
func (c *openaiClient) ChatStream(ctx context.Context, req ChatRequest) (<-chan ChatEvent, error) {
	params := c.buildParams(req)
	// OpenAI 는 StreamOptions.IncludeUsage 를 켜야 스트림 마지막 chunk 에 usage 가 실린다(미설정 시 항상 0).
	params.StreamOptions = openai.ChatCompletionStreamOptionsParam{IncludeUsage: openai.Bool(true)}
	stream := c.client.Chat.Completions.NewStreaming(ctx, params)

	ch := make(chan ChatEvent, 16)
	go func() {
		defer close(ch)

		var acc openai.ChatCompletionAccumulator
		for stream.Next() {
			chunk := stream.Current()
			acc.AddChunk(chunk)

			// 텍스트 델타를 토큰 이벤트로 방출한다.
			if len(chunk.Choices) > 0 {
				delta := chunk.Choices[0].Delta.Content
				if delta != "" {
					ch <- ChatEvent{Type: ChatEventToken, Token: delta}
				}
			}
		}

		if err := stream.Err(); err != nil {
			// 스트림 오류 시 빈 응답을 담은 done 이벤트를 방출한다.
			errResp := ChatResponse{}
			ch <- ChatEvent{Type: ChatEventDone, Response: &errResp}
			return
		}

		// 최종 응답 조립
		finalResp := buildOpenAIResponse(&acc.ChatCompletion)
		msg := finalResp.Message
		ch <- ChatEvent{Type: ChatEventMessage, Message: &msg}
		ch <- ChatEvent{Type: ChatEventDone, Response: &finalResp}
	}()

	return ch, nil
}

// Structured 는 OpenAI 네이티브 Structured Outputs(response_format: json_schema)로 스키마 강제 출력을 실행한다.
func (c *openaiClient) Structured(ctx context.Context, req ChatRequest, schema structured.Schema) (any, error) {
	params := c.buildParams(req)

	// JSON 스키마 변환: structured.Schema → map[string]any (anthropic 어댑터의 buildSchemaMap 재사용)
	schemaMap := buildSchemaMap(schema)

	name := schema.Name
	if name == "" {
		name = "structured_output"
	}

	params.ResponseFormat = openai.ChatCompletionNewParamsResponseFormatUnion{
		OfJSONSchema: &openai.ResponseFormatJSONSchemaParam{
			JSONSchema: openai.ResponseFormatJSONSchemaJSONSchemaParam{
				Name:   name,
				Schema: schemaMap,
			},
		},
	}

	resp, err := c.client.Chat.Completions.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("llm: OpenAI 구조화 출력 요청 실패: %w", err)
	}
	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("llm: OpenAI 구조화 출력 응답에 choice 가 없습니다")
	}

	rawJSON := resp.Choices[0].Message.Content
	if rawJSON == "" {
		return nil, fmt.Errorf("llm: OpenAI 구조화 출력 응답이 비어 있습니다")
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

// BindTools 는 도구 스키마를 바인딩해 응답의 tool_calls 파싱을 활성화한 새 Client 를 반환한다.
// 원본 Client 는 변경하지 않는다(불변 빌더 패턴).
func (c *openaiClient) BindTools(tools []tool.Schema) Client {
	clone := *c
	clone.boundTools = make([]tool.Schema, len(tools))
	copy(clone.boundTools, tools)
	return &clone
}

// ParseToolCalls 는 ChatResponse 에서 도구 호출 목록을 추출해 반환한다.
func (c *openaiClient) ParseToolCalls(resp ChatResponse) []message.ToolCall {
	if len(resp.ToolCalls) > 0 {
		result := make([]message.ToolCall, len(resp.ToolCalls))
		copy(result, resp.ToolCalls)
		return result
	}
	return []message.ToolCall{}
}

// WithModel 은 지정한 모델 이름을 사용하는 새 Client 를 반환한다.
func (c *openaiClient) WithModel(name string) Client {
	clone := *c
	clone.model = name
	return &clone
}

// ModelName 은 이 Client 가 사용하는 모델 이름을 반환한다.
func (c *openaiClient) ModelName() string {
	return c.model
}
