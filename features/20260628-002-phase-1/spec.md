# phase-1 — 핵심 런타임

## 1. 범위

langgraph-go의 단일 에이전트 런타임을 구성하는 Phase 1이다(README §26 Phase 1). 다음 패키지를 다룬다.

- `message` — 메시지 모델과 누적 리듀서(add_messages/remove/trim), 근사 토큰 카운트, pretty print.
- `llm` — 챗 모델 추상화(`Client` 인터페이스), `Chat`/`ChatStream`/`Structured`, 도구 바인딩(`BindTools`/
  `ParseToolCalls`), `InitChatModel`, 그리고 **Anthropic(Claude) 챗 어댑터**.
- `tool` — `Tool` 인터페이스·스키마·레지스트리·실행기, 인자 검증/디코딩, `Runtime` 주입, Go 함수 → Tool 헬퍼.
- `structured` — Go 타입 ↔ JSON 스키마 변환, 구조화 출력 파싱/검증, 표준 출력 스키마.
- `prompt` — 프롬프트 템플릿·메시지 플레이스홀더·체인.
- `agent` — `create_agent` 대응 ReAct 루프(도구 호출 루프 + 미들웨어 + 체크포인터 + 응답 포맷).
- `middleware` — `wrap_model_call`/`before_model`/`dynamic_prompt` 훅과 동적 프롬프트.
- `prebuilt` — ToolNode, tools_condition, 단기 메모리 요약 노드.
- `checkpoint` — 스레드 단위 상태 영속(단기 메모리), `InMemorySaver`.

이 Phase의 통합 산출물은 "단일 에이전트(도구 호출 루프 + 미들웨어 + 단기 메모리(트리밍·요약) + 구조화 출력)가
동작한다"이다(README §26 Phase 1).

## 2. 목표

다운스트림이 import해서 곧바로 단일 Claude 에이전트를 구동할 수 있는 핵심 런타임 primitive를 제공한다.
Phase 2 그래프 엔진 없이도 ReAct 루프·미들웨어·단기 메모리·구조화 출력이 동작하게 해, 이후 Phase(그래프·
멀티에이전트·RAG 등)가 이 primitive 위에 쌓일 토대를 만든다. 프로바이더는 Anthropic으로 고정하되, 런타임의
동작 검증이 실제 API 호출 없이도 가능하도록 `llm.Client` 인터페이스 경계를 유지한다.

## 3. 제약

- 의존 방향(README §28-1): Phase 1 패키지는 Phase 0 leaf(`core`/`config`)와 서로 간 단방향 의존만 가지며,
  **아직 존재하지 않는 Phase 2 패키지(`graph`/`command`/`streaming`)를 import하지 않는다**. README 시그니처가
  `graph.State`/`graph.NodeFunc`/`streaming.Mode` 등 Phase 2 타입을 가리키는 자리(예: `prebuilt`, `middleware`의
  상태 인자, `agent`/스트림 모드)는 `core` 소유 타입(`core.State`/`core.Mode`) 또는 Phase 1 로컬 타입으로 충족해
  Phase 1만으로 컴파일된다.
- `agent`의 ReAct 루프는 Phase 2 그래프 엔진에 의존하지 않고 직접 구현한다("그래프로 컴파일된다"는 개념적
  표현이며 Phase 1에서는 `runModel`/`runTools`/`shouldContinue` 류의 직접 루프로 충족한다).
- `tool.Runtime`은 `store`/`trace` 구체 타입이 아니라 `tool` 패키지 내 좁은 인터페이스(`Store`/`Event`)를
  반환·수신한다(README §28-1 규칙2). `store` 구현(Phase 5)·`trace` 구현(Phase 7)이 없어도 Phase 1은 컴파일·동작한다.
- 챗 프로바이더는 Anthropic만 구현한다. 기본 모델은 `claude-opus-4-8`이며, `InitChatModel`의 식별자는
  `provider:model` 형식(`anthropic:claude-opus-4-8`)을 받아 다른 프로바이더로 확장할 자리만 남긴다.
- Anthropic 어댑터는 Anthropic 공식 Go SDK(`github.com/anthropics/anthropic-sdk-go`)를 사용한다. 이 SDK가 Phase 1의
  신규 외부 의존성이다. 챗 추상화의 선택 파라미터는 `claude-opus-4-8`이 수용하는 요청 표면과 호환되어야 한다
  (이 모델은 `temperature`/`top_p`/`top_k`를 거부하므로 어댑터가 그런 미지원 샘플링 파라미터를 강제로 전송하지
  않는다). README의 OpenAI식 `tool_calls`/`ResponseFormat` 추상화는 Anthropic의 `tool_use` content block과
  `output_config.format`(구조화 출력)으로 매핑한다.
- 토큰 카운트는 근사형으로 충분하다(tiktoken 정밀 카운트 불필요, README §2).
- 완료 조건 검증: 런타임 동작(에이전트 루프·도구 디스패치·미들웨어·트리밍/요약·구조화 출력·체크포인트)은
  실제 네트워크 없이 stub `llm.Client` 구현으로 검증한다. 실제 Anthropic 챗 어댑터는 `ANTHROPIC_API_KEY`가 있을
  때만 실행되는 라이브 스모크 테스트로 검증하고, 키가 없으면 skip한다(CI는 키 없이 통과).

## 4. 제외 범위

- 임베딩(`InitEmbeddings`/`EmbeddingClient`/`Embed`/`EmbedQuery`)은 Phase 1 범위 밖이며 Phase 3(vectorstore)에서
  첫 소비 시점에 구현한다.
- Anthropic 외 챗 프로바이더(OpenAI/gemini 등) 구현은 제외한다(`InitChatModel`에 확장 자리만 남긴다).
- Phase 2 이후 패키지: `graph`/`command`/`streaming`(Phase 2), `document`/`vectorstore`(Phase 3),
  `store` 구현체(Phase 5 — Phase 1은 `tool.Store` 좁은 인터페이스만), `mcp`(Phase 6),
  `a2a`/`database`/`search`/`storage`/`trace` 구현·`config` 어셈블리 함수(Phase 7).
- 그래프 인터럽트(`interrupt`/`resume`) 기반 HITL은 제외하고, 추가 입력은 이후 A2A `input_required`로 처리한다
  (README §7·§22-4). Phase 1의 `agent`는 추가 입력 필요 신호(`RequireUserInput`)를 이벤트로 노출하는 데까지만 다룬다.
- 응용 계층(멀티에이전트·RAG·오케스트레이션)은 제외한다.

## 5. 완료 조건

1. 모듈 루트에서 `go build ./...`와 `go vet ./...`가 오류 없이 끝난다(Phase 1 신규 패키지와 Anthropic Go SDK
   의존이 추가된 상태).
2. `message` 리듀서가 호출자 기대대로 동작한다: `AddMessages`가 동일 ID는 upsert·신규는 append하고,
   `RemoveMessage`+`ApplyRemovals`가 삭제 마커(및 `RemoveAllSentinel` 전체 삭제)를 반영하며, `TrimMessages`가
   strategy/max_tokens로 윈도를 자르고, `LastAIMessage`/`HasToolCalls`/`ExtractToolCalls`가 기대값을 반환한다.
3. `tool`: Go 함수를 `FromFunc`/`WithArgsSchema`로 Tool화하면 `Executor`가 주어진 `message.ToolCall`을 그 Tool로
   디스패치해 `ToolMessage`를 산출한다. `Registry`의 등록/조회/`Schemas`, 알 수 없는 도구 시 `UnknownToolError`,
   `ValidateArgs`/`DecodeArgs`, `Runtime`의 `State`/`ToolCallID`/`Config`/`Store`/`Emit` 접근이 관찰된다.
4. `structured`: `BuildSchema[T]`가 Go 구조체에서 스키마를 생성하고, `ParseStructured[T]`/`Validate`가 raw JSON을
   파싱·검증하며, `EnumField` 제약과 표준 스키마(`BinaryScore`/`RouterChoice`/`AgentStatus`/`Plan`/
   `ConversationalResponse`/`PlannerResult`)를 호출자가 사용할 수 있다.
5. `llm.Client` 계약: 호출자가 인터페이스를 통해 `Chat`/`ChatStream`/`Structured`를 호출하고, `BindTools`로
   `tool_calls` 파싱을 활성화해 `ParseToolCalls`가 `[]message.ToolCall`을 반환하며, `Structured`/`ResponseFormat`이
   스키마에 맞는 값을 돌려주고, `InitChatModel`이 `anthropic:claude-opus-4-8` 형식 식별자를 해석한다. stub
   `Client` 구현으로 검증 가능하다.
6. Anthropic 챗 어댑터(라이브, 게이트): 실제 Claude 챗 호출이 어댑터를 통해 `ChatResponse`를 반환하고, 도구
   바인딩된 호출은 응답에서 `tool_calls`를 노출한다. `ANTHROPIC_API_KEY`가 있을 때만 도는 스모크 테스트로
   검증하며 키가 없으면 skip한다. 기본 모델은 `claude-opus-4-8`이다.
7. `prompt`: `PromptTemplate.Format(vars)`가 플레이스홀더(`MessagesPlaceholder` 포함)를 채운 `[]Message`를
   반환하고, `Pipe`로 만든 `Chain`의 `Invoke`가 모델을 호출하며 `WithStructuredOutput`이 구조화 결과를 낸다.
8. `agent` ReAct 루프: `agent.Invoke`가 도구 호출이 남는 동안 chatbot→tools→chatbot 루프를 돌려 도구를 실행하고
   메시지를 누적한 `Result`를 반환한다. `WithResponseFormat` 지정 시 종료 직전 `structured_response`가 채워지며,
   `Stream`이 진행 이벤트(`AgentEvent`: 토큰/업데이트/`IsTaskComplete`/`RequireUserInput`)를 방출한다. stub
   모델로 검증 가능하다.
9. `middleware`: `WrapModelCall`이 모델 호출을 감싸고, `BeforeModel`이 모델 호출 전에 실행되어 공유 상태에 접근/
   차단하며, `DynamicPrompt`가 호출마다 시스템 프롬프트를 생성한다. `ModelRequest`의 `Override`/`SetSystemPrompt`/
   `StateValue`가 에이전트 실행에 반영되는 것이 관찰된다.
10. `prebuilt`: ToolNode가 마지막 AI 메시지의 미처리 `tool_calls`를 실행해 `ToolMessage`를 상태에 추가하고,
    tools_condition이 도구 실행/종료로 라우팅하며, 요약 노드가 임계(메시지 수/토큰) 초과 시 누적 대화를 요약해
    `summary` 상태에 저장하고 과거 메시지를 제거하며 `InjectSummary`가 요약을 재주입한다.
11. `checkpoint`: `InMemorySaver`가 스레드 상태를 `Put`/`Get`/`List`/`DeleteThread`로 영속하고,
    `ThreadIDFromConfig`/`LoadState`/`SaveState`가 동작하며, `WithCheckpointer`로 만든 에이전트가 동일 thread_id의
    이전 메시지를 별도 `Invoke` 호출 간에 이어받는다.
12. 통합: 도구 호출 루프 + 미들웨어 + 단기 메모리(트리밍과 요약) + 구조화 출력을 결합한 단일 에이전트가 stub
    모델로 엔드투엔드 실행되어 기대한 메시지와 `structured_response`를 반환한다(README §26 Phase 1 산출물).
