// search_test.go 는 task-003 검증 조건(시맨틱 검색 정렬·limit·점수)을 담는 단위 테스트다.
// 결정적 stub EmbeddingClient 로 네트워크 없이 유사도 순서·limit 절단·점수를 단정한다.
package store_test

import (
	"context"
	"testing"

	"github.com/zipkero/langgraph-go/llm"
	"github.com/zipkero/langgraph-go/store"
)

// stubEmbeddingClient 는 미리 정한 텍스트→벡터 맵을 돌려주는 결정적 EmbeddingClient stub 이다.
// 알려진 텍스트는 texts 맵에서 조회하고, 없으면 제로 벡터를 반환한다.
type stubEmbeddingClient struct {
	// texts 는 텍스트(키) → 벡터(값) 매핑이다.
	texts map[string][]float32
}

var _ llm.EmbeddingClient = (*stubEmbeddingClient)(nil)

// Embed 는 배치 텍스트를 임베딩한다. 알려진 텍스트는 사전 벡터를, 모르는 텍스트는 제로 벡터를 반환한다.
func (s *stubEmbeddingClient) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	result := make([][]float32, len(texts))
	for i, t := range texts {
		if v, ok := s.texts[t]; ok {
			cp := make([]float32, len(v))
			copy(cp, v)
			result[i] = cp
		} else {
			// 알려지지 않은 텍스트는 [0, 0, 0] 반환
			result[i] = []float32{0, 0, 0}
		}
	}
	return result, nil
}

// EmbedQuery 는 단일 질의 텍스트를 임베딩한다.
func (s *stubEmbeddingClient) EmbedQuery(ctx context.Context, text string) ([]float32, error) {
	vecs, err := s.Embed(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	return vecs[0], nil
}

// newStubClient 는 텍스트→벡터 매핑으로 stub EmbeddingClient 를 생성한다.
func newStubClient(mapping map[string][]float32) *stubEmbeddingClient {
	return &stubEmbeddingClient{texts: mapping}
}

// newIndexedStore 는 주어진 stub 클라이언트로 임베딩 인덱스가 설정된 InMemoryStore 를 반환한다.
func newIndexedStore(client llm.EmbeddingClient) *store.InMemoryStore {
	return store.NewInMemoryStore(
		store.WithIndex(store.IndexConfig{
			Embed: client,
			Dims:  3,
		}),
	)
}

// TestSearch_유사도_내림차순_정렬은 질의에 가장 가까운 항목이 1순위로 반환됨을 검증한다.
//
// 설계:
//   - 항목 A: text "고양이 울음 야옹" → 벡터 [1, 0, 0]
//   - 항목 B: text "강아지 짖음 멍멍" → 벡터 [0, 1, 0]
//   - 항목 C: text "파도 소리 철썩" → 벡터 [0, 0, 1]
//   - 질의: "고양이" → 벡터 [0.9, 0.1, 0]  (A 에 가장 가까움)
//
// 코사인 유사도: A≈0.994, B≈0.0995, C=0 → Search 1순위 A, 2순위 B, 3순위 C.
func TestSearch_유사도_내림차순_정렬은(t *testing.T) {
	// valueToText 결과를 키로 사용해야 stub 이 Put 임베딩 호출과 일치한다.
	// valueToText 는 "키:값" 를 정렬해 반환하므로 단일 필드 맵은 "text:내용" 형태가 된다.
	mapping := map[string][]float32{
		"text:고양이 울음 야옹": {1, 0, 0},
		"text:강아지 짖음 멍멍": {0, 1, 0},
		"text:파도 소리 철썩":  {0, 0, 1},
		"고양이":            {0.9, 0.1, 0}, // 질의 벡터
	}

	s := newIndexedStore(newStubClient(mapping))
	ctx := context.Background()
	ns := store.Namespace{"test", "search"}

	if err := s.Put(ctx, ns, "a", map[string]any{"text": "고양이 울음 야옹"}); err != nil {
		t.Fatalf("Put A 실패: %v", err)
	}
	if err := s.Put(ctx, ns, "b", map[string]any{"text": "강아지 짖음 멍멍"}); err != nil {
		t.Fatalf("Put B 실패: %v", err)
	}
	if err := s.Put(ctx, ns, "c", map[string]any{"text": "파도 소리 철썩"}); err != nil {
		t.Fatalf("Put C 실패: %v", err)
	}

	results, err := s.Search(ctx, ns, "고양이", 0)
	if err != nil {
		t.Fatalf("Search 오류: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("결과 개수: 기대 3, 실제 %d", len(results))
	}

	// 1순위가 항목 A("고양이 울음 야옹")임을 확인
	if results[0]["text"] != "고양이 울음 야옹" {
		t.Errorf("1순위: 기대 '고양이 울음 야옹', 실제 %v", results[0]["text"])
	}
}

// TestSearch_limit_절단은 limit 수만큼만 결과가 반환됨을 검증한다.
func TestSearch_limit_절단은(t *testing.T) {
	mapping := map[string][]float32{
		"text:고양이 울음 야옹": {1, 0, 0},
		"text:강아지 짖음 멍멍": {0, 1, 0},
		"text:파도 소리 철썩":  {0, 0, 1},
		"고양이":            {0.9, 0.1, 0},
	}

	s := newIndexedStore(newStubClient(mapping))
	ctx := context.Background()
	ns := store.Namespace{"test", "limit"}

	s.Put(ctx, ns, "a", map[string]any{"text": "고양이 울음 야옹"})
	s.Put(ctx, ns, "b", map[string]any{"text": "강아지 짖음 멍멍"})
	s.Put(ctx, ns, "c", map[string]any{"text": "파도 소리 철썩"})

	// limit=2 이면 2개만 반환
	results, err := s.Search(ctx, ns, "고양이", 2)
	if err != nil {
		t.Fatalf("Search 오류: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("limit=2 결과 개수: 기대 2, 실제 %d", len(results))
	}

	// limit=1 이면 1개만 반환
	results1, err := s.Search(ctx, ns, "고양이", 1)
	if err != nil {
		t.Fatalf("Search limit=1 오류: %v", err)
	}
	if len(results1) != 1 {
		t.Errorf("limit=1 결과 개수: 기대 1, 실제 %d", len(results1))
	}

	// limit=0 이면 전체 반환
	resultsAll, err := s.Search(ctx, ns, "고양이", 0)
	if err != nil {
		t.Fatalf("Search limit=0 오류: %v", err)
	}
	if len(resultsAll) != 3 {
		t.Errorf("limit=0 전체 반환: 기대 3, 실제 %d", len(resultsAll))
	}
}

// TestSearchItems_점수_내림차순은 SearchItems 결과의 Score 가 내림차순임을 검증한다.
func TestSearchItems_점수_내림차순은(t *testing.T) {
	mapping := map[string][]float32{
		"text:고양이 울음 야옹": {1, 0, 0},
		"text:강아지 짖음 멍멍": {0, 1, 0},
		"text:파도 소리 철썩":  {0, 0, 1},
		"고양이":            {0.9, 0.1, 0},
	}

	s := newIndexedStore(newStubClient(mapping))
	ctx := context.Background()
	ns := store.Namespace{"test", "score"}

	s.Put(ctx, ns, "a", map[string]any{"text": "고양이 울음 야옹"})
	s.Put(ctx, ns, "b", map[string]any{"text": "강아지 짖음 멍멍"})
	s.Put(ctx, ns, "c", map[string]any{"text": "파도 소리 철썩"})

	items, err := s.SearchItems(ctx, ns, "고양이", 0)
	if err != nil {
		t.Fatalf("SearchItems 오류: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("결과 개수: 기대 3, 실제 %d", len(items))
	}

	// 1순위가 항목 A 이고 Score 가 가장 높다
	if items[0].Value["text"] != "고양이 울음 야옹" {
		t.Errorf("1순위 Value: 기대 '고양이 울음 야옹', 실제 %v", items[0].Value["text"])
	}
	if items[0].Score <= 0 {
		t.Errorf("1순위 Score: 0 초과여야 하는데 %v", items[0].Score)
	}

	// 점수가 내림차순인지 확인
	for i := 1; i < len(items); i++ {
		if items[i].Score > items[i-1].Score {
			t.Errorf("Score 내림차순 위반: items[%d].Score(%v) > items[%d].Score(%v)",
				i, items[i].Score, i-1, items[i-1].Score)
		}
	}
}

// TestSearchItems_limit_절단_점수_채움은 limit=2 이면 2개, 각 Item.Score 가 내림차순임을 검증한다.
func TestSearchItems_limit_절단_점수_채움은(t *testing.T) {
	mapping := map[string][]float32{
		"text:고양이 울음 야옹": {1, 0, 0},
		"text:강아지 짖음 멍멍": {0, 1, 0},
		"text:파도 소리 철썩":  {0, 0, 1},
		"고양이":            {0.9, 0.1, 0},
	}

	s := newIndexedStore(newStubClient(mapping))
	ctx := context.Background()
	ns := store.Namespace{"test", "limit_score"}

	s.Put(ctx, ns, "a", map[string]any{"text": "고양이 울음 야옹"})
	s.Put(ctx, ns, "b", map[string]any{"text": "강아지 짖음 멍멍"})
	s.Put(ctx, ns, "c", map[string]any{"text": "파도 소리 철썩"})

	items, err := s.SearchItems(ctx, ns, "고양이", 2)
	if err != nil {
		t.Fatalf("SearchItems 오류: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("limit=2 결과 개수: 기대 2, 실제 %d", len(items))
	}

	// 상위 2개 모두 Score > 0 이어야 한다(질의 벡터 [0.9,0.1,0] 에 대해 A,B 는 점수 > 0)
	for i, item := range items {
		if item.Score <= 0 {
			t.Errorf("items[%d].Score: 0 초과여야 하는데 %v", i, item.Score)
		}
	}

	// 점수 내림차순 확인
	if items[1].Score > items[0].Score {
		t.Errorf("Score 내림차순 위반: items[1].Score(%v) > items[0].Score(%v)",
			items[1].Score, items[0].Score)
	}

	// 1순위가 A 임을 확인
	if items[0].Value["text"] != "고양이 울음 야옹" {
		t.Errorf("1순위: 기대 '고양이 울음 야옹', 실제 %v", items[0].Value["text"])
	}
}

// TestSearchItems_메타데이터_필드는 SearchItems 결과 Item 의 Namespace·Key·CreatedAt 이 채워짐을 검증한다.
func TestSearchItems_메타데이터_필드는(t *testing.T) {
	mapping := map[string][]float32{
		"text:고양이 울음 야옹": {1, 0, 0},
		"고양이":            {0.9, 0.1, 0},
	}

	s := newIndexedStore(newStubClient(mapping))
	ctx := context.Background()
	ns := store.Namespace{"meta", "test"}

	s.Put(ctx, ns, "mykey", map[string]any{"text": "고양이 울음 야옹"})

	items, err := s.SearchItems(ctx, ns, "고양이", 0)
	if err != nil {
		t.Fatalf("SearchItems 오류: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("결과 개수: 기대 1, 실제 %d", len(items))
	}

	item := items[0]

	// Namespace 확인
	if len(item.Namespace) != 2 || item.Namespace[0] != "meta" || item.Namespace[1] != "test" {
		t.Errorf("Namespace: 기대 [meta test], 실제 %v", item.Namespace)
	}

	// Key 확인
	if item.Key != "mykey" {
		t.Errorf("Key: 기대 mykey, 실제 %q", item.Key)
	}

	// CreatedAt 이 zero 가 아님을 확인
	if item.CreatedAt.IsZero() {
		t.Error("CreatedAt: zero 여서는 안 된다")
	}

	// Score 가 0 초과임을 확인
	if item.Score <= 0 {
		t.Errorf("Score: 0 초과여야 하는데 %v", item.Score)
	}
}

// TestSearch_빈_네임스페이스는_빈_결과를_반환한다 는 항목이 없는 네임스페이스에서 Search 가
// 빈 슬라이스를 에러 없이 반환함을 검증한다.
func TestSearch_빈_네임스페이스는_빈_결과를_반환한다(t *testing.T) {
	mapping := map[string][]float32{
		"고양이": {0.9, 0.1, 0},
	}

	s := newIndexedStore(newStubClient(mapping))
	ctx := context.Background()
	ns := store.Namespace{"empty", "ns"}

	results, err := s.Search(ctx, ns, "고양이", 10)
	if err != nil {
		t.Fatalf("Search 오류: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("빈 네임스페이스: 기대 0개, 실제 %d개", len(results))
	}
}
