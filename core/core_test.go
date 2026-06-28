package core_test

import (
	"testing"
	"time"

	"github.com/zipkero/langgraph-go/config"
	"github.com/zipkero/langgraph-go/core"
)

// TestState는 State 맵의 키-값 set/get을 검증한다.
func TestState(t *testing.T) {
	st := core.State{}
	st["messages"] = []string{"hello", "world"}
	st["count"] = 42

	msgs, ok := st["messages"]
	if !ok {
		t.Fatal("State: messages 키가 없다")
	}
	if got, want := msgs.([]string)[0], "hello"; got != want {
		t.Errorf("State[messages][0] = %q, want %q", got, want)
	}

	cnt, ok := st["count"]
	if !ok {
		t.Fatal("State: count 키가 없다")
	}
	if got, want := cnt.(int), 42; got != want {
		t.Errorf("State[count] = %d, want %d", got, want)
	}
}

// TestStateUpdate는 StateUpdate 맵의 키-값 set/get을 검증한다.
func TestStateUpdate(t *testing.T) {
	upd := core.StateUpdate{}
	upd["messages"] = []string{"new message"}
	upd["count"] = 100

	msgs, ok := upd["messages"]
	if !ok {
		t.Fatal("StateUpdate: messages 키가 없다")
	}
	if got, want := msgs.([]string)[0], "new message"; got != want {
		t.Errorf("StateUpdate[messages][0] = %q, want %q", got, want)
	}

	cnt, ok := upd["count"]
	if !ok {
		t.Fatal("StateUpdate: count 키가 없다")
	}
	if got, want := cnt.(int), 100; got != want {
		t.Errorf("StateUpdate[count] = %d, want %d", got, want)
	}
}

// TestModeConstants는 Mode 상수 값이 명세와 일치하는지 검증한다.
func TestModeConstants(t *testing.T) {
	cases := []struct {
		mode core.Mode
		want string
	}{
		{core.ModeValues, "values"},
		{core.ModeMessages, "messages"},
		{core.ModeUpdates, "updates"},
		{core.ModeDebug, "debug"},
	}
	for _, c := range cases {
		if string(c.mode) != c.want {
			t.Errorf("Mode = %q, want %q", c.mode, c.want)
		}
	}
}

// TestStateSnapshotFields는 StateSnapshot 각 필드의 구성·접근을 검증한다.
func TestStateSnapshotFields(t *testing.T) {
	now := time.Now()
	cfg := config.RunConfig{
		Configurable: map[string]any{
			"thread_id": "thread-123",
		},
	}

	snap := core.StateSnapshot{
		Values: core.State{
			"messages": []string{"hi"},
		},
		Next:   []string{"node_a", "node_b"},
		Config: cfg,
		Metadata: map[string]any{
			"step": 1,
		},
		CreatedAt: now,
	}

	// Values 필드
	if snap.Values == nil {
		t.Fatal("StateSnapshot.Values가 nil이다")
	}
	if _, ok := snap.Values["messages"]; !ok {
		t.Error("StateSnapshot.Values[messages] 키가 없다")
	}

	// Next 필드
	if len(snap.Next) != 2 {
		t.Errorf("StateSnapshot.Next 길이 = %d, want 2", len(snap.Next))
	}
	if snap.Next[0] != "node_a" {
		t.Errorf("StateSnapshot.Next[0] = %q, want %q", snap.Next[0], "node_a")
	}

	// Config 필드 — config.RunConfig 타입 대입 및 접근
	threadID, ok := snap.Config.Configurable["thread_id"]
	if !ok {
		t.Fatal("StateSnapshot.Config.Configurable[thread_id] 키가 없다")
	}
	if got, want := threadID.(string), "thread-123"; got != want {
		t.Errorf("StateSnapshot.Config.Configurable[thread_id] = %q, want %q", got, want)
	}

	// Metadata 필드
	step, ok := snap.Metadata["step"]
	if !ok {
		t.Fatal("StateSnapshot.Metadata[step] 키가 없다")
	}
	if got, want := step.(int), 1; got != want {
		t.Errorf("StateSnapshot.Metadata[step] = %d, want %d", got, want)
	}

	// CreatedAt 필드
	if !snap.CreatedAt.Equal(now) {
		t.Errorf("StateSnapshot.CreatedAt = %v, want %v", snap.CreatedAt, now)
	}
}

// TestStateSnapshotConfigType은 Config 필드에 config.RunConfig 값을 대입할 수 있음을 검증한다.
func TestStateSnapshotConfigType(t *testing.T) {
	rc := config.RunConfig{
		Configurable: map[string]any{
			"user_id": "user-456",
		},
	}
	snap := core.StateSnapshot{Config: rc}

	userID, ok := snap.Config.Configurable["user_id"]
	if !ok {
		t.Fatal("Config.Configurable[user_id] 키가 없다")
	}
	if got, want := userID.(string), "user-456"; got != want {
		t.Errorf("Config.Configurable[user_id] = %q, want %q", got, want)
	}
}
