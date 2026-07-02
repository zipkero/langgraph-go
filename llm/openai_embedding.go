// openai_embedding.go 는 OpenAI 공식 Go SDK 기반 EmbeddingClient 구현체를 담는다.
// openai_adapter.go 의 SDK 클라이언트 생성 규약(WithAPIKey 옵션 → 없으면 OPENAI_API_KEY 자동)을 공유한다(D-b).
package llm

import (
	"context"
	"fmt"

	openai "github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

// defaultOpenAIEmbeddingModel 은 모델이 지정되지 않았을 때 사용할 기본 임베딩 모델이다(1536차원).
const defaultOpenAIEmbeddingModel = "text-embedding-3-small"

// openaiEmbeddingClient 는 OpenAI SDK 기반 EmbeddingClient 구현체다.
type openaiEmbeddingClient struct {
	// client 는 OpenAI SDK 클라이언트다.
	client openai.Client
	// model 은 임베딩에 사용할 모델 이름이다.
	model string
}

// newOpenAIEmbeddingClient 는 OpenAI SDK 기반 EmbeddingClient 를 생성한다.
func newOpenAIEmbeddingClient(model string, opts *clientOptions) (EmbeddingClient, error) {
	if model == "" {
		model = defaultOpenAIEmbeddingModel
	}

	var sdkOpts []option.RequestOption
	if opts != nil && opts.apiKey != "" {
		sdkOpts = append(sdkOpts, option.WithAPIKey(opts.apiKey))
	}
	// API 키가 없으면 SDK 가 OPENAI_API_KEY 환경변수를 자동으로 사용한다(챗 어댑터와 동일 규약).

	c := openai.NewClient(sdkOpts...)
	return &openaiEmbeddingClient{
		client: c,
		model:  model,
	}, nil
}

// Embed 는 텍스트 배치를 OpenAI Embeddings API 로 임베딩해 벡터 목록을 반환한다.
// 입력 순서와 반환 벡터 목록의 순서가 일치한다. 연결 실패·빈 응답은 error 로 반환한다(Ollama 구현과 동일 방식).
func (c *openaiEmbeddingClient) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return [][]float32{}, nil
	}

	resp, err := c.client.Embeddings.New(ctx, openai.EmbeddingNewParams{
		Input: openai.EmbeddingNewParamsInputUnion{OfArrayOfStrings: texts},
		Model: c.model,
	})
	if err != nil {
		return nil, fmt.Errorf("llm: OpenAI 임베딩 요청 실패: %w", err)
	}
	if len(resp.Data) == 0 {
		return nil, fmt.Errorf("llm: OpenAI 임베딩 응답에 벡터가 없습니다")
	}

	// 응답의 Index 필드를 기준으로 입력 순서를 보존한다. 개수·인덱스가 입력과 어긋난
	// 비정상 응답은 잘못된 매핑(panic·조용한 누락)을 만들 수 있으므로 error 로 반환한다.
	if len(resp.Data) != len(texts) {
		return nil, fmt.Errorf("llm: OpenAI 임베딩 응답 개수 불일치: got %d, want %d", len(resp.Data), len(texts))
	}
	result := make([][]float32, len(texts))
	for _, d := range resp.Data {
		if d.Index < 0 || int(d.Index) >= len(texts) {
			return nil, fmt.Errorf("llm: OpenAI 임베딩 응답 인덱스가 범위 밖입니다: %d", d.Index)
		}
		vec := make([]float32, len(d.Embedding))
		for i, v := range d.Embedding {
			vec[i] = float32(v)
		}
		result[d.Index] = vec
	}
	for i, vec := range result {
		if vec == nil {
			return nil, fmt.Errorf("llm: OpenAI 임베딩 응답에 texts[%d] 벡터가 없습니다(중복 인덱스)", i)
		}
	}
	return result, nil
}

// EmbedQuery 는 단일 질의 텍스트를 OpenAI Embeddings API 로 임베딩해 벡터를 반환한다.
// 연결 실패·빈 응답은 error 로 반환한다(Ollama 구현과 동일 방식).
func (c *openaiEmbeddingClient) EmbedQuery(ctx context.Context, text string) ([]float32, error) {
	embeddings, err := c.Embed(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(embeddings) == 0 || len(embeddings[0]) == 0 {
		return nil, fmt.Errorf("llm: OpenAI EmbedQuery 응답에 벡터가 없습니다")
	}
	return embeddings[0], nil
}
