// supabase.go 는 database.Client 에 위임하는 외부(pgvector) 벡터 백엔드 SupabaseVectorStore 를 담는다.
// pgvector 접근 로직의 유일 소유자는 database.Client 이며, SupabaseVectorStore 는 질의 임베딩과
// database.DocumentRecord → document.Document 변환만 맡는 얇은 어댑터다(ANALYSIS §1.5, §2.2, §5 D4).
package vectorstore

import (
	"context"
	"fmt"

	"github.com/zipkero/langgraph-go/database"
	"github.com/zipkero/langgraph-go/document"
	"github.com/zipkero/langgraph-go/llm"
)

// SupabaseVectorStore 는 database.Client 에 위임해 유사도 검색을 수행하는 외부 벡터 백엔드다.
// 최소 Retriever 계약을 충족해 InMemoryStore 와 동일한 AsRetriever 경로로 쓰인다(ANALYSIS §5 D4).
type SupabaseVectorStore struct {
	// client 는 pgvector 접근을 위임할 database 클라이언트다.
	client database.Client
	// emb 는 질의 임베딩 생성에 사용할 클라이언트다.
	emb llm.EmbeddingClient
}

// NewSupabaseVectorStore 는 client 에 위임하는 SupabaseVectorStore 를 생성한다.
// opts 는 InMemoryStore 의 FromDocuments 와 동일한 StoreOption 을 받으며, 현재는 확장 자리만 있다.
func NewSupabaseVectorStore(client database.Client, emb llm.EmbeddingClient, opts ...StoreOption) *SupabaseVectorStore {
	o := &storeOptions{}
	for _, opt := range opts {
		opt(o)
	}

	return &SupabaseVectorStore{
		client: client,
		emb:    emb,
	}
}

// MatchDocuments 는 queryEmbedding 과 유사도가 높은 문서 청크 상위 count 개를
// database.Client.MatchDocuments 로 조회한 뒤 document.Document 로 변환해 반환한다.
// pgvector 접근 로직은 재구현하지 않고 database.Client 호출 결과 변환만 한다(ANALYSIS §1.1, §1.5).
func (s *SupabaseVectorStore) MatchDocuments(ctx context.Context, queryEmbedding []float32, count int) ([]document.Document, error) {
	records, err := s.client.MatchDocuments(ctx, queryEmbedding, count)
	if err != nil {
		return nil, fmt.Errorf("vectorstore: database.MatchDocuments 실패: %w", err)
	}

	docs := make([]document.Document, len(records))
	for i, rec := range records {
		docs[i] = documentFromRecord(rec)
	}
	return docs, nil
}

// AsRetriever 는 opts 를 고정한 Retriever 를 반환한다.
// 반환된 Retriever.Invoke 는 질의를 emb 로 임베딩한 뒤 MatchDocuments 로 위임한다(ANALYSIS §2.2).
func (s *SupabaseVectorStore) AsRetriever(opts SearchOptions) Retriever {
	return &supabaseRetriever{
		store: s,
		opts:  opts,
	}
}

// supabaseRetriever 는 SupabaseVectorStore 를 고정된 SearchOptions 로 감싼 Retriever 구현체다.
type supabaseRetriever struct {
	// store 는 검색을 위임할 SupabaseVectorStore 다.
	store *SupabaseVectorStore
	// opts 는 고정된 검색 옵션이다. K 가 MatchDocuments 의 count 로 전달된다.
	opts SearchOptions
}

// Invoke 는 query 를 store.emb.EmbedQuery 로 임베딩한 뒤 store.MatchDocuments 로 위임해
// 결과 Document 목록을 반환한다(SPEC §5.5).
func (r *supabaseRetriever) Invoke(ctx context.Context, query string) ([]document.Document, error) {
	vec, err := r.store.emb.EmbedQuery(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("vectorstore: EmbedQuery 실패: %w", err)
	}

	return r.store.MatchDocuments(ctx, vec, r.opts.K)
}

// documentFromRecord 는 database.DocumentRecord 를 document.Document 로 변환한다.
// Content 는 PageContent 로, 나머지 필드는 Metadata 로 옮긴다.
func documentFromRecord(rec database.DocumentRecord) document.Document {
	return document.Document{
		PageContent: rec.Content,
		Metadata: map[string]any{
			"filename":      rec.Filename,
			"storage_ref":   rec.StorageRef,
			"chunk_index":   rec.ChunkIndex,
			"document_type": rec.DocumentType,
		},
	}
}
