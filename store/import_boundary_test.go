// import_boundary_test.go 는 store 패키지의 import 경계를 회귀 보호하는 테스트를 담는다.
// SPEC §5.7/§5.1:
//   - store 는 llm·config·표준만 직접 import하며,
//     agent·graph·graph/command·vectorstore·multiagent 를 의존하지 않는다.
//   - 하위 패키지(tool·llm·config·core·checkpoint·message·structured 등)는 store 를 역참조하지 않는다.
//
// multiagent/import_boundary_test.go 와 동일한 go list -deps 방식을 따른다.
package store_test

import (
	"bytes"
	"os/exec"
	"slices"
	"strings"
	"testing"
)

const storeModPath = "github.com/zipkero/langgraph-go"

// storeListDeps 는 pkg 의 전이적 의존 패키지 목록을 반환한다.
// go list -deps 를 실행해 결과 줄을 슬라이스로 돌려준다.
func storeListDeps(t *testing.T, pkg string) []string {
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

// storeHasDep 는 deps 목록에 target 이 포함되는지 반환한다.
func storeHasDep(deps []string, target string) bool {
	return slices.Contains(deps, target)
}

// TestImportBoundary_store는_금지_패키지를_import하지_않는다 는 store 의 전이 의존 목록에
// 금지 패키지(agent·graph·graph/command·vectorstore·multiagent)가 없음을 검증한다(SPEC §5.7).
//
// 주의: go list -deps 는 전이 의존을 포함하므로, store → llm → tool·message·structured 경로로
// tool/message/structured 가 나타난다. 이들은 허용 전이이므로 금지하지 않는다.
// 금지 집합은 store 가 절대 의존해서는 안 되는 상위·동급 패키지로만 구성한다.
func TestImportBoundary_store는_금지_패키지를_import하지_않는다(t *testing.T) {
	storePkg := storeModPath + "/store"

	deps := storeListDeps(t, storePkg)

	// store 가 의존해서는 안 되는 금지 패키지 집합
	forbidden := []string{
		storeModPath + "/agent",
		storeModPath + "/graph",
		storeModPath + "/graph/command",
		storeModPath + "/vectorstore",
		storeModPath + "/multiagent",
	}

	for _, f := range forbidden {
		if storeHasDep(deps, f) {
			t.Errorf("위반: %s 의존 목록에 허용되지 않은 패키지 %s 가 포함돼 있습니다(SPEC §5.7)", storePkg, f)
		}
	}
}

// TestImportBoundary_store는_llm을_import한다 는 store 의 의존 목록에
// llm 패키지가 포함됨을 검증한다(store 직접 import 확인, SPEC §5.7).
func TestImportBoundary_store는_llm을_import한다(t *testing.T) {
	storePkg := storeModPath + "/store"
	llmPkg := storeModPath + "/llm"

	deps := storeListDeps(t, storePkg)
	if !storeHasDep(deps, llmPkg) {
		t.Errorf("위반: %s 의존 목록에 %s 가 없습니다(SPEC §5.7)", storePkg, llmPkg)
	}
}

// TestImportBoundary_store는_config를_import한다 는 store 의 의존 목록에
// config 패키지가 포함됨을 검증한다(store 직접 import 확인, SPEC §5.7).
func TestImportBoundary_store는_config를_import한다(t *testing.T) {
	storePkg := storeModPath + "/store"
	configPkg := storeModPath + "/config"

	deps := storeListDeps(t, storePkg)
	if !storeHasDep(deps, configPkg) {
		t.Errorf("위반: %s 의존 목록에 %s 가 없습니다(SPEC §5.7)", storePkg, configPkg)
	}
}

// TestImportBoundary_tool은_store를_역참조하지_않는다 는 tool 패키지의 의존 목록에
// store 가 없음을 검증한다(역참조 금지, SPEC §5.1).
func TestImportBoundary_tool은_store를_역참조하지_않는다(t *testing.T) {
	toolPkg := storeModPath + "/tool"
	storePkg := storeModPath + "/store"

	deps := storeListDeps(t, toolPkg)
	if storeHasDep(deps, storePkg) {
		t.Errorf("위반: %s 의존 목록에 %s 가 포함돼 있습니다(역참조 금지, SPEC §5.1)", toolPkg, storePkg)
	}
}

// TestImportBoundary_llm은_store를_역참조하지_않는다 는 llm 패키지의 의존 목록에
// store 가 없음을 검증한다(역참조 금지, SPEC §5.1).
func TestImportBoundary_llm은_store를_역참조하지_않는다(t *testing.T) {
	llmPkg := storeModPath + "/llm"
	storePkg := storeModPath + "/store"

	deps := storeListDeps(t, llmPkg)
	if storeHasDep(deps, storePkg) {
		t.Errorf("위반: %s 의존 목록에 %s 가 포함돼 있습니다(역참조 금지, SPEC §5.1)", llmPkg, storePkg)
	}
}

// TestImportBoundary_config는_store를_역참조하지_않는다 는 config 패키지의 의존 목록에
// store 가 없음을 검증한다(역참조 금지, SPEC §5.1).
func TestImportBoundary_config는_store를_역참조하지_않는다(t *testing.T) {
	configPkg := storeModPath + "/config"
	storePkg := storeModPath + "/store"

	deps := storeListDeps(t, configPkg)
	if storeHasDep(deps, storePkg) {
		t.Errorf("위반: %s 의존 목록에 %s 가 포함돼 있습니다(역참조 금지, SPEC §5.1)", configPkg, storePkg)
	}
}

// TestImportBoundary_core는_store를_역참조하지_않는다 는 core 패키지의 의존 목록에
// store 가 없음을 검증한다(역참조 금지, SPEC §5.1).
func TestImportBoundary_core는_store를_역참조하지_않는다(t *testing.T) {
	corePkg := storeModPath + "/core"
	storePkg := storeModPath + "/store"

	deps := storeListDeps(t, corePkg)
	if storeHasDep(deps, storePkg) {
		t.Errorf("위반: %s 의존 목록에 %s 가 포함돼 있습니다(역참조 금지, SPEC §5.1)", corePkg, storePkg)
	}
}

// TestImportBoundary_checkpoint는_store를_역참조하지_않는다 는 checkpoint 패키지의 의존 목록에
// store 가 없음을 검증한다(역참조 금지, SPEC §5.1).
func TestImportBoundary_checkpoint는_store를_역참조하지_않는다(t *testing.T) {
	checkpointPkg := storeModPath + "/checkpoint"
	storePkg := storeModPath + "/store"

	deps := storeListDeps(t, checkpointPkg)
	if storeHasDep(deps, storePkg) {
		t.Errorf("위반: %s 의존 목록에 %s 가 포함돼 있습니다(역참조 금지, SPEC §5.1)", checkpointPkg, storePkg)
	}
}

// TestImportBoundary_message는_store를_역참조하지_않는다 는 message 패키지의 의존 목록에
// store 가 없음을 검증한다(역참조 금지, SPEC §5.1).
func TestImportBoundary_message는_store를_역참조하지_않는다(t *testing.T) {
	messagePkg := storeModPath + "/message"
	storePkg := storeModPath + "/store"

	deps := storeListDeps(t, messagePkg)
	if storeHasDep(deps, storePkg) {
		t.Errorf("위반: %s 의존 목록에 %s 가 포함돼 있습니다(역참조 금지, SPEC §5.1)", messagePkg, storePkg)
	}
}

// TestImportBoundary_structured는_store를_역참조하지_않는다 는 structured 패키지의 의존 목록에
// store 가 없음을 검증한다(역참조 금지, SPEC §5.1).
func TestImportBoundary_structured는_store를_역참조하지_않는다(t *testing.T) {
	structuredPkg := storeModPath + "/structured"
	storePkg := storeModPath + "/store"

	deps := storeListDeps(t, structuredPkg)
	if storeHasDep(deps, storePkg) {
		t.Errorf("위반: %s 의존 목록에 %s 가 포함돼 있습니다(역참조 금지, SPEC §5.1)", structuredPkg, storePkg)
	}
}

// TestImportBoundary_agent는_store를_역참조하지_않는다 는 agent 패키지의 의존 목록에
// store 가 없음을 검증한다(역참조 금지, SPEC §5.1).
func TestImportBoundary_agent는_store를_역참조하지_않는다(t *testing.T) {
	agentPkg := storeModPath + "/agent"
	storePkg := storeModPath + "/store"

	deps := storeListDeps(t, agentPkg)
	if storeHasDep(deps, storePkg) {
		t.Errorf("위반: %s 의존 목록에 %s 가 포함돼 있습니다(역참조 금지, SPEC §5.1)", agentPkg, storePkg)
	}
}

// TestImportBoundary_graph는_store를_역참조하지_않는다 는 graph 패키지의 의존 목록에
// store 가 없음을 검증한다(역참조 금지, SPEC §5.1).
func TestImportBoundary_graph는_store를_역참조하지_않는다(t *testing.T) {
	graphPkg := storeModPath + "/graph"
	storePkg := storeModPath + "/store"

	deps := storeListDeps(t, graphPkg)
	if storeHasDep(deps, storePkg) {
		t.Errorf("위반: %s 의존 목록에 %s 가 포함돼 있습니다(역참조 금지, SPEC §5.1)", graphPkg, storePkg)
	}
}

// TestImportBoundary_vectorstore는_store를_역참조하지_않는다 는 vectorstore 패키지의 의존 목록에
// store 가 없음을 검증한다(역참조 금지, SPEC §5.1).
func TestImportBoundary_vectorstore는_store를_역참조하지_않는다(t *testing.T) {
	vectorstorePkg := storeModPath + "/vectorstore"
	storePkg := storeModPath + "/store"

	deps := storeListDeps(t, vectorstorePkg)
	if storeHasDep(deps, storePkg) {
		t.Errorf("위반: %s 의존 목록에 %s 가 포함돼 있습니다(역참조 금지, SPEC §5.1)", vectorstorePkg, storePkg)
	}
}

// TestImportBoundary_multiagent는_store를_역참조하지_않는다 는 multiagent 패키지의 의존 목록에
// store 가 없음을 검증한다(역참조 금지, SPEC §5.1).
func TestImportBoundary_multiagent는_store를_역참조하지_않는다(t *testing.T) {
	multiagentPkg := storeModPath + "/multiagent"
	storePkg := storeModPath + "/store"

	deps := storeListDeps(t, multiagentPkg)
	if storeHasDep(deps, storePkg) {
		t.Errorf("위반: %s 의존 목록에 %s 가 포함돼 있습니다(역참조 금지, SPEC §5.1)", multiagentPkg, storePkg)
	}
}
