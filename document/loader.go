// loader.go 는 Loader 인터페이스와 Web·PDF·DOCX 로더 구현체를 담는다.
// 표준 라이브러리와 외부 파싱 라이브러리(ledongthuc/pdf, nguyenthenguyen/docx, golang.org/x/net/html)만
// 사용하며, 모듈 내 상위 패키지를 import하지 않는다(SPEC §5.9).
package document

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	pdflib "github.com/ledongthuc/pdf"
	"github.com/nguyenthenguyen/docx"
	"golang.org/x/net/html"
)

// Loader 는 문서를 적재하는 계약 인터페이스다.
// Load 는 전체 문서를 한 번에 반환하고, LazyLoad 는 채널로 스트리밍한다.
type Loader interface {
	// Load 는 컨텍스트를 받아 전체 문서 목록을 반환한다.
	// 파싱 실패·경로 오류 등은 error 로 반환한다.
	Load(ctx context.Context) ([]Document, error)

	// LazyLoad 는 컨텍스트를 받아 문서를 채널로 스트리밍한다.
	// 셋업 에러(파일 열기 실패 등)는 반환값 error 로 즉시 반환하고,
	// 적재 중 에러는 채널을 닫아 종료한다(부분 결과까지 흘린다).
	LazyLoad(ctx context.Context) (<-chan Document, error)
}

// ────────────────────────────────────────────
// Web 로더
// ────────────────────────────────────────────

// webLoader 는 URL 목록에서 HTML 본문 텍스트를 추출하는 로더다.
type webLoader struct {
	urls   []string
	client *http.Client
}

// NewWebLoader 는 주어진 URL 목록에서 HTML 본문 텍스트를 추출하는 Loader 를 반환한다.
// URL 이 비어 있으면 Load/LazyLoad 는 빈 결과를 반환한다.
func NewWebLoader(urls []string) Loader {
	return &webLoader{
		urls:   urls,
		client: &http.Client{},
	}
}

// Load 는 모든 URL 의 HTML 본문을 순서대로 적재해 Document 목록을 반환한다.
func (w *webLoader) Load(ctx context.Context) ([]Document, error) {
	var docs []Document
	for _, u := range w.urls {
		doc, err := w.fetchOne(ctx, u)
		if err != nil {
			return nil, fmt.Errorf("web 로더: URL %q 적재 실패: %w", u, err)
		}
		docs = append(docs, doc)
	}
	return docs, nil
}

// LazyLoad 는 URL 목록을 순서대로 적재해 채널로 Document 를 흘린다.
// 셋업 에러(URL 목록 검증 등)는 없고, 개별 URL 실패는 채널 종료로 알린다.
func (w *webLoader) LazyLoad(ctx context.Context) (<-chan Document, error) {
	ch := make(chan Document)
	go func() {
		defer close(ch)
		for _, u := range w.urls {
			doc, err := w.fetchOne(ctx, u)
			if err != nil {
				// 적재 중 에러는 채널 종료로 알리고 부분 결과까지 흘린다.
				return
			}
			select {
			case <-ctx.Done():
				return
			case ch <- doc:
			}
		}
	}()
	return ch, nil
}

// fetchOne 은 단일 URL 에서 HTTP GET 으로 HTML 을 가져와 텍스트를 추출한다.
func (w *webLoader) fetchOne(ctx context.Context, url string) (Document, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Document{}, fmt.Errorf("HTTP 요청 생성 실패: %w", err)
	}

	resp, err := w.client.Do(req)
	if err != nil {
		return Document{}, fmt.Errorf("HTTP 요청 실패: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Document{}, fmt.Errorf("HTTP 상태 코드 오류: %d", resp.StatusCode)
	}

	text, err := extractHTMLText(resp.Body)
	if err != nil {
		return Document{}, fmt.Errorf("HTML 텍스트 추출 실패: %w", err)
	}
	if strings.TrimSpace(text) == "" {
		return Document{}, fmt.Errorf("URL %q 에서 추출된 텍스트가 비어 있음", url)
	}

	return Document{
		PageContent: text,
		Metadata:    map[string]any{"source": url},
	}, nil
}

// extractHTMLText 는 HTML 본문에서 텍스트 노드만 추출해 이어 붙인 문자열을 반환한다.
// script/style 태그 내부는 건너뛴다.
func extractHTMLText(r io.Reader) (string, error) {
	doc, err := html.Parse(r)
	if err != nil {
		return "", fmt.Errorf("HTML 파싱 실패: %w", err)
	}

	var sb strings.Builder
	var walkNode func(*html.Node)
	walkNode = func(n *html.Node) {
		// script/style 태그 내부 텍스트는 건너뛴다.
		if n.Type == html.ElementNode {
			tag := strings.ToLower(n.Data)
			if tag == "script" || tag == "style" {
				return
			}
		}
		if n.Type == html.TextNode {
			trimmed := strings.TrimSpace(n.Data)
			if trimmed != "" {
				sb.WriteString(trimmed)
				sb.WriteString(" ")
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walkNode(child)
		}
	}
	walkNode(doc)

	return strings.TrimSpace(sb.String()), nil
}

// ────────────────────────────────────────────
// PDF 로더
// ────────────────────────────────────────────

// pdfLoader 는 PDF 파일을 페이지별 Document 로 적재하는 로더다.
// 각 Document 의 Metadata 에 page(1-base), source(파일 경로), total_pages 가 채워진다.
type pdfLoader struct {
	path string
}

// NewPDFLoader 는 주어진 파일 경로의 PDF 를 페이지별로 적재하는 Loader 를 반환한다.
func NewPDFLoader(path string) Loader {
	return &pdfLoader{path: path}
}

// Load 는 PDF 의 모든 페이지를 Document 목록으로 반환한다.
func (p *pdfLoader) Load(ctx context.Context) ([]Document, error) {
	return loadPDFFromPath(ctx, p.path)
}

// LazyLoad 는 PDF 페이지를 순서대로 채널로 흘린다.
// 파일 열기 실패 등 셋업 에러는 반환값 error 로 즉시 반환한다.
func (p *pdfLoader) LazyLoad(ctx context.Context) (<-chan Document, error) {
	// 셋업 단계: 파일과 Reader 를 미리 열어 에러를 즉시 반환한다.
	f, r, err := pdflib.Open(p.path)
	if err != nil {
		return nil, fmt.Errorf("PDF 로더: 파일 열기 실패(%q): %w", p.path, err)
	}

	ch := make(chan Document)
	go func() {
		defer close(ch)
		defer f.Close()

		totalPages := r.NumPage()
		for i := 1; i <= totalPages; i++ {
			select {
			case <-ctx.Done():
				return
			default:
			}

			page := r.Page(i)
			// 빈 페이지는 건너뛴다.
			if page.V.IsNull() {
				continue
			}

			text, err := page.GetPlainText(nil)
			if err != nil {
				// 적재 중 에러는 채널 종료(부분 결과까지 흘림).
				return
			}
			if strings.TrimSpace(text) == "" {
				continue
			}

			doc := Document{
				PageContent: strings.TrimSpace(text),
				Metadata: map[string]any{
					"page":        i,
					"source":      p.path,
					"total_pages": totalPages,
				},
			}
			select {
			case <-ctx.Done():
				return
			case ch <- doc:
			}
		}
	}()
	return ch, nil
}

// loadPDFFromPath 는 경로의 PDF 파일을 열어 페이지별 Document 목록을 반환한다.
func loadPDFFromPath(ctx context.Context, path string) ([]Document, error) {
	f, r, err := pdflib.Open(path)
	if err != nil {
		return nil, fmt.Errorf("PDF 로더: 파일 열기 실패(%q): %w", path, err)
	}
	defer f.Close()

	totalPages := r.NumPage()
	var docs []Document
	for i := 1; i <= totalPages; i++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		page := r.Page(i)
		if page.V.IsNull() {
			continue
		}

		text, err := page.GetPlainText(nil)
		if err != nil {
			return nil, fmt.Errorf("PDF 로더: 페이지 %d 텍스트 추출 실패: %w", i, err)
		}
		if strings.TrimSpace(text) == "" {
			continue
		}

		docs = append(docs, Document{
			PageContent: strings.TrimSpace(text),
			Metadata: map[string]any{
				"page":        i,
				"source":      path,
				"total_pages": totalPages,
			},
		})
	}
	return docs, nil
}

// ReadPDFBytes 는 PDF 파일 바이트에서 전체 텍스트를 추출해 반환한다.
// 파일 경로 없이 바이트 슬라이스에서 직접 파싱한다.
func ReadPDFBytes(b []byte) (string, error) {
	r, err := pdflib.NewReader(strings.NewReader(string(b)), int64(len(b)))
	if err != nil {
		return "", fmt.Errorf("ReadPDFBytes: PDF Reader 생성 실패: %w", err)
	}

	totalPages := r.NumPage()
	var sb strings.Builder
	for i := 1; i <= totalPages; i++ {
		page := r.Page(i)
		if page.V.IsNull() {
			continue
		}
		text, err := page.GetPlainText(nil)
		if err != nil {
			return "", fmt.Errorf("ReadPDFBytes: 페이지 %d 텍스트 추출 실패: %w", i, err)
		}
		sb.WriteString(text)
	}
	return sb.String(), nil
}

// ────────────────────────────────────────────
// DOCX 로더
// ────────────────────────────────────────────

// docxLoader 는 DOCX 파일에서 텍스트를 추출하는 로더다.
type docxLoader struct {
	path string
}

// NewDocxLoader 는 주어진 경로의 DOCX 파일에서 텍스트를 추출하는 Loader 를 반환한다.
func NewDocxLoader(path string) Loader {
	return &docxLoader{path: path}
}

// Load 는 DOCX 파일 전체 텍스트를 단일 Document 로 반환한다.
func (d *docxLoader) Load(ctx context.Context) ([]Document, error) {
	text, err := extractDocxText(d.path)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("DOCX 로더: 파일 %q 에서 텍스트를 추출할 수 없음", d.path)
	}
	return []Document{
		{
			PageContent: strings.TrimSpace(text),
			Metadata:    map[string]any{"source": d.path},
		},
	}, nil
}

// LazyLoad 는 DOCX 파일을 열어 단일 Document 를 채널로 흘린다.
// 파일 열기 실패는 반환값 error 로 즉시 반환한다.
func (d *docxLoader) LazyLoad(ctx context.Context) (<-chan Document, error) {
	// 셋업 단계: 파일이 열리는지 확인하고 에러를 즉시 반환한다.
	text, err := extractDocxText(d.path)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("DOCX 로더: 파일 %q 에서 텍스트를 추출할 수 없음", d.path)
	}

	ch := make(chan Document, 1)
	go func() {
		defer close(ch)
		doc := Document{
			PageContent: strings.TrimSpace(text),
			Metadata:    map[string]any{"source": d.path},
		}
		select {
		case <-ctx.Done():
		case ch <- doc:
		}
	}()
	return ch, nil
}

// extractDocxText 는 DOCX 파일 경로에서 텍스트를 추출해 반환한다.
// nguyenthenguyen/docx 의 GetContent 는 XML 원문을 반환하므로 XML 태그를 제거한다.
func extractDocxText(path string) (string, error) {
	// 파일이 존재하는지 확인한다.
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("DOCX 로더: 파일 접근 실패(%q): %w", path, err)
	}

	r, err := docx.ReadDocxFile(path)
	if err != nil {
		return "", fmt.Errorf("DOCX 로더: 파일 파싱 실패(%q): %w", path, err)
	}
	defer r.Close()

	xmlContent := r.Editable().GetContent()
	text, err := stripXMLTags(xmlContent)
	if err != nil {
		return "", fmt.Errorf("DOCX 로더: XML 파싱 실패: %w", err)
	}
	return text, nil
}

// stripXMLTags 는 XML 문자열에서 태그를 제거하고 텍스트 노드만 이어 붙여 반환한다.
// <w:t> 텍스트 노드 사이의 단어 경계를 보존하기 위해 공백을 추가한다.
func stripXMLTags(xmlStr string) (string, error) {
	dec := xml.NewDecoder(strings.NewReader(xmlStr))
	var sb strings.Builder
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			// XML 이 손상됐을 때: 안전하게 빈 문자열 반환 대신 에러 전파.
			return "", fmt.Errorf("XML 토큰 파싱 실패: %w", err)
		}
		if t, ok := tok.(xml.CharData); ok {
			trimmed := strings.TrimSpace(string(t))
			if trimmed != "" {
				sb.WriteString(trimmed)
				sb.WriteString(" ")
			}
		}
	}
	return strings.TrimSpace(sb.String()), nil
}
