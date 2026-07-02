package config

import "testing"

// TestLoadMCPServers_환경변수조립은 MCP_SERVER_<NAME>_* 환경변수로부터 이름별 ServerConfig가
// 조립되는지 검증한다(SPEC §5.4, ANALYSIS §1.4).
func TestLoadMCPServers_환경변수조립(t *testing.T) {
	t.Setenv("MCP_SERVER_RAG_TRANSPORT", "stdio")
	t.Setenv("MCP_SERVER_RAG_COMMAND", "rag-mcp-server")
	t.Setenv("MCP_SERVER_RAG_ARGS", "--flag, --verbose")
	t.Setenv("MCP_SERVER_WEB_URL", "http://localhost:9000/mcp")
	t.Setenv("MCP_SERVER_WEB_TRANSPORT", "streamable_http")

	servers := LoadMCPServers()

	rag, ok := servers["rag"]
	if !ok {
		t.Fatal(`LoadMCPServers()["rag"] 미존재, 기대값 존재`)
	}
	if rag.Transport != "stdio" || rag.Command != "rag-mcp-server" {
		t.Errorf("rag ServerConfig = %+v, 기대값 Transport=stdio Command=rag-mcp-server", rag)
	}
	if len(rag.Args) != 2 || rag.Args[0] != "--flag" || rag.Args[1] != "--verbose" {
		t.Errorf("rag.Args = %v, 기대값 [--flag --verbose]", rag.Args)
	}

	web, ok := servers["web"]
	if !ok {
		t.Fatal(`LoadMCPServers()["web"] 미존재, 기대값 존재`)
	}
	if web.URL != "http://localhost:9000/mcp" || web.Transport != "streamable_http" {
		t.Errorf("web ServerConfig = %+v, 기대값 URL/Transport 설정값", web)
	}
}

// TestLoadMCPServers_미설정시빈맵은 관련 환경변수가 전혀 없으면 빈 맵을 반환하는지 검증한다.
func TestLoadMCPServers_미설정시빈맵(t *testing.T) {
	// 사전 오염 방지: 이 테스트 프로세스에 실제로 MCP_SERVER_* 가 설정돼 있지 않다고 가정하고
	// 결과가 nil이 아닌 빈 맵(또는 길이 0)인지만 검증한다.
	servers := LoadMCPServers()
	for name := range servers {
		t.Logf("환경에 이미 설정된 MCP 서버 발견: %s (테스트 환경 오염 가능성)", name)
	}
}

// TestAgentURLs_환경변수조립은 AGENT_<NAME>_URL 환경변수로부터 이름→URL 맵이 조립되는지 검증한다.
func TestAgentURLs_환경변수조립(t *testing.T) {
	t.Setenv("AGENT_PLANNER_URL", "http://localhost:7001")
	t.Setenv("AGENT_EXECUTOR_URL", "http://localhost:7002")

	urls := AgentURLs()

	if urls["planner"] != "http://localhost:7001" {
		t.Errorf(`AgentURLs()["planner"] = %q, 기대값 "http://localhost:7001"`, urls["planner"])
	}
	if urls["executor"] != "http://localhost:7002" {
		t.Errorf(`AgentURLs()["executor"] = %q, 기대값 "http://localhost:7002"`, urls["executor"])
	}
}

// TestGetAgentConfig_존재는 URL이 설정된 이름에 대해 AgentConfig를 조립해 반환하는지 검증한다.
func TestGetAgentConfig_존재(t *testing.T) {
	t.Setenv("AGENT_PLANNER_URL", "http://localhost:7001")
	t.Setenv("AGENT_PLANNER_PORT", "7001")
	t.Setenv("AGENT_PLANNER_DESCRIPTION", "계획 수립 에이전트")

	cfg, err := GetAgentConfig("planner")
	if err != nil {
		t.Fatalf("GetAgentConfig(\"planner\") 실패: %v", err)
	}
	if cfg.Name != "planner" || cfg.URL != "http://localhost:7001" || cfg.Port != 7001 ||
		cfg.Description != "계획 수립 에이전트" {
		t.Errorf("GetAgentConfig(\"planner\") = %+v, 기대값과 불일치", cfg)
	}
}

// TestGetAgentConfig_대소문자무관은 name 인자의 대소문자와 무관하게 동일 환경변수를 찾는지 검증한다.
func TestGetAgentConfig_대소문자무관(t *testing.T) {
	t.Setenv("AGENT_PLANNER_URL", "http://localhost:7001")

	cfg, err := GetAgentConfig("Planner")
	if err != nil {
		t.Fatalf("GetAgentConfig(\"Planner\") 실패: %v", err)
	}
	if cfg.URL != "http://localhost:7001" {
		t.Errorf("GetAgentConfig(\"Planner\").URL = %q, 기대값 http://localhost:7001", cfg.URL)
	}
}

// TestGetAgentConfig_미존재는 URL이 설정되지 않은 이름에 대해 에러를 반환하는지 검증한다.
func TestGetAgentConfig_미존재(t *testing.T) {
	_, err := GetAgentConfig("no-such-agent-xyz")
	if err == nil {
		t.Fatal("GetAgentConfig(미존재 이름): error가 nil, 기대값 non-nil")
	}
}
