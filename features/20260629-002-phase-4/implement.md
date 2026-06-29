# phase-4 — 멀티에이전트 구현 체크리스트

이 문서는 spec.md §5(완료 조건)와 analysis.md(구조·Decision Points)를 실행 단위로 옮긴 순수 체크리스트다.
설계 근거는 analysis.md에 있다. 순서는 의존(다음이 가능하려면 무엇이 먼저 존재해야 하는가)을 따른다.

## Section: 워커 구성 어댑터

- [x] task-001: 워커 구성 어댑터(AgentAsNode/GraphAsNode/AgentAsTool) 구현
  - 목적: 에이전트와 서브그래프를 그래프 노드로, 에이전트를 도구로 노출해 상위 조립에서 실행 단위로 쓸 수 있다.
  - 접근: agent의 직접 루프(Invoke/Stream)와 graph의 AsNode를 호출만 하는 얇은 어댑터로 구현한다. agent 재배치 리팩터링은 하지 않는다.
  - 검증 조건:
    - 결과: AgentAsNode/GraphAsNode가 graph.NodeFunc를 반환하고 그래프 노드로 실행되면 에이전트·서브그래프 결과가 상태에 반영된다. AgentAsTool이 반환한 tool.Tool을 Execute하면 감싼 에이전트가 실행되어 결과가 도구 출력으로 나온다.
    - 확인: llm.StubClient로 만든 stub 에이전트를 어댑터로 감싸 노드/도구로 실행하는 결정적 단위 테스트를 작성해 통과. `go build ./...`·`go vet ./...` 무오류.
  - 참조: SPEC §5.3, SPEC §5.1 / ANALYSIS §1, ANALYSIS §5.7

## Section: 워커 추상화

- [x] task-002: 워커 추상화(Worker/WorkerRegistry/WorkerOutput) 구현
  - 목적: 이름 붙은 실행 단위를 레지스트리에 등록하고 이름으로 조회·열거할 수 있다.
  - 접근: Worker 인터페이스(Name/Description/Invoke/Stream)와 RegisterWorker/GetWorker/WorkerNames를 가진 레지스트리, 그리고 WorkerOutput{Messages, StructuredResponse}를 정의한다.
  - 검증 조건:
    - 결과: RegisterWorker로 등록한 워커를 GetWorker(name)로 조회하면 동일 워커가 반환되고, WorkerNames가 등록된 모든 이름을 열거한다. 미등록 이름 조회 시 not-found가 구분된다.
    - 확인: stub 워커를 등록/조회/열거하는 결정적 단위 테스트를 작성해 통과. `go build ./...`·`go vet ./...` 무오류.
  - 참조: SPEC §5.2, SPEC §5.1 / ANALYSIS §1

## Section: 수퍼바이저 라우팅

- [x] task-003: 수퍼바이저 라우팅(RouterTool/SelectNext/Route/MergeWorkerResult) 구현
  - 목적: 수퍼바이저가 라우터 도구 호출로 다음 워커를 정해 그 노드로 이동하고, 워커 산출을 상태에 병합하며, 라우터 미호출 시 종료(또는 상위 복귀)한다.
  - 접근: RouterTool로 라우터 도구를 만들고, SelectNext가 마지막 AI 메시지의 tool_calls를 RouterChoice로 해석하며, Route가 graph.State 위에서 command.Goto/End/ToParent를 분기 반환한다. MergeWorkerResult는 WorkerOutput 메시지를 상태에 병합만 한다(최종 통합은 LLM 프롬프트 책임).
  - 검증 조건:
    - 결과: 라우터 도구를 호출한 stub 응답이면 SelectNext가 next 워커 이름을 뽑고 Route가 해당 노드로 Goto한다. 라우터 미호출이면 Route가 End(또는 상위 복귀 구성에서 ToParent)를 반환한다. 미등록 워커 이름은 명시 에러로 처리된다. MergeWorkerResult 호출 후 워커 산출 메시지가 상태에 존재한다.
    - 확인: llm.StubClient로 라우터 호출/미호출/미등록 케이스를 구성한 결정적 단위 테스트를 작성해 통과. `go build ./...`·`go vet ./...` 무오류.
  - 참조: SPEC §5.4, SPEC §5.1 / ANALYSIS §2, ANALYSIS §5.1, ANALYSIS §5.2, ANALYSIS §5.5

## Section: 핸드오프

- [x] task-004: 핸드오프(CreateHandoffTool/HandoffBackMessages, ToParent/Fanout) 구현
  - 목적: 도구 기반 위임으로 단일 핸드오프는 부모 그래프 대상으로 이동하고 복수 호출은 워커마다 다른 입력으로 분배하며, 워커→수퍼바이저 복귀 메시지 쌍을 만든다.
  - 접근: CreateHandoffTool이 tool.Tool을 반환하고, Execute가 Runtime의 state/tool_call_id로 단일이면 command.ToParent, 복수 tool_calls면 command.Fanout([]Send{TargetParent})를 반환한다. HandoffBackMessages는 AI(tool_calls)+Tool(result) 메시지 쌍을 message 생성자로 구성한다. 복귀 goto 자체는 정적 엣지가 담당한다.
  - 검증 조건:
    - 결과: 단일 위임 시 도구가 ToParent Command를, 복수 tool_calls 시 Fanout([]Send) Command를 반환하고 각 Send가 부모 대상이며 워커마다 입력이 다르다. HandoffBackMessages가 AI/Tool 쌍을 반환하고 ToolCallID가 짝지어진다.
    - 확인: stub Runtime/메시지로 단일·다중 위임과 복귀 메시지 쌍을 검증하는 결정적 단위 테스트를 작성해 통과. `go build ./...`·`go vet ./...` 무오류.
  - 참조: SPEC §5.5, SPEC §5.1 / ANALYSIS §2, ANALYSIS §5.3, ANALYSIS §5.4

## Section: 네트워크

- [x] task-005: 네트워크(BuildNetwork/IsFinalAnswer) 구현
  - 목적: 워커들을 노드로 묶어 워커 간 동적 라우팅 그래프를 컴파일하고, 메시지에서 종료 신호를 판별한다.
  - 접근: BuildNetwork(workers)가 각 워커를 노드로 등록하고 command.Goto 동적 라우팅으로 연결한 graph.Compiled를 반환한다. IsFinalAnswer가 메시지에서 종료 신호를 판별한다. 빈 워커 목록은 graph.Compile validate에서 에러로 드러난다.
  - 검증 조건:
    - 결과: 워커 여러 개로 BuildNetwork를 호출하면 컴파일된 그래프가 반환되고, stub 워커가 Goto로 다음 워커로 왕복하다 IsFinalAnswer 신호에서 End로 끝난다. 빈 워커 목록은 에러를 반환한다.
    - 확인: stub 워커로 네트워크를 구성해 Goto 왕복과 종료를 검증하는 결정적 단위 테스트, 빈 목록 에러 케이스를 작성해 통과. `go build ./...`·`go vet ./...` 무오류.
  - 참조: SPEC §5.6, SPEC §5.1 / ANALYSIS §2, ANALYSIS §1

## Section: 플래너

- [x] task-006: 플래너(Plan/Replan/RecordStep/PlannerState/Step) 구현
  - 목적: 입력으로 계획을 만들고 한 스텝씩 소비하며 결과를 누적해 재계획하다 빈 계획에서 최종 응답으로 끝낸다.
  - 접근: Plan/Replan이 llm.Structured로 structured.PlannerResult를 반환한다(스키마는 structured 기존 타입 재사용). RecordStep이 소비한 Step(Task/Result)을 PlannerState.PastSteps에 누적하는 StateUpdate를 만든다. PlannerState/Step만 multiagent 신규 타입으로 둔다.
  - 검증 조건:
    - 결과: Plan이 Action=plan일 때 Steps를, Action=respond일 때 Response를 가진 PlannerResult를 반환한다. RecordStep 후 PastSteps에 해당 Step이 누적된다. plan 한 스텝 소비→RecordStep→Replan 루프가 빈 plan(Action=respond)에서 최종 응답으로 끝난다.
    - 확인: llm.StubClient(structured 응답 시퀀스)로 plan→record→replan→respond 루프를 결정적으로 검증하는 단위 테스트를 작성해 통과. `go build ./...`·`go vet ./...` 무오류.
  - 참조: SPEC §5.7, SPEC §5.1 / ANALYSIS §2, ANALYSIS §5.6

## Section: 종단·경계 검증

- [x] task-007: 실제 Claude 수퍼바이저+워커 e2e 테스트(ANTHROPIC_API_KEY 가드)
  - 목적: 실제 Claude 챗 모델로 수퍼바이저+워커를 실행하면 질의가 적합한 워커로 위임되고 워커 결과가 통합되어 최종 응답이 나온다.
  - 접근: 라우팅·핸드오프·통합을 묶는 e2e 테스트를 별도 _test.go로 작성한다. llm.InitChatModel("anthropic:...")로 실 모델을 만들고 ANTHROPIC_API_KEY 부재 시 t.Skip한다.
  - 검증 조건:
    - 결과: 키가 있을 때 e2e가 워커 위임과 결과 통합을 거쳐 최종 응답을 산출하고 통과한다. 키가 없을 때 t.Skip으로 건너뛰며 빌드·정적검사·다른 테스트는 영향받지 않는다.
    - 확인: vectorstore/e2e_test.go의 skip 가드 컨벤션에 맞춰 작성. 키 없는 환경에서 `go test ./multiagent/...`가 skip 포함 통과, `go vet ./...` 무오류.
  - 참조: SPEC §5.8, SPEC §5.1 / ANALYSIS §1, ANALYSIS §5.8

- [x] task-008: import 경계 회귀 테스트
  - 목적: multiagent가 하위 패키지만 import하고 하위 패키지가 multiagent를 역참조하지 않으며 Phase 1~3 기존 동작이 보존됨을 회귀로 보호한다.
  - 접근: graph/import_boundary_test.go의 go list -deps 패턴을 따라 multiagent의 의존이 허용 집합(agent/graph/graph/command/tool/llm/structured/message/core) 안인지, 하위 패키지 deps에 multiagent가 없는지 검사한다.
  - 검증 조건:
    - 결과: multiagent deps에 모듈 외 상위 패키지가 없고, agent·graph·tool 등 하위 패키지 deps에 multiagent가 없다. Phase 1~3 패키지 코드가 수정되지 않은 상태로 모듈 전체 빌드·정적검사가 통과한다.
    - 확인: 경계 테스트가 통과. `go build ./...`·`go vet ./...`·`go test ./...`가 모듈 루트에서 무오류.
  - 참조: SPEC §5.9, SPEC §5.1 / ANALYSIS §1, ANALYSIS §5.8
