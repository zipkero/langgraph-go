# a2a — Agent-to-Agent 프로토콜

Phase 7을 패키지별 feature로 분할한 첫 산출물이다(README §26 Phase 7, §22). 이 feature는 `a2a` 패키지
하나를 다룬다. 같은 Phase의 나머지 패키지(trace, config 어셈블리, database, search, storage)는 별도 feature로
진행한다.

## 1. 범위

신규 `a2a` 패키지를 구현한다. A2A의 JSON-RPC 2.0 over HTTP(+SSE 스트리밍) 프로토콜을 net/http로 직접 구현하고,
README §22의 API 표면을 제공한다(사용자 결정: 공식 SDK 미사용, 직접 구현).

- 프로토콜 타입(§22-1) — `AgentCard`/`AgentSkill`/`AgentCapabilities`, `Task`/`TaskStatus`/`TaskState`,
  `Message`(Role/Parts)·`Part`(TextPart/DataPart/FilePart union, `kind` 판별), `Artifact`,
  `MessageSendParams`/`SendMessageRequest`, 스트리밍 이벤트(`TaskStatusUpdateEvent`/`TaskArtifactUpdateEvent`/
  `Event`). a2a 고유 타입이며 `message` 패키지의 `Message`와 별개다.
- 서버(§22-2) — `AgentExecutor`(Execute/Cancel), `RequestContext`, `EventQueue`, `TaskUpdater`(UpdateStatus/
  AddArtifact/Complete/Cancel), `TaskStore`/`InMemoryTaskStore`, `RequestHandler`/`DefaultRequestHandler`,
  `NewServer`(AgentCard를 `/.well-known/agent-card.json`에 노출)·`Run`, 헬퍼(`NewTask`/`NewAgentTextMessage`).
- 클라이언트(§22-3) — `CardResolver`(GetAgentCard), `Client`(SendMessage/SendMessageStreaming), 아티팩트 추출
  헬퍼(`ArtifactText`/`ArtifactData`/`ArtifactFileURI`/`ArtifactFileBytes`).
- LangGraph 에이전트 어댑터(§22-4) — `StreamToTaskUpdates(ctx, a *agent.Agent, query, sessionID string,
  u *TaskUpdater) error`. 에이전트 실행을 태스크 수명주기로 매핑한다.

## 2. 목표

다운스트림이 LangGraph 에이전트를 표준 A2A 서버로 노출하고, 다른 A2A 에이전트를 클라이언트로 호출할 수 있게
한다. 에이전트 실행(도구 호출 루프·구조화 응답)을 A2A 태스크 상태(working/input_required/completed/failed/
canceled)와 아티팩트로 변환해, 오케스트레이션(§23, 응용 계층)이 이 위에 직접 구성될 토대를 제공한다.

## 3. 제약

- A2A 프로토콜을 net/http와 encoding/json으로 직접 구현한다. 공식 a2a-go SDK나 gRPC/protobuf 의존성을 추가하지
  않는다(사용자 결정). 전송은 HTTP 위 JSON-RPC 2.0과 SSE(스트리밍)만 다룬다.
- 와이어 포맷은 README §22 명세를 따른다: `Message`/`Part`는 `kind` 문자열(`message`/`text`/`data`/`file`)로
  union을 판별하고, `TaskState`는 `working`/`input_required`/`completed`/`failed`/`canceled` 값을 쓴다.
- import 경계(README §28-1, 상위→하위 단방향): `a2a`는 상위 응용 계층으로 `agent`·`config`·`structured`·
  `message`·`tool`·`core`와 표준 라이브러리를 import한다. 하위 패키지(특히 `agent`)가 `a2a`를 역참조하지 않는다.
  `a2a`는 자체 `Message`/`Part` 타입을 정의하며 `message.Message`를 재사용하지 않는다.
- `config.AgentConfig`는 재사용하되, `config`의 어셈블리 함수(`AgentURLs`/`GetAgentConfig`)는 별도 Phase 7
  feature 소관이라 이 feature에서 추가하지 않는다(§4).
- Phase 0~6 패키지의 기존 타입·동작(`agent.Agent`/`AgentEvent`, `structured.AgentStatus`, `config.AgentConfig`
  등)은 변경하지 않는다. a2a에 필요한 새 타입은 `a2a` 패키지 안에 둔다.
- 외부 네트워크·실행 중인 원격 에이전트에 의존하지 않고 결정적으로 검증할 수 있어야 한다. 서버↔클라이언트
  검증은 `net/http/httptest` 기반 인패키지 루프백(같은 프로세스)으로 수행한다.

## 4. 제외 범위

- `orchestrator`(README §23, 인텐트/플랜/실행·원격 호출·결과 통합)는 제외한다. 응용 계층이며 다운스트림이 A2A
  클라이언트 위에 직접 구현한다.
- gRPC·REST(HTTP+JSON) 전송은 제외한다. 이 feature는 JSON-RPC 2.0 over HTTP + SSE만 구현한다.
- push notification·인증/인가·멀티테넌트·클러스터 모드 등 §22에 없는 A2A 고급 기능은 제외한다.
- `config`의 어셈블리 함수(`AgentURLs`/`GetAgentConfig`/`LoadMCPServers`)는 제외한다(별도 Phase 7 config feature).
- 공식 a2a-go SDK·gRPC·protobuf 의존성 추가는 제외한다(직접 구현).
- Phase 7의 다른 패키지(`trace`·`database`·`search`·`storage`, `vectorstore.SupabaseVectorStore`)는 이 feature
  범위 밖이며 각각 별도 feature로 다룬다.

## 5. 완료 조건

1. 모듈 루트에서 `go build ./...`와 `go vet ./...`가 오류 없이 끝난다(`a2a` 신규 패키지가 추가되고 Phase 0~6
   패키지의 기존 동작은 변경되지 않은 상태). a2a 의존 그래프에 gRPC/protobuf·외부 a2a SDK가 없다.
2. 프로토콜 타입의 JSON 와이어 포맷이 명세와 맞는다: `Message`/`Part`(TextPart/DataPart/FilePart)가 `kind`
   판별자와 함께 직렬화·역직렬화되고, `Task`/`TaskStatus`/`TaskState`(working/input_required/completed/failed/
   canceled)·`Artifact`가 round-trip(marshal→unmarshal)에서 값이 보존된다.
3. 서버가 동작한다: `AgentExecutor`를 받은 `DefaultRequestHandler`/`NewServer`로 서버를 구성해 띄우면,
   `/.well-known/agent-card.json`에서 `AgentCard`가 조회되고, JSON-RPC `SendMessage` 요청이 실행기를 호출해
   `Task`(상태·아티팩트 포함)를 응답으로 돌려준다.
4. 태스크 수명주기가 동작한다: 실행기 안에서 `TaskUpdater`로 `UpdateStatus`(working/input_required/completed/
   failed/canceled)·`AddArtifact`·`Complete`·`Cancel`을 호출하면 그 전이·아티팩트가 태스크 상태와 스트리밍
   이벤트(`TaskStatusUpdateEvent`/`TaskArtifactUpdateEvent`)에 반영된다. `TaskStore`/`InMemoryTaskStore`로
   태스크를 저장·조회할 수 있다.
5. 클라이언트가 동작한다: `CardResolver.GetAgentCard`가 `/.well-known/agent-card.json`에서 카드를 조회하고,
   `Client.SendMessage`가 원격 서버를 호출해 `Task`를 반환하며, `SendMessageStreaming`이 이벤트 채널을 반환하고,
   아티팩트 추출 헬퍼가 `Artifact`에서 텍스트/데이터/파일(URI·bytes)을 꺼낸다.
6. 에이전트 어댑터가 동작한다: `StreamToTaskUpdates`가 `agent.Agent` 실행 스트림을 소비해 `AgentEvent`의
   `IsTaskComplete`/`RequireUserInput`/`Content`(기본 경로) 또는 `structured.AgentStatus`(대체 경로)를 태스크
   상태 전이와 아티팩트로 매핑한다: 진행 중→working, 추가 입력 필요→input_required(final), 완료→AddArtifact+
   Complete, 실행기 예외→failed, 취소→canceled.
7. 인패키지 루프백 e2e가 동작한다: 같은 패키지의 서버(httptest)를 띄우고 같은 패키지의 클라이언트가 접속해
   카드 조회→SendMessage(및 스트리밍)→태스크/아티팩트 수신 왕복이 기대 결과를 내며, 외부 네트워크·원격
   에이전트·키 없이 결정적으로 실행된다.
8. import 그래프 검사로 `a2a`가 `agent`·`config`·`structured`·`message`·`tool`·`core` 등 허용 패키지만 import
   하고, 하위 패키지(특히 `agent`)가 `a2a`를 역참조하지 않음을 확인할 수 있다. Phase 0~6 패키지는 기존 동작이
   수정되지 않는다.
