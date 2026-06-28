// streaming 패키지는 스트림 이벤트 타입과 방출 헬퍼를 정의한다.
// graph 패키지를 import하지 않고 core만 의존하므로, graph↔streaming 순환이 발생하지 않는다.
// 호출자·노드 코드가 core 타입만 가지고 독립 스트림 이벤트를 조립하거나,
// 멀티에이전트·RAG 같은 응용 계층이 자체 스트림 파이프라인을 구성할 때 사용한다(§28-1 규칙).
package streaming

import "github.com/zipkero/langgraph-go/core"

// Mode 는 core.Mode의 alias다.
// 스트림 방출 모드 상수는 core.ModeValues/ModeMessages/ModeUpdates/ModeDebug를 그대로 사용한다.
type Mode = core.Mode

// Metadata 는 이벤트에 붙이는 범용 메타데이터 맵이다.
type Metadata map[string]any

// Event 는 호출자·노드 코드가 조립하는 독립 스트림 이벤트다.
// graph.GraphEvent 가 엔진 소유의 채널 타입인 반면, 이 타입은 graph에 묶이지 않은
// 휴대용 이벤트로, EmitNodeUpdate/EmitStateValue/EmitMessageToken/EmitSubgraph 헬퍼가 생성한다.
type Event struct {
	// Node 는 이벤트를 발생시킨 노드 이름이다.
	Node string
	// Update 는 노드가 반환한 상태 변경분이다(EmitNodeUpdate에서 채워진다).
	Update core.StateUpdate
	// Value 는 전체 상태 스냅샷이다(EmitStateValue에서 채워진다).
	Value core.State
	// Token 은 메시지 토큰 단위 텍스트다(EmitMessageToken에서 채워진다).
	Token string
	// Metadata 는 이벤트에 첨부된 범용 메타데이터다.
	Metadata Metadata
	// Path 는 서브그래프 이벤트 전파 경로다(EmitSubgraph에서 채워진다).
	Path []string
}

// Options 는 스트림 방출 옵션을 담는다.
type Options struct {
	// Mode 는 방출할 이벤트 종류를 지정한다(core.ModeValues/ModeMessages/ModeUpdates/ModeDebug).
	Mode core.Mode
	// Subgraphs 가 true이면 서브그래프 이벤트를 경로(Path)와 함께 상위로 전파한다.
	Subgraphs bool
}

// EmitNodeUpdate 는 노드 이름과 상태 변경분으로 노드 업데이트 이벤트를 만든다.
// 반환된 Event의 Node 필드와 Update 필드가 채워진다.
func EmitNodeUpdate(node string, update core.StateUpdate) Event {
	return Event{
		Node:   node,
		Update: update,
	}
}

// EmitStateValue 는 전체 상태 스냅샷으로 상태 값 이벤트를 만든다.
// 반환된 Event의 Value 필드가 채워진다.
func EmitStateValue(st core.State) Event {
	return Event{
		Value: st,
	}
}

// EmitMessageToken 은 토큰 텍스트와 메타데이터로 메시지 토큰 이벤트를 만든다.
// 반환된 Event의 Token 필드와 Metadata 필드가 채워진다.
func EmitMessageToken(token string, md Metadata) Event {
	return Event{
		Token:    token,
		Metadata: md,
	}
}

// EmitSubgraph 는 서브그래프 이벤트를 path를 붙여 상위로 전파한다.
// inner 이벤트의 모든 필드를 그대로 유지하면서 Path를 path로 설정한 새 Event를 반환한다.
// 이미 inner.Path 가 있는 경우 path가 앞에 붙는 형태로 연결한다.
func EmitSubgraph(path []string, inner Event) Event {
	combined := make([]string, 0, len(path)+len(inner.Path))
	combined = append(combined, path...)
	combined = append(combined, inner.Path...)
	return Event{
		Node:     inner.Node,
		Update:   inner.Update,
		Value:    inner.Value,
		Token:    inner.Token,
		Metadata: inner.Metadata,
		Path:     combined,
	}
}
