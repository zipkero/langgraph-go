// schemas.go 는 structured 패키지의 표준 출력 스키마 6종을 정의한다.
// 호출자(라우팅·평가·에이전트 응답 포맷)가 그대로 가져다 쓴다.
package structured

// BinaryScore 는 관련성·환각 평가에 쓰이는 이진 점수 스키마다.
// binary_score 는 "yes" 또는 "no" 값만 허용한다.
type BinaryScore struct {
	// BinaryScore 는 평가 결과다. 허용값: "yes", "no".
	BinaryScore string `json:"binary_score" description:"이진 평가 점수. yes 또는 no."`
}

// BinaryScoreSchema 는 BinaryScore 타입에 대한 Schema 를 생성해 반환한다.
// enum 제약(yes|no)이 반영된다.
func BinaryScoreSchema() Schema {
	return BuildSchema[BinaryScore](EnumField("binary_score", "yes", "no"))
}

// RouterChoice 는 다음 실행 노드를 결정하는 라우터 선택 스키마다.
// next 필드에 T 타입 값을 담는다.
type RouterChoice[T any] struct {
	// Next 는 다음 노드 이름이나 경로다.
	Next T `json:"next" description:"다음으로 이동할 노드 이름."`
}

// RouterChoiceSchema 는 next 필드가 허용할 string 값 목록을 받아
// RouterChoice[string] 타입의 Schema 를 생성해 반환한다.
// 허용값이 없으면 enum 제약 없이 string 타입만 정의한다.
func RouterChoiceSchema(allowedValues ...string) Schema {
	opts := []FieldOption{}
	if len(allowedValues) > 0 {
		opts = append(opts, EnumField("next", allowedValues...))
	}
	return BuildSchema[RouterChoice[string]](opts...)
}

// AgentStatus 는 에이전트 응답 포맷 스키마다.
// status 는 "input_required", "completed", "error" 중 하나이며
// message 는 부가 설명 문자열이다.
type AgentStatus struct {
	// Status 는 에이전트 상태다. 허용값: "input_required", "completed", "error".
	Status string `json:"status" description:"에이전트 상태. input_required, completed, error 중 하나."`
	// Message 는 상태에 대한 부가 설명이다.
	Message string `json:"message" description:"상태에 대한 부가 설명 메시지."`
}

// AgentStatusSchema 는 AgentStatus 타입에 대한 Schema 를 생성해 반환한다.
// status 필드에 enum 제약(input_required|completed|error)이 반영된다.
func AgentStatusSchema() Schema {
	return BuildSchema[AgentStatus](EnumField("status", "input_required", "completed", "error"))
}

// Plan 은 실행 단계 목록을 담는 계획 스키마다.
type Plan struct {
	// Steps 는 실행 단계 문자열 목록이다.
	Steps []string `json:"steps" description:"순서대로 실행할 단계 목록."`
}

// PlanSchema 는 Plan 타입에 대한 Schema 를 생성해 반환한다.
func PlanSchema() Schema {
	return BuildSchema[Plan]()
}

// ConversationalResponse 는 대화형 응답 스키마다.
type ConversationalResponse struct {
	// Response 는 대화형 응답 텍스트다.
	Response string `json:"response" description:"대화형 응답 텍스트."`
}

// ConversationalResponseSchema 는 ConversationalResponse 타입에 대한 Schema 를 생성해 반환한다.
func ConversationalResponseSchema() Schema {
	return BuildSchema[ConversationalResponse]()
}

// PlannerAction 은 PlannerResult 의 액션 유형을 나타내는 열거 타입이다.
type PlannerAction string

const (
	// PlannerActionPlan 은 새 계획을 생성하는 액션이다.
	PlannerActionPlan PlannerAction = "plan"
	// PlannerActionRespond 은 대화형 응답을 반환하는 액션이다.
	PlannerActionRespond PlannerAction = "respond"
)

// PlannerResult 는 계획/재계획용 복합 스키마다.
// Go 에는 union 이 없어 태그 구조체로 표현한다.
// Action 이 "plan" 이면 Plan 필드가, "respond" 이면 Response 필드가 채워진다.
type PlannerResult struct {
	// Action 은 플래너가 선택한 액션이다. 허용값: "plan", "respond".
	Action string `json:"action" description:"플래너 액션. plan 또는 respond."`
	// Plan 은 Action 이 plan 일 때 채워지는 실행 계획이다.
	Plan *Plan `json:"plan,omitempty" description:"Action 이 plan 일 때 채워지는 실행 단계 목록."`
	// Response 는 Action 이 respond 일 때 채워지는 대화형 응답이다.
	Response *ConversationalResponse `json:"response,omitempty" description:"Action 이 respond 일 때 채워지는 대화형 응답."`
}

// PlannerResultSchema 는 PlannerResult 타입에 대한 Schema 를 생성해 반환한다.
// action 필드에 enum 제약(plan|respond)이 반영된다.
func PlannerResultSchema() Schema {
	return BuildSchema[PlannerResult](EnumField("action", "plan", "respond"))
}
