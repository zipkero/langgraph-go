// config 패키지는 실행 설정과 환경 로딩을 담당하는 무의존 최하위 leaf다.
// 모듈 내 다른 패키지를 import하지 않으며, 표준 라이브러리만 의존한다.
package config

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
)

// mcpServerEnvPrefix / mcpServerEnvSuffixes 는 LoadMCPServers가 스캔하는 환경변수 명명 규칙이다.
// 형식: MCP_SERVER_<NAME>_<SUFFIX> (예: MCP_SERVER_RAG_URL). <NAME>은 라이브러리가 고정하지 않고
// 존재하는 환경변수에서 동적으로 발견한다(README §24: 특정 이름을 고정하지 않음).
const mcpServerEnvPrefix = "MCP_SERVER_"

var mcpServerEnvSuffixes = []string{"_TRANSPORT", "_COMMAND", "_ARGS", "_URL"}

// agentEnvPrefix 는 AgentURLs/GetAgentConfig가 스캔하는 환경변수 명명 규칙의 접두사다.
// 형식: AGENT_<NAME>_<SUFFIX> (예: AGENT_RAG_URL). 필수 환경변수가 아니라 기본값(빈 값)이 있는
// 선택적 오버라이드이며, `.env.example`에는 선언하지 않는다(README §24).
const agentEnvPrefix = "AGENT_"

// Config 는 애플리케이션 실행에 필요한 자격증명과 설정을 담는다.
// 챗은 Anthropic(Claude), 임베딩은 OpenAI를 사용하므로 두 키를 모두 보관한다.
type Config struct {
	AnthropicAPIKey string
	OpenAIAPIKey    string
	TavilyAPIKey    string
	SupabaseURL     string
	SupabaseKey     string
	ModelConfig     ModelConfig
	ServerConfigs   map[string]ServerConfig
	AgentConfigs    map[string]AgentConfig
}

// RunConfig 는 그래프/에이전트 실행 시 전달되는 실행별 설정이다.
// Configurable 은 thread_id, user_id 등 임의 식별자와 실행 옵션을 담는 범용 맵이다.
type RunConfig struct {
	Configurable map[string]any
}

// ModelConfig 는 LLM 모델 호출에 사용하는 설정이다.
type ModelConfig struct {
	Model       string
	Temperature float64
}

// ServerConfig 는 MCP/A2A 엔드포인트 설정을 담는다.
// 어셈블리 함수(LoadMCPServers 등)는 Phase 7에서 추가된다.
type ServerConfig struct {
	Transport string
	Command   string
	Args      []string
	URL       string
}

// AgentConfig 는 원격 에이전트의 엔드포인트 설정을 담는다.
// 어셈블리 함수(GetAgentConfig, AgentURLs 등)는 Phase 7에서 추가된다.
type AgentConfig struct {
	Name        string
	URL         string
	Port        int
	Description string
}

// LoadEnv 는 path에 지정된 .env 파일에서 KEY=VALUE 형식으로 환경변수를 읽어
// 프로세스 환경에 반영한 뒤 Config를 채워 반환한다.
// 파일이 없거나 열 수 없을 때 error를 반환한다.
// 고급 .env 문법(따옴표 감싸기, 멀티라인, export 키워드 등)은 처리하지 않는다.
func LoadEnv(path string) (Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return Config{}, fmt.Errorf("config: .env 파일 열기 실패 (%s): %w", path, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// 빈 줄과 주석(#으로 시작)은 건너뛴다
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.IndexByte(line, '=')
		if idx < 1 {
			// '='가 없거나 키가 비어 있는 줄은 무시
			continue
		}
		key := strings.TrimSpace(line[:idx])
		value := strings.TrimSpace(line[idx+1:])
		// 이미 설정된 환경변수는 덮어쓰지 않는다
		if os.Getenv(key) == "" {
			os.Setenv(key, value)
		}
	}
	if err := scanner.Err(); err != nil {
		return Config{}, fmt.Errorf("config: .env 파일 읽기 실패 (%s): %w", path, err)
	}

	cfg := Config{
		AnthropicAPIKey: os.Getenv("ANTHROPIC_API_KEY"),
		OpenAIAPIKey:    os.Getenv("OPENAI_API_KEY"),
		TavilyAPIKey:    os.Getenv("TAVILY_API_KEY"),
		SupabaseURL:     os.Getenv("SUPABASE_URL"),
		SupabaseKey:     os.Getenv("SUPABASE_KEY"),
	}
	return cfg, nil
}

// GetThreadID 는 cfg.Configurable에서 "thread_id" 키의 값을 문자열로 반환한다.
// 키가 없거나 값이 문자열이 아니면 빈 문자열을 반환한다.
func GetThreadID(cfg RunConfig) string {
	v, ok := GetConfigurable(cfg, "thread_id")
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

// GetUserID 는 cfg.Configurable에서 "user_id" 키의 값을 문자열로 반환한다.
// 키가 없거나 값이 문자열이 아니면 빈 문자열을 반환한다.
func GetUserID(cfg RunConfig) string {
	v, ok := GetConfigurable(cfg, "user_id")
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

// GetConfigurable 은 cfg.Configurable에서 key에 해당하는 값과 존재 여부를 반환한다.
// 키가 없으면 (nil, false)를 반환한다.
func GetConfigurable(cfg RunConfig, key string) (any, bool) {
	if cfg.Configurable == nil {
		return nil, false
	}
	v, ok := cfg.Configurable[key]
	return v, ok
}

// LoadMCPServers 는 프로세스 환경변수에서 MCP_SERVER_<NAME>_* 형식의 값을 스캔해
// 이름별 ServerConfig 맵을 조립한다. <NAME>은 고정하지 않고 존재하는 환경변수만큼 동적으로
// 발견하며(예: MCP_SERVER_RAG_URL), 반환 타입은 config 자체 타입(ServerConfig)이라
// config는 mcp 등 상위 패키지를 import하지 않는 leaf로 유지된다(SPEC §5.4, ANALYSIS §1.4).
func LoadMCPServers() map[string]ServerConfig {
	names := discoverEnvNames(mcpServerEnvPrefix, mcpServerEnvSuffixes)
	servers := make(map[string]ServerConfig, len(names))
	for _, name := range names {
		upper := strings.ToUpper(name)
		servers[name] = ServerConfig{
			Transport: os.Getenv(mcpServerEnvPrefix + upper + "_TRANSPORT"),
			Command:   os.Getenv(mcpServerEnvPrefix + upper + "_COMMAND"),
			Args:      splitEnvList(os.Getenv(mcpServerEnvPrefix + upper + "_ARGS")),
			URL:       os.Getenv(mcpServerEnvPrefix + upper + "_URL"),
		}
	}
	return servers
}

// AgentURLs 는 프로세스 환경변수에서 AGENT_<NAME>_URL 형식의 값을 스캔해 이름→URL 맵을 조립한다.
// <NAME>은 라이브러리가 고정하지 않으며(RAG/WEB/FILE 등은 다운스트림 예시일 뿐), 필수 환경변수가 아닌
// 기본값 있는 선택적 오버라이드다(README §24).
func AgentURLs() map[string]string {
	names := discoverEnvNames(agentEnvPrefix, []string{"_URL"})
	urls := make(map[string]string, len(names))
	for _, name := range names {
		urls[name] = os.Getenv(agentEnvPrefix + strings.ToUpper(name) + "_URL")
	}
	return urls
}

// GetAgentConfig 는 name(대소문자 무관)에 대응하는 AGENT_<NAME>_URL/PORT/DESCRIPTION 환경변수로
// AgentConfig를 조립해 반환한다. URL이 설정돼 있지 않으면 에러를 반환한다.
func GetAgentConfig(name string) (AgentConfig, error) {
	upper := strings.ToUpper(name)
	url := os.Getenv(agentEnvPrefix + upper + "_URL")
	if url == "" {
		return AgentConfig{}, fmt.Errorf("config: 에이전트 설정을 찾을 수 없습니다: %s (%s%s_URL 미설정)",
			name, agentEnvPrefix, upper)
	}
	port := 0
	if raw := os.Getenv(agentEnvPrefix + upper + "_PORT"); raw != "" {
		if p, err := strconv.Atoi(raw); err == nil {
			port = p
		}
	}
	return AgentConfig{
		Name:        name,
		URL:         url,
		Port:        port,
		Description: os.Getenv(agentEnvPrefix + upper + "_DESCRIPTION"),
	}, nil
}

// discoverEnvNames 는 os.Environ()을 스캔해 "<prefix><NAME><suffix>" 형식의 키에서
// <NAME>을 추출한다(임의 suffix 중 하나라도 매치하면 채택). 중복 없이 소문자로 정규화해
// 정렬된 순서로 반환한다.
func discoverEnvNames(prefix string, suffixes []string) []string {
	seen := map[string]bool{}
	for _, kv := range os.Environ() {
		idx := strings.IndexByte(kv, '=')
		if idx < 0 {
			continue
		}
		key := kv[:idx]
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		rest := strings.TrimPrefix(key, prefix)
		for _, suffix := range suffixes {
			if strings.HasSuffix(rest, suffix) {
				name := strings.TrimSuffix(rest, suffix)
				if name != "" {
					seen[strings.ToLower(name)] = true
				}
				break
			}
		}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// splitEnvList 는 콤마로 구분된 환경변수 값을 공백 트리밍 후 슬라이스로 분리한다.
// 빈 문자열이면 nil을 반환하고, 빈 항목은 건너뛴다.
func splitEnvList(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}
