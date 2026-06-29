// worker.go 는 이름 붙은 실행 단위 추상화(Worker/WorkerOutput)와
// 이름 기반 등록·조회·열거를 제공하는 WorkerRegistry를 담는다.
// SPEC §5.2, ANALYSIS §1·§3 참조.
package multiagent

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/zipkero/langgraph-go/agent"
	"github.com/zipkero/langgraph-go/config"
	"github.com/zipkero/langgraph-go/core"
	"github.com/zipkero/langgraph-go/message"
)

// WorkerOutput 은 Worker.Invoke 가 반환하는 산출 타입이다.
// MergeWorkerResult 가 이를 받아 상태 메시지에 병합한다(SPEC §5.4).
type WorkerOutput struct {
	// Messages 는 워커 실행 후 누적된 대화 메시지 목록이다.
	Messages []message.Message
	// StructuredResponse 는 워커의 구조화 응답이다.
	// ResponseFormat 미지정이거나 생성 전이면 nil 이다.
	StructuredResponse any
}

// Worker 는 이름 붙은 실행 단위의 계약이다.
// 구체 구현은 AgentAsWorker 등 어댑터가 담당한다.
// Invoke/Stream 시그니처는 agent.Agent 의 공개 API와 정합하도록 맞춘다.
type Worker interface {
	// Name 은 레지스트리에서 이 워커를 식별하는 유일한 이름이다.
	Name() string
	// Description 은 수퍼바이저가 라우팅 판단에 사용하는 워커 설명이다.
	Description() string
	// Invoke 는 입력을 받아 동기적으로 실행하고 WorkerOutput 을 반환한다.
	Invoke(ctx context.Context, in agent.Input, cfg config.RunConfig) (WorkerOutput, error)
	// Stream 은 입력을 받아 비동기적으로 실행하고 AgentEvent 채널을 반환한다.
	Stream(ctx context.Context, in agent.Input, cfg config.RunConfig, mode core.Mode) (<-chan agent.AgentEvent, error)
}

// WorkerRegistry 는 Worker 를 이름으로 등록·조회·열거한다.
// 동시 접근에 안전하다(sync.RWMutex 보호).
type WorkerRegistry struct {
	mu      sync.RWMutex
	workers map[string]Worker
}

// NewWorkerRegistry 는 빈 WorkerRegistry 를 생성해 반환한다.
func NewWorkerRegistry() *WorkerRegistry {
	return &WorkerRegistry{
		workers: make(map[string]Worker),
	}
}

// RegisterWorker 는 w 를 레지스트리에 등록한다.
// 같은 이름이 이미 등록돼 있으면 에러를 반환한다.
func (r *WorkerRegistry) RegisterWorker(w Worker) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	name := w.Name()
	if _, exists := r.workers[name]; exists {
		return fmt.Errorf("multiagent: 워커 이름 %q 이 이미 등록되어 있습니다", name)
	}
	r.workers[name] = w
	return nil
}

// GetWorker 는 name 으로 등록된 Worker 를 반환한다.
// 미등록 이름이면 (nil, false) 를 반환한다.
func (r *WorkerRegistry) GetWorker(name string) (Worker, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	w, ok := r.workers[name]
	return w, ok
}

// WorkerNames 는 등록된 모든 워커 이름을 정렬된 순서로 반환한다.
func (r *WorkerRegistry) WorkerNames() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.workers))
	for name := range r.workers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
