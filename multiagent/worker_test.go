// worker_test.go 는 WorkerRegistry 의 등록·조회·열거 동작을 결정적으로 검증한다.
// stub 워커를 사용하며 네트워크·LLM 호출이 없다.
package multiagent

import (
	"context"
	"testing"

	"github.com/zipkero/langgraph-go/agent"
	"github.com/zipkero/langgraph-go/config"
	"github.com/zipkero/langgraph-go/core"
	"github.com/zipkero/langgraph-go/message"
)

// stubWorker 는 테스트용 Worker 구현체다.
type stubWorker struct {
	name string
	desc string
}

func (s *stubWorker) Name() string        { return s.name }
func (s *stubWorker) Description() string { return s.desc }
func (s *stubWorker) Invoke(_ context.Context, in agent.Input, _ config.RunConfig) (WorkerOutput, error) {
	return WorkerOutput{Messages: in.Messages}, nil
}
func (s *stubWorker) Stream(_ context.Context, _ agent.Input, _ config.RunConfig, _ core.Mode) (<-chan agent.AgentEvent, error) {
	ch := make(chan agent.AgentEvent)
	close(ch)
	return ch, nil
}

// TestWorkerRegistry_RegisterAndGet 은 등록한 워커를 이름으로 정확히 조회하는지 검증한다.
func TestWorkerRegistry_RegisterAndGet(t *testing.T) {
	reg := NewWorkerRegistry()

	w := &stubWorker{name: "alpha", desc: "알파 워커"}
	if err := reg.RegisterWorker(w); err != nil {
		t.Fatalf("RegisterWorker 실패: %v", err)
	}

	got, ok := reg.GetWorker("alpha")
	if !ok {
		t.Fatal("GetWorker: 등록한 이름으로 조회 실패")
	}
	if got != w {
		t.Fatal("GetWorker: 반환된 워커가 등록한 워커와 다름")
	}
}

// TestWorkerRegistry_GetNotFound 는 미등록 이름 조회 시 not-found 가 구분되는지 검증한다.
func TestWorkerRegistry_GetNotFound(t *testing.T) {
	reg := NewWorkerRegistry()

	_, ok := reg.GetWorker("nonexistent")
	if ok {
		t.Fatal("GetWorker: 미등록 이름인데 found 반환")
	}
}

// TestWorkerRegistry_WorkerNames 는 등록된 모든 이름이 열거되는지 검증한다.
func TestWorkerRegistry_WorkerNames(t *testing.T) {
	reg := NewWorkerRegistry()

	workers := []*stubWorker{
		{name: "gamma", desc: "감마"},
		{name: "beta", desc: "베타"},
		{name: "alpha", desc: "알파"},
	}
	for _, w := range workers {
		if err := reg.RegisterWorker(w); err != nil {
			t.Fatalf("RegisterWorker(%q) 실패: %v", w.name, err)
		}
	}

	names := reg.WorkerNames()
	if len(names) != 3 {
		t.Fatalf("WorkerNames: 3개 기대, 실제 %d", len(names))
	}
	// WorkerNames 는 정렬된 결과를 반환한다.
	expected := []string{"alpha", "beta", "gamma"}
	for i, want := range expected {
		if names[i] != want {
			t.Errorf("WorkerNames[%d]: 기대 %q, 실제 %q", i, want, names[i])
		}
	}
}

// TestWorkerRegistry_DuplicateRegister 는 같은 이름으로 중복 등록 시 에러를 반환하는지 검증한다.
func TestWorkerRegistry_DuplicateRegister(t *testing.T) {
	reg := NewWorkerRegistry()

	w1 := &stubWorker{name: "dup", desc: "첫 번째"}
	w2 := &stubWorker{name: "dup", desc: "두 번째"}

	if err := reg.RegisterWorker(w1); err != nil {
		t.Fatalf("첫 RegisterWorker 실패: %v", err)
	}
	if err := reg.RegisterWorker(w2); err == nil {
		t.Fatal("중복 이름 RegisterWorker: 에러 기대, 그런데 nil 반환")
	}
}

// TestWorkerOutput_Fields 는 WorkerOutput 필드 접근이 올바른지 검증한다.
func TestWorkerOutput_Fields(t *testing.T) {
	msgs := []message.Message{message.NewUserMessage("안녕")}
	out := WorkerOutput{
		Messages:           msgs,
		StructuredResponse: "응답",
	}

	if len(out.Messages) != 1 {
		t.Errorf("Messages 길이: 기대 1, 실제 %d", len(out.Messages))
	}
	if out.StructuredResponse != "응답" {
		t.Errorf("StructuredResponse: 기대 '응답', 실제 %v", out.StructuredResponse)
	}
}

// TestStubWorker_Invoke 는 stub 워커의 Invoke 가 입력 메시지를 그대로 반환하는지 검증한다.
func TestStubWorker_Invoke(t *testing.T) {
	w := &stubWorker{name: "test-worker", desc: "테스트"}
	msgs := []message.Message{message.NewUserMessage("질문")}
	in := agent.Input{Messages: msgs}

	out, err := w.Invoke(context.Background(), in, config.RunConfig{})
	if err != nil {
		t.Fatalf("Invoke 에러: %v", err)
	}
	if len(out.Messages) != len(msgs) {
		t.Errorf("Messages 길이: 기대 %d, 실제 %d", len(msgs), len(out.Messages))
	}
}
