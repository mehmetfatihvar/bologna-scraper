// Package scraper, sayfaları sırayla indirip parse eder ve bir Sink'e yazar.
package scraper

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/url"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mehmetfatihvar/staj/staj/bologna-scraper/internal/fetch"
	"github.com/mehmetfatihvar/staj/staj/bologna-scraper/internal/parse"
)

// OİBS Bologna modülünün sayfa adları. Site farklı isim kullanıyorsa sadece burayı değiştirin.
const (
	PageUnitSelection  = "unitSelection.aspx"     // ?type=lis|myo|yls|dok
	PageProgramAbout   = "progAbout.aspx"         // ?curSunit=
	PageProgramOutcome = "progLearnOutcomes.aspx" // ?curSunit=   (yoksa 404 -> sorun değil)
	PageCurriculum     = "progCourses.aspx"       // ?curSunit=
	PageCourseDetail   = "progCourseDetails.aspx" // ?curCourse=
)

// LevelTypes: CLI seviye adı -> unitSelection.aspx "type" parametresi.
var LevelTypes = map[string]string{
	"onlisans":     "myo",
	"lisans":       "lis",
	"yukseklisans": "yls",
	"doktora":      "dok",
}

// Sink, sonuçların yazılacağı hedef (PostgreSQL veya JSON çıktı).
type Sink interface {
	Unit(ctx context.Context, id int64, name, level string) error
	Program(ctx context.Context, ref parse.ProgramRef, p *parse.Program, url string) error
	ProgramOutcomes(ctx context.Context, programID int64, outs []parse.Outcome, source string) error
	Curriculum(ctx context.Context, programID int64, cur *parse.Curriculum) error
	// Course: programID dersin sahibi olan program; 0 ise sahip bilinmiyor.
	Course(ctx context.Context, programID, courseID int64, d *parse.CourseDetail, url string) error
}

// FreshChecker, isteğe bağlı olarak Sink tarafından uygulanır: ders detayı
// verilen zamandan sonra zaten yazılmışsa true döner (kesilen taramayı sürdürmek için).
type FreshChecker interface {
	CourseFresh(ctx context.Context, courseID int64, since time.Time) (bool, error)
}

type Scraper struct {
	Client      *fetch.Client
	Sink        Sink
	Log         *slog.Logger
	Workers     int
	SkipDetails bool
	// SkipPoolDetails: seçmeli havuz derslerinin (plan satırı yine yazılır) detay sayfalarını indirme.
	// Havuzlar büyük ve çoğu programda aynı ortak seçmeli derslerin kopyalarıdır; tam lisans taramasında
	// istek sayısını ~%80 azaltır.
	SkipPoolDetails bool
	// FreshWithin > 0 ise, detayı bu süre içinde yazılmış dersler tekrar indirilmez.
	FreshWithin time.Duration
	OnError     func(url string, err error)

	programs, courses, skipped, errors atomic.Int64
	done                               atomic.Int64 // biten program (başarılı/başarısız)
	courseSeen                         sync.Map     // curCourse -> struct{}: aynı çalışmada bir kez indir
}

type Stats struct {
	Programs, Courses, Skipped, Errors int64
}

func (s *Scraper) Stats() Stats {
	return Stats{s.programs.Load(), s.courses.Load(), s.skipped.Load(), s.errors.Load()}
}

func (s *Scraper) fail(u string, err error) {
	s.errors.Add(1)
	s.Log.Warn("hata", "url", u, "err", err)
	if s.OnError != nil {
		s.OnError(u, err)
	}
}

func id(v int64) string { return strconv.FormatInt(v, 10) }

// Discover, verilen seviye tipindeki tüm programları bulur.
func (s *Scraper) Discover(ctx context.Context, level, typ string) ([]parse.ProgramRef, error) {
	type item struct {
		url      string
		depth    int
		unitID   int64
		unitName string
	}
	start := s.Client.URL(PageUnitSelection, url.Values{"type": {typ}})
	queue := []item{{url: start}}
	visited := map[string]bool{}
	var (
		refs      []parse.ProgramRef
		seen      = map[int64]bool{}
		unitsWith = map[int64]bool{}
	)

	for len(queue) > 0 {
		it := queue[0]
		queue = queue[1:]
		if visited[it.url] {
			continue
		}
		visited[it.url] = true

		body, err := s.Client.Get(ctx, it.url)
		if err != nil {
			if it.depth == 0 {
				return nil, err
			}
			s.fail(it.url, err)
			continue
		}
		progs, units, err := parse.ParseUnitSelection(bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		for _, p := range progs {
			if p.UnitID == 0 {
				p.UnitID = it.unitID
			}
			if p.UnitName == "" {
				p.UnitName = it.unitName
			}
			p.Level = level
			if p.UnitID != 0 {
				unitsWith[p.UnitID] = true
			}
			if !seen[p.ID] {
				seen[p.ID] = true
				refs = append(refs, p)
			}
		}
		// Birim linkleri ayrı sayfaya gidiyorsa ve o birimden program görmediysek bir seviye in.
		if it.depth == 0 {
			for _, u := range units {
				if u.URL != "" && !unitsWith[u.ID] {
					queue = append(queue, item{url: s.Client.Resolve(u.URL), depth: 1, unitID: u.ID, unitName: u.Name})
				}
			}
		}
		s.Log.Debug("keşif", "url", it.url, "program", len(progs), "birim", len(units))
	}
	return refs, nil
}

// Run, programları Workers kadar paralel işler. Toplam istek hızı fetch.Client'ın RPS'i ile sınırlıdır.
func (s *Scraper) Run(ctx context.Context, refs []parse.ProgramRef) Stats {
	workers := s.Workers
	if workers < 1 {
		workers = 1
	}
	jobs := make(chan parse.ProgramRef)
	var wg sync.WaitGroup
	start, total := time.Now(), int64(len(refs))
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ref := range jobs {
				s.program(ctx, ref)
				if ctx.Err() != nil {
					continue
				}
				n := s.done.Add(1)
				eta := time.Duration(float64(time.Since(start)) / float64(n) * float64(total-n)).Round(time.Second)
				s.Log.Info("ilerleme", "biten", n, "toplam", total, "ders", s.courses.Load(), "hata", s.errors.Load(), "kalan_süre", eta)
			}
		}()
	}
loop:
	for _, r := range refs {
		select {
		case jobs <- r:
		case <-ctx.Done():
			break loop
		}
	}
	close(jobs)
	wg.Wait()
	return s.Stats()
}

func (s *Scraper) program(ctx context.Context, ref parse.ProgramRef) {
	if ctx.Err() != nil {
		return
	}
	log := s.Log.With("program", ref.ID)
	q := url.Values{"curSunit": {id(ref.ID)}}

	// 1) Birim
	if ref.UnitID != 0 {
		if err := s.Sink.Unit(ctx, ref.UnitID, ref.UnitName, ref.Level); err != nil {
			s.fail("unit:"+id(ref.UnitID), err)
		}
	}

	// 2) Program bilgileri (başarısız olsa bile FK'ler için minimal kayıt yazılır)
	aboutURL := s.Client.URL(PageProgramAbout, q)
	p := &parse.Program{}
	if body, err := s.Client.Get(ctx, aboutURL); err != nil {
		s.fail(aboutURL, err)
	} else if parsed, err := parse.ParseProgramAbout(bytes.NewReader(body)); err != nil {
		s.fail(aboutURL, err)
	} else {
		p = parsed
	}
	if err := s.Sink.Program(ctx, ref, p, aboutURL); err != nil {
		s.fail(aboutURL, err)
		return
	}
	if len(p.Outcomes) > 0 {
		if err := s.Sink.ProgramOutcomes(ctx, ref.ID, p.Outcomes, "about_text"); err != nil {
			s.fail(aboutURL, err)
		}
	}
	name := p.Name
	if name == "" {
		name = ref.Name
	}
	log.Info("program", "ad", name)

	// 3) Program yeterlilikleri sayfası (opsiyonel)
	outURL := s.Client.URL(PageProgramOutcome, q)
	if body, err := s.Client.Get(ctx, outURL); err == nil {
		if outs, err := parse.ParseOutcomesPage(bytes.NewReader(body)); err == nil && len(outs) > 0 {
			if err := s.Sink.ProgramOutcomes(ctx, ref.ID, outs, "outcomes_page"); err != nil {
				s.fail(outURL, err)
			}
		}
	} else if !fetch.IsNotFound(err) {
		log.Debug("yeterlilik sayfası alınamadı", "err", err)
	}

	// 4) Ders planı (+ seçmeli havuzları, bir seviye)
	curURL := s.Client.URL(PageCurriculum, q)
	body, err := s.Client.Get(ctx, curURL)
	if err != nil {
		s.fail(curURL, err)
		return
	}
	cur, err := parse.ParseCurriculum(body)
	if err != nil {
		s.fail(curURL, err)
		return
	}
	entries := cur.Entries
	have := map[int64]bool{}
	for _, e := range entries {
		have[e.CourseID] = true
	}
	for _, l := range cur.Links {
		lu := s.Client.Resolve(l)
		b, err := s.Client.Get(ctx, lu)
		if err != nil {
			log.Debug("alt sayfa alınamadı", "url", lu, "err", err)
			continue
		}
		sub, err := parse.ParseCurriculum(b)
		if err != nil {
			continue
		}
		for _, e := range sub.Entries {
			if have[e.CourseID] {
				continue
			}
			have[e.CourseID] = true
			if e.Type == "" {
				e.Type = "secmeli"
			}
			entries = append(entries, e)
		}
	}
	cur.Entries = entries
	// Birim adı keşif sayfasında güvenilir değil; ders planı başlığı ("FAKÜLTE / PROGRAM - Dersler") kesin kaynak.
	if ref.UnitID != 0 && cur.UnitName != "" && cur.UnitName != ref.UnitName {
		if err := s.Sink.Unit(ctx, ref.UnitID, cur.UnitName, ref.Level); err != nil {
			s.fail("unit:"+id(ref.UnitID), err)
		}
	}
	if err := s.Sink.Curriculum(ctx, ref.ID, cur); err != nil {
		s.fail(curURL, err)
		return
	}
	s.programs.Add(1)
	log.Info("ders planı", "ders", len(entries))

	if s.SkipDetails {
		return
	}

	// 5) Ders detayları. Seçmeli havuz dersleri de bu programa aittir: OİBS her programın planına
	// kendi kopyasını (ayrı curCourse) koyar ve sayfadaki P1…Pn bu programın çıktılarıdır.
	for _, e := range entries {
		if ctx.Err() != nil {
			return
		}
		switch {
		case e.IsGroup: // seçmeli grup satırının detay sayfası yok
		case e.GroupID != 0 && s.SkipPoolDetails:
		default:
			s.course(ctx, ref.ID, e.CourseID)
		}
	}
}

// course, ders detayını bir çalışmada en fazla bir kez indirip yazar.
func (s *Scraper) course(ctx context.Context, ownerID, courseID int64) {
	if _, dup := s.courseSeen.LoadOrStore(courseID, struct{}{}); dup {
		return
	}
	if fc, ok := s.Sink.(FreshChecker); ok && s.FreshWithin > 0 {
		if fresh, err := fc.CourseFresh(ctx, courseID, time.Now().Add(-s.FreshWithin)); err == nil && fresh {
			s.skipped.Add(1)
			return
		}
	}
	du := s.Client.URL(PageCourseDetail, url.Values{"curCourse": {id(courseID)}})
	b, err := s.Client.Get(ctx, du)
	if err != nil {
		s.fail(du, err)
		return
	}
	d, err := parse.ParseCourseDetail(bytes.NewReader(b))
	if err != nil {
		s.fail(du, err)
		return
	}
	if err := s.Sink.Course(ctx, ownerID, courseID, d, du); err != nil {
		s.fail(du, err)
		return
	}
	s.courses.Add(1)
	s.Log.Debug("ders", "id", courseID, "sahip", ownerID, "kod", d.Code, "ad", d.Name)
}

// JSONSink, veritabanı olmadan (--dry-run) sonuçları JSON satırları olarak yazar.
type JSONSink struct {
	mu  sync.Mutex
	enc *json.Encoder
}

func NewJSONSink(w io.Writer) *JSONSink {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return &JSONSink{enc: enc}
}

func (j *JSONSink) emit(kind string, v any) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.enc.Encode(map[string]any{"type": kind, "data": v})
}

func (j *JSONSink) Unit(_ context.Context, id int64, name, level string) error {
	return j.emit("unit", map[string]any{"id": id, "name": name, "level": level})
}
func (j *JSONSink) Program(_ context.Context, ref parse.ProgramRef, p *parse.Program, u string) error {
	return j.emit("program", map[string]any{"ref": ref, "program": p, "url": u})
}
func (j *JSONSink) ProgramOutcomes(_ context.Context, pid int64, outs []parse.Outcome, src string) error {
	return j.emit("program_outcomes", map[string]any{"program_id": pid, "source": src, "outcomes": outs})
}
func (j *JSONSink) Curriculum(_ context.Context, pid int64, cur *parse.Curriculum) error {
	return j.emit("curriculum", map[string]any{"program_id": pid, "curriculum": cur})
}
func (j *JSONSink) Course(_ context.Context, pid, cid int64, d *parse.CourseDetail, u string) error {
	return j.emit("course", map[string]any{"program_id": pid, "course_id": cid, "course": d, "url": u})
}
