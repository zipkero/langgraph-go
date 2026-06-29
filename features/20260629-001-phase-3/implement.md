# phase-3 — 문서·벡터스토어 IMPLEMENT

## Section: llm 임베딩

- [x] task-001: Ollama 임베딩 클라이언트와 InitEmbeddings 팩토리 추가
  - 목적: `llm.InitEmbeddings("ollama:<model>")`이 EmbeddingClient를 반환하고,
    Embed가 텍스트 배치를 `[][]float32`로, EmbedQuery가 단일 질의를 `[]float32`로
    로컬 Ollama 임베딩 결과로 돌려준다. 형식이 잘못된 spec이나 지원하지 않는
    프로바이더는 error로 거부되고, 기존 챗 Client·StubClient·어댑터 동작은
    변경 전후 동일하다.
  - 접근: `InitChatModel`과 같은 `"provider:model"` 파싱·옵션 빌더 규약을 따라
    `ollama` 분기만 구현하고, 표준 net/http로 Ollama 임베딩 엔드포인트를 직접
    호출한다. 베이스 URL 미지정 시 `http://localhost:11434`, 모델 미지정 시
    `nomic-embed-text`를 기본값으로 둔다.
  - 검증 조건:
    - 결과: 올바른 spec은 EmbeddingClient를 반환하고, 잘못된 형식(콜론 없음/빈
      provider/빈 model/빈 문자열)과 미지원 프로바이더(예: `openai:...`)는 error로
      거부된다. Embed/EmbedQuery는 Ollama 도달 시 비어 있지 않은 벡터를 반환하고,
      연결 실패·비정상 상태코드·빈 임베딩 응답은 error로 올린다.
    - 확인: 파싱·거부 단위 테스트는 네트워크 없이 항상 실행한다(`llm/llm_test.go`
      InitChatModel 파싱 테스트 패턴과 동일). Embed/EmbedQuery 실호출 테스트는
      Ollama 서버 도달 가능할 때만 실행하고 미도달 시 `t.Skip`한다. `go build ./...`
      와 `go vet ./...`가 오류 없이 끝나고, 기존 챗 테스트가 그대로 통과한다.
  - 참조: SPEC §5.1, §5.2 / ANALYSIS §1, §3, §5 (D1, D2, D7)

## Section: document

- [x] task-002: Document 값 타입과 재귀적 문자 분할기 추가
  - 목적: `Document`(PageContent, Metadata) 값 타입이 존재하고,
    `NewRecursiveCharacterSplitter(chunkSize, overlap)`의 SplitText가 텍스트를
    청크 문자열로, SplitDocuments가 Document를 청크 Document로 분할하며 overlap
    설정이 인접 청크 경계에 반영되고 원본 Metadata가 청크에 전파된다.
  - 접근: Document 구조와 TextSplitter 인터페이스를 정의하고, 재귀적 문자 분할
    구현체에서 SplitText로 청크화한 뒤 SplitDocuments가 각 Document의 PageContent를
    분할하며 Metadata를 복사한다.
  - 검증 조건:
    - 결과: 긴 텍스트가 chunkSize 기준 복수 청크로 나뉘고, 인접 청크가 overlap
      길이만큼 겹치며, SplitDocuments 결과 각 청크 Document의 Metadata가 원본과
      일치한다.
    - 확인: 분할·overlap·Metadata 전파를 검증하는 단위 테스트를 추가해 네트워크
      없이 항상 통과시킨다. `go build ./...`/`go vet ./...`가 오류 없이 끝난다.
  - 참조: SPEC §5.1, §5.4 / ANALYSIS §1, §3

- [x] task-003: 웹·PDF·DOCX 로더와 Load/LazyLoad 경로 추가
  - 목적: `NewWebLoader(urls)`가 URL 본문을, `NewPDFLoader(path)`가 페이지별
    Document와 메타(page/source/total_pages)를, `NewDocxLoader(path)`가 문서
    텍스트를 `[]document.Document`로 적재한다. Load(전량)와 LazyLoad(채널) 두 경로가
    동작하고, `ReadPDFBytes(b)`가 바이트에서 텍스트를 추출한다.
  - 접근: 구현 직전 Web 본문 추출·PDF 페이지 단위 텍스트 추출·DOCX 텍스트 추출
    Go 라이브러리 후보를 go.mod 호환·유지보수 상태로 조사해 형식별로 선정하고
    (PDF는 페이지별 추출 가능한 것), 각 로더가 그 파서를 감싸 Loader 인터페이스를
    충족하게 한다. LazyLoad는 셋업 에러를 반환값 error로, 적재 중 에러는 채널을
    닫아 종료한다.
  - 검증 조건:
    - 결과: 각 로더가 비어 있지 않은 PageContent의 Document를 반환하고, PDF
      Document에 page/source/total_pages 메타가 채워진다. Load와 LazyLoad가 같은
      문서 집합을 산출하고, ReadPDFBytes가 바이트 입력에서 텍스트를 반환한다.
      잘못된 경로·미지원 형식·파서 실패는 error로 반환된다.
    - 확인: 로컬 샘플 PDF/DOCX와 테스트용 HTTP 서버(또는 고정 입력)로 파싱·적재·
      LazyLoad·ReadPDFBytes를 검증하는 단위 테스트를 추가해 네트워크 외부 의존
      없이 항상 통과시킨다. `go build ./...`/`go vet ./...`가 오류 없이 끝난다.
  - 참조: SPEC §5.1, §5.3 / ANALYSIS §1, §2, §5 (D4, D5)

## Section: vectorstore

- [x] task-004: InMemoryStore 색인·유사도 검색과 FromDocuments 추가
  - 목적: `vectorstore.FromDocuments(ctx, docs, emb)` 또는 `(Store) Add(ctx, docs)`로
    문서를 임베딩 색인하고, `(Store) SimilaritySearch(ctx, query, SearchOptions{K, Filter})`
    가 임베딩 유사도 상위 K개 Document를 반환하며, Filter(메타데이터)가 지정되면
    검색 결과가 그에 맞게 필터링된다.
  - 접근: Store/Retriever 인터페이스와 SearchOptions(K, Filter)를 정의하고
    InMemoryStore에 벡터·Document를 보관한다. Add/FromDocuments는 청크 텍스트를
    EmbeddingClient.Embed로 임베딩하고, SimilaritySearch는 EmbedQuery로 질의
    벡터를 만들어 코사인 유사도 상위 K개를 반환하되 Filter가 있으면 Metadata
    일치 항목으로 한정한다. 백엔드는 인메모리 하나만 둔다.
  - 검증 조건:
    - 결과: 색인된 문서 집합에서 SimilaritySearch가 최대 K개의 Document를
      유사도 순으로 반환하고, Filter 지정 시 해당 Metadata에 맞는 항목만 결과에
      포함된다.
    - 확인: 유사도 순위와 K 제한, Filter 적용을 검증하는 테스트는 Ollama 서버
      도달 가능할 때 실행하고 미도달 시 `t.Skip`한다. `go build ./...`/
      `go vet ./...`가 오류 없이 끝난다.
  - 참조: SPEC §5.1, §5.5 / ANALYSIS §1, §2, §3, §5 (D3, D6)

- [x] task-005: AsRetriever와 Retriever.Invoke 추가
  - 목적: `(Store) AsRetriever(opts)`가 Retriever를 반환하고,
    `(Retriever) Invoke(ctx, query)`가 고정된 SearchOptions로 검색한 결과
    `[]document.Document`를 반환한다.
  - 접근: AsRetriever가 SearchOptions를 캡처한 Retriever를 반환하고 Invoke가
    같은 검색 경로(SimilaritySearch)를 그 옵션으로 호출한다.
  - 검증 조건:
    - 결과: AsRetriever로 만든 Retriever의 Invoke가 동일 질의에 대해
      SimilaritySearch와 일치하는 결과 Document를 반환한다.
    - 확인: Invoke 결과가 기대 문서를 포함하는지 검증하는 테스트는 Ollama 도달
      가능할 때 실행하고 미도달 시 `t.Skip`한다. `go build ./...`/`go vet ./...`
      가 오류 없이 끝난다.
  - 참조: SPEC §5.1, §5.6 / ANALYSIS §1, §3

- [x] task-006: CreateRetrieverTool 추가
  - 목적: `CreateRetrieverTool(r, name, description)`이 `tool.Tool`을 반환하고,
    그 도구를 실행하면 질의에 대한 검색 결과가 도구 출력으로 반환된다.
  - 접근: Retriever를 감싸 tool.Tool 계약(Schema로 질의 파라미터 노출, Execute에서
    Invoke 호출 후 결과를 Result.Content로 직렬화)을 충족하는 도구를 만든다.
  - 검증 조건:
    - 결과: 반환된 도구가 지정한 name/description과 질의 파라미터 Schema를 갖고,
      질의로 Execute하면 검색 결과 텍스트가 Result.Content에 담겨 반환된다.
    - 확인: 도구 메타와 Execute 출력을 검증하는 테스트는 Ollama 도달 가능할 때
      실행하고 미도달 시 `t.Skip`한다. `go build ./...`/`go vet ./...`가 오류 없이
      끝난다.
  - 참조: SPEC §5.1, §5.7 / ANALYSIS §1, §3

## Section: 통합·경계 검증

- [x] task-007: 적재→분할→임베딩→저장→검색 end-to-end 테스트 작성
  - 목적: 실제 Ollama 서버와 임베딩 모델(기본 `nomic-embed-text`)로 문서를
    적재·분할·임베딩·저장한 뒤, 의미상 가까운 질의가 관련 청크를 상위 결과로
    가져오는 의미 검색 경로가 관찰된다.
  - 접근: 로더 또는 고정 문서 집합을 분할해 FromDocuments로 색인하고,
    의미상 관련된 질의로 SimilaritySearch/Retriever 검색을 수행해 기대 청크가
    상위에 오는지 확인하는 통합 테스트를 둔다.
  - 검증 조건:
    - 결과: 색인된 문서 집합에서 의미상 가까운 질의가 관련 청크를 상위 K 결과로
      반환한다.
    - 확인: Ollama 서버 도달 가능 시 실행하고, 미도달 시 `t.Skip`으로 건너뛴다
      (기존 e2e 테스트 컨벤션을 따름). Ollama 가용 로컬 환경에서 통과한다.
  - 참조: SPEC §5.8 / ANALYSIS §2, §5 (D6, D7)

- [x] task-008: import 경계와 Phase 0~2 무수정 검증
  - 목적: import 그래프상 `vectorstore`가 `document`·`llm`·`tool`을 import하고
    `database`/벡터스토어 외부 백엔드를 참조하지 않으며, `document`가 모듈 내 상위
    패키지를 import하지 않고, Phase 0~2 패키지가 수정되지 않은(llm 임베딩 추가
    제외) 상태가 확인된다.
  - 접근: `go list`/`go mod` 기반 import 의존 점검과 Phase 0~2 패키지 변경 여부
    diff 확인으로 단방향 경계와 무수정을 검증한다.
  - 검증 조건:
    - 결과: vectorstore의 import 집합이 document·llm·tool을 포함하고 금지 대상
      (database·외부 벡터 백엔드)을 포함하지 않으며, document가 상위 패키지를
      import하지 않고, Phase 0~2 패키지(llm 임베딩 추가 제외)에 변경이 없다.
    - 확인: import 의존 점검 명령과 변경 범위 diff로 확인한다. `go build ./...`/
      `go vet ./...`가 오류 없이 끝난다.
  - 참조: SPEC §5.1, §5.9 / ANALYSIS §1, §4
