// tool 패키지는 도구 정의·스키마·레지스트리·실행기·런타임 주입을 담당한다.
// message(ToolCall/Message)와 config(RunConfig)에 의존하며,
// store/trace 구체 타입은 참조하지 않는다(§28-1 규칙2).
package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/zipkero/langgraph-go/config"
	"github.com/zipkero/langgraph-go/message"
)

// Input 은 도구에 전달되는 JSON 직렬화 인자다.
type Input = json.RawMessage

// Result 는 도구 실행 결과를 담는 타입이다.
type Result struct {
	// Content 는 도구 실행 결과 텍스트다.
	Content string
	// IsError 는 도구 실행이 오류로 끝났는지를 나타낸다.
	IsError bool
}

// Parameter 는 도구 스키마의 단일 파라미터를 나타낸다.
type Parameter struct {
	// Name 은 파라미터 이름이다.
	Name string
	// Type 은 파라미터의 JSON 타입이다(string/integer/number/boolean/array/object).
	Type string
	// Description 은 파라미터 설명이다.
	Description string
	// Required 는 파라미터가 필수인지를 나타낸다.
	Required bool
}

// Schema 는 도구의 이름·설명·파라미터 명세를 담는다.
type Schema struct {
	// Name 은 도구 이름이다.
	Name string
	// Description 은 도구 설명이다.
	Description string
	// Parameters 는 도구 파라미터 목록이다.
	Parameters []Parameter
}

// Store 는 tool 패키지가 소유하는 최소 스토어 인터페이스다.
// store.Store 가 이 인터페이스를 충족하지만 tool 은 store 를 import하지 않는다(§28-1 규칙2).
type Store interface {
	// Get 은 네임스페이스와 키로 항목을 조회한다.
	Get(ctx context.Context, namespace []string, key string) (map[string]any, bool, error)
	// Put 은 네임스페이스와 키로 값을 저장한다.
	Put(ctx context.Context, namespace []string, key string, value map[string]any) error
	// Search 는 네임스페이스에서 질의로 항목을 검색한다.
	Search(ctx context.Context, namespace []string, query string, limit int) ([]map[string]any, error)
}

// Event 는 도구 실행 이벤트를 나타내는 타입이다.
// trace 패키지가 이를 수신해 기록하지만 tool 은 trace 를 import하지 않는다(§28-1 규칙2).
type Event struct {
	// ToolName 은 이벤트를 발생시킨 도구 이름이다.
	ToolName string
	// ToolCallID 는 도구 호출 식별자다.
	ToolCallID string
	// Input 은 도구에 전달된 인자다.
	Input Input
	// Result 는 도구 실행 결과다. 실행 전 이벤트이면 nil이다.
	Result *Result
	// Err 는 실행 중 발생한 오류다.
	Err error
}

// Runtime 은 도구 실행 컨텍스트를 제공하는 인터페이스다.
// 도구 함수 내에서 상태·호출 ID·설정·스토어·이벤트 방출에 접근한다.
type Runtime interface {
	// State 는 현재 에이전트/그래프 상태를 반환한다.
	State() any
	// ToolCallID 는 현재 도구 호출의 식별자를 반환한다.
	ToolCallID() string
	// Config 는 현재 실행 설정을 반환한다.
	Config() config.RunConfig
	// Store 는 장기 메모리 스토어 인터페이스를 반환한다.
	Store() Store
	// Emit 은 도구 실행 이벤트를 방출한다.
	Emit(ev Event)
}

// Tool 은 실행 가능한 도구를 정의하는 인터페이스다.
type Tool interface {
	// Name 은 도구 이름을 반환한다.
	Name() string
	// Description 은 도구 설명을 반환한다.
	Description() string
	// Schema 는 도구의 입력 스키마를 반환한다.
	Schema() Schema
	// Execute 는 ctx와 input을 받아 도구를 실행하고 결과를 반환한다.
	Execute(ctx context.Context, input Input, rt Runtime) (Result, error)
}

// Registry 는 도구를 등록·조회하는 레지스트리다.
type Registry struct {
	tools map[string]Tool
	order []string
}

// NewRegistry 는 빈 Registry 를 생성한다.
func NewRegistry() *Registry {
	return &Registry{
		tools: make(map[string]Tool),
	}
}

// Register 는 t 를 레지스트리에 등록한다. 이미 같은 이름이 있으면 에러를 반환한다.
func (r *Registry) Register(t Tool) error {
	name := t.Name()
	if _, exists := r.tools[name]; exists {
		return fmt.Errorf("tool: 이미 등록된 도구 이름 %q", name)
	}
	r.tools[name] = t
	r.order = append(r.order, name)
	return nil
}

// RegisterMany 는 여러 도구를 순서대로 등록한다. 하나라도 실패하면 에러를 반환한다.
func (r *Registry) RegisterMany(ts ...Tool) error {
	for _, t := range ts {
		if err := r.Register(t); err != nil {
			return err
		}
	}
	return nil
}

// Get 은 name 에 해당하는 도구와 존재 여부를 반환한다.
func (r *Registry) Get(name string) (Tool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

// List 는 등록 순서대로 도구 목록을 반환한다.
func (r *Registry) List() []Tool {
	result := make([]Tool, 0, len(r.order))
	for _, name := range r.order {
		result = append(result, r.tools[name])
	}
	return result
}

// Schemas 는 등록된 모든 도구의 스키마를 등록 순서대로 반환한다.
func (r *Registry) Schemas() []Schema {
	result := make([]Schema, 0, len(r.order))
	for _, name := range r.order {
		result = append(result, r.tools[name].Schema())
	}
	return result
}

// unknownToolError 는 알 수 없는 도구 이름 오류를 나타내는 내부 타입이다.
type unknownToolError struct {
	name string
}

func (e *unknownToolError) Error() string {
	return fmt.Sprintf("tool: 알 수 없는 도구 %q", e.name)
}

// UnknownToolError 는 name 에 해당하는 도구가 없을 때 반환하는 에러를 생성한다.
func UnknownToolError(name string) error {
	return &unknownToolError{name: name}
}

// IsUnknownToolError 는 err 가 UnknownToolError 인지 판정한다.
func IsUnknownToolError(err error) bool {
	_, ok := err.(*unknownToolError)
	return ok
}

// Executor 는 Registry 와 연결돼 도구 호출을 디스패치하는 실행기다.
type Executor struct {
	registry *Registry
}

// NewExecutor 는 reg 를 사용하는 Executor 를 생성한다.
func NewExecutor(reg *Registry) *Executor {
	return &Executor{registry: reg}
}

// Execute 는 call 에 해당하는 도구를 찾아 실행하고 Result 를 반환한다.
// 도구가 없으면 UnknownToolError 를 반환한다.
func (e *Executor) Execute(ctx context.Context, call message.ToolCall, rt Runtime) (Result, error) {
	t, ok := e.registry.Get(call.Name)
	if !ok {
		return Result{}, UnknownToolError(call.Name)
	}
	return t.Execute(ctx, call.Args, rt)
}

// ExecuteMany 는 calls 를 순서대로 실행하고 각 결과를 ToolMessage 로 변환한 목록을 반환한다.
// 개별 실행에서 에러가 발생해도 나머지를 계속 실행한다.
// 알 수 없는 도구는 에러 내용을 IsError=true 인 ToolMessage 로 포함한다.
func (e *Executor) ExecuteMany(ctx context.Context, calls []message.ToolCall, rt Runtime) ([]message.Message, error) {
	msgs := make([]message.Message, 0, len(calls))
	for _, call := range calls {
		res, err := e.Execute(ctx, call, rt)
		if err != nil {
			// 실행 오류는 에러 내용을 담은 ToolMessage 로 변환
			res = Result{Content: err.Error(), IsError: true}
		}
		msgs = append(msgs, e.BuildToolMessage(call, res))
	}
	return msgs, nil
}

// ExecuteWithTimeout 은 d 시간 안에 call 을 실행한다. 제한 시간 초과 시 에러를 반환한다.
func (e *Executor) ExecuteWithTimeout(ctx context.Context, call message.ToolCall, rt Runtime, d time.Duration) (Result, error) {
	ctx, cancel := context.WithTimeout(ctx, d)
	defer cancel()
	return e.Execute(ctx, call, rt)
}

// BuildToolMessage 는 call 과 res 를 조합해 ToolMessage 를 생성한다.
func (e *Executor) BuildToolMessage(call message.ToolCall, res Result) message.Message {
	return message.NewToolMessage(call.ID, call.Name, res.Content)
}

// ValidateArgs 는 s 스키마에 대해 args JSON 의 필수 파라미터 유무를 검증한다.
// 필수 파라미터가 누락되면 에러를 반환한다.
func ValidateArgs(s Schema, args json.RawMessage) error {
	if len(args) == 0 {
		// 필수 파라미터가 있는데 인자가 없으면 에러
		for _, p := range s.Parameters {
			if p.Required {
				return fmt.Errorf("tool: 필수 파라미터 %q 가 누락됐습니다", p.Name)
			}
		}
		return nil
	}

	var data map[string]any
	if err := json.Unmarshal(args, &data); err != nil {
		return fmt.Errorf("tool: 인자 JSON 파싱 실패: %w", err)
	}

	for _, p := range s.Parameters {
		if p.Required {
			if _, ok := data[p.Name]; !ok {
				return fmt.Errorf("tool: 필수 파라미터 %q 가 누락됐습니다", p.Name)
			}
		}
	}
	return nil
}

// DecodeArgs 는 args JSON 을 T 타입으로 디코딩해 반환한다.
func DecodeArgs[T any](args json.RawMessage) (T, error) {
	var result T
	if err := json.Unmarshal(args, &result); err != nil {
		return result, fmt.Errorf("tool: 인자 디코딩 실패: %w", err)
	}
	return result, nil
}

// defaultRuntime 은 Runtime 의 기본 구현체다.
// 상태·스토어·이벤트 방출을 외부에서 주입받는다.
type defaultRuntime struct {
	state      any
	toolCallID string
	cfg        config.RunConfig
	store      Store
	emit       func(Event)
}

// NewRuntime 은 지정된 값들로 Runtime 을 생성한다.
// store 나 emit 이 nil 이면 no-op 구현을 쓴다.
func NewRuntime(state any, toolCallID string, cfg config.RunConfig, store Store, emit func(Event)) Runtime {
	if emit == nil {
		emit = func(Event) {}
	}
	return &defaultRuntime{
		state:      state,
		toolCallID: toolCallID,
		cfg:        cfg,
		store:      store,
		emit:       emit,
	}
}

func (r *defaultRuntime) State() any                { return r.state }
func (r *defaultRuntime) ToolCallID() string        { return r.toolCallID }
func (r *defaultRuntime) Config() config.RunConfig  { return r.cfg }
func (r *defaultRuntime) Store() Store              { return r.store }
func (r *defaultRuntime) Emit(ev Event)             { r.emit(ev) }

// funcTool 은 FromFunc 로 생성된 Tool 구현체다.
type funcTool struct {
	name   string
	desc   string
	schema Schema
	fn     func(ctx context.Context, input Input, rt Runtime) (Result, error)
}

func (t *funcTool) Name() string                                                         { return t.name }
func (t *funcTool) Description() string                                                  { return t.desc }
func (t *funcTool) Schema() Schema                                                       { return t.schema }
func (t *funcTool) Execute(ctx context.Context, input Input, rt Runtime) (Result, error) { return t.fn(ctx, input, rt) }

// FromFunc 는 임의의 Go 함수를 Tool 로 변환한다.
// fn 의 시그니처는 func(ctx context.Context, args T, rt Runtime) (Result, error) 이어야 한다.
// T 가 구조체이면 필드 태그(json/description)에서 스키마를 자동 도출한다.
// fn 이 지원하지 않는 시그니처이면 패닉한다.
func FromFunc(name, desc string, fn any) Tool {
	fnVal := reflect.ValueOf(fn)
	fnType := fnVal.Type()

	// 시그니처 검증: func(context.Context, T, Runtime) (Result, error)
	if fnType.Kind() != reflect.Func {
		panic(fmt.Sprintf("tool.FromFunc: fn 은 함수여야 합니다, 받은 타입: %T", fn))
	}
	if fnType.NumIn() != 3 || fnType.NumOut() != 2 {
		panic(fmt.Sprintf("tool.FromFunc: fn 시그니처는 func(ctx, T, Runtime) (Result, error) 이어야 합니다"))
	}

	argType := fnType.In(1)
	schema := buildSchemaFromType(name, desc, argType)

	execFn := func(ctx context.Context, input Input, rt Runtime) (Result, error) {
		// 인자 타입의 제로값 생성
		argPtr := reflect.New(argType)
		if len(input) > 0 {
			if err := json.Unmarshal(input, argPtr.Interface()); err != nil {
				return Result{}, fmt.Errorf("tool: 인자 디코딩 실패: %w", err)
			}
		}

		results := fnVal.Call([]reflect.Value{
			reflect.ValueOf(ctx),
			argPtr.Elem(),
			reflect.ValueOf(rt),
		})

		res := results[0].Interface().(Result)
		var execErr error
		if !results[1].IsNil() {
			execErr = results[1].Interface().(error)
		}
		return res, execErr
	}

	return &funcTool{
		name:   name,
		desc:   desc,
		schema: schema,
		fn:     execFn,
	}
}

// WithArgsSchema 는 타입 T 로 입력 스키마를 명시 지정한 Tool 을 생성한다.
// fn 은 func(ctx context.Context, args T, rt Runtime) (Result, error) 시그니처를 갖는다.
func WithArgsSchema[T any](name, desc string, fn func(ctx context.Context, args T, rt Runtime) (Result, error)) Tool {
	var zero T
	argType := reflect.TypeOf(zero)
	if argType == nil {
		argType = reflect.TypeOf((*T)(nil)).Elem()
	}
	schema := buildSchemaFromType(name, desc, argType)

	execFn := func(ctx context.Context, input Input, rt Runtime) (Result, error) {
		var args T
		if len(input) > 0 {
			if err := json.Unmarshal(input, &args); err != nil {
				return Result{}, fmt.Errorf("tool: 인자 디코딩 실패: %w", err)
			}
		}
		return fn(ctx, args, rt)
	}

	return &funcTool{
		name:   name,
		desc:   desc,
		schema: schema,
		fn:     execFn,
	}
}

// buildSchemaFromType 은 reflect.Type 에서 Tool Schema 를 도출한다.
// 구조체 타입이면 필드 태그(json/description)를 분석해 파라미터를 추출한다.
func buildSchemaFromType(toolName, toolDesc string, t reflect.Type) Schema {
	// 포인터 타입이면 기저 타입으로 이동
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	schema := Schema{
		Name:        toolName,
		Description: toolDesc,
	}

	if t.Kind() != reflect.Struct {
		return schema
	}

	params := make([]Parameter, 0, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)

		// 비공개 필드 건너뜀
		if !field.IsExported() {
			continue
		}

		// json 태그에서 필드명 추출
		jsonTag := field.Tag.Get("json")
		fieldName := field.Name
		omitempty := false
		if jsonTag != "" {
			parts := strings.Split(jsonTag, ",")
			if parts[0] == "-" {
				continue
			}
			if parts[0] != "" {
				fieldName = parts[0]
			}
			for _, opt := range parts[1:] {
				if opt == "omitempty" {
					omitempty = true
				}
			}
		}

		description := field.Tag.Get("description")
		fieldType := reflectTypeToJSONType(field.Type)

		params = append(params, Parameter{
			Name:        fieldName,
			Type:        fieldType,
			Description: description,
			Required:    !omitempty,
		})
	}

	schema.Parameters = params
	return schema
}

// reflectTypeToJSONType 은 reflect.Type 을 JSON 스키마 타입 문자열로 변환한다.
func reflectTypeToJSONType(t reflect.Type) string {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.String:
		return "string"
	case reflect.Bool:
		return "boolean"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return "integer"
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "integer"
	case reflect.Float32, reflect.Float64:
		return "number"
	case reflect.Slice, reflect.Array:
		return "array"
	case reflect.Map, reflect.Struct:
		return "object"
	default:
		return "string"
	}
}
