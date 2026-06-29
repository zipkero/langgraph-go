// planner_test.go 는 Plan/Replan/RecordStep/PlannerState/Step 의 동작을
// llm.StubClient(structured 응답 시퀀스)로 결정적으로 검증한다.
// 네트워크·API 키 없이 수행된다.
// SPEC §5.7, ANALYSIS §2·§5.6 참조.
package multiagent

import (
	"context"
	"testing"

	"github.com/zipkero/langgraph-go/llm"
	"github.com/zipkero/langgraph-go/message"
	"github.com/zipkero/langgraph-go/structured"
	"github.com/zipkero/langgraph-go/tool"
)

// ============================================================
// 시퀀스 stub — 호출 순서에 따라 미리 지정된 응답을 순차 반환
// ============================================================

// plannerSeqClient 는 Structured 호출 순서에 따라 미리 지정된 응답을 순차 반환하는
// 테스트 전용 llm.Client 구현체다.
// plan→record→replan→respond 루프를 결정적으로 검증하기 위해 사용한다.
type plannerSeqClient struct {
	responses []llm.StubResponse
	callIndex int
	model     string
}

func newPlannerSeqClient(responses ...llm.StubResponse) *plannerSeqClient {
	return &plannerSeqClient{
		responses: responses,
		model:     "stub-planner",
	}
}

func (p *plannerSeqClient) currentResponse() llm.StubResponse {
	if p.callIndex >= len(p.responses) {
		// 마지막 응답을 반복한다
		return p.responses[len(p.responses)-1]
	}
	resp := p.responses[p.callIndex]
	p.callIndex++
	return resp
}

func (p *plannerSeqClient) Chat(_ context.Context, _ llm.ChatRequest) (llm.ChatResponse, error) {
	resp := p.currentResponse()
	if resp.Err != nil {
		return llm.ChatResponse{}, resp.Err
	}
	return llm.ChatResponse{
		Message:      resp.Message,
		ToolCalls:    resp.ToolCalls,
		FinishReason: resp.FinishReason,
	}, nil
}

func (p *plannerSeqClient) ChatStream(_ context.Context, _ llm.ChatRequest) (<-chan llm.ChatEvent, error) {
	ch := make(chan llm.ChatEvent)
	close(ch)
	return ch, nil
}

func (p *plannerSeqClient) Structured(_ context.Context, _ llm.ChatRequest, _ structured.Schema) (any, error) {
	resp := p.currentResponse()
	if resp.Err != nil {
		return nil, resp.Err
	}
	if resp.StructuredValue != nil {
		return resp.StructuredValue, nil
	}
	return nil, nil
}

func (p *plannerSeqClient) BindTools(tools []tool.Schema) llm.Client {
	clone := *p
	return &clone
}

func (p *plannerSeqClient) ParseToolCalls(resp llm.ChatResponse) []message.ToolCall {
	return resp.ToolCalls
}

func (p *plannerSeqClient) WithModel(name string) llm.Client {
	clone := *p
	clone.model = name
	return &clone
}

func (p *plannerSeqClient) ModelName() string { return p.model }

// ============================================================
// 테스트 헬퍼
// ============================================================

// makePlanResult 는 Action=plan 인 PlannerResult 를 생성한다.
func makePlanResult(steps ...string) structured.PlannerResult {
	return structured.PlannerResult{
		Action: string(structured.PlannerActionPlan),
		Plan:   &structured.Plan{Steps: steps},
	}
}

// makeRespondResult 는 Action=respond 인 PlannerResult 를 생성한다.
func makeRespondResult(response string) structured.PlannerResult {
	return structured.PlannerResult{
		Action:   string(structured.PlannerActionRespond),
		Response: &structured.ConversationalResponse{Response: response},
	}
}

// ============================================================
// Plan 테스트
// ============================================================

// TestPlan_ActionPlan 은 Action=plan 일 때 Steps 가 채워진 PlannerResult 를 반환하는지 검증한다.
func TestPlan_ActionPlan(t *testing.T) {
	wantSteps := []string{"1단계 실행", "2단계 실행", "3단계 실행"}

	client := newPlannerSeqClient(llm.StubResponse{
		StructuredValue: makePlanResult(wantSteps...),
	})

	result, err := Plan(context.Background(), client, "세 단계로 작업을 실행하라")
	if err != nil {
		t.Fatalf("Plan 실패: %v", err)
	}

	if result.Action != string(structured.PlannerActionPlan) {
		t.Errorf("Action: 기대 %q, 실제 %q", structured.PlannerActionPlan, result.Action)
	}
	if result.Plan == nil {
		t.Fatal("Plan 필드가 nil — Action=plan 이면 Plan 이 채워져야 한다")
	}
	if len(result.Plan.Steps) != len(wantSteps) {
		t.Fatalf("Steps 개수: 기대 %d, 실제 %d", len(wantSteps), len(result.Plan.Steps))
	}
	for i, want := range wantSteps {
		if result.Plan.Steps[i] != want {
			t.Errorf("Steps[%d]: 기대 %q, 실제 %q", i, want, result.Plan.Steps[i])
		}
	}
}

// TestPlan_ActionRespond 은 Action=respond 일 때 Response 가 채워진 PlannerResult 를 반환하는지 검증한다.
func TestPlan_ActionRespond(t *testing.T) {
	wantResponse := "바로 응답합니다"

	client := newPlannerSeqClient(llm.StubResponse{
		StructuredValue: makeRespondResult(wantResponse),
	})

	result, err := Plan(context.Background(), client, "바로 답하라")
	if err != nil {
		t.Fatalf("Plan 실패: %v", err)
	}

	if result.Action != string(structured.PlannerActionRespond) {
		t.Errorf("Action: 기대 %q, 실제 %q", structured.PlannerActionRespond, result.Action)
	}
	if result.Response == nil {
		t.Fatal("Response 필드가 nil — Action=respond 이면 Response 가 채워져야 한다")
	}
	if result.Response.Response != wantResponse {
		t.Errorf("Response.Response: 기대 %q, 실제 %q", wantResponse, result.Response.Response)
	}
}

// TestPlan_ClientError 는 llm.Client.Structured 가 에러를 반환하면 Plan 이 에러를 전파하는지 검증한다.
func TestPlan_ClientError(t *testing.T) {
	client := newPlannerSeqClient(llm.StubResponse{
		Err: &testError{"stub 에러"},
	})

	_, err := Plan(context.Background(), client, "입력")
	if err == nil {
		t.Fatal("에러 stub 인데 Plan 이 nil 에러를 반환했습니다")
	}
}

// testError 는 테스트용 에러 타입이다.
type testError struct{ msg string }

func (e *testError) Error() string { return e.msg }

// ============================================================
// Replan 테스트
// ============================================================

// TestReplan_ActionPlan 은 Replan 이 Action=plan 인 PlannerResult 를 반환하는지 검증한다.
func TestReplan_ActionPlan(t *testing.T) {
	wantSteps := []string{"수정된 2단계", "수정된 3단계"}

	client := newPlannerSeqClient(llm.StubResponse{
		StructuredValue: makePlanResult(wantSteps...),
	})

	pastSteps := []Step{
		{Task: "1단계", Result: "1단계 완료"},
	}
	remainingPlan := []string{"원래 2단계", "원래 3단계"}

	result, err := Replan(context.Background(), client, remainingPlan, pastSteps)
	if err != nil {
		t.Fatalf("Replan 실패: %v", err)
	}

	if result.Action != string(structured.PlannerActionPlan) {
		t.Errorf("Action: 기대 %q, 실제 %q", structured.PlannerActionPlan, result.Action)
	}
	if result.Plan == nil || len(result.Plan.Steps) != len(wantSteps) {
		t.Errorf("Steps 개수: 기대 %d", len(wantSteps))
	}
}

// TestReplan_ActionRespond 은 Replan 이 빈 plan(Action=respond) 신호를 반환하는지 검증한다.
func TestReplan_ActionRespond(t *testing.T) {
	wantResponse := "최종 응답"

	client := newPlannerSeqClient(llm.StubResponse{
		StructuredValue: makeRespondResult(wantResponse),
	})

	result, err := Replan(context.Background(), client, []string{}, []Step{
		{Task: "마지막 단계", Result: "마지막 단계 완료"},
	})
	if err != nil {
		t.Fatalf("Replan 실패: %v", err)
	}

	if result.Action != string(structured.PlannerActionRespond) {
		t.Errorf("Action: 기대 %q, 실제 %q", structured.PlannerActionRespond, result.Action)
	}
	if result.Response == nil || result.Response.Response != wantResponse {
		t.Errorf("Response: 기대 %q", wantResponse)
	}
}

// ============================================================
// RecordStep 테스트
// ============================================================

// TestRecordStep_PastStepsAppended 는 RecordStep 후 PastSteps 에 Step 이 누적되는지 검증한다.
func TestRecordStep_PastStepsAppended(t *testing.T) {
	st := PlannerState{
		Input:     "원본 입력",
		Plan:      []string{"1단계", "2단계", "3단계"},
		PastSteps: []Step{},
		Response:  "",
	}

	step := Step{Task: "1단계", Result: "1단계 결과"}
	update := RecordStep(st, step)

	// StateUpdate 에 plannerStateKey 가 있어야 한다.
	raw, ok := update[plannerStateKey]
	if !ok {
		t.Fatalf("StateUpdate 에 %q 키가 없습니다", plannerStateKey)
	}

	newSt, ok := raw.(PlannerState)
	if !ok {
		t.Fatalf("StateUpdate[%q] 타입이 PlannerState 가 아닙니다: %T", plannerStateKey, raw)
	}

	// PastSteps 에 step 이 누적되어야 한다.
	if len(newSt.PastSteps) != 1 {
		t.Fatalf("PastSteps 개수: 기대 1, 실제 %d", len(newSt.PastSteps))
	}
	if newSt.PastSteps[0].Task != step.Task {
		t.Errorf("PastSteps[0].Task: 기대 %q, 실제 %q", step.Task, newSt.PastSteps[0].Task)
	}
	if newSt.PastSteps[0].Result != step.Result {
		t.Errorf("PastSteps[0].Result: 기대 %q, 실제 %q", step.Result, newSt.PastSteps[0].Result)
	}
}

// TestRecordStep_PlanConsumed 는 RecordStep 후 Plan 의 첫 번째 항목이 소비되는지 검증한다.
func TestRecordStep_PlanConsumed(t *testing.T) {
	st := PlannerState{
		Plan:      []string{"A단계", "B단계", "C단계"},
		PastSteps: []Step{},
	}

	update := RecordStep(st, Step{Task: "A단계", Result: "A 완료"})

	newSt := update[plannerStateKey].(PlannerState)
	if len(newSt.Plan) != 2 {
		t.Fatalf("Plan 남은 개수: 기대 2, 실제 %d", len(newSt.Plan))
	}
	if newSt.Plan[0] != "B단계" {
		t.Errorf("Plan[0]: 기대 'B단계', 실제 %q", newSt.Plan[0])
	}
}

// TestRecordStep_LastStep 은 마지막 Plan 항목 소비 후 Plan 이 비는지 검증한다.
func TestRecordStep_LastStep(t *testing.T) {
	st := PlannerState{
		Plan:      []string{"마지막단계"},
		PastSteps: []Step{},
	}

	update := RecordStep(st, Step{Task: "마지막단계", Result: "완료"})

	newSt := update[plannerStateKey].(PlannerState)
	if len(newSt.Plan) != 0 {
		t.Errorf("마지막 단계 소비 후 Plan: 기대 빈 슬라이스, 실제 %v", newSt.Plan)
	}
	if len(newSt.PastSteps) != 1 {
		t.Errorf("PastSteps 개수: 기대 1, 실제 %d", len(newSt.PastSteps))
	}
}

// TestRecordStep_MultipleAccumulation 은 여러 RecordStep 호출 후 PastSteps 가 순차 누적되는지 검증한다.
func TestRecordStep_MultipleAccumulation(t *testing.T) {
	st := PlannerState{
		Plan:      []string{"1단계", "2단계", "3단계"},
		PastSteps: []Step{},
	}

	step1 := Step{Task: "1단계", Result: "1결과"}
	update1 := RecordStep(st, step1)
	st1 := update1[plannerStateKey].(PlannerState)

	step2 := Step{Task: "2단계", Result: "2결과"}
	update2 := RecordStep(st1, step2)
	st2 := update2[plannerStateKey].(PlannerState)

	// 두 번 누적 후 PastSteps 개수
	if len(st2.PastSteps) != 2 {
		t.Fatalf("2회 RecordStep 후 PastSteps 개수: 기대 2, 실제 %d", len(st2.PastSteps))
	}
	if st2.PastSteps[0].Task != "1단계" {
		t.Errorf("PastSteps[0].Task: 기대 '1단계', 실제 %q", st2.PastSteps[0].Task)
	}
	if st2.PastSteps[1].Task != "2단계" {
		t.Errorf("PastSteps[1].Task: 기대 '2단계', 실제 %q", st2.PastSteps[1].Task)
	}
	// Plan 이 2개 소비됐으므로 1개 남아야 한다.
	if len(st2.Plan) != 1 || st2.Plan[0] != "3단계" {
		t.Errorf("남은 Plan: 기대 ['3단계'], 실제 %v", st2.Plan)
	}
}

// TestRecordStep_OriginalStateUnchanged 는 RecordStep 이 원본 PlannerState 를 변경하지 않는지 검증한다.
func TestRecordStep_OriginalStateUnchanged(t *testing.T) {
	original := PlannerState{
		Plan:      []string{"X단계"},
		PastSteps: []Step{},
	}

	_ = RecordStep(original, Step{Task: "X단계", Result: "X결과"})

	// 원본은 변경되지 않아야 한다.
	if len(original.PastSteps) != 0 {
		t.Errorf("원본 PastSteps 가 변경됐습니다: %v", original.PastSteps)
	}
	if len(original.Plan) != 1 {
		t.Errorf("원본 Plan 이 변경됐습니다: %v", original.Plan)
	}
}

// ============================================================
// plan→record→replan→respond 루프 종단 검증
// ============================================================

// TestPlannerLoop_PlanToRespond 는 plan 한 스텝 소비 → RecordStep → Replan 루프가
// 빈 plan(Action=respond)에서 최종 응답으로 끝나는지 검증한다.
// llm.Client.Structured 시퀀스: Plan(→plan/[step1]) → Replan(→respond/"최종 답변")
func TestPlannerLoop_PlanToRespond(t *testing.T) {
	// Structured 호출 시퀀스:
	//   1회(Plan):   Action=plan, Steps=["단일 작업"]
	//   2회(Replan): Action=respond, Response="최종 답변"
	client := newPlannerSeqClient(
		llm.StubResponse{
			StructuredValue: makePlanResult("단일 작업"),
		},
		llm.StubResponse{
			StructuredValue: makeRespondResult("최종 답변"),
		},
	)

	ctx := context.Background()

	// 1) Plan 호출: Action=plan, Steps=["단일 작업"]
	planResult, err := Plan(ctx, client, "작업을 계획하라")
	if err != nil {
		t.Fatalf("Plan 실패: %v", err)
	}
	if planResult.Action != string(structured.PlannerActionPlan) {
		t.Fatalf("Plan Action: 기대 %q, 실제 %q", structured.PlannerActionPlan, planResult.Action)
	}
	if planResult.Plan == nil || len(planResult.Plan.Steps) == 0 {
		t.Fatal("Plan.Steps 가 비어 있습니다")
	}

	// 2) 첫 번째 스텝 소비 시뮬레이션
	st := PlannerState{
		Input:     "작업을 계획하라",
		Plan:      planResult.Plan.Steps,
		PastSteps: []Step{},
	}

	// 소비할 스텝 가져오기
	currentTask := st.Plan[0]

	// 워커 실행 시뮬레이션 (stub: 작업 완료)
	workerResult := "단일 작업이 완료됐습니다"

	// 3) RecordStep: 소비한 스텝을 PastSteps 에 누적
	step := Step{Task: currentTask, Result: workerResult}
	update := RecordStep(st, step)

	newSt := update[plannerStateKey].(PlannerState)

	// PastSteps 에 스텝이 누적됐는지 확인
	if len(newSt.PastSteps) != 1 {
		t.Fatalf("RecordStep 후 PastSteps 개수: 기대 1, 실제 %d", len(newSt.PastSteps))
	}
	if newSt.PastSteps[0].Task != currentTask {
		t.Errorf("PastSteps[0].Task: 기대 %q, 실제 %q", currentTask, newSt.PastSteps[0].Task)
	}

	// Plan 이 비어 있어야 한다(1개 소비됨)
	if len(newSt.Plan) != 0 {
		t.Errorf("RecordStep 후 Plan: 기대 빈 슬라이스, 실제 %v", newSt.Plan)
	}

	// 4) Replan: 빈 plan 과 pastSteps 로 재계획 → Action=respond 로 루프 종료
	replanResult, err := Replan(ctx, client, newSt.Plan, newSt.PastSteps)
	if err != nil {
		t.Fatalf("Replan 실패: %v", err)
	}

	if replanResult.Action != string(structured.PlannerActionRespond) {
		t.Fatalf("Replan Action: 기대 %q(루프 종료), 실제 %q", structured.PlannerActionRespond, replanResult.Action)
	}
	if replanResult.Response == nil {
		t.Fatal("Replan Response 가 nil — Action=respond 이면 Response 가 채워져야 한다")
	}
	if replanResult.Response.Response != "최종 답변" {
		t.Errorf("Replan Response.Response: 기대 '최종 답변', 실제 %q", replanResult.Response.Response)
	}
}

// TestPlannerLoop_MultiStep 은 두 스텝을 소비하는 루프가 올바르게 누적되고
// 마지막에 respond 로 종료되는지 검증한다.
// Structured 시퀀스: Plan(plan/[step1, step2]) → Replan(plan/[step2]) → Replan(respond/"완료")
func TestPlannerLoop_MultiStep(t *testing.T) {
	client := newPlannerSeqClient(
		// 1회(Plan): 두 스텝 계획
		llm.StubResponse{StructuredValue: makePlanResult("스텝1", "스텝2")},
		// 2회(Replan): 스텝1 완료 후 스텝2 로 재계획
		llm.StubResponse{StructuredValue: makePlanResult("스텝2")},
		// 3회(Replan): 스텝2 완료 후 최종 응답
		llm.StubResponse{StructuredValue: makeRespondResult("두 스텝 모두 완료")},
	)

	ctx := context.Background()

	// Plan
	planResult, err := Plan(ctx, client, "두 단계 작업")
	if err != nil {
		t.Fatalf("Plan 실패: %v", err)
	}

	st := PlannerState{
		Input:     "두 단계 작업",
		Plan:      planResult.Plan.Steps, // ["스텝1", "스텝2"]
		PastSteps: []Step{},
	}

	// 루프: respond 가 나올 때까지 반복
	finalResponse := ""
	const maxIter = 10
	for range maxIter {
		if len(st.Plan) == 0 {
			// plan 이 비었으면 Replan 으로 최종 응답 요청
			replanResult, err := Replan(ctx, client, st.Plan, st.PastSteps)
			if err != nil {
				t.Fatalf("Replan(빈 plan) 실패: %v", err)
			}
			if replanResult.Action == string(structured.PlannerActionRespond) && replanResult.Response != nil {
				finalResponse = replanResult.Response.Response
				break
			}
			t.Fatalf("빈 plan 인데 Replan 이 respond 를 반환하지 않음: action=%q", replanResult.Action)
		}

		// 현재 스텝 소비
		currentTask := st.Plan[0]
		step := Step{Task: currentTask, Result: currentTask + " 완료"}
		update := RecordStep(st, step)
		st = update[plannerStateKey].(PlannerState)

		// 아직 plan 이 남아 있으면 Replan
		if len(st.Plan) > 0 {
			replanResult, err := Replan(ctx, client, st.Plan, st.PastSteps)
			if err != nil {
				t.Fatalf("Replan 실패: %v", err)
			}
			if replanResult.Action == string(structured.PlannerActionRespond) && replanResult.Response != nil {
				finalResponse = replanResult.Response.Response
				goto done
			}
			// plan 이 갱신됐으면 상태에 반영
			if replanResult.Plan != nil {
				st.Plan = replanResult.Plan.Steps
			}
		}
	}
done:
	if finalResponse == "" {
		t.Fatal("루프가 최종 응답 없이 종료됐습니다")
	}
	if finalResponse != "두 스텝 모두 완료" {
		t.Errorf("최종 응답: 기대 '두 스텝 모두 완료', 실제 %q", finalResponse)
	}

	// PastSteps 에 두 스텝이 누적됐는지 확인 (루프 종료 시점 st 기준)
	if len(st.PastSteps) != 2 {
		t.Errorf("최종 PastSteps 개수: 기대 2, 실제 %d", len(st.PastSteps))
	}
}
