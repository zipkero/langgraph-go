// splitter.go 는 TextSplitter 인터페이스와 재귀적 문자 분할기 구현체를 담는다.
// 표준 라이브러리만 사용하며 모듈 내 상위 패키지를 import하지 않는다(SPEC §5.9).
package document

import (
	"strings"
)

// TextSplitter 는 텍스트 또는 Document 목록을 청크로 분할하는 계약 인터페이스다.
type TextSplitter interface {
	// SplitText 는 텍스트를 청크 문자열 목록으로 분할한다.
	// 각 청크는 chunkSize 이하의 길이를 가지며, 인접 청크는 overlap 길이만큼 겹친다.
	SplitText(text string) []string

	// SplitDocuments 는 Document 목록의 각 PageContent 를 SplitText 로 분할하고,
	// 원본 Metadata 를 각 청크 Document 에 복사해 반환한다.
	SplitDocuments(docs []Document) []Document
}

// recursiveCharacterSplitter 는 재귀적 문자 분할기 구현체다.
// chunkSize 기준으로 텍스트를 청크화하며, overlap 길이만큼 인접 청크가 겹친다.
type recursiveCharacterSplitter struct {
	// chunkSize 는 청크의 최대 문자 수다.
	chunkSize int
	// overlap 은 인접 청크 사이에 겹치는 문자 수다.
	overlap int
	// separators 는 분할 시도 순서로 정렬된 구분자 목록이다.
	// 앞쪽 구분자부터 시도하며, 분할 결과가 chunkSize 에 맞으면 채택한다.
	separators []string
}

// NewRecursiveCharacterSplitter 는 재귀적 문자 분할기를 생성한다.
// chunkSize 는 청크 최대 길이(문자 수), overlap 은 인접 청크 경계 겹침 길이다.
// 기본 구분자 순서: 단락("\n\n") → 줄("\n") → 공백(" ") → 빈 문자열(문자 단위).
func NewRecursiveCharacterSplitter(chunkSize, overlap int) TextSplitter {
	return &recursiveCharacterSplitter{
		chunkSize: chunkSize,
		overlap:   overlap,
		separators: []string{
			"\n\n", // 단락 구분
			"\n",   // 줄 구분
			" ",    // 단어 구분
			"",     // 문자 단위(최후 수단)
		},
	}
}

// SplitText 는 텍스트를 chunkSize 기준으로 재귀적으로 분할하고 overlap 을 적용해 청크 목록을 반환한다.
func (s *recursiveCharacterSplitter) SplitText(text string) []string {
	return s.splitText(text, s.separators)
}

// SplitDocuments 는 각 Document 의 PageContent 를 SplitText 로 분할하고
// 원본 Metadata 를 각 청크 Document 에 복사해 반환한다.
func (s *recursiveCharacterSplitter) SplitDocuments(docs []Document) []Document {
	var result []Document
	for _, doc := range docs {
		chunks := s.SplitText(doc.PageContent)
		for _, chunk := range chunks {
			// 원본 Metadata 를 얕은 복사해 청크마다 독립적인 맵으로 전파한다.
			meta := copyMetadata(doc.Metadata)
			result = append(result, Document{
				PageContent: chunk,
				Metadata:    meta,
			})
		}
	}
	return result
}

// splitText 는 주어진 구분자 목록을 순서대로 시도해 텍스트를 재귀적으로 분할한다.
// 텍스트가 chunkSize 이하이면 그대로 반환한다.
func (s *recursiveCharacterSplitter) splitText(text string, separators []string) []string {
	// chunkSize 이하이면 더 이상 분할하지 않는다.
	if len([]rune(text)) <= s.chunkSize {
		if strings.TrimSpace(text) == "" {
			return nil
		}
		return []string{text}
	}

	// 구분자가 없으면(빈 문자열 구분자까지 소진) 강제 문자 단위 분할한다.
	if len(separators) == 0 {
		return s.splitByRune(text)
	}

	sep := separators[0]
	remaining := separators[1:]

	// 현재 구분자로 분할이 가능한지 확인한다.
	var splits []string
	if sep == "" {
		// 빈 구분자: 문자 단위 분할.
		return s.splitByRune(text)
	}

	if !strings.Contains(text, sep) {
		// 현재 구분자가 없으면 다음 구분자로 재귀한다.
		return s.splitText(text, remaining)
	}

	// 구분자로 텍스트를 나눈다.
	parts := strings.Split(text, sep)

	// 빈 조각을 제거하고 청크를 병합하면서 chunkSize 를 지킨다.
	splits = s.mergeSplits(parts, sep)

	// 여전히 chunkSize 를 초과하는 조각은 다음 구분자로 재귀 분할한다.
	var result []string
	for _, chunk := range splits {
		if len([]rune(chunk)) > s.chunkSize {
			result = append(result, s.splitText(chunk, remaining)...)
		} else if strings.TrimSpace(chunk) != "" {
			result = append(result, chunk)
		}
	}

	// overlap 을 적용해 인접 청크를 겹친다.
	return s.applyOverlap(result)
}

// mergeSplits 는 구분자로 나눈 조각들을 chunkSize 에 맞게 병합한다.
// 구분자를 다시 붙여 원문과 가깝게 복원하며, 단일 조각이 chunkSize 를 초과하면 그대로 통과시킨다.
func (s *recursiveCharacterSplitter) mergeSplits(parts []string, sep string) []string {
	var chunks []string
	current := ""

	for _, part := range parts {
		if part == "" {
			continue
		}

		// current 에 이 조각을 추가했을 때의 길이를 계산한다.
		candidate := part
		if current != "" {
			candidate = current + sep + part
		}

		if len([]rune(candidate)) <= s.chunkSize {
			current = candidate
		} else {
			// current 가 비어 있지 않으면 먼저 저장한다.
			if current != "" {
				chunks = append(chunks, current)
			}
			// 단일 조각이 chunkSize 를 초과해도 그대로 통과시켜 하위 구분자로 처리하게 한다.
			current = part
		}
	}

	if current != "" {
		chunks = append(chunks, current)
	}

	return chunks
}

// splitByRune 은 텍스트를 문자(rune) 단위로 강제 분할해 chunkSize·overlap 을 적용한다.
// 최후 수단으로 구분자 없이 순수 길이 기준으로 자른다.
func (s *recursiveCharacterSplitter) splitByRune(text string) []string {
	runes := []rune(text)
	if len(runes) == 0 {
		return nil
	}

	var chunks []string
	start := 0
	for start < len(runes) {
		end := start + s.chunkSize
		if end > len(runes) {
			end = len(runes)
		}
		chunks = append(chunks, string(runes[start:end]))
		// 다음 시작 위치: overlap 을 고려해 뒤로 overlap 만큼 겹친다.
		step := s.chunkSize - s.overlap
		if step <= 0 {
			step = 1 // 무한 루프 방지
		}
		start += step
	}
	return chunks
}

// applyOverlap 은 이미 분할된 청크 목록에 overlap 을 적용해 인접 청크가 겹치도록 재구성한다.
// splitByRune 이 아닌 경로(구분자 기반 분할 결과)에서 overlap 을 후처리로 적용한다.
// 각 청크의 끝에서 overlap 만큼을 다음 청크 앞에 붙여 준다.
func (s *recursiveCharacterSplitter) applyOverlap(chunks []string) []string {
	if s.overlap <= 0 || len(chunks) <= 1 {
		return chunks
	}

	result := make([]string, len(chunks))
	result[0] = chunks[0]

	for i := 1; i < len(chunks); i++ {
		prevRunes := []rune(chunks[i-1])
		// 이전 청크 끝의 overlap 길이만큼을 접두사로 붙인다.
		overlapStart := len(prevRunes) - s.overlap
		if overlapStart < 0 {
			overlapStart = 0
		}
		prefix := string(prevRunes[overlapStart:])
		result[i] = prefix + chunks[i]
	}

	return result
}

// copyMetadata 는 메타데이터 맵을 얕은 복사해 반환한다.
// nil 입력이면 nil 을 반환한다.
func copyMetadata(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	copied := make(map[string]any, len(m))
	for k, v := range m {
		copied[k] = v
	}
	return copied
}
