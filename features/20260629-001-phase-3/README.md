# phase-3

## 요약
langgraph-go 문서·벡터스토어. document(적재·분할) + vectorstore(인메모리 유사도 검색·retriever 도구화) +
llm 임베딩 팩토리(Ollama 로컬)를 구현해, 다운스트림이 적재→분할→임베딩→저장→검색 RAG primitive를 쓸 수 있게
한다. Chroma/Supabase 백엔드와 RAG 그래프는 범위 밖이다.

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
- 2026-06-29: task-001~008 구현·verify 완료 (IMPLEMENT approved)
