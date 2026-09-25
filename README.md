# bologna-scraper

Mersin Üniversitesi OİBS Bologna Bilgi Paketi'ni (`obs.mersin.edu.tr/oibs/bologna`) tarayıp
ilişkileri koruyarak PostgreSQL'e yazan Go CLI.

## Kurulum

```bash
go mod tidy                 # go.sum üretir (Go 1.22+)
make db                     # docker ile PostgreSQL 16 (isteğe bağlı)
make build                  # bin/bologna
export BOLOGNA_DB_URL="postgres://bologna:bologna@localhost:5432/bologna?sslmode=disable"
bin/bologna migrate
```

## Komutlar

| Komut | Ne yapar |
|---|---|
| `bologna migrate` | Şemayı oluşturur (idempotent; `scrape` da otomatik çalıştırır) |
| `bologna discover -l lisans [--json]` | Birim/programları listeler, DB'ye yazmaz |
| `bologna scrape -p 352` | Tek program (curSunit) |
| `bologna scrape -l lisans,onlisans -w 3` | Seviye(ler)in tümü |
| `bologna scrape --all --rps 1.5` | Her şey |
| `bologna scrape --from-file progs.json` | `discover --json` çıktısından |
| `bologna scrape -p 352 --dry-run \| jq` | DB'ye yazmadan JSON |
| `bologna inspect "progCourses.aspx?curSunit=352"` | Sayfayı parser'ın gözünden gösterir |
| `bologna inspect <url> --as course\|about\|curriculum\|units` | İlgili parser'ın JSON çıktısı |
| `bologna stats` | Tablo kayıt sayıları |

Ortak bayraklar: `--rps` (varsayılan 2 istek/sn, **tüm worker'lar toplamı**), `--retries`,
`--timeout`, `--cache-dir ./cache` + `--use-cache` (parser geliştirirken sunucuya tekrar gitmemek için),
`-v` (debug log), `--log-json`. Ctrl+C güvenle durdurur; çalışma `scrape_runs`'a `interrupted` yazılır.

## Veri modeli

```
units (curUnit) 1─N programs (curSunit) 1─N program_outcomes (PÇ no)
                          │                            │
                          N─M courses (curCourse) ──── course_program_outcomes (katkı 1-5)
                   program_courses                │
                   (yarıyıl, Z/S)                 ├─ course_learning_outcomes
                                                  ├─ course_weekly_topics
                                                  ├─ course_resources
                                                  ├─ course_assessments
                                                  └─ course_workloads
```

- Anahtarlar OİBS'nin kendi ID'leri → tekrar çalıştırmak kopya üretmez, günceller.
- Bir ders birden çok programın planında olabilir (`program_courses`); `courses.program_id` dersin sahibi olan programdır ve katkı matrisi o programın çıktılarına bağlanır.
- Program çıktısı açıklamaları üç kaynaktan gelir, öncelik sırası: `outcomes_page` > `course_matrix` > `about_text` (`program_outcomes.source`).
- Tanınmayan bölümler kaybolmaz: `programs.sections`, `programs.info`, `courses.details` (JSONB).
- Hazır view'lar: `v_curriculum`, `v_course_outcome_matrix`. Hatalar: `scrape_errors`.

## Parser nasıl çalışır / siteye uyarlama

Sayfalar ASP.NET; ID/class yerine her tablo **önündeki başlık metniyle** etiketlenir
("Dersin Öğrenme Çıktıları", "Değerlendirme Sistemi"...) ve sütunlar başlık satırından bulunur.
Sayfa adları `internal/scraper/scraper.go` başındaki sabitlerde, seviye→`type` eşlemesi `LevelTypes`'ta.
Bir bölüm yanlış okunursa: `bologna inspect <url>` ile tablo etiketlerine bakın, `internal/parse/`'daki
ilgili `Norm` kalıbını düzeltin, gerçek HTML'i `--save` ile `testdata`'ya ekleyip test yazın.

## Testler

`go test ./...` — `internal/parse/testdata` altındaki fixture'lar sentetiktir (gerçek sayfa yapısı örnek alınarak yazıldı).
Gerçek sayfaları `inspect --save` ile kaydedip fixture olarak eklemeniz önerilir.
