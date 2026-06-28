package checkpoint_test

import (
	"context"
	"testing"
	"time"

	"github.com/zipkero/langgraph-go/checkpoint"
	"github.com/zipkero/langgraph-go/config"
	"github.com/zipkero/langgraph-go/core"
)

// makeConfig 는 주어진 threadID 로 RunConfig 를 생성하는 헬퍼다.
func makeConfig(threadID string) config.RunConfig {
	return config.RunConfig{
		Configurable: map[string]any{
			"thread_id": threadID,
		},
	}
}

// TestInMemorySaver_PutGet_RoundTrip 은 Put 후 Get 으로 동일 체크포인트를 복원한다.
func TestInMemorySaver_PutGet_RoundTrip(t *testing.T) {
	ctx := context.Background()
	saver := checkpoint.NewInMemorySaver()

	cp := checkpoint.Checkpoint{
		Values: core.State{"key": "value"},
		Next:   []string{"node_a"},
		Metadata: map[string]any{
			"step": 1,
		},
		CreatedAt: time.Now(),
	}

	if err := saver.Put(ctx, "thread-1", cp); err != nil {
		t.Fatalf("Put 실패: %v", err)
	}

	got, ok, err := saver.Get(ctx, "thread-1")
	if err != nil {
		t.Fatalf("Get 실패: %v", err)
	}
	if !ok {
		t.Fatal("Get 이 false 를 반환함 — 저장된 체크포인트가 없음")
	}

	if got.ThreadID != "thread-1" {
		t.Errorf("ThreadID 불일치: got %q, want %q", got.ThreadID, "thread-1")
	}
	if got.Values["key"] != "value" {
		t.Errorf("Values 불일치: got %v, want %q", got.Values["key"], "value")
	}
	if len(got.Next) != 1 || got.Next[0] != "node_a" {
		t.Errorf("Next 불일치: got %v", got.Next)
	}
}

// TestInMemorySaver_Get_NotFound 는 존재하지 않는 threadID 조회 시 false 를 반환한다.
func TestInMemorySaver_Get_NotFound(t *testing.T) {
	ctx := context.Background()
	saver := checkpoint.NewInMemorySaver()

	_, ok, err := saver.Get(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("예상치 못한 에러: %v", err)
	}
	if ok {
		t.Fatal("존재하지 않는 thread 에서 ok=true 반환")
	}
}

// TestInMemorySaver_List_History 는 동일 threadID 에 여러 체크포인트를 저장하고
// List 가 최신 순(역순)으로 이력을 반환하는지 확인한다.
func TestInMemorySaver_List_History(t *testing.T) {
	ctx := context.Background()
	saver := checkpoint.NewInMemorySaver()

	for i := 1; i <= 3; i++ {
		cp := checkpoint.Checkpoint{
			Values: core.State{"step": i},
		}
		if err := saver.Put(ctx, "thread-hist", cp); err != nil {
			t.Fatalf("Put(step=%d) 실패: %v", i, err)
		}
	}

	history, err := saver.List(ctx, "thread-hist")
	if err != nil {
		t.Fatalf("List 실패: %v", err)
	}
	if len(history) != 3 {
		t.Fatalf("이력 개수 불일치: got %d, want 3", len(history))
	}
	// 최신 순이므로 첫 번째가 step=3 이어야 한다.
	if history[0].Values["step"] != 3 {
		t.Errorf("이력[0].Values[\"step\"] 불일치: got %v, want 3", history[0].Values["step"])
	}
	if history[2].Values["step"] != 1 {
		t.Errorf("이력[2].Values[\"step\"] 불일치: got %v, want 1", history[2].Values["step"])
	}
}

// TestInMemorySaver_List_Empty 는 이력이 없는 threadID 에서 List 가 빈 슬라이스를 반환한다.
func TestInMemorySaver_List_Empty(t *testing.T) {
	ctx := context.Background()
	saver := checkpoint.NewInMemorySaver()

	history, err := saver.List(ctx, "empty-thread")
	if err != nil {
		t.Fatalf("List 실패: %v", err)
	}
	if len(history) != 0 {
		t.Errorf("비어 있어야 하는데 %d 개 반환됨", len(history))
	}
}

// TestInMemorySaver_DeleteThread 는 DeleteThread 후 Get/List 가 빈 결과를 반환한다.
func TestInMemorySaver_DeleteThread(t *testing.T) {
	ctx := context.Background()
	saver := checkpoint.NewInMemorySaver()

	cp := checkpoint.Checkpoint{
		Values: core.State{"x": 1},
	}
	if err := saver.Put(ctx, "thread-del", cp); err != nil {
		t.Fatalf("Put 실패: %v", err)
	}

	if err := saver.DeleteThread(ctx, "thread-del"); err != nil {
		t.Fatalf("DeleteThread 실패: %v", err)
	}

	_, ok, err := saver.Get(ctx, "thread-del")
	if err != nil {
		t.Fatalf("삭제 후 Get 에러: %v", err)
	}
	if ok {
		t.Fatal("삭제 후 Get 이 ok=true 를 반환함")
	}

	history, err := saver.List(ctx, "thread-del")
	if err != nil {
		t.Fatalf("삭제 후 List 에러: %v", err)
	}
	if len(history) != 0 {
		t.Errorf("삭제 후 List 에 %d 개가 남아 있음", len(history))
	}
}

// TestThreadIDFromConfig_Present 는 thread_id 가 있을 때 올바른 ID 를 반환한다.
func TestThreadIDFromConfig_Present(t *testing.T) {
	cfg := makeConfig("my-thread")
	id, err := checkpoint.ThreadIDFromConfig(cfg)
	if err != nil {
		t.Fatalf("예상치 못한 에러: %v", err)
	}
	if id != "my-thread" {
		t.Errorf("thread_id 불일치: got %q, want %q", id, "my-thread")
	}
}

// TestThreadIDFromConfig_Missing 은 thread_id 가 없을 때 ErrNoThreadID 를 반환한다.
func TestThreadIDFromConfig_Missing(t *testing.T) {
	cfg := config.RunConfig{}
	_, err := checkpoint.ThreadIDFromConfig(cfg)
	if err == nil {
		t.Fatal("에러를 반환해야 하는데 nil 이 반환됨")
	}
	if err != checkpoint.ErrNoThreadID {
		t.Errorf("에러 불일치: got %v, want ErrNoThreadID", err)
	}
}

// TestInMemorySaver_SaveState_LoadState 는 SaveState 로 저장한 상태를 LoadState 로 복원한다.
func TestInMemorySaver_SaveState_LoadState(t *testing.T) {
	ctx := context.Background()
	saver := checkpoint.NewInMemorySaver()
	cfg := makeConfig("thread-save")

	st := core.State{
		"messages": []string{"hello", "world"},
		"counter":  42,
	}

	if err := saver.SaveState(ctx, cfg, st); err != nil {
		t.Fatalf("SaveState 실패: %v", err)
	}

	loaded, ok, err := saver.LoadState(ctx, cfg)
	if err != nil {
		t.Fatalf("LoadState 실패: %v", err)
	}
	if !ok {
		t.Fatal("LoadState 가 false 를 반환함 — 저장된 상태가 없음")
	}
	if loaded["counter"] != 42 {
		t.Errorf("counter 불일치: got %v, want 42", loaded["counter"])
	}
}

// TestInMemorySaver_LoadState_NoThreadID 는 thread_id 없는 cfg 로 LoadState 호출 시 에러를 반환한다.
func TestInMemorySaver_LoadState_NoThreadID(t *testing.T) {
	ctx := context.Background()
	saver := checkpoint.NewInMemorySaver()

	_, _, err := saver.LoadState(ctx, config.RunConfig{})
	if err == nil {
		t.Fatal("에러를 반환해야 하는데 nil 이 반환됨")
	}
	if err != checkpoint.ErrNoThreadID {
		t.Errorf("에러 불일치: got %v, want ErrNoThreadID", err)
	}
}

// TestInMemorySaver_SaveState_NoThreadID 는 thread_id 없는 cfg 로 SaveState 호출 시 에러를 반환한다.
func TestInMemorySaver_SaveState_NoThreadID(t *testing.T) {
	ctx := context.Background()
	saver := checkpoint.NewInMemorySaver()

	err := saver.SaveState(ctx, config.RunConfig{}, core.State{})
	if err == nil {
		t.Fatal("에러를 반환해야 하는데 nil 이 반환됨")
	}
	if err != checkpoint.ErrNoThreadID {
		t.Errorf("에러 불일치: got %v, want ErrNoThreadID", err)
	}
}

// TestInMemorySaver_Get_LatestCheckpoint 는 여러 체크포인트 중 Get 이 가장 최신 것을 반환한다.
func TestInMemorySaver_Get_LatestCheckpoint(t *testing.T) {
	ctx := context.Background()
	saver := checkpoint.NewInMemorySaver()

	for i := 1; i <= 5; i++ {
		cp := checkpoint.Checkpoint{
			Values: core.State{"step": i},
		}
		if err := saver.Put(ctx, "thread-latest", cp); err != nil {
			t.Fatalf("Put(step=%d) 실패: %v", i, err)
		}
	}

	got, ok, err := saver.Get(ctx, "thread-latest")
	if err != nil {
		t.Fatalf("Get 실패: %v", err)
	}
	if !ok {
		t.Fatal("Get 이 false 반환")
	}
	if got.Values["step"] != 5 {
		t.Errorf("최신 step 불일치: got %v, want 5", got.Values["step"])
	}
}

// TestInMemorySaver_InterfaceCompliance 는 InMemorySaver 가 Checkpointer 인터페이스를 만족하는지 컴파일 타임에 확인한다.
func TestInMemorySaver_InterfaceCompliance(t *testing.T) {
	var _ checkpoint.Checkpointer = checkpoint.NewInMemorySaver()
}
