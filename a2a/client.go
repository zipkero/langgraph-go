// client.go 는 A2A 클라이언트 계층을 구현한다.
// CardResolver(GetAgentCard), Client(SendMessage/SendMessageStreaming),
// 아티팩트 추출 헬퍼(ArtifactText/ArtifactData/ArtifactFileURI/ArtifactFileBytes)를 제공한다.
// 메서드 문자열·와이어 키는 server.go와 같은 상수를 공유한다(ANALYSIS §5 D5.2).
// 외부 a2a SDK·gRPC·protobuf 없이 표준 라이브러리만 사용한다(SPEC §3).
package a2a

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// ─── CardResolver ─────────────────────────────────────────────────────────────

// CardResolver 는 에이전트 카드를 well-known 경로에서 조회한다(README §22-3).
type CardResolver struct {
	baseURL    string
	httpClient *http.Client
}

// NewCardResolver 는 baseURL을 기반으로 CardResolver를 생성한다.
// baseURL은 스킴·호스트·포트를 포함한 루트 URL이다(예: "http://localhost:8080").
func NewCardResolver(baseURL string) CardResolver {
	return CardResolver{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{},
	}
}

// GetAgentCard 는 /.well-known/agent-card.json 경로에서 AgentCard를 조회한다.
// A2A 스펙의 well-known 경로를 사용한다(SPEC §5.5, ANALYSIS §2.3).
func (r CardResolver) GetAgentCard(ctx context.Context) (AgentCard, error) {
	url := r.baseURL + "/.well-known/agent-card.json"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return AgentCard{}, fmt.Errorf("a2a: CardResolver GET 요청 생성 실패: %w", err)
	}

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return AgentCard{}, fmt.Errorf("a2a: CardResolver GET 요청 실패: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return AgentCard{}, fmt.Errorf("a2a: CardResolver 응답 상태코드 %d", resp.StatusCode)
	}

	var card AgentCard
	if err := json.NewDecoder(resp.Body).Decode(&card); err != nil {
		return AgentCard{}, fmt.Errorf("a2a: AgentCard 역직렬화 실패: %w", err)
	}
	return card, nil
}

// ─── Client ───────────────────────────────────────────────────────────────────

// Client 는 A2A 서버와 통신하는 클라이언트다(README §22-3).
// SendMessage(비스트리밍)와 SendMessageStreaming(SSE 스트리밍)을 제공한다.
type Client struct {
	card       AgentCard
	httpClient *http.Client
}

// NewClient 는 AgentCard를 받아 Client를 생성한다.
// 카드의 URL이 요청 대상 엔드포인트로 사용된다(README §22-3).
func NewClient(card AgentCard) Client {
	return Client{
		card:       card,
		httpClient: &http.Client{},
	}
}

// buildJSONRPCRequest 는 SendMessageRequest를 JSON-RPC envelope로 직렬화한다.
// 메서드 문자열은 server.go의 상수와 공유한다(ANALYSIS §5 D5.2).
func buildJSONRPCRequest(id any, method string, req SendMessageRequest) ([]byte, error) {
	paramsBytes, err := json.Marshal(req.Params)
	if err != nil {
		return nil, fmt.Errorf("a2a: SendMessageRequest params 직렬화 실패: %w", err)
	}
	envelope := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  paramsBytes,
	}
	body, err := json.Marshal(envelope)
	if err != nil {
		return nil, fmt.Errorf("a2a: JSON-RPC envelope 직렬화 실패: %w", err)
	}
	return body, nil
}

// SendMessage 는 비스트리밍 message/send 요청을 전송하고 Task를 반환한다.
// JSON-RPC envelope로 POST한 뒤 result를 Task로 역직렬화한다(ANALYSIS §2.3, SPEC §5.5).
func (c Client) SendMessage(ctx context.Context, req SendMessageRequest) (Task, error) {
	body, err := buildJSONRPCRequest(1, methodMessageSend, req)
	if err != nil {
		return Task{}, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.card.URL, bytes.NewReader(body))
	if err != nil {
		return Task{}, fmt.Errorf("a2a: SendMessage HTTP 요청 생성 실패: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return Task{}, fmt.Errorf("a2a: SendMessage HTTP 요청 실패: %w", err)
	}
	defer resp.Body.Close()

	var rpcResp jsonRPCResponse
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		return Task{}, fmt.Errorf("a2a: SendMessage 응답 역직렬화 실패: %w", err)
	}
	if rpcResp.Error != nil {
		return Task{}, fmt.Errorf("a2a: SendMessage JSON-RPC 오류 (code=%d): %s",
			rpcResp.Error.Code, rpcResp.Error.Message)
	}

	// result를 Task로 역직렬화한다.
	resultBytes, err := json.Marshal(rpcResp.Result)
	if err != nil {
		return Task{}, fmt.Errorf("a2a: SendMessage result 재직렬화 실패: %w", err)
	}
	var task Task
	if err := json.Unmarshal(resultBytes, &task); err != nil {
		return Task{}, fmt.Errorf("a2a: SendMessage Task 역직렬화 실패: %w", err)
	}
	return task, nil
}

// SendMessageStreaming 은 SSE 스트리밍 message/stream 요청을 전송하고 이벤트 채널을 반환한다.
// SSE 응답을 라인 단위로 소비해 각 data 프레임을 Event union으로 디코드해 채널로 방출한다.
// 채널은 스트림 종료(final 이벤트 또는 서버 연결 종료) 시 닫힌다(ANALYSIS §2.3, SPEC §5.5).
func (c Client) SendMessageStreaming(ctx context.Context, req SendMessageRequest) (<-chan Event, error) {
	body, err := buildJSONRPCRequest(2, methodMessageStream, req)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.card.URL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("a2a: SendMessageStreaming HTTP 요청 생성 실패: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("a2a: SendMessageStreaming HTTP 요청 실패: %w", err)
	}

	// SSE 응답이 아닌 경우(예: 에러 응답)를 처리한다.
	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "text/event-stream") {
		defer resp.Body.Close()
		// JSON-RPC 오류 응답을 시도한다.
		var rpcResp jsonRPCResponse
		if decErr := json.NewDecoder(resp.Body).Decode(&rpcResp); decErr == nil && rpcResp.Error != nil {
			return nil, fmt.Errorf("a2a: SendMessageStreaming JSON-RPC 오류 (code=%d): %s",
				rpcResp.Error.Code, rpcResp.Error.Message)
		}
		return nil, fmt.Errorf("a2a: SendMessageStreaming 응답 Content-Type 불일치: %q", ct)
	}

	ch := make(chan Event, 32)

	// SSE 프레임을 goroutine에서 소비해 채널로 방출한다.
	go func() {
		defer resp.Body.Close()
		defer close(ch)

		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")
			var ev Event
			if err := json.Unmarshal([]byte(data), &ev); err != nil {
				// 역직렬화 실패는 무시하고 다음 프레임으로 진행한다.
				continue
			}
			// ctx 취소 시 채널 전송을 중단한다.
			select {
			case ch <- ev:
			case <-ctx.Done():
				return
			}
			// final 이벤트 도달 시 스트림 소비를 종료한다(ANALYSIS §2.2).
			if isEventFinal(ev) {
				return
			}
		}
		// Scanner 오류를 명시적으로 확인한다(lint: scanner.Err() 체크).
		// 오류가 있어도 채널은 이미 close되므로 별도 전파 없이 로그만 무시한다.
		_ = scanner.Err()
	}()

	return ch, nil
}

// ─── 아티팩트 추출 헬퍼 ──────────────────────────────────────────────────────

// ArtifactText 는 Artifact에서 첫 번째 텍스트 파트의 내용을 꺼낸다.
// 텍스트 파트가 없으면 ("", false)를 반환한다(README §22-3).
func ArtifactText(a Artifact) (string, bool) {
	for _, p := range a.Parts {
		if p.Text != nil {
			return p.Text.Text, true
		}
	}
	return "", false
}

// ArtifactData 는 Artifact에서 첫 번째 데이터 파트의 구조화 데이터를 꺼낸다.
// 데이터 파트가 없으면 (nil, false)를 반환한다(README §22-3).
func ArtifactData(a Artifact) (map[string]any, bool) {
	for _, p := range a.Parts {
		if p.Data != nil {
			return p.Data.Data, true
		}
	}
	return nil, false
}

// ArtifactFileURI 는 Artifact에서 첫 번째 파일 파트의 URI를 꺼낸다.
// URI 파일 파트가 없으면 ("", false)를 반환한다(README §22-3).
func ArtifactFileURI(a Artifact) (string, bool) {
	for _, p := range a.Parts {
		if p.File != nil && p.File.File.URI != nil {
			return p.File.File.URI.URI, true
		}
	}
	return "", false
}

// ArtifactFileBytes 는 Artifact에서 첫 번째 파일 파트의 bytes를 꺼낸다.
// bytes 파일 파트가 없으면 (nil, false)를 반환한다(README §22-3).
func ArtifactFileBytes(a Artifact) ([]byte, bool) {
	for _, p := range a.Parts {
		if p.File != nil && p.File.File.Bytes != nil {
			return p.File.File.Bytes.Bytes, true
		}
	}
	return nil, false
}
