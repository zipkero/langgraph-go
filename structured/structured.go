// structured 패키지는 Go 구조체 ↔ JSON 스키마 변환과 구조화 출력 파싱/검증을 담당한다.
// 다른 Phase 1 패키지에 의존하지 않는 독립 노드로, llm·agent·prompt가 이를 소비한다.
package structured

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

// Schema 는 JSON 스키마와 메타정보를 담는 타입이다.
type Schema struct {
	// Name 은 스키마 이름이다.
	Name string
	// Description 은 스키마 설명이다.
	Description string
	// JSONSchema 는 JSON 스키마 본문이다(map[string]any 형태).
	JSONSchema map[string]any
	// enumConstraints 는 필드 이름별 허용 열거값 맵이다(검증용 내부 상태).
	enumConstraints map[string][]string
	// requiredFields 는 필수 필드 이름 슬라이스다.
	requiredFields []string
}

// Validator 는 JSON 문자열을 Schema 에 대해 검증하는 타입이다.
type Validator struct {
	schema Schema
}

// NewValidator 는 s 를 기반으로 Validator 를 생성한다.
func NewValidator(s Schema) Validator {
	return Validator{schema: s}
}

// Validate 는 raw JSON 을 v 의 스키마에 대해 검증한다.
func (v Validator) Validate(raw string) error {
	return Validate(raw, v.schema)
}

// FieldOption 은 BuildSchema 나 필드 빌더에 전달하는 옵션 타입이다.
// EnumField 등이 반환하며, 제약·열거값을 스키마에 부착한다.
type FieldOption struct {
	// fieldName 은 이 옵션이 적용될 필드 이름이다.
	fieldName string
	// enumValues 는 허용되는 열거값 목록이다.
	enumValues []string
}

// EnumField 는 name 필드에 values 열거 제약을 부착하는 FieldOption 을 생성한다.
// binary_score, next 같은 제약 필드에 사용한다.
func EnumField(name string, values ...string) FieldOption {
	return FieldOption{fieldName: name, enumValues: values}
}

// BuildSchema 는 T 타입 구조체의 태그(json/description)를 분석해 Schema 를 생성한다.
// EnumField 를 통해 필드별 열거 제약을 추가할 수 있다.
func BuildSchema[T any](opts ...FieldOption) Schema {
	var zero T
	t := reflect.TypeOf(zero)
	if t == nil {
		t = reflect.TypeOf((*T)(nil)).Elem()
	}
	// 포인터 타입이면 기저 타입으로 이동
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	// FieldOption 을 이름별로 인덱싱
	enumMap := make(map[string][]string)
	for _, opt := range opts {
		if opt.fieldName != "" && len(opt.enumValues) > 0 {
			enumMap[opt.fieldName] = opt.enumValues
		}
	}

	properties, required, enumConstraints := buildProperties(t, enumMap)

	jsonSchema := map[string]any{
		"type":       "object",
		"properties": properties,
	}
	if len(required) > 0 {
		jsonSchema["required"] = required
	}

	return Schema{
		Name:            t.Name(),
		JSONSchema:      jsonSchema,
		enumConstraints: enumConstraints,
		requiredFields:  required,
	}
}

// buildProperties 는 구조체 타입 t 의 필드를 순회해 JSON 스키마 properties 맵,
// required 슬라이스, enum 제약 맵을 반환한다.
func buildProperties(t reflect.Type, enumMap map[string][]string) (
	properties map[string]any, required []string, enumConstraints map[string][]string,
) {
	// 구조체가 아니면 빈 결과 반환
	if t.Kind() != reflect.Struct {
		return map[string]any{}, nil, map[string][]string{}
	}

	properties = make(map[string]any)
	enumConstraints = make(map[string][]string)

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
				// json:"-" 태그는 JSON 직렬화에서 제외
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

		// description 태그 추출
		description := field.Tag.Get("description")

		// 필드 스키마 생성
		fieldSchema := buildFieldSchema(field.Type, description, enumMap[fieldName])

		properties[fieldName] = fieldSchema

		// omitempty 가 없으면 필수 필드로 등록
		if !omitempty {
			required = append(required, fieldName)
		}

		// enum 제약이 있으면 enumConstraints 에 등록
		if vals, ok := enumMap[fieldName]; ok {
			enumConstraints[fieldName] = vals
		} else if enums, ok := fieldSchema["enum"]; ok {
			// 필드 스키마에 enum 이 직접 정의된 경우
			if enumSlice, ok := enums.([]string); ok {
				enumConstraints[fieldName] = enumSlice
			}
		}
	}

	return properties, required, enumConstraints
}

// buildFieldSchema 는 reflect.Type t 에 대한 JSON 스키마 맵을 반환한다.
func buildFieldSchema(t reflect.Type, description string, enumValues []string) map[string]any {
	// 포인터 타입이면 기저 타입으로 이동
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	schema := make(map[string]any)

	switch t.Kind() {
	case reflect.String:
		schema["type"] = "string"
	case reflect.Bool:
		schema["type"] = "boolean"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		schema["type"] = "integer"
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		schema["type"] = "integer"
	case reflect.Float32, reflect.Float64:
		schema["type"] = "number"
	case reflect.Slice:
		schema["type"] = "array"
		itemSchema := buildFieldSchema(t.Elem(), "", nil)
		schema["items"] = itemSchema
	case reflect.Map:
		schema["type"] = "object"
	case reflect.Struct:
		schema["type"] = "object"
		innerProps, innerRequired, _ := buildProperties(t, nil)
		schema["properties"] = innerProps
		if len(innerRequired) > 0 {
			schema["required"] = innerRequired
		}
	default:
		schema["type"] = "string"
	}

	if description != "" {
		schema["description"] = description
	}

	if len(enumValues) > 0 {
		schema["enum"] = enumValues
	}

	return schema
}

// ParseStructured 는 raw JSON 문자열을 T 타입으로 파싱해 반환한다.
// 파싱 실패 시 에러를 반환한다.
func ParseStructured[T any](raw string) (T, error) {
	var result T
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return result, fmt.Errorf("structured: JSON 파싱 실패: %w", err)
	}
	return result, nil
}

// Validate 는 raw JSON 문자열을 s 스키마에 대해 검증한다.
// 필수 필드 누락·enum 위반이 있으면 에러를 반환한다.
func Validate(raw string, s Schema) error {
	// JSON 파싱
	var data map[string]any
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		return fmt.Errorf("structured: JSON 파싱 실패: %w", err)
	}

	// 필수 필드 누락 검사
	for _, fieldName := range s.requiredFields {
		if _, ok := data[fieldName]; !ok {
			return fmt.Errorf("structured: 필수 필드 누락: %q", fieldName)
		}
	}

	// enum 제약 위반 검사
	for fieldName, allowed := range s.enumConstraints {
		val, ok := data[fieldName]
		if !ok {
			// 필드가 없는 경우는 필수 필드 검사에서 처리됨
			continue
		}
		strVal, ok := val.(string)
		if !ok {
			return fmt.Errorf("structured: 필드 %q 는 문자열이어야 합니다", fieldName)
		}
		if !containsString(allowed, strVal) {
			return fmt.Errorf("structured: 필드 %q 의 값 %q 는 허용값 %v 에 없습니다", fieldName, strVal, allowed)
		}
	}

	return nil
}

// containsString 은 slice 에 s 가 포함되면 true 를 반환한다.
func containsString(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}
