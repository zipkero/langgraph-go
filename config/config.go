// config 패키지는 실행 설정과 환경 로딩을 담당하는 무의존 최하위 leaf다.
// 모듈 내 다른 패키지를 import하지 않으며, 표준 라이브러리만 의존한다.
package config

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

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
