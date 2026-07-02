// integration_test.go 는 PGClient 를 실제 Postgres/pgvector 인스턴스에 연결해 검증하는 통합 테스트다.
// DATABASE_URL 환경변수(연결 문자열, 예: postgres://user:pass@host:5432/db)가 없거나 연결에 실패하면
// t.Skip 으로 건너뛴다(크리덴셜/서버 부재 시 skip, ANALYSIS §근거).
package database_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/zipkero/langgraph-go/database"
	"github.com/zipkero/langgraph-go/llm"
)

// databaseURL 은 환경변수에서 DATABASE_URL 을 읽어 반환한다.
func databaseURL() string {
	return os.Getenv("DATABASE_URL")
}

// skipIfNoDatabase 는 DATABASE_URL 이 없으면 테스트를 skip 한다.
func skipIfNoDatabase(t *testing.T) string {
	t.Helper()
	url := databaseURL()
	if url == "" {
		t.Skip("DATABASE_URL 이 없으므로 실제 DB 통합 테스트를 건너뜁니다")
	}
	return url
}

// skipIfNoOpenAIKey 는 OPENAI_API_KEY 가 없으면 테스트를 skip 한다(ANALYSIS §5 D-f).
func skipIfNoOpenAIKey(t *testing.T) {
	t.Helper()
	if os.Getenv("OPENAI_API_KEY") == "" {
		t.Skip("OPENAI_API_KEY 가 없으므로 실제 OpenAI 임베딩 통합 테스트를 건너뜁니다")
	}
}

// TestPGClient_Connect_실제DB 는 실제 Postgres/pgvector 인스턴스에 연결·해제할 수 있는지 검증한다.
// 스키마(web_content/documents/match_documents RPC)가 이미 준비된 환경에서만 유효하다(ANALYSIS §5 D2).
func TestPGClient_Connect_실제DB(t *testing.T) {
	url := skipIfNoDatabase(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	c := database.NewPGClient(url)
	if err := c.Connect(ctx); err != nil {
		t.Fatalf("Connect 실패: %v", err)
	}
	defer c.Close(ctx)
}

// TestPGClient_웹콘텐츠_삽입및전문검색_실제DB 는 웹 콘텐츠를 삽입한 뒤 title 전문 검색으로 조회되는지 검증한다.
func TestPGClient_웹콘텐츠_삽입및전문검색_실제DB(t *testing.T) {
	url := skipIfNoDatabase(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	c := database.NewPGClient(url)
	if err := c.Connect(ctx); err != nil {
		t.Fatalf("Connect 실패: %v", err)
	}
	defer c.Close(ctx)

	rec := database.WebContentRecord{
		Title:   "langgraph-go 통합 테스트 고유 제목 QWERTY123",
		URL:     "https://example.com/integration-test",
		Content: "통합 테스트 본문",
	}
	if err := c.InsertWebContent(ctx, rec); err != nil {
		t.Fatalf("InsertWebContent 실패: %v", err)
	}

	results, err := c.SearchWebContent(ctx, "QWERTY123")
	if err != nil {
		t.Fatalf("SearchWebContent 실패: %v", err)
	}
	found := false
	for _, r := range results {
		if r.URL == rec.URL {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("삽입한 레코드가 전문 검색 결과에 없음: %+v", results)
	}
}

// TestPGClient_문서청크_삽입및유사도매칭_실제DB 는 문서 청크를 삽입한 뒤 MatchDocuments 로 조회되는지 검증한다.
func TestPGClient_문서청크_삽입및유사도매칭_실제DB(t *testing.T) {
	url := skipIfNoDatabase(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	c := database.NewPGClient(url)
	if err := c.Connect(ctx); err != nil {
		t.Fatalf("Connect 실패: %v", err)
	}
	defer c.Close(ctx)

	embedding := make([]float32, 1536)
	for i := range embedding {
		embedding[i] = 0.001
	}
	rec := database.DocumentRecord{
		Content:      "통합 테스트 문서 청크",
		Embedding:    embedding,
		Filename:     "integration-test.txt",
		ChunkIndex:   0,
		DocumentType: "txt",
	}
	if err := c.InsertDocumentChunks(ctx, []database.DocumentRecord{rec}); err != nil {
		t.Fatalf("InsertDocumentChunks 실패: %v", err)
	}

	matched, err := c.MatchDocuments(ctx, embedding, 5)
	if err != nil {
		t.Fatalf("MatchDocuments 실패: %v", err)
	}
	if len(matched) == 0 {
		t.Errorf("유사도 매칭 결과가 비어 있음")
	}
}

// TestPGClient_QueryDocuments_실제DB 는 eq 필터로 QueryDocuments 가 조회되는지 검증한다.
func TestPGClient_QueryDocuments_실제DB(t *testing.T) {
	url := skipIfNoDatabase(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	c := database.NewPGClient(url)
	if err := c.Connect(ctx); err != nil {
		t.Fatalf("Connect 실패: %v", err)
	}
	defer c.Close(ctx)

	_, err := c.QueryDocuments(ctx, database.DocumentQuery{
		Eq:    map[string]any{"document_type": "txt"},
		Limit: 1,
	})
	if err != nil {
		t.Fatalf("QueryDocuments 실패: %v", err)
	}
}

// TestPGClient_OpenAI임베딩_문서청크_삽입및유사도매칭_실제DB 는 OpenAI 임베딩(1536차원)으로 적재한 문서
// 청크가 Supabase의 1536차원 벡터 경로(match_documents)에서 정상 조회되는지 검증한다(SPEC §5.4, ANALYSIS §2.4).
// DATABASE_URL·OPENAI_API_KEY 가 모두 있어야 실행되며, schema.sql 이 vector(1536)으로 적용된 DB를 가정한다.
func TestPGClient_OpenAI임베딩_문서청크_삽입및유사도매칭_실제DB(t *testing.T) {
	url := skipIfNoDatabase(t)
	skipIfNoOpenAIKey(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	embedder, err := llm.InitEmbeddings("openai:text-embedding-3-small")
	if err != nil {
		t.Fatalf("InitEmbeddings 실패: %v", err)
	}

	content := "langgraph-go OpenAI 임베딩 통합 테스트 문서 청크 QWERTY456"
	vectors, err := embedder.Embed(ctx, []string{content})
	if err != nil {
		t.Fatalf("Embed 실패: %v", err)
	}
	if len(vectors) != 1 || len(vectors[0]) != 1536 {
		t.Fatalf("임베딩 결과가 예상과 다름: len(vectors)=%d, dim=%d", len(vectors), len(vectors[0]))
	}

	c := database.NewPGClient(url)
	if err := c.Connect(ctx); err != nil {
		t.Fatalf("Connect 실패: %v", err)
	}
	defer c.Close(ctx)

	rec := database.DocumentRecord{
		Content:      content,
		Embedding:    vectors[0],
		Filename:     "openai-integration-test.txt",
		ChunkIndex:   0,
		DocumentType: "txt",
	}
	if err := c.InsertDocumentChunks(ctx, []database.DocumentRecord{rec}); err != nil {
		t.Fatalf("InsertDocumentChunks 실패: %v", err)
	}

	matched, err := c.MatchDocuments(ctx, vectors[0], 5)
	if err != nil {
		t.Fatalf("MatchDocuments 실패: %v", err)
	}
	found := false
	for _, m := range matched {
		if m.Content == content {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("OpenAI 임베딩으로 적재한 문서가 유사도 매칭 결과에 없음: %+v", matched)
	}
}
