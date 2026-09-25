package parse

import (
	"fmt"
	"io"
	"regexp"
	"strings"
	"unicode/utf8"
)

// ProgramRef, keşif (discover) aşamasında bulunan program kimliği.
type ProgramRef struct {
	ID       int64  `json:"id"` // curSunit
	Name     string `json:"name,omitempty"`
	UnitID   int64  `json:"unit_id,omitempty"` // curUnit
	UnitName string `json:"unit_name,omitempty"`
	Level    string `json:"level,omitempty"`
}

type Outcome struct {
	No          int    `json:"no"`
	Description string `json:"description"`
}

// Program, progAbout.aspx sayfasından çıkarılan bilgiler.
type Program struct {
	Name            string            `json:"name"`
	Language        string            `json:"language,omitempty"`
	Duration        *int              `json:"duration_years,omitempty"`
	MaxDuration     *int              `json:"max_duration_years,omitempty"`
	Quota           string            `json:"quota,omitempty"`
	Internship      string            `json:"internship,omitempty"`
	Head            string            `json:"head,omitempty"`
	ECTSCoordinator string            `json:"ects_coordinator,omitempty"`
	Content         string            `json:"content,omitempty"`
	History         string            `json:"history,omitempty"`
	Degree          string            `json:"degree,omitempty"`
	Admission       string            `json:"admission,omitempty"`
	Progression     string            `json:"progression,omitempty"`
	Graduation      string            `json:"graduation,omitempty"`
	Employment      string            `json:"employment,omitempty"`
	Assessment      string            `json:"assessment,omitempty"`
	Info            map[string]string `json:"info"`               // üstteki anahtar/değer tablosu, ham
	Sections        map[string]string `json:"sections"`           // tüm bölümler, ham (hiçbir şey kaybolmasın)
	Outcomes        []Outcome         `json:"outcomes,omitempty"` // içerik metninden yedek çıkarım
}

type sectionRule struct {
	pats []string
	set  func(*Program, string)
}

// Sıra önemli: ilk eşleşen kural uygulanır.
var programSectionRules = []sectionRule{
	{[]string{"program icerig"}, func(p *Program, v string) { p.Content = v }},
	{[]string{"koordinator"}, func(p *Program, v string) { p.ECTSCoordinator = v }},
	{[]string{"baskan"}, func(p *Program, v string) { p.Head = v }},
	{[]string{"tarihce"}, func(p *Program, v string) { p.History = v }},
	{[]string{"kazanilan derece"}, func(p *Program, v string) { p.Degree = v }},
	{[]string{"kabul kosul"}, func(p *Program, v string) { p.Admission = v }},
	{[]string{"ust kademe"}, func(p *Program, v string) { p.Progression = v }},
	{[]string{"mezuniyet kosul"}, func(p *Program, v string) { p.Graduation = v }},
	{[]string{"yeterlilik kosul"}, func(p *Program, v string) { p.Graduation = v }},
	{[]string{"istihdam"}, func(p *Program, v string) { p.Employment = v }},
	{[]string{"olcme"}, func(p *Program, v string) { p.Assessment = v }},
}

func isInfoTable(t *Table) bool {
	if t.Width() != 2 {
		return false
	}
	for _, kv := range t.KV() {
		n := Norm(kv.Key)
		if strings.Contains(n, "suresi") || n == "dili" {
			return true
		}
	}
	return false
}

// ParseProgramAbout, progAbout.aspx?curSunit=… sayfasını ayrıştırır.
func ParseProgramAbout(r io.Reader) (*Program, error) {
	ts, _, err := ParseTables(r)
	if err != nil {
		return nil, err
	}
	p := &Program{Info: map[string]string{}, Sections: map[string]string{}}
	for i := range ts {
		t := &ts[i]
		if p.Name == "" && t.Match("program", "bilgi") {
			p.Name = Clean(strings.SplitN(t.Name(), " - ", 2)[0])
		}
		if isInfoTable(t) {
			for _, kv := range t.KV() {
				p.Info[kv.Key] = kv.Value
				n := Norm(kv.Key)
				switch {
				case strings.Contains(n, "dili"):
					p.Language = kv.Value
				case strings.Contains(n, "azami"):
					p.MaxDuration = Int(kv.Value)
				case strings.Contains(n, "suresi"):
					p.Duration = Int(kv.Value)
				case strings.Contains(n, "kontenjan"):
					p.Quota = kv.Value
				case strings.Contains(n, "staj"):
					p.Internship = kv.Value
				}
			}
			continue
		}
		text := t.Text()
		if text == "" {
			continue
		}
		label := t.Name()
		if label == "" {
			label = fmt.Sprintf("tablo_%d", i)
		}
		p.Sections[label] = text
		n := Norm(label)
		for _, rule := range programSectionRules {
			if containsAll(n, rule.pats...) {
				rule.set(p, text)
				break
			}
		}
	}
	p.Outcomes = ExtractProgramOutcomesFromText(p.Content)
	return p, nil
}

var outcomeHeadings = []string{
	"program ogrenme ciktilari", "program ciktilari", "program yeterlilikleri", "(poc)",
}

// ExtractProgramOutcomesFromText, "Program Çıktıları (PÖÇ) 1. … 2. …" gibi serbest
// metinden numaralı maddeleri çıkarır. Yalnızca yedek kaynaktır.
func ExtractProgramOutcomesFromText(content string) []Outcome {
	if content == "" {
		return nil
	}
	folded := string(foldRunes(content))
	idx := -1
	for _, h := range outcomeHeadings {
		if i := strings.LastIndex(folded, h); i >= 0 && (idx < 0 || i > idx) {
			idx = i
		}
	}
	if idx < 0 {
		return nil
	}
	runeIdx := utf8.RuneCountInString(folded[:idx])
	return ExtractNumbered(string([]rune(content)[runeIdx:]))
}

// ExtractNumbered, 1'den başlayıp ardışık artan "N." / "N)" maddelerini ayırır.
func ExtractNumbered(s string) []Outcome {
	var out []Outcome
	pos, prevEnd, cur := 0, -1, 1
	for {
		re := regexp.MustCompile(fmt.Sprintf(`(?:^|[^\d])(%d)\s*[.)]\s*`, cur))
		m := re.FindStringSubmatchIndex(s[pos:])
		if m == nil {
			break
		}
		start, end := pos+m[2], pos+m[1]
		if prevEnd >= 0 {
			out = append(out, Outcome{No: cur - 1, Description: Clean(s[prevEnd:start])})
		}
		prevEnd, pos = end, end
		cur++
		if cur > 60 {
			break
		}
	}
	if prevEnd >= 0 {
		last := s[prevEnd:]
		if i := strings.Index(last, "\n"); i >= 0 {
			last = last[:i]
		}
		out = append(out, Outcome{No: cur - 1, Description: Clean(last)})
	}
	if len(out) < 2 { // tek madde büyük ihtimalle yanlış pozitif
		return nil
	}
	return out
}

// ParseOutcomesPage, program yeterlilikleri sayfasındaki (No | Açıklama) tabloyu okur.
func ParseOutcomesPage(r io.Reader) ([]Outcome, error) {
	ts, _, err := ParseTables(r)
	if err != nil {
		return nil, err
	}
	var best []Outcome
	for i := range ts {
		var outs []Outcome
		for _, row := range ts[i].Rows {
			if len(row.Cells) < 2 {
				continue
			}
			no, ok := OrdinalNo(row.Cell(0))
			if !ok || row.Cell(1) == "" {
				continue
			}
			outs = append(outs, Outcome{No: no, Description: Clean(row.Cell(1))})
		}
		if len(outs) > len(best) {
			best = outs
		}
	}
	return best, nil
}
