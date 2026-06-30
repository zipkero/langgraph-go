# runtime-semantics-fixes — 구현 체크리스트

`features/20260630-002-runtime-semantics-fixes/spec.md` §5와 `analysis.md`로부터 도출한 순수 실행 체크리스트다.
설계 근거는 analysis.md에 있다. 순서는 의존(다음이 가능하려면 무엇이 먼저 존재해야 하는가)을 따른다 — Fanout 병합을
세 경로에 적용한 뒤 일관성 e2e를 두고, 도달성 수정 후 그 결과로 더미 배선을 제거하며, Stream 미들웨어는 독립적으로 둔다.

- [x] task-001: Fanout 분기 병합을 리듀서 경유로 (Invoke 경로)
  - 목적: `graph/exec.go`의 fanout/Send 분기 병합이 분기 결과를 상태에 raw 덮어쓰기(last-write-wins)하던 것을,
    리듀서로 누적 병합하도록 바꿔 모든 분기의 업데이트가 보존되게 한다.
  - 접근: `invokeLoop`의 분기 병합 루프와 `runFromNode`의 중첩 분기 병합 루프에서 `for k,v := range branchState { state[k]=v }`
    형태의 raw 덮어쓰기를 제거한다. 각 분기 종료 상태(`branchState`)와 fanout 직전 base 상태의 차이를 산출하는 unexported
    헬퍼를 신설해, base와 값이 같은 키는 제외하고 분기가 추가·변경한 키만 담은 델타를 만든다. 그 델타를
    `state = applyReducers(state, delta, compiled.schema)`로 누적한다. 순차 실행은 유지한다(병렬화 없음). 분기 입력 격리는
    현 동작(fanout 시점 스냅샷 + `*FromNode` 진입 시 입력 복사)을 그대로 두고 변경하지 않는다.
  - 검증 조건:
    - 결과: 같은 리듀서 등록 키(`messages`/`AddMessages`)를 두 Send 분기가 각각 갱신할 때, Invoke 결과 상태에 두 분기의
      메시지가 모두 보존된다. 리듀서 미등록 키는 종전대로 마지막 분기 값이 남는다.
    - 확인: `graph` 패키지 테스트에 두 Send 분기가 `messages` 키를 갱신하는 fanout 그래프를 Invoke해 양쪽 메시지가 모두
      결과에 있는지 검증하는 회귀 테스트를 추가한다. base 키가 이중 누적되지 않음(델타만 병합됨)도 같은 테스트에서 함께
      확인한다. `go test ./graph/...` 통과.
  - 참조: SPEC §5.1, §5.2 / ANALYSIS §1.1, §2.1, §2.2

- [x] task-002: Fanout 분기 병합 리듀서 경유 (subgraph 경로)
  - 목적: subgraph 실행 경로의 fanout 분기 병합도 task-001과 동일하게 리듀서 경유 누적으로 바꿔, subgraph 내부에서도
    분기 업데이트가 보존되게 한다.
  - 접근: `graph/subgraph.go`의 `invokeSubgraph`·`invokeSubgraphFromNode`의 분기 병합 루프에서 raw 덮어쓰기를 task-001과
    같은 헬퍼·패턴(분기 델타 산출 → `applyReducers`)으로 교체한다. `branchResult.parentCmd != nil`이어서 병합을 거치지 않고
    즉시 전파하는 분기는 종전대로 둔다(병합 변경과 충돌 없음).
  - 검증 조건:
    - 결과: subgraph 안에서 같은 리듀서 키를 갱신하는 둘 이상의 분기가 실행되면 모든 업데이트가 병합되어 보존된다.
    - 확인: subgraph fanout을 검증하는 기존/신규 테스트에서 누적 키 보존을 확인한다. `go test ./graph/...` 통과.
  - 참조: SPEC §5.1, §5.3 / ANALYSIS §1.1, §2.2

- [x] task-003: Fanout 분기 병합 리듀서 경유 (Stream 경로)
  - 목적: Stream 실행 경로의 fanout 분기 병합도 동일 의미로 교정해 Invoke·subgraph와 일관되게 한다.
  - 접근: `graph/stream.go`의 `streamLoop`·`streamFromNode`·`streamSubgraphLoop`의 분기 병합 루프에서 raw 덮어쓰기를
    task-001 헬퍼·패턴으로 교체한다. 토큰/이벤트 방출 순서는 유지하고 병합 의미만 바꾼다.
  - 검증 조건:
    - 결과: Stream 실행에서도 같은 리듀서 키를 갱신하는 둘 이상의 분기 업데이트가 모두 보존되며, Invoke·subgraph와 동일한
      최종 상태가 관찰된다.
    - 확인: Stream fanout 회귀 테스트에서 누적 키 보존을 확인한다. `go test ./graph/...` 통과.
  - 참조: SPEC §5.3 / ANALYSIS §1.1, §2.1

- [ ] task-004: Fanout 병합 일관성 e2e 검증
  - 목적: 분기 병합·격리 의미가 Invoke·Stream·subgraph 세 경로에서 동일하게 관찰됨을 한 시나리오로 교차 검증한다.
  - 접근: 같은 fanout 그래프(두 Send 분기가 `messages` 누적 키를 갱신, 입력은 fanout 직전 상태 기준)를 세 실행 경로로
    각각 구동해 최종 상태를 비교하는 e2e 테스트를 추가한다. 한 분기의 상태 쓰기가 같은 단계 다른 분기의 입력에 반영되지
    않음(격리)도 확인한다. 기존 fanout 테스트 중 last-write-wins 결과를 기대하던 것이 있으면 새 병합 의미에 맞게 기대값을
    조정한다.
  - 검증 조건:
    - 결과: 세 경로 모두 두 분기 메시지를 보존하고 동일한 최종 상태를 낸다. 분기 격리가 깨지지 않는다.
    - 확인: 추가한 e2e 테스트와 기존 테스트가 모두 통과한다. `go test ./graph/... ./multiagent/...` 통과.
  - 참조: SPEC §5.1, §5.2, §5.3 / ANALYSIS §2.1, §2.2

- [ ] task-005: 도달성 인접에 WithDestinations 포함
  - 목적: 정적 엣지·조건 엣지 없이 `command.Goto`(또는 노드 `WithDestinations` 선언)만으로 도달하는 노드가 Compile
    도달성 검사에서 거부되지 않게 한다.
  - 접근: `graph/validate.go`의 `buildAdjacency`에서 `b.nodes`를 순회해 각 `nodeEntry.destinations`를 `adj[name]`에
    덧붙인다(정적·조건 엣지 인접에 합산). `validateNodeRefs`에 destinations 대상 존재 검사는 추가하지 않는다(DP2 미채택,
    런타임 `validateGotoTarget`에 맡김). BFS 방문 집합은 단조 증가하므로 기존에 통과하던 그래프는 계속 통과한다.
  - 검증 조건:
    - 결과: 보조 `AddEdge` 없이 `WithDestinations`만 선언한 노드를 가진 그래프가 Compile에 성공하고, 실행 시 Goto로 해당
      노드에 도달한다. 기존 Compile 통과 그래프는 여전히 통과한다.
    - 확인: `graph/command_test.go`의 Goto 테스트 중 최소 한 개에서 보조 `AddEdge`를 제거하고 `WithDestinations`만으로
      Compile·Invoke가 통과하도록 정비한다. `go test ./graph/...` 통과.
  - 참조: SPEC §5.4 / ANALYSIS §1.2, §2.3

- [ ] task-006: multiagent network 더미 조건엣지 제거
  - 목적: 도달성 수정(task-005)의 결과로 불필요해진 multiagent network 그래프의 컴파일 통과용 더미 조건엣지를 제거한다.
  - 접근: `multiagent/network.go`의 `BuildNetwork`에서 워커가 2개 이상일 때 `dummyRouter`로 더미 조건 엣지를 추가하던
    블록을 제거하고, `dummyRouter` 변수와 관련 주석도 함께 제거한다. 워커 노드는 이미 `WithDestinations(names...)`를 선언하고
    있으므로 task-005 변경으로 도달성이 충족된다.
  - 검증 조건:
    - 결과: `multiagent` network 그래프가 더미 조건엣지 없이 Compile되고 실행된다.
    - 확인: `multiagent/e2e_test.go`의 더미 조건 엣지·`dummyRouter` 블록도 제거하고, 그래도 Compile·실행이 통과하는지
      확인한다. `go test ./multiagent/...` 통과.
  - 참조: SPEC §5.5 / ANALYSIS §1.3, §2.3, §4.2

- [ ] task-007: agent.Stream 모델 호출을 미들웨어 체인 경유로
  - 목적: `agent.Stream`의 모델 호출이 `agent.Invoke`와 동일하게 `WrapModelCall`·`BeforeModel`·`DynamicPrompt`
    미들웨어 체인을 거치게 해, 동적 프롬프트 수정·before_model 차단·모델 오버라이드가 스트리밍 경로의 관찰 가능한 출력에
    반영되게 한다.
  - 접근: `agent/agent.go`의 `runModelStream`을 재배치한다(옵션 B). (1) 스트림 진입 전 `middlewareChain`으로 `ModelRequest`를
    확정한다 — `BeforeModel` 차단 시 에러 이벤트로 조기 종료(토큰 미방출), `DynamicPrompt`의 SystemPrompt 치환, Override된
    Model 반영. (2) 확정된 요청으로 `ChatStream`을 호출해 토큰을 원본대로 방출한다. (3) 스트림 종료 후 `ChatEventDone`의 최종
    `ChatResponse`를 `ModelResponse`로 감싸 `WrapModelCall` 체인의 응답 가공부에 한 번 통과시켜(터미널은 수집한 응답 반환)
    최종 메시지를 확정한다. 현 `ChatEventDone` 처리(tool_calls 재구성)와 겹치지 않도록 "최종 응답 수집 → WrapModelCall 통과 →
    최종 메시지 확정" 순서를 명확히 둔다. 공개 타입·시그니처(`Stream`/`Invoke`/`Create`, `middleware` 계약)는 바꾸지 않는다.
  - 검증 조건:
    - 결과: Stream 실행 시 DynamicPrompt의 SystemPrompt 치환이 방출 토큰/최종 메시지에 반영되고, BeforeModel 차단 시 토큰이
      방출되지 않고 에러로 끝나며, WrapModelCall의 응답 가공이 최종 메시지·상태에 반영된다. 미들웨어를 쓰지 않는 호출(빈 Chain)은
      동작이 불변이다.
    - 확인: stub LLM을 활용해 Stream 경로에서 DynamicPrompt·BeforeModel·WrapModelCall 각각이 적용됨을 관찰하는 테스트를
      추가한다. `go test ./agent/...` 통과.
  - 참조: SPEC §5.6 / ANALYSIS §1.4, §2.4, §4.3, §5 DP1
