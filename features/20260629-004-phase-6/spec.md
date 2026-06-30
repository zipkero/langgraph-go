# phase-6 — MCP

## 1. 범위

langgraph-go의 MCP(Model Context Protocol) 연동을 담당하는 Phase 6다(README §26 Phase 6, §21). 신규 `mcp`
패키지 하나를 다루며, 공식 MCP Go SDK를 래핑해 README §21의 API를 stdio 전송 기준으로 제공한다.

- 클라이언트(단일) — 외부 MCP 서버에 연결해 초기화하고, 원격 도구 목록·호출, 프롬프트 로딩, 종료를 수행한다
  (`Client`, `Connect`/`Initialize`/`ListTools`/`CallTool`/`LoadPrompt`/`Close`).
- 멀티서버 클라이언트 — 여러 MCP 서버를 묶어 도구를 통합/서버별로 조회하고 프롬프트를 가져온다
  (`MultiServerClient`, `GetTools`/`GetToolsByServer`/`GetPrompt`).
- 도구·프롬프트 어댑터 — MCP 원격 도구를 기존 `tool.Tool`로, MCP 프롬프트를 `message.Message`로 변환한다
  (`LoadMCPTools`/`LoadMCPPrompt`/`WrapMCPTool`, `RemoteTool`).
- 서버 — 기존 `tool.Tool`/`tool.Registry`와 프롬프트를 MCP 프로토콜로 노출한다(`Server`, `NewServer`,
  `RegisterTool`/`RegisterTools`/`RegisterPrompt`, `ServeStdio`).
- 설정 타입 — `ServerConfig`(Transport, Command, Args, URL), `Transport`. 이 Phase의 전송은 `stdio`다.

이 패키지는 Phase 1의 `tool`·`message` primitive 위에 얹는 연동 계층이다. README §21이 서버를 "Go에 대응물이
없어 직접 제공한다"고 적은 것과 달리, 이 작업은 공식 MCP Go SDK를 의존성으로 두고 §21 API로 래핑한다(사용자
결정). 그에 따른 README 본문 정리는 이 Phase 범위 밖이다(§4).

## 2. 목표

다운스트림이 MCP 서버의 원격 도구·프롬프트를 기존 `tool.Tool`/`message.Message`로 받아 에이전트·그래프에서
그대로 쓸 수 있게 하고, 반대로 자신의 `tool.Tool`/`tool.Registry`를 MCP 서버로 노출할 수 있게 한다. 멀티에이전트
/오케스트레이션 시나리오에서 외부 도구(예: Tavily MCP)를 표준 프로토콜로 끌어오는 토대를 제공한다. 도구·프롬프트
표현은 Phase 1의 `tool`·`message` 타입을 재사용한다.

## 3. 제약

- 공식 MCP Go SDK(`modelcontextprotocol/go-sdk`)를 의존성으로 사용하고, README §21의 API 표면(`Client`/
  `MultiServerClient`/`Server`/어댑터/`ServerConfig`)으로 래핑한다(사용자 결정). SDK 타입을 그대로 노출하지 않고
  §21 이름·계약으로 감싼다. 정확한 import 경로·버전은 analysis에서 확정한다.
- 이 Phase의 전송은 `stdio`만 구현한다. `streamable_http`는 보류한다(§4).
- import 경계(README §28-1, 상위→하위 단방향): `mcp`는 `tool`(Tool/Schema/Result/Registry)·`message`·`core`와
  MCP SDK·표준 라이브러리를 import한다. `agent`·`graph` 등 상위 패키지를 import하지 않으며, 하위 패키지가 `mcp`를
  역참조하지 않는다. `ServerConfig`는 `mcp`가 소유한다. `config.LoadMCPServers`(config→중립 맵) 어셈블리는 Phase 7
  소관이라 이 Phase에서 다루지 않는다(§4).
- Phase 1~5 패키지의 기존 타입·동작(`tool.Tool`/`tool.Schema`/`tool.Result`/`tool.Registry`, `message.Message`
  등)은 변경하지 않는다. mcp에 필요한 새 타입은 `mcp` 패키지 안에 둔다.
- 외부 MCP 서버·네트워크에 의존하지 않고 결정적으로 검증할 수 있어야 한다. 검증은 이 패키지의 `Server`가 노출한
  도구·프롬프트에 같은 패키지의 `Client`가 접속해 왕복하는 인패키지 루프백으로 수행한다(외부 프로세스/키 불요).

## 4. 제외 범위

- `streamable_http` 전송은 제외한다(후속 Phase). 이 Phase는 `stdio` 전송만 구현한다.
- `config.LoadMCPServers` 및 `config`의 MCP 어셈블리(중립 맵 → `mcp.ServerConfig` 변환)는 제외한다(Phase 7,
  README §24·§28-1). `config`를 leaf로 유지하기 위한 이 경로는 Phase 7에서 다룬다.
- `search` 모듈의 Tavily MCP 경유 사용(README §18)은 제외한다. 이 Phase는 MCP 연동 메커니즘만 제공하고, 특정
  외부 서버 연동은 다루지 않는다.
- 실제 외부 MCP 서버(npx 기반 공개 서버, Tavily MCP 등) 대상의 e2e는 제외한다. 검증은 인패키지 루프백으로 한다.
- README §21의 "Go에 대응물이 없어 직접 제공한다" 등 표기를 SDK 래핑 산출에 맞게 고치는 문서 정리는 제외한다
  (후속 문서 작업).
- Phase 6 외 패키지(`a2a`·`database`·`search`·`storage`·`trace`, Phase 7)는 범위 밖이다.

## 5. 완료 조건

1. 모듈 루트에서 `go build ./...`와 `go vet ./...`가 오류 없이 끝난다(`mcp` 신규 패키지와 MCP SDK 의존성이
   추가되고 Phase 1~5 패키지의 기존 동작은 변경되지 않은 상태).
2. 단일 클라이언트가 stdio MCP 서버에 연결·초기화한 뒤, `ListTools`가 원격 도구를 `[]tool.Schema`로 반환하고,
   `CallTool(name, args)`가 원격 실행 결과를 `tool.Result`로 반환하며, `LoadPrompt(name, args)`가 프롬프트를
   `[]message.Message`로 반환하고, `Close`로 연결을 정리한다.
3. 어댑터가 동작한다: `LoadMCPTools`가 원격 도구를 `[]tool.Tool`로 반환하고 그 도구의 `Execute`가 원격 호출로
   결과를 내며, `WrapMCPTool`이 단일 스키마를 `tool.Tool`로 감싸고, `LoadMCPPrompt`가 프롬프트를
   `[]message.Message`로 반환한다.
4. 멀티서버 클라이언트가 동작한다: `NewMultiServerClient(servers)`로 여러 서버를 묶어 `GetTools`가 전체 도구를
   통합 반환하고, `GetToolsByServer(server)`가 서버별 도구를, `GetPrompt(server, name, args)`가 해당 서버의
   프롬프트를 반환한다. 등록되지 않은 서버 이름은 not-found/에러로 구분된다.
5. 서버가 동작한다: `NewServer(name)`에 `RegisterTool`/`RegisterTools(tool.Registry)`로 도구를,
   `RegisterPrompt`로 프롬프트를 등록하면 `ServeStdio`로 노출되며, 접속한 클라이언트가 그 도구를 목록 조회·호출하고
   프롬프트를 가져올 수 있다.
6. 인패키지 루프백 e2e가 동작한다: 이 패키지의 `Server`가 도구·프롬프트를 노출하고 같은 패키지의 `Client`가 stdio로
   접속해 `ListTools`→`CallTool`→`LoadPrompt` 왕복이 기대 결과를 내며 통과한다. 외부 프로세스·네트워크·키 없이
   결정적으로 실행된다.
7. import 그래프 검사로 `mcp`가 `tool`·`message`·`core`(및 MCP SDK) 등만 import하고 `agent`·`graph`를 import하지
   않으며, 하위 패키지가 `mcp`를 역참조하지 않음을 확인할 수 있다. Phase 1~5 패키지는 기존 동작이 수정되지 않는다.
