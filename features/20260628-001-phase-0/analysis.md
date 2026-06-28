# phase-0 — 토대 (leaf) ANALYSIS

## 근거

작성 전에 읽은 것과 코드베이스에서 확인한 사실을 분리해 적는다.

**읽은 spec.md 범위**: spec.md 전체(§1 범위 ~ §5 완료 조건). 범위는 §1로 제한되며, `config`·`core`
두 패키지와 `go.mod` 초기화로 한정된다.

**읽은 README.md 범위**: §1(패키지 구조 — `config` 무의존 leaf, `core`는 `config`에만 의존하는 leaf),
§7(graph — `State`/`StateUpdate`/`StateSnapshot`/`Mode`가 `core` alias이고 `StateSnapshot` 필드 구성이
`Values`/`Next`/`Config`/`Metadata`/`CreatedAt`이며 `Config` 필드가 `config.RunConfig`라는 설명),
§13(streaming — `Mode` 값 집합 `values`/`messages`/`updates`/`debug`, `Mode`가 `core.Mode` alias),
§24(config — `Config`/`RunConfig`/`ModelConfig`/`ServerConfig`/`AgentConfig` 타입과 `LoadEnv`/`GetThreadID`/
`GetUserID`/`GetConfigurable`/`AgentURLs`/`GetAgentConfig`/`LoadMCPServers` 함수, 읽는 환경변수 목록),
§26 Phase 0(범위와 어셈블리 함수 Phase 7 분리, store/trace 미구현 상태로도 Phase 1 컴파일),
§27(핵심 최소 집합 — `config`/`core` 행), §28-1(import 사이클 회피 규칙 1~4와 leaf 경계 근거).

**코드베이스에서 확인한 사실**:
- 저장소는 그린필드다. `go.mod` 없음, `.go` 소스 파일 없음, 패키지 디렉토리 없음. `README.md`만 존재한다.
- 따라서 이 feature가 건드릴 기존 호출자·구현체·저장 데이터·외부 contract가 없다. 새로 만드는 leaf라
  깨질 기존 계약이 없다(§4 영향 범위에 반영).

**이미 확정된 결정** (질문 없이 §5 채택안으로 반영):
- `go.mod` 모듈 경로는 `github.com/zipkero/langgraph-go`. 이후 패키지는
  `github.com/zipkero/langgraph-go/config`, `.../core`로 import된다.
- `config` 범위에서 제외하는 것은 어셈블리 "함수"(`LoadMCPServers`/`AgentURLs`/`GetAgentConfig`)뿐이며,
  `ServerConfig`/`AgentConfig` 타입 정의 자체는 Phase 0 범위에 포함된다.

**추정**: Go 버전은 최신 안정 버전을 쓴다(spec.md §3이 "최신 안정 버전을 사용한다(가정)"로 명시). 정확한
패치 버전은 결과에 영향을 주지 않는 세부라 구현 시점의 안정 버전을 따른다.

## 1. 구조

모듈 루트(`github.com/zipkero/langgraph-go`) 하위에 두 개의 공개 패키지 디렉토리를 둔다.
의존 트리 최하단의 leaf 경계를 확정하는 것이 이 Phase의 본질이며, 파일이 아니라 경계로 기술한다.

- `config/` — 무의존 최하위 leaf. 모듈 내 어떤 패키지도 import하지 않는다. 외부로는 표준 라이브러리만
  의존한다(.env 파싱 선택은 §5에서 commit). 실행 설정·식별자·환경 로딩을 담는다.
- `core/` — `config`에만 단방향 의존하는 leaf. 모듈 내에서는 `config`만 import하고 그 외 어떤 패키지도
  import하지 않는다. 공유 원시 타입(`State`/`StateUpdate`/`Mode`/`StateSnapshot`)을 담는다.

**의존 방향과 순환 차단**: `core.StateSnapshot`의 `Config` 필드 타입이 `config.RunConfig`라서 `core → config`
단방향 의존이 생긴다. 역방향(`config → core`)은 없으므로 순환이 아니다(SPEC §5.5). 이 두 leaf가 공유 원시
타입의 단일 소유자가 됨으로써, 이후 Phase의 상위 패키지들이 서로를 역참조하지 않고도 동일 타입을 공유한다.
구체적으로 `core`가 `StateSnapshot`을 소유하므로 Phase 1의 `checkpoint`·`agent`가 Phase 2의 `graph`를
역참조하지 않고 `core.StateSnapshot`만 보면 되고(Phase 역전 차단), `core`가 `Mode`를 소유하므로 `graph`·
`agent`가 Phase 2의 `streaming`을 역참조하지 않고 `core.Mode`만 보면 된다(README §28-1 규칙1). 이 leaf
토대가 있어야 Phase 1 핵심 런타임이 `store`(Phase 5)·`trace`(Phase 7) 구현 없이도 인터페이스만으로
컴파일된다(README §26 Phase 0).

**경계의 외부 노출 계약**: 상위 패키지(`graph`/`streaming`/`checkpoint`/`agent`)는 이 두 leaf 타입을 그대로
쓰거나 alias로 재노출한다(예: `type State = core.State`). 그 alias 정의는 각 상위 Phase 소관이며 Phase 0
범위 밖이다. Phase 0은 alias가 가리킬 원본 타입만 제공한다.

**모듈 레이아웃**: 모듈 루트에 `go.mod`(경로 `github.com/zipkero/langgraph-go`), 그 하위에 `config/`와
`core/` 두 디렉토리. 각 디렉토리는 디렉토리명과 같은 패키지명(`package config`, `package core`)을 갖는다.

## 2. 데이터 흐름

Phase 0은 동작 로직이 아니라 타입과 조회·로딩 함수만 담는 leaf라, "request → 처리 → response" 흐름은
호출자가 데이터를 넣고 빼는 두 갈래로 좁다. 그래프 실행·리듀서 적용·스냅샷 영속 같은 동작은 상위 Phase
소관이다(spec.md §4).

**갈래 1 — 환경 로딩 (`config.LoadEnv`)**:
호출자가 `.env` 파일 경로를 넘긴다 → `LoadEnv(path)`가 대상 `.env`/프로세스 환경에서 변수를 읽는다 →
`ANTHROPIC_API_KEY`(챗)·`OPENAI_API_KEY`(임베딩)·`TAVILY_API_KEY`·`SUPABASE_URL`/`SUPABASE_KEY`를
`Config`(및 그 안의 `ModelConfig` 등 하위 설정)에 채워 반환한다 → 호출자가 `Config`에서 자격증명 값을
회수한다(SPEC §5.3). 실패 경로: 대상 파일 부재 등으로 로딩이 불가하면 `error`를 반환하고 `Config`는 의미
있는 값을 담지 않는다(SPEC §5.3). `config`는 값을 읽어 보관할 뿐 외부(Anthropic/OpenAI/Supabase/Tavily)를
호출하지 않는다(spec.md §4).

**갈래 2 — 식별자 조회 (`config.RunConfig` 접근자)**:
호출자가 `RunConfig`를 구성하고 그 `Configurable map[string]any`에 `thread_id`/`user_id` 등을 담는다 →
`GetThreadID(cfg)`/`GetUserID(cfg)`가 `Configurable`에서 해당 키를 읽어 문자열로 반환한다. 키가 없으면 빈
문자열을 반환한다(SPEC §5.2). `GetConfigurable(cfg, key)`는 임의 키에 대해 값과 존재 여부 `(any, bool)`을
반환해, 키 부재와 "값이 비어 있음"을 호출자가 구분할 수 있게 한다(SPEC §5.2). 이 접근자들은 `RunConfig`를
읽기만 하며 외부 상태를 바꾸지 않는다.

**`core` 타입의 흐름**: `core`는 함수가 아닌 데이터 모델만 제공한다. 호출자가 `State`/`StateUpdate`(맵 기반)를
만들어 키-값을 넣고 읽고, `Mode` 값(`values`/`messages`/`updates`/`debug`)을 지정하고, `StateSnapshot`을
구성해 `Values`/`Next`/`Config`/`Metadata`/`CreatedAt` 필드에 접근한다(SPEC §5.4). `StateSnapshot.Config`는
`config.RunConfig`라 스냅샷을 만든 실행 설정이 스냅샷에 그대로 실린다. 상태 전이·도달 가능 상태 집합 같은
"흐르는 상태"는 Phase 0에 없다 — 이 타입들을 소비해 전이를 일으키는 것은 `graph`(Phase 2)·`checkpoint`
(Phase 1)다.

상태 머신이나 다중 경계 통과가 없으므로 다이어그램은 두지 않는다(흐름이 위 두 갈래로 선형이다).

## 3. 인터페이스

경계를 가로지르는 계약은 (a) 다운스트림·상위 Phase가 import해 쓰는 `config`/`core`의 공개 타입·함수,
(b) `core → config` 단방향 타입 참조 두 가지다. README §7·§13·§24의 제안형 시그니처를 Go 관례로 따르되,
README가 "인터페이스 경계만 유지하면 된다"고 한 범위에서 기술한다. 내부 helper는 범위 밖이다.

**`config` 패키지 공개 계약 (README §24)**:
- 타입: `Config`, `RunConfig`(필드 `Configurable map[string]any`), `ModelConfig`, `ServerConfig`(MCP/A2A
  엔드포인트), `AgentConfig`(`Name`/`URL`/`Port`/`Description`). `ServerConfig`/`AgentConfig`는 타입 정의만
  Phase 0에 포함되고, 이를 조립하는 함수는 제외된다(spec.md §4).
- 함수:
  - `LoadEnv(path string) (Config, error)` — 환경 로딩, 실패 시 error(SPEC §5.3).
  - `GetThreadID(cfg RunConfig) string` — 없으면 빈 문자열(SPEC §5.2).
  - `GetUserID(cfg RunConfig) string` — 없으면 빈 문자열(SPEC §5.2).
  - `GetConfigurable(cfg RunConfig, key string) (any, bool)` — 값과 존재 여부(SPEC §5.2).
- Phase 0 제외(시그니처도 만들지 않음): `AgentURLs() map[string]string`, `GetAgentConfig(name string)
  (AgentConfig, error)`, `LoadMCPServers() map[string]ServerConfig` — 어셈블리 함수라 Phase 7로 미룬다
  (spec.md §4, README §26 Phase 0).

**`core` 패키지 공개 계약 (README §7·§13)**:
- `State`, `StateUpdate` — 맵 기반 상태/갱신 타입(SPEC §5.4). 상위에서 `graph.State = core.State`처럼 alias로
  노출되지만 그 alias는 Phase 0 밖이다.
- `Mode` — 값 집합 `values`/`messages`/`updates`/`debug`(SPEC §5.4, README §13). `core`가 스트림 모드의
  소유자이며, Phase 2 이후 `streaming.Mode`/`graph` 시그니처가 이 타입의 alias로 참조한다.
- `StateSnapshot` — 필드 `Values`/`Next`/`Config`/`Metadata`/`CreatedAt`. `Config` 필드 타입은
  `config.RunConfig`라서 여기서 `core → config` 단방향 의존이 발생한다(SPEC §5.4·§5.5).

**모듈 빌드 계약 (SPEC §5.1)**: 모듈 루트에서 `go build ./...`와 `go vet ./...`가 오류 없이 끝난다.
이는 두 패키지의 공개 시그니처가 서로 정합하고(`core`가 `config.RunConfig`를 올바로 참조) 컴파일된다는
경계 계약이다.

**import 그래프 계약 (SPEC §5.5)**: `go list -deps` 등으로 `config`가 모듈 내 다른 패키지를 import하지 않고
(무의존 leaf), `core`는 모듈 내에서 `config`만 import함(단일 의존 leaf)을 외부에서 검증할 수 있다.

## 4. 영향 범위

이 feature는 그린필드에 새 모듈과 두 패키지를 신규 생성한다. 변경 대상의 직접·간접 의존(호출자·구현체·참조)을
탐색한 결과, 저장소에 `go.mod`도 `.go` 소스도 없어(근거 참조) 이 Phase가 건드릴 기존 코드가 없다.

- 신규 생성: `go.mod`, `config/`(패키지 `config`), `core/`(패키지 `core`).
- 기존 모듈·파일·DB 테이블 수정: 해당 없음(건드릴 기존 자산이 없다).
- 하위 호환·마이그레이션: 해당 없음. 깨질 기존 호출자·저장 데이터·외부 contract가 없는 신규 leaf다.
- 이후 Phase에 대한 영향은 "이 두 패키지가 존재해야 상위 Phase가 컴파일된다"는 전방 의존이며, 이는 이
  feature가 깨뜨리는 기존 계약이 아니라 의도된 토대 제공이다(§1 참조).

## 5. Decision Points

### D1. `core.State`/`StateUpdate`의 표현 방식 — 맵 기반

- 고려한 옵션: (a) `map[string]any` 기반 동적 상태, (b) 제네릭/구조체 기반 정적 상태.
- 트레이드오프: (b)는 타입 안전성이 높지만, 그래프 상태는 노드마다 다른 키 집합을 누적·병합하고 리듀서를
  필드별로 등록하는 동적 모델이라(README §7) 정적 구조로는 범용 상태 맵을 표현하기 어렵다. (a)는 LangGraph의
  상태 모델과 직접 대응하고, 상위 `graph`/`agent`가 alias로 그대로 노출하기에 자연스럽다.
- 채택: (a) 맵 기반. spec.md §5.4가 "맵 기반 상태/갱신"을 명시하므로 이를 따른다.
- 근거: spec.md §5.4, README §7. `StateUpdate`도 같은 맵 계열로 두어 리듀서 병합(Phase 2)의 입력이 되게
  한다. 리듀서 적용 로직 자체는 Phase 0이 아니라 `graph`(Phase 2) 소관이다(spec.md §4).

### D2. `core.Mode`의 타입 형태와 값 집합

- 고려한 옵션: (a) 명명된 문자열 타입(`type Mode string`) + 상수 4개, (b) 정수 enum(iota).
- 트레이드오프: (b)는 비교가 약간 빠르나 스트림 모드는 직렬화·로깅·외부 식별에서 문자열 값(`values` 등)으로
  드러나는 편이 자연스럽고, README §13이 값을 문자열 리터럴로 기술한다. (a)는 alias 노출(`streaming.Mode =
  core.Mode`)과 문자열 값 비교에 모두 잘 맞는다.
- 채택: (a) 명명된 문자열 타입 + `values`/`messages`/`updates`/`debug` 상수.
- 근거: spec.md §3·§5.4, README §13. `core`가 모드 소유자이고 상위가 alias로 참조한다는 경계를 유지한다.

### D3. `StateSnapshot.Config`를 `config.RunConfig`로 두어 `core → config` 의존을 인정

- 고려한 옵션: (a) `Config` 필드를 `config.RunConfig`로 직접 둔다(`core → config` 단방향), (b) `core`에 별도
  설정 타입을 두거나 `any`로 느슨하게 두어 `core`를 완전 무의존으로 만든다.
- 트레이드오프: (b)는 `core`를 무의존 leaf로 만들지만, 스냅샷을 소비하는 모든 상위 패키지가 설정 타입을
  다시 변환·단언해야 하고 단일 소유자 원칙이 깨진다. (a)는 `config`가 무의존 최하위 leaf라 `core → config`가
  단방향으로 닫혀 순환이 없고, `RunConfig`라는 단일 설정 타입을 그대로 실어 보낼 수 있다.
- 채택: (a). `core`는 `config`에만 의존하는 leaf로 두고 `StateSnapshot.Config`는 `config.RunConfig`로 한다.
- 근거: spec.md §3·§5.4·§5.5, README §28-1 규칙1. 이 단방향 의존이 leaf 경계의 핵심이며, 상위 Phase의
  순환을 끊는 토대다(§1 참조).

### D4. `config`의 `.env` 로딩 구현 — 표준 라이브러리

- 고려한 옵션: (a) 표준 라이브러리만으로 최소 `.env` 파서를 직접 작성(`os`로 환경변수 읽기 + 파일에서
  `KEY=VALUE` 줄 파싱), (b) 외부 의존성으로 `.env` 파서 도입(예: `github.com/joho/godotenv`).
- 트레이드오프: (b)는 따옴표·주석·export 구문 등 `.env` 변형 처리를 검증된 라이브러리에 맡길 수 있으나,
  spec.md §3·README가 강조하는 외부 의존성 최소화 제약과 충돌하고, 무의존 leaf가 되려는 `config`에 첫 외부
  의존을 들인다. (a)는 외부 의존 없이 `KEY=VALUE` 기본 포맷만 다루면 되어 Phase 0이 읽어야 하는 변수
  집합(OPENAI/TAVILY/SUPABASE)에 충분하고, 무의존 제약을 지킨다. 비용은 고급 `.env` 문법(따옴표/멀티라인
  등)을 직접 처리해야 한다는 점인데, 이는 Phase 0 완료 조건이 요구하지 않는다.
- 채택: (a) 표준 라이브러리 기반 최소 `.env` 파서. 파일에서 `KEY=VALUE` 줄을 읽어 환경에 반영한 뒤
  `os.Getenv`로 회수하는 경로로 `LoadEnv`를 구성한다. 대상 파일 부재 시 error를 반환한다(SPEC §5.3).
- 근거: spec.md §3("표준 라이브러리 또는 최소 .env 파서, 구체 선택은 analysis 단계", 외부 의존성 최소화),
  README §24(읽는 환경변수 목록). 향후 고급 문법이 필요해지면 그때 §5 재검토로 (b)를 도입할 수 있으나
  Phase 0 범위에서는 (a)로 충분하다. 따옴표/주석 처리 같은 세부는 결과에 영향이 작은 구현 디테일이라
  implementer 재량에 둔다.

### D5. `RunConfig`에서 `thread_id`/`user_id`를 읽는 위치 — `Configurable` 맵

- 고려한 옵션: (a) `GetThreadID`/`GetUserID`가 `RunConfig.Configurable["thread_id"]`/`["user_id"]`를 읽는다,
  (b) `RunConfig`에 `ThreadID`/`UserID` 전용 필드를 둔다.
- 트레이드오프: (b)는 접근이 직접적이나 LangGraph의 `configurable` 컨벤션과 어긋나고, 임의 식별자를 담는
  `GetConfigurable(cfg, key)`와 일관되지 않는다. (a)는 `thread_id`/`user_id`도 임의 configurable 키의 특수
  케이스로 통일해, `GetConfigurable`와 `GetThreadID`/`GetUserID`가 같은 맵을 공유한다.
- 채택: (a). `RunConfig.Configurable map[string]any`에서 키로 읽는다. 키 부재 시 `GetThreadID`/`GetUserID`는
  빈 문자열을, `GetConfigurable`은 `(nil, false)`를 반환한다.
- 근거: spec.md §5.2, README §24(`RunConfig (Configurable map[string]any)`). 값이 문자열이 아닐 때의 처리는
  접근자가 빈 문자열로 떨어뜨리는 보수적 동작을 기본값으로 두되, 이는 결과 영향이 작은 구현 디테일이라
  implementer 재량에 둔다.

### D6. 모듈 디렉토리 레이아웃 — 루트 하위 평면 배치

- 고려한 옵션: (a) 모듈 루트 하위에 `config/`, `core/`를 평면 배치, (b) `internal/` 등 하위에 감춤.
- 트레이드오프: (b)는 외부 노출을 막지만, README §1이 "내부 전용으로 감추는 패키지는 두지 않는다"고 명시하고
  두 패키지는 다운스트림이 `github.com/zipkero/langgraph-go/config` 등으로 import하는 공개 패키지다.
- 채택: (a) 루트 하위 평면 배치. 이후 모든 Phase 패키지도 같은 레벨에 추가된다.
- 근거: spec.md §1, README §1. 판단 거리가 크지 않으나 모듈 경로 형태(SPEC §5.1)와 직결돼 명시한다.
