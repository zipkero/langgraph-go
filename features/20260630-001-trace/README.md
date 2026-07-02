# trace

## 요약
langgraph-go 실행 추적·디버그 출력. README §25의 선택 모듈로, 그래프·에이전트 실행(노드·도구·LLM·에러)을
시간순 기록하고 JSON·사람이 읽는 텍스트·mermaid 다이어그램으로 내보낸다. tool.Event Emit 싱크로 도구 실행을
자동 기록한다. Phase 7을 패키지별로 분할한 두 번째 feature다.

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
- 2026-06-30: ANALYSIS 작성
- 2026-06-30: IMPLEMENT 체크리스트 작성
- 2026-07-02: task-001~006 구현·verify 완료(전부 approved)
