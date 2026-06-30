// a2a 패키지는 Agent-to-Agent 프로토콜 타입·서버·클라이언트와 태스크 수명주기를 제공한다.
// JSON-RPC 2.0 over HTTP(+SSE)를 net/http와 encoding/json으로 직접 구현한다.
// 외부 a2a SDK·gRPC·protobuf 의존 없이 표준 라이브러리만 사용한다(SPEC §3).
//
// import 방향: a2a → agent·config·structured·message·tool·core + 표준 라이브러리.
// 하위 패키지(특히 agent)는 a2a를 역참조하지 않는다(README §28-1).
package a2a

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
)

// ─── AgentCard / AgentSkill / AgentCapabilities ────────────────────────────

// AgentCard 는 에이전트의 메타데이터 카드다.
// /.well-known/agent-card.json 경로에서 노출된다(README §22-1).
type AgentCard struct {
	// Name 은 에이전트 이름이다.
	Name string `json:"name"`
	// Description 은 에이전트 설명이다.
	Description string `json:"description,omitempty"`
	// URL 은 에이전트 엔드포인트 URL이다.
	URL string `json:"url"`
	// Version 은 에이전트 버전 문자열이다.
	Version string `json:"version,omitempty"`
	// DefaultInputModes 는 기본 입력 모드 목록이다(예: ["text"]).
	DefaultInputModes []string `json:"defaultInputModes,omitempty"`
	// DefaultOutputModes 는 기본 출력 모드 목록이다(예: ["text"]).
	DefaultOutputModes []string `json:"defaultOutputModes,omitempty"`
	// Capabilities 는 에이전트 능력 선언이다.
	Capabilities AgentCapabilities `json:"capabilities"`
	// Skills 는 에이전트가 제공하는 스킬 목록이다.
	Skills []AgentSkill `json:"skills,omitempty"`
}

// AgentSkill 은 에이전트가 제공하는 단일 스킬을 기술한다.
type AgentSkill struct {
	// ID 는 스킬 식별자다.
	ID string `json:"id"`
	// Name 은 스킬 이름이다.
	Name string `json:"name"`
	// Description 은 스킬 설명이다.
	Description string `json:"description,omitempty"`
	// Tags 는 스킬 태그 목록이다.
	Tags []string `json:"tags,omitempty"`
	// Examples 는 스킬 사용 예시 목록이다.
	Examples []string `json:"examples,omitempty"`
}

// AgentCapabilities 는 에이전트가 선언하는 능력 집합이다.
type AgentCapabilities struct {
	// Streaming 은 SSE 스트리밍 지원 여부다.
	Streaming bool `json:"streaming"`
	// InputModes 는 지원하는 입력 모드 목록이다.
	InputModes []string `json:"inputModes,omitempty"`
	// OutputModes 는 지원하는 출력 모드 목록이다.
	OutputModes []string `json:"outputModes,omitempty"`
}

// ─── TaskState / TaskStatus / Task ─────────────────────────────────────────

// TaskState 는 태스크 상태 열거값이다.
// 와이어에서 문자열로 직렬화된다(README §22-1).
type TaskState string

const (
	// TaskStateWorking 는 태스크가 진행 중임을 나타낸다.
	TaskStateWorking TaskState = "working"
	// TaskStateInputRequired 는 추가 사용자 입력이 필요함을 나타낸다.
	TaskStateInputRequired TaskState = "input_required"
	// TaskStateCompleted 는 태스크가 완료됐음을 나타낸다.
	TaskStateCompleted TaskState = "completed"
	// TaskStateFailed 는 태스크가 실패했음을 나타낸다.
	TaskStateFailed TaskState = "failed"
	// TaskStateCanceled 는 태스크가 취소됐음을 나타낸다.
	TaskStateCanceled TaskState = "canceled"
)

// TaskStatus 는 태스크의 현재 상태와 관련 메시지를 담는다.
type TaskStatus struct {
	// State 는 태스크 상태값이다.
	State TaskState `json:"state"`
	// Message 는 상태 관련 메시지다(선택적).
	Message *Message `json:"message,omitempty"`
}

// Task 는 A2A 태스크를 나타낸다.
// 태스크는 ID·컨텍스트·상태·메시지 이력·아티팩트로 구성된다(README §22-1).
type Task struct {
	// ID 는 태스크 고유 식별자다.
	ID string `json:"id"`
	// ContextID 는 태스크가 속한 컨텍스트 식별자다.
	ContextID string `json:"contextId,omitempty"`
	// Status 는 태스크 현재 상태다.
	Status TaskStatus `json:"status"`
	// History 는 태스크의 메시지 이력이다.
	History []Message `json:"history,omitempty"`
	// Artifacts 는 태스크 실행 결과 아티팩트 목록이다.
	Artifacts []Artifact `json:"artifacts,omitempty"`
}

// ─── Message / Role ─────────────────────────────────────────────────────────

// Role 은 메시지 발신자 역할이다.
type Role string

const (
	// RoleUser 는 사용자 역할이다.
	RoleUser Role = "user"
	// RoleAgent 는 에이전트 역할이다.
	RoleAgent Role = "agent"
)

// messageWire 는 Message의 JSON 직렬화 중간 표현이다.
// kind 필드를 포함해 와이어 포맷과 일치시킨다.
type messageWire struct {
	Kind      string          `json:"kind"`
	Role      Role            `json:"role"`
	Parts     []json.RawMessage `json:"parts,omitempty"`
	MessageID string          `json:"messageId,omitempty"`
}

// Message 는 A2A 메시지다.
// 와이어에서 kind:"message"를 부착해 직렬화한다(README §22-1).
// a2a 고유 타입이며 message.Message와 별개다(SPEC §3).
type Message struct {
	// Role 은 메시지 발신자 역할이다.
	Role Role
	// Parts 는 메시지를 구성하는 파트 목록이다.
	Parts []Part
	// MessageID 는 메시지 고유 식별자다(선택적).
	MessageID string
}

// MarshalJSON 은 Message를 kind:"message" 필드와 함께 직렬화한다.
func (m Message) MarshalJSON() ([]byte, error) {
	rawParts := make([]json.RawMessage, len(m.Parts))
	for i, p := range m.Parts {
		b, err := json.Marshal(p)
		if err != nil {
			return nil, fmt.Errorf("a2a: Message.Parts[%d] 직렬화 실패: %w", i, err)
		}
		rawParts[i] = b
	}
	wire := messageWire{
		Kind:      "message",
		Role:      m.Role,
		Parts:     rawParts,
		MessageID: m.MessageID,
	}
	return json.Marshal(wire)
}

// UnmarshalJSON 은 kind:"message" 와이어에서 Message를 역직렬화한다.
func (m *Message) UnmarshalJSON(data []byte) error {
	var wire messageWire
	if err := json.Unmarshal(data, &wire); err != nil {
		return fmt.Errorf("a2a: Message 역직렬화 실패: %w", err)
	}
	m.Role = wire.Role
	m.MessageID = wire.MessageID
	m.Parts = make([]Part, len(wire.Parts))
	for i, raw := range wire.Parts {
		var p Part
		if err := json.Unmarshal(raw, &p); err != nil {
			return fmt.Errorf("a2a: Message.Parts[%d] 역직렬화 실패: %w", i, err)
		}
		m.Parts[i] = p
	}
	return nil
}

// ─── Part union (TextPart / DataPart / FilePart) ────────────────────────────

// TextPart 는 텍스트 콘텐츠를 담는 파트다.
type TextPart struct {
	// Text 는 파트의 텍스트 내용이다.
	Text string `json:"text"`
}

// DataPart 는 구조화 데이터를 담는 파트다.
type DataPart struct {
	// Data 는 파트의 구조화 데이터다.
	Data map[string]any `json:"data"`
}

// FilePart 는 파일을 담는 파트다.
// File 필드는 FileWithUri 또는 FileWithBytes union이다(README §22-1).
type FilePart struct {
	// File 은 파일 내용이다(URI 또는 bytes 중 하나를 담는다).
	File FileVariant `json:"file"`
}

// FileWithUri 는 URI로 참조하는 파일이다.
type FileWithUri struct {
	// URI 는 파일 참조 URI다.
	URI string `json:"uri"`
	// MimeType 은 파일 MIME 타입이다(선택적).
	MimeType string `json:"mimeType,omitempty"`
	// Name 은 파일 이름이다(선택적).
	Name string `json:"name,omitempty"`
}

// FileWithBytes 는 bytes(base64)로 인라인된 파일이다.
type FileWithBytes struct {
	// Bytes 는 base64 인코딩된 파일 데이터다.
	Bytes []byte `json:"bytes"`
	// MimeType 은 파일 MIME 타입이다(선택적).
	MimeType string `json:"mimeType,omitempty"`
	// Name 은 파일 이름이다(선택적).
	Name string `json:"name,omitempty"`
}

// fileWithBytesWire 는 FileWithBytes의 JSON 중간 표현이다.
// bytes 필드를 base64 문자열로 직렬화하기 위해 사용한다.
type fileWithBytesWire struct {
	Bytes    string `json:"bytes"`
	MimeType string `json:"mimeType,omitempty"`
	Name     string `json:"name,omitempty"`
}

// MarshalJSON 은 FileWithBytes.Bytes를 base64 문자열로 직렬화한다.
func (f FileWithBytes) MarshalJSON() ([]byte, error) {
	wire := fileWithBytesWire{
		Bytes:    base64.StdEncoding.EncodeToString(f.Bytes),
		MimeType: f.MimeType,
		Name:     f.Name,
	}
	return json.Marshal(wire)
}

// UnmarshalJSON 은 base64 문자열을 FileWithBytes.Bytes로 역직렬화한다.
func (f *FileWithBytes) UnmarshalJSON(data []byte) error {
	var wire fileWithBytesWire
	if err := json.Unmarshal(data, &wire); err != nil {
		return fmt.Errorf("a2a: FileWithBytes 역직렬화 실패: %w", err)
	}
	decoded, err := base64.StdEncoding.DecodeString(wire.Bytes)
	if err != nil {
		return fmt.Errorf("a2a: FileWithBytes.Bytes base64 디코딩 실패: %w", err)
	}
	f.Bytes = decoded
	f.MimeType = wire.MimeType
	f.Name = wire.Name
	return nil
}

// fileVariantKind 는 파일 변형 판별에 사용하는 내부 감지 구조체다.
type fileVariantKind struct {
	URI   *string `json:"uri"`
	Bytes *string `json:"bytes"`
}

// FileVariant 는 FileWithUri 또는 FileWithBytes union이다.
// 와이어에서 uri 필드 유무로 변형을 판별한다(README §22-1).
type FileVariant struct {
	// URI 는 URI 참조 파일이다(FileWithUri인 경우 nil이 아님).
	URI *FileWithUri
	// Bytes 는 인라인 bytes 파일이다(FileWithBytes인 경우 nil이 아님).
	Bytes *FileWithBytes
}

// MarshalJSON 은 FileVariant를 변형 판별 필드와 함께 직렬화한다.
func (f FileVariant) MarshalJSON() ([]byte, error) {
	if f.URI != nil {
		return json.Marshal(f.URI)
	}
	if f.Bytes != nil {
		return json.Marshal(f.Bytes)
	}
	return []byte("{}"), nil
}

// UnmarshalJSON 은 uri/bytes 필드 유무로 FileVariant 변형을 판별해 역직렬화한다.
func (f *FileVariant) UnmarshalJSON(data []byte) error {
	var kind fileVariantKind
	if err := json.Unmarshal(data, &kind); err != nil {
		return fmt.Errorf("a2a: FileVariant 변형 판별 실패: %w", err)
	}
	if kind.URI != nil {
		var u FileWithUri
		if err := json.Unmarshal(data, &u); err != nil {
			return fmt.Errorf("a2a: FileWithUri 역직렬화 실패: %w", err)
		}
		f.URI = &u
		return nil
	}
	if kind.Bytes != nil {
		var b FileWithBytes
		if err := json.Unmarshal(data, &b); err != nil {
			return fmt.Errorf("a2a: FileWithBytes 역직렬화 실패: %w", err)
		}
		f.Bytes = &b
		return nil
	}
	return nil
}

// partKind 는 Part 판별에 사용하는 내부 감지 구조체다.
type partKind struct {
	Kind string `json:"kind"`
}

// Part 는 메시지·아티팩트를 구성하는 단위 요소 union이다.
// 와이어에서 kind 필드("text"/"data"/"file")로 변형을 판별한다(README §22-1, SPEC §3).
type Part struct {
	// Text 는 텍스트 파트다(kind="text"인 경우 nil이 아님).
	Text *TextPart
	// Data 는 데이터 파트다(kind="data"인 경우 nil이 아님).
	Data *DataPart
	// File 은 파일 파트다(kind="file"인 경우 nil이 아님).
	File *FilePart
}

// textPartWire 는 TextPart의 JSON 와이어 표현이다.
type textPartWire struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
}

// dataPartWire 는 DataPart의 JSON 와이어 표현이다.
type dataPartWire struct {
	Kind string         `json:"kind"`
	Data map[string]any `json:"data"`
}

// filePartWire 는 FilePart의 JSON 와이어 표현이다.
type filePartWire struct {
	Kind string      `json:"kind"`
	File FileVariant `json:"file"`
}

// MarshalJSON 은 Part를 kind 필드와 함께 직렬화한다.
func (p Part) MarshalJSON() ([]byte, error) {
	if p.Text != nil {
		wire := textPartWire{Kind: "text", Text: p.Text.Text}
		return json.Marshal(wire)
	}
	if p.Data != nil {
		wire := dataPartWire{Kind: "data", Data: p.Data.Data}
		return json.Marshal(wire)
	}
	if p.File != nil {
		wire := filePartWire{Kind: "file", File: p.File.File}
		return json.Marshal(wire)
	}
	return []byte(`{"kind":"text","text":""}`), nil
}

// UnmarshalJSON 은 kind 필드로 Part 변형을 판별해 역직렬화한다.
func (p *Part) UnmarshalJSON(data []byte) error {
	var kind partKind
	if err := json.Unmarshal(data, &kind); err != nil {
		return fmt.Errorf("a2a: Part kind 판별 실패: %w", err)
	}
	switch kind.Kind {
	case "text":
		var wire textPartWire
		if err := json.Unmarshal(data, &wire); err != nil {
			return fmt.Errorf("a2a: TextPart 역직렬화 실패: %w", err)
		}
		p.Text = &TextPart{Text: wire.Text}
	case "data":
		var wire dataPartWire
		if err := json.Unmarshal(data, &wire); err != nil {
			return fmt.Errorf("a2a: DataPart 역직렬화 실패: %w", err)
		}
		p.Data = &DataPart{Data: wire.Data}
	case "file":
		var wire filePartWire
		if err := json.Unmarshal(data, &wire); err != nil {
			return fmt.Errorf("a2a: FilePart 역직렬화 실패: %w", err)
		}
		p.File = &FilePart{File: wire.File}
	default:
		return fmt.Errorf("a2a: 알 수 없는 Part kind: %q", kind.Kind)
	}
	return nil
}

// ─── Artifact ───────────────────────────────────────────────────────────────

// Artifact 는 태스크 실행 결과 아티팩트다(README §22-1).
type Artifact struct {
	// ArtifactID 는 아티팩트 고유 식별자다.
	ArtifactID string `json:"artifactId,omitempty"`
	// Name 은 아티팩트 이름이다.
	Name string `json:"name,omitempty"`
	// Description 은 아티팩트 설명이다.
	Description string `json:"description,omitempty"`
	// Parts 는 아티팩트를 구성하는 파트 목록이다.
	Parts []Part `json:"parts,omitempty"`
	// Metadata 는 아티팩트 메타데이터다.
	Metadata map[string]any `json:"metadata,omitempty"`
}

// ─── MessageSendParams / SendMessageRequest ─────────────────────────────────

// MessageSendParams 는 메시지 전송 파라미터다(README §22-1).
type MessageSendParams struct {
	// Message 는 전송할 메시지다.
	Message Message `json:"message"`
	// ContextID 는 메시지가 속할 컨텍스트 식별자다(선택적).
	ContextID string `json:"contextId,omitempty"`
	// TaskID 는 기존 태스크에 이어 붙일 경우 태스크 식별자다(선택적).
	TaskID string `json:"taskId,omitempty"`
}

// SendMessageRequest 는 JSON-RPC message/send 요청이다(README §22-1).
type SendMessageRequest struct {
	// Params 는 메시지 전송 파라미터다.
	Params MessageSendParams `json:"params"`
}

// ─── 스트리밍 이벤트 ────────────────────────────────────────────────────────

// TaskStatusUpdateEvent 는 태스크 상태 갱신 이벤트다(README §22-1).
type TaskStatusUpdateEvent struct {
	// TaskID 는 이벤트를 발생시킨 태스크 식별자다.
	TaskID string `json:"taskId"`
	// Status 는 갱신된 태스크 상태다.
	Status TaskStatus `json:"status"`
	// Final 은 이 이벤트가 최종 상태임을 나타낸다.
	Final bool `json:"final,omitempty"`
}

// TaskArtifactUpdateEvent 는 태스크 아티팩트 갱신 이벤트다(README §22-1).
type TaskArtifactUpdateEvent struct {
	// TaskID 는 이벤트를 발생시킨 태스크 식별자다.
	TaskID string `json:"taskId"`
	// Artifact 는 추가된 아티팩트다.
	Artifact Artifact `json:"artifact"`
}

// eventKind 는 Event union 판별에 사용하는 내부 감지 구조체다.
type eventKind struct {
	// ID 는 Task 이벤트 판별을 위한 필드다(Task에만 존재).
	ID *string `json:"id"`
	// TaskID 는 TaskStatusUpdateEvent/TaskArtifactUpdateEvent 판별용 필드다.
	TaskID *string `json:"taskId"`
	// Artifact 는 TaskArtifactUpdateEvent 판별용 필드다.
	Artifact *json.RawMessage `json:"artifact"`
}

// Event 는 SendMessageStreaming 채널 원소인 스트리밍 이벤트 union이다.
// Task / TaskStatusUpdateEvent / TaskArtifactUpdateEvent 세 변형을 담는다(README §22-1).
type Event struct {
	// Task 는 태스크 이벤트다(Task 변형인 경우 nil이 아님).
	Task *Task
	// StatusUpdate 는 상태 갱신 이벤트다(TaskStatusUpdateEvent 변형인 경우 nil이 아님).
	StatusUpdate *TaskStatusUpdateEvent
	// ArtifactUpdate 는 아티팩트 갱신 이벤트다(TaskArtifactUpdateEvent 변형인 경우 nil이 아님).
	ArtifactUpdate *TaskArtifactUpdateEvent
}

// MarshalJSON 은 Event를 변형 타입 그대로 직렬화한다.
func (e Event) MarshalJSON() ([]byte, error) {
	if e.Task != nil {
		return json.Marshal(e.Task)
	}
	if e.ArtifactUpdate != nil {
		return json.Marshal(e.ArtifactUpdate)
	}
	if e.StatusUpdate != nil {
		return json.Marshal(e.StatusUpdate)
	}
	return []byte("{}"), nil
}

// UnmarshalJSON 은 이벤트 필드 유무로 Event 변형을 판별해 역직렬화한다.
// 판별 순서: Task(id 필드) → TaskArtifactUpdateEvent(taskId+artifact) → TaskStatusUpdateEvent(taskId).
func (e *Event) UnmarshalJSON(data []byte) error {
	var kind eventKind
	if err := json.Unmarshal(data, &kind); err != nil {
		return fmt.Errorf("a2a: Event 변형 판별 실패: %w", err)
	}
	// id 필드가 있으면 Task다.
	if kind.ID != nil {
		var t Task
		if err := json.Unmarshal(data, &t); err != nil {
			return fmt.Errorf("a2a: Task 이벤트 역직렬화 실패: %w", err)
		}
		e.Task = &t
		return nil
	}
	// taskId + artifact 필드가 있으면 TaskArtifactUpdateEvent다.
	if kind.TaskID != nil && kind.Artifact != nil {
		var ev TaskArtifactUpdateEvent
		if err := json.Unmarshal(data, &ev); err != nil {
			return fmt.Errorf("a2a: TaskArtifactUpdateEvent 역직렬화 실패: %w", err)
		}
		e.ArtifactUpdate = &ev
		return nil
	}
	// taskId만 있으면 TaskStatusUpdateEvent다.
	if kind.TaskID != nil {
		var ev TaskStatusUpdateEvent
		if err := json.Unmarshal(data, &ev); err != nil {
			return fmt.Errorf("a2a: TaskStatusUpdateEvent 역직렬화 실패: %w", err)
		}
		e.StatusUpdate = &ev
		return nil
	}
	return fmt.Errorf("a2a: 알 수 없는 Event 변형")
}
