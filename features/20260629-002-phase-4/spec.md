# phase-4 — 멀티에이전트

## 승인 전 확인

- multiagent를 재사용 가능한 라이브러리 패키지로 만드는 결정이, README §14·§26·§27이 multiagent를 "응용 계층
  (라이브러리 아님)"으로 규정한 것과 상충한다. 이 작업에서 §14의 구체 API를 라이브러리 산출물로 확정하고 README
  본문 정리는 후속으로 미루는 방향이 의도와 맞는지. 관련 본문: §1, §3
- 실제 Claude(Anthropic) e2e 검증이 `ANTHROPIC_API_KEY` 부재 시 건너뛰도록(t.Skip) 한 채택이, "실제 LLM로
  라우팅·핸드오프를 검증하되 키 없는 환경에서 빌드·정적검사는 막지 않는다"는 의도와 맞는지. 관련 본문: §3, §5

## 1. 범위

langgraph-go의 멀티에이전트 오케스트레이션을 구성하는 Phase 4다(README §26 Phase 4). 신규 `multiagent` 패키지
하나를 다루며, README §14의 전체 API를 구현한다.

- 워커 추상화 — `Worker`(Name/Description/Invoke/Stream), `WorkerRegistry`(Register/Get/Names),
  `WorkerOutput`(Messages/StructuredResponse).
- 수퍼바이저 라우팅 — `RouterTool`(TypedDict 라우터 도구 바인딩), `SelectNext`(`tool_calls`에서 next 해석),
  `Route`(선택 노드로 goto/종료), `MergeWorkerResult`(워커 산출 상태 병합).
- 핸드오프(도구 기반 위임) — `CreateHandoffTool`, 단일 위임(`command.ToParent`)·다중 위임
  (`command.Fanout([]Send)`), `HandoffBackMessages`(워커→수퍼바이저 복귀 메시지 쌍).
- 네트워크 — `BuildNetwork`(워커 간 `command.Goto` 동적 라우팅 그래프 컴파일), `IsFinalAnswer`(종료 판별).
- 플래너 — `Plan`/`Replan`(계획·재계획), `RecordStep`(소비된 단계 누적), `PlannerState`(Input/Plan/PastSteps/
  Response), `Step`(Task/Result).
- 워커 구성 어댑터 — `AgentAsNode`(에이전트→그래프 노드), `GraphAsNode`(서브그래프→그래프 노드),
  `AgentAsTool`(에이전트→도구).

이 패키지는 Phase 1 `agent`와 Phase 2 `graph`/`graph/command` primitive 위에 얹는 최상위 조립 계층이다.
README §14·§26·§27이 multiagent를 "응용 계층, 라이브러리 아님"으로 적은 것과 달리, 이 작업은 §14의 구체 API를
import 가능한 라이브러리 패키지로 산출한다(사용자 결정). 그에 따른 README 본문 정리는 이 Phase 범위 밖이다(§4).

## 2. 목표

다운스트림이 멀티에이전트 패턴(수퍼바이저 라우팅, 핸드오프 위임, 워커 네트워크, 플래너 기반 계획/재계획)을 매번
직접 조립하지 않고 `multiagent` 패키지로 재사용할 수 있게 한다. Phase 1~2의 primitive(`agent`/`graph`/
`command`)를 표준 수퍼바이저·워커 구성으로 묶어, 여러 에이전트가 협업하는 그래프를 조립할 토대를 제공한다.
챗 모델은 Phase 1의 Anthropic(Claude) 어댑터를 그대로 사용한다.

## 3. 제약

- import 경계(README §28-1, 상위→하위 단방향): `multiagent`는 `agent`·`graph`·`graph/command`·`tool`·
  `message`·`structured`·`llm`·`core`를 import하는 최상위 조립 계층이다. 하위 패키지가 `multiagent`를 역참조하지
  않는다.
- Phase 1~3 패키지의 기존 타입·동작은 변경하지 않는다. 플래너의 구조화 출력 스키마 등 새 타입이 필요하면
  `multiagent` 패키지 안에 두거나, 첫 소비처가 `multiagent`인 추가 타입으로만 더하되(Phase 3가 `llm`에 임베딩을
  더한 선례) 기존 동작은 보존한다.
- `agent`는 Phase 1의 직접 루프를 그대로 유지한다. 수퍼바이저 그래프는 `AgentAsNode`/`GraphAsNode` 어댑터로
  에이전트·서브그래프를 노드로 결합하며, agent를 graph 엔진 위로 재배치하는 리팩터링은 하지 않는다(§4).
- 챗 모델은 기존 Anthropic(Claude, 기본 `claude-opus-4-8`) 어댑터만 사용한다. 새 챗 프로바이더(Ollama 챗
  클라이언트 등)는 추가하지 않는다(§4).
- 라우팅·핸드오프·상태 병합·플래너 루프 등 LLM 비의존 제어 로직은 stub agent/stub LLM로 네트워크 없이 결정적으로
  검증할 수 있어야 한다. 실제 라우팅·핸드오프 동작은 실제 Claude(Anthropic) e2e로 검증하며, e2e는
  `ANTHROPIC_API_KEY`가 있을 때 실행하고 없으면 건너뛴다(t.Skip).

## 4. 제외 범위

- 새 챗 프로바이더(Ollama 챗 클라이언트, BindTools/ParseToolCalls의 Ollama 구현 등)는 제외한다. Phase 4는
  기존 Anthropic 어댑터로만 챗을 수행한다.
- `orchestrator`(원격 에이전트 호출·결과 통합, README §23)는 제외한다. A2A 클라이언트(Phase 7)에 의존하는 응용
  계층이다.
- `agent`를 graph 엔진 위로 재배치하는 리팩터링은 제외한다(Phase 2와 동일). agent의 직접 루프를 유지한다.
- 그래프 인터럽트(`interrupt`/`resume`) 기반 HITL은 제외한다(README §7·§22-4, Phase 2와 동일).
- README §14·§26·§27의 "응용 계층, 라이브러리 아님" 표기를 이 패키지 산출에 맞게 고치는 README 본문 정리는 제외
  한다(후속 문서 작업).
- Phase 4 외 패키지(`rag`·`store`(Phase 5)·`mcp`(Phase 6)·`a2a`·`database`·`search`·`storage`·`trace`
  (Phase 7))는 범위 밖이다.

## 5. 완료 조건

1. 모듈 루트에서 `go build ./...`와 `go vet ./...`가 오류 없이 끝난다(`multiagent` 신규 패키지가 추가되고
   Phase 1~3 패키지의 기존 동작은 변경되지 않은 상태).
2. `WorkerRegistry`에 `Worker`를 등록하고(`RegisterWorker`), 이름으로 조회(`GetWorker`)·열거(`WorkerNames`)할
   수 있다. `Worker`는 Name/Description/Invoke/Stream 계약을 가진다.
3. 워커 구성 어댑터가 동작한다: `AgentAsNode`/`GraphAsNode`가 에이전트·서브그래프를 `graph.NodeFunc`로 감싸
   그래프 노드로 실행할 수 있고, `AgentAsTool`이 에이전트를 `tool.Tool`로 노출한다.
4. 수퍼바이저 라우팅이 동작한다: `RouterTool`을 바인딩한 수퍼바이저가 `SelectNext`로 `tool_calls`에서 다음 워커를
   해석하고, `Route`가 선택 노드로 이동하며 라우터 미호출 시 종료(또는 상위 복귀)한다. `MergeWorkerResult`가 워커
   산출(`WorkerOutput`)을 상태에 병합한다.
5. 핸드오프(도구 기반 위임)가 동작한다: `CreateHandoffTool`이 `tool.Tool`을 반환하고, 단일 핸드오프는
   `command.ToParent`로 부모 그래프 대상으로 이동하며, 복수 tool_calls는 `command.Fanout([]Send)`로 분배해
   워커마다 다른 입력을 전달한다. `HandoffBackMessages`가 워커→수퍼바이저 복귀용 AI/Tool 메시지 쌍을 만든다.
6. 네트워크 구성이 동작한다: `BuildNetwork`가 워커 간 `command.Goto` 동적 라우팅 그래프를 컴파일하고,
   `IsFinalAnswer`가 종료 신호를 판별한다.
7. 플래너가 동작한다: `Plan`/`Replan`이 계획·재계획을 만들고, `RecordStep`이 소비된 `Step`(Task/Result)을
   `PlannerState.PastSteps`에 누적하며, 한 스텝씩 소비하는 루프가 빈 plan에서 최종 응답으로 끝난다.
8. 실제 Claude(Anthropic) 챗 모델로 수퍼바이저+워커 구성을 실행하면 라우팅·핸드오프가 관찰된다: 질의가 적합한
   워커로 위임되고 워커 결과가 수퍼바이저에 통합되어 최종 응답이 나온다. 이 e2e는 `ANTHROPIC_API_KEY`가 있을 때
   실행되고, 없으면 건너뛴다.
9. import 그래프 검사로 `multiagent`가 `agent`·`graph`·`graph/command`·`tool` 등 하위 패키지만 import하고
   하위 패키지가 `multiagent`를 역참조하지 않음을 확인할 수 있다. Phase 1~3 패키지는 기존 동작이 수정되지 않는다.
