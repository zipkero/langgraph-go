// checkpoint 패키지는 스레드 단위 상태 영속화(체크포인팅)를 담당한다.
// core·config만 의존하며, graph·agent 등 상위 패키지를 참조하지 않는다.
// InMemorySaver 는 메모리 기반 구현이며, 동시 접근을 mutex 로 보호한다.
package checkpoint

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/zipkero/langgraph-go/config"
	"github.com/zipkero/langgraph-go/core"
)

// StateSnapshot 은 core.StateSnapshot 의 타입 alias 다.
// graph·checkpoint 가 graph 를 import 하지 않고 이 타입을 공유하기 위해 alias 로 정의한다.
type StateSnapshot = core.StateSnapshot

// Checkpoint 는 특정 스레드의 단일 체크포인트 레코드다.
// Put/Get/List 에서 저장·조회 단위로 쓰인다.
type Checkpoint struct {
	// ThreadID 는 이 체크포인트가 속한 스레드 식별자다.
	ThreadID string
	// Values 는 체크포인트 시점의 전체 상태 맵이다.
	Values core.State
	// Next 는 다음에 실행될 노드 이름 목록이다.
	Next []string
	// Metadata 는 체크포인트 ID·단계 번호 등 실행 메타데이터를 담는 범용 맵이다.
	Metadata map[string]any
	// CreatedAt 은 체크포인트가 생성된 시각이다.
	CreatedAt time.Time
	// ParentConfig 는 이 체크포인트를 생성한 직전 실행 설정이다.
	// 첫 번째 체크포인트는 nil 이다.
	ParentConfig *config.RunConfig
}

// Checkpointer 는 스레드 단위 체크포인트 저장소의 인터페이스다.
// InMemorySaver 를 포함한 모든 구현체가 이 인터페이스를 만족해야 한다.
type Checkpointer interface {
	// Get 은 threadID 에 해당하는 가장 최신 체크포인트를 반환한다.
	// 체크포인트가 없으면 (Checkpoint{}, false, nil) 을 반환한다.
	Get(ctx context.Context, threadID string) (Checkpoint, bool, error)

	// Put 은 threadID 에 체크포인트를 저장(추가)한다.
	Put(ctx context.Context, threadID string, cp Checkpoint) error

	// List 는 threadID 에 저장된 체크포인트 이력을 생성 역순(최신 먼저)으로 반환한다.
	List(ctx context.Context, threadID string) ([]Checkpoint, error)

	// DeleteThread 는 threadID 에 속한 모든 체크포인트를 삭제한다.
	DeleteThread(ctx context.Context, threadID string) error
}

// InMemorySaver 는 메모리 맵 기반의 Checkpointer 구현체다.
// 동시 접근은 mu(RWMutex)로 보호한다.
type InMemorySaver struct {
	mu   sync.RWMutex
	data map[string][]Checkpoint // threadID → 체크포인트 이력(오래된 순)
}

// NewInMemorySaver 는 초기화된 InMemorySaver 를 반환한다.
func NewInMemorySaver() *InMemorySaver {
	return &InMemorySaver{
		data: make(map[string][]Checkpoint),
	}
}

// Get 은 threadID 에 해당하는 가장 최신 체크포인트를 반환한다.
// 체크포인트가 없으면 (Checkpoint{}, false, nil) 을 반환한다.
func (s *InMemorySaver) Get(ctx context.Context, threadID string) (Checkpoint, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	history, ok := s.data[threadID]
	if !ok || len(history) == 0 {
		return Checkpoint{}, false, nil
	}
	// 마지막 요소가 가장 최신이다.
	return history[len(history)-1], true, nil
}

// Put 은 threadID 에 체크포인트를 추가 저장한다.
// cp.CreatedAt 이 zero 이면 현재 시각으로 채운다.
func (s *InMemorySaver) Put(ctx context.Context, threadID string, cp Checkpoint) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if cp.CreatedAt.IsZero() {
		cp.CreatedAt = time.Now()
	}
	cp.ThreadID = threadID
	s.data[threadID] = append(s.data[threadID], cp)
	return nil
}

// List 는 threadID 에 저장된 체크포인트 이력을 최신 순(역순)으로 반환한다.
// 이력이 없으면 빈 슬라이스를 반환한다.
func (s *InMemorySaver) List(ctx context.Context, threadID string) ([]Checkpoint, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	history, ok := s.data[threadID]
	if !ok || len(history) == 0 {
		return []Checkpoint{}, nil
	}
	// 복사본을 만들어 역순으로 반환한다.
	result := make([]Checkpoint, len(history))
	for i, cp := range history {
		result[len(history)-1-i] = cp
	}
	return result, nil
}

// DeleteThread 는 threadID 에 속한 모든 체크포인트를 삭제한다.
func (s *InMemorySaver) DeleteThread(ctx context.Context, threadID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.data, threadID)
	return nil
}

// ErrNoThreadID 는 RunConfig 에 thread_id 가 없을 때 반환되는 에러다.
var ErrNoThreadID = errors.New("checkpoint: RunConfig 에 thread_id 가 없음")

// ThreadIDFromConfig 는 cfg.Configurable 에서 thread_id 를 뽑아 반환한다.
// thread_id 가 없거나 빈 문자열이면 ErrNoThreadID 를 반환한다.
func ThreadIDFromConfig(cfg config.RunConfig) (string, error) {
	id := config.GetThreadID(cfg)
	if id == "" {
		return "", ErrNoThreadID
	}
	return id, nil
}

// LoadState 는 cfg 에서 thread_id 를 뽑아 saver 에서 가장 최신 상태를 조회한다.
// 체크포인트가 없으면 (nil, false, nil) 을 반환한다.
// thread_id 가 없으면 ErrNoThreadID 를 반환한다.
func (s *InMemorySaver) LoadState(ctx context.Context, cfg config.RunConfig) (core.State, bool, error) {
	threadID, err := ThreadIDFromConfig(cfg)
	if err != nil {
		return nil, false, err
	}

	cp, ok, err := s.Get(ctx, threadID)
	if err != nil || !ok {
		return nil, false, err
	}
	return cp.Values, true, nil
}

// SaveState 는 cfg 에서 thread_id 를 뽑아 st 를 새 체크포인트로 저장한다.
// thread_id 가 없으면 ErrNoThreadID 를 반환한다.
func (s *InMemorySaver) SaveState(ctx context.Context, cfg config.RunConfig, st core.State) error {
	threadID, err := ThreadIDFromConfig(cfg)
	if err != nil {
		return err
	}

	cp := Checkpoint{
		ThreadID:  threadID,
		Values:    st,
		CreatedAt: time.Now(),
	}
	return s.Put(ctx, threadID, cp)
}
