package threads

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// wowaMaxBodyBytes caps the go-wowa response body read.
const wowaMaxBodyBytes = 8 << 20

// wowaInteractTimeout caps a single go-wowa interact call.
const wowaInteractTimeout = 60 * time.Second

// wowaTransport is a thin net/http client for go-wowa's /api/v1/chrome/interact
// endpoint. It carries the base URL and the soft-auth secret. No go-browser /
// go-rod dependency — talk to go-wowa over plain HTTP+JSON only.
type wowaTransport struct {
	hc     *http.Client
	base   string
	secret string
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func newWowaTransport(base, secret string) *wowaTransport {
	return &wowaTransport{
		hc:     &http.Client{Timeout: wowaInteractTimeout},
		base:   strings.TrimRight(base, "/"),
		secret: secret,
	}
}

// wowaAction is a single go-wowa interact action (evaluate / navigate).
type wowaAction struct {
	Type   string `json:"type"`
	Script string `json:"script,omitempty"`
	URL    string `json:"url,omitempty"`
}

// wowaInteractRequest is the POST body for /api/v1/chrome/interact.
type wowaInteractRequest struct {
	URL     string       `json:"url"`
	Mode    string       `json:"mode"`
	Session string       `json:"session"`
	Actions []wowaAction `json:"actions"`
}

// wowaActionResult mirrors browser.ActionResult — only the fields doCDP reads.
type wowaActionResult struct {
	Action string          `json:"action"`
	Ok     bool            `json:"ok"`
	Data   json.RawMessage `json:"data,omitempty"`
	Error  string          `json:"error,omitempty"`
}

// wowaInteractResponse mirrors browser.InteractResponse.
type wowaInteractResponse struct {
	URL     string             `json:"url"`
	Status  string             `json:"status"`
	Actions []wowaActionResult `json:"actions"`
	Error   string             `json:"error,omitempty"`
}

// fetchResult is the JS-side return value of the in-page fetch IIFE.
type fetchResult struct {
	Redirected bool   `json:"redirected"`
	Status     int    `json:"status"`
	Body       string `json:"body"`
}

// interact POSTs to go-wowa's /api/v1/chrome/interact with the given actions
// and returns the last action's result data.
func (w *wowaTransport) interact(ctx context.Context, session, pageURL string, actions []wowaAction) (json.RawMessage, error) {
	body := wowaInteractRequest{
		URL:     pageURL,
		Mode:    "default",
		Session: session,
		Actions: actions,
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal interact request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.base+"/api/v1/chrome/interact", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("build interact request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if w.secret != "" {
		req.Header.Set("X-Internal-Secret", w.secret)
	}
	resp, err := w.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("interact HTTP: %w", err)
	}
	defer resp.Body.Close()

	limited := io.LimitReader(resp.Body, wowaMaxBodyBytes)
	raw, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("read interact response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("go-wowa status %d: %s", resp.StatusCode, truncate(string(raw), 200))
	}

	var ir wowaInteractResponse
	if err := json.Unmarshal(raw, &ir); err != nil {
		return nil, fmt.Errorf("unmarshal interact response: %w", err)
	}
	if ir.Status != "ok" {
		return nil, fmt.Errorf("go-wowa interact status %q: %s", ir.Status, ir.Error)
	}
	if len(ir.Actions) == 0 {
		return nil, fmt.Errorf("go-wowa returned no actions")
	}
	last := ir.Actions[len(ir.Actions)-1]
	if !last.Ok {
		return nil, fmt.Errorf("go-wowa action %q failed: %s", last.Action, last.Error)
	}
	return last.Data, nil
}

// parseFetchResult decodes the evaluate action's data into a fetchResult.
func parseFetchResult(data json.RawMessage) (fetchResult, error) {
	var fr fetchResult
	if len(data) == 0 {
		return fr, fmt.Errorf("empty data")
	}
	if err := json.Unmarshal(data, &fr); err != nil {
		return fr, fmt.Errorf("unmarshal fetchResult: %w", err)
	}
	return fr, nil
}

// buildFetchScript constructs the in-page fetch JS. The endpoint, method,
// body, app id, lsd, asbd id and friendly name are interpolated via json.Marshal
// of the Go strings, producing safe JS string literals — never string-concat raw.
func buildFetchScript(endpoint, method, body, appID, lsd, asbdID, friendlyName string) (string, error) {
	epJSON, err := json.Marshal(endpoint)
	if err != nil {
		return "", fmt.Errorf("marshal endpoint: %w", err)
	}
	methodJSON, err := json.Marshal(method)
	if err != nil {
		return "", fmt.Errorf("marshal method: %w", err)
	}
	bodyJSON, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("marshal body: %w", err)
	}
	appIDJSON, err := json.Marshal(appID)
	if err != nil {
		return "", fmt.Errorf("marshal app id: %w", err)
	}
	lsdJSON, err := json.Marshal(lsd)
	if err != nil {
		return "", fmt.Errorf("marshal lsd: %w", err)
	}
	asbdJSON, err := json.Marshal(asbdID)
	if err != nil {
		return "", fmt.Errorf("marshal asbd id: %w", err)
	}
	friendlyJSON, err := json.Marshal(friendlyName)
	if err != nil {
		return "", fmt.Errorf("marshal friendly name: %w", err)
	}

	return `(async () => {
  const cm = document.cookie.match(/csrftoken=([^;]+)/);
  const csrf = cm ? cm[1] : "";
  const lsd = ` + string(lsdJSON) + `;
  const asbd = ` + string(asbdJSON) + `;
  const friend = ` + string(friendlyJSON) + `;
  const opts = {method:` + string(methodJSON) + `,
    headers:{"x-csrftoken":csrf,"x-ig-app-id":` + string(appIDJSON) + `,"x-requested-with":"XMLHttpRequest","accept":"*/*"},
    credentials:"include", redirect:"manual"};
  if (lsd) opts.headers["x-fb-lsd"] = lsd;
  if (asbd) opts.headers["x-asbd-id"] = asbd;
  if (friend) opts.headers["x-fb-friendly-name"] = friend;
  if (` + string(methodJSON) + ` === "POST") {
    opts.headers["content-type"] = "application/x-www-form-urlencoded";
    opts.body = ` + string(bodyJSON) + `;
  }
  const r = await fetch(` + string(epJSON) + `, opts);
  if (r.type==="opaqueredirect" || r.status===0) return {redirected:true, status:302};
  const body = await r.text();
  return {status:r.status, body:body};
})()`, nil
}

// wowaFetchOnce runs a single in-page fetch via go-wowa, with one on-demand
// re-navigate retry if the first attempt is redirected/opaque/status-0. It
// returns the fetchResult for the caller to classify.
func (c *Client) wowaFetchOnce(ctx context.Context, session, pageURL, script string) (fetchResult, error) {
	var fr fetchResult
	res, err := c.wowa.interact(ctx, session, pageURL, []wowaAction{{Type: "evaluate", Script: script}})
	if err != nil {
		return fr, fmt.Errorf("go-wowa interact: %w", err)
	}
	fr, err = parseFetchResult(res)
	if err != nil {
		return fr, fmt.Errorf("parse go-wowa result: %w", err)
	}

	if fr.Redirected || fr.Status == 302 || fr.Status == 0 {
		retryRes, rerr := c.wowa.interact(ctx, session, pageURL, []wowaAction{
			{Type: "navigate", URL: pageURL},
			{Type: "evaluate", Script: script},
		})
		if rerr != nil {
			// A transport failure on the retry leg is a go-wowa problem, NOT a
			// redirect — surface it as a wrapped error so it is not misclassified
			// as a 302 (which downstream treats as block->rotate account).
			return fr, fmt.Errorf("go-wowa interact (retry): %w", rerr)
		}
		retryFR, perr := parseFetchResult(retryRes)
		if perr != nil {
			return fr, fmt.Errorf("parse go-wowa result (retry): %w", perr)
		}
		return retryFR, nil
	}
	return fr, nil
}

// doCDP routes a private API call through go-wowa's evaluate seam as an
// in-page fetch from a www.instagram.com tab. It targets the web API endpoints
// confirmed in PART 1; the mobile i.instagram.com path is left untouched when
// Config.WowaURL is empty.
func (c *Client) doCDP(ctx context.Context, endpoint, method, path string, form url.Values) ([]byte, error) {
	webURL, appID, body, err := c.buildWebRequest(endpoint, method, path, form)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", endpoint, err)
	}

	js, err := buildFetchScript(webURL, method, body, appID, "", "", "")
	if err != nil {
		return nil, fmt.Errorf("%s: build fetch script: %w", endpoint, err)
	}

	pageURL := igWebBaseURL + "/"
	fr, err := c.wowaFetchOnce(ctx, c.cfg.Session, pageURL, js)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", endpoint, err)
	}
	if fr.Redirected || fr.Status == 302 || fr.Status == 0 {
		return nil, &APIError{Status: 302, Class: errLoginRedirect, Message: "redirect detected"}
	}

	if fr.Status == 200 && len(fr.Body) > 0 && fr.Body[0] == '<' {
		return nil, fmt.Errorf("%s: HTML response from API", endpoint)
	}
	if fr.Status != 200 {
		return nil, &APIError{Status: fr.Status, Class: classifyHTTPStatus(fr.Status), Message: fmt.Sprintf("HTTP %d", fr.Status)}
	}
	return []byte(fr.Body), nil
}

// doGraphQLCDP routes a Threads GraphQL POST through go-wowa as an in-page
// same-origin fetch from a www.threads.net tab. It returns the raw response
// body, the HTTP status, and any transport/script error.
func (c *Client) doGraphQLCDP(ctx context.Context, endpoint, bodyStr, lsd, friendlyName string) ([]byte, int, error) {
	script, err := buildFetchScript(
		threadsBaseURL+"/api/graphql",
		http.MethodPost,
		bodyStr,
		igAppID,
		lsd,
		xAsbdID,
		friendlyName,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("%s: build fetch script: %w", endpoint, err)
	}

	pageURL := threadsBaseURL + "/"
	fr, err := c.wowaFetchOnce(ctx, c.cfg.Session, pageURL, script)
	if err != nil {
		return nil, 0, fmt.Errorf("%s: %w", endpoint, err)
	}
	if fr.Redirected || fr.Status == 302 || fr.Status == 0 {
		return nil, 302, nil
	}
	if fr.Status == 200 && len(fr.Body) > 0 && fr.Body[0] == '<' {
		return nil, 0, fmt.Errorf("%s: HTML response from API", endpoint)
	}
	if fr.Status != 200 {
		return nil, fr.Status, nil
	}
	return []byte(fr.Body), 200, nil
}

// fetchPageCDP fetches an arbitrary page URL through go-wowa as an in-page
// same-origin GET. It returns the body, status, and any transport/script error.
func (c *Client) fetchPageCDP(ctx context.Context, pageURL string) ([]byte, int, error) {
	appID := igWebAppID
	if strings.HasPrefix(pageURL, threadsBaseURL) {
		appID = igAppID
	}

	script, err := buildFetchScript(pageURL, http.MethodGet, "", appID, "", "", "")
	if err != nil {
		return nil, 0, fmt.Errorf("build fetch script: %w", err)
	}

	fr, err := c.wowaFetchOnce(ctx, c.cfg.Session, pageURL, script)
	if err != nil {
		return nil, 0, err
	}
	if fr.Redirected || fr.Status == 302 || fr.Status == 0 {
		return nil, 302, nil
	}
	if fr.Status != 200 {
		return nil, fr.Status, nil
	}
	return []byte(fr.Body), 200, nil
}

// buildWebRequest maps a mobile Private API path to the equivalent Instagram
// web endpoint and returns the full URL, app id, and body/query string.
func (c *Client) buildWebRequest(endpoint, method, path string, form url.Values) (string, string, string, error) {
	if method == http.MethodGet {
		webURL := igWebBaseURL + path
		if len(form) > 0 {
			webURL += "?" + form.Encode()
		}
		return webURL, igWebAppID, "", nil
	}

	if method == http.MethodPost {
		switch {
		case path == pathPublishText:
			return "", "", "", fmt.Errorf("web publish endpoint/doc_id not confirmed (TODO: capture authenticated web publish)")

		case strings.HasPrefix(path, "/api/v1/media/") && strings.Contains(path, "/like/"):
			id, ok := mediaIDFromMobilePath(path)
			if !ok {
				return "", "", "", fmt.Errorf("cannot extract media id from %q", path)
			}
			return igWebBaseURL + fmt.Sprintf(igWebLikePath, id), igWebAppID, form.Encode(), nil

		case strings.HasPrefix(path, "/api/v1/media/") && strings.Contains(path, "/unlike/"):
			id, ok := mediaIDFromMobilePath(path)
			if !ok {
				return "", "", "", fmt.Errorf("cannot extract media id from %q", path)
			}
			return igWebBaseURL + fmt.Sprintf(igWebUnlikePath, id), igWebAppID, form.Encode(), nil

		case strings.HasPrefix(path, "/api/v1/friendships/create/"):
			id, ok := userIDFromFriendshipsPath(path, "/api/v1/friendships/create/")
			if !ok {
				return "", "", "", fmt.Errorf("cannot extract user id from %q", path)
			}
			return igWebBaseURL + fmt.Sprintf(igWebFollowPath, id), igWebAppID, form.Encode(), nil

		case strings.HasPrefix(path, "/api/v1/friendships/destroy/"):
			id, ok := userIDFromFriendshipsPath(path, "/api/v1/friendships/destroy/")
			if !ok {
				return "", "", "", fmt.Errorf("cannot extract user id from %q", path)
			}
			return igWebBaseURL + fmt.Sprintf(igWebUnfollowPath, id), igWebAppID, form.Encode(), nil
		}
	}

	return "", "", "", fmt.Errorf("no CDP mapping for %s %s", method, path)
}

// mediaIDFromMobilePath extracts the numeric media id from a mobile path like
// /api/v1/media/<id>_<user_id>/like/.
func mediaIDFromMobilePath(path string) (string, bool) {
	prefix := "/api/v1/media/"
	if !strings.HasPrefix(path, prefix) {
		return "", false
	}
	rest := strings.TrimPrefix(path, prefix)
	parts := strings.SplitN(rest, "/", 2)
	if len(parts) == 0 || parts[0] == "" {
		return "", false
	}
	idParts := strings.SplitN(parts[0], "_", 2)
	if idParts[0] == "" {
		return "", false
	}
	// Ensure it is numeric, mirroring the private API convention.
	if _, err := strconv.ParseInt(idParts[0], 10, 64); err != nil {
		return "", false
	}
	return idParts[0], true
}

// userIDFromFriendshipsPath extracts the target user id from a friendships
// create/destroy mobile path.
func userIDFromFriendshipsPath(path, prefix string) (string, bool) {
	if !strings.HasPrefix(path, prefix) {
		return "", false
	}
	rest := strings.TrimPrefix(path, prefix)
	rest = strings.TrimSuffix(rest, "/")
	if rest == "" {
		return "", false
	}
	if _, err := strconv.ParseInt(rest, 10, 64); err != nil {
		return "", false
	}
	return rest, true
}
