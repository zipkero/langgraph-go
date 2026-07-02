// tool_test.go 는 SearchTool·WebContentLoaderTool 의 단위 테스트와
// ExtractURLs 순수 함수의 단위 테스트를 담는다(외부 의존 없음/httptest stub, SPEC §5.2).
package search_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zipkero/langgraph-go/config"
	"github.com/zipkero/langgraph-go/search"
	"github.com/zipkero/langgraph-go/tool"
)

// noopRuntime 은 tool.Runtime 의 최소 no-op 구현체다(database/tool_test.go 패턴 참조).
type noopRuntime struct{}

func (noopRuntime) State() any               { return nil }
func (noopRuntime) ToolCallID() string       { return "" }
func (noopRuntime) Config() config.RunConfig { return config.RunConfig{} }
func (noopRuntime) Store() tool.Store        { return nil }
func (noopRuntime) Emit(tool.Event)          {}

// ─── SearchTool ─────────────────────────────────────────────────────────────

// TestSearchTool_스키마 는 SearchTool 의 스키마가 구조체 태그에서 도출됐는지 검증한다.
func TestSearchTool_스키마(t *testing.T) {
	c := search.NewSearchClient("key")
	tl := search.SearchTool(c)

	if tl.Name() != "web_search" {
		t.Errorf("Name() = %q, want web_search", tl.Name())
	}
	names := make(map[string]bool)
	for _, p := range tl.Schema().Parameters {
		names[p.Name] = true
	}
	if !names["query"] {
		t.Errorf("스키마 파라미터에 query 가 없음: %+v", tl.Schema().Parameters)
	}
}

// TestSearchTool_위임호출 은 도구 실행이 stub 서버를 거쳐 검색 결과를 직렬화하는지 검증한다.
func TestSearchTool_위임호출(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results": [{"title": "제목", "url": "https://example.com", "content": "본문", "score": 0.8}]}`))
	}))
	defer ts.Close()

	c := search.NewSearchClient("key", search.WithBaseURL(ts.URL))
	tl := search.SearchTool(c)

	input, _ := json.Marshal(map[string]any{"query": "검색어"})
	res, err := tl.Execute(context.Background(), input, noopRuntime{})
	if err != nil {
		t.Fatalf("Execute 실패: %v", err)
	}
	if res.IsError {
		t.Fatalf("예상치 못한 IsError: %s", res.Content)
	}
	if !strings.Contains(res.Content, "제목") {
		t.Errorf("검색 결과가 직렬화되지 않음: %s", res.Content)
	}
}

// TestSearchTool_빈질의_에러 는 query 가 비어 있으면 IsError 결과를 반환하는지 검증한다.
func TestSearchTool_빈질의_에러(t *testing.T) {
	c := search.NewSearchClient("key")
	tl := search.SearchTool(c)

	input, _ := json.Marshal(map[string]string{"query": "  "})
	res, err := tl.Execute(context.Background(), input, noopRuntime{})
	if err != nil {
		t.Fatalf("Execute 실패: %v", err)
	}
	if !res.IsError {
		t.Errorf("빈 query 인데 IsError 가 아님")
	}
}

// TestSearchTool_클라이언트에러전파 는 SearchClient 에러가 IsError 결과로 전파되는지 검증한다.
func TestSearchTool_클라이언트에러전파(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	c := search.NewSearchClient("key", search.WithBaseURL(ts.URL))
	tl := search.SearchTool(c)

	input, _ := json.Marshal(map[string]string{"query": "검색어"})
	res, err := tl.Execute(context.Background(), input, noopRuntime{})
	if err != nil {
		t.Fatalf("Execute 실패: %v", err)
	}
	if !res.IsError {
		t.Errorf("클라이언트 에러가 IsError 로 전파되지 않음: %+v", res)
	}
}

// ─── WebContentLoaderTool ───────────────────────────────────────────────────

// TestWebContentLoaderTool_스키마 는 WebContentLoaderTool 의 스키마에 urls 파라미터가 있는지 검증한다.
func TestWebContentLoaderTool_스키마(t *testing.T) {
	tl := search.WebContentLoaderTool()

	if tl.Name() != "load_web_content" {
		t.Errorf("Name() = %q, want load_web_content", tl.Name())
	}
	names := make(map[string]bool)
	for _, p := range tl.Schema().Parameters {
		names[p.Name] = true
	}
	if !names["urls"] {
		t.Errorf("스키마 파라미터에 urls 가 없음: %+v", tl.Schema().Parameters)
	}
}

// TestWebContentLoaderTool_본문적재 는 stub HTML 서버에서 본문 텍스트를 적재하는지 검증한다.
func TestWebContentLoaderTool_본문적재(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body><p>안녕하세요 웹 본문입니다</p></body></html>`))
	}))
	defer ts.Close()

	tl := search.WebContentLoaderTool()
	input, _ := json.Marshal(map[string]any{"urls": []string{ts.URL}})
	res, err := tl.Execute(context.Background(), input, noopRuntime{})
	if err != nil {
		t.Fatalf("Execute 실패: %v", err)
	}
	if res.IsError {
		t.Fatalf("예상치 못한 IsError: %s", res.Content)
	}
	if !strings.Contains(res.Content, "안녕하세요") {
		t.Errorf("본문이 적재되지 않음: %s", res.Content)
	}
}

// TestWebContentLoaderTool_빈URL목록_에러 는 urls 가 비어 있으면 IsError 결과를 반환하는지 검증한다.
func TestWebContentLoaderTool_빈URL목록_에러(t *testing.T) {
	tl := search.WebContentLoaderTool()

	input, _ := json.Marshal(map[string]any{"urls": []string{}})
	res, err := tl.Execute(context.Background(), input, noopRuntime{})
	if err != nil {
		t.Fatalf("Execute 실패: %v", err)
	}
	if !res.IsError {
		t.Errorf("빈 urls 인데 IsError 가 아님")
	}
}

// ─── ExtractURLs ────────────────────────────────────────────────────────────

// TestExtractURLs 는 텍스트에서 URL 을 등장 순서대로 추출하는지 검증한다(외부 의존 없는 순수 함수).
func TestExtractURLs(t *testing.T) {
	cases := []struct {
		name string
		text string
		want []string
	}{
		{
			name: "단일_URL",
			text: "참고 자료는 https://example.com/page 입니다.",
			want: []string{"https://example.com/page"},
		},
		{
			name: "복수_URL_순서보존",
			text: "첫번째 http://a.example/1 그리고 두번째 https://b.example/2, 확인.",
			want: []string{"http://a.example/1", "https://b.example/2"},
		},
		{
			name: "URL_없음",
			text: "URL 이 전혀 없는 문장입니다.",
			want: nil,
		},
		{
			name: "괄호와_따옴표에_감싸인_URL",
			text: `자료(https://example.com/x)와 "https://example.com/y" 를 참고.`,
			want: []string{"https://example.com/x", "https://example.com/y"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := search.ExtractURLs(tc.text)
			if len(got) != len(tc.want) {
				t.Fatalf("결과 개수 불일치: got %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("결과[%d] 불일치: got %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}
