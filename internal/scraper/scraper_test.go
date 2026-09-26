package scraper

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/mehmetfatihvar/staj/staj/bologna-scraper/internal/fetch"
	"github.com/mehmetfatihvar/staj/staj/bologna-scraper/internal/parse"
)

// fixtureServer, parse/testdata altındaki sayfaları sayfa adına göre sunar ve istekleri sayar.
func fixtureServer(t *testing.T) (*httptest.Server, map[string]int, *sync.Mutex) {
	t.Helper()
	hits := map[string]int{}
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits[r.URL.Path+"?"+r.URL.Query().Get("curCourse")]++
		mu.Unlock()
		b, err := os.ReadFile(filepath.Join("..", "parse", "testdata", filepath.Base(r.URL.Path)))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(b)
	}))
	t.Cleanup(srv.Close)
	return srv, hits, &mu
}

func TestDiscoverAndRun(t *testing.T) {
	srv, hits, mu := fixtureServer(t)
	c, err := fetch.New(fetch.Options{BaseURL: srv.URL + "/oibs/bologna/", Lang: "tr", RPS: 1000})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	sc := &Scraper{Client: c, Sink: NewJSONSink(&out), Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Workers: 3}

	refs, err := sc.Discover(context.Background(), "lisans", "lis")
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 3 || refs[2].ID != 352 || refs[2].UnitID != 12 || refs[0].Level != "lisans" {
		t.Fatalf("keşif: %+v", refs)
	}

	st := sc.Run(context.Background(), refs)
	if st.Programs != 3 || st.Errors != 0 {
		t.Fatalf("stats: %+v", st)
	}
	// 4 farklı ders (3 plan + 1 seçmeli havuz) üç programda ortak: her detay yalnızca bir kez indirilmeli.
	if st.Courses != 4 {
		t.Fatalf("ders detayı sayısı %d, 4 beklendi", st.Courses)
	}
	mu.Lock()
	for k, n := range hits {
		if strings.Contains(k, "progCourseDetails") && n != 1 {
			t.Errorf("%s %d kez indirildi", k, n)
		}
	}
	mu.Unlock()

	kinds := map[string]int{}
	var secmeli bool
	sc2 := bufio.NewScanner(&out)
	sc2.Buffer(make([]byte, 1<<20), 1<<20)
	for sc2.Scan() {
		var m struct {
			Type string          `json:"type"`
			Data json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(sc2.Bytes(), &m); err != nil {
			t.Fatal(err)
		}
		kinds[m.Type]++
		if m.Type == "curriculum" && strings.Contains(string(m.Data), `"course_id":100010`) && strings.Contains(string(m.Data), `"secmeli"`) {
			secmeli = true
		}
	}
	if kinds["unit"] != 3 || kinds["program"] != 3 || kinds["curriculum"] != 3 || kinds["course"] != 4 {
		t.Fatalf("çıktı türleri: %v", kinds)
	}
	if !secmeli {
		t.Fatal("seçmeli havuzdaki ders plana eklenmedi")
	}
}

func TestCancelledRunStops(t *testing.T) {
	srv, _, _ := fixtureServer(t)
	c, _ := fetch.New(fetch.Options{BaseURL: srv.URL + "/", RPS: 1000})
	sc := &Scraper{Client: c, Sink: NewJSONSink(io.Discard), Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	refs, _ := sc.Discover(context.Background(), "lisans", "lis")
	if st := sc.Run(ctx, refs); st.Programs != 0 {
		t.Fatalf("iptal edilen çalışma program işledi: %+v", st)
	}
}

// Gerçek BİLGİSAYAR MÜHENDİSLİĞİ planı: 43 doğrudan ders + 285 seçmeli havuz dersi. OİBS havuz
// derslerini her programa ayrı kopya (curCourse) olarak koyar; hepsi bu programa aittir.
// 17 grup satırının detay sayfası yoktur. --skip-pool-details ile yalnızca 43 detay indirilir.
func TestRealPlanDetailsAndOwnership(t *testing.T) {
	for _, tc := range []struct {
		skipPool    bool
		wantDetails int
	}{{false, 328}, {true, 43}} {
		srv, hits := realSite(t)
		c, _ := fetch.New(fetch.Options{BaseURL: srv.URL + "/", RPS: 5000})
		var out bytes.Buffer
		sc := &Scraper{Client: c, Sink: NewJSONSink(&out), Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Workers: 2, SkipPoolDetails: tc.skipPool}
		st := sc.Run(context.Background(), []parse.ProgramRef{{ID: 1371, UnitID: 22}})
		if st.Errors != 0 || st.Courses != int64(tc.wantDetails) || hits() != tc.wantDetails {
			t.Fatalf("skipPool=%v stats=%+v detay isteği=%d", tc.skipPool, st, hits())
		}
		owners := map[int64]int{}
		dec := json.NewDecoder(&out)
		for dec.More() {
			var m struct {
				Type string `json:"type"`
				Data struct {
					ProgramID int64 `json:"program_id"`
				} `json:"data"`
			}
			if err := dec.Decode(&m); err != nil {
				t.Fatal(err)
			}
			if m.Type == "course" {
				owners[m.Data.ProgramID]++
			}
		}
		if len(owners) != 1 || owners[1371] != tc.wantDetails {
			t.Fatalf("sahiplik: %v", owners)
		}
	}
}

// realSite, testdata/real sayfalarını sunar; her ders için aynı detay sayfası döner.
func realSite(t *testing.T) (*httptest.Server, func() int) {
	t.Helper()
	real := filepath.Join("..", "parse", "testdata", "real")
	pages := map[string]string{
		"/progAbout.aspx":         "progAbout_1371.html",
		"/progLearnOutcomes.aspx": "progLearnOutcomes_1371.html",
		"/progCourses.aspx":       "progCourses_1371.html",
		"/progCourseDetails.aspx": "progCourseDetails_1098805.html",
	}
	var mu sync.Mutex
	detailHits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f, ok := pages[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		if r.URL.Path == "/progCourseDetails.aspx" {
			mu.Lock()
			detailHits++
			mu.Unlock()
		}
		b, _ := os.ReadFile(filepath.Join(real, f))
		_, _ = w.Write(b)
	}))
	t.Cleanup(srv.Close)
	return srv, func() int { mu.Lock(); defer mu.Unlock(); return detailHits }
}
