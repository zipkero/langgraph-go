// planner.go 는 Plan/Replan/RecordStep 과 플래너 상태 타입(PlannerState/Step)을 담는다.
// Plan/Replan 은 llm.Client.Structured 로 structured.PlannerResult 를 반환한다.
// RecordStep 은 소비한 Step(Task/Result) 을 PlannerState.PastSteps 에 누적하는 StateUpdate 를 만든다.
// PlannerState/Step 은 multiagent 고유 타입으로 structured 패키지를 재사용한다.
// SPEC §5.7, ANALYSIS §2·§5.6 참조.
package multiagent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/zipkero/langgraph-go/graph"
	"github.com/zipkero/langgraph-go/llm"
	"github.com/zipkero/langgraph-go/message"
	"github.com/zipkero/langgraph-go/structured"
)

// Step 은 플래너가 소비한 단일 실행 단계를 나타낸다.
// Task 는 계획에서 추출한 작업 설명이고, Result 는 워커 실행 결과다.
type Step struct {
	// Task 는 실행한 계획 단계 설명이다.
	Task string
	// Result 는 해당 단계를 실행한 워커의 결과다.
	Result string
}

// PlannerState 는 플래너 루프의 그래프 상태를 담는다.
// 플래너 노드에서 이 구조체를 graph.State 의 "planner" 키로 저장·복원한다.
type PlannerState struct {
	// Input 은 최초 사용자 입력이다.
	Input string
	// Plan 은 현재 남은 실행 단계 목록이다. 소비할수록 줄어든다.
	Plan []string
	// PastSteps 는 지금까지 소비·완료된 Step 목록이다. RecordStep 이 누적한다.
	PastSteps []Step
	// Response 는 Action=respond 일 때 채워지는 최종 응답 텍스트다.
	Response string
}

// plannerStateKey 는 graph.State 에서 PlannerState 를 저장·조회할 때 사용하는 키다.
const plannerStateKey = "planner_state"

// Plan 은 input 을 llm.Client.Structured 로 전달해 structured.PlannerResult 를 반환한다.
// Action=plan 이면 Plan.Steps 가 채워지고, Action=respond 이면 Response 가 채워진다.
// client 와 input 이 필요하며, 메시지는 단순 user 메시지 하나로 구성한다.
func Plan(ctx context.Context, client llm.Client, input string) (structured.PlannerResult, error) {
	req := llm.ChatRequest{
		Messages: []message.Message{
			message.NewUserMessage(input),
		},
	}

	raw, err := client.Structured(ctx, req, structured.PlannerResultSchema())
	if err != nil {
		return structured.PlannerResult{}, fmt.Errorf("multiagent: Plan — Structured 호출 실패: %w", err)
	}

	return parsePlannerResult(raw)
}

// Replan 은 남은 plan 단계와 지금까지 완료한 pastSteps 를 컨텍스트로
// llm.Client.Structured 를 호출해 재계획된 structured.PlannerResult 를 반환한다.
// Action=respond 이면 빈 plan 으로 루프를 종료할 신호다.
func Replan(ctx context.Context, client llm.Client, plan []string, pastSteps []Step) (structured.PlannerResult, error) {
	// 재계획 프롬프트: 완료된 단계와 남은 계획을 컨텍스트로 제공한다.
	prompt := buildReplanPrompt(plan, pastSteps)

	req := llm.ChatRequest{
		Messages: []message.Message{
			message.NewUserMessage(prompt),
		},
	}

	raw, err := client.Structured(ctx, req, structured.PlannerResultSchema())
	if err != nil {
		return structured.PlannerResult{}, fmt.Errorf("multiagent: Replan — Structured 호출 실패: %w", err)
	}

	return parsePlannerResult(raw)
}

// RecordStep 은 st(PlannerState) 에 step 을 PastSteps 에 누적하고
// plan 의 첫 번째 항목을 소비한 StateUpdate 를 반환한다.
// 반환된 StateUpdate 를 그래프 NodeFunc 의 반환값으로 쓰면 상태가 갱신된다.
func RecordStep(st PlannerState, step Step) graph.StateUpdate {
	// PastSteps 에 새 Step 을 추가한다.
	newPastSteps := make([]Step, len(st.PastSteps)+1)
	copy(newPastSteps, st.PastSteps)
	newPastSteps[len(st.PastSteps)] = step

	// Plan 의 첫 번째 항목(소비된 단계)을 제거한다.
	newPlan := []string{}
	if len(st.Plan) > 1 {
		newPlan = make([]string, len(st.Plan)-1)
		copy(newPlan, st.Plan[1:])
	}

	updatedState := PlannerState{
		Input:     st.Input,
		Plan:      newPlan,
		PastSteps: newPastSteps,
		Response:  st.Response,
	}

	return graph.StateUpdate{
		plannerStateKey: updatedState,
	}
}

// parsePlannerResult 는 llm.Client.Structured 가 반환한 any 값을
// structured.PlannerResult 로 변환한다.
// StructuredValue 로 직접 PlannerResult 를 받거나, map[string]any 를 JSON 왕복 변환해 처리한다.
func parsePlannerResult(raw any) (structured.PlannerResult, error) {
	// 이미 PlannerResult 타입이면 그대로 반환한다.
	if pr, ok := raw.(structured.PlannerResult); ok {
		return pr, nil
	}

	// map[string]any 이면 JSON 왕복 변환으로 PlannerResult 를 도출한다.
	b, err := json.Marshal(raw)
	if err != nil {
		return structured.PlannerResult{}, fmt.Errorf("multiagent: parsePlannerResult — JSON 직렬화 실패: %w", err)
	}
	var result structured.PlannerResult
	if err := json.Unmarshal(b, &result); err != nil {
		return structured.PlannerResult{}, fmt.Errorf("multiagent: parsePlannerResult — JSON 파싱 실패: %w", err)
	}
	return result, nil
}

// buildReplanPrompt 는 재계획에 사용할 프롬프트 문자열을 생성한다.
// 완료된 단계(pastSteps) 와 남은 계획(plan) 을 포함한다.
func buildReplanPrompt(plan []string, pastSteps []Step) string {
	var b strings.Builder
	b.WriteString("지금까지 완료한 단계:\n")
	if len(pastSteps) == 0 {
		b.WriteString("  (없음)\n")
	} else {
		for i, s := range pastSteps {
			fmt.Fprintf(&b, "  %d. 작업: %s\n     결과: %s\n", i+1, s.Task, s.Result)
		}
	}

	b.WriteString("\n남은 계획:\n")
	if len(plan) == 0 {
		b.WriteString("  (없음)\n")
	} else {
		for i, step := range plan {
			fmt.Fprintf(&b, "  %d. %s\n", i+1, step)
		}
	}

	b.WriteString("\n위 정보를 바탕으로 계획을 재검토하고 계속 진행하거나 최종 응답을 제공하세요.")
	return b.String()
}
