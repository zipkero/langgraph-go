// store 패키지는 장기 메모리 키-값 스토어 추상화와 인메모리 구현체를 담당한다.
// llm·config·표준 라이브러리만 의존하며, tool·agent·graph·vectorstore를 import하지 않는다(§28-1 규칙2·4).
package store

import (
	"context"
	"fmt"
	"maps"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/zipkero/langgraph-go/config"
	"github.com/zipkero/langgraph-go/llm"
)

// Namespace 는 네임스페이스 튜플이다. 타입 별칭(=)으로 선언해 tool.Store []string 시그니처와 호환한다.
type Namespace = []string

// Item 은 저장 항목의 메타데이터 동반 표현이다.
// Score 는 시맨틱 검색(SearchItems) 결과에서만 의미를 가지며, Get/GetItem·비검색 경로에서는 0이다.
type Item struct {
	// Namespace 는 항목이 속한 네임스페이스 튜플이다.
	Namespace Namespace
	// Key 는 네임스페이스 안에서 항목을 식별하는 키다.
	Key string
	// Value 는 저장된 맵 값이다.
	Value map[string]any
	// Score 는 시맨틱 검색 결과의 코사인 유사도 점수다. 비검색 경로에서는 0.
	Score float32
	// CreatedAt 은 항목이 처음 저장된 시각이다.
	CreatedAt time.Time
	// UpdatedAt 은 항목이 갱신된 시각이다. 최초 저장 시에는 zero value.
	UpdatedAt time.Time
}

// IndexConfig 는 임베딩 인덱스 설정값이다.
// WithIndex 옵션으로 NewInMemoryStore에 전달한다.
type IndexConfig struct {
	// Embed 는 임베딩 호출에 사용할 클라이언트다.
	Embed llm.EmbeddingClient
	// Dims 는 임베딩 차원 메타데이터다(선언적 용도; 코사인 계산에 강제 적용하지 않음).
	Dims int
}

// Store 는 장기 메모리 스토어 인터페이스다.
// Get/Put/Search 는 tool.Store 계약(map 기반)과 글자 그대로 같은 시그니처를 가진다.
// store 는 tool 을 import할 수 없으므로(§28-1) interface 임베딩 없이 동일 시그니처를 직접 나열한다.
type Store interface {
	// Get 은 네임스페이스와 키로 항목을 조회한다.
	// 항목이 있으면 (복사본, true, nil), 없으면 (nil, false, nil)을 반환한다.
	Get(ctx context.Context, namespace Namespace, key string) (map[string]any, bool, error)

	// Put 은 네임스페이스와 키로 값을 저장한다.
	// 이미 같은 키가 있으면 값과 UpdatedAt을 갱신한다.
	Put(ctx context.Context, namespace Namespace, key string, value map[string]any) error

	// Search 는 네임스페이스에서 질의로 항목을 검색한다.
	// 인덱스 설정 시 코사인 유사도 내림차순, 미설정 시 키 정렬 폴백으로 limit 개 반환한다.
	// limit <= 0 이면 전체를 반환한다.
	Search(ctx context.Context, namespace Namespace, query string, limit int) ([]map[string]any, error)

	// Delete 는 네임스페이스에서 키를 삭제한다.
	// 삭제 후 Get 은 not-found를 반환한다.
	Delete(ctx context.Context, namespace Namespace, key string) error

	// GetItem 은 네임스페이스와 키로 메타데이터 동반 항목을 조회한다.
	// 항목이 있으면 (Item, true, nil), 없으면 (Item{}, false, nil)을 반환한다. Score 는 0.
	GetItem(ctx context.Context, namespace Namespace, key string) (Item, bool, error)

	// SearchItems 는 네임스페이스에서 질의로 항목을 검색하고 점수와 메타데이터를 함께 반환한다.
	// 인덱스 설정 시 Score 필드가 채워지고, 미설정 시 0이다.
	SearchItems(ctx context.Context, namespace Namespace, query string, limit int) ([]Item, error)
}

// record 는 내부 저장 단위다. 값·임베딩 벡터·타임스탬프를 함께 보관한다.
type record struct {
	namespace Namespace
	key       string
	value     map[string]any
	vector    []float32
	createdAt time.Time
	updatedAt time.Time
}

// InMemoryStore 는 Store 의 인메모리 구현체다.
// 저장 구조: 네임스페이스 결합 키 문자열 → (키 → record) 2단 맵.
// 동시 접근은 sync.RWMutex 로 보호한다(checkpoint.InMemorySaver 선례).
type InMemoryStore struct {
	mu    sync.RWMutex
	data  map[string]map[string]*record // nsKey → key → record
	index *IndexConfig
}

// 정적 단언: *InMemoryStore 가 Store 를 충족함을 컴파일 타임에 보장한다.
var _ Store = (*InMemoryStore)(nil)

// StoreOption 은 NewInMemoryStore 에 전달하는 옵션 함수 타입이다.
type StoreOption func(*InMemoryStore)

// WithIndex 는 임베딩 인덱스를 설정하는 StoreOption 을 반환한다.
// 이 옵션을 지정하지 않으면 인덱스가 비활성화되고 Search 는 키 정렬 폴백으로 동작한다.
func WithIndex(cfg IndexConfig) StoreOption {
	return func(s *InMemoryStore) {
		s.index = &cfg
	}
}

// NewInMemoryStore 는 opts 를 적용해 초기화된 InMemoryStore 를 반환한다.
func NewInMemoryStore(opts ...StoreOption) *InMemoryStore {
	s := &InMemoryStore{
		data: make(map[string]map[string]*record),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// namespaceKey 는 네임스페이스 튜플을 결합 키 문자열로 정규화한다.
// 각 세그먼트를 "\x00" 으로 구분해 단순 문자열 포함 충돌을 방지한다.
func namespaceKey(ns Namespace) string {
	return strings.Join(ns, "\x00")
}

// copyValue 는 map[string]any 의 얕은 복사본을 반환한다.
// 반환 값이 외부에서 변경돼도 내부 레코드에 영향이 없도록 보호한다.
func copyValue(v map[string]any) map[string]any {
	if v == nil {
		return nil
	}
	cp := make(map[string]any, len(v))
	maps.Copy(cp, v)
	return cp
}

// Get 은 네임스페이스와 키로 항목을 조회한다.
// 항목이 있으면 (값 복사본, true, nil), 없으면 (nil, false, nil)을 반환한다.
func (s *InMemoryStore) Get(ctx context.Context, namespace Namespace, key string) (map[string]any, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	nsKey := namespaceKey(namespace)
	inner, ok := s.data[nsKey]
	if !ok {
		return nil, false, nil
	}
	rec, ok := inner[key]
	if !ok {
		return nil, false, nil
	}
	return copyValue(rec.value), true, nil
}

// Put 은 네임스페이스와 키로 값을 저장한다.
// 이미 같은 키가 있으면 값과 UpdatedAt 을 갱신하고, 신규이면 CreatedAt 을 채운다.
// 인덱스가 설정돼 있으면 값의 텍스트 표현을 임베딩해 레코드 벡터로 보관한다.
func (s *InMemoryStore) Put(ctx context.Context, namespace Namespace, key string, value map[string]any) error {
	// 인덱스가 설정된 경우 잠금 전에 임베딩한다(네트워크 호출이므로 잠금 밖).
	var vec []float32
	if s.index != nil && s.index.Embed != nil {
		text := valueToText(value)
		v, err := s.index.Embed.EmbedQuery(ctx, text)
		if err != nil {
			return err
		}
		vec = v
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	nsKey := namespaceKey(namespace)
	if s.data[nsKey] == nil {
		s.data[nsKey] = make(map[string]*record)
	}

	now := time.Now()
	existing, exists := s.data[nsKey][key]
	if exists {
		// 기존 레코드 갱신
		existing.value = copyValue(value)
		existing.vector = vec
		existing.updatedAt = now
	} else {
		// 신규 레코드 생성
		nsCopy := make(Namespace, len(namespace))
		copy(nsCopy, namespace)
		s.data[nsKey][key] = &record{
			namespace: nsCopy,
			key:       key,
			value:     copyValue(value),
			vector:    vec,
			createdAt: now,
		}
	}
	return nil
}

// Delete 는 네임스페이스에서 키를 삭제한다.
// 삭제 후 Get 은 not-found(nil, false, nil)를 반환한다.
func (s *InMemoryStore) Delete(ctx context.Context, namespace Namespace, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	nsKey := namespaceKey(namespace)
	inner, ok := s.data[nsKey]
	if !ok {
		return nil
	}
	delete(inner, key)
	return nil
}

// GetItem 은 네임스페이스와 키로 메타데이터 동반 항목을 조회한다.
// 항목이 있으면 (Item, true, nil), 없으면 (Item{}, false, nil)을 반환한다. Score 는 0.
func (s *InMemoryStore) GetItem(ctx context.Context, namespace Namespace, key string) (Item, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	nsKey := namespaceKey(namespace)
	inner, ok := s.data[nsKey]
	if !ok {
		return Item{}, false, nil
	}
	rec, ok := inner[key]
	if !ok {
		return Item{}, false, nil
	}
	item := Item{
		Namespace: rec.namespace,
		Key:       rec.key,
		Value:     copyValue(rec.value),
		Score:     0,
		CreatedAt: rec.createdAt,
		UpdatedAt: rec.updatedAt,
	}
	return item, true, nil
}

// Search 는 네임스페이스에서 질의로 항목을 검색한다.
// 인덱스 설정 시 코사인 유사도 내림차순, 미설정 시 키 정렬 폴백으로 limit 개를 반환한다.
// limit <= 0 이면 전체를 반환한다.
func (s *InMemoryStore) Search(ctx context.Context, namespace Namespace, query string, limit int) ([]map[string]any, error) {
	items, err := s.SearchItems(ctx, namespace, query, limit)
	if err != nil {
		return nil, err
	}
	result := make([]map[string]any, len(items))
	for i, item := range items {
		result[i] = item.Value
	}
	return result, nil
}

// SearchItems 는 네임스페이스에서 질의로 항목을 검색하고 점수와 메타데이터를 함께 반환한다.
// 인덱스 설정 시 Score 필드가 채워지고, 미설정 시 0이다.
func (s *InMemoryStore) SearchItems(ctx context.Context, namespace Namespace, query string, limit int) ([]Item, error) {
	// 인덱스가 설정된 경우 잠금 전에 질의 임베딩한다.
	var queryVec []float32
	if s.index != nil && s.index.Embed != nil {
		v, err := s.index.Embed.EmbedQuery(ctx, query)
		if err != nil {
			return nil, err
		}
		queryVec = v
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	nsKey := namespaceKey(namespace)
	inner := s.data[nsKey]
	if len(inner) == 0 {
		return []Item{}, nil
	}

	// 후보 수집
	candidates := make([]Item, 0, len(inner))
	for _, rec := range inner {
		item := Item{
			Namespace: rec.namespace,
			Key:       rec.key,
			Value:     copyValue(rec.value),
			CreatedAt: rec.createdAt,
			UpdatedAt: rec.updatedAt,
		}
		if queryVec != nil && len(rec.vector) > 0 {
			item.Score = cosineSimilarity(queryVec, rec.vector)
		}
		candidates = append(candidates, item)
	}

	// 정렬: 인덱스 설정 시 유사도 내림차순, 미설정 시 키 정렬
	if queryVec != nil {
		sort.Slice(candidates, func(i, j int) bool {
			return candidates[i].Score > candidates[j].Score
		})
	} else {
		sort.Slice(candidates, func(i, j int) bool {
			return candidates[i].Key < candidates[j].Key
		})
	}

	// limit 절단
	if limit > 0 && len(candidates) > limit {
		candidates = candidates[:limit]
	}
	return candidates, nil
}

// cosineSimilarity 는 두 벡터의 코사인 유사도를 반환한다.
// 빈 벡터 또는 영벡터이면 0을 반환한다. 두 벡터의 더 짧은 길이까지 합산해 길이 불일치에 견딘다.
// vectorstore.cosineSimilarity (L207-230) 와 동일한 수식 — store→vectorstore import 금지(§28-1)로 자체 구현.
func cosineSimilarity(a, b []float32) float32 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	n := min(len(b), len(a))
	var dot, normA, normB float64
	for i := range n {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	denom := math.Sqrt(normA) * math.Sqrt(normB)
	if denom == 0 {
		return 0
	}
	return float32(dot / denom)
}

// valueToText 는 map[string]any 값을 임베딩 입력용 텍스트로 변환한다.
// 키를 정렬해 결정적인 출력을 보장한다.
func valueToText(v map[string]any) string {
	if len(v) == 0 {
		return ""
	}
	keys := make([]string, 0, len(v))
	for k := range v {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	for i, k := range keys {
		if i > 0 {
			sb.WriteByte(' ')
		}
		sb.WriteString(k)
		sb.WriteByte(':')
		sb.WriteString(strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(
			strings.ReplaceAll(fmt.Sprint(v[k]), "\n", " "), "\t", " "), "\r", " ")))
	}
	return sb.String()
}

// contextKey 는 context 에 store 를 주입할 때 사용하는 비공개 키 타입이다.
type contextKey struct{}

// WithStore 는 s 를 ctx 에 실어 새 context.Context 를 반환한다.
func WithStore(ctx context.Context, s Store) context.Context {
	return context.WithValue(ctx, contextKey{}, s)
}

// FromContext 는 ctx 에서 Store 를 꺼낸다.
// store 가 실려 있으면 (Store, true), 없으면 (nil, false)를 반환한다.
func FromContext(ctx context.Context) (Store, bool) {
	s, ok := ctx.Value(contextKey{}).(Store)
	return s, ok
}

// UserIDFromConfig 는 cfg.Configurable 에서 user_id 를 뽑아 반환한다.
// config.GetUserID 의 래퍼 — 키가 없거나 문자열이 아니면 빈 문자열을 반환한다.
func UserIDFromConfig(cfg config.RunConfig) string {
	return config.GetUserID(cfg)
}
