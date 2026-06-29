// retriever_tool_test.go 는 CreateRetrieverTool 의 메타 및 Execute 출력을 검증한다.
// stub EmbeddingClient(결정적 벡터)로 vectorstore 를 구성해 네트워크 없이 항상 실행한다(task-006).
package vectorstore_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/zipkero/langgraph-go/document"
	"github.com/zipkero/langgraph-go/tool"
	"github.com/zipkero/langgraph-go/vectorstore"
)

// TestCreateRetrieverTool_Meta 는 CreateRetrieverTool 이 반환하는 도구가 지정한
// name/description 과 질의 파라미터 Schema 를 갖는지 검증한다.
func TestCreateRetrieverTool_Meta(t *testing.T) {
	// 빈 retriever 로도 메타 확인은 가능하다.
	stub := makeStub(map[string][]float32{})
	ctx := context.Background()

	store, err := vectorstore.FromDocuments(ctx, []document.Document{}, stub)
	if err != nil {
		t.Fatalf("FromDocuments 실패: %v", err)
	}

	retriever := store.AsRetriever(vectorstore.SearchOptions{K: 3})
	const toolName = "search_documents"
	const toolDesc = "문서 데이터베이스에서 관련 내용을 검색합니다"

	rt := vectorstore.CreateRetrieverTool(retriever, toolName, toolDesc)

	// name 검증
	if rt.Name() != toolName {
		t.Errorf("Name 기대 %q, 실제 %q", toolName, rt.Name())
	}

	// description 검증
	if rt.Description() != toolDesc {
		t.Errorf("Description 기대 %q, 실제 %q", toolDesc, rt.Description())
	}

	// Schema 검증: query 파라미터가 존재해야 한다.
	schema := rt.Schema()
	if schema.Name != toolName {
		t.Errorf("Schema.Name 기대 %q, 실제 %q", toolName, schema.Name)
	}
	if schema.Description != toolDesc {
		t.Errorf("Schema.Description 기대 %q, 실제 %q", toolDesc, schema.Description)
	}

	// query 파라미터 존재 여부 확인
	var queryParam *tool.Parameter
	for i := range schema.Parameters {
		if schema.Parameters[i].Name == "query" {
			queryParam = &schema.Parameters[i]
			break
		}
	}
	if queryParam == nil {
		t.Fatalf("Schema.Parameters 에 'query' 파라미터가 없음: %+v", schema.Parameters)
	}
	if queryParam.Type != "string" {
		t.Errorf("query 파라미터 타입 기대 string, 실제 %q", queryParam.Type)
	}
}

// TestCreateRetrieverTool_Execute 는 질의로 Execute 를 호출했을 때 검색 결과 텍스트가
// Result.Content 에 담겨 반환되는지 검증한다.
// stub EmbeddingClient 로 결정적 벡터를 주입해 네트워크 없이 항상 실행한다.
func TestCreateRetrieverTool_Execute(t *testing.T) {
	// 유사도: result1(1.0) > result2(≈0.7) > result3(0.0)
	stub := makeStub(map[string][]float32{
		"golang":  {1, 0, 0},
		"result1": {1, 0, 0},
		"result2": {0.7, 0.7, 0},
		"result3": {0, 1, 0},
	})

	docs := []document.Document{
		{PageContent: "result1", Metadata: map[string]any{"source": "doc_a"}},
		{PageContent: "result2", Metadata: map[string]any{"source": "doc_b"}},
		{PageContent: "result3", Metadata: map[string]any{"source": "doc_c"}},
	}

	ctx := context.Background()
	store, err := vectorstore.FromDocuments(ctx, docs, stub)
	if err != nil {
		t.Fatalf("FromDocuments 실패: %v", err)
	}

	retriever := store.AsRetriever(vectorstore.SearchOptions{K: 2})
	rt := vectorstore.CreateRetrieverTool(retriever, "search", "검색 도구")

	// JSON 인자로 질의를 전달한다.
	args, _ := json.Marshal(map[string]string{"query": "golang"})
	result, err := rt.Execute(ctx, args, nil)
	if err != nil {
		t.Fatalf("Execute 실패: %v", err)
	}

	// 오류 없이 성공해야 한다.
	if result.IsError {
		t.Fatalf("Execute 가 오류를 반환함: %s", result.Content)
	}

	// Content 가 비어 있으면 안 된다.
	if strings.TrimSpace(result.Content) == "" {
		t.Fatal("Result.Content 가 비어 있음")
	}

	// 상위 2개 결과(result1, result2)가 Content 에 포함되어야 한다.
	if !strings.Contains(result.Content, "result1") {
		t.Errorf("Content 에 result1 이 없음: %s", result.Content)
	}
	if !strings.Contains(result.Content, "result2") {
		t.Errorf("Content 에 result2 이 없음: %s", result.Content)
	}
	// K=2 이므로 result3 은 포함되지 않아야 한다.
	if strings.Contains(result.Content, "result3") {
		t.Errorf("Content 에 K 초과 결과 result3 이 포함됨: %s", result.Content)
	}
}

// TestCreateRetrieverTool_EmptyQuery 는 빈 질의로 Execute 를 호출했을 때 IsError=true 로
// 반환되는지 검증한다.
func TestCreateRetrieverTool_EmptyQuery(t *testing.T) {
	stub := makeStub(map[string][]float32{})
	ctx := context.Background()

	store, err := vectorstore.FromDocuments(ctx, []document.Document{}, stub)
	if err != nil {
		t.Fatalf("FromDocuments 실패: %v", err)
	}

	retriever := store.AsRetriever(vectorstore.SearchOptions{K: 3})
	rt := vectorstore.CreateRetrieverTool(retriever, "search", "검색 도구")

	// 빈 query 를 전달한다.
	args, _ := json.Marshal(map[string]string{"query": "   "})
	result, err := rt.Execute(ctx, args, nil)
	if err != nil {
		t.Fatalf("Execute 가 error 를 반환해서는 안 됨: %v", err)
	}

	if !result.IsError {
		t.Fatalf("빈 질의는 IsError=true 여야 함, Content: %s", result.Content)
	}
}

// TestCreateRetrieverTool_NoResults 는 검색 결과가 없을 때 안내 문자열이 반환되는지 검증한다.
func TestCreateRetrieverTool_NoResults(t *testing.T) {
	// 질의 벡터는 등록하나 도큐먼트가 없어 결과가 없다.
	stub := makeStub(map[string][]float32{
		"unknown_query": {1, 0, 0},
	})

	ctx := context.Background()
	store, err := vectorstore.FromDocuments(ctx, []document.Document{}, stub)
	if err != nil {
		t.Fatalf("FromDocuments 실패: %v", err)
	}

	retriever := store.AsRetriever(vectorstore.SearchOptions{K: 3})
	rt := vectorstore.CreateRetrieverTool(retriever, "search", "검색 도구")

	args, _ := json.Marshal(map[string]string{"query": "unknown_query"})
	result, err := rt.Execute(ctx, args, nil)
	if err != nil {
		t.Fatalf("Execute 실패: %v", err)
	}

	if result.IsError {
		t.Fatalf("결과 없음은 오류가 아님, Content: %s", result.Content)
	}
	if !strings.Contains(result.Content, "없습니다") {
		t.Errorf("결과 없음 안내 문자열 기대, 실제: %s", result.Content)
	}
}
