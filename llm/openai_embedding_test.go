// openai_embedding_test.go 는 OpenAI 임베딩 어댑터의 단위 테스트와 라이브 스모크 테스트를 담는다.
//
// 테스트 두 종류:
//  1. 스펙 파싱·빈 입력 단위 테스트 — 네트워크 불필요, 항상 실행
//  2. 라이브 스모크 테스트(1536차원·입력 순서 보존) — OPENAI_API_KEY 환경변수 없으면 skip(D-f)
package llm_test

import (
	"context"
	"math"
	"testing"

	"github.com/zipkero/langgraph-go/llm"
)

// ─── 단위 테스트 (네트워크 불필요) ───────────────────────────────────────────

// TestOpenAIEmbedding_InitEmbeddings_Success 는 openai 프로바이더로 클라이언트가
// 키 부재 환경에서도 생성되는지 검증한다(SPEC §3).
func TestOpenAIEmbedding_InitEmbeddings_Success(t *testing.T) {
	client, err := llm.InitEmbeddings("openai:text-embedding-3-small")
	if err != nil {
		t.Fatalf("InitEmbeddings 가 에러를 반환함: %v", err)
	}
	if client == nil {
		t.Fatal("클라이언트가 nil 을 반환함")
	}
}

// TestOpenAIEmbedding_InitEmbeddings_WithAPIKeyOption 은 WithAPIKey 옵션이 키 없이도
// 생성에 영향을 주지 않는지(네트워크를 타지 않는지) 검증한다.
func TestOpenAIEmbedding_InitEmbeddings_WithAPIKeyOption(t *testing.T) {
	client, err := llm.InitEmbeddings("openai:text-embedding-3-small", llm.WithAPIKey("dummy-key"))
	if err != nil {
		t.Fatalf("InitEmbeddings 가 에러를 반환함: %v", err)
	}
	if client == nil {
		t.Fatal("클라이언트가 nil 을 반환함")
	}
}

// TestOpenAIEmbedding_Embed_Empty 는 빈 입력에 대해 빈 결과를 반환하는지 검증한다.
// 이 테스트는 네트워크 없이 실행 가능하다.
func TestOpenAIEmbedding_Embed_Empty(t *testing.T) {
	client, err := llm.InitEmbeddings("openai:text-embedding-3-small")
	if err != nil {
		t.Fatalf("InitEmbeddings 실패: %v", err)
	}

	result, err := client.Embed(context.Background(), []string{})
	if err != nil {
		t.Fatalf("빈 입력 Embed 에서 예상치 않은 에러: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("빈 입력 결과 개수 불일치: got %d, want 0", len(result))
	}
}

// ─── 라이브 스모크 테스트 (OPENAI_API_KEY 게이트, D-f) ───────────────────────
// skipIfNoOpenAIKey 는 openai_adapter_test.go 에 정의돼 있다(같은 패키지 공유).

// TestOpenAIEmbedding_LiveEmbed 는 실제 OpenAI API 를 호출해 1536차원 벡터를
// 입력 순서대로 반환하는지 검증한다(SPEC §5.2).
func TestOpenAIEmbedding_LiveEmbed(t *testing.T) {
	skipIfNoOpenAIKey(t)

	client, err := llm.InitEmbeddings("openai:text-embedding-3-small")
	if err != nil {
		t.Fatalf("InitEmbeddings 실패: %v", err)
	}

	texts := []string{
		"고양이는 독립적인 동물이다.",
		"개는 충성스러운 동물이다.",
		"Go 언어는 정적 타입 언어다.",
	}
	embeddings, err := client.Embed(context.Background(), texts)
	if err != nil {
		t.Fatalf("라이브 Embed 실패: %v", err)
	}
	if len(embeddings) != len(texts) {
		t.Fatalf("임베딩 수 불일치: got %d, want %d", len(embeddings), len(texts))
	}
	for i, vec := range embeddings {
		if len(vec) != 1536 {
			t.Errorf("texts[%d] 임베딩 차원 불일치: got %d, want 1536", i, len(vec))
		}
	}

	// 입력 순서 보존 검증: 같은 텍스트를 단독 임베딩한 벡터는 배치 결과 중
	// 같은 위치의 벡터와 가장 유사해야 한다.
	for i, text := range texts {
		q, err := client.EmbedQuery(context.Background(), text)
		if err != nil {
			t.Fatalf("EmbedQuery(texts[%d]) 실패: %v", i, err)
		}
		best, bestSim := -1, -2.0
		for j, vec := range embeddings {
			if sim := cosineSim(q, vec); sim > bestSim {
				best, bestSim = j, sim
			}
		}
		if best != i {
			t.Errorf("입력 순서 불일치: texts[%d] 단독 임베딩이 embeddings[%d]와 가장 유사(sim=%.4f)",
				i, best, bestSim)
		}
	}
}

// cosineSim 은 두 벡터의 코사인 유사도를 반환한다.
func cosineSim(a, b []float32) float64 {
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

// TestOpenAIEmbedding_LiveEmbedQuery 는 라이브 EmbedQuery 가 1536차원 벡터를 반환하는지 검증한다.
func TestOpenAIEmbedding_LiveEmbedQuery(t *testing.T) {
	skipIfNoOpenAIKey(t)

	client, err := llm.InitEmbeddings("openai:text-embedding-3-small")
	if err != nil {
		t.Fatalf("InitEmbeddings 실패: %v", err)
	}

	vec, err := client.EmbedQuery(context.Background(), "Go 언어의 특징은 무엇인가?")
	if err != nil {
		t.Fatalf("라이브 EmbedQuery 실패: %v", err)
	}
	if len(vec) != 1536 {
		t.Errorf("EmbedQuery 임베딩 차원 불일치: got %d, want 1536", len(vec))
	}
}
