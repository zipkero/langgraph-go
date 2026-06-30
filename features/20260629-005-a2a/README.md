# a2a

## 요약
langgraph-go A2A(Agent-to-Agent) 연동. A2A의 JSON-RPC 2.0 over HTTP(+SSE) 프로토콜을 net/http로 직접 구현해
README §22 표면(프로토콜 타입·서버·클라이언트·LangGraph 에이전트 어댑터)을 제공한다. Phase 7을 패키지별로 분할한
첫 feature다.

## 상태
- [x] SPEC
- [x] ANALYSIS
- [x] IMPLEMENT

## 문서
- [spec.md](./spec.md)
- [analysis.md](./analysis.md) (ANALYSIS 단계에서 생성)
- [implement.md](./implement.md) (IMPLEMENT 단계에서 생성)

## 작업 히스토리
- 2026-06-29: SPEC 작성
- 2026-06-29: ANALYSIS 작성
- 2026-06-29: IMPLEMENT 체크리스트 작성
- 2026-06-30: task-001~006 구현·verify 완료(전부 approved)
