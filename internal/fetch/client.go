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
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
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
	base     *url.URL
	opts     Options
	http     *http.Client
	mu       sync.Mutex
	next     time.Time
	interval time.Duration
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
	if o.UserAgent == "" {
		o.UserAgent = "bologna-scraper/1.0 (+staj projesi)"
	}
	if o.CacheDir != "" {
		if err := os.MkdirAll(o.CacheDir, 0o755); err != nil {
			return nil, err
		}
	}
	return &Client{
		base:     u,
		opts:     o,
		http:     &http.Client{Timeout: o.Timeout},
		interval: time.Duration(float64(time.Second) / o.RPS),
	}, nil
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
	if c.opts.CacheDir != "" && c.opts.UseCache {
		if b, err := os.ReadFile(c.cachePath(rawURL)); err == nil {
			return b, nil
		}
	}

	var lastErr error
	for attempt := 0; attempt <= c.opts.Retries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(1<<uint(attempt-1)) * time.Second
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
		}
		if err := c.wait(ctx); err != nil {
			return nil, err
		}
		body, status, err := c.do(ctx, rawURL)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			lastErr = err
			continue
		}
		if status == http.StatusOK {
			if c.opts.CacheDir != "" {
				_ = os.WriteFile(c.cachePath(rawURL), body, 0o644)
			}
			return body, nil
		}
		lastErr = &HTTPError{URL: rawURL, Status: status}
		if status >= 400 && status < 500 && status != http.StatusTooManyRequests {
			return nil, lastErr
		}
	}
	return nil, lastErr
}

func (c *Client) do(ctx context.Context, rawURL string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("User-Agent", c.opts.UserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.Header.Set("Accept-Language", "tr-TR,tr;q=0.9,en;q=0.5")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 20<<20))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return body, resp.StatusCode, nil
}
