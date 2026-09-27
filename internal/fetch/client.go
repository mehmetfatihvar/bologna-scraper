// Package fetch, OİBS sunucusuna nazik (rate-limited) ve dayanıklı (retry)
// HTTP GET istekleri atar. İsteğe bağlı olarak ham HTML'i diske cache'ler;
// parser geliştirirken sunucuyu tekrar tekrar yormamak için kullanışlıdır.
package fetch

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"golang.org/x/net/html/charset"
	"golang.org/x/text/encoding/charmap"
)

type Options struct {
	BaseURL   string        // ör. https://obs.mersin.edu.tr/oibs/bologna/
	Lang      string        // tr | en  (her isteğe lang= olarak eklenir)
	RPS       float64       // saniyedeki azami istek sayısı (tüm worker'lar toplamı)
	Timeout   time.Duration // tek istek zaman aşımı
	Retries   int           // 5xx / 429 / ağ hatasında tekrar sayısı
	UserAgent string
	CacheDir  string // boş değilse ham HTML buraya yazılır
	UseCache  bool   // true ise önce cache'e bakılır
	// Cooldown: bir istek tüm denemelere rağmen sunucu kaynaklı (5xx/429/zaman aşımı) başarısız olursa
	// TÜM istekler bu süre durur; ardışık her başarısızlıkta ikiye katlanır (en fazla 10×), ilk başarıda
	// sıfırlanır. Sunucu yük altındayken üstüne gitmemek için. Varsayılan 30 sn.
	Cooldown   time.Duration
	OnCooldown func(d time.Duration, err error) // isteğe bağlı: bekleme başlarken (loglamak için)
}

type HTTPError struct {
	URL    string
	Status int
}

func (e *HTTPError) Error() string { return fmt.Sprintf("HTTP %d: %s", e.Status, e.URL) }

// IsNotFound, sayfanın bu program için mevcut olmadığını gösterir.
func IsNotFound(err error) bool {
	var he *HTTPError
	return errors.As(err, &he) && he.Status == http.StatusNotFound
}

type Client struct {
	base *url.URL
	opts Options
	http *http.Client
	*limiter
}

// limiter, hız sınırı ve bekleme (cooldown) durumu; bir istemcinin tüm oturumları arasında ortaktır.
type limiter struct {
	mu       sync.Mutex
	next     time.Time
	interval time.Duration
	cooldown time.Duration // şu anki ceza süresi (0: sağlıklı)
}

func New(o Options) (*Client, error) {
	u, err := url.Parse(strings.TrimRight(o.BaseURL, "/") + "/")
	if err != nil {
		return nil, fmt.Errorf("base url: %w", err)
	}
	if o.RPS <= 0 {
		o.RPS = 1
	}
	if o.Timeout <= 0 {
		o.Timeout = 30 * time.Second
	}
	if o.Cooldown <= 0 {
		o.Cooldown = 30 * time.Second
	}
	if o.UserAgent == "" {
		o.UserAgent = "bologna-scraper/1.0 (+staj projesi)"
	}
	if o.CacheDir != "" {
		if err := os.MkdirAll(o.CacheDir, 0o755); err != nil {
			return nil, err
		}
	}
	// ASP.NET sayfaları oturum çerezi (ASP.NET_SessionId) bekleyebilir.
	jar, _ := cookiejar.New(nil)
	return &Client{
		base:    u,
		opts:    o,
		http:    &http.Client{Timeout: o.Timeout, Jar: jar},
		limiter: &limiter{interval: time.Duration(float64(time.Second) / o.RPS)},
	}, nil
}

// Session, kendi çerez kavanozu (ayrı ASP.NET oturumu) olan bir istemci döndürür; hız sınırı ve
// bekleme durumu bu istemciyle ortaktır. ASP.NET, postback'le seçilen değeri (ör. ders planı yılı)
// oturumda tutar: aynı oturumu paylaşan başka bir istek yanlış/boş sayfa alabilir.
func (c *Client) Session() *Client {
	jar, _ := cookiejar.New(nil)
	cp := *c
	cp.http = &http.Client{Timeout: c.opts.Timeout, Jar: jar, Transport: c.http.Transport}
	return &cp
}

// URL, base altındaki bir sayfa için tam adres üretir ve lang parametresini ekler.
func (c *Client) URL(page string, q url.Values) string {
	u := c.Resolve(page)
	pu, _ := url.Parse(u)
	existing := pu.Query()
	for k, v := range q {
		existing[k] = v
	}
	pu.RawQuery = existing.Encode()
	return pu.String()
}

// Resolve, sayfada bulunan göreli bir linki base'e göre çözer.
func (c *Client) Resolve(ref string) string {
	r, err := url.Parse(strings.TrimSpace(ref))
	if err != nil {
		return ref
	}
	u := c.base.ResolveReference(r)
	q := u.Query()
	if c.opts.Lang != "" && q.Get("lang") == "" {
		q.Set("lang", c.opts.Lang)
		u.RawQuery = q.Encode()
	}
	return u.String()
}

func (c *Client) wait(ctx context.Context) error {
	c.mu.Lock()
	now := time.Now()
	t := c.next
	if t.Before(now) {
		t = now
	}
	c.next = t.Add(c.interval)
	c.mu.Unlock()

	d := time.Until(t)
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (c *Client) cachePath(rawURL string) string {
	h := sha1.Sum([]byte(rawURL))
	return filepath.Join(c.opts.CacheDir, hex.EncodeToString(h[:])+".html")
}

// Get, sayfayı indirir. 4xx (429 hariç) hatalarında tekrar denemez.
func (c *Client) Get(ctx context.Context, rawURL string) ([]byte, error) {
	return c.fetch(ctx, rawURL, nil)
}

// Post, ASP.NET form gönderimi (postback) yapar; tekrar deneme ve bekleme kuralları Get ile aynıdır.
func (c *Client) Post(ctx context.Context, rawURL string, form url.Values) ([]byte, error) {
	if form == nil {
		form = url.Values{}
	}
	return c.fetch(ctx, rawURL, form)
}

func (c *Client) fetch(ctx context.Context, rawURL string, form url.Values) ([]byte, error) {
	cacheKey := rawURL
	if form != nil {
		cacheKey += "\nPOST " + form.Encode()
	}
	if c.opts.CacheDir != "" && c.opts.UseCache {
		if b, err := os.ReadFile(c.cachePath(cacheKey)); err == nil {
			return b, nil
		}
	}

	var (
		lastErr    error
		retryAfter time.Duration
	)
	for attempt := 0; attempt <= c.opts.Retries; attempt++ {
		if attempt > 0 {
			// Üstel geri çekilme (2, 4, 8 sn…) + jitter; sunucu Retry-After verdiyse ona uy.
			backoff := time.Duration(1<<uint(attempt))*time.Second + time.Duration(rand.Int64N(int64(time.Second)))
			if retryAfter > backoff {
				backoff = retryAfter
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
		}
		if err := c.wait(ctx); err != nil {
			return nil, err
		}
		body, status, ra, err := c.do(ctx, rawURL, form)
		retryAfter = ra
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			lastErr = err
			continue
		}
		if status == http.StatusOK {
			c.healthy()
			if c.opts.CacheDir != "" {
				_ = os.WriteFile(c.cachePath(cacheKey), body, 0o644)
			}
			return body, nil
		}
		lastErr = &HTTPError{URL: rawURL, Status: status}
		if status >= 400 && status < 500 && status != http.StatusTooManyRequests {
			c.healthy() // 404 vb. sunucunun sağlıklı cevabıdır
			return nil, lastErr
		}
	}
	if ctx.Err() == nil {
		c.penalize(lastErr)
	}
	return nil, lastErr
}

// penalize: sunucu kaynaklı kalıcı hata → tüm istekleri cooldown kadar durdur, cezayı büyüt.
func (c *Client) penalize(err error) {
	c.mu.Lock()
	if c.cooldown == 0 {
		c.cooldown = c.opts.Cooldown
	} else {
		c.cooldown = min(c.cooldown*2, 10*c.opts.Cooldown)
	}
	d := c.cooldown
	if until := time.Now().Add(d); until.After(c.next) {
		c.next = until
	}
	c.mu.Unlock()
	if c.opts.OnCooldown != nil {
		c.opts.OnCooldown(d, err)
	}
}

func (c *Client) healthy() {
	c.mu.Lock()
	c.cooldown = 0
	c.mu.Unlock()
}

func (c *Client) do(ctx context.Context, rawURL string, form url.Values) ([]byte, int, time.Duration, error) {
	method, body := http.MethodGet, io.Reader(nil)
	if form != nil {
		method, body = http.MethodPost, strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, body)
	if err != nil {
		return nil, 0, 0, err
	}
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Referer", rawURL)
	}
	req.Header.Set("User-Agent", c.opts.UserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.Header.Set("Accept-Language", "tr-TR,tr;q=0.9,en;q=0.5")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, 0, err
	}
	defer resp.Body.Close()
	ra := parseRetryAfter(resp.Header.Get("Retry-After"))
	out, err := io.ReadAll(io.LimitReader(resp.Body, 20<<20))
	if err != nil {
		return nil, resp.StatusCode, ra, err
	}
	if resp.StatusCode == http.StatusOK {
		out = toUTF8(out, resp.Header.Get("Content-Type"))
	}
	return out, resp.StatusCode, ra, nil
}

// toUTF8, sayfayı (Content-Type / <meta charset> / içerik tahmini ile) UTF-8'e çevirir.
// Eski ASP.NET sayfaları windows-1254 / ISO-8859-9 olabilir; parser UTF-8 bekler.
func toUTF8(body []byte, contentType string) []byte {
	enc, name, certain := charset.DetermineEncoding(body, contentType)
	if name == "utf-8" || enc == nil {
		return body
	}
	if !certain {
		// Bildirim yok. DetermineEncoding yalnızca ilk 1 KB'a bakar; OİBS sayfalarının başı saf ASCII
		// olduğundan UTF-8 sayfayı windows-1252 sanar. Tüm gövde geçerli UTF-8 ise dokunma.
		if utf8.Valid(body) {
			return body
		}
		if name == "windows-1252" { // Türkçe site için 1254 daha doğru tahmin
			enc = charmap.Windows1254
		}
	}
	out, err := enc.NewDecoder().Bytes(body)
	if err != nil {
		return body
	}
	return out
}

// parseRetryAfter, saniye veya HTTP tarihi biçimindeki Retry-After başlığını okur (en fazla 2 dk).
func parseRetryAfter(v string) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	var d time.Duration
	if secs, err := strconv.Atoi(v); err == nil {
		d = time.Duration(secs) * time.Second
	} else if t, err := http.ParseTime(v); err == nil {
		d = time.Until(t)
	}
	return min(max(d, 0), 2*time.Minute)
}
