# phase-1 — 핵심 런타임 ANALYSIS

## 근거

작성 전에 읽은 것과 코드베이스에서 확인한 사실을 분리해 적는다.

**읽은 spec.md 범위**: spec.md 전체(§1 범위 ~ §5 완료 조건). 범위는 §1의 9개 패키지(`message`/`llm`/`tool`/
`structured`/`prompt`/`agent`/`middleware`/`prebuilt`/`checkpoint`)와 Anthropic Go SDK 도입으로 한정되고, 완료
조건은 §5(1~12)다. 제외 범위는 §4(임베딩·Anthropic 외 프로바이더·Phase 2 이후 패키지·그래프 인터럽트 HITL·응용
계층)다.

**읽은 README.md 범위**: §1(패키지 구조와 의존 트리 — `config`/`core` leaf 토대, 상위→하위 단방향),
§2(message — 타입·생성·조회·리듀서·근사 토큰·pretty print), §3(prompt — `PromptTemplate`/`MessagesPlaceholder`/
`MessageSpec`/`Chain`, `Pipe`/`WithStructuredOutput`), §4(llm — `Client`/`ChatRequest`/`ChatResponse`/`ChatEvent`/
`ResponseFormat`, `Chat`/`ChatStream`/`Structured`, `BindTools`/`ParseToolCalls`, `InitChatModel` `provider:model`
형식, 임베딩 항목은 Phase 3로 분리), §5(tool — `Tool`/`Schema`/`Registry`/`Executor`/`Runtime`/`Store`/`Event`,
`FromFunc`/`WithArgsSchema`, `ValidateArgs`/`DecodeArgs`, Runtime 접근자), §6(structured — `Schema`/`Validator`/
`FieldOption`, `BuildSchema[T]`/`ParseStructured[T]`/`Validate`/`EnumField`, 표준 스키마 6종), §8(prebuilt —
ToolNode/tools_condition/Summarization, 시그니처가 `graph.NodeFunc`/`graph.State`를 가리킴), §9(agent — `Agent`/
`Config`/`State`/`Result`/`Decision`/`AgentEvent`, `Create`/`Invoke`/`Stream`/`GetState`, 내부 루프 `runModel`/
`runTools`/`shouldContinue`/`applyResponseFormat`), §10(middleware — `Middleware`/`ModelRequest`/`ModelResponse`/
`ModelHandler`/`Runtime`, `WrapModelCall`/`BeforeModel`/`DynamicPrompt`, 상태 인자가 `graph.State`라는 경계 주석),
§11(checkpoint — `Checkpointer`/`InMemorySaver`/`Checkpoint`/`StateSnapshot`, `Get`/`Put`/`List`/`DeleteThread`,
`ThreadIDFromConfig`/`LoadState`/`SaveState`), §22-4(agent→A2A 어댑터가 읽는 `IsTaskComplete`/`RequireUserInput`/
`Content`와 `structured_response.status` 매핑 — Phase 1은 이벤트 노출까지만), §24(config 공개 계약),
§26(구현 순서 — Phase 0/1 범위, store/trace 미구현 컴파일), §27(핵심 최소 집합), §28(Go 생태계 유의사항),
§28-1(import 사이클 회피 규칙 1~4와 확인된 순환 목록).

**코드베이스에서 확인한 사실(Phase 0 산출물)**:
- `core/core.go`: `State`/`StateUpdate`(둘 다 `map[string]any`), `Mode`(명명 문자열 + `ModeValues`/`ModeMessages`/
  `ModeUpdates`/`ModeDebug`), `StateSnapshot`(`Values State`/`Next []string`/`Config config.RunConfig`/
  `Metadata map[string]any`/`CreatedAt time.Time`)가 구현돼 있다. `core`는 `config`만 import한다.
- `config/config.go`: `Config`(자격증명 + `ModelConfig`/`ServerConfigs`/`AgentConfigs`), `RunConfig`(`Configurable
  map[string]any`), `ModelConfig`(`Model`/`Temperature`), `ServerConfig`/`AgentConfig` 타입, `LoadEnv`/`GetThreadID`/
  `GetUserID`/`GetConfigurable`가 구현돼 있다. `config`는 무의존 leaf다.
- 모듈 경로는 `github.com/zipkero/langgraph-go`(core.go의 import 경로로 확인). Phase 1 패키지는
  `github.com/zipkero/langgraph-go/message` 등으로 import된다.
- Phase 2 패키지(`graph`/`command`/`streaming`)와 Phase 3+ 패키지는 아직 디렉토리·소스가 없다. 따라서 Phase 1은
  이들을 import할 수 없으며, README가 `graph.State`/`graph.NodeFunc`/`streaming.Mode`를 가리키는 자리는 `core`
  타입이나 Phase 1 로컬 타입으로 충족해야 한다.

**이미 확정된 결정**(질문 없이 §5 Decision Points 채택안으로 반영):
- 챗 프로바이더는 Anthropic만, 기본 모델 `claude-opus-4-8`, 외부 의존성으로 Anthropic 공식 Go SDK
  `github.com/anthropics/anthropic-sdk-go` 도입.
- 임베딩(`InitEmbeddings`/`EmbeddingClient`)은 Phase 1 범위 밖(Phase 3). 시그니처도 만들지 않는다.
- Phase 2 패키지에 의존 금지. `agent` ReAct 루프는 그래프 엔진 없이 직접 구현한다.
- `tool.Runtime`은 `store`/`trace` 구체 타입이 아니라 `tool` 패키지 내 좁은 인터페이스(`Store`/`Event`)를
  반환·수신한다(§28-1 규칙2).
- 검증은 stub `llm.Client`로 런타임을, `ANTHROPIC_API_KEY` 게이트 라이브 스모크로 Anthropic 어댑터를 다룬다.

**Anthropic Go SDK 매핑 사실**(claude-api 스킬에서 확인된 것으로 프롬프트가 제공, §2·§3·§5에 반영):
챗은 `client.Messages.New`/`NewStreaming`, 도구는 `anthropic.ToolParam`/`ToolUnionParam`과 `tool_use` content
block(`block.AsAny().(anthropic.ToolUseBlock)`, `block.ID`/`block.Name`/`block.JSON.Input.Raw()`), 종료 판정은
`resp.StopReason != anthropic.StopReasonToolUse`, 도구 결과 회신은 `anthropic.NewToolResultBlock`, 히스토리
append는 `resp.ToParam()`. 구조화 출력은 OpenAI식 `response_format`이 아니라 `output_config.format`(json_schema)
또는 도구 강제 사용. `claude-opus-4-8`은 `temperature`/`top_p`/`top_k`를 보내면 400이고 `max_tokens`는 필수다.

**추정**: Anthropic SDK의 정확한 버전 핀은 구현 시점 안정 버전을 쓰며 결과에 영향을 주지 않는 세부라
implementer 재량에 둔다. `max_tokens` 기본값(추상 요청에 미지정 시 어댑터가 채울 값)은 §5 D5에서 commit한다.

## 1. 구조

모듈 루트(`github.com/zipkero/langgraph-go`) 하위에 9개의 공개 패키지를 신규로 추가한다. Phase 0 leaf
(`core`/`config`) 위에 쌓이며, 이 Phase의 본질은 **그래프 엔진(Phase 2) 없이도 단일 에이전트가 도는 단방향
의존 트리를 닫는 것**이다. 파일이 아니라 패키지 경계와 의존 방향으로 기술한다.

### 1-1. 의존 트리(단방향)

아래 트리는 "위가 아래를 import한다"를 뜻한다(`A → B`는 `A`가 `B`를 import). 역방향 화살표는 없다(§28-1 규칙4).
괄호는 외부 의존이다.

```text
config            (leaf, 무의존 — Phase 0)
  ↑
core              (→ config — Phase 0)
  ↑
message           (→ core; 외부 없음)
structured        (→ 외부 JSON 스키마/reflect; message·core 비참조 — 6 참고)
tool              (→ message, config; 패키지 내 Store/Event 좁은 인터페이스 자체 소유 — store/trace 비참조)
llm               (→ message, tool, structured, core; → anthropic-sdk-go)
prompt            (→ message, llm, structured)
prebuilt          (→ message, tool, llm, core; graph 비참조 — NodeFunc/State는 core 기반 로컬 타입)
middleware        (→ message, llm, core; agent 비참조 — 상태 인자는 core.State)
agent             (→ message, llm, tool, structured, middleware, prebuilt, checkpoint, core, config)
checkpoint        (→ core, config; graph 비참조 — StateSnapshot은 core.StateSnapshot)
```

핵심 경계 결정:

- **`tool`은 `store`/`trace`를 import하지 않는다.** `Runtime.Store()`는 `tool` 패키지 안에 선언한 최소
  인터페이스 `tool.Store`(Get/Put/Search)를 반환하고, `Runtime.Emit(ev Event)`는 `tool` 패키지 안의 `tool.Event`
  타입을 받는다. Phase 5 `store.Store`·Phase 7 `trace`가 이 좁은 타입을 충족·수신하지만, Phase 1에는 그 구현이
  없어도 `tool`이 인터페이스만으로 컴파일된다(§28-1 규칙2, SPEC §5.1·§5.3).
- **`agent`는 `graph`(Phase 2)를 import하지 않는다.** ReAct 루프를 `runModel`/`runTools`/`shouldContinue` 직접
  루프로 구현하고, 상태 스냅샷은 `core.StateSnapshot`을 쓴다(§28-1 규칙1, SPEC §5.8).
- **`checkpoint`는 `graph`를 import하지 않는다.** `StateSnapshot`은 `core.StateSnapshot`을 그대로 쓰고,
  `LoadState`/`SaveState`는 `core.State`를 다룬다. README §11이 `graph.State`라 적은 자리를 `core.State`로 충족해
  Phase 역전을 끊는다(§28-1 규칙1, SPEC §5.11).
- **`middleware`는 `agent`를 import하지 않는다.** README §10이 못박은 대로, 상태 인자는 `core.State`(범용 상태
  맵)이지 `agent.State`가 아니다. `agent`가 `middleware`를 import하므로(`Config.Middleware`), 역참조하면 순환이
  된다(§28-1 규칙4, SPEC §5.9).
- **`prebuilt`의 노드 타입은 `core` 기반 로컬 타입이다.** README §8이 `graph.NodeFunc`/`graph.State`를 가리키지만
  Phase 2가 없으므로, `prebuilt`는 `func(ctx, core.State) (core.StateUpdate, error)` 형태의 로컬 노드 함수 타입을
  쓴다(아래 1-3). `graph`가 생기면 Phase 2에서 이 타입을 `graph.NodeFunc`로 흡수/정합시키되, Phase 1은 그것
  없이 닫힌다(SPEC §5.10).

### 1-2. 패키지별 책임 경계

- **`message`** — 메시지 모델(`Message`/`ToolCall`/`ToolResult`/`Role`)과 누적 리듀서. `AddMessages`(ID upsert/
  신규 append), `RemoveMessage`+`ApplyRemovals`(삭제 마커·`RemoveAllSentinel`), `TrimMessages`(strategy/start_on/
  end_on/max_tokens 윈도 트리밍), `CountTokensApprox`(근사 토큰), 조회(`LastMessage`/`LastAIMessage`/
  `HasToolCalls`/`ExtractToolCalls`/`FilterByName`), pretty print. `core`만 의존하며 다른 Phase 1 패키지를
  참조하지 않는 가장 하단 노드다(SPEC §5.2).
- **`structured`** — Go 타입 ↔ JSON 스키마 변환과 구조화 출력 파싱/검증. `BuildSchema[T]`(구조체 태그 기반 제네릭
  스키마), `ParseStructured[T]`, `Validate`, `EnumField`(제약 필드), 표준 스키마 6종(`BinaryScore`/`RouterChoice[T]`/
  `AgentStatus`/`Plan`/`ConversationalResponse`/`PlannerResult`). 다른 Phase 1 패키지에 의존하지 않는 독립
  노드로, `llm`·`agent`가 이를 소비한다(SPEC §5.4).
- **`tool`** — 도구 정의·스키마·레지스트리·실행기·런타임 주입. `Tool` 인터페이스, `Schema`/`Parameter`,
  `Registry`(등록/조회/`Schemas`), `Executor`(`Execute`/`ExecuteMany`/`ExecuteWithTimeout`/`BuildToolMessage`/
  `UnknownToolError`), `ValidateArgs`/`DecodeArgs[T]`, `Runtime`(`State`/`ToolCallID`/`Config`/`Store`/`Emit`),
  `FromFunc`/`WithArgsSchema[T]`. `message`(`ToolCall`/`ToolMessage`)와 `config`(`RunConfig`)에 의존하고,
  `Store`/`Event` 좁은 인터페이스는 자체 소유한다(SPEC §5.3).
- **`llm`** — 챗 추상화와 Anthropic 어댑터. `Client` 인터페이스(`Chat`/`ChatStream`/`Structured`/`BindTools`/
  `ParseToolCalls`/`WithModel`/`ModelName`), `ChatRequest`/`ChatResponse`/`ChatEvent`/`ResponseFormat`/
  `TokenUsage`, `InitChatModel`(`provider:model` 파서), 그리고 Anthropic 챗 어댑터(SDK 호출 + message↔content-block
  변환). 임베딩 항목은 만들지 않는다(SPEC §5.5·§5.6).
- **`prompt`** — 프롬프트 템플릿과 체인. `PromptTemplate`/`MessagesPlaceholder`/`MessageSpec`/`Chain`,
  `FromMessages`/`FromTemplate`/`Format`/`Pipe`/`Invoke`/`WithStructuredOutput`. `message`·`llm`·`structured`를
  소비한다(SPEC §5.7).
- **`prebuilt`** — 사전 구성 노드. `ToolNode`(마지막 AI 메시지의 미처리 `tool_calls` 실행 → `ToolMessage` 추가),
  `ToolsCondition`/`HasPendingToolCalls`(라우팅), `NewSummarizationNode`/`ShouldSummarize`/`InjectSummary`/
  `SummarizeOptions`(요약 압축). 노드 타입은 `core.State` 기반 로컬 타입이다(SPEC §5.10).
- **`middleware`** — 모델 호출 훅. `Middleware` 인터페이스, `ModelRequest`(상태 인자 `core.State`)/`ModelResponse`/
  `ModelHandler`/`Runtime`, `WrapModelCall`/`BeforeModel`/`DynamicPrompt`, `ModelRequest.Override`/
  `SetSystemPrompt`/`StateValue`. `agent`를 참조하지 않는다(SPEC §5.9).
- **`checkpoint`** — 스레드 단위 상태 영속. `Checkpointer` 인터페이스, `InMemorySaver`, `Checkpoint`,
  `StateSnapshot=core.StateSnapshot`, `Get`/`Put`/`List`/`DeleteThread`, `ThreadIDFromConfig`/`LoadState`/
  `SaveState`. `core`·`config`만 의존한다(SPEC §5.11).
- **`agent`** — ReAct 루프 통합. `Agent`/`Config`/`State`/`Result`/`Input`/`Decision`/`AgentEvent`,
  `Create`/`Invoke`/`Stream`/`GetState`, 내부 루프(`runModel`/`runTools`/`shouldContinue`/`applyResponseFormat`).
  미들웨어·체크포인터·스토어·응답 포맷을 결합하는, 트리 최상단 통합 노드다(SPEC §5.8·§5.12).

### 1-3. graph 없는 노드 시그니처 — `prebuilt`/`middleware`/`agent` 공통 처리

README가 Phase 2 타입(`graph.State`/`graph.NodeFunc`/`streaming.Mode`)을 가리키는 자리를 Phase 1에서 어떻게
충족하는지가 이 Phase 구조 설계의 가장 큰 논점이다. 세 자리를 다음과 같이 닫는다(상세 옵션은 §5 D1).

- 노드 함수 타입: `prebuilt`가 `type NodeFunc func(ctx context.Context, st core.State) (core.StateUpdate, error)`
  류의 **로컬 타입**을 소유한다(README의 `graph.NodeFunc`를 `core.State`/`core.StateUpdate` 기반으로 치환).
  `ToolNode`/`SummarizationNode`는 이 타입의 값을 반환한다.
- 상태 인자: `middleware`의 `ModelRequest.State`와 `BeforeModel`/`DynamicPrompt`의 상태 인자는 `core.State`다
  (README §10 경계 주석 그대로).
- 스트림 모드: `agent.Stream`의 모드 인자는 `core.Mode`다(README §9가 `core.Mode`로 이미 명시; `streaming.Mode`
  alias는 Phase 2 소관).

Phase 2가 도입되면 `graph.NodeFunc = func(ctx, core.State) (any, error)`, `graph.State = core.State` alias로
정합되며, 이 로컬 타입들은 그때 흡수·재노출 대상이 된다. Phase 1은 그 alias 없이 컴파일·동작한다.

## 2. 데이터 흐름

Phase 1의 핵심 흐름은 (A) 단일 에이전트 ReAct 루프, (B) llm 추상 요청 ↔ Anthropic content-block 변환,
(C) 단기 메모리 압축(트리밍/요약), (D) 체크포인트 이어받기 네 갈래다. 각 갈래가 §5 완료 조건의 동작에 어떻게
기여하는지 평문으로 푼다.

### 2-1. 에이전트 ReAct 루프 (A)

호출자가 `agent.Invoke(ctx, in, cfg)`를 부른다. 루프는 그래프 없이 직접 돈다(SPEC §5.8):

```text
입력 메시지 누적 → [runModel] 모델 호출
                       ↓
              shouldContinue(state) 판정
            ┌──────────┼───────────┐
        continue     respond        end
       (tool_calls)  (ResponseFormat) (정상 종료)
            ↓            ↓             ↓
       [runTools]   applyResponseFormat  Result 반환
        도구 실행      structured_response  (Messages,
        ToolMessage    채움                StructuredResponse)
        누적 → 루프      ↓
        선두로 복귀     Result 반환
```

- `runModel`은 (미들웨어 체인을 거쳐) `llm.Client.Chat`을 호출해 AI 메시지를 상태에 누적한다(2-3 미들웨어 결합).
- `shouldContinue`는 `Decision`을 반환한다: 마지막 AI 메시지에 미처리 `tool_calls`가 있으면 `continue`,
  없고 `ResponseFormat`이 지정됐으며 아직 `structured_response`가 비었으면 `respond`, 그 외엔 `end`(SPEC §5.8).
- `runTools`는 마지막 AI 메시지의 `tool_calls`를 `tool.Executor.ExecuteMany`로 디스패치해 `ToolMessage`들을
  상태에 누적하고 루프 선두로 돌아간다(SPEC §5.8·§5.3; `prebuilt.ToolNode`와 동일 실행 의미 — 2-5).
- `applyResponseFormat`은 종료 직전 `ResponseFormat`/스키마로 구조화 출력을 생성해 상태의 `structured_response`에
  저장한다(SPEC §5.8). 이때 llm은 `Structured`(2-2의 구조화 경로)를 쓴다.
- `MaxSteps` 초과 시 루프를 강제 종료한다(무한 루프 방지). `Stream`은 같은 루프를 돌며 `AgentEvent`
  (`Token`/`Update`/`IsTaskComplete`/`RequireUserInput`/`Content`/`Node`)를 채널로 방출한다(SPEC §5.8). 추가 입력
  필요는 `RequireUserInput` 이벤트로만 노출하고 인터럽트/resume은 다루지 않는다(spec.md §4, README §22-4).

이 루프가 미들웨어·체크포인터·단기 메모리·구조화 출력과 결합한 것이 통합 산출물이다(SPEC §5.12).

### 2-2. llm 추상 요청 ↔ Anthropic content-block 변환 (B)

`llm.Client`는 프로바이더 중립 계약이고, Anthropic 어댑터가 그 계약을 SDK 호출로 번역한다. 변환이 어댑터의
핵심 책임이다.

**Chat 경로**:
```text
ChatRequest{Messages([]message.Message OpenAI식), Tools, ToolChoice, ResponseFormat, Model, Temperature}
   │  (어댑터: message.Message → anthropic content-block 메시지)
   │   - system role → MessageNewParams.System
   │   - user/assistant content → text block
   │   - assistant ToolCalls → tool_use block
   │   - tool role(ToolCallID) → user 메시지의 tool_result block(NewToolResultBlock)
   │   - Tools → []anthropic.ToolUnionParam{{OfTool:&ToolParam{Name,Description,InputSchema}}}
   │   - Temperature 등 미지원 샘플링 파라미터: claude-opus-4-8엔 전송 안 함(§5 D4)
   │   - MaxTokens: 필수라 어댑터가 반드시 채움(§5 D5)
   ▼
client.Messages.New(ctx, params)
   │  (어댑터: 응답 content block 배열 → ChatResponse)
   │   - text block → ChatResponse.Message.Content
   │   - tool_use block(block.ID/Name/JSON.Input.Raw()) → ChatResponse.ToolCalls(message.ToolCall)
   │   - resp.StopReason → FinishReason (StopReasonToolUse면 도구 호출 미완)
   │   - usage → TokenUsage
   ▼
ChatResponse{Message, ToolCalls, Usage, FinishReason}
```

- `BindTools(tools []tool.Schema)`는 응답에서 `tool_calls` 파싱을 활성화한 `Client`를 돌려준다. `ParseToolCalls`는
  `ChatResponse`의 tool_use를 `[]message.ToolCall`로 반환한다(SPEC §5.5). 어댑터는 도구 결과를 다음 호출에
  `tool_result` block(user 메시지)으로 회신하고, 직전 어시스턴트 응답은 `resp.ToParam()`으로 히스토리에 append해
  멀티턴 도구 루프를 유지한다.
- **ChatStream 경로**: `client.Messages.NewStreaming` → `stream.Next()`/`stream.Current()`/`message.Accumulate`로
  토큰을 모아 `ChatEvent`(토큰/메시지/완료)로 방출한다(SPEC §5.5).
- **Structured 경로**: `Structured(ctx, req, schema)`는 Anthropic의 `output_config.format`(json_schema) 또는 도구
  강제 사용으로 스키마 강제 출력을 받아 `structured`로 파싱한 값을 돌려준다(README §6식 OpenAI `response_format`이
  아님). README의 `ResponseFormat`(text/json_object/json_schema)은 이 경로로 매핑한다(SPEC §5.5).
- **InitChatModel**: `anthropic:claude-opus-4-8` 형식 식별자를 파싱해 provider=anthropic이면 Anthropic 어댑터를,
  미지원 provider면 에러를 돌려준다(확장 자리만, SPEC §5.5).
- **stub Client**: 같은 `Client` 인터페이스를 구현한 stub이 정해진 응답/도구 호출을 돌려주어, 네트워크 없이
  ReAct 루프·미들웨어·요약·구조화·체크포인트 전체를 검증한다(SPEC §5.5·§5.8·§5.12). 실제 Anthropic 어댑터는
  `ANTHROPIC_API_KEY` 게이트 라이브 스모크로만 검증한다(SPEC §5.6).

### 2-3. 미들웨어 결합 (A의 runModel 내부)

`runModel`은 모델을 직접 부르지 않고 미들웨어 체인으로 감싼다(SPEC §5.9):

```text
agent state ──→ ModelRequest{State(core.State), Model, SystemPrompt}
   │  BeforeModel 훅들: 모델 호출 전 실행, core.State 접근/차단(에러 반환 시 호출 중단)
   │  DynamicPrompt 훅들: 호출마다 SystemPrompt 생성/치환
   │  WrapModelCall 체인: ModelHandler를 감싸 요청/응답 가공
   ▼
ModelHandler(최종) → llm.Client.Chat → ModelResponse
```

- `ModelRequest.Override(model)`는 이 호출에 한해 다른 `llm.Client`를 쓰게 하고, `SetSystemPrompt`는 프롬프트를
  치환하며, `StateValue(key)`는 공유 상태(`core.State`)에서 값을 읽는다. 이들이 에이전트 실행에 반영되는 것을
  stub 모델로 관찰한다(SPEC §5.9).
- 상태 인자가 `core.State`이므로 `middleware`는 `agent`를 import하지 않고, `agent`가 `middleware`를 단방향으로만
  import한다(§1-1).

### 2-4. 단기 메모리 압축 (C)

두 경로가 배타적이지 않게 공존한다(README §11 주석, SPEC §5.2·§5.10):

- **트리밍**: `message.TrimMessages(msgs, opts)`가 strategy(`last` 등)/`start_on`/`end_on`/`max_tokens`로 윈도를
  잘라낸다. 토큰 길이는 `CountTokensApprox`(근사)로 잰다. 잘려나간 메시지는 버려진다(SPEC §5.2).
- **요약**: `prebuilt.ShouldSummarize`가 임계(메시지 수/토큰) 초과를 판정하면 `SummarizationNode`가 누적 대화를
  `llm.Client`로 요약해 `summary` 상태에 저장하고, 요약에 반영된 과거 메시지를 `message.RemoveMessage`+
  `ApplyRemovals`로 제거한다. 다음 호출 때 `InjectSummary`가 그 요약을 SystemMessage로 앞에 재주입한다
  (SPEC §5.10). 트리밍이 윈도를 버리는 데 비해 요약은 버려질 대화를 요약문으로 보존한다.

### 2-5. ToolNode와 라우팅 (prebuilt)

`prebuilt.ToolNode`는 마지막 AI 메시지의 미처리 `tool_calls`를 `tool.Registry`에서 이름으로 찾아 실행하고
`ToolMessage`를 상태에 추가하는 노드 함수다(SPEC §5.10·§5.3). `ToolsCondition`은 미처리 도구 호출이 있으면
`tools`로, 없으면 `END`로 라우팅한다(`HasPendingToolCalls`로 판정). agent 루프의 `runTools`와 ToolNode는 같은
실행 의미(도구 디스패치 → ToolMessage 누적)를 공유하되, 전자는 직접 루프용, 후자는 (Phase 2의) 그래프 노드용
표면이다. Phase 1에서는 둘 다 `tool.Executor`를 통해 디스패치한다.

### 2-6. 체크포인트 이어받기 (D)

```text
Invoke #1 (thread_id=T) → 루프 종료 → checkpoint.SaveState(cfg, state) → InMemorySaver.Put(T, cp)
Invoke #2 (thread_id=T) → checkpoint.LoadState(cfg) → InMemorySaver.Get(T) → 이전 메시지 복원 후 루프 시작
```

`ThreadIDFromConfig(cfg)`가 `config.GetThreadID`로 thread_id를 뽑고, `WithCheckpointer`로 만든 에이전트가
별도 `Invoke` 호출 간에 같은 thread_id의 이전 메시지를 이어받는다(SPEC §5.11). `InMemorySaver`는
`Put`/`Get`/`List`(히스토리)/`DeleteThread`로 스레드 상태를 영속한다. 스냅샷 타입은 `core.StateSnapshot`이라
`graph` 역참조가 없다(§1-1).

### 2-7. structured 흐름

`BuildSchema[T]`가 Go 구조체 태그(`json`/`description`/`EnumField` 제약)에서 `structured.Schema`(JSON 스키마 +
메타)를 만든다. `ParseStructured[T](raw)`가 raw JSON을 `T`로 파싱하고 `Validate(raw, schema)`가 스키마 검증한다.
표준 스키마 6종은 호출자(라우팅·평가·에이전트 응답 포맷)가 그대로 가져다 쓴다(SPEC §5.4). 이 스키마들이 llm의
`Structured`/`ResponseFormat`과 agent의 `applyResponseFormat`이 강제하는 출력 형태를 정의한다.

## 3. 인터페이스

경계를 가로지르는 계약을 패키지별로 정리한다. README의 제안형 시그니처를 Go 관례로 따르되, Phase 2 타입을
가리키는 자리는 §1-3대로 `core`/로컬 타입으로 치환한다. 내부 helper는 범위 밖이다.

**message (README §2, SPEC §5.2)**: `Role`/`Message`/`ToolCall`/`ToolResult`/식별자 타입. 생성자
(`NewSystemMessage`/`NewUserMessage`/`NewAssistantMessage`/`NewToolMessage`/`NewAssistantToolCalls`/`WithName`).
조회(`LastMessage`/`LastAIMessage`/`HasToolCalls`/`ExtractToolCalls`/`FilterByName`). 리듀서(`AddMessages`/
`RemoveMessage`/`RemoveAllSentinel`/`ApplyRemovals`/`TrimMessages`/`CountTokensApprox`). 출력(`PrettyPrint`/
`PrettyPrintMessages`).

**structured (README §6, SPEC §5.4)**: `Schema`/`Validator`/`FieldOption`. `BuildSchema[T]() Schema`/
`ParseStructured[T](raw string) (T, error)`/`Validate(raw string, s Schema) error`/`EnumField(name string, values
...string) FieldOption`. 표준 스키마: `BinaryScore`/`RouterChoice[T]`/`AgentStatus`/`Plan`/`ConversationalResponse`/
`PlannerResult`.

**tool (README §5, SPEC §5.3)**: `Tool` 인터페이스(`Name`/`Description`/`Schema`/`Execute(ctx, Input, Runtime)`),
`Schema`/`Parameter`/`Input`/`Result`, `Registry`(`Register`/`RegisterMany`/`Get`/`List`/`Schemas`), `Executor`
(`Execute`/`ExecuteMany`/`ExecuteWithTimeout`/`BuildToolMessage`/`UnknownToolError`), `ValidateArgs`/`DecodeArgs[T]`,
`Runtime`(`State() any`/`ToolCallID() string`/`Config() config.RunConfig`/`Store() Store`/`Emit(Event)`), 좁은
인터페이스 `Store`(Get/Put/Search)와 `Event` 타입(tool 패키지 소유), `FromFunc(name, desc, fn) Tool`/
`WithArgsSchema[T](name, desc, fn) Tool`. **임베딩·store/trace 구체 타입은 노출하지 않는다.**

**llm (README §4, SPEC §5.5·§5.6)**: `Client` 인터페이스(`Chat`/`ChatStream`/`Structured`/`BindTools(tools
[]tool.Schema) Client`/`ParseToolCalls(resp) []message.ToolCall`/`WithModel(name) Client`/`ModelName() string`),
`ChatRequest`(`Messages`/`Tools`/`ToolChoice`/`ResponseFormat`/`Model`/`Temperature`)/`ChatResponse`(`Message`/
`ToolCalls`/`Usage`/`FinishReason`)/`ChatEvent`/`ResponseFormat`(text/json_object/json_schema)/`TokenUsage`,
`InitChatModel(spec string, opts ...Option) (Client, error)`. **임베딩 타입·팩토리(`EmbeddingClient`/
`InitEmbeddings`/`Embed`/`EmbedQuery`)는 만들지 않는다(spec.md §4).** Anthropic 어댑터는 `Client` 구현이며
SDK(`github.com/anthropics/anthropic-sdk-go`)를 내부적으로만 쓴다(외부에 SDK 타입을 노출하지 않는다).

**prompt (README §3, SPEC §5.7)**: `PromptTemplate`/`MessagesPlaceholder`/`MessageSpec`/`Chain`. `FromMessages`/
`FromTemplate`/`(PromptTemplate) Format(vars) ([]Message, error)`/`Pipe(template, model llm.Client) Chain`/
`(Chain) Invoke(ctx, vars) (any, error)`/`(Chain) WithStructuredOutput(schema structured.Schema) Chain`.

**prebuilt (README §8, SPEC §5.10)**: 로컬 노드 함수 타입(`func(ctx, core.State) (core.StateUpdate, error)`).
`NewToolNode(reg tool.Registry) NodeFunc`/`ToolsCondition(ctx, st core.State) string`/`HasPendingToolCalls(st
core.State) bool`/`NewSummarizationNode(model llm.Client, opts SummarizeOptions) NodeFunc`/`ShouldSummarize(st
core.State, opts SummarizeOptions) bool`/`InjectSummary(msgs []message.Message, summary string) []message.Message`/
`SummarizeOptions`(MaxMessages/MaxTokens/KeepLast/SummaryKey). README의 `graph.NodeFunc`/`graph.State`를 `core`
기반으로 치환한 것이 유일한 차이다(§1-3).

**middleware (README §10, SPEC §5.9)**: `Middleware` 인터페이스, `ModelRequest`(`State core.State`/`Model`/
`SystemPrompt`)/`ModelResponse`/`ModelHandler(ctx, ModelRequest) (ModelResponse, error)`/`Runtime`. `WrapModelCall`/
`BeforeModel(name, fn func(ctx, core.State, Runtime) error)`/`DynamicPrompt`. `(ModelRequest) Override(model
llm.Client)`/`SetSystemPrompt(p string)`/`StateValue(key string) any`. 상태 인자는 모두 `core.State`다.

**checkpoint (README §11, SPEC §5.11)**: `Checkpointer` 인터페이스, `InMemorySaver`, `Checkpoint`(ThreadID/Values/
Next/Metadata/CreatedAt/ParentConfig), `StateSnapshot = core.StateSnapshot`. `Get`/`Put`/`List`/`DeleteThread`,
`ThreadIDFromConfig(cfg config.RunConfig) string`/`LoadState(ctx, cfg) (core.State, bool, error)`/`SaveState(ctx,
cfg, st core.State) error`. README가 `graph.State`라 적은 자리를 `core.State`로 둔다.

**agent (README §9, SPEC §5.8·§5.12)**: `Agent`/`Config`(Model/Tools/SystemPrompt/Middleware/Checkpointer/Store/
ResponseFormat/MaxSteps)/`State`(MessagesState 확장 + StructuredResponse)/`Result`(Messages/StructuredResponse)/
`Input`/`Decision`(continue/respond/end)/`AgentEvent`(IsTaskComplete/RequireUserInput/Content/Node/Token/Update).
`Create(model llm.Client, tools []tool.Tool, opts ...Option) (*Agent, error)`(옵션 `WithSystemPrompt`/
`WithMiddleware`/`WithCheckpointer`/`WithStore`/`WithResponseFormat`/`WithMaxSteps`), `Invoke(ctx, in, cfg)
(Result, error)`/`Stream(ctx, in, cfg, mode core.Mode) (<-chan AgentEvent, error)`/`GetState(cfg) (core.StateSnapshot,
error)`. 내부 루프 `runModel`/`runTools`/`shouldContinue`/`applyResponseFormat`는 비공개다.

**모듈 빌드/import 계약 (SPEC §5.1)**: 모듈 루트에서 `go build ./...`/`go vet ./...`가 Anthropic SDK 의존이 추가된
상태로 오류 없이 끝난다. Phase 1 패키지가 Phase 2 패키지를 import하지 않고(graph/command/streaming 미참조),
`tool`이 store/trace를 import하지 않음을 `go list -deps`로 외부 검증할 수 있다.

## 4. 영향 범위

이 feature는 Phase 0 leaf 위에 9개 신규 패키지와 1개 외부 의존을 추가하는 **그린필드 확장**이다. 변경 대상의
직접·간접 의존을 탐색한 결과, Phase 1이 건드릴 기존 호출자·구현체·마이그레이션 대상이 없다.

- **신규 생성(패키지 9개)**: `message`/`structured`/`tool`/`llm`/`prompt`/`prebuilt`/`middleware`/`checkpoint`/
  `agent`. 각 디렉토리는 디렉토리명과 같은 패키지명을 갖는다.
- **신규 외부 의존**: `github.com/anthropics/anthropic-sdk-go`(go.mod에 추가). Anthropic 어댑터 내부에서만 쓰고
  공개 API에 SDK 타입을 노출하지 않는다(SPEC §5.6).
- **기존 자산 수정**: `core`/`config`(Phase 0)는 수정 대상이 아니다. Phase 1은 이들을 import만 한다. 단,
  `go.mod`에 SDK require 라인이 추가된다(코드 수정 아님, 의존 선언).
- **하위 호환·마이그레이션**: 해당 없음. 깨질 기존 호출자·저장 데이터·외부 contract가 없는 신규 패키지 추가다.
- **이후 Phase에 대한 영향**: Phase 2(`graph`/`command`/`streaming`)가 `prebuilt`의 로컬 노드 타입을
  `graph.NodeFunc`로 흡수/정합시키고, `agent.Stream`의 `core.Mode`를 `streaming.Mode` alias로 재노출한다. 이는
  Phase 1이 깨뜨리는 계약이 아니라 의도된 토대 제공이며, Phase 1은 그 alias 없이 닫힌다(§1-3).
- **store/trace 영향**: Phase 5 `store.Store`·Phase 7 `trace`가 `tool.Store`/`tool.Event` 좁은 인터페이스를
  충족·수신하면 되도록 경계만 만든다. Phase 1엔 그 구현이 없다(§28-1 규칙2).

## 5. Decision Points

### D1. graph 없는 노드/상태/모드 타입의 표현과 소유 위치

- 고려한 옵션: (a) `prebuilt`가 로컬 `NodeFunc`/노드 입력 타입을 소유하고 상태는 `core.State`, 모드는
  `core.Mode`, 미들웨어 상태 인자도 `core.State`로 둔다. (b) `core`에 `NodeFunc` 류 함수 타입까지 끌어내려
  공용화한다. (c) Phase 2가 생길 때까지 prebuilt/agent 일부 시그니처를 비워 둔다.
- 트레이드오프: (b)는 한 곳에 모이지만 `core`는 "데이터 원시 타입"의 leaf인데 동작 시그니처(노드 함수)를 넣으면
  Phase 0의 leaf 책임이 흐려지고, 함수 타입은 import 순환을 만들지 않으므로 굳이 core로 내릴 이득이 없다.
  (c)는 SPEC §5.10·§5.12의 "Phase 1만으로 동작·검증"을 깨뜨린다. (a)는 README §8/§9/§10이 가리키는 Phase 2
  타입을 `core` 소유 타입으로 1:1 치환하고, 노드 함수 타입만 소비처인 `prebuilt`에 로컬로 두어 경계가 가장 좁다.
- 채택: (a). 노드 함수 타입은 `prebuilt` 로컬, 상태는 `core.State`/`core.StateUpdate`, 모드는 `core.Mode`,
  미들웨어 상태 인자는 `core.State`. Phase 2가 `graph.NodeFunc`/`graph.State` alias로 흡수·정합한다.
- 근거: spec.md §3(Phase 2 미참조, README 시그니처를 core/로컬 타입으로 충족), README §8/§9/§10/§28-1 규칙1,
  SPEC §5.8·§5.9·§5.10·§5.12. 로컬 타입의 정확한 이름·시그니처 형태는 결과에 영향이 작아 implementer 재량에 둔다.

### D2. agent ReAct 루프 — 그래프 없는 직접 루프

- 고려한 옵션: (a) `runModel`/`runTools`/`shouldContinue` 직접 루프로 구현하고 `Decision`(continue/respond/end)으로
  분기. (b) Phase 1 안에 미니 그래프 실행기를 따로 만들어 chatbot→tools 노드를 컴파일.
- 트레이드오프: (b)는 README의 "그래프로 컴파일된다"는 표현에 형식적으로 가깝지만, Phase 2 `graph`와 중복
  구현이 되어 나중에 버려지거나 충돌하고, spec.md §3이 "직접 루프로 충족"을 명시했다. (a)는 코드가 단순하고
  stub 모델로 종단 검증이 쉬우며 Phase 2 도입 시 `agent`가 `graph` 위로 재배치될 여지를 막지 않는다.
- 채택: (a). 직접 루프 + `Decision` enum. `MaxSteps`로 무한 루프를 막고 `Stream`은 같은 루프에서 `AgentEvent`를
  방출한다.
- 근거: spec.md §3, README §9(내부 루프 함수 명시), SPEC §5.8·§5.12.

### D3. 구조화 출력 매핑 — Anthropic output_config/도구강제, OpenAI response_format 비사용

- 고려한 옵션: (a) `llm.ResponseFormat`/`Structured`를 Anthropic `output_config.format`(json_schema) 또는 도구
  강제 사용으로 매핑. (b) OpenAI식 `response_format`을 그대로 SDK에 전달.
- 트레이드오프: (b)는 README 표기에 글자 그대로 가깝지만 Anthropic SDK는 OpenAI식 `response_format`을 받지 않아
  런타임 실패한다(프롬프트 제공 사실). (a)는 추상 계약(`ResponseFormat` text/json_object/json_schema,
  `structured.Schema`)은 유지하되 어댑터 내부에서 Anthropic 표면으로 번역해, 계약을 깨지 않고 실제 호출이
  성립한다.
- 채택: (a). `ResponseFormat`/`Structured`/`structured.Schema`는 프로바이더 중립 계약으로 두고, Anthropic 어댑터가
  `output_config.format`(또는 도구 강제)로 번역한다.
- 근거: spec.md §3(ResponseFormat→output_config 매핑), README §4·§6, 프롬프트의 SDK 매핑 사실, SPEC §5.5.
  json_schema 강제와 도구 강제 중 어느 쪽을 기본 경로로 쓸지는 어댑터 구현 디테일이라 implementer 재량에 두되,
  스키마에 맞는 값을 돌려준다는 외부 계약(SPEC §5.5)은 고정한다.

### D4. claude-opus-4-8 샘플링 파라미터 — 미지원 파라미터 비전송

- 고려한 옵션: (a) `ChatRequest.Temperature` 등 추상 샘플링 필드를 Anthropic 어댑터가 `claude-opus-4-8` 대상
  으로는 SDK에 전송하지 않는다(요청에서 누락). (b) 추상 필드를 항상 전송한다. (c) 추상 필드 자체를 `llm`에서
  제거한다.
- 트레이드오프: (b)는 `claude-opus-4-8`이 `temperature`/`top_p`/`top_k`를 받으면 400을 반환하므로 라이브 호출이
  실패한다(프롬프트 제공 사실). (c)는 README §4가 `ChatRequest.Temperature`를 두므로 계약과 어긋나고 다른
  모델 확장 자리를 없앤다. (a)는 추상 필드는 유지하되 어댑터가 모델별로 수용 가능한 파라미터만 매핑해, 계약을
  지키면서 라이브 호출을 성립시킨다.
- 채택: (a). 어댑터는 모델이 거부하는 샘플링 파라미터를 강제 전송하지 않는다. `claude-opus-4-8`에는
  `temperature`/`top_p`/`top_k`를 생략한다.
- 근거: spec.md §3(미지원 샘플링 파라미터 강제 전송 금지), 프롬프트의 SDK 매핑 사실, README §4, SPEC §5.6.
  값이 들어와도 무시하는 보수적 처리이며, 모델별 수용 파라미터 판정 위치는 결과 영향이 작아 implementer 재량에 둔다.

### D5. max_tokens 필수 처리 — 어댑터가 기본값 보장

- 고려한 옵션: (a) `ChatRequest`에 명시 max_tokens가 없으면 Anthropic 어댑터가 모델 한도 내 합리적 기본값으로
  채워 전송. (b) max_tokens 미지정 시 에러를 반환해 호출자에게 강제. (c) `ChatRequest`에 max_tokens 필드를 추가.
- 트레이드오프: Anthropic은 `max_tokens`가 필수라 미전송 시 400이다(프롬프트 제공 사실). (b)는 stub·라이브 양쪽
  호출자가 매번 값을 채워야 해 ReAct 루프 코드가 번잡해진다. (c)는 README §4 `ChatRequest` 표면을 늘리며 굳이
  필요치 않다(추상 계약은 max_tokens를 안 드러낸다). (a)는 호출자 부담 없이 라이브 호출을 성립시키고, 호출자가
  값을 주면 그대로 쓴다.
- 채택: (a). 어댑터가 max_tokens 누락 시 기본값을 채운다. 기본값의 구체 수치는 결과에 영향이 작아 implementer
  재량에 두되, "max_tokens 미전송으로 인한 400이 발생하지 않는다"는 동작은 고정한다.
- 근거: 프롬프트의 SDK 매핑 사실(max_tokens 필수), spec.md §3, SPEC §5.6.

### D6. tool.Runtime의 Store/Event — tool 패키지 내 좁은 인터페이스

- 고려한 옵션: (a) `Runtime.Store()`가 `tool.Store`(Get/Put/Search) 좁은 인터페이스를, `Runtime.Emit`이
  `tool.Event` 타입을 다룬다(tool이 store/trace 미참조). (b) `Runtime`이 `store.Store`/`trace.Event` 구체 타입을
  직접 다룬다.
- 트레이드오프: (b)는 `tool → store`(Phase 5)·`tool → trace`(Phase 7) 역방향 import와 Phase 역전을 만들고,
  그 구현이 없는 Phase 1에서 `tool`이 컴파일되지 않는다(README §28-1의 확인된 순환). (a)는 `tool`이 자체
  소유한 최소 타입만 노출하고 상위 패키지가 이를 충족·주입해, store/trace 구현 없이 Phase 1이 컴파일된다.
- 채택: (a). `Store`(Get/Put/Search)와 `Event`를 `tool` 패키지에 선언하고 `tool`은 상위 패키지를 import하지
  않는다. `Runtime.Config()`는 leaf인 `config.RunConfig`를 구체 타입 그대로 받는다(인터페이스화 불필요).
- 근거: spec.md §3, README §5·§28-1 규칙2, SPEC §5.1·§5.3.

### D7. checkpoint/agent의 상태 스냅샷 타입 — core.StateSnapshot 직참조

- 고려한 옵션: (a) `checkpoint.StateSnapshot`/`agent.GetState`가 `core.StateSnapshot`을 쓰고, `LoadState`/
  `SaveState`가 `core.State`를 다룬다. (b) README 표기대로 `graph.StateSnapshot`/`graph.State`를 참조한다.
- 트레이드오프: (b)는 `graph`(Phase 2)가 없는 Phase 1에서 컴파일 불가이고, 있더라도 `checkpoint`/`agent`(Phase 1)
  → `graph`(Phase 2) 역방향 import와 Phase 역전을 만든다(README §28-1 규칙1). (a)는 `core`가 `StateSnapshot`/
  `State`를 소유하므로 Phase 1이 `graph` 없이 닫히고, Phase 2가 `graph.StateSnapshot = core.StateSnapshot` alias로
  같은 타입을 재노출한다.
- 채택: (a). `core.StateSnapshot`/`core.State` 직참조. Phase 0 산출물(`core/core.go`)이 이미 이 타입을
  제공하므로 추가 정의 없이 참조만 한다.
- 근거: README §9·§11·§28-1 규칙1, Phase 0 `core/core.go`(확인된 구현), SPEC §5.8·§5.11.

### D8. 토큰 카운트 — 근사형, 외부 의존 없음

- 고려한 옵션: (a) `CountTokensApprox`를 표준 라이브러리 기반 근사(문자/단어 휴리스틱)로 구현. (b) `tiktoken-go`
  등 외부 토크나이저 도입.
- 트레이드오프: (b)는 정밀하지만 외부 의존이 늘고, spec.md §3·README §2가 "근사형으로 충분(tiktoken 정밀
  불필요)"을 명시한다. 트리밍/요약 임계 판정은 근사값으로 충분하다. (a)는 외부 의존 없이 충분한 정확도를 낸다.
- 채택: (a) 근사형. Anthropic SDK 외 추가 토크나이저 의존을 들이지 않는다.
- 근거: spec.md §3, README §2·§8, SPEC §5.2·§5.10. 근사 알고리즘의 구체 계수는 결과 영향이 작아 implementer
  재량에 둔다.

### D9. 검증 전략 — stub Client + 게이트 라이브 스모크

- 고려한 옵션: (a) 런타임 동작(루프·디스패치·미들웨어·트리밍/요약·구조화·체크포인트)은 stub `llm.Client`로,
  Anthropic 어댑터는 `ANTHROPIC_API_KEY` 게이트 라이브 스모크(없으면 skip)로 검증. (b) 모든 검증을 라이브
  호출로. (c) Anthropic 어댑터도 모킹으로만.
- 트레이드오프: (b)는 CI가 키 없이 통과 못 하고 비용·불안정이 크다. (c)는 SDK 매핑(message↔content-block,
  StopReason, tool_use)이 실제 와이어에서 맞는지 확인할 수 없다. (a)는 결정 로직을 네트워크 없이 빠르게 검증하고
  (SPEC §5.2·§5.4·§5.5·§5.8~§5.12), 어댑터의 실제 매핑만 키가 있을 때 라이브로 확인한다(SPEC §5.6, 키 없으면
  skip → CI 통과).
- 채택: (a). `Client` 인터페이스 경계 덕에 stub 주입이 가능하고, 라이브 스모크는 키 게이트로 skip 가능하게 둔다.
- 근거: spec.md §3·§5.6, 이미 확정된 결정, SPEC §5.1·§5.5·§5.6·§5.12.
