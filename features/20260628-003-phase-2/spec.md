# phase-2 — 그래프 엔진

## 1. 범위

langgraph-go의 상태 그래프 실행 엔진을 구성하는 Phase 2다(README §26 Phase 2). 다음 세 패키지를 다룬다.

- `graph` — StateGraph 빌드·컴파일·실행. 필드별 리듀서를 등록하는 상태 스키마, 노드·엣지·조건엣지·진입점·
  destinations, 입출력 스키마 분리, 서브그래프(컴파일된 그래프를 노드로), 상태 조회·갱신(GetState/
  GetStateHistory/UpdateState), mermaid 텍스트 시각화.
- `graph/command` — 노드 반환으로 제어 흐름과 상태 갱신을 함께 표현하는 Command(Goto/End/ToParent/Fanout)와
  Send, GraphTarget(current/parent).
- `streaming` — 스트림 모드(`core.Mode` alias)와 이벤트(Event/Metadata/Options), 방출 헬퍼, 서브그래프 이벤트
  전파.

이 Phase의 통합 산출물은 "노드/엣지/조건엣지/진입점/destinations/입출력 스키마/서브그래프, 스트림 모드
(values/messages/updates/subgraphs)가 동작한다"이다(README §26 Phase 2).

## 2. 목표

다운스트림이 `StateGraph`로 임의의 상태 그래프를 조립·실행할 수 있는 범용 그래프 엔진을 제공한다. Command 기반
제어 흐름 + 필드별 상태 리듀서 + 스트림 모드 + 서브그래프를 갖춰, 이후 Phase의 응용 계층(멀티에이전트·RAG 등,
README §14·§17)이 이 primitive 위에 직접 그래프를 구성할 토대를 만든다. 엔진은 Phase 0 leaf 타입(`core`)을
공유 상태로 쓰되, Phase 1 `agent`(자체 직접 루프 유지)와는 독립적으로 동작한다.

## 3. 제약

- import 경계(README §28-1): `graph`의 `State`/`StateUpdate`는 `core` 타입의 alias(`type State = core.State`),
  `StateSnapshot`은 `core.StateSnapshot` alias, 스트림 모드 인자는 `core.Mode`를 직접 참조한다. `command`와
  `streaming`은 `graph`를 import하지 않고 `core`만 참조해 `graph ↔ command`·`graph ↔ streaming` 순환을 끊는다.
  `graph`는 `command`를 import한다(NodeFunc가 `command.Command`를 반환). `Compile(WithCheckpointer)`의
  `graph → checkpoint` 단방향 의존은 `checkpoint`가 `graph`를 참조하지 않으므로 순환을 만들지 않는다.
- Phase 2는 Phase 1 패키지를 수정하지 않는다. `agent`는 Phase 1의 직접 루프를 그대로 유지하고, `prebuilt`
  노드를 graph 엔진에 정합시키는 작업은 이 Phase 범위 밖이다(아래 §4).
- 그래프 실행은 최대 스텝 한도를 강제해 순환 그래프가 무한히 돌지 않게 한다.
- `graph`/`command`/`streaming` 코어 엔진은 순수 Go로 구현하며 새 외부 의존성을 도입하지 않는다. mermaid
  시각화는 텍스트(`DrawMermaid`)로 제공한다.
- 검증은 실제 LLM 없이 stub 노드 함수(정해진 StateUpdate/Command를 반환)로 그래프 동작을 구동해 수행한다.
  네트워크·API 키가 필요하지 않다.

## 4. 제외 범위

- `agent`를 graph 엔진 위로 재배치하는 리팩터링은 제외한다. Phase 1의 직접 루프를 유지한다(README §9의 "내부적
  으로 그래프로 컴파일"은 이후 별도 작업).
- `prebuilt` 노드(ToolNode/ToolsCondition/SummarizationNode)를 `graph.NodeFunc` 시그니처에 정합시키는 통합은
  제외한다(deferred). Phase 2에서 컴파일된 그래프는 직접 작성한 노드 함수로 검증한다.
- 그래프 인터럽트(`interrupt`/`resume`) 기반 HITL은 제외한다. 사용자 추가 입력은 이후 A2A `input_required`로
  처리한다(README §7·§22-4).
- `DrawMermaidPNG`(PNG 렌더링)는 외부 렌더링 의존을 피하기 위해 이 Phase에서 제외한다(텍스트 `DrawMermaid`로
  충분). 필요 시 후속 작업으로 둔다.
- Phase 3 이후 패키지(`document`/`vectorstore`/`store`/`mcp`/`a2a`/`database`/`search`/`storage`/`trace` 등)와
  응용 계층(`rag`/`multiagent`/`orchestrator`)은 범위 밖이다.
- 새 체크포인트 백엔드는 추가하지 않는다. graph는 Phase 1의 `checkpoint.Checkpointer` 인터페이스(InMemorySaver)를
  그대로 결합한다.

## 5. 완료 조건

1. 모듈 루트에서 `go build ./...`와 `go vet ./...`가 오류 없이 끝난다(graph/command/streaming 신규 패키지가
   추가되고 Phase 1 패키지는 수정되지 않은 상태).
2. 호출자가 `NewStateGraph`로 필드별 리듀서를 등록한 상태 스키마와 함께 그래프를 만들고, `AddNode`
   (+`WithDestinations`)·`AddEdge`·`AddConditionalEdges`·`SetEntryPoint`·`SetConditionalEntryPoint`로 노드·엣지·
   진입점을 구성한 뒤 `Compile`로 실행 가능한 컴파일 결과를 얻는다. 도달 불가 노드·미정의 엣지 등 잘못된 그래프는
   컴파일 시 error로 거부된다.
3. `Compiled.Invoke`가 진입점부터 엣지를 따라 노드를 실행하고, 각 노드가 반환한 `StateUpdate`를 상태 스키마에
   등록된 필드별 리듀서로 병합해 최종 `State`를 반환한다.
4. `AddConditionalEdges`의 라우터가 mapping에 따라 다음 노드를 선택하고, 조건 진입점(`SetConditionalEntryPoint`)도
   동작한다.
5. 노드가 `command.Command`를 반환하면 그에 따라 흐름이 결정된다: `Goto`는 대상 노드로 이동하며 update를 적용하고,
   `End`는 그래프를 종료하며, `ToParent`는 부모 그래프 대상으로 이동하고, `Fanout([]Send)`은 다중 분기한다(부모
   그래프 대상 가능). 각 `Send`는 분기마다 다른 상태를 분배한다.
6. `WithInputSchema`/`WithOutputSchema`로 입력·출력 스키마를 분리하면, 입력 필터링과 출력 추출이 관찰된다.
7. 컴파일된 그래프를 다른 그래프의 노드로 등록해 실행할 수 있다(서브그래프). 서브그래프는 부모와 상태를
   공유하거나 독립 상태로 실행된다.
8. `Compiled.Stream(mode core.Mode)`이 모드별로 이벤트를 방출한다: `values`는 전체 상태 스냅샷, `updates`는 노드별
   변경분, `messages`는 토큰 단위, `debug`는 진단 이벤트. `Subgraphs` 옵션이 켜지면 서브그래프 이벤트가 경로
   (path)와 함께 전파된다. `streaming` 패키지의 `Mode`/`Event`/`Metadata`/`Options`와 방출 헬퍼를 호출자가 쓸 수
   있다.
9. `GetState`/`GetStateHistory`가 `StateSnapshot`(`core.StateSnapshot`)을 반환하고, `UpdateState`가 수동 상태
   갱신을 적용한다. `Compile(WithCheckpointer)` 지정 시 동일 thread_id에서 상태가 별도 `Invoke` 호출 간에
   영속된다.
10. 그래프 실행이 최대 스텝 한도를 강제해, 순환 구조의 그래프가 무한히 실행되지 않고 한도에서 멈춘다.
11. import 그래프 검사로 `command`·`streaming`이 모듈 내 `graph`를 import하지 않고 `core`만 참조하며(`graph`는
    `core.Mode`를 직접 참조), `graph`가 `command`를 import함을 확인할 수 있다. Phase 1 패키지는 수정되지 않는다.
12. `DrawMermaid`가 컴파일된 그래프의 mermaid 텍스트 표현을 반환한다.
