// integration_test.go 는 SearchClient 를 실제 Tavily API 에 연결해 검증하는 통합 테스트다.
// TAVILY_API_KEY 환경변수가 없으면 t.Skip 으로 건너뛴다(크리덴셜 부재 시 skip, database/integration_test.go 패턴 참조).
package search_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/zipkero/langgraph-go/search"
)

// tavilyAPIKey 는 환경변수에서 TAVILY_API_KEY 를 읽어 반환한다.
func tavilyAPIKey() string {
	return os.Getenv("TAVILY_API_KEY")
}

// skipIfNoTavilyKey 는 TAVILY_API_KEY 가 없으면 테스트를 skip 한다.
func skipIfNoTavilyKey(t *testing.T) string {
	t.Helper()
	key := tavilyAPIKey()
	if key == "" {
		t.Skip("TAVILY_API_KEY 가 없으므로 실제 Tavily API 통합 테스트를 건너뜁니다")
	}
	return key
}

// TestSearchClient_Search_실제Tavily 는 실제 Tavily API 로 검색해 결과를 받는지 검증한다.
func TestSearchClient_Search_실제Tavily(t *testing.T) {
	key := skipIfNoTavilyKey(t)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	c := search.NewSearchClient(key)
	results, err := c.Search(ctx, "langgraph-go golang agent framework", 3)
	if err != nil {
		t.Fatalf("Search 실패: %v", err)
	}
	if len(results) == 0 {
		t.Error("검색 결과가 비어 있음")
	}
	for _, r := range results {
		if r.URL == "" {
			t.Errorf("URL 이 비어 있는 결과: %+v", r)
		}
	}
}
