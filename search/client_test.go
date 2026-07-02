// client_test.go 는 SearchClient.Search 의 HTTP 요청·응답 파싱 로직을
// net/http/httptest 기반 stub 서버로 검증하는 단위 테스트다(외부 네트워크 없음, SPEC §5.2).
package search_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/zipkero/langgraph-go/search"
)

// TestSearchClient_Search_응답을_파싱한다 는 Tavily 응답 형식을 흉내낸 stub 서버로부터
// SearchClient.Search 가 SearchResult 목록을 올바르게 파싱하는지 검증한다.
func TestSearchClient_Search_응답을_파싱한다(t *testing.T) {
	var gotBody map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search" {
			t.Errorf("예상치 못한 경로: %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("예상치 못한 메서드: %s", r.Method)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("요청 본문 디코딩 실패: %v", err)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"results": [
				{"title": "제목1", "url": "https://example.com/1", "content": "본문1", "score": 0.9},
				{"title": "제목2", "url": "https://example.com/2", "content": "본문2", "score": 0.5}
			]
		}`))
	}))
	defer ts.Close()

	c := search.NewSearchClient("test-api-key", search.WithBaseURL(ts.URL))
	results, err := c.Search(context.Background(), "테스트 질의", 2)
	if err != nil {
		t.Fatalf("Search 실패: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("결과 개수 불일치: got %d, want 2", len(results))
	}
	if results[0].Title != "제목1" || results[0].URL != "https://example.com/1" ||
		results[0].Content != "본문1" || results[0].Score != 0.9 {
		t.Errorf("첫 결과 불일치: %+v", results[0])
	}
	if results[1].Title != "제목2" {
		t.Errorf("둘째 결과 불일치: %+v", results[1])
	}

	if gotBody["query"] != "테스트 질의" {
		t.Errorf("요청 본문 query 불일치: %v", gotBody["query"])
	}
	if gotBody["api_key"] != "test-api-key" {
		t.Errorf("요청 본문 api_key 불일치: %v", gotBody["api_key"])
	}
}

// TestSearchClient_Search_빈결과 는 결과가 없을 때 빈 슬라이스를 반환하는지 검증한다.
func TestSearchClient_Search_빈결과(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results": []}`))
	}))
	defer ts.Close()

	c := search.NewSearchClient("test-api-key", search.WithBaseURL(ts.URL))
	results, err := c.Search(context.Background(), "빈 질의", 5)
	if err != nil {
		t.Fatalf("Search 실패: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("빈 결과가 아님: %+v", results)
	}
}

// TestSearchClient_Search_비정상상태코드 는 비정상 HTTP 상태코드가 error 로 반환되는지 검증한다.
func TestSearchClient_Search_비정상상태코드(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error": "invalid api key"}`))
	}))
	defer ts.Close()

	c := search.NewSearchClient("bad-key", search.WithBaseURL(ts.URL))
	_, err := c.Search(context.Background(), "질의", 5)
	if err == nil {
		t.Fatal("비정상 상태코드에서 에러가 반환되지 않음")
	}
}
