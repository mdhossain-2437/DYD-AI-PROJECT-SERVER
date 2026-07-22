// Package turnstile verifies Cloudflare Turnstile tokens server-side. Public
// forms (apply, contact, newsletter) send a Turnstile token; we verify it with
// Cloudflare before doing any work, which blocks bots and cheap automated abuse
// at the edge of our own logic. When disabled (dev/local), Verify is a no-op
// that always passes.
package turnstile

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const verifyURL = "https://challenges.cloudflare.com/turnstile/v0/siteverify"

type Verifier struct {
	secret  string
	enabled bool
	client  *http.Client
}

func New(secret string, enabled bool) *Verifier {
	return &Verifier{
		secret:  secret,
		enabled: enabled,
		client:  &http.Client{Timeout: 5 * time.Second},
	}
}

type siteVerifyResp struct {
	Success    bool     `json:"success"`
	ErrorCodes []string `json:"error-codes"`
}

// Verify checks a token against Cloudflare. remoteIP is the client IP (optional
// but recommended). Returns nil when the challenge passed (or when disabled).
func (v *Verifier) Verify(ctx context.Context, token, remoteIP string) error {
	if !v.enabled {
		return nil
	}
	if strings.TrimSpace(token) == "" {
		return ErrFailed
	}

	form := url.Values{}
	form.Set("secret", v.secret)
	form.Set("response", token)
	if remoteIP != "" {
		form.Set("remoteip", remoteIP)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, verifyURL, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := v.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var out siteVerifyResp
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return err
	}
	if !out.Success {
		return ErrFailed
	}
	return nil
}

// ErrFailed indicates the Turnstile challenge did not pass.
var ErrFailed = &verifyError{"turnstile verification failed"}

type verifyError struct{ msg string }

func (e *verifyError) Error() string { return e.msg }
