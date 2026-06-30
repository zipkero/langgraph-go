# runtime-semantics-fixes — ANALYSIS

## 승인 전 확인

- destinations를 도달성 인접에 더할 때 `validateNodeRefs`에 destinations 대상 검사를 **추가하지 않는**(런타임
  `validateGotoTarget`에만 맡기는) 기본값이 의도와 맞는가. 관련 본문: §5 Decision Point 2

## 근거

확인 사실과 추정을 분리한다.

### 확인 사실 (코드를 직접 열어 확인)

- **Fanout 병합이 리듀서를 무시한다.** `graph/exec.go`의 `invokeLoop`(분기 병합 루프)와 `runFromNode`(중첩 분기 병합 루프)는 분기 결과를 `for k,v := range branchState { state[k]=v }`로
  덮어쓴다(last-write-wins). 같은 함수 안에서 노드 업데이트 병합은 `applyReducers(state, res.Update, compiled.schema)`로 리듀서를 거치는데, 분기 병합만 이 경로를 우회한다. 같은 패턴이
  `graph/subgraph.go`(`invokeSubgraph`·`invokeSubgraphFromNode`)와 `graph/stream.go`(`streamLoop`·`streamFromNode`·`streamSubgraphLoop`)에도 동일하게 반복된다 — 분기 병합은 세 실행 경로
  모두 raw 덮어쓰기다.
- **컴파일된 리듀서 맵은 병합 지점에서 이미 접근 가능하다.** `Compiled.schema StateSchema`는 `Builder.schema`에서 복사돼 보관되고, 위 모든 루프 함수가 `compiled`(또는 `c`)를 인자로 받는다.
  실제로 같은 함수 안에서 `applyReducers(state, res.Update, compiled.schema)`를 이미 호출 중이다. 따라서 §1에서 새로운 배선(리듀서 맵을 병합 지점까지 전달하는 경로 추가)은 **불필요**하다 —
  기존 `applyReducers`를 분기 병합에도 적용하면 된다.
- **분기 입력 격리는 이미 부분적으로 성립한다.** `coerceSendState`는 `Send.State == nil`이면 현재 state를 **얕은 복사**해 분기에 넘긴다(`exec.go`). `runFromNode`/`streamFromNode`/
  `invokeSubgraphFromNode`도 진입 시 `initState`를 새 맵으로 복사한다. 즉 분기가 받는 맵 자체는 분리돼 있다. `se.state`는 `resolveNext`가 fanout 시점에 한 번 만든 스냅샷이므로, 분기 간
  입력 오염은 발생하지 않는다(확인). 문제는 오직 결과 병합 의미(리듀서 무시)다.
- **도달성 검사가 destinations·Goto를 인접으로 보지 않는다.** `graph/validate.go`의 `buildAdjacency`는 정적 엣지(`b.edges`)와 조건 엣지(`b.condEdges`)의 mapping 값만 인접으로 넣는다.
  노드의 `WithDestinations`(= `nodeEntry.destinations`, `builder.go`)는 인접에 포함되지 않는다. 결과로 Goto/WithDestinations로만 도달하는 노드는 BFS에서 미방문 → `validateReachability`가
  "도달 불가" 에러를 낸다.
- **이 한계를 우회하려고 더미 배선이 들어가 있다.** `multiagent/network.go`의 `BuildNetwork`는 워커가 2개 이상일 때 "validate BFS 통과용" 더미 조건 엣지를 추가하고(`dummyRouter`는
  빈 문자열만 반환, 실제 실행 시 호출 안 됨), 주석으로 "도달 가능성 검사를 통과하기 위한 구조 선언"이라 명시한다. 같은 우회가 테스트에도 박혀 있다 — `graph/command_test.go`의 Goto 테스트들은
  `AddEdge`를 추가해 컴파일을 통과시키며(46~57행 주석에 "실제 실행 경로와 무관, 검증 통과용"), `multiagent/e2e_test.go`(340~351행)도 동일한 더미 조건 엣지를 둔다.
- **destinations는 런타임 검증에 이미 쓰인다.** `exec.go`의 `validateGotoTarget`은 `nodeEntry.destinations`로 Goto 대상을 검증한다(미선언이면 무제한 허용). 즉 destinations는
  "이 노드가 Goto로 갈 수 있는 노드 목록"이라는 정적 선언이며, 도달성 인접 관계로 쓰기에 의미가 정확히 맞는다.
- **agent.Stream의 모델 호출이 미들웨어를 우회한다.** `agent/agent.go`의 `runModel`(Invoke 경로)은 `middlewareChain.Handler(terminal)`로 합성한 핸들러를 호출해 WrapModelCall·BeforeModel·
  DynamicPrompt를 모두 거친다. 반면 `runModelStream`(Stream 경로)은 `a.boundModel.ChatStream`을 직접 호출하고 `a.cfg.SystemPrompt`를 `buildMessages`에 그대로 넣는다 — 미들웨어 체인을 전혀
  타지 않는다. 따라서 동적 프롬프트 치환, BeforeModel 차단, 모델 오버라이드가 Stream에는 반영되지 않는다.
- **미들웨어 계약은 비스트리밍이다.** `middleware/middleware.go`의 `ModelHandler`는 `(ctx, ModelRequest) → (ModelResponse, error)`이고 `ModelResponse`는 단일 `llm.ChatResponse`를 감싼다.
  `ChatStream`은 `llm.go`에서 `(ctx, ChatRequest) → (<-chan ChatEvent, error)`로, 별도 시그니처다. `ChatEventDone` 이벤트가 최종 `*ChatResponse`를 실어 보내므로(`anthropic_adapter.go`·`stub.go`),
  스트림 종료 시점에 단일 응답을 재구성할 수 있다.

### 추정

- 더미 조건 엣지 제거 후, `network.go`와 두 e2e/command 테스트는 컴파일이 깨진다(추정 — 도달성 인접에 destinations를 더하면 회복된다고 봄). §4에서 영향 대상으로 다룬다.
- WrapModelCall을 "응답 후처리(토큰 누적 후 최종 응답 가공)" 형태로 스트림에 적용하면 토큰 단위 가공은 불가능하지만 SPEC §5.6(방출된 메시지·상태에 반영)은 충족된다고 본다(추정).

## 1. 구조

세 결함을 고치는 변경 지점과 그 구조적 위치.

### 1.1 Fanout 분기 병합 — 리듀서 경유 + 격리 (SPEC §5.1~§5.3)

분기 결과를 공유 상태에 합치는 모든 지점에서 raw 덮어쓰기를 `applyReducers`로 교체한다. 대상 함수는 세 실행 경로에 걸쳐 다음과 같다.

- `graph/exec.go`: `invokeLoop`의 분기 병합 루프, `runFromNode`의 중첩 분기 병합 루프.
- `graph/subgraph.go`: `invokeSubgraph`·`invokeSubgraphFromNode`의 분기 병합 루프.
- `graph/stream.go`: `streamLoop`·`streamFromNode`·`streamSubgraphLoop`의 분기 병합 루프.

병합은 분기 단위로 한다. 각 분기 결과를 하나의 `StateUpdate`(분기가 만든 델타, §2.2)로 보고 `state = applyReducers(state, 분기델타, compiled.schema)`로 누적한다. 이렇게 하면 리듀서 등록
키(예: `messages`)는 여러 분기 업데이트가 누적 병합되고, 미등록 키는 종전대로 last-write-wins가 유지된다(applyReducers의 기존 분기 로직 그대로). 이로써 SPEC §5.1(모든 분기 업데이트 보존)과
§5.3(세 경로 일관)이 충족된다.

**격리**는 추가 구조 변경 없이 현 동작으로 충족된다(SPEC §5.2). 분기 입력 스냅샷(`se.state`)은 fanout 시점에 `resolveNext`가 한 번 만들고, 각 `*FromNode`는 진입 시 입력을 새 맵으로 복사하므로
분기 간 입력 오염이 없다(근거 §확인사실). 병렬화는 spec.md §4 제외 — 순차 실행을 유지하되 위 병합만 리듀서 경유로 바꾼다.

### 1.2 도달성 인접에 destinations 포함 (SPEC §5.4~§5.5)

`graph/validate.go`의 `buildAdjacency`에 노드의 `WithDestinations` 선언을 인접으로 추가한다. `b.nodes`를 순회해 각 `nodeEntry.destinations`를 `adj[name]`에 덧붙인다(정적·조건 엣지 인접에 합산).
이 변경으로 정적/조건 엣지 없이 Goto/WithDestinations로만 도달하는 노드가 BFS에서 방문되어 컴파일을 통과한다(SPEC §5.4). destinations 대상의 존재 검증을 컴파일 단계에 더할지는 §5 Decision
Point 2의 선택지로, 기본값은 추가하지 않는다.

### 1.3 multiagent network 더미 배선 제거 (SPEC §5.5)

`multiagent/network.go`의 `BuildNetwork`에서 워커 간 더미 조건 엣지 추가 블록(workers>1일 때 dummyRouter로 otherMapping 조건 엣지를 다는 부분)을 제거한다. 워커 노드 등록 시 이미
`WithDestinations(names...)`가 선언돼 있으므로 §1.2 변경 후 도달성이 destinations로 충족된다. `dummyRouter` 변수와 관련 주석도 함께 제거한다.

### 1.4 agent.Stream을 미들웨어 체인 경유로 (SPEC §5.6)

`agent/agent.go`의 `runModelStream`이 `runModel`과 동일하게 미들웨어 체인(WrapModelCall·BeforeModel·DynamicPrompt)을 거치도록 한다. BeforeModel·DynamicPrompt·Override는 `ModelRequest`만
변형하므로 스트림 진입 전 체인을 한 번 통과시켜 (a) BeforeModel 차단 시 에러로 조기 종료, (b) DynamicPrompt가 만든 SystemPrompt와 Override된 Model을 확정한 뒤 그 확정 `ModelRequest`로
`ChatStream`을 호출한다. WrapModelCall은 §5 Decision Point 1 채택안(옵션 B)에 따라 스트림 종료 후 최종 응답을 체인에 통과시켜 최종 메시지를 확정한다. 구조적으로 `runModelStream` 본체가
"체인으로 요청 확정 → 확정 요청으로 스트림 호출 → 토큰 방출 → 최종 응답을 WrapModelCall 체인 통과 → 최종 메시지 확정"으로 재배치된다.

## 2. 데이터 흐름

### 2.1 Fanout 병합 흐름 (Invoke 경로 예시)

```
[fanout 노드 실행] → res.Control.Sends
  → resolveNext: 각 Send → sendEntry{target, state=coerceSendState(s.State, 현재 state 스냅샷)}
  → invokeLoop: fanout 직전 base = state
       for se in sends (순차):
         branchState = runFromNode(se.target, se.state, ...)   // 분기 격리 실행
         delta = 분기델타(branchState, base)                    // §2.2
         state = applyReducers(state, delta, schema)            // ← 변경: raw 덮어쓰기 → 리듀서 병합
       break
```

핵심 의미: `messages` 같은 누적 리듀서 키는 분기 A의 메시지와 분기 B의 메시지가 모두 보존된다(현재는 B만 남음). 미등록 키는 마지막 분기 값(종전 동작 유지).

### 2.2 분기 결과의 의미 경계 (범위 내 확정)

`branchState`(또는 subgraph `branchResult.state`)는 "분기 종료 시 그 분기가 보유한 전체 상태"로, (1) 분기가 입력으로 받은 base 키들과 (2) 분기가 새로 갱신한 키들이 섞여 있다. 이를 통째로
`applyReducers`에 넘기면 base 키도 리듀서로 다시 합쳐져 **이중 누적**이 생길 수 있다(예: `messages`가 base + 분기 추가가 아니라 base + (base+분기)로 합산).

SPEC §5.1("모든 분기의 업데이트가 리듀서로 병합")·§5.2("분기 입력은 fanout 직전 상태")의 문언에서, 입력은 fanout 직전 상태이고 병합 대상은 "업데이트"(델타)임이 직접 도출된다. 따라서 병합 시
"분기 종료 상태 − fanout 직전 base = 분기 델타"를 산출해 update로 넘긴다(base와 값이 동일한 키는 제외, 분기가 추가·변경한 키만 포함). 이 델타 산출은 §1.1의 `applyReducers` 적용 직전 unexported
헬퍼로 처리하며 세 경로 모두 동일하다. 이 경계는 사용자 결정 없이 SPEC 문언으로 확정된다.

### 2.3 도달성 흐름

```
Compile → validate → validateReachability → buildAdjacency
  adj[name] = 정적엣지(to) + 조건엣지(mapping값) + WithDestinations(destinations)   // ← destinations 추가
  → BFS(starts) → 미방문 노드 = 컴파일 거부
```

`multiagent.BuildNetwork`: 워커 노드에 `WithDestinations(모든 워커)`가 있으므로 진입 워커에서 BFS가 모든 워커로 전파 → 더미 조건 엣지 없이 통과.

### 2.4 agent.Stream 미들웨어 흐름 (옵션 B)

```
Stream goroutine → step loop → runModelStream
  → middlewareChain로 ModelRequest 확정:
       DynamicPrompt: req.SystemPrompt 치환
       BeforeModel: req.State 검사 → 에러면 ch <- Error; return (토큰 미방출)
       Override: req.Model 교체
  → 확정된 req.Model.ChatStream(확정 SystemPrompt 반영 메시지) → 토큰 그대로 방출
  → ChatEventDone의 최종 ChatResponse 수집
  → WrapModelCall 체인에 통과(터미널은 수집한 응답 반환) → 최종 메시지 확정
```

관찰 가능 효과(SPEC §5.6): 방출되는 Token은 확정 요청(동적 프롬프트·오버라이드 모델) 기준으로 생성되고, 최종 메시지·상태는 WrapModelCall 가공이 반영되며, BeforeModel 차단 시 토큰이 방출되지
않고 에러 이벤트로 끝난다. 토큰은 가공 전 원본, 최종 메시지는 가공 후 — 이 차이는 옵션 B의 명시적 트레이드오프다(§5 DP1).

## 3. 인터페이스

공개 API 시그니처는 spec.md §3에 따라 유지한다. 변경은 내부에 한정한다.

- `graph` 내부: `buildAdjacency`는 입력 `*Builder`에서 `b.nodes`의 destinations를 추가로 읽을 뿐 시그니처 동일. 분기 델타 산출 헬퍼는 unexported 신설이며 공개 표면에 노출 안 됨.
  `applyReducers`·`Compiled.schema`는 그대로 재사용.
- `multiagent.BuildNetwork`: 시그니처·반환 타입 동일. 내부 더미 엣지 생성 제거뿐.
- `agent`: `Stream`·`Invoke`·`Create` 시그니처 동일. `runModelStream`은 unexported이므로 내부 재배치 자유. `Agent.middlewareChain`은 이미 보관 중이라 새 필드 불필요.
- `middleware`: 옵션 B 채택으로 공개 타입 변경 없음 — 기존 `ModelHandler`/`ModelResponse` 계약을 그대로 써서 최종 응답을 한 번 통과시킨다. 새 공개 표면을 만들지 않는다.

## 4. 영향 범위

grep으로 확인한 호출자·구현체·참조와 변경 파급.

### 4.1 Fanout 병합 변경의 파급

- 병합 지점 3경로 6함수(§1.1 목록)가 모두 같은 raw-merge 패턴이라 동일 헬퍼로 일괄 교체된다. 누락 시 SPEC §5.3(Invoke·Stream·subgraph 일관) 위반.
- subgraph 경로는 `branchResult.parentCmd != nil`이면 즉시 전파하는 분기가 별도로 있어, 이 경로는 병합을 거치지 않고 바로 반환하므로 병합 변경과 충돌하지 않는다(확인).
- 기존 fanout 테스트(`graph/command_test.go`의 Fanout 계열, subgraph_test, stream 관련 테스트)가 last-write-wins 결과를 기대하는지 점검 필요. 누적 리듀서가 없는 키만 검증하는 테스트는
  영향 없고, `messages` 누적을 검증하는 테스트가 있으면 기대값이 바뀐다(실제 테스트 기대값 확인은 구현 단계 검증 항목).

### 4.2 도달성 변경의 파급

- `validateReachability`/`buildAdjacency`는 모든 `Compile` 경로가 거친다. destinations를 인접에 더하면 **기존에 통과하던 그래프는 계속 통과**한다(인접이 늘어날 뿐 줄지 않음, BFS 방문 집합
  단조 증가). 따라서 기존 컴파일을 깨지 않는다(확인).
- 더미 배선 제거 영향처: `multiagent/network.go`(제거 대상 본체), `multiagent/e2e_test.go`(340~351행 더미 조건 엣지·dummyRouter — 이제 불필요하므로 spec.md §3 단서 "우회용 테스트 배선
  조정 허용"에 따라 제거/조정), `graph/command_test.go`(46~57행 등 Goto 테스트의 보조 `AddEdge`). 보조 AddEdge가 있어도 컴파일은 여전히 통과하므로 전부 제거가 필수는 아니나, SPEC §5.4가
  "엣지 없이 통과"를 요구하므로 적어도 한 개의 테스트는 보조 엣지 없이 통과하도록 정비해야 검증이 성립한다.

### 4.3 agent.Stream 미들웨어 변경의 파급

- `runModelStream` 호출처는 `agent.Stream` 단일(grep 확인). 내부 재배치라 외부 파급 없음.
- 영향 받는 동작: Stream 사용 코드 전반(`multiagent`의 network/supervisor가 Stream을 쓰는 경우 포함)에서 미들웨어가 비로소 적용된다. 미들웨어를 쓰지 않는 호출은 체인이 비어 있어 동작 불변
  (`Create`가 빈 Chain을 만들고 `Handler(terminal)`은 terminal을 그대로 반환).
- `runModelStream`의 현 `ChatEventDone` 처리(tool_calls 재구성)와 옵션 B의 WrapModelCall 통과가 겹치지 않도록, "최종 응답 수집 → WrapModelCall 통과 → 최종 메시지/상태 확정" 순서를
  명확히 둔다.

## 5. Decision Points

### Decision Point 1 — agent.Stream에서 WrapModelCall을 어떻게 적용할 것인가 (SPEC §5.6)

현 미들웨어 계약은 비스트리밍 단일 응답 핸들러(`ModelHandler`/`ModelResponse`)다. BeforeModel·DynamicPrompt·Override는 `ModelRequest`만 변형하므로 스트림 진입 전 체인을 통과시켜 그대로
적용한다(어느 옵션이든 공통). 쟁점은 WrapModelCall("모델 호출을 감싸 응답을 가공")을 토큰 스트림에 어떻게 투영하는가였다.

- 옵션 A — 요청 변형만 적용, 응답 가공 WrapModelCall은 Stream에 미반영. 변경 최소이나 응답 가공형 WrapModelCall에서 Invoke/Stream 비대칭이 남아 SPEC §5.6 충족이 약하다.
- **옵션 B (채택)** — 토큰은 원본대로 방출하고, 스트림 종료 시 `ChatEventDone`의 최종 `ChatResponse`를 `ModelResponse`로 감싸 WrapModelCall 체인의 응답 가공부에 한 번 통과시켜 최종
  메시지·상태를 확정한다(터미널은 수집한 응답 반환). 공개 타입 변경 없이(SPEC §3) §5.6의 "방출된 메시지·상태에 반영"을 충족한다. 트레이드오프: 토큰 단위 가공은 불가(토큰은 가공 전, 최종
  메시지만 가공).
- 옵션 C — `middleware`에 스트리밍 핸들러 타입을 신설해 토큰 단위까지 완전 대칭. 그러나 새 공개 표면이 생기고 미들웨어가 이원화되며, spec.md §1의 "새 기능 추가가 아닌 의미 교정" 범위와
  결이 다르다.

**채택: 옵션 B.** 근거 — spec.md §1(의미 교정, 기능 추가 아님)·§3(시그니처 호환)과 가장 부합하면서 §5.6(메시지·상태 반영)을 충족한다. 옵션 A는 §5.6 충족이 약하고, 옵션 C는 범위와 충돌한다.
evidence: `middleware/middleware.go`의 비스트리밍 계약, `llm/llm.go`의 `ChatEvent`(Done이 최종 ChatResponse 운반), `agent/agent.go`의 `runModel`(체인 경유) vs `runModelStream`(직접 호출) 비대칭.

### Decision Point 2 — destinations에 대한 validateNodeRefs 검사 추가 여부 (SPEC §5.4)

도달성 인접에 destinations를 더하는 김에 `validateNodeRefs`가 destinations 대상이 정의된 노드인지도 검사할지.

- 채택 시: destinations에 오타/미정의 노드가 있으면 Goto 런타임 검증 전에 컴파일에서 잡혀 일관적이다.
- 미채택 시: 기존처럼 런타임 `validateGotoTarget`에서만 잡힌다. 동작 변화 최소.

**채택: 미채택(기본값).** spec.md §5에 이 검사를 요구하는 조항이 없고 범위는 "의미 교정"이라, 동작 변화를 최소화하고 되돌리기 쉬운 기본값을 둔다. main이 정합성 강화를 원하면 채택 가능하며,
이 선택은 승인 전 확인 항목으로 올려 두었다.
