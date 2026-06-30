// e2e_test.go 는 task-005 인패키지 루프백 e2e를 담는다.
// 우리 Server와 Client가 공개 API(Server+Client+어댑터)를 한 시나리오로 관통하는 왕복을 검증한다.
// 두 경로:
//  1. 인메모리 주경로: NewInMemoryTransports 쌍으로 ListTools→CallTool→LoadPrompt 왕복
//  2. stdio 실증 경로: os.Pipe 두 쌍 + IOTransport로 같은 프로세스에서 stdio 코드 경로 실증
//
// 외부 프로세스·네트워크·키 없이 결정적으로 실행되며, t.Skip 가드가 없다.
package mcp

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zipkero/langgraph-go/message"
	"github.com/zipkero/langgraph-go/tool"
)

// setupE2EServer 는 에코 도구와 greeting 프롬프트가 등록된 서버를 반환한다.
// e2e 시나리오 전체에서 공유되는 고정 등록 집합이다.
func setupE2EServer(t *testing.T) *Server {
	t.Helper()
	srv := NewServer("e2e-server")
	if err := srv.RegisterTool(makeEchoTool("echo", "에코 도구")); err != nil {
		t.Fatalf("RegisterTool 실패: %v", err)
	}
	msgs := []message.Message{
		message.NewUserMessage("안녕하세요"),
		message.NewAssistantMessage("무엇을 도와드릴까요?"),
	}
	if err := srv.RegisterPrompt("greeting", msgs); err != nil {
		t.Fatalf("RegisterPrompt 실패: %v", err)
	}
	return srv
}

// runE2ERoundTrip 는 client를 받아 ListTools→CallTool→LoadPrompt 왕복을 단정한다.
// 인메모리·stdio 두 경로 모두 동일한 단정 로직을 사용한다.
func runE2ERoundTrip(t *testing.T, client *Client) {
	t.Helper()
	ctx := context.Background()

	// --- ListTools: 등록한 도구가 스키마 목록으로 와야 한다 ---
	schemas, err := client.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools 실패: %v", err)
	}

	var echoSchema *tool.Schema
	for i := range schemas {
		if schemas[i].Name == "echo" {
			echoSchema = &schemas[i]
			break
		}
	}
	if echoSchema == nil {
		t.Fatalf("ListTools: 등록한 도구 %q를 스키마 목록에서 찾지 못함 (실제 목록: %v)", "echo", schemas)
	}
	if echoSchema.Description != "에코 도구" {
		t.Errorf("ListTools: 설명 불일치 — 기대 %q, 실제 %q", "에코 도구", echoSchema.Description)
	}

	// --- LoadMCPTools + Execute: 어댑터까지 관통하는 왕복 ---
	tools, err := LoadMCPTools(ctx, client)
	if err != nil {
		t.Fatalf("LoadMCPTools 실패: %v", err)
	}

	var echoTool tool.Tool
	for _, tl := range tools {
		if tl.Name() == "echo" {
			echoTool = tl
			break
		}
	}
	if echoTool == nil {
		t.Fatal("LoadMCPTools: 어댑터 목록에서 echo 도구를 찾지 못함")
	}

	// --- CallTool: 도구 호출 결과가 기대값이어야 한다 ---
	args, err := json.Marshal(map[string]any{"msg": "e2e-hello"})
	if err != nil {
		t.Fatalf("인자 직렬화 실패: %v", err)
	}

	callRes, err := client.CallTool(ctx, "echo", args)
	if err != nil {
		t.Fatalf("CallTool 실패: %v", err)
	}
	if callRes.IsError {
		t.Errorf("CallTool: IsError가 true여야 하지 않음")
	}
	if callRes.Content != "e2e-hello" {
		t.Errorf("CallTool: Content 불일치 — 기대 %q, 실제 %q", "e2e-hello", callRes.Content)
	}

	// --- RemoteTool.Execute: 어댑터 도구 실행이 원격 호출로 결과를 내야 한다 ---
	execRes, err := echoTool.Execute(ctx, args, nil)
	if err != nil {
		t.Fatalf("RemoteTool.Execute 실패: %v", err)
	}
	if execRes.Content != "e2e-hello" {
		t.Errorf("RemoteTool.Execute: Content 불일치 — 기대 %q, 실제 %q", "e2e-hello", execRes.Content)
	}

	// --- LoadPrompt: 프롬프트 로딩이 기대 메시지를 내야 한다 ---
	promptMsgs, err := client.LoadPrompt(ctx, "greeting", nil)
	if err != nil {
		t.Fatalf("LoadPrompt 실패: %v", err)
	}
	if len(promptMsgs) != 2 {
		t.Fatalf("LoadPrompt: 메시지 수 불일치 — 기대 2, 실제 %d", len(promptMsgs))
	}
	if promptMsgs[0].Role != message.RoleUser {
		t.Errorf("LoadPrompt: Messages[0].Role 불일치 — 기대 %q, 실제 %q", message.RoleUser, promptMsgs[0].Role)
	}
	if promptMsgs[0].Content != "안녕하세요" {
		t.Errorf("LoadPrompt: Messages[0].Content 불일치 — 기대 %q, 실제 %q", "안녕하세요", promptMsgs[0].Content)
	}
	if promptMsgs[1].Role != message.RoleAssistant {
		t.Errorf("LoadPrompt: Messages[1].Role 불일치 — 기대 %q, 실제 %q", message.RoleAssistant, promptMsgs[1].Role)
	}
	if promptMsgs[1].Content != "무엇을 도와드릴까요?" {
		t.Errorf("LoadPrompt: Messages[1].Content 불일치 — 기대 %q, 실제 %q", "무엇을 도와드릴까요?", promptMsgs[1].Content)
	}

	// --- LoadMCPPrompt 어댑터: 동일한 프롬프트를 어댑터 경로로 확인 ---
	adapterMsgs, err := LoadMCPPrompt(ctx, client, "greeting", nil)
	if err != nil {
		t.Fatalf("LoadMCPPrompt 실패: %v", err)
	}
	if len(adapterMsgs) != 2 {
		t.Fatalf("LoadMCPPrompt: 메시지 수 불일치 — 기대 2, 실제 %d", len(adapterMsgs))
	}
}

// TestE2E_인메모리_루프백_전체_왕복은 인메모리 주경로로 Server+Client+어댑터 공개 API 전체를
// 한 시나리오(ListTools→LoadMCPTools→CallTool→RemoteTool.Execute→LoadPrompt→LoadMCPPrompt)로
// 관통하는 왕복을 단정한다.
// 가용성 가드(t.Skip) 없음: 외부 의존 없이 항상 실행 가능하다.
func TestE2E_인메모리_루프백_전체_왕복(t *testing.T) {
	srv := setupE2EServer(t)

	// 인메모리 전송으로 서버와 클라이언트를 연결한다.
	serverTransport, clientTransport := sdkmcp.NewInMemoryTransports()

	ctx := t.Context()

	// 서버를 별도 고루틴에서 실행한다(ctx 취소 시 종료).
	go func() {
		_ = srv.sdkServer.Run(ctx, serverTransport)
	}()

	// newClientWithTransport 테스트 훅으로 클라이언트를 인메모리 전송에 연결한다.
	client, err := newClientWithTransport(ctx, clientTransport)
	if err != nil {
		t.Fatalf("인메모리 클라이언트 연결 실패: %v", err)
	}
	defer client.Close()

	// 공통 왕복 단정 실행.
	runE2ERoundTrip(t, client)
}

// TestE2E_stdio_IOTransport_경로_실증은 os.Pipe 두 쌍 + IOTransport로 같은 프로세스에서
// stdio 전송 코드 경로를 실증한다. 외부 프로세스 없이 결정적으로 실행된다.
// IOTransport(io.ReadCloser + io.WriteCloser)는 StdioTransport와 동일한 newline-delimited JSON
// 전송이므로 stdio 코드 경로를 실증하기에 충분하다.
// 가용성 가드(t.Skip) 없음: os.Pipe는 플랫폼 표준이며 외부 의존이 없다.
func TestE2E_stdio_IOTransport_경로_실증(t *testing.T) {
	srv := setupE2EServer(t)

	// os.Pipe 두 쌍으로 서버↔클라이언트 양방향 채널을 구성한다.
	// 서버 읽기 ← 클라이언트 쓰기 (clientW → serverR)
	serverR, clientW, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe(clientW→serverR) 실패: %v", err)
	}
	// 클라이언트 읽기 ← 서버 쓰기 (serverW → clientR)
	clientR, serverW, err := os.Pipe()
	if err != nil {
		clientW.Close()
		serverR.Close()
		t.Fatalf("os.Pipe(serverW→clientR) 실패: %v", err)
	}

	// 서버 측 IOTransport: 클라이언트가 쓴 바이트를 serverR로 읽고, serverW로 응답한다.
	serverIOTransport := &sdkmcp.IOTransport{
		Reader: serverR,
		Writer: serverW,
	}

	// 클라이언트 측 IOTransport: 서버가 쓴 바이트를 clientR로 읽고, clientW로 요청한다.
	clientIOTransport := &sdkmcp.IOTransport{
		Reader: clientR,
		Writer: clientW,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 서버를 별도 고루틴에서 실행한다. ctx 취소 또는 파이프 닫힘 시 종료된다.
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		_ = srv.sdkServer.Run(ctx, serverIOTransport)
	}()

	// 클라이언트를 IOTransport로 연결한다.
	client, err := newClientWithTransport(ctx, clientIOTransport)
	if err != nil {
		t.Fatalf("IOTransport 클라이언트 연결 실패: %v", err)
	}

	// 왕복 단정 실행.
	runE2ERoundTrip(t, client)

	// 클라이언트 연결 종료 후 서버 파이프를 닫아 서버 고루틴이 정리되게 한다.
	client.Close()
	clientW.Close()
	serverR.Close()

	// 파이프 닫힘으로 서버가 정리될 때까지 기다린다.
	cancel()
	// serverW/clientR은 서버 Run이 종료된 후 닫는다.
	<-serverDone
	serverW.Close()
	clientR.Close()
}
