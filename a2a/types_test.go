// types_test.go 는 task-001 검증 조건인 프로토콜 타입의 JSON round-trip 보존을 단정한다.
// SPEC §5.2: 텍스트/데이터/파일 파트를 가진 메시지·태스크·아티팩트를 직렬화 후 역직렬화하면
// 종류 판별과 모든 필드 값이 원본 그대로 보존된다.
package a2a

import (
	"encoding/json"
	"reflect"
	"testing"
)

// ─── 헬퍼 ────────────────────────────────────────────────────────────────────

// roundTrip 은 v를 JSON 직렬화한 뒤 같은 타입의 새 값으로 역직렬화해 반환한다.
func roundTrip[T any](t *testing.T, v T) T {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("직렬화 실패: %v", err)
	}
	var got T
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("역직렬화 실패: %v (JSON: %s)", err, b)
	}
	return got
}

// assertEqual 은 got과 want가 deep equal하지 않으면 테스트를 실패시킨다.
func assertEqual(t *testing.T, label string, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s: got %#v, want %#v", label, got, want)
	}
}

// ─── Part round-trip ──────────────────────────────────────────────────────────

// TestPart_TextPart_RoundTrip 는 TextPart가 kind:"text"로 직렬화되고 역직렬화 시 원본과 같음을 검증한다.
func TestPart_TextPart_RoundTrip(t *testing.T) {
	original := Part{Text: &TextPart{Text: "안녕하세요"}}
	got := roundTrip(t, original)

	if got.Text == nil {
		t.Fatal("TextPart가 nil: kind 판별 실패")
	}
	assertEqual(t, "Text.Text", got.Text.Text, original.Text.Text)
	if got.Data != nil || got.File != nil {
		t.Errorf("TextPart round-trip 후 불필요한 변형 필드가 설정됨")
	}
}

// TestPart_DataPart_RoundTrip 는 DataPart가 kind:"data"로 직렬화되고 역직렬화 시 원본과 같음을 검증한다.
func TestPart_DataPart_RoundTrip(t *testing.T) {
	original := Part{Data: &DataPart{Data: map[string]any{
		"key":   "value",
		"count": float64(42), // JSON 역직렬화 시 숫자는 float64로 오므로 맞춘다.
	}}}
	got := roundTrip(t, original)

	if got.Data == nil {
		t.Fatal("DataPart가 nil: kind 판별 실패")
	}
	assertEqual(t, "Data.Data", got.Data.Data, original.Data.Data)
	if got.Text != nil || got.File != nil {
		t.Errorf("DataPart round-trip 후 불필요한 변형 필드가 설정됨")
	}
}

// TestPart_FilePart_WithUri_RoundTrip 는 FileWithUri FilePart가 kind:"file"로 직렬화되고
// 역직렬화 시 URI 변형이 보존됨을 검증한다.
func TestPart_FilePart_WithUri_RoundTrip(t *testing.T) {
	original := Part{File: &FilePart{File: FileVariant{
		URI: &FileWithUri{
			URI:      "https://example.com/file.txt",
			MimeType: "text/plain",
			Name:     "file.txt",
		},
	}}}
	got := roundTrip(t, original)

	if got.File == nil {
		t.Fatal("FilePart가 nil: kind 판별 실패")
	}
	if got.File.File.URI == nil {
		t.Fatal("FileWithUri가 nil: file variant 판별 실패")
	}
	assertEqual(t, "File.URI.URI", got.File.File.URI.URI, original.File.File.URI.URI)
	assertEqual(t, "File.URI.MimeType", got.File.File.URI.MimeType, original.File.File.URI.MimeType)
	assertEqual(t, "File.URI.Name", got.File.File.URI.Name, original.File.File.URI.Name)
	if got.File.File.Bytes != nil {
		t.Errorf("FileWithUri round-trip 후 Bytes 변형이 잘못 설정됨")
	}
}

// TestPart_FilePart_WithBytes_RoundTrip 는 FileWithBytes FilePart가 kind:"file"로 직렬화되고
// bytes가 base64 round-trip에서 보존됨을 검증한다.
func TestPart_FilePart_WithBytes_RoundTrip(t *testing.T) {
	original := Part{File: &FilePart{File: FileVariant{
		Bytes: &FileWithBytes{
			Bytes:    []byte("hello, world"),
			MimeType: "text/plain",
			Name:     "hello.txt",
		},
	}}}
	got := roundTrip(t, original)

	if got.File == nil {
		t.Fatal("FilePart가 nil: kind 판별 실패")
	}
	if got.File.File.Bytes == nil {
		t.Fatal("FileWithBytes가 nil: file variant 판별 실패")
	}
	assertEqual(t, "File.Bytes.Bytes", string(got.File.File.Bytes.Bytes), string(original.File.File.Bytes.Bytes))
	assertEqual(t, "File.Bytes.MimeType", got.File.File.Bytes.MimeType, original.File.File.Bytes.MimeType)
	assertEqual(t, "File.Bytes.Name", got.File.File.Bytes.Name, original.File.File.Bytes.Name)
	if got.File.File.URI != nil {
		t.Errorf("FileWithBytes round-trip 후 URI 변형이 잘못 설정됨")
	}
}

// ─── Part 와이어 포맷 검증 ────────────────────────────────────────────────────

// TestPart_Kind_WireFormat 은 TextPart 직렬화 결과에 kind:"text" 필드가 실제로 존재함을 검증한다.
func TestPart_Kind_WireFormat(t *testing.T) {
	p := Part{Text: &TextPart{Text: "test"}}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("직렬화 실패: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatalf("원시 역직렬화 실패: %v", err)
	}
	if raw["kind"] != "text" {
		t.Errorf("TextPart 와이어에 kind:\"text\"가 없음: %s", b)
	}
}

// TestPart_DataKind_WireFormat 은 DataPart 직렬화 결과에 kind:"data" 필드가 존재함을 검증한다.
func TestPart_DataKind_WireFormat(t *testing.T) {
	p := Part{Data: &DataPart{Data: map[string]any{"x": float64(1)}}}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("직렬화 실패: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatalf("원시 역직렬화 실패: %v", err)
	}
	if raw["kind"] != "data" {
		t.Errorf("DataPart 와이어에 kind:\"data\"가 없음: %s", b)
	}
}

// TestPart_FileKind_WireFormat 은 FilePart 직렬화 결과에 kind:"file" 필드가 존재함을 검증한다.
func TestPart_FileKind_WireFormat(t *testing.T) {
	p := Part{File: &FilePart{File: FileVariant{URI: &FileWithUri{URI: "https://x.com/f"}}}}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("직렬화 실패: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatalf("원시 역직렬화 실패: %v", err)
	}
	if raw["kind"] != "file" {
		t.Errorf("FilePart 와이어에 kind:\"file\"가 없음: %s", b)
	}
}

// ─── Message round-trip ───────────────────────────────────────────────────────

// TestMessage_RoundTrip 는 세 종류의 파트를 가진 Message가 round-trip에서 보존됨을 검증한다.
// kind:"message"가 와이어에 부착되고, Role·MessageID·Parts 모두 보존되어야 한다.
func TestMessage_RoundTrip(t *testing.T) {
	original := Message{
		Role:      RoleUser,
		MessageID: "msg-001",
		Parts: []Part{
			{Text: &TextPart{Text: "안녕하세요"}},
			{Data: &DataPart{Data: map[string]any{"k": "v"}}},
			{File: &FilePart{File: FileVariant{URI: &FileWithUri{URI: "https://example.com/f"}}}},
		},
	}
	got := roundTrip(t, original)

	assertEqual(t, "Role", got.Role, original.Role)
	assertEqual(t, "MessageID", got.MessageID, original.MessageID)
	if len(got.Parts) != 3 {
		t.Fatalf("Parts 길이 불일치: got %d, want 3", len(got.Parts))
	}
	if got.Parts[0].Text == nil || got.Parts[0].Text.Text != "안녕하세요" {
		t.Errorf("Parts[0] TextPart 보존 실패")
	}
	if got.Parts[1].Data == nil {
		t.Errorf("Parts[1] DataPart 보존 실패")
	}
	if got.Parts[2].File == nil || got.Parts[2].File.File.URI == nil {
		t.Errorf("Parts[2] FilePart 보존 실패")
	}
}

// TestMessage_Kind_WireFormat 은 Message 직렬화 결과에 kind:"message" 필드가 존재함을 검증한다.
func TestMessage_Kind_WireFormat(t *testing.T) {
	msg := Message{Role: RoleAgent, Parts: []Part{{Text: &TextPart{Text: "응답"}}}}
	b, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("직렬화 실패: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatalf("원시 역직렬화 실패: %v", err)
	}
	if raw["kind"] != "message" {
		t.Errorf("Message 와이어에 kind:\"message\"가 없음: %s", b)
	}
}

// ─── TaskState 와이어 포맷 검증 ─────────────────────────────────────────────────

// TestTaskState_WireValues 는 TaskState 상수가 명세 문자열로 직렬화됨을 검증한다.
func TestTaskState_WireValues(t *testing.T) {
	cases := []struct {
		state TaskState
		wire  string
	}{
		{TaskStateWorking, `"working"`},
		{TaskStateInputRequired, `"input_required"`},
		{TaskStateCompleted, `"completed"`},
		{TaskStateFailed, `"failed"`},
		{TaskStateCanceled, `"canceled"`},
	}
	for _, c := range cases {
		b, err := json.Marshal(c.state)
		if err != nil {
			t.Fatalf("TaskState(%s) 직렬화 실패: %v", c.state, err)
		}
		if string(b) != c.wire {
			t.Errorf("TaskState(%s): got %s, want %s", c.state, b, c.wire)
		}
	}
}

// ─── Task round-trip ──────────────────────────────────────────────────────────

// TestTask_RoundTrip 는 상태·이력·아티팩트를 가진 Task가 round-trip에서 보존됨을 검증한다.
func TestTask_RoundTrip(t *testing.T) {
	original := Task{
		ID:        "task-001",
		ContextID: "ctx-abc",
		Status: TaskStatus{
			State: TaskStateWorking,
			Message: &Message{
				Role:  RoleAgent,
				Parts: []Part{{Text: &TextPart{Text: "처리 중"}}},
			},
		},
		History: []Message{
			{Role: RoleUser, Parts: []Part{{Text: &TextPart{Text: "질문"}}}},
		},
		Artifacts: []Artifact{
			{
				ArtifactID: "art-001",
				Name:       "결과",
				Parts: []Part{
					{Text: &TextPart{Text: "결과 텍스트"}},
					{Data: &DataPart{Data: map[string]any{"score": float64(99)}}},
				},
			},
		},
	}
	got := roundTrip(t, original)

	assertEqual(t, "ID", got.ID, original.ID)
	assertEqual(t, "ContextID", got.ContextID, original.ContextID)
	assertEqual(t, "Status.State", got.Status.State, original.Status.State)
	if got.Status.Message == nil {
		t.Fatal("Status.Message가 nil")
	}
	if len(got.History) != 1 {
		t.Fatalf("History 길이 불일치: got %d, want 1", len(got.History))
	}
	if len(got.Artifacts) != 1 {
		t.Fatalf("Artifacts 길이 불일치: got %d, want 1", len(got.Artifacts))
	}
	art := got.Artifacts[0]
	assertEqual(t, "Artifacts[0].ArtifactID", art.ArtifactID, original.Artifacts[0].ArtifactID)
	assertEqual(t, "Artifacts[0].Name", art.Name, original.Artifacts[0].Name)
	if len(art.Parts) != 2 {
		t.Fatalf("Artifacts[0].Parts 길이 불일치: got %d, want 2", len(art.Parts))
	}
	if art.Parts[0].Text == nil || art.Parts[0].Text.Text != "결과 텍스트" {
		t.Errorf("Artifacts[0].Parts[0] TextPart 보존 실패")
	}
	if art.Parts[1].Data == nil {
		t.Errorf("Artifacts[0].Parts[1] DataPart 보존 실패")
	}
}

// TestTask_camelCase_WireFormat 은 Task의 JSON 필드명이 camelCase로 나감을 검증한다.
func TestTask_camelCase_WireFormat(t *testing.T) {
	task := Task{
		ID:        "t1",
		ContextID: "c1",
		Status:    TaskStatus{State: TaskStateCompleted},
	}
	b, err := json.Marshal(task)
	if err != nil {
		t.Fatalf("직렬화 실패: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatalf("원시 역직렬화 실패: %v", err)
	}
	// contextId 필드가 camelCase로 나와야 한다.
	if _, ok := raw["contextId"]; !ok {
		t.Errorf("Task 와이어에 contextId 필드가 없음(camelCase 위반): %s", b)
	}
}

// ─── Artifact round-trip ──────────────────────────────────────────────────────

// TestArtifact_RoundTrip 는 Artifact가 텍스트·데이터·파일 파트와 함께 round-trip에서 보존됨을 검증한다.
func TestArtifact_RoundTrip(t *testing.T) {
	original := Artifact{
		ArtifactID:  "art-xyz",
		Name:        "분석 결과",
		Description: "LLM 분석 결과물",
		Parts: []Part{
			{Text: &TextPart{Text: "요약 텍스트"}},
			{Data: &DataPart{Data: map[string]any{"items": []any{"a", "b"}}}},
			{File: &FilePart{File: FileVariant{
				Bytes: &FileWithBytes{
					Bytes:    []byte{0x50, 0x4B},
					MimeType: "application/zip",
					Name:     "result.zip",
				},
			}}},
		},
		Metadata: map[string]any{"version": float64(1)},
	}
	got := roundTrip(t, original)

	assertEqual(t, "ArtifactID", got.ArtifactID, original.ArtifactID)
	assertEqual(t, "Name", got.Name, original.Name)
	assertEqual(t, "Description", got.Description, original.Description)
	if len(got.Parts) != 3 {
		t.Fatalf("Parts 길이 불일치: got %d, want 3", len(got.Parts))
	}
	// TextPart 보존
	if got.Parts[0].Text == nil || got.Parts[0].Text.Text != "요약 텍스트" {
		t.Errorf("Parts[0] TextPart 보존 실패")
	}
	// DataPart 보존
	if got.Parts[1].Data == nil {
		t.Errorf("Parts[1] DataPart 보존 실패")
	}
	// FilePart(Bytes) 보존
	if got.Parts[2].File == nil || got.Parts[2].File.File.Bytes == nil {
		t.Errorf("Parts[2] FileWithBytes 보존 실패")
	}
	if string(got.Parts[2].File.File.Bytes.Bytes) != string(original.Parts[2].File.File.Bytes.Bytes) {
		t.Errorf("Parts[2] Bytes 데이터 불일치")
	}
	assertEqual(t, "Metadata", got.Metadata, original.Metadata)
}

// ─── Event union round-trip ───────────────────────────────────────────────────

// TestEvent_Task_RoundTrip 는 Task Event 변형이 round-trip에서 보존됨을 검증한다.
func TestEvent_Task_RoundTrip(t *testing.T) {
	original := Event{
		Task: &Task{
			ID:     "t1",
			Status: TaskStatus{State: TaskStateCompleted},
		},
	}
	got := roundTrip(t, original)

	if got.Task == nil {
		t.Fatal("Task 이벤트 변형 판별 실패: Task가 nil")
	}
	assertEqual(t, "Task.ID", got.Task.ID, original.Task.ID)
	assertEqual(t, "Task.Status.State", got.Task.Status.State, original.Task.Status.State)
	if got.StatusUpdate != nil || got.ArtifactUpdate != nil {
		t.Errorf("Task 이벤트 round-trip 후 불필요한 변형이 설정됨")
	}
}

// TestEvent_StatusUpdate_RoundTrip 는 TaskStatusUpdateEvent Event 변형이 round-trip에서 보존됨을 검증한다.
func TestEvent_StatusUpdate_RoundTrip(t *testing.T) {
	original := Event{
		StatusUpdate: &TaskStatusUpdateEvent{
			TaskID: "t2",
			Status: TaskStatus{State: TaskStateWorking},
			Final:  false,
		},
	}
	got := roundTrip(t, original)

	if got.StatusUpdate == nil {
		t.Fatal("StatusUpdate 이벤트 변형 판별 실패: StatusUpdate가 nil")
	}
	assertEqual(t, "StatusUpdate.TaskID", got.StatusUpdate.TaskID, original.StatusUpdate.TaskID)
	assertEqual(t, "StatusUpdate.Status.State", got.StatusUpdate.Status.State, original.StatusUpdate.Status.State)
	if got.Task != nil || got.ArtifactUpdate != nil {
		t.Errorf("StatusUpdate 이벤트 round-trip 후 불필요한 변형이 설정됨")
	}
}

// TestEvent_ArtifactUpdate_RoundTrip 는 TaskArtifactUpdateEvent Event 변형이 round-trip에서 보존됨을 검증한다.
func TestEvent_ArtifactUpdate_RoundTrip(t *testing.T) {
	original := Event{
		ArtifactUpdate: &TaskArtifactUpdateEvent{
			TaskID: "t3",
			Artifact: Artifact{
				ArtifactID: "art-1",
				Parts:      []Part{{Text: &TextPart{Text: "결과"}}},
			},
		},
	}
	got := roundTrip(t, original)

	if got.ArtifactUpdate == nil {
		t.Fatal("ArtifactUpdate 이벤트 변형 판별 실패: ArtifactUpdate가 nil")
	}
	assertEqual(t, "ArtifactUpdate.TaskID", got.ArtifactUpdate.TaskID, original.ArtifactUpdate.TaskID)
	assertEqual(t, "ArtifactUpdate.Artifact.ArtifactID", got.ArtifactUpdate.Artifact.ArtifactID, original.ArtifactUpdate.Artifact.ArtifactID)
	if got.Task != nil || got.StatusUpdate != nil {
		t.Errorf("ArtifactUpdate 이벤트 round-trip 후 불필요한 변형이 설정됨")
	}
}

// ─── AgentCard round-trip ─────────────────────────────────────────────────────

// TestAgentCard_RoundTrip 는 AgentCard가 round-trip에서 보존됨을 검증한다.
func TestAgentCard_RoundTrip(t *testing.T) {
	original := AgentCard{
		Name:               "TestAgent",
		Description:        "테스트 에이전트",
		URL:                "http://localhost:8080",
		Version:            "1.0.0",
		DefaultInputModes:  []string{"text"},
		DefaultOutputModes: []string{"text"},
		Capabilities: AgentCapabilities{
			Streaming:   true,
			InputModes:  []string{"text"},
			OutputModes: []string{"text", "data"},
		},
		Skills: []AgentSkill{
			{
				ID:          "skill-1",
				Name:        "검색",
				Description: "웹 검색 스킬",
				Tags:        []string{"search", "web"},
				Examples:    []string{"최신 뉴스 검색"},
			},
		},
	}
	got := roundTrip(t, original)

	assertEqual(t, "Name", got.Name, original.Name)
	assertEqual(t, "URL", got.URL, original.URL)
	assertEqual(t, "Version", got.Version, original.Version)
	assertEqual(t, "Capabilities.Streaming", got.Capabilities.Streaming, original.Capabilities.Streaming)
	if len(got.Skills) != 1 {
		t.Fatalf("Skills 길이 불일치: got %d, want 1", len(got.Skills))
	}
	assertEqual(t, "Skills[0].ID", got.Skills[0].ID, original.Skills[0].ID)
}
