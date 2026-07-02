// import_boundary_test.go 는 task-006 검증 조건 중 패키지 import 경계를 회귀 보호하는 테스트다.
// SPEC §5.5, ANALYSIS §1.5:
//   - vectorstore 는 document·llm·tool·database 를 import하고, Chroma/Supabase 등 외부 벡터 백엔드 SDK는
//     import하지 않는다.
//   - database 는 vectorstore 를 역참조하지 않는다(vectorstore→database 단방향).
//   - document 는 모듈 내 상위 패키지(llm/tool/graph/vectorstore/database 등)를 import하지 않는다.
//
// go/build 로 모듈 내부 소스를 정적 파싱한다. 런타임에 go list 같은 하위 프로세스를 띄우지 않으므로
// 빌드 캐시 잠금·안티바이러스 행위탐지(temp 실행파일이 자식 프로세스 spawn)로 인한 비정상 종료(exit 259)가 없다
// (database/import_boundary_test.go 와 동일 패턴). 이전에는 go list -deps 서브프로세스 방식이었으나,
// database import 허용으로의 요구사항 변경(ANALYSIS §1.5)에 맞춰 정적 파싱 방식으로 전환했다.
package vectorstore_test

import (
	"go/build"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const vsModPrefix = "github.com/zipkero/langgraph-go"

// vsModuleRoot 는 작업 디렉터리에서 위로 올라가며 go.mod 가 있는 모듈 루트를 찾는다.
func vsModuleRoot(t *testing.T) string {
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

// vsPkgDir 는 모듈 내부 import 경로를 디스크 디렉터리로 변환한다.
func vsPkgDir(root, importPath string) string {
	rel := strings.TrimPrefix(importPath, vsModPrefix)
	rel = strings.TrimPrefix(rel, "/")
	return filepath.Join(root, filepath.FromSlash(rel))
}

// vsIsInternal 은 import 경로가 이 모듈 내부 패키지인지 판별한다.
func vsIsInternal(importPath string) bool {
	return importPath == vsModPrefix || strings.HasPrefix(importPath, vsModPrefix+"/")
}

// vsCollectDeps 는 startPkg 의 전이적 의존을 정적 파싱으로 수집한다.
// 모듈 내부 패키지만 재귀로 내려가며(외부 패키지는 import 문자열만 기록), 빌드 import만 본다(test import 제외).
func vsCollectDeps(t *testing.T, root, startPkg string) (internal, external map[string]bool) {
	t.Helper()
	internal = map[string]bool{}
	external = map[string]bool{}

	var walk func(pkgPath string)
	walk = func(pkgPath string) {
		if internal[pkgPath] {
			return
		}
		internal[pkgPath] = true

		bp, err := build.ImportDir(vsPkgDir(root, pkgPath), 0)
		if err != nil {
			t.Fatalf("%s 정적 파싱 실패: %v", pkgPath, err)
		}
		for _, imp := range bp.Imports {
			if vsIsInternal(imp) {
				walk(imp)
			} else {
				external[imp] = true
			}
		}
	}
	walk(startPkg)
	return internal, external
}

// TestImportBoundary_vectorstore 는 vectorstore 패키지의 import 경계 규칙을 정적 분석으로
// 단정하는 회귀 테스트다(SPEC §5.5, ANALYSIS §1.5).
//
// 검사 항목:
//  1. vectorstore 의 모듈 내부 의존에 document·llm·tool·database 가 포함된다.
//  2. vectorstore 의 전이 의존(외부 패키지)에 Chroma/Supabase 등 금지된 외부 벡터 백엔드 SDK가 없다.
//  3. database 의 모듈 내부 의존에 vectorstore 가 없다(역참조 금지, 단방향 보장).
func TestImportBoundary_vectorstore(t *testing.T) {
	root := vsModuleRoot(t)
	vsPkg := vsModPrefix + "/vectorstore"

	internal, external := vsCollectDeps(t, root, vsPkg)

	t.Run("vectorstore는_document를_import한다", func(t *testing.T) {
		if !internal[vsModPrefix+"/document"] {
			t.Errorf("위반: %s 의 의존 목록에 %s 가 없습니다(SPEC §5.5)", vsPkg, vsModPrefix+"/document")
		}
	})

	t.Run("vectorstore는_llm을_import한다", func(t *testing.T) {
		if !internal[vsModPrefix+"/llm"] {
			t.Errorf("위반: %s 의 의존 목록에 %s 가 없습니다(SPEC §5.5)", vsPkg, vsModPrefix+"/llm")
		}
	})

	t.Run("vectorstore는_tool을_import한다", func(t *testing.T) {
		if !internal[vsModPrefix+"/tool"] {
			t.Errorf("위반: %s 의 의존 목록에 %s 가 없습니다(SPEC §5.5)", vsPkg, vsModPrefix+"/tool")
		}
	})

	t.Run("vectorstore는_database를_import한다", func(t *testing.T) {
		if !internal[vsModPrefix+"/database"] {
			t.Errorf("위반: %s 의 의존 목록에 %s 가 없습니다(SPEC §5.5, ANALYSIS §1.5)", vsPkg, vsModPrefix+"/database")
		}
	})

	t.Run("vectorstore는_금지된_외부벡터백엔드SDK를_import하지_않는다", func(t *testing.T) {
		forbidden := []string{
			"github.com/amikos-tech/chroma-go",
			"github.com/supabase-community/supabase-go",
		}
		for _, f := range forbidden {
			if external[f] {
				t.Errorf("위반: %s 의 의존 목록에 허용되지 않은 패키지 %s 가 포함돼 있습니다(SPEC §5.5)", vsPkg, f)
			}
		}
	})

	t.Run("database는_vectorstore를_역참조하지_않는다", func(t *testing.T) {
		dbInternal, _ := vsCollectDeps(t, root, vsModPrefix+"/database")
		if dbInternal[vsPkg] {
			t.Errorf("위반: %s 의 의존 목록에 %s 가 포함돼 있습니다(역참조 금지, SPEC §5.5)", vsModPrefix+"/database", vsPkg)
		}
	})
}

// TestImportBoundary_document는_상위_패키지를_import하지_않는다 는 document 의 의존 목록에
// 모듈 내 상위 패키지(llm/tool/graph/vectorstore/database 등)가 없음을 검증한다(SPEC §5.9).
func TestImportBoundary_document는_상위_패키지를_import하지_않는다(t *testing.T) {
	root := vsModuleRoot(t)
	docPkg := vsModPrefix + "/document"

	internal, _ := vsCollectDeps(t, root, docPkg)

	// document 가 import 해서는 안 되는 모듈 내 상위 패키지 목록
	forbidden := []string{
		vsModPrefix + "/llm",
		vsModPrefix + "/tool",
		vsModPrefix + "/vectorstore",
		vsModPrefix + "/database",
		vsModPrefix + "/graph",
		vsModPrefix + "/graph/command",
		vsModPrefix + "/agent",
		vsModPrefix + "/prebuilt",
		vsModPrefix + "/middleware",
		vsModPrefix + "/checkpoint",
		vsModPrefix + "/streaming",
		vsModPrefix + "/structured",
		vsModPrefix + "/prompt",
		vsModPrefix + "/message",
	}
	for _, f := range forbidden {
		if internal[f] {
			t.Errorf("위반: %s 의 의존 목록에 허용되지 않은 패키지 %s 가 포함돼 있습니다(SPEC §5.9)", docPkg, f)
		}
	}
}
