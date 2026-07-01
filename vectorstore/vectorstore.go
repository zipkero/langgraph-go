// vectorstore 패키지는 벡터 저장·유사도 검색 추상화를 담당한다.
// Store/Retriever 인터페이스, SearchOptions, InMemoryStore 구현체,
// FromDocuments 생성 경로가 여기에 있다.
// document(document.Document)와 llm(llm.EmbeddingClient)에 의존하며, 역참조는 만들지 않는다(SPEC §5.9).
package vectorstore

import (
	"context"
	"fmt"
	"math"
	"sort"
	"sync"

	"github.com/zipkero/langgraph-go/document"
	"github.com/zipkero/langgraph-go/llm"
)

// SearchOptions 는 유사도 검색 옵션을 담는 구조체다.
type SearchOptions struct {
	// K 는 반환할 최대 결과 수다. 0이면 저장된 모든 결과를 반환한다.
	K int
	// Filter 는 메타데이터 필터다. 지정하면 Metadata에 이 항목이 모두 포함된 Document 만 결과에 포함한다.
	Filter map[string]any
}

// Store 는 벡터 저장·유사도 검색의 계약 인터페이스다.
// InMemoryStore 가 이를 구현하며, 후속 백엔드 확장 자리를 남긴다(D3).
type Store interface {
	// Add 는 Document 목록을 임베딩해 스토어에 색인한다.
	Add(ctx context.Context, docs []document.Document) error

	// SimilaritySearch 는 질의 텍스트와 가장 유사한 Document 를 최대 opts.K 개 반환한다.
	// opts.Filter 가 지정되면 해당 Metadata 항목을 모두 포함하는 Document 만 검색 대상으로 삼는다.
	SimilaritySearch(ctx context.Context, query string, opts SearchOptions) ([]document.Document, error)

	// AsRetriever 는 SearchOptions 를 고정한 Retriever 를 반환한다.
	AsRetriever(opts SearchOptions) Retriever
}

// Retriever 는 고정된 SearchOptions 로 유사도 검색을 실행하는 계약 인터페이스다.
// AsRetriever 로 생성하며, Invoke 는 동일 검색 경로(SimilaritySearch)를 호출한다(SPEC §5.6).
type Retriever interface {
	// Invoke 는 질의 텍스트로 고정된 SearchOptions 에 따라 검색을 실행하고
	// 결과 Document 목록을 반환한다.
	Invoke(ctx context.Context, query string) ([]document.Document, error)
}

// storeEntry 는 InMemoryStore 가 보관하는 단일 색인 항목이다.
type storeEntry struct {
	// doc 은 원본 Document 다.
	doc document.Document
	// vector 는 doc.PageContent 의 임베딩 벡터다.
	vector []float32
}

// InMemoryStore 는 벡터와 Document 를 메모리에 보관하는 Store 구현체다.
// 색인 시 EmbeddingClient.Embed 로 벡터를 생성하고,
// 검색 시 EmbedQuery 로 질의 벡터를 생성해 코사인 유사도 기준 상위 K 개를 반환한다(SPEC §5.5, D3).
type InMemoryStore struct {
	// emb 는 임베딩 생성에 사용할 클라이언트다.
	emb llm.EmbeddingClient
	// mu 는 entries 를 보호하는 뮤텍스다.
	mu sync.RWMutex
	// entries 는 색인된 항목 목록이다.
	entries []storeEntry
}

// newInMemoryStore 는 빈 InMemoryStore 를 생성한다.
func newInMemoryStore(emb llm.EmbeddingClient) *InMemoryStore {
	return &InMemoryStore{
		emb: emb,
	}
}

// Add 는 docs 의 PageContent 를 Embed 로 임베딩하고 벡터·Document 를 스토어에 보관한다.
// 빈 docs 는 아무 작업도 하지 않고 nil 을 반환한다.
func (s *InMemoryStore) Add(ctx context.Context, docs []document.Document) error {
	if len(docs) == 0 {
		return nil
	}

	// PageContent 를 일괄 수집해 한 번의 Embed 호출로 임베딩한다.
	texts := make([]string, len(docs))
	for i, doc := range docs {
		texts[i] = doc.PageContent
	}

	vectors, err := s.emb.Embed(ctx, texts)
	if err != nil {
		return err
	}

	if len(vectors) != len(docs) {
		return errorMismatch(len(docs), len(vectors))
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for i, doc := range docs {
		s.entries = append(s.entries, storeEntry{
			doc:    doc,
			vector: vectors[i],
		})
	}

	return nil
}

// SimilaritySearch 는 query 를 EmbedQuery 로 임베딩한 뒤 보관된 벡터와 코사인 유사도를 계산해
// 상위 opts.K 개의 Document 를 반환한다.
// opts.Filter 가 지정되면 Metadata 에 해당 key-value 를 모두 포함하는 항목만 검색 대상으로 삼는다.
func (s *InMemoryStore) SimilaritySearch(ctx context.Context, query string, opts SearchOptions) ([]document.Document, error) {
	// 질의 벡터 생성
	queryVec, err := s.emb.EmbedQuery(ctx, query)
	if err != nil {
		return nil, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	// 유사도 점수 계산 및 Filter 적용
	type scored struct {
		doc   document.Document
		score float32
	}

	var candidates []scored
	for _, entry := range s.entries {
		// Filter 가 지정된 경우 Metadata 에 모든 key-value 가 포함되어야 한다.
		if !matchesFilter(entry.doc.Metadata, opts.Filter) {
			continue
		}
		sim := cosineSimilarity(queryVec, entry.vector)
		candidates = append(candidates, scored{doc: entry.doc, score: sim})
	}

	// 유사도 내림차순 정렬
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})

	// K 제한 적용
	k := opts.K
	if k <= 0 || k > len(candidates) {
		k = len(candidates)
	}

	result := make([]document.Document, k)
	for i := 0; i < k; i++ {
		result[i] = candidates[i].doc
	}

	return result, nil
}

// AsRetriever 는 opts 를 고정한 retriever 를 반환한다.
// Invoke 는 같은 SimilaritySearch 경로를 그 옵션으로 호출한다(SPEC §5.6).
func (s *InMemoryStore) AsRetriever(opts SearchOptions) Retriever {
	return &inMemoryRetriever{
		store: s,
		opts:  opts,
	}
}

// inMemoryRetriever 는 InMemoryStore 를 고정된 SearchOptions 로 감싼 Retriever 구현체다.
type inMemoryRetriever struct {
	// store 는 검색을 위임할 InMemoryStore 다.
	store *InMemoryStore
	// opts 는 고정된 검색 옵션이다.
	opts SearchOptions
}

// Invoke 는 고정된 SearchOptions 로 SimilaritySearch 를 실행하고 결과 Document 목록을 반환한다.
func (r *inMemoryRetriever) Invoke(ctx context.Context, query string) ([]document.Document, error) {
	return r.store.SimilaritySearch(ctx, query, r.opts)
}

// StoreOption 은 FromDocuments 의 스토어 생성 옵션 타입이다.
type StoreOption func(*storeOptions)

// storeOptions 는 스토어 생성 옵션을 담는 내부 타입이다.
type storeOptions struct {
	// 현재는 확장 자리만 남긴다 — 후속 옵션(청크 사이즈 재분할 등)이 추가될 수 있다.
}

// FromDocuments 는 docs 를 emb 로 임베딩해 InMemoryStore 에 색인하고 Store 를 반환한다.
// opts 는 스토어 생성 옵션으로, 현재는 확장 자리만 있다(SPEC §5.5, D3).
func FromDocuments(ctx context.Context, docs []document.Document, emb llm.EmbeddingClient, opts ...StoreOption) (Store, error) {
	// 옵션 적용 — 단일 인스턴스를 만들어 전달된 모든 옵션을 누적 적용한다.
	o := &storeOptions{}
	for _, opt := range opts {
		opt(o)
	}

	store := newInMemoryStore(emb)
	if err := store.Add(ctx, docs); err != nil {
		return nil, err
	}

	return store, nil
}

// cosineSimilarity 는 두 벡터의 코사인 유사도를 계산해 반환한다.
// 어느 쪽 벡터가 영벡터면 0 을 반환한다.
func cosineSimilarity(a, b []float32) float32 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}

	var dot, normA, normB float64
	minLen := len(a)
	if len(b) < minLen {
		minLen = len(b)
	}

	for i := 0; i < minLen; i++ {
		fa, fb := float64(a[i]), float64(b[i])
		dot += fa * fb
		normA += fa * fa
		normB += fb * fb
	}

	if normA == 0 || normB == 0 {
		return 0
	}

	return float32(dot / (math.Sqrt(normA) * math.Sqrt(normB)))
}

// errorMismatch 는 Embed 가 반환한 벡터 수가 입력 문서 수와 다를 때 에러를 반환한다.
func errorMismatch(wantDocs, gotVectors int) error {
	return fmt.Errorf("vectorstore: Embed 결과 벡터 수(%d)가 입력 문서 수(%d)와 다릅니다", gotVectors, wantDocs)
}

// matchesFilter 는 metadata 가 filter 의 모든 key-value 를 포함하는지 확인한다.
// filter 가 nil 이거나 빈 맵이면 항상 true 를 반환한다.
func matchesFilter(metadata map[string]any, filter map[string]any) bool {
	if len(filter) == 0 {
		return true
	}
	for k, v := range filter {
		mv, ok := metadata[k]
		if !ok {
			return false
		}
		// 단순 동등 비교 — 중첩 구조는 이 Phase 범위 밖이다.
		if mv != v {
			return false
		}
	}
	return true
}
