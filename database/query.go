// query.go 는 SQL 조립을 담당하는 순수 함수를 담는다.
// DB 연결 없이 단위 테스트로 검증 가능하도록 문자열 조립 로직만 분리했다.
package database

import (
	"fmt"
	"sort"
	"strings"
)

// documentColumns 는 documents 테이블(및 match_documents RPC 반환)의 컬럼 목록이다.
// DocumentRecord 필드 순서와 일치한다.
var documentColumns = []string{"content", "embedding", "filename", "storage_ref", "chunk_index", "document_type"}

// webContentColumns 는 web_content 테이블의 컬럼 목록이다.
// WebContentRecord 필드 순서와 일치한다.
var webContentColumns = []string{"title", "url", "content", "expanded_query"}

// buildDocumentQuerySQL 은 q 를 SELECT ... FROM documents WHERE ... ORDER BY ... LIMIT ... 문자열과
// 위치 인자(파라미터) 목록으로 조립한다. 필터가 하나도 없으면 WHERE 절 없이 전체를 조회하는 SQL을 만든다.
// 맵 순회 순서에 의한 비결정성을 없애기 위해 Eq/ILike/Gte/Lte 각각을 키 이름 오름차순으로 정렬해 조립한다.
func buildDocumentQuerySQL(q DocumentQuery) (string, []any) {
	var sb strings.Builder
	sb.WriteString("SELECT ")
	sb.WriteString(strings.Join(documentColumns, ", "))
	sb.WriteString(" FROM documents")

	var conditions []string
	var args []any

	appendCond := func(keys []string, get func(string) any, op string) {
		for _, k := range keys {
			args = append(args, get(k))
			conditions = append(conditions, fmt.Sprintf("%s %s $%d", k, op, len(args)))
		}
	}

	appendCond(sortedKeys(q.Eq), func(k string) any { return q.Eq[k] }, "=")
	appendCond(sortedStringKeys(q.ILike), func(k string) any { return q.ILike[k] }, "ILIKE")
	appendCond(sortedKeys(q.Gte), func(k string) any { return q.Gte[k] }, ">=")
	appendCond(sortedKeys(q.Lte), func(k string) any { return q.Lte[k] }, "<=")

	if len(conditions) > 0 {
		sb.WriteString(" WHERE ")
		sb.WriteString(strings.Join(conditions, " AND "))
	}

	if q.OrderBy != "" {
		sb.WriteString(" ORDER BY ")
		sb.WriteString(q.OrderBy)
		if q.OrderDesc {
			sb.WriteString(" DESC")
		} else {
			sb.WriteString(" ASC")
		}
	}

	if q.Limit > 0 {
		sb.WriteString(fmt.Sprintf(" LIMIT %d", q.Limit))
	}

	return sb.String(), args
}

// sortedKeys 는 m 의 키를 오름차순으로 정렬해 반환한다(결정적 SQL 조립을 위함).
func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// sortedStringKeys 는 m(string 값 맵)의 키를 오름차순으로 정렬해 반환한다.
func sortedStringKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// matchDocumentsSQL 은 match_documents RPC 호출 SQL을 반환한다.
// 이 RPC는 (query_embedding, match_count) 시그니처로 필터 인자를 받지 않는다.
func matchDocumentsSQL() string {
	return "SELECT " + strings.Join(documentColumns, ", ") + " FROM match_documents($1, $2)"
}

// insertWebContentSQL 은 web_content 삽입 SQL을 반환한다.
func insertWebContentSQL() string {
	return fmt.Sprintf(
		"INSERT INTO web_content (%s) VALUES ($1, $2, $3, $4)",
		strings.Join(webContentColumns, ", "),
	)
}

// searchWebContentSQL 은 title 전문 검색(tsvector) SQL을 반환한다.
func searchWebContentSQL() string {
	return fmt.Sprintf(
		"SELECT %s FROM web_content WHERE to_tsvector('simple', title) @@ plainto_tsquery('simple', $1)",
		strings.Join(webContentColumns, ", "),
	)
}

// insertDocumentChunkSQL 은 documents 단건 삽입 SQL을 반환한다(배치는 이를 트랜잭션으로 반복 실행).
func insertDocumentChunkSQL() string {
	return fmt.Sprintf(
		"INSERT INTO documents (%s) VALUES ($1, $2, $3, $4, $5, $6)",
		strings.Join(documentColumns, ", "),
	)
}
