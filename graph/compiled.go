// compiled.go 는 Compile이 반환하는 불변 실행 결과 타입과 그 메서드를 정의한다.
// Invoke는 task-004에서 구현됐으며, exec.go의 invokeLoop를 위임한다.
// Stream은 task-010에서 구현됐으며, stream.go의 streamLoop를 goroutine으로 실행한다.
// GetState/GetStateHistory/UpdateState는 task-011에서 구현됐으며 체크포인터와 연동한다.
// DrawMermaid는 task-012에서 구현된다.
package graph

import (
	"context"
	"fmt"
	"time"

	"github.com/zipkero/langgraph-go/checkpoint"
	"github.com/zipkero/langgraph-go/config"
	"github.com/zipkero/langgraph-go/core"
)

// Compiled 는 Compile이 반환하는 불변 그래프 실행 결과다.
// 빌드 시 누적된 노드·엣지·진입점·스키마 정보를 고정해 보유하며,
// 실행 루프(Invoke/Stream)와 상태 조회(GetState/GetStateHistory/UpdateState)를 제공한다.
type Compiled struct {
	schema       StateSchema
	schemaOpts   schemaOptions
	nodes        map[string]nodeEntry
	edges        []edgeEntry
	condEdges    []conditionalEdgeEntry
	entryPoint   string
	condEntry    *conditionalEntryPoint
	checkpointer checkpoint.Checkpointer
	maxSteps     int
}

// Invoke 는 input을 초기 상태로 그래프를 실행하고 최종 State를 반환한다.
// 진입점부터 정적 엣지를 따라 노드를 순차 실행하며, 각 노드의 StateUpdate를
// 스키마의 필드별 리듀서(미등록 필드는 last-write-wins)로 병합한다(§2.1, §2.4 D4).
func (c *Compiled) Invoke(ctx context.Context, input State, cfg config.RunConfig) (State, error) {
	// 노드 함수가 시그니처 변경 없이 실행별 설정에 접근할 수 있도록 ctx 에 주입한다.
	// 서브그래프 내부 경로는 이 진입점을 다시 거치지 않으므로 부모 주입값이 그대로 흐른다.
	ctx = config.WithRunConfig(ctx, cfg)
	return invokeLoop(ctx, c, input, cfg)
}

// Stream 은 Invoke와 같은 루프를 돌되 GraphEvent를 채널로 방출한다(task-010, §2.6).
// 루프는 goroutine에서 실행되며 채널은 루프 완료(또는 오류·ctx 취소) 시 닫힌다.
//
// mode별 방출 페이로드:
//   - ModeValues:   각 노드 실행 후 전체 상태 스냅샷(GraphEvent.Value)
//   - ModeUpdates:  각 노드의 StateUpdate(GraphEvent.Update)
//   - ModeMessages: 노드가 StreamTokens(ctx) 채널로 보낸 토큰(GraphEvent.Token)
//   - ModeDebug:    노드 진입/이탈 진단 이벤트(GraphEvent.Metadata)
//
// WithSubgraphs() 옵션을 전달하면 서브그래프 이벤트의 GraphEvent.Path가 채워진다.
// 서브그래프 노드는 AsStreamNode를 통해 자동으로 스트리밍 어댑터로 교체된다.
func (c *Compiled) Stream(ctx context.Context, input State, cfg config.RunConfig, mode core.Mode, sopts ...StreamOption) (<-chan GraphEvent, error) {
	// Invoke 와 동일하게 노드 함수용 실행별 설정을 ctx 에 주입한다.
	ctx = config.WithRunConfig(ctx, cfg)
	so := streamOptions{}
	for _, opt := range sopts {
		opt(&so)
	}

	out := make(chan GraphEvent, 32)

	go func() {
		defer close(out)

		opts := streamLoopOpts{
			mode:  mode,
			sOpts: so,
			out:   out,
		}

		// Subgraphs 옵션이 켜진 경우: 서브그래프 노드를 AsStreamNode 어댑터로 교체한 임시 Compiled를 사용한다.
		// 이는 서브그래프 이벤트에 path를 채워 방출하기 위함이다.
		compiled := c
		if so.subgraphs {
			compiled = c.withStreamSubgraphNodes(opts)
		}

		_, err := streamLoop(ctx, compiled, input, cfg, opts)
		if err != nil {
			// 오류 이벤트 방출: 수신자가 오류를 인지할 수 있도록 Metadata에 담는다.
			// ctx가 이미 취소된 경우 방출하지 않는다.
			select {
			case <-ctx.Done():
			case out <- GraphEvent{
				Mode:     mode,
				Metadata: map[string]any{"error": err.Error()},
			}:
			}
		}
	}()

	return out, nil
}

// withStreamSubgraphNodes 는 Subgraphs 옵션이 켜진 경우 서브그래프 노드를
// AsStreamNode 어댑터로 교체한 새 Compiled를 반환한다.
// 원본 Compiled는 수정하지 않는다(불변성 보장).
func (c *Compiled) withStreamSubgraphNodes(opts streamLoopOpts) *Compiled {
	newNodes := make(map[string]nodeEntry, len(c.nodes))
	for name, entry := range c.nodes {
		// 노드 함수가 서브그래프 어댑터(AsNode로 생성된)인지 판별하는 방법이 없으므로,
		// 모든 노드의 함수를 그대로 사용하되 AsStreamNode 교체는 사용자가 직접 등록한
		// Compiled 노드(AsNode 반환값)에만 적용해야 한다.
		// 현재 구조에서 NodeFunc이 Compiled에서 생성됐는지 판별할 수 없으므로,
		// 이 메서드는 nodeEntry에 subCompiled 참조를 보유하는 확장을 통해 구현한다.
		// task-010 범위에서는 nodeEntry에 subCompiled 필드를 추가해 Subgraphs 전파를 지원한다.
		if entry.subCompiled != nil {
			// 서브그래프 노드: AsStreamNode 어댑터로 교체
			streamFn := entry.subCompiled.AsStreamNode(name, opts)
			newNodes[name] = nodeEntry{
				name:         entry.name,
				fn:           streamFn,
				destinations: entry.destinations,
				subCompiled:  entry.subCompiled,
			}
		} else {
			newNodes[name] = entry
		}
	}

	return &Compiled{
		schema:       c.schema,
		schemaOpts:   c.schemaOpts,
		nodes:        newNodes,
		edges:        c.edges,
		condEdges:    c.condEdges,
		entryPoint:   c.entryPoint,
		condEntry:    c.condEntry,
		checkpointer: c.checkpointer,
		maxSteps:     c.maxSteps,
	}
}

// GetState 는 cfg에서 thread_id를 꺼내 현재 상태 스냅샷을 반환한다(SPEC §5.9, D8).
// 체크포인터가 없으면 error를 반환한다.
// 체크포인터가 있으나 해당 thread_id에 체크포인트가 없으면 빈 스냅샷을 반환한다.
func (c *Compiled) GetState(cfg config.RunConfig) (StateSnapshot, error) {
	if c.checkpointer == nil {
		return StateSnapshot{}, fmt.Errorf("graph: GetState는 체크포인터가 설정된 경우에만 사용할 수 있습니다")
	}

	threadID, err := checkpoint.ThreadIDFromConfig(cfg)
	if err != nil {
		return StateSnapshot{}, fmt.Errorf("graph: GetState: %w", err)
	}

	ctx := context.Background()
	cp, ok, err := c.checkpointer.Get(ctx, threadID)
	if err != nil {
		return StateSnapshot{}, fmt.Errorf("graph: GetState: 체크포인트 조회 실패: %w", err)
	}
	if !ok {
		// 체크포인트가 없으면 빈 스냅샷 반환
		return StateSnapshot{
			Config:    cfg,
			CreatedAt: time.Time{},
		}, nil
	}

	return StateSnapshot{
		Values:    cp.Values,
		Next:      cp.Next,
		Config:    cfg,
		Metadata:  cp.Metadata,
		CreatedAt: cp.CreatedAt,
	}, nil
}

// GetStateHistory 는 cfg에서 thread_id를 꺼내 상태 이력을 최신 순으로 반환한다(SPEC §5.9, D8).
// 체크포인터가 없으면 error를 반환한다.
func (c *Compiled) GetStateHistory(cfg config.RunConfig) ([]StateSnapshot, error) {
	if c.checkpointer == nil {
		return nil, fmt.Errorf("graph: GetStateHistory는 체크포인터가 설정된 경우에만 사용할 수 있습니다")
	}

	threadID, err := checkpoint.ThreadIDFromConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("graph: GetStateHistory: %w", err)
	}

	ctx := context.Background()
	cps, err := c.checkpointer.List(ctx, threadID)
	if err != nil {
		return nil, fmt.Errorf("graph: GetStateHistory: 체크포인트 이력 조회 실패: %w", err)
	}

	snapshots := make([]StateSnapshot, 0, len(cps))
	for _, cp := range cps {
		snapshots = append(snapshots, StateSnapshot{
			Values:    cp.Values,
			Next:      cp.Next,
			Config:    cfg,
			Metadata:  cp.Metadata,
			CreatedAt: cp.CreatedAt,
		})
	}
	return snapshots, nil
}

// UpdateState 는 cfg에서 thread_id를 꺼내 update를 현재 상태에 병합하고 새 체크포인트로 저장한다(SPEC §5.9, D8).
// 체크포인터가 없으면 error를 반환한다.
// 기존 상태가 없으면 update만으로 새 체크포인트를 만든다.
func (c *Compiled) UpdateState(cfg config.RunConfig, update StateUpdate) error {
	if c.checkpointer == nil {
		return fmt.Errorf("graph: UpdateState는 체크포인터가 설정된 경우에만 사용할 수 있습니다")
	}

	threadID, err := checkpoint.ThreadIDFromConfig(cfg)
	if err != nil {
		return fmt.Errorf("graph: UpdateState: %w", err)
	}

	ctx := context.Background()

	// 현재 상태를 로드해 update와 병합한다.
	var baseState State
	cp, ok, err := c.checkpointer.Get(ctx, threadID)
	if err != nil {
		return fmt.Errorf("graph: UpdateState: 현재 상태 조회 실패: %w", err)
	}
	if ok && cp.Values != nil {
		baseState = make(State, len(cp.Values))
		for k, v := range cp.Values {
			baseState[k] = v
		}
	} else {
		baseState = make(State)
	}

	// update를 스키마 리듀서로 병합한다.
	merged := applyReducers(baseState, update, c.schema)

	// 병합된 상태를 새 체크포인트로 저장한다.
	newCp := checkpoint.Checkpoint{
		ThreadID: threadID,
		Values:   merged,
	}
	if err := c.checkpointer.Put(ctx, threadID, newCp); err != nil {
		return fmt.Errorf("graph: UpdateState: 체크포인트 저장 실패: %w", err)
	}
	return nil
}

// DrawMermaid 는 컴파일된 그래프의 mermaid flowchart 텍스트를 반환한다(SPEC §5.12, D10).
// 노드·정적 엣지·조건 엣지·진입점을 flowchart TD 형식으로 렌더한다.
// DrawMermaidPNG는 정의하지 않는다(SPEC §4).
func (c *Compiled) DrawMermaid() string {
	return drawMermaid(c)
}
