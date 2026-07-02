// tool.go 는 Client 메서드를 tool.Tool 로 감싸는 파일 조작 도구 8종을 담는다.
// storage 가 tool 패키지에 단방향으로 의존하며 역참조는 없다(database/tool.go 패턴 참조).
package storage

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/zipkero/langgraph-go/tool"
)

// uploadFileArgs 는 UploadFileTool 의 입력 스키마다.
type uploadFileArgs struct {
	// Filename 은 업로드할 파일명이다.
	Filename string `json:"filename" description:"업로드할 파일명"`
	// MimeType 은 업로드할 파일의 MIME 타입이다.
	MimeType string `json:"mime_type" description:"업로드할 파일의 MIME 타입"`
	// Base64Content 는 업로드할 파일 본문(base64 인코딩)이다.
	Base64Content string `json:"base64_content" description:"업로드할 파일 본문(base64 인코딩)"`
	// FolderID 는 업로드 대상 폴더 ID다. 비어 있으면 앱 전용 폴더를 사용한다.
	FolderID string `json:"folder_id,omitempty" description:"업로드 대상 폴더 ID(생략 시 앱 전용 폴더)"`
}

// UploadFileTool 은 c.Upload 를 감싸 tool.Tool 계약을 충족하는 파일 업로드 도구를 반환한다.
func UploadFileTool(c Client) tool.Tool {
	return tool.WithArgsSchema("upload_file", "파일을 외부 스토리지에 업로드합니다",
		func(ctx context.Context, args uploadFileArgs, _ tool.Runtime) (tool.Result, error) {
			if strings.TrimSpace(args.Filename) == "" {
				return tool.Result{IsError: true, Content: "storage: filename 이 비어 있습니다"}, nil
			}
			content, err := base64.StdEncoding.DecodeString(args.Base64Content)
			if err != nil {
				return tool.Result{IsError: true, Content: fmt.Sprintf("storage: base64_content 디코딩 실패: %v", err)}, nil
			}

			var opts []UploadOption
			if args.FolderID != "" {
				opts = append(opts, WithUploadFolder(args.FolderID))
			}
			meta, err := c.Upload(ctx, content, args.Filename, args.MimeType, opts...)
			if err != nil {
				return tool.Result{IsError: true, Content: fmt.Sprintf("storage: 파일 업로드 실패: %v", err)}, nil
			}
			return tool.Result{Content: fmt.Sprintf("업로드 완료: %s (storage_ref=%s)", meta.Filename, meta.StorageRef)}, nil
		})
}

// downloadFileArgs 는 DownloadFileTool 의 입력 스키마다. FileID/StorageRef 중 하나가 필요하다.
type downloadFileArgs struct {
	// FileID 는 다운로드할 파일 ID다.
	FileID string `json:"file_id,omitempty" description:"다운로드할 파일 ID"`
	// StorageRef 는 다운로드할 파일의 storage_ref 다.
	StorageRef string `json:"storage_ref,omitempty" description:"다운로드할 파일의 storage_ref"`
}

// DownloadFileTool 은 c.DownloadAsBase64/DownloadByStorageRef 를 감싸 tool.Tool 계약을 충족하는
// 파일 다운로드 도구를 반환한다. storage_ref 가 있으면 우선 사용한다.
func DownloadFileTool(c Client) tool.Tool {
	return tool.WithArgsSchema("download_file", "파일을 base64로 다운로드합니다(file_id 또는 storage_ref 중 하나 필요)",
		func(ctx context.Context, args downloadFileArgs, _ tool.Runtime) (tool.Result, error) {
			var (
				fc  FileContent
				err error
			)
			switch {
			case strings.TrimSpace(args.StorageRef) != "":
				fc, err = c.DownloadByStorageRef(ctx, args.StorageRef)
			case strings.TrimSpace(args.FileID) != "":
				fc, err = c.DownloadAsBase64(ctx, args.FileID)
			default:
				return tool.Result{IsError: true, Content: "storage: file_id 또는 storage_ref 중 하나가 필요합니다"}, nil
			}
			if err != nil {
				return tool.Result{IsError: true, Content: fmt.Sprintf("storage: 파일 다운로드 실패: %v", err)}, nil
			}
			return tool.Result{Content: fmt.Sprintf("%s (%s)\n%s", fc.Filename, fc.MimeType, fc.Base64Content)}, nil
		})
}

// getFileInfoArgs 는 GetFileInfoTool 의 입력 스키마다.
type getFileInfoArgs struct {
	// FileID 는 정보를 조회할 파일 ID다.
	FileID string `json:"file_id" description:"정보를 조회할 파일 ID"`
}

// GetFileInfoTool 은 c.GetFileInfo 를 감싸 tool.Tool 계약을 충족하는 파일 정보 조회 도구를 반환한다.
func GetFileInfoTool(c Client) tool.Tool {
	return tool.WithArgsSchema("get_file_info", "파일 메타데이터를 조회합니다",
		func(ctx context.Context, args getFileInfoArgs, _ tool.Runtime) (tool.Result, error) {
			if strings.TrimSpace(args.FileID) == "" {
				return tool.Result{IsError: true, Content: "storage: file_id 가 비어 있습니다"}, nil
			}
			meta, err := c.GetFileInfo(ctx, args.FileID)
			if err != nil {
				return tool.Result{IsError: true, Content: fmt.Sprintf("storage: 파일 정보 조회 실패: %v", err)}, nil
			}
			return tool.Result{Content: serializeFileMetadata(meta)}, nil
		})
}

// serializeFileMetadata 는 FileMetadata 하나를 Result.Content 에 담을 문자열로 직렬화한다.
func serializeFileMetadata(m FileMetadata) string {
	return fmt.Sprintf("%s (%s, %d bytes, storage_ref=%s)", m.Filename, m.MimeType, m.Size, m.StorageRef)
}

// listFilesArgs 는 ListFilesTool 의 입력 스키마다.
type listFilesArgs struct {
	// FolderID 는 목록을 조회할 폴더 ID다. 비어 있으면 폴더로 필터링하지 않는다.
	FolderID string `json:"folder_id,omitempty" description:"목록을 조회할 폴더 ID"`
	// MimeType 은 필터링할 MIME 타입이다. 비어 있으면 MIME 타입으로 필터링하지 않는다.
	MimeType string `json:"mime_type,omitempty" description:"필터링할 MIME 타입"`
	// PageSize 는 반환받을 최대 결과 수다.
	PageSize int `json:"page_size,omitempty" description:"반환받을 최대 결과 수"`
	// Query 는 백엔드 검색 쿼리 문법을 그대로 추가할 자유 형식 필터 조각이다.
	Query string `json:"query,omitempty" description:"추가 검색 쿼리 조각"`
}

// ListFilesTool 은 c.ListFiles 를 감싸 tool.Tool 계약을 충족하는 파일 목록 조회 도구를 반환한다.
func ListFilesTool(c Client) tool.Tool {
	return tool.WithArgsSchema("list_files", "폴더/필터 조건으로 파일 목록을 조회합니다",
		func(ctx context.Context, args listFilesArgs, _ tool.Runtime) (tool.Result, error) {
			opts := ListOptions{
				FolderID: args.FolderID,
				MimeType: args.MimeType,
				PageSize: args.PageSize,
				Query:    args.Query,
			}
			files, err := c.ListFiles(ctx, opts)
			if err != nil {
				return tool.Result{IsError: true, Content: fmt.Sprintf("storage: 파일 목록 조회 실패: %v", err)}, nil
			}
			return tool.Result{Content: serializeFileMetadataList(files)}, nil
		})
}

// serializeFileMetadataList 는 FileMetadata 목록을 Result.Content 에 담을 문자열로 직렬화한다.
func serializeFileMetadataList(files []FileMetadata) string {
	if len(files) == 0 {
		return "조회된 파일이 없습니다."
	}
	var sb strings.Builder
	for i, f := range files {
		fmt.Fprintf(&sb, "[%d] %s\n", i+1, serializeFileMetadata(f))
	}
	return strings.TrimRight(sb.String(), "\n")
}

// findFolderArgs 는 FindFolderTool 의 입력 스키마다.
type findFolderArgs struct {
	// Name 은 검색할 폴더 이름이다.
	Name string `json:"name" description:"검색할 폴더 이름"`
}

// FindFolderTool 은 c.FindFolderByName 을 감싸 tool.Tool 계약을 충족하는 폴더 검색 도구를 반환한다.
func FindFolderTool(c Client) tool.Tool {
	return tool.WithArgsSchema("find_folder", "이름으로 폴더를 검색합니다",
		func(ctx context.Context, args findFolderArgs, _ tool.Runtime) (tool.Result, error) {
			if strings.TrimSpace(args.Name) == "" {
				return tool.Result{IsError: true, Content: "storage: name 이 비어 있습니다"}, nil
			}
			folders, err := c.FindFolderByName(ctx, args.Name)
			if err != nil {
				return tool.Result{IsError: true, Content: fmt.Sprintf("storage: 폴더 검색 실패: %v", err)}, nil
			}
			if len(folders) == 0 {
				return tool.Result{Content: "일치하는 폴더가 없습니다."}, nil
			}
			var sb strings.Builder
			for i, f := range folders {
				fmt.Fprintf(&sb, "[%d] %s (id=%s)\n", i+1, f.Name, f.FolderID)
			}
			return tool.Result{Content: strings.TrimRight(sb.String(), "\n")}, nil
		})
}

// deleteFileArgs 는 DeleteFileTool 의 입력 스키마다.
type deleteFileArgs struct {
	// FileID 는 삭제할 파일 ID다.
	FileID string `json:"file_id" description:"삭제할 파일 ID"`
	// Permanent 가 true면 영구 삭제, false(기본)면 휴지통으로 이동한다.
	Permanent bool `json:"permanent,omitempty" description:"true면 영구 삭제, false면 휴지통 이동"`
}

// DeleteFileTool 은 c.Delete 를 감싸 tool.Tool 계약을 충족하는 파일 삭제 도구를 반환한다.
func DeleteFileTool(c Client) tool.Tool {
	return tool.WithArgsSchema("delete_file", "파일을 삭제합니다(휴지통 이동 또는 영구 삭제)",
		func(ctx context.Context, args deleteFileArgs, _ tool.Runtime) (tool.Result, error) {
			if strings.TrimSpace(args.FileID) == "" {
				return tool.Result{IsError: true, Content: "storage: file_id 가 비어 있습니다"}, nil
			}
			if err := c.Delete(ctx, args.FileID, args.Permanent); err != nil {
				return tool.Result{IsError: true, Content: fmt.Sprintf("storage: 파일 삭제 실패: %v", err)}, nil
			}
			return tool.Result{Content: fmt.Sprintf("삭제 완료: %s", args.FileID)}, nil
		})
}

// updateFileArgs 는 UpdateFileTool 의 입력 스키마다.
type updateFileArgs struct {
	// FileID 는 수정할 파일 ID다.
	FileID string `json:"file_id" description:"수정할 파일 ID"`
	// Base64Content 는 교체할 파일 본문(base64 인코딩)이다. 비어 있으면 본문을 유지한다.
	Base64Content string `json:"base64_content,omitempty" description:"교체할 파일 본문(base64 인코딩, 생략 시 본문 유지)"`
	// NewName 은 변경할 파일명이다. 비어 있으면 이름을 유지한다.
	NewName string `json:"new_name,omitempty" description:"변경할 파일명(생략 시 이름 유지)"`
}

// UpdateFileTool 은 c.Update 를 감싸 tool.Tool 계약을 충족하는 파일 수정 도구를 반환한다.
func UpdateFileTool(c Client) tool.Tool {
	return tool.WithArgsSchema("update_file", "파일 본문 또는 이름을 수정합니다",
		func(ctx context.Context, args updateFileArgs, _ tool.Runtime) (tool.Result, error) {
			if strings.TrimSpace(args.FileID) == "" {
				return tool.Result{IsError: true, Content: "storage: file_id 가 비어 있습니다"}, nil
			}

			var content []byte
			if args.Base64Content != "" {
				decoded, err := base64.StdEncoding.DecodeString(args.Base64Content)
				if err != nil {
					return tool.Result{IsError: true, Content: fmt.Sprintf("storage: base64_content 디코딩 실패: %v", err)}, nil
				}
				content = decoded
			}

			meta, err := c.Update(ctx, args.FileID, content, args.NewName)
			if err != nil {
				return tool.Result{IsError: true, Content: fmt.Sprintf("storage: 파일 수정 실패: %v", err)}, nil
			}
			return tool.Result{Content: fmt.Sprintf("수정 완료: %s", serializeFileMetadata(meta))}, nil
		})
}

// createFolderArgs 는 CreateFolderTool 의 입력 스키마다.
type createFolderArgs struct {
	// Name 은 생성할 폴더 이름이다.
	Name string `json:"name" description:"생성할 폴더 이름"`
	// ParentID 는 상위 폴더 ID다. 비어 있으면 루트에 생성한다.
	ParentID string `json:"parent_id,omitempty" description:"상위 폴더 ID(생략 시 루트)"`
}

// CreateFolderTool 은 c.CreateFolder 를 감싸 tool.Tool 계약을 충족하는 폴더 생성 도구를 반환한다.
func CreateFolderTool(c Client) tool.Tool {
	return tool.WithArgsSchema("create_folder", "새 폴더를 생성합니다",
		func(ctx context.Context, args createFolderArgs, _ tool.Runtime) (tool.Result, error) {
			if strings.TrimSpace(args.Name) == "" {
				return tool.Result{IsError: true, Content: "storage: name 이 비어 있습니다"}, nil
			}
			folder, err := c.CreateFolder(ctx, args.Name, args.ParentID)
			if err != nil {
				return tool.Result{IsError: true, Content: fmt.Sprintf("storage: 폴더 생성 실패: %v", err)}, nil
			}
			return tool.Result{Content: fmt.Sprintf("생성 완료: %s (id=%s)", folder.Name, folder.FolderID)}, nil
		})
}
