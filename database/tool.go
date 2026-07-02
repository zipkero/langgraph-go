// tool.go 는 Client 메서드를 tool.Tool 로 래핑해 노출하는 SaveWebDataTool/SearchWebDataTool 을 담는다.
// database 패키지가 tool 패키지에 단방향으로 의존하며, 역참조는 없다(vectorstore/retriever_tool.go 패턴 참조).
package database

import (
	"context"
	"fmt"
	"strings"

	"github.com/zipkero/langgraph-go/tool"
)

// saveWebDataArgs 는 SaveWebDataTool 의 입력 스키마다.
// WithArgsSchema 가 구조체 태그(json/description)를 읽어 파라미터 스키마를 도출한다.
type saveWebDataArgs struct {
	// Title 은 저장할 웹 콘텐츠 제목이다.
	Title string `json:"title" description:"웹 콘텐츠 제목"`
	// URL 은 원본 웹 페이지 주소다.
	URL string `json:"url" description:"원본 웹 페이지 URL"`
	// Content 는 웹 페이지 본문이다.
	Content string `json:"content" description:"웹 페이지 본문"`
	// ExpandedQuery 는 검색 확장에 사용된 질의 문자열이다.
	ExpandedQuery string `json:"expanded_query,omitempty" description:"검색 확장에 사용된 질의 문자열"`
}

// SaveWebDataTool 은 c.InsertWebContent 를 감싸 tool.Tool 계약을 충족하는 도구를 반환한다.
func SaveWebDataTool(c Client) tool.Tool {
	return tool.WithArgsSchema("save_web_data", "웹 콘텐츠를 title 전문 검색용 저장소에 저장합니다",
		func(ctx context.Context, args saveWebDataArgs, _ tool.Runtime) (tool.Result, error) {
			if strings.TrimSpace(args.Title) == "" {
				return tool.Result{IsError: true, Content: "database: title 이 비어 있습니다"}, nil
			}

			rec := WebContentRecord{
				Title:         args.Title,
				URL:           args.URL,
				Content:       args.Content,
				ExpandedQuery: args.ExpandedQuery,
			}
			if err := c.InsertWebContent(ctx, rec); err != nil {
				return tool.Result{IsError: true, Content: fmt.Sprintf("database: 웹 콘텐츠 저장 실패: %v", err)}, nil
			}
			return tool.Result{Content: fmt.Sprintf("웹 콘텐츠 저장 완료: %s", args.Title)}, nil
		})
}

// searchWebDataArgs 는 SearchWebDataTool 의 입력 스키마다.
type searchWebDataArgs struct {
	// Keyword 는 title 전문 검색에 사용할 검색어다.
	Keyword string `json:"keyword" description:"title 전문 검색에 사용할 검색어"`
}

// SearchWebDataTool 은 c.SearchWebContent 를 감싸 tool.Tool 계약을 충족하는 도구를 반환한다.
func SearchWebDataTool(c Client) tool.Tool {
	return tool.WithArgsSchema("search_web_data", "저장된 웹 콘텐츠를 title 전문 검색으로 조회합니다",
		func(ctx context.Context, args searchWebDataArgs, _ tool.Runtime) (tool.Result, error) {
			if strings.TrimSpace(args.Keyword) == "" {
				return tool.Result{IsError: true, Content: "database: keyword 가 비어 있습니다"}, nil
			}

			recs, err := c.SearchWebContent(ctx, args.Keyword)
			if err != nil {
				return tool.Result{IsError: true, Content: fmt.Sprintf("database: 웹 콘텐츠 검색 실패: %v", err)}, nil
			}
			return tool.Result{Content: serializeWebContentRecords(recs)}, nil
		})
}

// serializeWebContentRecords 는 WebContentRecord 목록을 Result.Content 에 담을 문자열로 직렬화한다.
func serializeWebContentRecords(recs []WebContentRecord) string {
	if len(recs) == 0 {
		return "검색 결과가 없습니다."
	}

	var sb strings.Builder
	for i, rec := range recs {
		fmt.Fprintf(&sb, "[%d] %s (%s)\n%s\n", i+1, rec.Title, rec.URL, rec.Content)
	}
	return strings.TrimRight(sb.String(), "\n")
}
