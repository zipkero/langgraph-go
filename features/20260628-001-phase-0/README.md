# phase-0

## 요약
langgraph-go의 의존 트리 최하단을 구성하는 Phase 0. 모듈을 초기화하고 무의존 leaf `config`(RunConfig·식별자
추출·환경 로딩)와 `config`에만 의존하는 leaf `core`(State/StateUpdate/Mode/StateSnapshot)를 만들어, 이후 모든
Phase가 참조하는 토대를 확정한다.

## 상태
- [x] SPEC
- [x] ANALYSIS
- [x] IMPLEMENT

## 문서
- [spec.md](./spec.md)
- [analysis.md](./analysis.md) (ANALYSIS 단계에서 생성)
- [implement.md](./implement.md) (IMPLEMENT 단계에서 생성)

## 작업 히스토리
- 2026-06-28: SPEC 작성
- 2026-06-28: ANALYSIS 작성
- 2026-06-28: IMPLEMENT 체크리스트 작성
- 2026-06-28: task-001·task-002 구현·검증 완료 (IMPLEMENT)
