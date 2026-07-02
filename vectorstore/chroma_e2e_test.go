// chroma_e2e_test.go 는 실제 Chroma 서버(chromadb 1.x)를 백엔드로 하는 ChromaVectorStore 의
// 추가→유사도 검색→retriever 왕복 end-to-end 테스트를 담는다(task-004).
//
// Chroma 서버 heartbeat 미도달 시 t.Skip 으로 건너뛴다(ANALYSIS §5 D-f). 임베딩은 기존
// e2e_test.go 의 Ollama 클라이언트(e2eEmbeddingClient)를 재사용하므로 Ollama 미가용 시에도 skip 된다.
package vectorstore_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/zipkero/langgraph-go/vectorstore"
)

// chromaE2EBaseURL 은 e2e 테스트가 접속할 Chroma 서버의 기본 베이스 URL이다(로컬 Docker 기본 포트).
const chromaE2EBaseURL = "http://localhost:8000"

// checkChromaAvailable 은 Chroma 서버의 v2 heartbeat 엔드포인트가 응답하는지 확인한다.
// 서버 미기동·경로 미도달 모두 false 를 반환해 t.Skip 을 유발한다(ANALYSIS §5 D-f).
func checkChromaAvailable(baseURL string) bool {
	httpClient := &http.Client{Timeout: 3 * time.Second}
	resp, err := httpClient.Get(baseURL + "/api/v2/heartbeat")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// skipIfChromaUnavailable 은 Chroma 서버가 준비되지 않으면 t.Skip 을 호출한다.
func skipIfChromaUnavailable(t *testing.T) {
	t.Helper()
	if !checkChromaAvailable(chromaE2EBaseURL) {
		t.Skipf("Chroma 서버(%s)에 도달할 수 없어 e2e 테스트를 건너뜁니다", chromaE2EBaseURL)
	}
}

// chromaE2ECollectionName 은 실행마다 새 임의 이름을 생성해 반복 실행 간 collection 상태가
// 섞이지 않도록 한다(Chroma 서버가 collection 을 영속 보관하는 전제).
func chromaE2ECollectionName(t *testing.T) string {
	t.Helper()
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		t.Fatalf("collection 이름 임의값 생성 실패: %v", err)
	}
	return "langgraph_go_e2e_" + hex.EncodeToString(buf)
}

// TestChromaE2E_AddAndSearch 는 문서 추가 후 SimilaritySearch 가 의미상 가까운 청크를
// 상위 결과로 반환하는지 검증한다.
func TestChromaE2E_AddAndSearch(t *testing.T) {
	skipIfChromaUnavailable(t)
	skipIfOllamaUnavailable(t)

	ctx := context.Background()
	emb := e2eEmbeddingClient(t)
	store := vectorstore.NewChromaVectorStore(chromaE2EBaseURL, chromaE2ECollectionName(t), emb)

	if err := store.Add(ctx, e2eFixedDocs); err != nil {
		t.Fatalf("Add 실패: %v", err)
	}

	results, err := store.SimilaritySearch(ctx, "고양이와 개 같은 반려동물의 특성", vectorstore.SearchOptions{K: 2})
	if err != nil {
		t.Fatalf("SimilaritySearch 실패: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("검색 결과가 없습니다")
	}

	top1Topic, _ := results[0].Metadata["topic"].(string)
	if top1Topic != "animal" {
		t.Errorf("상위 1위 topic=%q, animal 기대\nPageContent=%q", top1Topic, results[0].PageContent)
	}
}

// TestChromaE2E_RetrieverInvoke 는 AsRetriever/Invoke 경로가 SimilaritySearch 와 동일한
// 의미 검색을 수행하는지 검증한다.
func TestChromaE2E_RetrieverInvoke(t *testing.T) {
	skipIfChromaUnavailable(t)
	skipIfOllamaUnavailable(t)

	ctx := context.Background()
	emb := e2eEmbeddingClient(t)
	store := vectorstore.NewChromaVectorStore(chromaE2EBaseURL, chromaE2ECollectionName(t), emb)

	if err := store.Add(ctx, e2eFixedDocs); err != nil {
		t.Fatalf("Add 실패: %v", err)
	}

	retriever := store.AsRetriever(vectorstore.SearchOptions{K: 2})
	results, err := retriever.Invoke(ctx, "정적 타입 컴파일 언어와 프로그래밍")
	if err != nil {
		t.Fatalf("Retriever.Invoke 실패: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("Invoke 결과가 없습니다")
	}

	top1Topic, _ := results[0].Metadata["topic"].(string)
	if top1Topic != "programming" {
		t.Errorf("상위 1위 topic=%q, programming 기대\nPageContent=%q", top1Topic, results[0].PageContent)
	}
}

// TestChromaE2E_RetrieverTool 은 CreateRetrieverTool 이 ChromaVectorStore 의 Retriever 를
// 그대로 받아 도구로 동작하는지 검증한다(SPEC §5.3 — 기존 계약과 동일한 형태의 결과).
func TestChromaE2E_RetrieverTool(t *testing.T) {
	skipIfChromaUnavailable(t)
	skipIfOllamaUnavailable(t)

	ctx := context.Background()
	emb := e2eEmbeddingClient(t)
	store := vectorstore.NewChromaVectorStore(chromaE2EBaseURL, chromaE2ECollectionName(t), emb)

	if err := store.Add(ctx, e2eFixedDocs); err != nil {
		t.Fatalf("Add 실패: %v", err)
	}

	retriever := store.AsRetriever(vectorstore.SearchOptions{K: 2})
	toolInst := vectorstore.CreateRetrieverTool(retriever, "chroma_doc_search", "Chroma 문서에서 관련 내용을 검색한다")

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
}
