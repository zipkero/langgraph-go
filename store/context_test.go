// context_test.go 는 task-005 검증 조건(context 주입/회수 헬퍼·사용자 식별자 헬퍼)을 담는 단위 테스트다.
// - WithStore(ctx, s) 로 실은 store 를 FromContext(ctx) 로 회수하면 (그 store, true)
// - 안 실은 context.Background() 에서는 (nil, false)
// - UserIDFromConfig: Configurable["user_id"] 있으면 그 값, 없으면 ""
package store_test

import (
	"context"
	"testing"

	"github.com/zipkero/langgraph-go/config"
	"github.com/zipkero/langgraph-go/store"
)

// TestWithStore_FromContext_주입후_회수는_같은_store를_반환한다 는
// WithStore 로 실은 store 를 FromContext 로 회수하면 동일 store 와 true 가 반환됨을 검증한다.
func TestWithStore_FromContext_주입후_회수는_같은_store를_반환한다(t *testing.T) {
	s := store.NewInMemoryStore()
	ctx := store.WithStore(context.Background(), s)

	got, ok := store.FromContext(ctx)
	if !ok {
		t.Fatal("FromContext: ok 가 true 여야 하는데 false")
	}
	if got == nil {
		t.Fatal("FromContext: 반환된 store 가 nil 이어서는 안 된다")
	}
	// 동일 인스턴스인지 확인 — Put 을 통해 동일 동작 객체임을 단정한다.
	putCtx := context.Background()
	ns := store.Namespace{"test"}
	key := "k"
	val := map[string]any{"v": 1}
	if err := s.Put(putCtx, ns, key, val); err != nil {
		t.Fatalf("Put 실패: %v", err)
	}
	_, okGet, err := got.Get(putCtx, ns, key)
	if err != nil || !okGet {
		t.Fatalf("FromContext 반환 store 의 Get: ok=%v err=%v", okGet, err)
	}
}

// TestFromContext_주입하지_않은_context에서는_nil_false를_반환한다 는
// store 를 싣지 않은 context 에서 FromContext 가 (nil, false) 를 반환함을 검증한다.
func TestFromContext_주입하지_않은_context에서는_nil_false를_반환한다(t *testing.T) {
	got, ok := store.FromContext(context.Background())
	if ok {
		t.Errorf("store 미주입 context: ok 가 false 여야 하는데 true")
	}
	if got != nil {
		t.Errorf("store 미주입 context: 반환값이 nil 이어야 하는데 %v", got)
	}
}

// TestWithStore_FromContext_왕복은_not_found를_구분한다 는
// 왕복(주입→회수)과 부재(not-found) 두 분기를 함께 단정한다.
func TestWithStore_FromContext_왕복은_not_found를_구분한다(t *testing.T) {
	s := store.NewInMemoryStore()

	// 주입된 context
	ctxWith := store.WithStore(context.Background(), s)
	gotWith, okWith := store.FromContext(ctxWith)
	if !okWith || gotWith == nil {
		t.Errorf("주입된 context: (store, true) 기대, 실제 (%v, %v)", gotWith, okWith)
	}

	// 미주입 context
	ctxEmpty := context.Background()
	gotEmpty, okEmpty := store.FromContext(ctxEmpty)
	if okEmpty || gotEmpty != nil {
		t.Errorf("미주입 context: (nil, false) 기대, 실제 (%v, %v)", gotEmpty, okEmpty)
	}
}

// TestUserIDFromConfig_값있을때_그값을_반환한다 는
// Configurable["user_id"] 가 있을 때 UserIDFromConfig 가 그 값을 반환함을 검증한다.
func TestUserIDFromConfig_값있을때_그값을_반환한다(t *testing.T) {
	cfg := config.RunConfig{
		Configurable: map[string]any{
			"user_id": "user-42",
		},
	}
	got := store.UserIDFromConfig(cfg)
	if got != "user-42" {
		t.Errorf("UserIDFromConfig: 기대 user-42, 실제 %q", got)
	}
}

// TestUserIDFromConfig_키없을때_빈문자열을_반환한다 는
// Configurable 에 "user_id" 키가 없을 때 UserIDFromConfig 가 "" 를 반환함을 검증한다.
func TestUserIDFromConfig_키없을때_빈문자열을_반환한다(t *testing.T) {
	cfg := config.RunConfig{
		Configurable: map[string]any{},
	}
	got := store.UserIDFromConfig(cfg)
	if got != "" {
		t.Errorf("UserIDFromConfig(키 없음): 기대 \"\", 실제 %q", got)
	}
}

// TestUserIDFromConfig_nil_Configurable_빈문자열을_반환한다 는
// Configurable 이 nil 일 때 UserIDFromConfig 가 "" 를 반환함을 검증한다.
func TestUserIDFromConfig_nil_Configurable_빈문자열을_반환한다(t *testing.T) {
	cfg := config.RunConfig{}
	got := store.UserIDFromConfig(cfg)
	if got != "" {
		t.Errorf("UserIDFromConfig(nil Configurable): 기대 \"\", 실제 %q", got)
	}
}

// TestUserIDFromConfig_값이_문자열아닐때_빈문자열을_반환한다 는
// Configurable["user_id"] 값이 문자열이 아닐 때 UserIDFromConfig 가 "" 를 반환함을 검증한다.
func TestUserIDFromConfig_값이_문자열아닐때_빈문자열을_반환한다(t *testing.T) {
	cfg := config.RunConfig{
		Configurable: map[string]any{
			"user_id": 12345, // 정수 — 문자열 변환 불가
		},
	}
	got := store.UserIDFromConfig(cfg)
	if got != "" {
		t.Errorf("UserIDFromConfig(비문자열): 기대 \"\", 실제 %q", got)
	}
}
