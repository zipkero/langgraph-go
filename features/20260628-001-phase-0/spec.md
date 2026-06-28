# phase-0 — 토대 (leaf)

## 1. 범위

langgraph-go 라이브러리의 의존 트리 최하단(leaf)을 구성하는 Phase 0이다. 다음을 다룬다.

- 모듈 초기화: `go.mod`의 모듈 경로를 `github.com/zipkero/langgraph-go`로 둔다. 이후 모든 패키지가
  `github.com/zipkero/langgraph-go/<pkg>` 형태로 import된다.
- `config` 패키지: 실행 설정과 식별자 관리. RunConfig, thread_id/user_id 추출, 환경 로딩, 설정 타입
  (README §24). 단 A2A/MCP 어셈블리 함수(LoadMCPServers / AgentURLs / GetAgentConfig)는 제외한다(§4).
- `core` 패키지: 공유 원시 타입(State, StateUpdate, Mode, StateSnapshot). `config`에만 단방향 의존하는 leaf
  (README §1·§28-1).

이후 모든 Phase(§26)가 이 두 패키지의 구체 타입을 참조한다.

## 2. 목표

순환 import와 Phase 역전을 구조적으로 차단하는 무의존/단일의존 leaf 토대를 먼저 확정한다. `State`/`StateUpdate`/
`Mode`/`StateSnapshot`을 `core`에, `RunConfig`를 `config`에 내려두면, 상위 패키지(`graph`/`agent`/`checkpoint`/
`command`/`streaming` 등)가 서로를 역참조하지 않고도 동일한 원시 타입을 공유할 수 있다(README §28-1 규칙1·4).
이 토대가 존재해야 Phase 1 핵심 런타임이 `store`/`trace` 구현 없이도 인터페이스만으로 컴파일된다.

## 3. 제약

- 의존 방향: `config`는 모듈 내 어떤 패키지도 import하지 않는 무의존 최하위 leaf다. `core`는 `config`만
  단방향으로 import하는 leaf이며, 그 외 모듈 내 패키지를 import하지 않는다(README §28-1 규칙1).
- `core.StateSnapshot`의 `Config` 필드 타입은 `config.RunConfig`다. 이 때문에 `core → config` 단방향 의존이
  생기며, 역방향(`config → core`)은 없다(순환 없음).
- `Mode`의 값 집합은 `values` / `messages` / `updates` / `debug`다(README §13). 스트림 모드의 소유자는 `core`이며,
  Phase 2 이후 `streaming.Mode` / `graph` 시그니처는 이 타입의 alias로 참조한다.
- Go 버전은 최신 안정 버전을 사용한다(가정). 외부 의존성은 최소화하며, `config`의 환경 로딩은 표준 라이브러리
  또는 최소한의 `.env` 파서로 충족한다(구체 선택은 analysis 단계).

## 4. 제외 범위

- `config`의 A2A/MCP 어셈블리 함수 `LoadMCPServers` / `AgentURLs` / `GetAgentConfig`는 Phase 7로 미룬다
  (README §26 Phase 0·§28-1). 이들이 다루는 엔드포인트·에이전트 맵 조립은 Phase 0에서 구현하지 않는다.
  (`ServerConfig`/`AgentConfig` 타입 정의 자체는 §1 범위에 포함된다 — 어셈블리 "함수"만 제외.)
- Phase 1 이후의 모든 패키지(`message`/`llm`/`tool`/`structured`/`prompt`/`agent`/`middleware`/`prebuilt`/
  `checkpoint`/`graph`/`command`/`streaming` 등)는 범위 밖이다.
- `core`는 원시 타입 정의만 담는다. 리듀서 적용·그래프 실행·스냅샷 영속 등 동작 로직은 상위 패키지
  (`graph` Phase 2, `checkpoint` Phase 1) 소관이다.
- 실제 LLM·외부 서비스(Anthropic/OpenAI/Supabase/Tavily 등) 연동은 다루지 않는다. `config`는 자격증명 값을
  읽어 보관할 뿐 외부를 호출하지 않는다.

## 5. 완료 조건

1. 모듈 루트에서 `go build ./...`와 `go vet ./...`가 오류 없이 끝난다(`go.mod` 모듈 경로는
   `github.com/zipkero/langgraph-go`).
2. `GetThreadID(cfg)`와 `GetUserID(cfg)`는 `RunConfig`에 담긴 thread_id / user_id 값을 반환하고, 해당 키가
   없으면 빈 문자열을 반환한다. `GetConfigurable(cfg, key)`는 값과 존재 여부 `(any, bool)`를 반환한다.
3. `LoadEnv(path)`는 대상 `.env`/환경의 변수를 읽어 `Config`를 구성해 반환하며, `ANTHROPIC_API_KEY`(챗)·
   `OPENAI_API_KEY`(임베딩)·`TAVILY_API_KEY`·`SUPABASE_URL`/`SUPABASE_KEY` 값을 호출자가 회수할 수 있다.
   대상 파일 부재 등 실패 상황에서는 error를 반환한다.
4. 호출자가 `State`·`StateUpdate`(맵 기반 상태/갱신), `Mode`(`values`/`messages`/`updates`/`debug`),
   `StateSnapshot`(`Values`/`Next`/`Config`/`Metadata`/`CreatedAt`)을 생성하고 각 필드에 접근할 수 있으며,
   `StateSnapshot.Config`의 타입은 `config.RunConfig`다.
5. import 그래프 검사(`go list -deps` 등)로 `config`가 모듈 내 다른 패키지를 import하지 않고, `core`는
   `config`만 import함을 확인할 수 있다.
