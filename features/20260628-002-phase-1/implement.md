# phase-1 — 핵심 런타임 IMPLEMENT

> 순수 실행 체크리스트. 설계 근거는 analysis.md, 요구사항 레벨 완료 조건은 spec.md §5에 둔다.
> 의존성 순서(analysis.md §1-1 단방향 트리)를 line order로 표현한다. 위가 먼저 존재해야 아래가 가능하다.

## Section: leaf 데이터 패키지 (message / structured)

- [x] task-001: message 리듀서·트리밍·조회·근사 토큰
  - 목적: 호출자가 메시지 목록을 동일 ID upsert·신규 append로 누적하고, 삭제 마커·전체 삭제로 제거하며,
    strategy/max_tokens로 윈도를 잘라내고, 마지막 AI 메시지·도구 호출 유무·도구 호출 추출을 기대대로 조회할 수 있다.
  - 접근: `Message`/`ToolCall`/`ToolResult`/`Role`과 생성자, 리듀서(`AddMessages`/`RemoveMessage`/
    `RemoveAllSentinel`/`ApplyRemovals`/`TrimMessages`), 근사 토큰(`CountTokensApprox`, 외부 토크나이저 없음),
    조회(`LastMessage`/`LastAIMessage`/`HasToolCalls`/`ExtractToolCalls`/`FilterByName`), pretty print을 구현한다.
  - 검증 조건:
    - 결과: `AddMessages`가 같은 ID는 덮어쓰고 새 ID는 뒤에 붙인다. `RemoveMessage`+`ApplyRemovals`가 지정 메시지를
      지우고 `RemoveAllSentinel`이 전체를 비운다. `TrimMessages`가 strategy/start_on/end_on/max_tokens로 윈도를
      자르고 토큰 길이는 `CountTokensApprox`로 잰다. `LastAIMessage`/`HasToolCalls`/`ExtractToolCalls`가 기대값을 낸다.
    - 확인: 위 리듀서·트리밍·조회 동작을 덮는 단위 테스트가 통과한다(upsert/append, 단건·전체 삭제, 윈도 경계,
      도구 호출 추출 케이스 포함).
  - 참조: SPEC §5.2 / ANALYSIS §1-2, §2-4

- [x] task-002: structured 스키마 빌드·파싱·검증·표준 스키마
  - 목적: 호출자가 Go 구조체에서 JSON 스키마를 생성하고, raw JSON을 타입으로 파싱·검증하며, enum 제약 필드와
    표준 출력 스키마 6종을 그대로 가져다 쓸 수 있다.
  - 접근: `BuildSchema[T]`(구조체 태그 기반), `ParseStructured[T]`, `Validate`, `EnumField`,
    표준 스키마(`BinaryScore`/`RouterChoice[T]`/`AgentStatus`/`Plan`/`ConversationalResponse`/`PlannerResult`)를
    구현한다. 다른 Phase 1 패키지에 의존하지 않는다.
  - 검증 조건:
    - 결과: `BuildSchema[T]`가 태그(`json`/`description`/enum 제약)를 반영한 스키마를 만들고,
      `ParseStructured[T]`가 유효 JSON을 파싱하고 스키마 위반 JSON은 `Validate`가 거부하며, 표준 스키마 6종이
      각각 생성·사용 가능하다.
    - 확인: 스키마 생성·파싱 성공·검증 실패(enum 위반·필수 누락)·표준 스키마 6종을 덮는 단위 테스트가 통과한다.
  - 참조: SPEC §5.4 / ANALYSIS §1-2, §2-7

## Section: 도구 (tool)

- [x] task-003: tool 레지스트리·실행기·Runtime·함수 어댑터
  - 목적: 호출자가 Go 함수를 Tool로 등록·조회하고, 도구 호출을 실행기로 디스패치해 ToolMessage를 얻으며, 알 수
    없는 도구는 명시 에러로 막히고, 실행 중인 도구가 상태·호출 ID·설정·스토어·이벤트 방출에 접근할 수 있다.
  - 접근: `Tool` 인터페이스·`Schema`/`Parameter`/`Input`/`Result`, `Registry`(`Register`/`RegisterMany`/`Get`/
    `List`/`Schemas`), `Executor`(`Execute`/`ExecuteMany`/`ExecuteWithTimeout`/`BuildToolMessage`/`UnknownToolError`),
    `ValidateArgs`/`DecodeArgs[T]`, `Runtime`(`State`/`ToolCallID`/`Config`/`Store`/`Emit`), 좁은 인터페이스
    `Store`(Get/Put/Search)와 `Event`(tool 패키지 소유), `FromFunc`/`WithArgsSchema[T]`를 구현한다. store/trace
    구체 타입은 참조하지 않는다.
  - 검증 조건:
    - 결과: `FromFunc`/`WithArgsSchema`로 만든 Tool에 `message.ToolCall`을 디스패치하면 해당 Tool이 실행돼
      `ToolMessage`가 산출된다. `Registry` 등록/조회/`Schemas`가 동작하고, 미등록 도구 호출은 `UnknownToolError`를
      낸다. `ValidateArgs`/`DecodeArgs`가 인자를 검증·디코딩하고, Tool 구현이 `Runtime`으로 상태·`ToolCallID`·
      `Config`·`Store`·`Emit`에 접근한다.
    - 확인: 함수→Tool 디스패치, 인자 검증/디코딩, UnknownToolError, Runtime 접근(스텁 Store/Event 주입)을 덮는
      단위 테스트가 통과한다.
  - 참조: SPEC §5.3 / ANALYSIS §1-2, §5(D6)

## Section: LLM 추상화와 Anthropic 어댑터 (llm)

- [x] task-004: llm.Client 계약·도구 바인딩·구조화·InitChatModel (stub 검증)
  - 목적: 호출자가 인터페이스를 통해 챗·스트림·구조화 출력을 호출하고, 도구 바인딩으로 응답의 도구 호출 파싱을
    켜며, 구조화 경로가 스키마에 맞는 값을 돌려주고, `provider:model` 식별자가 해석되며, 이 모든 것을 네트워크
    없이 stub 구현으로 구동할 수 있다.
  - 접근: `Client` 인터페이스(`Chat`/`ChatStream`/`Structured`/`BindTools`/`ParseToolCalls`/`WithModel`/
    `ModelName`), `ChatRequest`/`ChatResponse`/`ChatEvent`/`ResponseFormat`(text/json_object/json_schema)/
    `TokenUsage`, `InitChatModel`(`provider:model` 파서, anthropic만 지원·그 외 에러)을 정의하고, 검증용 stub
    `Client`(정해진 응답/도구 호출 반환)를 둔다. 임베딩 타입·팩토리는 만들지 않는다.
  - 검증 조건:
    - 결과: stub `Client`로 `Chat`/`ChatStream`/`Structured`가 호출되고, `BindTools` 후 `ParseToolCalls`가
      `[]message.ToolCall`을 반환하며, `Structured`/`ResponseFormat`이 스키마에 맞는 값을 돌려주고,
      `InitChatModel`이 `anthropic:claude-opus-4-8`을 해석하고 미지원 provider에 에러를 낸다.
    - 확인: stub 기반으로 계약 메서드·도구 파싱·구조화·`InitChatModel` 파싱(성공/미지원 provider)을 덮는 단위
      테스트가 통과한다. 실제 네트워크 호출은 쓰지 않는다.
  - 참조: SPEC §5.5 / ANALYSIS §1-2, §2-2, §5(D9)

- [x] task-005: Anthropic 챗 어댑터 — message↔content-block 변환·도구·구조화 (게이트 라이브 스모크)
  - 목적: 실제 Claude 챗 호출이 어댑터를 통해 ChatResponse를 반환하고, 도구 바인딩된 호출이 응답에서 도구 호출을
    노출하며, 키가 없으면 검증이 건너뛰어진다.
  - 접근: Anthropic 공식 Go SDK(`github.com/anthropics/anthropic-sdk-go`)를 go.mod에 추가하고 `Client` 구현을
    둔다. 추상 요청을 content-block 메시지로(system→System, user/assistant→text, ToolCalls→tool_use,
    tool role→tool_result), 응답을 ChatResponse로(text→Content, tool_use→ToolCalls, StopReason→FinishReason,
    usage→TokenUsage) 변환한다. `claude-opus-4-8`에는 `temperature`/`top_p`/`top_k`를 전송하지 않고 `max_tokens`
    미지정 시 기본값을 채운다. 구조화는 `output_config.format`(json_schema) 또는 도구 강제로 매핑한다. SDK 타입은
    공개 API에 노출하지 않는다.
  - 검증 조건:
    - 결과: `ANTHROPIC_API_KEY`가 있을 때 라이브 호출이 `ChatResponse`를 반환하고 도구 바인딩 호출이 응답에서
      도구 호출을 노출한다. 키가 없으면 스모크 테스트가 skip된다. 미지원 샘플링 파라미터 미전송·`max_tokens`
      기본값 보장으로 400이 발생하지 않는다. 기본 모델은 `claude-opus-4-8`이다.
    - 확인: `ANTHROPIC_API_KEY` 게이트 라이브 스모크 테스트(챗 + 도구 바인딩)가 키 있을 때 통과하고 키 없으면
      skip된다. 어댑터 추가 후 모듈 루트에서 `go build ./...`/`go vet ./...`가 SDK 의존 포함 상태로 오류 없이 끝난다.
  - 참조: SPEC §5.6, §5.1 / ANALYSIS §2-2, §5(D3, D4, D5, D9)

## Section: 프롬프트·미들웨어·노드·체크포인트

- [x] task-006: prompt 템플릿·체인·구조화 출력
  - 목적: 호출자가 플레이스홀더(메시지 플레이스홀더 포함)를 채운 메시지 목록을 템플릿으로 만들고, 파이프로 만든
    체인을 호출해 모델을 실행하며, 구조화 출력 옵션으로 구조화 결과를 얻을 수 있다.
  - 접근: `PromptTemplate`/`MessagesPlaceholder`/`MessageSpec`/`Chain`과 `FromMessages`/`FromTemplate`/`Format`/
    `Pipe`/`Invoke`/`WithStructuredOutput`을 구현한다. `message`·`llm`·`structured`를 소비한다.
  - 검증 조건:
    - 결과: `Format(vars)`가 `MessagesPlaceholder`를 포함한 플레이스홀더를 채운 `[]Message`를 반환하고,
      `Pipe`로 만든 `Chain.Invoke`가 (stub) 모델을 호출하며 `WithStructuredOutput`이 구조화 결과를 낸다.
    - 확인: 템플릿 포매팅(플레이스홀더·메시지 플레이스홀더), stub 모델 기반 체인 Invoke, 구조화 출력 경로를
      덮는 단위 테스트가 통과한다.
  - 참조: SPEC §5.7 / ANALYSIS §1-2

- [x] task-007: middleware 훅 — WrapModelCall·BeforeModel·DynamicPrompt·ModelRequest 조작
  - 목적: 모델 호출이 미들웨어로 감싸지고, 모델 호출 전 훅이 공유 상태에 접근하거나 호출을 차단하며, 호출마다
    시스템 프롬프트가 동적으로 생성되고, 요청 객체의 모델 교체·프롬프트 치환·상태 값 읽기가 실행에 반영된다.
  - 접근: `Middleware` 인터페이스, `ModelRequest`(`State core.State`/`Model`/`SystemPrompt`)/`ModelResponse`/
    `ModelHandler`/`Runtime`, `WrapModelCall`/`BeforeModel`/`DynamicPrompt`, `ModelRequest.Override`/
    `SetSystemPrompt`/`StateValue`를 구현한다. 상태 인자는 `core.State`이고 `agent`를 참조하지 않는다.
  - 검증 조건:
    - 결과: `WrapModelCall`이 핸들러를 감싸 요청/응답을 가공하고, `BeforeModel`이 모델 호출 전 실행돼 `core.State`에
      접근하거나 에러로 호출을 차단하며, `DynamicPrompt`가 호출마다 시스템 프롬프트를 만든다. `Override`/
      `SetSystemPrompt`/`StateValue`의 효과가 (stub 모델) 호출에 반영된다.
    - 확인: 체인 래핑·BeforeModel 차단/통과·DynamicPrompt 치환·ModelRequest 조작을 stub 모델로 관찰하는 단위
      테스트가 통과한다.
  - 참조: SPEC §5.9 / ANALYSIS §1-2, §2-3, §5(D1)

- [x] task-008: prebuilt ToolNode·ToolsCondition·SummarizationNode
  - 목적: 마지막 AI 메시지의 미처리 도구 호출을 실행해 ToolMessage를 상태에 추가하는 노드, 도구 실행/종료로
    라우팅하는 조건, 임계 초과 시 누적 대화를 요약해 상태에 저장하고 과거 메시지를 제거한 뒤 요약을 재주입하는
    노드를 호출자가 쓸 수 있다.
  - 접근: 로컬 노드 함수 타입(`func(ctx, core.State) (core.StateUpdate, error)`), `NewToolNode`/`ToolsCondition`/
    `HasPendingToolCalls`/`NewSummarizationNode`/`ShouldSummarize`/`InjectSummary`/`SummarizeOptions`를 구현한다.
    도구 디스패치는 `tool.Executor`, 요약은 `llm.Client`를 쓰고, graph 타입은 참조하지 않는다.
  - 검증 조건:
    - 결과: ToolNode가 미처리 `tool_calls`를 실행해 `ToolMessage`를 상태에 추가하고, `ToolsCondition`이 미처리
      도구 호출 유무로 tools/END를 가르며, `ShouldSummarize`가 임계(메시지 수/토큰) 초과를 판정하면
      `SummarizationNode`가 대화를 (stub) 요약해 `summary` 상태에 저장하고 과거 메시지를 제거하며 `InjectSummary`가
      요약을 SystemMessage로 재주입한다.
    - 확인: ToolNode 실행·라우팅 분기·요약 임계 판정/요약 저장/과거 제거/재주입을 stub 모델로 덮는 단위 테스트가
      통과한다.
  - 참조: SPEC §5.10, §5.3 / ANALYSIS §1-3, §2-4, §2-5, §5(D1)

- [x] task-009: checkpoint InMemorySaver·ThreadIDFromConfig·LoadState·SaveState
  - 목적: 스레드 단위 상태가 메모리에 영속되고 조회·이력·삭제가 가능하며, 설정에서 스레드 ID를 뽑아 상태를
    저장·복원할 수 있다.
  - 접근: `Checkpointer` 인터페이스, `InMemorySaver`, `Checkpoint`, `StateSnapshot = core.StateSnapshot`,
    `Get`/`Put`/`List`/`DeleteThread`, `ThreadIDFromConfig`/`LoadState`/`SaveState`를 구현한다. `core`·`config`만
    의존하고 graph는 참조하지 않는다.
  - 검증 조건:
    - 결과: `InMemorySaver`가 `Put`/`Get`/`List`/`DeleteThread`로 스레드 상태를 영속하고, `ThreadIDFromConfig`가
      `config`에서 thread_id를 뽑으며, `SaveState`로 저장한 `core.State`를 `LoadState`가 같은 thread_id로 복원한다.
    - 확인: Put/Get round-trip, List 이력, DeleteThread, ThreadIDFromConfig, SaveState→LoadState 복원을 덮는
      단위 테스트가 통과한다.
  - 참조: SPEC §5.11 / ANALYSIS §1-2, §2-6, §5(D7)

## Section: agent 통합

- [x] task-010: agent ReAct 루프 — Invoke·Stream·Decision·응답 포맷
  - 목적: 도구 호출이 남는 동안 모델→도구→모델 루프를 직접 돌려 도구를 실행하고 메시지를 누적한 결과를 반환하며,
    응답 포맷 지정 시 종료 직전 구조화 응답을 채우고, 스트림이 진행 이벤트를 방출한다.
  - 접근: `Agent`/`Config`/`State`/`Result`/`Input`/`Decision`(continue/respond/end)/`AgentEvent`와 `Create`/
    `Invoke`/`Stream`/`GetState`, 옵션(`WithSystemPrompt`/`WithMiddleware`/`WithCheckpointer`/`WithStore`/
    `WithResponseFormat`/`WithMaxSteps`)을 구현한다. 내부 루프 `runModel`(미들웨어 체인 경유)/`runTools`(Executor
    디스패치)/`shouldContinue`/`applyResponseFormat`를 직접 구현하고 graph는 참조하지 않는다. `Stream` 모드 인자는
    `core.Mode`, 스냅샷은 `core.StateSnapshot`이다. `MaxSteps`로 무한 루프를 막는다.
  - 검증 조건:
    - 결과: `Invoke`가 도구 호출이 남는 동안 루프를 돌려 도구를 실행하고 메시지를 누적한 `Result`를 반환한다.
      `WithResponseFormat` 지정 시 종료 직전 `structured_response`가 채워진다. `Stream`이
      `AgentEvent`(토큰/업데이트/`IsTaskComplete`/`RequireUserInput`)를 방출한다. `shouldContinue`가
      continue/respond/end를 올바로 가르고 `MaxSteps` 초과 시 강제 종료한다.
    - 확인: stub 모델로 도구 루프 다회 반복·`shouldContinue` 분기·`applyResponseFormat`·`Stream` 이벤트 방출·
      `MaxSteps` 종료를 덮는 단위 테스트가 통과한다.
  - 참조: SPEC §5.8 / ANALYSIS §1-1, §2-1, §2-3, §5(D2, D7)

- [x] task-011: 단일 에이전트 end-to-end 통합 (stub 모델)
  - 목적: 도구 호출 루프 + 미들웨어 + 단기 메모리(트리밍과 요약) + 구조화 출력을 결합한 단일 에이전트가 종단
    실행돼 기대한 메시지와 구조화 응답을 반환하고, 같은 스레드의 별도 호출이 이전 메시지를 이어받는다.
  - 접근: task-001~task-010 산출물을 묶어, stub `llm.Client` + 도구 레지스트리 + 미들웨어 + 체크포인터 +
    트리밍/요약 + 응답 포맷을 한 에이전트에 결합한 e2e 검증을 구성한다. 라이브 어댑터는 쓰지 않는다.
  - 검증 조건:
    - 결과: stub 모델로 구동한 단일 에이전트가 도구 루프를 돌고, 미들웨어가 호출에 반영되며, 단기 메모리(트리밍·
      요약)가 적용되고, 구조화 출력이 `structured_response`에 채워진 기대 결과를 반환한다. `WithCheckpointer`로 만든
      에이전트가 동일 thread_id의 이전 메시지를 별도 `Invoke` 간에 이어받는다. 모듈 루트에서 `go build ./...`/
      `go vet ./...`가 오류 없이 끝난다.
    - 확인: 네 기능(도구 루프·미들웨어·단기 메모리·구조화)을 결합한 e2e 단위 테스트와 체크포인트 이어받기
      테스트가 stub 모델로 통과하고, `go build ./...`/`go vet ./...`가 통과한다.
  - 참조: SPEC §5.12, §5.11, §5.1 / ANALYSIS §2-1, §2-3, §2-4, §2-6, §5(D9)
