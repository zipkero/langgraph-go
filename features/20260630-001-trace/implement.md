# trace — 구현 체크리스트

`features/20260630-001-trace/spec.md` §5와 `analysis.md`로부터 도출한 순수 실행 체크리스트다. Task는 의존 순서대로 나열했다(위가 먼저).

- [ ] task-001: 코어 타입과 기록·조회 표면 구현
  - 목적: 한 run 단위로 노드 진입/종료·도구 호출/결과·LLM 요청/응답·에러를 시간순으로 기록하고, run 시작/종료 경계를
    표시하며, 기록한 이벤트를 호출 순서대로 돌려주는 인메모리 추적기를 만든다.
  - 접근: 신규 `trace/` 디렉토리에 자체 `Event`/`Trace` 타입을 정의한다. `Event`는 공통 메타(종류 판별 + 순서)를 담고
    종류별 페이로드 타입(`NodeTrace`/`ToolTrace`/`LLMTrace`/`ErrorTrace`)을 감싸는 래퍼로 둔다(ANALYSIS Decision (a)).
    `NodeTrace`는 노드 이름과 상태(`graph.State`=`map[string]any`)/업데이트(`graph.StateUpdate`)를, `ToolTrace`는 도구
    호출(`message.ToolCall`)·결과(`tool.Result`)에서 추출한 값을, `LLMTrace`는 요청(`llm.ChatRequest`)·응답
    (`llm.ChatResponse`)에서 추출한 값을, `ErrorTrace`는 에러 메시지를 문자열 필드로 담는다(error는 문자열 보관,
    ANALYSIS Decision (b)). `func New() *Trace` 생성자, `StartRun(runID)`/`EndRun(runID)`, `RecordNodeStart`/`RecordNodeEnd`,
    `RecordToolCall`/`RecordToolResult`, `RecordLLMRequest`/`RecordLLMResponse`, `RecordError`, `Events() []Event`를 §25
    시그니처대로 구현한다. 누적 슬라이스와 run 상태는 `Trace`의 mutex로 보호해 기록·조회를 직렬화한다(ANALYSIS
    Decision (d)). 자동 계측은 하지 않는다 — 수신 표면만 제공한다.
  - 검증 조건:
    - 결과: `New()`로 만든 `Trace`에 `StartRun`/`EndRun`과 각 `Record*`를 호출하면 대응하는 종류의 이벤트가 누적되고,
      `Events()`가 호출한 순서 그대로 반환한다.
    - 확인: 각 `Record*` 호출이 대응 이벤트 종류(`NodeTrace`/`ToolTrace`/`LLMTrace`/`ErrorTrace`)로 기록되는지, `Events()`가
      호출 순서를 보존하는지 검증하는 단위 테스트를 추가한다. `go test ./trace/`, 모듈 루트 `go build ./...`·`go vet ./...`가
      오류 없이 끝나고 Phase 0~6 패키지 동작이 변하지 않음을 확인한다.
  - 참조: SPEC §5.2, §5.1; ANALYSIS §1, §3, Decision (a),(d).

- [ ] task-002: JSON 내보내기와 round-trip 보존 구현
  - 목적: 기록된 이벤트를 JSON 바이트로 내보내고, 그 바이트를 다시 읽으면 각 이벤트의 종류와 필드 값이 보존되게 한다.
  - 접근: `(*Trace) ExportJSON() ([]byte, error)`를 §25 시그니처대로 구현한다. `Event`에 종류 판별 필드(`Kind`)를 두고
    비어 있는 종류 페이로드는 `omitempty`로 생략해, 표준 `encoding/json`만으로 `Kind`와 채워진 페이로드 값이 보존되도록
    한다(ANALYSIS Decision (b)). 커스텀 마샬러는 두지 않는다. 직렬화 실패 시 에러를 전파한다. `ErrorTrace`는 에러
    메시지 문자열을, 노드 상태의 임의 값은 표준 JSON 규칙(숫자 float64 복원 등)을 따르는 값으로 round-trip이 성립한다.
  - 검증 조건:
    - 결과: 여러 종류의 이벤트를 기록한 뒤 `ExportJSON()`의 바이트를 역직렬화하면 각 이벤트의 종류와 필드 값이
      원본과 일치한다.
    - 확인: 네 종류 이벤트를 기록 → `ExportJSON` → 역직렬화 → 종류·필드 값 동등 비교(표준 JSON 규칙 안의 값으로
      구성)하는 round-trip 단위 테스트를 추가한다. `go test ./trace/`가 통과한다.
  - 참조: SPEC §5.3; ANALYSIS §2, Decision (a),(b).

- [ ] task-003: pretty-print 텍스트 출력 구현
  - 목적: 기록된 trace를 사람이 읽는 텍스트로 렌더링해 run 구간과 모든 이벤트 종류(노드·도구·LLM·에러)가 식별
    가능하게 한다.
  - 접근: `(*Trace) Pretty() string`을 구현한다(error 미반환, 순수 문자열 생성, ANALYSIS Decision (c)). run 시작/종료
    경계로 run 구간을 표시하고, 누적 이벤트를 순서대로 순회하며 각 종류를 구분 가능한 형태의 텍스트 라인으로
    렌더링한다. 외부 렌더링 라이브러리·네트워크를 쓰지 않는다.
  - 검증 조건:
    - 결과: run을 시작·종료하고 네 종류 이벤트를 기록한 뒤 `Pretty()`를 호출하면 run 구간과 각 이벤트 종류가 텍스트에
      식별 가능한 형태로 나타난다.
    - 확인: run 경계와 노드·도구·LLM·에러 이벤트가 모두 출력에 식별 가능하게 포함되는지 검증하는 단위 테스트를
      추가한다. `go test ./trace/`가 통과한다.
  - 참조: SPEC §5.4; ANALYSIS §1, §2, Decision (c).

- [ ] task-004: mermaid 노드 흐름 출력 구현
  - 목적: 기록된 노드 실행 흐름을 mermaid 다이어그램 텍스트로 렌더링해 노드 진입/종료 순서가 mermaid 문법의 노드·
    엣지로 표현되게 한다.
  - 접근: `(*Trace) Mermaid() string`을 구현한다(error 미반환, 순수 문자열 생성, ANALYSIS Decision (c)). `NodeTrace`
    이벤트만 진입/종료 순서대로 mermaid 노드·엣지로 변환한다 — 도구·LLM·에러 이벤트는 흐름에 포함하지 않는다
    (ANALYSIS Decision (c)). 외부 렌더링 라이브러리·네트워크를 쓰지 않는다.
  - 검증 조건:
    - 결과: 여러 노드의 진입/종료를 기록한 뒤 `Mermaid()`를 호출하면 노드 진입/종료 순서가 mermaid 문법의 노드·
      엣지로 나타나고, 도구·LLM·에러 이벤트는 흐름에 들어가지 않는다.
    - 확인: 노드 흐름이 mermaid 노드·엣지로 표현되는지, 비노드 이벤트가 흐름에서 제외되는지 검증하는 단위 테스트를
      추가한다. `go test ./trace/`가 통과한다.
  - 참조: SPEC §5.5; ANALYSIS §1, §2, Decision (c).

- [ ] task-005: tool.Event 싱크 구현과 자동 ToolTrace 기록
  - 목적: `tool.Runtime`의 이벤트 방출에 꽂을 수 있는 `func(tool.Event)` 싱크를 제공해, 도구가 이벤트를 방출하면 그
    호출/결과가 별도 수작업 호출 없이 `ToolTrace`로 자동 기록되게 한다.
  - 접근: `(*Trace) ToolEventSink() func(tool.Event)`를 구현해, `tool.Event`(ToolName/ToolCallID/Input/Result/Err)를
    `ToolTrace`로 매핑·누적하는 클로저를 반환한다(ANALYSIS Decision (e)). 반환 함수는 `tool.NewRuntime(..., emit
    func(Event))`의 `emit` 인자에 그대로 대입 가능한 형태여야 한다. 매핑·누적은 task-001의 mutex 보호 경로를
    공유한다. `tool` 패키지는 수정하지 않는다(이미 `func(Event)` emit 주입 표면 보유).
  - 검증 조건:
    - 결과: `ToolEventSink()`가 반환한 함수를 `tool.NewRuntime`의 emit으로 연결하고 도구가 이벤트를 방출하면, 그
      도구 호출/결과가 `ToolTrace`로 기록되어 `Events()`에 나타난다.
    - 확인: 싱크를 `tool.NewRuntime`(또는 동치 emit 호출)에 연결해 `tool.Event`를 방출 → `Events()`에 대응 `ToolTrace`가
      나타나는지 검증하는 단위 테스트를 추가한다. `tool` 패키지가 수정되지 않았고 `go build ./...`·`go vet ./...`가
      오류 없이 끝남을 확인한다. `go test ./trace/`가 통과한다.
  - 참조: SPEC §5.6; ANALYSIS §1, §2, Decision (e).

- [ ] task-006: import 경계 정적 검증 테스트 추가
  - 목적: `trace`가 허용 패키지만 import하고 하위 패키지가 `trace`를 역참조하지 않음을 정적으로 확인하며, 이 경계를
    회귀로 보호한다.
  - 접근: `trace/import_boundary_test.go`를 기존 `a2a`/`mcp`/`store`의 `import_boundary_test.go` 방식대로 작성한다
    (ANALYSIS Decision (f)). `go/build`의 `build.ImportDir`로 모듈 내부 소스를 정적 파싱하고 전이 의존을 재귀
    수집한다 — `go list` 등 하위 프로세스를 띄우지 않는다(Windows 안티바이러스·빌드 캐시 잠금 회피). 검사: (1)
    `trace`의 모듈 내부 의존이 허용 집합(`graph`·`message`·`llm`·`tool`·`core`·`config` + 전이 의존) 이내, (2) 하위
    패키지(`graph`·`tool`·`message`·`llm`·`core`·`config`)의 전이 의존에 `trace` 부재. 외부 백엔드 미사용 보호가
    필요하면 a2a식 forbidden-prefix·go.mod 매니페스트 검사를 필요한 범위로 차용한다.
  - 검증 조건:
    - 결과: 경계 테스트가 `trace`의 허용 외 import과 하위 패키지의 `trace` 역참조를 둘 다 검출하며, 현재 코드에서는
      통과한다.
    - 확인: 위 두 검사를 담은 `import_boundary_test.go`를 추가하고 `go test ./trace/`가 통과한다. 모듈 루트
      `go build ./...`·`go vet ./...`가 오류 없이 끝나고 Phase 0~6 패키지 동작이 변하지 않음을 확인한다.
  - 참조: SPEC §5.7, §5.1; ANALYSIS §4, Decision (f).
