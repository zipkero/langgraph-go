// driveclient.go 는 Client 인터페이스의 Google Drive 기반 구현체(DriveClient)를 담는다.
// OAuth credentials/token 은 env 가 아니라 파일 경로로 생성 인자에서 주입받아 Initialize 가 로드·리프레시한다
// (README §24, ANALYSIS §5 D3).
package storage

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"
)

// folderMimeType 은 Drive 상에서 폴더를 나타내는 MIME 타입이다.
const folderMimeType = "application/vnd.google-apps.folder"

// defaultAppFolderName 은 Initialize 가 보장하는 앱 전용 폴더의 기본 이름이다.
const defaultAppFolderName = "langgraph-go-storage"

// driveFileFields 는 File 조회·생성·수정 응답에서 FileMetadata 로 변환할 때 필요한 필드 목록이다.
const driveFileFields = "id, name, mimeType, size, createdTime, modifiedTime, webViewLink"

// googleDocsExportMimeTypes 는 구글 문서형(Docs/Sheets/Slides) 파일을 다운로드할 때 변환할 대상 MIME 타입이다.
// 이 파일들은 바이너리 본문이 없어 미디어 다운로드가 아니라 export 로만 받을 수 있다.
var googleDocsExportMimeTypes = map[string]string{
	"application/vnd.google-apps.document":     "application/pdf",
	"application/vnd.google-apps.spreadsheet":  "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	"application/vnd.google-apps.presentation": "application/vnd.openxmlformats-officedocument.presentationml.presentation",
}

// errNotInitialized 는 Initialize 를 호출하지 않은 상태에서 메서드가 호출됐을 때 반환하는 에러다.
var errNotInitialized = fmt.Errorf("storage: Initialize 가 호출되지 않았습니다")

// DriveOption 은 DriveClient 생성 옵션이다.
type DriveOption func(*DriveClient)

// WithAppFolderName 은 Initialize 가 보장할 앱 전용 폴더 이름을 재정의한다.
func WithAppFolderName(name string) DriveOption {
	return func(c *DriveClient) {
		c.appFolderName = name
	}
}

// DriveClient 는 Google Drive 로 Client 계약을 구현하는 백엔드다.
type DriveClient struct {
	// credentialsPath 는 OAuth client credentials(credentials.json) 파일 경로다.
	credentialsPath string
	// tokenPath 는 OAuth 토큰(token.json) 파일 경로다.
	tokenPath string
	// appFolderName 은 Initialize 가 보장할 앱 전용 폴더 이름이다.
	appFolderName string
	// svc 는 Initialize 이후 사용하는 Drive API 서비스 클라이언트다. Initialize 전에는 nil이다.
	svc *drive.Service
	// appFolderID 는 Initialize 가 보장한 앱 전용 폴더의 ID다. Upload 가 폴더 미지정 시 기본값으로 쓴다.
	appFolderID string
}

// NewDriveClient 는 credentialsPath/tokenPath 로 DriveClient 를 생성한다.
// 실제 인증·앱 전용 폴더 보장은 Initialize 호출 시 이루어진다.
func NewDriveClient(credentialsPath, tokenPath string, opts ...DriveOption) *DriveClient {
	c := &DriveClient{
		credentialsPath: credentialsPath,
		tokenPath:       tokenPath,
		appFolderName:   defaultAppFolderName,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Initialize 는 credentialsPath/tokenPath 파일에서 OAuth 설정·토큰을 읽어 Drive 서비스를 생성하고,
// 앱 전용 폴더(appFolderName)를 찾거나 없으면 생성해 이후 Upload 기본 폴더로 보관한다.
func (c *DriveClient) Initialize(ctx context.Context) error {
	credBytes, err := os.ReadFile(c.credentialsPath)
	if err != nil {
		return fmt.Errorf("storage: OAuth credentials 파일 읽기 실패: %w", err)
	}
	cfg, err := google.ConfigFromJSON(credBytes, drive.DriveScope)
	if err != nil {
		return fmt.Errorf("storage: OAuth credentials 파싱 실패: %w", err)
	}

	tokBytes, err := os.ReadFile(c.tokenPath)
	if err != nil {
		return fmt.Errorf("storage: OAuth token 파일 읽기 실패: %w", err)
	}
	var tok oauth2.Token
	if err := json.Unmarshal(tokBytes, &tok); err != nil {
		return fmt.Errorf("storage: OAuth token 파싱 실패: %w", err)
	}

	svc, err := drive.NewService(ctx, option.WithTokenSource(cfg.TokenSource(ctx, &tok)))
	if err != nil {
		return fmt.Errorf("storage: Drive 서비스 생성 실패: %w", err)
	}
	c.svc = svc

	folderID, err := c.ensureAppFolder(ctx)
	if err != nil {
		return err
	}
	c.appFolderID = folderID
	return nil
}

// ensureAppFolder 는 appFolderName 폴더를 검색해 있으면 그 ID를, 없으면 새로 생성한 폴더의 ID를 반환한다.
func (c *DriveClient) ensureAppFolder(ctx context.Context) (string, error) {
	folders, err := c.FindFolderByName(ctx, c.appFolderName)
	if err != nil {
		return "", err
	}
	if len(folders) > 0 {
		return folders[0].FolderID, nil
	}
	folder, err := c.CreateFolder(ctx, c.appFolderName, "")
	if err != nil {
		return "", err
	}
	return folder.FolderID, nil
}

// Upload 는 content 를 filename/mimeType 으로 업로드한다. opts 로 폴더를 지정하지 않으면 앱 전용 폴더에 저장한다.
func (c *DriveClient) Upload(ctx context.Context, content []byte, filename, mimeType string, opts ...UploadOption) (FileMetadata, error) {
	if c.svc == nil {
		return FileMetadata{}, errNotInitialized
	}

	cfg := &uploadConfig{folderID: c.appFolderID}
	for _, opt := range opts {
		opt(cfg)
	}

	f := &drive.File{Name: filename}
	if cfg.folderID != "" {
		f.Parents = []string{cfg.folderID}
	}

	created, err := c.svc.Files.Create(f).
		Media(bytes.NewReader(content), googleapi.ContentType(mimeType)).
		Fields(googleapi.Field(driveFileFields)).
		Context(ctx).
		Do()
	if err != nil {
		return FileMetadata{}, fmt.Errorf("storage: 파일 업로드 실패: %w", err)
	}
	return fileMetadataFromDrive(created)
}

// DownloadAsBase64 는 fileID 파일을 base64 로 다운로드한다.
// 구글 문서형 파일은 googleDocsExportMimeTypes 로 변환 export 하고, 그 외는 미디어 다운로드한다.
func (c *DriveClient) DownloadAsBase64(ctx context.Context, fileID string) (FileContent, error) {
	if c.svc == nil {
		return FileContent{}, errNotInitialized
	}

	meta, err := c.GetFileInfo(ctx, fileID)
	if err != nil {
		return FileContent{}, err
	}

	var resp *http.Response
	if exportMime, ok := googleDocsExportMimeTypes[meta.MimeType]; ok {
		resp, err = c.svc.Files.Export(fileID, exportMime).Context(ctx).Download()
	} else {
		resp, err = c.svc.Files.Get(fileID).Context(ctx).Download()
	}
	if err != nil {
		return FileContent{}, fmt.Errorf("storage: 파일 다운로드 실패: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return FileContent{}, fmt.Errorf("storage: 파일 본문 읽기 실패: %w", err)
	}

	return FileContent{
		FileMetadata:  meta,
		Base64Content: base64.StdEncoding.EncodeToString(data),
	}, nil
}

// DownloadByStorageRef 는 storage_ref 문자열을 fileID 로 해소해 다운로드한다.
func (c *DriveClient) DownloadByStorageRef(ctx context.Context, ref string) (FileContent, error) {
	fileID, ok := ParseStorageRef(ref)
	if !ok {
		return FileContent{}, fmt.Errorf("storage: 잘못된 storage_ref: %s", ref)
	}
	return c.DownloadAsBase64(ctx, fileID)
}

// ListFiles 는 opts 필터로 파일 목록을 조회한다. 휴지통 파일은 제외한다.
func (c *DriveClient) ListFiles(ctx context.Context, opts ListOptions) ([]FileMetadata, error) {
	if c.svc == nil {
		return nil, errNotInitialized
	}

	call := c.svc.Files.List().
		Q(buildListQuery(opts)).
		Fields(googleapi.Field("files(" + driveFileFields + ")")).
		Context(ctx)
	if opts.PageSize > 0 {
		call = call.PageSize(int64(opts.PageSize))
	}

	res, err := call.Do()
	if err != nil {
		return nil, fmt.Errorf("storage: 파일 목록 조회 실패: %w", err)
	}

	files := make([]FileMetadata, 0, len(res.Files))
	for _, f := range res.Files {
		fm, err := fileMetadataFromDrive(f)
		if err != nil {
			return nil, err
		}
		files = append(files, fm)
	}
	return files, nil
}

// buildListQuery 는 ListOptions 로 Drive 검색 쿼리 문자열을 만든다.
func buildListQuery(opts ListOptions) string {
	parts := []string{"trashed = false"}
	if opts.FolderID != "" {
		parts = append(parts, fmt.Sprintf("'%s' in parents", escapeDriveQueryValue(opts.FolderID)))
	}
	if opts.MimeType != "" {
		parts = append(parts, fmt.Sprintf("mimeType = '%s'", escapeDriveQueryValue(opts.MimeType)))
	}
	if opts.Query != "" {
		parts = append(parts, opts.Query)
	}
	return strings.Join(parts, " and ")
}

// GetFileInfo 는 fileID 파일의 메타데이터를 조회한다.
func (c *DriveClient) GetFileInfo(ctx context.Context, fileID string) (FileMetadata, error) {
	if c.svc == nil {
		return FileMetadata{}, errNotInitialized
	}

	f, err := c.svc.Files.Get(fileID).Fields(googleapi.Field(driveFileFields)).Context(ctx).Do()
	if err != nil {
		return FileMetadata{}, fmt.Errorf("storage: 파일 정보 조회 실패: %w", err)
	}
	return fileMetadataFromDrive(f)
}

// Update 는 fileID 파일의 본문·이름을 수정한다. content 가 nil이면 본문을 유지하고,
// newName 이 빈 문자열이면 이름을 유지한다.
func (c *DriveClient) Update(ctx context.Context, fileID string, content []byte, newName string) (FileMetadata, error) {
	if c.svc == nil {
		return FileMetadata{}, errNotInitialized
	}

	f := &drive.File{}
	if newName != "" {
		f.Name = newName
	}

	call := c.svc.Files.Update(fileID, f).Fields(googleapi.Field(driveFileFields)).Context(ctx)
	if content != nil {
		call = call.Media(bytes.NewReader(content))
	}

	updated, err := call.Do()
	if err != nil {
		return FileMetadata{}, fmt.Errorf("storage: 파일 수정 실패: %w", err)
	}
	return fileMetadataFromDrive(updated)
}

// Delete 는 fileID 파일을 삭제한다. permanent 가 true면 영구 삭제, false면 휴지통으로 이동한다.
func (c *DriveClient) Delete(ctx context.Context, fileID string, permanent bool) error {
	if c.svc == nil {
		return errNotInitialized
	}

	if permanent {
		if err := c.svc.Files.Delete(fileID).Context(ctx).Do(); err != nil {
			return fmt.Errorf("storage: 파일 영구 삭제 실패: %w", err)
		}
		return nil
	}

	if _, err := c.svc.Files.Update(fileID, &drive.File{Trashed: true}).Context(ctx).Do(); err != nil {
		return fmt.Errorf("storage: 파일 휴지통 이동 실패: %w", err)
	}
	return nil
}

// FindFolderByName 은 name 과 일치하는(휴지통 제외) 폴더를 검색한다.
func (c *DriveClient) FindFolderByName(ctx context.Context, name string) ([]FolderMetadata, error) {
	if c.svc == nil {
		return nil, errNotInitialized
	}

	q := fmt.Sprintf("name = '%s' and mimeType = '%s' and trashed = false",
		escapeDriveQueryValue(name), folderMimeType)
	res, err := c.svc.Files.List().Q(q).Fields(googleapi.Field("files(id, name, parents)")).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("storage: 폴더 검색 실패: %w", err)
	}

	folders := make([]FolderMetadata, 0, len(res.Files))
	for _, f := range res.Files {
		folders = append(folders, FolderMetadata{FolderID: f.Id, Name: f.Name, ParentID: firstOrEmpty(f.Parents)})
	}
	return folders, nil
}

// CreateFolder 는 parentID 아래에 name 폴더를 생성한다. parentID 가 빈 문자열이면 루트에 생성한다.
func (c *DriveClient) CreateFolder(ctx context.Context, name, parentID string) (FolderMetadata, error) {
	if c.svc == nil {
		return FolderMetadata{}, errNotInitialized
	}

	f := &drive.File{Name: name, MimeType: folderMimeType}
	if parentID != "" {
		f.Parents = []string{parentID}
	}

	created, err := c.svc.Files.Create(f).Fields(googleapi.Field("id, name, parents")).Context(ctx).Do()
	if err != nil {
		return FolderMetadata{}, fmt.Errorf("storage: 폴더 생성 실패: %w", err)
	}
	return FolderMetadata{FolderID: created.Id, Name: created.Name, ParentID: firstOrEmpty(created.Parents)}, nil
}

// fileMetadataFromDrive 는 Drive API File 응답을 FileMetadata 로 변환한다.
func fileMetadataFromDrive(f *drive.File) (FileMetadata, error) {
	created, err := parseDriveTime(f.CreatedTime)
	if err != nil {
		return FileMetadata{}, err
	}
	updated, err := parseDriveTime(f.ModifiedTime)
	if err != nil {
		return FileMetadata{}, err
	}
	return FileMetadata{
		FileID:      f.Id,
		StorageRef:  CreateStorageRef(f.Id),
		Filename:    f.Name,
		MimeType:    f.MimeType,
		Size:        f.Size,
		CreatedAt:   created,
		UpdatedAt:   updated,
		WebViewLink: f.WebViewLink,
	}, nil
}

// parseDriveTime 은 Drive API 의 RFC 3339 시각 문자열을 time.Time 으로 변환한다. 빈 문자열이면 zero value다.
func parseDriveTime(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("storage: 시간 파싱 실패: %w", err)
	}
	return t, nil
}

// firstOrEmpty 는 목록의 첫 원소를 반환하거나, 비어 있으면 빈 문자열을 반환한다.
func firstOrEmpty(ss []string) string {
	if len(ss) == 0 {
		return ""
	}
	return ss[0]
}

// escapeDriveQueryValue 는 Drive 검색 쿼리 리터럴에 들어갈 문자열의 백슬래시·홑따옴표를 이스케이프한다.
func escapeDriveQueryValue(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `'`, `\'`)
	return s
}
