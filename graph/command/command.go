// command 패키지는 그래프 노드가 반환할 수 있는 제어 흐름 타입을 정의한다.
// 노드 코드는 Goto/End/ToParent/Fanout/NewSend 생성자로 Command·Send 값을 만들어
// 실행 엔진에 제어 흐름과 상태 갱신을 함께 전달한다.
// core 패키지만 의존하며 graph 패키지를 import하지 않는다(§28-1 규칙).
package command

import "github.com/zipkero/langgraph-go/core"

// GraphTarget 은 Send·Command가 가리키는 대상 그래프를 나타낸다.
// 서브그래프 맥락에서 현재 그래프(current)와 부모 그래프(parent)를 구분한다.
type GraphTarget string

const (
	// TargetCurrent 는 현재(자신) 그래프를 대상으로 한다.
	TargetCurrent GraphTarget = "current"
	// TargetParent 는 부모 그래프를 대상으로 한다.
	// 서브그래프 노드가 부모 그래프의 노드로 직접 라우팅할 때 사용한다.
	TargetParent GraphTarget = "parent"
)

// Send 는 Fanout 분기 하나를 나타낸다.
// Target 노드, 해당 분기에 전달할 State, 대상 그래프(GraphTarget)를 담는다.
type Send struct {
	// Target 은 이 Send가 가리키는 노드 이름이다.
	Target string
	// State 는 이 분기에서 해당 노드로 전달할 상태다.
	State any
	// Graph 는 Target이 속한 그래프를 나타낸다(current 또는 parent).
	Graph GraphTarget
}

// Command 는 노드가 반환하는 제어 흐름 지시를 담는 구조체다.
// Goto/End/ToParent/Fanout 생성자가 이 타입의 값을 만든다.
// 엔진은 IsEnd/IsParent 판별자를 통해 종류를 구분한다.
type Command struct {
	// Goto 는 이동할 대상 노드 이름이다.
	// End() 생성자의 경우 빈 문자열이다.
	Goto string
	// Update 는 이 Command와 함께 적용할 상태 갱신 맵이다.
	Update core.StateUpdate
	// Graph 는 Goto 대상이 속한 그래프를 나타낸다.
	Graph GraphTarget
	// Sends 는 Fanout 분기 목록이다. Fanout 생성자만 이 필드를 채운다.
	Sends []Send
	// end 는 이 Command가 그래프 종료를 나타내는지 표시하는 내부 플래그다.
	end bool
}

// Goto 는 현재 그래프의 target 노드로 이동하며 update를 적용하는 Command를 만든다.
// update 에 nil 을 전달하면 상태 변경 없이 이동만 한다.
func Goto(target string, update core.StateUpdate) Command {
	return Command{
		Goto:   target,
		Update: update,
		Graph:  TargetCurrent,
	}
}

// End 는 그래프 실행을 종료하는 Command를 만든다.
// update 가 nil 이 아니면 종료 전에 상태를 갱신한다.
func End(update core.StateUpdate) Command {
	return Command{
		Update: update,
		Graph:  TargetCurrent,
		end:    true,
	}
}

// ToParent 는 서브그래프에서 부모 그래프의 target 노드로 이동하는 Command를 만든다.
// update 가 nil 이 아니면 이동 전에 상태를 갱신한다.
func ToParent(target string, update core.StateUpdate) Command {
	return Command{
		Goto:   target,
		Update: update,
		Graph:  TargetParent,
	}
}

// Fanout 은 여러 Send로 구성된 다중 분기 Command를 만든다.
// 각 Send 는 서로 다른 노드와 상태를 가질 수 있으며, 부모 그래프 대상도 가능하다.
func Fanout(sends []Send) Command {
	return Command{
		Graph: TargetCurrent,
		Sends: sends,
	}
}

// NewSend 는 현재 그래프의 target 노드로 st 상태를 전달하는 Send를 만든다.
func NewSend(target string, st any) Send {
	return Send{
		Target: target,
		State:  st,
		Graph:  TargetCurrent,
	}
}

// IsEnd 는 이 Command가 그래프 종료를 나타내면 true를 반환한다.
func (c Command) IsEnd() bool {
	return c.end
}

// IsParent 는 이 Command의 대상이 부모 그래프이면 true를 반환한다.
func (c Command) IsParent() bool {
	return c.Graph == TargetParent
}
