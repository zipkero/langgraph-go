# a2a — 분석·설계 (analysis.md)

## 근거

직접 읽은 파일·자료:

- `features/20260629-005-a2a/spec.md` — 범위·제약·완료 조건(§1~§5). 본 분석은 spec §1 범위로만 한정한다.
- `README.md` §22(와이어 포맷·API 표면), §22-4(어댑터 상태 매핑), §28/§28-1(직접 구현·import 경계 규칙).
- `agent/agent.go` — `agent.Create`/`Invoke`/`Stream` 시그니처, `AgentEvent` 필드, `Input`/`Result` 구조.
- `structured/schemas.go` — `AgentStatus{Status, Message}`(허용값 input_required/completed/error)와 스키마 생성자.
- `config/config.go` — `AgentConfig{Name/URL/Port/Description}`(leaf, 어셈블리 함수 부재 확인).
- `message/message.go` — `message.Message`/`NewUserMessage` 등(a2a 자체 타입과 별개임 확인).
- `vectorstore/import_boundary_test.go` — `go list -deps` 기반 경계 테스트 선례.
- A2A 스펙(a2a-protocol.org) — well-known 경로 `/.well-known/agent-card.json`, SSE content-type
  `text/event-stream` 직접 확인. JSON-RPC 메서드 문자열은 fetch 본문 절단으로 직접 인용 확보 실패(§5.2 참조).

추정과 확인의 분리: 와이어 필드명·kind 값·상태 문자열은 README §22로 확인된 사실이다. JSON-RPC 메서드
문자열(`message/send` 등)은 README가 심볼명만(`SendMessage`/`SendMessageStreaming`) 명시하고 와이어
문자열을 고정하지 않아 설계 결정으로 commit한다(§5.2).

## 1. 구조

신규 `a2a` 패키지 하나를 추가한다. 패키지 내부는 책임별 파일군으로 나뉘되, 전부 같은 `package a2a`로
컴파일되어 인패키지 루프백 e2e(서버·클라이언트가 한 패키지)를 가능케 한다(spec §5.7).

논리적 구성 단위:

- 프로토콜 타입군 — `AgentCard`/`AgentSkill`/`AgentCapabilities`, `Task`/`TaskStatus`/`TaskState`,
  `Message`/`Part`(TextPart/DataPart/FilePart)/`Artifact`, `MessageSendParams`/`SendMessageRequest`,
  스트리밍 이벤트(`TaskStatusUpdateEvent`/`TaskArtifactUpdateEvent`/`Event`). a2a 고유 타입이며
  `message.Message`와 별개다(spec §3, README §22-1).
- JSON 직렬화 계층 — `Part`·`FilePart` union의 `kind`/file-variant 판별 직렬화. 와이어 표면을 한곳에
  모아 round-trip 보존을 보장한다(SPEC §5.2).
- 서버 계층 — `AgentExecutor`(인터페이스)·`RequestContext`·`EventQueue`·`TaskUpdater`·`TaskStore`/
  `InMemoryTaskStore`·`RequestHandler`/`DefaultRequestHandler`·`Server`(`NewServer`/`Run`)와 헬퍼
  (`NewTask`/`NewAgentTextMessage`). JSON-RPC 디스패치와 SSE 스트리밍을 담당한다.
- 클라이언트 계층 — `CardResolver`·`Client`(`SendMessage`/`SendMessageStreaming`)와 아티팩트 추출 헬퍼.
- 어댑터 계층 — `StreamToTaskUpdates`. `agent.Agent` 실행을 태스크 수명주기로 매핑한다(README §22-4).

레이어 간 의존은 단방향이다: 어댑터→(agent + 서버 타입), 서버·클라이언트→(프로토콜 타입 + 직렬화),
직렬화→프로토콜 타입. 외부 의존은 표준 라이브러리(`net/http`·`encoding/json`·`encoding/base64`·`bufio`/
`io` 등)뿐이다(spec §3, §5.1).

## 2. 데이터 흐름

### 2.1 서버 수신 경로 (SendMessage, 비스트리밍)

1. HTTP POST → `DefaultRequestHandler`가 JSON-RPC envelope를 파싱하고 `method`로 디스패치한다.
2. `MessageSendParams`를 역직렬화해 `RequestContext`(사용자 입력·기존 태스크·메시지)를 구성한다. 기존
   태스크가 없으면 어댑터/실행기 진입 시 `NewTask`로 생성한다(README §22-4).
3. `AgentExecutor.Execute(ctx, rc, queue)`를 호출한다. 실행기는 `EventQueue`로 이벤트를 push하고,
   `TaskUpdater`가 큐+`TaskStore`를 감싸 상태 전이·아티팩트 적재를 수행한다(SPEC §5.3, §5.4).
4. 비스트리밍에서는 큐로 흘러온 이벤트를 최종 `Task`로 수합해 JSON-RPC result로 응답한다. 태스크는
   `TaskStore`에 저장돼 이후 조회 가능하다(SPEC §5.3, §5.4).

### 2.2 서버 스트리밍 경로 (SendMessageStreaming)

`text/event-stream` 응답을 열고 `http.Flusher`로 즉시 flush한다. 실행기가 push한 이벤트
(`TaskStatusUpdateEvent`/`TaskArtifactUpdateEvent`, 그리고 초기/최종 `Task`)를 SSE data 프레임으로
직렬화해 순차 전송한다. final 상태(`completed`/`failed`/`canceled`/`input_required(final)`) 이벤트
이후 스트림을 종료한다(SPEC §5.4, §5.5).

### 2.3 클라이언트 경로

- `CardResolver.GetAgentCard`: `baseURL + /.well-known/agent-card.json` GET → `AgentCard` 역직렬화
  (SPEC §5.5, well-known 경로는 스펙 확인).
- `Client.SendMessage`: `SendMessageRequest`를 JSON-RPC envelope로 POST → result를 `Task`로
  역직렬화(SPEC §5.5).
- `Client.SendMessageStreaming`: SSE 응답을 라인 단위로 소비해 각 `data:` 프레임을 `Event`
  (Task/StatusUpdate/ArtifactUpdate union)로 디코드, `<-chan Event`로 방출(SPEC §5.5).
- 아티팩트 헬퍼: `Artifact.Parts`를 순회해 텍스트/데이터/파일(URI·bytes)을 꺼낸다(SPEC §5.5).

### 2.4 어댑터 흐름 (StreamToTaskUpdates)

`query`를 `message.NewUserMessage`로 감싼 `agent.Input{Messages}`를 만들고 `sessionID`를
`config.RunConfig`(thread_id)로 전달해 `agent.Stream`을 호출한다. 방출되는 `AgentEvent`를 루프 소비하며
`TaskUpdater`로 매핑한다:

- 진행 중(`Content`/중간 이벤트) → `UpdateStatus(working, ...)`.
- `RequireUserInput` → `UpdateStatus(input_required, Final(true))`.
- `IsTaskComplete` → `AddArtifact(parts, name)` 후 `Complete()`.
- 실행기 레벨 예외(`AgentEvent.Error` 또는 Stream 시작 실패) → `failed`.
- 취소(ctx 취소) → `Cancel(msg)`로 `canceled`.

대체 경로로 `agent.Result/State`의 `structured_response`를 `structured.AgentStatus`로 타입 단언해
`status`(input_required/completed/error)를 매핑하되, `error`는 `failed`가 아니라 `input_required`로
흐른다(README §22-4, SPEC §5.6). 기본 노출 경로는 (1) 플래그 매핑이고 (2)는 에이전트별 택일 대체다.
이 기본/대체 구분은 spec §3·README §22-4가 이미 확정한 사항이다.

## 3. 인터페이스

심볼 시그니처는 README §22를 그대로 따른다. 핵심 경계만 정리한다(전 목록은 spec §1·README §22-1~4).

### 3.1 와이어 타입

- `Part`는 `TextPart`/`DataPart`/`FilePart` 래퍼로, JSON `kind` 필드(`text`/`data`/`file`)로 union을
  판별한다. `Message`는 `kind:"message"`를 부착한다. `FilePart.File`은 `FileWithUri`/`FileWithBytes`
  union이다(README §22-1). JSON 필드명은 camelCase(`messageId`/`artifactId`/`contextId`/`mimeType`
  등), `TaskState`는 문자열 상수(`working`/`input_required`/`completed`/`failed`/`canceled`)다.
- round-trip 보존(marshal→unmarshal에서 값 동일)이 직렬화 계층의 계약이다(SPEC §5.2).

### 3.2 서버 인터페이스

- `AgentExecutor interface { Execute(ctx, RequestContext, EventQueue) error; Cancel(...) error }` — 응용이
  구현한다.
- `TaskStore interface` — `InMemoryTaskStore`가 mutex+map으로 구현한다. 저장/조회 동시성을 보장한다
  (SPEC §5.4).
- `RequestHandler interface` — `DefaultRequestHandler(executor, store)`가 JSON-RPC 디스패치를 구현하고
  `NewServer(card, handler)`가 수신한다(README §22-2).
- `TaskUpdater`: `UpdateStatus(state, msg, opts...)`(`Final(true)` 옵션), `AddArtifact(parts, name)`,
  `Complete()`, `Cancel(msg)`. `EventQueue`+`TaskStore`를 감싸 전이 시 양쪽을 동시 갱신한다(SPEC §5.4).

### 3.3 클라이언트·어댑터

- `CardResolver`/`Client`/아티팩트 헬퍼는 README §22-3 시그니처 그대로다.
- `StreamToTaskUpdates(ctx, a *agent.Agent, query, sessionID string, u *TaskUpdater) error` — a2a가
  `agent`를 단방향 import하는 유일한 결합점이다(SPEC §5.6, §5.8).

### 3.4 import 경계

`a2a`는 `agent`·`config`·`structured`·`message`·`tool`·`core` + 표준 라이브러리만 import한다.
`config.AgentConfig`는 재사용하되 어셈블리 함수는 추가하지 않는다. 하위 패키지(특히 `agent`)는 `a2a`를
역참조하지 않는다(spec §3, README §28-1 규칙4, SPEC §5.8).

## 4. 영향 범위

- `a2a`는 신규 패키지라 **기존 파일 수정이 없다**. 디렉토리·소스 추가만 발생한다(spec §3·§5.1·§5.8).
- `agent`(`agent.go`)·`config`(`config.go`)·`structured`(`schemas.go`)·`message`(`message.go`)는
  **재사용만** 하며 수정하지 않는다 — 위 4개 파일을 직접 읽어 a2a가 호출하는 심볼(`agent.Create`/
  `Stream`/`AgentEvent`, `config.AgentConfig`/`RunConfig`, `structured.AgentStatus`,
  `message.NewUserMessage`)이 이미 존재하고 시그니처 변경이 불필요함을 확인했다(spec §3).
- `config.AgentConfig`는 leaf라 a2a가 import해도 단방향 트리를 위반하지 않는다(README §28-1 규칙4).
- `go.mod`에 **신규 직접 의존을 추가하지 않는다**. gRPC/protobuf·외부 a2a SDK 없이 표준 라이브러리만
  쓴다(spec §3·§4·§5.1). a2a 의존 그래프에 금지 경로가 없음을 `go list -deps`로 검증한다.
- Phase 0~6 패키지의 기존 동작은 불변이다(SPEC §5.1, §5.8).

검증 수단(완료 조건 충족 근거이며 별도 검증 섹션이 아님): 타입 직렬화 단위 테스트(round-trip),
인패키지 루프백 e2e(`httptest.Server`로 서버 기동→클라이언트가 카드 조회→SendMessage/스트리밍 왕복, skip
가드 없음 — 외부 의존 없음), `go list -deps` import 경계 테스트(`vectorstore/import_boundary_test.go`
선례 방식). `mcp/e2e_test.go`의 인패키지 루프백 선례를 따른다(spec §5.2·§5.7·§5.8).

## 5. Decision Points

### 5.1 Part/FilePart union의 kind 판별 직렬화 방식

- 채택: 각 union 래퍼(`Part`, `FilePart`)에 커스텀 `MarshalJSON`/`UnmarshalJSON`을 구현한다. 직렬화 시
  `kind`(또는 file variant 판별 필드)를 부착하고, 역직렬화 시 먼저 판별 필드를 읽어 구체 타입으로 분기한다.
- 근거: Go에는 union이 없고 README §22-1은 와이어에 `kind`를 부착하라고 명시한다. 단일 태그 구조체(모든
  필드를 한 struct에 평탄화)는 `kind`별 필드 충돌·omitempty 모호성으로 round-trip 보존(SPEC §5.2)을
  깨기 쉽다. 커스텀 마샬러가 와이어 계약을 한곳에 가둬 보존을 보장한다.
- 트레이드오프: 커스텀 마샬러는 코드량이 늘고 unmarshal 시 2-pass(판별→본문)가 필요하다. 단 round-trip
  정확성이 SPEC §5.2의 합격선이라 이 비용을 수용한다.

### 5.2 JSON-RPC 메서드 문자열 규약

- 채택: 와이어 메서드 문자열을 A2A 스펙 표준인 `message/send`(비스트리밍)·`message/stream`(스트리밍)으로
  고정하고, 태스크 조회·취소가 필요하면 `tasks/get`·`tasks/cancel`을 같은 규약으로 둔다. Go 심볼은 README
  §22의 `SendMessage`/`SendMessageStreaming`을 그대로 쓴다(와이어 문자열과 별개).
- 근거: README §22는 Go API 심볼명만 명시하고 JSON-RPC `method` 문자열을 고정하지 않는다. A2A 스펙의
  JSON-RPC 바인딩 표준 문자열이 `message/send`/`message/stream` 계열이며, 인패키지 루프백(서버·클라이언트
  동일 패키지)에서는 서버 디스패치 키와 클라이언트 송신 키가 같은 상수만 공유하면 결정적으로 닫힌다.
- 트레이드오프: 외부 A2A 에이전트와의 상호운용을 노린다면 스펙 표준 문자열 일치가 중요하다. 이 feature는
  루프백 검증만 요구(spec §5.7)하므로 내부 일치만으로 충분하나, 표준 문자열을 택해 향후 상호운용 비용을
  미리 제거한다. 직접 인용 확보는 fetch 절단으로 실패했고 well-known 경로·SSE content-type만 스펙에서
  직접 확인됐다 — 외부 상호운용까지 보장하려면 구현 전 스펙의 JSON-RPC 바인딩 method 문자열을 1회
  재확인할 것을 권장한다.

### 5.3 EventQueue/TaskUpdater/TaskStore의 동시성과 결합

- 채택: `EventQueue`를 버퍼드 채널 기반으로 두고, 스트리밍에서는 큐 이벤트를 SSE로 흘리며 비스트리밍에서는
  최종 `Task`로 수합한다. `TaskUpdater`는 큐 push와 `TaskStore` 갱신을 한 호출에서 함께 수행한다.
  `InMemoryTaskStore`는 mutex+map으로 동시 접근을 보호한다.
- 근거: 같은 이벤트 스트림을 스트리밍/비스트리밍 두 응답 형태로 재사용하려면 큐가 단일 진실원이어야 한다.
  채널은 실행기 goroutine과 HTTP 핸들러 goroutine 간 자연스러운 전달 매개다. README §22-2가 `TaskUpdater`를
  상태 전이의 단일 진입점으로 규정하므로, 큐+스토어 동시 갱신을 여기에 집중한다(SPEC §5.4).
- 트레이드오프: 채널 버퍼 크기·종료 신호(final 이벤트 후 close) 처리가 누락되면 누수·블로킹 위험이 있다.
  final 상태 이벤트를 종료 신호로 삼아 양쪽 경로에서 일관되게 닫는다.

### 5.4 어댑터 상태 매핑의 기본 경로

- 채택: 기본 노출 경로는 `AgentEvent`의 `IsTaskComplete`/`RequireUserInput`/`Content` 플래그 매핑으로
  두고, `structured.AgentStatus`(`status`) 매핑을 대체 경로로 지원한다. `status="error"`는 `failed`가
  아니라 `input_required`로 흐른다.
- 근거: spec §3과 README §22-4가 기본=(1) 플래그, 대체=(2) AgentStatus, 그리고 error→input_required를
  이미 확정했다. 이 결정은 본 feature에서 재논의 대상이 아니라 그 확정을 구현 계약으로 명시할 뿐이다.
- 트레이드오프: 두 경로를 모두 두므로 어댑터가 어느 경로로 들어왔는지 판정 로직이 필요하다. 기본 경로를
  우선 적용하고, `structured_response`가 `AgentStatus`로 단언될 때만 대체 경로를 타도록 분기한다.
