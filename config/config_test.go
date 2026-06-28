package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoadEnv_성공경로는 유효한 .env 파일에서 다섯 자격증명을 Config로 회수하는지 검증한다.
func TestLoadEnv_성공경로(t *testing.T) {
	// 테스트용 임시 .env 파일 생성
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")
	content := `ANTHROPIC_API_KEY=test-anthropic-key
OPENAI_API_KEY=test-openai-key
TAVILY_API_KEY=test-tavily-key
SUPABASE_URL=https://test.supabase.co
SUPABASE_KEY=test-supabase-key
`
	if err := os.WriteFile(envFile, []byte(content), 0600); err != nil {
		t.Fatalf("임시 .env 파일 생성 실패: %v", err)
	}

	// 테스트 전 환경변수 초기화 (이전 테스트 또는 실제 환경의 오염 방지)
	for _, key := range []string{"ANTHROPIC_API_KEY", "OPENAI_API_KEY", "TAVILY_API_KEY", "SUPABASE_URL", "SUPABASE_KEY"} {
		t.Setenv(key, "")
	}

	cfg, err := LoadEnv(envFile)
	if err != nil {
		t.Fatalf("LoadEnv 실패: %v", err)
	}

	cases := []struct {
		field string
		got   string
		want  string
	}{
		{"AnthropicAPIKey", cfg.AnthropicAPIKey, "test-anthropic-key"},
		{"OpenAIAPIKey", cfg.OpenAIAPIKey, "test-openai-key"},
		{"TavilyAPIKey", cfg.TavilyAPIKey, "test-tavily-key"},
		{"SupabaseURL", cfg.SupabaseURL, "https://test.supabase.co"},
		{"SupabaseKey", cfg.SupabaseKey, "test-supabase-key"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("Config.%s = %q, 기대값 %q", c.field, c.got, c.want)
		}
	}
}

// TestLoadEnv_파일부재는 존재하지 않는 경로를 넘겼을 때 error를 반환하는지 검증한다.
func TestLoadEnv_파일부재(t *testing.T) {
	_, err := LoadEnv("/tmp/notexist_langgraph_go_test.env")
	if err == nil {
		t.Fatal("파일 부재 시 error를 반환해야 하는데 nil이 반환됨")
	}
}

// TestGetThreadID_키존재는 Configurable에 thread_id가 있을 때 값을 반환하는지 검증한다.
func TestGetThreadID_키존재(t *testing.T) {
	cfg := RunConfig{Configurable: map[string]any{"thread_id": "tid-123"}}
	got := GetThreadID(cfg)
	if got != "tid-123" {
		t.Errorf("GetThreadID = %q, 기대값 %q", got, "tid-123")
	}
}

// TestGetThreadID_키부재는 thread_id 키가 없을 때 빈 문자열을 반환하는지 검증한다.
func TestGetThreadID_키부재(t *testing.T) {
	cfg := RunConfig{Configurable: map[string]any{}}
	got := GetThreadID(cfg)
	if got != "" {
		t.Errorf("GetThreadID(키 없음) = %q, 기대값 빈 문자열", got)
	}
}

// TestGetThreadID_비문자열값은 thread_id 값이 문자열이 아닐 때 빈 문자열을 반환하는지 검증한다.
func TestGetThreadID_비문자열값(t *testing.T) {
	cfg := RunConfig{Configurable: map[string]any{"thread_id": 42}}
	got := GetThreadID(cfg)
	if got != "" {
		t.Errorf("GetThreadID(비문자열 값) = %q, 기대값 빈 문자열", got)
	}
}

// TestGetUserID_키존재는 Configurable에 user_id가 있을 때 값을 반환하는지 검증한다.
func TestGetUserID_키존재(t *testing.T) {
	cfg := RunConfig{Configurable: map[string]any{"user_id": "uid-abc"}}
	got := GetUserID(cfg)
	if got != "uid-abc" {
		t.Errorf("GetUserID = %q, 기대값 %q", got, "uid-abc")
	}
}

// TestGetUserID_키부재는 user_id 키가 없을 때 빈 문자열을 반환하는지 검증한다.
func TestGetUserID_키부재(t *testing.T) {
	cfg := RunConfig{Configurable: map[string]any{}}
	got := GetUserID(cfg)
	if got != "" {
		t.Errorf("GetUserID(키 없음) = %q, 기대값 빈 문자열", got)
	}
}

// TestGetConfigurable_키존재는 지정 키에 대해 (value, true)를 반환하는지 검증한다.
func TestGetConfigurable_키존재(t *testing.T) {
	cfg := RunConfig{Configurable: map[string]any{"foo": "bar"}}
	v, ok := GetConfigurable(cfg, "foo")
	if !ok {
		t.Fatal("GetConfigurable: ok = false, 기대값 true")
	}
	if v != "bar" {
		t.Errorf("GetConfigurable: value = %v, 기대값 %q", v, "bar")
	}
}

// TestGetConfigurable_키부재는 존재하지 않는 키에 대해 (nil, false)를 반환하는지 검증한다.
func TestGetConfigurable_키부재(t *testing.T) {
	cfg := RunConfig{Configurable: map[string]any{}}
	v, ok := GetConfigurable(cfg, "missing")
	if ok {
		t.Fatal("GetConfigurable(키 없음): ok = true, 기대값 false")
	}
	if v != nil {
		t.Errorf("GetConfigurable(키 없음): value = %v, 기대값 nil", v)
	}
}

// TestGetConfigurable_nil맵은 Configurable이 nil일 때 (nil, false)를 반환하는지 검증한다.
func TestGetConfigurable_nil맵(t *testing.T) {
	cfg := RunConfig{}
	v, ok := GetConfigurable(cfg, "any")
	if ok {
		t.Fatal("GetConfigurable(nil 맵): ok = true, 기대값 false")
	}
	if v != nil {
		t.Errorf("GetConfigurable(nil 맵): value = %v, 기대값 nil", v)
	}
}
