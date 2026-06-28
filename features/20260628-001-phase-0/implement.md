# phase-0 — 토대 (leaf) IMPLEMENT

- [x] task-001: config 패키지 — 환경 로딩과 RunConfig 접근자
  - 목적: 호출자가 `LoadEnv(path)`로 `.env`/환경에서 `ANTHROPIC_API_KEY`(챗)·`OPENAI_API_KEY`(임베딩)·
    `TAVILY_API_KEY`·`SUPABASE_URL`/`SUPABASE_KEY`를 읽어 `Config`로 회수할 수 있고, 파일 부재 등 실패 시
    error를 받으며,
    `GetThreadID`/`GetUserID`로 thread_id/user_id를(없으면 빈 문자열) `GetConfigurable`로 임의 키의
    값과 존재 여부를 얻을 수 있다.
  - 접근: 모듈 경로 `github.com/zipkero/langgraph-go`로 `go.mod`를 초기화한다. 모듈 내 어떤 패키지도
    import하지 않는 `config` 패키지에 `Config`/`RunConfig`(`Configurable map[string]any`)/`ModelConfig`/
    `ServerConfig`/`AgentConfig` 타입과 `LoadEnv`/`GetThreadID`/`GetUserID`/`GetConfigurable`를 둔다.
    `LoadEnv`는 표준 라이브러리만으로 `KEY=VALUE` 줄을 읽어 환경에 반영한 뒤 `os.Getenv`로 회수하고,
    대상 파일이 없으면 error를 반환한다. 접근자는 `RunConfig.Configurable`에서 키로 읽는다.
  - 검증 조건:
    - 결과: `LoadEnv`가 대상 `.env`에서 다섯 자격증명(`ANTHROPIC_API_KEY`/`OPENAI_API_KEY`/`TAVILY_API_KEY`/
      `SUPABASE_URL`/`SUPABASE_KEY`)을 `Config`로 회수 가능하게 채우고, 파일 부재 시 error를 반환한다. `GetThreadID`/`GetUserID`는 키 존재 시 값을, 부재 시 빈 문자열을 반환하고,
      `GetConfigurable`는 `(value, true)` / `(nil, false)`를 반환한다. 어셈블리 함수
      (`LoadMCPServers`/`AgentURLs`/`GetAgentConfig`)는 존재하지 않는다. `config`는 모듈 내 다른 패키지를
      import하지 않는다.
    - 확인: 단위 테스트로 (a) `LoadEnv` 성공 경로(다섯 키 회수)와 실패 경로(파일 부재 시 error),
      (b) 접근자의 키 존재/부재 분기(`GetThreadID`/`GetUserID` 빈 문자열, `GetConfigurable`의 bool)를
      커버하고 로컬에서 통과한다. `go build ./config` / `go vet ./config` 오류 없음.
  - 참조: SPEC §5.1, §5.2, §5.3, §5.5 / ANALYSIS §3, §5(D4·D5·D6)

- [x] task-002: core 패키지 — 공유 원시 타입과 모듈 leaf 경계 확정
  - 목적: 호출자가 맵 기반 `State`/`StateUpdate`를 만들고, `Mode`를 `values`/`messages`/`updates`/`debug`
    값으로 지정하며, `StateSnapshot`의 `Values`/`Next`/`Config`/`Metadata`/`CreatedAt` 필드를 구성·접근할 수
    있고, `StateSnapshot.Config`는 `config.RunConfig` 타입이며, 모듈 전체가 빌드되고 import 그래프에서
    `core`가 `config`만 의존하는 leaf로 관찰된다.
  - 접근: 모듈 내에서 `config`만 import하는 `core` 패키지에 맵 기반 `State`/`StateUpdate`, 명명된 문자열
    타입 `Mode`와 네 상수, `StateSnapshot`(필드 `Values`/`Next`/`Config`/`Metadata`/`CreatedAt`,
    `Config` 타입은 `config.RunConfig`)을 정의한다.
  - 검증 조건:
    - 결과: 네 타입이 사용 가능하고 `Mode` 상수 값이 `values`/`messages`/`updates`/`debug`이며,
      `StateSnapshot.Config` 필드가 `config.RunConfig` 타입으로 컴파일된다. 모듈 루트에서 전체 빌드·정적검사가
      오류 없이 끝나고, import 그래프에서 `config`는 모듈 내 다른 패키지를 import하지 않으며 `core`는
      `config`만 import한다.
    - 확인: 단위 테스트로 `State`/`StateUpdate` 키-값 set/get, `Mode` 상수 값, `StateSnapshot` 각 필드
      구성·접근 및 `Config`에 `config.RunConfig` 값 대입을 검증하고 로컬에서 통과한다. 모듈 루트에서
      `go build ./...` / `go vet ./...` 오류 없음. `go list -deps`(또는 `go list -f '{{.Imports}}'`)로 두
      패키지의 모듈 내 import 집합이 위 결과(`config` 없음, `core`는 `config`만)와 일치함을 확인한다.
  - 참조: SPEC §5.1, §5.4, §5.5 / ANALYSIS §1, §3, §5(D1·D2·D3)
