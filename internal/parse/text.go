package parse

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// foldRune, Türkçe karakterleri ASCII karşılığına indirger ve küçük harfe çevirir.
// Rune başına 1:1 eşleme yapar; böylece katlanmış metindeki indeksler orijinale
// rune bazında karşılık gelir.
func foldRune(r rune) rune {
	switch r {
	case 'ı', 'I', 'İ', 'î', 'Î':
		return 'i'
	case 'ş', 'Ş':
		return 's'
	case 'ğ', 'Ğ':
		return 'g'
	case 'ü', 'Ü', 'û', 'Û':
		return 'u'
	case 'ö', 'Ö':
		return 'o'
	case 'ç', 'Ç':
		return 'c'
	case 'â', 'Â':
		return 'a'
	case '\u00a0':
		return ' '
	}
	return unicode.ToLower(r)
}

func foldRunes(s string) []rune {
	rs := []rune(s)
	for i, r := range rs {
		rs[i] = foldRune(r)
	}
	return rs
}

// Norm, karşılaştırma için metni katlar ve boşlukları tekler.
// "Dersin Öğrenme Çıktıları" -> "dersin ogrenme ciktilari"
func Norm(s string) string {
	return strings.Join(strings.Fields(string(foldRunes(s))), " ")
}

// Clean, tek satırlık temiz metin döndürür.
func Clean(s string) string {
	s = strings.ReplaceAll(s, "\u00a0", " ")
	return strings.Join(strings.Fields(s), " ")
}

// CleanLines, satır yapısını korur ama her satırı temizler ve boş satırları atar.
func CleanLines(s string) string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if l = Clean(l); l != "" {
			out = append(out, l)
		}
	}
	return strings.Join(out, "\n")
}

func containsAll(norm string, pats ...string) bool {
	for _, p := range pats {
		if !strings.Contains(norm, p) {
			return false
		}
	}
	return true
}

var (
	reInt     = regexp.MustCompile(`\d+`)
	reNum     = regexp.MustCompile(`\d+(?:[.,]\d+)?`)
	reOnlyNum = regexp.MustCompile(`^\D{0,6}?(\d{1,3})\s*[.):]?$`)
)

// Int, metindeki ilk tam sayıyı döndürür.
func Int(s string) *int {
	m := reInt.FindString(s)
	if m == "" {
		return nil
	}
	v, err := strconv.Atoi(m)
	if err != nil {
		return nil
	}
	return &v
}

// Float, metindeki ilk sayıyı ("%40", "7,5") döndürür.
func Float(s string) *float64 {
	m := reNum.FindString(s)
	if m == "" {
		return nil
	}
	v, err := strconv.ParseFloat(strings.ReplaceAll(m, ",", "."), 64)
	if err != nil {
		return nil
	}
	return &v
}

// OrdinalNo, "1", "1.", "PÇ1", "ÖÇ 3" gibi numara hücrelerini tanır.
func OrdinalNo(s string) (int, bool) {
	m := reOnlyNum.FindStringSubmatch(Clean(s))
	if m == nil {
		return 0, false
	}
	v, err := strconv.Atoi(m[1])
	return v, err == nil
}

// SplitTU, "3+0" veya "2+2+0" biçimindeki teorik/uygulama saatlerini ayırır.
func SplitTU(s string) (t, u *int) {
	t, u, _ = SplitTUL(s)
	return t, u
}

// SplitTUL, "3+2+0" (teorik+uygulama+laboratuvar) biçimini ayırır; lab yoksa nil döner.
func SplitTUL(s string) (t, u, l *int) {
	parts := strings.Split(s, "+")
	if len(parts) < 2 {
		return nil, nil, nil
	}
	t, u = Int(parts[0]), Int(parts[1])
	if len(parts) > 2 {
		l = Int(parts[2])
	}
	return t, u, l
}
