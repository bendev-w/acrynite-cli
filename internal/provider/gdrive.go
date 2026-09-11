package provider

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"acrynite-cli/utils"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
)

// Google requires its own OAuth client (client ID + secret) registered by whoever
// distributes the app. There is no way to safely embed one for every user of this
// CLI, so the operator must create a "Desktop app" OAuth client in Google Cloud
// Console and provide its credentials via these environment variables.
const (
	envGDriveClientID     = "ACRYNITE_GDRIVE_CLIENT_ID"
	envGDriveClientSecret = "ACRYNITE_GDRIVE_CLIENT_SECRET"
)

func gdriveOAuthConfig(redirectURL string) (*oauth2.Config, error) {
	clientID := strings.TrimSpace(os.Getenv(envGDriveClientID))
	clientSecret := strings.TrimSpace(os.Getenv(envGDriveClientSecret))
	if clientID == "" || clientSecret == "" {
		return nil, fmt.Errorf(
			"gdrive requires a Google OAuth client: set %s and %s env vars "+
				"(create a 'Desktop app' OAuth client in Google Cloud Console with the Drive API enabled)",
			envGDriveClientID, envGDriveClientSecret)
	}

	return &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Endpoint:     google.Endpoint,
		Scopes:       []string{drive.DriveFileScope},
		RedirectURL:  redirectURL,
	}, nil
}

// AddGDrive runs the interactive OAuth consent flow for a new Google Drive provider,
// storing the refresh token locally and returning the provider metadata to save.
func AddGDrive(name string, secrets *SecretStore) (*Config, error) {
	return runGDriveOAuth(name, secrets)
}

// ReauthGDrive re-runs the OAuth flow for a provider whose token expired or was revoked.
func ReauthGDrive(name string, secrets *SecretStore) (*Config, error) {
	return runGDriveOAuth(name, secrets)
}

func runGDriveOAuth(name string, secrets *SecretStore) (*Config, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("failed to start local OAuth callback listener: %w", err)
	}
	defer listener.Close()

	port := listener.Addr().(*net.TCPAddr).Port
	redirectURL := fmt.Sprintf("http://127.0.0.1:%d/oauth2callback", port)

	oauthCfg, err := gdriveOAuthConfig(redirectURL)
	if err != nil {
		return nil, err
	}

	stateBytes := make([]byte, 24)
	if _, err := rand.Read(stateBytes); err != nil {
		return nil, fmt.Errorf("failed to generate OAuth state: %w", err)
	}
	state := hex.EncodeToString(stateBytes)
	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/oauth2callback", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("state") != state {
			http.Error(w, "invalid OAuth state", http.StatusBadRequest)
			errCh <- fmt.Errorf("received callback with mismatched state")
			return
		}
		if errMsg := r.URL.Query().Get("error"); errMsg != "" {
			http.Error(w, "authorization denied", http.StatusBadRequest)
			errCh <- fmt.Errorf("google authorization denied: %s", errMsg)
			return
		}
		code := r.URL.Query().Get("code")
		if code == "" {
			http.Error(w, "missing authorization code", http.StatusBadRequest)
			errCh <- fmt.Errorf("callback did not include an authorization code")
			return
		}
		fmt.Fprint(w, "Authorization complete. You can close this window and return to the CLI.")
		codeCh <- code
	})

	server := &http.Server{Handler: mux}
	go server.Serve(listener)
	defer server.Close()

	authURL := oauthCfg.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.ApprovalForce)
	utils.Info("Opening browser for Google authorization...")
	utils.Info("If it doesn't open automatically, visit:")
	fmt.Println(authURL)
	openBrowser(authURL)

	var code string
	select {
	case code = <-codeCh:
	case err := <-errCh:
		return nil, err
	case <-time.After(5 * time.Minute):
		return nil, fmt.Errorf("timed out waiting for Google authorization")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	token, err := oauthCfg.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("failed to exchange authorization code: %w", err)
	}
	if token.RefreshToken == "" {
		return nil, fmt.Errorf("Google did not return a refresh token - revoke prior access at https://myaccount.google.com/permissions and try again")
	}

	tokenJSON, err := json.Marshal(token)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize token: %w", err)
	}
	if err := secrets.Set(name+":gdrive_token", string(tokenJSON)); err != nil {
		return nil, fmt.Errorf("failed to store token: %w", err)
	}

	driveSvc, err := drive.NewService(ctx, option.WithTokenSource(oauthCfg.TokenSource(ctx, token)))
	if err != nil {
		return nil, fmt.Errorf("failed to create Drive client: %w", err)
	}
	about, err := driveSvc.About.Get().Fields("user").Do()
	account := ""
	if err == nil && about.User != nil {
		account = about.User.EmailAddress
	} else {
		utils.Warning("Authorized, but failed to fetch account email: %v", err)
	}

	return &Config{
		Name:    name,
		Type:    TypeGDrive,
		Account: account,
		Status:  StatusConnected,
	}, nil
}

// NewDriveService builds an authenticated Drive client for a registered gdrive provider
// using the refresh token saved during `provider add`/`provider reauth`.
func NewDriveService(cfg *Config, secrets *SecretStore) (*drive.Service, error) {
	if cfg.Type != TypeGDrive {
		return nil, fmt.Errorf("provider '%s' is not a gdrive provider", cfg.Name)
	}

	tokenJSON, err := secrets.Get(cfg.Name + ":gdrive_token")
	if err != nil {
		return nil, fmt.Errorf("no stored Google token for provider '%s' - run 'acrynite provider reauth %s': %w", cfg.Name, cfg.Name, err)
	}

	var token oauth2.Token
	if err := json.Unmarshal([]byte(tokenJSON), &token); err != nil {
		return nil, fmt.Errorf("failed to parse stored token: %w", err)
	}

	// redirect URL is unused for refresh, but required to build the oauth2.Config
	oauthCfg, err := gdriveOAuthConfig("")
	if err != nil {
		return nil, err
	}

	ctx := context.Background()
	return drive.NewService(ctx, option.WithTokenSource(oauthCfg.TokenSource(ctx, &token)))
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		utils.Warning("Failed to open browser automatically: %v", err)
	}
}
