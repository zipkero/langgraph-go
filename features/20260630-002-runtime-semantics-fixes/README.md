# runtime-semantics-fixes

## 요약
graph·agent·multiagent 런타임에서 LangGraph 의미와 어긋난 동작 세 가지(Send/Fanout 리듀서 병합, Command.Goto/
WithDestinations 도달성, agent.Stream 미들웨어 적용)를 바로잡고, network 그래프의 더미 조건엣지를 제거한다.

## 상태
- [x] SPEC
- [x] ANALYSIS
- [x] IMPLEMENT

## 문서
- [spec.md](./spec.md)
- [analysis.md](./analysis.md) (ANALYSIS 단계에서 생성)
- [implement.md](./implement.md) (IMPLEMENT 단계에서 생성)

## 작업 히스토리
- 2026-06-30: SPEC 작성
- 2026-06-30: ANALYSIS 작성 (Send/Fanout 리듀서 병합·도달성 destinations·agent.Stream 미들웨어 3개 의미 교정 설계, WrapModelCall 스트리밍은 옵션 B 채택)
- 2026-06-30: IMPLEMENT 체크리스트 작성
- 2026-07-01: task-004~007 구현·verify 완료 (fanout 일관성 e2e, 도달성 destinations, network 더미 엣지 제거, agent.Stream 미들웨어 경유)
