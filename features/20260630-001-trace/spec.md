# trace — 실행 추적·디버그 출력

Phase 7을 패키지별 feature로 분할한 두 번째 산출물이다(README §26 Phase 7, §25). 이 feature는 `trace` 패키지
하나를 다룬다. 같은 Phase의 나머지 패키지(config 어셈블리, database, search, storage)는 별도 feature로 진행한다.

## 1. 범위

신규 `trace` 패키지를 구현한다. README §25의 API 표면을 제공하는 선택 모듈로, 그래프·에이전트 실행을
시간순으로 기록하고 JSON·사람이 읽는 텍스트·mermaid 다이어그램으로 내보낸다.

- 타입(§25) — `Trace`, `Event`, 그리고 이벤트 종류별 `NodeTrace`/`ToolTrace`/`LLMTrace`/`ErrorTrace`.
  `trace`는 자체 `Event`/`Trace` 타입을 정의하며 `tool.Event`와 별개다.
- 기록 메소드(§25) — `StartRun(runID)`/`EndRun(runID)`, `RecordNodeStart(node, st)`/`RecordNodeEnd(node, update)`,
  `RecordToolCall(call)`/`RecordToolResult(res)`, `RecordLLMRequest(req)`/`RecordLLMResponse(resp)`, `RecordError(err)`.
- 조회·내보내기(§25) — `Events() []Event`, `ExportJSON() ([]byte, error)`.
- 출력(§43 모듈 책임) — 기록된 trace를 사람이 읽는 텍스트로 렌더링하는 pretty-print 출력과, 노드 실행 흐름을
  mermaid 다이어그램 텍스트로 렌더링하는 mermaid 출력(사용자 결정: 포함).
- tool.Event Emit 싱크(§28-1 규칙2) — `tool.Event`를 받아 내부 기록으로 매핑하는 싱크(`func(tool.Event)` 형태)를
  제공해 `tool.Runtime`의 이벤트 방출에 연결할 수 있다(사용자 결정: 포함).

## 2. 목표

다운스트림이 그래프·에이전트 실행을 추적해 디버깅·검사할 수 있게 한다. 노드 진입/종료, 도구 호출/결과,
LLM 요청/응답, 에러를 한 run 단위로 시간순 기록하고, JSON(기계 소비)·텍스트(사람 열람)·mermaid(흐름 시각화)로
내보내 실행 흐름을 들여다볼 수 있게 한다. tool.Event 싱크를 `tool.Runtime` 방출에 연결하면 도구 실행이 별도
수작업 호출 없이 자동 기록된다.

## 3. 제약

- `trace`는 선택 모듈이다(§25, §28-1). 모듈 내부에서는 상위→하위 단방향으로 `graph`·`message`·`llm`·`tool`·
  `core`·`config`와 그 전이 의존, 그리고 표준 라이브러리만 import한다. 하위 패키지(특히 `graph`·`tool`·`message`·
  `llm`·`core`·`config`)는 `trace`를 역참조하지 않는다.
- tool.Event 싱크는 `tool` 패키지가 소유한 `tool.Event` 타입을 그대로 받는다(§28-1 규칙2). `trace`는 상위
  타입으로서 `tool`을 import하지만, `tool`은 `trace`를 import하지 않는다.
- pretty-print·mermaid 출력은 외부 렌더링 라이브러리·네트워크 없이 문자열로만 생성한다. LangSmith 등 외부
  추적 백엔드에 의존하지 않는다.
- Phase 0~6 패키지의 기존 타입·동작(`graph.State`/`StateUpdate`, `message.ToolCall`, `tool.Result`/`tool.Event`,
  `llm.ChatRequest`/`ChatResponse` 등)은 변경하지 않는다. `trace`에 필요한 새 타입은 `trace` 패키지 안에 둔다.
- 외부 네트워크·실행 중인 백엔드에 의존하지 않고 결정적으로 검증할 수 있어야 한다(인메모리 기록).
- import 경계 검증은 런타임 하위 프로세스(`go list` 등) 없이 정적으로 수행한다(기존 패키지 경계 테스트와 동일
  방식, Windows 안티바이러스·빌드 캐시 잠금 회피).

## 4. 제외 범위

- LangSmith·OpenTelemetry 등 외부 추적 백엔드·표준 트레이싱 프로토콜 연동은 제외한다(§25 선택 모듈, §26 범위 외).
- 실시간 스트리밍 추적 UI·웹 대시보드·HTTP 노출은 제외한다. 출력은 텍스트·JSON·mermaid 문자열 산출까지다.
- Phase 7의 다른 패키지(`config` 어셈블리 함수, `database`, `search`, `storage`, `vectorstore.SupabaseVectorStore`)는
  이 feature 범위 밖이며 각각 별도 feature로 다룬다.
- 기존 `graph`·`agent` 실행 경로에 trace 호출을 자동 삽입하는 계측(instrumentation) 변경은 제외한다. `trace`는
  Record* 메소드와 tool.Event 싱크라는 수신 표면만 제공하고, 연결은 다운스트림 책임이다.

## 5. 완료 조건

1. 모듈 루트에서 `go build ./...`와 `go vet ./...`가 오류 없이 끝난다(`trace` 신규 패키지가 추가되고 Phase 0~6
   패키지의 기존 동작은 변경되지 않은 상태).
2. 기록과 조회가 동작한다: `Trace`를 만들어 `StartRun`/`EndRun`, `RecordNodeStart`/`RecordNodeEnd`,
   `RecordToolCall`/`RecordToolResult`, `RecordLLMRequest`/`RecordLLMResponse`, `RecordError`를 호출하면 각 호출이
   대응하는 이벤트(`NodeTrace`/`ToolTrace`/`LLMTrace`/`ErrorTrace`)로 기록되고, `Events()`가 호출 순서대로 돌려준다.
3. JSON 내보내기가 동작한다: `ExportJSON()`이 기록된 이벤트를 JSON 바이트로 직렬화해 반환하며, 그 바이트를
   다시 역직렬화하면 기록한 이벤트의 종류·필드 값이 보존된다.
4. pretty-print 출력이 동작한다: 기록된 trace를 사람이 읽는 텍스트로 렌더링하면 run 구간과 각 이벤트(노드·도구·
   LLM·에러)가 식별 가능한 형태로 나타난다.
5. mermaid 출력이 동작한다: 기록된 노드 실행 흐름을 mermaid 다이어그램 텍스트로 렌더링하면 노드 진입/종료
   순서가 mermaid 문법의 노드·엣지로 표현된다.
6. tool.Event 싱크가 동작한다: `trace`가 제공하는 `func(tool.Event)` 싱크를 `tool.Runtime`의 이벤트 방출에
   연결한 뒤 도구가 이벤트를 방출하면, 그 도구 호출/결과가 `trace`에 `ToolTrace`로 기록되어 `Events()`에 나타난다.
7. import 그래프 검사로 `trace`가 `graph`·`message`·`llm`·`tool`·`core`·`config` 등 허용 패키지만 import하고,
   하위 패키지(특히 `graph`·`tool`·`message`·`llm`·`core`·`config`)가 `trace`를 역참조하지 않음을 확인할 수 있다.
   Phase 0~6 패키지는 기존 동작이 수정되지 않는다.
