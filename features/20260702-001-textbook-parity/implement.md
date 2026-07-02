# textbook-parity — 구현 체크리스트

이 문서는 spec.md §5(완료 조건 6개)와 analysis.md(구조·데이터 흐름·인터페이스·Decision Points D-a~D-f)를 실행
단위로 옮긴 순수 실행 체크리스트다. 설계 근거는 analysis.md에 있고, 요구사항 레벨 완료 조건은 spec.md §5에 있다.
순서는 의존(다음이 가능하려면 무엇이 먼저 존재해야 하는가)을 위치로 표현한다. OpenAI 챗이 openai-go SDK 의존성을
도입하므로 맨 앞에 두고, 임베딩은 같은 SDK를 재활용하므로 그 뒤에 둔다. Supabase 1536 전환과 Chroma 백엔드는 각각
임베딩 클라이언트가 있어야 e2e 확인이 가능하므로 임베딩 뒤에 두며 서로는 독립이다. README 갱신은 신규 구현 반영분이
코드 완료에 의존하므로 맨 뒤에 둔다.

## Section: OpenAI 챗

- [x] task-001: OpenAI 챗 어댑터와 InitChatModel openai 분기
  - 목적: 호출자가 `openai:<model>` 스펙 문자열로 챗 클라이언트를 초기화해 일반 대화, 도구 바인딩·호출, 토큰
    스트리밍, 구조화 출력을 기존 챗 클라이언트 계약과 동일한 형태로 사용할 수 있고, 기존 Anthropic 경로와
    미지원 프로바이더 에러 동작은 변경 전후 동일하다(openai가 미지원 목록에서 빠지는 것만 달라진다).
  - 접근: 공식 openai-go SDK 의존성을 추가하고, SDK 타입을 내부에 은닉하는 anthropic 어댑터와 동일한 패턴으로
    Client 7개 메서드(Chat/ChatStream/Structured/BindTools/ParseToolCalls/WithModel/ModelName)를 구현한다.
    스트리밍은 SSE 델타를 기존 ChatEvent 계약(Token/Message/Done)으로 방출하고, 구조화 출력은 OpenAI 네이티브
    Structured Outputs(response_format: json_schema)를 쓴다. InitChatModel switch에 openai 분기를 추가하고
    미지원 에러 메시지의 지원 목록 문구를 갱신하며, API 키는 WithAPIKey 옵션 → 없으면 OPENAI_API_KEY 자동
    사용(anthropic과 동일 규약)으로 둔다. openai를 미지원 입력으로 단정하던 기존 llm 테스트를 지원 상태에 맞게
    수정한다.
  - 검증 조건:
    - 결과: `InitChatModel("openai:gpt-4o-mini")`가 키 없이도 생성에 성공하고, 키가 있으면 일반 대화·도구
      호출·토큰 스트리밍·구조화 출력 4개 축이 기존 ChatResponse/ChatEvent 계약대로 동작한다. 키 부재
      환경에서도 `go build ./...`와 네트워크 없는 단위 테스트가 통과하고, `TestInitChatModel_UnsupportedProvider`·
      `TestInitChatModel_MultipleProviders`가 openai를 지원 프로바이더로 반영한 상태로 통과한다.
    - 확인: OPENAI_API_KEY 부재 시 skip하는 4개 축 통합 테스트와, 스펙 파싱·BindTools/WithModel 불변 빌더·
      도구 스키마 변환을 네트워크 없이 단정하는 단위 테스트가 통과하며, 수정된 기존 테스트를 포함해
      `go test ./llm/...`와 `go build ./...`가 통과한다.
  - 참조: SPEC §5.1, SPEC §5.5 / ANALYSIS §2.1, ANALYSIS §3, ANALYSIS §4, ANALYSIS §5 D-a, ANALYSIS §5 D-f

## Section: OpenAI 임베딩과 Supabase 1536

- [x] task-002: OpenAI 임베딩 클라이언트와 InitEmbeddings openai 분기
  - 목적: 호출자가 `openai:text-embedding-3-small` 스펙 문자열로 임베딩 클라이언트를 초기화해 1536차원 벡터를
    입력 순서 그대로 받을 수 있고, 기존 Ollama 임베딩 경로의 동작은 변경 전후 동일하다.
  - 접근: 챗과 같은 openai-go SDK 클라이언트 생성 규약을 공유하는 별도 EmbeddingClient 구현체(Embed/EmbedQuery)를
    추가하고, InitEmbeddings switch에 openai 분기와 미지원 에러 메시지 갱신을 넣는다. 키 부재·연결 실패·빈 응답은
    Ollama 구현과 같은 방식으로 error를 반환한다. openai를 미지원 입력으로 단정하던 기존 임베딩 테스트를 지원
    상태에 맞게 수정한다.
  - 검증 조건:
    - 결과: `InitEmbeddings("openai:text-embedding-3-small")`가 키 없이도 생성에 성공하고, 키가 있으면
      Embed/EmbedQuery가 1536차원 벡터를 입력 순서대로 반환한다. `TestInitEmbeddings_UnsupportedProvider`가
      openai를 미지원 목록에서 제거한 상태로 통과하고, 기존 Ollama 임베딩 테스트가 무수정으로 통과한다.
    - 확인: OPENAI_API_KEY 부재 시 skip하며 반환 차원 1536과 입력 순서 보존을 단정하는 통합 테스트와, 스펙
      파싱·에러 경로를 네트워크 없이 단정하는 단위 테스트가 통과하고, 수정된 기존 테스트를 포함해
      `go test ./llm/...`와 `go build ./...`가 통과한다.
  - 참조: SPEC §5.2, SPEC §5.5 / ANALYSIS §2.2, ANALYSIS §3, ANALYSIS §4, ANALYSIS §5 D-b, ANALYSIS §5 D-f

- [x] task-003: Supabase 벡터 경로 1536 전환
  - 목적: OpenAI 임베딩(1536차원)으로 적재한 문서가 Supabase 벡터 검색 경로에서 정상 조회되고, DB 스키마와
    match_documents 함수가 교재 CHAP11 SQL 예제와 차원·시그니처가 일치하며, 기존 768 스키마는 폐기·재적재로
    단일 전환된다.
  - 접근: `database/schema.sql`의 vector(768) 3곳(documents.embedding, match_documents 인자·반환)을
    vector(1536)로 제자리 개정하고, "768로 개정" 헤더 주석을 교재 원본 복원 취지로 갱신하며, 기존 테이블 drop 후
    재실행하는 전환 안내 주석 한 줄을 더한다. Go 코드는 차원 중립([]float32)이라 무변경이며, 적재→조회 관찰은
    OpenAI 임베딩을 주입한 Supabase 경로 통합 테스트로 확인한다. 실 DB에 대한 drop·스키마 재적용은 사용자
    수동 1회 절차다.
  - 검증 조건:
    - 결과: schema.sql에 vector(768)이 남지 않고 `match_documents(query_embedding vector(1536),
      match_count int)` 시그니처가 교재 원본과 일치하며, 1536 스키마가 적용된 DB에서 OpenAI 임베딩으로 적재한
      문서가 MatchDocuments 조회로 반환된다. database·vectorstore의 Go 파일은 무변경이다.
    - 확인: schema.sql diff로 차원 3곳·헤더 주석·전환 안내를 확인하고, DATABASE_URL·OPENAI_API_KEY가 모두 있으면
      OpenAI 임베딩 적재→match_documents 조회를 단정하고 하나라도 없으면 skip하는 통합 테스트가 통과하며(실 DB의
      drop·스키마 재적용은 수동 확인), `go build ./...`와 기존 database·vectorstore 테스트가 통과한다.
  - 참조: SPEC §5.4, SPEC §5.5 / ANALYSIS §2.4, ANALYSIS §3, ANALYSIS §5 D-d, ANALYSIS §5 D-f

## Section: Chroma 벡터스토어

- [x] task-004: Chroma 백엔드 벡터스토어
  - 목적: 호출자가 외부 Chroma 서버를 백엔드로 지정한 벡터스토어를 생성해 문서 추가, 유사도 검색, retriever
    변환을 기존 벡터스토어 계약(InMemoryStore와 동급)과 동일한 형태로 사용할 수 있고, 서버 미기동 환경에서도
    빌드·나머지 테스트는 깨지지 않는다.
  - 접근: chromadb 1.x의 v2 REST API(heartbeat, collection get-or-create, add, query 4종 —
    default_tenant/default_database)를 Ollama 임베딩 전례대로 net/http로 직접 호출하는 Store 구현체를 추가한다.
    Add는 주입된 EmbeddingClient로 클라이언트 측 임베딩 후 add를 호출하고, SimilaritySearch는 EmbedQuery 후
    query(n_results=K, where=Filter) 응답을 document.Document로 변환하며, AsRetriever는 기존 retriever들과 동일
    구조로 위임한다. 서버 연결 실패는 error로 반환하고, v2 경로·필드는 실 서버로 확정한다.
  - 검증 조건:
    - 결과: ChromaVectorStore가 Store 인터페이스(Add/SimilaritySearch/AsRetriever)에 대입 가능하고, 실행 중인
      Chroma 서버에 대해 문서 추가→유사도 검색→retriever Invoke가 기존 계약 형태의 결과를 반환하며,
      CreateRetrieverTool에 그대로 물릴 수 있다. 서버 미기동 시 e2e는 skip되고 `go build ./...`와 나머지
      테스트는 통과하며, import 경계 테스트(금지 SDK 목록 포함)는 무수정으로 통과한다.
    - 확인: httptest 기반으로 add/query 요청 생성·응답 변환을 네트워크 없이 단정하는 단위 테스트와, heartbeat
      미도달 시 skip하고 도달 시 추가→검색→retriever 왕복을 단정하는 e2e 테스트가 통과하고,
      `vectorstore/import_boundary_test.go`가 무수정 통과하며, `go build ./...`와 기존 vectorstore 테스트가
      통과한다.
  - 참조: SPEC §5.3, SPEC §5.5 / ANALYSIS §2.3, ANALYSIS §3, ANALYSIS §5 D-c, ANALYSIS §5 D-f

## Section: 문서

- [x] task-005: README 프로바이더 기준·Chroma 선언 갱신
  - 목적: README만 보고도 OpenAI가 기본 기준 프로바이더이고 교재 대비 치환 없이 대응됨을 알 수 있으며,
    Anthropic/Ollama를 명시 지정으로 계속 쓰는 대체 경로와 Chroma 백엔드 지원 상태가 실제 코드와 일치하게
    기재된다.
  - 접근: 레포 README의 §1 구조도(vectorstore 줄의 Chroma 제외 문구), §4 프로바이더 분담 note(기본 기준
    OpenAI + Anthropic/Ollama 대체 경로 사용법), §16 Chroma 제외 선언 해제·백엔드 목록, §26 후순위 목록,
    §28·§28-1 Chroma 제외 항목을 이번 구현 상태에 맞게 갱신한다. 코드 기본값은 신설하지 않고 문서 기준만
    전환한다.
  - 검증 조건:
    - 결과: README에 OpenAI가 기본 기준 프로바이더로 기재되고, 교재 무치환 대응과 Anthropic/Ollama 대체 경로
      사용법이 문서화되며, Chroma 제외 선언이 지원 선언으로 대체된다. 갱신된 각 서술이 실제 코드 상태
      (InitChatModel/InitEmbeddings 분기, ChromaVectorStore)와 일치한다.
    - 확인: README 갱신 지점(§1·§4·§16·§26·§28·§28-1)을 실제 스펙 문자열·구현체와 대조해 불일치가 없음을 수동
      확인하고, `go build ./...`와 기존 테스트가 통과한다.
  - 참조: SPEC §5.6 / ANALYSIS §4, ANALYSIS §5 D-e
