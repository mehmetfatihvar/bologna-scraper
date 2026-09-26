package store

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mehmetfatihvar/staj/staj/bologna-scraper/internal/parse"
)

// testStore, BOLOGNA_TEST_DB_URL verilmişse geçici bir şemada çalışan Store döndürür.
// Örn: BOLOGNA_TEST_DB_URL=postgres://bologna:bologna@localhost:5432/bologna?sslmode=disable go test ./internal/store/
func testStore(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("BOLOGNA_TEST_DB_URL")
	if dsn == "" {
		t.Skip("BOLOGNA_TEST_DB_URL yok; PostgreSQL entegrasyon testi atlandı")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("bologna_test_%d", time.Now().UnixNano())
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
		admin.Close()
	})
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	st, err := Open(ctx, dsn+sep+"search_path="+schema)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	for i := 0; i < 2; i++ { // migrate idempotent olmalı
		if err := st.Migrate(ctx); err != nil {
			t.Fatal(err)
		}
	}
	return st
}

func ip(v int) *int         { return &v }
func fp(v float64) *float64 { return &v }
func (s *Store) count(t *testing.T, q string, args ...any) int {
	t.Helper()
	var n int
	if err := s.pool.QueryRow(context.Background(), q, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestStoreRoundTrip(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	// Birim: aynı enstitü iki seviyede görünür, seviyeler birikmeli.
	must(t, st.Unit(ctx, 7, "FEN BİLİMLERİ ENSTİTÜSÜ", "yukseklisans"))
	must(t, st.Unit(ctx, 7, "", "doktora"))
	must(t, st.Unit(ctx, 7, "", "doktora"))
	var levels []string
	var name string
	must(t, st.pool.QueryRow(ctx, `SELECT name, levels FROM units WHERE id = 7`).Scan(&name, &levels))
	if name != "FEN BİLİMLERİ ENSTİTÜSÜ" || strings.Join(levels, ",") != "yukseklisans,doktora" {
		t.Fatalf("unit: %q %v", name, levels)
	}

	refA := parse.ProgramRef{ID: 1, UnitID: 7, Level: "yukseklisans", Name: "A"}
	refB := parse.ProgramRef{ID: 2, UnitID: 7, Level: "doktora"}
	must(t, st.Program(ctx, refA, &parse.Program{Name: "PROGRAM A", Duration: ip(2), Info: map[string]string{"Dili": "Türkçe"}, Sections: map[string]string{}}, "u"))
	must(t, st.Program(ctx, refB, &parse.Program{Info: map[string]string{}, Sections: map[string]string{}}, "u"))

	// Çıktılar: metin tahmini (5 madde) → resmî sayfa (3 madde) → düşük öncelikli kaynak ezemez.
	var guess []parse.Outcome
	for i := 1; i <= 5; i++ {
		guess = append(guess, parse.Outcome{No: i, Description: fmt.Sprintf("tahmin %d", i)})
	}
	must(t, st.ProgramOutcomes(ctx, 1, guess, SourceAboutText))
	must(t, st.ProgramOutcomes(ctx, 1, []parse.Outcome{{No: 1, Description: "resmi 1"}, {No: 2, Description: "resmi 2"}, {No: 3, Description: "resmi 3"}}, SourceOutcomesPage))
	must(t, st.ProgramOutcomes(ctx, 1, []parse.Outcome{{No: 1, Description: "tahmin tekrar"}}, SourceAboutText))
	if n := st.count(t, `SELECT count(*) FROM program_outcomes WHERE program_id = 1`); n != 3 {
		t.Fatalf("yanlış pozitif maddeler silinmedi: %d", n)
	}
	if n := st.count(t, `SELECT count(*) FROM program_outcomes WHERE program_id = 1 AND source = 'outcomes_page' AND description LIKE 'resmi%'`); n != 3 {
		t.Fatalf("öncelik bozuk: %d", n)
	}

	// Ders planı: ortak ders (10) iki programda; ikinci çalıştırma planı değiştirir.
	year := 2026
	must(t, st.Curriculum(ctx, 1, &parse.Curriculum{Year: &year, Entries: []parse.CurriculumEntry{
		{CourseID: 10, Semester: 1, Code: "MAT501", Name: "İleri Matematik", Type: "zorunlu", ECTS: fp(7.5), Lab: ip(2), Delivery: "Yüz Yüze"},
		{CourseID: 11, Semester: 2, Code: "MAT502"},
	}}))
	must(t, st.Curriculum(ctx, 2, &parse.Curriculum{Entries: []parse.CurriculumEntry{{CourseID: 10, Type: "secmeli"}}}))
	must(t, st.Curriculum(ctx, 1, &parse.Curriculum{Entries: []parse.CurriculumEntry{
		{CourseID: 10, Semester: 1, Type: "zorunlu"},
		// seçmeli grup + havuz dersi
		{CourseID: 90, Semester: 5, Code: "1555S01", Name: "TEKNİK SEÇMELİ DERS", Type: "secmeli", IsGroup: true, GroupCount: ip(2)},
		{CourseID: 91, Semester: 5, Code: "1555101", Name: "YAPAY ZEKA", Type: "secmeli", GroupID: 90},
	}}))
	if n := st.count(t, `SELECT count(*) FROM programs WHERE id = 1 AND curriculum_year = 2026`); n != 1 {
		t.Fatal("curriculum_year yazılmadı")
	}
	if n := st.count(t, `SELECT count(*) FROM v_curriculum WHERE course_id = 91 AND elective_group_code = '1555S01' AND lab_hours IS NULL`); n != 1 {
		t.Fatal("havuz dersi grubuna bağlanmadı")
	}
	if n := st.count(t, `SELECT count(*) FROM courses WHERE id = 91 AND program_id = 1`); n != 1 {
		t.Fatal("havuz dersi planında göründüğü programa ait olmalı (OİBS her programa ayrı kopya koyar)")
	}
	// Paylaşımlı havuz: 93, program 2'deki kopya; içeriği 91'den alır, matrisi yazılmaz.
	src := &parse.CourseDetail{Code: "1555101", Name: "YAPAY ZEKA", Department: "BİLGİSAYAR", Details: map[string]string{"Bölümü / Programı": "BİLGİSAYAR"},
		LearningOutcomes: []parse.LearningOutcome{{No: 1, Description: "x"}},
		OutcomeMatrix:    []parse.MatrixCell{{LO: 1, PO: 1, Level: 3}}}
	must(t, st.Course(ctx, 1, 91, src, "u"))
	must(t, st.Course(ctx, 2, 93, src.AsCopyOf(91), "u"))
	if n := st.count(t, `SELECT count(*) FROM courses WHERE id = 93 AND detail_source_id = 91 AND program_id = 2`); n != 1 {
		t.Fatal("detail_source_id yazılmadı")
	}
	if n := st.count(t, `SELECT (SELECT count(*) FROM course_learning_outcomes WHERE course_id = 93)*10 + (SELECT count(*) FROM course_outcome_matrix WHERE course_id = 93)`); n != 10 {
		t.Fatalf("kopya içerik/matris: %d", n)
	}
	if fresh, _ := st.CourseFresh(ctx, 93, time.Now().Add(-time.Hour)); !fresh {
		t.Fatal("kopya taze sayılmalı")
	}
	// Sitede boş ders sayfası: detail_empty işaretlenir, sonraki dolu yazımda kalkar.
	must(t, st.Course(ctx, 1, 94, &parse.CourseDetail{Details: map[string]string{}, Empty: true}, "u"))
	if n := st.count(t, `SELECT count(*) FROM courses WHERE id = 94 AND detail_empty`); n != 1 {
		t.Fatal("detail_empty yazılmadı")
	}
	must(t, st.Course(ctx, 1, 94, &parse.CourseDetail{Name: "DOLU", Details: map[string]string{}}, "u"))
	if n := st.count(t, `SELECT count(*) FROM courses WHERE id = 94 AND NOT detail_empty`); n != 1 {
		t.Fatal("detail_empty sıfırlanmadı")
	}
	// Sütundan önce yazılmış boş sayfa: Migrate işaretler, dolu ders etkilenmez; tekrar çalışması zararsız.
	must(t, st.Course(ctx, 1, 95, &parse.CourseDetail{Details: map[string]string{"tablo: Dersin Ayrıntıları": "Kayıt Yok..."}}, "u"))
	must(t, st.Migrate(ctx))
	must(t, st.Migrate(ctx))
	if n := st.count(t, `SELECT count(*) FROM courses WHERE id = 95 AND detail_empty AND details = '{}'::jsonb`); n != 1 {
		t.Fatal("eski boş sayfa işaretlenmedi")
	}
	if n := st.count(t, `SELECT count(*) FROM courses WHERE id IN (91, 94) AND detail_empty`); n != 0 {
		t.Fatal("dolu ders boş işaretlendi")
	}
	// Eski plan yılı: arşiv planı güncel yılı değiştirmez; güncel plan yeniden yazılınca eski yıl silinmez.
	y2025 := 2025
	must(t, st.Curriculum(ctx, 1, &parse.Curriculum{Year: &y2025, Archived: true, Entries: []parse.CurriculumEntry{
		{CourseID: 200, Semester: 1, Code: "155-1007", Name: "ESKİ PLAN DERSİ"},
	}}))
	must(t, st.Curriculum(ctx, 1, &parse.Curriculum{Year: &year, Entries: []parse.CurriculumEntry{
		{CourseID: 10, Semester: 1, Type: "zorunlu"},
		{CourseID: 90, Semester: 5, Code: "1555S01", Name: "TEKNİK SEÇMELİ DERS", Type: "secmeli", IsGroup: true, GroupCount: ip(2)},
		{CourseID: 91, Semester: 5, Code: "1555101", Name: "YAPAY ZEKA", Type: "secmeli", GroupID: 90},
	}}))
	if n := st.count(t, `SELECT count(*) FROM programs WHERE id = 1 AND curriculum_year = 2026`); n != 1 {
		t.Fatal("arşiv planı güncel yılı değiştirdi")
	}
	if n := st.count(t, `SELECT count(*) FROM v_curriculum WHERE program_id = 1 AND course_id = 200 AND plan_year = 2025 AND NOT is_current`); n != 1 {
		t.Fatal("eski yıl planı silindi ya da güncel sayıldı")
	}
	if n := st.count(t, `SELECT count(*) FROM v_curriculum WHERE program_id = 1 AND is_current`); n != 3 {
		t.Fatalf("güncel plan: %d", n)
	}
	// Birim tanıtımı; boş ad mevcut adı silmez.
	must(t, st.UnitAbout(ctx, 7, &parse.UnitAbout{About: "Tanıtım\nİkinci paragraf"}))
	if n := st.count(t, `SELECT count(*) FROM units WHERE id = 7 AND name = 'FEN BİLİMLERİ ENSTİTÜSÜ' AND about LIKE 'Tanıtım%' AND about_scraped_at IS NOT NULL`); n != 1 {
		t.Fatal("birim tanıtımı")
	}
	// Sahibi bilinmeden yazılmış ders: içerik yazılır, katkısı bağlanmaz ve taze sayılmaz (yeniden indirilir).
	must(t, st.Course(ctx, 0, 92, &parse.CourseDetail{Name: "SAHİPSİZ", Details: map[string]string{},
		LearningOutcomes: []parse.LearningOutcome{{No: 1, Description: "x"}},
		ProgramOutcomes:  []parse.OutcomeContribution{{No: 1, Level: ip(4)}},
		OutcomeMatrix:    []parse.MatrixCell{{LO: 1, PO: 1, Level: 4}}}, "u"))
	if n := st.count(t, `SELECT count(*) FROM course_learning_outcomes WHERE course_id = 92`); n != 1 {
		t.Fatal("sahipsiz ders içeriği yazılmadı")
	}
	if n := st.count(t, `SELECT (SELECT count(*) FROM course_program_outcomes WHERE course_id = 92) + (SELECT count(*) FROM course_outcome_matrix WHERE course_id = 92)`); n != 0 {
		t.Fatal("sahipsiz dersin katkısı bir programa bağlandı")
	}
	if fresh, _ := st.CourseFresh(ctx, 92, time.Now().Add(-time.Hour)); fresh {
		t.Fatal("sahipsiz ders taze sayılmamalı")
	}
	if n := st.count(t, `SELECT count(*) FROM program_courses WHERE course_id = 90 AND is_elective_group AND group_course_count = 2`); n != 1 {
		t.Fatal("seçmeli grup satırı")
	}
	if n := st.count(t, `SELECT count(*) FROM courses WHERE id = 10 AND lab_hours = 2`); n != 1 {
		t.Fatal("lab_hours")
	}
	if n := st.count(t, `SELECT count(*) FROM program_courses WHERE program_id = 1 AND course_id = 11`); n != 0 {
		t.Fatalf("plandan çıkan ders silinmedi: %d", n)
	}
	if n := st.count(t, `SELECT count(*) FROM v_curriculum WHERE course_id = 10`); n != 2 {
		t.Fatalf("N:M ilişki: %d", n)
	}

	// Ders detayı: alt tablolar her yazımda değiştirilir, matris sahibi programa (1) bağlanır.
	d := &parse.CourseDetail{
		Code: "MAT501", Name: "İleri Matematik", ECTS: fp(7.5), Details: map[string]string{"Dersin Dili": "Türkçe"},
		LearningOutcomes: []parse.LearningOutcome{{No: 1, Description: "a"}, {No: 2, Description: "b"}},
		WeeklyTopics:     []parse.WeeklyTopic{{Week: ip(1), Topic: "giriş"}},
		Assessments:      []parse.Assessment{{Activity: "Vize", Percentage: fp(40)}},
		ProgramOutcomes:  []parse.OutcomeContribution{{No: 1, Level: ip(5)}, {No: 4, Description: "matristen", Level: ip(2)}, {No: 7}},
		OutcomeMatrix:    []parse.MatrixCell{{LO: 1, PO: 1, Level: 5}, {LO: 2, PO: 1, Level: 3}, {LO: 1, PO: 6, Level: 1}},
		TeachingMode:     "Yüz Yüze",
	}
	since := time.Now().Add(-time.Minute)
	if fresh, _ := st.CourseFresh(ctx, 10, since); fresh {
		t.Fatal("detay yazılmadan taze sayıldı")
	}
	must(t, st.Course(ctx, 1, 10, d, "u"))
	d.LearningOutcomes = d.LearningOutcomes[:1]
	must(t, st.Course(ctx, 2, 10, d, "u"))
	if fresh, _ := st.CourseFresh(ctx, 10, since); !fresh {
		t.Fatal("CourseFresh")
	}
	if n := st.count(t, `SELECT count(*) FROM course_learning_outcomes WHERE course_id = 10`); n != 1 {
		t.Fatalf("alt tablo değiştirilmedi: %d", n)
	}
	if n := st.count(t, `SELECT count(*) FROM v_course_outcome_matrix WHERE course_id = 10 AND program_id = 1`); n != 2 {
		t.Fatalf("matris: %d", n)
	}
	if n := st.count(t, `SELECT count(*) FROM v_course_lo_po_matrix WHERE course_id = 10 AND program_id = 1`); n != 3 {
		t.Fatalf("ÖÇ×PÇ matrisi: %d", n)
	}
	if n := st.count(t, `SELECT count(*) FROM v_course_lo_po_matrix WHERE course_id = 10 AND lo_no = 1 AND outcome_no = 1 AND learning_outcome = 'a' AND contribution = 5`); n != 1 {
		t.Fatal("matris ÖÇ açıklamasına bağlanmadı")
	}
	if n := st.count(t, `SELECT count(*) FROM courses WHERE id = 10 AND teaching_mode = 'Yüz Yüze'`); n != 1 {
		t.Fatal("teaching_mode")
	}
	if n := st.count(t, `SELECT count(*) FROM program_outcomes WHERE program_id = 1 AND no = 4 AND source = 'course_matrix'`); n != 1 {
		t.Fatalf("matristen gelen PÇ oluşmadı: %d", n)
	}

	// Çalışma kaydı
	run, err := st.StartRun(ctx, map[string]any{"x": 1})
	must(t, err)
	st.LogError(ctx, run, "http://x", "boom")
	must(t, st.FinishRun(ctx, run, "partial", 2, 1, 1))
	if n := st.count(t, `SELECT count(*) FROM scrape_errors WHERE run_id = $1`, run); n != 1 {
		t.Fatal("scrape_errors")
	}
	if _, err := st.Stats(ctx); err != nil {
		t.Fatal(err)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
