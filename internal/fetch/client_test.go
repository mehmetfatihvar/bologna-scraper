package fetch

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/text/encoding/charmap"
)

func newTest(t *testing.T, h http.HandlerFunc, o Options) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	o.BaseURL = srv.URL + "/oibs/bologna/"
	if o.RPS == 0 {
		o.RPS = 1000
	}
	c, err := New(o)
	if err != nil {
		t.Fatal(err)
	}
	return c, srv
}

func TestURLAddsLang(t *testing.T) {
	c, _ := New(Options{BaseURL: "https://obs.example/oibs/bologna", Lang: "tr"})
	got := c.URL("progAbout.aspx", map[string][]string{"curSunit": {"352"}})
	if got != "https://obs.example/oibs/bologna/progAbout.aspx?curSunit=352&lang=tr" {
		t.Fatal(got)
	}
	if got := c.Resolve("index.aspx?lang=en&curSunit=1"); !strings.Contains(got, "lang=en") || strings.Contains(got, "lang=tr") {
		t.Fatalf("mevcut lang ezilmemeli: %s", got)
	}
}

func TestWindows1254Decoded(t *testing.T) {
	raw, _ := charmap.Windows1254.NewEncoder().String("<html><body>Dersin Öğrenme Çıktıları ş ı İ</body></html>")
	c, srv := newTest(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=windows-1254")
		_, _ = w.Write([]byte(raw))
	}, Options{})
	b, err := c.Get(context.Background(), srv.URL+"/x")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "Öğrenme Çıktıları ş ı İ") {
		t.Fatalf("UTF-8'e çevrilmedi: %q", b)
	}
}

func TestUndeclaredLegacyEncodingAssumedTurkish(t *testing.T) {
	raw, _ := charmap.Windows1254.NewEncoder().String("<html><body>Yarıyıl ş ğ</body></html>")
	c, srv := newTest(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(raw))
	}, Options{})
	b, _ := c.Get(context.Background(), srv.URL+"/x")
	if !strings.Contains(string(b), "Yarıyıl ş ğ") {
		t.Fatalf("got %q", b)
	}
}

// Gerçek OİBS sayfası: charset bildirimi yok, ilk 1 KB saf ASCII, Türkçe metin çok sonra.
func TestUndeclaredUTF8WithLongASCIIPrefixKept(t *testing.T) {
	page := "<html><head>" + strings.Repeat("<link href='x.css' rel='stylesheet' />", 100) + "</head><body>MÜHENDİSLİK FAKÜLTESİ 1.Yarıyıl</body></html>"
	c, srv := newTest(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(page))
	}, Options{})
	b, _ := c.Get(context.Background(), srv.URL+"/x")
	if string(b) != page {
		t.Fatalf("UTF-8 sayfa bozuldu: %q", b[len(b)-60:])
	}
}

func TestRetryOn429ThenOK(t *testing.T) {
	var n atomic.Int32
	c, srv := newTest(t, func(w http.ResponseWriter, r *http.Request) {
		if n.Add(1) == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte("ok"))
	}, Options{Retries: 2})
	b, err := c.Get(context.Background(), srv.URL+"/x")
	if err != nil || string(b) != "ok" || n.Load() != 2 {
		t.Fatalf("b=%q err=%v n=%d", b, err, n.Load())
	}
}

func TestNotFoundNotRetried(t *testing.T) {
	var n atomic.Int32
	c, srv := newTest(t, func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		http.NotFound(w, r)
	}, Options{Retries: 3})
	_, err := c.Get(context.Background(), srv.URL+"/x")
	if !IsNotFound(err) || n.Load() != 1 {
		t.Fatalf("err=%v n=%d", err, n.Load())
	}
}

func TestCookiesKept(t *testing.T) {
	c, srv := newTest(t, func(w http.ResponseWriter, r *http.Request) {
		if _, err := r.Cookie("ASP.NET_SessionId"); err != nil {
			http.SetCookie(w, &http.Cookie{Name: "ASP.NET_SessionId", Value: "abc", Path: "/"})
			_, _ = w.Write([]byte("yeni"))
			return
		}
		_, _ = w.Write([]byte("oturum"))
	}, Options{})
	ctx := context.Background()
	_, _ = c.Get(ctx, srv.URL+"/a")
	b, _ := c.Get(ctx, srv.URL+"/b")
	if string(b) != "oturum" {
		t.Fatalf("çerez taşınmadı: %q", b)
	}
}

func TestCache(t *testing.T) {
	var n atomic.Int32
	dir := t.TempDir()
	h := func(w http.ResponseWriter, r *http.Request) { n.Add(1); _, _ = w.Write([]byte("sayfa")) }
	c, srv := newTest(t, h, Options{CacheDir: dir, UseCache: true})
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if b, err := c.Get(ctx, srv.URL+"/x"); err != nil || string(b) != "sayfa" {
			t.Fatal(b, err)
		}
	}
	if n.Load() != 1 {
		t.Fatalf("cache kullanılmadı: %d istek", n.Load())
	}
}

func TestRateLimitAndCancel(t *testing.T) {
	c, srv := newTest(t, func(w http.ResponseWriter, r *http.Request) {}, Options{RPS: 5})
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	start, done := time.Now(), 0
	for {
		if _, err := c.Get(ctx, srv.URL+"/x"); err != nil {
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal(err)
			}
			break
		}
		done++
	}
	// 5 rps ile 0.5 sn'de en fazla ~3 istek geçebilir.
	if done > 4 || time.Since(start) > 2*time.Second {
		t.Fatalf("hız sınırı çalışmıyor: %d istek", done)
	}
}

func TestParseRetryAfter(t *testing.T) {
	if parseRetryAfter("3") != 3*time.Second || parseRetryAfter("") != 0 || parseRetryAfter("9999") != 2*time.Minute {
		t.Fatal("parseRetryAfter")
	}
}

// Sunucu 503 dönmeye devam ederse istemci tüm istekleri durdurur ve cezayı büyütür; başarıda sıfırlar.
func TestCooldownOnPersistentServerErrors(t *testing.T) {
	var fail atomic.Bool
	fail.Store(true)
	var cooldowns []time.Duration
	c, srv := newTest(t, func(w http.ResponseWriter, r *http.Request) {
		if fail.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("ok"))
	}, Options{Retries: 0, Cooldown: 100 * time.Millisecond, OnCooldown: func(d time.Duration, _ error) { cooldowns = append(cooldowns, d) }})
	ctx := context.Background()
	_, _ = c.Get(ctx, srv.URL+"/a")
	start := time.Now()
	_, _ = c.Get(ctx, srv.URL+"/b") // ilk cezayı (100ms) beklemeli
	if time.Since(start) < 90*time.Millisecond {
		t.Fatalf("cooldown beklenmedi: %v", time.Since(start))
	}
	if len(cooldowns) != 2 || cooldowns[0] != 100*time.Millisecond || cooldowns[1] != 200*time.Millisecond {
		t.Fatalf("ceza büyümedi: %v", cooldowns)
	}
	fail.Store(false)
	time.Sleep(250 * time.Millisecond)
	if b, err := c.Get(ctx, srv.URL+"/c"); err != nil || string(b) != "ok" {
		t.Fatal(b, err)
	}
	fail.Store(true)
	_, _ = c.Get(ctx, srv.URL+"/d")
	if cooldowns[len(cooldowns)-1] != 100*time.Millisecond {
		t.Fatalf("başarıdan sonra ceza sıfırlanmadı: %v", cooldowns)
	}
}

func TestNotFoundDoesNotTriggerCooldown(t *testing.T) {
	called := false
	c, srv := newTest(t, func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) },
		Options{Retries: 0, OnCooldown: func(time.Duration, error) { called = true }})
	_, _ = c.Get(context.Background(), srv.URL+"/x")
	if called {
		t.Fatal("404 sunucu hatası sayılmamalı")
	}
}

// Session: ayrı çerez kavanozu, ortak hız sınırı/bekleme durumu.
func TestSessionSharesLimiter(t *testing.T) {
	c, err := New(Options{BaseURL: "http://example.invalid/", RPS: 1})
	if err != nil {
		t.Fatal(err)
	}
	s := c.Session()
	if s.limiter != c.limiter || s.http == c.http || s.http.Jar == c.http.Jar {
		t.Fatal("oturum hız sınırını paylaşmalı, çerezleri paylaşmamalı")
	}
}
