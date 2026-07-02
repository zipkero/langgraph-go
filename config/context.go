// context.go 는 RunConfig 를 context 로 전달하는 헬퍼를 담는다.
// 그래프 엔진이 실행 진입점에서 주입하고, 노드 함수(prebuilt 노드·에이전트 어댑터 등)가
// 시그니처 변경 없이 실행별 설정(thread_id/user_id 등)에 접근하는 통로다.
package config

import "context"

// runConfigCtxKey 는 RunConfig 를 context 에 담을 때 쓰는 비공개 키 타입이다.
type runConfigCtxKey struct{}

// WithRunConfig 는 ctx 에 cfg 를 주입한 새 context 를 반환한다.
func WithRunConfig(ctx context.Context, cfg RunConfig) context.Context {
	return context.WithValue(ctx, runConfigCtxKey{}, cfg)
}

// RunConfigFromContext 는 ctx 에 주입된 RunConfig 를 반환한다.
// 주입된 값이 없으면 zero value 와 false 를 반환한다.
func RunConfigFromContext(ctx context.Context) (RunConfig, bool) {
	cfg, ok := ctx.Value(runConfigCtxKey{}).(RunConfig)
	return cfg, ok
}
