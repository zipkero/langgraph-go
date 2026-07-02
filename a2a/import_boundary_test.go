// import_boundary_test.go 는 task-006 검증 조건 중 패키지 import 경계를 회귀 보호하는 테스트를 담는다.
// SPEC §5.8, §5.1:
//   - a2a 는 agent·config·structured·message·tool·core 와 표준 라이브러리만 import하고,
//     gRPC/protobuf·외부 a2a SDK(google.golang.org/grpc, google.golang.org/protobuf,
//     a2aproject/a2a-go 등)가 a2a 자신의 의존 그래프에 없다.
//   - 하위 패키지(특히 agent·config·structured·message·tool·core)가 a2a 를 역참조하지 않는다.
//   - Phase 0~6 패키지의 기존 동작은 수정되지 않는다.
//
// go/build 로 모듈 내부 소스를 정적 파싱한다. 런타임에 `go list` 같은 하위 프로세스를 띄우지 않으므로
// 빌드 캐시 잠금·안티바이러스 행위탐지(temp 실행파일이 자식 프로세스 spawn)로 인한 비정상 종료가 없다.
// 금지 외부 의존(gRPC/protobuf/외부 a2a SDK) 검사는 a2a 자신의 전이 의존(collectDeps 가 수집하는 external)
// 범위로 국한한다. go.mod 전체를 스캔하는 매니페스트 검사는 두지 않는다 — storage 등 무관 패키지가 자신의
// 필요로 google.golang.org/api 계열을 go.mod 에 추가하는 것은 이 경계와 무관하다(phase7-and-docs task-003,
// 사용자 결정: Drive SDK 유지 + a2a 정책 완화로 개정됨. 과거 "SDK/gRPC 미추가"는 a2a 자신에 한정된 결정이었다).
package a2a_test

import (
	"go/build"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const modPrefix = "github.com/zipkero/langgraph-go"

// 금지 외부 경로/모듈 접두사: gRPC / protobuf / 외부 a2a SDK.
var forbiddenPrefixes = []string{
	"google.golang.org/grpc",
	"google.golang.org/protobuf",
	"a2aproject/",
	"google.golang.org/api",
}

// moduleRoot 는 현재 작업 디렉터리(테스트 실행 시 a2a 패키지 디렉터리)에서 위로 올라가며
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

// TestImportBoundary 는 a2a 패키지의 import 경계 규칙 전체를 정적 분석으로 단정하는 회귀 테스트다(SPEC §5.8, §5.1).
//
// 검사 항목:
//  1. a2a 의 모듈 내부 의존이 허용 집합(agent·config·structured·message·tool·core 및 그 전이 의존) 안에 있다.
//  2. a2a 의 전이 의존(모듈 내부 패키지가 직접 import하는 외부 경로)에 gRPC·protobuf·외부 a2a SDK 가 없다.
//  3. 하위 패키지(agent·config·structured·message·tool·core)의 전이 의존에 a2a 가 없다(역참조 금지).
func TestImportBoundary(t *testing.T) {
	root := moduleRoot(t)
	a2aPkg := modPrefix + "/a2a"

	internal, external := collectDeps(t, root, a2aPkg)

	t.Run("a2a_모듈내부_의존_허용집합_이내", func(t *testing.T) {
		// SPEC §3: agent·config·structured·message·tool·core 와 그 전이 의존(checkpoint·llm·middleware 포함).
		allowed := map[string]bool{
			modPrefix + "/a2a":        true,
			modPrefix + "/agent":      true,
			modPrefix + "/config":     true,
			modPrefix + "/structured": true,
			modPrefix + "/message":    true,
			modPrefix + "/tool":       true,
			modPrefix + "/core":       true,
			// agent 의 전이 의존
			modPrefix + "/checkpoint": true,
			modPrefix + "/llm":        true,
			modPrefix + "/middleware": true,
		}
		for d := range internal {
			if !allowed[d] {
				t.Errorf("위반: %s 의 모듈 내부 의존에 허용되지 않은 패키지 %s 가 포함돼 있습니다(SPEC §5.8)",
					a2aPkg, d)
			}
		}
	})

	t.Run("a2a_금지외부의존_없음", func(t *testing.T) {
		for d := range external {
			for _, prefix := range forbiddenPrefixes {
				if strings.HasPrefix(d, prefix) {
					t.Errorf("위반: %s 의존 목록에 금지된 외부 패키지 %s 가 포함돼 있습니다(SPEC §5.8)",
						a2aPkg, d)
				}
			}
		}
	})

	t.Run("하위패키지_역참조_없음", func(t *testing.T) {
		lowerPkgs := []string{
			modPrefix + "/agent",
			modPrefix + "/config",
			modPrefix + "/structured",
			modPrefix + "/message",
			modPrefix + "/tool",
			modPrefix + "/core",
		}
		for _, lp := range lowerPkgs {
			lowerInternal, _ := collectDeps(t, root, lp)
			if lowerInternal[a2aPkg] {
				t.Errorf("위반: 하위 패키지 %s 의 의존 목록에 %s 가 포함돼 있습니다(역참조 금지, SPEC §5.1)",
					lp, a2aPkg)
			}
		}
	})
}
