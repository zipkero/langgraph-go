// import_boundary_test.go 는 task-001 검증 조건 중 패키지 import 경계를 회귀 보호하는 테스트를 담는다.
// SPEC §5.1, ANALYSIS §1.1:
//   - database 는 tool·표준 라이브러리·DB 드라이버(pgx/pgvector-go)에만 의존하고,
//     상위 패키지(vectorstore/rag 등)를 import하지 않는다.
//   - tool(및 그 전이 의존: config·message)이 database 를 역참조하지 않는다.
//
// go/build 로 모듈 내부 소스를 정적 파싱한다. 런타임에 `go list` 같은 하위 프로세스를 띄우지 않으므로
// 빌드 캐시 잠금·안티바이러스 행위탐지(temp 실행파일이 자식 프로세스 spawn)로 인한 비정상 종료(exit 259)가 없다
// (a2a/import_boundary_test.go·multiagent/import_boundary_test.go와 동일 패턴).
package database_test

import (
	"go/build"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const dbModPrefix = "github.com/zipkero/langgraph-go"

// dbModuleRoot 는 작업 디렉터리에서 위로 올라가며 go.mod 가 있는 모듈 루트를 찾는다.
func dbModuleRoot(t *testing.T) string {
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

// dbPkgDir 는 모듈 내부 import 경로를 디스크 디렉터리로 변환한다.
func dbPkgDir(root, importPath string) string {
	rel := strings.TrimPrefix(importPath, dbModPrefix)
	rel = strings.TrimPrefix(rel, "/")
	return filepath.Join(root, filepath.FromSlash(rel))
}

// dbIsInternal 은 import 경로가 이 모듈 내부 패키지인지 판별한다.
func dbIsInternal(importPath string) bool {
	return importPath == dbModPrefix || strings.HasPrefix(importPath, dbModPrefix+"/")
}

// dbCollectDeps 는 startPkg 의 전이적 의존을 정적 파싱으로 수집한다.
// 모듈 내부 패키지만 재귀로 내려가며(외부 패키지는 import 문자열만 기록), 빌드 import만 본다(test import 제외).
func dbCollectDeps(t *testing.T, root, startPkg string) (internal, external map[string]bool) {
	t.Helper()
	internal = map[string]bool{}
	external = map[string]bool{}

	var walk func(pkgPath string)
	walk = func(pkgPath string) {
		if internal[pkgPath] {
			return
		}
		internal[pkgPath] = true

		bp, err := build.ImportDir(dbPkgDir(root, pkgPath), 0)
		if err != nil {
			t.Fatalf("%s 정적 파싱 실패: %v", pkgPath, err)
		}
		for _, imp := range bp.Imports {
			if dbIsInternal(imp) {
				walk(imp)
			} else {
				external[imp] = true
			}
		}
	}
	walk(startPkg)
	return internal, external
}

// TestImportBoundary_database 는 database 패키지의 import 경계 규칙을 정적 분석으로 단정하는 회귀 테스트다
// (SPEC §5.1, ANALYSIS §1.1).
//
// 검사 항목:
//  1. database 의 모듈 내부 의존이 허용 집합(tool 및 그 전이 의존: config·message) 안에 있다.
//  2. database 의 전이 의존에 상위 패키지(vectorstore/rag 등)가 없다.
//  3. 하위 패키지(tool·config·message)의 전이 의존에 database 가 없다(역참조 금지).
func TestImportBoundary_database(t *testing.T) {
	root := dbModuleRoot(t)
	dbPkg := dbModPrefix + "/database"

	internal, external := dbCollectDeps(t, root, dbPkg)

	t.Run("database_모듈내부_의존_허용집합_이내", func(t *testing.T) {
		allowed := map[string]bool{
			dbModPrefix + "/database": true,
			dbModPrefix + "/tool":     true,
			// tool 의 전이 의존
			dbModPrefix + "/config":  true,
			dbModPrefix + "/message": true,
		}
		for d := range internal {
			if !allowed[d] {
				t.Errorf("위반: %s 의 모듈 내부 의존에 허용되지 않은 패키지 %s 가 포함돼 있습니다(SPEC §5.1)",
					dbPkg, d)
			}
		}
	})

	t.Run("database_상위패키지_역참조없음", func(t *testing.T) {
		forbidden := []string{
			dbModPrefix + "/vectorstore",
			dbModPrefix + "/rag",
		}
		for d := range internal {
			for _, f := range forbidden {
				if d == f {
					t.Errorf("위반: %s 의 모듈 내부 의존에 상위 패키지 %s 가 포함돼 있습니다(SPEC §5.1)", dbPkg, d)
				}
			}
		}
		_ = external // 외부(비모듈) 의존은 pgx/pgvector-go 등 DB 드라이버로 제한 없이 허용된다(D1).
	})

	t.Run("하위패키지_역참조_없음", func(t *testing.T) {
		lowerPkgs := []string{
			dbModPrefix + "/tool",
			dbModPrefix + "/config",
			dbModPrefix + "/message",
		}
		for _, lp := range lowerPkgs {
			lowerInternal, _ := dbCollectDeps(t, root, lp)
			if lowerInternal[dbPkg] {
				t.Errorf("위반: 하위 패키지 %s 의 의존 목록에 %s 가 포함돼 있습니다(역참조 금지, SPEC §5.1)",
					lp, dbPkg)
			}
		}
	})
}
