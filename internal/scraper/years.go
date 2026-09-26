package scraper

import (
	"context"
	"fmt"
	"net/url"
	"strconv"

	"github.com/mehmetfatihvar/staj/staj/bologna-scraper/internal/fetch"
	"github.com/mehmetfatihvar/staj/staj/bologna-scraper/internal/parse"
)

// FindPlanYear, "2025" gibi bir yılı ya da doğrudan seçenek değerini sayfadaki seçeneklerle eşleştirir.
func FindPlanYear(years []parse.PlanYear, want string) (parse.PlanYear, bool) {
	for _, y := range years {
		if y.Value == want {
			return y, true
		}
	}
	if n, err := strconv.Atoi(want); err == nil {
		for _, y := range years {
			if y.Year == n {
				return y, true
			}
		}
	}
	return parse.PlanYear{}, false
}

// YearPostback, ders planı sayfasında yıl seçiciyi değiştirmeyi tarayıcı gibi taklit eder: sayfanın
// formunu (ViewState dahil) aynen geri gönderir, yalnızca yıl alanını ve olay hedefini değiştirir.
func YearPostback(ctx context.Context, c *fetch.Client, pageURL string, page []byte, value string) ([]byte, error) {
	f, err := parse.ParseForm(page)
	if err != nil {
		return nil, err
	}
	f.Fields.Set(parse.YearField, value)
	f.Fields.Set("__EVENTTARGET", parse.YearField)
	f.Fields.Set("__EVENTARGUMENT", "")
	target := pageURL
	if f.Action != "" {
		base, err := url.Parse(pageURL)
		if err != nil {
			return nil, err
		}
		ref, err := url.Parse(f.Action)
		if err != nil {
			return nil, fmt.Errorf("form action %q: %w", f.Action, err)
		}
		target = base.ResolveReference(ref).String()
	}
	return c.Post(ctx, target, f.Fields)
}
