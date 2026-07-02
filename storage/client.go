// storage 패키지는 외부 파일 스토리지 접근과 storage_ref 추상화, 그 도구화를 담당한다.
// Client 인터페이스는 구체 구현(예: Google Drive 백엔드)과 무관하게 유지되며,
// tool·표준 라이브러리·스토리지 SDK 에만 의존한다(상위 vectorstore/rag 등은 참조하지 않는다).
package storage

import (
	"context"
	"time"
)

// FileMetadata 는 스토리지에 저장된 파일 하나의 메타데이터다.
type FileMetadata struct {
	// FileID 는 스토리지 백엔드 내부 파일 식별자다.
	FileID string
	// StorageRef 는 이 파일을 가리키는 storage_ref 문자열(CreateStorageRef 결과)이다.
	StorageRef string
	// Filename 은 파일명이다.
	Filename string
	// MimeType 은 파일의 MIME 타입이다.
	MimeType string
	// Size 는 파일 크기(바이트)다.
	Size int64
	// CreatedAt 은 파일 생성 시각이다.
	CreatedAt time.Time
	// UpdatedAt 은 파일 최종 수정 시각이다.
	UpdatedAt time.Time
	// WebViewLink 는 웹에서 파일을 열람할 수 있는 링크다.
	WebViewLink string
}

// FileContent 는 다운로드한 파일의 메타데이터와 base64 인코딩된 본문이다.
// 문서형 파일(Google Docs 등)은 변환 export 로, 일반 파일은 미디어 다운로드로 채워진다.
type FileContent struct {
	FileMetadata
	// Base64Content 는 파일 본문을 base64 로 인코딩한 문자열이다.
	Base64Content string
}

// FolderMetadata 는 폴더 하나의 메타데이터다.
type FolderMetadata struct {
	// FolderID 는 스토리지 백엔드 내부 폴더 식별자다.
	FolderID string
	// Name 은 폴더 이름이다.
	Name string
	// ParentID 는 상위 폴더 식별자다. 루트 폴더면 빈 문자열이다.
	ParentID string
}

// ListOptions 는 ListFiles 의 목록 필터다.
type ListOptions struct {
	// FolderID 는 이 폴더 내부 파일만 조회할 때 지정한다. 비어 있으면 폴더로 필터링하지 않는다.
	FolderID string
	// MimeType 은 이 MIME 타입 파일만 조회할 때 지정한다. 비어 있으면 MIME 타입으로 필터링하지 않는다.
	MimeType string
	// PageSize 는 반환받을 최대 결과 수다. 0 이하이면 백엔드 기본값을 사용한다.
	PageSize int
	// Query 는 백엔드 검색 쿼리 문법을 그대로 추가할 자유 형식 필터 조각이다. 비어 있으면 추가하지 않는다.
	Query string
}

// uploadConfig 는 UploadOption 이 채우는 Upload 내부 설정이다.
type uploadConfig struct {
	// folderID 는 업로드 대상 폴더 ID다. 비어 있으면 구현체 기본 폴더를 사용한다.
	folderID string
}

// UploadOption 은 Upload 호출을 조정하는 가변 옵션이다.
type UploadOption func(*uploadConfig)

// WithUploadFolder 는 업로드 대상 폴더 ID를 지정한다.
func WithUploadFolder(folderID string) UploadOption {
	return func(c *uploadConfig) {
		c.folderID = folderID
	}
}

// Client 는 외부 파일 스토리지 접근 계약이다.
// 구체 구현체(예: Google Drive 기반 DriveClient)와 무관하게 이 인터페이스만으로 호출자가 상호작용한다.
type Client interface {
	// Initialize 는 인증을 수행하고 이후 호출에 필요한 상태(예: 앱 전용 폴더)를 준비한다.
	Initialize(ctx context.Context) error

	// Upload 는 content 를 filename/mimeType 으로 업로드한다.
	Upload(ctx context.Context, content []byte, filename, mimeType string, opts ...UploadOption) (FileMetadata, error)
	// DownloadAsBase64 는 fileID 파일을 base64 로 다운로드한다.
	DownloadAsBase64(ctx context.Context, fileID string) (FileContent, error)
	// DownloadByStorageRef 는 storage_ref 문자열로 파일을 해소해 다운로드한다.
	DownloadByStorageRef(ctx context.Context, ref string) (FileContent, error)
	// ListFiles 는 opts 필터로 파일 목록을 조회한다.
	ListFiles(ctx context.Context, opts ListOptions) ([]FileMetadata, error)
	// GetFileInfo 는 fileID 파일의 메타데이터를 조회한다.
	GetFileInfo(ctx context.Context, fileID string) (FileMetadata, error)
	// Update 는 fileID 파일의 본문·이름을 수정한다. content 가 nil이면 본문을 유지하고,
	// newName 이 빈 문자열이면 이름을 유지한다.
	Update(ctx context.Context, fileID string, content []byte, newName string) (FileMetadata, error)
	// Delete 는 fileID 파일을 삭제한다. permanent 가 true면 영구 삭제, false면 휴지통으로 이동한다.
	Delete(ctx context.Context, fileID string, permanent bool) error
	// FindFolderByName 은 name 과 일치하는 폴더를 검색한다.
	FindFolderByName(ctx context.Context, name string) ([]FolderMetadata, error)
	// CreateFolder 는 parentID 아래에 name 폴더를 생성한다. parentID 가 빈 문자열이면 루트에 생성한다.
	CreateFolder(ctx context.Context, name, parentID string) (FolderMetadata, error)
}
