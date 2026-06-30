// multi_server_test.go 는 task-004 검증 조건을 단정하는 단위 테스트를 담는다.
// newMultiServerClientWithClients 테스트 훅으로 인메모리 루프백 *Client를 주입해
// 외부 프로세스·네트워크 없이 통합·서버별·프롬프트·미등록 분기를 검증한다.
package mcp

import (
	"context"
	"errors"
	"testing"

	"github.com/zipkero/langgraph-go/message"
)

// newMultiServerClientWithClients 는 이미 만들어진 *Client 맵과 순서 슬라이스로
// MultiServerClient를 구성하는 테스트 훅이다.
// 인메모리 루프백 테스트에서 Connect 없이 주입할 때 사용한다.
func newMultiServerClientWithClients(clients map[string]*Client, order []string) *MultiServerClient {
	return &MultiServerClient{
		clients: clients,
		order:   order,
	}
}

// buildClientForServer 는 서버를 인메모리 전송으로 띄우고 *Client를 연결해 반환한다.
// connectClientToServer(client_test.go)와 동일한 방식이나, cancel 생명주기를 호출자가 관리한다.
func buildClientForServer(t *testing.T, srv *Server) (*Client, context.CancelFunc) {
	t.Helper()
	return connectClientToServer(t, srv)
}

// TestMultiServerClient_GetTools_통합_순서 는 두 서버를 묶어 GetTools를 호출했을 때
// 두 서버의 도구가 이름 sort 순서(등록 순서)로 합쳐져 반환되는지 검증한다.
func TestMultiServerClient_GetTools_통합_순서(t *testing.T) {
	// 서버 A: "tool-b" 도구 등록
	srvA := NewServer("server-a")
	if err := srvA.RegisterTool(makeEchoTool("tool-b", "B 도구")); err != nil {
		t.Fatalf("RegisterTool 실패: %v", err)
	}
	// 서버 B: "tool-a" 도구 등록
	srvB := NewServer("server-b")
	if err := srvB.RegisterTool(makeEchoTool("tool-a", "A 도구")); err != nil {
		t.Fatalf("RegisterTool 실패: %v", err)
	}

	clientA, cancelA := buildClientForServer(t, srvA)
	defer cancelA()
	defer clientA.Close()

	clientB, cancelB := buildClientForServer(t, srvB)
	defer cancelB()
	defer clientB.Close()

	// order: ["server-a", "server-b"] (이름 sort 기준)
	mc := newMultiServerClientWithClients(
		map[string]*Client{
			"server-a": clientA,
			"server-b": clientB,
		},
		[]string{"server-a", "server-b"},
	)

	tools, err := mc.GetTools(context.Background())
	if err != nil {
		t.Fatalf("GetTools 실패: %v", err)
	}

	// server-a의 "tool-b"가 먼저, server-b의 "tool-a"가 다음에 와야 한다.
	if len(tools) != 2 {
		t.Fatalf("도구 수 불일치: 기대 2, 실제 %d", len(tools))
	}
	if tools[0].Name() != "tool-b" {
		t.Errorf("tools[0].Name 불일치: 기대 %q, 실제 %q", "tool-b", tools[0].Name())
	}
	if tools[1].Name() != "tool-a" {
		t.Errorf("tools[1].Name 불일치: 기대 %q, 실제 %q", "tool-a", tools[1].Name())
	}
}

// TestMultiServerClient_GetTools_여러_서버_모든_도구_포함 은 두 서버의 도구가 모두
// 통합 목록에 포함되는지 검증한다.
func TestMultiServerClient_GetTools_여러_서버_모든_도구_포함(t *testing.T) {
	srvA := NewServer("alpha")
	if err := srvA.RegisterTool(makeEchoTool("alpha-tool", "알파 도구")); err != nil {
		t.Fatalf("RegisterTool 실패: %v", err)
	}
	srvB := NewServer("beta")
	if err := srvB.RegisterTool(makeEchoTool("beta-tool", "베타 도구")); err != nil {
		t.Fatalf("RegisterTool 실패: %v", err)
	}

	clientA, cancelA := buildClientForServer(t, srvA)
	defer cancelA()
	defer clientA.Close()

	clientB, cancelB := buildClientForServer(t, srvB)
	defer cancelB()
	defer clientB.Close()

	mc := newMultiServerClientWithClients(
		map[string]*Client{
			"alpha": clientA,
			"beta":  clientB,
		},
		[]string{"alpha", "beta"},
	)

	tools, err := mc.GetTools(context.Background())
	if err != nil {
		t.Fatalf("GetTools 실패: %v", err)
	}

	names := make(map[string]bool)
	for _, tl := range tools {
		names[tl.Name()] = true
	}
	for _, expected := range []string{"alpha-tool", "beta-tool"} {
		if !names[expected] {
			t.Errorf("도구 %q가 통합 목록에 없음", expected)
		}
	}
}

// TestMultiServerClient_GetToolsByServer_서버별_도구만_반환 은 GetToolsByServer가
// 지정한 서버의 도구만 반환하는지 검증한다.
func TestMultiServerClient_GetToolsByServer_서버별_도구만_반환(t *testing.T) {
	srvA := NewServer("server-a")
	if err := srvA.RegisterTool(makeEchoTool("tool-from-a", "A 도구")); err != nil {
		t.Fatalf("RegisterTool 실패: %v", err)
	}
	srvB := NewServer("server-b")
	if err := srvB.RegisterTool(makeEchoTool("tool-from-b", "B 도구")); err != nil {
		t.Fatalf("RegisterTool 실패: %v", err)
	}

	clientA, cancelA := buildClientForServer(t, srvA)
	defer cancelA()
	defer clientA.Close()

	clientB, cancelB := buildClientForServer(t, srvB)
	defer cancelB()
	defer clientB.Close()

	mc := newMultiServerClientWithClients(
		map[string]*Client{
			"server-a": clientA,
			"server-b": clientB,
		},
		[]string{"server-a", "server-b"},
	)

	// server-a의 도구만 조회한다.
	tools, err := mc.GetToolsByServer(context.Background(), "server-a")
	if err != nil {
		t.Fatalf("GetToolsByServer 실패: %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("도구 수 불일치: 기대 1, 실제 %d", len(tools))
	}
	if tools[0].Name() != "tool-from-a" {
		t.Errorf("도구 이름 불일치: 기대 %q, 실제 %q", "tool-from-a", tools[0].Name())
	}

	// server-b의 도구만 조회한다.
	toolsB, err := mc.GetToolsByServer(context.Background(), "server-b")
	if err != nil {
		t.Fatalf("GetToolsByServer(server-b) 실패: %v", err)
	}
	if len(toolsB) != 1 {
		t.Fatalf("도구 수 불일치: 기대 1, 실제 %d", len(toolsB))
	}
	if toolsB[0].Name() != "tool-from-b" {
		t.Errorf("도구 이름 불일치: 기대 %q, 실제 %q", "tool-from-b", toolsB[0].Name())
	}
}

// TestMultiServerClient_GetPrompt_해당_서버_프롬프트만_반환 은 GetPrompt가 지정한
// 서버의 프롬프트만 반환하는지 검증한다.
func TestMultiServerClient_GetPrompt_해당_서버_프롬프트만_반환(t *testing.T) {
	srvA := NewServer("server-a")
	msgsA := []message.Message{
		message.NewUserMessage("A 서버 질문"),
		message.NewAssistantMessage("A 서버 답변"),
	}
	if err := srvA.RegisterPrompt("prompt-a", msgsA); err != nil {
		t.Fatalf("RegisterPrompt 실패: %v", err)
	}

	srvB := NewServer("server-b")
	msgsB := []message.Message{
		message.NewUserMessage("B 서버 질문"),
	}
	if err := srvB.RegisterPrompt("prompt-b", msgsB); err != nil {
		t.Fatalf("RegisterPrompt 실패: %v", err)
	}

	clientA, cancelA := buildClientForServer(t, srvA)
	defer cancelA()
	defer clientA.Close()

	clientB, cancelB := buildClientForServer(t, srvB)
	defer cancelB()
	defer clientB.Close()

	mc := newMultiServerClientWithClients(
		map[string]*Client{
			"server-a": clientA,
			"server-b": clientB,
		},
		[]string{"server-a", "server-b"},
	)

	// server-a의 prompt-a를 조회한다.
	result, err := mc.GetPrompt(context.Background(), "server-a", "prompt-a", nil)
	if err != nil {
		t.Fatalf("GetPrompt 실패: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("메시지 수 불일치: 기대 2, 실제 %d", len(result))
	}
	if result[0].Content != "A 서버 질문" {
		t.Errorf("result[0].Content 불일치: 기대 %q, 실제 %q", "A 서버 질문", result[0].Content)
	}
	if result[1].Content != "A 서버 답변" {
		t.Errorf("result[1].Content 불일치: 기대 %q, 실제 %q", "A 서버 답변", result[1].Content)
	}

	// server-b의 prompt-b를 조회한다.
	resultB, err := mc.GetPrompt(context.Background(), "server-b", "prompt-b", nil)
	if err != nil {
		t.Fatalf("GetPrompt(server-b) 실패: %v", err)
	}
	if len(resultB) != 1 {
		t.Fatalf("메시지 수 불일치: 기대 1, 실제 %d", len(resultB))
	}
	if resultB[0].Content != "B 서버 질문" {
		t.Errorf("resultB[0].Content 불일치: 기대 %q, 실제 %q", "B 서버 질문", resultB[0].Content)
	}
}

// TestMultiServerClient_미등록_서버_GetTools_not_found 는 존재하지 않는 서버 이름으로
// GetToolsByServer를 호출하면 IsServerNotFound(err)가 true인 에러가 반환되는지 검증한다.
func TestMultiServerClient_미등록_서버_GetToolsByServer_not_found(t *testing.T) {
	mc := newMultiServerClientWithClients(
		map[string]*Client{},
		[]string{},
	)

	_, err := mc.GetToolsByServer(context.Background(), "nonexistent-server")
	if err == nil {
		t.Fatal("에러가 반환되어야 하지만 nil이 반환됨")
	}
	if !IsServerNotFound(err) {
		t.Errorf("IsServerNotFound(err)가 true여야 하지만 false임; err: %v", err)
	}
	if !errors.Is(err, ErrServerNotFound) {
		t.Errorf("errors.Is(err, ErrServerNotFound)가 true여야 하지만 false임; err: %v", err)
	}
}

// TestMultiServerClient_미등록_서버_GetPrompt_not_found 는 존재하지 않는 서버 이름으로
// GetPrompt를 호출하면 IsServerNotFound(err)가 true인 에러가 반환되는지 검증한다.
func TestMultiServerClient_미등록_서버_GetPrompt_not_found(t *testing.T) {
	mc := newMultiServerClientWithClients(
		map[string]*Client{},
		[]string{},
	)

	_, err := mc.GetPrompt(context.Background(), "no-such-server", "my-prompt", nil)
	if err == nil {
		t.Fatal("에러가 반환되어야 하지만 nil이 반환됨")
	}
	if !IsServerNotFound(err) {
		t.Errorf("IsServerNotFound(err)가 true여야 하지만 false임; err: %v", err)
	}
	if !errors.Is(err, ErrServerNotFound) {
		t.Errorf("errors.Is(err, ErrServerNotFound)가 true여야 하지만 false임; err: %v", err)
	}
}

// TestIsServerNotFound_센티널_에러_직접_판정 은 ErrServerNotFound 자체를 IsServerNotFound로
// 판정했을 때 true를 반환하는지 검증한다.
func TestIsServerNotFound_센티널_에러_직접_판정(t *testing.T) {
	if !IsServerNotFound(ErrServerNotFound) {
		t.Error("IsServerNotFound(ErrServerNotFound)가 true여야 함")
	}
}

// TestIsServerNotFound_다른_에러는_false 는 일반 에러를 IsServerNotFound로 판정했을 때
// false를 반환하는지 검증한다.
func TestIsServerNotFound_다른_에러는_false(t *testing.T) {
	err := errors.New("다른 에러")
	if IsServerNotFound(err) {
		t.Error("IsServerNotFound(일반 에러)가 false여야 함")
	}
}
