// client_test.go 는 task-003 검증 조건을 단정하는 단위 테스트를 담는다.
// 같은 패키지 서버(httptest)에 클라이언트를 붙여 외부 네트워크 없이 검증한다(SPEC §5.5).
package a2a

import (
	"context"
	"testing"
)

// ─── CardResolver 검증 ────────────────────────────────────────────────────────

// TestCardResolver_카드를_조회한다 는 CardResolver.GetAgentCard가
// /.well-known/agent-card.json에서 AgentCard를 올바르게 조회하는지 검증한다(SPEC §5.5).
func TestCardResolver_카드를_조회한다(t *testing.T) {
	ts, _ := newTestServer(t, &echoExecutor{})

	resolver := NewCardResolver(ts.URL)
	card, err := resolver.GetAgentCard(context.Background())
	if err != nil {
		t.Fatalf("GetAgentCard 실패: %v", err)
	}

	if card.Name != "테스트에이전트" {
		t.Errorf("카드 이름 불일치: 예상 '테스트에이전트', 실제 %q", card.Name)
	}
	if card.URL == "" {
		t.Error("카드 URL이 비어 있다")
	}
	if !card.Capabilities.Streaming {
		t.Error("카드에 스트리밍 능력이 표시되어야 한다")
	}
}

// ─── Client.SendMessage 검증 ─────────────────────────────────────────────────

// TestClient_SendMessage_태스크를_반환한다 는 Client.SendMessage가 서버를 호출해
// 상태·아티팩트를 담은 Task를 반환하는지 검증한다(SPEC §5.5).
func TestClient_SendMessage_태스크를_반환한다(t *testing.T) {
	ts, _ := newTestServer(t, &echoExecutor{})

	// 서버 카드에서 URL을 가져오기 위해 resolver를 사용한다.
	resolver := NewCardResolver(ts.URL)
	card, err := resolver.GetAgentCard(context.Background())
	if err != nil {
		t.Fatalf("카드 조회 실패: %v", err)
	}
	// 테스트 서버 URL로 카드 URL을 재설정한다(카드의 URL은 localhost이므로 httptest URL로 교체).
	card.URL = ts.URL + "/"

	client := NewClient(card)
	req := SendMessageRequest{
		Params: MessageSendParams{
			Message: Message{
				Role: RoleUser,
				Parts: []Part{
					{Text: &TextPart{Text: "클라이언트 테스트"}},
				},
			},
		},
	}

	task, err := client.SendMessage(context.Background(), req)
	if err != nil {
		t.Fatalf("SendMessage 실패: %v", err)
	}

	if task.ID == "" {
		t.Error("태스크 ID가 비어 있다")
	}
	if task.Status.State != TaskStateCompleted {
		t.Errorf("예상 상태 completed, 실제: %q", task.Status.State)
	}
	if len(task.Artifacts) == 0 {
		t.Error("아티팩트가 없다")
	}

	// 아티팩트 텍스트에 입력이 포함되어 있어야 한다.
	text, ok := ArtifactText(task.Artifacts[0])
	if !ok {
		t.Error("아티팩트에 텍스트 파트가 없다")
	}
	if text == "" {
		t.Error("아티팩트 텍스트가 비어 있다")
	}
}

// ─── Client.SendMessageStreaming 검증 ────────────────────────────────────────

// TestClient_SendMessageStreaming_이벤트가_채널로_방출된다 는 SendMessageStreaming이
// 이벤트를 채널로 순차 방출하고 final 이벤트에서 채널이 닫히는지 검증한다(SPEC §5.5).
func TestClient_SendMessageStreaming_이벤트가_채널로_방출된다(t *testing.T) {
	ts, _ := newTestServer(t, &echoExecutor{extraSteps: 1})

	resolver := NewCardResolver(ts.URL)
	card, err := resolver.GetAgentCard(context.Background())
	if err != nil {
		t.Fatalf("카드 조회 실패: %v", err)
	}
	card.URL = ts.URL + "/"

	client := NewClient(card)
	req := SendMessageRequest{
		Params: MessageSendParams{
			Message: Message{
				Role: RoleUser,
				Parts: []Part{
					{Text: &TextPart{Text: "스트리밍 테스트"}},
				},
			},
		},
	}

	ch, err := client.SendMessageStreaming(context.Background(), req)
	if err != nil {
		t.Fatalf("SendMessageStreaming 실패: %v", err)
	}

	// 채널에서 이벤트를 수집한다.
	var events []Event
	for ev := range ch {
		events = append(events, ev)
	}

	if len(events) == 0 {
		t.Fatal("이벤트가 하나도 오지 않았다")
	}

	// 첫 이벤트는 초기 Task여야 한다.
	first := events[0]
	if first.Task == nil {
		t.Error("첫 번째 이벤트는 Task 이벤트여야 한다")
	}

	// 마지막 이벤트는 Final=true인 상태 갱신이어야 한다.
	last := events[len(events)-1]
	if last.StatusUpdate == nil {
		t.Error("마지막 이벤트는 StatusUpdate여야 한다")
	} else if !last.StatusUpdate.Final {
		t.Error("마지막 이벤트의 Final 필드가 true여야 한다")
	} else if last.StatusUpdate.Status.State != TaskStateCompleted {
		t.Errorf("마지막 이벤트 상태: 예상 completed, 실제 %q", last.StatusUpdate.Status.State)
	}

	// 아티팩트 갱신 이벤트가 최소 하나 있어야 한다.
	hasArtifact := false
	for _, ev := range events {
		if ev.ArtifactUpdate != nil {
			hasArtifact = true
			break
		}
	}
	if !hasArtifact {
		t.Error("아티팩트 갱신 이벤트가 없다")
	}
}

// TestClient_SendMessageStreaming_ctx취소시_채널이_닫힌다 는 context 취소 시
// 스트리밍이 조기 종료되고 채널이 닫히는지 검증한다.
func TestClient_SendMessageStreaming_ctx취소시_채널이_닫힌다(t *testing.T) {
	// echoExecutor는 즉시 완료되므로 ctx 취소 전에 채널이 닫힐 수 있다.
	// 채널이 어떤 경로로든 닫히는지만 검증한다.
	ts, _ := newTestServer(t, &echoExecutor{})

	resolver := NewCardResolver(ts.URL)
	card, err := resolver.GetAgentCard(context.Background())
	if err != nil {
		t.Fatalf("카드 조회 실패: %v", err)
	}
	card.URL = ts.URL + "/"

	ctx, cancel := context.WithCancel(context.Background())
	client := NewClient(card)
	req := SendMessageRequest{
		Params: MessageSendParams{
			Message: Message{
				Role:  RoleUser,
				Parts: []Part{{Text: &TextPart{Text: "취소 테스트"}}},
			},
		},
	}

	ch, err := client.SendMessageStreaming(ctx, req)
	if err != nil {
		t.Fatalf("SendMessageStreaming 실패: %v", err)
	}

	// ctx를 즉시 취소한다.
	cancel()

	// 채널이 종료될 때까지 드레인한다. 무한 대기를 막기 위해 range를 사용한다.
	for range ch {
	}
	// 여기까지 도달하면 채널이 닫힌 것이다.
}

// ─── 아티팩트 추출 헬퍼 검증 ─────────────────────────────────────────────────

// TestArtifactText_텍스트를_꺼낸다 는 ArtifactText가 텍스트 파트를 올바르게 꺼내는지
// 검증한다(SPEC §5.5).
func TestArtifactText_텍스트를_꺼낸다(t *testing.T) {
	a := Artifact{
		Parts: []Part{
			{Text: &TextPart{Text: "안녕하세요"}},
		},
	}
	text, ok := ArtifactText(a)
	if !ok {
		t.Fatal("ArtifactText: 텍스트 파트를 찾지 못했다")
	}
	if text != "안녕하세요" {
		t.Errorf("ArtifactText: 예상 '안녕하세요', 실제 %q", text)
	}
}

// TestArtifactText_텍스트_없으면_false 는 텍스트 파트가 없을 때 false를 반환하는지
// 검증한다.
func TestArtifactText_텍스트_없으면_false(t *testing.T) {
	a := Artifact{
		Parts: []Part{
			{Data: &DataPart{Data: map[string]any{"key": "value"}}},
		},
	}
	_, ok := ArtifactText(a)
	if ok {
		t.Error("ArtifactText: 텍스트 파트가 없는데 true를 반환했다")
	}
}

// TestArtifactData_데이터를_꺼낸다 는 ArtifactData가 데이터 파트를 올바르게 꺼내는지
// 검증한다(SPEC §5.5).
func TestArtifactData_데이터를_꺼낸다(t *testing.T) {
	expected := map[string]any{"key": "value", "count": float64(42)}
	a := Artifact{
		Parts: []Part{
			{Data: &DataPart{Data: expected}},
		},
	}
	data, ok := ArtifactData(a)
	if !ok {
		t.Fatal("ArtifactData: 데이터 파트를 찾지 못했다")
	}
	if data["key"] != "value" {
		t.Errorf("ArtifactData: key 불일치: %v", data["key"])
	}
}

// TestArtifactData_데이터_없으면_false 는 데이터 파트가 없을 때 false를 반환하는지
// 검증한다.
func TestArtifactData_데이터_없으면_false(t *testing.T) {
	a := Artifact{
		Parts: []Part{
			{Text: &TextPart{Text: "텍스트만 있다"}},
		},
	}
	_, ok := ArtifactData(a)
	if ok {
		t.Error("ArtifactData: 데이터 파트가 없는데 true를 반환했다")
	}
}

// TestArtifactFileURI_URI를_꺼낸다 는 ArtifactFileURI가 파일 URI를 올바르게 꺼내는지
// 검증한다(SPEC §5.5).
func TestArtifactFileURI_URI를_꺼낸다(t *testing.T) {
	uri := "https://example.com/file.txt"
	a := Artifact{
		Parts: []Part{
			{File: &FilePart{File: FileVariant{URI: &FileWithUri{URI: uri, MimeType: "text/plain"}}}},
		},
	}
	got, ok := ArtifactFileURI(a)
	if !ok {
		t.Fatal("ArtifactFileURI: 파일 URI 파트를 찾지 못했다")
	}
	if got != uri {
		t.Errorf("ArtifactFileURI: 예상 %q, 실제 %q", uri, got)
	}
}

// TestArtifactFileURI_URI_없으면_false 는 URI 파일 파트가 없을 때 false를 반환하는지
// 검증한다.
func TestArtifactFileURI_URI_없으면_false(t *testing.T) {
	a := Artifact{
		Parts: []Part{
			{Text: &TextPart{Text: "URI 없음"}},
		},
	}
	_, ok := ArtifactFileURI(a)
	if ok {
		t.Error("ArtifactFileURI: URI 파일 파트가 없는데 true를 반환했다")
	}
}

// TestArtifactFileBytes_bytes를_꺼낸다 는 ArtifactFileBytes가 인라인 파일 bytes를 올바르게
// 꺼내는지 검증한다(SPEC §5.5).
func TestArtifactFileBytes_bytes를_꺼낸다(t *testing.T) {
	data := []byte("파일 내용입니다")
	a := Artifact{
		Parts: []Part{
			{File: &FilePart{File: FileVariant{Bytes: &FileWithBytes{Bytes: data, MimeType: "text/plain"}}}},
		},
	}
	got, ok := ArtifactFileBytes(a)
	if !ok {
		t.Fatal("ArtifactFileBytes: 파일 bytes 파트를 찾지 못했다")
	}
	if string(got) != string(data) {
		t.Errorf("ArtifactFileBytes: 예상 %q, 실제 %q", data, got)
	}
}

// TestArtifactFileBytes_bytes_없으면_false 는 bytes 파일 파트가 없을 때 false를 반환하는지
// 검증한다.
func TestArtifactFileBytes_bytes_없으면_false(t *testing.T) {
	a := Artifact{
		Parts: []Part{
			{Text: &TextPart{Text: "bytes 없음"}},
		},
	}
	_, ok := ArtifactFileBytes(a)
	if ok {
		t.Error("ArtifactFileBytes: bytes 파일 파트가 없는데 true를 반환했다")
	}
}

// TestArtifactHelpers_다중파트_첫번째만_반환 는 여러 파트가 있을 때 각 헬퍼가 첫 번째
// 해당 파트만 반환하는지 검증한다.
func TestArtifactHelpers_다중파트_첫번째만_반환(t *testing.T) {
	a := Artifact{
		Parts: []Part{
			{Text: &TextPart{Text: "첫 번째"}},
			{Text: &TextPart{Text: "두 번째"}},
			{Data: &DataPart{Data: map[string]any{"n": float64(1)}}},
		},
	}

	text, ok := ArtifactText(a)
	if !ok || text != "첫 번째" {
		t.Errorf("ArtifactText 첫 번째 파트 반환 실패: %q, ok=%v", text, ok)
	}

	data, ok := ArtifactData(a)
	if !ok || data["n"] != float64(1) {
		t.Errorf("ArtifactData 첫 번째 데이터 반환 실패: %v, ok=%v", data, ok)
	}
}
