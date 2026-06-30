// import_boundary_test.go 는 task-006 검증 조건 중 패키지 import 경계를 회귀 보호하는 테스트를 담는다.
// SPEC §5.7/§5.1:
//   - mcp 는 tool·message·config(전이)만 모듈 내부에서 import하고, agent·graph·graph/command·prebuilt·
//     multiagent·vectorstore·store·document 등 상위/동급 패키지를 import하지 않는다.
//   - 하위 패키지(tool·message·config·core)는 mcp 를 역참조하지 않는다.
//   - 다른 상위 패키지(agent·graph 등)도 현재 mcp 를 import하지 않는다.
//
// go/build 로 모듈 내부 소스를 정적 파싱한다. 런타임에 `go list` 같은 하위 프로세스를 띄우지 않으므로
// 빌드 캐시 잠금·안티바이러스 행위탐지(temp 실행파일이 자식 프로세스 spawn)로 인한 비정상 종료(exit 259)가 없다.
package mcp_test

import (
	"go/build"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const modPrefix = "github.com/zipkero/langgraph-go"

// moduleRoot 는 작업 디렉터리에서 위로 올라가며 go.mod 가 있는 모듈 루트를 찾는다.
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("작업 디렉터리 조회 실패: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod 를 찾지 못했습니다")
		}
		dir = parent
	}
}

// pkgDir 는 모듈 내부 import 경로를 디스크 디렉터리로 변환한다.
func pkgDir(root, importPath string) string {
	rel := strings.TrimPrefix(importPath, modPrefix)
	rel = strings.TrimPrefix(rel, "/")
	return filepath.Join(root, filepath.FromSlash(rel))
}

// isInternal 은 import 경로가 이 모듈 내부 패키지인지 판별한다.
func isInternal(importPath string) bool {
	return importPath == modPrefix || strings.HasPrefix(importPath, modPrefix+"/")
}

// depsOf 는 pkg 의 전이적 의존 중 모듈 내부 패키지 목록(pkg 자신 포함)을 반환한다.
// go/build 로 모듈 내부 소스만 재귀 파싱하며, 외부/표준 라이브러리 import는 따라가지 않는다(서브프로세스 없음).
func depsOf(t *testing.T, pkg string) []string {
	t.Helper()
	root := moduleRoot(t)
	visited := map[string]bool{}
	var walk func(p string)
	walk = func(p string) {
		if visited[p] {
			return
		}
		visited[p] = true
		bp, err := build.ImportDir(pkgDir(root, p), 0)
		if err != nil {
			t.Fatalf("%s 정적 파싱 실패: %v", p, err)
		}
		for _, imp := range bp.Imports {
			if isInternal(imp) {
				walk(imp)
			}
		}
	}
	walk(pkg)
	result := make([]string, 0, len(visited))
	for p := range visited {
		result = append(result, p)
	}
	return result
}

// hasModDep 는 deps 목록에 target 이 포함되는지 반환한다.
func hasModDep(deps []string, target string) bool {
	return slices.Contains(deps, target)
}

// filterModDeps 는 deps 목록에서 모듈 내부 패키지(modPrefix 접두사)만 추출해 반환한다.
func filterModDeps(deps []string) []string {
	result := make([]string, 0)
	for _, d := range deps {
		if strings.HasPrefix(d, modPrefix+"/") {
			result = append(result, d)
		}
	}
	return result
}

// ── 정방향: mcp 의 포지티브 단언(포함되어야 할 패키지) ──────────────────────────────────

// TestImportBoundary_mcp는_tool을_import한다 는 mcp 의 의존 목록에
// tool 패키지가 포함됨을 검증한다(SPEC §5.7).
func TestImportBoundary_mcp는_tool을_import한다(t *testing.T) {
	mcpPkg := modPrefix + "/mcp"
	toolPkg := modPrefix + "/tool"

	deps := depsOf(t, mcpPkg)
	if !hasModDep(deps, toolPkg) {
		t.Errorf("위반: %s 의존 목록에 %s 가 없습니다(SPEC §5.7)", mcpPkg, toolPkg)
	}
}

// TestImportBoundary_mcp는_message를_import한다 는 mcp 의 의존 목록에
// message 패키지가 포함됨을 검증한다(SPEC §5.7).
func TestImportBoundary_mcp는_message를_import한다(t *testing.T) {
	mcpPkg := modPrefix + "/mcp"
	messagePkg := modPrefix + "/message"

	deps := depsOf(t, mcpPkg)
	if !hasModDep(deps, messagePkg) {
		t.Errorf("위반: %s 의존 목록에 %s 가 없습니다(SPEC §5.7)", mcpPkg, messagePkg)
	}
}

// TestImportBoundary_mcp는_config를_전이로_포함한다 는 mcp 의 의존 목록에
// config 패키지가 포함됨을 검증한다(tool·message의 전이 의존, SPEC §5.7).
func TestImportBoundary_mcp는_config를_전이로_포함한다(t *testing.T) {
	mcpPkg := modPrefix + "/mcp"
	configPkg := modPrefix + "/config"

	deps := depsOf(t, mcpPkg)
	if !hasModDep(deps, configPkg) {
		t.Errorf("위반: %s 의존 목록에 %s 가 없습니다(SPEC §5.7)", mcpPkg, configPkg)
	}
}

// ── 정방향: mcp 의 금지 단언(포함되어서는 안 될 상위/동급 패키지) ──────────────────────

// TestImportBoundary_mcp는_금지_패키지를_import하지_않는다 는 mcp 의 의존 목록에
// 상위/동급 패키지(agent·graph·graph/command·prebuilt·multiagent·vectorstore·store·document)가
// 없음을 검증한다(SPEC §5.7).
func TestImportBoundary_mcp는_금지_패키지를_import하지_않는다(t *testing.T) {
	mcpPkg := modPrefix + "/mcp"

	// mcp가 import해서는 안 되는 모듈 내 상위/동급 패키지 목록.
	// go list -deps github.com/zipkero/langgraph-go/mcp 의 모듈 내부 결과(config/message/tool/mcp)를
	// 기준으로 이 목록에 없는 모든 모듈 내부 패키지가 금지 대상이다.
	forbidden := []string{
		modPrefix + "/agent",
		modPrefix + "/graph",
		modPrefix + "/graph/command",
		modPrefix + "/prebuilt",
		modPrefix + "/multiagent",
		modPrefix + "/vectorstore",
		modPrefix + "/store",
		modPrefix + "/document",
		modPrefix + "/llm",
		modPrefix + "/structured",
		modPrefix + "/prompt",
		modPrefix + "/checkpoint",
		modPrefix + "/middleware",
		modPrefix + "/streaming",
	}

	deps := depsOf(t, mcpPkg)
	for _, f := range forbidden {
		if hasModDep(deps, f) {
			t.Errorf("위반: %s 의존 목록에 허용되지 않은 패키지 %s 가 포함돼 있습니다(SPEC §5.7)", mcpPkg, f)
		}
	}
}

// TestImportBoundary_mcp는_허용집합만_import한다 는 mcp 의 모듈 내부 의존이
// 허용 집합(config·message·tool·mcp) 밖을 참조하지 않음을 화이트리스트로 검증한다(SPEC §5.7).
// 포지티브 단언과 금지 단언을 이중으로 보호한다.
func TestImportBoundary_mcp는_허용집합만_import한다(t *testing.T) {
	mcpPkg := modPrefix + "/mcp"

	// go list -deps 로 확인한 mcp 의 모듈 내부 허용 집합(config/message/tool/mcp).
	allowed := map[string]bool{
		modPrefix + "/config":  true,
		modPrefix + "/message": true,
		modPrefix + "/tool":    true,
		modPrefix + "/mcp":     true,
	}

	deps := depsOf(t, mcpPkg)
	moduleDeps := filterModDeps(deps)

	for _, d := range moduleDeps {
		if !allowed[d] {
			t.Errorf("위반: %s 의 모듈 내부 의존에 허용되지 않은 패키지 %s 가 포함돼 있습니다(SPEC §5.7)", mcpPkg, d)
		}
	}
}

// ── 역방향: 하위 패키지가 mcp 를 역참조하지 않는다 ──────────────────────────────────────

// TestImportBoundary_tool은_mcp를_역참조하지_않는다 는 tool 패키지의 의존 목록에
// mcp 가 없음을 검증한다(역참조 금지, SPEC §5.1).
func TestImportBoundary_tool은_mcp를_역참조하지_않는다(t *testing.T) {
	toolPkg := modPrefix + "/tool"
	mcpPkg := modPrefix + "/mcp"

	deps := depsOf(t, toolPkg)
	if hasModDep(deps, mcpPkg) {
		t.Errorf("위반: %s 의존 목록에 %s 가 포함돼 있습니다(역참조 금지, SPEC §5.1)", toolPkg, mcpPkg)
	}
}

// TestImportBoundary_message는_mcp를_역참조하지_않는다 는 message 패키지의 의존 목록에
// mcp 가 없음을 검증한다(역참조 금지, SPEC §5.1).
func TestImportBoundary_message는_mcp를_역참조하지_않는다(t *testing.T) {
	messagePkg := modPrefix + "/message"
	mcpPkg := modPrefix + "/mcp"

	deps := depsOf(t, messagePkg)
	if hasModDep(deps, mcpPkg) {
		t.Errorf("위반: %s 의존 목록에 %s 가 포함돼 있습니다(역참조 금지, SPEC §5.1)", messagePkg, mcpPkg)
	}
}

// TestImportBoundary_config는_mcp를_역참조하지_않는다 는 config 패키지의 의존 목록에
// mcp 가 없음을 검증한다(역참조 금지, SPEC §5.1).
func TestImportBoundary_config는_mcp를_역참조하지_않는다(t *testing.T) {
	configPkg := modPrefix + "/config"
	mcpPkg := modPrefix + "/mcp"

	deps := depsOf(t, configPkg)
	if hasModDep(deps, mcpPkg) {
		t.Errorf("위반: %s 의존 목록에 %s 가 포함돼 있습니다(역참조 금지, SPEC §5.1)", configPkg, mcpPkg)
	}
}

// TestImportBoundary_agent는_mcp를_역참조하지_않는다 는 agent 패키지의 의존 목록에
// mcp 가 없음을 검증한다(역참조 금지, SPEC §5.1).
func TestImportBoundary_agent는_mcp를_역참조하지_않는다(t *testing.T) {
	agentPkg := modPrefix + "/agent"
	mcpPkg := modPrefix + "/mcp"

	deps := depsOf(t, agentPkg)
	if hasModDep(deps, mcpPkg) {
		t.Errorf("위반: %s 의존 목록에 %s 가 포함돼 있습니다(역참조 금지, SPEC §5.1)", agentPkg, mcpPkg)
	}
}

// TestImportBoundary_graph는_mcp를_역참조하지_않는다 는 graph 패키지의 의존 목록에
// mcp 가 없음을 검증한다(역참조 금지, SPEC §5.1).
func TestImportBoundary_graph는_mcp를_역참조하지_않는다(t *testing.T) {
	graphPkg := modPrefix + "/graph"
	mcpPkg := modPrefix + "/mcp"

	deps := depsOf(t, graphPkg)
	if hasModDep(deps, mcpPkg) {
		t.Errorf("위반: %s 의존 목록에 %s 가 포함돼 있습니다(역참조 금지, SPEC §5.1)", graphPkg, mcpPkg)
	}
}

// TestImportBoundary_multiagent는_mcp를_역참조하지_않는다 는 multiagent 패키지의 의존 목록에
// mcp 가 없음을 검증한다(역참조 금지, SPEC §5.1).
func TestImportBoundary_multiagent는_mcp를_역참조하지_않는다(t *testing.T) {
	multiagentPkg := modPrefix + "/multiagent"
	mcpPkg := modPrefix + "/mcp"

	deps := depsOf(t, multiagentPkg)
	if hasModDep(deps, mcpPkg) {
		t.Errorf("위반: %s 의존 목록에 %s 가 포함돼 있습니다(역참조 금지, SPEC §5.1)", multiagentPkg, mcpPkg)
	}
}
