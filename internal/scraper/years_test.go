package scraper

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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
