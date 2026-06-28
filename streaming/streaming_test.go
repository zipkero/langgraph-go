package streaming_test

import (
	"reflect"
	"testing"

	"github.com/zipkero/langgraph-go/core"
	"github.com/zipkero/langgraph-go/streaming"
)

// TestEmitNodeUpdate 는 EmitNodeUpdate 가 Node·Update 필드를 올바르게 채우는지 검증한다.
func TestEmitNodeUpdate(t *testing.T) {
	update := core.StateUpdate{"count": 1}
	ev := streaming.EmitNodeUpdate("node-a", update)

	if ev.Node != "node-a" {
		t.Errorf("Node: want %q, got %q", "node-a", ev.Node)
	}
	if !reflect.DeepEqual(ev.Update, update) {
		t.Errorf("Update: want %v, got %v", update, ev.Update)
	}
	// 다른 필드는 비어 있어야 한다
	if ev.Value != nil {
		t.Errorf("Value: want nil, got %v", ev.Value)
	}
	if ev.Token != "" {
		t.Errorf("Token: want empty, got %q", ev.Token)
	}
	if ev.Metadata != nil {
		t.Errorf("Metadata: want nil, got %v", ev.Metadata)
	}
	if ev.Path != nil {
		t.Errorf("Path: want nil, got %v", ev.Path)
	}
}

// TestEmitStateValue 는 EmitStateValue 가 Value 필드를 올바르게 채우는지 검증한다.
func TestEmitStateValue(t *testing.T) {
	st := core.State{"key": "val"}
	ev := streaming.EmitStateValue(st)

	if !reflect.DeepEqual(ev.Value, st) {
		t.Errorf("Value: want %v, got %v", st, ev.Value)
	}
	// 다른 필드는 비어 있어야 한다
	if ev.Node != "" {
		t.Errorf("Node: want empty, got %q", ev.Node)
	}
	if ev.Update != nil {
		t.Errorf("Update: want nil, got %v", ev.Update)
	}
	if ev.Token != "" {
		t.Errorf("Token: want empty, got %q", ev.Token)
	}
}

// TestEmitMessageToken 은 EmitMessageToken 이 Token·Metadata 필드를 올바르게 채우는지 검증한다.
func TestEmitMessageToken(t *testing.T) {
	md := streaming.Metadata{"model": "claude-3"}
	ev := streaming.EmitMessageToken("hello", md)

	if ev.Token != "hello" {
		t.Errorf("Token: want %q, got %q", "hello", ev.Token)
	}
	if !reflect.DeepEqual(ev.Metadata, md) {
		t.Errorf("Metadata: want %v, got %v", md, ev.Metadata)
	}
	// 다른 필드는 비어 있어야 한다
	if ev.Node != "" {
		t.Errorf("Node: want empty, got %q", ev.Node)
	}
	if ev.Update != nil {
		t.Errorf("Update: want nil, got %v", ev.Update)
	}
	if ev.Value != nil {
		t.Errorf("Value: want nil, got %v", ev.Value)
	}
}

// TestEmitMessageToken_NilMetadata 는 nil 메타데이터로도 정상 동작하는지 검증한다.
func TestEmitMessageToken_NilMetadata(t *testing.T) {
	ev := streaming.EmitMessageToken("token", nil)
	if ev.Token != "token" {
		t.Errorf("Token: want %q, got %q", "token", ev.Token)
	}
	if ev.Metadata != nil {
		t.Errorf("Metadata: want nil, got %v", ev.Metadata)
	}
}

// TestEmitSubgraph_PathPropagation 은 EmitSubgraph 가 path를 inner 이벤트에 올바르게 전파하는지 검증한다.
func TestEmitSubgraph_PathPropagation(t *testing.T) {
	inner := streaming.EmitNodeUpdate("child-node", core.StateUpdate{"x": 42})
	path := []string{"parent", "subgraph-a"}

	ev := streaming.EmitSubgraph(path, inner)

	if ev.Node != "child-node" {
		t.Errorf("Node: want %q, got %q", "child-node", ev.Node)
	}
	if !reflect.DeepEqual(ev.Update, inner.Update) {
		t.Errorf("Update: want %v, got %v", inner.Update, ev.Update)
	}
	wantPath := []string{"parent", "subgraph-a"}
	if !reflect.DeepEqual(ev.Path, wantPath) {
		t.Errorf("Path: want %v, got %v", wantPath, ev.Path)
	}
}

// TestEmitSubgraph_InnerPathCombined 는 inner 이벤트에 이미 Path 가 있을 때 path가 앞에 붙는지 검증한다.
func TestEmitSubgraph_InnerPathCombined(t *testing.T) {
	inner := streaming.Event{
		Node:  "deep-node",
		Token: "tok",
		Path:  []string{"level2"},
	}
	ev := streaming.EmitSubgraph([]string{"level1"}, inner)

	wantPath := []string{"level1", "level2"}
	if !reflect.DeepEqual(ev.Path, wantPath) {
		t.Errorf("Path: want %v, got %v", wantPath, ev.Path)
	}
	if ev.Token != "tok" {
		t.Errorf("Token: want %q, got %q", "tok", ev.Token)
	}
}

// TestEmitSubgraph_EmptyPath 는 빈 path 로 EmitSubgraph 를 호출할 때 inner.Path 가 그대로 유지되는지 검증한다.
func TestEmitSubgraph_EmptyPath(t *testing.T) {
	inner := streaming.Event{Node: "n", Path: []string{"a"}}
	ev := streaming.EmitSubgraph([]string{}, inner)
	if !reflect.DeepEqual(ev.Path, []string{"a"}) {
		t.Errorf("Path: want [a], got %v", ev.Path)
	}
}

// TestOptions 는 Options 의 Mode·Subgraphs 필드가 설정되는지 검증한다.
func TestOptions(t *testing.T) {
	opts := streaming.Options{
		Mode:      core.ModeValues,
		Subgraphs: true,
	}
	if opts.Mode != core.ModeValues {
		t.Errorf("Mode: want %q, got %q", core.ModeValues, opts.Mode)
	}
	if !opts.Subgraphs {
		t.Error("Subgraphs: want true, got false")
	}

	// 모드 전체 상수 확인
	modes := []core.Mode{core.ModeValues, core.ModeMessages, core.ModeUpdates, core.ModeDebug}
	for _, m := range modes {
		o := streaming.Options{Mode: m}
		if o.Mode != m {
			t.Errorf("Mode: want %q, got %q", m, o.Mode)
		}
	}
}

// TestOptions_SubgraphsFalse 는 Subgraphs 기본값이 false 임을 확인한다.
func TestOptions_SubgraphsFalse(t *testing.T) {
	var opts streaming.Options
	if opts.Subgraphs {
		t.Error("Subgraphs: want false by default, got true")
	}
}

// TestModeAlias 는 streaming.Mode 가 core.Mode 와 동일한 타입 alias 임을 검증한다.
func TestModeAlias(t *testing.T) {
	var m streaming.Mode = core.ModeUpdates
	if m != core.ModeUpdates {
		t.Errorf("Mode alias: want %q, got %q", core.ModeUpdates, m)
	}
}
