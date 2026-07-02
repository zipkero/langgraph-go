// embedding.go 는 임베딩 클라이언트 추상화와 InitEmbeddings 팩토리를 담는다.
// EmbeddingClient 인터페이스, InitEmbeddings("provider:model") 팩토리,
// Ollama 임베딩 구현체(표준 net/http 직접 호출)가 여기에 있다.
// 기존 챗 Client/StubClient/어댑터는 이 파일과 무관하며 수정하지 않는다(Phase 3, D1).
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// defaultOllamaBaseURL 은 Ollama 서버의 기본 베이스 URL이다(D2).
const defaultOllamaBaseURL = "http://localhost:11434"

// defaultEmbeddingModel 은 모델이 지정되지 않았을 때 사용할 기본 임베딩 모델이다(D7).
const defaultEmbeddingModel = "nomic-embed-text"

// EmbeddingClient 는 텍스트 임베딩 호출 계약 인터페이스다.
// Ollama 임베딩 구현체와 테스트용 stub 이 이를 구현한다.
type EmbeddingClient interface {
	// Embed 는 텍스트 배치를 임베딩해 벡터 목록을 반환한다.
	// texts 의 순서와 반환 벡터 목록의 순서가 일치한다.
	Embed(ctx context.Context, texts []string) ([][]float32, error)

	// EmbedQuery 는 단일 질의 텍스트를 임베딩해 벡터를 반환한다.
	EmbedQuery(ctx context.Context, text string) ([]float32, error)
}

// InitEmbeddings 는 "provider:model" 형식 식별자로 EmbeddingClient 를 생성한다.
// provider=ollama 이면 Ollama 임베딩 클라이언트를, 그 외는 에러를 반환한다.
// 베이스 URL 미지정 시 http://localhost:11434, 모델 미지정 시 nomic-embed-text 를 기본값으로 사용한다(D2, D7).
func InitEmbeddings(spec string, opts ...Option) (EmbeddingClient, error) {
	ps, err := parseProviderSpec(spec)
	if err != nil {
		return nil, err
	}

	// 옵션 빌더 적용 — InitChatModel 과 같은 Option/clientOptions 규약을 따른다.
	o := &clientOptions{}
	for _, opt := range opts {
		opt(o)
	}

	// 모델 미지정 시 파싱된 모델 이름 사용; spec 모델도 없으면 기본 임베딩 모델을 적용한다.
	model := ps.model
	if model == "" {
		model = defaultEmbeddingModel
	}
	if o.defaultModel != "" {
		model = o.defaultModel
	}

	switch ps.provider {
	case "ollama":
		// 베이스 URL 미지정 시 기본값 사용(D2).
		// apiKey 필드를 베이스 URL 주입에 사용하지 않고, WithBaseURL 이 없으므로
		// clientOptions 에 baseURL 이 없다 — 기본값을 사용한다.
		return newOllamaEmbeddingClient(model, defaultOllamaBaseURL), nil
	case "openai":
		return newOpenAIEmbeddingClient(model, o)
	default:
		return nil, fmt.Errorf(
			"llm: 지원하지 않는 임베딩 프로바이더 %q — 현재는 \"ollama\", \"openai\" 만 지원합니다",
			ps.provider,
		)
	}
}

// ollamaEmbedRequest 는 Ollama /api/embed 엔드포인트 요청 본문이다.
// Ollama v0.1.26+ 에서 도입된 통합 임베딩 엔드포인트를 사용한다.
type ollamaEmbedRequest struct {
	// Model 은 임베딩에 사용할 Ollama 모델 이름이다.
	Model string `json:"model"`
	// Input 은 임베딩할 텍스트 목록이다. 단일 텍스트도 목록으로 전달한다.
	Input []string `json:"input"`
}

// ollamaEmbedResponse 는 Ollama /api/embed 엔드포인트 응답 본문이다.
type ollamaEmbedResponse struct {
	// Embeddings 는 입력 텍스트 순서에 대응하는 벡터 목록이다.
	Embeddings [][]float32 `json:"embeddings"`
}

// ollamaEmbeddingClient 는 Ollama 임베딩 엔드포인트를 표준 net/http 로 직접 호출하는 구현체다(D1).
type ollamaEmbeddingClient struct {
	// model 은 임베딩에 사용할 Ollama 모델 이름이다.
	model string
	// baseURL 은 Ollama 서버의 베이스 URL이다(예: http://localhost:11434).
	baseURL string
	// httpClient 는 HTTP 호출에 사용할 클라이언트다.
	httpClient *http.Client
}

// newOllamaEmbeddingClient 는 Ollama 임베딩 클라이언트를 생성한다.
func newOllamaEmbeddingClient(model, baseURL string) *ollamaEmbeddingClient {
	return &ollamaEmbeddingClient{
		model:      model,
		baseURL:    baseURL,
		httpClient: &http.Client{},
	}
}

// Embed 는 텍스트 배치를 Ollama /api/embed 엔드포인트로 임베딩해 벡터 목록을 반환한다.
// 연결 실패·비정상 상태코드·빈 임베딩 응답은 error 로 반환한다.
func (c *ollamaEmbeddingClient) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return [][]float32{}, nil
	}

	reqBody := ollamaEmbedRequest{
		Model: c.model,
		Input: texts,
	}

	embeddings, err := c.callEmbedAPI(ctx, reqBody)
	if err != nil {
		return nil, err
	}

	if len(embeddings) == 0 {
		return nil, fmt.Errorf("llm: Ollama 임베딩 응답에 벡터가 없습니다")
	}

	return embeddings, nil
}

// EmbedQuery 는 단일 질의 텍스트를 Ollama /api/embed 엔드포인트로 임베딩해 벡터를 반환한다.
// 연결 실패·비정상 상태코드·빈 임베딩 응답은 error 로 반환한다.
func (c *ollamaEmbeddingClient) EmbedQuery(ctx context.Context, text string) ([]float32, error) {
	embeddings, err := c.Embed(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(embeddings) == 0 || len(embeddings[0]) == 0 {
		return nil, fmt.Errorf("llm: Ollama EmbedQuery 응답에 벡터가 없습니다")
	}
	return embeddings[0], nil
}

// callEmbedAPI 는 Ollama /api/embed 엔드포인트를 호출해 임베딩 결과를 반환한다.
// 연결 실패·비정상 HTTP 상태코드는 error 로 반환한다.
func (c *ollamaEmbeddingClient) callEmbedAPI(ctx context.Context, reqBody ollamaEmbedRequest) ([][]float32, error) {
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("llm: Ollama 임베딩 요청 직렬화 실패: %w", err)
	}

	url := c.baseURL + "/api/embed"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("llm: Ollama 임베딩 요청 생성 실패: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("llm: Ollama 임베딩 서버 연결 실패: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("llm: Ollama 임베딩 비정상 상태코드 %d: %s", resp.StatusCode, string(body))
	}

	var embedResp ollamaEmbedResponse
	if err := json.NewDecoder(resp.Body).Decode(&embedResp); err != nil {
		return nil, fmt.Errorf("llm: Ollama 임베딩 응답 파싱 실패: %w", err)
	}

	return embedResp.Embeddings, nil
}
