// vectorstore_internal_test.go 는 FromDocuments 의 옵션 배선을 검증하는 내부 테스트를 담는다(task-001).
// StoreOption 의 파라미터 타입 *storeOptions 가 비공개라 외부 vectorstore_test 패키지에서는
// StoreOption 클로저를 직접 작성할 수 없다(ANALYSIS §5 D1). 따라서 이 테스트는 package vectorstore
// 내부에서 비공개 storeOptions 를 다루는 StoreOption 을 직접 만들어, 전달된 옵션이 실제로 호출되고
// 그 반영이 생성 경로로 흘러드는 동일 인스턴스에 나타나는지를 관찰한다.
package vectorstore

import (
	"context"
	"testing"

	"github.com/zipkero/langgraph-go/document"
	"github.com/zipkero/langgraph-go/llm"
)

// noopEmbeddingClient 는 색인 경로 진입 여부만 확인하면 되는 테스트를 위한 최소 EmbeddingClient 구현체다.
// 입력 개수와 동일한 개수의 영벡터를 반환해 FromDocuments 가 정상 완료되도록 한다.
type noopEmbeddingClient struct{}

func (noopEmbeddingClient) Embed(_ context.Context, texts []string) ([][]float32, error) {
	result := make([][]float32, len(texts))
	for i := range texts {
		result[i] = []float32{0, 0, 0}
	}
	return result, nil
}

func (noopEmbeddingClient) EmbedQuery(_ context.Context, _ string) ([]float32, error) {
	return []float32{0, 0, 0}, nil
}

var _ llm.EmbeddingClient = noopEmbeddingClient{}

// TestFromDocuments_옵션이_단일_인스턴스에_누적_적용된다 는 FromDocuments 에 전달된 여러
// StoreOption 이 각자 독립된 임시 storeOptions 가 아니라 동일한 하나의 인스턴스에 적용되는지 검증한다.
// 각 옵션이 클로저 밖의 포인터에 자신이 받은 *storeOptions 주소를 기록해두고, 모든 옵션이
// 같은 주소를 받았는지 비교한다.
func TestFromDocuments_옵션이_단일_인스턴스에_누적_적용된다(t *testing.T) {
	var seen []*storeOptions
	record := func(o *storeOptions) {
		seen = append(seen, o)
	}

	opt1 := StoreOption(func(o *storeOptions) { record(o) })
	opt2 := StoreOption(func(o *storeOptions) { record(o) })

	docs := []document.Document{{PageContent: "hello"}}

	store, err := FromDocuments(context.Background(), docs, noopEmbeddingClient{}, opt1, opt2)
	if err != nil {
		t.Fatalf("FromDocuments 실패: %v", err)
	}
	if store == nil {
		t.Fatal("FromDocuments 가 nil Store 를 반환했습니다")
	}

	if len(seen) != 2 {
		t.Fatalf("옵션이 모두 호출되지 않았습니다: 호출 횟수=%d, want=2", len(seen))
	}
	if seen[0] != seen[1] {
		t.Errorf("각 옵션이 서로 다른 storeOptions 인스턴스를 받았습니다(버그: 매 반복 새 인스턴스 생성): %p != %p", seen[0], seen[1])
	}
}
