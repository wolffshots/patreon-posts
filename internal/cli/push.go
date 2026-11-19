package cli

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// PushSecretEnv names the environment variable that holds the push secret.
// It has the same name as the variable that ta-companion reads on the server.
const PushSecretEnv = "COMPANION_API_TOKEN"

// PushCookies sends the Patreon Cookie header to a ta-companion token
// endpoint: PUT <baseURL>/patreon/token with "Authorization: Bearer <secret>".
func PushCookies(ctx context.Context, hc *http.Client, baseURL, secret, header string) error {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if !strings.HasPrefix(baseURL, "http://") && !strings.HasPrefix(baseURL, "https://") {
		return fmt.Errorf("push URL %q must start with http:// or https://", baseURL)
	}
	if secret == "" {
		return fmt.Errorf("no push secret: set %s or companion_api_token in the config", PushSecretEnv)
	}
	if strings.TrimSpace(header) == "" {
		return fmt.Errorf("no Patreon session to push")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, baseURL+"/patreon/token", strings.NewReader(header))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+secret)
	req.Header.Set("Content-Type", "text/plain")
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 == 2 {
		return nil
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	msg := strings.TrimSpace(string(body))
	switch resp.StatusCode {
	case http.StatusUnauthorized:
		return fmt.Errorf("server returned 401: the push secret does not match %s on the server", PushSecretEnv)
	case http.StatusNotFound:
		return fmt.Errorf("server returned 404: the URL is not a ta-companion token endpoint")
	}
	return fmt.Errorf("server returned %d: %s", resp.StatusCode, msg)
}
