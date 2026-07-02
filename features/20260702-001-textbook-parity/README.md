# textbook-parity

## 요약
hanbit-aiagent 교재를 치환 규칙 없이 따라갈 수 있도록 OpenAI 챗·임베딩과 Chroma 벡터스토어를
지원하고, Supabase 스키마를 1536차원으로 전환하며 문서 기준을 OpenAI로 맞춘다.

## 상태
- [x] SPEC
- [x] ANALYSIS
- [x] IMPLEMENT

## 문서
- [spec.md](./spec.md)
- [analysis.md](./analysis.md) (ANALYSIS 단계에서 생성)
- [implement.md](./implement.md) (IMPLEMENT 단계에서 생성)

## 작업 히스토리
- 2026-07-02: SPEC 작성
- 2026-07-02: 승인 전 확인 3건(768 데이터 재적재·OpenAI 기본 전환·API 키 사용) 모두 확정, SPEC 승인
- 2026-07-02: ANALYSIS 작성
- 2026-07-02: IMPLEMENT 체크리스트 작성
- 2026-07-02: task-001~005 구현·verify 전부 approved, IMPLEMENT 완료
- 2026-07-02: 실환경 검증 완료 — OpenAI 라이브 6건(챗 기본 모델 gpt-5.4-nano-2026-03-17로 변경),
  Supabase 1536 적재→조회(신규 프로젝트+DATABASE_URL), Chroma e2e 3건(docker compose 기동) 전부 통과
