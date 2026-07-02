-- schema.sql — Supabase(Postgres+pgvector) 스키마 DDL.
-- database 패키지는 이 스키마가 이미 존재한다고 가정해 호출만 한다(DDL·마이그레이션 미소유, client.go).
-- 이 파일은 코드가 실행하지 않는 참고 자산이며, Supabase SQL Editor에서 1회 수동 실행해 적용한다.
--
-- hanbit-aiagent 교재(CHAP11) index.sql 원본과 동일하게 OpenAI text-embedding-3-small 기준
-- VECTOR(1536)이다. 다른 임베딩 모델을 쓰면 아래 vector(1536) 세 곳을 그 모델 차원으로 맞춘다.
-- 기존에 vector(768) 스키마를 적용했다면 documents 테이블과 match_documents 함수를 drop 한 뒤
-- 이 파일을 재실행한다(차원 변경 시 기존 임베딩 데이터는 재적재가 필요하다).
--
-- 컬럼 계약은 database/query.go 와 1:1 이다:
--   documents        → documentColumns  (content, embedding, filename, storage_ref, chunk_index, document_type)
--   web_content      → webContentColumns(title, url, content, expanded_query)
--   match_documents  → (query_embedding, match_count) 시그니처, documentColumns 반환

-- pgvector 확장 활성화 (Supabase는 extensions 스키마에 설치돼 있다)
create extension if not exists vector;

-- ── web_content: 웹 콘텐츠 저장 + title 전문 검색 ────────────────────────────
create table if not exists web_content (
    id             bigserial primary key,
    title          text not null,
    url            text not null default '',
    content        text not null default '',
    expanded_query text not null default '',
    created_at     timestamptz not null default now()
);

-- SearchWebContent 의 to_tsvector('simple', title) 검색을 인덱스로 지원한다.
-- 'simple' 구성은 한국어 형태소 분석 없이 공백 토큰화만 하므로 질의 측(plainto_tsquery('simple', ...))과 짝을 이룬다.
create index if not exists web_content_title_tsv_idx
    on web_content using gin (to_tsvector('simple', title));

-- ── documents: 문서 청크 + 임베딩 ────────────────────────────────────────────
create table if not exists documents (
    id            bigserial primary key,
    content       text not null,
    embedding     vector(1536),
    filename      text not null default '',
    storage_ref   text not null default '',
    chunk_index   int  not null default 0,
    document_type text not null default '',
    created_at    timestamptz not null default now()
);

-- 코사인 거리(<=>) 기반 근사 검색 인덱스. lists 는 행 수가 커지면(수만 이상) 늘린다.
create index if not exists documents_embedding_idx
    on documents using ivfflat (embedding vector_cosine_ops) with (lists = 100);

-- ── match_documents: 코사인 유사도 상위 match_count 개 조회 RPC ──────────────
-- database.MatchDocuments 가 "SELECT ... FROM match_documents($1, $2)" 로 호출한다.
-- 반환 컬럼은 documentColumns 와 이름·순서·타입이 일치해야 한다.
create or replace function match_documents(
    query_embedding vector(1536),
    match_count     int
)
returns table (
    content       text,
    embedding     vector(1536),
    filename      text,
    storage_ref   text,
    chunk_index   int,
    document_type text
)
language sql stable
as $$
    select
        d.content,
        d.embedding,
        d.filename,
        d.storage_ref,
        d.chunk_index,
        d.document_type
    from documents d
    where d.embedding is not null
    order by d.embedding <=> query_embedding
    limit match_count;
$$;
