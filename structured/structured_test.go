package structured_test

import (
	"strings"
	"testing"

	"github.com/zipkero/langgraph-go/structured"
)

// --- BuildSchema 테스트 ---

// testPerson 은 BuildSchema 테스트용 구조체다.
type testPerson struct {
	Name  string `json:"name" description:"이름"`
	Age   int    `json:"age,omitempty" description:"나이"`
	Score string `json:"score" description:"점수"`
}

// TestBuildSchema_BasicFields 는 기본 필드 스키마 생성을 검증한다.
func TestBuildSchema_BasicFields(t *testing.T) {
	s := structured.BuildSchema[testPerson]()

	if s.Name != "testPerson" {
		t.Errorf("스키마 이름 불일치: got %q, want %q", s.Name, "testPerson")
	}

	props, ok := s.JSONSchema["properties"].(map[string]any)
	if !ok {
		t.Fatal("properties 필드가 없거나 타입이 잘못됨")
	}

	// name 필드 확인
	nameProp, ok := props["name"].(map[string]any)
	if !ok {
		t.Fatal("name 프로퍼티가 없음")
	}
	if nameProp["type"] != "string" {
		t.Errorf("name 타입 불일치: got %v, want string", nameProp["type"])
	}
	if nameProp["description"] != "이름" {
		t.Errorf("name description 불일치: got %v, want 이름", nameProp["description"])
	}

	// age 필드 확인 (omitempty → required 아님)
	_, ok = props["age"]
	if !ok {
		t.Fatal("age 프로퍼티가 없음")
	}
}

// TestBuildSchema_RequiredFields 는 required 필드 목록을 검증한다.
func TestBuildSchema_RequiredFields(t *testing.T) {
	s := structured.BuildSchema[testPerson]()

	required, ok := s.JSONSchema["required"].([]string)
	if !ok {
		t.Fatal("required 필드가 없거나 타입이 잘못됨")
	}

	// name, score 는 required. age 는 omitempty → required 아님
	has := func(name string) bool {
		for _, r := range required {
			if r == name {
				return true
			}
		}
		return false
	}

	if !has("name") {
		t.Error("name 이 required 에 없음")
	}
	if !has("score") {
		t.Error("score 가 required 에 없음")
	}
	if has("age") {
		t.Error("age(omitempty)가 required 에 있으면 안 됨")
	}
}

// TestBuildSchema_EnumField 는 EnumField 옵션이 스키마에 반영됨을 검증한다.
func TestBuildSchema_EnumField(t *testing.T) {
	s := structured.BuildSchema[testPerson](structured.EnumField("score", "A", "B", "C"))

	props, _ := s.JSONSchema["properties"].(map[string]any)
	scoreProp, ok := props["score"].(map[string]any)
	if !ok {
		t.Fatal("score 프로퍼티가 없음")
	}

	enum, ok := scoreProp["enum"].([]string)
	if !ok {
		t.Fatal("score 에 enum 이 없거나 타입이 잘못됨")
	}

	if len(enum) != 3 || enum[0] != "A" || enum[1] != "B" || enum[2] != "C" {
		t.Errorf("enum 값 불일치: got %v", enum)
	}
}

// TestBuildSchema_SliceField 는 슬라이스 타입 필드가 array 로 생성됨을 검증한다.
func TestBuildSchema_SliceField(t *testing.T) {
	type hasSlice struct {
		Items []string `json:"items"`
	}

	s := structured.BuildSchema[hasSlice]()
	props, _ := s.JSONSchema["properties"].(map[string]any)
	itemsProp, ok := props["items"].(map[string]any)
	if !ok {
		t.Fatal("items 프로퍼티가 없음")
	}
	if itemsProp["type"] != "array" {
		t.Errorf("items 타입 불일치: got %v, want array", itemsProp["type"])
	}
}

// --- ParseStructured 테스트 ---

// testOutput 은 파싱 테스트용 구조체다.
type testOutput struct {
	Result string `json:"result"`
	Count  int    `json:"count,omitempty"`
}

// TestParseStructured_Valid 는 유효한 JSON 파싱이 성공함을 검증한다.
func TestParseStructured_Valid(t *testing.T) {
	raw := `{"result":"ok","count":3}`
	out, err := structured.ParseStructured[testOutput](raw)
	if err != nil {
		t.Fatalf("파싱 실패: %v", err)
	}
	if out.Result != "ok" {
		t.Errorf("Result 불일치: got %q, want ok", out.Result)
	}
	if out.Count != 3 {
		t.Errorf("Count 불일치: got %d, want 3", out.Count)
	}
}

// TestParseStructured_Invalid 는 잘못된 JSON 이 에러를 반환함을 검증한다.
func TestParseStructured_Invalid(t *testing.T) {
	_, err := structured.ParseStructured[testOutput](`{invalid json}`)
	if err == nil {
		t.Fatal("잘못된 JSON 에 에러가 없음")
	}
}

// --- Validate 테스트 ---

// testEnum 은 Validate 검증 테스트용 enum 구조체다.
type testEnum struct {
	Status string `json:"status"`
	Label  string `json:"label,omitempty"`
}

// TestValidate_Valid 는 유효한 JSON 이 검증을 통과함을 검증한다.
func TestValidate_Valid(t *testing.T) {
	s := structured.BuildSchema[testEnum](structured.EnumField("status", "on", "off"))

	err := structured.Validate(`{"status":"on"}`, s)
	if err != nil {
		t.Errorf("유효한 JSON 이 검증 실패: %v", err)
	}
}

// TestValidate_EnumViolation 은 enum 위반이 에러를 반환함을 검증한다.
func TestValidate_EnumViolation(t *testing.T) {
	s := structured.BuildSchema[testEnum](structured.EnumField("status", "on", "off"))

	err := structured.Validate(`{"status":"maybe"}`, s)
	if err == nil {
		t.Fatal("enum 위반에 에러가 없음")
	}
	if !strings.Contains(err.Error(), "허용값") {
		t.Errorf("에러 메시지에 '허용값'이 없음: %v", err)
	}
}

// TestValidate_RequiredMissing 은 필수 필드 누락이 에러를 반환함을 검증한다.
func TestValidate_RequiredMissing(t *testing.T) {
	s := structured.BuildSchema[testEnum](structured.EnumField("status", "on", "off"))

	err := structured.Validate(`{"label":"hello"}`, s)
	if err == nil {
		t.Fatal("필수 필드 누락에 에러가 없음")
	}
	if !strings.Contains(err.Error(), "필수 필드 누락") {
		t.Errorf("에러 메시지에 '필수 필드 누락'이 없음: %v", err)
	}
}

// TestValidate_InvalidJSON 은 잘못된 JSON 이 에러를 반환함을 검증한다.
func TestValidate_InvalidJSON(t *testing.T) {
	s := structured.BuildSchema[testEnum]()
	err := structured.Validate(`not json`, s)
	if err == nil {
		t.Fatal("잘못된 JSON 에 에러가 없음")
	}
}

// --- 표준 스키마 6종 테스트 ---

// TestBinaryScoreSchema 는 BinaryScore 스키마 생성 및 사용을 검증한다.
func TestBinaryScoreSchema(t *testing.T) {
	s := structured.BinaryScoreSchema()

	if s.Name != "BinaryScore" {
		t.Errorf("스키마 이름 불일치: got %q, want BinaryScore", s.Name)
	}

	// 유효한 값 통과
	if err := structured.Validate(`{"binary_score":"yes"}`, s); err != nil {
		t.Errorf("yes 값이 검증 실패: %v", err)
	}
	if err := structured.Validate(`{"binary_score":"no"}`, s); err != nil {
		t.Errorf("no 값이 검증 실패: %v", err)
	}

	// 잘못된 값 거부
	if err := structured.Validate(`{"binary_score":"maybe"}`, s); err == nil {
		t.Error("maybe 값이 통과하면 안 됨")
	}

	// 파싱 확인
	bs, err := structured.ParseStructured[structured.BinaryScore](`{"binary_score":"yes"}`)
	if err != nil {
		t.Fatalf("BinaryScore 파싱 실패: %v", err)
	}
	if bs.BinaryScore != "yes" {
		t.Errorf("BinaryScore 값 불일치: got %q, want yes", bs.BinaryScore)
	}
}

// TestRouterChoiceSchema 는 RouterChoice 스키마 생성 및 사용을 검증한다.
func TestRouterChoiceSchema(t *testing.T) {
	s := structured.RouterChoiceSchema("search", "generate", "end")

	if s.Name != "RouterChoice[string]" {
		t.Errorf("스키마 이름 불일치: got %q", s.Name)
	}

	// 유효한 값 통과
	if err := structured.Validate(`{"next":"search"}`, s); err != nil {
		t.Errorf("search 값이 검증 실패: %v", err)
	}

	// 허용값 외 거부
	if err := structured.Validate(`{"next":"unknown"}`, s); err == nil {
		t.Error("허용값 외 노드가 통과하면 안 됨")
	}

	// 파싱 확인
	rc, err := structured.ParseStructured[structured.RouterChoice[string]](`{"next":"generate"}`)
	if err != nil {
		t.Fatalf("RouterChoice 파싱 실패: %v", err)
	}
	if rc.Next != "generate" {
		t.Errorf("RouterChoice.Next 불일치: got %q, want generate", rc.Next)
	}
}

// TestAgentStatusSchema 는 AgentStatus 스키마 생성 및 사용을 검증한다.
func TestAgentStatusSchema(t *testing.T) {
	s := structured.AgentStatusSchema()

	if s.Name != "AgentStatus" {
		t.Errorf("스키마 이름 불일치: got %q", s.Name)
	}

	// 유효한 상태 통과
	for _, status := range []string{"input_required", "completed", "error"} {
		raw := `{"status":"` + status + `","message":"테스트"}`
		if err := structured.Validate(raw, s); err != nil {
			t.Errorf("%q 상태가 검증 실패: %v", status, err)
		}
	}

	// 잘못된 상태 거부
	if err := structured.Validate(`{"status":"unknown","message":""}`, s); err == nil {
		t.Error("unknown 상태가 통과하면 안 됨")
	}

	// 파싱 확인
	as, err := structured.ParseStructured[structured.AgentStatus](`{"status":"completed","message":"완료"}`)
	if err != nil {
		t.Fatalf("AgentStatus 파싱 실패: %v", err)
	}
	if as.Status != "completed" {
		t.Errorf("AgentStatus.Status 불일치: got %q, want completed", as.Status)
	}
	if as.Message != "완료" {
		t.Errorf("AgentStatus.Message 불일치: got %q, want 완료", as.Message)
	}
}

// TestPlanSchema 는 Plan 스키마 생성 및 사용을 검증한다.
func TestPlanSchema(t *testing.T) {
	s := structured.PlanSchema()

	if s.Name != "Plan" {
		t.Errorf("스키마 이름 불일치: got %q", s.Name)
	}

	// 유효한 JSON 통과
	raw := `{"steps":["1단계","2단계","3단계"]}`
	if err := structured.Validate(raw, s); err != nil {
		t.Errorf("Plan 검증 실패: %v", err)
	}

	// 파싱 확인
	p, err := structured.ParseStructured[structured.Plan](raw)
	if err != nil {
		t.Fatalf("Plan 파싱 실패: %v", err)
	}
	if len(p.Steps) != 3 || p.Steps[0] != "1단계" {
		t.Errorf("Plan.Steps 불일치: got %v", p.Steps)
	}
}

// TestConversationalResponseSchema 는 ConversationalResponse 스키마 생성 및 사용을 검증한다.
func TestConversationalResponseSchema(t *testing.T) {
	s := structured.ConversationalResponseSchema()

	if s.Name != "ConversationalResponse" {
		t.Errorf("스키마 이름 불일치: got %q", s.Name)
	}

	raw := `{"response":"안녕하세요"}`
	if err := structured.Validate(raw, s); err != nil {
		t.Errorf("ConversationalResponse 검증 실패: %v", err)
	}

	// 파싱 확인
	cr, err := structured.ParseStructured[structured.ConversationalResponse](raw)
	if err != nil {
		t.Fatalf("ConversationalResponse 파싱 실패: %v", err)
	}
	if cr.Response != "안녕하세요" {
		t.Errorf("ConversationalResponse.Response 불일치: got %q", cr.Response)
	}
}

// TestPlannerResultSchema 는 PlannerResult 스키마 생성 및 사용을 검증한다.
func TestPlannerResultSchema(t *testing.T) {
	s := structured.PlannerResultSchema()

	if s.Name != "PlannerResult" {
		t.Errorf("스키마 이름 불일치: got %q", s.Name)
	}

	// plan 액션 통과
	rawPlan := `{"action":"plan","plan":{"steps":["step1","step2"]}}`
	if err := structured.Validate(rawPlan, s); err != nil {
		t.Errorf("plan 액션 검증 실패: %v", err)
	}

	// respond 액션 통과
	rawRespond := `{"action":"respond","response":{"response":"대화 응답"}}`
	if err := structured.Validate(rawRespond, s); err != nil {
		t.Errorf("respond 액션 검증 실패: %v", err)
	}

	// 잘못된 액션 거부
	if err := structured.Validate(`{"action":"unknown"}`, s); err == nil {
		t.Error("unknown 액션이 통과하면 안 됨")
	}

	// plan 액션 파싱 확인
	steps := []string{"step1", "step2"}
	planVal := &structured.Plan{Steps: steps}
	pr := structured.PlannerResult{Action: "plan", Plan: planVal}
	if pr.Action != "plan" {
		t.Errorf("PlannerResult.Action 불일치: got %q", pr.Action)
	}
	if pr.Plan == nil || len(pr.Plan.Steps) != 2 {
		t.Errorf("PlannerResult.Plan 불일치: got %+v", pr.Plan)
	}
}

// TestValidator_Integration 은 Validator 타입을 통한 검증을 확인한다.
func TestValidator_Integration(t *testing.T) {
	s := structured.BinaryScoreSchema()
	v := structured.NewValidator(s)

	if err := v.Validate(`{"binary_score":"yes"}`); err != nil {
		t.Errorf("Validator 유효 케이스 실패: %v", err)
	}
	if err := v.Validate(`{"binary_score":"invalid"}`); err == nil {
		t.Error("Validator enum 위반이 통과하면 안 됨")
	}
}
