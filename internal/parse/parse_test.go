package parse

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestNorm(t *testing.T) {
	if got := Norm("  Dersin ÖĞRENME  Çıktıları "); got != "dersin ogrenme ciktilari" {
		t.Fatalf("Norm = %q", got)
	}
}

func TestProgramAbout(t *testing.T) {
	p, err := ParseProgramAbout(bytes.NewReader(fixture(t, "progAbout.aspx")))
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "EBELİK" || p.Language != "Türkçe" || *p.Duration != 4 || *p.MaxDuration != 7 {
		t.Fatalf("temel alanlar hatalı: %+v", p)
	}
	if p.Head == "" || p.ECTSCoordinator == "" || p.History == "" || p.Graduation == "" {
		t.Fatalf("bölümler eşlenmedi: %+v", p)
	}
	if len(p.Outcomes) != 3 || p.Outcomes[1].Description != "Normal doğumu yönetir." {
		t.Fatalf("PÖÇ yedek çıkarımı hatalı: %+v", p.Outcomes)
	}
}

func TestOutcomesPage(t *testing.T) {
	outs, _ := ParseOutcomesPage(bytes.NewReader(fixture(t, "progLearnOutcomes.aspx")))
	if len(outs) != 3 || outs[2].No != 3 {
		t.Fatalf("got %+v", outs)
	}
}

func TestCurriculum(t *testing.T) {
	c, err := ParseCurriculum(fixture(t, "progCourses.aspx"))
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Entries) != 3 {
		t.Fatalf("3 ders beklendi, %d", len(c.Entries))
	}
	e := c.Entries[2]
	if e.CourseID != 100003 || e.Semester != 2 || *e.ECTS != 4.5 || e.Type != "zorunlu" {
		t.Fatalf("got %+v", e)
	}
	if len(c.Links) != 1 {
		t.Fatalf("seçmeli havuz linki bulunamadı: %v", c.Links)
	}
}

func TestCourseDetail(t *testing.T) {
	d, err := ParseCourseDetail(bytes.NewReader(fixture(t, "progCourseDetails.aspx")))
	if err != nil {
		t.Fatal(err)
	}
	if d.Code != "EBE101" || *d.ECTS != 10 || *d.Theory != 4 || d.Language != "Türkçe" || d.DeliveryMode != "Örgün Öğretim" || d.Type != "Zorunlu" {
		t.Fatalf("özet/detay hatalı: %+v", d)
	}
	if len(d.LearningOutcomes) != 2 || len(d.WeeklyTopics) != 2 || len(d.Resources) != 2 {
		t.Fatalf("listeler hatalı: %+v", d)
	}
	if len(d.Assessments) != 2 || len(d.Workloads) != 1 {
		t.Fatalf("toplam satırları ayıklanmadı: %+v %+v", d.Assessments, d.Workloads)
	}
	want := map[int]int{1: 4, 2: 5, 4: 1}
	if len(d.ProgramOutcomes) != 3 {
		t.Fatalf("matris: %+v", d.ProgramOutcomes)
	}
	for _, oc := range d.ProgramOutcomes {
		if oc.Level == nil || *oc.Level != want[oc.No] {
			t.Fatalf("PÇ%d düzeyi hatalı: %+v", oc.No, oc)
		}
	}
}

func TestMatrixNumericLayout(t *testing.T) {
	html := `<div>Program Çıktıları ile İlişkisi</div><table>
	<tr><th>No</th><th>Program Çıktısı</th><th>Katkı Düzeyi</th></tr>
	<tr><td>1</td><td>Analiz eder</td><td>3</td></tr><tr><td>2</td><td>Tasarlar</td><td>5</td></tr></table>`
	d, _ := ParseCourseDetail(bytes.NewReader([]byte(html)))
	if len(d.ProgramOutcomes) != 2 || *d.ProgramOutcomes[1].Level != 5 || d.ProgramOutcomes[0].Description != "Analiz eder" {
		t.Fatalf("got %+v", d.ProgramOutcomes)
	}
}

func TestUnits(t *testing.T) {
	ps, _, err := ParseUnitSelection(bytes.NewReader(fixture(t, "unitSelection.aspx")))
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != 3 || ps[2].ID != 352 || ps[2].UnitID != 12 || ps[2].UnitName != "MÜHENDİSLİK FAKÜLTESİ" {
		t.Fatalf("got %+v", ps)
	}
}

func TestExtractNumbered(t *testing.T) {
	got := ExtractNumbered("Giriş 2. paragraf. 1. Analiz eder. 2.5 puanlık 2) Tasarlar 12. değil 3. Uygular\nsonraki satır")
	if len(got) != 3 || got[0].Description != "Analiz eder. 2.5 puanlık" || got[1].Description != "Tasarlar 12. değil" || got[2].Description != "Uygular" {
		t.Fatalf("got %+v", got)
	}
	if ExtractNumbered("sadece 1. madde") != nil {
		t.Fatal("tek madde yanlış pozitif sayılmamalı")
	}
}

func TestMatrixMarkedColumnsDeterministic(t *testing.T) {
	html := `<div>Dersin Program Çıktılarına Katkısı</div><table>
	<tr><th>No</th><th>Program Çıktısı</th><th>1</th><th>2</th><th>3</th><th>4</th><th>5</th></tr>
	<tr><td>1</td><td>Analiz</td><td></td><td>X</td><td></td><td>X</td><td></td></tr>
	<tr><td>2</td><td>Tasarım</td><td></td><td></td><td></td><td></td><td>✓</td></tr></table>`
	for i := 0; i < 20; i++ { // map sırası rastgele: sonuç her seferinde aynı olmalı
		d, _ := ParseCourseDetail(bytes.NewReader([]byte(html)))
		if len(d.ProgramOutcomes) != 2 || *d.ProgramOutcomes[0].Level != 4 || *d.ProgramOutcomes[1].Level != 5 || d.ProgramOutcomes[0].Description != "Analiz" {
			t.Fatalf("got %+v", d.ProgramOutcomes)
		}
	}
}

func TestMatrixWideLayout(t *testing.T) {
	html := `<div>Öğrenme Çıktıları ile Program Çıktıları İlişkisi</div><table>
	<tr><th></th><th>PÇ1</th><th>PÇ2</th><th>PÇ3</th></tr>
	<tr><td>ÖÇ1</td><td>3</td><td></td><td>5</td></tr>
	<tr><td>ÖÇ2</td><td>4</td><td>1</td><td></td></tr></table>`
	d, _ := ParseCourseDetail(bytes.NewReader([]byte(html)))
	want := []int{4, 1, 5}
	if len(d.ProgramOutcomes) != 3 {
		t.Fatalf("got %+v", d.ProgramOutcomes)
	}
	for i, oc := range d.ProgramOutcomes {
		if oc.No != i+1 || oc.Level == nil || *oc.Level != want[i] {
			t.Fatalf("PÇ%d: %+v", i+1, oc)
		}
	}
}

// ---- Gerçek OİBS sayfaları (testdata/real, obs.mersin.edu.tr'den 2026-09-26'da alındı; ViewState kırpıldı) ----

func TestRealCurriculum(t *testing.T) {
	c, err := ParseCurriculum(fixture(t, "real/progCourses_1371.html"))
	if err != nil {
		t.Fatal(err)
	}
	if c.UnitName != "MÜHENDİSLİK FAKÜLTESİ" || c.ProgramName != "BİLGİSAYAR MÜHENDİSLİĞİ" || c.Year == nil || *c.Year != 2026 {
		t.Fatalf("başlık: %q %q %v", c.UnitName, c.ProgramName, c.Year)
	}
	if len(c.Entries) != 345 {
		t.Fatalf("345 kayıt beklendi, %d", len(c.Entries))
	}
	byID := map[int64]CurriculumEntry{}
	groups, pool, bySem := 0, 0, map[int]int{}
	for _, e := range c.Entries {
		byID[e.CourseID] = e
		bySem[e.Semester]++
		if e.IsGroup {
			groups++
		}
		if e.GroupID != 0 {
			pool++
		}
	}
	if groups != 17 || pool != 285 || bySem[0] != 0 || bySem[1] != 9 || bySem[8] != 29 {
		t.Fatalf("grup=%d havuz=%d yarıyıl=%v", groups, pool, bySem)
	}
	e := byID[1098805] // PROGRAMLAMAYA GİRİŞ I
	if e.Code != "1551014" || e.Semester != 1 || e.Type != "zorunlu" || *e.Theory != 3 || *e.Practice != 2 || *e.Lab != 0 || *e.ECTS != 5 || e.Delivery != "Yüz Yüze" {
		t.Fatalf("zorunlu ders: %+v", e)
	}
	g := byID[1098790] // NORMAL EĞİTİM PAKETİ: 5 ders seçilir
	if !g.IsGroup || g.Semester != 8 || g.GroupCount == nil || *g.GroupCount != 5 {
		t.Fatalf("grup: %+v", g)
	}
	p := byID[1137367] // SİBER GÜVENLİĞE GİRİŞ, MD-OSD GÜZ2 havuzunda
	if p.GroupID != 1098854 || p.Type != "secmeli" || p.Delivery != "Uzaktan" || p.Semester != 3 {
		t.Fatalf("havuz dersi: %+v", p)
	}
}

func TestRealProgramAbout(t *testing.T) {
	p, err := ParseProgramAbout(bytes.NewReader(fixture(t, "real/progAbout_1371.html")))
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "BİLGİSAYAR MÜHENDİSLİĞİ" || p.Language != "%30 İngilizce" || *p.Duration != 4 || *p.MaxDuration != 7 ||
		p.Internship != "Var" || p.Head != "Prof. Dr. Erdinç AVAROĞLU" || p.ECTSCoordinator != "Prof. Dr. Zeki YETGİN" {
		t.Fatalf("temel alanlar: %+v", p)
	}
	if p.History == "" || p.Degree == "" || p.Admission == "" || p.Progression == "" || p.Graduation == "" || p.Employment == "" || p.Assessment == "" {
		t.Fatalf("bölümler eksik: %+v", p)
	}
	if p.Info["Mezuniyet Unvanı"] != "Bilgisayar Mühendisi" {
		t.Fatalf("info: %v", p.Info)
	}
}

func TestUnitHelpTextIgnored(t *testing.T) {
	html := `<div class="panel-heading">Fakültelere bağlı programları listelemek için fakültelerin sol tarafında yer alan artı(+) butonuna basınız</div>
	<a href="index.aspx?curOp=showPac&curUnit=22&curSunit=1371">BİLGİSAYAR MÜHENDİSLİĞİ</a>`
	ps, _, _ := ParseUnitSelection(bytes.NewReader([]byte(html)))
	if len(ps) != 1 || ps[0].UnitID != 22 || ps[0].UnitName != "" {
		t.Fatalf("got %+v", ps)
	}
}
