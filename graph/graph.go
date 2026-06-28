// graph 패키지는 StateGraph 빌드·컴파일·실행 엔진을 담당한다.
// core·config·command·checkpoint에 의존하며, streaming은 import하지 않는다(§28-1 규칙).
// graph.State/StateUpdate/StateSnapshot은 core 타입의 alias이고,
// 스트림 모드 인자는 core.Mode를 직접 참조한다.
package graph

import (
	"context"

	"github.com/zipkero/langgraph-go/core"
	"github.com/zipkero/langgraph-go/graph/command"
)

// State 는 core.State의 alias다.
// 그래프 엔진이 다루는 상태 맵 타입이다.
type State = core.State

// StateUpdate 는 core.StateUpdate의 alias다.
// 노드가 반환하는 상태 변경분 맵 타입이다.
type StateUpdate = core.StateUpdate

// StateSnapshot 은 core.StateSnapshot의 alias다.
// GetState/GetStateHistory가 반환하는 스냅샷 타입이다.
type StateSnapshot = core.StateSnapshot

// NodeFunc 는 그래프 노드의 실행 함수 타입이다.
// context와 현재 State를 받아 StateUpdate 또는 command.Command를 반환한다.
// 반환값은 any이며, 엔진이 runNode에서 타입 스위치로 정규화한다(§2.2 D2).
type NodeFunc func(ctx context.Context, st State) (any, error)

// ConditionalRouter 는 조건 엣지·조건 진입점에서 다음 노드 키를 결정하는 함수 타입이다.
// 반환한 문자열을 mapping 맵에서 노드 이름으로 변환한다.
type ConditionalRouter func(ctx context.Context, st State) string

// GraphEvent 는 Compiled.Stream이 채널로 방출하는 엔진 소유의 이벤트다.
// streaming.Event와는 소유자·용도가 다른 별도 타입이다(§1.3 분리 설계).
type GraphEvent struct {
	// Node 는 이벤트를 방출한 노드 이름이다.
	Node string
	// Mode 는 이 이벤트가 방출된 스트림 모드다.
	Mode core.Mode
	// Update 는 노드별 변경분이다(ModeUpdates 모드에서 채워진다).
	Update StateUpdate
	// Value 는 전체 상태 스냅샷이다(ModeValues 모드에서 채워진다).
	Value State
	// Token 은 메시지 토큰 텍스트다(ModeMessages 모드에서 채워진다).
	Token string
	// Metadata 는 진단 이벤트 메타데이터다(ModeDebug 모드에서 채워진다).
	Metadata map[string]any
	// Path 는 서브그래프 이벤트 전파 경로다.
	Path []string
}

// NodeResult 는 runNode가 반환하는 내부 정규형이다.
// 엔진 루프가 applyReducers와 resolveNext에 이 구조를 전달한다(§2.2 D2).
type NodeResult struct {
	// Update 는 이 노드 실행 후 적용할 상태 변경분이다.
	Update StateUpdate
	// Control 은 command.Command 반환 시 채워지는 제어 흐름 지시다.
	// nil이면 정적 엣지·조건 엣지로 진행한다.
	Control *command.Command
}
