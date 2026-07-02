// http_transport_test.go 는 task-005 검증 조건(streamable HTTP 전송)을 단정하는 테스트를 담는다.
// httptest 서버로 Server.streamableHTTPHandler()가 노출하는 도구·프롬프트를 실제 Client.Connect
// (TransportStreamableHTTP)로 왕복 확인하고, ServeStreamableHTTP(ctx, addr)가 실제 포트에 바인딩해
// graceful shutdown하는지도 별도로 확인한다.
package mcp

import (
	"context"
	"encoding/json"
	"net"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/zipkero/langgraph-go/message"
)

// TestStreamableHTTP_httptest_왕복 은 httptest 서버로 노출한 도구·프롬프트를
// 실제 Client.Connect(TransportStreamableHTTP) 경로로 ListTools/CallTool/LoadPrompt까지
// 왕복 확인한다. 기존 stdio 경로와 동일한 등록물 공유 방식을 검증한다.
func TestStreamableHTTP_httptest_왕복(t *testing.T) {
	srv := NewServer("http-test-srv")
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

	ts := httptest.NewServer(srv.streamableHTTPHandler())
	defer ts.Close()

	client := NewClient(ServerConfig{
		Transport: TransportStreamableHTTP,
		URL:       ts.URL,
	})

	ctx := t.Context()
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect 실패: %v", err)
	}
	defer client.Close()

	// --- ListTools: 등록한 도구가 스키마 목록으로 와야 한다 ---
	schemas, err := client.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools 실패: %v", err)
	}
	found := false
	for _, s := range schemas {
		if s.Name == "echo" {
			found = true
			if s.Description != "에코 도구" {
				t.Errorf("설명 불일치: 기대 %q, 실제 %q", "에코 도구", s.Description)
			}
		}
	}
	if !found {
		t.Fatalf("ListTools: 등록한 도구 %q를 찾지 못함 (실제 목록: %v)", "echo", schemas)
	}

	// --- CallTool: 도구 호출 결과가 기대값이어야 한다 ---
	args, err := json.Marshal(map[string]any{"msg": "http-hello"})
	if err != nil {
		t.Fatalf("인자 직렬화 실패: %v", err)
	}
	res, err := client.CallTool(ctx, "echo", args)
	if err != nil {
		t.Fatalf("CallTool 실패: %v", err)
	}
	if res.IsError {
		t.Error("IsError가 true여야 하지 않음")
	}
	if res.Content != "http-hello" {
		t.Errorf("Content 불일치: 기대 %q, 실제 %q", "http-hello", res.Content)
	}

	// --- LoadPrompt: 프롬프트 로딩이 기대 메시지를 내야 한다 ---
	promptMsgs, err := client.LoadPrompt(ctx, "greeting", nil)
	if err != nil {
		t.Fatalf("LoadPrompt 실패: %v", err)
	}
	if len(promptMsgs) != 2 {
		t.Fatalf("메시지 수 불일치: 기대 2, 실제 %d", len(promptMsgs))
	}
	if promptMsgs[0].Role != message.RoleUser || promptMsgs[0].Content != "안녕하세요" {
		t.Errorf("Messages[0] 불일치: %+v", promptMsgs[0])
	}
	if promptMsgs[1].Role != message.RoleAssistant || promptMsgs[1].Content != "무엇을 도와드릴까요?" {
		t.Errorf("Messages[1] 불일치: %+v", promptMsgs[1])
	}
}

// TestClientConnect_streamable_http_URL_필수 는 URL이 비어 있으면 Connect가
// stdio의 Command 검증과 대칭으로 에러를 반환하는지 확인한다.
func TestClientConnect_streamable_http_URL_필수(t *testing.T) {
	client := NewClient(ServerConfig{Transport: TransportStreamableHTTP})
	if err := client.Connect(t.Context()); err == nil {
		t.Fatal("URL이 없으면 에러가 반환되어야 함")
	}
}

// TestServeStreamableHTTP_실제_포트_바인딩과_graceful_shutdown 은
// ServeStreamableHTTP(ctx, addr)가 실제 TCP 포트에 바인딩해 도구를 노출하고,
// ctx 취소 시 블로킹 호출이 정상 반환(graceful shutdown)되는지 확인한다.
func TestServeStreamableHTTP_실제_포트_바인딩과_graceful_shutdown(t *testing.T) {
	srv := NewServer("http-serve-srv")
	if err := srv.RegisterTool(makeEchoTool("echo", "에코 도구")); err != nil {
		t.Fatalf("RegisterTool 실패: %v", err)
	}

	// 임시로 빈 포트를 예약한 뒤 즉시 반환해, ServeStreamableHTTP가 재바인딩할 addr을 얻는다.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("포트 예약 실패: %v", err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatalf("포트 예약 해제 실패: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	serveErr := make(chan error, 1)
	go func() {
		serveErr <- srv.ServeStreamableHTTP(ctx, addr)
	}()

	client := NewClient(ServerConfig{
		Transport: TransportStreamableHTTP,
		URL:       "http://" + addr,
	})

	// 서버 기동 완료까지 재시도한다.
	var connectErr error
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		connectErr = client.Connect(ctx)
		if connectErr == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if connectErr != nil {
		cancel()
		t.Fatalf("Connect 실패(재시도 후): %v", connectErr)
	}
	defer client.Close()

	schemas, err := client.ListTools(ctx)
	if err != nil {
		cancel()
		t.Fatalf("ListTools 실패: %v", err)
	}
	found := false
	for _, s := range schemas {
		if s.Name == "echo" {
			found = true
		}
	}
	if !found {
		cancel()
		t.Fatalf("ListTools: 등록한 도구 %q를 찾지 못함", "echo")
	}

	// ctx 취소로 graceful shutdown을 유도하고, ServeStreamableHTTP가 반환되는지 확인한다.
	cancel()
	select {
	case err := <-serveErr:
		if err != nil {
			t.Errorf("ServeStreamableHTTP가 graceful shutdown 후 에러 반환: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ServeStreamableHTTP가 ctx 취소 후 반환되지 않음(타임아웃)")
	}
}
