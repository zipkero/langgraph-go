# phase-1

## 요약
langgraph-go 핵심 런타임. message·llm·tool·structured·prompt → agent·middleware·prebuilt·checkpoint를 구현해,
Phase 2 그래프 엔진 없이도 단일 Claude 에이전트(도구 호출 루프 + 미들웨어 + 단기 메모리 + 구조화 출력)가
동작하게 한다. 챗은 Anthropic만, 임베딩은 Phase 3로 미룬다.

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
- 2026-06-28: task-001~011 구현·검증 완료 (IMPLEMENT)
