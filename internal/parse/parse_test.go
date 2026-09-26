package parse

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
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

// İçerik girilmemiş ders sayfası: yalnızca "Kayıt Yok..." tablosu ve boş katkılı PÇ listesi.
func TestEmptyCourseDetail(t *testing.T) {
	page := `<html><body>
<div>Dersin Ayrıntıları</div><table><tr><td>Kayıt Yok...</td></tr></table>
<div>Program Çıktıları ile İlişkisi</div>
<table><tr><th></th><th>P1</th><th>P2</th></tr><tr><td>Ö1</td><td></td><td></td></tr></table>
</body></html>`
	d, err := ParseCourseDetail(strings.NewReader(page))
	if err != nil {
		t.Fatal(err)
	}
	if !d.Empty || len(d.Details) != 0 {
		t.Fatalf("boş sayfa işaretlenmedi: empty=%v details=%v", d.Empty, d.Details)
	}
	full, err := ParseCourseDetail(bytes.NewReader(fixture(t, "progCourseDetails.aspx")))
	if err != nil {
		t.Fatal(err)
	}
	if full.Empty {
		t.Fatal("dolu sayfa boş sayıldı")
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

func TestRealOutcomesPage(t *testing.T) {
	outs, err := ParseOutcomesPage(bytes.NewReader(fixture(t, "real/progLearnOutcomes_1371.html")))
	if err != nil {
		t.Fatal(err)
	}
	if len(outs) != 11 || outs[0].No != 1 || outs[10].No != 11 ||
		!strings.HasPrefix(outs[5].Description, "Disiplin içi ve çok disiplinli takımlarda") {
		t.Fatalf("got %d: %+v", len(outs), outs)
	}
}

func TestRealCourseDetail(t *testing.T) {
	d, err := ParseCourseDetail(bytes.NewReader(fixture(t, "real/progCourseDetails_1098805.html")))
	if err != nil {
		t.Fatal(err)
	}
	if d.Code != "1551014" || d.Name != "PROGRAMLAMAYA GİRİŞ I" || *d.Semester != 1 || *d.Theory != 3 || *d.Practice != 2 ||
		*d.Credit != 4 || *d.ECTS != 5 || d.Language != "Türkçe" || d.Type != "Zorunlu" ||
		d.DeliveryMode != "Örgün Öğretim" || d.TeachingMode != "Yüz Yüze" || d.Instructors != "Dr. Öğretim Üyesi Mehmet ACI" {
		t.Fatalf("özet/detay: %+v", d)
	}
	if d.Details["Son Güncelleme Tarihi"] != "16.06.2026" {
		t.Fatalf("details: %v", d.Details)
	}
	for k := range d.Details {
		if strings.HasPrefix(k, "tablo") {
			t.Fatalf("tanınmayan tablo kaldı: %q", k)
		}
	}
	if len(d.LearningOutcomes) != 3 || len(d.WeeklyTopics) != 15 || len(d.Resources) != 1 {
		t.Fatalf("listeler: ÖÇ=%d hafta=%d kaynak=%d", len(d.LearningOutcomes), len(d.WeeklyTopics), len(d.Resources))
	}
	if w := d.WeeklyTopics[0]; w.Preparation != "" || w.Documents != "" || !strings.HasPrefix(w.Topic, "Genel Kavramlar") {
		t.Fatalf("hafta 1: %+v", w)
	}
	if len(d.Assessments) != 2 || *d.Assessments[1].Percentage != 60 {
		t.Fatalf("değerlendirme: %+v", d.Assessments)
	}
	if len(d.Workloads) != 5 || *d.Workloads[0].Count != 14 || *d.Workloads[0].Hours != 3 || *d.Workloads[0].Total != 42 {
		t.Fatalf("iş yükü: %+v", d.Workloads)
	}
	// Matris: 3 ÖÇ × 11 PÇ, P7/P10/P11 boş → 24 hücre; PÇ başına en yüksek düzey.
	if len(d.OutcomeMatrix) != 24 || d.OutcomeMatrix[0] != (MatrixCell{LO: 1, PO: 1, Level: 2}) {
		t.Fatalf("matris: %d %+v", len(d.OutcomeMatrix), d.OutcomeMatrix)
	}
	want := map[int]int{1: 3, 5: 2, 6: 3, 8: 2}
	if len(d.ProgramOutcomes) != 11 {
		t.Fatalf("PÇ: %+v", d.ProgramOutcomes)
	}
	for _, oc := range d.ProgramOutcomes {
		if l, ok := want[oc.No]; ok && (oc.Level == nil || *oc.Level != l) {
			t.Fatalf("PÇ%d: %+v", oc.No, oc)
		}
		if (oc.No == 7 || oc.No == 10 || oc.No == 11) && oc.Level != nil {
			t.Fatalf("PÇ%d boş olmalı: %+v", oc.No, oc)
		}
	}
}

func TestRealUnitSelection(t *testing.T) {
	ps, us, err := ParseUnitSelection(bytes.NewReader(fixture(t, "real/unitSelection_lis.html")))
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != 153 {
		t.Fatalf("153 program beklendi, %d", len(ps))
	}
	byID := map[int64]ProgramRef{}
	for _, p := range ps {
		byID[p.ID] = p
		if p.UnitID == 0 || p.UnitName == "" {
			t.Fatalf("birimi eksik program: %+v", p)
		}
	}
	if p := byID[1371]; p.Name != "BİLGİSAYAR MÜHENDİSLİĞİ" || p.UnitID != 22 || p.UnitName != "MÜHENDİSLİK FAKÜLTESİ" {
		t.Fatalf("1371: %+v", p)
	}
	if p := byID[2297]; p.UnitID != 20 || p.UnitName != "İSLAMİ İLİMLER FAKÜLTESİ" {
		t.Fatalf("2297: %+v", p)
	}
	names := map[int64]string{}
	for _, u := range us {
		names[u.ID] = u.Name
		if u.URL != "" {
			t.Fatalf("birim linki takip edilmemeli: %+v", u)
		}
	}
	if names[22] != "MÜHENDİSLİK FAKÜLTESİ" || len(us) < 20 {
		t.Fatalf("birimler (%d): %v", len(us), names)
	}
}

func TestRealUnitAbout(t *testing.T) {
	read := func(f string) *UnitAbout {
		t.Helper()
		b, err := os.ReadFile(filepath.Join("testdata", "real", f))
		if err != nil {
			t.Fatal(err)
		}
		a, err := ParseUnitAbout(b)
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	a := read("facAbout_22.html")
	if a.Name != "MÜHENDİSLİK FAKÜLTESİ" || !strings.HasPrefix(a.About, "Mühendislik Fakültesi 3 Temmuz 1992") || strings.Count(a.About, "\n") != 4 {
		t.Fatalf("fakülte: %q\n%s", a.Name, a.About)
	}
	if a := read("facAbout_2.html"); a.Name != "FEN BİLİMLERİ ENSTİTÜSÜ" || !strings.Contains(a.About, "lisansüstü öğretim") {
		t.Fatalf("enstitü: %+v", a)
	}
	if a := read("facAbout_empty.html"); a.Name != "" || a.About != "" {
		t.Fatalf("tanımsız birim: %+v", a)
	}
}

// Yıl seçiciyle istenen eski plan: farklı kod biçimi (155-1007) ve ayrı ders ID'leri.
func TestRealCurriculum2025(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("testdata", "real", "progCourses_1371_2025.html"))
	if err != nil {
		t.Fatal(err)
	}
	cur, err := ParseCurriculum(b)
	if err != nil {
		t.Fatal(err)
	}
	if cur.Year == nil || *cur.Year != 2025 || len(cur.Entries) != 332 || len(cur.Years) != 3 || cur.Years[0].Selected {
		t.Fatalf("yıl=%v satır=%d yıllar=%+v", cur.Year, len(cur.Entries), cur.Years)
	}
	if e := cur.Entries[0]; e.Code != "155-1007" || e.Semester != 1 || e.CourseID == 0 {
		t.Fatalf("ilk satır: %+v", e)
	}
}
