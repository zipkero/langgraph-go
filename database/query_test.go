// query_test.go 는 buildDocumentQuerySQL 등 순수 SQL 조립 로직의 단위 테스트다.
// DB 연결이 필요 없어 크리덴셜 유무와 무관하게 항상 실행된다.
package database

import (
	"strings"
	"testing"
)

// TestBuildDocumentQuerySQL_필터없음 은 필터가 전혀 없을 때 WHERE 절 없는 SQL을 조립하는지 검증한다.
func TestBuildDocumentQuerySQL_필터없음(t *testing.T) {
	sql, args := buildDocumentQuerySQL(DocumentQuery{})

	if strings.Contains(sql, "WHERE") {
		t.Errorf("필터가 없는데 WHERE 절이 포함됨: %s", sql)
	}
	if len(args) != 0 {
		t.Errorf("필터가 없는데 인자가 있음: %v", args)
	}
	if !strings.HasPrefix(sql, "SELECT content, embedding, filename, storage_ref, chunk_index, document_type FROM documents") {
		t.Errorf("SELECT 절이 예상과 다름: %s", sql)
	}
}

// TestBuildDocumentQuerySQL_eq필터 는 Eq 필터가 등호 조건과 위치 인자로 조립되는지 검증한다.
func TestBuildDocumentQuerySQL_eq필터(t *testing.T) {
	sql, args := buildDocumentQuerySQL(DocumentQuery{
		Eq: map[string]any{"document_type": "pdf"},
	})

	if !strings.Contains(sql, "document_type = $1") {
		t.Errorf("eq 조건이 SQL에 없음: %s", sql)
	}
	if len(args) != 1 || args[0] != "pdf" {
		t.Errorf("인자가 예상과 다름: %v", args)
	}
}

// TestBuildDocumentQuerySQL_전체조합 은 eq/ilike/gte/lte/order/limit 을 모두 조합했을 때
// 결정적 순서(키 오름차순)로 조립되고 위치 인자 번호가 순서대로 매겨지는지 검증한다.
func TestBuildDocumentQuerySQL_전체조합(t *testing.T) {
	q := DocumentQuery{
		Eq:        map[string]any{"document_type": "pdf"},
		ILike:     map[string]string{"filename": "%report%"},
		Gte:       map[string]any{"chunk_index": 0},
		Lte:       map[string]any{"chunk_index": 10},
		OrderBy:   "chunk_index",
		OrderDesc: true,
		Limit:     5,
	}
	sql, args := buildDocumentQuerySQL(q)

	wantConds := []string{
		"document_type = $1",
		"filename ILIKE $2",
		"chunk_index >= $3",
		"chunk_index <= $4",
	}
	for _, c := range wantConds {
		if !strings.Contains(sql, c) {
			t.Errorf("조건 %q 가 SQL에 없음: %s", c, sql)
		}
	}
	if !strings.Contains(sql, "ORDER BY chunk_index DESC") {
		t.Errorf("ORDER BY 절이 예상과 다름: %s", sql)
	}
	if !strings.Contains(sql, "LIMIT 5") {
		t.Errorf("LIMIT 절이 예상과 다름: %s", sql)
	}
	wantArgs := []any{"pdf", "%report%", 0, 10}
	if len(args) != len(wantArgs) {
		t.Fatalf("인자 개수가 다름: got %v, want %v", args, wantArgs)
	}
	for i, w := range wantArgs {
		if args[i] != w {
			t.Errorf("인자[%d] = %v, want %v", i, args[i], w)
		}
	}
}

// TestBuildDocumentQuerySQL_결정적순서 는 맵 키가 여러 개일 때 항상 같은 SQL이 조립되는지(비결정성 없음) 검증한다.
func TestBuildDocumentQuerySQL_결정적순서(t *testing.T) {
	q := DocumentQuery{
		Eq: map[string]any{"z_col": 1, "a_col": 2, "m_col": 3},
	}
	sql1, _ := buildDocumentQuerySQL(q)
	for i := 0; i < 20; i++ {
		sql2, _ := buildDocumentQuerySQL(q)
		if sql1 != sql2 {
			t.Fatalf("반복 조립 결과가 다름(비결정적): %q vs %q", sql1, sql2)
		}
	}
	if !strings.Contains(sql1, "a_col = $1") || !strings.Contains(sql1, "m_col = $2") || !strings.Contains(sql1, "z_col = $3") {
		t.Errorf("키 오름차순 조립이 아님: %s", sql1)
	}
}

// TestMatchDocumentsSQL 은 match_documents RPC 호출 SQL이 두 위치 인자를 사용하는지 검증한다.
func TestMatchDocumentsSQL(t *testing.T) {
	sql := matchDocumentsSQL()
	if !strings.Contains(sql, "match_documents($1, $2)") {
		t.Errorf("match_documents 호출 형태가 예상과 다름: %s", sql)
	}
}

// TestSearchWebContentSQL 은 title tsvector 전문 검색 SQL 형태를 검증한다.
func TestSearchWebContentSQL(t *testing.T) {
	sql := searchWebContentSQL()
	if !strings.Contains(sql, "to_tsvector('simple', title)") || !strings.Contains(sql, "plainto_tsquery('simple', $1)") {
		t.Errorf("전문 검색 SQL 형태가 예상과 다름: %s", sql)
	}
}
