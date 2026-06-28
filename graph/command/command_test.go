package command_test

import (
	"testing"

	"github.com/zipkero/langgraph-go/core"
	"github.com/zipkero/langgraph-go/graph/command"
)

// TestGoto 는 Goto 생성자가 Goto·Update·Graph 필드를 올바르게 보존하는지 검증한다.
func TestGoto(t *testing.T) {
	update := core.StateUpdate{"count": 1}
	cmd := command.Goto("nodeA", update)

	if cmd.Goto != "nodeA" {
		t.Errorf("Goto = %q, want %q", cmd.Goto, "nodeA")
	}
	if cmd.Update["count"] != 1 {
		t.Errorf("Update[count] = %v, want 1", cmd.Update["count"])
	}
	if cmd.Graph != command.TargetCurrent {
		t.Errorf("Graph = %q, want %q", cmd.Graph, command.TargetCurrent)
	}
	if cmd.IsEnd() {
		t.Error("IsEnd() = true, want false")
	}
	if cmd.IsParent() {
		t.Error("IsParent() = true, want false")
	}
}

// TestGotoNilUpdate 는 Goto 에 nil update 를 전달해도 패닉 없이 동작하는지 검증한다.
func TestGotoNilUpdate(t *testing.T) {
	cmd := command.Goto("nodeB", nil)
	if cmd.Goto != "nodeB" {
		t.Errorf("Goto = %q, want %q", cmd.Goto, "nodeB")
	}
	if cmd.Update != nil {
		t.Errorf("Update = %v, want nil", cmd.Update)
	}
}

// TestEnd 는 End 생성자가 종료 표식과 update 를 올바르게 보존하는지 검증한다.
func TestEnd(t *testing.T) {
	update := core.StateUpdate{"result": "done"}
	cmd := command.End(update)

	if cmd.Goto != "" {
		t.Errorf("Goto = %q, want empty string", cmd.Goto)
	}
	if cmd.Update["result"] != "done" {
		t.Errorf("Update[result] = %v, want \"done\"", cmd.Update["result"])
	}
	if !cmd.IsEnd() {
		t.Error("IsEnd() = false, want true")
	}
	if cmd.IsParent() {
		t.Error("IsParent() = true, want false")
	}
}

// TestEndNilUpdate 는 End 에 nil update 를 전달해도 패닉 없이 종료 표식만 세워지는지 검증한다.
func TestEndNilUpdate(t *testing.T) {
	cmd := command.End(nil)
	if !cmd.IsEnd() {
		t.Error("IsEnd() = false, want true")
	}
	if cmd.Update != nil {
		t.Errorf("Update = %v, want nil", cmd.Update)
	}
}

// TestToParent 는 ToParent 생성자가 부모 대상·Goto·update 를 올바르게 보존하는지 검증한다.
func TestToParent(t *testing.T) {
	update := core.StateUpdate{"step": 2}
	cmd := command.ToParent("parentNode", update)

	if cmd.Goto != "parentNode" {
		t.Errorf("Goto = %q, want %q", cmd.Goto, "parentNode")
	}
	if cmd.Update["step"] != 2 {
		t.Errorf("Update[step] = %v, want 2", cmd.Update["step"])
	}
	if cmd.Graph != command.TargetParent {
		t.Errorf("Graph = %q, want %q", cmd.Graph, command.TargetParent)
	}
	if cmd.IsEnd() {
		t.Error("IsEnd() = true, want false")
	}
	if !cmd.IsParent() {
		t.Error("IsParent() = false, want true")
	}
}

// TestFanout 은 Fanout 생성자가 Sends 목록을 올바르게 보존하는지 검증한다.
func TestFanout(t *testing.T) {
	sends := []command.Send{
		command.NewSend("branch1", core.State{"x": 10}),
		command.NewSend("branch2", core.State{"x": 20}),
	}
	cmd := command.Fanout(sends)

	if len(cmd.Sends) != 2 {
		t.Fatalf("len(Sends) = %d, want 2", len(cmd.Sends))
	}
	if cmd.Sends[0].Target != "branch1" {
		t.Errorf("Sends[0].Target = %q, want %q", cmd.Sends[0].Target, "branch1")
	}
	if cmd.Sends[1].Target != "branch2" {
		t.Errorf("Sends[1].Target = %q, want %q", cmd.Sends[1].Target, "branch2")
	}
	if cmd.IsEnd() {
		t.Error("IsEnd() = true, want false")
	}
	if cmd.IsParent() {
		t.Error("IsParent() = true, want false")
	}
}

// TestNewSend 는 NewSend 생성자가 Target·State·Graph 필드를 올바르게 보존하는지 검증한다.
func TestNewSend(t *testing.T) {
	st := core.State{"key": "value"}
	s := command.NewSend("targetNode", st)

	if s.Target != "targetNode" {
		t.Errorf("Target = %q, want %q", s.Target, "targetNode")
	}
	stMap, ok := s.State.(core.State)
	if !ok {
		t.Fatalf("State 타입 단언 실패: got %T", s.State)
	}
	if stMap["key"] != "value" {
		t.Errorf("State[key] = %v, want %q", stMap["key"], "value")
	}
	if s.Graph != command.TargetCurrent {
		t.Errorf("Graph = %q, want %q", s.Graph, command.TargetCurrent)
	}
}

// TestGraphTargetConstants 는 GraphTarget 상수가 지정된 문자열 값을 갖는지 검증한다.
func TestGraphTargetConstants(t *testing.T) {
	if command.TargetCurrent != "current" {
		t.Errorf("TargetCurrent = %q, want \"current\"", command.TargetCurrent)
	}
	if command.TargetParent != "parent" {
		t.Errorf("TargetParent = %q, want \"parent\"", command.TargetParent)
	}
}

// TestIsEndIsParentMutualExclusion 은 End 와 ToParent 가 서로 다른 표식을 세우는지 교차 검증한다.
func TestIsEndIsParentMutualExclusion(t *testing.T) {
	endCmd := command.End(nil)
	if !endCmd.IsEnd() {
		t.Error("End: IsEnd() = false, want true")
	}
	if endCmd.IsParent() {
		t.Error("End: IsParent() = true, want false")
	}

	parentCmd := command.ToParent("p", nil)
	if parentCmd.IsEnd() {
		t.Error("ToParent: IsEnd() = true, want false")
	}
	if !parentCmd.IsParent() {
		t.Error("ToParent: IsParent() = false, want true")
	}
}
