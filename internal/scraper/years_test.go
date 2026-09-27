package scraper

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/mehmetfatihvar/staj/staj/bologna-scraper/internal/fetch"
	"github.com/mehmetfatihvar/staj/staj/bologna-scraper/internal/parse"
)

// Gerçek sayfadaki yıl seçici: 2026 seçili, 2025 ve 2024 eski planlar.
func TestRealPlanYears(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "parse", "testdata", "real", "progCourses_1371.html"))
	if err != nil {
		t.Fatal(err)
	}
	cur, err := parse.ParseCurriculum(b)
	if err != nil {
		t.Fatal(err)
	}
	if len(cur.Years) != 3 || cur.Year == nil || *cur.Year != 2026 || !cur.Years[0].Selected || cur.Years[1].Value != "16638" {
		t.Fatalf("yıllar: %+v seçili=%v", cur.Years, cur.Year)
	}
	if py, ok := FindPlanYear(cur.Years, "2025"); !ok || py.Value != "16638" {
		t.Fatalf("2025 bulunamadı: %+v", py)
	}
	if _, ok := FindPlanYear(cur.Years, "16528"); !ok {
		t.Fatal("değerle arama")
	}
}

// Postback: formun gizli alanları aynen geri gönderilir, yalnızca yıl ve olay hedefi değişir.
func TestYearPostback(t *testing.T) {
	page := func(sel string) string {
		opt := func(v, y string) string {
			s := ""
			if v == sel {
				s = ` selected="selected"`
			}
			return `<option` + s + ` value="` + v + `">` + y + ` (` + y + ` - PROG)</option>`
		}
		return `<html><body><form method="post" action="./progCourses.aspx?curSunit=5&amp;lang=tr" id="form1">
<input type="hidden" name="__VIEWSTATE" value="VS-` + sel + `" />
<input type="hidden" name="__EVENTVALIDATION" value="EV" />
<input type="submit" name="btn" value="x" />
<select name="cmbYillar">` + opt("200", "2026") + opt("100", "2025") + `</select>
</form></body></html>`
	}
	var got map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			_ = r.ParseForm()
			got = map[string]string{"path": r.URL.RequestURI()}
			for k := range r.PostForm {
				got[k] = r.PostForm.Get(k)
			}
			_, _ = io.WriteString(w, page(r.PostForm.Get("cmbYillar")))
			return
		}
		_, _ = io.WriteString(w, page("200"))
	}))
	defer srv.Close()
	c, _ := fetch.New(fetch.Options{BaseURL: srv.URL + "/", RPS: 1000})
	pageURL := srv.URL + "/progCourses.aspx?curSunit=5&lang=tr"
	first, err := c.Get(context.Background(), pageURL)
	if err != nil {
		t.Fatal(err)
	}
	out, err := YearPostback(context.Background(), c, pageURL, first, "100")
	if err != nil {
		t.Fatal(err)
	}
	if got["path"] != "/progCourses.aspx?curSunit=5&lang=tr" || got["__VIEWSTATE"] != "VS-200" || got["__EVENTVALIDATION"] != "EV" ||
		got["__EVENTTARGET"] != "cmbYillar" || got["cmbYillar"] != "100" || got["btn"] != "" {
		t.Fatalf("gönderilen form: %v", got)
	}
	cur, _ := parse.ParseCurriculum(out)
	if cur.Year == nil || *cur.Year != 2025 || !strings.Contains(string(out), "VS-100") {
		t.Fatalf("dönen sayfa: %v", cur.Year)
	}
}

// Aynı yıla ait iki plan (sitede görüldü: bir programda iki ayrı "2026"): ikisi de ayrı plan sayılır.
func TestOlderYearsSameYear(t *testing.T) {
	y := 2025
	cur := &parse.Curriculum{Year: &y, PlanID: "10", Years: []parse.PlanYear{
		{Value: "30", Year: 2026}, {Value: "31", Year: 2026}, {Value: "10", Year: 2025, Selected: true},
		{Value: "11", Year: 2025}, {Value: "5", Year: 2023},
	}}
	var got []string
	for _, py := range (&Scraper{PlanYears: -1}).olderYears(cur) {
		got = append(got, py.Value)
	}
	if strings.Join(got, ",") != "30,31,11,5" {
		t.Fatalf("eski planlar: %v", got)
	}
	got = nil
	for _, py := range (&Scraper{PlanYears: 3}).olderYears(cur) {
		got = append(got, py.Value)
	}
	if strings.Join(got, ",") != "30,31" {
		t.Fatalf("--years 3: %v", got)
	}
	if len((&Scraper{}).olderYears(cur)) != 0 {
		t.Fatal("varsayılan yalnızca güncel plan")
	}
}

// sessionSite, OİBS'nin oturum davranışını taklit eder: yıl seçici postback'i seçilen planı ASP.NET
// oturumunda saklar; aynı oturumla başka programın ders planı istenirse sayfa boş döner.
func sessionSite(t *testing.T) (*httptest.Server, func() int) {
	t.Helper()
	plans := map[string]struct {
		prog, year string
		course     int
	}{"100": {"1", "2026", 1001}, "101": {"1", "2025", 1002}, "200": {"2", "2026", 2001}, "201": {"2", "2025", 2002}}
	current := map[string]string{"1": "100", "2": "200"}
	page := func(prog, sel string) string {
		var opts strings.Builder
		for _, v := range []string{"100", "101", "200", "201"} {
			if plans[v].prog != prog {
				continue
			}
			s := ""
			if v == sel {
				s = ` selected="selected"`
			}
			opts.WriteString(`<option` + s + ` value="` + v + `">` + plans[v].year + ` (` + plans[v].year + ` - P)</option>`)
		}
		id := strconv.Itoa(plans[sel].course)
		return `<html><body><form method="post" action="./progCourses.aspx?curSunit=` + prog + `" id="form1">
<input type="hidden" name="__VIEWSTATE" value="VS" /><select name="cmbYillar">` + opts.String() + `</select>
<h4>1. Yarıyıl Ders Planı</h4><table><tr><th>Kodu</th><th>Dersin Adı</th><th>Z/S</th><th>AKTS</th></tr>
<tr><td><a href="progCourseDetails.aspx?curCourse=` + id + `">K` + id + `</a></td><td>Ders</td><td>Z</td><td>5</td></tr></table></form></body></html>`
	}
	var mu sync.Mutex
	sessions := map[string]string{} // oturum -> seçili plan
	n, empty := 0, 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/progCourses.aspx":
		case "/progAbout.aspx":
			_, _ = io.WriteString(w, `<html><body></body></html>`)
			return
		default:
			http.NotFound(w, r)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		sid := ""
		if c, err := r.Cookie("ASP.NET_SessionId"); err == nil {
			sid = c.Value
		} else {
			n++
			sid = strconv.Itoa(n)
			http.SetCookie(w, &http.Cookie{Name: "ASP.NET_SessionId", Value: sid, Path: "/"})
		}
		prog := r.URL.Query().Get("curSunit")
		sel := current[prog]
		if r.Method == http.MethodPost {
			sel = r.FormValue("cmbYillar")
			sessions[sid] = sel
		} else if v, ok := sessions[sid]; ok {
			sel = v
		}
		if plans[sel].prog != prog { // oturumdaki plan başka programın: site boş sayfa döner
			empty++
			_, _ = io.WriteString(w, `<html><body><form method="post" action="x"></form></body></html>`)
			return
		}
		_, _ = io.WriteString(w, page(prog, sel))
	}))
	t.Cleanup(srv.Close)
	return srv, func() int { mu.Lock(); defer mu.Unlock(); return empty }
}

// İki program ard arda (aynı istemciyle) taranır: ikincisinin güncel planı, birincisinin yıl seçimi
// yüzünden boş gelmemelidir.
func TestPlanYearsSessionIsolation(t *testing.T) {
	srv, empties := sessionSite(t)
	c, _ := fetch.New(fetch.Options{BaseURL: srv.URL + "/", RPS: 5000})
	var out bytes.Buffer
	sc := &Scraper{Client: c, Sink: NewJSONSink(&out), Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Workers: 1, PlanYears: -1, SkipDetails: true}
	st := sc.Run(context.Background(), []parse.ProgramRef{{ID: 1}, {ID: 2}})
	if st.Errors != 0 || st.Programs != 2 || empties() != 0 {
		t.Fatalf("stats=%+v boş sayfa=%d", st, empties())
	}
	var plans []string
	dec := json.NewDecoder(&out)
	for dec.More() {
		var m struct {
			Type string `json:"type"`
			Data struct {
				ProgramID  int64 `json:"program_id"`
				Curriculum struct {
					PlanID  string                  `json:"plan_id"`
					Entries []parse.CurriculumEntry `json:"entries"`
				} `json:"curriculum"`
			} `json:"data"`
		}
		if err := dec.Decode(&m); err != nil {
			t.Fatal(err)
		}
		if m.Type == "curriculum" {
			plans = append(plans, strconv.FormatInt(m.Data.ProgramID, 10)+":"+m.Data.Curriculum.PlanID+"/"+strconv.Itoa(len(m.Data.Curriculum.Entries)))
		}
	}
	if strings.Join(plans, ",") != "1:100/1,1:101/1,2:200/1,2:201/1" {
		t.Fatalf("planlar: %v", plans)
	}
}
