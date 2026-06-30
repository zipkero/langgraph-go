// client_test.go 는 task-002 검증 조건을 단정하는 단위 테스트를 담는다.
// 인메모리 전송으로 우리 Server(task-001)에 우리 Client를 붙이는 루프백으로 검증한다.
// newClientWithTransport(테스트 훅)를 사용해 CommandTransport 없이 변환 경로를 검증한다.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zipkero/langgraph-go/message"
	"github.com/zipkero/langgraph-go/tool"
)

// newClientWithTransport 는 이미 만들어진 전송으로 클라이언트를 생성하는 테스트 훅이다.
// 인메모리 전송 등 외부에서 전송을 직접 주입할 때 사용한다.
func newClientWithTransport(ctx context.Context, tr sdkmcp.Transport) (*Client, error) {
	sdkCli := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "langgraph-go-client"}, nil)
	cs, err := sdkCli.Connect(ctx, tr, nil)
	if err != nil {
		return nil, fmt.Errorf("mcp: 전송 연결 실패: %w", err)
	}
	return &Client{
		sdkCli:  sdkCli,
		session: cs,
	}, nil
}

// connectClientToServer 는 우리 Server에 인메모리 전송으로 우리 Client를 연결한다.
// newClientWithTransport 테스트 훅을 사용하므로 stdio CommandTransport가 필요 없다.
func connectClientToServer(t *testing.T, srv *Server) (*Client, context.CancelFunc) {
	t.Helper()
	serverTransport, clientTransport := sdkmcp.NewInMemoryTransports()

	ctx, cancel := context.WithCancel(context.Background())

	// 서버를 별도 고루틴에서 실행한다.
	go func() {
		_ = srv.sdkServer.Run(ctx, serverTransport)
	}()

	client, err := newClientWithTransport(ctx, clientTransport)
	if err != nil {
		cancel()
		t.Fatalf("클라이언트 연결 실패: %v", err)
	}
	return client, cancel
}

// TestClient_ListTools_원격_도구가_스키마_목록으로_온다 는 ListTools가 서버에 등록된
// 도구를 우리 tool.Schema 목록으로 반환하는지 검증한다.
func TestClient_ListTools_원격_도구가_스키마_목록으로_온다(t *testing.T) {
	srv := NewServer("test-srv")
	if err := srv.RegisterTool(makeEchoTool("echo", "에코 도구")); err != nil {
		t.Fatalf("RegisterTool 실패: %v", err)
	}

	client, cancel := connectClientToServer(t, srv)
	defer cancel()
	defer client.Close()

	schemas, err := client.ListTools(context.Background())
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
		t.Errorf("등록한 도구 %q를 스키마 목록에서 찾지 못함", "echo")
	}
}

// TestClient_ListTools_스키마_파라미터_변환 은 ListTools가 반환한 스키마에
// InputSchema properties가 []tool.Parameter로 변환되는지 검증한다.
func TestClient_ListTools_스키마_파라미터_변환(t *testing.T) {
	srv := NewServer("test-srv")
	if err := srv.RegisterTool(makeEchoTool("echo", "에코 도구")); err != nil {
		t.Fatalf("RegisterTool 실패: %v", err)
	}

	client, cancel := connectClientToServer(t, srv)
	defer cancel()
	defer client.Close()

	schemas, err := client.ListTools(context.Background())
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
		t.Fatal("echo 도구 스키마를 찾지 못함")
	}

	// makeEchoTool이 "msg" 파라미터를 가지므로 Parameters에 있어야 한다.
	found := false
	for _, p := range echoSchema.Parameters {
		if p.Name == "msg" {
			found = true
			if p.Type != "string" {
				t.Errorf("msg 파라미터 Type 불일치: 기대 %q, 실제 %q", "string", p.Type)
			}
		}
	}
	if !found {
		t.Errorf("msg 파라미터를 스키마에서 찾지 못함; Parameters: %+v", echoSchema.Parameters)
	}
}

// TestClient_CallTool_실행_결과가_우리_결과_타입으로_온다 는 CallTool이 원격 도구 실행
// 결과를 tool.Result(텍스트·IsError=false)로 반환하는지 검증한다.
func TestClient_CallTool_실행_결과가_우리_결과_타입으로_온다(t *testing.T) {
	srv := NewServer("test-srv")
	if err := srv.RegisterTool(makeEchoTool("echo", "에코")); err != nil {
		t.Fatalf("RegisterTool 실패: %v", err)
	}

	client, cancel := connectClientToServer(t, srv)
	defer cancel()
	defer client.Close()

	args, err := json.Marshal(map[string]any{"msg": "hello client"})
	if err != nil {
		t.Fatalf("인자 직렬화 실패: %v", err)
	}

	res, err := client.CallTool(context.Background(), "echo", args)
	if err != nil {
		t.Fatalf("CallTool 실패: %v", err)
	}

	if res.IsError {
		t.Error("IsError가 true여야 하지 않음")
	}
	if res.Content != "hello client" {
		t.Errorf("Content 불일치: 기대 %q, 실제 %q", "hello client", res.Content)
	}
}

// TestClient_CallTool_에러_플래그 는 IsError=true를 반환하는 도구 호출 결과에서
// tool.Result.IsError가 true인지 검증한다.
func TestClient_CallTool_에러_플래그(t *testing.T) {
	srv := NewServer("test-srv")
	if err := srv.RegisterTool(makeErrorTool("fail-tool", "에러 도구")); err != nil {
		t.Fatalf("RegisterTool 실패: %v", err)
	}

	client, cancel := connectClientToServer(t, srv)
	defer cancel()
	defer client.Close()

	args, err := json.Marshal(map[string]any{"msg": "boom"})
	if err != nil {
		t.Fatalf("인자 직렬화 실패: %v", err)
	}

	res, err := client.CallTool(context.Background(), "fail-tool", args)
	if err != nil {
		t.Fatalf("CallTool 실패: %v", err)
	}

	if !res.IsError {
		t.Error("IsError가 true여야 함")
	}
	if res.Content != "에러: boom" {
		t.Errorf("Content 불일치: 기대 %q, 실제 %q", "에러: boom", res.Content)
	}
}

// TestClient_LoadPrompt_역할별_메시지_목록으로_온다 는 LoadPrompt가 서버에 등록된
// 프롬프트를 역할별 message.Message 목록으로 반환하는지 검증한다.
func TestClient_LoadPrompt_역할별_메시지_목록으로_온다(t *testing.T) {
	msgs := []message.Message{
		message.NewUserMessage("안녕하세요"),
		message.NewAssistantMessage("무엇을 도와드릴까요?"),
	}

	srv := NewServer("test-srv")
	if err := srv.RegisterPrompt("greeting", msgs); err != nil {
		t.Fatalf("RegisterPrompt 실패: %v", err)
	}

	client, cancel := connectClientToServer(t, srv)
	defer cancel()
	defer client.Close()

	result, err := client.LoadPrompt(context.Background(), "greeting", nil)
	if err != nil {
		t.Fatalf("LoadPrompt 실패: %v", err)
	}

	if len(result) != 2 {
		t.Fatalf("메시지 수 불일치: 기대 2, 실제 %d", len(result))
	}

	if result[0].Role != message.RoleUser {
		t.Errorf("result[0].Role 불일치: 기대 %q, 실제 %q", message.RoleUser, result[0].Role)
	}
	if result[0].Content != "안녕하세요" {
		t.Errorf("result[0].Content 불일치: 기대 %q, 실제 %q", "안녕하세요", result[0].Content)
	}

	if result[1].Role != message.RoleAssistant {
		t.Errorf("result[1].Role 불일치: 기대 %q, 실제 %q", message.RoleAssistant, result[1].Role)
	}
	if result[1].Content != "무엇을 도와드릴까요?" {
		t.Errorf("result[1].Content 불일치: 기대 %q, 실제 %q", "무엇을 도와드릴까요?", result[1].Content)
	}
}

// TestClient_Close_에러_없이_끝난다 는 Close가 오류 없이 연결을 정리하는지 검증한다.
func TestClient_Close_에러_없이_끝난다(t *testing.T) {
	srv := NewServer("test-srv")

	client, cancel := connectClientToServer(t, srv)
	defer cancel()

	if err := client.Close(); err != nil {
		t.Errorf("Close 실패: %v", err)
	}

	// 두 번 Close해도 에러가 없어야 한다(nil session 방어).
	if err := client.Close(); err != nil {
		t.Errorf("두 번째 Close 실패: %v", err)
	}
}

// TestClient_Initialize_연결_후_no_op 는 Connect 후 Initialize가 에러 없이 반환하는지 검증한다.
func TestClient_Initialize_연결_후_no_op(t *testing.T) {
	srv := NewServer("test-srv")

	client, cancel := connectClientToServer(t, srv)
	defer cancel()
	defer client.Close()

	if err := client.Initialize(context.Background()); err != nil {
		t.Errorf("Initialize 실패: %v", err)
	}
}

// TestSdkCallResultToToolResult_텍스트_연결 은 여러 TextContent를 이어 붙이는지 검증한다.
func TestSdkCallResultToToolResult_텍스트_연결(t *testing.T) {
	res := &sdkmcp.CallToolResult{
		Content: []sdkmcp.Content{
			&sdkmcp.TextContent{Text: "첫 번째"},
			&sdkmcp.TextContent{Text: " 두 번째"},
		},
		IsError: false,
	}

	result := sdkCallResultToToolResult(res)
	if result.Content != "첫 번째 두 번째" {
		t.Errorf("텍스트 연결 불일치: 기대 %q, 실제 %q", "첫 번째 두 번째", result.Content)
	}
	if result.IsError {
		t.Error("IsError가 false여야 함")
	}
}

// TestSdkPromptMessagesToMessages_역할_매핑 은 SDK PromptMessage 역할이 우리 Role로
// 올바르게 변환되는지 단위로 검증한다.
func TestSdkPromptMessagesToMessages_역할_매핑(t *testing.T) {
	pms := []*sdkmcp.PromptMessage{
		{Role: "user", Content: &sdkmcp.TextContent{Text: "질문"}},
		{Role: "assistant", Content: &sdkmcp.TextContent{Text: "답변"}},
	}

	msgs := sdkPromptMessagesToMessages(pms)

	if len(msgs) != 2 {
		t.Fatalf("메시지 수 불일치: 기대 2, 실제 %d", len(msgs))
	}
	if msgs[0].Role != message.RoleUser {
		t.Errorf("msgs[0].Role 불일치: 기대 %q, 실제 %q", message.RoleUser, msgs[0].Role)
	}
	if msgs[0].Content != "질문" {
		t.Errorf("msgs[0].Content 불일치: 기대 %q, 실제 %q", "질문", msgs[0].Content)
	}
	if msgs[1].Role != message.RoleAssistant {
		t.Errorf("msgs[1].Role 불일치: 기대 %q, 실제 %q", message.RoleAssistant, msgs[1].Role)
	}
}

// TestExtractParameters_properties_required_변환 은 JSON Schema map[string]any에서
// parameters를 올바르게 추출하는지 단위로 검증한다.
func TestExtractParameters_properties_required_변환(t *testing.T) {
	inputSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{
				"type":        "string",
				"description": "이름",
			},
			"age": map[string]any{
				"type":        "integer",
				"description": "나이",
			},
		},
		"required": []any{"name"},
	}

	params := extractParameters(inputSchema)

	if len(params) != 2 {
		t.Fatalf("파라미터 수 불일치: 기대 2, 실제 %d", len(params))
	}

	paramMap := make(map[string]tool.Parameter)
	for _, p := range params {
		paramMap[p.Name] = p
	}

	name, ok := paramMap["name"]
	if !ok {
		t.Fatal("name 파라미터를 찾지 못함")
	}
	if name.Type != "string" {
		t.Errorf("name.Type 불일치: 기대 %q, 실제 %q", "string", name.Type)
	}
	if name.Description != "이름" {
		t.Errorf("name.Description 불일치: 기대 %q, 실제 %q", "이름", name.Description)
	}
	if !name.Required {
		t.Error("name이 Required=true여야 함")
	}

	age, ok := paramMap["age"]
	if !ok {
		t.Fatal("age 파라미터를 찾지 못함")
	}
	if age.Required {
		t.Error("age가 Required=false여야 함")
	}
}
