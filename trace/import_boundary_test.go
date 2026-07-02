// import_boundary_test.go 는 task-006 검증 조건 중 패키지 import 경계를 회귀 보호하는 테스트를 담는다.
// SPEC §5.7, §5.1, §3:
//   - trace 는 모듈 내부에서 graph·message·llm·tool·core·config 와 그 전이 의존, 그리고 표준 라이브러리만
//     import한다.
//   - 하위 패키지(특히 graph·tool·message·llm·core·config)가 trace 를 역참조하지 않는다.
//   - Phase 0~6 패키지의 기존 동작은 수정되지 않는다.
//
// go/build 로 모듈 내부 소스를 정적 파싱한다. 런타임에 `go list` 같은 하위 프로세스를 띄우지 않으므로
// 빌드 캐시 잠금·안티바이러스 행위탐지(temp 실행파일이 자식 프로세스 spawn)로 인한 비정상 종료가 없다
// (a2a/import_boundary_test.go 와 동일 방식, ANALYSIS Decision (f)).
//
// trace 는 외부 SDK 사용 금지 요구가 없으므로(spec §3은 렌더링 라이브러리·네트워크 미사용만 요구하며,
// 이는 코드에 그런 import 자체가 없다는 사실로 이미 충족된다) a2a식 forbidden-prefix·go.mod 매니페스트
// 검사는 두지 않는다 — 필요한 범위(모듈 내부 의존 허용집합 + 하위 패키지 역참조 금지)만 검사한다.
package trace_test

import (
	"go/build"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const modPrefix = "github.com/zipkero/langgraph-go"

// moduleRoot 는 현재 작업 디렉터리(테스트 실행 시 trace 패키지 디렉터리)에서 위로 올라가며
// go.mod 가 있는 모듈 루트 경로를 찾는다.
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

// collectDeps 는 startPkg 의 전이적 의존을 정적 파싱으로 수집한다.
// 모듈 내부 패키지만 재귀로 내려가며(외부 패키지는 import 문자열만 기록), 빌드 import만 본다(test import 제외).
//   - internal: 방문한 모듈 내부 패키지 집합(startPkg 포함)
//   - external: 모듈 외부(표준 라이브러리 포함) import 경로 집합
func collectDeps(t *testing.T, root, startPkg string) (internal, external map[string]bool) {
	t.Helper()
	internal = map[string]bool{}
	external = map[string]bool{}

	var walk func(pkgPath string)
	walk = func(pkgPath string) {
		if internal[pkgPath] {
			return
		}
		internal[pkgPath] = true

		bp, err := build.ImportDir(pkgDir(root, pkgPath), 0)
		if err != nil {
			t.Fatalf("%s 정적 파싱 실패: %v", pkgPath, err)
		}
		for _, imp := range bp.Imports {
			if isInternal(imp) {
				walk(imp)
			} else {
				external[imp] = true
			}
		}
	}
	walk(startPkg)
	return internal, external
}

// TestImportBoundary 는 trace 패키지의 import 경계 규칙을 정적 분석으로 단정하는 회귀 테스트다(SPEC §5.7, §5.1).
//
// 검사 항목:
//  1. trace 의 모듈 내부 의존이 허용 집합(graph·message·llm·tool·core·config 및 그 전이 의존) 안에 있다.
//  2. 하위 패키지(graph·tool·message·llm·core·config)의 전이 의존에 trace 가 없다(역참조 금지).
func TestImportBoundary(t *testing.T) {
	root := moduleRoot(t)
	tracePkg := modPrefix + "/trace"

	internal, _ := collectDeps(t, root, tracePkg)

	t.Run("trace_모듈내부_의존_허용집합_이내", func(t *testing.T) {
		// SPEC §3: graph·message·llm·tool·core·config 와 그 전이 의존.
		// graph -> checkpoint, config, core, graph/command
		// llm -> message, structured, tool
		// tool -> config, message
		// core -> config
		// checkpoint -> config, core
		// graph/command -> core
		allowed := map[string]bool{
			modPrefix + "/trace":   true,
			modPrefix + "/graph":   true,
			modPrefix + "/message": true,
			modPrefix + "/llm":     true,
			modPrefix + "/tool":    true,
			modPrefix + "/core":    true,
			modPrefix + "/config":  true,
			// graph 의 전이 의존
			modPrefix + "/checkpoint":    true,
			modPrefix + "/graph/command": true,
			// llm 의 전이 의존
			modPrefix + "/structured": true,
		}
		for d := range internal {
			if !allowed[d] {
				t.Errorf("위반: %s 의 모듈 내부 의존에 허용되지 않은 패키지 %s 가 포함돼 있습니다(SPEC §5.7)",
					tracePkg, d)
			}
		}
	})

	t.Run("하위패키지_역참조_없음", func(t *testing.T) {
		lowerPkgs := []string{
			modPrefix + "/graph",
			modPrefix + "/tool",
			modPrefix + "/message",
			modPrefix + "/llm",
			modPrefix + "/core",
			modPrefix + "/config",
		}
		for _, lp := range lowerPkgs {
			lowerInternal, _ := collectDeps(t, root, lp)
			if lowerInternal[tracePkg] {
				t.Errorf("위반: 하위 패키지 %s 의 의존 목록에 %s 가 포함돼 있습니다(역참조 금지, SPEC §5.1)",
					lp, tracePkg)
			}
		}
	})
}
