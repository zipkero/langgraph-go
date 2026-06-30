// adapter_test.go 는 task-003 검증 조건을 단정하는 단위 테스트를 담는다.
// 인메모리 전송으로 우리 Server(task-001)에 우리 Client를 붙이는 루프백으로 검증한다.
// connectClientToServer(테스트 훅)를 재사용해 외부 의존 없이 어댑터를 검증한다.
package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/zipkero/langgraph-go/message"
	"github.com/zipkero/langgraph-go/tool"
)

// TestLoadMCPTools_원격_도구가_tool_Tool_목록으로_온다 는 LoadMCPTools가 서버에 등록된
// 도구를 []tool.Tool로 반환하는지 검증한다.
func TestLoadMCPTools_원격_도구가_tool_Tool_목록으로_온다(t *testing.T) {
	srv := NewServer("adapter-srv")
	if err := srv.RegisterTool(makeEchoTool("echo", "에코 도구")); err != nil {
		t.Fatalf("RegisterTool 실패: %v", err)
	}

	client, cancel := connectClientToServer(t, srv)
	defer cancel()
	defer client.Close()

	tools, err := LoadMCPTools(context.Background(), client)
	if err != nil {
		t.Fatalf("LoadMCPTools 실패: %v", err)
	}

	if len(tools) == 0 {
		t.Fatal("반환된 도구 목록이 비어있음")
	}

	found := false
	for _, tl := range tools {
		if tl.Name() == "echo" {
			found = true
			if tl.Description() != "에코 도구" {
				t.Errorf("설명 불일치: 기대 %q, 실제 %q", "에코 도구", tl.Description())
			}
			// Schema가 올바르게 반환되는지 확인한다.
			s := tl.Schema()
			if s.Name != "echo" {
				t.Errorf("Schema.Name 불일치: 기대 %q, 실제 %q", "echo", s.Name)
			}
		}
	}
	if !found {
		t.Errorf("등록한 도구 %q를 tool.Tool 목록에서 찾지 못함", "echo")
	}
}

// TestLoadMCPTools_Execute_원격_실행_결과가_반영된다 는 LoadMCPTools로 받은 tool.Tool의
// Execute가 원격 호출 결과를 반환하는지 검증한다(RemoteTool.Execute 원격 위임 검증).
func TestLoadMCPTools_Execute_원격_실행_결과가_반영된다(t *testing.T) {
	srv := NewServer("adapter-srv")
	if err := srv.RegisterTool(makeEchoTool("echo", "에코")); err != nil {
		t.Fatalf("RegisterTool 실패: %v", err)
	}

	client, cancel := connectClientToServer(t, srv)
	defer cancel()
	defer client.Close()

	tools, err := LoadMCPTools(context.Background(), client)
	if err != nil {
		t.Fatalf("LoadMCPTools 실패: %v", err)
	}

	// echo 도구를 찾는다.
	var echoTool tool.Tool
	for _, tl := range tools {
		if tl.Name() == "echo" {
			echoTool = tl
			break
		}
	}
	if echoTool == nil {
		t.Fatal("echo 도구를 목록에서 찾지 못함")
	}

	// Execute에 rt=nil을 전달해도 원격 호출에 위임하므로 동작해야 한다.
	args, err := json.Marshal(map[string]any{"msg": "adapter-test"})
	if err != nil {
		t.Fatalf("인자 직렬화 실패: %v", err)
	}

	res, err := echoTool.Execute(context.Background(), tool.Input(args), nil)
	if err != nil {
		t.Fatalf("Execute 실패: %v", err)
	}
	if res.IsError {
		t.Error("IsError가 true여야 하지 않음")
	}
	if res.Content != "adapter-test" {
		t.Errorf("Content 불일치: 기대 %q, 실제 %q", "adapter-test", res.Content)
	}
}

// TestLoadMCPTools_Execute_에러_플래그_원격_위임 는 원격에서 IsError=true를 반환하는
// 도구를 Execute했을 때 tool.Result.IsError가 true인지 검증한다.
func TestLoadMCPTools_Execute_에러_플래그_원격_위임(t *testing.T) {
	srv := NewServer("adapter-srv")
	if err := srv.RegisterTool(makeErrorTool("fail-tool", "에러 도구")); err != nil {
		t.Fatalf("RegisterTool 실패: %v", err)
	}

	client, cancel := connectClientToServer(t, srv)
	defer cancel()
	defer client.Close()

	tools, err := LoadMCPTools(context.Background(), client)
	if err != nil {
		t.Fatalf("LoadMCPTools 실패: %v", err)
	}

	var failTool tool.Tool
	for _, tl := range tools {
		if tl.Name() == "fail-tool" {
			failTool = tl
			break
		}
	}
	if failTool == nil {
		t.Fatal("fail-tool을 목록에서 찾지 못함")
	}

	args, err := json.Marshal(map[string]any{"msg": "boom"})
	if err != nil {
		t.Fatalf("인자 직렬화 실패: %v", err)
	}

	res, err := failTool.Execute(context.Background(), tool.Input(args), nil)
	if err != nil {
		t.Fatalf("Execute 실패: %v", err)
	}
	if !res.IsError {
		t.Error("IsError가 true여야 함")
	}
	if res.Content != "에러: boom" {
		t.Errorf("Content 불일치: 기대 %q, 실제 %q", "에러: boom", res.Content)
	}
}

// TestWrapMCPTool_단일_스키마_래핑이_tool_Tool로_동작한다 는 WrapMCPTool이 단일 스키마를
// tool.Tool로 감싸고, 그 도구를 Execute하면 원격 결과가 반환되는지 검증한다.
func TestWrapMCPTool_단일_스키마_래핑이_tool_Tool로_동작한다(t *testing.T) {
	srv := NewServer("adapter-srv")
	if err := srv.RegisterTool(makeEchoTool("echo", "에코 도구")); err != nil {
		t.Fatalf("RegisterTool 실패: %v", err)
	}

	client, cancel := connectClientToServer(t, srv)
	defer cancel()
	defer client.Close()

	// 단일 스키마를 직접 구성해 WrapMCPTool로 감싼다.
	s := tool.Schema{
		Name:        "echo",
		Description: "에코 도구",
		Parameters: []tool.Parameter{
			{Name: "msg", Type: "string", Description: "에코할 메시지", Required: true},
		},
	}
	wrapped := WrapMCPTool(client, s)

	// 인터페이스 메서드 검증.
	if wrapped.Name() != "echo" {
		t.Errorf("Name 불일치: 기대 %q, 실제 %q", "echo", wrapped.Name())
	}
	if wrapped.Description() != "에코 도구" {
		t.Errorf("Description 불일치: 기대 %q, 실제 %q", "에코 도구", wrapped.Description())
	}
	gotSchema := wrapped.Schema()
	if gotSchema.Name != "echo" {
		t.Errorf("Schema.Name 불일치: 기대 %q, 실제 %q", "echo", gotSchema.Name)
	}

	// Execute로 원격 호출이 위임되는지 검증한다.
	args, err := json.Marshal(map[string]any{"msg": "wrap-test"})
	if err != nil {
		t.Fatalf("인자 직렬화 실패: %v", err)
	}

	res, err := wrapped.Execute(context.Background(), tool.Input(args), nil)
	if err != nil {
		t.Fatalf("Execute 실패: %v", err)
	}
	if res.Content != "wrap-test" {
		t.Errorf("Content 불일치: 기대 %q, 실제 %q", "wrap-test", res.Content)
	}
}

// TestLoadMCPPrompt_프롬프트_어댑터가_메시지_목록을_반환한다 는 LoadMCPPrompt가 서버에
// 등록된 프롬프트를 역할별 []message.Message로 반환하는지 검증한다.
func TestLoadMCPPrompt_프롬프트_어댑터가_메시지_목록을_반환한다(t *testing.T) {
	msgs := []message.Message{
		message.NewUserMessage("질문입니다"),
		message.NewAssistantMessage("답변입니다"),
	}

	srv := NewServer("adapter-srv")
	if err := srv.RegisterPrompt("qa", msgs); err != nil {
		t.Fatalf("RegisterPrompt 실패: %v", err)
	}

	client, cancel := connectClientToServer(t, srv)
	defer cancel()
	defer client.Close()

	result, err := LoadMCPPrompt(context.Background(), client, "qa", nil)
	if err != nil {
		t.Fatalf("LoadMCPPrompt 실패: %v", err)
	}

	if len(result) != 2 {
		t.Fatalf("메시지 수 불일치: 기대 2, 실제 %d", len(result))
	}

	if result[0].Role != message.RoleUser {
		t.Errorf("result[0].Role 불일치: 기대 %q, 실제 %q", message.RoleUser, result[0].Role)
	}
	if result[0].Content != "질문입니다" {
		t.Errorf("result[0].Content 불일치: 기대 %q, 실제 %q", "질문입니다", result[0].Content)
	}

	if result[1].Role != message.RoleAssistant {
		t.Errorf("result[1].Role 불일치: 기대 %q, 실제 %q", message.RoleAssistant, result[1].Role)
	}
	if result[1].Content != "답변입니다" {
		t.Errorf("result[1].Content 불일치: 기대 %q, 실제 %q", "답변입니다", result[1].Content)
	}
}

// TestRemoteTool_rt_nil_전달_패닉_없음 은 RemoteTool.Execute에 rt=nil을 전달해도
// 패닉 없이 원격 호출이 위임되는지 검증한다(rt 미사용 보장).
func TestRemoteTool_rt_nil_전달_패닉_없음(t *testing.T) {
	srv := NewServer("adapter-srv")
	if err := srv.RegisterTool(makeEchoTool("echo", "에코")); err != nil {
		t.Fatalf("RegisterTool 실패: %v", err)
	}

	client, cancel := connectClientToServer(t, srv)
	defer cancel()
	defer client.Close()

	rt := &RemoteTool{
		client: client,
		schema: tool.Schema{Name: "echo", Description: "에코"},
	}

	args, err := json.Marshal(map[string]any{"msg": "no-runtime"})
	if err != nil {
		t.Fatalf("인자 직렬화 실패: %v", err)
	}

	// rt=nil을 전달해도 Execute는 원격 호출에만 위임하므로 패닉 없이 동작해야 한다.
	res, err := rt.Execute(context.Background(), tool.Input(args), nil)
	if err != nil {
		t.Fatalf("Execute 실패: %v", err)
	}
	if res.Content != "no-runtime" {
		t.Errorf("Content 불일치: 기대 %q, 실제 %q", "no-runtime", res.Content)
	}
}
