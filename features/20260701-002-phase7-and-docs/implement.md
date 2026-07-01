# phase7-and-docs — 구현 체크리스트

이 문서는 spec.md §5(완료 조건 7개)와 analysis.md(구조·데이터 흐름·인터페이스·Decision Points D1~D7)를 실행 단위로
옮긴 순수 실행 체크리스트다. 설계 근거는 analysis.md에 있고, 요구사항 레벨 완료 조건은 spec.md §5에 있다. 순서는
의존(다음이 가능하려면 무엇이 먼저 존재해야 하는가)을 위치로 표현한다. SupabaseVectorStore는 database의 클라이언트
계약이 있어야 하므로 database를 앞에 두고, README 정합은 신규 구현 반영분이 코드 완료에 의존하므로 맨 뒤에 둔다.
그 사이의 search·storage·config·mcp HTTP는 서로 독립이다.

## Section: 관계형/벡터 DB 접근

- [ ] task-001: database 클라이언트와 DB 접근 도구
  - 목적: 호출자가 database 클라이언트로 웹콘텐츠·문서청크를 삽입하고, 웹콘텐츠를 제목 전문검색으로 조회하고,
    문서를 임베딩 유사도로 매칭하고, eq/ilike/gte/lte/정렬/한도 필터로 문서를 질의할 수 있으며, 웹데이터 저장·검색이
    도구로도 노출된다. 크리덴셜 없는 환경에서는 실제 DB 통합 테스트가 skip되고 빌드·단위 테스트는 통과한다.
  - 접근: pgx+pgvector-go 백엔드로 Client 인터페이스(Connect/Close/InsertWebContent/SearchWebContent/
    InsertDocumentChunks/MatchDocuments/QueryDocuments)를 구현하고, match_documents 등 스키마·RPC는 이미 존재한다고
    가정해 호출만 한다. tool 래핑으로 저장·검색 도구를 노출한다.
  - 검증 조건:
    - 결과: 클라이언트가 삽입·전문검색·유사도매칭·필터질의 메서드와 웹데이터 저장·검색 도구를 노출하고, 도구는
      구조체 태그에서 도출된 스키마를 가진다. 실제 DB 크리덴셜이 없으면 통합 테스트는 skip하고, 단위 테스트와
      `go build ./...`는 통과한다.
    - 확인: 크리덴셜/DB 부재 시 skip하는 통합 테스트와 그와 무관하게 통과하는 단위 테스트가 있고, database가
      tool·표준 라이브러리·DB 드라이버에만 의존하고 상위(vectorstore/rag)를 역참조하지 않음을 go/build 정적 파싱
      import 경계 테스트가 단정하며, go.mod에 pgx·pgvector-go require가 추가된 상태로 `go build ./...`와 기존
      테스트가 통과한다.
  - 참조: SPEC §5.1 / ANALYSIS §1.1, ANALYSIS §2.1, ANALYSIS §2.2, ANALYSIS §5 D1, ANALYSIS §5 D2

## Section: 웹 검색

- [ ] task-002: search 클라이언트와 웹검색·본문로딩 도구
  - 목적: 호출자가 search 클라이언트로 웹 검색을 수행해 제목·URL·본문·점수를 담은 결과를 받고, 검색과 URL 본문
    로딩이 도구로 노출되며 응답에서 URL을 추출할 수 있다. 크리덴셜 없는 환경에서 통합 테스트는 skip되고 빌드·단위
    테스트는 통과한다.
  - 접근: SearchClient가 Tavily REST를 net/http로 직접 호출하고, 웹 본문 로딩은 기존 document 웹 로더를 재사용해
    상위→하위 단방향으로 쓴다. SearchTool·WebContentLoaderTool·ExtractURLs를 노출한다.
  - 검증 조건:
    - 결과: 클라이언트가 검색 메서드와 검색·본문로딩 도구·URL 추출을 노출하고, HTTP 응답 파싱을 httptest/stub으로
      검증할 수 있다. API 키가 없으면 실서비스 통합 테스트는 skip하고, 단위 테스트와 `go build ./...`는 통과한다.
    - 확인: 키 부재 시 skip하는 통합 테스트와 stub/httptest 기반 단위 테스트가 있고, search가 tool·(선택)document·
      표준 라이브러리·외부 HTTP에만 의존하고 상위를 역참조하지 않음을 go/build 정적 파싱 import 경계 테스트가
      단정하며, `go build ./...`와 기존 테스트가 통과한다.
  - 참조: SPEC §5.2 / ANALYSIS §1.2, ANALYSIS §2.1

## Section: 파일 스토리지

- [ ] task-003: storage 클라이언트와 파일 조작 도구
  - 목적: 호출자가 storage 클라이언트로 외부 파일 스토리지에 업로드·다운로드·목록·정보조회·수정·삭제·폴더 조작을
    수행하고, storage_ref로 파일을 지칭·해소할 수 있으며, 파일 조작 8종이 도구로 노출된다. 크리덴셜 없는 환경에서
    통합 테스트는 skip되고 빌드·단위 테스트는 통과한다.
  - 접근: Google Drive(google.golang.org/api/drive/v3 + oauth2) 백엔드로 Client 인터페이스와 FileMetadata 등
    타입을 구현하고, OAuth credentials/token 파일 경로를 생성 인자로 주입받아 Initialize에서 로드·리프레시한다.
    CreateStorageRef/ParseStorageRef는 외부 의존 없는 순수 함수로 둔다.
  - 검증 조건:
    - 결과: 클라이언트가 업로드·다운로드·목록·폴더 메서드와 파일 조작 도구 8종을 노출하고, storage_ref 생성·파싱이
      왕복 보존된다. 인증 파일이 없으면 Drive 통합 테스트는 skip하고, storage_ref 순수 함수 단위 테스트와
      `go build ./...`는 통과한다.
    - 확인: 인증 파일 부재 시 skip하는 Drive 통합 테스트와 storage_ref 왕복을 단정하는 단위 테스트가 있고, storage가
      상위를 역참조하지 않음을 go/build 정적 파싱 import 경계 테스트가 단정하며, `go build ./...`와 기존 테스트가
      통과한다.
  - 참조: SPEC §5.3 / ANALYSIS §1.3, ANALYSIS §2.3, ANALYSIS §5 D3

## Section: config 어셈블리

- [ ] task-004: config MCP·에이전트 어셈블리 함수
  - 목적: 호출자가 config에서 MCP 서버 설정 맵과 에이전트 엔드포인트 맵·개별 에이전트 설정을 조립해 받을 수 있고,
    이때 config는 상위 패키지를 import하지 않는 leaf로 유지되며 중립 타입만 반환한다.
  - 접근: LoadMCPServers()·AgentURLs()·GetAgentConfig(name)을 config 자체 타입(ServerConfig/AgentConfig/기본 맵)만
    반환하도록 추가한다. config→mcp 변환 헬퍼(ServerConfigFromConfig 등)는 config가 아니라 mcp 쪽에 둔다.
  - 검증 조건:
    - 결과: 세 어셈블리 함수가 config 자체 타입을 반환하고, config가 mcp를 비롯한 상위 패키지를 import하지 않는다.
    - 확인: 어셈블리 함수 반환을 단정하는 단위 테스트가 통과하고, config가 leaf로 유지됨(상위 패키지 미import)을
      import 경계 테스트가 단정하며, `go build ./...`와 기존 테스트가 통과한다.
  - 참조: SPEC §5.4 / ANALYSIS §1.4, ANALYSIS §2.4, ANALYSIS §5 D7

## Section: MCP HTTP 스트리밍 전송

- [ ] task-005: mcp HTTP 스트리밍 전송(클라이언트·서버)
  - 목적: 호출자가 mcp 클라이언트를 HTTP 스트리밍 전송으로 원격 서버에 연결해 기존 도구·프롬프트 조회·호출을
    사용할 수 있고, 서버를 HTTP로 띄워 등록된 도구·프롬프트를 노출할 수 있으며, 기존 stdio 전송의 동작·시그니처는
    변경 전후 동일하다.
  - 접근: TransportStreamableHTTP 상수를 추가하고 Client.Connect switch에 URL 기반 StreamableClientTransport 분기를
    더하며(URL 비면 에러), Server.ServeStreamableHTTP(ctx, addr)를 NewStreamableHTTPHandler + http.Server로 두고
    ctx 취소 시 graceful shutdown한다. 기존 stdio 경로는 그대로 둔다(additive).
  - 검증 조건:
    - 결과: 전송 종류에 HTTP 스트리밍이 포함되고, HTTP 서버로 띄운 mcp가 도구·프롬프트를 제공하며 HTTP 클라이언트가
      이를 조회·호출한다. 기존 stdio Connect·Serve 시그니처와 동작은 불변이다.
    - 확인: HTTP 전송의 서버 노출·클라이언트 왕복을 테스트 서버로 단정하는 테스트가 있고, 기존 stdio 테스트가 그대로
      통과하며, `go build ./...`와 기존 테스트가 통과한다.
  - 참조: SPEC §5.6 / ANALYSIS §1.6, ANALYSIS §2.5

## Section: 외부 벡터 백엔드

- [ ] task-006: vectorstore SupabaseVectorStore
  - 목적: 호출자가 database 클라이언트에 위임하는 외부(pgvector) 벡터 백엔드를 생성해, 인메모리 백엔드와 동일한
    retriever 계약으로 질의 임베딩 기반 유사도 검색을 수행할 수 있다. 크리덴셜 없는 환경에서 통합 테스트는 skip되고
    빌드·단위 테스트는 통과한다.
  - 접근: NewSupabaseVectorStore(client database.Client, emb llm.EmbeddingClient, opts...)로 두고, MatchDocuments가
    질의 임베딩 후 database.MatchDocuments에 위임해 document.Document로 변환하며 pgvector 로직은 재구현하지 않는다.
    최소 Retriever 계약을 충족해 AsRetriever 경로로 쓰이게 한다.
  - 검증 조건:
    - 결과: SupabaseVectorStore가 database 클라이언트와 임베딩 클라이언트를 받아 Retriever 계약을 충족하고 유사도
      검색을 위임한다. 기존 InMemoryStore 시그니처는 불변이다. 실제 DB 크리덴셜이 없으면 통합 테스트는 skip하고,
      fake database.Client 주입 단위 테스트와 `go build ./...`는 통과한다.
    - 확인: fake database.Client를 주입해 위임·변환을 단정하는 단위 테스트(크리덴셜 부재 시 skip 통합 테스트 포함)가
      통과하고, `vectorstore/import_boundary_test.go`가 database import 허용·vectorstore→database 단방향(database가
      vectorstore 역참조 없음) 단정으로 갱신되며 go/build 정적 파싱 방식으로 전환된 상태로 통과하고, `go build ./...`와
      기존 테스트가 통과한다.
  - 참조: SPEC §5.5 / ANALYSIS §1.5, ANALYSIS §2.2, ANALYSIS §3, ANALYSIS §5 D4

## Section: README 정합

- [ ] task-007: README 드리프트 정정 및 신규 구현 반영
  - 목적: README만 보고도 실제 동작을 정확히 파악할 수 있도록, 임베딩 프로바이더·제외된 시각화·로컬 벡터 백엔드·
    multiagent 계층 서술·제거된 필드의 드리프트를 실제 상태에 맞게 정정하고, 이번에 새로 구현된 외부연동 항목이
    명세 서술과 부합하게 반영한다.
  - 접근: (a) 임베딩 서술을 OpenAI→Ollama로 정정(OpenAIAPIKey 필드는 유지, 문서만), (b) DrawMermaidPNG를 노출
    목록에서 제거, (c) Chroma 로컬 백엔드를 제외/미구현 표기, (d) multiagent를 라이브러리로 재분류(rag/orchestrator는
    응용 계층 유지), (e) 제거된 필드 정정, (f) 신규 database·search·storage·config 어셈블리·SupabaseVectorStore·
    mcp HTTP 전송을 실제 시그니처에 맞게 반영한다.
  - 검증 조건:
    - 결과: README (a)~(f)의 각 서술이 실제 코드 상태와 일치하고, 신규 구현된 타입·함수·전송이 README 서술과
      부합한다.
    - 확인: README의 각 정정 지점을 실제 코드 상태(struct 필드·함수 시그니처·전송 상수·패키지 계층)와 대조해
      불일치가 없음을 확인하고, `go build ./...`와 기존 테스트가 통과한다.
  - 참조: SPEC §5.7 / ANALYSIS §1.7, ANALYSIS §4, ANALYSIS §5 D5, ANALYSIS §5 D6, ANALYSIS §5 D7
