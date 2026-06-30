# phase-5 — 장기 메모리 스토어

## 1. 범위

langgraph-go의 장기 메모리를 담당하는 Phase 5다(README §26 Phase 5, §12). 신규 `store` 패키지 하나를 다룬다.

- 네임스페이스 기반 키-값 저장 — `Namespace`(`[]string` 튜플)로 분리된 영역에 `map[string]any` 값을 저장하고
  키로 조회·삭제한다. 저장 항목의 메타데이터(Score/CreatedAt/UpdatedAt 등)는 `Item` 타입으로 표현한다.
- 시맨틱 검색 — `IndexConfig`(Embed=`llm.EmbeddingClient`, Dims)를 설정하면 검색이 질의 임베딩과 저장 항목
  임베딩의 유사도 순으로 결과를 반환한다. 인덱스 미설정 시의 검색 동작도 정의한다.
- 인메모리 구현 — `Store` 인터페이스와 동시성 안전한 `InMemoryStore` 구현체. `NewInMemoryStore(opts...)`,
  `WithIndex(IndexConfig)` 주입.
- 도구 함수 주입 — `FromContext(ctx)`로 store를 회수하는 경로와, `UserIDFromConfig(cfg)`로 실행 설정의
  사용자 식별자를 얻는 헬퍼.
- 에이전트 연동 — agent에 이미 존재하는 `WithStore(tool.Store)` 주입 경로(도구 실행 시 `rt.Store()`)와 호환
  되도록, `InMemoryStore`가 agent가 기대하는 주입 계약을 충족해 도구 함수에서 장기 메모리를 쓸 수 있게 한다.

`tool.Store`는 `tool` 패키지가 소유하는 좁은 인터페이스이며(`tool/tool.go` 주석이 "store.Store가 이를
충족한다"고 이미 규정), `Get`/`Search`가 `map[string]any` 기반 시그니처를 가진다. `InMemoryStore`는 한 타입이라
같은 이름의 메서드를 한 시그니처로만 가질 수 있고 agent 주입(`tool.Store`)을 받아야 하므로, `store.Store`의
`Get`/`Search`는 이 `tool.Store` 계약(맵 반환)을 그대로 따른다. README §12가 `Search`를 `[]Item` 반환으로
적은 부분은 이 계약 쪽으로 화해하며, `Item`(Score 포함) 기반의 풍부한 조회는 별도 이름의 점수 동반 접근자로
노출한다(정확한 메서드 형태는 analysis 소관). README 본문(§12) 표기 정리는 이 Phase 범위 밖이다(§4).

## 2. 목표

다운스트림이 사용자·스레드를 가로지르는 장기 메모리(네임스페이스 저장)와 임베딩 기반 시맨틱 검색을, 에이전트
도구 함수 안에서 바로 쓸 수 있게 한다. Phase 1에서 agent 도구 루프에 이미 뚫어 둔 Store 주입 지점(`WithStore`,
`rt.Store()`)을 실제 동작하는 구현으로 채워, 단기 메모리(`checkpoint`, Phase 1)와 짝을 이루는 장기 메모리
계층을 제공한다. 임베딩은 Phase 3에서 도입한 `llm.EmbeddingClient`를 재사용한다.

## 3. 제약

- import 경계(README §28-1, 상위→하위 단방향): `store`는 `llm`(EmbeddingClient)·`config`(RunConfig/사용자
  식별자)·`core`만 import한다. `agent`·`graph`·`tool` 등 상위/동급 패키지를 import하지 않는다. 특히 `tool`을
  import하지 않으며(§28-1 규칙2 — `tool.Store`는 `tool` 패키지가 소유하는 좁은 인터페이스), `InMemoryStore`가
  그 주입 계약을 구조적으로 충족하기만 한다.
- Phase 1~4 패키지의 기존 타입·동작은 변경하지 않는다. `agent.Config.Store`의 타입(`tool.Store`)과
  `WithStore` 시그니처, `tool.Store`/`tool.Runtime` 정의, `config.GetUserID` 등 기존 심볼을 수정하지 않는다.
  store에 필요한 새 타입은 `store` 패키지 안에 둔다(Phase 3가 `llm`에 임베딩을 더한 선례처럼 기존 동작 보존).
- `store.Store`는 기존 `tool.Store` 계약을 충족해야 한다(`tool/tool.go` 주석이 이미 규정). 즉 `Get`/`Search`는
  `tool.Store`와 같은 시그니처(`map[string]any` 반환)를 가진다. 이를 어겨 `tool.Store`를 변경하거나
  `InMemoryStore`가 agent에 주입되지 못하게 만들지 않는다.
- 임베딩은 기존 `llm.EmbeddingClient`만 사용한다. 새 임베딩 프로바이더나 새 챗 프로바이더는 추가하지 않는다.
- `InMemoryStore`는 동시 접근에 안전해야 한다(`checkpoint.InMemorySaver`의 mutex+map 선례).
- LLM/임베딩 비의존 로직(저장·조회·삭제·네임스페이스 분리·유사도 정렬)은 stub `EmbeddingClient`로 네트워크
  없이 결정적으로 검증할 수 있어야 한다. 실제 임베딩 기반 시맨틱 검색은 실제 Ollama 임베딩 e2e로 검증하며,
  e2e는 임베딩 서비스/모델이 있을 때 실행하고 없으면 건너뛴다(t.Skip).

## 4. 제외 범위

- 외부·영속 백엔드(Redis/Postgres/파일 등)는 제외한다. Phase 5는 `InMemoryStore`만 다룬다.
- `checkpoint`(단기 메모리, 스레드 상태 영속화)는 Phase 1에서 이미 구현됐다. 범위 밖이다.
- `vectorstore`(Phase 3)와의 통합이나 기능 중복은 제외한다. store는 네임스페이스 장기 메모리 용도이며
  vectorstore의 문서 검색과 별개다.
- 새 임베딩/챗 프로바이더 추가는 제외한다(기존 `llm.EmbeddingClient` 재사용).
- agent를 store에 직접 의존시키는 리팩터링(`agent.Config.Store`를 `store.Store`로 바꾸는 등)은 제외한다.
  agent는 기존 `tool.Store` 주입 계약을 유지한다.
- README §12의 `Store.Search`를 `[]Item` 반환으로 적은 표기 등 README 본문을 이 패키지 산출에 맞게 고치는
  문서 정리는 제외한다(후속 문서 작업).
- Phase 5 외 패키지(`mcp`(Phase 6), `a2a`·`database`·`search`·`storage`·`trace`(Phase 7))는 범위 밖이다.

## 5. 완료 조건

1. 모듈 루트에서 `go build ./...`와 `go vet ./...`가 오류 없이 끝난다(`store` 신규 패키지가 추가되고 Phase 1~4
   패키지의 기존 동작은 변경되지 않은 상태).
2. 네임스페이스 키-값 저장이 동작한다: `Put`으로 저장한 값을 같은 `Namespace`/키로 `Get`하면 동일 값이
   반환되고, `Delete` 후에는 조회되지 않으며, 존재하지 않는 키 조회는 not-found로 구분된다. 서로 다른
   `Namespace`에 같은 키로 저장한 값은 분리되어 조회된다. 저장 항목의 메타데이터(Score/타임스탬프)는 `Item`
   기반 접근자로 관찰할 수 있다.
3. 시맨틱 검색이 동작한다: `WithIndex(IndexConfig{Embed, Dims})`로 임베딩 인덱스를 설정하면 검색이 질의
   임베딩과 저장 항목 임베딩의 유사도 순으로 정렬된 결과를 `limit` 개수만큼 반환하며, 각 결과의 유사도 점수를
   확인할 수 있다. 임베딩 인덱스를 설정하지 않은 경우의 검색 동작도 정의되어 관찰 가능하다.
4. 도구 함수 주입 헬퍼가 동작한다: store가 실린 context에서 `FromContext(ctx)`가 그 store를 회수하고(없으면
   not-found로 구분), `UserIDFromConfig(cfg)`가 실행 설정의 사용자 식별자를 반환한다.
5. 에이전트 연동이 동작한다: agent의 기존 `WithStore`로 `store.InMemoryStore`를 주입하면, 도구 함수가
   `rt.Store()`로 받은 store에 대해 Put/Get/Search를 수행해 그 결과가 도구 출력·후속 상태에 반영된다(기존
   `tool.Store` 주입 계약을 store 구현이 충족함).
6. 실제 임베딩(예: Ollama)으로 store 시맨틱 검색을 실행하면 의미적으로 가까운 항목이 상위로 정렬되어 반환된다.
   이 e2e는 임베딩 서비스/모델이 있을 때 실행되고, 없으면 건너뛴다.
7. import 그래프 검사로 `store`가 `llm`·`config`·`core` 등 하위 패키지만 import하고 `tool`·`agent`·`graph`를
   import하지 않으며, 하위 패키지(`tool`·`llm`·`config`·`core`·`checkpoint`·`agent` 등)가 `store`를
   역참조하지 않음을 확인할 수 있다. Phase 1~4 패키지는 기존 동작이 수정되지 않는다.
