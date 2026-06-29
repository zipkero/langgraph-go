// retriever_tool.go 는 Retriever 를 tool.Tool 로 래핑하는 생성 함수를 제공한다.
// vectorstore 패키지가 tool 패키지에 단방향으로 의존하며, 역참조는 없다(SPEC §5.7, §5.9).
package vectorstore

import (
	"context"
	"fmt"
	"strings"

	"github.com/zipkero/langgraph-go/document"
	"github.com/zipkero/langgraph-go/tool"
)

// retrieverToolArgs 는 CreateRetrieverTool 이 생성하는 도구의 입력 스키마를 나타낸다.
// WithArgsSchema 가 구조체 태그(json/description)를 읽어 파라미터 스키마를 도출한다.
type retrieverToolArgs struct {
	// Query 는 유사도 검색에 사용할 질의 문자열이다.
	Query string `json:"query" description:"검색할 질의 텍스트"`
}

// CreateRetrieverTool 은 r 을 감싸 tool.Tool 계약을 충족하는 도구를 반환한다.
// 반환된 도구는 name/description 과 질의 파라미터 스키마를 갖고,
// Execute 호출 시 질의를 파싱해 r.Invoke 를 실행한 뒤 검색 결과를 Result.Content 로 직렬화한다.
func CreateRetrieverTool(r Retriever, name, description string) tool.Tool {
	return tool.WithArgsSchema(name, description, func(ctx context.Context, args retrieverToolArgs, _ tool.Runtime) (tool.Result, error) {
		// 질의 텍스트가 비어 있으면 오류를 반환한다.
		if strings.TrimSpace(args.Query) == "" {
			return tool.Result{IsError: true, Content: "retriever_tool: 질의 텍스트가 비어 있습니다"}, nil
		}

		// Retriever 로 유사도 검색을 실행한다.
		docs, err := r.Invoke(ctx, args.Query)
		if err != nil {
			return tool.Result{IsError: true, Content: fmt.Sprintf("retriever_tool: 검색 실패: %v", err)}, nil
		}

		// 검색 결과 Document 를 텍스트로 직렬화한다.
		content := serializeDocs(docs)
		return tool.Result{Content: content}, nil
	})
}

// serializeDocs 는 Document 목록을 Result.Content 에 담을 문자열로 직렬화한다.
// 각 Document 의 PageContent 를 번호와 함께 나열하고, 결과가 없으면 안내 문자열을 반환한다.
func serializeDocs(docs []document.Document) string {
	if len(docs) == 0 {
		return "검색 결과가 없습니다."
	}

	var sb strings.Builder
	for i, doc := range docs {
		// 순위와 PageContent 를 기록한다.
		fmt.Fprintf(&sb, "[%d] %s", i+1, doc.PageContent)
		// Metadata 가 있으면 추가 정보로 병기한다.
		if len(doc.Metadata) > 0 {
			fmt.Fprintf(&sb, " (metadata: %v)", doc.Metadata)
		}
		sb.WriteByte('\n')
	}
	return strings.TrimRight(sb.String(), "\n")
}
