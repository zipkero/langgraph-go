// tool_test.go 는 SaveWebDataTool/SearchWebDataTool 의 단위 테스트다.
// 실제 DB 없이 fakeClient 를 주입해 스키마 도출과 위임 호출을 검증한다.
package database_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/zipkero/langgraph-go/config"
	"github.com/zipkero/langgraph-go/database"
	"github.com/zipkero/langgraph-go/tool"
)

// fakeClient 는 database.Client 의 테스트용 인메모리 구현체다.
type fakeClient struct {
	inserted     []database.WebContentRecord
	searchResult []database.WebContentRecord
	searchErr    error
	insertErr    error
	lastKeyword  string
}

func (f *fakeClient) Connect(ctx context.Context) error { return nil }
func (f *fakeClient) Close(ctx context.Context) error    { return nil }

func (f *fakeClient) InsertWebContent(ctx context.Context, rec database.WebContentRecord) error {
	if f.insertErr != nil {
		return f.insertErr
	}
	f.inserted = append(f.inserted, rec)
	return nil
}

func (f *fakeClient) SearchWebContent(ctx context.Context, keyword string) ([]database.WebContentRecord, error) {
	f.lastKeyword = keyword
	if f.searchErr != nil {
		return nil, f.searchErr
	}
	return f.searchResult, nil
}

func (f *fakeClient) InsertDocumentChunks(ctx context.Context, recs []database.DocumentRecord) error {
	return nil
}

func (f *fakeClient) MatchDocuments(ctx context.Context, embedding []float32, count int) ([]database.DocumentRecord, error) {
	return nil, nil
}

func (f *fakeClient) QueryDocuments(ctx context.Context, q database.DocumentQuery) ([]database.DocumentRecord, error) {
	return nil, nil
}

// noopRuntime 은 tool.Runtime 의 최소 no-op 구현체다.
type noopRuntime struct{}

func (noopRuntime) State() any               { return nil }
func (noopRuntime) ToolCallID() string       { return "" }
func (noopRuntime) Config() config.RunConfig { return config.RunConfig{} }
func (noopRuntime) Store() tool.Store        { return nil }
func (noopRuntime) Emit(tool.Event)          {}

// TestSaveWebDataTool_스키마 는 SaveWebDataTool 의 스키마가 구조체 태그에서 도출됐는지 검증한다.
func TestSaveWebDataTool_스키마(t *testing.T) {
	c := &fakeClient{}
	tl := database.SaveWebDataTool(c)

	if tl.Name() != "save_web_data" {
		t.Errorf("Name() = %q, want save_web_data", tl.Name())
	}
	schema := tl.Schema()
	names := make(map[string]bool)
	for _, p := range schema.Parameters {
		names[p.Name] = true
	}
	for _, want := range []string{"title", "url", "content"} {
		if !names[want] {
			t.Errorf("스키마 파라미터에 %q 가 없음: %+v", want, schema.Parameters)
		}
	}
}

// TestSaveWebDataTool_위임호출 은 도구 실행이 Client.InsertWebContent 로 위임되는지 검증한다.
func TestSaveWebDataTool_위임호출(t *testing.T) {
	c := &fakeClient{}
	tl := database.SaveWebDataTool(c)

	input, _ := json.Marshal(map[string]string{
		"title":   "제목",
		"url":     "https://example.com",
		"content": "본문",
	})
	res, err := tl.Execute(context.Background(), input, noopRuntime{})
	if err != nil {
		t.Fatalf("Execute 실패: %v", err)
	}
	if res.IsError {
		t.Fatalf("예상치 못한 IsError: %s", res.Content)
	}
	if len(c.inserted) != 1 || c.inserted[0].Title != "제목" {
		t.Errorf("InsertWebContent 로 위임되지 않음: %+v", c.inserted)
	}
}

// TestSaveWebDataTool_빈제목_에러 는 title 이 비어 있으면 IsError 결과를 반환하는지 검증한다.
func TestSaveWebDataTool_빈제목_에러(t *testing.T) {
	c := &fakeClient{}
	tl := database.SaveWebDataTool(c)

	input, _ := json.Marshal(map[string]string{"title": "  "})
	res, err := tl.Execute(context.Background(), input, noopRuntime{})
	if err != nil {
		t.Fatalf("Execute 실패: %v", err)
	}
	if !res.IsError {
		t.Errorf("빈 title 인데 IsError 가 아님")
	}
}

// TestSaveWebDataTool_클라이언트에러전파 는 Client 에러가 IsError 결과로 전파되는지 검증한다.
func TestSaveWebDataTool_클라이언트에러전파(t *testing.T) {
	c := &fakeClient{insertErr: errors.New("연결 실패")}
	tl := database.SaveWebDataTool(c)

	input, _ := json.Marshal(map[string]string{"title": "제목"})
	res, err := tl.Execute(context.Background(), input, noopRuntime{})
	if err != nil {
		t.Fatalf("Execute 실패: %v", err)
	}
	if !res.IsError || !strings.Contains(res.Content, "연결 실패") {
		t.Errorf("클라이언트 에러가 전파되지 않음: %+v", res)
	}
}

// TestSearchWebDataTool_위임호출 은 도구 실행이 Client.SearchWebContent 로 위임되는지 검증한다.
func TestSearchWebDataTool_위임호출(t *testing.T) {
	c := &fakeClient{
		searchResult: []database.WebContentRecord{
			{Title: "결과1", URL: "https://a.example", Content: "내용1"},
		},
	}
	tl := database.SearchWebDataTool(c)

	input, _ := json.Marshal(map[string]string{"keyword": "검색어"})
	res, err := tl.Execute(context.Background(), input, noopRuntime{})
	if err != nil {
		t.Fatalf("Execute 실패: %v", err)
	}
	if res.IsError {
		t.Fatalf("예상치 못한 IsError: %s", res.Content)
	}
	if c.lastKeyword != "검색어" {
		t.Errorf("SearchWebContent 로 위임되지 않음: lastKeyword=%q", c.lastKeyword)
	}
	if !strings.Contains(res.Content, "결과1") {
		t.Errorf("검색 결과가 직렬화되지 않음: %s", res.Content)
	}
}

// TestSearchWebDataTool_빈키워드_에러 는 keyword 가 비어 있으면 IsError 결과를 반환하는지 검증한다.
func TestSearchWebDataTool_빈키워드_에러(t *testing.T) {
	c := &fakeClient{}
	tl := database.SearchWebDataTool(c)

	input, _ := json.Marshal(map[string]string{"keyword": ""})
	res, err := tl.Execute(context.Background(), input, noopRuntime{})
	if err != nil {
		t.Fatalf("Execute 실패: %v", err)
	}
	if !res.IsError {
		t.Errorf("빈 keyword 인데 IsError 가 아님")
	}
}

// TestSearchWebDataTool_결과없음 은 검색 결과가 없을 때 안내 문자열을 반환하는지 검증한다.
func TestSearchWebDataTool_결과없음(t *testing.T) {
	c := &fakeClient{}
	tl := database.SearchWebDataTool(c)

	input, _ := json.Marshal(map[string]string{"keyword": "검색어"})
	res, err := tl.Execute(context.Background(), input, noopRuntime{})
	if err != nil {
		t.Fatalf("Execute 실패: %v", err)
	}
	if !strings.Contains(res.Content, "검색 결과가 없습니다") {
		t.Errorf("결과 없음 안내 문구가 없음: %s", res.Content)
	}
}
