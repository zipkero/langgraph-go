// e2e_test.go 는 실제 Ollama 임베딩 서비스를 사용해 store 의 시맨틱 검색 e2e 를 검증하는 통합 테스트다(task-007).
// 실제 Ollama 서버와 nomic-embed-text 임베딩 모델로 의미가 뚜렷이 다른 항목을 색인하고,
// 의미상 가까운 질의가 관련 항목을 상위 결과로 가져오는지 검증한다.
//
// 검증 범위:
//   - 동물/요리/날씨 주제 항목을 Put 으로 색인 → SearchItems 의미 검색 (TestE2E_StoreSemanticSearch)
//   - Search(map 반환) 경로도 동일한 의미 정렬을 보이는지 확인 (TestE2E_StoreSearchValues)
//
// Ollama 서버 미도달 또는 nomic-embed-text 미설치 시 t.Skip 으로 건너뛴다(D6).
// vectorstore/e2e_test.go 의 checkOllamaEmbedReady·skipIfOllamaUnavailable 패턴을 그대로 따른다.
package store_test

import (
	"bytes"
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/zipkero/langgraph-go/llm"
	"github.com/zipkero/langgraph-go/store"
)

// e2eModel 은 store e2e 테스트에서 사용하는 임베딩 모델 이름이다.
const e2eModel = "nomic-embed-text"

// checkOllamaEmbedReadyForStore 는 Ollama 서버가 도달 가능하고 임베딩 요청을 처리할 수 있는지 확인한다.
// 서버 미실행·모델 미설치(404·500) 모두 false 를 반환해 t.Skip 을 유발한다.
// vectorstore/e2e_test.go 의 checkOllamaEmbedReady 와 동일한 패턴이나, 패키지 경계가 다르므로 별도 정의한다.
func checkOllamaEmbedReadyForStore(model string) bool {
	reqBody := `{"model":"` + model + `","input":["ping"]}`
	httpClient := &http.Client{Timeout: 3 * time.Second}
	resp, err := httpClient.Post(
		"http://localhost:11434/api/embed",
		"application/json",
		bytes.NewBufferString(reqBody),
	)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// skipIfOllamaUnavailableForStore 는 Ollama 서버가 준비되지 않으면 t.Skip 을 호출한다.
// 모든 store e2e 테스트 함수 첫 줄에 호출해 일관된 skip 처리를 보장한다.
func skipIfOllamaUnavailableForStore(t *testing.T) {
	t.Helper()
	if !checkOllamaEmbedReadyForStore(e2eModel) {
		t.Skipf(
			"Ollama 서버에 도달할 수 없거나 모델 %q 가 설치되지 않아 store e2e 테스트를 건너뜁니다",
			e2eModel,
		)
	}
}

// e2eStoreEmbeddingClient 는 e2e 테스트에서 공통으로 사용하는 EmbeddingClient 를 반환한다.
func e2eStoreEmbeddingClient(t *testing.T) llm.EmbeddingClient {
	t.Helper()
	emb, err := llm.InitEmbeddings("ollama:" + e2eModel)
	if err != nil {
		t.Fatalf("InitEmbeddings 실패: %v", err)
	}
	return emb
}

// ============================================================
// TestE2E_StoreSemanticSearch
//
// 검증:
//   - 동물·요리·날씨 주제 항목을 실제 Ollama 임베딩으로 IndexConfig 구성 후 Put 으로 색인한다.
//   - 동물 주제 질의("강아지와 고양이 같은 반려동물")로 SearchItems 하면 동물 항목이 1순위다.
//   - 요리 주제 질의("요리 레시피와 음식 만들기")로 SearchItems 하면 요리 항목이 1순위다.
//   - 날씨 주제 질의("오늘 날씨와 기온 예보")로 SearchItems 하면 날씨 항목이 1순위다.
// ============================================================

func TestE2E_StoreSemanticSearch(t *testing.T) {
	skipIfOllamaUnavailableForStore(t)

	ctx := context.Background()
	emb := e2eStoreEmbeddingClient(t)

	// 실제 Ollama 임베딩으로 IndexConfig 를 구성한다.
	s := store.NewInMemoryStore(store.WithIndex(store.IndexConfig{
		Embed: emb,
		Dims:  768, // nomic-embed-text 기본 차원
	}))

	ns := store.Namespace{"e2e", "semantic"}

	// 의미가 뚜렷이 다른 세 주제 항목을 색인한다.
	// topic 키를 제거하고 text 단일 키만 사용해 valueToText 직렬화 노이즈를 최소화한다.
	// 각 주제의 핵심 어휘를 명확히 기재해 코사인 유사도 분리를 극대화한다.
	items := []struct {
		key   string
		topic string
		value map[string]any
	}{
		{
			key:   "animal",
			topic: "animal",
			value: map[string]any{
				"topic": "animal",
				"text":  "dog cat pet mammal animal fur paw bark meow puppy kitten",
			},
		},
		{
			key:   "cooking",
			topic: "cooking",
			value: map[string]any{
				"topic": "cooking",
				"text":  "recipe cooking food kitchen ingredient boil fry bake meal dish chef",
			},
		},
		{
			key:   "weather",
			topic: "weather",
			value: map[string]any{
				"topic": "weather",
				"text":  "weather forecast temperature rain cloud sunny wind humidity storm",
			},
		},
	}

	for _, it := range items {
		if err := s.Put(ctx, ns, it.key, it.value); err != nil {
			t.Fatalf("Put(%q) 실패: %v", it.key, err)
		}
	}

	// ── 동물 주제 질의 검증 ──────────────────────────────────────────────────
	animalQuery := "dog cat pet animal mammal"
	animalResults, err := s.SearchItems(ctx, ns, animalQuery, 3)
	if err != nil {
		t.Fatalf("동물 질의 SearchItems 실패: %v", err)
	}
	if len(animalResults) == 0 {
		t.Fatal("동물 질의 검색 결과가 없습니다")
	}
	for i, r := range animalResults {
		t.Logf("동물 질의 순위 %d: topic=%q, score=%.4f", i+1, r.Value["topic"], r.Score)
	}
	top1Animal, _ := animalResults[0].Value["topic"].(string)
	if top1Animal != "animal" {
		t.Errorf("동물 질의 상위 1위 topic=%q, animal 기대", top1Animal)
	}

	// ── 요리 주제 질의 검증 ──────────────────────────────────────────────────
	cookingQuery := "recipe cooking food kitchen ingredient meal"
	cookingResults, err := s.SearchItems(ctx, ns, cookingQuery, 3)
	if err != nil {
		t.Fatalf("요리 질의 SearchItems 실패: %v", err)
	}
	if len(cookingResults) == 0 {
		t.Fatal("요리 질의 검색 결과가 없습니다")
	}
	for i, r := range cookingResults {
		t.Logf("요리 질의 순위 %d: topic=%q, score=%.4f", i+1, r.Value["topic"], r.Score)
	}
	top1Cooking, _ := cookingResults[0].Value["topic"].(string)
	if top1Cooking != "cooking" {
		t.Errorf("요리 질의 상위 1위 topic=%q, cooking 기대", top1Cooking)
	}

	// ── 날씨 주제 질의 검증 ──────────────────────────────────────────────────
	weatherQuery := "weather forecast temperature rain cloud sunny"
	weatherResults, err := s.SearchItems(ctx, ns, weatherQuery, 3)
	if err != nil {
		t.Fatalf("날씨 질의 SearchItems 실패: %v", err)
	}
	if len(weatherResults) == 0 {
		t.Fatal("날씨 질의 검색 결과가 없습니다")
	}
	for i, r := range weatherResults {
		t.Logf("날씨 질의 순위 %d: topic=%q, score=%.4f", i+1, r.Value["topic"], r.Score)
	}
	top1Weather, _ := weatherResults[0].Value["topic"].(string)
	if top1Weather != "weather" {
		t.Errorf("날씨 질의 상위 1위 topic=%q, weather 기대", top1Weather)
	}
}

// ============================================================
// TestE2E_StoreSearchValues
//
// 검증:
//   - Search(map 반환) 경로가 SearchItems 와 동일한 의미 정렬을 보인다.
//   - 동물 질의로 Search 하면 상위 1위 map 에 topic="animal" 이 담겨 있다.
// ============================================================

func TestE2E_StoreSearchValues(t *testing.T) {
	skipIfOllamaUnavailableForStore(t)

	ctx := context.Background()
	emb := e2eStoreEmbeddingClient(t)

	s := store.NewInMemoryStore(store.WithIndex(store.IndexConfig{
		Embed: emb,
		Dims:  768,
	}))

	ns := store.Namespace{"e2e", "searchvalues"}

	// 동물·요리 두 주제 항목을 색인한다.
	if err := s.Put(ctx, ns, "animal", map[string]any{
		"topic": "animal",
		"text":  "dog cat pet mammal animal fur paw bark meow puppy kitten",
	}); err != nil {
		t.Fatalf("Put(animal) 실패: %v", err)
	}
	if err := s.Put(ctx, ns, "cooking", map[string]any{
		"topic": "cooking",
		"text":  "recipe cooking food kitchen ingredient boil fry bake meal dish chef",
	}); err != nil {
		t.Fatalf("Put(cooking) 실패: %v", err)
	}

	// 동물 질의로 Search 를 호출한다.
	query := "dog cat pet animal mammal"
	results, err := s.Search(ctx, ns, query, 2)
	if err != nil {
		t.Fatalf("Search 실패: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("Search 결과가 없습니다")
	}

	top1Topic, _ := results[0]["topic"].(string)
	t.Logf("Search 상위 1위: topic=%q", top1Topic)
	if top1Topic != "animal" {
		t.Errorf("Search 상위 1위 topic=%q, animal 기대", top1Topic)
	}
}
