# phase-5 — 장기 메모리 스토어 (analysis)

## 근거

조사한 파일(모두 직접 읽어 시그니처 확인):

- `tool/tool.go` — `tool.Store` 인터페이스(L51-60): `Get(ctx, namespace []string, key string)
  (map[string]any, bool, error)` / `Put(ctx, namespace []string, key string, value
  map[string]any) error` / `Search(ctx, namespace []string, query string, limit int)
  ([]map[string]any, error)`. 주석에 "store.Store 가 이 인터페이스를 충족하지만 tool 은 store 를
  import하지 않는다(§28-1 규칙2)". `Runtime.Store() Store`(L86-87), `NewRuntime(..., store Store, ...)`
  (L279)는 nil 허용.
- `agent/agent.go` — `Config.Store tool.Store`(L117-118), `WithStore(s tool.Store) Option`(L162-165),
  도구 실행 시 `tool.NewRuntime(st.toCoreState(), "", cfg, a.cfg.Store, nil)`(L528)로 주입. 이미 구현됨
  (placeholder 아님). store 구현체가 `tool.Store`를 구조적으로 충족하면 수정 없이 주입된다.
- `llm/embedding.go` — `EmbeddingClient`(L24-31): `Embed(ctx, texts []string)([][]float32, error)` /
  `EmbedQuery(ctx, text string)([]float32, error)`. Ollama 구현은 `/api/embed` 직호출.
- `vectorstore/vectorstore.go` — `cosineSimilarity(a, b []float32) float32`(L207-230): 영벡터/빈벡터
  0 반환, 그 외 `dot/(sqrt(normA)*sqrt(normB))`. `InMemoryStore`가 `sync.RWMutex`+`[]storeEntry`로
  동시성 보호. 검색은 `EmbedQuery`로 질의 벡터 생성→코사인 정렬→K 절단. store가 따를 패턴 선례.
- `checkpoint/checkpoint.go` — config/core만 import. `sync.RWMutex`+`map` 기반 `InMemorySaver`,
  인터페이스(`Checkpointer`)+구현이 같은 패키지. `ThreadIDFromConfig`가 `config.GetThreadID` 래퍼+
  `ErrNoThreadID` 패턴. store가 그대로 따를 구조.
- `config/config.go` — `RunConfig{Configurable map[string]any}`(L27-29), `GetUserID(cfg) string`
  (L115-125)는 키 없거나 문자열 아니면 `""`. `store.UserIDFromConfig`는 이 래퍼면 충분.
- `README.md` §12(L481-504): `Get`을 `(Item, bool, error)`, `Search`를 `[]Item` 반환으로 적었으나
  이는 `tool.Store` 계약(map 반환)과 충돌한다. §28-1 규칙2(L1168-1171)는 "주입 접근자는 좁은
  인터페이스로, 구현은 상위 패키지가 주입"을 명시한다. README §28-1 해소 규칙은 store→tool 역참조를
  금지(L1174-1175 단방향 트리)한다.
- `vectorstore/import_boundary_test.go`, `vectorstore/e2e_test.go` — `go list -deps` 기반 경계 회귀
  테스트와 Ollama 가용성 체크(`checkOllamaEmbedReady`)+`t.Skip` e2e 패턴. store가 그대로 답습한다.

확정 사실: `store/` 패키지는 신규(현재 없음). agent/tool/config/llm은 store 추가만으로 수정 불필요.

추정(검증 범위 밖): Ollama 서버/모델 가용 여부는 실행 환경 의존이라 e2e는 skip guard로 처리한다.

---

## 1. 구조

신규 `store` 패키지 하나를 추가한다. 기존 파일은 수정하지 않는다(§4).

패키지가 소유하는 타입·심볼:

- `Namespace` (`[]string` 별칭) — 네임스페이스 튜플. 키 분리의 단위.
- `Item` — 저장 항목의 메타데이터 동반 표현. 필드는 README §12와 정합:
  `Namespace`/`Key`/`Value(map[string]any)`/`Score(float32)`/`CreatedAt`/`UpdatedAt(time.Time)`.
  `Score`는 시맨틱 검색 결과에서만 의미를 가지며, 직접 `Get`/비검색 경로에서는 0이다.
- `IndexConfig` — `Embed(llm.EmbeddingClient)`, `Dims(int)`. 임베딩 인덱스 설정값.
- `Store` (인터페이스) — `tool.Store` 계약(Get/Put/Search, map 기반)을 충족하고 거기에 `Delete`와
  점수 동반 접근자를 더한다. §3에 시그니처를 둔다.
- `InMemoryStore` — `Store` 구현체. 동시성은 `checkpoint.InMemorySaver` 선례대로 `sync.RWMutex`로
  보호한다. 저장 구조는 네임스페이스→키→레코드의 2단 맵(네임스페이스 튜플을 결합 키 문자열로
  정규화한 외부 맵 + 키→레코드 내부 맵). 레코드는 값·임베딩 벡터·타임스탬프를 함께 보관한다.
- `StoreOption` / `WithIndex(IndexConfig) StoreOption` / `NewInMemoryStore(opts ...StoreOption)
  *InMemoryStore` — 옵션 주입 생성자. `WithIndex` 미지정이면 인덱스 비활성.
- `FromContext(ctx) (Store, bool)` 와 context 주입 헬퍼 — §3·§5 참조.
- `UserIDFromConfig(cfg config.RunConfig) string` — `config.GetUserID` 래퍼.

import 경계: store는 `llm`(EmbeddingClient)·`config`(RunConfig/사용자 식별자)·`core`·표준 라이브러리
(`context`/`sync`/`time`/`math`/`sort`/`strings`)만 import한다. `tool`·`agent`·`graph`·`vectorstore`는
import하지 않는다(§3 제약, §28-1 규칙2·4). 코사인 유사도는 `vectorstore`를 빌려오지 않고 store 안에
자체 구현한다 — 이유는 §5에서 결정한다.

인터페이스+구현+헬퍼+테스트를 같은 패키지에 두는 단일 패키지 배치는 `checkpoint`/`vectorstore`의
일관된 선례를 따른다.

---

## 2. 데이터 흐름

### 저장·조회·삭제 (SPEC §5.2)

1. `Put(ctx, ns, key, value)` — 네임스페이스 튜플을 결합 키로 정규화해 외부 맵에서 내부 맵을 찾고
   (없으면 생성) `key`에 레코드를 넣는다. 인덱스가 설정돼 있으면 이 시점에 `value`의 텍스트 표현을
   `Embed`로 임베딩해 레코드 벡터로 보관한다(인덱스 미설정이면 벡터는 비운다). 신규면 `CreatedAt`,
   갱신이면 `UpdatedAt`을 채운다. 쓰기 잠금(`Lock`).
2. `Get(ctx, ns, key)` — 결합 키로 내부 맵을 찾아 레코드의 `value` 복사본을 `map[string]any`로
   반환한다(`tool.Store` 계약). 없으면 `(nil, false, nil)`로 not-found를 구분한다. 읽기 잠금
   (`RLock`). 메타데이터(Score/타임스탬프) 동반 조회는 점수 동반 접근자(`GetItem`)로 한다.
3. `Delete(ctx, ns, key)` — 내부 맵에서 키를 제거한다. 이후 `Get`은 not-found. 쓰기 잠금.
4. 네임스페이스 분리 — 서로 다른 `Namespace`는 다른 외부 맵 항목이므로 같은 키라도 독립 보관·조회된다.

### 시맨틱 검색 (SPEC §5.3)

`Search(ctx, ns, query, limit)`의 분기:

- **인덱스 설정 시**: 질의를 `EmbedQuery`로 임베딩한다. 해당 네임스페이스의 각 레코드 벡터와
  코사인 유사도를 계산해 점수를 매기고, 유사도 내림차순으로 정렬한 뒤 상위 `limit`개의 `value`를
  `[]map[string]any`로 반환한다(`tool.Store` 계약). `limit<=0`이면 vectorstore의 K 처리 선례대로
  전체를 반환한다. 점수까지 필요한 호출자는 점수 동반 접근자(`SearchItems`)를 쓴다.
- **인덱스 미설정 시**: 임베딩 없이 동작이 정의되어 관찰 가능해야 한다(§5.3 요구). 채택 폴백은
  §5에서 결정하며, 그 동작도 동일하게 `limit`로 절단해 반환한다.

읽기 잠금 아래 후보를 수집하고, 정렬·절단 후 값 복사본을 반환한다. `vectorstore.InMemoryStore.
SimilaritySearch`(L113-156)의 수집→정렬→K절단 형태를 그대로 따른다.

### 도구 함수 주입 두 경로 (SPEC §5.4, §5.5)

store는 도구 함수에서 두 경로로 닿는다. 두 경로는 독립적이며 서로를 대체하지 않는다.

- **agent 주입 경로(rt.Store())**: 호출자가 `agent.WithStore(store.NewInMemoryStore(...))`로 주입하면
  agent가 도구 실행 시 `tool.NewRuntime(..., a.cfg.Store, ...)`로 넘기고, 도구 함수는 `rt.Store()`로
  받아 Put/Get/Search를 호출한다. store 구현체가 `tool.Store`를 구조적으로 충족하므로 store 쪽에
  agent 연동을 위한 추가 코드는 없다. 결과는 도구 출력·후속 상태에 반영된다(§5.5).
- **context 주입 경로(FromContext)**: store를 context에 실어 두고 도구 함수가 `FromContext(ctx)`로
  회수한다(없으면 `(nil, false)`로 not-found 구분, §5.4). store 패키지가 주입 헬퍼와 회수 헬퍼를
  한 쌍으로 제공한다. 이 경로는 agent를 거치지 않는 도구·노드에서 store에 닿기 위한 것이다.

`UserIDFromConfig(cfg)`는 두 경로 어느 쪽에서든 도구가 `rt.Config()`/외부 `RunConfig`에서 사용자
식별자를 얻어 네임스페이스를 구성할 때 쓴다(`config.GetUserID` 래퍼).

### e2e 흐름 (SPEC §5.6)

실제 Ollama 임베딩(`llm.InitEmbeddings("ollama:...")`)으로 인덱스를 설정하고, 의미가 다른 항목들을
`Put`한 뒤 의미적으로 한 항목에 가까운 질의로 `Search`/`SearchItems`하면 그 항목이 상위로 온다.
Ollama 미가용이면 `t.Skip`(`vectorstore/e2e_test.go`의 가용성 체크 패턴 답습).

---

## 3. 인터페이스

`store.Store` (인터페이스). `Get`/`Put`/`Search`는 `tool.Store`와 글자 그대로 같은 시그니처를 가져야
agent에 주입된다(`map` 반환). 거기에 `Delete`와 점수 동반 접근자를 더한다.

```go
type Namespace = []string

type Item struct {
    Namespace Namespace
    Key       string
    Value     map[string]any
    Score     float32   // 시맨틱 검색 결과에서만 의미. 그 외 0.
    CreatedAt time.Time
    UpdatedAt time.Time
}

type IndexConfig struct {
    Embed llm.EmbeddingClient
    Dims  int
}

type Store interface {
    // tool.Store 계약 3종 (map 기반 — agent 주입 호환):
    Get(ctx context.Context, ns Namespace, key string) (map[string]any, bool, error)
    Put(ctx context.Context, ns Namespace, key string, value map[string]any) error
    Search(ctx context.Context, ns Namespace, query string, limit int) ([]map[string]any, error)

    // store 고유 확장:
    Delete(ctx context.Context, ns Namespace, key string) error

    // 점수·메타데이터 동반 접근자 (Item 기반 풍부한 조회):
    GetItem(ctx context.Context, ns Namespace, key string) (Item, bool, error)
    SearchItems(ctx context.Context, ns Namespace, query string, limit int) ([]Item, error)
}
```

생성·주입 심볼:

```go
type StoreOption func(*InMemoryStore)
func WithIndex(cfg IndexConfig) StoreOption
func NewInMemoryStore(opts ...StoreOption) *InMemoryStore   // *InMemoryStore 가 Store 충족

func FromContext(ctx context.Context) (Store, bool)         // 회수 (없으면 false)
func WithStore(ctx context.Context, s Store) context.Context // context 주입 헬퍼

func UserIDFromConfig(cfg config.RunConfig) string          // config.GetUserID 래퍼
```

`Store`가 `tool.Store`를 interface 임베딩으로 끌어오지 않고 동일 메서드 3종을 직접 나열하는 이유,
점수 동반 접근자 이름·반환형의 확정, context 주입 헬퍼 이름은 §5에서 결정한다.

비고: `*InMemoryStore`가 `Store`와 `tool.Store`를 동시에 충족함을 컴파일 타임에 보장하기 위해, store
패키지 내부 정적 단언은 `var _ Store = (*InMemoryStore)(nil)`만 둔다. `tool.Store` 충족 여부는 store가
tool을 import할 수 없으므로(§28-1) store 패키지 안에서 단언하지 않고, agent 주입 사용처(테스트)에서
실제 `WithStore`에 넘겨 컴파일로 검증한다.

---

## 4. 영향 범위

- **신규**: `store/` 패키지(구현 파일 + stub 임베딩 단위 테스트 + e2e 테스트 + import 경계 테스트).
- **기존 파일 수정 없음**(§4 원칙). 탐색으로 확인한 근거:
  - `agent.Config.Store`가 이미 `tool.Store` 타입이고 `WithStore`/`NewRuntime` 주입이 구현돼 있어,
    store 구현체가 `tool.Store`를 구조적으로 충족하기만 하면 agent는 수정 불필요(`agent.go` L117/162/528).
  - `tool.Store`/`tool.Runtime` 정의는 이미 store 충족을 전제로 작성됨(`tool.go` L51-60 주석). 수정 금지.
  - `config.GetUserID`(L115)·`llm.EmbeddingClient`(L24)는 그대로 재사용. 수정 금지.
  - `core`는 store가 import만 한다(현재로선 직접 타입 차용 없을 수 있음 — `config`/`llm`/표준만으로
    충족 가능하면 core import는 생략 가능. 경계 테스트는 "core 등 하위만 import"를 위반으로 보지
    않는다).
- **README 본문 §12·§28**: `Get`을 `(Item, bool, error)`, `Search`를 `[]Item`로 적은 표기와 실제
  화해된 계약의 차이는 후속 문서 정리이며 이 Phase 범위 밖이다(spec §4). 이 analysis는 표기를 고치지
  않고 화해 결과만 §3에 확정한다.
- **회귀 보호**: `vectorstore/import_boundary_test.go`와 같은 `go list -deps` 방식으로 store가
  `tool`/`agent`/`graph`/`vectorstore`를 import하지 않고 `llm`/`config`를 import함을, 그리고
  하위 패키지(`tool`/`llm`/`config`/`core`/`checkpoint`/`agent`)가 store를 역참조하지 않음을 검사한다.

---

## 5. Decision Points

### D1. store.Store의 메서드 집합과 tool.Store 임베딩 방식

채택: `store.Store`는 `tool.Store`의 3종(Get/Put/Search)을 **동일 시그니처로 직접 나열**하고,
`Delete` + 점수 동반 접근자 2종을 더한다. `tool.Store`를 interface 임베딩하지 않는다.

근거: store는 `tool`을 import할 수 없으므로(§28-1 규칙2·4, SPEC §3) `interface{ tool.Store; ... }`
임베딩 자체가 불가능하다. 따라서 같은 시그니처를 명시 나열하는 것이 유일한 합법 선택이다. 두 인터페이스의
구조적 호환은 `*InMemoryStore`가 양쪽을 모두 충족함으로 성립하고, agent 주입 컴파일로 검증된다(§3 비고).
`Delete`는 README §12·SPEC §5.2(Delete 후 not-found)가 요구하므로 포함한다.

### D2. Item/Score 노출 접근자의 형태

채택: 점수·메타데이터 동반 조회를 별도 이름의 `Item` 반환 메서드로 노출한다.
`GetItem(ctx, ns, key) (Item, bool, error)` 와 `SearchItems(ctx, ns, query, limit) ([]Item, error)`.

근거: 한 타입에 같은 이름 메서드를 두 시그니처로 둘 수 없다(SPEC §1). `Get`/`Search`는 agent 주입을
위해 map 계약에 고정되므로, `Item`(Score/CreatedAt/UpdatedAt 포함) 기반 풍부한 조회는 반드시 다른
이름이어야 한다. `GetItem`/`SearchItems`는 기존 `Get`/`Search`와의 대응이 직관적이고, 반환형이
README §12의 원래 의도(`Get`→Item, `Search`→[]Item)를 보존한다. `Score`는 `SearchItems` 결과에만
채워지고 `GetItem`/비검색 경로에서는 0이다. 이것이 implementer가 상속할 핵심 결정이다.

대안과 트레이드오프: `Search`가 점수를 옵션 인자로 받아 한 메서드로 통합하는 안은 `tool.Store` 시그니처를
깨므로 배제. 별도 `Scored` 래퍼 타입을 만드는 안은 README §12의 `Item` 타입을 중복시키므로 배제.

### D3. 코사인 유사도 구현 위치

채택: `vectorstore`를 import하지 않고 store 패키지 안에 자체 코사인 유사도 함수를 둔다(빈/영벡터 0,
그 외 `dot/(sqrt*sqrt)` — `vectorstore.cosineSimilarity` L207-230과 동일한 수식).

근거: `vectorstore.cosineSimilarity`는 비공개 함수라 import해도 호출할 수 없다. 게다가 SPEC §4가
"vectorstore와의 통합·기능 중복 제외"를, SPEC §3·§28-1이 store의 import 화이트리스트(llm/config/core)를
못박았다. store→vectorstore import는 경계 위반이자 불필요한 결합이다. 수식이 짧아 자체 구현 비용이
낮고, 두 패키지가 독립적으로 진화할 수 있다. 약간의 코드 중복은 경계 보존과 맞바꾼 의도된 비용이다.

`IndexConfig.Dims`의 역할: 채택은 Dims를 임베딩 차원의 선언적 메타데이터로 두되, 코사인 계산 자체는
`vectorstore`처럼 두 벡터의 더 짧은 길이까지만 합산해 길이 불일치에 견디게 한다(엄격한 차원 강제는
하지 않음). Dims는 향후 검증·문서화 용도의 자리로 남기고, 불일치 시 에러를 던지지 않는다. 근거: SPEC은
Dims 기반 강제 검증을 완료 조건에 두지 않았고, 강제하면 실제 임베딩 모델의 차원과 어긋날 때 e2e가
불필요하게 깨진다.

### D4. 인덱스 미설정 시 Search 동작

채택: 인덱스가 설정되지 않은 경우 `Search`/`SearchItems`는 에러를 내지 않고 **비시맨틱 폴백**으로
해당 네임스페이스의 저장 항목을 `limit`개까지 반환한다(점수는 0). 질의 문자열은 무시한다.

근거: SPEC §5.3은 "임베딩 인덱스를 설정하지 않은 경우의 검색 동작도 정의되어 관찰 가능"만 요구하고
에러/폴백 선택은 열어 두었다. 폴백 반환은 (a) 인덱스 없이도 네임스페이스 나열이라는 관찰 가능한 결정적
동작을 주고, (b) stub 임베딩 없이도 저장·조회 단위 테스트에서 Search 경로를 검증하게 하며(SPEC §3
"LLM 비의존 로직은 stub 없이 결정적 검증"과 정합), (c) agent 도구가 인덱스 미설정 store에 Search를
호출해도 패닉/에러 없이 동작하게 한다.

대안과 트레이드오프: "인덱스 미설정 시 에러" 안은 도구 함수가 인덱스 유무를 사전 분기해야 해 사용성이
떨어지고, 단기적으로 시맨틱 검색을 안 쓰는 호출자에게 불필요한 장벽이다. 폴백의 비용은 "검색"이라는
이름과 달리 의미 순위가 없다는 점인데, 이는 인덱스 미설정이라는 전제에서 호출자가 이미 인지하는
계약이므로 수용한다. 반환 순서는 결정적이어야 하므로(테스트 관찰), 삽입 순서 또는 키 정렬 순서 중
하나로 고정한다(키 정렬 채택 — 맵 순회 비결정성 회피).

### D5. context 주입/회수 헬퍼와 rt.Store() 경로의 관계

채택: store 패키지가 비공개 context 키를 두고 `WithStore(ctx, s) context.Context`(주입)와
`FromContext(ctx) (Store, bool)`(회수)를 한 쌍으로 제공한다. `FromContext`는 store 부재 시
`(nil, false)`로 not-found를 구분한다(SPEC §5.4).

근거: agent 경로(`rt.Store()`)와 context 경로(`FromContext`)는 서로 다른 진입점이다. agent 경로는
agent가 이미 소유한 `tool.NewRuntime` 주입이라 store가 관여하지 않고, context 경로는 agent를 거치지
않는 도구·노드가 store에 닿기 위한 store 자체의 책임이다(README §12가 `FromContext`를 store 심볼로
등재). 두 경로는 독립이며 store는 context 경로만 제공한다. 주입 헬퍼 이름은 context.Context를 받고
반환하는 표준 패턴을 따라 `WithStore`로 한다(agent의 `WithStore` Option과 패키지가 달라 충돌 없음).

대안과 트레이드오프: 회수만 제공하고 주입 헬퍼를 생략하는 안은 호출자가 비공개 키에 접근할 수 없어
context 경로를 실제로 쓸 수 없게 만든다(SPEC §5.4의 "store가 실린 context"를 호출자가 구성할 방법이
없음). 따라서 주입 헬퍼를 함께 제공한다.

### D6. 동시성 모델과 저장 구조

채택: `checkpoint.InMemorySaver` 선례대로 `sync.RWMutex`로 보호하고, 저장은 네임스페이스 결합 키→
(키→레코드)의 2단 맵으로 둔다. 읽기 경로(Get/GetItem/Search/SearchItems)는 `RLock`, 쓰기 경로
(Put/Delete)는 `Lock`. 반환 값은 내부 맵의 외부 변경을 막기 위해 복사본으로 돌려준다.

근거: 동시 접근 안전은 SPEC §3 제약이고, RWMutex+map은 checkpoint/vectorstore가 일관되게 쓰는
모듈 내 선례다. 2단 맵은 네임스페이스 분리(SPEC §5.2)를 자연스럽게 표현한다. 별도 트레이드오프가
없어 직접구현 기본을 채택한다.

### D7. 검증 전략

채택: (a) stub `EmbeddingClient`로 저장·조회·삭제·네임스페이스 분리·유사도 정렬을 네트워크 없이
결정적으로 검증하는 단위 테스트, (b) 실제 Ollama 임베딩 e2e(`vectorstore/e2e_test.go`의 가용성
체크+`t.Skip` 패턴 답습), (c) `go list -deps` 기반 import 경계 회귀 테스트(`vectorstore/
import_boundary_test.go` 형태).

근거: SPEC §3·§5.6·§5.7이 세 검증을 각각 요구한다. stub 임베딩은 결정적 벡터를 돌려줘 코사인 정렬
순서를 단정 가능하게 한다(예: 미리 정한 텍스트→벡터 맵). 별도 트레이드오프가 없어 직접구현 기본을
채택한다.
