# phase7-and-docs — ANALYSIS

## 승인 전 확인
- 라이브러리(`database`)가 Supabase/pgvector의 테이블·인덱스·`match_documents` RPC 같은 DB 스키마를 소유·생성하지
  않고 "이미 존재한다"고 가정해 호출만 하는 것이 맞는가(DDL·마이그레이션은 운영/다운스트림 소유). 관련 본문:
  §5 D2

## 근거

조사 범위: 루트 `README.md`(§16·§18~§21·§24·§26 Phase 7·§28·§28-1), 기존 패턴 소스(`llm/embedding.go`,
`mcp/client.go`·`mcp/server.go`, `a2a/`(client·server·import_boundary_test), `vectorstore/`(vectorstore.go·
retriever_tool.go·import_boundary_test.go), `config/config.go`, `tool/tool.go`, `document/loader.go`), go.mod,
그리고 MCP Go SDK의 streamable HTTP API. 근거 표기: [확인]=코드/문서 직접 확인, [추정]=관례·SDK 시그니처 추론.

- 외부 HTTP는 SDK 없이 `net/http` 직접 호출이 관례다 [확인] — `llm/embedding.go`(Ollama /api/embed),
  `a2a/client.go`(JSON-RPC over HTTP). 단 pgvector는 바이너리 프로토콜이라 드라이버가 필요(→ §5 D1).
- 도구화는 `tool.WithArgsSchema`/`FromFunc`로 구조체 태그에서 스키마 도출 [확인] — `vectorstore/retriever_tool.go`.
- import 경계는 `go/build` 정적 파싱 테스트로 회귀 보호(서브프로세스 금지) [확인] — `a2a`/`multiagent`
  import_boundary_test. 단 `vectorstore/import_boundary_test.go`는 아직 `go list` 서브프로세스 방식이며 §5.5로 갱신
  필요.
- 통합 테스트는 크리덴셜/서버 도달 불가 시 `t.Skip`, 단위·e2e는 stub/httptest로 가드 없이 실행 [확인] —
  `llm/embedding_test.go`, `a2a/e2e_test.go`.
- `config`는 현재 leaf이고 `ServerConfig`/`AgentConfig` 타입은 있으나 어셈블리 함수는 미구현(주석 "Phase 7") [확인].
- 현 `vectorstore/import_boundary_test.go`가 `.../database` import를 **금지**로 단정 [확인] — §5.5에서 갱신 필수.
- MCP Go SDK가 streamable HTTP 양방향(`StreamableClientTransport`, `NewStreamableHTTPHandler`)을 제공 [확인].
- `mcp`는 이미 `config`를 참조 [확인](`config.RunConfig` 사용). `document`는 leaf이며 웹 로더 보유 [확인].
- README §14·§26·§27·트리는 `multiagent`를 "응용 계층(라이브러리 아님)"으로 서술하나 실제로 `multiagent/`는
  `graph`/`command`/`agent`/`tool`을 import하는 완전한 라이브러리 패키지로 구현됨 [확인] — 문서-구현 모순.
- README §24 주석·§26 Phase 3은 임베딩을 "OpenAI"로 서술하나 실제 `llm/embedding.go`는 Ollama 전용 [확인].
- `google.golang.org/api`·`golang.org/x/oauth2`는 go.mod에 이미 존재 [확인].

## 1. 구조 (완료 조건별 패키지 설계)

### 1.1 database (SPEC §5.1) — 신규 패키지
README §19가 1차 근거. 관계형/벡터 DB 접근과 도구화를 담당하는 신규 패키지다.

- `Client` 인터페이스: `Connect`/`Close`, `InsertWebContent`, `SearchWebContent`(title tsvector 전문검색),
  `InsertDocumentChunks`, `MatchDocuments(embedding []float32, count)`(RPC — 필터 인자 없음),
  `QueryDocuments(DocumentQuery)`(eq/ilike/gte/lte/order/limit).
- 레코드 타입: `WebContentRecord`, `DocumentRecord`, `DocumentQuery`. `document.Document`를 직접 참조하지 않고
  자체 레코드 타입 사용 [확인] README §19.
- 구체 구현체: pgx+pgvector-go 기반 Supabase/pgvector 백엔드(§5 D1). `Client` 인터페이스는 구현 방식과 무관하게 유지.
- 도구: `SaveWebDataTool`, `SearchWebDataTool`(README §19 명시). 필요 시 문서 검색 도구 추가 가능.
- 의존 방향: `tool`·표준 라이브러리·DB 드라이버에만 의존. 상위(`vectorstore`/`rag`) 미참조.
- MatchDocuments 단일 소유: pgvector 접근 로직의 유일 소유자는 `database.Client`이며 `vectorstore`는 위임만 한다
  [확인] README §16·§19. 이 경계가 §5.5를 규정.

### 1.2 search (SPEC §5.2) — 신규 패키지
README §18이 1차 근거.

- `SearchClient`, `SearchResult`(Title/URL/Content/Score).
- `NewSearchClient(apiKey)`, `Search(ctx, query, maxResults)` — Tavily REST를 net/http로 직접 호출.
- `SearchTool`, `WebContentLoaderTool`(URL 적재 후 본문 정리), `ExtractURLs`.
- 웹 본문 로딩은 기존 `document` 웹 로더 재사용(신규 의존 회피). `search → document`는 상위→하위 단방향이라 허용
  [확인] §28-1 규칙4.
- 의존 방향: `tool`·(선택)`document`·표준 라이브러리·외부 HTTP. 상위 미참조.

### 1.3 storage (SPEC §5.3) — 신규 패키지
README §20이 1차 근거.

- `Client` 인터페이스: `Initialize`/`Upload`/`DownloadAsBase64`/`DownloadByStorageRef`/`ListFiles`/`GetFileInfo`/
  `Update`/`Delete`/`FindFolderByName`/`CreateFolder`.
- 타입: `FileMetadata`/`FileContent`/`FolderMetadata`/`ListOptions`/`UploadOption`.
- storage_ref: `CreateStorageRef(fileID)`(`<scheme>://file/{id}`), `ParseStorageRef(ref)`. 외부 의존 없는 순수
  함수라 단위 테스트로 완전 커버.
- 도구 8종: Upload/Download/GetFileInfo/ListFiles/FindFolder/Delete/Update/CreateFolder.
- 백엔드: Google Drive(`google.golang.org/api/drive/v3` + `golang.org/x/oauth2`, 둘 다 go.mod 존재). 인증은 §5 D3.
- a2a 경계 테스트 무관 [확인]: `a2a`의 `google.golang.org/api` 금지는 a2a 의존 트리 국한. `storage`는 그 트리 밖.

### 1.4 config 어셈블리 함수 (SPEC §5.4) — 기존 config 확장
README §24가 1차 근거. leaf 유지한 채 함수 3개 추가.

- `LoadMCPServers() map[string]ServerConfig`(중립 타입), `AgentURLs() map[string]string`,
  `GetAgentConfig(name) (AgentConfig, error)`.
- leaf 유지 [확인] §28-1 규칙3: 반환 타입이 모두 `config` 자체 타입이라 상위 구체 타입을 반환하지 않는다. `config`는
  `mcp`를 import하지 않는다.
- 변환 소유: `config.ServerConfig → mcp.ServerConfig` 변환은 `mcp`가 소유(예: `mcp.ServerConfigFromConfig`). `mcp`는
  이미 `config`를 import하므로 자연스럽다.

### 1.5 vectorstore.SupabaseVectorStore (SPEC §5.5) — vectorstore 확장
README §16이 1차 근거. `database.Client`에 위임하는 외부 벡터 백엔드.

- `SupabaseVectorStore`, `NewSupabaseVectorStore(client database.Client, emb llm.EmbeddingClient, opts...)`(§5 D4),
  `MatchDocuments(queryEmbedding, count) ([]document.Document, error)` — `database.MatchDocuments` 위임 후
  `document.Document`로 변환. pgvector 로직 미재구현.
- retriever 계약: 최소 `Retriever` 계약을 충족해 인메모리 백엔드와 동일하게 `AsRetriever` 경로로 쓰인다(§5 D4).
- **import 경계 변경(중요)** [확인]: `vectorstore → database` 단방향 import가 새로 생기므로
  `vectorstore/import_boundary_test.go`의 "database 금지" 단정을 "database import 허용, database→vectorstore 역참조
  없음(단방향)"으로 갱신하고, 동시에 `go list` 서브프로세스 → `go/build` 정적 파싱 방식으로 전환한다. 이는 요구사항
  변경에 따른 테스트 갱신이지 공개 API 변경이 아니다.

### 1.6 mcp HTTP 스트리밍 전송 (SPEC §5.6) — mcp 확장
README §21이 1차 근거. 현재 stdio만 지원 [확인].

- `Transport` 상수 추가: `TransportStreamableHTTP`.
- `Client.Connect` switch에 분기 추가 → `StreamableClientTransport{Endpoint: c.cfg.URL}`. `URL` 비면 에러(stdio의
  Command 검증과 대칭).
- `Server.ServeStreamableHTTP(ctx, addr)` 추가 → `NewStreamableHTTPHandler` + `http.Server` 바인딩, ctx 취소 시
  graceful shutdown(a2a `Server.Run` 패턴 참조).
- 기존 `RegisterTool`/`RegisterPrompt` 등록물이 동일 `*mcp.Server` 공유로 HTTP에서도 그대로 노출.
- additive [확인] SPEC §3: 기존 stdio 시그니처 불변.

### 1.7 README 정합 (SPEC §5.7) — 문서 수정(코드 아님)
감사에서 실증한 드리프트를 실제 상태에 맞춘다. 코드 변경(§5.1~§5.6)과 문서 변경(§5.7)의 경계는 §4에 명시.

- (a) 임베딩 프로바이더: README(OpenAI) → 실제(Ollama)로 정정. `OpenAIAPIKey` 필드는 유지하고 문서만 정정(§5 D7).
- (b) `DrawMermaidPNG`: README 노출 목록에서 제거(실제 미구현, 코드 주석에 제외 명시).
- (c) Chroma 로컬 백엔드: README에서 "제외/미구현"으로 표기(SPEC §4가 제외 확정).
- (d) multiagent: README §14·§27·트리의 "라이브러리 아님" 서술을 실제(라이브러리 패키지)에 맞춰 재분류(§5 D5).
  rag/orchestrator는 미구현 응용 계층으로 유지.
- (e) 제거된 필드: 과거·이번 feature까지 제거된 타입 필드가 README 타입 절에 잔존하면 정정(구현 시 각 타입 struct와
  README 타입 절 대조).
- (f) §5.1~§5.6 신규 구현 반영: 신규 타입·함수·전송이 README 서술과 부합하도록 확인·보정. 구현 중 §28-1 경계로
  시그니처가 조정되면 해당 서술을 실제에 맞춘다.

## 2. 데이터 흐름

### 2.1 웹검색 → 저장 → 검색 (search + database)
`search.Search(query)` → Tavily REST → `[]SearchResult`. 저장: `SaveWebDataTool` →
`database.InsertWebContent(WebContentRecord)`. 조회: `SearchWebDataTool`/`SearchWebContent(keyword)` → title
tsvector 전문검색 → `[]WebContentRecord`.

### 2.2 문서 인덱싱 → 유사도 검색 (database + vectorstore)
인덱싱: 청크 → 임베딩(`llm.EmbeddingClient`, Ollama) → `database.InsertDocumentChunks`. 유사도 검색:
`SupabaseVectorStore.AsRetriever(opts).Invoke(query)` → 질의 임베딩(내부 보유 `EmbeddingClient`) →
`database.MatchDocuments(embedding, count)` → `[]DocumentRecord` → `[]document.Document` 변환. pgvector 접근은
`database` 단독, `vectorstore`는 위임만. 메타 필터가 필요하면 `database.QueryDocuments` 경로(vectorstore는
순수 유사도만 노출).

### 2.3 파일 스토리지 흐름 (storage)
업로드: `Upload(...)` → Drive → `FileMetadata` → `CreateStorageRef(fileID)` → `storage_ref`. 해소:
`storage_ref` → `ParseStorageRef` → fileID → `DownloadByStorageRef` → `FileContent`(base64). 도구는 각 메서드를
`tool` 래핑한 8종.

### 2.4 config 어셈블리 → mcp/a2a 조립 (config leaf)
`config.LoadMCPServers()` → `map[string]config.ServerConfig`(중립) → (mcp) `ServerConfigFromConfig` →
`mcp.NewMultiServerClient`. `config.AgentURLs()` → `map[string]string` → 다운스트림 오케스트레이터. 데이터는
config(하위)→상위로만 흐르고 config은 상위를 import하지 않는다.

### 2.5 MCP HTTP 전송 (mcp)
클라이언트: `NewClient(ServerConfig{Transport: streamable_http, URL})` → `Connect` →
`StreamableClientTransport{Endpoint}` → 기존 `ListTools`/`CallTool`/`LoadPrompt` 재사용. 서버: `NewServer` →
`RegisterTools` → `ServeStreamableHTTP(ctx, addr)` → `NewStreamableHTTPHandler` + `http.Server`. 도구 실행 핸들러는
stdio 경로와 동일.

## 3. 인터페이스 (경계 계약)

- `database.Client`(신규): §1.1 메서드 집합. pgvector 소유자. `vectorstore`가 이 인터페이스(구체 구현 아님)에
  의존해 테스트 시 fake 주입 가능.
- `vectorstore.Retriever/Store`(기존, 확장 없음): `SupabaseVectorStore`가 최소 `Retriever` 계약 충족. 기존
  `InMemoryStore` 시그니처 불변.
- `tool.Tool/Runtime`(기존, 불변): search/storage/database 도구가 이 계약으로 노출.
- `config.ServerConfig/AgentConfig`(기존 타입, 어셈블리 함수만 추가): 중립 타입. 상위 변환은 상위 소유.
- `mcp.Transport/ServerConfig`(기존, additive): streamable_http 상수 + Connect 분기 + ServeStreamableHTTP.
- `llm.EmbeddingClient`(기존, 불변): SupabaseVectorStore 질의 임베딩에 재사용.

공개 API 호환 [확인] SPEC §3: 전부 신규 패키지 추가 또는 additive 확장. 유일한 예외인 vectorstore 경계 테스트의
단정 대상 변경은 요구사항 변경에 따른 테스트 갱신이지 공개 시그니처 변경이 아니다.

## 4. 영향 범위

신규 패키지: `database/`(client·tool·types·테스트·정적 경계 테스트), `search/`(client·tool·테스트·경계 테스트),
`storage/`(client(Drive)·storage_ref·tool·테스트·경계 테스트; storage_ref는 순수함수 단위, Drive는 크리덴셜 없으면
skip).

기존 파일 수정(additive): `config/config.go`(어셈블리 3함수 + config 경계 테스트 신규), `vectorstore/`(supabase.go
신규 + import_boundary_test.go 갱신: database import 허용·단방향 단정·go/build 전환), `mcp/server.go`·`mcp/client.go`
(streamable_http 상수·분기·ServeStreamableHTTP + config→mcp 변환 헬퍼).

문서 수정(§5.7): `README.md` (a)~(f) 정정.

go.mod: pgx·pgvector-go 신규 require(§5 D1). Tavily는 REST 직접이라 require 없음. Google Drive·oauth2는 이미 존재.

기존 테스트 [확인] SPEC §3: 계속 통과해야 하며, 유일한 의도적 변경은 vectorstore 경계 테스트(요구사항 변경).

코드/문서 경계: §5.1~§5.6은 코드, §5.7은 README. §5.7 (f)는 코드 완료 결과를 반영하므로 순서 의존이 있다(§5 D6).

## 5. Decision Points

### D1 — database 외부 접근 방식 (SPEC §5.1)
- 옵션: (i) pgx + pgvector-go 직접 연결, (ii) Supabase REST(net/http).
- **채택: (i) pgx + pgvector-go.** 근거 — README §28이 명시 지목하며, `match_documents` RPC·tsvector 전문검색·
  eq/ilike/gte/lte 필터를 SQL로 온전히 구현하기 쉽다. net/http 관례는 "SDK가 불필요한 HTTP"에 대한 것이지 DB
  바이너리 프로토콜까지 배제하지 않는다. go.mod에 pgx·pgvector-go require를 추가한다.

### D2 — pgvector 스키마·RPC 소유 경계 (SPEC §5.1, §5.5)
- **채택: 라이브러리는 스키마/RPC를 소유·생성하지 않고 존재를 가정해 호출만 한다.** DDL·마이그레이션은 운영/
  다운스트림 소유. 근거 — README §19가 `match_documents`를 기정 RPC로 전제하고, 라이브러리는 DB 프로비저닝 도구가
  아니다. 통합 테스트는 스키마가 준비된 크리덴셜 환경에서만 실행(없으면 skip). 이 가정은 승인 전 확인 항목으로 올려
  두었다.

### D3 — Google Drive 인증 취급 및 SDK 도입 (SPEC §5.3)
- **채택: OAuth 파일 경로 주입.** `credentials.json`/`token.json` 경로를 `storage` 생성 인자/옵션으로 받고
  `Initialize`가 토큰을 로드·리프레시한다. 경로는 하드코딩하지 않고 호출자가 주입한다(config가 아닌 storage 생성
  인자 — config leaf 유지, env 아님). 근거 — README §24가 "env가 아닌 파일 경로 설정"으로 명시. 통합 테스트는 인증
  파일 부재 시 skip.
- **채택: Google Drive SDK(`google.golang.org/api/drive/v3`) 도입 + a2a 전역 정책 완화.** 구현 중
  `a2a/import_boundary_test.go`가 go.mod 전체 텍스트를 스캔해 `google.golang.org/api`·`grpc`·`protobuf`를 금지
  ("SDK/gRPC 미추가"라는 a2a feature 당시 사용자 결정 보호)함이 확인됐다. Drive SDK는 이 세 모듈을 전이 의존으로
  끌어와 정책과 충돌한다. 사용자 결정으로 그 기존 정책을 재개정해 **Drive SDK 도입을 허용**한다. 이에 따라 a2a의
  go.mod 전역 금지 스캔은 a2a **자기 의존 트리** 검사로 좁힌다(a2a 자신은 여전히 이 모듈들을 쓰지 않음을 보장하되,
  storage 등 무관 패키지가 go.mod에 추가하는 것은 허용). 이는 요구사항(정책) 변경에 따른 테스트 단정 대상 갱신이며,
  §1.5의 vectorstore 경계 테스트 갱신과 동일 성격이다(공개 API 변경 아님). 대안(REST 직접 호출로 SDK 회피)은
  기각됐다.

### D4 — SupabaseVectorStore 계약 범위와 질의 임베딩 소유 (SPEC §5.5)
- **채택: `Retriever` 계약을 충족하고 질의 임베딩을 위해 `llm.EmbeddingClient`를 보유.**
  `NewSupabaseVectorStore(client database.Client, emb llm.EmbeddingClient, opts...)`로 둔다(신규 함수라 README §16의
  `client`만 받는 서술과의 차이는 §3 위반이 아니며, §5.7 (f)에서 실제에 맞춰 정정). `Store` 전체(`Add` 등)는 §5.5
  범위상 필수가 아니다(인덱싱은 `database.InsertDocumentChunks` 경로 담당). 코드로 확정되는 결정.

### D5 — multiagent README 계층 재서술 (SPEC §5.7)
- **채택: multiagent를 라이브러리로 재분류.** README §14·§26 Phase 4·§27·트리의 "응용 계층/라이브러리 아님/import
  대상 아님" 문구를 실제(라이브러리 패키지)에 맞춰 정정하고 import 대상으로 표기한다. rag/orchestrator는 미구현 응용
  계층으로 유지한다(multiagent만 라이브러리로 승격되는 비대칭을 수용). 근거 — SPEC §5.7이 "실제 상태 반영"이 목표이고
  `multiagent/`가 실재하는 라이브러리다.

### D6 — README 동기화 시점 (SPEC §5.7)
- **채택: 드리프트 정정((a)~(e))은 코드와 독립이므로 선행·병행 가능하고, 신규 구현 반영((f))은 각 패키지 완료 직후
  해당 섹션을 갱신하는 혼합 방식.** 실제 Task 배치·순서는 implement.md(`/implement-init`) 소관이며, 이 §5는
  "(f)는 대응 코드 완료에 순서 의존한다"는 설계 사실만 확정한다.

### D7 — OpenAIAPIKey 필드 처리 (SPEC §5.7)
- **채택: 필드·env 로딩 유지 + README만 Ollama로 정정.** 근거 — `config.Config`는 공개 구조체라 필드 제거는 SPEC
  §3("이미 노출된 타입의 형태는 바꾸지 않는다")을 위반할 소지가 있다. 문서만 정정하는 것이 호환 제약과 정합한다.
  따라서 §5.7 (e)의 "제거된 필드" 정정 대상에서 `OpenAIAPIKey`는 제외되고, 순수 문서 정정((a))으로 다룬다.
