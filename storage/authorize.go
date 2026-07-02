// authorize.go 는 OAuth 토큰(token.json) 최초 발급 헬퍼를 담는다.
// Initialize 는 기존 token.json 을 읽어 리프레시만 하므로(driveclient.go), 토큰 파일이 아직 없는
// 최초 1회는 Authorize 로 브라우저 동의 흐름을 거쳐 발급한다(파이썬 google-auth-oauthlib
// InstalledAppFlow.run_local_server 상당). 이후 실행은 Initialize 만으로 충분하다.
package storage

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/drive/v3"
)

// Authorize 는 credentialsPath 의 OAuth 클라이언트 설정으로 브라우저 동의 흐름을 진행해
// tokenPath 에 토큰 파일을 저장한다. 로컬 루프백 서버를 임시 포트에 띄워 리다이렉트를 받고,
// 사용자에게 인증 URL을 표준 출력으로 안내한 뒤 동의가 끝날 때까지(또는 ctx 취소까지) 대기한다.
// credentials 는 Google Cloud Console 의 "데스크톱 앱" 유형 OAuth 클라이언트여야 한다
// (루프백 리다이렉트는 데스크톱 앱 유형에서만 허용된다).
func (c *DriveClient) Authorize(ctx context.Context) error {
	credBytes, err := os.ReadFile(c.credentialsPath)
	if err != nil {
		return fmt.Errorf("storage: OAuth credentials 파일 읽기 실패: %w", err)
	}
	cfg, err := google.ConfigFromJSON(credBytes, drive.DriveScope)
	if err != nil {
		return fmt.Errorf("storage: OAuth credentials 파싱 실패: %w", err)
	}

	// 임시 포트의 루프백 서버로 리다이렉트를 받는다. credentials.json 의 redirect 설정 대신
	// 실제 리슨 주소를 써야 포트 충돌 없이 항상 동작한다.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("storage: 루프백 리슨 실패: %w", err)
	}
	defer ln.Close()
	cfg.RedirectURL = fmt.Sprintf("http://%s/", ln.Addr().String())

	state, err := randomState()
	if err != nil {
		return err
	}

	// prompt=consent 를 강제해야 재발급(기존 동의가 남아 있는 경우)에도 refresh_token 이 항상 내려온다.
	authURL := cfg.AuthCodeURL(state,
		oauth2.AccessTypeOffline,
		oauth2.SetAuthURLParam("prompt", "consent"),
	)

	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)
	srv := &http.Server{Handler: authCallbackHandler(state, codeCh, errCh)}
	go func() {
		if serveErr := srv.Serve(ln); serveErr != nil && serveErr != http.ErrServerClosed {
			errCh <- fmt.Errorf("storage: 루프백 서버 오류: %w", serveErr)
		}
	}()
	defer srv.Close()

	fmt.Printf("브라우저에서 다음 URL을 열어 Google Drive 접근을 허용하세요:\n\n%s\n\n동의를 기다리는 중...\n", authURL)

	var code string
	select {
	case code = <-codeCh:
	case err := <-errCh:
		return err
	case <-ctx.Done():
		return fmt.Errorf("storage: 동의 대기가 취소되었습니다: %w", ctx.Err())
	}

	tok, err := cfg.Exchange(ctx, code)
	if err != nil {
		return fmt.Errorf("storage: 토큰 교환 실패: %w", err)
	}

	tokBytes, err := json.MarshalIndent(tok, "", "  ")
	if err != nil {
		return fmt.Errorf("storage: 토큰 직렬화 실패: %w", err)
	}
	// 토큰은 자격 증명이므로 소유자 전용 권한으로 저장한다.
	if err := os.WriteFile(c.tokenPath, tokBytes, 0o600); err != nil {
		return fmt.Errorf("storage: 토큰 파일 저장 실패: %w", err)
	}

	fmt.Printf("토큰이 저장되었습니다: %s\n", c.tokenPath)
	return nil
}

// authCallbackHandler 는 OAuth 리다이렉트 콜백에서 state 를 검증하고 code 를 채널로 전달한다.
func authCallbackHandler(state string, codeCh chan<- string, errCh chan<- error) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if errParam := q.Get("error"); errParam != "" {
			http.Error(w, "인증이 거부되었습니다. 터미널을 확인하세요.", http.StatusBadRequest)
			errCh <- fmt.Errorf("storage: 인증 거부: %s", errParam)
			return
		}
		if q.Get("state") != state {
			http.Error(w, "state 불일치. 터미널에서 다시 시도하세요.", http.StatusBadRequest)
			errCh <- fmt.Errorf("storage: OAuth state 불일치")
			return
		}
		code := q.Get("code")
		if code == "" {
			http.Error(w, "code 누락", http.StatusBadRequest)
			errCh <- fmt.Errorf("storage: OAuth code 누락")
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, "<html><body>인증이 완료되었습니다. 이 창을 닫고 터미널로 돌아가세요.</body></html>")
		codeCh <- code
	})
}

// randomState 는 CSRF 방지용 state 문자열을 생성한다.
func randomState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("storage: state 생성 실패: %w", err)
	}
	return hex.EncodeToString(b), nil
}
