// import_boundary_test.go 는 task-004 검증 조건 중 config의 leaf 유지를 회귀 보호하는 테스트다.
// SPEC §5.4, ANALYSIS §1.4:
//   - config는 mcp를 비롯한 상위 패키지를 import하지 않는 무의존 최하위 leaf다.
//   - 어셈블리 함수(LoadMCPServers/AgentURLs/GetAgentConfig)가 추가돼도 이 경계는 불변이다.
//
// go/build 로 모듈 내부 소스를 정적 파싱한다. 런타임에 `go list` 같은 하위 프로세스를 띄우지 않으므로
// 빌드 캐시 잠금·안티바이러스 행위탐지로 인한 비정상 종료(exit 259)가 없다
// (database/import_boundary_test.go 등과 동일 패턴).
package config_test

import (
	"go/build"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const cfgModPrefix = "github.com/zipkero/langgraph-go"

// cfgModuleRoot 는 작업 디렉터리에서 위로 올라가며 go.mod 가 있는 모듈 루트를 찾는다.
func cfgModuleRoot(t *testing.T) string {
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

// cfgPkgDir 는 모듈 내부 import 경로를 디스크 디렉터리로 변환한다.
func cfgPkgDir(root, importPath string) string {
	rel := strings.TrimPrefix(importPath, cfgModPrefix)
	rel = strings.TrimPrefix(rel, "/")
	return filepath.Join(root, filepath.FromSlash(rel))
}

// cfgIsInternal 은 import 경로가 이 모듈 내부 패키지인지 판별한다.
func cfgIsInternal(importPath string) bool {
	return importPath == cfgModPrefix || strings.HasPrefix(importPath, cfgModPrefix+"/")
}

// cfgCollectDeps 는 startPkg 의 전이적 의존을 정적 파싱으로 수집한다.
// 모듈 내부 패키지만 재귀로 내려가며(외부 패키지는 import 문자열만 기록), 빌드 import만 본다(test import 제외).
func cfgCollectDeps(t *testing.T, root, startPkg string) (internal, external map[string]bool) {
	t.Helper()
	internal = map[string]bool{}
	external = map[string]bool{}

	var walk func(pkgPath string)
	walk = func(pkgPath string) {
		if internal[pkgPath] {
			return
		}
		internal[pkgPath] = true

		bp, err := build.ImportDir(cfgPkgDir(root, pkgPath), 0)
		if err != nil {
			t.Fatalf("%s 정적 파싱 실패: %v", pkgPath, err)
		}
		for _, imp := range bp.Imports {
			if cfgIsInternal(imp) {
				walk(imp)
			} else {
				external[imp] = true
			}
		}
	}
	walk(startPkg)
	return internal, external
}

// TestImportBoundary_config 는 config 패키지가 무의존 최하위 leaf임을 정적 분석으로 단정하는
// 회귀 테스트다(SPEC §5.4, ANALYSIS §1.4).
//
// 검사 항목:
//  1. config 의 모듈 내부 의존이 자기 자신뿐이다(다른 어떤 내부 패키지도 import하지 않는다).
//  2. config 는 표준 라이브러리 외부 의존을 갖지 않는다(외부 SDK 미도입).
func TestImportBoundary_config(t *testing.T) {
	root := cfgModuleRoot(t)
	cfgPkg := cfgModPrefix + "/config"

	internal, external := cfgCollectDeps(t, root, cfgPkg)

	t.Run("config_모듈내부_무의존", func(t *testing.T) {
		for d := range internal {
			if d != cfgPkg {
				t.Errorf("위반: %s 의 모듈 내부 의존에 %s 가 포함돼 있습니다(config는 leaf여야 함, SPEC §5.4)",
					cfgPkg, d)
			}
		}
	})

	t.Run("config_표준라이브러리외_외부의존없음", func(t *testing.T) {
		for e := range external {
			// 표준 라이브러리는 '.'을 포함하지 않는 단일 경로 세그먼트 또는 알려진 표준 경로 규칙을 따른다.
			// 외부 모듈 경로는 도메인(.)을 포함하므로 이를 기준으로 판별한다.
			if strings.Contains(strings.SplitN(e, "/", 2)[0], ".") {
				t.Errorf("위반: %s 의 외부 의존에 표준 라이브러리가 아닌 %s 가 포함돼 있습니다(config는 leaf여야 함, SPEC §5.4)",
					cfgPkg, e)
			}
		}
	})
}
