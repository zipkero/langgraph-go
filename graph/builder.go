// builder.go 는 StateGraph 빌드 표면을 정의한다.
// NewStateGraph로 Builder를 만들고, AddNode/AddEdge/AddConditionalEdges/
// SetEntryPoint/SetConditionalEntryPoint로 노드·엣지·진입점을 누적한다.
// Compile이 validate를 거쳐 불변 Compiled를 반환한다(§3.1, D3).
package graph

import (
	"fmt"

	"github.com/zipkero/langgraph-go/checkpoint"
)

// ReducerFunc 는 StateSchema에 등록되는 필드별 리듀서 함수 타입이다.
// 현재 필드 값(cur)과 업데이트 값(upd)을 받아 병합된 결과를 반환한다(§2.4 D4).
type ReducerFunc func(cur, upd any) any

// StateSchema 는 필드별 리듀서 맵을 보유하는 상태 스키마다.
// Reducers 에 등록된 필드는 applyReducers에서 리듀서로 병합되고,
// 미등록 필드는 last-write-wins(덮어쓰기)로 처리된다.
type StateSchema struct {
	// Reducers 는 필드명 → 리듀서 함수 맵이다.
	Reducers map[string]ReducerFunc
}

// schemaOptions 는 NewStateGraph에 전달하는 스키마 옵션을 축적하는 내부 구조체다.
type schemaOptions struct {
	inputSchema  []string // 입력 필터 필드 목록(nil이면 비활성)
	outputSchema []string // 출력 추출 필드 목록(nil이면 비활성)
}

// SchemaOption 은 NewStateGraph에 전달하는 옵션 함수 타입이다.
type SchemaOption func(*schemaOptions)

// WithInputSchema 는 Invoke 입력을 지정 필드로 필터링하는 옵션이다.
func WithInputSchema(fields ...string) SchemaOption {
	return func(o *schemaOptions) {
		o.inputSchema = fields
	}
}

// WithOutputSchema 는 Invoke 출력을 지정 필드로 추출하는 옵션이다.
func WithOutputSchema(fields ...string) SchemaOption {
	return func(o *schemaOptions) {
		o.outputSchema = fields
	}
}

// nodeOptions 는 AddNode에 전달하는 노드 옵션을 축적하는 내부 구조체다.
type nodeOptions struct {
	destinations []string // WithDestinations로 선언된 도달 가능 노드 목록
}

// NodeOption 은 AddNode에 전달하는 옵션 함수 타입이다.
type NodeOption func(*nodeOptions)

// WithDestinations 는 이 노드에서 Goto를 통해 이동 가능한 노드 목록을 선언하는 옵션이다.
// command.Goto의 target이 이 목록 안에 있어야 validate를 통과한다(§2.3 D3).
func WithDestinations(targets ...string) NodeOption {
	return func(o *nodeOptions) {
		o.destinations = targets
	}
}

// compileOptions 는 Compile에 전달하는 컴파일 옵션을 축적하는 내부 구조체다.
type compileOptions struct {
	checkpointer checkpoint.Checkpointer
	maxSteps     int
}

// CompileOption 은 Compile에 전달하는 옵션 함수 타입이다.
type CompileOption func(*compileOptions)

// WithCheckpointer 는 컴파일된 그래프에 체크포인터를 결합하는 옵션이다.
// 지정 시 Invoke/Stream이 thread_id 단위로 상태를 저장·복원한다(D8).
func WithCheckpointer(cp checkpoint.Checkpointer) CompileOption {
	return func(o *compileOptions) {
		o.checkpointer = cp
	}
}

// WithMaxSteps 는 실행 루프 최대 스텝 수를 지정하는 옵션이다.
// 0이하이면 defaultMaxSteps(25)를 사용한다.
func WithMaxSteps(n int) CompileOption {
	return func(o *compileOptions) {
		if n > 0 {
			o.maxSteps = n
		}
	}
}

// nodeEntry 는 Builder가 누적하는 단일 노드 레코드다.
type nodeEntry struct {
	name         string
	fn           NodeFunc
	destinations []string    // WithDestinations로 선언된 허용 Goto 목적지
	subCompiled  *Compiled   // 이 노드가 서브그래프 어댑터일 경우 원본 Compiled 참조. nil이면 일반 노드.
}

// edgeEntry 는 정적 엣지 레코드다.
type edgeEntry struct {
	from string
	to   string
}

// conditionalEdgeEntry 는 조건 엣지 레코드다.
type conditionalEdgeEntry struct {
	from    string
	router  ConditionalRouter
	mapping map[string]string // 라우터 반환 키 → 노드 이름
}

// conditionalEntryPoint 는 조건 진입점 레코드다.
type conditionalEntryPoint struct {
	router  ConditionalRouter
	mapping map[string]string
}

// Builder 는 그래프 구성 정보를 누적하는 빌더다.
// Compile 호출 전에는 실행하지 않는다.
type Builder struct {
	schema       StateSchema
	schemaOpts   schemaOptions
	nodes        map[string]nodeEntry
	edges        []edgeEntry
	condEdges    []conditionalEdgeEntry
	entryPoint   string
	condEntry    *conditionalEntryPoint
}

// defaultMaxSteps 는 maxSteps 미지정 시 적용하는 기본 최대 스텝 수다.
const defaultMaxSteps = 25

// NewStateGraph 는 schema와 옵션으로 새 Builder를 만든다.
// WithInputSchema/WithOutputSchema 옵션으로 입출력 스키마를 지정할 수 있다.
func NewStateGraph(schema StateSchema, opts ...SchemaOption) *Builder {
	so := schemaOptions{}
	for _, opt := range opts {
		opt(&so)
	}
	return &Builder{
		schema:     schema,
		schemaOpts: so,
		nodes:      make(map[string]nodeEntry),
	}
}

// AddNode 는 이름과 실행 함수로 노드를 빌더에 등록한다.
// 같은 이름이 이미 존재하면 error를 반환한다.
// WithDestinations 옵션으로 허용 Goto 목적지를 선언할 수 있다(D3).
func (b *Builder) AddNode(name string, fn NodeFunc, opts ...NodeOption) error {
	if name == "" {
		return fmt.Errorf("graph: 노드 이름은 빈 문자열일 수 없습니다")
	}
	if fn == nil {
		return fmt.Errorf("graph: 노드 %q의 함수는 nil일 수 없습니다", name)
	}
	if _, exists := b.nodes[name]; exists {
		return fmt.Errorf("graph: 노드 %q가 이미 존재합니다", name)
	}

	no := nodeOptions{}
	for _, opt := range opts {
		opt(&no)
	}

	b.nodes[name] = nodeEntry{
		name:         name,
		fn:           fn,
		destinations: no.destinations,
	}
	return nil
}

// AddSubgraphNode 는 컴파일된 서브그래프를 부모 그래프의 노드로 등록한다.
// AddNode(name, sub.AsNode(), ...) 와 동일하게 동작하지만,
// 내부에 Compiled 참조를 보관해 WithSubgraphs() 스트림 옵션 시 이벤트 경로(Path) 전파를 지원한다.
// opts는 AddNode와 동일한 NodeOption을 사용한다(예: WithDestinations).
func (b *Builder) AddSubgraphNode(name string, sub *Compiled, opts ...NodeOption) error {
	if name == "" {
		return fmt.Errorf("graph: 노드 이름은 빈 문자열일 수 없습니다")
	}
	if sub == nil {
		return fmt.Errorf("graph: 서브그래프 노드 %q의 Compiled는 nil일 수 없습니다", name)
	}
	if _, exists := b.nodes[name]; exists {
		return fmt.Errorf("graph: 노드 %q가 이미 존재합니다", name)
	}

	no := nodeOptions{}
	for _, opt := range opts {
		opt(&no)
	}

	b.nodes[name] = nodeEntry{
		name:         name,
		fn:           sub.AsNode(),
		destinations: no.destinations,
		subCompiled:  sub,
	}
	return nil
}

// AddEdge 는 from → to 정적 엣지를 빌더에 추가한다.
func (b *Builder) AddEdge(from, to string) error {
	if from == "" || to == "" {
		return fmt.Errorf("graph: AddEdge: from·to는 빈 문자열일 수 없습니다")
	}
	b.edges = append(b.edges, edgeEntry{from: from, to: to})
	return nil
}

// AddConditionalEdges 는 from 노드에서 router와 mapping으로 구성된 조건 엣지를 추가한다.
// router가 반환하는 키를 mapping에서 노드 이름으로 변환한다.
func (b *Builder) AddConditionalEdges(from string, router ConditionalRouter, mapping map[string]string) error {
	if from == "" {
		return fmt.Errorf("graph: AddConditionalEdges: from은 빈 문자열일 수 없습니다")
	}
	if router == nil {
		return fmt.Errorf("graph: AddConditionalEdges: router는 nil일 수 없습니다")
	}
	b.condEdges = append(b.condEdges, conditionalEdgeEntry{
		from:    from,
		router:  router,
		mapping: mapping,
	})
	return nil
}

// SetEntryPoint 는 그래프의 첫 번째 실행 노드를 지정한다.
func (b *Builder) SetEntryPoint(name string) error {
	if name == "" {
		return fmt.Errorf("graph: SetEntryPoint: name은 빈 문자열일 수 없습니다")
	}
	b.entryPoint = name
	return nil
}

// SetConditionalEntryPoint 는 조건부 진입점을 지정한다.
// router가 반환하는 키를 mapping으로 첫 노드 이름에 매핑한다.
func (b *Builder) SetConditionalEntryPoint(router ConditionalRouter, mapping map[string]string) error {
	if router == nil {
		return fmt.Errorf("graph: SetConditionalEntryPoint: router는 nil일 수 없습니다")
	}
	b.condEntry = &conditionalEntryPoint{
		router:  router,
		mapping: mapping,
	}
	return nil
}

// Compile 은 빌더의 누적 구성을 검증하고 불변 Compiled를 반환한다.
// 검증 실패(미정의 엣지, 도달 불가 노드) 시 error를 반환한다(D3).
func (b *Builder) Compile(opts ...CompileOption) (*Compiled, error) {
	co := compileOptions{maxSteps: defaultMaxSteps}
	for _, opt := range opts {
		opt(&co)
	}
	if co.maxSteps <= 0 {
		co.maxSteps = defaultMaxSteps
	}

	if err := validate(b); err != nil {
		return nil, err
	}

	// 노드 맵 복사(불변성 보장)
	nodes := make(map[string]nodeEntry, len(b.nodes))
	for k, v := range b.nodes {
		nodes[k] = v
	}

	// 정적 엣지 복사
	edges := make([]edgeEntry, len(b.edges))
	copy(edges, b.edges)

	// 조건 엣지 복사
	condEdges := make([]conditionalEdgeEntry, len(b.condEdges))
	copy(condEdges, b.condEdges)

	return &Compiled{
		schema:      b.schema,
		schemaOpts:  b.schemaOpts,
		nodes:       nodes,
		edges:       edges,
		condEdges:   condEdges,
		entryPoint:  b.entryPoint,
		condEntry:   b.condEntry,
		checkpointer: co.checkpointer,
		maxSteps:    co.maxSteps,
	}, nil
}
