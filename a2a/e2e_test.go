// e2e_test.go 는 task-005 인패키지 루프백 e2e를 담는다.
// 같은 패키지의 서버(httptest)를 띄우고 같은 패키지의 클라이언트가 접속해
// 카드 조회 → SendMessage(비스트리밍) → SendMessageStreaming 왕복이
// 기대 결과를 내는지 검증한다(SPEC §5.7).
//
// 외부 네트워크·원격 에이전트·키 없이 결정적으로 실행된다.
// t.Skip 가드는 없다.
package a2a

import (
	"context"
	"strings"
	"testing"
)

// setupE2EServer 는 e2e 시나리오 전체에서 공유하는 echoExecutor 기반 테스트 서버와
// 그 카드 URL로 초기화된 Client를 반환한다.
// httptest.Server 정리는 t.Cleanup에 등록된다.
func setupE2EServer(t *testing.T) (card AgentCard, client Client) {
	t.Helper()

	// server_test.go 에 정의된 newTestServer / echoExecutor 재사용.
	ts, _ := newTestServer(t, &echoExecutor{})

	// 카드를 조회해 실제 카드 구조를 확인한다.
	resolver := NewCardResolver(ts.URL)
	var err error
	card, err = resolver.GetAgentCard(context.Background())
	if err != nil {
		t.Fatalf("setupE2EServer: GetAgentCard 실패: %v", err)
	}

	// 테스트 서버의 URL로 카드 URL을 재설정한다(카드 필드는 "http://localhost"이므로
	// httptest URL로 교체해야 요청이 테스트 서버로 전달된다).
	card.URL = ts.URL + "/"

	client = NewClient(card)
	return card, client
}

// TestE2E_루프백_카드조회는_카드를_반환한다 는 CardResolver.GetAgentCard 가
// /.well-known/agent-card.json 에서 올바른 카드를 반환하는지 e2e 관점에서 검증한다.
// 이 단계는 비스트리밍·스트리밍 전송이 올바른 대상에 도달하기 위한 전제 조건이다(SPEC §5.7).
func TestE2E_루프백_카드조회는_카드를_반환한다(t *testing.T) {
	ts, _ := newTestServer(t, &echoExecutor{})

	resolver := NewCardResolver(ts.URL)
	card, err := resolver.GetAgentCard(context.Background())
	if err != nil {
		t.Fatalf("GetAgentCard 실패: %v", err)
	}

	if card.Name != "테스트에이전트" {
		t.Errorf("카드 이름 불일치: 기대 '테스트에이전트', 실제 %q", card.Name)
	}
	if card.URL == "" {
		t.Error("카드 URL이 비어 있다")
	}
	if !card.Capabilities.Streaming {
		t.Error("카드 Capabilities.Streaming이 true여야 한다")
	}
}

// TestE2E_루프백_비스트리밍_전체흐름 은 카드 조회 → SendMessage(비스트리밍) → 태스크·아티팩트 수신
// 왕복 전 과정을 단일 흐름으로 검증한다(SPEC §5.7).
// 결정적 실행: 외부 네트워크 없음, skip 없음.
func TestE2E_루프백_비스트리밍_전체흐름(t *testing.T) {
	card, client := setupE2EServer(t)

	// 카드 확인: 비스트리밍 전송 전에 카드가 올바른지 재확인한다.
	if card.Name != "테스트에이전트" {
		t.Fatalf("카드 이름 불일치: 기대 '테스트에이전트', 실제 %q", card.Name)
	}

	// 비스트리밍 전송: SendMessage → Task 수신.
	req := SendMessageRequest{
		Params: MessageSendParams{
			Message: Message{
				Role: RoleUser,
				Parts: []Part{
					{Text: &TextPart{Text: "e2e-비스트리밍"}},
				},
			},
		},
	}

	task, err := client.SendMessage(context.Background(), req)
	if err != nil {
		t.Fatalf("SendMessage 실패: %v", err)
	}

	// 태스크 기본 필드 검증.
	if task.ID == "" {
		t.Error("태스크 ID가 비어 있다")
	}
	if task.Status.State != TaskStateCompleted {
		t.Errorf("태스크 상태: 기대 completed, 실제 %q", task.Status.State)
	}

	// 아티팩트 검증: echoExecutor 는 입력 텍스트를 포함한 아티팩트를 내보낸다.
	if len(task.Artifacts) == 0 {
		t.Fatal("아티팩트가 없다")
	}
	text, ok := ArtifactText(task.Artifacts[0])
	if !ok {
		t.Fatal("아티팩트에 텍스트 파트가 없다")
	}
	if !strings.Contains(text, "e2e-비스트리밍") {
		t.Errorf("아티팩트 텍스트에 입력이 포함되지 않았다: %q", text)
	}
}

// TestE2E_루프백_스트리밍_전체흐름 은 카드 조회 → SendMessageStreaming → 이벤트 시퀀스 수신
// 왕복 전 과정을 단일 흐름으로 검증한다(SPEC §5.7).
// 결정적 실행: 외부 네트워크 없음, skip 없음.
func TestE2E_루프백_스트리밍_전체흐름(t *testing.T) {
	// 스트리밍 경로는 중간 working 이벤트가 최소 하나 있어야 흐름이 완전히 검증되므로
	// extraSteps=1 실행기를 사용하는 별도 서버를 구성한다.
	ts, _ := newTestServer(t, &echoExecutor{extraSteps: 1})

	resolver := NewCardResolver(ts.URL)
	card, err := resolver.GetAgentCard(context.Background())
	if err != nil {
		t.Fatalf("GetAgentCard 실패: %v", err)
	}
	card.URL = ts.URL + "/"

	client := NewClient(card)
	req := SendMessageRequest{
		Params: MessageSendParams{
			Message: Message{
				Role: RoleUser,
				Parts: []Part{
					{Text: &TextPart{Text: "e2e-스트리밍"}},
				},
			},
		},
	}

	ch, err := client.SendMessageStreaming(context.Background(), req)
	if err != nil {
		t.Fatalf("SendMessageStreaming 실패: %v", err)
	}

	// 채널에서 이벤트 전체 수집.
	var events []Event
	for ev := range ch {
		events = append(events, ev)
	}

	if len(events) == 0 {
		t.Fatal("이벤트가 하나도 오지 않았다")
	}

	// 첫 이벤트는 초기 Task 이벤트여야 한다(서버가 스트림 시작 시 초기 태스크를 전송).
	first := events[0]
	if first.Task == nil {
		t.Error("첫 번째 이벤트는 Task 이벤트여야 한다")
	}

	// 마지막 이벤트는 Final=true 인 상태 갱신이어야 한다.
	last := events[len(events)-1]
	if last.StatusUpdate == nil {
		t.Error("마지막 이벤트는 StatusUpdate여야 한다")
	} else {
		if !last.StatusUpdate.Final {
			t.Error("마지막 이벤트의 Final 필드가 true여야 한다")
		}
		if last.StatusUpdate.Status.State != TaskStateCompleted {
			t.Errorf("마지막 이벤트 상태: 기대 completed, 실제 %q", last.StatusUpdate.Status.State)
		}
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

	// 아티팩트 이벤트의 파트에 입력 텍스트가 포함되어 있어야 한다.
	for _, ev := range events {
		if ev.ArtifactUpdate == nil {
			continue
		}
		for _, p := range ev.ArtifactUpdate.Artifact.Parts {
			if p.Text != nil && strings.Contains(p.Text.Text, "e2e-스트리밍") {
				return // 검증 통과.
			}
		}
	}
	t.Error("아티팩트 이벤트 파트에 입력 텍스트 'e2e-스트리밍'이 포함되지 않았다")
}

// TestE2E_루프백_비스트리밍과_스트리밍_동일서버 는 같은 서버에 비스트리밍과 스트리밍 요청을
// 순차로 전송해 두 경로가 동일한 서버에서 올바르게 동작하는지 통합 검증한다(SPEC §5.7).
// 이 테스트가 핵심 e2e 통합 단정이다: 카드 조회 → 비스트리밍 → 스트리밍이 하나의 흐름으로 묶인다.
func TestE2E_루프백_비스트리밍과_스트리밍_동일서버(t *testing.T) {
	// 스트리밍에서 working 이벤트가 최소 하나 있어야 하므로 extraSteps=1 실행기를 사용한다.
	ts, _ := newTestServer(t, &echoExecutor{extraSteps: 1})

	// ── 1단계: 카드 조회 ──────────────────────────────────────────────────────
	resolver := NewCardResolver(ts.URL)
	card, err := resolver.GetAgentCard(context.Background())
	if err != nil {
		t.Fatalf("1단계 카드 조회 실패: %v", err)
	}
	if card.Name != "테스트에이전트" {
		t.Errorf("카드 이름 불일치: 기대 '테스트에이전트', 실제 %q", card.Name)
	}
	if !card.Capabilities.Streaming {
		t.Error("카드 Capabilities.Streaming이 true여야 한다")
	}

	// 테스트 서버 URL로 카드 URL을 교체한다.
	card.URL = ts.URL + "/"
	client := NewClient(card)

	// ── 2단계: 비스트리밍 전송 ───────────────────────────────────────────────
	nonStreamReq := SendMessageRequest{
		Params: MessageSendParams{
			Message: Message{
				Role: RoleUser,
				Parts: []Part{
					{Text: &TextPart{Text: "통합-비스트리밍"}},
				},
			},
		},
	}
	task, err := client.SendMessage(context.Background(), nonStreamReq)
	if err != nil {
		t.Fatalf("2단계 SendMessage 실패: %v", err)
	}
	if task.Status.State != TaskStateCompleted {
		t.Errorf("2단계 태스크 상태: 기대 completed, 실제 %q", task.Status.State)
	}
	if len(task.Artifacts) == 0 {
		t.Error("2단계 아티팩트가 없다")
	} else {
		text, ok := ArtifactText(task.Artifacts[0])
		if !ok || !strings.Contains(text, "통합-비스트리밍") {
			t.Errorf("2단계 아티팩트 텍스트에 입력이 없다: %q", text)
		}
	}

	// ── 3단계: 스트리밍 전송 ─────────────────────────────────────────────────
	streamReq := SendMessageRequest{
		Params: MessageSendParams{
			Message: Message{
				Role: RoleUser,
				Parts: []Part{
					{Text: &TextPart{Text: "통합-스트리밍"}},
				},
			},
		},
	}
	ch, err := client.SendMessageStreaming(context.Background(), streamReq)
	if err != nil {
		t.Fatalf("3단계 SendMessageStreaming 실패: %v", err)
	}

	var events []Event
	for ev := range ch {
		events = append(events, ev)
	}

	if len(events) == 0 {
		t.Fatal("3단계 이벤트가 하나도 오지 않았다")
	}

	// 첫 이벤트는 Task여야 한다.
	if events[0].Task == nil {
		t.Error("3단계 첫 번째 이벤트는 Task 이벤트여야 한다")
	}

	// 마지막 이벤트는 Final=true 인 completed 상태여야 한다.
	last := events[len(events)-1]
	if last.StatusUpdate == nil {
		t.Error("3단계 마지막 이벤트는 StatusUpdate여야 한다")
	} else {
		if !last.StatusUpdate.Final {
			t.Error("3단계 마지막 이벤트의 Final 필드가 true여야 한다")
		}
		if last.StatusUpdate.Status.State != TaskStateCompleted {
			t.Errorf("3단계 마지막 이벤트 상태: 기대 completed, 실제 %q", last.StatusUpdate.Status.State)
		}
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
		t.Error("3단계 아티팩트 갱신 이벤트가 없다")
	}
}
