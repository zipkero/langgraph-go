// store_test.go 는 task-001 검증 조건(저장/조회/삭제/네임스페이스 분리)을 담는 단위 테스트다.
// stub EmbeddingClient 없이 결정적으로 실행된다.
package store_test

import (
	"context"
	"testing"

	"github.com/zipkero/langgraph-go/store"
)

// newTestStore 는 인덱스 미설정 인메모리 스토어를 반환한다.
func newTestStore() *store.InMemoryStore {
	return store.NewInMemoryStore()
}

// TestPutGet_저장후_동일_키_조회는_저장값을_반환한다 는 Put 후 같은 키 Get 이 (값, true, nil)을 반환함을 검증한다.
func TestPutGet_저장후_동일_키_조회는_저장값을_반환한다(t *testing.T) {
	s := newTestStore()
	ctx := context.Background()
	ns := store.Namespace{"user", "test"}
	key := "profile"
	value := map[string]any{"name": "alice", "age": 30}

	if err := s.Put(ctx, ns, key, value); err != nil {
		t.Fatalf("Put 실패: %v", err)
	}

	got, ok, err := s.Get(ctx, ns, key)
	if err != nil {
		t.Fatalf("Get 오류: %v", err)
	}
	if !ok {
		t.Fatal("Get: ok 가 false 여야 할 때 true 를 기대했으나 false")
	}
	if got["name"] != "alice" {
		t.Errorf("name: 기대 alice, 실제 %v", got["name"])
	}
	if got["age"] != 30 {
		t.Errorf("age: 기대 30, 실제 %v", got["age"])
	}
}

// TestGet_없는_키는_not_found를_반환한다 는 존재하지 않는 키의 Get 이 (nil, false, nil)을 반환함을 검증한다.
func TestGet_없는_키는_not_found를_반환한다(t *testing.T) {
	s := newTestStore()
	ctx := context.Background()
	ns := store.Namespace{"user", "test"}

	got, ok, err := s.Get(ctx, ns, "nonexistent")
	if err != nil {
		t.Fatalf("Get 오류: %v", err)
	}
	if ok {
		t.Errorf("없는 키: ok 가 false 여야 하는데 true")
	}
	if got != nil {
		t.Errorf("없는 키: 반환값이 nil 이어야 하는데 %v", got)
	}
}

// TestDelete_삭제후_Get은_not_found를_반환한다 는 Delete 후 Get 이 (nil, false, nil)을 반환함을 검증한다.
func TestDelete_삭제후_Get은_not_found를_반환한다(t *testing.T) {
	s := newTestStore()
	ctx := context.Background()
	ns := store.Namespace{"user", "test"}
	key := "session"
	value := map[string]any{"token": "abc123"}

	if err := s.Put(ctx, ns, key, value); err != nil {
		t.Fatalf("Put 실패: %v", err)
	}
	// 저장 확인
	_, ok, _ := s.Get(ctx, ns, key)
	if !ok {
		t.Fatal("Put 후 Get 이 ok=true 여야 한다")
	}

	if err := s.Delete(ctx, ns, key); err != nil {
		t.Fatalf("Delete 실패: %v", err)
	}

	got, ok, err := s.Get(ctx, ns, key)
	if err != nil {
		t.Fatalf("삭제 후 Get 오류: %v", err)
	}
	if ok {
		t.Errorf("삭제 후: ok 가 false 여야 하는데 true")
	}
	if got != nil {
		t.Errorf("삭제 후: 반환값이 nil 이어야 하는데 %v", got)
	}
}

// TestNamespace_분리_동일키는_독립적으로_보관된다 는 서로 다른 네임스페이스에 같은 키를 저장하면
// 각 네임스페이스에서 독립적으로 조회됨을 검증한다.
func TestNamespace_분리_동일키는_독립적으로_보관된다(t *testing.T) {
	s := newTestStore()
	ctx := context.Background()
	ns1 := store.Namespace{"user", "alice"}
	ns2 := store.Namespace{"user", "bob"}
	key := "preference"
	val1 := map[string]any{"theme": "dark"}
	val2 := map[string]any{"theme": "light"}

	if err := s.Put(ctx, ns1, key, val1); err != nil {
		t.Fatalf("ns1 Put 실패: %v", err)
	}
	if err := s.Put(ctx, ns2, key, val2); err != nil {
		t.Fatalf("ns2 Put 실패: %v", err)
	}

	// ns1 조회
	got1, ok1, err := s.Get(ctx, ns1, key)
	if err != nil || !ok1 {
		t.Fatalf("ns1 Get 실패: ok=%v err=%v", ok1, err)
	}
	if got1["theme"] != "dark" {
		t.Errorf("ns1 theme: 기대 dark, 실제 %v", got1["theme"])
	}

	// ns2 조회
	got2, ok2, err := s.Get(ctx, ns2, key)
	if err != nil || !ok2 {
		t.Fatalf("ns2 Get 실패: ok=%v err=%v", ok2, err)
	}
	if got2["theme"] != "light" {
		t.Errorf("ns2 theme: 기대 light, 실제 %v", got2["theme"])
	}

	// ns1 삭제가 ns2에 영향을 주지 않는다
	if err := s.Delete(ctx, ns1, key); err != nil {
		t.Fatalf("ns1 Delete 실패: %v", err)
	}
	_, okAfter1, _ := s.Get(ctx, ns1, key)
	if okAfter1 {
		t.Error("ns1 삭제 후 ok 가 false 여야 한다")
	}
	_, okAfter2, _ := s.Get(ctx, ns2, key)
	if !okAfter2 {
		t.Error("ns1 삭제 후 ns2 항목은 여전히 조회돼야 한다")
	}
}

// TestPut_갱신시_값이_교체된다 는 동일 키 재저장 시 값이 갱신됨을 검증한다.
func TestPut_갱신시_값이_교체된다(t *testing.T) {
	s := newTestStore()
	ctx := context.Background()
	ns := store.Namespace{"ns"}
	key := "k"

	s.Put(ctx, ns, key, map[string]any{"v": 1})
	s.Put(ctx, ns, key, map[string]any{"v": 2})

	got, ok, err := s.Get(ctx, ns, key)
	if err != nil || !ok {
		t.Fatalf("Get 실패: ok=%v err=%v", ok, err)
	}
	if got["v"] != 2 {
		t.Errorf("갱신 후 v: 기대 2, 실제 %v", got["v"])
	}
}

// TestGet_반환값_수정이_내부에_영향없다 는 Get 반환값을 수정해도 스토어 내부 값이 변경되지 않음을 검증한다.
func TestGet_반환값_수정이_내부에_영향없다(t *testing.T) {
	s := newTestStore()
	ctx := context.Background()
	ns := store.Namespace{"ns"}
	key := "k"

	s.Put(ctx, ns, key, map[string]any{"x": "original"})

	got, _, _ := s.Get(ctx, ns, key)
	got["x"] = "mutated"

	got2, _, _ := s.Get(ctx, ns, key)
	if got2["x"] != "original" {
		t.Errorf("외부 변경이 내부에 영향: 기대 original, 실제 %v", got2["x"])
	}
}

// TestDelete_없는_키_삭제는_에러없이_동작한다 는 존재하지 않는 키를 삭제해도 에러가 없음을 검증한다.
func TestDelete_없는_키_삭제는_에러없이_동작한다(t *testing.T) {
	s := newTestStore()
	ctx := context.Background()
	ns := store.Namespace{"ns"}

	if err := s.Delete(ctx, ns, "nonexistent"); err != nil {
		t.Errorf("없는 키 삭제 에러: %v", err)
	}
}
