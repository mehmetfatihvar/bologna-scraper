package parse

import (
	"bytes"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// CurriculumEntry, ders planındaki bir satır (program ↔ ders ilişkisi).
//
// OİBS seçmeli dersleri iki katmanlı gösterir: bir "grup" satırı (ör. "OSD GÜZ2",
// kaç ders seçileceği "Grup Ders Adedi"nde) ve altında açılan havuz dersleri.
// Grup satırı IsGroup=true ile, havuz dersleri GroupID=<grup satırının ID'si> ile gelir.
type CurriculumEntry struct {
	CourseID   int64    `json:"course_id"` // curCourse (grup satırında grubun ID'si)
	Semester   int      `json:"semester,omitempty"`
	Code       string   `json:"code,omitempty"`
	Name       string   `json:"name,omitempty"`
	Type       string   `json:"type,omitempty"` // zorunlu | secmeli | ham değer
	Theory     *int     `json:"theory,omitempty"`
	Practice   *int     `json:"practice,omitempty"`
	Lab        *int     `json:"lab,omitempty"`
	Credit     *float64 `json:"credit,omitempty"`
	ECTS       *float64 `json:"ects,omitempty"`
	Delivery   string   `json:"delivery,omitempty"` // Öğretim Şekli: Yüz Yüze | Uzaktan …
	IsGroup    bool     `json:"is_group,omitempty"`
	GroupCount *int     `json:"group_count,omitempty"` // grup satırında: seçilecek ders adedi
	GroupID    int64    `json:"group_id,omitempty"`    // havuz dersinde: bağlı olduğu grup
}

type Curriculum struct {
	Entries []CurriculumEntry `json:"entries"`
	// Sayfa başlığından: "MÜHENDİSLİK FAKÜLTESİ / BİLGİSAYAR MÜHENDİSLİĞİ - Dersler"
	UnitName    string `json:"unit_name,omitempty"`
	ProgramName string `json:"program_name,omitempty"`
	Year        *int   `json:"year,omitempty"` // seçili ders planı yılı (cmbYillar)
	// Years: sayfadaki yıl seçicisinin (cmbYillar) tüm seçenekleri; eski planlar bunlarla istenir.
	Years []PlanYear `json:"years,omitempty"`
	// PlanID: seçili planın sitedeki kimliği (yıl seçicinin değeri). Aynı yıla ait birden çok plan
	// olabildiği için planlar yılla değil bununla ayırt edilir.
	PlanID string `json:"plan_id,omitempty"`
	// Archived: bu, sitede varsayılan olarak seçili olmayan eski bir plan (scraper doldurur);
	// programın güncel plan yılı bununla değişmez.
	Archived bool `json:"archived,omitempty"`
	// Links: ders planı tablolarındaki, ders detayı olmayan alt sayfa linkleri
	// (ör. seçmeli ders havuzları). Scraper bunları bir seviye takip eder.
	Links []string `json:"links,omitempty"`
}

var (
	// Eski şablon href="progCourseDetails.aspx?curCourse=N", güncel şablon onclick="prolizOpenCourseDetails(N)".
	reCourseID  = regexp.MustCompile(`(?i)(?:curCourse=|prolizOpenCourseDetails\(\s*['"]?)(\d+)`)
	reGroupID   = regexp.MustCompile(`(?i)toggleRow\(\s*this\s*,\s*['"]?(\d+)`)
	reGroupRow  = regexp.MustCompile(`(?i)\bcollapse_(\d+)\b`)
	reSemester  = regexp.MustCompile(`(\d{1,2})\s*\.?\s*(?:yariyil|donem|semester|yy)`)
	reAspx      = regexp.MustCompile(`(?i)([\w./-]+\.aspx(?:\?[^'"\s)]*)?)`)
	reHeaderTag = regexp.MustCompile(`(?i)\s*-\s*(?:dersler|courses)\s*$`)
)

func firstMatchID(re *regexp.Regexp, refs ...string) int64 {
	for _, r := range refs {
		if m := re.FindStringSubmatch(r); m != nil {
			id, _ := strconv.ParseInt(m[1], 10, 64)
			return id
		}
	}
	return 0
}

func courseIDFrom(refs []string) int64 { return firstMatchID(reCourseID, refs...) }

// PageLinks, href/onclick değerlerinden .aspx linklerini çıkarır.
func PageLinks(refs []string) []string {
	var out []string
	for _, r := range refs {
		for _, m := range reAspx.FindAllStringSubmatch(r, -1) {
			out = append(out, m[1])
		}
	}
	return out
}

func normType(s string) string {
	n := Norm(s)
	switch {
	case n == "":
		return ""
	case strings.HasPrefix(n, "z"):
		return "zorunlu"
	case strings.HasPrefix(n, "s") || strings.HasPrefix(n, "e"): // seçmeli / elective
		return "secmeli"
	case strings.HasPrefix(n, "c"): // compulsory
		return "zorunlu"
	}
	return Clean(s)
}

// ParseCurriculum, progCourses.aspx?curSunit=… sayfasını ayrıştırır.
func ParseCurriculum(body []byte) (*Curriculum, error) {
	ts, doc, err := ParseTables(bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	cur := &Curriculum{}
	parseCurriculumHeader(doc, cur)
	seen := map[int64]int{} // courseID -> index
	linkSeen := map[string]bool{}
	lastSem := 0

	for i := range ts {
		t := &ts[i]
		if m := reSemester.FindStringSubmatch(Norm(t.Label + " " + t.Title)); m != nil {
			lastSem, _ = strconv.Atoi(m[1])
		}
		hi, hdr := t.FindHeader("akts", "ects", "kod", "code")
		if hi < 0 {
			continue
		}
		col := struct{ code, name, typ, tu, t, u, credit, ects, group, delivery int }{
			code: colIndex(hdr, func(h string) bool { return strings.Contains(h, "kod") || strings.Contains(h, "code") }),
			name: colIndex(hdr, func(h string) bool {
				return (strings.Contains(h, "adi") || strings.HasPrefix(h, "ders") || strings.Contains(h, "name") || strings.Contains(h, "title")) && !strings.Contains(h, "kod")
			}),
			typ: colIndex(hdr, func(h string) bool {
				return h == "z/s" || strings.Contains(h, "tur") || strings.Contains(h, "zorunlu") || strings.Contains(h, "secmeli") || h == "c/e" || strings.Contains(h, "type")
			}),
			tu:       colIndex(hdr, func(h string) bool { return strings.HasPrefix(h, "t+u") }),
			t:        colIndex(hdr, func(h string) bool { return h == "t" }),
			u:        colIndex(hdr, func(h string) bool { return h == "u" }),
			credit:   colIndex(hdr, func(h string) bool { return strings.Contains(h, "kredi") || strings.Contains(h, "credit") }),
			ects:     colIndex(hdr, func(h string) bool { return strings.Contains(h, "akts") || strings.Contains(h, "ects") }),
			group:    colIndex(hdr, func(h string) bool { return strings.Contains(h, "grup") || strings.Contains(h, "group") }),
			delivery: colIndex(hdr, func(h string) bool { return strings.Contains(h, "ogretim sekli") || strings.Contains(h, "delivery") }),
		}

		for ri, row := range t.Rows {
			if ri <= hi {
				// Başlık satırından önceki "1.Yarıyıl Ders Planı" satırı ilk yarıyılı belirler.
				if m := reSemester.FindStringSubmatch(Norm(strings.Join(row.Texts(), " "))); m != nil {
					lastSem, _ = strconv.Atoi(m[1])
				}
				continue
			}
			id := courseIDFrom(row.Refs)
			groupID := firstMatchID(reGroupID, row.Refs...)
			if id == 0 && groupID != 0 {
				id = groupID // seçmeli grup satırı: detay sayfası yok, ID'si havuzu bağlar
			}
			if id == 0 {
				// Güncel şablonda yarıyıl başlıkları tablonun içinde ayrı satırdır: "1.Yarıyıl Ders Planı"
				if m := reSemester.FindStringSubmatch(Norm(strings.Join(row.Texts(), " "))); m != nil {
					lastSem, _ = strconv.Atoi(m[1])
					continue
				}
				for _, l := range PageLinks(row.Refs) {
					ln := strings.ToLower(l)
					if strings.Contains(ln, "progcoursedetails") || strings.HasPrefix(ln, "index.aspx") || linkSeen[l] {
						continue
					}
					linkSeen[l] = true
					cur.Links = append(cur.Links, l)
				}
				continue
			}
			e := CurriculumEntry{
				CourseID: id,
				Semester: lastSem,
				Code:     Clean(row.Cell(col.code)),
				Name:     Clean(row.Cell(col.name)),
				Type:     normType(row.Cell(col.typ)),
				Credit:   Float(row.Cell(col.credit)),
				ECTS:     Float(row.Cell(col.ects)),
				Delivery: Clean(row.Cell(col.delivery)),
			}
			if col.tu >= 0 {
				e.Theory, e.Practice, e.Lab = SplitTUL(row.Cell(col.tu))
			} else {
				e.Theory, e.Practice = Int(row.Cell(col.t)), Int(row.Cell(col.u))
			}
			if groupID != 0 && courseIDFrom(row.Refs) == 0 {
				e.IsGroup = true
				e.GroupCount = Int(row.Cell(col.group))
			}
			if m := reGroupRow.FindStringSubmatch(row.Class); m != nil {
				e.GroupID, _ = strconv.ParseInt(m[1], 10, 64)
				if e.Type == "" {
					e.Type = "secmeli"
				}
			}
			if idx, ok := seen[id]; ok {
				if cur.Entries[idx].Semester == 0 {
					cur.Entries[idx].Semester = e.Semester
				}
				continue
			}
			seen[id] = len(cur.Entries)
			cur.Entries = append(cur.Entries, e)
		}
	}

	// Güvenlik ağı: tablolarda yakalanmayan ders linkleri de kaybolmasın.
	for _, m := range reCourseID.FindAllSubmatch(body, -1) {
		id, _ := strconv.ParseInt(string(m[1]), 10, 64)
		if _, ok := seen[id]; !ok && id != 0 {
			seen[id] = len(cur.Entries)
			cur.Entries = append(cur.Entries, CurriculumEntry{CourseID: id})
		}
	}
	return cur, nil
}

// PlanYear, ders planı yıl seçicisinin bir seçeneği ("2025 (2025 - BİLGİSAYAR MÜHENDİSLİĞİ)").
type PlanYear struct {
	Value    string `json:"value"` // form değeri (sitenin plan kimliği)
	Year     int    `json:"year"`
	Label    string `json:"label"`
	Selected bool   `json:"selected,omitempty"`
}

// parseCurriculumHeader, "FAKÜLTE / PROGRAM - Dersler" başlığını ve seçili plan yılını okur.
func parseCurriculumHeader(doc *html.Node, cur *Curriculum) {
	walkNodes(doc, func(n *html.Node) bool {
		if n.Type != html.ElementNode {
			return true
		}
		switch {
		case n.DataAtom == atom.Span && strings.EqualFold(attr(n, "id"), "lblProgInfoDers"):
			h := reHeaderTag.ReplaceAllString(Clean(textOf(n)), "")
			if unit, prog, ok := strings.Cut(h, " / "); ok {
				cur.UnitName, cur.ProgramName = Clean(unit), Clean(prog)
			} else {
				cur.ProgramName = h
			}
			return false
		case n.DataAtom == atom.Select && strings.EqualFold(attr(n, "name"), YearField):
			for o := n.FirstChild; o != nil; o = o.NextSibling {
				if o.Type != html.ElementNode || o.DataAtom != atom.Option {
					continue
				}
				py := PlanYear{Value: attr(o, "value"), Label: Clean(textOf(o)), Selected: hasAttr(o, "selected")}
				if y := Int(py.Label); y != nil && *y > 1900 {
					py.Year = *y
				}
				if py.Value == "" || py.Year == 0 {
					continue
				}
				cur.Years = append(cur.Years, py)
				if py.Selected {
					y := py.Year
					cur.Year, cur.PlanID = &y, py.Value
				}
			}
			return false
		case n.DataAtom == atom.Option && attr(n, "selected") != "" && cur.Year == nil:
			if y := Int(textOf(n)); y != nil && *y > 1900 {
				cur.Year = y
			}
			return false
		}
		return true
	})
}
