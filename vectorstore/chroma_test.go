// chroma_test.go 는 ChromaVectorStore 의 요청 생성·응답 변환을 httptest 서버로 네트워크 없이
// 검증한다(task-004). 실 Chroma 서버 왕복은 chroma_e2e_test.go 가 담당한다.
package vectorstore_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/zipkero/langgraph-go/document"
	"github.com/zipkero/langgraph-go/vectorstore"
)

// newChromaTestServer 는 collection get-or-create/add/query 3개 엔드포인트를 처리하는
// httptest 서버를 만든다. handleAdd/handleQuery 는 각 요청 본문을 검사·응답할 훅이다.
func newChromaTestServer(t *testing.T, collectionID string, handleAdd func(body map[string]any), handleQuery func(body map[string]any) map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("요청 본문 디코딩 실패: %v", err)
		}

		switch {
		case r.Method == http.MethodPost && hasSuffix(r.URL.Path, "/collections"):
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"id": collectionID})
		case r.Method == http.MethodPost && hasSuffix(r.URL.Path, "/collections/"+collectionID+"/add"):
			if handleAdd != nil {
				handleAdd(body)
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{})
		case r.Method == http.MethodPost && hasSuffix(r.URL.Path, "/collections/"+collectionID+"/query"):
			resp := map[string]any{}
			if handleQuery != nil {
				resp = handleQuery(body)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		default:
			t.Fatalf("예상치 못한 요청: %s %s", r.Method, r.URL.Path)
		}
	}))
}

// hasSuffix 는 s 가 suffix 로 끝나는지 확인한다(strings.HasSuffix 래핑, 지역 헬퍼).
func hasSuffix(s, suffix string) bool {
	if len(suffix) > len(s) {
		return false
	}
	return s[len(s)-len(suffix):] == suffix
}

// TestChromaVectorStore_Add_요청생성 은 Add 가 collection get-or-create 후 add 엔드포인트에
// ids/embeddings/documents/metadatas 를 문서 순서대로 담아 전송하는지 검증한다.
func TestChromaVectorStore_Add_요청생성(t *testing.T) {
	stub := makeStub(map[string][]float32{
		"문서1": {1, 0, 0},
		"문서2": {0, 1, 0},
	})

	var gotBody map[string]any
	server := newChromaTestServer(t, "coll-1", func(body map[string]any) {
		gotBody = body
	}, nil)
	defer server.Close()

	store := vectorstore.NewChromaVectorStore(server.URL, "test-collection", stub)
	docs := []document.Document{
		{PageContent: "문서1", Metadata: map[string]any{"topic": "a"}},
		{PageContent: "문서2", Metadata: map[string]any{"topic": "b"}},
	}

	if err := store.Add(context.Background(), docs); err != nil {
		t.Fatalf("Add 실패: %v", err)
	}

	if gotBody == nil {
		t.Fatal("add 엔드포인트가 호출되지 않았습니다")
	}

	ids, _ := gotBody["ids"].([]any)
	if len(ids) != 2 {
		t.Fatalf("ids 개수 = %d, want 2", len(ids))
	}
	if ids[0] == ids[1] {
		t.Errorf("ids가 서로 달라야 합니다: %v", ids)
	}

	docsField, _ := gotBody["documents"].([]any)
	if len(docsField) != 2 || docsField[0] != "문서1" || docsField[1] != "문서2" {
		t.Errorf("documents = %v, want [문서1 문서2] 순서 보존", docsField)
	}

	embeddings, _ := gotBody["embeddings"].([]any)
	if len(embeddings) != 2 {
		t.Fatalf("embeddings 개수 = %d, want 2", len(embeddings))
	}
	vec0, _ := embeddings[0].([]any)
	if len(vec0) != 3 || vec0[0].(float64) != 1 {
		t.Errorf("embeddings[0] = %v, want [1 0 0]", vec0)
	}

	metadatas, _ := gotBody["metadatas"].([]any)
	if len(metadatas) != 2 {
		t.Fatalf("metadatas 개수 = %d, want 2", len(metadatas))
	}
	meta0, _ := metadatas[0].(map[string]any)
	if meta0["topic"] != "a" {
		t.Errorf("metadatas[0] = %v, want topic=a", meta0)
	}
}

// TestChromaVectorStore_SimilaritySearch_응답변환 은 query 엔드포인트 응답(documents/metadatas
// 배치 0번)을 document.Document 목록으로 정확히 변환하는지 검증한다.
func TestChromaVectorStore_SimilaritySearch_응답변환(t *testing.T) {
	stub := makeStub(map[string][]float32{"질의": {1, 0, 0}})

	var gotQueryBody map[string]any
	server := newChromaTestServer(t, "coll-1", nil, func(body map[string]any) map[string]any {
		gotQueryBody = body
		return map[string]any{
			"documents": [][]string{{"결과1", "결과2"}},
			"metadatas": [][]map[string]any{{
				{"topic": "x"},
				{"topic": "y"},
			}},
		}
	})
	defer server.Close()

	store := vectorstore.NewChromaVectorStore(server.URL, "test-collection", stub)
	results, err := store.SimilaritySearch(context.Background(), "질의", vectorstore.SearchOptions{K: 2})
	if err != nil {
		t.Fatalf("SimilaritySearch 실패: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("결과 개수 = %d, want 2", len(results))
	}
	if results[0].PageContent != "결과1" || results[0].Metadata["topic"] != "x" {
		t.Errorf("results[0] = %+v, want PageContent=결과1 topic=x", results[0])
	}
	if results[1].PageContent != "결과2" || results[1].Metadata["topic"] != "y" {
		t.Errorf("results[1] = %+v, want PageContent=결과2 topic=y", results[1])
	}

	if nResults, _ := gotQueryBody["n_results"].(float64); int(nResults) != 2 {
		t.Errorf("n_results = %v, want opts.K=2", gotQueryBody["n_results"])
	}
}

// TestChromaVectorStore_AsRetriever_위임 은 AsRetriever 가 반환한 Retriever.Invoke 가
// 고정된 SearchOptions 로 SimilaritySearch 와 동일한 query 요청을 수행하는지 검증한다.
func TestChromaVectorStore_AsRetriever_위임(t *testing.T) {
	stub := makeStub(map[string][]float32{"질의": {1, 0, 0}})

	server := newChromaTestServer(t, "coll-1", nil, func(_ map[string]any) map[string]any {
		return map[string]any{
			"documents": [][]string{{"결과1"}},
			"metadatas": [][]map[string]any{{{}}},
		}
	})
	defer server.Close()

	store := vectorstore.NewChromaVectorStore(server.URL, "test-collection", stub)
	retriever := store.AsRetriever(vectorstore.SearchOptions{K: 1})

	results, err := retriever.Invoke(context.Background(), "질의")
	if err != nil {
		t.Fatalf("Invoke 실패: %v", err)
	}
	if len(results) != 1 || results[0].PageContent != "결과1" {
		t.Errorf("Invoke 결과 = %+v, want [결과1]", results)
	}
}

// TestChromaVectorStore_서버에러_error반환 은 Chroma 서버가 비정상 상태코드를 반환하면
// Add/SimilaritySearch 가 error 를 반환하는지 검증한다(Ollama 임베딩 클라이언트와 동일한 에러 계약).
func TestChromaVectorStore_서버에러_error반환(t *testing.T) {
	stub := makeStub(map[string][]float32{"문서": {1, 0, 0}, "질의": {1, 0, 0}})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("서버 오류"))
	}))
	defer server.Close()

	store := vectorstore.NewChromaVectorStore(server.URL, "test-collection", stub)

	if err := store.Add(context.Background(), []document.Document{{PageContent: "문서"}}); err == nil {
		t.Error("Add 가 서버 500 응답에 대해 에러를 반환하지 않았습니다")
	}

	if _, err := store.SimilaritySearch(context.Background(), "질의", vectorstore.SearchOptions{K: 1}); err == nil {
		t.Error("SimilaritySearch 가 서버 500 응답에 대해 에러를 반환하지 않았습니다")
	}
}

// TestChromaVectorStore_연결실패_error반환 은 서버 자체가 없을 때(연결 실패) 생성자는 성공하고
// Add/SimilaritySearch 호출이 error 를 반환하는지 검증한다(SPEC §3 — 서버 미기동 시 나머지 무결).
func TestChromaVectorStore_연결실패_error반환(t *testing.T) {
	stub := makeStub(nil)
	// 존재하지 않는 로컬 포트를 가리켜 연결을 실패시킨다.
	store := vectorstore.NewChromaVectorStore("http://127.0.0.1:1", "test-collection", stub)

	if err := store.Add(context.Background(), []document.Document{{PageContent: "문서"}}); err == nil {
		t.Error("Add 가 연결 실패에 대해 에러를 반환하지 않았습니다")
	}
}
