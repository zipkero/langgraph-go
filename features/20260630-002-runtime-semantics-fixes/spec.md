# runtime-semantics-fixes — SPEC

## 승인 전 확인
- 이번 감사에서 함께 발견된 저심각도 항목(스트리밍 에러 전파 누락, 빈 Content+ID 메시지의 삭제 마커 오판정,
  A2A `NewAgentTextMessage` 미사용 파라미터, `collectFinalTask` 블로킹, MCP 정적 프롬프트)을 범위에서 제외하는
  것이 맞는가. 관련 본문: §4
- Send 분기의 병렬(동시) 실행은 이번에 다루지 않고 병합 의미만 바로잡는 것이 맞는가. 관련 본문: §4

## 1. 범위
graph 실행 엔진과 agent 런타임에서 LangGraph 의미와 어긋난 동작 세 가지를 바로잡는다.

- Send/Fanout 분기 결과 병합: 분기 업데이트를 상태 키에 그대로 덮어쓰는(last-write-wins) 대신, 등록된
  리듀서로 병합한다. 분기 입력 기준 상태가 같은 단계의 다른 분기 쓰기에 오염되지 않도록 한다.
- 도달성 검사: `command.Goto`(및 노드의 목적지 선언 `WithDestinations`)만으로 도달하는 노드가 컴파일 도달성
  검사에서 거부되지 않게 한다.
- 스트리밍 미들웨어: `agent.Stream`의 모델 호출이 `agent.Invoke`와 동일한 미들웨어 체인을 거치게 한다.

위 도달성 수정의 결과로, `multiagent` network 그래프가 컴파일을 통과시키려고 넣어 둔 더미 조건엣지를 제거한다.

세 동작은 `graph`·`agent`·`multiagent` 패키지에 걸친 이미 구현된 런타임의 의미 교정이며, 새 기능 추가가
아니다.

## 2. 목표
멀티에이전트 fan-out/aggregate, `Command` 기반 동적 라우팅, 스트리밍 경로의 미들웨어가 LangGraph와 동일한
관찰 가능 동작을 갖게 한다. 이 런타임 위에 다운스트림이 조립하는 멀티에이전트·RAG 응용이 기반 의미 왜곡
없이 동작하는 것이 목적이다. 특히 여러 워커 결과를 누적 리듀서로 모으는 supervisor/handoff 패턴에서 분기
결과가 손실되지 않게 하고, `Invoke`와 `Stream`의 동작이 미들웨어 측면에서 어긋나지 않게 한다.

## 3. 제약
- 기존 공개 API의 시그니처 호환을 유지한다. 병합 의미 교정을 위해 내부 배선(컴파일된 리듀서 맵을 병합
  지점에서 접근 가능하게 하는 것 등) 변경은 허용하되, 호출자에게 노출된 함수·타입의 형태는 바꾸지 않는다.
- 기존 테스트가 계속 통과해야 한다. 단, 더미 조건엣지 제거처럼 이번 수정이 의도적으로 무효화하는 우회용
  테스트 배선은 정합성에 맞게 조정할 수 있다.
- 세 동작은 Invoke·Stream·subgraph 실행 경로 전반에서 일관되게 적용되어야 한다(한 경로만 고치고 다른 경로를
  남겨 두지 않는다).

## 4. 제외 범위
- Send 분기의 병렬(동시) 실행. 병합 의미만 교정하며 실행 순서(순차)는 유지할 수 있다.
- 이번 감사의 저심각도 항목: 스트리밍 에러 전파 누락, 빈 Content+ID 메시지의 삭제 마커 오판정, A2A
  `NewAgentTextMessage` 미사용 파라미터, `collectFinalTask` 블로킹 가능성, MCP 정적 프롬프트(인자 치환 불가).
- Phase 7 외부연동: database, search, storage, MCP streamable_http, OpenAI 챗/임베딩, vectorstore Supabase
  백엔드, trace.
- `StateSnapshot` 필드 확장(parent_config, tasks, interrupts 등) 및 그래프 interrupt/resume.

## 5. 완료 조건
1. 같은 리듀서 등록 키(예: `messages`의 `AddMessages`)를 갱신하는 둘 이상의 Send 분기가 한 fanout 단계에서
   실행될 때, 모든 분기의 업데이트가 그 키의 리듀서로 병합되어 결과 상태에 보존된다(마지막 분기 값만 남지
   않는다).
2. 한 fanout 단계의 각 분기는 그 단계 직전 상태를 동일한 입력 기준으로 받는다. 한 분기의 상태 쓰기가 같은
   단계 다른 분기의 입력에 반영되지 않는다.
3. §5.1–§5.2의 병합·격리 동작이 Invoke, Stream, subgraph 세 실행 경로에서 동일하게 관찰된다.
4. 정적 엣지·조건 엣지 없이 `command.Goto`(또는 노드의 `WithDestinations` 선언)만으로 도달하는 노드를 가진
   그래프가 Compile에서 거부되지 않고 성공하며, 실행 시 해당 노드로 이동한다.
5. `multiagent` network 그래프가 컴파일 통과용 더미 조건엣지 없이 Compile되고 실행된다.
6. `agent.Stream` 실행 시 `WrapModelCall`·`BeforeModel`·`DynamicPrompt`가 `agent.Invoke`와 동일하게 적용된다.
   동적 프롬프트 수정, before_model의 차단/단축, 모델 오버라이드가 스트리밍 경로의 관찰 가능한 출력(방출된
   메시지·상태)에 반영된다.
