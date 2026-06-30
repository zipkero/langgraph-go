// item_test.go 는 task-002 검증 조건(GetItem 메타데이터 접근자, 타임스탬프, 점수 0)을 담는 단위 테스트다.
// stub EmbeddingClient 없이 결정적으로 실행된다.
package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/zipkero/langgraph-go/store"
)

// TestGetItem_저장후_메타데이터_조회는 Put 후 GetItem 이 값·네임스페이스·키·타임스탬프를 채우고 Score=0 을 반환함을 검증한다.
func TestGetItem_저장후_메타데이터_조회는(t *testing.T) {
	s := store.NewInMemoryStore()
	ctx := context.Background()
	ns := store.Namespace{"user", "test"}
	key := "profile"
	value := map[string]any{"name": "alice"}

	before := time.Now()
	if err := s.Put(ctx, ns, key, value); err != nil {
		t.Fatalf("Put 실패: %v", err)
	}
	after := time.Now()

	item, ok, err := s.GetItem(ctx, ns, key)
	if err != nil {
		t.Fatalf("GetItem 오류: %v", err)
	}
	if !ok {
		t.Fatal("GetItem: ok 가 true 여야 한다")
	}

	// 값 확인
	if item.Value["name"] != "alice" {
		t.Errorf("Value.name: 기대 alice, 실제 %v", item.Value["name"])
	}

	// 네임스페이스 확인
	if len(item.Namespace) != 2 || item.Namespace[0] != "user" || item.Namespace[1] != "test" {
		t.Errorf("Namespace: 기대 [user test], 실제 %v", item.Namespace)
	}

	// 키 확인
	if item.Key != key {
		t.Errorf("Key: 기대 %q, 실제 %q", key, item.Key)
	}

	// 점수는 비검색 경로이므로 0
	if item.Score != 0 {
		t.Errorf("Score: 기대 0, 실제 %v", item.Score)
	}

	// CreatedAt 이 Put 호출 전후 범위 안에 있다
	if item.CreatedAt.IsZero() {
		t.Error("CreatedAt: zero 여서는 안 된다")
	}
	if item.CreatedAt.Before(before) || item.CreatedAt.After(after) {
		t.Errorf("CreatedAt(%v) 이 Put 전후(%v ~ %v) 범위 밖이다", item.CreatedAt, before, after)
	}

	// 최초 저장이므로 UpdatedAt 은 zero value
	if !item.UpdatedAt.IsZero() {
		t.Errorf("최초 저장 후 UpdatedAt: zero 여야 하는데 %v", item.UpdatedAt)
	}
}

// TestGetItem_없는_키는_not_found를_반환한다 는 없는 키에 대해 GetItem 이 (Item{}, false, nil)을 반환함을 검증한다.
func TestGetItem_없는_키는_not_found를_반환한다(t *testing.T) {
	s := store.NewInMemoryStore()
	ctx := context.Background()
	ns := store.Namespace{"user", "test"}

	item, ok, err := s.GetItem(ctx, ns, "nonexistent")
	if err != nil {
		t.Fatalf("GetItem 오류: %v", err)
	}
	if ok {
		t.Error("없는 키: ok 가 false 여야 한다")
	}
	// 반환된 Item 의 각 필드가 zero value 여야 한다(Namespace 는 슬라이스라 직접 비교 불가).
	if item.Key != "" || item.Value != nil || item.Score != 0 ||
		!item.CreatedAt.IsZero() || !item.UpdatedAt.IsZero() {
		t.Errorf("없는 키: 반환 Item 이 zero 여야 하는데 %+v", item)
	}
}

// TestGetItem_재저장시_UpdatedAt이_갱신된다 는 동일 키 재저장 시 UpdatedAt 이 갱신되고 CreatedAt 이 보존됨을 검증한다.
func TestGetItem_재저장시_UpdatedAt이_갱신된다(t *testing.T) {
	s := store.NewInMemoryStore()
	ctx := context.Background()
	ns := store.Namespace{"ns"}
	key := "k"

	// 1차 저장
	if err := s.Put(ctx, ns, key, map[string]any{"v": 1}); err != nil {
		t.Fatalf("1차 Put 실패: %v", err)
	}
	item1, _, _ := s.GetItem(ctx, ns, key)
	createdAt := item1.CreatedAt

	// 1차 저장 직후 UpdatedAt 은 zero
	if !item1.UpdatedAt.IsZero() {
		t.Errorf("1차 저장 후 UpdatedAt: zero 여야 하는데 %v", item1.UpdatedAt)
	}

	// 2차 저장(갱신)
	if err := s.Put(ctx, ns, key, map[string]any{"v": 2}); err != nil {
		t.Fatalf("2차 Put 실패: %v", err)
	}
	afterUpdate := time.Now()
	item2, ok, err := s.GetItem(ctx, ns, key)
	if err != nil || !ok {
		t.Fatalf("2차 GetItem 실패: ok=%v err=%v", ok, err)
	}

	// CreatedAt 은 1차 저장 시각 그대로 보존
	if !item2.CreatedAt.Equal(createdAt) {
		t.Errorf("CreatedAt 보존: 기대 %v, 실제 %v", createdAt, item2.CreatedAt)
	}

	// UpdatedAt 은 zero 가 아니고 CreatedAt 이상(동일 나노초도 허용)
	if item2.UpdatedAt.IsZero() {
		t.Error("갱신 후 UpdatedAt: zero 여서는 안 된다")
	}
	if item2.UpdatedAt.Before(item2.CreatedAt) {
		t.Errorf("UpdatedAt(%v) 이 CreatedAt(%v) 보다 이전이다", item2.UpdatedAt, item2.CreatedAt)
	}
	// UpdatedAt 이 afterUpdate 이하임을 확인(미래 시각이 찍히지 않음)
	if item2.UpdatedAt.After(afterUpdate) {
		t.Errorf("UpdatedAt(%v) 이 측정 시각(%v) 이후다", item2.UpdatedAt, afterUpdate)
	}

	// 점수는 0
	if item2.Score != 0 {
		t.Errorf("Score: 기대 0, 실제 %v", item2.Score)
	}

	// 값은 갱신됨
	if item2.Value["v"] != 2 {
		t.Errorf("갱신 후 Value.v: 기대 2, 실제 %v", item2.Value["v"])
	}
}

// TestGetItem_반환값_수정이_내부에_영향없다 는 GetItem 반환값의 Value 를 수정해도 스토어 내부가 변경되지 않음을 검증한다.
func TestGetItem_반환값_수정이_내부에_영향없다(t *testing.T) {
	s := store.NewInMemoryStore()
	ctx := context.Background()
	ns := store.Namespace{"ns"}
	key := "k"

	s.Put(ctx, ns, key, map[string]any{"x": "original"})

	item, _, _ := s.GetItem(ctx, ns, key)
	item.Value["x"] = "mutated"

	item2, _, _ := s.GetItem(ctx, ns, key)
	if item2.Value["x"] != "original" {
		t.Errorf("외부 변경이 내부에 영향: 기대 original, 실제 %v", item2.Value["x"])
	}
}
