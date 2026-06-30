# phase-6 — MCP analysis

## 근거

조사한 사실과 추정을 구분해 기록한다.

### 코드베이스(직접 확인)

- `tool/tool.go`
  - `tool.Tool` 인터페이스: `Name() string`, `Description() string`, `Schema() Schema`,
    `Execute(ctx context.Context, input Input, rt Runtime) (Result, error)`. `Input = json.RawMessage`.
  - `tool.Schema{Name, Description, Parameters []Parameter}`, `tool.Parameter{Name, Type, Description, Required bool}`.
    `Type`은 JSON 타입 문자열(`string`/`integer`/`number`/`boolean`/`array`/`object`).
  - `tool.Result{Content string, IsError bool}` — 단일 텍스트 콘텐츠 + 오류 플래그.
  - `tool.Registry`: `List() []Tool`, `Schemas() []Schema`, `Get(name) (Tool, bool)` 등. 등록 순서를 `order`로 보존.
  - `tool.Runtime` 인터페이스(State/ToolCallID/Config/Store/Emit). `Execute`에 주입되며, 원격 도구는 이 컨텍스트를 쓰지
    않아도 시그니처는 충족해야 한다.
  - `tool` 패키지는 `config`·`message`만 import한다(파일 상단 주석 §28-1 규칙2).
- `message/message.go`
  - `message.Message{Role, Content, Name, ID, ToolCalls, ToolCallID}`. `Role`은 `system`/`user`/`assistant`/`tool`.
  - 생성자: `NewSystemMessage`, `NewUserMessage`, `NewAssistantMessage`, `NewToolMessage`. MCP 프롬프트의 role은
    `user`/`assistant`만 존재하므로 `NewUserMessage`/`NewAssistantMessage`로 직접 대응된다.
  - `message`는 `core`만 의존하는 최하단 노드다.
- `go.mod`: 모듈 `github.com/zipkero/langgraph-go`, go 1.26.4. 기존에 공식 SDK 의존 선례 있음
  (`github.com/anthropics/anthropic-sdk-go v1.52.0`). 새 SDK 추가는 `require`에 한 줄을 더하고 indirect 전이 의존을
  늘릴 뿐, 기존 코드 수정 없이 가능하다.
- import 경계 테스트 선례(`vectorstore/import_boundary_test.go`): `go list -deps <pkg>`를 `exec.Command`로 돌려
  전이 의존 목록을 받고, 포함되어야 할 패키지와 금지 패키지를 `hasDep`로 단언한다. `<feature>_test` 외부 패키지에서
  실행. Phase 6도 같은 패턴으로 `mcp`의 deps에 `tool`/`message`/`core` 포함, `agent`/`graph` 미포함을 단언할 수 있다.
- e2e 선례(`vectorstore/e2e_test.go`): 외부(Ollama) 의존이라 `skipIfOllamaUnavailable`로 가드. Phase 6 루프백은
  외부 의존이 없으므로 이 skip 가드가 필요 없다(아래 §2·§5 D5).

### MCP Go SDK(WebFetch로 확인)

출처: `https://pkg.go.dev/github.com/modelcontextprotocol/go-sdk/mcp` (2회 fetch).

- 모듈 경로: `github.com/modelcontextprotocol/go-sdk`, 패키지 import 경로: `.../go-sdk/mcp`. 최신 안정 버전 표기
  v1.6.1(pkg.go.dev 표기 기준). 정확한 require 버전은 implement 단계에서 `go get`으로 확정한다(§5 D8).
- 클라이언트: `mcp.NewClient(*Implementation, *ClientOptions) *Client` → `client.Connect(ctx, transport, opts)
  (*ClientSession, error)`. 세션 메서드 `ListTools(ctx, *ListToolsParams) (*ListToolsResult, error)`,
  `CallTool(ctx, *CallToolParams) (*CallToolResult, error)`, `GetPrompt(ctx, *GetPromptParams) (*GetPromptResult,
  error)`, `Close() error`.
- 서버: `mcp.NewServer(*Implementation, *ServerOptions) *Server`. 도구 등록 `server.AddTool(t *Tool, h ToolHandler)`
  (untyped) 및 제네릭 `mcp.AddTool[In,Out](s, t, ToolHandlerFor[In,Out])`(스키마 자동 추론). 프롬프트 등록
  `server.AddPrompt(p *Prompt, h PromptHandler)`. 서빙 `server.Run(ctx, Transport) error`(블로킹).
- 전송: `*StdioTransport`(서버 `Run`에 전달), `*CommandTransport{Command: exec.Command(...)}`(클라이언트가
  서브프로세스 stdio 접속), 그리고 테스트용 **인메모리 파이프** `mcp.NewInMemoryTransports() (*InMemoryTransport,
  *InMemoryTransport)` — 같은 프로세스 내에서 서버·클라이언트를 양방향 연결.
- 타입:
  - `Tool{Name, Description string, InputSchema, OutputSchema *jsonschema.Schema}`. InputSchema 타입은
    `github.com/google/jsonschema-go`의 `*jsonschema.Schema`(JSON Schema 구조체).
  - `CallToolParams{Name string, Arguments any}` — Arguments는 JSON 직렬화 가능한 임의 값(map[string]any 또는
    구조체).
  - `CallToolResult{Content []Content, IsError bool}`. `Content`는 인터페이스, 구현 `TextContent{Text string,...}`.
  - `GetPromptResult{Description string, Messages []*PromptMessage}`, `PromptMessage{Role Role, Content Content}`,
    Role 값은 `"user"`/`"assistant"`.
  - `ListToolsResult{Tools []*Tool}`.
  - `Implementation{Name, Version string}` — 클라이언트/서버 식별자.

### 불명확/확정 보류(추정하지 않음)

- 정확한 require 버전 핀, 그리고 일부 타입의 미세 필드(예: `CallToolResult`의 structured-output 필드 유무,
  `ToolHandler` 시그니처의 정확한 인자명)는 pkg.go.dev 요약 수준에서만 확인했다. 구현 시 `go doc`/컴파일로 확정한다.
- `jsonschema.Schema`의 정확한 빌더 API(properties/required 채우는 방식)는 implement에서 `go doc github.com/
  google/jsonschema-go/jsonschema`로 확정한다(§5 D2).
- analysis가 의존하는 핵심 사실(모듈 경로 존재, stdio 클라이언트/서버 전송 제공, 인메모리 전송 제공, ListTools/
  CallTool/GetPrompt/AddTool/AddPrompt/Run API 존재)은 모두 확인됐다. 따라서 설계를 확정 가능하다.

## 1. 구조

신규 단일 패키지 `mcp`(`github.com/zipkero/langgraph-go/mcp`)를 추가한다. 내부를 책임별 파일로 나눈다(파일명은
구현 단계 재량이나 책임 경계는 아래와 같이 commit).

- 클라이언트 계층 — `Client`(단일 서버 세션 래퍼)와 `MultiServerClient`(여러 `Client` 묶음). SDK의
  `*Client`/`*ClientSession`/전송을 내부에 감추고 §21 이름·계약만 노출한다. SPEC §5.2·§5.4.
- 어댑터 계층 — MCP 원격 도구를 `tool.Tool`로 감싸는 `RemoteTool`/`WrapMCPTool`/`LoadMCPTools`, MCP 프롬프트를
  `[]message.Message`로 변환하는 `LoadMCPPrompt`/`LoadPrompt`. SDK 타입 ↔ 우리 타입 변환의 단일 책임 지점.
  SPEC §5.2·§5.3.
- 서버 계층 — `Server`/`NewServer`/`RegisterTool`/`RegisterTools`/`RegisterPrompt`/`ServeStdio`. 기존
  `tool.Tool`/`tool.Registry`/프롬프트를 SDK 서버의 도구·프롬프트 핸들러로 역어댑팅해 노출. SPEC §5.5.
- 설정 타입 — `Transport`(명명 문자열, 값 `stdio`), `ServerConfig{Transport, Command, Args, URL}`. `mcp`가 소유.
  `config.LoadMCPServers`는 Phase 7 소관이라 여기서 만들지 않는다. SPEC §5.1.

패키지 의존 방향: `mcp` → `tool`/`message`/`core` + `mcp-go-sdk`(+ 표준 라이브러리). `agent`/`graph` 미import.
이 단방향이 §28-1 규칙을 충족한다. SPEC §5.7.

## 2. 데이터 흐름

### 클라이언트 인바운드(원격 → 우리 타입)

연결: `Client.Connect(ctx)`가 `ServerConfig`를 보고 전송을 만든다 — `Transport=stdio`이면 `CommandTransport
{Command: exec.Command(cfg.Command, cfg.Args...)}`를 구성해 `sdkClient.Connect`로 `*ClientSession`을 얻는다.
`Initialize`는 SDK가 `Connect` 시 핸드셰이크를 수행하므로 우리 `Initialize`는 그 보장을 표면화하는 얇은 메서드이거나
`Connect`에 흡수된다(§5 D1에서 매핑 확정).

도구 목록: `ListTools` → `session.ListTools` → `ListToolsResult.Tools([]*Tool)`의 각 `Tool`을 `tool.Schema`로
변환(아래 §3 매핑). `[]tool.Schema` 반환. SPEC §5.2.

도구 호출: `CallTool(name, args)` → `args(json.RawMessage 또는 map)`를 `CallToolParams{Name, Arguments}`로 싣고
`session.CallTool` → `CallToolResult.Content`의 `TextContent.Text`를 이어 붙여 `tool.Result.Content`로,
`CallToolResult.IsError`를 `tool.Result.IsError`로 옮긴다. SPEC §5.2.

프롬프트: `LoadPrompt(name, args)` → `session.GetPrompt(GetPromptParams)` → `GetPromptResult.Messages`의 각
`PromptMessage`를 role별로 `message.NewUserMessage`/`NewAssistantMessage`에 `TextContent.Text`를 넣어
`[]message.Message`로 변환. SPEC §5.2·§5.3.

종료: `Close` → `session.Close`. SPEC §5.2.

### 어댑터(원격 도구 → tool.Tool)

`LoadMCPTools(client)`는 `ListTools` 결과 각 스키마를 `WrapMCPTool`로 감싼 `[]tool.Tool`을 만든다. `RemoteTool`은
`tool.Tool` 구현체로, `Schema()`는 보관한 `tool.Schema`를 반환하고, `Execute(ctx, input, rt)`는 입력 `input
(json.RawMessage)`을 그대로 `client.CallTool(name, input)`에 위임해 받은 `tool.Result`를 반환한다. `rt(Runtime)`는
원격 호출에서 쓰지 않으나 시그니처 충족을 위해 받는다. `LoadMCPPrompt`는 `client.LoadPrompt`를 그대로 위임한다.
SPEC §5.3.

### 멀티서버 클라이언트

`NewMultiServerClient(servers map/[]named ServerConfig)`가 서버 이름→`Client` 맵을 구성한다(각각 연결). `GetTools`는
모든 `Client`의 `LoadMCPTools` 결과를 통합 반환, `GetToolsByServer(server)`는 해당 이름의 `Client`만, `GetPrompt
(server, name, args)`는 해당 `Client.LoadPrompt`로 위임. 미등록 server 이름은 not-found 에러로 구분(§3·§5 D6).
SPEC §5.4.

### 서버 아웃바운드(우리 타입 → 원격 노출)

`NewServer(name)`이 `sdk.NewServer(&Implementation{Name:name}, nil)`을 감싼다. `RegisterTool(t tool.Tool)`은
`t.Schema()`를 SDK `*Tool{Name, Description, InputSchema}`로 역변환(아래 §3)하고, `t.Execute`를 호출하는 SDK
`ToolHandler`를 만들어 `server.AddTool`로 등록한다. 핸들러는 들어온 `CallToolParams.Arguments`를 `json.RawMessage`로
직렬화해 `t.Execute(ctx, input, rt)`에 넘기고(서버측 `rt`는 최소 no-op Runtime), 받은 `tool.Result`를
`CallToolResult{Content:[]Content{&TextContent{Text:res.Content}}, IsError:res.IsError}`로 싣는다.
`RegisterTools(reg tool.Registry)`는 `reg.List()`를 순회해 각각 `RegisterTool`. `RegisterPrompt(name, msgs/handler)`는
`[]message.Message`를 `GetPromptResult.Messages([]*PromptMessage)`로 변환하는 SDK `PromptHandler`를 `server.AddPrompt`로
등록. `ServeStdio(ctx)` → `server.Run(ctx, &StdioTransport{})`. SPEC §5.5.

### 루프백 e2e 데이터 경로

같은 프로세스에서 `Server`가 도구·프롬프트를 등록하고, `NewInMemoryTransports()`로 만든 한 쌍을 서버·클라이언트에
각각 물려 `ListTools → CallTool → LoadPrompt` 왕복을 검증한다. 외부 프로세스·네트워크·키 없음. 단, spec이
"stdio로 접속"을 명시하므로 stdio 전송 경로 자체도 결정적으로 태운다(§5 D5). SPEC §5.6.

## 3. 인터페이스

§21 API 표면(SDK 위 래퍼). 정확한 메서드 시그니처는 D1에서 SDK 타입 누수 없이 commit한다.

- `type Transport string`; `const TransportStdio Transport = "stdio"`.
- `type ServerConfig struct { Transport Transport; Command string; Args []string; URL string }`.
- `type Client struct{...}` — 필드는 비공개(SDK `*Client`/`*ClientSession` 보관). 메서드: `Connect(ctx) error`,
  `Initialize(ctx) error`(또는 Connect 흡수), `ListTools(ctx) ([]tool.Schema, error)`, `CallTool(ctx, name string,
  args json.RawMessage) (tool.Result, error)`, `LoadPrompt(ctx, name string, args map[string]string)
  ([]message.Message, error)`, `Close() error`.
- `type MultiServerClient struct{...}`; `NewMultiServerClient(...)`; `GetTools(ctx) ([]tool.Tool, error)`,
  `GetToolsByServer(ctx, server string) ([]tool.Tool, error)`, `GetPrompt(ctx, server, name string, args ...)
  ([]message.Message, error)`. 미등록 server는 명시적 not-found 에러(센티널/타입 기반, §5 D6).
- 어댑터: `RemoteTool`(`tool.Tool` 구현), `WrapMCPTool(client *Client, s tool.Schema) tool.Tool`,
  `LoadMCPTools(ctx, client *Client) ([]tool.Tool, error)`, `LoadMCPPrompt(ctx, client *Client, name string,
  args ...) ([]message.Message, error)`.
- 서버: `type Server struct{...}`; `NewServer(name string) *Server`; `RegisterTool(t tool.Tool) error`,
  `RegisterTools(reg *tool.Registry) error`, `RegisterPrompt(name string, msgs []message.Message) error`(또는
  핸들러 형태, §5 D3), `ServeStdio(ctx) error`.

스키마 매핑(양방향):

- MCP → tool: `Tool.Name/Description` → `Schema.Name/Description`. `Tool.InputSchema(*jsonschema.Schema)`의
  properties를 순회해 각 프로퍼티명→`Parameter.Name`, JSON Schema type→`Parameter.Type`, description→
  `Parameter.Description`, `required` 목록 포함 여부→`Parameter.Required`. SPEC §5.2.
- tool → MCP: `Schema.Parameters`를 `*jsonschema.Schema`(type=object, properties + required 배열)로 빌드해
  `Tool.InputSchema`에 싣는다. `jsonschema-go` 빌더 API는 D2에서 확정. SPEC §5.5.

결과 매핑: `CallToolResult.Content`(여러 `TextContent`) → `tool.Result.Content`(텍스트 연결), `IsError` 그대로.
역방향(서버): `tool.Result` → 단일 `TextContent` + `IsError`. SPEC §5.2·§5.5.

프롬프트 매핑: `PromptMessage{Role,Content}` ↔ `message.Message`. role `user`↔`RoleUser`, `assistant`↔
`RoleAssistant`, 콘텐츠는 `TextContent.Text` ↔ `Message.Content`. SPEC §5.3·§5.5.

## 4. 영향 범위

- 신규: `mcp` 패키지(소스 파일 + 단위 테스트 + import 경계 테스트 + 루프백 e2e 테스트). 기존 파일 수정 없음이 원칙.
- `go.mod`/`go.sum`: MCP Go SDK(`github.com/modelcontextprotocol/go-sdk`)와 그 전이 의존(`github.com/google/
  jsonschema-go` 등)이 추가된다. 이는 코드 수정이 아니라 의존성 추가다. SPEC §5.1.
- `tool`·`message`는 **재사용만** 한다. `tool.Tool`/`Schema`/`Result`/`Parameter`/`Registry`, `message.Message`와
  생성자가 모두 `mcp`가 필요로 하는 표면을 이미 공개(export)하고 있어, 두 패키지에 신규 export나 시그니처 변경이
  필요 없음을 직접 읽어 확인했다(§근거). SPEC §5.1·§5.7.
- README §21/§24/§26·§28-1은 이 Phase에서 본문 수정하지 않는다(spec §4: 문서 정리·`config.LoadMCPServers`·
  `streamable_http` 제외). feature README 상태·작업 히스토리 갱신은 main이 수행.
- 회귀 위험은 신규 의존 추가로 인한 전역 빌드 영향뿐이며, `go build ./...`/`go vet ./...`로 가드한다. SPEC §5.1.

## 5. Decision Points

### D1. §21 API ↔ SDK 래핑 경계 (SPEC §5.2·§5.4·§5.5)

채택: SDK 타입(`*Client`/`*ClientSession`/`*Server`/`*Tool`/`CallToolResult`/`PromptMessage`/전송)을 우리 구조체의
비공개 필드로만 보관하고, 공개 표면은 §21 이름과 `tool`/`message` 타입만으로 구성한다. 위임/어댑터 구분:

- SDK 호출 단순 위임: `Client.Connect/Close`(→ `sdkClient.Connect`/`session.Close`), `ServeStdio`(→ `server.Run`).
- 우리 어댑터 로직: `ListTools`(스키마 변환), `CallTool`(Result 변환), `LoadPrompt`(메시지 변환), `RegisterTool`
  (Schema 역변환 + 핸들러 합성), `RegisterPrompt`(메시지 역변환). `Initialize`는 SDK가 Connect에서 핸드셰이크하므로
  별도 RPC가 없으면 Connect에 흡수하거나 no-op 보장 메서드로 둔다 — 구현 시 `go doc`로 확정.

근거: spec §3이 "SDK 타입 그대로 노출 금지, §21 이름·계약으로 감싼다"를 명시. 다운스트림이 SDK 버전 변화에서 격리됨.

### D2. MCP 도구 스키마 ↔ tool.Schema/tool.Tool 매핑 (SPEC §5.2·§5.3·§5.5)

채택: §3의 양방향 매핑을 단일 변환 함수로 둔다. MCP→tool은 `*jsonschema.Schema`의 properties/required를 평탄하게
순회해 `[]tool.Parameter`로(중첩 object는 type=`object`로만 표기, 깊은 재귀 분해는 하지 않음 — `tool.Parameter`가
평면 구조이므로). tool→MCP는 `tool.Parameter`로 type=object jsonschema를 빌드. `RemoteTool`은 원격 호출을
`Execute`로 노출하는 `tool.Tool` 구현체이며 `rt`를 사용하지 않는다.

미확정→implement: `jsonschema-go`의 정확한 빌더/접근 API. `go doc github.com/google/jsonschema-go/jsonschema`로
확정한다. 이는 매핑 골격(필드 대응)이 확정된 위에서의 저수준 호출 형식이라 설계를 막지 않는다.

### D3. MCP 프롬프트 ↔ []message.Message (SPEC §5.3·§5.5)

채택: role 매핑 `user↔RoleUser`, `assistant↔RoleAssistant`. 콘텐츠는 `TextContent`만 지원(이미지/오디오/리소스
콘텐츠는 이 Phase 범위에서 텍스트화하거나 무시 — spec §1의 도구·프롬프트 텍스트 흐름에 한정). 서버측
`RegisterPrompt`는 인자 없는 정적 메시지 목록을 기본으로 하고, 인자 치환이 필요하면 핸들러 형태로 확장한다(구현 시
spec 범위 내 최소형 채택).

근거: `message.Role`에 system/tool도 있으나 MCP 프롬프트 역할은 user/assistant뿐이므로 2-way로 충분.

### D4. 서버측 tool.Tool/Registry → SDK 서버 등록 (SPEC §5.5)

채택: untyped `server.AddTool(t *Tool, h ToolHandler)`를 사용한다(제네릭 `AddTool[In,Out]`은 컴파일 타임 Go 타입을
요구해 런타임 `tool.Tool` 집합 등록에 부적합). 핸들러는 들어온 `Arguments`를 `json.RawMessage`로 직렬화해
`tool.Tool.Execute`에 위임하고, 서버측 `Runtime`은 상태·스토어 없는 최소 no-op(`tool.NewRuntime(nil, "", cfg, nil,
nil)`)으로 합성한다. `InputSchema`는 D2 역변환으로 채운다.

근거: 도구가 런타임 컬렉션(`tool.Registry`)으로 주어지므로 제네릭 타입 추론을 쓸 수 없다. untyped 경로가 spec의
`RegisterTools(tool.Registry)` 계약과 맞는다.

### D5. stdio 루프백 e2e 구성 (SPEC §5.6)

채택: 결정적·자족 검증을 위해 **두 경로**를 모두 둔다.

- 주 경로(인메모리): `mcp.NewInMemoryTransports()` 쌍으로 같은 프로세스 내 서버↔클라이언트를 연결해 `ListTools→
  CallTool→LoadPrompt` 왕복을 검증. 서브프로세스·바이너리 빌드 불필요, 가장 결정적.
- stdio 실증 경로: spec이 "stdio로 접속"을 명시하므로, 같은 프로세스에서 `os.Pipe`(또는 SDK가 제공하는 Reader/Writer
  전송)로 서버 `StdioTransport`와 클라이언트를 잇는 루프백을 추가해 stdio 전송 코드 경로 자체를 한 번 태운다. SDK
  `IOTransport`(Reader/Writer) 제공 여부를 implement에서 `go doc`로 확인해, 제공되면 그것으로, 아니면 헬퍼 서버
  바이너리 대신 인메모리로 stdio 요구를 충족한 것으로 본다.

skip 가드 없음: 외부 의존이 없어 항상 실행 가능하므로 `vectorstore`/`store` e2e의 `t.Skip` 가드를 두지 않는다.

근거: SDK가 인메모리·stdio 전송을 모두 제공함을 확인(§근거). 인메모리가 결정성·단순성에서 우월하고, stdio 경로는
spec 요구를 코드로 실증한다. 둘 다 동일 프로세스라 외부 프로세스·키 불요. 이 결정으로 main 위임이 불필요해졌다.

### D6. 멀티서버 도구 통합·서버별 조회·미등록 에러 (SPEC §5.4)

채택: 서버 이름→`*Client` 맵으로 보관(연결 순서 보존을 위해 이름 순서 슬라이스 병행). `GetTools`는 등록 순서대로 각
서버 도구를 이어 붙여 통합 반환(이름 충돌 시 그대로 둠 — 호출자가 서버 prefix로 구분, spec이 충돌 정책을 요구하지
않음). `GetToolsByServer`/`GetPrompt`의 미등록 server 이름은 전용 not-found 에러(예: `IsServerNotFound(err)` 판정
가능한 센티널/타입)로 구분한다.

근거: spec §5.4가 "등록되지 않은 서버 이름은 not-found/에러로 구분"을 명시.

### D7. ServerConfig 소유·전송 한정 (SPEC §5.1)

채택: `ServerConfig{Transport, Command, Args, URL}`·`Transport`는 `mcp`가 소유. 이 Phase는 `Transport=stdio`만
구현하고, `streamable_http`/`URL` 경로는 미구현(향후). `config.LoadMCPServers`는 만들지 않는다(Phase 7).

근거: spec §3·§4가 소유·전송 범위를 못박음.

### D8. import 경계·검증 전략 (SPEC §5.1·§5.7)

채택: `mcp`는 `tool`/`message`/`core` + MCP SDK + 표준 라이브러리만 import. 검증 3종:

- import 경계 테스트: `vectorstore/import_boundary_test.go`와 동일한 `go list -deps` 패턴으로 `mcp` deps에
  `tool`/`message` 포함, `agent`/`graph`/`prebuilt` 등 상위 미포함을 단언. SPEC §5.7.
- 단위 테스트: 스키마·결과·프롬프트 매핑 변환 함수 단위 검증(SDK 없이도 변환 로직 직접 검증 가능한 부분).
- 루프백 e2e(D5): `ListTools→CallTool→LoadPrompt` 왕복. SPEC §5.6.
- 전역 빌드/정적검사: `go build ./...`·`go vet ./...`. SPEC §5.1.

근거: 기존 Phase 회귀 보호 관행을 그대로 따른다.

미확정→implement: SDK require 정확 버전 핀(`go get`으로 확정). 핵심 API 존재는 확인됐으므로 설계 비차단.
