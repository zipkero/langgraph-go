// vectorstore_test.go 는 InMemoryStore 의 색인·유사도 검색·Filter·K 제한을 검증한다.
// stub EmbeddingClient 로 결정적 벡터를 주입해 네트워크 없이 항상 실행한다(D6).
// 실제 Ollama 의존 검증은 task-007 e2e 가 담당한다.
package vectorstore_test

import (
	"context"
	"testing"

	"github.com/zipkero/langgraph-go/document"
	"github.com/zipkero/langgraph-go/vectorstore"
)

// stubEmbeddingClient 는 테스트용 결정적 벡터를 반환하는 EmbeddingClient 구현체다.
// 네트워크 없이 유사도·K·Filter 로직을 검증하기 위해 vectorstore_test 패키지 내부에 둔다(D6).
// 각 텍스트에 미리 등록된 벡터를 반환하며, 미등록 텍스트는 에러를 반환한다.
type stubEmbeddingClient struct {
	// vectors 는 텍스트 → 벡터 매핑이다.
	vectors map[string][]float32
}

func (s *stubEmbeddingClient) Embed(_ context.Context, texts []string) ([][]float32, error) {
	result := make([][]float32, len(texts))
	for i, text := range texts {
		v, ok := s.vectors[text]
		if !ok {
			// 미등록 텍스트는 영벡터를 반환한다.
			v = []float32{0, 0, 0}
		}
		result[i] = v
	}
	return result, nil
}

func (s *stubEmbeddingClient) EmbedQuery(_ context.Context, text string) ([]float32, error) {
	v, ok := s.vectors[text]
	if !ok {
		return []float32{0, 0, 0}, nil
	}
	return v, nil
}

// makeStub 은 텍스트→벡터 매핑을 지정해 stubEmbeddingClient 를 생성한다.
func makeStub(m map[string][]float32) *stubEmbeddingClient {
	return &stubEmbeddingClient{vectors: m}
}

// TestSimilaritySearch_ReturnsTopK 는 SimilaritySearch 가 유사도 내림차순으로 최대 K 개를
// 반환하는지 검증한다.
func TestSimilaritySearch_ReturnsTopK(t *testing.T) {
	// 벡터 정의: 질의와 가장 가까운 순서로 doc1 > doc2 > doc3 이 되도록 설계한다.
	// 코사인 유사도: (1,0,0)·(1,0,0)=1, (1,0,0)·(0.7,0.7,0)≈0.7, (1,0,0)·(0,1,0)=0
	stub := makeStub(map[string][]float32{
		"query":  {1, 0, 0},
		"doc1":   {1, 0, 0},
		"doc2":   {0.7, 0.7, 0},
		"doc3":   {0, 1, 0},
	})

	docs := []document.Document{
		{PageContent: "doc1", Metadata: map[string]any{"id": "1"}},
		{PageContent: "doc2", Metadata: map[string]any{"id": "2"}},
		{PageContent: "doc3", Metadata: map[string]any{"id": "3"}},
	}

	ctx := context.Background()
	store, err := vectorstore.FromDocuments(ctx, docs, stub)
	if err != nil {
		t.Fatalf("FromDocuments 실패: %v", err)
	}

	// K=2 로 검색하면 doc1, doc2 순으로 반환되어야 한다.
	results, err := store.SimilaritySearch(ctx, "query", vectorstore.SearchOptions{K: 2})
	if err != nil {
		t.Fatalf("SimilaritySearch 실패: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("결과 수 기대 2, 실제 %d", len(results))
	}
	if results[0].PageContent != "doc1" {
		t.Errorf("1위 기대 doc1, 실제 %q", results[0].PageContent)
	}
	if results[1].PageContent != "doc2" {
		t.Errorf("2위 기대 doc2, 실제 %q", results[1].PageContent)
	}
}

// TestSimilaritySearch_KLimit 는 K 가 결과를 정확히 제한하는지 검증한다.
func TestSimilaritySearch_KLimit(t *testing.T) {
	stub := makeStub(map[string][]float32{
		"query": {1, 0, 0},
		"doc1":  {1, 0, 0},
		"doc2":  {0.9, 0.1, 0},
		"doc3":  {0.5, 0.5, 0},
		"doc4":  {0, 1, 0},
		"doc5":  {0, 0, 1},
	})

	docs := []document.Document{
		{PageContent: "doc1"},
		{PageContent: "doc2"},
		{PageContent: "doc3"},
		{PageContent: "doc4"},
		{PageContent: "doc5"},
	}

	ctx := context.Background()
	store, err := vectorstore.FromDocuments(ctx, docs, stub)
	if err != nil {
		t.Fatalf("FromDocuments 실패: %v", err)
	}

	// K=3 일 때 정확히 3개만 반환되어야 한다.
	results, err := store.SimilaritySearch(ctx, "query", vectorstore.SearchOptions{K: 3})
	if err != nil {
		t.Fatalf("SimilaritySearch 실패: %v", err)
	}

	if len(results) != 3 {
		t.Fatalf("K=3 일 때 결과 수 기대 3, 실제 %d", len(results))
	}

	// K=0 이면 전체를 반환해야 한다.
	results, err = store.SimilaritySearch(ctx, "query", vectorstore.SearchOptions{K: 0})
	if err != nil {
		t.Fatalf("K=0 SimilaritySearch 실패: %v", err)
	}

	if len(results) != 5 {
		t.Fatalf("K=0 일 때 결과 수 기대 5, 실제 %d", len(results))
	}
}

// TestSimilaritySearch_Filter 는 Filter 가 Metadata 일치 항목만 검색 결과에 포함하는지 검증한다.
func TestSimilaritySearch_Filter(t *testing.T) {
	stub := makeStub(map[string][]float32{
		"query":    {1, 0, 0},
		"cat-doc1": {1, 0, 0},   // 카테고리 A, 질의와 가장 유사
		"cat-doc2": {0.9, 0.1, 0}, // 카테고리 B
		"cat-doc3": {0.8, 0.2, 0}, // 카테고리 A
		"cat-doc4": {0.7, 0.3, 0}, // 카테고리 B
	})

	docs := []document.Document{
		{PageContent: "cat-doc1", Metadata: map[string]any{"category": "A"}},
		{PageContent: "cat-doc2", Metadata: map[string]any{"category": "B"}},
		{PageContent: "cat-doc3", Metadata: map[string]any{"category": "A"}},
		{PageContent: "cat-doc4", Metadata: map[string]any{"category": "B"}},
	}

	ctx := context.Background()
	store, err := vectorstore.FromDocuments(ctx, docs, stub)
	if err != nil {
		t.Fatalf("FromDocuments 실패: %v", err)
	}

	// Filter: category=A 인 항목만 포함해야 한다.
	results, err := store.SimilaritySearch(ctx, "query", vectorstore.SearchOptions{
		K:      10,
		Filter: map[string]any{"category": "A"},
	})
	if err != nil {
		t.Fatalf("SimilaritySearch with Filter 실패: %v", err)
	}

	// 카테고리 A 항목은 cat-doc1, cat-doc3 두 개여야 한다.
	if len(results) != 2 {
		t.Fatalf("Filter=A 결과 수 기대 2, 실제 %d", len(results))
	}
	for _, doc := range results {
		if doc.Metadata["category"] != "A" {
			t.Errorf("Filter 위반: 카테고리 A 가 아닌 항목 %q 가 포함됨", doc.PageContent)
		}
	}

	// Filter 로 줄어든 결과에서도 유사도 순서가 유지되어야 한다(cat-doc1 > cat-doc3).
	if results[0].PageContent != "cat-doc1" {
		t.Errorf("Filter 후 1위 기대 cat-doc1, 실제 %q", results[0].PageContent)
	}
	if results[1].PageContent != "cat-doc3" {
		t.Errorf("Filter 후 2위 기대 cat-doc3, 실제 %q", results[1].PageContent)
	}
}

// TestSimilaritySearch_FilterAndK 는 Filter 와 K 가 함께 적용될 때의 동작을 검증한다.
func TestSimilaritySearch_FilterAndK(t *testing.T) {
	stub := makeStub(map[string][]float32{
		"query": {1, 0, 0},
		"a1":    {1, 0, 0},
		"a2":    {0.9, 0.1, 0},
		"a3":    {0.8, 0.2, 0},
		"b1":    {0.7, 0.3, 0},
	})

	docs := []document.Document{
		{PageContent: "a1", Metadata: map[string]any{"cat": "A"}},
		{PageContent: "a2", Metadata: map[string]any{"cat": "A"}},
		{PageContent: "a3", Metadata: map[string]any{"cat": "A"}},
		{PageContent: "b1", Metadata: map[string]any{"cat": "B"}},
	}

	ctx := context.Background()
	store, err := vectorstore.FromDocuments(ctx, docs, stub)
	if err != nil {
		t.Fatalf("FromDocuments 실패: %v", err)
	}

	// Filter=A, K=2 → A 카테고리 3개 중 상위 2개
	results, err := store.SimilaritySearch(ctx, "query", vectorstore.SearchOptions{
		K:      2,
		Filter: map[string]any{"cat": "A"},
	})
	if err != nil {
		t.Fatalf("SimilaritySearch Filter+K 실패: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("Filter+K=2 결과 수 기대 2, 실제 %d", len(results))
	}
	if results[0].PageContent != "a1" {
		t.Errorf("1위 기대 a1, 실제 %q", results[0].PageContent)
	}
	if results[1].PageContent != "a2" {
		t.Errorf("2위 기대 a2, 실제 %q", results[1].PageContent)
	}
}

// TestFromDocuments_Empty 는 빈 docs 로 FromDocuments 를 호출했을 때 스토어가 정상 생성되는지 검증한다.
func TestFromDocuments_Empty(t *testing.T) {
	stub := makeStub(map[string][]float32{})
	ctx := context.Background()

	store, err := vectorstore.FromDocuments(ctx, []document.Document{}, stub)
	if err != nil {
		t.Fatalf("빈 docs FromDocuments 실패: %v", err)
	}

	results, err := store.SimilaritySearch(ctx, "query", vectorstore.SearchOptions{K: 5})
	if err != nil {
		t.Fatalf("빈 스토어 SimilaritySearch 실패: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("빈 스토어 결과 수 기대 0, 실제 %d", len(results))
	}
}

// TestAdd_Incremental 은 Add 를 여러 번 호출해 점진적으로 색인이 누적되는지 검증한다.
func TestAdd_Incremental(t *testing.T) {
	stub := makeStub(map[string][]float32{
		"query": {1, 0, 0},
		"first": {1, 0, 0},
		"later": {0.9, 0.1, 0},
	})

	ctx := context.Background()
	store, err := vectorstore.FromDocuments(ctx, []document.Document{
		{PageContent: "first"},
	}, stub)
	if err != nil {
		t.Fatalf("FromDocuments 실패: %v", err)
	}

	// Add 로 추가 문서를 색인한다.
	if err := store.Add(ctx, []document.Document{{PageContent: "later"}}); err != nil {
		t.Fatalf("Add 실패: %v", err)
	}

	results, err := store.SimilaritySearch(ctx, "query", vectorstore.SearchOptions{K: 2})
	if err != nil {
		t.Fatalf("SimilaritySearch 실패: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("누적 색인 결과 수 기대 2, 실제 %d", len(results))
	}
}

// TestRetriever_InvokeMatchesSimilaritySearch 는 AsRetriever 로 생성한 Retriever 의 Invoke 가
// 동일 질의·SearchOptions 의 SimilaritySearch 결과와 완전히 일치하는지 검증한다(task-005).
// stub EmbeddingClient 로 결정적 벡터를 주입해 네트워크 없이 항상 실행한다.
func TestRetriever_InvokeMatchesSimilaritySearch(t *testing.T) {
	// 유사도: doc1(1.0) > doc2(≈0.7) > doc3(0.0)
	stub := makeStub(map[string][]float32{
		"query": {1, 0, 0},
		"doc1":  {1, 0, 0},
		"doc2":  {0.7, 0.7, 0},
		"doc3":  {0, 1, 0},
	})

	docs := []document.Document{
		{PageContent: "doc1", Metadata: map[string]any{"rank": "1"}},
		{PageContent: "doc2", Metadata: map[string]any{"rank": "2"}},
		{PageContent: "doc3", Metadata: map[string]any{"rank": "3"}},
	}

	ctx := context.Background()
	store, err := vectorstore.FromDocuments(ctx, docs, stub)
	if err != nil {
		t.Fatalf("FromDocuments 실패: %v", err)
	}

	opts := vectorstore.SearchOptions{K: 2}
	retriever := store.AsRetriever(opts)

	// Invoke 결과를 수집한다.
	invokeResults, err := retriever.Invoke(ctx, "query")
	if err != nil {
		t.Fatalf("Retriever.Invoke 실패: %v", err)
	}

	// SimilaritySearch 결과를 수집한다.
	searchResults, err := store.SimilaritySearch(ctx, "query", opts)
	if err != nil {
		t.Fatalf("SimilaritySearch 실패: %v", err)
	}

	// 결과 길이가 일치해야 한다.
	if len(invokeResults) != len(searchResults) {
		t.Fatalf("결과 수 불일치: Invoke=%d, SimilaritySearch=%d", len(invokeResults), len(searchResults))
	}

	// 각 위치의 PageContent 가 동일해야 한다.
	for i := range invokeResults {
		if invokeResults[i].PageContent != searchResults[i].PageContent {
			t.Errorf("순위 %d PageContent 불일치: Invoke=%q, SimilaritySearch=%q",
				i+1, invokeResults[i].PageContent, searchResults[i].PageContent)
		}
	}

	// 기대 문서(doc1, doc2)가 Invoke 결과에 포함되어야 한다.
	if len(invokeResults) < 2 {
		t.Fatalf("기대 결과 수 2 이상, 실제 %d", len(invokeResults))
	}
	if invokeResults[0].PageContent != "doc1" {
		t.Errorf("1위 기대 doc1, 실제 %q", invokeResults[0].PageContent)
	}
	if invokeResults[1].PageContent != "doc2" {
		t.Errorf("2위 기대 doc2, 실제 %q", invokeResults[1].PageContent)
	}
}

// TestRetriever_InvokeWithFilter 는 AsRetriever 에 Filter 가 포함된 SearchOptions 를 지정했을 때
// Invoke 가 Filter 를 준수하는지 검증한다(task-005).
func TestRetriever_InvokeWithFilter(t *testing.T) {
	stub := makeStub(map[string][]float32{
		"query": {1, 0, 0},
		"alpha": {1, 0, 0},   // 카테고리 X, 질의와 가장 유사
		"beta":  {0.9, 0.1, 0}, // 카테고리 Y
		"gamma": {0.8, 0.2, 0}, // 카테고리 X
	})

	docs := []document.Document{
		{PageContent: "alpha", Metadata: map[string]any{"cat": "X"}},
		{PageContent: "beta", Metadata: map[string]any{"cat": "Y"}},
		{PageContent: "gamma", Metadata: map[string]any{"cat": "X"}},
	}

	ctx := context.Background()
	store, err := vectorstore.FromDocuments(ctx, docs, stub)
	if err != nil {
		t.Fatalf("FromDocuments 실패: %v", err)
	}

	opts := vectorstore.SearchOptions{K: 5, Filter: map[string]any{"cat": "X"}}
	retriever := store.AsRetriever(opts)

	invokeResults, err := retriever.Invoke(ctx, "query")
	if err != nil {
		t.Fatalf("Retriever.Invoke(Filter) 실패: %v", err)
	}

	// X 카테고리 문서만 포함되어야 한다.
	if len(invokeResults) != 2 {
		t.Fatalf("Filter=X 결과 수 기대 2, 실제 %d", len(invokeResults))
	}
	for _, doc := range invokeResults {
		if doc.Metadata["cat"] != "X" {
			t.Errorf("Filter 위반: cat=X 가 아닌 항목 %q 가 포함됨", doc.PageContent)
		}
	}

	// 유사도 순서도 유지되어야 한다(alpha > gamma).
	if invokeResults[0].PageContent != "alpha" {
		t.Errorf("1위 기대 alpha, 실제 %q", invokeResults[0].PageContent)
	}
}

// TestSimilaritySearch_OrderConsistency 는 동일 유사도 점수가 아닌 경우 순위가
// 일관되게 내림차순임을 검증한다.
func TestSimilaritySearch_OrderConsistency(t *testing.T) {
	// 유사도: doc_a(1.0) > doc_b(0.6) > doc_c(0.0)
	stub := makeStub(map[string][]float32{
		"q":     {1, 0},
		"doc_a": {1, 0},
		"doc_b": {0.6, 0.8},
		"doc_c": {0, 1},
	})

	docs := []document.Document{
		// 의도적으로 유사도가 낮은 순서로 추가해 정렬이 올바른지 확인한다.
		{PageContent: "doc_c"},
		{PageContent: "doc_b"},
		{PageContent: "doc_a"},
	}

	ctx := context.Background()
	store, err := vectorstore.FromDocuments(ctx, docs, stub)
	if err != nil {
		t.Fatalf("FromDocuments 실패: %v", err)
	}

	results, err := store.SimilaritySearch(ctx, "q", vectorstore.SearchOptions{K: 3})
	if err != nil {
		t.Fatalf("SimilaritySearch 실패: %v", err)
	}

	order := []string{"doc_a", "doc_b", "doc_c"}
	for i, expected := range order {
		if results[i].PageContent != expected {
			t.Errorf("순위 %d 기대 %q, 실제 %q", i+1, expected, results[i].PageContent)
		}
	}
}
