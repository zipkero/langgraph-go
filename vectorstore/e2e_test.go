// e2e_test.go 는 적재→분할→임베딩→저장→검색 end-to-end 통합 테스트를 담는다(task-007).
// 실제 Ollama 서버와 nomic-embed-text 임베딩 모델로 문서를 색인하고,
// 의미상 가까운 질의가 관련 청크를 상위 결과로 가져오는지 검증한다.
//
// 검증 범위:
//   - 고정 문서 집합 분할 → FromDocuments 색인 → SimilaritySearch 의미 검색 (TestE2E_IndexAndSearch)
//   - AsRetriever/Invoke 경로로 동일 의미 검색 검증 (TestE2E_RetrieverInvoke)
//   - CreateRetrieverTool 도구 실행 결과 검증 (TestE2E_RetrieverTool)
//
// Ollama 서버 미도달 또는 nomic-embed-text 미설치 시 t.Skip 으로 건너뛴다(D6).
// Ollama 가용 환경에서만 실제 통과가 보장되며, CI 에서는 빌드·정적검사만 실행된다.
package vectorstore_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/zipkero/langgraph-go/document"
	"github.com/zipkero/langgraph-go/llm"
	"github.com/zipkero/langgraph-go/vectorstore"
)

// e2eOllamaModel 은 e2e 테스트에서 사용하는 임베딩 모델 이름이다(D7).
const e2eOllamaModel = "nomic-embed-text"

// checkOllamaEmbedReady 는 Ollama 서버가 도달 가능하고 임베딩 요청을 처리할 수 있는지 확인한다.
// 서버 미실행·모델 미설치(404·500) 모두 false 를 반환해 t.Skip 을 유발한다(D6).
// embedding_test.go 의 ollamaEmbedReady 와 같은 패턴이나, 패키지 경계가 다르므로 별도 정의한다.
func checkOllamaEmbedReady(model string) bool {
	reqBody := `{"model":"` + model + `","input":["ping"]}`
	httpClient := &http.Client{Timeout: 3 * time.Second}
	resp, err := httpClient.Post(
		"http://localhost:11434/api/embed",
		"application/json",
		bytes.NewBufferString(reqBody),
	)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// skipIfOllamaUnavailable 은 Ollama 서버가 준비되지 않으면 t.Skip 을 호출한다.
// 모든 e2e 테스트 함수 첫 줄에 호출해 일관된 skip 처리를 보장한다(D6).
func skipIfOllamaUnavailable(t *testing.T) {
	t.Helper()
	if !checkOllamaEmbedReady(e2eOllamaModel) {
		t.Skipf(
			"Ollama 서버에 도달할 수 없거나 모델 %q 가 설치되지 않아 e2e 테스트를 건너뜁니다",
			e2eOllamaModel,
		)
	}
}

// e2eEmbeddingClient 는 e2e 테스트에서 공통으로 사용하는 EmbeddingClient 를 반환한다.
func e2eEmbeddingClient(t *testing.T) llm.EmbeddingClient {
	t.Helper()
	emb, err := llm.InitEmbeddings("ollama:" + e2eOllamaModel)
	if err != nil {
		t.Fatalf("InitEmbeddings 실패: %v", err)
	}
	return emb
}

// e2eFixedDocs 는 서로 다른 주제의 고정 문서 집합이다.
// 동물 주제(topic=animal)와 프로그래밍 주제(topic=programming) 두 그룹으로 나뉜다.
// 의미 검색 검증: 동물 질의 → animal 청크 상위, 프로그래밍 질의 → programming 청크 상위.
var e2eFixedDocs = []document.Document{
	{
		PageContent: "고양이는 독립적인 성격을 가진 포유류다. 조용하고 청결하며 사냥 본능이 강하다.",
		Metadata:    map[string]any{"topic": "animal", "id": "cat"},
	},
	{
		PageContent: "개는 충성스럽고 사교적인 동물이다. 인간과 오랜 공생 관계를 유지해 왔다.",
		Metadata:    map[string]any{"topic": "animal", "id": "dog"},
	},
	{
		PageContent: "Go 언어는 구글이 설계한 정적 타입 컴파일 언어다. 간결한 문법과 빠른 컴파일이 특징이다.",
		Metadata:    map[string]any{"topic": "programming", "id": "go"},
	},
	{
		PageContent: "Python 은 동적 타입 인터프리터 언어로 데이터 과학과 머신러닝 분야에서 널리 쓰인다.",
		Metadata:    map[string]any{"topic": "programming", "id": "python"},
	},
}

// ============================================================
// TestE2E_IndexAndSearch
//
// 검증:
//   - 고정 문서 집합을 RecursiveCharacterSplitter 로 분할하고 FromDocuments 로 색인한다.
//   - 동물 주제 질의("고양이와 개 같은 애완동물")가 animal 청크를 상위에 반환한다.
//   - 프로그래밍 주제 질의("컴파일 언어와 인터프리터 언어 비교")가 programming 청크를 상위에 반환한다.
// ============================================================

func TestE2E_IndexAndSearch(t *testing.T) {
	skipIfOllamaUnavailable(t)

	ctx := context.Background()
	emb := e2eEmbeddingClient(t)

	// 문서 분할: chunkSize=200, overlap=20 으로 재귀 분할한다.
	splitter := document.NewRecursiveCharacterSplitter(200, 20)
	chunks := splitter.SplitDocuments(e2eFixedDocs)
	if len(chunks) == 0 {
		t.Fatal("분할 결과 청크가 없습니다")
	}

	// FromDocuments 로 색인한다.
	store, err := vectorstore.FromDocuments(ctx, chunks, emb)
	if err != nil {
		t.Fatalf("FromDocuments 색인 실패: %v", err)
	}

	// ── 동물 주제 질의 검증 ──────────────────────────────────────────────────
	animalQuery := "고양이와 개 같은 반려동물의 특성"
	animalResults, err := store.SimilaritySearch(ctx, animalQuery, vectorstore.SearchOptions{K: 2})
	if err != nil {
		t.Fatalf("동물 주제 SimilaritySearch 실패: %v", err)
	}
	if len(animalResults) == 0 {
		t.Fatal("동물 주제 검색 결과가 없습니다")
	}

	// 상위 1위가 animal 주제 청크여야 한다.
	top1Topic, _ := animalResults[0].Metadata["topic"].(string)
	if top1Topic != "animal" {
		t.Errorf(
			"동물 질의 상위 1위 topic=%q, animal 기대\nPageContent=%q",
			top1Topic, animalResults[0].PageContent,
		)
	}

	// ── 프로그래밍 주제 질의 검증 ────────────────────────────────────────────
	progQuery := "정적 타입 컴파일 언어와 프로그래밍"
	progResults, err := store.SimilaritySearch(ctx, progQuery, vectorstore.SearchOptions{K: 2})
	if err != nil {
		t.Fatalf("프로그래밍 주제 SimilaritySearch 실패: %v", err)
	}
	if len(progResults) == 0 {
		t.Fatal("프로그래밍 주제 검색 결과가 없습니다")
	}

	// 상위 1위가 programming 주제 청크여야 한다.
	top1ProgTopic, _ := progResults[0].Metadata["topic"].(string)
	if top1ProgTopic != "programming" {
		t.Errorf(
			"프로그래밍 질의 상위 1위 topic=%q, programming 기대\nPageContent=%q",
			top1ProgTopic, progResults[0].PageContent,
		)
	}
}

// ============================================================
// TestE2E_RetrieverInvoke
//
// 검증:
//   - AsRetriever/Invoke 경로가 SimilaritySearch 와 동일한 의미 검색을 수행한다.
//   - 동물 질의로 Invoke 한 결과 상위 1위가 animal 주제 청크다.
// ============================================================

func TestE2E_RetrieverInvoke(t *testing.T) {
	skipIfOllamaUnavailable(t)

	ctx := context.Background()
	emb := e2eEmbeddingClient(t)

	splitter := document.NewRecursiveCharacterSplitter(200, 20)
	chunks := splitter.SplitDocuments(e2eFixedDocs)

	store, err := vectorstore.FromDocuments(ctx, chunks, emb)
	if err != nil {
		t.Fatalf("FromDocuments 색인 실패: %v", err)
	}

	// AsRetriever 로 K=2 고정 Retriever 를 생성한다.
	retriever := store.AsRetriever(vectorstore.SearchOptions{K: 2})

	animalQuery := "포유류 동물의 습성과 특징"
	invokeResults, err := retriever.Invoke(ctx, animalQuery)
	if err != nil {
		t.Fatalf("Retriever.Invoke 실패: %v", err)
	}
	if len(invokeResults) == 0 {
		t.Fatal("Retriever.Invoke 결과가 없습니다")
	}

	// 동일 질의로 SimilaritySearch 결과와 비교한다.
	searchResults, err := store.SimilaritySearch(ctx, animalQuery, vectorstore.SearchOptions{K: 2})
	if err != nil {
		t.Fatalf("SimilaritySearch 실패: %v", err)
	}

	// Invoke 와 SimilaritySearch 결과 수가 일치해야 한다.
	if len(invokeResults) != len(searchResults) {
		t.Errorf("결과 수 불일치: Invoke=%d, SimilaritySearch=%d", len(invokeResults), len(searchResults))
	}

	// 상위 1위가 animal 청크여야 한다.
	top1Topic, _ := invokeResults[0].Metadata["topic"].(string)
	if top1Topic != "animal" {
		t.Errorf(
			"Retriever.Invoke 상위 1위 topic=%q, animal 기대\nPageContent=%q",
			top1Topic, invokeResults[0].PageContent,
		)
	}

	// Invoke 와 SimilaritySearch 의 순위가 일치해야 한다.
	for i := range invokeResults {
		if i >= len(searchResults) {
			break
		}
		if invokeResults[i].PageContent != searchResults[i].PageContent {
			t.Errorf(
				"순위 %d 불일치: Invoke=%q, SimilaritySearch=%q",
				i+1, invokeResults[i].PageContent, searchResults[i].PageContent,
			)
		}
	}
}

// ============================================================
// TestE2E_RetrieverTool
//
// 검증:
//   - CreateRetrieverTool 이 반환한 도구를 실행하면 질의 관련 청크 텍스트가 Result.Content 에 담긴다.
//   - 프로그래밍 질의로 도구를 실행해 programming 주제 청크가 Content 에 포함되는지 확인한다.
// ============================================================

func TestE2E_RetrieverTool(t *testing.T) {
	skipIfOllamaUnavailable(t)

	ctx := context.Background()
	emb := e2eEmbeddingClient(t)

	splitter := document.NewRecursiveCharacterSplitter(200, 20)
	chunks := splitter.SplitDocuments(e2eFixedDocs)

	store, err := vectorstore.FromDocuments(ctx, chunks, emb)
	if err != nil {
		t.Fatalf("FromDocuments 색인 실패: %v", err)
	}

	// K=2 로 고정한 Retriever 를 도구로 감싼다.
	retriever := store.AsRetriever(vectorstore.SearchOptions{K: 2})
	toolInst := vectorstore.CreateRetrieverTool(retriever, "doc_search", "문서에서 관련 내용을 검색한다")

	// 도구 메타 확인
	if toolInst.Name() != "doc_search" {
		t.Errorf("도구 이름=%q, doc_search 기대", toolInst.Name())
	}
	if toolInst.Description() != "문서에서 관련 내용을 검색한다" {
		t.Errorf("도구 설명=%q, 기대값과 다름", toolInst.Description())
	}

	// 프로그래밍 질의로 Execute 를 호출한다.
	argsJSON, err := json.Marshal(map[string]string{"query": "Go 언어 컴파일 특징"})
	if err != nil {
		t.Fatalf("인자 직렬화 실패: %v", err)
	}

	result, err := toolInst.Execute(ctx, argsJSON, nil)
	if err != nil {
		t.Fatalf("도구 Execute 실패: %v", err)
	}
	if result.IsError {
		t.Fatalf("도구 실행 오류: %s", result.Content)
	}
	if result.Content == "" {
		t.Fatal("도구 Execute 결과 Content 가 비어 있습니다")
	}

	// Content 에 Go 언어 관련 텍스트가 포함되어야 한다.
	// 정확한 청크 내용 대신 "Go" 키워드 포함 여부로 느슨하게 검증한다.
	if !strings.Contains(result.Content, "Go") && !strings.Contains(result.Content, "Python") {
		t.Errorf(
			"도구 Execute Content 에 프로그래밍 관련 텍스트가 없습니다\nContent=%q",
			result.Content,
		)
	}
}
