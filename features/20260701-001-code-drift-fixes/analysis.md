# code-drift-fixes — ANALYSIS

## 근거

조사 범위: `vectorstore/vectorstore.go`, `llm/llm.go`, `llm/anthropic_adapter.go`, `middleware/middleware.go`,
`checkpoint/checkpoint.go`와 각 패키지 테스트, 그리고 레포 전역 grep(`ResponseFormat*`, `StoreOption`,
`ParentConfig`, `.Messages`). spec.md §1~§5 범위 안에서만 확인했다.

확인 (코드/grep으로 검증):

- `FromDocuments`(vectorstore.go:190~203)는 `_ = &storeOptions{}` 후 루프에서 `opt(&storeOptions{})`로
  **매 반복마다 새 `storeOptions{}` 인스턴스**를 만들어 즉시 버린다. 옵션 적용 결과는 어디에도 흘러들지 않고,
  `newInMemoryStore(emb)`는 옵션과 무관하게 생성된다(197행). spec §5.1의 "적용 즉시 버려지는 임시 인스턴스"와
  정확히 일치한다.
- `storeOptions`(184~186행)는 필드가 없는 빈 구조체다. `StoreOption`(181행)은 공개 타입이나 파라미터 타입
  `*storeOptions`는 비공개다. 레포 내 `WithXxx` 형태 옵션 생성자는 없다(grep 결과 vectorstore 패키지에 `func With`
  없음). 테스트는 모두 `package vectorstore_test`(외부 패키지)이며 옵션 없이 `FromDocuments`를 호출한다.
- `llm.ResponseFormat` 타입(llm.go:28~35), `ResponseFormatType`(16~17), 상수 3종(`ResponseFormatText`/
  `ResponseFormatJSONObject`/`ResponseFormatJSONSchema`, 20~26), `ChatRequest.ResponseFormat` 필드(55~56)는
  전역 grep 결과 **`llm/llm.go` 자체 선언·주석 밖에서 참조가 전혀 없다.** `agent.go`의 `ResponseFormat` 일치 항목은
  `agent.Config.ResponseFormat *structured.Schema`라는 **별개의 살아있는 필드**이며 llm 타입과 무관하다.
- Anthropic 챗 경로(`buildParams`, anthropic_adapter.go:191~249)는 `req.Model`/`req.Messages`/`req.Tools`/
  `req.ToolChoice`/`req.Temperature`만 읽고 **`req.ResponseFormat`을 참조하지 않는다.** `Chat`(252행)은
  `buildParams`만 경유한다. 구조화/JSON 출력은 `Structured`(308~319행)가 인자 `schema`를 직접 받아
  `OutputConfig.Format`(json_schema)로 강제하며, `req.ResponseFormat`과 독립이다.
- `middleware.ModelRequest.Messages []string`(middleware.go:32)은 middleware 패키지 안에서 생성·기록·판독하는
  곳이 없다(grep `ModelRequest{`/`Messages:`/`req.Messages` in middleware = 0건). 레포 다른 곳의 `.Messages`
  일치는 모두 `agent.Result`/`multiagent.WorkerOutput`/MCP `res.Messages`/`llm.ChatRequest.Messages` 등
  이름만 같은 별개 필드다.
- `checkpoint.Checkpoint.ParentConfig *config.RunConfig`(checkpoint.go:35)는 grep 결과 선언·주석 외 참조가 없다
  (Put/Get/List/SaveState/LoadState 어디서도 세팅·판독하지 않음). `config` import는 `ThreadIDFromConfig`/
  `LoadState`/`SaveState`(129·140·155행)가 계속 쓰므로 필드 제거 후에도 import는 유지된다.

추정:

- `ResponseFormatJSONSchema`/`Schema` 등이 `Structured` 경로 설계 초기 흔적으로 남았을 가능성이 있으나, 현재
  코드 기준으로는 사용처가 없다(위 확인 근거로 충분). 외부 다운스트림 참조 가능성은 spec "승인 전 확인"이 다루므로
  여기서 재론하지 않는다.

## 1. 구조 (무엇을 어떻게 바꾸는가)

세 교정 모두 동일한 성격이다 — "선언은 있으나 살아있는 동작에 연결되지 않은 요소"를 제거하거나(§5.2·§5.3),
설계 의도대로 연결한다(§5.1). 관찰 가능한 공개 동작은 새로 도입하지 않는다.

1. vectorstore 옵션 반영 (SPEC §5.1)
   `FromDocuments`가 단일 `storeOptions` 인스턴스를 만들어 그 인스턴스에 모든 옵션을 적용하고, 그 인스턴스가
   실제 생성 경로(현재는 `newInMemoryStore`)로 전달되도록 배선한다. 빈 `storeOptions`라도 "옵션이 호출되어
   생성 경로에 반영됨"을 외부에서 관찰할 수 있어야 한다(SPEC §5.1). 실제 신규 옵션 필드는 추가하지 않는다
   (기능 추가 = spec §4 위반).

2. llm 죽은 응답 포맷 요소 제거 (SPEC §5.2)
   `ChatRequest.ResponseFormat` 필드와, 그것이 참조하던 `ResponseFormat` 타입·`ResponseFormatType`·상수 3종을
   제거 대상으로 본다. 구조화/JSON 출력은 종전대로 `Structured()` 전용 경로가 담당한다.

3. 미사용 필드 제거 (SPEC §5.3)
   `middleware.ModelRequest.Messages []string`와 `checkpoint.Checkpoint.ParentConfig *config.RunConfig`를
   선언에서 제거한다. 각 패키지의 다른 필드·메서드·import는 유지한다.

## 2. 데이터 흐름

vectorstore 옵션 (SPEC §5.1) — 현재 vs 교정 후:

```diff
 func FromDocuments(ctx, docs, emb, opts ...StoreOption) (Store, error) {
-    _ = &storeOptions{}
-    for _, opt := range opts {
-        opt(&storeOptions{})   // 매 반복 새 인스턴스 → 즉시 버려짐
-    }
-    store := newInMemoryStore(emb)
+    o := &storeOptions{}       // 단일 인스턴스
+    for _, opt := range opts {
+        opt(o)                 // 같은 인스턴스에 누적 적용
+    }
+    store := newInMemoryStore(emb)  // o 가 생성 경로에 도달함(배선 방식은 §5 D1)
     ...
 }
```

핵심은 "옵션 적용 대상 인스턴스가 하나이고, 그 인스턴스가 생성 경로에서 소비된다"는 것이다. `storeOptions`가
빈 타입인 이상 옵션이 남길 상태가 없으므로, "옵션이 호출됐다"는 사실 자체를 관찰 대상으로 삼는다(§5 D1 채택안).

llm 응답 포맷 (SPEC §5.2) — 산 경로 vs 죽은 경로:

- 산 경로: `Structured(ctx, req, schema)` → `buildParams(req)` → `params.OutputConfig.Format = json_schema` →
  Anthropic 호출. `schema`는 인자로 직접 흐르며 `ChatRequest.ResponseFormat`을 거치지 않는다.
- 죽은 경로: `ChatRequest.ResponseFormat`은 `Chat`/`ChatStream`/`buildParams` 어디에서도 읽히지 않는다.
  구성해도 API 파라미터로 전환되지 않는다. 즉 이 필드·타입·상수는 데이터 흐름에 진입점이 없다.

미사용 필드 (SPEC §5.3):

- `ModelRequest.Messages`: 미들웨어 훅(WrapModelCall/BeforeModel/DynamicPrompt)과 조작 메서드
  (Override/SetSystemPrompt/StateValue)는 `State`/`Model`/`SystemPrompt`만 사용한다. `Messages`로 흘러드는
  데이터가 없다.
- `Checkpoint.ParentConfig`: Put/Get/List는 `ThreadID`/`Values`/`Next`/`Metadata`/`CreatedAt`만 저장·조회하며
  `ParentConfig`를 세팅·판독하는 코드가 없다. 이 필드로 흘러드는 데이터가 없다.

## 3. 인터페이스 (공개 표면 영향)

- `vectorstore.FromDocuments`: 시그니처·반환형 불변. 내부 배선만 교정. 공개 타입 `StoreOption`은 유지(현재
  참조되는 공개 요소이며 README에도 문서화됨 → 살아있는 표면으로 취급, 제거 대상 아님).
- `llm`: 제거 시 공개 표면에서 `ResponseFormat` 타입, `ResponseFormatType` 타입, 상수 3종,
  `ChatRequest.ResponseFormat` 필드가 사라진다. `Client` 인터페이스(`Chat`/`ChatStream`/`Structured`/
  `BindTools`/`ParseToolCalls`/`WithModel`/`ModelName`) 시그니처는 전부 불변 — `ChatRequest`를 값으로 받되
  제거되는 필드를 아무도 채우지 않으므로 호출부 영향 없음.
- `middleware.ModelRequest`: 공개 필드 `Messages` 제거. `State`/`Model`/`SystemPrompt`와 메서드
  (`Override`/`SetSystemPrompt`/`StateValue`) 불변.
- `checkpoint.Checkpoint`: 공개 필드 `ParentConfig` 제거. 나머지 필드·`Checkpointer` 인터페이스·`InMemorySaver`
  메서드 불변. `ThreadIDFromConfig`/`LoadState`/`SaveState`는 spec §4가 명시적으로 변경 제외한 항목이므로
  손대지 않는다.

살아있는 공개 API의 시그니처·동작은 전부 불변이며, 제거 대상은 레포 내 미참조 죽은 공개 요소에 한정된다(spec §3 준수).

## 4. 영향 범위 (제거해도 깨지지 않는 경계)

빌드·테스트 무결성 경계:

- SPEC §5.1: 시그니처 불변이라 기존 vectorstore 테스트(`vectorstore_test`, `retriever_tool_test`, `e2e_test`)의
  `FromDocuments(...)` 호출은 그대로 통과한다. 신규 관찰 테스트만 추가된다(§5 D1 채택안: 내부 테스트 파일).
- SPEC §5.2: `ResponseFormat`/`ResponseFormatType`/상수/`ChatRequest.ResponseFormat`은 llm.go 자체 외 참조 0건
  (grep 확인). `llm_test.go:177`의 "ResponseFormat 테스트"는 주석 섹션 라벨이며 실제 코드 참조가 아님 — 다만
  최종 편집 시 해당 테스트가 필드를 실제로 구성하는지 1차 확인 후 제거해야 한다(구현 단계 점검 항목). `agent`의
  `ResponseFormat`은 별개 타입이라 무영향. `Structured` 경로 무변경으로 구조화/JSON 출력 계약 유지 → §5.2
  완료 조건("구조화/JSON은 종전 전용 경로로 계속 제공") 충족.
- SPEC §5.3(`ModelRequest.Messages`): middleware 패키지 및 이를 import하는 agent 어디서도 이 필드를 구성·판독하지
  않음(grep 확인). `[]string`은 별도 import를 쓰지 않아 import 정리 불필요.
- SPEC §5.3(`Checkpoint.ParentConfig`): 세팅·판독처 0건(grep 확인). `config` import는 `ThreadIDFromConfig`/
  `LoadState`/`SaveState`가 계속 사용하므로 **import 제거 금지**(제거하면 컴파일 실패). checkpoint 테스트는
  `ParentConfig`를 구성하지 않음(구현 단계에서 `checkpoint_test.go`의 Checkpoint 리터럴에 해당 필드가 없음을
  최종 확인).

외부 다운스트림 가능성은 spec "승인 전 확인"이 소유하는 판단이므로, 여기서는 "레포 내부 참조 0건"이라는 제거 근거만
제시하고 그 승인 결과에 제거 실행을 종속시킨다.

검증 수단: 세 교정 후 `go build ./...`와 기존 테스트 전량 통과가 공통 기준이다(spec §5의 각 항목). SPEC §5.1은 추가로
"옵션 호출·반영이 테스트로 관찰됨"을 요구한다.

## 5. Decision Points

### D1 — vectorstore 옵션 관찰 방법 (SPEC §5.1)

`storeOptions`가 빈 타입이고 `StoreOption`의 파라미터 타입 `*storeOptions`가 비공개이므로, 외부
`vectorstore_test` 패키지에서는 `StoreOption` 클로저를 **직접 작성할 수 없다**(파라미터 타입을 이름으로 지을 수
없음). 따라서 관찰 테스트는 다음 중 하나여야 한다.

- **옵션 A (채택)** — 내부 테스트 파일(`package vectorstore`)을 신설해 비공개 `storeOptions`를 다루는
  `StoreOption`을 테스트가 직접 만들고, 옵션 호출 여부(예: 클로저가 세팅하는 로컬 bool)와 스토어 정상 반환을
  관찰한다. 공개 표면 무변경으로 spec §2·§3(관찰 동작 불변·죽은 요소만 제거)과 정합.
- 옵션 B — 테스트 전용 공개 옵션 생성자 추가. 외부 테스트에서 관찰 가능하나 공개 표면을 늘려 spec §3과 충돌 소지.

**채택: 옵션 A.** 근거 — spec §3이 공개 표면 확대를 금지하므로, 관찰을 공개 표면 변경 없이 달성하는 내부 테스트
파일만이 제약과 정합한다. 옵션 B는 spec §3 위반 소지로 배제된다(spec이 사실상 옵션 A를 강제).

### D2 — `llm.ResponseFormat` 제거 범위 (SPEC §5.2)

필드만 제거할지, 필드+타입(`ResponseFormat`)+`ResponseFormatType`+상수 3종까지 통째로 제거할지.

- **옵션 A (채택)** — 전부 제거. 필드를 제거하면 `ResponseFormat` 타입과 `ResponseFormatType`/상수는 레포 내
  참조가 완전히 0이 되어(현재도 0건) 죽은 요소로 남는다. spec §5.2가 "필드와 그 관련 상수" 제거를 명시하므로
  상수까지 포함하는 것이 완료 조건에 부합한다.
- 옵션 B — 필드만 제거하고 타입·상수는 확장 자리로 보존. 여전히 죽은 공개 요소가 남아 spec §2 목표("죽은 요소
  노출 제거")를 부분만 달성.

**채택: 옵션 A.** 근거 — spec §5.2 문언("필드와 그 관련 상수")과 §2 목표(죽은 요소 노출 제거)가 전부 제거를
직접 지시한다. 이 결정은 spec 문언으로 확정되며 별도 사용자 판단을 요하지 않는다.

### D3 — 세 교정의 변경 묶음 (SPEC §5.1~§5.3)

세 교정은 서로 독립적이고(상호 의존 없음, 영향 파일 비중첩) 성격이 동일하며(죽은/미연결 요소 정합), 모두
"`go build ./...`+기존 테스트 통과"라는 동일 검증을 공유한다.

- **채택: 독립 교정으로 보되 공통 검증을 공유.** 세 교정을 서로 의존 없는 병렬 항목으로 다루고, 검증은 완료 조건
  1/2/3별 관찰 + 공통 빌드/테스트로 확인한다. 근거 — 상호 의존이 없어 순서 제약이 없고, 회귀 위험이 낮다.
- 실제 Task 분할(하나로 묶을지 항목별로 나눌지)은 implement.md 작성(`/implement-init`) 소관으로 넘긴다. 이
  §5는 "세 항목이 독립이며 순서 제약이 없다"는 설계 사실만 확정한다.
