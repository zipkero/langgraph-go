// validate.go 는 Compile이 호출하는 그래프 구조 검증 로직을 담는다.
// 미정의 노드를 가리키는 엣지와 진입점에서 도달 불가한 노드를 컴파일 시 거부한다(D3, §5.2).
package graph

import (
	"fmt"
	"strings"
)

// validate 는 Builder의 구성이 유효한지 검사하고 위반 시 error를 반환한다.
// 검사 순서:
//  1. 진입점이 정의됐는지
//  2. 모든 엣지(정적·조건 엣지·조건 진입점)가 정의된 노드만 참조하는지
//  3. 진입점에서 BFS로 도달 불가한 노드가 없는지
func validate(b *Builder) error {
	// 진입점 존재 확인
	if b.entryPoint == "" && b.condEntry == nil {
		return fmt.Errorf("graph: 진입점이 설정되지 않았습니다(SetEntryPoint 또는 SetConditionalEntryPoint 필요)")
	}

	// 1단계: 미정의 노드 참조 검사
	if err := validateNodeRefs(b); err != nil {
		return err
	}

	// 2단계: 도달 불가 노드 검사
	if err := validateReachability(b); err != nil {
		return err
	}

	return nil
}

// validateNodeRefs 는 엣지·진입점이 정의된 노드만 참조하는지 확인한다.
func validateNodeRefs(b *Builder) error {
	defined := b.nodes

	// 정적 진입점 검사
	if b.entryPoint != "" {
		if _, ok := defined[b.entryPoint]; !ok {
			return fmt.Errorf("graph: 진입점 %q가 정의된 노드가 아닙니다", b.entryPoint)
		}
	}

	// 조건 진입점 매핑 검사
	if b.condEntry != nil {
		for key, target := range b.condEntry.mapping {
			if _, ok := defined[target]; !ok {
				return fmt.Errorf("graph: 조건 진입점 mapping의 키 %q가 가리키는 노드 %q가 정의되지 않았습니다", key, target)
			}
		}
	}

	// 정적 엣지 검사
	for _, e := range b.edges {
		if _, ok := defined[e.from]; !ok {
			return fmt.Errorf("graph: 엣지 from %q가 정의된 노드가 아닙니다", e.from)
		}
		if _, ok := defined[e.to]; !ok {
			return fmt.Errorf("graph: 엣지 to %q가 정의된 노드가 아닙니다(from: %q)", e.to, e.from)
		}
	}

	// 조건 엣지 검사
	for _, ce := range b.condEdges {
		if _, ok := defined[ce.from]; !ok {
			return fmt.Errorf("graph: 조건 엣지 from %q가 정의된 노드가 아닙니다", ce.from)
		}
		for key, target := range ce.mapping {
			if _, ok := defined[target]; !ok {
				return fmt.Errorf("graph: 조건 엣지(from=%q) mapping의 키 %q가 가리키는 노드 %q가 정의되지 않았습니다",
					ce.from, key, target)
			}
		}
	}

	return nil
}

// validateReachability 는 BFS로 진입점에서 모든 노드에 도달 가능한지 확인한다.
// 진입점이 조건 진입점인 경우 mapping의 모든 첫 노드에서 BFS를 시작한다.
func validateReachability(b *Builder) error {
	// 진행 그래프 구성: from → []to
	adj := buildAdjacency(b)

	// BFS 시작 노드 결정
	starts := collectStartNodes(b)
	if len(starts) == 0 {
		return nil
	}

	// BFS
	visited := make(map[string]bool)
	queue := make([]string, 0, len(starts))
	for _, s := range starts {
		if !visited[s] {
			visited[s] = true
			queue = append(queue, s)
		}
	}

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, next := range adj[cur] {
			if !visited[next] {
				visited[next] = true
				queue = append(queue, next)
			}
		}
	}

	// 도달 불가 노드 수집
	var unreachable []string
	for name := range b.nodes {
		if !visited[name] {
			unreachable = append(unreachable, name)
		}
	}

	if len(unreachable) > 0 {
		return fmt.Errorf("graph: 진입점에서 도달 불가한 노드가 있습니다: [%s]", strings.Join(unreachable, ", "))
	}

	return nil
}

// buildAdjacency 는 빌더의 엣지 정보로 인접 리스트를 만든다.
// 조건 엣지의 mapping 값과 노드의 WithDestinations 선언도 인접 노드로 포함한다.
func buildAdjacency(b *Builder) map[string][]string {
	adj := make(map[string][]string)

	// 모든 노드를 키로 초기화(간선이 없는 노드도 포함)
	for name := range b.nodes {
		adj[name] = nil
	}

	// 정적 엣지
	for _, e := range b.edges {
		adj[e.from] = append(adj[e.from], e.to)
	}

	// 조건 엣지(매핑의 모든 대상 노드를 인접으로 취급)
	for _, ce := range b.condEdges {
		for _, target := range ce.mapping {
			adj[ce.from] = append(adj[ce.from], target)
		}
	}

	// WithDestinations 선언(command.Goto로 이동 가능한 노드)도 인접으로 취급
	for name, ne := range b.nodes {
		adj[name] = append(adj[name], ne.destinations...)
	}

	return adj
}

// collectStartNodes 는 BFS의 시작 노드 목록을 결정한다.
// 정적 진입점이 있으면 그것만, 조건 진입점만 있으면 mapping 값 전체를 시작점으로 한다.
func collectStartNodes(b *Builder) []string {
	if b.entryPoint != "" {
		return []string{b.entryPoint}
	}
	if b.condEntry != nil {
		starts := make([]string, 0, len(b.condEntry.mapping))
		for _, target := range b.condEntry.mapping {
			starts = append(starts, target)
		}
		return starts
	}
	return nil
}
