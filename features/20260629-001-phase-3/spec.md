# phase-3 — 문서·벡터스토어

## 승인 전 확인

- 임베딩 검증을 실제 Ollama 호출로 잡았다. 로컬 Ollama 서버와 임베딩 모델이 없는 환경(CI 등)에서 임베딩·검색
  관련 검증을 건너뛰게 할지(서버 부재 시 skip), 아니면 그 환경을 전제에서 배제할지. 관련 본문: §3, §5
- 임베딩 기본 모델을 `nomic-embed-text`로 가정했다. 다른 Ollama 임베딩 모델을 기본으로 둘지. 관련 본문: §3

## 1. 범위

langgraph-go의 문서 적재·분할과 벡터 저장·검색을 구성하는 Phase 3다(README §26 Phase 3). 다음을 다룬다.

- `document` — 문서 적재와 분할. 페이지/본문 단위 `Document`(PageContent, Metadata), 로더(웹/PDF/DOCX), 재귀적
  문자 분할기.
- `vectorstore` — 인메모리 벡터 저장·유사도 검색과 retriever 생성. `FromDocuments`/`Add`/`SimilaritySearch`/
  `AsRetriever`, `Retriever.Invoke`, retriever 도구화(`CreateRetrieverTool`).
- `llm`(임베딩 팩토리 추가) — Phase 1에서 미뤘던 `EmbeddingClient` 인터페이스와 `InitEmbeddings` 팩토리.
  임베딩 프로바이더는 Ollama 로컬로 구현한다.

이 Phase의 통합 산출물은 "적재→분할→임베딩→저장, 유사도 검색, retriever 도구화가 동작한다"이다
(README §26 Phase 3). 단 벡터 백엔드는 인메모리까지만 완성하며, Chroma/Supabase 백엔드는 범위 밖이다(§4).

## 2. 목표

다운스트림이 임의의 문서를 적재·분할해 임베딩으로 색인하고, 의미 기반 유사도 검색과 retriever 도구를 쓸 수 있는
RAG primitive를 제공한다. RAG 그래프(검색→증강→생성)는 라이브러리가 아니라 다운스트림이 이 primitive와 Phase 2
`graph`/`command` 위에 직접 조립하는 응용 계층이므로(README §17), 이 Phase는 그 토대(문서·임베딩·벡터검색·
retriever)까지만 책임진다. Phase 1에서 첫 소비처를 기다리며 미뤄둔 임베딩 팩토리(README §4·§26)를 그 첫 소비처인
`vectorstore`와 함께 이 Phase에서 채운다.

## 3. 제약

- import 경계(README §28-1, 상위→하위 단방향): `vectorstore`는 `document`(`document.Document`),
  `llm`(`llm.EmbeddingClient`), `tool`(`CreateRetrieverTool`이 `tool.Tool` 반환)에 의존한다. `document`는 모듈 내
  상위 패키지를 import하지 않는다. `vectorstore` 역방향 의존(`document`/`llm`/`tool`이 `vectorstore`를 참조)은
  만들지 않는다.
- 임베딩 프로바이더는 Ollama 로컬만 구현한다. Claude(Anthropic)는 임베딩 API가 없어 제외하고, OpenAI 임베딩도 이
  Phase에서는 구현하지 않는다(§4). `InitEmbeddings`는 `"provider:model"` 형식(`InitChatModel`과 동일 규약, 예:
  `ollama:nomic-embed-text`)을 받아 `ollama` 분기를 구현하고, 다른 프로바이더로 확장할 자리만 남긴다. 임베딩 기본
  모델은 `nomic-embed-text`로 둔다.
- 임베딩 관련 검증은 실제 Ollama 서버와 임베딩 모델로 수행한다(stub 임베딩으로 대체하지 않는다). 로컬 Ollama
  서버가 필요하지만 외부 API 키·인터넷은 필요하지 않다. 챗(Phase 1) 동작 검증은 기존 방식을 유지한다.
- `document` 로더(웹/PDF/DOCX)와 본문 파싱을 위해 새 외부 의존성을 도입할 수 있다. Phase 2의 "코어 엔진은 순수
  Go" 제약은 그래프 엔진에 한정되며, 문서 파싱에는 적용하지 않는다.
- `llm` 패키지 변경은 임베딩 타입·팩토리(`EmbeddingClient`/`InitEmbeddings`/`Embed`/`EmbedQuery`) 추가에
  한정한다. 기존 챗 `Client`·`StubClient`·어댑터의 동작은 바꾸지 않는다.
- Phase 0~2 패키지(`config`/`core`/`message`/`tool`/`structured`/`prompt`/`agent`/`middleware`/`prebuilt`/
  `checkpoint`/`graph`/`graph/command`/`streaming`)는 수정하지 않는다(`llm`의 임베딩 추가는 예외).

## 4. 제외 범위

- `ChromaStore`/`NewChromaStore`(로컬 영속 벡터 백엔드)는 제외한다(deferred). README §28대로 Go에서 Chroma
  `persist_directory` 직접 접근이 어렵고 서버 기동을 전제해야 하므로, 인메모리 백엔드 완성 후 후속 작업으로 둔다.
- `SupabaseVectorStore`/`NewSupabaseVectorStore`/`MatchDocuments`와 `database` 연동은 제외한다. `database.Client`
  (Phase 7)에 위임 의존하므로 Phase 7로 분리한다(README §16·§19).
- OpenAI 임베딩 프로바이더는 제외한다. 이 Phase의 `InitEmbeddings`는 Ollama 분기만 구현한다.
- RAG 응용 계층(검색→증강→생성 그래프, `ChatbotNode`/`RetrieveNode`/`VectorSearchNode`/`IndexDocumentNode`,
  관련성·환각 평가, 라우팅)은 제외한다. 다운스트림이 이 primitive와 `graph`/`command` 위에 직접 구현한다
  (README §17).
- Phase 3 외 패키지(`store`/`mcp`/`a2a`/`database`/`search`/`storage`/`trace`)와 그 응용 계층
  (`multiagent`/`orchestrator`)은 범위 밖이다.

## 5. 완료 조건

1. 모듈 루트에서 `go build ./...`와 `go vet ./...`가 오류 없이 끝난다(`document`/`vectorstore` 신규 패키지가
   추가되고, `llm`에 임베딩 팩토리가 추가되며, 그 외 Phase 0~2 패키지는 수정되지 않은 상태).
2. `llm.InitEmbeddings("ollama:<model>")`이 `EmbeddingClient`를 반환하고, `Embed(ctx, []string)`이
   `[][]float32`를, `EmbedQuery(ctx, string)`이 `[]float32`를 로컬 Ollama 임베딩으로 반환한다. 형식이 잘못된
   spec이나 지원하지 않는 프로바이더는 error로 거부된다.
3. `document` 로더가 `[]document.Document`(PageContent, Metadata)를 반환한다: `NewWebLoader(urls)`는 URL 본문을,
   `NewPDFLoader(path)`는 페이지별 Document와 메타(page/source/total_pages)를, `NewDocxLoader(path)`는 문서
   텍스트를 적재한다. `Load`와 `LazyLoad`(채널) 두 경로가 동작하고, `ReadPDFBytes(b)`가 바이트에서 텍스트를
   추출한다.
4. `NewRecursiveCharacterSplitter(chunkSize, overlap)`의 `SplitDocuments([]Document)`와 `SplitText(string)`이
   문서·텍스트를 청크로 분할하며, overlap 설정이 인접 청크에 반영된다.
5. `vectorstore.FromDocuments(ctx, docs, emb)` 또는 `(Store) Add(ctx, docs)`로 문서를 임베딩 색인하고,
   `(Store) SimilaritySearch(ctx, query, SearchOptions{K, Filter})`가 임베딩 유사도 상위 K개 Document를
   반환한다. `Filter`(메타데이터)가 지정되면 검색 결과가 필터링된다.
6. `(Store) AsRetriever(opts)`가 `Retriever`를 반환하고, `(Retriever) Invoke(ctx, query)`가 검색 결과
   `[]document.Document`를 반환한다.
7. `CreateRetrieverTool(r, name, description)`이 `tool.Tool`을 반환하고, 그 도구를 실행하면 질의에 대한 검색
   결과가 도구 출력으로 반환된다.
8. 실제 Ollama 서버와 임베딩 모델(기본 `nomic-embed-text`)을 사용해 적재→분할→임베딩→저장→유사도 검색 경로가
   관찰된다: 색인된 문서 집합에서 의미상 가까운 질의가 관련 청크를 상위 결과로 가져온다.
9. import 그래프 검사로 `vectorstore`가 `document`·`llm`·`tool`을 import하고 `database`/`vectorstore` 외부
   백엔드를 참조하지 않으며, `document`가 모듈 내 상위 패키지를 import하지 않음을 확인할 수 있다. Phase 0~2
   패키지는 수정되지 않는다(`llm` 임베딩 추가 제외).
