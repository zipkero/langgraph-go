// embedding_test.go 는 InitEmbeddings 파싱·거부 단위 테스트와
// Ollama 서버 실호출 테스트를 담는다.
// 파싱·거부 테스트는 네트워크 없이 항상 실행하고,
// Embed/EmbedQuery 실호출 테스트는 Ollama 서버 도달 가능할 때만 실행한다(D6).
package llm_test

import (
	"bytes"
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/zipkero/langgraph-go/llm"
)

// ─── InitEmbeddings 파싱·거부 테스트 ─────────────────────────────────────────
// 이 테스트들은 네트워크 없이 항상 실행된다(InitChatModel 파싱 테스트 패턴과 동일).

// TestInitEmbeddings_InvalidFormat 은 잘못된 형식의 식별자가 에러를 반환하는지 검증한다.
func TestInitEmbeddings_InvalidFormat(t *testing.T) {
	cases := []string{
		"ollama",            // 콜론 없음
		":nomic-embed-text", // provider 비어 있음
		"ollama:",           // model 비어 있음
		"",                  // 빈 문자열
	}
	for _, spec := range cases {
		_, err := llm.InitEmbeddings(spec)
		if err == nil {
			t.Errorf("잘못된 형식 %q 에 에러가 반환되지 않음", spec)
		}
	}
}

// TestInitEmbeddings_UnsupportedProvider 는 미지원 프로바이더가 에러를 반환하는지 검증한다.
func TestInitEmbeddings_UnsupportedProvider(t *testing.T) {
	unsupported := []string{
		"anthropic:claude-3",
		"gemini:embedding-001",
		"cohere:embed-english-v3",
	}
	for _, spec := range unsupported {
		_, err := llm.InitEmbeddings(spec)
		if err == nil {
			t.Errorf("미지원 프로바이더 %q 에 에러가 반환되지 않음", spec)
		}
	}
}

// TestInitEmbeddings_OpenAIProvider 는 openai 프로바이더가 파싱돼 지원 상태로 반환되는지 검증한다.
// InitEmbeddings 는 네트워크를 타지 않으므로 OPENAI_API_KEY 부재 환경에서도 생성이 성공해야 한다.
func TestInitEmbeddings_OpenAIProvider(t *testing.T) {
	client, err := llm.InitEmbeddings("openai:text-embedding-3-small")
	if err != nil {
		t.Fatalf("openai 는 지원 provider 인데 에러 반환: %v", err)
	}
	if client == nil {
		t.Fatal("InitEmbeddings 가 nil 을 반환함")
	}
}

// TestInitEmbeddings_OllamaProvider 는 ollama 프로바이더가 에러 없이 EmbeddingClient 를 반환하는지 검증한다.
func TestInitEmbeddings_OllamaProvider(t *testing.T) {
	client, err := llm.InitEmbeddings("ollama:nomic-embed-text")
	if err != nil {
		t.Fatalf("ollama provider 초기화 실패: %v", err)
	}
	if client == nil {
		t.Fatal("InitEmbeddings 가 nil 을 반환함")
	}
}

// TestInitEmbeddings_OllamaDefaultModel 은 모델을 지정해도 EmbeddingClient 가 반환되는지 검증한다.
// 실제 Embed 호출은 하지 않으므로 네트워크 없이 실행 가능하다.
func TestInitEmbeddings_OllamaDefaultModel(t *testing.T) {
	// nomic-embed-text 는 기본값이지만, 명시 지정도 허용한다(D7).
	cases := []string{
		"ollama:nomic-embed-text",
		"ollama:mxbai-embed-large",
		"ollama:all-minilm",
	}
	for _, spec := range cases {
		client, err := llm.InitEmbeddings(spec)
		if err != nil {
			t.Errorf("spec %q 초기화 실패: %v", spec, err)
		}
		if client == nil {
			t.Errorf("spec %q 에 nil 클라이언트 반환", spec)
		}
	}
}

// ─── Ollama 실호출 테스트 ─────────────────────────────────────────────────────
// 이 테스트들은 Ollama 서버가 도달 가능할 때만 실행된다(D6).
// 도달 불가 시 t.Skip 으로 건너뛴다.

// ollamaEmbedReady 는 Ollama 서버가 도달 가능하고 임베딩 요청을 처리할 수 있는지 확인한다.
// /api/embed 엔드포인트에 빈 입력으로 헬스 체크를 수행한다.
// 모델 미설치(404)·서버 미실행은 false 를 반환해 실호출 테스트를 skip 하게 한다(D6).
func ollamaEmbedReady(model string) bool {
	reqBody := `{"model":"` + model + `","input":["test"]}`
	httpClient := &http.Client{Timeout: 3 * time.Second}
	resp, err := httpClient.Post(
		"http://localhost:11434/api/embed",
		"application/json",
		bytes.NewBufferString(reqBody),
	)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// TestOllamaEmbeddingClient_Embed 는 Ollama Embed 가 비어 있지 않은 벡터 목록을 반환하는지 검증한다.
func TestOllamaEmbeddingClient_Embed(t *testing.T) {
	const model = "nomic-embed-text"
	if !ollamaEmbedReady(model) {
		t.Skipf("Ollama 서버에 도달할 수 없거나 모델 %q 가 설치되지 않아 테스트를 건너뜁니다", model)
	}

	client, err := llm.InitEmbeddings("ollama:nomic-embed-text")
	if err != nil {
		t.Fatalf("InitEmbeddings 실패: %v", err)
	}

	texts := []string{
		"고양이는 독립적인 동물이다.",
		"개는 충성스러운 동물이다.",
	}
	embeddings, err := client.Embed(context.Background(), texts)
	if err != nil {
		t.Fatalf("Embed 호출 실패: %v", err)
	}
	if len(embeddings) != len(texts) {
		t.Fatalf("임베딩 수 불일치: got %d, want %d", len(embeddings), len(texts))
	}
	for i, vec := range embeddings {
		if len(vec) == 0 {
			t.Errorf("texts[%d] 임베딩 벡터가 비어 있음", i)
		}
	}
}

// TestOllamaEmbeddingClient_EmbedQuery 는 Ollama EmbedQuery 가 비어 있지 않은 벡터를 반환하는지 검증한다.
func TestOllamaEmbeddingClient_EmbedQuery(t *testing.T) {
	const model = "nomic-embed-text"
	if !ollamaEmbedReady(model) {
		t.Skipf("Ollama 서버에 도달할 수 없거나 모델 %q 가 설치되지 않아 테스트를 건너뜁니다", model)
	}

	client, err := llm.InitEmbeddings("ollama:nomic-embed-text")
	if err != nil {
		t.Fatalf("InitEmbeddings 실패: %v", err)
	}

	vec, err := client.EmbedQuery(context.Background(), "Go 언어의 특징은 무엇인가?")
	if err != nil {
		t.Fatalf("EmbedQuery 호출 실패: %v", err)
	}
	if len(vec) == 0 {
		t.Fatal("EmbedQuery 가 비어 있는 벡터를 반환함")
	}
}

// TestOllamaEmbeddingClient_Embed_Empty 는 빈 입력에 대해 빈 결과를 반환하는지 검증한다.
// 이 테스트는 네트워크 없이 실행 가능하다.
func TestOllamaEmbeddingClient_Embed_Empty(t *testing.T) {
	client, err := llm.InitEmbeddings("ollama:nomic-embed-text")
	if err != nil {
		t.Fatalf("InitEmbeddings 실패: %v", err)
	}

	// 빈 입력은 빈 결과를 반환해야 한다(네트워크 호출 없음).
	result, err := client.Embed(context.Background(), []string{})
	if err != nil {
		t.Fatalf("빈 입력 Embed 에서 예상치 않은 에러: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("빈 입력 결과 개수 불일치: got %d, want 0", len(result))
	}
}
