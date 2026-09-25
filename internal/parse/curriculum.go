package parse

import (
	"bytes"
	"regexp"
	"strconv"
	"strings"
)

// CurriculumEntry, ders planındaki bir satır (program ↔ ders ilişkisi).
type CurriculumEntry struct {
	CourseID int64    `json:"course_id"` // curCourse
	Semester int      `json:"semester,omitempty"`
	Code     string   `json:"code,omitempty"`
	Name     string   `json:"name,omitempty"`
	Type     string   `json:"type,omitempty"` // zorunlu | secmeli | ham değer
	Theory   *int     `json:"theory,omitempty"`
	Practice *int     `json:"practice,omitempty"`
	Credit   *float64 `json:"credit,omitempty"`
	ECTS     *float64 `json:"ects,omitempty"`
}

type Curriculum struct {
	Entries []CurriculumEntry `json:"entries"`
	// Links: ders planı tablolarındaki, ders detayı olmayan alt sayfa linkleri
	// (ör. seçmeli ders havuzları). Scraper bunları bir seviye takip eder.
	Links []string `json:"links,omitempty"`
}

var (
	reCourseID = regexp.MustCompile(`(?i)curCourse=(\d+)`)
	reSemester = regexp.MustCompile(`(\d{1,2})\s*\.?\s*(?:yariyil|donem|semester|yy)`)
	reAspx     = regexp.MustCompile(`(?i)([\w./-]+\.aspx(?:\?[^'"\s)]*)?)`)
)

func courseIDFrom(refs []string) int64 {
	for _, r := range refs {
		if m := reCourseID.FindStringSubmatch(r); m != nil {
			id, _ := strconv.ParseInt(m[1], 10, 64)
			return id
		}
	}
	return 0
}

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
	ts, _, err := ParseTables(bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	cur := &Curriculum{}
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
		col := struct{ code, name, typ, tu, t, u, credit, ects int }{
			code: colIndex(hdr, func(h string) bool { return strings.Contains(h, "kod") || strings.Contains(h, "code") }),
			name: colIndex(hdr, func(h string) bool {
				return (strings.Contains(h, "adi") || strings.HasPrefix(h, "ders") || strings.Contains(h, "name") || strings.Contains(h, "title")) && !strings.Contains(h, "kod")
			}),
			typ: colIndex(hdr, func(h string) bool {
				return h == "z/s" || strings.Contains(h, "tur") || strings.Contains(h, "zorunlu") || strings.Contains(h, "secmeli") || h == "c/e" || strings.Contains(h, "type")
			}),
			tu:     colIndex(hdr, func(h string) bool { return strings.HasPrefix(h, "t+u") }),
			t:      colIndex(hdr, func(h string) bool { return h == "t" }),
			u:      colIndex(hdr, func(h string) bool { return h == "u" }),
			credit: colIndex(hdr, func(h string) bool { return strings.Contains(h, "kredi") || strings.Contains(h, "credit") }),
			ects:   colIndex(hdr, func(h string) bool { return strings.Contains(h, "akts") || strings.Contains(h, "ects") }),
		}

		for ri, row := range t.Rows {
			if ri <= hi {
				continue
			}
			id := courseIDFrom(row.Refs)
			if id == 0 {
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
			}
			if col.tu >= 0 {
				e.Theory, e.Practice = SplitTU(row.Cell(col.tu))
			} else {
				e.Theory, e.Practice = Int(row.Cell(col.t)), Int(row.Cell(col.u))
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
