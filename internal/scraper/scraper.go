// Package scraper, sayfaları sırayla indirip parse eder ve bir Sink'e yazar.
package scraper

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"slices"
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
	PageUnitAbout      = "facAbout.aspx"          // ?curUnit=
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
	UnitAbout(ctx context.Context, id int64, a *parse.UnitAbout) error
	Program(ctx context.Context, ref parse.ProgramRef, p *parse.Program, url string) error
	ProgramOutcomes(ctx context.Context, programID int64, outs []parse.Outcome, source string) error
	Curriculum(ctx context.Context, programID int64, cur *parse.Curriculum) error
	// Course: programID dersin sahibi olan program; 0 ise sahip bilinmiyor.
	Course(ctx context.Context, programID, courseID int64, d *parse.CourseDetail, url string) error
}

const (
	PoolOff    = ""
	PoolShared = "shared"
	PoolAll    = "all"
)

// poolCopy, bir havuz dersinin bir programdaki kopyası.
type poolCopy struct{ courseID, programID int64 }

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
	// PoolDetails: seçmeli havuz derslerinin detay sayfaları. OİBS havuz derslerini her programa ayrı kopya
	// (ayrı curCourse) olarak koyar; lisansta ~30 bin kopya, ~4,6 bin farklı ders kodu vardır.
	//   PoolOff    (""):       indirilmez; plan satırı (grup, AKTS, saatler) yine yazılır.
	//   PoolShared ("shared"): her ders kodu bir kez indirilir. Kod tek programdaysa ders eksiksiz yazılır;
	//                          birden çok programdaysa bir kopya eksiksiz, diğerlerine içerik (katkı matrisi
	//                          hariç — o sayfadaki PÇ'ler kaynak programındır) detail_source_id ile yazılır.
	//   PoolAll    ("all"):    her kopya ayrı indirilir (programa özel matrisler dahil, ~30 bin istek).
	PoolDetails string
	// PlanYears: taranacak ders planı yılı sayısı. 0 veya 1: yalnızca sitede seçili (güncel) plan;
	// N > 1: güncel + en yeni N-1 eski plan; < 0: sayfadaki tüm yıllar. Eski planların ders ID'leri ayrıdır,
	// detayları da ayrıca indirilir (süre yıl sayısıyla katlanır).
	PlanYears int
	// FreshWithin > 0 ise, detayı bu süre içinde yazılmış dersler tekrar indirilmez.
	FreshWithin time.Duration
	OnError     func(url string, err error)

	programs, courses, skipped, errors atomic.Int64
	done                               atomic.Int64 // biten program (başarılı/başarısız)
	courseSeen                         sync.Map     // curCourse -> struct{}: aynı çalışmada bir kez indir
	unitSeen                           sync.Map     // curUnit -> struct{}: tanıtım sayfası bir kez
	poolMu                             sync.Mutex
	pools                              map[string][]poolCopy // PoolShared: ders kodu -> kopyalar
}

type Stats struct {
	Programs, Courses, Skipped, Errors int64
}

func (s *Scraper) Stats() Stats {
	return Stats{s.programs.Load(), s.courses.Load(), s.skipped.Load(), s.errors.Load()}
}

func (s *Scraper) fail(u string, err error) {
	if errors.Is(err, context.Canceled) { // kullanıcı durdurdu: çalışma zaten "interrupted" olarak kaydedilir
		return
	}
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
	if ctx.Err() == nil && !s.SkipDetails && s.PoolDetails == PoolShared {
		s.runSharedPools(ctx)
	}
	return s.Stats()
}

func (s *Scraper) program(ctx context.Context, ref parse.ProgramRef) {
	if ctx.Err() != nil {
		return
	}
	log := s.Log.With("program", ref.ID)
	q := url.Values{"curSunit": {id(ref.ID)}}

	// 1) Birim (+ tanıtım sayfası, birim başına bir kez)
	if ref.UnitID != 0 {
		if err := s.Sink.Unit(ctx, ref.UnitID, ref.UnitName, ref.Level); err != nil {
			s.fail("unit:"+id(ref.UnitID), err)
		}
		if _, dup := s.unitSeen.LoadOrStore(ref.UnitID, struct{}{}); !dup {
			s.unitAbout(ctx, ref.UnitID)
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

	// 4) Ders planı: sitede seçili (güncel) yıl, sonra istenen eski yıllar (yıl seçici postback'i ile)
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
	// Birim adı keşif sayfasında güvenilir değil; ders planı başlığı ("FAKÜLTE / PROGRAM - Dersler") kesin kaynak.
	if ref.UnitID != 0 && cur.UnitName != "" && cur.UnitName != ref.UnitName {
		if err := s.Sink.Unit(ctx, ref.UnitID, cur.UnitName, ref.Level); err != nil {
			s.fail("unit:"+id(ref.UnitID), err)
		}
	}
	if !s.plan(ctx, ref, cur, curURL, log) {
		return
	}
	s.programs.Add(1)
	for _, py := range s.olderYears(cur) {
		if ctx.Err() != nil {
			return
		}
		yu := curURL + "#" + strconv.Itoa(py.Year)
		b, err := YearPostback(ctx, s.Client, curURL, body, py.Value)
		if err != nil {
			s.fail(yu, err)
			continue
		}
		old, err := parse.ParseCurriculum(b)
		if err != nil {
			s.fail(yu, err)
			continue
		}
		if old.Year == nil || *old.Year != py.Year {
			s.fail(yu, fmt.Errorf("yıl seçilemedi: %d istendi, sayfa %v döndü", py.Year, old.Year))
			continue
		}
		old.Archived = true
		s.plan(ctx, ref, old, yu, log)
	}
}

// olderYears, PlanYears ayarına göre taranacak eski plan yıllarını (yeniden eskiye) döndürür.
func (s *Scraper) olderYears(cur *parse.Curriculum) []parse.PlanYear {
	if s.PlanYears >= 0 && s.PlanYears <= 1 {
		return nil
	}
	var old []parse.PlanYear
	for _, y := range cur.Years {
		if !y.Selected && (cur.Year == nil || y.Year != *cur.Year) {
			old = append(old, y)
		}
	}
	slices.SortFunc(old, func(a, b parse.PlanYear) int { return cmp.Compare(b.Year, a.Year) })
	if s.PlanYears > 1 && len(old) > s.PlanYears-1 {
		old = old[:s.PlanYears-1]
	}
	return old
}

// unitAbout, birimin tanıtım sayfasını (facAbout.aspx) indirip yazar.
func (s *Scraper) unitAbout(ctx context.Context, unitID int64) {
	u := s.Client.URL(PageUnitAbout, url.Values{"curUnit": {id(unitID)}})
	b, err := s.Client.Get(ctx, u)
	if err != nil {
		if !fetch.IsNotFound(err) {
			s.fail(u, err)
		}
		return
	}
	a, err := parse.ParseUnitAbout(b)
	if err != nil {
		s.fail(u, err)
		return
	}
	if err := s.Sink.UnitAbout(ctx, unitID, a); err != nil {
		s.fail(u, err)
	}
}

// plan, bir yılın ders planını (seçmeli alt sayfalarıyla) yazar ve derslerin detaylarını indirir.
// Plan yazılamadıysa false döner.
func (s *Scraper) plan(ctx context.Context, ref parse.ProgramRef, cur *parse.Curriculum, curURL string, log *slog.Logger) bool {
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
	if err := s.Sink.Curriculum(ctx, ref.ID, cur); err != nil {
		s.fail(curURL, err)
		return false
	}
	year := 0
	if cur.Year != nil {
		year = *cur.Year
	}
	log.Info("ders planı", "yıl", year, "ders", len(entries))

	if s.SkipDetails {
		return true
	}

	// 5) Ders detayları. Seçmeli havuz dersleri de bu programa aittir: OİBS her programın planına
	// kendi kopyasını (ayrı curCourse) koyar ve sayfadaki P1…Pn bu programın çıktılarıdır.
	for _, e := range entries {
		if ctx.Err() != nil {
			return true
		}
		switch {
		case e.IsGroup: // seçmeli grup satırının detay sayfası yok
		case e.GroupID != 0 && s.PoolDetails == PoolShared:
			s.addPoolCopy(e, year, ref.ID) // tüm programların planları okununca kod başına bir kez indirilir
		case e.GroupID != 0 && s.PoolDetails != PoolAll:
		default:
			s.course(ctx, ref.ID, e.CourseID)
		}
	}
	return true
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

// addPoolCopy: kopyalar ders kodu ve plan yılıyla eşleştirilir (farklı yılların içeriği farklı olabilir).
func (s *Scraper) addPoolCopy(e parse.CurriculumEntry, year int, programID int64) {
	key := strconv.Itoa(year) + "|" + e.Code
	if e.Code == "" { // kodsuz satır başka kopyalarla eşleştirilemez
		key = "id:" + id(e.CourseID)
	}
	s.poolMu.Lock()
	defer s.poolMu.Unlock()
	if s.pools == nil {
		s.pools = map[string][]poolCopy{}
	}
	for _, c := range s.pools[key] {
		if c.courseID == e.CourseID {
			return
		}
	}
	s.pools[key] = append(s.pools[key], poolCopy{e.CourseID, programID})
}

// runSharedPools, her havuz ders kodu için bir sayfa indirip tüm kopyalarına yazar.
func (s *Scraper) runSharedPools(ctx context.Context) {
	s.poolMu.Lock()
	keys := make([]string, 0, len(s.pools))
	copies := 0
	for k, cs := range s.pools {
		slices.SortFunc(cs, func(a, b poolCopy) int { return cmp.Compare(a.courseID, b.courseID) })
		keys = append(keys, k)
		copies += len(cs)
	}
	s.poolMu.Unlock()
	if len(keys) == 0 {
		return
	}
	slices.Sort(keys)
	s.Log.Info("seçmeli havuz dersleri", "kopya", copies, "farklı_kod", len(keys))
	start := time.Now()
	var done atomic.Int64
	jobs := make(chan string)
	var wg sync.WaitGroup
	for i := 0; i < max(s.Workers, 1); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for k := range jobs {
				s.poolGroup(ctx, s.pools[k])
				if n := done.Add(1); n%200 == 0 || n == int64(len(keys)) {
					eta := time.Duration(float64(time.Since(start)) / float64(n) * float64(int64(len(keys))-n)).Round(time.Second)
					s.Log.Info("havuz ilerleme", "biten_kod", n, "toplam_kod", len(keys), "ders", s.courses.Load(), "hata", s.errors.Load(), "kalan_süre", eta)
				}
			}
		}()
	}
	for _, k := range keys {
		if ctx.Err() != nil {
			break
		}
		jobs <- k
	}
	close(jobs)
	wg.Wait()
}

func (s *Scraper) poolGroup(ctx context.Context, all []poolCopy) {
	// Aynı ID başka bir planda doğrudan ders olarak zaten indirildiyse (matrisiyle) dokunma.
	var cs []poolCopy
	for _, c := range all {
		if _, dup := s.courseSeen.LoadOrStore(c.courseID, struct{}{}); !dup {
			cs = append(cs, c)
		}
	}
	if len(cs) == 0 {
		return
	}
	stale := cs
	if fc, ok := s.Sink.(FreshChecker); ok && s.FreshWithin > 0 {
		stale = nil
		since := time.Now().Add(-s.FreshWithin)
		for _, c := range cs {
			if fresh, err := fc.CourseFresh(ctx, c.courseID, since); err == nil && fresh {
				s.skipped.Add(1)
			} else {
				stale = append(stale, c)
			}
		}
		if len(stale) == 0 {
			return
		}
	}
	rep := cs[0] // en küçük ID: tekrar çalıştırmalarda aynı kopya kaynak olur
	du := s.Client.URL(PageCourseDetail, url.Values{"curCourse": {id(rep.courseID)}})
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
	for _, c := range stale {
		cd := d
		if c.courseID != rep.courseID {
			cd = d.AsCopyOf(rep.courseID)
		}
		if err := s.Sink.Course(ctx, c.programID, c.courseID, cd, du); err != nil {
			s.fail(du, err)
			continue
		}
		s.courses.Add(1)
	}
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
func (j *JSONSink) UnitAbout(_ context.Context, id int64, a *parse.UnitAbout) error {
	return j.emit("unit_about", map[string]any{"id": id, "about": a})
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
