# phase-2 — 그래프 엔진: 구현 체크리스트

> 순수 실행 체크리스트. 설계 근거는 `analysis.md` §5(D1~D11), 요구사항 레벨 완료 조건은 `spec.md` §5에 둔다.
> 순서는 의존성(line order) 기준이며 Task ID는 문서 전체에서 연속한다. 모든 검증은 stub 노드(정해진
> `StateUpdate`/`command.Command` 반환)로 네트워크·API 키 없이 수행한다(ANALYSIS §5 D11).

## Section: command·streaming leaf 패키지

- [x] task-001: command 제어 흐름 타입과 생성자
  - 목적: 노드 코드가 `Goto`/`End`/`ToParent`/`Fanout`/`NewSend`로 제어 흐름과 상태 갱신을 함께 담은 값을
    만들고, `IsEnd`/`IsParent`로 그 종류를 판별할 수 있다.
  - 접근: `Command`/`Send`/`GraphTarget`(current/parent)과 생성자·판별 메서드를 `core`만 의존해 정의하고
    update 인자는 `core.StateUpdate`로 둔다.
  - 검증 조건:
    - 결과: `Goto`/`End`/`ToParent`가 각 표식(목적지·종료·부모)과 update를 보존하고, `Fanout([]Send)`이
      Send 목록을 보존하며, `NewSend`가 target·state·Graph를 담는다. `IsEnd`/`IsParent`가 올바른 불린을 낸다.
    - 확인: 각 생성자·판별자에 대한 단위 테스트로 필드·불린을 검증하고 `go build`/`go vet`이 무오류다.
  - 참조: SPEC §5.5, ANALYSIS §3.2, ANALYSIS §5 D5

- [x] task-002: streaming 이벤트 타입과 방출 헬퍼
  - 목적: 호출자·노드 코드가 `EmitNodeUpdate`/`EmitStateValue`/`EmitMessageToken`/`EmitSubgraph`로 core
    타입만 가지고 독립 스트림 이벤트를 조립하고, `Mode`/`Event`/`Metadata`/`Options`를 쓸 수 있다.
  - 접근: `Mode = core.Mode` alias와 `Event`/`Metadata`/`Options`, 네 방출 헬퍼를 `core`만 의존해 정의한다.
  - 검증 조건:
    - 결과: 각 헬퍼가 입력 core 값을 대응 `Event` 필드(Node/Update/Value/Token/Metadata)에 채우고,
      `EmitSubgraph(path, inner)`가 path를 전파한 Event를 만든다. `Options.Subgraphs`/`Mode`가 설정된다.
    - 확인: 각 헬퍼 출력 Event 필드를 검증하는 단위 테스트와 `go build`/`go vet` 무오류.
  - 참조: SPEC §5.8, ANALYSIS §3.3, ANALYSIS §5 D7

## Section: graph 빌더·컴파일·검증

- [x] task-003: StateGraph 빌드·컴파일과 그래프 검증
  - 목적: 호출자가 `NewStateGraph`로 필드별 리듀서 스키마를 만들고 `AddNode`(+`WithDestinations`)·`AddEdge`·
    `AddConditionalEdges`·`SetEntryPoint`·`SetConditionalEntryPoint`로 그래프를 구성한 뒤 `Compile`로 실행
    가능한 결과를 얻으며, 도달 불가 노드·미정의 엣지 등 잘못된 그래프는 컴파일 시 error로 거부된다.
  - 접근: `Builder`에 노드·엣지·조건엣지·진입점을 누적하고, `Compile`이 `validate`를 거쳐 불변 `Compiled`를
    만들며 `graph.State`/`StateUpdate`/`StateSnapshot`은 core alias로 둔다.
  - 검증 조건:
    - 결과: 정상 그래프는 `Compile`이 성공하고, 미정의 노드를 가리키는 엣지·진입점에서 도달 불가한 노드를
      가진 그래프는 `Compile`이 error를 반환한다.
    - 확인: 정상/도달불가/미정의엣지 그래프 각각에 대한 단위 테스트로 Compile 성공·error를 검증한다.
  - 참조: SPEC §5.2, SPEC §5.1, ANALYSIS §1.2, ANALYSIS §5 D1, ANALYSIS §5 D3

## Section: graph 실행 루프·리듀서·command 흐름

- [x] task-004: Invoke 실행 루프와 필드별 리듀서 병합
  - 목적: `Compiled.Invoke`가 진입점부터 엣지를 따라 노드를 실행하고, 각 노드가 반환한 `StateUpdate`를
    스키마에 등록된 필드별 리듀서로 병합(미등록 필드는 덮어쓰기)해 최종 `State`를 반환한다.
  - 접근: `runNode`가 노드 반환 `any`를 `command.Command`/`core.StateUpdate`/`nil`로 타입 스위치해
    `NodeResult`로 정규화하고, `applyReducers`가 update 키별 리듀서/last-write-wins로 병합하는 루프를 돈다.
  - 검증 조건:
    - 결과: 등록된 리듀서 필드는 stub 노드들의 update가 누적 병합되고, 미등록 필드는 마지막 값으로 덮어써져
      최종 State에 반영된다. 지원하지 않는 반환 타입은 error다.
    - 확인: 누적 리듀서 필드와 덮어쓰기 필드를 가진 stub 노드 그래프를 Invoke해 최종 State를 검증하고,
      잘못된 반환 타입 노드가 error를 내는지 확인하는 단위 테스트.
  - 참조: SPEC §5.3, ANALYSIS §2.1, ANALYSIS §2.2, ANALYSIS §2.4, ANALYSIS §5 D2, ANALYSIS §5 D4

- [x] task-005: maxSteps 무한 루프 차단
  - 목적: 순환 구조 그래프가 무한히 실행되지 않고 최대 스텝 한도에서 멈춰 error로 끝난다.
  - 접근: 실행 루프가 매 스텝 `checkMaxSteps(step)`로 한도 초과를 검사해 초과 시 error를 반환한다.
  - 검증 조건:
    - 결과: 종료 없는 순환 그래프를 Invoke하면 한도에서 멈추고 error를 반환하며, 한도 내 그래프는 정상 종료한다.
    - 확인: 자기 자신으로 되돌아가는 stub 노드 그래프가 한도 초과 error를 내는지 검증하는 단위 테스트.
  - 참조: SPEC §5.10, ANALYSIS §2.1, ANALYSIS §5 D3

- [x] task-006: 조건엣지 라우팅과 조건 진입점
  - 목적: `AddConditionalEdges`의 라우터가 mapping에 따라 다음 노드를 선택하고, `SetConditionalEntryPoint`로
    첫 노드도 라우터+mapping으로 결정된다.
  - 접근: `resolveNext`가 Command가 없을 때 조건엣지 라우터 키를 mapping으로 노드 이름에 매핑하고, 시작
    노드 결정 시 조건 진입점 라우터를 먼저 실행한다.
  - 검증 조건:
    - 결과: 라우터 반환 키에 따라 서로 다른 분기 노드가 실행되고, 조건 진입점이 상태에 따라 다른 첫 노드로
      진입한다.
    - 확인: 라우터 키별로 다른 경로를 타는 stub 그래프와 조건 진입점 그래프를 Invoke해 실행 경로를 검증하는
      단위 테스트.
  - 참조: SPEC §5.4, ANALYSIS §2.1, ANALYSIS §2.3, ANALYSIS §5 D3

- [x] task-007: command 기반 제어 흐름 실행
  - 목적: 노드가 `command.Command`를 반환하면 `Goto`는 대상 노드로 이동하며 update를 적용하고, `End`는
    그래프를 종료하며, `Fanout([]Send)`은 다중 분기해 각 Send가 분기마다 다른 상태를 분배한다.
  - 접근: `resolveNext`가 `res.Control`을 End/Goto/Fanout으로 해석해 종료·이동·다중 분기를 처리하고,
    Goto target은 `WithDestinations` 선언에 대해 validate된 것만 허용하며 분기별 상태를 분배 실행한다.
  - 검증 조건:
    - 결과: Goto stub은 대상 노드로 이동하며 update가 적용되고, End stub은 즉시 종료하며, Fanout stub은 각
      Send target이 해당 Send 상태로 실행된다.
    - 확인: Goto/End/Fanout을 각각 반환하는 stub 노드 그래프를 Invoke해 이동·종료·분기별 상태를 검증하는
      단위 테스트.
  - 참조: SPEC §5.5, ANALYSIS §2.2, ANALYSIS §2.3, ANALYSIS §5 D5

- [x] task-008: 입출력 스키마 분리
  - 목적: `WithInputSchema`/`WithOutputSchema`를 지정하면 Invoke 입력이 입력 스키마 필드로 필터링되고
    출력이 출력 스키마 필드로 추출되는 것이 관찰된다.
  - 접근: Invoke 시작에서 입력 스키마로 input을 필터링하고 종료에서 출력 스키마로 최종 State를 추출한다.
  - 검증 조건:
    - 결과: 입력 스키마 밖 필드는 노드에 전달되지 않고, 출력 스키마 밖 필드는 최종 반환 State에서 빠진다.
    - 확인: 스키마 밖 필드를 포함한 입력으로 Invoke해 노드 관찰 입력과 최종 출력 키 집합을 검증하는 단위 테스트.
  - 참조: SPEC §5.6, ANALYSIS §2.1, ANALYSIS §5 D9

## Section: 서브그래프·스트림·상태 조회

- [x] task-009: 서브그래프(공유·독립 상태)
  - 목적: 컴파일된 그래프를 다른 그래프의 노드로 등록해 실행할 수 있고, 부모와 상태를 공유하거나 입출력
    스키마로 가린 독립 상태로 실행되며, `ToParent`/부모 대상 `Send`가 서브그래프에서 부모 노드로 라우팅한다.
  - 접근: `Compiled`를 NodeFunc 어댑터로 감싸 부모 노드로 등록하고, 공유 모드는 부모 상태를 그대로 넘기며
    독립 모드는 입력/출력 스키마로 경계를 가리고, `GraphTarget`으로 Send/Command의 대상 그래프를 구분한다.
  - 검증 조건:
    - 결과: 공유 모드 서브그래프는 부모 상태 변경을 부모로 반영하고, 독립 모드는 입력 필터·출력 추출 경계만
      넘기며, ToParent/부모 Send를 반환한 서브그래프 노드가 부모 노드로 라우팅된다.
    - 확인: 서브그래프를 노드로 등록한 부모 그래프를 공유·독립·ToParent 시나리오로 Invoke해 상태 전파를
      검증하는 단위 테스트.
  - 참조: SPEC §5.7, SPEC §5.5, SPEC §5.6, ANALYSIS §2.5, ANALYSIS §5 D6

- [x] task-010: 모드별 스트림 이벤트 방출과 서브그래프 경로 전파
  - 목적: `Compiled.Stream(mode core.Mode)`이 모드별로 이벤트를 방출한다 — `values`는 전체 상태 스냅샷,
    `updates`는 노드별 변경분, `messages`는 토큰 단위, `debug`는 진단 이벤트. `Subgraphs` 옵션이 켜지면
    서브그래프 이벤트가 경로(path)와 함께 전파된다.
  - 접근: Invoke와 같은 루프를 돌며 단계마다 mode에 따라 `GraphEvent`를 채널로 방출하고, Subgraphs 옵션 시
    서브그래프 이벤트에 path를 채워 전파한다(graph는 streaming을 import하지 않는다).
  - 검증 조건:
    - 결과: 각 모드에서 해당 페이로드(전체 상태/변경분/토큰/진단)를 담은 GraphEvent가 순서대로 방출되고,
      Subgraphs 켜짐 시 서브그래프 이벤트의 Path가 채워진다.
    - 확인: stub 그래프를 모드별로 Stream해 수집한 GraphEvent 시퀀스와 페이로드·Path를 검증하는 단위 테스트.
  - 참조: SPEC §5.8, ANALYSIS §1.3, ANALYSIS §2.6, ANALYSIS §5 D7

- [x] task-011: 상태 조회·갱신과 체크포인터 영속
  - 목적: `GetState`/`GetStateHistory`가 `StateSnapshot`을 반환하고 `UpdateState`가 수동 상태 갱신을 적용하며,
    `Compile(WithCheckpointer)` 지정 시 동일 thread_id에서 상태가 별도 `Invoke` 호출 간에 영속된다.
  - 접근: 실행 루프가 체크포인터 지정 시 스텝마다 thread_id로 SaveState하고 시작 시 LoadState로 병합하며,
    GetState/GetStateHistory/UpdateState는 core alias StateSnapshot으로 checkpoint API를 결합한다.
  - 검증 조건:
    - 결과: WithCheckpointer로 같은 thread_id에 두 번 Invoke하면 두 번째가 첫 번째 상태를 이어받고,
      GetState/GetStateHistory가 스냅샷을 반환하며 UpdateState 후 GetState에 갱신이 반영된다.
    - 확인: InMemorySaver를 결합한 stub 그래프로 thread_id 영속·조회·수동 갱신을 검증하는 단위 테스트.
  - 참조: SPEC §5.9, ANALYSIS §2.1, ANALYSIS §5 D8

## Section: 시각화·import 경계

- [x] task-012: DrawMermaid 텍스트와 패키지 import 경계
  - 목적: `DrawMermaid`가 컴파일된 그래프의 mermaid 텍스트 표현을 반환하고, import 그래프 검사로
    `command`·`streaming`이 `graph`를 import하지 않고 `core`만 참조하며 `graph`가 `command`를 import함을
    확인할 수 있다(Phase 1 패키지 미수정).
  - 접근: `DrawMermaid`가 노드·엣지를 mermaid flowchart 문자열로 렌더하고, 세 패키지가 모두 존재하는 시점에
    import 의존 방향을 검사한다(`DrawMermaidPNG`는 정의하지 않는다).
  - 검증 조건:
    - 결과: stub 그래프의 `DrawMermaid`가 노드·엣지를 포함한 mermaid 텍스트를 반환하고, `command`/`streaming`
      의존성에 `graph`가 없으며 `graph` 의존성에 `command`가 있다. Phase 1 패키지는 변경되지 않는다.
    - 확인: DrawMermaid 출력 문자열을 검증하는 단위 테스트와 `go list -deps`(또는 import 점검)로 경계 확인,
      `go build ./...`/`go vet ./...` 무오류 및 Phase 1 파일 미변경 확인.
  - 참조: SPEC §5.12, SPEC §5.11, SPEC §5.1, ANALYSIS §1.1, ANALYSIS §4, ANALYSIS §5 D10, ANALYSIS §5 D11
