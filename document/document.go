// document 패키지는 문서 적재·분할 추상화를 담당한다.
// Document 값 타입, Loader 인터페이스, TextSplitter 인터페이스와 구현체가 여기에 있다.
// 모듈 내 상위 패키지를 import하지 않고 표준 라이브러리와 외부 파싱 라이브러리에만 의존한다(SPEC §5.9).
package document

// Document 는 단일 문서 조각을 나타내는 값 타입이다.
// PageContent 는 문서 본문 텍스트를 담고, Metadata 는 출처·페이지 번호 등 부가 정보를 담는다.
// 포인터가 아닌 값 타입으로 설계해 불변 복사를 명확하게 한다.
type Document struct {
	// PageContent 는 문서 본문 텍스트다.
	PageContent string
	// Metadata 는 문서의 부가 메타데이터다(출처·페이지 번호 등).
	// nil 허용이며, 분할 시 원본 Metadata 가 복사되어 청크에 전파된다.
	Metadata map[string]any
}
