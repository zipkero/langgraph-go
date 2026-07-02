// integration_test.go 는 DriveClient 를 실제 Google Drive 에 연결해 검증하는 통합 테스트다.
// GOOGLE_DRIVE_CREDENTIALS_PATH/GOOGLE_DRIVE_TOKEN_PATH 환경변수가 가리키는 인증 파일이 없으면 t.Skip 으로
// 건너뛴다(크리덴셜 부재 시 skip, database/integration_test.go 패턴 참조).
// storage 클라이언트 자체는 파일 경로를 생성 인자로 받으며(ANALYSIS §5 D3), 이 환경변수는 테스트가 그 경로를
// 찾기 위한 용도일 뿐 storage 패키지의 인증 방식이 env 로 바뀌는 것은 아니다.
package storage_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/zipkero/langgraph-go/storage"
)

// skipIfNoDriveAuth 는 GOOGLE_DRIVE_CREDENTIALS_PATH/GOOGLE_DRIVE_TOKEN_PATH 가 없거나
// 가리키는 파일이 없으면 테스트를 skip 한다.
func skipIfNoDriveAuth(t *testing.T) (credentialsPath, tokenPath string) {
	t.Helper()

	credentialsPath = os.Getenv("GOOGLE_DRIVE_CREDENTIALS_PATH")
	tokenPath = os.Getenv("GOOGLE_DRIVE_TOKEN_PATH")
	if credentialsPath == "" || tokenPath == "" {
		t.Skip("GOOGLE_DRIVE_CREDENTIALS_PATH/GOOGLE_DRIVE_TOKEN_PATH 가 없으므로 실제 Drive 통합 테스트를 건너뜁니다")
	}
	if _, err := os.Stat(credentialsPath); err != nil {
		t.Skipf("credentials 파일이 없으므로 건너뜁니다: %v", err)
	}
	if _, err := os.Stat(tokenPath); err != nil {
		t.Skipf("token 파일이 없으므로 건너뜁니다: %v", err)
	}
	return credentialsPath, tokenPath
}

// TestDriveClient_Initialize_실제Drive 는 실제 Drive 인증·앱 전용 폴더 보장이 성공하는지 검증한다.
func TestDriveClient_Initialize_실제Drive(t *testing.T) {
	credentialsPath, tokenPath := skipIfNoDriveAuth(t)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	c := storage.NewDriveClient(credentialsPath, tokenPath)
	if err := c.Initialize(ctx); err != nil {
		t.Fatalf("Initialize 실패: %v", err)
	}
}

// TestDriveClient_업로드_다운로드_삭제_실제Drive 는 파일을 업로드하고 storage_ref 로 다시 다운로드한 뒤
// 정리를 위해 영구 삭제하는 흐름을 실제 Drive 로 검증한다.
func TestDriveClient_업로드_다운로드_삭제_실제Drive(t *testing.T) {
	credentialsPath, tokenPath := skipIfNoDriveAuth(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	c := storage.NewDriveClient(credentialsPath, tokenPath)
	if err := c.Initialize(ctx); err != nil {
		t.Fatalf("Initialize 실패: %v", err)
	}

	content := []byte("langgraph-go 통합 테스트 본문")
	meta, err := c.Upload(ctx, content, "langgraph-go-integration-test.txt", "text/plain")
	if err != nil {
		t.Fatalf("Upload 실패: %v", err)
	}
	defer c.Delete(ctx, meta.FileID, true)

	fileID, ok := storage.ParseStorageRef(meta.StorageRef)
	if !ok || fileID != meta.FileID {
		t.Fatalf("storage_ref 왕복 불일치: %+v", meta)
	}

	fc, err := c.DownloadByStorageRef(ctx, meta.StorageRef)
	if err != nil {
		t.Fatalf("DownloadByStorageRef 실패: %v", err)
	}
	if fc.Base64Content == "" {
		t.Error("다운로드한 본문이 비어 있음")
	}
}
