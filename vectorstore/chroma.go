// chroma.go 는 외부 Chroma 서버(chromadb 1.x)를 백엔드로 쓰는 ChromaVectorStore 를 담는다.
// SDK 의존 없이 v2 REST API(heartbeat/collection get-or-create/add/query)를 표준 net/http 로
// 직접 호출한다(ANALYSIS §5 D-c). tenant/database 는 기본값(default_tenant/default_database)을 쓴다.
package vectorstore

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/zipkero/langgraph-go/document"
	"github.com/zipkero/langgraph-go/llm"
)

// chromaDefaultTenant/chromaDefaultDatabase 는 chromadb 1.x v2 API 의 고정 tenant/database 값이다.
const (
	chromaDefaultTenant   = "default_tenant"
	chromaDefaultDatabase = "default_database"
)

// ChromaVectorStore 는 외부 Chroma 서버에 위임해 문서 색인·유사도 검색을 수행하는 Store 구현체다.
// collection 조회·생성은 최초 Add/SimilaritySearch 호출 시 지연 수행되므로, 생성자 자체는 네트워크를
// 타지 않아 서버 미기동 환경에서도 빌드·나머지 테스트에 영향을 주지 않는다(SPEC §3, ANALYSIS §2.3).
type ChromaVectorStore struct {
	// baseURL 은 Chroma 서버의 베이스 URL(예: http://localhost:8000)이다.
	baseURL string
	// collectionName 은 get-or-create 로 조회·생성할 collection 이름이다.
	collectionName string
	// emb 는 문서·질의 임베딩 생성에 사용할 클라이언트다.
	emb llm.EmbeddingClient
	// httpClient 는 HTTP 호출에 사용할 클라이언트다.
	httpClient *http.Client

	// mu 는 collectionID 의 지연 초기화를 보호한다.
	mu sync.Mutex
	// collectionID 는 get-or-create 로 확정된 collection UUID다. 최초 호출 전에는 비어 있다.
	collectionID string
}

// NewChromaVectorStore 는 baseURL 의 Chroma 서버와 collectionName 컬렉션을 대상으로 하는
// ChromaVectorStore 를 생성한다. collection get-or-create 는 지연 초기화하므로 이 호출은
// 네트워크를 타지 않는다.
func NewChromaVectorStore(baseURL, collectionName string, emb llm.EmbeddingClient) *ChromaVectorStore {
	return &ChromaVectorStore{
		baseURL:        strings.TrimRight(baseURL, "/"),
		collectionName: collectionName,
		emb:            emb,
		httpClient:     &http.Client{},
	}
}

// Add 는 docs 를 emb.Embed 로 클라이언트 측 임베딩한 뒤 Chroma add 엔드포인트로 색인한다.
// 빈 docs 는 아무 작업도 하지 않고 nil 을 반환한다.
func (s *ChromaVectorStore) Add(ctx context.Context, docs []document.Document) error {
	if len(docs) == 0 {
		return nil
	}

	collectionID, err := s.ensureCollection(ctx)
	if err != nil {
		return err
	}

	texts := make([]string, len(docs))
	for i, doc := range docs {
		texts[i] = doc.PageContent
	}

	vectors, err := s.emb.Embed(ctx, texts)
	if err != nil {
		return err
	}
	if len(vectors) != len(docs) {
		return errorMismatch(len(docs), len(vectors))
	}

	ids, err := chromaGenerateIDs(len(docs))
	if err != nil {
		return fmt.Errorf("vectorstore: chroma ID 생성 실패: %w", err)
	}

	metadatas := make([]map[string]any, len(docs))
	for i, doc := range docs {
		metadatas[i] = doc.Metadata
	}

	reqBody := chromaAddRequest{
		IDs:        ids,
		Embeddings: vectors,
		Documents:  texts,
		Metadatas:  metadatas,
	}
	return s.callAddAPI(ctx, collectionID, reqBody)
}

// SimilaritySearch 는 query 를 emb.EmbedQuery 로 임베딩한 뒤 Chroma query 엔드포인트로
// 유사도 검색을 실행하고 결과를 document.Document 로 변환해 반환한다.
// opts.Filter 가 지정되면 Chroma where 조건으로 전달된다.
func (s *ChromaVectorStore) SimilaritySearch(ctx context.Context, query string, opts SearchOptions) ([]document.Document, error) {
	collectionID, err := s.ensureCollection(ctx)
	if err != nil {
		return nil, err
	}

	queryVec, err := s.emb.EmbedQuery(ctx, query)
	if err != nil {
		return nil, err
	}

	// n_results 는 Chroma 서버가 요구하는 필수 값이다. K<=0(InMemoryStore 의 "전체 반환" 의미)은
	// Chroma 에 대응 개념이 없으므로 Chroma 자체 기본값(10)으로 대체한다.
	nResults := opts.K
	if nResults <= 0 {
		nResults = 10
	}

	reqBody := chromaQueryRequest{
		QueryEmbeddings: [][]float32{queryVec},
		NResults:        nResults,
	}
	if len(opts.Filter) > 0 {
		reqBody.Where = opts.Filter
	}

	resp, err := s.callQueryAPI(ctx, collectionID, reqBody)
	if err != nil {
		return nil, err
	}

	if len(resp.Documents) == 0 || len(resp.Documents[0]) == 0 {
		return []document.Document{}, nil
	}

	batch := resp.Documents[0]
	result := make([]document.Document, len(batch))
	for i, content := range batch {
		var metadata map[string]any
		if len(resp.Metadatas) > 0 && i < len(resp.Metadatas[0]) {
			metadata = resp.Metadatas[0][i]
		}
		result[i] = document.Document{
			PageContent: content,
			Metadata:    metadata,
		}
	}
	return result, nil
}

// AsRetriever 는 opts 를 고정한 Retriever 를 반환한다.
// 반환된 Retriever.Invoke 는 같은 SimilaritySearch 경로로 위임한다(InMemory·Supabase retriever와 동일 구조).
func (s *ChromaVectorStore) AsRetriever(opts SearchOptions) Retriever {
	return &chromaRetriever{
		store: s,
		opts:  opts,
	}
}

// chromaRetriever 는 ChromaVectorStore 를 고정된 SearchOptions 로 감싼 Retriever 구현체다.
type chromaRetriever struct {
	// store 는 검색을 위임할 ChromaVectorStore 다.
	store *ChromaVectorStore
	// opts 는 고정된 검색 옵션이다.
	opts SearchOptions
}

// Invoke 는 고정된 SearchOptions 로 SimilaritySearch 를 실행하고 결과 Document 목록을 반환한다.
func (r *chromaRetriever) Invoke(ctx context.Context, query string) ([]document.Document, error) {
	return r.store.SimilaritySearch(ctx, query, r.opts)
}

// ensureCollection 은 collectionID 를 지연 초기화한다. 이미 확정돼 있으면 네트워크 호출 없이
// 캐시된 값을 반환한다.
func (s *ChromaVectorStore) ensureCollection(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.collectionID != "" {
		return s.collectionID, nil
	}

	id, err := s.getOrCreateCollection(ctx)
	if err != nil {
		return "", err
	}
	s.collectionID = id
	return id, nil
}

// chromaCollectionRequest 는 collection get-or-create 엔드포인트 요청 본문이다.
type chromaCollectionRequest struct {
	// Name 은 조회·생성할 collection 이름이다.
	Name string `json:"name"`
	// GetOrCreate 는 true 면 동명 collection 이 있을 때 생성 대신 조회한다.
	GetOrCreate bool `json:"get_or_create"`
}

// chromaCollectionResponse 는 collection get-or-create 엔드포인트 응답 본문이다.
type chromaCollectionResponse struct {
	// ID 는 이후 add/query 호출에 쓰는 collection UUID다.
	ID string `json:"id"`
}

// getOrCreateCollection 은 Chroma collection get-or-create 엔드포인트를 호출해 collection UUID 를 반환한다.
func (s *ChromaVectorStore) getOrCreateCollection(ctx context.Context) (string, error) {
	reqBody := chromaCollectionRequest{
		Name:        s.collectionName,
		GetOrCreate: true,
	}

	var resp chromaCollectionResponse
	url := s.baseURL + "/api/v2/tenants/" + chromaDefaultTenant + "/databases/" + chromaDefaultDatabase + "/collections"
	if err := s.postJSON(ctx, url, reqBody, &resp); err != nil {
		return "", fmt.Errorf("vectorstore: chroma collection get-or-create 실패: %w", err)
	}
	if resp.ID == "" {
		return "", fmt.Errorf("vectorstore: chroma collection get-or-create 응답에 id 가 없습니다")
	}
	return resp.ID, nil
}

// chromaAddRequest 는 add 엔드포인트 요청 본문이다.
type chromaAddRequest struct {
	// IDs 는 문서별 고유 ID 목록이다(Documents/Embeddings/Metadatas 와 순서 대응).
	IDs []string `json:"ids"`
	// Embeddings 는 문서별 임베딩 벡터 목록이다.
	Embeddings [][]float32 `json:"embeddings"`
	// Documents 는 문서 원문(PageContent) 목록이다.
	Documents []string `json:"documents"`
	// Metadatas 는 문서별 메타데이터 목록이다.
	Metadatas []map[string]any `json:"metadatas"`
}

// callAddAPI 는 add 엔드포인트를 호출해 문서를 collection 에 색인한다.
func (s *ChromaVectorStore) callAddAPI(ctx context.Context, collectionID string, reqBody chromaAddRequest) error {
	url := s.baseURL + "/api/v2/tenants/" + chromaDefaultTenant + "/databases/" + chromaDefaultDatabase +
		"/collections/" + collectionID + "/add"
	if err := s.postJSON(ctx, url, reqBody, nil); err != nil {
		return fmt.Errorf("vectorstore: chroma add 실패: %w", err)
	}
	return nil
}

// chromaQueryRequest 는 query 엔드포인트 요청 본문이다.
type chromaQueryRequest struct {
	// QueryEmbeddings 는 질의 벡터 배치다(여기서는 항상 원소 1개).
	QueryEmbeddings [][]float32 `json:"query_embeddings"`
	// NResults 는 반환받을 최대 결과 수다.
	NResults int `json:"n_results"`
	// Where 는 메타데이터 필터 조건이다. 비어 있으면 필드 자체를 생략한다.
	Where map[string]any `json:"where,omitempty"`
}

// chromaQueryResponse 는 query 엔드포인트 응답 본문이다.
// 각 필드는 QueryEmbeddings 배치 단위([][]형태)이며, 이 파일에서는 배치 0번만 사용한다.
type chromaQueryResponse struct {
	// Documents 는 질의별 결과 문서 원문이다.
	Documents [][]string `json:"documents"`
	// Metadatas 는 질의별 결과 메타데이터다.
	Metadatas [][]map[string]any `json:"metadatas"`
}

// callQueryAPI 는 query 엔드포인트를 호출해 유사도 검색 결과를 반환한다.
func (s *ChromaVectorStore) callQueryAPI(ctx context.Context, collectionID string, reqBody chromaQueryRequest) (chromaQueryResponse, error) {
	url := s.baseURL + "/api/v2/tenants/" + chromaDefaultTenant + "/databases/" + chromaDefaultDatabase +
		"/collections/" + collectionID + "/query"
	var resp chromaQueryResponse
	if err := s.postJSON(ctx, url, reqBody, &resp); err != nil {
		return chromaQueryResponse{}, fmt.Errorf("vectorstore: chroma query 실패: %w", err)
	}
	return resp, nil
}

// postJSON 은 reqBody 를 JSON 으로 직렬화해 url 에 POST 하고, out 이 nil 이 아니면 응답 본문을
// out 에 디코딩한다. 연결 실패·비정상 상태코드는 error 로 반환한다(Ollama 임베딩 클라이언트와 동일 패턴).
func (s *ChromaVectorStore) postJSON(ctx context.Context, url string, reqBody, out any) error {
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("요청 직렬화 실패: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("요청 생성 실패: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("서버 연결 실패: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("비정상 상태코드 %d: %s", resp.StatusCode, string(body))
	}

	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("응답 파싱 실패: %w", err)
	}
	return nil
}

// chromaGenerateIDs 는 add 요청에 쓸 고유 ID n개를 생성한다.
// 문서 자체에 ID 개념이 없으므로(document.Document) crypto/rand 기반 16바이트 임의값을 16진 문자열로 쓴다.
func chromaGenerateIDs(n int) ([]string, error) {
	ids := make([]string, n)
	buf := make([]byte, 16)
	for i := range n {
		if _, err := rand.Read(buf); err != nil {
			return nil, err
		}
		ids[i] = hex.EncodeToString(buf)
	}
	return ids, nil
}
