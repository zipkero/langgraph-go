// fallback_search_test.go 는 task-004 검증 조건(인덱스 미설정 폴백 검색)을 담는 단위 테스트다.
// WithIndex 없이 생성한 store(인덱스 미설정)에서 Search/SearchItems 를 호출해
// 키 정렬 순서·limit 절단·점수 0·무에러·질의 무시를 단정한다.
// 임베딩 클라이언트를 주입하지 않으므로 임베딩 호출 자체가 발생하지 않는다.
package store_test

import (
	"context"
	"testing"

	"github.com/zipkero/langgraph-go/store"
)

// newFallbackStore 는 WithIndex 없이 생성한 인덱스 미설정 InMemoryStore 를 반환한다.
// 임베딩 클라이언트를 주입하지 않으므로 Search/SearchItems 에서 임베딩 호출이 발생하지 않는다.
func newFallbackStore() *store.InMemoryStore {
	return store.NewInMemoryStore()
}

// TestFallbackSearch_키정렬_순서는 인덱스 미설정 store 에서 Search 가
// 키 정렬 순서(사전순)로 결과를 반환함을 검증한다.
func TestFallbackSearch_키정렬_순서는(t *testing.T) {
	s := newFallbackStore()
	ctx := context.Background()
	ns := store.Namespace{"fallback", "order"}

	// 의도적으로 알파벳 역순 키로 저장해 정렬 결정성을 검증한다.
	if err := s.Put(ctx, ns, "charlie", map[string]any{"name": "charlie"}); err != nil {
		t.Fatalf("Put charlie 실패: %v", err)
	}
	if err := s.Put(ctx, ns, "alice", map[string]any{"name": "alice"}); err != nil {
		t.Fatalf("Put alice 실패: %v", err)
	}
	if err := s.Put(ctx, ns, "bob", map[string]any{"name": "bob"}); err != nil {
		t.Fatalf("Put bob 실패: %v", err)
	}

	results, err := s.Search(ctx, ns, "아무_질의", 0)
	if err != nil {
		t.Fatalf("Search 오류: %v", err)
	}
	// 3개 모두 반환
	if len(results) != 3 {
		t.Fatalf("결과 개수: 기대 3, 실제 %d", len(results))
	}
	// 키 정렬 순서: alice < bob < charlie
	expected := []string{"alice", "bob", "charlie"}
	for i, exp := range expected {
		if results[i]["name"] != exp {
			t.Errorf("results[%d].name: 기대 %q, 실제 %v", i, exp, results[i]["name"])
		}
	}
}

// TestFallbackSearch_limit_절단은 인덱스 미설정 store 에서 Search 가
// limit 개수만큼만 반환함을 검증한다.
func TestFallbackSearch_limit_절단은(t *testing.T) {
	s := newFallbackStore()
	ctx := context.Background()
	ns := store.Namespace{"fallback", "limit"}

	s.Put(ctx, ns, "z-key", map[string]any{"v": "z"})
	s.Put(ctx, ns, "a-key", map[string]any{"v": "a"})
	s.Put(ctx, ns, "m-key", map[string]any{"v": "m"})
	s.Put(ctx, ns, "b-key", map[string]any{"v": "b"})

	// limit=2 이면 키 정렬 상위 2개만 반환
	results, err := s.Search(ctx, ns, "질의", 2)
	if err != nil {
		t.Fatalf("Search limit=2 오류: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("limit=2 결과 개수: 기대 2, 실제 %d", len(results))
	}
	// 키 정렬 상위 2개: a-key, b-key
	if results[0]["v"] != "a" {
		t.Errorf("results[0].v: 기대 a, 실제 %v", results[0]["v"])
	}
	if results[1]["v"] != "b" {
		t.Errorf("results[1].v: 기대 b, 실제 %v", results[1]["v"])
	}

	// limit=1 이면 1개만 반환
	results1, err := s.Search(ctx, ns, "질의", 1)
	if err != nil {
		t.Fatalf("Search limit=1 오류: %v", err)
	}
	if len(results1) != 1 {
		t.Errorf("limit=1 결과 개수: 기대 1, 실제 %d", len(results1))
	}
	if results1[0]["v"] != "a" {
		t.Errorf("results1[0].v: 기대 a, 실제 %v", results1[0]["v"])
	}

	// limit=0 이면 전체(4개) 반환
	resultsAll, err := s.Search(ctx, ns, "질의", 0)
	if err != nil {
		t.Fatalf("Search limit=0 오류: %v", err)
	}
	if len(resultsAll) != 4 {
		t.Errorf("limit=0 전체 반환: 기대 4, 실제 %d", len(resultsAll))
	}
}

// TestFallbackSearch_무에러는 인덱스 미설정 store 에서 Search/SearchItems 가
// 에러를 반환하지 않음을 검증한다.
func TestFallbackSearch_무에러는(t *testing.T) {
	s := newFallbackStore()
	ctx := context.Background()
	ns := store.Namespace{"fallback", "noerr"}

	s.Put(ctx, ns, "k1", map[string]any{"data": "value1"})
	s.Put(ctx, ns, "k2", map[string]any{"data": "value2"})

	_, err := s.Search(ctx, ns, "질의", 10)
	if err != nil {
		t.Errorf("Search 에러: 기대 nil, 실제 %v", err)
	}

	_, err = s.SearchItems(ctx, ns, "질의", 10)
	if err != nil {
		t.Errorf("SearchItems 에러: 기대 nil, 실제 %v", err)
	}
}

// TestFallbackSearchItems_점수_0은 인덱스 미설정 store 에서 SearchItems 결과의
// 모든 Item.Score 가 0임을 검증한다.
func TestFallbackSearchItems_점수_0은(t *testing.T) {
	s := newFallbackStore()
	ctx := context.Background()
	ns := store.Namespace{"fallback", "score"}

	s.Put(ctx, ns, "k1", map[string]any{"val": 1})
	s.Put(ctx, ns, "k2", map[string]any{"val": 2})
	s.Put(ctx, ns, "k3", map[string]any{"val": 3})

	items, err := s.SearchItems(ctx, ns, "임의질의", 0)
	if err != nil {
		t.Fatalf("SearchItems 오류: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("결과 개수: 기대 3, 실제 %d", len(items))
	}
	for i, item := range items {
		if item.Score != 0 {
			t.Errorf("items[%d].Score: 기대 0, 실제 %v", i, item.Score)
		}
	}
}

// TestFallbackSearch_질의_무시는 서로 다른 질의로 검색해도 동일한 순서·집합이 반환됨을 검증한다.
// 인덱스 미설정 시 질의는 결과에 영향을 주지 않는다.
func TestFallbackSearch_질의_무시는(t *testing.T) {
	s := newFallbackStore()
	ctx := context.Background()
	ns := store.Namespace{"fallback", "query_ignore"}

	s.Put(ctx, ns, "apple", map[string]any{"fruit": "apple"})
	s.Put(ctx, ns, "banana", map[string]any{"fruit": "banana"})
	s.Put(ctx, ns, "cherry", map[string]any{"fruit": "cherry"})

	// 질의 1
	res1, err := s.Search(ctx, ns, "apple", 0)
	if err != nil {
		t.Fatalf("질의1 Search 오류: %v", err)
	}

	// 질의 2 (완전히 다른 질의)
	res2, err := s.Search(ctx, ns, "완전히_다른_질의_xyz", 0)
	if err != nil {
		t.Fatalf("질의2 Search 오류: %v", err)
	}

	// 개수 동일
	if len(res1) != len(res2) {
		t.Errorf("질의에 따라 개수가 다름: res1=%d res2=%d", len(res1), len(res2))
	}

	// 순서·집합 동일
	for i := range res1 {
		if res1[i]["fruit"] != res2[i]["fruit"] {
			t.Errorf("results[%d] 불일치: res1=%v res2=%v", i, res1[i]["fruit"], res2[i]["fruit"])
		}
	}
}

// TestFallbackSearchItems_키정렬_순서는 인덱스 미설정 store 에서 SearchItems 가
// 키 정렬 순서로 Item 을 반환하고, Namespace·Key·Value 필드가 채워짐을 검증한다.
func TestFallbackSearchItems_키정렬_순서는(t *testing.T) {
	s := newFallbackStore()
	ctx := context.Background()
	ns := store.Namespace{"fallback", "items_order"}

	// 역순으로 저장
	s.Put(ctx, ns, "c", map[string]any{"n": "C"})
	s.Put(ctx, ns, "a", map[string]any{"n": "A"})
	s.Put(ctx, ns, "b", map[string]any{"n": "B"})

	items, err := s.SearchItems(ctx, ns, "질의", 0)
	if err != nil {
		t.Fatalf("SearchItems 오류: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("결과 개수: 기대 3, 실제 %d", len(items))
	}

	// 키 정렬 순서: a < b < c
	expectedKeys := []string{"a", "b", "c"}
	for i, expKey := range expectedKeys {
		if items[i].Key != expKey {
			t.Errorf("items[%d].Key: 기대 %q, 실제 %q", i, expKey, items[i].Key)
		}
		if items[i].Score != 0 {
			t.Errorf("items[%d].Score: 기대 0, 실제 %v", i, items[i].Score)
		}
		// Namespace 필드가 채워져 있는지 확인
		if len(items[i].Namespace) != 2 {
			t.Errorf("items[%d].Namespace: 기대 2개 세그먼트, 실제 %v", i, items[i].Namespace)
		}
	}
}

// TestFallbackSearch_빈_네임스페이스는 항목이 없는 네임스페이스에서 Search 가
// 빈 슬라이스를 에러 없이 반환함을 검증한다.
func TestFallbackSearch_빈_네임스페이스는(t *testing.T) {
	s := newFallbackStore()
	ctx := context.Background()
	ns := store.Namespace{"fallback", "empty"}

	results, err := s.Search(ctx, ns, "질의", 5)
	if err != nil {
		t.Errorf("빈 네임스페이스 Search 오류: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("빈 네임스페이스: 기대 0개, 실제 %d개", len(results))
	}

	items, err := s.SearchItems(ctx, ns, "질의", 5)
	if err != nil {
		t.Errorf("빈 네임스페이스 SearchItems 오류: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("빈 네임스페이스 SearchItems: 기대 0개, 실제 %d개", len(items))
	}
}
