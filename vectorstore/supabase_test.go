// supabase_test.go 는 SupabaseVectorStore 가 fake database.Client 에 위임·변환하는지,
// AsRetriever 경로가 질의 임베딩 후 위임하는지를 검증한다. 네트워크·실 DB 없이 항상 실행된다(task-006).
package vectorstore_test

import (
	"context"
	"errors"
	"testing"

	"github.com/zipkero/langgraph-go/database"
	"github.com/zipkero/langgraph-go/vectorstore"
)

// fakeDatabaseClient 는 database.Client 를 구현하는 테스트용 fake다.
// MatchDocuments 호출 인자를 기록하고 미리 설정된 레코드·에러를 반환한다.
type fakeDatabaseClient struct {
	// wantRecords 는 MatchDocuments 가 반환할 레코드다.
	wantRecords []database.DocumentRecord
	// wantErr 는 MatchDocuments 가 반환할 에러다.
	wantErr error

	// gotEmbedding 은 MatchDocuments 에 전달된 embedding 인자를 기록한다.
	gotEmbedding []float32
	// gotCount 는 MatchDocuments 에 전달된 count 인자를 기록한다.
	gotCount int
}

func (f *fakeDatabaseClient) Connect(_ context.Context) error { return nil }
func (f *fakeDatabaseClient) Close(_ context.Context) error   { return nil }

func (f *fakeDatabaseClient) InsertWebContent(_ context.Context, _ database.WebContentRecord) error {
	return nil
}

func (f *fakeDatabaseClient) SearchWebContent(_ context.Context, _ string) ([]database.WebContentRecord, error) {
	return nil, nil
}

func (f *fakeDatabaseClient) InsertDocumentChunks(_ context.Context, _ []database.DocumentRecord) error {
	return nil
}

func (f *fakeDatabaseClient) MatchDocuments(_ context.Context, embedding []float32, count int) ([]database.DocumentRecord, error) {
	f.gotEmbedding = embedding
	f.gotCount = count
	if f.wantErr != nil {
		return nil, f.wantErr
	}
	return f.wantRecords, nil
}

func (f *fakeDatabaseClient) QueryDocuments(_ context.Context, _ database.DocumentQuery) ([]database.DocumentRecord, error) {
	return nil, nil
}

var _ database.Client = (*fakeDatabaseClient)(nil)

// TestSupabaseVectorStore_MatchDocuments_위임및변환 은 MatchDocuments 가 fake client 에 그대로
// 위임하고, 반환된 DocumentRecord 를 document.Document 로 변환하는지 검증한다.
func TestSupabaseVectorStore_MatchDocuments_위임및변환(t *testing.T) {
	fake := &fakeDatabaseClient{
		wantRecords: []database.DocumentRecord{
			{Content: "청크1", Filename: "a.txt", ChunkIndex: 0, DocumentType: "txt"},
			{Content: "청크2", Filename: "b.txt", ChunkIndex: 1, DocumentType: "txt"},
		},
	}
	store := vectorstore.NewSupabaseVectorStore(fake, makeStub(nil))

	queryEmbedding := []float32{1, 2, 3}
	docs, err := store.MatchDocuments(context.Background(), queryEmbedding, 5)
	if err != nil {
		t.Fatalf("MatchDocuments 실패: %v", err)
	}

	if len(fake.gotEmbedding) != 3 || fake.gotEmbedding[0] != 1 {
		t.Errorf("database.Client.MatchDocuments 에 전달된 embedding이 예상과 다름: %v", fake.gotEmbedding)
	}
	if fake.gotCount != 5 {
		t.Errorf("database.Client.MatchDocuments 에 전달된 count = %d, want 5", fake.gotCount)
	}

	if len(docs) != 2 {
		t.Fatalf("변환된 Document 개수 = %d, want 2", len(docs))
	}
	if docs[0].PageContent != "청크1" {
		t.Errorf("docs[0].PageContent = %q, want 청크1", docs[0].PageContent)
	}
	if docs[0].Metadata["filename"] != "a.txt" {
		t.Errorf("docs[0].Metadata[filename] = %v, want a.txt", docs[0].Metadata["filename"])
	}
}

// TestSupabaseVectorStore_MatchDocuments_에러전파 는 database.Client 가 에러를 반환하면
// SupabaseVectorStore.MatchDocuments 도 에러를 반환하는지 검증한다.
func TestSupabaseVectorStore_MatchDocuments_에러전파(t *testing.T) {
	fake := &fakeDatabaseClient{wantErr: errors.New("db 연결 실패")}
	store := vectorstore.NewSupabaseVectorStore(fake, makeStub(nil))

	_, err := store.MatchDocuments(context.Background(), []float32{1}, 1)
	if err == nil {
		t.Fatal("에러를 기대했지만 nil이 반환됨")
	}
}

// TestSupabaseVectorStore_AsRetriever_질의임베딩후위임 은 AsRetriever 가 반환한 Retriever.Invoke 가
// 질의를 emb 로 임베딩한 뒤 그 벡터로 database.Client.MatchDocuments 를 호출하는지 검증한다.
func TestSupabaseVectorStore_AsRetriever_질의임베딩후위임(t *testing.T) {
	fake := &fakeDatabaseClient{
		wantRecords: []database.DocumentRecord{{Content: "결과"}},
	}
	stub := makeStub(map[string][]float32{"query": {0.5, 0.5}})
	store := vectorstore.NewSupabaseVectorStore(fake, stub)

	retriever := store.AsRetriever(vectorstore.SearchOptions{K: 3})
	docs, err := retriever.Invoke(context.Background(), "query")
	if err != nil {
		t.Fatalf("Invoke 실패: %v", err)
	}

	if len(fake.gotEmbedding) != 2 || fake.gotEmbedding[0] != 0.5 {
		t.Errorf("MatchDocuments 에 전달된 embedding이 질의 임베딩과 다름: %v", fake.gotEmbedding)
	}
	if fake.gotCount != 3 {
		t.Errorf("MatchDocuments 에 전달된 count = %d, want 3(opts.K)", fake.gotCount)
	}
	if len(docs) != 1 || docs[0].PageContent != "결과" {
		t.Errorf("Invoke 결과가 예상과 다름: %v", docs)
	}
}
