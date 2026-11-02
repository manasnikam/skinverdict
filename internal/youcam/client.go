// Package youcam is a small client for the Perfect Corp YouCam v2 REST API.
//
// Every feature follows the same four steps:
//  1. POST /s2s/v2.0/file/{feature}   -> file_id + presigned PUT URL
//  2. PUT bytes to that URL
//  3. POST /s2s/v2.0/task/{feature}   -> task_id
//  4. GET  /s2s/v2.0/task/{feature}/{task_id} until success|error
//
// Units are only charged on success. Results are cached on disk keyed by
// feature + body + image hash so re-running the same input costs 0 units.
package youcam

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const DefaultBase = "https://yce-api-01.makeupar.com"

type Client struct {
	Base     string
	Key      string
	HTTP     *http.Client
	CacheDir string // "" disables caching
	Log      func(format string, a ...any)
}

func New(key string) *Client {
	return &Client{
		Base: DefaultBase, Key: key,
		HTTP:     &http.Client{Timeout: 60 * time.Second},
		CacheDir: ".youcam-cache",
		Log:      func(string, ...any) {},
	}
}

// ---------- generic transport ----------

type apiErr struct {
	Status int
	Body   string
}

func (e *apiErr) Error() string {
	return fmt.Sprintf("youcam: http %d: %s", e.Status, trunc(e.Body, 300))
}

func (c *Client) do(ctx context.Context, method, p string, body any, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.Base+p, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Key)
	req.Header.Set("Content-Type", "application/json")
	res, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, p, err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode == 429 {
		return errors.New("youcam: 429 rate limited (5 QPS / 250 per 5 min)")
	}
	if res.StatusCode >= 300 {
		return &apiErr{res.StatusCode, string(raw)}
	}
	if out != nil {
		return json.Unmarshal(raw, out)
	}
	return nil
}

// ---------- units ----------

type creditRow struct {
	Type      string  `json:"type"`
	Amount    float64 `json:"amount"`
	AmountDec float64 `json:"amount_dec"`
	Expiry    int64   `json:"expiry"`
}

// Balance returns the total units across all credit buckets.
func (c *Client) Balance(ctx context.Context) (float64, error) {
	var r struct {
		Results []creditRow `json:"results"`
	}
	if err := c.do(ctx, "GET", "/s2s/v1.0/client/credit", nil, &r); err != nil {
		return 0, err
	}
	var n float64
	for _, row := range r.Results {
		if row.AmountDec != 0 {
			n += row.AmountDec
		} else {
			n += row.Amount
		}
	}
	return n, nil
}

// ---------- upload ----------

func contentType(name string) string {
	if strings.HasSuffix(strings.ToLower(name), ".png") {
		return "image/png"
	}
	return "image/jpeg"
}

// Upload registers and uploads an image, returning its file_id.
func (c *Client) Upload(ctx context.Context, feature, name string, img []byte) (string, error) {
	meta := map[string]any{"files": []map[string]any{{
		"content_type": contentType(name), "file_name": name, "file_size": len(img),
	}}}
	var r struct {
		Data struct {
			Files []struct {
				FileID   string `json:"file_id"`
				Requests []struct {
					Method  string            `json:"method"`
					URL     string            `json:"url"`
					Headers map[string]string `json:"headers"`
				} `json:"requests"`
			} `json:"files"`
		} `json:"data"`
	}
	err := c.do(ctx, "POST", "/s2s/v2.0/file/"+feature, meta, &r)
	var ae *apiErr
	if errors.As(err, &ae) && ae.Status == 404 { // feature-scoped file endpoint not available for this feature
		err = c.do(ctx, "POST", "/s2s/v2.0/file", meta, &r)
	}
	if err != nil {
		return "", err
	}
	if len(r.Data.Files) == 0 || len(r.Data.Files[0].Requests) == 0 {
		return "", errors.New("youcam: file api returned no upload request")
	}
	f := r.Data.Files[0]
	rq := f.Requests[0]
	method := rq.Method
	if method == "" {
		method = "PUT"
	}
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		req, _ := http.NewRequestWithContext(ctx, method, rq.URL, bytes.NewReader(img))
		for k, v := range rq.Headers {
			req.Header.Set(k, v)
		}
		req.ContentLength = int64(len(img))
		res, err := c.HTTP.Do(req)
		if err != nil {
			lastErr = err
			time.Sleep(1500 * time.Millisecond)
			continue
		}
		b, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode >= 300 {
			return "", fmt.Errorf("youcam: s3 put %d: %s", res.StatusCode, trunc(string(b), 200))
		}
		return f.FileID, nil
	}
	return "", fmt.Errorf("youcam: s3 put: %w", lastErr)
}

// ---------- task + poll ----------

type TaskResult struct {
	Feature   string          `json:"feature"`
	TaskID    string          `json:"task_id"`
	RunMS     int64           `json:"run_ms"`
	UnitsCost float64         `json:"units_cost"`
	Cached    bool            `json:"cached"`
	Data      json.RawMessage `json:"data"` // the `data` object of the final GET
}

// RunTask uploads files (field -> bytes), creates the task, polls to completion.
// files maps the request field name (src_file_id, ref_file_id, ...) to image bytes.
func (c *Client) RunTask(ctx context.Context, feature string, body map[string]any, files map[string][]byte) (*TaskResult, error) {
	key := cacheKey(feature, body, files)
	if c.CacheDir != "" {
		if tr, ok := c.readCache(feature, key); ok {
			c.Log("youcam cache hit %s %s", feature, key)
			tr.Cached = true
			return tr, nil
		}
	}
	before, _ := c.Balance(ctx)

	b := map[string]any{}
	for k, v := range body {
		b[k] = v
	}
	for field, img := range files {
		id, err := c.Upload(ctx, feature, field+pickExt(img), img)
		if err != nil {
			return nil, err
		}
		b[field] = id
	}
	var created struct {
		Data struct {
			TaskID string `json:"task_id"`
		} `json:"data"`
	}
	if err := c.do(ctx, "POST", "/s2s/v2.0/task/"+feature, b, &created); err != nil {
		return nil, err
	}
	taskID := created.Data.TaskID
	if taskID == "" {
		return nil, errors.New("youcam: no task_id in response")
	}

	t0 := time.Now()
	wait := 2 * time.Second
	var data json.RawMessage
	for {
		var r struct {
			Data json.RawMessage `json:"data"`
		}
		if err := c.do(ctx, "GET", "/s2s/v2.0/task/"+feature+"/"+taskID, nil, &r); err != nil {
			return nil, err
		}
		var st struct {
			TaskStatus      string          `json:"task_status"`
			Status          string          `json:"status"`
			PollingInterval float64         `json:"polling_interval"`
			Error           json.RawMessage `json:"error"`
		}
		_ = json.Unmarshal(r.Data, &st)
		status := st.TaskStatus
		if status == "" {
			status = st.Status
		}
		switch status {
		case "success":
			data = r.Data
		case "error":
			return nil, fmt.Errorf("youcam: task %s failed: %s", feature, trunc(string(st.Error), 300))
		}
		if data != nil {
			break
		}
		if st.PollingInterval > 0 {
			wait = time.Duration(st.PollingInterval) * time.Millisecond
			if wait < time.Second {
				wait = time.Second
			}
		}
		if time.Since(t0) > 3*time.Minute {
			return nil, errors.New("youcam: poll timeout")
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}
		if wait < 10*time.Second {
			wait = wait * 3 / 2
		}
	}
	after, _ := c.Balance(ctx)
	tr := &TaskResult{Feature: feature, TaskID: taskID, RunMS: time.Since(t0).Milliseconds(), UnitsCost: before - after, Data: data}
	if c.CacheDir != "" {
		c.writeCache(feature, key, tr)
	}
	c.Log("youcam %s ok: %d ms, %.0f units", feature, tr.RunMS, tr.UnitsCost)
	return tr, nil
}

// ---------- skin analysis ----------

// SD concern names (cannot be mixed with hd_* names in one call).
var SDConcerns = []string{"wrinkle", "droopy_upper_eyelid", "droopy_lower_eyelid", "firmness", "acne", "moisture",
	"eye_bag", "dark_circle_v2", "age_spot", "radiance", "redness", "oiliness", "pore", "texture", "tear_trough", "skin_type"}

type ConcernScore struct {
	Type     string   `json:"type"`
	RawScore float64  `json:"raw_score"` // 1-100, higher = healthier. Use THIS for deltas.
	UIScore  float64  `json:"ui_score"`  // consumer-flattering number. Display only.
	MaskURLs []string `json:"mask_urls"` // presigned, ~2h TTL: download at scan time.
}

type SkinAnalysis struct {
	Concerns   map[string]ConcernScore    `json:"concerns"`
	Overall    float64                    `json:"overall"`
	SkinAge    float64                    `json:"skin_age"`
	ResizedURL string                     `json:"resized_url"`
	Extra      map[string]json.RawMessage `json:"extra"` // non-score outputs (e.g. skin_type), raw
	Task       *TaskResult                `json:"task"`
}

// SkinType extracts a profile label from the skin_type output when requested.
// The engine returns Normal / Oily / Dry / Combination (optionally "& Redness").
func (sa *SkinAnalysis) SkinType() string {
	raw, ok := sa.Extra["skin_type"]
	if !ok {
		return ""
	}
	s := strings.ToLower(string(raw))
	for _, k := range []string{"combination", "oily", "dry", "normal"} {
		if strings.Contains(s, k) {
			if strings.Contains(s, "redness") {
				return k + "+redness"
			}
			return k
		}
	}
	return ""
}

// SkinAnalyze runs SD skin analysis for the given concerns on a selfie.
// Cost: 9 units for 1-4 concerns, 12 for 5-8, 14 for 9-12, 16 for 13-16.
func (c *Client) SkinAnalyze(ctx context.Context, selfie []byte, concerns []string) (*SkinAnalysis, error) {
	if len(concerns) == 0 {
		return nil, errors.New("youcam: at least one concern required")
	}
	cs := append([]string(nil), concerns...)
	sort.Strings(cs) // stable cache key
	body := map[string]any{
		"dst_actions":     cs,
		"format":          "json",
		"miniserver_args": map[string]any{"enable_mask_overlay": true},
	}
	tr, err := c.RunTask(ctx, "skin-analysis", body, map[string][]byte{"src_file_id": selfie})
	if err != nil {
		return nil, err
	}
	var d struct {
		Results struct {
			Output []struct {
				Type     string   `json:"type"`
				RawScore float64  `json:"raw_score"`
				UIScore  float64  `json:"ui_score"`
				Score    float64  `json:"score"`
				MaskURLs []string `json:"mask_urls"`
			} `json:"output"`
		} `json:"results"`
	}
	if err := json.Unmarshal(tr.Data, &d); err != nil {
		return nil, fmt.Errorf("youcam: decode skin analysis: %w", err)
	}
	var rawOut struct {
		Results struct {
			Output []json.RawMessage `json:"output"`
		} `json:"results"`
	}
	_ = json.Unmarshal(tr.Data, &rawOut)
	sa := &SkinAnalysis{Concerns: map[string]ConcernScore{}, Extra: map[string]json.RawMessage{}, Task: tr}
	for i, o := range d.Results.Output {
		switch o.Type {
		case "skin_type":
			if i < len(rawOut.Results.Output) {
				sa.Extra[o.Type] = rawOut.Results.Output[i]
			}
			continue
		case "all":
			sa.Overall = o.Score
		case "skin_age":
			sa.SkinAge = o.Score
		case "resize_image":
			if len(o.MaskURLs) > 0 {
				sa.ResizedURL = o.MaskURLs[0]
			}
		default:
			sa.Concerns[o.Type] = ConcernScore{Type: o.Type, RawScore: o.RawScore, UIScore: o.UIScore, MaskURLs: o.MaskURLs}
		}
	}
	return sa, nil
}

// Download fetches a result/mask URL (no auth needed; presigned).
func (c *Client) Download(ctx context.Context, url string) ([]byte, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	res, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		return nil, fmt.Errorf("download %d", res.StatusCode)
	}
	return io.ReadAll(res.Body)
}

// ---------- cache ----------

func cacheKey(feature string, body map[string]any, files map[string][]byte) string {
	h := sha1.New()
	h.Write([]byte(feature))
	bj, _ := json.Marshal(body)
	h.Write(bj)
	keys := make([]string, 0, len(files))
	for k := range files {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		h.Write([]byte(k))
		h.Write(files[k])
	}
	return hex.EncodeToString(h.Sum(nil))[:12]
}

func (c *Client) cachePath(feature, key string) string {
	return filepath.Join(c.CacheDir, feature+"-"+key+".json")
}

func (c *Client) readCache(feature, key string) (*TaskResult, bool) {
	b, err := os.ReadFile(c.cachePath(feature, key))
	if err != nil {
		return nil, false
	}
	var tr TaskResult
	if json.Unmarshal(b, &tr) != nil {
		return nil, false
	}
	return &tr, true
}

func (c *Client) writeCache(feature, key string, tr *TaskResult) {
	_ = os.MkdirAll(c.CacheDir, 0o755)
	b, _ := json.MarshalIndent(tr, "", "  ")
	_ = os.WriteFile(c.cachePath(feature, key), b, 0o644)
}

func pickExt(img []byte) string {
	if len(img) > 8 && string(img[1:4]) == "PNG" {
		return ".png"
	}
	return ".jpg"
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
