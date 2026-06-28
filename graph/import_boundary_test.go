// import_boundary_test.go 는 task-012 검증 조건 중 패키지 import 경계를 검사하는 테스트를 담는다.
// SPEC §5.11: command·streaming이 graph를 import하지 않고, graph가 command를 import함을 확인한다.
// go list -deps 를 사용해 실제 의존 그래프를 검사한다(D11).
package graph_test

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"
)

const modulePath = "github.com/zipkero/langgraph-go"

// depsOf 는 pkg의 전이적 의존 패키지 목록을 반환한다.
// go list -deps 를 실행해 모듈 내 패키지만 필터링한다.
func depsOf(t *testing.T, pkg string) []string {
	t.Helper()
	cmd := exec.Command("go", "list", "-deps", pkg)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("go list -deps %s 실패: %v\n출력: %s", pkg, err, out.String())
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	result := make([]string, 0, len(lines))
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l != "" {
			result = append(result, l)
		}
	}
	return result
}

// containsDep 는 deps 목록에 target이 포함되는지 반환한다.
func containsDep(deps []string, target string) bool {
	for _, d := range deps {
		if d == target {
			return true
		}
	}
	return false
}

// TestImportBoundary_command는_graph를_import하지_않는다 는 command 패키지의 의존 목록에
// graph 패키지가 없음을 검증한다(SPEC §5.11).
func TestImportBoundary_command는_graph를_import하지_않는다(t *testing.T) {
	commandPkg := modulePath + "/graph/command"
	graphPkg := modulePath + "/graph"

	deps := depsOf(t, commandPkg)
	if containsDep(deps, graphPkg) {
		t.Errorf("위반: %s 의존 목록에 %s 가 포함돼 있습니다(SPEC §5.11 위반)", commandPkg, graphPkg)
	}
}

// TestImportBoundary_streaming은_graph를_import하지_않는다 는 streaming 패키지의 의존 목록에
// graph 패키지가 없음을 검증한다(SPEC §5.11).
func TestImportBoundary_streaming은_graph를_import하지_않는다(t *testing.T) {
	streamingPkg := modulePath + "/streaming"
	graphPkg := modulePath + "/graph"

	deps := depsOf(t, streamingPkg)
	if containsDep(deps, graphPkg) {
		t.Errorf("위반: %s 의존 목록에 %s 가 포함돼 있습니다(SPEC §5.11 위반)", streamingPkg, graphPkg)
	}
}

// TestImportBoundary_graph는_command를_import한다 는 graph 패키지의 의존 목록에
// command 패키지가 포함됨을 검증한다(SPEC §5.11).
func TestImportBoundary_graph는_command를_import한다(t *testing.T) {
	graphPkg := modulePath + "/graph"
	commandPkg := modulePath + "/graph/command"

	deps := depsOf(t, graphPkg)
	if !containsDep(deps, commandPkg) {
		t.Errorf("위반: %s 의존 목록에 %s 가 없습니다(SPEC §5.11 위반)", graphPkg, commandPkg)
	}
}

// TestImportBoundary_command는_core만_참조한다 는 command 패키지의 의존 목록에
// 모듈 내 다른 패키지(core 제외)가 없음을 검증한다(SPEC §5.11 경계 규칙).
func TestImportBoundary_command는_core만_참조한다(t *testing.T) {
	commandPkg := modulePath + "/graph/command"
	corePkg := modulePath + "/core"

	deps := depsOf(t, commandPkg)

	// core는 있어야 한다
	if !containsDep(deps, corePkg) {
		t.Errorf("예상 외: %s 의존 목록에 %s 가 없습니다", commandPkg, corePkg)
	}

	// 모듈 내 다른 패키지(core 이외)는 없어야 한다
	forbidden := []string{
		modulePath + "/graph",
		modulePath + "/streaming",
		modulePath + "/checkpoint",
		modulePath + "/message",
		modulePath + "/agent",
		modulePath + "/prebuilt",
		modulePath + "/llm",
		modulePath + "/tool",
	}
	for _, f := range forbidden {
		if containsDep(deps, f) {
			t.Errorf("위반: %s 의존 목록에 허용되지 않은 패키지 %s 가 포함돼 있습니다", commandPkg, f)
		}
	}
}

// TestImportBoundary_streaming은_core만_참조한다 는 streaming 패키지의 의존 목록에
// 모듈 내 다른 패키지(core 제외)가 없음을 검증한다(SPEC §5.11 경계 규칙).
func TestImportBoundary_streaming은_core만_참조한다(t *testing.T) {
	streamingPkg := modulePath + "/streaming"
	corePkg := modulePath + "/core"

	deps := depsOf(t, streamingPkg)

	// core는 있어야 한다
	if !containsDep(deps, corePkg) {
		t.Errorf("예상 외: %s 의존 목록에 %s 가 없습니다", streamingPkg, corePkg)
	}

	// 모듈 내 다른 패키지(core 이외)는 없어야 한다
	forbidden := []string{
		modulePath + "/graph",
		modulePath + "/graph/command",
		modulePath + "/checkpoint",
		modulePath + "/message",
		modulePath + "/agent",
		modulePath + "/prebuilt",
		modulePath + "/llm",
		modulePath + "/tool",
	}
	for _, f := range forbidden {
		if containsDep(deps, f) {
			t.Errorf("위반: %s 의존 목록에 허용되지 않은 패키지 %s 가 포함돼 있습니다", streamingPkg, f)
		}
	}
}
