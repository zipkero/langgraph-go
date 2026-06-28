// core 패키지는 모듈 전체가 공유하는 원시 타입을 정의한다.
// config만 import하는 leaf 패키지이며, 리듀서·그래프 실행 로직은 포함하지 않는다.
// 순환 import를 방지하기 위해 다른 패키지(graph, agent, streaming 등)가 core를 참조하며,
// core는 config 외 모듈 내 패키지를 import하지 않는다(§28-1 규칙1).
package core

import (
	"time"

	"github.com/zipkero/langgraph-go/config"
)

// State 는 그래프/에이전트 실행 중 공유되는 범용 상태 맵이다.
// 키는 문자열, 값은 임의 타입(any)으로 구성된다.
type State map[string]any

// StateUpdate 는 리듀서가 반환하는 상태 변경분 맵이다.
// 전체 상태를 교체하는 것이 아니라 변경된 키-값만 담는다.
type StateUpdate map[string]any

// Mode 는 스트림 방출 모드를 나타내는 명명된 문자열 타입이다.
// graph/agent 시그니처가 streaming 패키지를 import하지 않고 스트림 모드를 다루기 위해
// core에 위치한다(§28-1 규칙1). streaming 패키지는 core.Mode의 alias를 노출한다.
type Mode string

const (
	// ModeValues 는 각 노드 실행 후 전체 상태 스냅샷을 방출하는 모드다.
	ModeValues Mode = "values"
	// ModeMessages 는 토큰 단위 메시지 스트리밍을 방출하는 모드다.
	ModeMessages Mode = "messages"
	// ModeUpdates 는 각 노드 실행 후 변경분만 방출하는 모드다.
	ModeUpdates Mode = "updates"
	// ModeDebug 는 내부 진단 이벤트를 방출하는 모드다.
	ModeDebug Mode = "debug"
)

// StateSnapshot 은 특정 시점의 스레드 상태 스냅샷이다.
// GetState/GetStateHistory가 반환하며, checkpoint·agent가 graph를 import하지 않고
// 이 타입을 참조하기 위해 core에 위치한다(§28-1 규칙1).
// graph.StateSnapshot/checkpoint.StateSnapshot은 이 타입의 alias다.
type StateSnapshot struct {
	// Values 는 스냅샷 시점의 전체 상태 맵이다.
	Values State
	// Next 는 다음에 실행될 노드 이름 목록이다.
	Next []string
	// Config 는 이 스냅샷을 생성한 실행 설정이다.
	Config config.RunConfig
	// Metadata 는 체크포인트 ID, 단계 번호 등 실행 메타데이터를 담는 범용 맵이다.
	Metadata map[string]any
	// CreatedAt 은 스냅샷이 생성된 시각이다.
	CreatedAt time.Time
}
