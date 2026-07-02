// search 패키지는 웹 검색 클라이언트와 그 도구화를 담당한다.
// SearchClient 는 Tavily REST API 를 표준 net/http 로 직접 호출한다(공식 SDK 미사용, SPEC §3·ANALYSIS §근거).
// tool·(선택)document·표준 라이브러리·외부 HTTP 에만 의존하며, 상위 패키지(vectorstore/rag/orchestrator 등)를
// 참조하지 않는다.
package search

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// defaultTavilyBaseURL 은 Tavily 검색 API 의 기본 베이스 URL 이다.
const defaultTavilyBaseURL = "https://api.tavily.com"

// SearchResult 는 검색 결과 하나를 나타낸다.
type SearchResult struct {
	// Title 은 검색 결과 페이지 제목이다.
	Title string
	// URL 은 검색 결과 페이지 주소다.
	URL string
	// Content 는 검색 결과 요약 본문이다.
	Content string
	// Score 는 검색 결과의 관련도 점수다.
	Score float64
}

// Option 은 SearchClient 생성 옵션이다.
type Option func(*SearchClient)

// WithBaseURL 은 Tavily API 베이스 URL 을 재정의한다(테스트에서 httptest 서버를 가리키는 용도).
func WithBaseURL(baseURL string) Option {
	return func(c *SearchClient) {
		c.baseURL = baseURL
	}
}

// SearchClient 는 Tavily REST 검색 API 클라이언트다.
type SearchClient struct {
	// apiKey 는 Tavily API 키다.
	apiKey string
	// baseURL 은 Tavily API 베이스 URL 이다.
	baseURL string
	// httpClient 는 HTTP 호출에 사용할 클라이언트다.
	httpClient *http.Client
}

// NewSearchClient 는 apiKey 로 SearchClient 를 생성한다.
func NewSearchClient(apiKey string, opts ...Option) *SearchClient {
	c := &SearchClient{
		apiKey:     apiKey,
		baseURL:    defaultTavilyBaseURL,
		httpClient: &http.Client{},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// tavilySearchRequest 는 Tavily /search 엔드포인트 요청 본문이다.
type tavilySearchRequest struct {
	// APIKey 는 Tavily API 키다.
	APIKey string `json:"api_key"`
	// Query 는 검색어다.
	Query string `json:"query"`
	// MaxResults 는 반환받을 최대 결과 수다.
	MaxResults int `json:"max_results,omitempty"`
}

// tavilySearchResponse 는 Tavily /search 엔드포인트 응답 본문이다.
type tavilySearchResponse struct {
	// Results 는 검색 결과 목록이다.
	Results []tavilyResult `json:"results"`
}

// tavilyResult 는 Tavily 응답의 개별 검색 결과다.
type tavilyResult struct {
	// Title 은 결과 페이지 제목이다.
	Title string `json:"title"`
	// URL 은 결과 페이지 주소다.
	URL string `json:"url"`
	// Content 는 결과 요약 본문이다.
	Content string `json:"content"`
	// Score 는 관련도 점수다.
	Score float64 `json:"score"`
}

// Search 는 query 로 Tavily 웹 검색을 수행해 최대 maxResults 개의 결과를 반환한다.
// 연결 실패·비정상 상태코드는 error 로 반환한다.
func (c *SearchClient) Search(ctx context.Context, query string, maxResults int) ([]SearchResult, error) {
	reqBody := tavilySearchRequest{
		APIKey:     c.apiKey,
		Query:      query,
		MaxResults: maxResults,
	}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("search: Tavily 요청 직렬화 실패: %w", err)
	}

	url := c.baseURL + "/search"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("search: Tavily 요청 생성 실패: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("search: Tavily 서버 연결 실패: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("search: Tavily 비정상 상태코드 %d: %s", resp.StatusCode, string(body))
	}

	var searchResp tavilySearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&searchResp); err != nil {
		return nil, fmt.Errorf("search: Tavily 응답 파싱 실패: %w", err)
	}

	results := make([]SearchResult, 0, len(searchResp.Results))
	for _, r := range searchResp.Results {
		results = append(results, SearchResult{
			Title:   r.Title,
			URL:     r.URL,
			Content: r.Content,
			Score:   r.Score,
		})
	}
	return results, nil
}
