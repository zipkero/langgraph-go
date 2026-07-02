// database 패키지는 관계형/벡터 DB(Supabase/pgvector) 접근과 그 도구화를 담당한다.
// Client 인터페이스는 구현 방식(pgx+pgvector-go 등)과 무관하게 유지되며,
// tool·표준 라이브러리·DB 드라이버에만 의존한다(상위 vectorstore/rag 는 참조하지 않는다).
// match_documents RPC 등 스키마는 이미 존재한다고 가정해 호출만 한다(DDL·마이그레이션 미소유, ANALYSIS §5 D2).
package database

import "context"

// WebContentRecord 는 웹 콘텐츠 저장·조회 단위 레코드다.
// SearchWebContent 는 Title 컬럼에 대한 전문 검색(tsvector)으로 이 레코드를 조회한다.
type WebContentRecord struct {
	// Title 은 웹 콘텐츠 제목이다. 전문 검색 대상 컬럼이다.
	Title string
	// URL 은 원본 웹 페이지 주소다.
	URL string
	// Content 는 웹 페이지 본문이다.
	Content string
	// ExpandedQuery 는 검색 확장에 사용된 질의 문자열이다.
	ExpandedQuery string
}

// DocumentRecord 는 문서 청크 저장·조회 단위 레코드다.
type DocumentRecord struct {
	// Content 는 청크 본문 텍스트다.
	Content string
	// Embedding 은 청크 본문의 임베딩 벡터다.
	Embedding []float32
	// Filename 은 원본 파일명이다.
	Filename string
	// StorageRef 는 원본 파일을 가리키는 storage_ref 문자열이다.
	StorageRef string
	// ChunkIndex 는 원본 문서 내 청크 순번이다.
	ChunkIndex int
	// DocumentType 은 문서 종류(예: pdf/docx/txt)다.
	DocumentType string
}

// DocumentQuery 는 QueryDocuments 의 필터 조합 입력이다.
// eq/ilike/gte/lte 필터와 정렬·한도를 조합해 문서 청크를 질의한다.
type DocumentQuery struct {
	// Eq 는 컬럼명→값의 등호(=) 필터 목록이다.
	Eq map[string]any
	// ILike 는 컬럼명→패턴의 대소문자 무시 부분일치(ILIKE) 필터 목록이다.
	ILike map[string]string
	// Gte 는 컬럼명→값의 이상(>=) 필터 목록이다.
	Gte map[string]any
	// Lte 는 컬럼명→값의 이하(<=) 필터 목록이다.
	Lte map[string]any
	// OrderBy 는 정렬 기준 컬럼명이다. 비어 있으면 정렬을 지정하지 않는다.
	OrderBy string
	// OrderDesc 는 OrderBy 가 내림차순인지를 나타낸다.
	OrderDesc bool
	// Limit 은 반환할 최대 행 수다. 0 이하이면 한도를 지정하지 않는다.
	Limit int
}

// Client 는 관계형/벡터 DB 접근 계약이다.
// 구체 구현체(예: pgx+pgvector-go 기반 PGClient)와 무관하게 이 인터페이스만으로 호출자가 상호작용한다.
type Client interface {
	// Connect 는 DB 커넥션(풀)을 연다.
	Connect(ctx context.Context) error
	// Close 는 커넥션 풀을 해제한다.
	Close(ctx context.Context) error

	// InsertWebContent 는 웹 콘텐츠 레코드 하나를 저장한다.
	InsertWebContent(ctx context.Context, rec WebContentRecord) error
	// SearchWebContent 는 keyword 로 title 전문 검색(tsvector)을 수행해 일치하는 레코드를 반환한다.
	SearchWebContent(ctx context.Context, keyword string) ([]WebContentRecord, error)

	// InsertDocumentChunks 는 문서 청크 레코드 목록을 저장한다.
	InsertDocumentChunks(ctx context.Context, recs []DocumentRecord) error
	// MatchDocuments 는 embedding 과 유사도가 높은 문서 청크 상위 count 개를 match_documents RPC로 조회한다.
	// 이 RPC는 필터 인자를 받지 않는다(메타 필터는 QueryDocuments 가 담당).
	MatchDocuments(ctx context.Context, embedding []float32, count int) ([]DocumentRecord, error)
	// QueryDocuments 는 q 의 eq/ilike/gte/lte/order/limit 조합으로 문서 청크를 질의한다.
	QueryDocuments(ctx context.Context, q DocumentQuery) ([]DocumentRecord, error)
}
