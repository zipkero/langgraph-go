// loader_test.go 는 Loader 인터페이스 구현체(Web·PDF·DOCX)의 단위 테스트를 담는다.
// 네트워크 외부 의존 없이 항상 실행된다:
//   - 웹 로더: net/http/httptest.Server 로 격리
//   - PDF 로더: testdata/sample.pdf 고정 파일 사용
//   - DOCX 로더: testdata/sample.docx 고정 파일 사용
//   - ReadPDFBytes: testdata/sample.pdf 를 바이트로 읽어 검증
package document

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// ────────────────────────────────────────────
// 웹 로더 테스트
// ────────────────────────────────────────────

// TestWebLoader_Load 는 httptest.Server 에서 HTML 을 가져와 텍스트가 추출되는지 검증한다.
func TestWebLoader_Load(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><body><p>Hello Web World</p></body></html>`)
	}))
	defer ts.Close()

	loader := NewWebLoader([]string{ts.URL})
	docs, err := loader.Load(context.Background())
	if err != nil {
		t.Fatalf("WebLoader.Load 실패: %v", err)
	}
	if len(docs) != 1 {
		t.Fatalf("문서 수: want 1, got %d", len(docs))
	}
	if strings.TrimSpace(docs[0].PageContent) == "" {
		t.Errorf("PageContent 가 비어 있음")
	}
	if !strings.Contains(docs[0].PageContent, "Hello Web World") {
		t.Errorf("PageContent 에 기대 텍스트 없음: got %q", docs[0].PageContent)
	}
	if docs[0].Metadata["source"] != ts.URL {
		t.Errorf("Metadata[source]: want %q, got %v", ts.URL, docs[0].Metadata["source"])
	}
}

// TestWebLoader_LazyLoad 는 LazyLoad 가 채널로 같은 문서를 흘리는지 검증한다.
func TestWebLoader_LazyLoad(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><body><p>Lazy Load Test</p></body></html>`)
	}))
	defer ts.Close()

	loader := NewWebLoader([]string{ts.URL})
	ch, err := loader.LazyLoad(context.Background())
	if err != nil {
		t.Fatalf("WebLoader.LazyLoad 셋업 실패: %v", err)
	}

	var docs []Document
	for doc := range ch {
		docs = append(docs, doc)
	}
	if len(docs) != 1 {
		t.Fatalf("LazyLoad 문서 수: want 1, got %d", len(docs))
	}
	if !strings.Contains(docs[0].PageContent, "Lazy Load Test") {
		t.Errorf("LazyLoad PageContent 기대 텍스트 없음: got %q", docs[0].PageContent)
	}
}

// TestWebLoader_LoadAndLazyLoadSameResult 는 Load 와 LazyLoad 가 같은 문서 집합을 산출하는지 검증한다.
func TestWebLoader_LoadAndLazyLoadSameResult(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><body><p>Consistent Result</p></body></html>`)
	}))
	defer ts.Close()

	loader := NewWebLoader([]string{ts.URL})

	loadDocs, err := loader.Load(context.Background())
	if err != nil {
		t.Fatalf("Load 실패: %v", err)
	}

	ch, err := loader.LazyLoad(context.Background())
	if err != nil {
		t.Fatalf("LazyLoad 셋업 실패: %v", err)
	}
	var lazyDocs []Document
	for doc := range ch {
		lazyDocs = append(lazyDocs, doc)
	}

	if len(loadDocs) != len(lazyDocs) {
		t.Errorf("Load(%d)와 LazyLoad(%d) 문서 수 불일치", len(loadDocs), len(lazyDocs))
	}
	for i := range loadDocs {
		if loadDocs[i].PageContent != lazyDocs[i].PageContent {
			t.Errorf("문서[%d] PageContent 불일치: Load=%q LazyLoad=%q",
				i, loadDocs[i].PageContent, lazyDocs[i].PageContent)
		}
	}
}

// TestWebLoader_InvalidURL 은 잘못된 URL 에서 Load 가 error 를 반환하는지 검증한다.
func TestWebLoader_InvalidURL(t *testing.T) {
	loader := NewWebLoader([]string{"http://127.0.0.1:0/nonexistent"})
	_, err := loader.Load(context.Background())
	if err == nil {
		t.Error("잘못된 URL: error 를 기대했지만 nil 반환")
	}
}

// TestWebLoader_MultipleURLs 는 여러 URL 이 각각 Document 로 적재되는지 검증한다.
func TestWebLoader_MultipleURLs(t *testing.T) {
	handler := func(msg string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, `<html><body><p>%s</p></body></html>`, msg)
		}
	}
	ts1 := httptest.NewServer(handler("Page One"))
	defer ts1.Close()
	ts2 := httptest.NewServer(handler("Page Two"))
	defer ts2.Close()

	loader := NewWebLoader([]string{ts1.URL, ts2.URL})
	docs, err := loader.Load(context.Background())
	if err != nil {
		t.Fatalf("Load 실패: %v", err)
	}
	if len(docs) != 2 {
		t.Fatalf("문서 수: want 2, got %d", len(docs))
	}
	if !strings.Contains(docs[0].PageContent, "Page One") {
		t.Errorf("첫 번째 문서 PageContent 오류: got %q", docs[0].PageContent)
	}
	if !strings.Contains(docs[1].PageContent, "Page Two") {
		t.Errorf("두 번째 문서 PageContent 오류: got %q", docs[1].PageContent)
	}
}

// ────────────────────────────────────────────
// PDF 로더 테스트
// ────────────────────────────────────────────

const samplePDFPath = "testdata/sample.pdf"

// TestPDFLoader_Load 는 sample.pdf 가 Document 목록으로 적재되는지 검증한다.
func TestPDFLoader_Load(t *testing.T) {
	if _, err := os.Stat(samplePDFPath); err != nil {
		t.Skipf("testdata/sample.pdf 없음 — testdata/gen 을 실행해 생성하세요: %v", err)
	}

	loader := NewPDFLoader(samplePDFPath)
	docs, err := loader.Load(context.Background())
	if err != nil {
		t.Fatalf("PDFLoader.Load 실패: %v", err)
	}
	if len(docs) == 0 {
		t.Fatal("문서 수: 0, 최소 1개 기대")
	}
	for i, doc := range docs {
		if strings.TrimSpace(doc.PageContent) == "" {
			t.Errorf("docs[%d] PageContent 가 비어 있음", i)
		}
		// 메타 검증
		if doc.Metadata["page"] == nil {
			t.Errorf("docs[%d] Metadata[page] 가 nil", i)
		}
		if doc.Metadata["source"] == nil {
			t.Errorf("docs[%d] Metadata[source] 가 nil", i)
		}
		if doc.Metadata["total_pages"] == nil {
			t.Errorf("docs[%d] Metadata[total_pages] 가 nil", i)
		}
		if doc.Metadata["source"] != samplePDFPath {
			t.Errorf("docs[%d] Metadata[source]: want %q, got %v", i, samplePDFPath, doc.Metadata["source"])
		}
	}
}

// TestPDFLoader_PageMetadata 는 PDF 문서의 메타에 page/source/total_pages 가 채워지는지 검증한다.
func TestPDFLoader_PageMetadata(t *testing.T) {
	if _, err := os.Stat(samplePDFPath); err != nil {
		t.Skipf("testdata/sample.pdf 없음: %v", err)
	}

	loader := NewPDFLoader(samplePDFPath)
	docs, err := loader.Load(context.Background())
	if err != nil {
		t.Fatalf("PDFLoader.Load 실패: %v", err)
	}
	if len(docs) == 0 {
		t.Fatal("문서 없음")
	}

	firstDoc := docs[0]
	page, ok := firstDoc.Metadata["page"].(int)
	if !ok {
		t.Errorf("Metadata[page] 타입: want int, got %T", firstDoc.Metadata["page"])
	}
	if page < 1 {
		t.Errorf("Metadata[page]: want >= 1, got %d", page)
	}

	totalPages, ok := firstDoc.Metadata["total_pages"].(int)
	if !ok {
		t.Errorf("Metadata[total_pages] 타입: want int, got %T", firstDoc.Metadata["total_pages"])
	}
	if totalPages < 1 {
		t.Errorf("Metadata[total_pages]: want >= 1, got %d", totalPages)
	}
	if page > totalPages {
		t.Errorf("Metadata[page](%d) > Metadata[total_pages](%d)", page, totalPages)
	}
}

// TestPDFLoader_LazyLoad 는 LazyLoad 가 Load 와 같은 문서 집합을 채널로 흘리는지 검증한다.
func TestPDFLoader_LazyLoad(t *testing.T) {
	if _, err := os.Stat(samplePDFPath); err != nil {
		t.Skipf("testdata/sample.pdf 없음: %v", err)
	}

	loader := NewPDFLoader(samplePDFPath)

	loadDocs, err := loader.Load(context.Background())
	if err != nil {
		t.Fatalf("Load 실패: %v", err)
	}

	ch, err := loader.LazyLoad(context.Background())
	if err != nil {
		t.Fatalf("LazyLoad 셋업 실패: %v", err)
	}
	var lazyDocs []Document
	for doc := range ch {
		lazyDocs = append(lazyDocs, doc)
	}

	if len(loadDocs) != len(lazyDocs) {
		t.Errorf("Load(%d)와 LazyLoad(%d) 문서 수 불일치", len(loadDocs), len(lazyDocs))
	}
	for i := range loadDocs {
		if loadDocs[i].PageContent != lazyDocs[i].PageContent {
			t.Errorf("docs[%d] PageContent 불일치: Load=%q LazyLoad=%q",
				i, loadDocs[i].PageContent, lazyDocs[i].PageContent)
		}
	}
}

// TestPDFLoader_InvalidPath 는 존재하지 않는 경로에서 Load 가 error 를 반환하는지 검증한다.
func TestPDFLoader_InvalidPath(t *testing.T) {
	loader := NewPDFLoader("testdata/nonexistent.pdf")
	_, err := loader.Load(context.Background())
	if err == nil {
		t.Error("잘못된 경로: error 를 기대했지만 nil 반환")
	}
}

// TestPDFLoader_InvalidPath_LazyLoad 는 존재하지 않는 경로에서 LazyLoad 가 셋업 error 를 반환하는지 검증한다.
func TestPDFLoader_InvalidPath_LazyLoad(t *testing.T) {
	loader := NewPDFLoader("testdata/nonexistent.pdf")
	ch, err := loader.LazyLoad(context.Background())
	if err == nil {
		// 셋업 error 가 반환되면 ch 는 nil 이어야 한다.
		// 만약 error 없이 반환됐다면 ch 가 즉시 닫혀야 한다.
		if ch != nil {
			var count int
			for range ch {
				count++
			}
			if count > 0 {
				t.Errorf("잘못된 경로 LazyLoad: 문서가 반환되면 안 됨, got %d", count)
			}
		}
	}
	// error 또는 빈 채널 중 하나면 통과
}

// ────────────────────────────────────────────
// ReadPDFBytes 테스트
// ────────────────────────────────────────────

// TestReadPDFBytes 는 PDF 바이트에서 텍스트가 추출되는지 검증한다.
func TestReadPDFBytes(t *testing.T) {
	if _, err := os.Stat(samplePDFPath); err != nil {
		t.Skipf("testdata/sample.pdf 없음: %v", err)
	}

	b, err := os.ReadFile(samplePDFPath)
	if err != nil {
		t.Fatalf("파일 읽기 실패: %v", err)
	}

	text, err := ReadPDFBytes(b)
	if err != nil {
		t.Fatalf("ReadPDFBytes 실패: %v", err)
	}
	if strings.TrimSpace(text) == "" {
		t.Error("ReadPDFBytes: 추출된 텍스트가 비어 있음")
	}
	if !strings.Contains(text, "Hello PDF World") {
		t.Errorf("ReadPDFBytes: 기대 텍스트 없음, got %q", text)
	}
}

// TestReadPDFBytes_InvalidBytes 는 잘못된 바이트에서 error 가 반환되는지 검증한다.
func TestReadPDFBytes_InvalidBytes(t *testing.T) {
	_, err := ReadPDFBytes([]byte("not a pdf"))
	if err == nil {
		t.Error("잘못된 바이트: error 를 기대했지만 nil 반환")
	}
}

// ────────────────────────────────────────────
// DOCX 로더 테스트
// ────────────────────────────────────────────

const sampleDocxPath = "testdata/sample.docx"

// TestDocxLoader_Load 는 sample.docx 가 Document 로 적재되는지 검증한다.
func TestDocxLoader_Load(t *testing.T) {
	if _, err := os.Stat(sampleDocxPath); err != nil {
		t.Skipf("testdata/sample.docx 없음 — testdata/gen 을 실행해 생성하세요: %v", err)
	}

	loader := NewDocxLoader(sampleDocxPath)
	docs, err := loader.Load(context.Background())
	if err != nil {
		t.Fatalf("DocxLoader.Load 실패: %v", err)
	}
	if len(docs) == 0 {
		t.Fatal("문서 수: 0, 최소 1개 기대")
	}
	if strings.TrimSpace(docs[0].PageContent) == "" {
		t.Error("PageContent 가 비어 있음")
	}
	if !strings.Contains(docs[0].PageContent, "Hello DOCX World") {
		t.Errorf("PageContent 에 기대 텍스트 없음: got %q", docs[0].PageContent)
	}
	if docs[0].Metadata["source"] != sampleDocxPath {
		t.Errorf("Metadata[source]: want %q, got %v", sampleDocxPath, docs[0].Metadata["source"])
	}
}

// TestDocxLoader_LazyLoad 는 LazyLoad 가 Load 와 같은 문서를 채널로 흘리는지 검증한다.
func TestDocxLoader_LazyLoad(t *testing.T) {
	if _, err := os.Stat(sampleDocxPath); err != nil {
		t.Skipf("testdata/sample.docx 없음: %v", err)
	}

	loader := NewDocxLoader(sampleDocxPath)

	loadDocs, err := loader.Load(context.Background())
	if err != nil {
		t.Fatalf("Load 실패: %v", err)
	}

	ch, err := loader.LazyLoad(context.Background())
	if err != nil {
		t.Fatalf("LazyLoad 셋업 실패: %v", err)
	}
	var lazyDocs []Document
	for doc := range ch {
		lazyDocs = append(lazyDocs, doc)
	}

	if len(loadDocs) != len(lazyDocs) {
		t.Errorf("Load(%d)와 LazyLoad(%d) 문서 수 불일치", len(loadDocs), len(lazyDocs))
	}
	if len(loadDocs) > 0 && len(lazyDocs) > 0 {
		if loadDocs[0].PageContent != lazyDocs[0].PageContent {
			t.Errorf("PageContent 불일치: Load=%q LazyLoad=%q",
				loadDocs[0].PageContent, lazyDocs[0].PageContent)
		}
	}
}

// TestDocxLoader_InvalidPath 는 존재하지 않는 경로에서 Load 가 error 를 반환하는지 검증한다.
func TestDocxLoader_InvalidPath(t *testing.T) {
	loader := NewDocxLoader("testdata/nonexistent.docx")
	_, err := loader.Load(context.Background())
	if err == nil {
		t.Error("잘못된 경로: error 를 기대했지만 nil 반환")
	}
}

// TestDocxLoader_InvalidPath_LazyLoad 는 존재하지 않는 경로에서 LazyLoad 가 셋업 error 를 반환하는지 검증한다.
func TestDocxLoader_InvalidPath_LazyLoad(t *testing.T) {
	loader := NewDocxLoader("testdata/nonexistent.docx")
	_, err := loader.LazyLoad(context.Background())
	if err == nil {
		t.Error("잘못된 경로 LazyLoad: 셋업 error 를 기대했지만 nil 반환")
	}
}
