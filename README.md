# langgraph-go

LangGraph / LangChain 에이전트 런타임을 Go로 구현하기 위한 기능 명세다. 그래프 실행 엔진, 에이전트 루프, 도구 실행,
메시지 모델, 구조화 출력, 메모리/체크포인트, 스트리밍, MCP, A2A, 그리고 벡터스토어·웹검색·DB·스토리지 연동까지
**라이브러리**로 제공한다. 멀티에이전트·RAG·오케스트레이션은 이 primitive 위에 다운스트림이 직접 구현하는 응용
계층이며, 본 문서는 그 설계 가이드를 함께 담는다(§1).

문서는 "무엇을 만들 것인가"만 담는다. 각 패키지는 담당 책임 한 줄과, 구현할 타입·함수·메소드 목록으로 기술한다.
시그니처는 Go 관례로 다듬은 제안형이며, 실제 구현 시 인터페이스 경계만 유지하면 된다.

---

## 1. 패키지 구조

각 패키지는 모듈 루트 하위의 **공개 패키지**다. 외부 프로젝트에서 `<module-path>/graph`, `<module-path>/agent`
형태로 import해서 사용한다(`<module-path>`는 go.mod의 모듈 경로). 내부 전용으로 감추는 패키지는 두지 않는다.

```text
<module-root>/
# ── 라이브러리 (다운스트림이 import해서 사용) ──
├── core           공유 원시 타입(State, StateUpdate, Mode, StateSnapshot) — config에만 의존하는 leaf, 순환 import 차단용(§28-1)
├── message        메시지 모델, tool_calls, 리듀서(add_messages/remove/trim)
├── prompt         프롬프트 템플릿, 메시지 플레이스홀더, 체인(파이프)
├── llm            챗 모델 추상화, 도구 바인딩, 구조화/JSON 출력, 임베딩, 스트리밍
├── tool           Tool 인터페이스, 스키마, 레지스트리, 실행기, ToolRuntime 주입
├── structured     Go 타입 → JSON 스키마, 구조화 출력 파싱/검증
├── graph          StateGraph 빌더/컴파일/실행, 노드·엣지·리듀서, 입출력 스키마
│   └── command    Command(goto/update/parent), Send
├── prebuilt       ToolNode, tools_condition, 요약 노드
├── agent          create_agent(ReAct 루프) + 미들웨어/체크포인터/스토어/응답포맷
├── middleware     wrap_model_call, before_model, dynamic_prompt
├── checkpoint     Checkpointer, InMemorySaver, 스레드 상태/히스토리
├── store          장기 메모리 스토어(put/get/search), 네임스페이스, 임베딩 인덱스
├── streaming      스트림 모드(values/messages/updates), subgraphs
├── document       문서 로더(PDF/DOCX/Web), 텍스트 분할
├── vectorstore    벡터스토어(인메모리/Chroma/Supabase-pgvector), 유사도 검색, retriever
├── search         웹 검색, 웹 페이지 로더
├── database       관계형/벡터 DB 클라이언트(문서·웹콘텐츠 테이블)
├── storage        외부 파일 스토리지 클라이언트, storage_ref 추상화
├── mcp            MCP 클라이언트/서버(stdio/streamable_http), 멀티서버, 도구·프롬프트 로딩·노출
├── a2a            A2A 타입/서버/클라이언트, 태스크 상태 전이, 아티팩트
├── config         실행 설정(RunConfig 등 무의존 leaf — §28-1 규칙3), 모델/서버/에이전트 설정, thread_id/user_id
├── trace          실행 추적, pretty-print, mermaid 출력 (선택 모듈, §25)
│
# ── 응용 계층 (라이브러리 아님 — 다운스트림이 위 primitive로 직접 구현) ──
# 전용 패키지가 아니라 StateGraph·Command·Send·create_agent를 조합해 만드는 패턴이다.
# 아래 §14·§17·§23은 "무엇을 어떻게 조립하는가"의 설계 가이드이지 import 대상 API가 아니다.
└── (다운스트림 구현)
    ├── rag            RAG 상태/노드/엣지, 관련성·환각 평가, 검색/인덱싱 라우팅 (§17)
    ├── multiagent     supervisor/router, handoff, worker-as-node/subgraph, network, planner (§14)
    └── orchestrator   인텐트 분석, 플랜 생성/실행, 원격 에이전트 호출, 결과 통합 (§23)
```

구현 단계(§26)는 의존 순서를 따른다. `config`는 무의존 최하위 leaf, `core`는 `config`에만 의존하는 leaf로, 둘 다 가장 먼저
존재한다(Phase 1 이전). `core`가 `config`에 단방향으로만 의존하므로 순환은 없다(§28-1 규칙1). `a2a` 등 상위 라이브러리
패키지는 하위 핵심(`message`/`llm`/`tool`/`graph`/`agent`)에 의존한다.

응용 계층(`rag`/`multiagent`/`orchestrator`)은 라이브러리 완성 후 다운스트림이 primitive로 직접 구현한다. 이들은
전용 패키지가 따로 있는 게 아니라 `graph`/`command`/`agent`를 조합해 만드는 응용 패턴이라, 라이브러리가 완성형을
고정하면 응용 설계의 자유도가 사라진다. 그래서 의도적으로 분리하고, 이 문서는 그 구현 설계 가이드(§14·§17·§23)를
함께 제공한다.

시그니처에 등장하는 `graph.State`, `tool.Schema`, `message.ToolCall`, `config.RunConfig` 등의 한정자는 각 패키지의
패키지명(import 시 마지막 경로 요소)을 가리킨다.

---

## 2. message

메시지 모델과 누적 리듀서를 담당한다.

### 타입

- `Role` (`system` / `user` / `assistant` / `tool`)
- `Message` (Role, Content, Name, ID, ToolCalls, ToolCallID)
- `ToolCall` (ID, Name, Args(`json.RawMessage`))
- `ToolResult` (ToolCallID, Name, Content, IsError)
- `MessageID`, `ToolCallID`, `MessageName`

### 생성

- `NewSystemMessage(content string) Message`
- `NewUserMessage(content string) Message`
- `NewAssistantMessage(content string) Message`
- `NewToolMessage(toolCallID, name, content string) Message`
- `NewAssistantToolCalls(calls []ToolCall) Message`
- `WithName(m Message, name string) Message`

### 조회·조작

- `LastMessage(msgs []Message) (Message, bool)`
- `LastAIMessage(msgs []Message) (Message, bool)`
- `HasToolCalls(m Message) bool`
- `ExtractToolCalls(m Message) []ToolCall`
- `FilterByName(msgs []Message, name string) []Message`

### 리듀서 (그래프 상태 병합)

- `AddMessages(base, incoming []Message) []Message` — 동일 ID는 upsert, 신규는 append
- `RemoveMessage(id MessageID) Message` — 삭제 마커
- `RemoveAllSentinel` — 전체 삭제 센티널 ID 상수
- `ApplyRemovals(msgs []Message) []Message` — 삭제 마커 반영
- `TrimMessages(msgs []Message, opts TrimOptions) []Message` — strategy/start_on/end_on/max_tokens
- `CountTokensApprox(msgs []Message) int`

> `TrimMessages`/`CountTokensApprox`는 단기 메모리(윈도 트리밍)에서 쓰는 기능이다. 대응 동작은
> `trim_messages(strategy="last", token_counter=count_tokens_approximately, start_on, end_on, max_tokens)`와
> `RemoveMessage`이며, 토큰 카운터는 근사형(`count_tokens_approximately`)으로 충분해 tiktoken 정밀 카운트는
> 필수가 아니다.

### 출력

- `PrettyPrint(m Message) string`
- `PrettyPrintMessages(msgs []Message) string`

---

## 3. prompt

프롬프트 템플릿과 체인 합성을 담당한다.

### 타입

- `PromptTemplate` (역할별 템플릿 + 플레이스홀더)
- `MessagesPlaceholder` (변수명으로 메시지 리스트 삽입)
- `MessageSpec` (Role + 템플릿 텍스트 쌍 또는 `MessagesPlaceholder` — `FromMessages` 입력 단위)
- `Chain` (Template → Model → 파서로 이어지는 호출 단위)

### 함수·메소드

- `FromMessages(specs []MessageSpec) PromptTemplate`
- `FromTemplate(text string) PromptTemplate`
- `(PromptTemplate) Format(vars map[string]any) ([]Message, error)`
- `Pipe(template PromptTemplate, model llm.Client) Chain`
- `(Chain) Invoke(ctx, vars map[string]any) (any, error)`
- `(Chain) WithStructuredOutput(schema structured.Schema) Chain`

---

## 4. llm

챗 모델 호출, 도구 바인딩, 구조화/JSON 출력, 임베딩, 스트리밍을 담당한다.

### 타입

- `Client` (인터페이스)
- `ChatRequest` (Messages, Tools, ToolChoice, ResponseFormat, Model, Temperature)
- `ChatResponse` (Message, ToolCalls, Usage, FinishReason)
- `ChatEvent` (토큰/메시지/완료)
- `ModelConfig`, `TokenUsage`
- `ResponseFormat` (`text` / `json_object` / `json_schema`)
- `EmbeddingClient`

### 호출

- `Chat(ctx, req ChatRequest) (ChatResponse, error)`
- `ChatStream(ctx, req ChatRequest) (<-chan ChatEvent, error)`
- `Structured(ctx, req ChatRequest, schema structured.Schema) (any, error)`

### 도구 바인딩

- `BindTools(tools []tool.Schema) Client` — 응답에서 `tool_calls` 파싱 활성화
- `ParseToolCalls(resp ChatResponse) []message.ToolCall`

### 모델·임베딩 팩토리

- `InitChatModel(spec string, opts ...Option) (Client, error)` — `openai:gpt-4o` 형식 식별자
- `(Client) WithModel(name string) Client`
- `(Client) ModelName() string`
- `InitEmbeddings(spec string) (EmbeddingClient, error)`
- `(EmbeddingClient) Embed(ctx, texts []string) ([][]float32, error)`
- `(EmbeddingClient) EmbedQuery(ctx, text string) ([]float32, error)`

> 라우팅 패턴 두 가지를 모두 지원한다. (1) TypedDict/구조체를 도구로 바인딩(`BindTools`)해 `tool_calls[0].Args`에서 결정값을
> 읽는 방식, (2) `Structured`로 스키마 강제 출력을 받는 방식. 또한 원시 `json_object` 응답 포맷과 함수호출 디스패치 루프를
> 직접 구동하는 경로도 필요하다.

> 프로바이더 분담: 챗은 Anthropic(Claude)을 사용하며 기본 모델은 `claude-opus-4-8`이다. 임베딩은 Anthropic에
> 대응 API가 없어 OpenAI를 사용한다. `InitChatModel`의 스펙 형식(`provider:model`, 예: `anthropic:claude-opus-4-8`)은
> 다른 프로바이더로 확장할 자리만 남겨둔다(현 단계에서 OpenAI 챗·gemini는 구현 범위 밖). 임베딩 팩토리
> (`InitEmbeddings`/`EmbeddingClient`)는 Phase 1에서 구현하지 않고, 첫 소비처인 `vectorstore`와 함께 Phase 3에서
> 구현한다(§26).

---

## 5. tool

도구 정의·스키마·레지스트리·실행과 런타임 주입을 담당한다.

### 타입

- `Tool` (인터페이스: `Name`, `Description`, `Schema`, `Execute`)
- `Schema` (Name, Description, Parameters(JSON 스키마))
- `Parameter` (Name, Type, Description, Required)
- `Registry`
- `Executor`
- `Runtime` (도구 실행 컨텍스트)
- `Store` (`tool` 패키지 내 최소 인터페이스: Get/Put/Search — `store.Store`가 충족, §28-1 규칙2)
- `Event` (`tool` 패키지 내 실행 이벤트 타입 — `trace`가 수신, §28-1 규칙2)
- `Input` (`json.RawMessage`)
- `Result`

### Tool 인터페이스

- `Name() string`
- `Description() string`
- `Schema() Schema`
- `Execute(ctx, input Input, rt Runtime) (Result, error)`

### Registry

- `Register(t Tool) error`
- `RegisterMany(ts ...Tool) error`
- `Get(name string) (Tool, bool)`
- `List() []Tool`
- `Schemas() []Schema`

### Executor

- `Execute(ctx, call message.ToolCall, rt Runtime) (Result, error)`
- `ExecuteMany(ctx, calls []message.ToolCall, rt Runtime) ([]message.Message, error)`
- `ExecuteWithTimeout(ctx, call message.ToolCall, rt Runtime, d time.Duration) (Result, error)`
- `BuildToolMessage(call message.ToolCall, res Result) message.Message`
- `UnknownToolError(name string) error`

### 인자 검증·디코딩

- `ValidateArgs(s Schema, args json.RawMessage) error`
- `DecodeArgs[T any](args json.RawMessage) (T, error)`

### Runtime 주입 (도구 함수가 상태/식별자/스토어에 접근)

- `(Runtime) State() any`
- `(Runtime) ToolCallID() string`
- `(Runtime) Config() config.RunConfig` — `config`는 leaf라 단방향 의존(§28-1, §24)
- `(Runtime) Store() Store` — `tool` 패키지 내 최소 인터페이스(Get/Put/Search). `store.Store`가 충족하며 `tool`은 `store`를 import하지 않는다
- `(Runtime) Emit(ev Event)` — `tool` 패키지 내 Event 타입. `trace`가 이를 수신해 기록(§28-1)

### 생성 헬퍼 (Go 함수 → Tool)

- `FromFunc(name, desc string, fn any) Tool` — 구조체 입력으로 스키마 자동 도출. 파라미터 설명은 입력 구조체 필드
  태그(`json`/`description`)에서 추출한다(파이썬 `Field(description=...)`/`parse_docstring` 대응)
- `WithArgsSchema[T any](name, desc string, fn func(ctx, T, Runtime) (Result, error)) Tool` — 입력 스키마 타입 `T`를
  명시 지정(파이썬 `@tool(args_schema=...)` 대응)

---

## 6. structured

Go 타입과 JSON 스키마 사이 변환, 구조화 출력 파싱/검증을 담당한다.

### 타입

- `Schema` (JSON 스키마 + 메타)
- `Validator`
- `FieldOption` (`BuildSchema`/필드 빌더 옵션 — `EnumField` 등이 반환, 제약·열거값 부착)

### 함수

- `BuildSchema[T any]() Schema` — 구조체 태그 기반 스키마 생성
- `ParseStructured[T any](raw string) (T, error)`
- `Validate(raw string, s Schema) error`
- `EnumField(name string, values ...string) FieldOption` — `binary_score`, `next` 같은 제약 필드

### 표준 출력 스키마 (런타임 제공)

- `BinaryScore` (`binary_score: yes|no`) — 관련성·환각 평가용
- `RouterChoice[T]` (`next: T`) — 다음 노드 결정용
- `AgentStatus` (`status: input_required|completed|error`, `message: string`) — 에이전트 응답 포맷
- `Plan` (`steps: []string`), `ConversationalResponse` (`response: string`)
- `PlannerResult` (`Action` 판별 필드 + `Plan`/`Response` 중 채워진 쪽 — Go엔 union이 없어 태그 구조체로 표현) — 계획/재계획용

---

## 7. graph

상태 그래프의 빌드·컴파일·실행을 담당한다.

### 타입

- `StateSchema` (필드별 리듀서 등록)
- `Builder`
- `Compiled`
- `Node`, `NodeFunc(ctx, State) (any, error)` — 반환은 `StateUpdate` 또는 `command.Command`
- `Edge`, `ConditionalRouter(ctx, State) string`
- `State`, `StateUpdate` — `core` 패키지 타입의 alias(`type State = core.State`). 다른 섹션의 `graph.State` 표기는
  그대로 유효하며, `command`·`streaming`만 `core`를 직접 참조해 순환을 끊는다(§28-1).
- `Mode` — 스트림 모드도 `core` 소유(`streaming.Mode`/`graph` 시그니처는 `core.Mode`를 가리킨다). `graph`/`agent`가
  `streaming`을 import하지 않고 스트림 모드를 받기 위함(§28-1 규칙1).
- `GraphEvent`, `NodeResult`
- `StateSnapshot` (Values, Next, Config, Metadata, CreatedAt — `GetState`/`GetStateHistory` 반환형) — `core` 패키지 타입의
  alias(`type StateSnapshot = core.StateSnapshot`). 소유는 `core`이며 `checkpoint`(§11)·`agent`(§9)가 `core`만 참조해
  `graph` 역방향 import와 Phase 역전을 피한다(§28-1 규칙1). `Config` 필드는 `config.RunConfig`라 `core`는 `config`에 단방향 의존한다

### 빌더

- `NewStateGraph(schema StateSchema, opts ...SchemaOption) *Builder` — `WithInputSchema`/`WithOutputSchema` 분리 지원
- `(Builder) AddNode(name string, fn NodeFunc, opts ...NodeOption) error` — `WithDestinations(...)`로 goto 대상 선언
- `(Builder) AddEdge(from, to string) error`
- `(Builder) AddConditionalEdges(from string, router ConditionalRouter, mapping map[string]string) error`
- `(Builder) SetEntryPoint(name string) error`
- `(Builder) SetConditionalEntryPoint(router ConditionalRouter, mapping map[string]string) error`
- `(Builder) Compile(opts ...CompileOption) (*Compiled, error)` — `WithCheckpointer`/`WithStore`

### 실행

- `(Compiled) Invoke(ctx, input State, cfg config.RunConfig) (State, error)`
- `(Compiled) Stream(ctx, input State, cfg config.RunConfig, mode core.Mode) (<-chan GraphEvent, error)`
- `(Compiled) GetState(cfg config.RunConfig) (StateSnapshot, error)`
- `(Compiled) GetStateHistory(cfg config.RunConfig) ([]StateSnapshot, error)`
- `(Compiled) UpdateState(cfg config.RunConfig, update StateUpdate) error`

### 내부

- `runNode(ctx, name string, st State) (NodeResult, error)`
- `resolveNext(name string, res NodeResult) ([]string, error)` — Command/Send/conditional 해석
- `applyReducers(st State, update StateUpdate) (State, error)`
- `validate() error` — 도달 불가 노드·미정의 엣지 검사
- `checkMaxSteps(step int) error`

### 시각화

- `(Compiled) DrawMermaid() string`
- `(Compiled) DrawMermaidPNG() ([]byte, error)`

> 컴파일된 그래프 자체를 다른 그래프의 노드로 등록할 수 있어야 한다(서브그래프). 서브그래프는 부모와 상태를 공유하거나
> 독립 상태로 실행될 수 있다.

> 범위 경계: 그래프 인터럽트(`interrupt`/`resume`)는 이 런타임 범위에서 제외하고, 사용자 추가 입력은 A2A
> `input_required` 상태로 처리한다(§22-4).

### 7-1. graph/command

노드 반환으로 제어 흐름과 상태 갱신을 함께 표현한다.

#### 타입

- `Command` (Goto, Update, Graph, Sends)
- `Send` (Target, State)
- `GraphTarget` (`current` / `parent`)

#### 함수

- `Goto(target string, update core.StateUpdate) Command`
- `End(update core.StateUpdate) Command`
- `ToParent(target string, update core.StateUpdate) Command`
- `Fanout(sends []Send) Command` — 다중 분기(부모 그래프 대상 가능)
- `NewSend(target string, st any) Send`
- `(Command) IsEnd() bool`, `(Command) IsParent() bool`

---

## 8. prebuilt

사전 구성 노드와 조건 함수를 담당한다.

### ToolNode

- `NewToolNode(reg tool.Registry) graph.NodeFunc`
- 마지막 AI 메시지의 `tool_calls`를 이름으로 찾아 실행하고 `ToolMessage`를 상태에 추가한다.

### tools_condition

- `ToolsCondition(ctx, st graph.State) string` — `tools` 또는 `END` 반환
- `HasPendingToolCalls(st graph.State) bool`

### Summarization (단기 메모리 요약 압축)

- `NewSummarizationNode(model llm.Client, opts SummarizeOptions) graph.NodeFunc` — 메시지 임계 초과 시 누적 대화를
  LLM으로 요약해 `summary` 상태에 저장하고, 요약에 반영된 과거 메시지를 `message.RemoveMessage`로 정리한다
- `ShouldSummarize(st graph.State, opts SummarizeOptions) bool` — 메시지 수/토큰 임계 도달 판정(조건 엣지·분기용)
- `InjectSummary(msgs []message.Message, summary string) []message.Message` — 저장된 요약을 SystemMessage로 앞에 주입
- `SummarizeOptions` (MaxMessages, MaxTokens, KeepLast, SummaryKey)

> 트리밍(`message.TrimMessages`)이 윈도를 잘라내는 데 비해, 요약 노드는 잘려나갈 대화를 요약문으로 보존한다.
> 임계 분기 → 요약 → 오래된 메시지 일괄 삭제 → 다음 호출 시 SystemMessage 주입 패턴이다.

---

## 9. agent

ReAct 에이전트 루프(create_agent 대응)를 담당한다. 미들웨어·체크포인터·스토어·응답 포맷을 결합한다.

### 타입

- `Agent`
- `Config` (Model, Tools, SystemPrompt, Middleware, Checkpointer, Store, ResponseFormat, MaxSteps)
- `State` (MessagesState 확장; StructuredResponse 포함)
- `Option`
- `Result` (Messages, StructuredResponse)
- `Input`
- `Decision` (`shouldContinue` 반환 enum: `continue`(도구 실행) / `respond`(구조화 출력) / `end`)
- `AgentEvent` (IsTaskComplete, RequireUserInput, Content, Node, Token, Update) — `Stream`이 방출하는 진행 이벤트.
  `a2a` 실행기는 `IsTaskComplete`/`RequireUserInput`/`Content`를 읽어 태스크 상태(working/input_required/완료)로 변환한다.

### 생성

- `Create(model llm.Client, tools []tool.Tool, opts ...Option) (*Agent, error)`
- 옵션: `WithSystemPrompt`, `WithMiddleware`, `WithCheckpointer`, `WithStore`, `WithResponseFormat(schema)`, `WithMaxSteps`

### 실행

- `(Agent) Invoke(ctx, in Input, cfg config.RunConfig) (Result, error)`
- `(Agent) Stream(ctx, in Input, cfg config.RunConfig, mode core.Mode) (<-chan AgentEvent, error)`
- `(Agent) GetState(cfg config.RunConfig) (core.StateSnapshot, error)` — `StructuredResponse` 회수 포함
  (`graph.StateSnapshot`과 동일 타입. `agent`는 `core`만 참조해 `graph` 역방향 import를 피한다, §28-1 규칙1)

### 내부 루프

- `runModel(ctx, st State) (message.Message, error)`
- `runTools(ctx, st State) ([]message.Message, error)`
- `shouldContinue(st State) Decision`
- `applyResponseFormat(ctx, st State) (any, error)`

> 내부적으로 `chatbot → tools → chatbot` 루프 그래프로 컴파일된다. `ResponseFormat` 지정 시 종료 직전 구조화 출력을 생성해
> 상태의 `structured_response`에 저장한다.

---

## 10. middleware

에이전트 모델 호출 전후 훅과 동적 프롬프트를 담당한다.

### 타입

- `Middleware` (인터페이스)
- `ModelRequest` (State `graph.State`, Model, SystemPrompt)
- `ModelResponse`
- `ModelHandler(ctx, ModelRequest) (ModelResponse, error)`
- `Runtime`

### 훅 등록 (세 유형)

- `WrapModelCall(fn func(ctx, ModelRequest, ModelHandler) (ModelResponse, error)) Middleware` — 모델 호출 감싸기(노드 미추가)
- `BeforeModel(name string, fn func(ctx, State, Runtime) error) Middleware` — 모델 호출 전 노드(차단 가능)
- `DynamicPrompt(fn func(ctx, ModelRequest) (string, error)) Middleware` — 호출마다 시스템 프롬프트 생성

### 요청 조작

- `(ModelRequest) Override(model llm.Client) ModelRequest`
- `(ModelRequest) SetSystemPrompt(p string) ModelRequest`
- `(ModelRequest) StateValue(key string) any`

> `State` 및 `BeforeModel`·`DynamicPrompt`의 상태 인자는 모두 `graph.State`(범용 상태 맵)다. `agent.State`가 아니다.
> 미들웨어가 `agent.State`를 참조하면 `agent → middleware`(Config.Middleware) ↔ `middleware → agent`로 순환
> import가 생기므로, 미들웨어는 그래프 공유 상태 타입에만 의존한다(§28-1).

---

## 11. checkpoint

스레드 단위 상태 영속화(단기 메모리)를 담당한다.

### 타입

- `Checkpointer` (인터페이스)
- `InMemorySaver`
- `Checkpoint` (ThreadID, Values, Next, Metadata, CreatedAt, ParentConfig)
- `StateSnapshot` — `core.StateSnapshot`을 참조한다(`graph.StateSnapshot`도 같은 타입의 alias). `GetState`/`List`가
  반환하는 스레드 상태 스냅샷. `checkpoint`는 `core`만 import하고 `graph`는 import하지 않는다(§28-1 규칙1)

### 메소드

- `Get(ctx, threadID string) (Checkpoint, bool, error)`
- `Put(ctx, threadID string, cp Checkpoint) error`
- `List(ctx, threadID string) ([]Checkpoint, error)` — 히스토리
- `DeleteThread(ctx, threadID string) error`

### config 연동

- `ThreadIDFromConfig(cfg config.RunConfig) string`
- `LoadState(ctx, cfg config.RunConfig) (graph.State, bool, error)`
- `SaveState(ctx, cfg config.RunConfig, st graph.State) error`

> 체크포인터가 스레드 상태를 영속하는 동안 컨텍스트는 두 축으로 압축한다. (1) `message.TrimMessages`로 윈도 트리밍,
> (2) `prebuilt.NewSummarizationNode`로 누적 대화를 요약해 `summary` 상태에 보존하고 과거 메시지를 제거(§8).
> 두 방식은 배타적이지 않다.

---

## 12. store

네임스페이스 기반 장기 메모리 스토어를 담당한다.

### 타입

- `Store` (인터페이스)
- `InMemoryStore`
- `Item` (Namespace, Key, Value, Score, CreatedAt, UpdatedAt)
- `Namespace` (`[]string` 튜플)
- `IndexConfig` (Embed(EmbeddingClient), Dims)

### 메소드

- `Put(ctx, ns Namespace, key string, value map[string]any) error`
- `Get(ctx, ns Namespace, key string) (Item, bool, error)`
- `Search(ctx, ns Namespace, query string, limit int) ([]Item, error)` — 임베딩 인덱스 설정 시 시맨틱 검색
- `Delete(ctx, ns Namespace, key string) error`

### 주입

- `NewInMemoryStore(opts ...StoreOption) *InMemoryStore` — `WithIndex(IndexConfig)`
- `FromContext(ctx) (Store, bool)` — 도구 함수 내부에서 스토어 회수
- `UserIDFromConfig(cfg config.RunConfig) string`

---

## 13. streaming

스트림 모드와 서브그래프 전파를 담당한다.

### 타입

- `Mode` (`values` / `messages` / `updates` / `debug`) — `core` 패키지 타입의 alias(`type Mode = core.Mode`).
  `agent`(§9)·`graph`(§7)가 `core.Mode`를 직접 참조해 `agent`/`graph` → `streaming` 역방향 import를 끊는다(§28-1 규칙1).
- `Event` (Node, Update, Value, Token, Metadata, Path)
- `Metadata` (`map[string]any` 토큰 메타: 노드/모델/run 식별자 등)
- `Options` (Mode, Subgraphs)

### 함수

- `EmitNodeUpdate(node string, update core.StateUpdate) Event`
- `EmitStateValue(st core.State) Event`
- `EmitMessageToken(token string, md Metadata) Event`
- `EmitSubgraph(path []string, inner Event) Event`

> `updates` 모드는 노드별 변경분, `values`는 전체 상태 스냅샷, `messages`는 토큰 단위를 방출한다. `Subgraphs=true`이면
> 서브그래프 이벤트를 경로와 함께 함께 방출한다.

---

## 14. multiagent

> **응용 계층(라이브러리 아님).** 이 패턴들은 전용 패키지가 아니라 `Command`/`Send`/`bind_tools`/`create_agent`를
> 조합해 만든다. 다운스트림이 §7·§9의 primitive로 직접 구현하는 것이 목표이며, 아래는 그 설계 가이드다(import
> 대상 API가 아니다).

수퍼바이저·핸드오프·네트워크·플래너 패턴을 담당한다.

### 타입

- `Worker` (인터페이스: Name, Description, Invoke, Stream)
- `WorkerRegistry`
- `WorkerOutput` (워커 실행 산출: Messages, StructuredResponse — `MergeWorkerResult` 입력)
- `Supervisor`
- `HandoffTool`
- `Network`
- `Planner`
- `Step` (Task, Result) — 실행된 단계 1건. `structured.Plan.steps`(`[]string`, 계획 단계)와 달리 결과가 붙은 완료 단계다
- `PlannerState` (Input, Plan(`[]string`), PastSteps(`[]Step`), Response) — 플래너 그래프 상태

### Worker 레지스트리

- `RegisterWorker(w Worker) error`
- `GetWorker(name string) (Worker, bool)`
- `WorkerNames() []string`

### Supervisor (라우팅)

supervisor 구성은 세 갈래다. 아래는 그중 두 구성을 다룬다. (a) supervisor를 **별도 plain 노드**로 두고
`RouterTool`을 바인딩해 `next`를 읽는 방식(아래 함수들), (b) supervisor 자체를 `create_agent`로 만들고 handoff 도구를
바인딩해 라우팅을 도구 호출에 위임하는 방식(별도 Route 노드 없음 — 아래 Handoff 절), (c) planner 결합형(아래 Planner 절).

- `RouterTool[T](choices ...T) tool.Tool` — TypedDict 라우터를 도구로 바인딩
- `SelectNext(ctx, st agent.State) (string, error)` — `tool_calls[0].Args.next` 해석. tool_call이 없으면(라우터 미호출)
  작업 완료로 보고 종료/상위 복귀를 의미한다
- `Route(ctx, st agent.State) command.Command` — 선택 노드로 goto, 종료 시 END
- `MergeWorkerResult(st agent.State, out WorkerOutput) agent.State` — 상태 병합 헬퍼. 단 최종 답변 통합은
  supervisor LLM이 시스템 프롬프트로 수행한다(코드 머지가 아니라 프롬프트 기반 합성)

### Handoff (도구 기반 위임)

위임(supervisor→worker)과 복귀(worker→supervisor)는 메커니즘이 다르다. 위임은 핸드오프 도구가 **부모 그래프 대상**
Command(`graph=parent`)로 점프하고, 복귀는 핸드오프 도구가 아니라 정적 엣지(`AddEdge(worker, supervisor)`)로 돌아오며
`HandoffBackMessages`가 만든 AI/Tool 메시지 쌍을 상태에 남긴다.

- `CreateHandoffTool(agentName, description string) tool.Tool` — `ToolRuntime`에서 state/tool_call_id 주입
- 단일 핸드오프: `command.ToParent(agentName, update)` 반환(부모 그래프 노드로 goto)
- 다중 핸드오프: 마지막 AI 메시지의 tool_calls가 복수면 `command.Fanout([]Send{...})` 반환. 각 `Send`는 부모 그래프
  대상이며 워커마다 다른 query를 분배한다
- `HandoffBackMessages(agentName string) []message.Message` — 워커 → 수퍼바이저 복귀용 AI/Tool 메시지 쌍 생성.
  복귀 goto 자체는 이 메시지가 아니라 정적 엣지가 담당한다

### Network (Command 왕복)

- `BuildNetwork(workers []Worker) (*graph.Compiled, error)` — 워커 간 `command.Goto` 동적 라우팅
- `IsFinalAnswer(m message.Message) bool` — 종료 신호 판별

### Planner (계획/재계획)

supervisor가 `plan[0]`(첫 미완료 작업)만 워커에 위임하고, 워커 완료 시 `(task, result)`를 `past_steps`에 누적한 뒤
planning으로 복귀한다. replan은 남은 작업으로 `plan` 전체를 교체하며, 빈 plan이면 최종 응답을 낸다(한 스텝씩 소비하는 루프).

- `Plan(ctx, input string) (structured.PlannerResult, error)` — 계획 또는 대화형 응답
- `Replan(ctx, plan []string, pastSteps []Step) (structured.PlannerResult, error)` — 남은 작업으로 `plan` 전체 재작성
- `RecordStep(st PlannerState, step Step) graph.StateUpdate` — `past_steps` 누적(소비된 작업 기록)

### Worker 구성 어댑터

- `AgentAsNode(a *agent.Agent) graph.NodeFunc` — 에이전트를 그래프 노드로
- `GraphAsNode(g *graph.Compiled) graph.NodeFunc` — 서브그래프를 그래프 노드로
- `AgentAsTool(a *agent.Agent, name, desc string) tool.Tool` — 에이전트를 도구로

---

## 15. document

문서 적재와 분할을 담당한다.

### 타입

- `Document` (PageContent, Metadata)
- `Loader` (인터페이스)
- `TextSplitter`

### 로더

- `NewPDFLoader(path string) Loader` — 페이지별 Document, 메타(page/source/total_pages)
- `NewDocxLoader(path string) Loader`
- `NewWebLoader(urls []string) Loader`
- `(Loader) Load(ctx) ([]Document, error)`
- `(Loader) LazyLoad(ctx) (<-chan Document, error)`
- `ReadPDFBytes(b []byte) (string, error)` — 바이트에서 직접 텍스트 추출

### 분할

- `NewRecursiveCharacterSplitter(chunkSize, overlap int) TextSplitter`
- `(TextSplitter) SplitDocuments(docs []Document) []Document`
- `(TextSplitter) SplitText(text string) []string`

---

## 16. vectorstore

벡터 저장·검색과 retriever 생성을 담당한다. 인메모리/로컬 영속(Chroma)/외부(Supabase-pgvector) 백엔드를 제공한다.

> 로컬 백엔드(Chroma)와 외부 호스팅 백엔드(Supabase-pgvector) 두 경로가 있어 둘 다 구현 대상이다.
> 구현 순서는 인메모리/Chroma(로컬) 먼저, Supabase(외부) 나중으로 한다. 우선순위 문제일
> 뿐 어느 하나가 다른 하나를 대체하지 않는다.
>
> 단, Chroma의 로컬 영속(`persist_directory`)은 Go에서 파이썬 내부 포맷을 직접 읽기 어렵다. Chroma 서버 HTTP API
> 호출이나 다른 로컬 백엔드 대체가 현실적이다(§28 참조). 인메모리 백엔드는 제약 없이 그대로 성립한다.
>
> Phase 경계: 인메모리/Chroma 백엔드는 Phase 3에 들어간다. `SupabaseVectorStore`는 `database.Client`(Phase 7)에
> 위임 의존하므로(§19) Phase 7로 분리한다. 즉 Phase 3의 `vectorstore`는 Supabase 백엔드를 제외한 범위에서 완성된다.

### 타입

- `Store` (인터페이스)
- `InMemoryStore` (로컬 임베딩 검색)
- `ChromaStore` (영속 디렉토리 기반 로컬 벡터스토어, collection 단위 — `persist_directory`/`collection_name` 대응)
- `SupabaseVectorStore` (pgvector 백엔드, `match_documents` RPC 유사도 검색)
- `Retriever`
- `SearchOptions` (K, Filter)

### 메소드

- `FromDocuments(ctx, docs []document.Document, emb llm.EmbeddingClient, opts ...StoreOption) (Store, error)`
- `(Store) Add(ctx, docs []document.Document) error`
- `(Store) SimilaritySearch(ctx, query string, opts SearchOptions) ([]document.Document, error)` — 메타 필터 지원
- `(Store) AsRetriever(opts SearchOptions) Retriever`
- `(Retriever) Invoke(ctx, query string) ([]document.Document, error)`

### retriever 도구화

- `CreateRetrieverTool(r Retriever, name, description string) tool.Tool`

### 외부 백엔드 (Supabase)

- `MatchDocuments(ctx, queryEmbedding []float32, count int) ([]document.Document, error)` — `match_documents` RPC 유사도
  검색. 이 RPC는 `(query_embedding, match_count)` 시그니처로 메타 필터를 받지 않는다. 메타데이터 필터링이 필요하면
  `database.QueryDocuments`(eq/ilike/gte/lte) 경로를 쓴다
- `NewSupabaseVectorStore(client database.Client, opts ...StoreOption) *SupabaseVectorStore` — `database` 패키지의 클라이언트 재사용
- `NewChromaStore(persistDir, collection string, emb llm.EmbeddingClient) (*ChromaStore, error)`

---

## 17. rag

> **응용 계층(라이브러리 아님).** RAG 그래프는 전용 패키지가 아니라 `StateGraph`+노드/엣지로 조립하는 패턴이다
> (create_agent 없이 그래프로 직접 구성). 다운스트림이 §7·§15·§16의 primitive로 직접 구현하는 것이 목표이며,
> 아래는 그 설계 가이드다.

검색·증강·생성 그래프와 평가 로직을 담당한다.

### 타입

- `State` (Messages 확장: Question, Context, Answer, RetryNum)
- `SearchState` (Intent, RawInput, Question, SearchType, SearchResults, Sources, IndexRequest, IndexResult, Answer)
- `FileInfo` (Filename, StorageRef)

### 노드

- `ChatbotNode(model llm.Client, retrieverTool tool.Tool) graph.NodeFunc` — retriever를 도구로 바인딩해 호출
- `RetrieveNode(r vectorstore.Retriever) graph.NodeFunc` — 마지막 AI 메시지의 `tool_call_id`에 묶인 검색 결과를
  `ToolMessage`로 상태에 추가한다(prebuilt `ToolNode`가 아닌 커스텀 노드)
- `VectorSearchNode(r vectorstore.Retriever) graph.NodeFunc` — 임베딩 유사도 검색(`RouteSearchType`의 `vector` 분기)
- `SQLSearchNode(model llm.Client, c database.Client) graph.NodeFunc` — 자연어 질의에서 메타데이터 필터(예: 파일명
  부분일치, 생성일 범위, 문서 종류, 전체 목록 여부)를 추출해 `database.DocumentQuery`(eq/ilike/gte/lte/order/limit)로
  검색(`sql` 분기). 파일 단위 목록이 필요하면 대표 청크만 조회해 중복을 제거하고, 전체 목록 요청 시 모든 청크를
  가져온다. 결과형에 따라 목록형/내용형 생성으로 분기한다
- `ContextOrganizerNode(model llm.Client) graph.NodeFunc`
- `TransformQueryNode(model llm.Client) graph.NodeFunc`
- `GenerateNode(model llm.Client) graph.NodeFunc`
- `IndexDocumentNode(...) graph.NodeFunc` — 적재 → 분할 → 임베딩 → 저장

### 엣지·라우터

- `DecideToGenerate(ctx, st State) string` — `transform_query`(재검색) 또는 `generate`. 단 `RetryNum`이 상한(기본 3)에
  도달하면 더 변환하지 않고 강제로 `generate`로 보내며, 이때 `GenerateNode`는 답변 불가를 알리는 사과/대안 안내
  프롬프트로 분기한다
- `CheckHallucinations(ctx, st State) string` — `not supported`(재생성) 또는 `support`(END)
- `RouteIntent(ctx, st SearchState) string` — `index` 또는 `search`
- `RouteSearchType(ctx, st SearchState) string` — `vector` 또는 `sql`

### 평가

- `GradeRelevance(ctx, model llm.Client, question, doc string) (bool, error)` — `BinaryScore` 스키마
- `GradeHallucination(ctx, model llm.Client, docs, answer string) (bool, error)`

### 조립

- `BuildRAGGraph(...) (*graph.Compiled, error)`
- `FormatDocuments(docs []document.Document) string`

---

## 18. search

웹 검색과 웹 페이지 적재를 담당한다.

### 타입

- `SearchClient`
- `SearchResult` (Title, URL, Content, Score)

### 함수

- `NewSearchClient(apiKey string) SearchClient`
- `(SearchClient) Search(ctx, query string, maxResults int) ([]SearchResult, error)`
- `SearchTool(c SearchClient) tool.Tool`
- `WebContentLoaderTool() tool.Tool` — URL 목록 적재 후 본문 정리
- `ExtractURLs(text string) []string`

> 웹검색 경로는 둘로 나뉜다. (1) 이 `search` 모듈의 직접 클라이언트(Tavily SDK 대응), (2) `mcp` 모듈을 통한
> Tavily MCP 서버(`streamable_http`) 호출. 단일 에이전트는 (1) 직접 클라이언트가, 멀티에이전트/오케스트레이션
> 시나리오는 (2) MCP 경유가 자연스러우므로, 두 경로를 모두 구현 대상으로 둔다.

---

## 19. database

관계형/벡터 DB 접근과 도구화를 담당한다.

### 타입

- `Client` (인터페이스)
- `WebContentRecord` (Title, URL, Content, ExpandedQuery — `SearchWebContent`는 `title` 컬럼 전문 검색(tsvector))
- `DocumentRecord` (Content, Embedding, Filename, StorageRef, ChunkIndex, DocumentType)
- `DocumentQuery` (필터 조합: eq/ilike/gte/lte/order/limit — `QueryDocuments` 입력)

### 메소드

- `Connect(ctx) error`
- `Close(ctx) error` — 커넥션 풀 해제
- `InsertWebContent(ctx, rec WebContentRecord) error`
- `SearchWebContent(ctx, keyword string) ([]WebContentRecord, error)` — 전문 검색
- `InsertDocumentChunks(ctx, recs []DocumentRecord) error`
- `MatchDocuments(ctx, embedding []float32, count int) ([]DocumentRecord, error)` — `match_documents` RPC. 이 RPC는
  필터 인자가 없다(메타 필터는 아래 `QueryDocuments` 경로 담당)
- `QueryDocuments(ctx, q DocumentQuery) ([]DocumentRecord, error)` — eq/ilike/gte/lte/order/limit 조합

### 도구

- `SaveWebDataTool(c Client) tool.Tool`
- `SearchWebDataTool(c Client) tool.Tool`

> `MatchDocuments`는 `database`와 `vectorstore.SupabaseVectorStore` 양쪽 시그니처에 나타나지만, 실제 구현의 단일
> 소유자는 `database.Client`다. `vectorstore`는 이를 위임 호출만 한다(pgvector 접근 로직을 중복 구현하지 않는다).
> RPC와 테이블 쿼리를 한 클라이언트에서 겸할 수도 있으나, 여기선 순환·중복 회피를 위해 둘을 분리한다.

---

## 20. storage

외부 파일 스토리지 접근과 `storage_ref` 추상화를 담당한다.

### 타입

- `Client` (인터페이스)
- `FileMetadata` (FileID, StorageRef, Filename, MimeType, Size, CreatedAt, UpdatedAt, WebViewLink)
- `FileContent` (FileMetadata + Base64Content)
- `FolderMetadata` (FolderID, Name, ParentID — `FindFolderByName`/`CreateFolder` 반환)
- `ListOptions` (FolderID, MimeType, PageSize, Query 등 목록 필터), `UploadOption` (폴더 지정 등 가변 옵션)

### storage_ref

- `CreateStorageRef(fileID string) string` — `<scheme>://file/{id}` 형식
- `ParseStorageRef(ref string) (fileID string, ok bool)`

### 메소드

- `Initialize(ctx) error` — 인증, 앱 전용 폴더 보장
- `Upload(ctx, content []byte, filename, mimeType string, opts ...UploadOption) (FileMetadata, error)`
- `DownloadAsBase64(ctx, fileID string) (FileContent, error)` — 문서형은 변환 export, 일반 파일은 미디어 다운로드
- `DownloadByStorageRef(ctx, ref string) (FileContent, error)`
- `ListFiles(ctx, opts ListOptions) ([]FileMetadata, error)`
- `GetFileInfo(ctx, fileID string) (FileMetadata, error)`
- `Update(ctx, fileID string, content []byte, newName string) (FileMetadata, error)`
- `Delete(ctx, fileID string, permanent bool) error`
- `FindFolderByName(ctx, name string) ([]FolderMetadata, error)`
- `CreateFolder(ctx, name, parentID string) (FolderMetadata, error)`

### 도구 (파일 스토리지)

- `UploadFileTool`, `DownloadFileTool`, `GetFileInfoTool`, `ListFilesTool`, `FindFolderTool`,
  `DeleteFileTool`, `UpdateFileTool`, `CreateFolderTool`

---

## 21. mcp

MCP 서버 연결, 멀티서버, 도구·프롬프트 로딩을 담당한다.

### 타입

- `Client`
- `MultiServerClient`
- `ServerConfig` (Transport, Command, Args, URL)
- `Transport` (`stdio` / `streamable_http`)
- `RemoteTool`

### 단일 클라이언트

- `Connect(ctx, cfg ServerConfig) (Client, error)`
- `(Client) Initialize(ctx) error`
- `(Client) ListTools(ctx) ([]tool.Schema, error)`
- `(Client) CallTool(ctx, name string, args json.RawMessage) (tool.Result, error)`
- `(Client) LoadPrompt(ctx, name string, args map[string]any) ([]message.Message, error)`
- `(Client) Close(ctx) error`

### 멀티서버

- `NewMultiServerClient(servers map[string]ServerConfig) *MultiServerClient`
- `(MultiServerClient) GetTools(ctx) ([]tool.Tool, error)`
- `(MultiServerClient) GetToolsByServer(ctx, server string) ([]tool.Tool, error)`
- `(MultiServerClient) GetPrompt(ctx, server, name string, args map[string]any) ([]message.Message, error)`

### 어댑터

- `LoadMCPTools(ctx, c Client) ([]tool.Tool, error)`
- `LoadMCPPrompt(ctx, c Client, name string, args map[string]any) ([]message.Message, error)`
- `WrapMCPTool(server string, schema tool.Schema, c Client) tool.Tool`

### 서버 (FastMCP 대응)

도구·프롬프트를 MCP 프로토콜로 노출한다. 기존 `tool.Tool`/`tool.Registry`를 그대로 재사용한다.
파이썬은 `FastMCP`를 의존성으로 가져오지만 Go에는 대응물이 없어 서버 측을 직접 제공한다.

- `Server`
- `NewServer(name string) *Server`
- `(Server) RegisterTool(t tool.Tool) error` / `RegisterTools(reg tool.Registry) error`
- `(Server) RegisterPrompt(name string, fn func(args map[string]any) ([]message.Message, error)) error`
- `(Server) ServeStdio(ctx) error`
- `(Server) ServeStreamableHTTP(ctx, addr string) error`

---

## 22. a2a

Agent-to-Agent 타입·서버·클라이언트와 태스크 수명주기를 담당한다.

### 22-1. 타입

- `AgentCard` (Name, Description, URL, Version, DefaultInputModes, DefaultOutputModes, Capabilities, Skills)
- `AgentSkill` (ID, Name, Description, Tags, Examples)
- `AgentCapabilities` (Streaming, InputModes, OutputModes)
- `Task` (ID, ContextID, Status, History(`[]Message`), Artifacts), `TaskStatus` (State, Message)
- `TaskState` (`working` / `input_required` / `completed` / `failed` / `canceled`)
- `Message` (Role, Parts, MessageID)
- `Part` (TextPart / DataPart / FilePart 래퍼)
- `TextPart` (Text)
- `DataPart` (Data `map[string]any`)
- `FilePart` (File: `FileWithUri` 또는 `FileWithBytes`)
- `FileWithUri` (URI, MimeType, Name), `FileWithBytes` (Bytes(base64), MimeType, Name)
- `Artifact` (ArtifactID, Name, Description, Parts, Metadata)
- `MessageSendParams`, `SendMessageRequest`
- `TaskStatusUpdateEvent`, `TaskArtifactUpdateEvent`
- `Event` (스트리밍 이벤트 합집합: `Task` / `TaskStatusUpdateEvent` / `TaskArtifactUpdateEvent` — `SendMessageStreaming` 채널 원소)

> JSON-RPC 와이어 포맷에서 `Message`/`Part`는 `kind` 문자열(`message`/`text`/`data`/`file`)로 union을 판별한다. Go
> 래퍼 구조는 직렬화 시 이 `kind` 필드를 부착해야 한다(§28의 "스펙 직접 구현"). `Task`의 `history`는 누적 메시지
> 이력으로, 결과 추출 시 `artifacts`와 함께 읽는다.

### 22-2. 서버

- `AgentExecutor` (인터페이스: `Execute(ctx, rc RequestContext, q EventQueue) error`, `Cancel(...)`)
- `RequestContext` (GetUserInput, CurrentTask, Message)
- `EventQueue` (EnqueueEvent)
- `TaskUpdater`
  - `UpdateStatus(state TaskState, msg Message, opts ...UpdaterOption)` — `Final(true)` 옵션
  - `AddArtifact(parts []Part, name string)`
  - `Complete()`
  - `Cancel(msg Message)`
- `TaskStore` (인터페이스: 태스크 저장/조회 — `InMemoryTaskStore`가 구현)
- `InMemoryTaskStore`
- `RequestHandler` (인터페이스: JSON-RPC 요청 → 실행기 디스패치 — `DefaultRequestHandler`가 구현, `NewServer`가 수신)
- `DefaultRequestHandler(executor AgentExecutor, store TaskStore)`
- `NewServer(card AgentCard, handler RequestHandler) *Server` — HTTP 앱 구성. AgentCard를 표준 경로
  `/.well-known/agent-card.json`에 노출한다(클라이언트 `CardResolver`가 이 경로로 조회)
- `(Server) Run(ctx, host string, port int) error`
- 헬퍼: `NewTask(msg Message) Task`, `NewAgentTextMessage(text, contextID, taskID string) Message`

### 22-3. 클라이언트

- `CardResolver`
  - `NewCardResolver(baseURL string) CardResolver`
  - `GetAgentCard(ctx) (AgentCard, error)` — `baseURL`의 `/.well-known/agent-card.json`에서 카드를 조회
- `Client`
  - `NewClient(card AgentCard) Client`
  - `SendMessage(ctx, req SendMessageRequest) (Task, error)`
  - `SendMessageStreaming(ctx, req SendMessageRequest) (<-chan Event, error)`
- 아티팩트 추출 헬퍼: `ArtifactText(a Artifact) (string, bool)`, `ArtifactData(a Artifact) (map[string]any, bool)`,
  `ArtifactFileURI(a Artifact) (string, bool)`, `ArtifactFileBytes(a Artifact) ([]byte, bool)`

### 22-4. LangGraph 에이전트 → A2A 어댑터

- `StreamToTaskUpdates(ctx, a *agent.Agent, query, sessionID string, u *TaskUpdater) error`
  - 실행기 진입 시 current_task가 없으면 `NewTask`로 생성해 큐에 적재한 뒤 `TaskUpdater`를 만든다
  - 도구 호출 중 → `working`, 추가 입력 필요 → `input_required(final)`, 완료 → `AddArtifact` + `Complete`,
    실행기 레벨 예외 → `failed`, 취소 → `Cancel`로 `canceled`
- 상태 판정 경로 두 가지를 모두 지원한다(에이전트별 택일이며, 통합 시나리오의 기본은 (1)이다). (1) `agent.AgentEvent`의
  `IsTaskComplete`/`RequireUserInput`/`Content` 플래그로 매핑, (2) 에이전트의 `structured_response.status`
  (`input_required|completed|error`)를 태스크 상태로 매핑. 단 status `error`는 `failed`가 아니라 `input_required`로
  흐른다(`failed`는 실행기 예외에서만 발생)

---

## 23. orchestrator

> **응용 계층(라이브러리 아님).** 오케스트레이터는 전용 패키지가 아니라 인텐트/플랜/실행 로직을 직접 작성하고
> 원격 호출만 A2A 클라이언트를 쓰는 응용이다. 다운스트림이 §22 A2A 클라이언트 위에 직접 구현하는 것이 목표이며,
> 아래는 그 설계 가이드다.

인텐트 분석, 플랜 생성·실행, 원격 에이전트 호출, 결과 통합을 담당한다.

### 타입

- `Orchestrator`
- `Intent` (Kind, Plan, DirectAnswer)
- `PlanStep` (Agent, Query)
- `RemoteAgentRef` (Name, Client)
- `CallResult` (Agent, Artifacts, Text, Data)
- `ProgressEvent` (Phase, Agent, Message, Result — `Stream`이 방출하는 진행 이벤트)

### 메소드

- `Initialize(ctx, agentURLs map[string]string) error` — 카드 조회 후 클라이언트 구성
- `AnalyzeIntent(ctx, query string) (Intent, error)` — JSON 출력으로 인텐트/플랜/직접답변 도출
- `CallRemoteAgent(ctx, name, query string) (CallResult, error)`
- `ExecutePlan(ctx, plan []PlanStep) ([]CallResult, error)` — 스텝을 순차 실행하되, 선행 스텝 실패 시 후속 스텝을
  스킵하고, 직전 결과(텍스트/아티팩트)를 다음 스텝 질의에 컨텍스트로 주입(`[이전 결과]`/`[검색 결과]` 치환)한다
- `GenerateFinalResponse(ctx, query string, results []CallResult) (string, error)`
- `Stream(ctx, query, sessionID string) (<-chan ProgressEvent, error)`

### 에이전트 간 데이터 전달

- 원격 에이전트는 결과를 두 종류로 나눠 방출할 수 있다. 구조화 데이터는 `DataPart`, 사람이 읽는 텍스트는
  `TextPart`다. 각 Part의 `name`과 페이로드 스키마는 다운스트림이 정한다(예: 목록형 결과를 `DataPart`로 전달하고,
  `storage_ref`를 포함한 자연어로 다음 단계 에이전트 질의에 주입).
- 다운스트림은 `CallResult.Data`에서 자신의 스키마를 추출한다(예: 파일 목록 → `[]FileInfo`).

---

## 24. config

실행 설정과 식별자 관리를 담당한다.

### 타입

- `Config`
- `RunConfig` (Configurable `map[string]any`)
- `ModelConfig`
- `ServerConfig` (MCP/A2A 엔드포인트)
- `AgentConfig` (Name, URL, Port, Description)

### 함수

- `LoadEnv(path string) (Config, error)`
- `GetThreadID(cfg RunConfig) string`
- `GetUserID(cfg RunConfig) string`
- `GetConfigurable(cfg RunConfig, key string) (any, bool)`
- `AgentURLs() map[string]string`
- `GetAgentConfig(name string) (AgentConfig, error)`
- `LoadMCPServers() map[string]ServerConfig` — `config` 중립 타입을 반환(mcp 어셈블리에서 `mcp.ServerConfig`로 변환). `config`를 leaf로 유지(§28-1)

> 흩어진 실행 설정과 `.env`를 한 패키지로 통합한 설계다. 일부 함수(`LoadMCPServers`, `AgentURLs` 등)는 그 통합
> 과정에서 추가된 것이다.
>
> `LoadEnv`가 읽는 주요 환경변수: `ANTHROPIC_API_KEY`(챗), `OPENAI_API_KEY`(임베딩), `TAVILY_API_KEY`,
> `SUPABASE_URL`/`SUPABASE_KEY`.
> 구글 드라이브 OAuth 자격증명은 환경변수가 아니라 `credentials.json`/`token.json` 파일 경로로 다루므로, env가
> 아닌 파일 경로 설정으로 둔다.
> 에이전트 엔드포인트는 필수 환경변수가 아니라 기본값이 있는 선택적 오버라이드다(`.env.example`에 선언되지 않음).
> `AgentURLs()`는 임의의 에이전트 이름→URL 맵을 다루며(§23 `Initialize(agentURLs map)`와 정합), 특정 이름
> (RAG/WEB/FILE 등)은 다운스트림 프로젝트의 예시일 뿐 라이브러리가 고정하지 않는다.

---

## 25. trace

실행 추적과 디버그 출력을 담당한다.

> 선택 모듈이다(LangSmith 등 외부 추적 미사용). 핵심 동작에 필수가 아니므로 우선순위는 낮게 둔다.

### 타입

- `Trace`, `Event`
- `NodeTrace`, `ToolTrace`, `LLMTrace`, `ErrorTrace`

### 메소드

- `StartRun(runID string)` / `EndRun(runID string)`
- `RecordNodeStart(node string, st graph.State)` / `RecordNodeEnd(node string, update graph.StateUpdate)`
- `RecordToolCall(call message.ToolCall)` / `RecordToolResult(res tool.Result)`
- `RecordLLMRequest(req llm.ChatRequest)` / `RecordLLMResponse(resp llm.ChatResponse)`
- `RecordError(err error)`
- `Events() []Event`
- `ExportJSON() ([]byte, error)`

---

## 26. 구현 순서

> 범위 외(이 런타임이 구현하지 않는 것): LangGraph 개발 서버·배포 설정(`langgraph-cli`/`langgraph.json`),
> 그래프 인터럽트 기반 HITL(`interrupt`/`resume` — A2A `input_required`로 대체, §7·§22-4),
> LangSmith 추적(§25), OpenAI 외 프로바이더(anthropic/gemini — 확장 자리만, §4), 도메인 특화 도구(차트/캔버스
> 생성·코드 실행 등 — `tool.FromFunc`로 다운스트림 프로젝트가 직접 정의, §5).

> 라이브러리 vs 응용 계층: 아래 Phase는 **라이브러리(import 대상)** 구현 순서다. `rag`·`multiagent`·`orchestrator`는
> 라이브러리가 아니라 다운스트림이 직접 구현하는 응용 계층이므로(§1), 해당 primitive가 준비된 시점 이후의
> "(응용)" 구현으로 따로 표기한다.

### Phase 0 — 토대 (leaf)

`config`(RunConfig·thread_id/user_id 추출 — 무의존 최하위 leaf), `core`(State/StateUpdate/Mode/StateSnapshot —
`config`에만 의존하는 leaf). 이후 모든 Phase가 구체 타입으로 참조한다. `StateSnapshot`을 `core`가 소유하므로
`checkpoint`·`agent`(Phase 1)가 `graph`(Phase 2)를 역참조하지 않는다(§28-1 규칙1). `config`의 A2A/MCP 어셈블리 함수(`LoadMCPServers`/`AgentURLs`/`GetAgentConfig`)는 Phase 7에서
채운다(§28-1 규칙3). store/trace는 인터페이스로 끊겨 미구현 상태로도 Phase 1이 컴파일된다.

### Phase 1 — 핵심 런타임

`message`, `llm`, `tool`, `structured`, `prompt` → `agent`(create_agent), `middleware`(wrap_model_call/before_model/
dynamic_prompt), `prebuilt`(ToolNode/tools_condition/SummarizationNode), `checkpoint`. 단일 에이전트(도구 호출 루프 +
미들웨어 + 단기 메모리(트리밍·요약) + 구조화 출력)가 동작한다. 챗 프로바이더는 Anthropic(Claude, 기본
`claude-opus-4-8`)만 구현하며, 임베딩 팩토리(`InitEmbeddings`/`EmbeddingClient`)는 첫 소비처인 Phase 3로 미룬다(§4).

### Phase 2 — 그래프 엔진

`graph` + `graph/command` + `streaming`. 노드/엣지/조건엣지/진입점/destinations/입출력 스키마/서브그래프,
스트림 모드(values/messages/updates/subgraphs)가 동작한다.

### Phase 3 — 문서·벡터스토어

`document`, `vectorstore`. 적재→분할→임베딩→저장, 유사도 검색, retriever 도구화가 동작한다. 임베딩 팩토리
(`llm.InitEmbeddings`/`EmbeddingClient`, OpenAI)는 Phase 1에서 미뤄 이 Phase에서 구현한다(첫 소비처가 vectorstore라서).
벡터 백엔드는 인메모리 → Chroma(로컬 영속)까지 이 Phase에서 다룬다. Supabase-pgvector(`match_documents` RPC)는 `database.Client`에
의존하므로 Phase 7로 분리한다(§16). RAG 그래프(검색→증강→생성, 관련성·환각 평가, 라우팅)는 응용 계층(§17)이라
다운스트림이 이 primitive 위에 직접 구현한다.

### Phase 4 — (응용) 멀티에이전트

응용 계층(§14). 라이브러리 산출물이 아니라, Phase 1~2의 primitive(`agent`/`graph`/`command`)가 준비된 뒤 다운스트림이
직접 구현한다. 수퍼바이저 라우팅(도구 바인딩), 핸드오프(단일/다중 Send), 워커-as-노드/서브그래프, 네트워크 왕복,
플래너(계획/재계획)가 대상이다.

### Phase 5 — 메모리 확장

`store`. 네임스페이스 장기 메모리와 임베딩 기반 시맨틱 검색, 도구 함수 내 스토어 주입이 동작한다.

### Phase 6 — MCP

`mcp`. stdio/streamable_http 전송, 멀티서버 도구·프롬프트 로딩(클라이언트)과 도구·프롬프트 노출(서버),
도구 어댑터가 동작한다.

### Phase 7 — A2A & 외부 연동

`a2a`, `database`, `search`, `storage`, `trace`, `config`의 어셈블리 함수, `vectorstore.SupabaseVectorStore`.
A2A 서버/클라이언트, 태스크 상태 전이, 아티팩트(텍스트/데이터/파일), 웹검색, 외부 벡터/관계형 DB·파일 스토리지
연동이 동작한다(`config` leaf 토대는 Phase 0에서 이미 존재). 오케스트레이터(원격 에이전트 호출·결과 통합)는
응용 계층(§23)이라 다운스트림이 A2A 클라이언트 위에 직접 구현한다.

---

## 27. 핵심 최소 집합

라이브러리 토대가 되는 최소 구현 집합(응용 계층 `rag`/`multiagent`/`orchestrator`는 제외 — 다운스트림이 이 위에 직접 구현):

```text
config:      RunConfig, GetThreadID, GetUserID (무의존 최하위 leaf 토대)
core:        State, StateUpdate, Mode, StateSnapshot (config에만 의존하는 leaf 토대)
message:     Message, ToolCall, ToolResult, AddMessages, LastAIMessage, HasToolCalls
llm:         Client.Chat, Client.ChatStream, Client.Structured, BindTools, ParseToolCalls
tool:        Tool, Schema, Registry, Executor, Runtime
agent:       Create, Invoke, Stream, GetState (ReAct 루프 + 미들웨어 + 체크포인터 + 응답포맷)
graph:       NewStateGraph, AddNode, AddEdge, AddConditionalEdges, SetEntryPoint, Compile, Invoke, Stream
command:     Goto, End, ToParent, Fanout, Send
prebuilt:    ToolNode, ToolsCondition, NewSummarizationNode
structured:  BuildSchema, ParseStructured, Validate
checkpoint:  Checkpointer, InMemorySaver, ThreadIDFromConfig
mcp:         Client, MultiServerClient, LoadMCPTools, LoadMCPPrompt
a2a:         AgentExecutor, TaskUpdater, Client, CardResolver, AgentCard
```

> 응용 계층은 위 primitive를 조합해 다운스트림이 직접 구현한다(§14·§17·§23이 설계 가이드). 대표 조립 단위:
> `multiagent`(Supervisor, HandoffTool, AgentAsNode/GraphAsNode/AgentAsTool), `rag`(BuildRAGGraph, GradeRelevance,
> GradeHallucination), `orchestrator`(AnalyzeIntent, ExecutePlan, CallRemoteAgent, GenerateFinalResponse).

---

## 28. 구현 유의사항 (Go 생태계)

파이썬 의존성 중 일부는 Go 직접 대응물이 없어 구현 난이도가 크게 다르다. 모듈 설계가 아니라 구현 단계에서
주의할 지점만 정리한다.

- `a2a` — Go용 공식 a2a-sdk가 없다. A2A의 JSON-RPC 프로토콜(메시지/태스크/아티팩트/스트리밍)을 스펙에서 직접
  구현해야 하며, 이 명세에서 작업량이 가장 큰 모듈이다.
- `vectorstore` (Chroma) — `persist_directory` 영속은 Chroma(파이썬·sqlite) 내부 포맷이다. Go에서 그
  파일을 직접 읽기는 어렵고, 보통 Chroma 서버 HTTP API를 호출하거나 다른 백엔드로 대체한다. "임베디드 로컬"
  뉘앙스(§16)는 Go에선 그대로 성립하지 않을 수 있다.
- `mcp` — `FastMCP` 무대응은 §21에 명시. 다만 MCP는 공식 Go SDK(`modelcontextprotocol/go-sdk`)가 있어
  클라이언트·서버 양쪽에서 활용할 수 있다.
- `document` — `pypdf`/`docx2txt` 대응 Go 라이브러리는 존재하나 한국어 PDF 텍스트 추출 품질 편차가 크다.
- 무난한 영역 — Google Drive(`google.golang.org/api/drive/v3`), Supabase/pgvector(`pgx` + `pgvector-go`),
  토큰 카운트(`tiktoken-go`), 웹 본문 파싱(`goquery` — bs4 대응)은 Go 생태계가 갖춰져 있다.

---

## 28-1. import 사이클 회피 (패키지 경계 규칙)

§2~§25 시그니처를 글자 그대로 따르면 Go에서 컴파일되지 않는 순환 import가 생긴다. 아래 네 규칙으로 경계를 잡는다.
실제 구현 시 인터페이스 경계만 유지하면 시그니처 세부는 조정해도 된다.

**확인된 순환과 원인** (아래는 초기 설계 시그니처(구체 타입 직참조)를 글자 그대로 따랐을 때 생기는 순환이다. 본문
§5·§7·§9·§11·§13·§24는 이미 아래 해소 규칙을 반영해 alias·중립 타입·좁은 인터페이스로 고쳐져 있으므로, 현재 본문 자체에는
이 순환이 없다.)

- `tool ↔ trace` — `(Runtime) Emit(trace.Event)`(§5)와 `RecordToolCall/Result(message.ToolCall/tool.Result)`(§25)
- `config → mcp → tool → config` — `LoadMCPServers() map[string]mcp.ServerConfig`(§24) → `CallTool/ListTools`(§21) →
  `(Runtime) Config() config.RunConfig`(§5)
- `tool ↔ store`(+config 경유) — `(Runtime) Store() store.Store`(§5)와 `UserIDFromConfig(config.RunConfig)`(§12)
- `graph ↔ graph/command` — `NodeFunc`가 `command.Command` 반환(§7)인데 `command`는 `StateUpdate`(graph 소유)에 의존(§7-1)
- `graph ↔ streaming` — `Stream(..., streaming.Mode)`(§7)와 `EmitNodeUpdate(graph.StateUpdate)`(§13)
- `graph ↔ checkpoint` — `Compile(WithCheckpointer)`(§7)가 `checkpoint.Checkpointer`를 받는데(graph→checkpoint),
  `checkpoint`의 `StateSnapshot`/`LoadState`/`SaveState`가 `graph.StateSnapshot`·`graph.State`에 의존(checkpoint→graph).
  추가로 checkpoint·agent(Phase 1)가 graph(Phase 2) 타입에 의존하면 Phase 역전까지 발생

**해소 규칙**

1. **공유 원시 타입은 leaf로 내린다.** `State`/`StateUpdate`/`Mode`/`StateSnapshot`은 `core`에, `RunConfig`는
   `config`에 둔다. `config`는 무의존 최하위 leaf이고 `core`는 `config`에만 단방향 의존하는 leaf다(`StateSnapshot.Config`가
   `config.RunConfig`이므로 — 단방향이라 순환 없음). 그러면 `command`·`streaming`은 `graph`를 import하지 않고 `core`만
   참조하며, `graph`/`agent`도 `core.Mode`를 직접 참조해 `streaming` 역의존을 피한다. 또한 `StateSnapshot`을 `core`가
   소유하므로 `checkpoint`·`agent`(Phase 1)는 `core.StateSnapshot`만 참조해 `graph`(Phase 2) 역방향 import와 Phase
   역전을 동시에 끊는다. `graph`/`agent`/`checkpoint`는 `graph.StateSnapshot`을 `core` alias로 노출한다(본문
   §7·§9·§11·§13이 이 alias를 이미 반영). 한편 `Compile(WithCheckpointer)`의 `graph → checkpoint` 단방향 의존은
   `checkpoint`가 더 이상 `graph`를 import하지 않으므로 순환을 만들지 않는다.
2. **주입 접근자는 좁은 인터페이스로.** `tool.Runtime`은 `store.Store`/`trace.Event` 구체 타입 대신 `tool` 패키지 안에
   선언한 최소 타입(`Store`, `Event` — 본문 §5 타입 절에 등재)을 반환·수신하고, 구현은 상위 패키지(`store`/`trace`)가
   주입한다. `tool`은 상위 패키지를 import하지 않는다. (`config.RunConfig`는 규칙3대로 leaf라 구체 타입 그대로 받아도
   순환이 없다 — 인터페이스화 대상이 아니다.)
3. **`config`는 leaf로 유지한다.** `LoadMCPServers`가 `mcp.ServerConfig`를 직접 반환하지 않도록 중립 맵을 반환해 `mcp`가
   해석하거나, 서버 조립 책임을 상위 조립 패키지로 옮긴다. `config`는 어떤 상위 패키지도 import하지 않는다.
4. **의존 방향은 §1 단방향 트리를 강제한다.** 상위(`agent`/`multiagent`/`rag`/`a2a`/`orchestrator`)→하위(`message`/`llm`/
   `tool`/`graph`)로만 흐르게 하고, 역참조가 필요하면 1·2번처럼 인터페이스/leaf 타입으로 끊는다.

이 규칙을 적용하면 §26 Phase 1(`tool` 포함)은 `core`·`config` leaf 토대(Phase 0)만 있으면 `store`(Phase 5)·`trace`
(Phase 7)의 **구현 없이도** 인터페이스만으로 컴파일된다. `config.RunConfig`는 구체 타입으로 쓰지만 leaf라 Phase 0에서
이미 존재하므로 문제되지 않는다(`config`의 A2A/MCP 어셈블리 함수만 Phase 7로 미뤄진다).
