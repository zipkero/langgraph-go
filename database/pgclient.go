// pgclient.go 는 Client 인터페이스의 pgx+pgvector-go 기반 구현체(PGClient)를 담는다.
// Supabase/pgvector 를 pgx 커넥션 풀로 직접 연결하며, match_documents RPC 등 스키마는
// 이미 존재한다고 가정해 호출만 한다(DDL·마이그레이션 미소유, ANALYSIS §5 D2).
package database

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgvector/pgvector-go"
	pgxvec "github.com/pgvector/pgvector-go/pgx"
)

// PGClient 는 pgx 커넥션 풀로 Client 계약을 구현하는 Postgres/pgvector 백엔드다.
type PGClient struct {
	// connString 은 Postgres 연결 문자열(DSN)이다.
	connString string
	// pool 은 Connect 이후 사용하는 커넥션 풀이다. Connect 전에는 nil이다.
	pool *pgxpool.Pool
}

// NewPGClient 는 connString 으로 연결할 PGClient 를 생성한다.
// 실제 연결은 Connect 호출 시 이루어진다.
func NewPGClient(connString string) *PGClient {
	return &PGClient{connString: connString}
}

// Connect 는 커넥션 풀을 생성하고, 각 연결에 pgvector 타입을 등록한다.
func (c *PGClient) Connect(ctx context.Context) error {
	cfg, err := pgxpool.ParseConfig(c.connString)
	if err != nil {
		return fmt.Errorf("database: 연결 문자열 파싱 실패: %w", err)
	}
	cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		return pgxvec.RegisterTypes(ctx, conn)
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return fmt.Errorf("database: 커넥션 풀 생성 실패: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return fmt.Errorf("database: DB 연결 확인 실패: %w", err)
	}

	c.pool = pool
	return nil
}

// Close 는 커넥션 풀을 해제한다. Connect 되지 않은 상태에서 호출해도 안전하다.
func (c *PGClient) Close(ctx context.Context) error {
	if c.pool != nil {
		c.pool.Close()
		c.pool = nil
	}
	return nil
}

// InsertWebContent 는 웹 콘텐츠 레코드 하나를 web_content 테이블에 저장한다.
func (c *PGClient) InsertWebContent(ctx context.Context, rec WebContentRecord) error {
	if c.pool == nil {
		return errNotConnected
	}
	_, err := c.pool.Exec(ctx, insertWebContentSQL(), rec.Title, rec.URL, rec.Content, rec.ExpandedQuery)
	if err != nil {
		return fmt.Errorf("database: InsertWebContent 실패: %w", err)
	}
	return nil
}

// SearchWebContent 는 keyword 로 title tsvector 전문 검색을 수행한다.
func (c *PGClient) SearchWebContent(ctx context.Context, keyword string) ([]WebContentRecord, error) {
	if c.pool == nil {
		return nil, errNotConnected
	}
	rows, err := c.pool.Query(ctx, searchWebContentSQL(), keyword)
	if err != nil {
		return nil, fmt.Errorf("database: SearchWebContent 실패: %w", err)
	}
	defer rows.Close()

	var recs []WebContentRecord
	for rows.Next() {
		var rec WebContentRecord
		if err := rows.Scan(&rec.Title, &rec.URL, &rec.Content, &rec.ExpandedQuery); err != nil {
			return nil, fmt.Errorf("database: SearchWebContent 행 스캔 실패: %w", err)
		}
		recs = append(recs, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("database: SearchWebContent 행 순회 실패: %w", err)
	}
	return recs, nil
}

// InsertDocumentChunks 는 문서 청크 레코드 목록을 documents 테이블에 배치 삽입한다.
func (c *PGClient) InsertDocumentChunks(ctx context.Context, recs []DocumentRecord) error {
	if c.pool == nil {
		return errNotConnected
	}
	if len(recs) == 0 {
		return nil
	}

	batch := &pgx.Batch{}
	sql := insertDocumentChunkSQL()
	for _, rec := range recs {
		batch.Queue(sql,
			rec.Content,
			pgvector.NewVector(rec.Embedding),
			rec.Filename,
			rec.StorageRef,
			rec.ChunkIndex,
			rec.DocumentType,
		)
	}

	br := c.pool.SendBatch(ctx, batch)
	defer br.Close()
	for range recs {
		if _, err := br.Exec(); err != nil {
			return fmt.Errorf("database: InsertDocumentChunks 배치 실행 실패: %w", err)
		}
	}
	return nil
}

// MatchDocuments 는 match_documents RPC로 embedding 과 유사도가 높은 문서 청크 상위 count 개를 조회한다.
// 이 RPC는 필터 인자를 받지 않는다(메타 필터는 QueryDocuments 가 담당).
func (c *PGClient) MatchDocuments(ctx context.Context, embedding []float32, count int) ([]DocumentRecord, error) {
	if c.pool == nil {
		return nil, errNotConnected
	}
	rows, err := c.pool.Query(ctx, matchDocumentsSQL(), pgvector.NewVector(embedding), count)
	if err != nil {
		return nil, fmt.Errorf("database: MatchDocuments 실패: %w", err)
	}
	defer rows.Close()
	return scanDocumentRows(rows)
}

// QueryDocuments 는 q 의 eq/ilike/gte/lte/order/limit 조합으로 documents 테이블을 질의한다.
func (c *PGClient) QueryDocuments(ctx context.Context, q DocumentQuery) ([]DocumentRecord, error) {
	if c.pool == nil {
		return nil, errNotConnected
	}
	sql, args := buildDocumentQuerySQL(q)
	rows, err := c.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("database: QueryDocuments 실패: %w", err)
	}
	defer rows.Close()
	return scanDocumentRows(rows)
}

// scanDocumentRows 는 documentColumns 순서(content, embedding, filename, storage_ref, chunk_index,
// document_type)로 행을 스캔해 DocumentRecord 목록으로 변환한다.
func scanDocumentRows(rows pgx.Rows) ([]DocumentRecord, error) {
	var recs []DocumentRecord
	for rows.Next() {
		var rec DocumentRecord
		var vec pgvector.Vector
		if err := rows.Scan(&rec.Content, &vec, &rec.Filename, &rec.StorageRef, &rec.ChunkIndex, &rec.DocumentType); err != nil {
			return nil, fmt.Errorf("database: 문서 행 스캔 실패: %w", err)
		}
		rec.Embedding = vec.Slice()
		recs = append(recs, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("database: 문서 행 순회 실패: %w", err)
	}
	return recs, nil
}

// errNotConnected 는 Connect 를 호출하지 않은 상태에서 메서드가 호출됐을 때 반환하는 에러다.
var errNotConnected = fmt.Errorf("database: Connect 가 호출되지 않았습니다")
