// storage_ref_test.go 는 CreateStorageRef/ParseStorageRef 순수 함수의 왕복 보존을 단정하는 단위 테스트다
// (SPEC §5.3, ANALYSIS §1.3).
package storage_test

import (
	"testing"

	"github.com/zipkero/langgraph-go/storage"
)

// TestCreateStorageRef_ParseStorageRef_왕복 은 CreateStorageRef 로 만든 storage_ref 를 ParseStorageRef 로
// 되돌렸을 때 원래 fileID 가 보존되는지 검증한다.
func TestCreateStorageRef_ParseStorageRef_왕복(t *testing.T) {
	cases := []string{"abc123", "file-with-dashes_and_underscore", "1", "1a2B3c-XYZ"}
	for _, fileID := range cases {
		ref := storage.CreateStorageRef(fileID)
		got, ok := storage.ParseStorageRef(ref)
		if !ok {
			t.Fatalf("ParseStorageRef(%q) ok=false, want true", ref)
		}
		if got != fileID {
			t.Errorf("ParseStorageRef(%q) = %q, want %q", ref, got, fileID)
		}
	}
}

// TestParseStorageRef_잘못된형식 은 storage_ref 형식이 아니거나 fileID 가 비어 있는 입력을 거부하는지 검증한다.
func TestParseStorageRef_잘못된형식(t *testing.T) {
	cases := []string{"", "not-a-ref", "gdrive://folder/abc", "gdrive://file/", "http://file/abc"}
	for _, ref := range cases {
		if _, ok := storage.ParseStorageRef(ref); ok {
			t.Errorf("ParseStorageRef(%q) ok=true, want false", ref)
		}
	}
}
