// stream.go 는 Compiled.Stream의 실행 루프와 StreamOption을 정의한다.
// Invoke와 같은 루프를 공유하되 GraphEvent를 채널로 방출한다(§2.6, SPEC §5.8).
//
// messages 모드에서 토큰을 노드에서 엔진으로 전달하는 방법:
//   - streamLoop가 context에 토큰 채널(chan string)을 주입한다.
//   - 노드는 StreamTokens(ctx)로 채널을 꺼내 토큰을 보낼 수 있다.
//   - 엔진은 노드 실행 전 별도 goroutine에서 채널을 읽어 GraphEvent로 방출한다.
//   - graph는 streaming을 import하지 않으며, 토큰 채널 키 타입도 graph 패키지 내부에 둔다.
package graph

import (
	"context"
	"fmt"

	"github.com/zipkero/langgraph-go/checkpoint"
	"github.com/zipkero/langgraph-go/config"
	"github.com/zipkero/langgraph-go/core"
	"github.com/zipkero/langgraph-go/graph/command"
)

// streamOptions 는 Stream에 전달하는 옵션을 축적하는 내부 구조체다.
type streamOptions struct {
	// subgraphs 가 true이면 서브그래프 이벤트를 경로(Path)와 함께 방출한다(§2.6).
	subgraphs bool
}

// StreamOption 은 Stream에 전달하는 옵션 함수 타입이다.
type StreamOption func(*streamOptions)

// WithSubgraphs 는 서브그래프 이벤트를 경로(Path)와 함께 전파하는 옵션이다.
// Stream에 이 옵션을 전달하면 서브그래프 이벤트의 GraphEvent.Path가 채워진다(§2.6).
func WithSubgraphs() StreamOption {
	return func(o *streamOptions) {
		o.subgraphs = true
	}
}

// tokenChanKeyType 은 context에 토큰 채널을 주입할 때 사용하는 내부 키 타입이다.
// unexported로 graph 패키지 외부에서 접근할 수 없다.
type tokenChanKeyType struct{}

// tokenChanKey 는 context.WithValue에서 사용하는 싱글턴 키다.
var tokenChanKey = tokenChanKeyType{}

// contextWithTokenChan 은 ctx에 토큰 채널을 주입한 새 context를 반환한다.
// streamLoop가 노드 실행 전에 호출한다.
func contextWithTokenChan(ctx context.Context, ch chan string) context.Context {
	return context.WithValue(ctx, tokenChanKey, ch)
}

// StreamTokens 는 ctx에 주입된 토큰 채널을 반환한다.
// messages 모드에서 노드 코드가 이 채널로 토큰을 보내면 엔진이 GraphEvent로 방출한다.
// Stream 외부에서 호출한 경우(채널이 없는 경우) nil을 반환하므로 전송 전 nil 체크가 필요하다.
func StreamTokens(ctx context.Context) chan<- string {
	if ch, ok := ctx.Value(tokenChanKey).(chan string); ok {
		return ch
	}
	return nil
}

// streamLoopOpts 는 streamLoop에 전달하는 설정 묶음이다.
// invokeLoop와의 공유 경계를 분리해 매개변수 증가를 억제한다.
type streamLoopOpts struct {
	mode      core.Mode
	sOpts     streamOptions
	out       chan<- GraphEvent
	pathPrefix []string // 서브그래프 경로 접두어(부모→서브그래프 전파 시 채워진다)
}

// streamLoop 는 Stream의 실행 루프 본체다.
// invokeLoop와 동일한 순서로 노드를 실행하되 단계마다 mode에 따라 GraphEvent를 out에 방출한다.
// ctx가 취소되면 방출을 멈추고 루프를 종료한다.
//
// 방출 시점과 페이로드(§2.6):
//   - ModeValues:   applyReducers 직후 전체 상태 스냅샷 방출
//   - ModeUpdates:  applyReducers 입력 update를 방출
//   - ModeMessages: 노드 실행 중 노드가 context 채널로 보낸 토큰을 방출
//   - ModeDebug:    노드 진입 전·후에 진단 이벤트 방출
func streamLoop(ctx context.Context, compiled *Compiled, input State, cfg config.RunConfig, opts streamLoopOpts) (State, error) {
	// 체크포인터 로드: thread_id에 저장된 기존 상태를 로드해 input과 병합한다(SPEC §5.9).
	if compiled.checkpointer != nil {
		threadID, err := checkpoint.ThreadIDFromConfig(cfg)
		if err == nil && threadID != "" {
			cp, ok, loadErr := compiled.checkpointer.Get(ctx, threadID)
			if loadErr == nil && ok && cp.Values != nil {
				merged := make(State, len(cp.Values)+len(input))
				for k, v := range cp.Values {
					merged[k] = v
				}
				for k, v := range input {
					merged[k] = v
				}
				input = merged
			}
		}
	}

	// 입력 스키마 필터링
	filtered := filterBySchema(input, compiled.schemaOpts.inputSchema)
	state := make(State, len(filtered))
	for k, v := range filtered {
		state[k] = v
	}

	// 진입점 결정
	var current string
	if compiled.condEntry != nil {
		key := compiled.condEntry.router(ctx, state)
		target, ok := compiled.condEntry.mapping[key]
		if !ok {
			return nil, fmt.Errorf("graph: 조건 진입점 라우터가 반환한 키 %q가 mapping에 없습니다", key)
		}
		current = target
	} else {
		current = compiled.entryPoint
	}

	for step := 0; current != ""; step++ {
		// ctx 취소 감지
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		if err := checkMaxSteps(step, compiled.maxSteps); err != nil {
			return nil, err
		}

		// debug 모드: 노드 진입 이벤트 방출
		if opts.mode == core.ModeDebug {
			evt := GraphEvent{
				Node:     current,
				Mode:     core.ModeDebug,
				Metadata: map[string]any{"event": "node_enter", "step": step},
			}
			if len(opts.pathPrefix) > 0 {
				evt.Path = opts.pathPrefix
			}
			if !emitEvent(ctx, opts.out, evt) {
				return nil, ctx.Err()
			}
		}

		// messages 모드: 토큰 채널을 context에 주입하고 별도 goroutine에서 토큰 방출
		var tokenCh chan string
		var tokenDone chan struct{}
		if opts.mode == core.ModeMessages {
			tokenCh = make(chan string, 64)
			tokenDone = make(chan struct{})
			nodeName := current // goroutine 캡처용 로컬 복사
			pathCopy := append([]string(nil), opts.pathPrefix...)
			go func() {
				defer close(tokenDone)
				for token := range tokenCh {
					evt := GraphEvent{
						Node:  nodeName,
						Mode:  core.ModeMessages,
						Token: token,
					}
					if len(pathCopy) > 0 {
						evt.Path = pathCopy
					}
					if !emitEvent(ctx, opts.out, evt) {
						// ctx가 취소된 경우 남은 토큰은 버린다
						for range tokenCh {
						}
						return
					}
				}
			}()
			ctx = contextWithTokenChan(ctx, tokenCh)
		}

		res, err := runNode(ctx, current, compiled.nodes, state)

		// messages 모드: 노드 완료 후 토큰 채널 닫고 goroutine 종료 대기
		if opts.mode == core.ModeMessages && tokenCh != nil {
			close(tokenCh)
			<-tokenDone
			// context에서 토큰 채널 제거(다음 노드는 새 채널을 받아야 하므로)
			ctx = context.WithValue(ctx, tokenChanKey, (*chan string)(nil))
		}

		if err != nil {
			return nil, err
		}

		// updates 모드: applyReducers 전에 update 방출
		if opts.mode == core.ModeUpdates && res.Update != nil {
			updateCopy := make(StateUpdate, len(res.Update))
			for k, v := range res.Update {
				updateCopy[k] = v
			}
			evt := GraphEvent{
				Node:   current,
				Mode:   core.ModeUpdates,
				Update: updateCopy,
			}
			if len(opts.pathPrefix) > 0 {
				evt.Path = opts.pathPrefix
			}
			if !emitEvent(ctx, opts.out, evt) {
				return nil, ctx.Err()
			}
		}

		state = applyReducers(state, res.Update, compiled.schema)

		// 체크포인터 저장: applyReducers 직후 현재 state를 thread_id에 저장한다(SPEC §5.9).
		checkpointSave(ctx, compiled, cfg, state)

		// values 모드: applyReducers 직후 전체 상태 스냅샷 방출
		if opts.mode == core.ModeValues {
			valueCopy := make(State, len(state))
			for k, v := range state {
				valueCopy[k] = v
			}
			evt := GraphEvent{
				Node:  current,
				Mode:  core.ModeValues,
				Value: valueCopy,
			}
			if len(opts.pathPrefix) > 0 {
				evt.Path = opts.pathPrefix
			}
			if !emitEvent(ctx, opts.out, evt) {
				return nil, ctx.Err()
			}
		}

		// debug 모드: 노드 이탈 이벤트 방출
		if opts.mode == core.ModeDebug {
			evt := GraphEvent{
				Node:     current,
				Mode:     core.ModeDebug,
				Metadata: map[string]any{"event": "node_exit", "step": step},
			}
			if len(opts.pathPrefix) > 0 {
				evt.Path = opts.pathPrefix
			}
			if !emitEvent(ctx, opts.out, evt) {
				return nil, ctx.Err()
			}
		}

		next, err := resolveNext(ctx, current, res, state, compiled.edges, compiled.condEdges, compiled.nodes)
		if err != nil {
			return nil, err
		}

		// Fanout 처리
		if len(next.sends) > 0 {
			for _, se := range next.sends {
				branchState, branchErr := streamFromNode(ctx, compiled, se.target, se.state, step+1, cfg, opts)
				if branchErr != nil {
					return nil, fmt.Errorf("graph: Stream Fanout 분기 %q 실행 실패: %w", se.target, branchErr)
				}
				for k, v := range branchState {
					state[k] = v
				}
			}
			break
		}

		current = next.target
	}

	return filterBySchema(state, compiled.schemaOpts.outputSchema), nil
}

// streamFromNode 는 스트리밍 모드의 Fanout 분기 실행 헬퍼다.
// invokeLoop의 runFromNode에 대응하며 GraphEvent 방출을 포함한다.
func streamFromNode(ctx context.Context, compiled *Compiled, startNode string, initState State, stepOffset int, cfg config.RunConfig, opts streamLoopOpts) (State, error) {
	state := make(State, len(initState))
	for k, v := range initState {
		state[k] = v
	}

	current := startNode
	for step := stepOffset; current != ""; step++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		if err := checkMaxSteps(step, compiled.maxSteps); err != nil {
			return nil, err
		}

		if opts.mode == core.ModeDebug {
			evt := GraphEvent{
				Node:     current,
				Mode:     core.ModeDebug,
				Metadata: map[string]any{"event": "node_enter", "step": step},
			}
			if len(opts.pathPrefix) > 0 {
				evt.Path = opts.pathPrefix
			}
			emitEvent(ctx, opts.out, evt)
		}

		var tokenCh chan string
		var tokenDone chan struct{}
		if opts.mode == core.ModeMessages {
			tokenCh = make(chan string, 64)
			tokenDone = make(chan struct{})
			nodeName := current
			pathCopy := append([]string(nil), opts.pathPrefix...)
			go func() {
				defer close(tokenDone)
				for token := range tokenCh {
					evt := GraphEvent{
						Node:  nodeName,
						Mode:  core.ModeMessages,
						Token: token,
					}
					if len(pathCopy) > 0 {
						evt.Path = pathCopy
					}
					emitEvent(ctx, opts.out, evt)
				}
			}()
			ctx = contextWithTokenChan(ctx, tokenCh)
		}

		res, err := runNode(ctx, current, compiled.nodes, state)

		if opts.mode == core.ModeMessages && tokenCh != nil {
			close(tokenCh)
			<-tokenDone
			ctx = context.WithValue(ctx, tokenChanKey, (*chan string)(nil))
		}

		if err != nil {
			return nil, err
		}

		if opts.mode == core.ModeUpdates && res.Update != nil {
			updateCopy := make(StateUpdate, len(res.Update))
			for k, v := range res.Update {
				updateCopy[k] = v
			}
			evt := GraphEvent{
				Node:   current,
				Mode:   core.ModeUpdates,
				Update: updateCopy,
			}
			if len(opts.pathPrefix) > 0 {
				evt.Path = opts.pathPrefix
			}
			emitEvent(ctx, opts.out, evt)
		}

		state = applyReducers(state, res.Update, compiled.schema)

		if opts.mode == core.ModeValues {
			valueCopy := make(State, len(state))
			for k, v := range state {
				valueCopy[k] = v
			}
			evt := GraphEvent{
				Node:  current,
				Mode:  core.ModeValues,
				Value: valueCopy,
			}
			if len(opts.pathPrefix) > 0 {
				evt.Path = opts.pathPrefix
			}
			emitEvent(ctx, opts.out, evt)
		}

		if opts.mode == core.ModeDebug {
			evt := GraphEvent{
				Node:     current,
				Mode:     core.ModeDebug,
				Metadata: map[string]any{"event": "node_exit", "step": step},
			}
			if len(opts.pathPrefix) > 0 {
				evt.Path = opts.pathPrefix
			}
			emitEvent(ctx, opts.out, evt)
		}

		next, err := resolveNext(ctx, current, res, state, compiled.edges, compiled.condEdges, compiled.nodes)
		if err != nil {
			return nil, err
		}

		if len(next.sends) > 0 {
			for _, se := range next.sends {
				branchState, branchErr := streamFromNode(ctx, compiled, se.target, se.state, step+1, cfg, opts)
				if branchErr != nil {
					return nil, fmt.Errorf("graph: 중첩 Stream Fanout 분기 %q 실행 실패: %w", se.target, branchErr)
				}
				for k, v := range branchState {
					state[k] = v
				}
			}
			break
		}

		current = next.target
	}

	return state, nil
}

// emitEvent 는 GraphEvent를 out 채널로 방출한다.
// ctx가 취소되거나 채널이 닫힌 경우 false를 반환한다.
func emitEvent(ctx context.Context, out chan<- GraphEvent, evt GraphEvent) bool {
	select {
	case <-ctx.Done():
		return false
	case out <- evt:
		return true
	}
}

// streamSubgraphLoop 는 Subgraphs 옵션이 켜진 경우 서브그래프에서 사용하는 스트리밍 루프다.
// 서브그래프 노드로 등록된 Compiled 그래프를 실행하면서 GraphEvent에 path를 채워 방출한다.
// 부모 streamLoop에서 AsStreamNode()로 생성된 NodeFunc 어댑터를 통해 호출된다.
//
// 서브그래프 ToParent/parent-Send는 invokeSubgraph와 동일한 방식으로 처리하되,
// 이벤트 방출이 추가된다.
func streamSubgraphLoop(ctx context.Context, compiled *Compiled, input State, cfg config.RunConfig, opts streamLoopOpts) (subgraphResult, error) {
	state := make(State, len(input))
	for k, v := range input {
		state[k] = v
	}

	var current string
	if compiled.condEntry != nil {
		key := compiled.condEntry.router(ctx, state)
		target, ok := compiled.condEntry.mapping[key]
		if !ok {
			return subgraphResult{}, fmt.Errorf("graph: 서브그래프 조건 진입점 라우터가 반환한 키 %q가 mapping에 없습니다", key)
		}
		current = target
	} else {
		current = compiled.entryPoint
	}

	for step := 0; current != ""; step++ {
		select {
		case <-ctx.Done():
			return subgraphResult{}, ctx.Err()
		default:
		}

		if err := checkMaxSteps(step, compiled.maxSteps); err != nil {
			return subgraphResult{}, err
		}

		if opts.mode == core.ModeDebug {
			evt := GraphEvent{
				Node:     current,
				Mode:     core.ModeDebug,
				Metadata: map[string]any{"event": "node_enter", "step": step},
				Path:     opts.pathPrefix,
			}
			emitEvent(ctx, opts.out, evt)
		}

		var tokenCh chan string
		var tokenDone chan struct{}
		if opts.mode == core.ModeMessages {
			tokenCh = make(chan string, 64)
			tokenDone = make(chan struct{})
			nodeName := current
			pathCopy := append([]string(nil), opts.pathPrefix...)
			go func() {
				defer close(tokenDone)
				for token := range tokenCh {
					evt := GraphEvent{
						Node:  nodeName,
						Mode:  core.ModeMessages,
						Token: token,
						Path:  pathCopy,
					}
					emitEvent(ctx, opts.out, evt)
				}
			}()
			ctx = contextWithTokenChan(ctx, tokenCh)
		}

		res, err := runNode(ctx, current, compiled.nodes, state)

		if opts.mode == core.ModeMessages && tokenCh != nil {
			close(tokenCh)
			<-tokenDone
			ctx = context.WithValue(ctx, tokenChanKey, (*chan string)(nil))
		}

		if res.Control != nil {
			ctrl := res.Control
			if ctrl.IsParent() && !ctrl.IsEnd() && len(ctrl.Sends) == 0 {
				if ctrl.Update != nil {
					state = applyReducers(state, ctrl.Update, compiled.schema)
				}
				return subgraphResult{state: state, parentCmd: ctrl}, nil
			}
			if len(ctrl.Sends) > 0 {
				hasParent := false
				for _, s := range ctrl.Sends {
					if s.Graph == command.TargetParent {
						hasParent = true
						break
					}
				}
				if hasParent {
					if ctrl.Update != nil {
						state = applyReducers(state, ctrl.Update, compiled.schema)
					}
					return subgraphResult{state: state, parentCmd: ctrl}, nil
				}
			}
		}

		if err != nil {
			return subgraphResult{}, err
		}

		if opts.mode == core.ModeUpdates && res.Update != nil {
			updateCopy := make(StateUpdate, len(res.Update))
			for k, v := range res.Update {
				updateCopy[k] = v
			}
			evt := GraphEvent{
				Node:   current,
				Mode:   core.ModeUpdates,
				Update: updateCopy,
				Path:   opts.pathPrefix,
			}
			emitEvent(ctx, opts.out, evt)
		}

		state = applyReducers(state, res.Update, compiled.schema)

		if opts.mode == core.ModeValues {
			valueCopy := make(State, len(state))
			for k, v := range state {
				valueCopy[k] = v
			}
			evt := GraphEvent{
				Node:  current,
				Mode:  core.ModeValues,
				Value: valueCopy,
				Path:  opts.pathPrefix,
			}
			emitEvent(ctx, opts.out, evt)
		}

		if opts.mode == core.ModeDebug {
			evt := GraphEvent{
				Node:     current,
				Mode:     core.ModeDebug,
				Metadata: map[string]any{"event": "node_exit", "step": step},
				Path:     opts.pathPrefix,
			}
			emitEvent(ctx, opts.out, evt)
		}

		next, err := resolveNext(ctx, current, res, state, compiled.edges, compiled.condEdges, compiled.nodes)
		if err != nil {
			return subgraphResult{}, err
		}

		if len(next.sends) > 0 {
			for _, se := range next.sends {
				childOpts := opts
				branchState, branchErr := streamFromNode(ctx, compiled, se.target, se.state, step+1, cfg, childOpts)
				if branchErr != nil {
					return subgraphResult{}, fmt.Errorf("graph: 서브그래프 Stream Fanout 분기 %q 실행 실패: %w", se.target, branchErr)
				}
				for k, v := range branchState {
					state[k] = v
				}
			}
			break
		}

		current = next.target
	}

	return subgraphResult{state: state}, nil
}

// AsStreamNode 는 Subgraphs 옵션이 켜진 Stream 실행에서 사용하는 NodeFunc 어댑터를 반환한다.
// AsNode와 동일한 경계(공유/독립 상태, ToParent)를 유지하되 streamLoopOpts를 받아
// 서브그래프 이벤트를 path와 함께 부모 채널로 방출한다.
func (c *Compiled) AsStreamNode(nodeName string, opts streamLoopOpts) NodeFunc {
	// 서브그래프 path = 부모 pathPrefix + 이 서브그래프의 노드 이름
	subPath := make([]string, len(opts.pathPrefix)+1)
	copy(subPath, opts.pathPrefix)
	subPath[len(opts.pathPrefix)] = nodeName

	childOpts := streamLoopOpts{
		mode:       opts.mode,
		sOpts:      opts.sOpts,
		out:        opts.out,
		pathPrefix: subPath,
	}

	return func(ctx context.Context, parentState State) (any, error) {
		subInput := filterBySchema(parentState, c.schemaOpts.inputSchema)
		cfg := config.RunConfig{}
		result, err := streamSubgraphLoop(ctx, c, subInput, cfg, childOpts)
		if err != nil {
			return nil, fmt.Errorf("graph: 서브그래프 스트리밍 실행 실패: %w", err)
		}
		if result.parentCmd != nil {
			return *result.parentCmd, nil
		}
		outputState := filterBySchema(result.state, c.schemaOpts.outputSchema)
		return StateUpdate(outputState), nil
	}
}
