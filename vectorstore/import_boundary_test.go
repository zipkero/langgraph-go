// import_boundary_test.go 는 task-008 검증 조건 중 패키지 import 경계를 회귀 보호하는 테스트를 담는다.
// SPEC §5.9:
//   - vectorstore 는 document·llm·tool 을 import 하고, database·외부 벡터 백엔드(Chroma/Supabase 등)를 import 하지 않는다.
//   - document 는 모듈 내 상위 패키지(llm/tool/graph/vectorstore 등)를 import 하지 않는다.
//
// graph/import_boundary_test.go 와 동일한 go list -deps 방식을 따른다.
package vectorstore_test

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"
)

const modulePath = "github.com/zipkero/langgraph-go"

// depsOfPkg 는 pkg 의 전이적 의존 패키지 목록을 반환한다.
// go list -deps 를 실행해 결과 줄을 슬라이스로 돌려준다.
func depsOfPkg(t *testing.T, pkg string) []string {
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

// hasDep 는 deps 목록에 target 이 포함되는지 반환한다.
func hasDep(deps []string, target string) bool {
	for _, d := range deps {
		if d == target {
			return true
		}
	}
	return false
}

// TestImportBoundary_vectorstore는_document를_import한다 는 vectorstore 의 의존 목록에
// document 패키지가 포함됨을 검증한다(SPEC §5.9).
func TestImportBoundary_vectorstore는_document를_import한다(t *testing.T) {
	vsPkg := modulePath + "/vectorstore"
	docPkg := modulePath + "/document"

	deps := depsOfPkg(t, vsPkg)
	if !hasDep(deps, docPkg) {
		t.Errorf("위반: %s 의존 목록에 %s 가 없습니다(SPEC §5.9)", vsPkg, docPkg)
	}
}

// TestImportBoundary_vectorstore는_llm을_import한다 는 vectorstore 의 의존 목록에
// llm 패키지가 포함됨을 검증한다(SPEC §5.9).
func TestImportBoundary_vectorstore는_llm을_import한다(t *testing.T) {
	vsPkg := modulePath + "/vectorstore"
	llmPkg := modulePath + "/llm"

	deps := depsOfPkg(t, vsPkg)
	if !hasDep(deps, llmPkg) {
		t.Errorf("위반: %s 의존 목록에 %s 가 없습니다(SPEC §5.9)", vsPkg, llmPkg)
	}
}

// TestImportBoundary_vectorstore는_tool을_import한다 는 vectorstore 의 의존 목록에
// tool 패키지가 포함됨을 검증한다(SPEC §5.9).
func TestImportBoundary_vectorstore는_tool을_import한다(t *testing.T) {
	vsPkg := modulePath + "/vectorstore"
	toolPkg := modulePath + "/tool"

	deps := depsOfPkg(t, vsPkg)
	if !hasDep(deps, toolPkg) {
		t.Errorf("위반: %s 의존 목록에 %s 가 없습니다(SPEC §5.9)", vsPkg, toolPkg)
	}
}

// TestImportBoundary_vectorstore는_금지_패키지를_import하지_않는다 는 vectorstore 의 의존 목록에
// database 및 외부 벡터 백엔드(Chroma/Supabase 등) 관련 패키지가 없음을 검증한다(SPEC §5.9).
func TestImportBoundary_vectorstore는_금지_패키지를_import하지_않는다(t *testing.T) {
	vsPkg := modulePath + "/vectorstore"

	deps := depsOfPkg(t, vsPkg)

	// 금지 대상: 모듈 내 database 패키지 및 외부 벡터 백엔드 경로
	forbidden := []string{
		modulePath + "/database",
		"github.com/amikos-tech/chroma-go",
		"github.com/supabase-community/supabase-go",
	}
	for _, f := range forbidden {
		if hasDep(deps, f) {
			t.Errorf("위반: %s 의존 목록에 허용되지 않은 패키지 %s 가 포함돼 있습니다(SPEC §5.9)", vsPkg, f)
		}
	}
}

// TestImportBoundary_document는_상위_패키지를_import하지_않는다 는 document 의 의존 목록에
// 모듈 내 상위 패키지(llm/tool/graph/vectorstore 등)가 없음을 검증한다(SPEC §5.9).
func TestImportBoundary_document는_상위_패키지를_import하지_않는다(t *testing.T) {
	docPkg := modulePath + "/document"

	deps := depsOfPkg(t, docPkg)

	// document 가 import 해서는 안 되는 모듈 내 상위 패키지 목록
	forbidden := []string{
		modulePath + "/llm",
		modulePath + "/tool",
		modulePath + "/vectorstore",
		modulePath + "/graph",
		modulePath + "/graph/command",
		modulePath + "/agent",
		modulePath + "/prebuilt",
		modulePath + "/middleware",
		modulePath + "/checkpoint",
		modulePath + "/streaming",
		modulePath + "/structured",
		modulePath + "/prompt",
		modulePath + "/message",
	}
	for _, f := range forbidden {
		if hasDep(deps, f) {
			t.Errorf("위반: %s 의존 목록에 허용되지 않은 패키지 %s 가 포함돼 있습니다(SPEC §5.9)", docPkg, f)
		}
	}
}
