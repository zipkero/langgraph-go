// splitter_test.go 는 TextSplitter 인터페이스와 재귀적 문자 분할기의 단위 테스트를 담는다.
// 네트워크 없이 항상 실행되며, 분할·overlap·Metadata 전파를 검증한다.
package document

import (
	"strings"
	"testing"
)

// TestDocument_ValueType 은 Document 가 값 타입으로 동작하는지 확인한다.
func TestDocument_ValueType(t *testing.T) {
	original := Document{
		PageContent: "Hello",
		Metadata:    map[string]any{"source": "test"},
	}
	// 값 복사 후 원본 변경이 복사본에 영향을 주지 않아야 한다.
	copied := original
	copied.PageContent = "World"
	if original.PageContent != "Hello" {
		t.Errorf("Document 값 복사: 원본 PageContent 가 변경됨, got %q", original.PageContent)
	}
}

// TestSplitText_ShortText 는 텍스트가 chunkSize 이하이면 그대로 단일 청크로 반환되는지 확인한다.
func TestSplitText_ShortText(t *testing.T) {
	s := NewRecursiveCharacterSplitter(100, 0)
	text := "짧은 텍스트"
	chunks := s.SplitText(text)
	if len(chunks) != 1 {
		t.Fatalf("청크 수: want 1, got %d", len(chunks))
	}
	if chunks[0] != text {
		t.Errorf("청크 내용: want %q, got %q", text, chunks[0])
	}
}

// TestSplitText_SplitsLongText 는 긴 텍스트가 chunkSize 기준으로 복수 청크로 나뉘는지 확인한다.
func TestSplitText_SplitsLongText(t *testing.T) {
	chunkSize := 20
	s := NewRecursiveCharacterSplitter(chunkSize, 0)

	// 각 단어가 5자이고 구분자는 공백이므로 chunkSize=20 이면 여러 청크가 만들어진다.
	text := "hello world foo bar baz qux quux corge grault"
	chunks := s.SplitText(text)

	if len(chunks) < 2 {
		t.Fatalf("복수 청크 기대: got %d 청크 (%v)", len(chunks), chunks)
	}
	for i, chunk := range chunks {
		runeLen := len([]rune(chunk))
		if runeLen > chunkSize {
			t.Errorf("청크[%d] 길이 %d 가 chunkSize %d 초과: %q", i, runeLen, chunkSize, chunk)
		}
	}
}

// TestSplitText_Overlap 은 overlap 이 지정되면 인접 청크가 overlap 길이만큼 겹치는지 확인한다.
func TestSplitText_Overlap(t *testing.T) {
	chunkSize := 15
	overlap := 5
	s := NewRecursiveCharacterSplitter(chunkSize, overlap)

	// 충분히 긴 텍스트를 공백 구분자로 분할하면 overlap 이 적용되어야 한다.
	text := "AAAA BBBB CCCC DDDD EEEE FFFF GGGG HHHH"
	chunks := s.SplitText(text)

	if len(chunks) < 2 {
		t.Fatalf("overlap 검증을 위해 복수 청크 필요: got %d 청크 (%v)", len(chunks), chunks)
	}

	// 인접 청크 사이의 겹침을 확인한다: 이전 청크의 끝 부분이 다음 청크의 앞부분에 포함되어야 한다.
	for i := 1; i < len(chunks); i++ {
		prev := []rune(chunks[i-1])
		curr := chunks[i]

		// 이전 청크의 overlap 만큼을 접두사로 기대한다.
		overlapStart := len(prev) - overlap
		if overlapStart < 0 {
			overlapStart = 0
		}
		expectedPrefix := string(prev[overlapStart:])

		if !strings.HasPrefix(curr, expectedPrefix) {
			t.Errorf("청크[%d](%q)이 이전 청크[%d](%q)의 overlap 접두사 %q 로 시작하지 않음",
				i, curr, i-1, chunks[i-1], expectedPrefix)
		}
	}
}

// TestSplitText_ParagraphSeparator 는 단락("\n\n") 구분자로 먼저 분할을 시도하는지 확인한다.
// chunkSize 가 단락 하나 길이보다 작으면 단락 구분자로 쪼갠다.
func TestSplitText_ParagraphSeparator(t *testing.T) {
	// 각 단락은 약 10자이므로 chunkSize=15 로 설정하면 두 단락으로 분할된다.
	chunkSize := 15
	s := NewRecursiveCharacterSplitter(chunkSize, 0)

	// 전체 텍스트 길이 > chunkSize 이어야 분할이 일어난다.
	text := "first paragraph\n\nsecond paragraph"
	chunks := s.SplitText(text)

	if len(chunks) < 2 {
		t.Fatalf("단락 분할: want >=2 청크, got %d (%v)", len(chunks), chunks)
	}
}

// TestSplitText_ForcedRuneSplit 은 구분자가 없는 긴 단일 단어가 문자 단위로 강제 분할되는지 확인한다.
func TestSplitText_ForcedRuneSplit(t *testing.T) {
	chunkSize := 5
	overlap := 0
	s := NewRecursiveCharacterSplitter(chunkSize, overlap)

	// 공백·줄바꿈 없이 chunkSize 초과하는 단어.
	text := "ABCDEFGHIJ" // 10자
	chunks := s.SplitText(text)

	if len(chunks) != 2 {
		t.Fatalf("강제 문자 분할: want 2 청크, got %d (%v)", len(chunks), chunks)
	}
	if chunks[0] != "ABCDE" {
		t.Errorf("첫 청크: want %q, got %q", "ABCDE", chunks[0])
	}
	if chunks[1] != "FGHIJ" {
		t.Errorf("둘째 청크: want %q, got %q", "FGHIJ", chunks[1])
	}
}

// TestSplitDocuments_SplitsPageContent 는 SplitDocuments 가 각 Document 의 PageContent 를
// SplitText 로 분할해 복수 청크 Document 를 만드는지 확인한다.
func TestSplitDocuments_SplitsPageContent(t *testing.T) {
	chunkSize := 20
	s := NewRecursiveCharacterSplitter(chunkSize, 0)

	docs := []Document{
		{PageContent: "hello world foo bar baz qux quux corge grault", Metadata: map[string]any{"source": "file1"}},
	}
	result := s.SplitDocuments(docs)

	if len(result) < 2 {
		t.Fatalf("SplitDocuments: 복수 청크 기대, got %d", len(result))
	}
	for i, d := range result {
		if d.PageContent == "" {
			t.Errorf("청크[%d] PageContent 가 비어 있음", i)
		}
	}
}

// TestSplitDocuments_MetadataPropagation 은 원본 Metadata 가 각 청크 Document 에 복사되는지 확인한다.
func TestSplitDocuments_MetadataPropagation(t *testing.T) {
	chunkSize := 20
	s := NewRecursiveCharacterSplitter(chunkSize, 0)

	meta := map[string]any{
		"source": "doc.txt",
		"page":   1,
	}
	docs := []Document{
		{PageContent: "hello world foo bar baz qux quux corge grault", Metadata: meta},
	}
	result := s.SplitDocuments(docs)

	if len(result) < 2 {
		t.Fatalf("Metadata 전파 검증을 위해 복수 청크 필요: got %d", len(result))
	}

	for i, d := range result {
		// 원본과 동일한 값이어야 한다.
		if d.Metadata["source"] != "doc.txt" {
			t.Errorf("청크[%d] Metadata[source]: want %q, got %v", i, "doc.txt", d.Metadata["source"])
		}
		if d.Metadata["page"] != 1 {
			t.Errorf("청크[%d] Metadata[page]: want 1, got %v", i, d.Metadata["page"])
		}
	}
}

// TestSplitDocuments_MetadataIsolation 은 한 청크의 Metadata 를 수정해도 다른 청크와 원본에
// 영향을 주지 않는지 확인한다(얕은 복사 격리).
func TestSplitDocuments_MetadataIsolation(t *testing.T) {
	chunkSize := 20
	s := NewRecursiveCharacterSplitter(chunkSize, 0)

	original := map[string]any{"source": "original"}
	docs := []Document{
		{PageContent: "hello world foo bar baz qux quux corge grault", Metadata: original},
	}
	result := s.SplitDocuments(docs)

	if len(result) < 1 {
		t.Fatal("청크가 없음")
	}

	// 첫 번째 청크의 Metadata 를 변경한다.
	result[0].Metadata["source"] = "modified"

	// 원본 map 은 바뀌지 않아야 한다.
	if original["source"] != "original" {
		t.Errorf("원본 Metadata 가 변경됨: %v", original["source"])
	}
}

// TestSplitDocuments_NilMetadata 는 원본 Metadata 가 nil 이면 청크 Metadata 도 nil 인지 확인한다.
func TestSplitDocuments_NilMetadata(t *testing.T) {
	chunkSize := 20
	s := NewRecursiveCharacterSplitter(chunkSize, 0)

	docs := []Document{
		{PageContent: "hello world foo bar baz qux quux", Metadata: nil},
	}
	result := s.SplitDocuments(docs)

	for i, d := range result {
		if d.Metadata != nil {
			t.Errorf("청크[%d] Metadata: want nil, got %v", i, d.Metadata)
		}
	}
}

// TestSplitDocuments_MultipleDocuments 는 복수 Document 가 각각 분할되어 모두 결과에 포함되는지 확인한다.
func TestSplitDocuments_MultipleDocuments(t *testing.T) {
	chunkSize := 20
	s := NewRecursiveCharacterSplitter(chunkSize, 0)

	docs := []Document{
		{PageContent: "hello world foo bar baz qux quux", Metadata: map[string]any{"doc": "A"}},
		{PageContent: "one two three four five six seven eight nine", Metadata: map[string]any{"doc": "B"}},
	}
	result := s.SplitDocuments(docs)

	// 두 문서 모두 분할되었으므로 결과는 최소 2개 이상이어야 한다.
	if len(result) < 2 {
		t.Fatalf("복수 문서 분할: want >=2, got %d", len(result))
	}
}

// TestSplitText_EmptyString 은 빈 문자열 입력이 빈 결과를 반환하는지 확인한다.
func TestSplitText_EmptyString(t *testing.T) {
	s := NewRecursiveCharacterSplitter(20, 5)
	chunks := s.SplitText("")
	if len(chunks) != 0 {
		t.Errorf("빈 입력: want 0 청크, got %d (%v)", len(chunks), chunks)
	}
}
