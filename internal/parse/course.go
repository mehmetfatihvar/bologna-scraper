package parse

import (
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
)

type LearningOutcome struct {
	No          int    `json:"no"`
	Description string `json:"description"`
	Extra       string `json:"extra,omitempty"` // öğretim/ölçme yöntemleri vb. ek sütunlar
}

type WeeklyTopic struct {
	Week        *int   `json:"week,omitempty"`
	Topic       string `json:"topic"`
	Preparation string `json:"preparation,omitempty"`
	Documents   string `json:"documents,omitempty"`
}

type Resource struct {
	Kind    string `json:"kind,omitempty"`
	Content string `json:"content"`
}

type Assessment struct {
	Activity   string   `json:"activity"`
	Count      *int     `json:"count,omitempty"`
	Percentage *float64 `json:"percentage,omitempty"`
}

type Workload struct {
	Activity string   `json:"activity"`
	Count    *float64 `json:"count,omitempty"`
	Hours    *float64 `json:"hours,omitempty"`
	Total    *float64 `json:"total,omitempty"`
}

// OutcomeContribution: dersin, program çıktısı No'ya katkı düzeyi (genelde 1-5).
type OutcomeContribution struct {
	No          int    `json:"no"`
	Description string `json:"description,omitempty"`
	Level       *int   `json:"level,omitempty"`
}

// MatrixCell: dersin LO no'lu öğrenme çıktısının PO no'lu program çıktısına katkısı
// (gerçek sayfadaki "Ö1 × P1" matrisi; boş hücreler yazılmaz).
type MatrixCell struct {
	LO    int `json:"lo"`
	PO    int `json:"po"`
	Level int `json:"level"`
}

type CourseDetail struct {
	Semester      *int     `json:"semester,omitempty"`
	Code          string   `json:"code,omitempty"`
	Name          string   `json:"name,omitempty"`
	Theory        *int     `json:"theory,omitempty"`
	Practice      *int     `json:"practice,omitempty"`
	Credit        *float64 `json:"credit,omitempty"`
	ECTS          *float64 `json:"ects,omitempty"`
	Language      string   `json:"language,omitempty"`
	Level         string   `json:"level,omitempty"`
	Department    string   `json:"department,omitempty"`
	DeliveryMode  string   `json:"delivery_mode,omitempty"` // Öğrenim Türü: Örgün Öğretim …
	TeachingMode  string   `json:"teaching_mode,omitempty"` // Dersin Öğretim Şekli: Yüz Yüze | Uzaktan …
	Type          string   `json:"type,omitempty"`
	Objective     string   `json:"objective,omitempty"`
	Content       string   `json:"content,omitempty"`
	Prerequisites string   `json:"prerequisites,omitempty"`
	Coordinator   string   `json:"coordinator,omitempty"`
	Instructors   string   `json:"instructors,omitempty"`
	Assistants    string   `json:"assistants,omitempty"`

	Details          map[string]string     `json:"details"` // tüm anahtar/değerler + tanınmayan tablolar
	LearningOutcomes []LearningOutcome     `json:"learning_outcomes,omitempty"`
	WeeklyTopics     []WeeklyTopic         `json:"weekly_topics,omitempty"`
	Resources        []Resource            `json:"resources,omitempty"`
	Assessments      []Assessment          `json:"assessments,omitempty"`
	Workloads        []Workload            `json:"workloads,omitempty"`
	ProgramOutcomes  []OutcomeContribution `json:"program_outcomes,omitempty"`
	OutcomeMatrix    []MatrixCell          `json:"outcome_matrix,omitempty"`

	// SourceCourseID: içerik başka bir kopyanın sayfasından alındıysa o dersin ID'si (scraper doldurur).
	SourceCourseID int64 `json:"source_course_id,omitempty"`
	// Empty: sitede ders için içerik girilmemiş ("Dersin Ayrıntıları: Kayıt Yok...").
	Empty bool `json:"empty,omitempty"`
}

// AsCopyOf, aynı kodlu başka bir programın kopyası için içeriği paylaşan bir detay döndürür: ders içeriği
// aynen alınır; programa özel olan katkı matrisi ve "Bölümü / Programı" bilgisi alınmaz.
func (c *CourseDetail) AsCopyOf(sourceID int64) *CourseDetail {
	cp := *c
	cp.ProgramOutcomes, cp.OutcomeMatrix, cp.Department = nil, nil, ""
	cp.Details = make(map[string]string, len(c.Details))
	for k, v := range c.Details {
		if Norm(k) != "bolumu / programi" {
			cp.Details[k] = v
		}
	}
	cp.SourceCourseID = sourceID
	return &cp
}

var courseDetailRules = []sectionRule2{
	{[]string{"ogretim sekli"}, func(c *CourseDetail, v string) { c.TeachingMode = v }},
	{[]string{"ogrenim tur"}, func(c *CourseDetail, v string) { c.DeliveryMode = v }},
	{[]string{"dersin tur"}, func(c *CourseDetail, v string) { c.Type = v }},
	{[]string{"dili"}, func(c *CourseDetail, v string) { c.Language = v }},
	{[]string{"duzey"}, func(c *CourseDetail, v string) { c.Level = v }},
	{[]string{"bolum"}, func(c *CourseDetail, v string) { c.Department = v }},
	{[]string{"amac"}, func(c *CourseDetail, v string) { c.Objective = v }},
	{[]string{"icerig"}, func(c *CourseDetail, v string) { c.Content = v }},
	{[]string{"on kosul"}, func(c *CourseDetail, v string) { c.Prerequisites = v }},
	{[]string{"on sart"}, func(c *CourseDetail, v string) { c.Prerequisites = v }},
	{[]string{"koordinator"}, func(c *CourseDetail, v string) { c.Coordinator = v }},
	{[]string{"yardimci"}, func(c *CourseDetail, v string) { c.Assistants = v }},
	{[]string{"veren"}, func(c *CourseDetail, v string) { c.Instructors = v }},
	{[]string{"ogretim eleman"}, func(c *CourseDetail, v string) { c.Instructors = v }},
}

type sectionRule2 struct {
	pats []string
	set  func(*CourseDetail, string)
}

// ParseCourseDetail, progCourseDetails.aspx?curCourse=… sayfasını ayrıştırır.
func ParseCourseDetail(r io.Reader) (*CourseDetail, error) {
	ts, _, err := ParseTables(r)
	if err != nil {
		return nil, err
	}
	c := &CourseDetail{Details: map[string]string{}}
	summaryDone := false

	for i := range ts {
		t := &ts[i]
		n := Norm(t.Label + " " + t.Title)

		switch {
		// 1) Özet: Yarıyıl | Kodu | Adı | T+U | Kredi | AKTS
		case !summaryDone && hasSummaryHeader(t):
			parseSummary(t, c)
			summaryDone = true

		// 2) Program çıktılarıyla ilişki matrisi (öğrenme çıktılarından ÖNCE kontrol edilmeli)
		case strings.Contains(n, "program") && (strings.Contains(n, "cikti") || strings.Contains(n, "yeterlil") || strings.Contains(n, "katki") || strings.Contains(n, "iliski")):
			ocs, cells := parseMatrix(t)
			c.ProgramOutcomes = append(c.ProgramOutcomes, ocs...)
			c.OutcomeMatrix = append(c.OutcomeMatrix, cells...)

		// 3) Dersin öğrenme çıktıları
		case strings.Contains(n, "ogrenme cikti") || strings.Contains(n, "kazanim"):
			c.LearningOutcomes = append(c.LearningOutcomes, parseLearningOutcomes(t)...)

		// 4) Haftalık konular
		case strings.Contains(n, "haftalik") || strings.Contains(n, "konular") || strings.Contains(n, "ders akisi"):
			c.WeeklyTopics = append(c.WeeklyTopics, parseWeekly(t)...)

		// 5) İş yükü (değerlendirmeden önce: "AKTS İş Yükü Tablosu" / gerçek sayfada "AKTS Hesaplama İçeriği")
		case strings.Contains(n, "is yuku") || strings.Contains(n, "isyuku") || strings.Contains(n, "akts hesap"):
			c.Workloads = append(c.Workloads, parseWorkload(t)...)

		// 6) Değerlendirme sistemi
		case strings.Contains(n, "degerlendirme"):
			c.Assessments = append(c.Assessments, parseAssessment(t)...)

		// 7) Kaynaklar
		case strings.Contains(n, "kaynak"):
			c.Resources = append(c.Resources, parseResources(t)...)

		// 8) Anahtar/değer detay tablosu
		case strings.Contains(n, "detay") || (t.Width() == 2 && looksLikeKV(t)):
			for _, kv := range t.KV() {
				c.Details[kv.Key] = kv.Value
				kn := Norm(kv.Key)
				for _, rule := range courseDetailRules {
					if containsAll(kn, rule.pats...) {
						rule.set(c, kv.Value)
						break
					}
				}
			}

		// Tanınmayan tablolar ham metin olarak saklanır; veri kaybı olmaz.
		default:
			if txt := t.Text(); txt != "" {
				key := t.Name()
				if key == "" {
					key = fmt.Sprintf("tablo_%d", i)
				}
				c.Details["tablo: "+key] = txt
			}
		}
	}
	markEmpty(c)
	return c, nil
}

// markEmpty, içerik girilmemiş sayfaları işaretler. OİBS bu durumda yalnızca "Kayıt Yok..." yazan bir
// tablo (ve kopyalarda boş katkılı P1…Pn listesi) gösterir; bu yer tutucu metin Details'e yazılmaz.
func markEmpty(c *CourseDetail) {
	if c.Code != "" || c.Name != "" || len(c.LearningOutcomes) > 0 || len(c.WeeklyTopics) > 0 ||
		len(c.Assessments) > 0 || len(c.Workloads) > 0 || len(c.Resources) > 0 || len(c.OutcomeMatrix) > 0 {
		return
	}
	for _, oc := range c.ProgramOutcomes {
		if oc.Level != nil {
			return
		}
	}
	placeholder := false
	for k, v := range c.Details {
		switch nv := Norm(v); {
		case strings.HasPrefix(nv, "kayit yok"):
			placeholder = true
			delete(c.Details, k)
		case nv != "":
			return // tanınmayan ama dolu bir alan var
		}
	}
	c.Empty = placeholder
}

func hasSummaryHeader(t *Table) bool {
	hi, hdr := t.FindHeader("akts", "ects")
	if hi < 0 {
		return false
	}
	return colIndex(hdr, func(h string) bool { return strings.Contains(h, "kod") || strings.Contains(h, "code") }) >= 0
}

func parseSummary(t *Table, c *CourseDetail) {
	hi, hdr := t.FindHeader("akts", "ects")
	if hi+1 >= len(t.Rows) {
		return
	}
	row := t.Rows[hi+1]
	get := func(pred func(string) bool) string { return row.Cell(colIndex(hdr, pred)) }
	c.Semester = Int(get(func(h string) bool {
		return strings.Contains(h, "yariyil") || strings.Contains(h, "semester") || strings.Contains(h, "donem")
	}))
	c.Code = Clean(get(func(h string) bool { return strings.Contains(h, "kod") || strings.Contains(h, "code") }))
	c.Name = Clean(get(func(h string) bool {
		return (strings.Contains(h, "adi") || strings.Contains(h, "name") || strings.Contains(h, "title")) && !strings.Contains(h, "kod")
	}))
	c.Theory, c.Practice = SplitTU(get(func(h string) bool { return strings.HasPrefix(h, "t+u") }))
	c.Credit = Float(get(func(h string) bool { return strings.Contains(h, "kredi") || strings.Contains(h, "credit") }))
	c.ECTS = Float(get(func(h string) bool { return strings.Contains(h, "akts") || strings.Contains(h, "ects") }))
	// Tanınsın tanınmasın özet satırının tümü (ör. "Son Güncelleme Tarihi") ham olarak da saklanır.
	for i, h := range t.Rows[hi].Cells {
		if k := Clean(h.Text); k != "" && row.Cell(i) != "" {
			c.Details[k] = Clean(row.Cell(i))
		}
	}
}

func looksLikeKV(t *Table) bool {
	for _, kv := range t.KV() {
		if strings.HasPrefix(Norm(kv.Key), "dersin") {
			return true
		}
	}
	return false
}

func parseLearningOutcomes(t *Table) []LearningOutcome {
	var out []LearningOutcome
	for _, r := range t.Rows {
		if len(r.Cells) < 2 {
			continue
		}
		no, ok := OrdinalNo(r.Cell(0))
		if !ok || r.Cell(1) == "" {
			continue
		}
		lo := LearningOutcome{No: no, Description: Clean(r.Cell(1))}
		if len(r.Cells) > 2 {
			lo.Extra = strings.Join(r.Texts()[2:], " | ")
		}
		out = append(out, lo)
	}
	return out
}

func parseWeekly(t *Table) []WeeklyTopic {
	_, hdr := t.FindHeader("hafta", "konu", "week", "topic")
	prepCol := colIndex(hdr, func(h string) bool { return strings.Contains(h, "hazirlik") || strings.Contains(h, "preparation") })
	docCol := colIndex(hdr, func(h string) bool { return strings.Contains(h, "dokuman") || strings.Contains(h, "document") })
	var out []WeeklyTopic
	for _, r := range t.Rows {
		if r.IsHeader() || len(r.Cells) < 2 {
			continue
		}
		wk := Int(r.Cell(0))
		if wk == nil { // "Hafta" başlık satırı vb.
			continue
		}
		w := WeeklyTopic{Week: wk, Topic: Clean(r.Cell(1))}
		if prepCol >= 0 || docCol >= 0 {
			w.Preparation, w.Documents = Clean(r.Cell(prepCol)), Clean(r.Cell(docCol))
		} else if len(r.Cells) > 2 {
			w.Preparation = joinNonEmpty(r.Texts()[2:], " | ")
		}
		if w.Topic != "" || w.Preparation != "" || w.Documents != "" {
			out = append(out, w)
		}
	}
	return out
}

func parseResources(t *Table) []Resource {
	var out []Resource
	for _, r := range t.Rows {
		if r.IsHeader() {
			continue
		}
		switch len(r.Cells) {
		case 0:
		case 1:
			if r.Cell(0) != "" {
				out = append(out, Resource{Content: r.Cell(0)})
			}
		default:
			if content := strings.Join(r.Texts()[1:], "\n"); strings.TrimSpace(content) != "" {
				out = append(out, Resource{Kind: Clean(r.Cell(0)), Content: content})
			}
		}
	}
	return out
}

func isTotalRow(s string) bool {
	n := Norm(s)
	return strings.HasPrefix(n, "toplam") || strings.HasPrefix(n, "total")
}

func parseAssessment(t *Table) []Assessment {
	hi, hdr := t.FindHeader("katki", "yuzde", "%", "sayi", "adet")
	countCol := colIndex(hdr, func(h string) bool { return strings.Contains(h, "sayi") || strings.Contains(h, "adet") })
	pctCol := colIndex(hdr, func(h string) bool {
		return strings.Contains(h, "katki") || strings.Contains(h, "yuzde") || strings.Contains(h, "%") || strings.Contains(h, "agirlik")
	})
	var out []Assessment
	for ri, r := range t.Rows {
		if ri <= hi || len(r.Cells) < 2 || r.Cell(0) == "" || isTotalRow(r.Cell(0)) {
			continue
		}
		a := Assessment{Activity: Clean(r.Cell(0))}
		if countCol >= 0 {
			a.Count = Int(r.Cell(countCol))
		}
		if pctCol >= 0 {
			a.Percentage = Float(r.Cell(pctCol))
		} else {
			a.Percentage = Float(r.Cell(len(r.Cells) - 1))
		}
		if a.Count == nil && a.Percentage == nil {
			continue
		}
		out = append(out, a)
	}
	return out
}

func parseWorkload(t *Table) []Workload {
	hi, hdr := t.FindHeader("sure", "sayi", "toplam", "saat")
	countCol := colIndex(hdr, func(h string) bool { return strings.Contains(h, "sayi") || strings.Contains(h, "adet") })
	hoursCol := colIndex(hdr, func(h string) bool { return strings.Contains(h, "sure") || strings.Contains(h, "saat") })
	totalCol := colIndex(hdr, func(h string) bool { return strings.Contains(h, "toplam") })
	var out []Workload
	for ri, r := range t.Rows {
		if ri <= hi || len(r.Cells) < 2 || r.Cell(0) == "" || isTotalRow(r.Cell(0)) {
			continue
		}
		w := Workload{Activity: Clean(r.Cell(0))}
		if countCol >= 0 {
			w.Count = Float(r.Cell(countCol))
		}
		if hoursCol >= 0 {
			w.Hours = Float(r.Cell(hoursCol))
		}
		if totalCol >= 0 {
			w.Total = Float(r.Cell(totalCol))
		}
		if w.Count == nil && w.Hours == nil && w.Total == nil {
			continue
		}
		out = append(out, w)
	}
	return out
}

// parseMatrix üç düzeni destekler:
//
//	(a) No | Program Çıktısı | Katkı Düzeyi (sayı)
//	(b) No | Program Çıktısı | 1 | 2 | 3 | 4 | 5   (işaretli sütun = düzey)
//	(c) yatay: başlık PÇ1 | PÇ2 | … , altında düzey satır(lar)ı (ör. ÖÇ×PÇ matrisi);
//	    birden çok satır varsa her PÇ için en yüksek düzey alınır.
func parseMatrix(t *Table) ([]OutcomeContribution, []MatrixCell) {
	if out, cells, ok := parseWideMatrix(t); ok {
		return out, cells
	}
	hi, hdr := t.FindHeader("katki", "duzey", "no", "cikti", "1", "5")
	levelCols := map[int]int{} // sütun -> düzey
	for i, h := range hdr {
		if len(h) == 1 && h[0] >= '0' && h[0] <= '5' {
			levelCols[i] = int(h[0] - '0')
		}
	}
	var out []OutcomeContribution
	for ri, r := range t.Rows {
		if (hi >= 0 && ri <= hi) || len(r.Cells) < 2 {
			continue
		}
		no, ok := OrdinalNo(r.Cell(0))
		if !ok {
			continue
		}
		oc := OutcomeContribution{No: no}
		if _, isNum := OrdinalNo(r.Cell(1)); !isNum || len(r.Cells) > 2 {
			if _, lc := levelCols[1]; !lc {
				oc.Description = Clean(r.Cell(1))
			}
		}
		if len(levelCols) >= 3 {
			for col, lvl := range levelCols { // birden çok işaret varsa en yükseği (deterministik)
				if strings.TrimSpace(r.Cell(col)) != "" && (oc.Level == nil || lvl > *oc.Level) {
					l := lvl
					oc.Level = &l
				}
			}
		} else {
			for j := len(r.Cells) - 1; j >= 1; j-- {
				if v, ok := OrdinalNo(r.Cell(j)); ok && v >= 0 && v <= 5 {
					oc.Level = &v
					break
				}
				if r.Cell(j) != "" && j > 1 { // son dolu hücre sayı değilse düzey yok
					break
				}
			}
			if oc.Level != nil && oc.Description != "" && len(r.Cells) == 2 {
				oc.Description = "" // 2 sütunlu No|Düzey tablosu
			}
		}
		out = append(out, oc)
	}
	return out, nil
}

// reOutcomeHeader: "PÇ1", "PÖÇ 2", "PO3", "P.Ç.4" gibi yatay matris başlıkları.
var reOutcomeHeader = regexp.MustCompile(`^p[a-z.]{0,4}\s*(\d{1,2})$`)

func parseWideMatrix(t *Table) ([]OutcomeContribution, []MatrixCell, bool) {
	for hi, r := range t.Rows {
		if hi > 3 {
			break
		}
		cols := map[int]int{} // sütun -> PÇ no
		for j, c := range r.Cells {
			if m := reOutcomeHeader.FindStringSubmatch(Norm(c.Text)); m != nil {
				n, _ := strconv.Atoi(m[1])
				cols[j] = n
			}
		}
		if len(cols) < 2 {
			continue
		}
		best := map[int]*int{}
		var order []int
		for j := 0; j < len(r.Cells); j++ {
			if n, ok := cols[j]; ok {
				if _, dup := best[n]; !dup {
					best[n] = nil
					order = append(order, n)
				}
			}
		}
		// Başlık satırı ile veri satırlarının hücre sayısı farklıysa (ör. köşe hücresi yok) sağdan hizala.
		var cells []MatrixCell
		for _, row := range t.Rows[hi+1:] {
			shift := len(row.Cells) - len(r.Cells)
			lo, hasLO := 0, false
			if shift >= 0 && len(row.Cells) > 0 {
				lo, hasLO = OrdinalNo(row.Cell(0)) // "Ö1", "ÖÇ 2"
			}
			for j := 0; j < len(r.Cells); j++ { // sütun sırasıyla (map sırası rastgele)
				n, ok := cols[j]
				if !ok {
					continue
				}
				v := Int(row.Cell(j + shift))
				if v == nil || *v > 5 {
					continue
				}
				if hasLO {
					cells = append(cells, MatrixCell{LO: lo, PO: n, Level: *v})
				}
				if best[n] == nil || *v > *best[n] {
					best[n] = v
				}
			}
		}
		out := make([]OutcomeContribution, 0, len(order))
		for _, n := range order {
			out = append(out, OutcomeContribution{No: n, Level: best[n]})
		}
		return out, cells, true
	}
	return nil, nil, false
}

func joinNonEmpty(xs []string, sep string) string {
	var out []string
	for _, x := range xs {
		if x = Clean(x); x != "" {
			out = append(out, x)
		}
	}
	return strings.Join(out, sep)
}
