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
