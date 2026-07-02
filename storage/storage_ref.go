// storage_ref.go 는 storage_ref 문자열(<scheme>://file/{id} 형식)의 생성·해소를 담당한다.
// 외부 의존 없는 순수 함수라 단위 테스트로 완전히 커버된다(ANALYSIS §1.3).
package storage

import "strings"

// storageRefScheme 은 storage_ref 의 scheme 부분이다. 현재 유일한 백엔드인 Google Drive 를 가리킨다.
const storageRefScheme = "gdrive"

// storageRefPrefix 는 storage_ref 의 고정 접두사(scheme://file/)다.
const storageRefPrefix = storageRefScheme + "://file/"

// CreateStorageRef 는 fileID 로 storage_ref 문자열(<scheme>://file/{id})을 만든다.
func CreateStorageRef(fileID string) string {
	return storageRefPrefix + fileID
}

// ParseStorageRef 는 storage_ref 문자열에서 fileID 를 추출한다.
// ref 가 storage_ref 형식이 아니거나 fileID 가 비어 있으면 ok=false 를 반환한다.
func ParseStorageRef(ref string) (fileID string, ok bool) {
	if !strings.HasPrefix(ref, storageRefPrefix) {
		return "", false
	}
	id := strings.TrimPrefix(ref, storageRefPrefix)
	if id == "" {
		return "", false
	}
	return id, true
}
