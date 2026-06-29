// import_boundary_test.go 는 task-008 검증 조건 중 패키지 import 경계를 회귀 보호하는 테스트를 담는다.
// SPEC §5.9/§5.1:
//   - multiagent 는 허용된 하위 패키지(agent/graph/graph/command/tool/llm/structured/message/core/config)만 import한다.
//   - 하위 패키지(agent/graph/tool 등)는 multiagent 를 역참조하지 않는다.
//
// graph/import_boundary_test.go 와 동일한 go list -deps 방식을 따른다.
package multiagent_test

import (
	"bytes"
	"os/exec"
	"slices"
	"strings"
	"testing"
)

const modPath = "github.com/zipkero/langgraph-go"

// depsOfPackage 는 pkg 의 전이적 의존 패키지 목록을 반환한다.
// go list -deps 를 실행해 결과 줄을 슬라이스로 돌려준다.
func depsOfPackage(t *testing.T, pkg string) []string {
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

// hasDependency 는 deps 목록에 target 이 포함되는지 반환한다.
func hasDependency(deps []string, target string) bool {
	return slices.Contains(deps, target)
}

// filterModuleDeps 는 deps 목록에서 모듈 내부 패키지(modPath 접두사)만 추출해 반환한다.
func filterModuleDeps(deps []string) []string {
	result := make([]string, 0)
	for _, d := range deps {
		if strings.HasPrefix(d, modPath+"/") {
			result = append(result, d)
		}
	}
	return result
}

// TestImportBoundary_multiagent는_허용집합만_import한다 는 multiagent 의 모듈 내부 의존 목록이
// 허용된 하위 패키지 집합에만 속하는지 검증한다(SPEC §5.9).
// 허용 집합: agent/graph/graph/command/tool/llm/structured/message/core/config,
// 그리고 이들의 전이 의존(checkpoint/middleware — agent 패키지가 간접 참조).
func TestImportBoundary_multiagent는_허용집합만_import한다(t *testing.T) {
	multiagentPkg := modPath + "/multiagent"

	// go list -deps 는 전이 의존을 포함한다.
	// multiagent 가 직접 import 하는 허용 집합과 그 전이 의존을 모두 허용한다.
	// checkpoint/middleware 는 agent 패키지의 전이 의존으로 포함된다.
	allowed := map[string]bool{
		modPath + "/multiagent":    true,
		modPath + "/agent":         true,
		modPath + "/graph":         true,
		modPath + "/graph/command": true,
		modPath + "/tool":          true,
		modPath + "/llm":           true,
		modPath + "/structured":    true,
		modPath + "/message":       true,
		modPath + "/core":          true,
		modPath + "/config":        true,
		// agent 의 전이 의존
		modPath + "/checkpoint": true,
		modPath + "/middleware":  true,
	}

	deps := depsOfPackage(t, multiagentPkg)
	moduleDeps := filterModuleDeps(deps)

	for _, d := range moduleDeps {
		if !allowed[d] {
			t.Errorf("위반: %s 의 모듈 내부 의존에 허용되지 않은 패키지 %s 가 포함돼 있습니다(SPEC §5.9)", multiagentPkg, d)
		}
	}
}

// TestImportBoundary_agent는_multiagent를_역참조하지_않는다 는 agent 패키지의 의존 목록에
// multiagent 가 없음을 검증한다(역참조 금지, SPEC §5.1).
func TestImportBoundary_agent는_multiagent를_역참조하지_않는다(t *testing.T) {
	agentPkg := modPath + "/agent"
	multiagentPkg := modPath + "/multiagent"

	deps := depsOfPackage(t, agentPkg)
	if hasDependency(deps, multiagentPkg) {
		t.Errorf("위반: %s 의존 목록에 %s 가 포함돼 있습니다(역참조 금지, SPEC §5.1)", agentPkg, multiagentPkg)
	}
}

// TestImportBoundary_graph는_multiagent를_역참조하지_않는다 는 graph 패키지의 의존 목록에
// multiagent 가 없음을 검증한다(역참조 금지, SPEC §5.1).
func TestImportBoundary_graph는_multiagent를_역참조하지_않는다(t *testing.T) {
	graphPkg := modPath + "/graph"
	multiagentPkg := modPath + "/multiagent"

	deps := depsOfPackage(t, graphPkg)
	if hasDependency(deps, multiagentPkg) {
		t.Errorf("위반: %s 의존 목록에 %s 가 포함돼 있습니다(역참조 금지, SPEC §5.1)", graphPkg, multiagentPkg)
	}
}

// TestImportBoundary_tool은_multiagent를_역참조하지_않는다 는 tool 패키지의 의존 목록에
// multiagent 가 없음을 검증한다(역참조 금지, SPEC §5.1).
func TestImportBoundary_tool은_multiagent를_역참조하지_않는다(t *testing.T) {
	toolPkg := modPath + "/tool"
	multiagentPkg := modPath + "/multiagent"

	deps := depsOfPackage(t, toolPkg)
	if hasDependency(deps, multiagentPkg) {
		t.Errorf("위반: %s 의존 목록에 %s 가 포함돼 있습니다(역참조 금지, SPEC §5.1)", toolPkg, multiagentPkg)
	}
}

// TestImportBoundary_llm은_multiagent를_역참조하지_않는다 는 llm 패키지의 의존 목록에
// multiagent 가 없음을 검증한다(역참조 금지, SPEC §5.1).
func TestImportBoundary_llm은_multiagent를_역참조하지_않는다(t *testing.T) {
	llmPkg := modPath + "/llm"
	multiagentPkg := modPath + "/multiagent"

	deps := depsOfPackage(t, llmPkg)
	if hasDependency(deps, multiagentPkg) {
		t.Errorf("위반: %s 의존 목록에 %s 가 포함돼 있습니다(역참조 금지, SPEC §5.1)", llmPkg, multiagentPkg)
	}
}

// TestImportBoundary_message는_multiagent를_역참조하지_않는다 는 message 패키지의 의존 목록에
// multiagent 가 없음을 검증한다(역참조 금지, SPEC §5.1).
func TestImportBoundary_message는_multiagent를_역참조하지_않는다(t *testing.T) {
	messagePkg := modPath + "/message"
	multiagentPkg := modPath + "/multiagent"

	deps := depsOfPackage(t, messagePkg)
	if hasDependency(deps, multiagentPkg) {
		t.Errorf("위반: %s 의존 목록에 %s 가 포함돼 있습니다(역참조 금지, SPEC §5.1)", messagePkg, multiagentPkg)
	}
}

// TestImportBoundary_core는_multiagent를_역참조하지_않는다 는 core 패키지의 의존 목록에
// multiagent 가 없음을 검증한다(역참조 금지, SPEC §5.1).
func TestImportBoundary_core는_multiagent를_역참조하지_않는다(t *testing.T) {
	corePkg := modPath + "/core"
	multiagentPkg := modPath + "/multiagent"

	deps := depsOfPackage(t, corePkg)
	if hasDependency(deps, multiagentPkg) {
		t.Errorf("위반: %s 의존 목록에 %s 가 포함돼 있습니다(역참조 금지, SPEC §5.1)", corePkg, multiagentPkg)
	}
}

// TestImportBoundary_structured는_multiagent를_역참조하지_않는다 는 structured 패키지의 의존 목록에
// multiagent 가 없음을 검증한다(역참조 금지, SPEC §5.1).
func TestImportBoundary_structured는_multiagent를_역참조하지_않는다(t *testing.T) {
	structuredPkg := modPath + "/structured"
	multiagentPkg := modPath + "/multiagent"

	deps := depsOfPackage(t, structuredPkg)
	if hasDependency(deps, multiagentPkg) {
		t.Errorf("위반: %s 의존 목록에 %s 가 포함돼 있습니다(역참조 금지, SPEC §5.1)", structuredPkg, multiagentPkg)
	}
}
