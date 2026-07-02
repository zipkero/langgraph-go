// tool.go 는 SearchClient 와 document 웹 로더를 tool.Tool 로 감싸는 SearchTool·WebContentLoaderTool,
// 그리고 텍스트에서 URL을 추출하는 순수 함수 ExtractURLs 를 담는다.
// search 가 tool·document 패키지에 단방향으로 의존하며 역참조는 없다(database/tool.go 패턴 참조).
package search

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/zipkero/langgraph-go/document"
	"github.com/zipkero/langgraph-go/tool"
)

// searchArgs 는 SearchTool 의 입력 스키마다.
type searchArgs struct {
	// Query 는 검색어다.
	Query string `json:"query" description:"검색어"`
	// MaxResults 는 반환받을 최대 결과 수다. 0 이하이면 클라이언트 기본값을 사용한다.
	MaxResults int `json:"max_results,omitempty" description:"반환받을 최대 결과 수"`
}

// SearchTool 은 c.Search 를 감싸 tool.Tool 계약을 충족하는 웹 검색 도구를 반환한다.
func SearchTool(c *SearchClient) tool.Tool {
	return tool.WithArgsSchema("web_search", "Tavily 웹 검색을 수행해 제목·URL·본문·점수를 담은 결과를 반환합니다",
		func(ctx context.Context, args searchArgs, _ tool.Runtime) (tool.Result, error) {
			if strings.TrimSpace(args.Query) == "" {
				return tool.Result{IsError: true, Content: "search: query 가 비어 있습니다"}, nil
			}

			results, err := c.Search(ctx, args.Query, args.MaxResults)
			if err != nil {
				return tool.Result{IsError: true, Content: fmt.Sprintf("search: 웹 검색 실패: %v", err)}, nil
			}
			return tool.Result{Content: serializeSearchResults(results)}, nil
		})
}

// serializeSearchResults 는 SearchResult 목록을 Result.Content 에 담을 문자열로 직렬화한다.
func serializeSearchResults(results []SearchResult) string {
	if len(results) == 0 {
		return "검색 결과가 없습니다."
	}

	var sb strings.Builder
	for i, r := range results {
		fmt.Fprintf(&sb, "[%d] %s (%s) score=%.4f\n%s\n", i+1, r.Title, r.URL, r.Score, r.Content)
	}
	return strings.TrimRight(sb.String(), "\n")
}

// webContentLoaderArgs 는 WebContentLoaderTool 의 입력 스키마다.
type webContentLoaderArgs struct {
	// URLs 는 본문을 적재할 웹 페이지 주소 목록이다.
	URLs []string `json:"urls" description:"본문을 적재할 웹 페이지 URL 목록"`
}

// WebContentLoaderTool 은 기존 document 웹 로더(document.NewWebLoader)를 재사용해
// URL 목록의 HTML 본문을 적재하는 tool.Tool 을 반환한다(신규 HTTP 클라이언트를 만들지 않는다).
func WebContentLoaderTool() tool.Tool {
	return tool.WithArgsSchema("load_web_content", "URL 목록에서 웹 페이지 본문 텍스트를 적재합니다",
		func(ctx context.Context, args webContentLoaderArgs, _ tool.Runtime) (tool.Result, error) {
			if len(args.URLs) == 0 {
				return tool.Result{IsError: true, Content: "search: urls 가 비어 있습니다"}, nil
			}

			loader := document.NewWebLoader(args.URLs)
			docs, err := loader.Load(ctx)
			if err != nil {
				return tool.Result{IsError: true, Content: fmt.Sprintf("search: 웹 본문 적재 실패: %v", err)}, nil
			}
			return tool.Result{Content: serializeDocuments(docs)}, nil
		})
}

// serializeDocuments 는 document.Document 목록을 Result.Content 에 담을 문자열로 직렬화한다.
func serializeDocuments(docs []document.Document) string {
	if len(docs) == 0 {
		return "적재된 본문이 없습니다."
	}

	var sb strings.Builder
	for i, d := range docs {
		source, _ := d.Metadata["source"].(string)
		fmt.Fprintf(&sb, "[%d] %s\n%s\n", i+1, source, d.PageContent)
	}
	return strings.TrimRight(sb.String(), "\n")
}

// urlPattern 은 텍스트에서 http/https URL 을 찾는 정규식이다.
// 공백·따옴표·괄호·홑화살괄호·마침표(문장 종결) 등 URL에 포함되지 않는 구두점 앞에서 멈춘다.
var urlPattern = regexp.MustCompile(`https?://[^\s<>"'` + "`" + `\)\]]+`)

// ExtractURLs 는 text 에서 http/https URL을 찾아 등장 순서대로 반환한다(외부 의존 없는 순수 함수).
// URL 뒤에 붙은 문장 종결 구두점(., ,, ;, :, !, ?)은 잘라낸다.
func ExtractURLs(text string) []string {
	matches := urlPattern.FindAllString(text, -1)
	urls := make([]string, 0, len(matches))
	for _, m := range matches {
		urls = append(urls, strings.TrimRight(m, ".,;:!?"))
	}
	return urls
}
