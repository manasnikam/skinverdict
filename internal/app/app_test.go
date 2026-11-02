package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/manasnikam/skinverdict/internal/catalog"
	"github.com/manasnikam/skinverdict/internal/store"
	"github.com/manasnikam/skinverdict/internal/verdict"
	"github.com/manasnikam/skinverdict/internal/youcam"
)

// fakeYouCam mimics the v2 file/task/poll flow and the exact response shape
// observed from the real API (results.output[] with type/raw_score/ui_score/mask_urls).
func fakeYouCam(t *testing.T, scores []map[string]float64) *httptest.Server {
	t.Helper()
	call := 0
	mux := http.NewServeMux()
	var srv *httptest.Server
	mux.HandleFunc("GET /s2s/v1.0/client/credit", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"status":200,"results":[{"type":"ApiPaygToken","amount":%d,"amount_dec":%d}]}`, 1000-9*call, 1000-9*call)
	})
	mux.HandleFunc("POST /s2s/v2.0/file/{feature}", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"status":200,"data":{"files":[{"file_id":"f1","requests":[{"method":"PUT","url":"%s/put","headers":{"Content-Type":"image/jpeg"}}]}]}}`, srv.URL)
	})
	mux.HandleFunc("PUT /put", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("POST /s2s/v2.0/task/{feature}", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["src_file_id"] != "f1" {
			t.Errorf("src_file_id not forwarded: %v", body)
		}
		fmt.Fprintf(w, `{"status":200,"data":{"task_id":"t%d"}}`, call)
	})
	mux.HandleFunc("GET /s2s/v2.0/task/{feature}/{id}", func(w http.ResponseWriter, _ *http.Request) {
		sc := scores[call%len(scores)]
		call++
		var out []string
		for k, v := range sc {
			out = append(out, fmt.Sprintf(`{"type":%q,"raw_score":%v,"ui_score":%v,"mask_urls":["%s/mask/%s"]}`, k, v, v+20, srv.URL, k))
		}
		out = append(out, `{"type":"all","score":80}`, `{"type":"skin_age","score":33}`)
		fmt.Fprintf(w, `{"status":200,"data":{"task_status":"success","results":{"output":[%s]}}}`, strings.Join(out, ","))
	})
	mux.HandleFunc("GET /mask/{c}", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("jpegbytes")) })
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestScanVerdictCheckIn(t *testing.T) {
	dir := t.TempDir()
	cat, err := catalog.Load(filepath.Join("..", "..", "data", "catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	st, _ := store.OpenJSON(filepath.Join(dir, "db.json"))
	srv := fakeYouCam(t, []map[string]float64{
		{"pore": 22, "oiliness": 60, "redness": 90}, // baseline
		{"pore": 25, "oiliness": 61, "redness": 89}, // day 7
		{"pore": 31, "oiliness": 63, "redness": 88}, // day 21 -> working
	})
	yc := youcam.New("k")
	yc.Base = srv.URL
	yc.CacheDir = "" // distinct images below anyway
	a := &App{Store: st, Catalog: cat, YouCam: yc, Agent: RuleAgent{}, DataDir: dir, Options: verdict.DefaultOptions(), CheckGap: time.Hour}

	p := &store.Persona{Name: "Manas"}
	if err := st.UpsertPersona(p); err != nil {
		t.Fatal(err)
	}
	tr, err := a.StartTrial(p.ID, "nia10")
	if err != nil {
		t.Fatal(err)
	}
	if tr.Target != "pore" || len(tr.Concerns) != 4 || tr.Concerns[3] != "skin_type" {
		t.Fatalf("trial %+v", tr)
	}

	day0 := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	for i, d := range []int{0, 7, 21} {
		img := []byte(fmt.Sprintf("img%d", i))
		sc, err := a.Scan(context.Background(), tr.ID, img, day0.AddDate(0, 0, d), "directed")
		if err != nil {
			t.Fatal(err)
		}
		if sc.Scores["pore"] == 0 || sc.MaskPaths["pore"] == "" {
			t.Fatalf("scan %d: %+v", i, sc)
		}
	}

	_, scans, v, err := a.Verdict(tr.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(scans) != 3 || v.Status != verdict.Working || v.Delta != 9 || v.Days != 21 {
		t.Fatalf("verdict %+v", v)
	}

	tr2, _, err := a.CheckIn(context.Background(), tr.ID)
	if err != nil {
		t.Fatal(err)
	}
	if tr2.Status != "reordered" || tr2.LastAction == nil || tr2.LastAction.Kind != "reorder" {
		t.Fatalf("after checkin %+v", tr2)
	}
}

func TestProofGuaranteeAndAdherence(t *testing.T) {
	dir := t.TempDir()
	cat, _ := catalog.Load(filepath.Join("..", "..", "data", "catalog.json"))
	st, _ := store.OpenJSON(filepath.Join(dir, "db.json"))
	// 4 scans over 28 days, flat/declining on pore, oiliness low => "oily" profile via heuristic
	srv := fakeYouCam(t, []map[string]float64{
		{"pore": 30, "oiliness": 40, "redness": 90},
		{"pore": 29, "oiliness": 41, "redness": 90},
		{"pore": 31, "oiliness": 39, "redness": 90},
		{"pore": 28, "oiliness": 40, "redness": 90},
	})
	yc := youcam.New("k")
	yc.Base, yc.CacheDir = srv.URL, ""
	a := &App{Store: st, Catalog: cat, YouCam: yc, Agent: RuleAgent{}, DataDir: dir, Options: verdict.DefaultOptions(), CheckGap: time.Hour}
	p := &store.Persona{Name: "N"}
	_ = st.UpsertPersona(p)
	tr, _ := a.StartTrial(p.ID, "nia10")
	day0 := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	for i, d := range []int{0, 7, 14, 28} {
		if _, err := a.Scan(context.Background(), tr.ID, []byte(fmt.Sprintf("i%d", i)), day0.AddDate(0, 0, d), "directed"); err != nil {
			t.Fatal(err)
		}
	}
	if got, _ := st.Persona(p.ID); got.Profile != "oily" {
		t.Fatalf("profile %q", got.Profile)
	}
	tr2, v, err := a.CheckIn(context.Background(), tr.ID)
	if err != nil {
		t.Fatal(err)
	}
	if v.Status != verdict.NotWorking || v.Confidence != "high" {
		t.Fatalf("verdict %+v", v)
	}
	if tr2.Status != "credited" || tr2.LastAction.Kind != "refund_credit" || tr2.LastAction.CreditINR != 699 || tr2.LastAction.SwapTo == "" {
		t.Fatalf("action %+v", tr2.LastAction)
	}
	// a second check-in on a closed trial must not issue another credit
	tr4, _, err := a.CheckIn(context.Background(), tr.ID)
	if err != nil || tr4.LastAction.OrderRef != tr2.LastAction.OrderRef {
		t.Fatalf("re-check-in on credited trial: %v %+v", err, tr4.LastAction)
	}

	// adherence: a scan after skipped use is excluded from the judgement
	tr3, _ := a.StartTrial(p.ID, "nia10")
	srv2 := fakeYouCam(t, []map[string]float64{{"pore": 30}, {"pore": 20}})
	yc.Base = srv2.URL
	_, _ = a.Scan(context.Background(), tr3.ID, []byte("a"), day0, "directed")
	_, _ = a.Scan(context.Background(), tr3.ID, []byte("b"), day0.AddDate(0, 0, 14), "skipped")
	_, _, v3, _ := a.Verdict(tr3.ID)
	if v3.Status != verdict.NotYet || v3.Scans != 1 {
		t.Fatalf("adherence verdict %+v", v3)
	}
}
