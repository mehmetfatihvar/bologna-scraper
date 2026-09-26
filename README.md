# bologna-scraper

Mersin Üniversitesi OİBS Bologna Bilgi Paketi'ni (`obs.mersin.edu.tr/oibs/bologna`) tarayıp
ilişkileri koruyarak PostgreSQL'e yazan Go CLI.

## Kurulum

```bash
# Go 1.22+
go install github.com/mehmetfatihvar/staj/staj/bologna-scraper/cmd/bologna@latest
# veya repo içinden:
make db                     # docker ile PostgreSQL 16 (isteğe bağlı)
make build                  # bin/bologna
export BOLOGNA_DB_URL="postgres://bologna:bologna@localhost:5432/bologna?sslmode=disable"
bin/bologna migrate
```

Docker ile hepsi birden: `docker compose run --rm scraper scrape --all`

## Komutlar

| Komut | Ne yapar |
|---|---|
| `bologna migrate` | Şemayı oluşturur (idempotent; `scrape` da otomatik çalıştırır) |
| `bologna discover -l lisans [--json]` | Birim/programları listeler, DB'ye yazmaz |
| `bologna scrape -p 352` | Tek program (curSunit) |
| `bologna scrape -p "https://obs.mersin.edu.tr/oibs/bologna/index.aspx?...&curUnit=12&curSunit=352"` | Tarayıcıdaki adresi doğrudan verin |
| `bologna scrape -l lisans,onlisans -w 3` | Seviye(ler)in tümü |
| `bologna scrape --all --rps 1.5` | Her şey |
| `bologna scrape --all --fresh-within 24h` | Kesilen taramayı sürdürür: son 24 saatte yazılmış ders detaylarını atlar |
| `bologna scrape -l lisans --pool-details` | Seçmeli havuz detayları: her ders kodu **bir kez** indirilir, kopyalara yazılır (varsayılan: indirmez) |
| `bologna scrape -l lisans --pool-details=all --rps 1` | Havuz detayları: her kopya ayrı indirilir (~30 bin sayfa) |
| `bologna scrape --from-file progs.json` | `discover --json` çıktısından |
| `bologna scrape -p 352 --dry-run \| jq` | DB'ye yazmadan JSON |
| `bologna inspect "progCourses.aspx?curSunit=352"` | Sayfayı parser'ın gözünden gösterir |
| `bologna inspect <url> --as course\|about\|curriculum\|units` | İlgili parser'ın JSON çıktısı |
| `bologna stats` | Tablo kayıt sayıları |

Ortak bayraklar: `--rps` (varsayılan 2 istek/sn, **tüm worker'lar toplamı**), `--retries`,
`--timeout`, `--cache-dir ./cache` + `--use-cache` (parser geliştirirken sunucuya tekrar gitmemek için),
`-v` (debug log), `--log-json`. Ctrl+C güvenle durdurur; çalışma `scrape_runs`'a `interrupted` yazılır.
Her program bitince `ilerleme` logu (biten/toplam, kalan süre tahmini) basılır.

HTTP katmanı: ASP.NET oturum çerezi saklanır, `windows-1254`/`ISO-8859-9` sayfalar UTF-8'e çevrilir,
429/5xx'te üstel geri çekilme + `Retry-After` uygulanır, 404 tekrar denenmez. Bir istek tüm denemelere
rağmen sunucu kaynaklı başarısız olursa **tüm istekler** 30 sn durur; hata sürerse bekleme 5 dk'ya kadar katlanır.

**Seçmeli havuz dersleri:** Varsayılan taramada havuz dersleri plan satırı olarak yazılır (hangi gruba ait,
grupta kaç ders seçilir, AKTS, saatler, öğretim şekli) ama **detay sayfaları indirilmez**. Havuzlar her programa
kopyalanmış ortak seçmeli derslerdir: lisans planları ~38 bin satırdır, bunun ~32 bini havuz dersidir. Varsayılan
tam lisans taraması (~6 bin ders sayfası) yaklaşık 1 saat, sonraki güncellemeler (`--fresh-within`) birkaç
dakika sürer.

Havuz detayları için `--pool-details` (= `shared`): programlar bittikten sonra her farklı havuz ders kodu
**bir kez** indirilir (lisansta 29.616 kopya yerine ~4.600 sayfa, 2 istek/sn ile ~40 dk). Aynı kodlu kopyaların
sayfaları aynıdır; yalnızca "Bölümü / Programı" alanı ve katkı matrisindeki P1…Pn (o programın çıktıları) farklıdır.
Bu yüzden en küçük ID'li kopya kaynak olur ve tam yazılır; diğer kopyalara içerik (öğrenme çıktıları, haftalık
konular, değerlendirme, iş yükü...) aynen yazılır, `courses.detail_source_id` kaynağı gösterir, **katkı matrisi
yazılmaz** (havuz derslerinin matrisi sitede de genelde boştur). Her kopyanın kendi programına ait matrisi
gerekiyorsa `--pool-details=all` her kopyayı ayrı indirir (~30 bin sayfa; tercihen gece, `--rps 1`).

## Veri modeli

```
units (curUnit)  1─N programs (curSunit) 1─N program_outcomes (PÇ no)
                          │                            │
                          N─M courses (curCourse) ──── course_program_outcomes (katkı 1-5)
                   program_courses                │
                   (yarıyıl, Z/S, seçmeli grup)   ├─ course_learning_outcomes
                                                  ├─ course_weekly_topics
                                                  ├─ course_resources
                                                  ├─ course_assessments
                                                  └─ course_workloads
```

- Anahtarlar OİBS'nin kendi ID'leri → tekrar çalıştırmak kopya üretmez, günceller.
  Ders planı ve dersin alt tabloları her yazımda **değiştirilir** (plandan çıkan ders ilişkisi silinir).
- Bir ders birden çok programın planında olabilir (`program_courses`); detay sayfası bir çalışmada
  yalnızca bir kez indirilir. `courses.program_id` dersin sahibidir ve katkı matrisi o programın çıktılarına bağlanır.
  OİBS seçmeli havuz derslerini (ör. OSD GÜZ2) her programın planına **ayrı kopya** (ayrı `curCourse`) olarak koyar;
  sayfalarındaki P1…Pn o programın çıktılarıdır, bu yüzden havuz dersleri de planında göründükleri programa aittir.
- Sitede boş olan katkı hücreleri ("katkı yok") satır olarak yazılmaz.
- Seçmeli dersler iki katmanlıdır: grup satırı (`program_courses.is_elective_group`, kaç ders
  seçileceği `group_course_count`, ör. "NORMAL EĞİTİM PAKETİ" → 5) ve ona `elective_group_id` ile
  bağlı havuz dersleri. `v_curriculum` grup kodu/adını da gösterir. Grup satırlarının detay sayfası yoktur.
- `programs.curriculum_year`: taranan ders planının yılı (sitede seçili olan, genelde en güncel).
- `courses.detail_empty`: sitede ders sayfası boş ("Dersin Ayrıntıları: Kayıt Yok..."). Lisansta öğrenme
  çıktısı olmayan havuz derslerinin neredeyse tamamı böyledir; kod, ad, AKTS ve saatler yine ders planından gelir.
- `units.levels` bir dizidir: enstitüler hem yüksek lisans hem doktora programı barındırır.
- Program çıktısı açıklamaları üç kaynaktan gelir, öncelik sırası: `outcomes_page` > `course_matrix` > `about_text` (`program_outcomes.source`).
  Resmî çıktı sayfası geldiğinde metinden tahmin edilip listede olmayan maddeler silinir.
- Katkı matrisi üç düzende okunur: `No | Çıktı | Düzey`, `No | Çıktı | 1..5 (X işaretli)` ve yatay `PÇ1 | PÇ2 | …` (ÖÇ×PÇ ise her PÇ için en yüksek düzey).
- Tanınmayan bölümler kaybolmaz: `programs.sections`, `programs.info`, `courses.details` (JSONB).
- Ders ↔ program çıktısı katkısı iki düzeyde tutulur: `course_outcome_matrix` sitedeki ÖÇ×PÇ matrisinin
  tamamı (hangi öğrenme çıktısı hangi PÇ'ye ne katkı veriyor), `course_program_outcomes` PÇ başına en yüksek düzey.
- Hazır view'lar: `v_curriculum`, `v_course_outcome_matrix`, `v_course_lo_po_matrix`. Hatalar: `scrape_errors`.

## Gerçek siteye dair notlar

- Ders linkleri `href` değil `onclick="prolizOpenCourseDetails(ID)"`; ID `progCourseDetails.aspx?curCourse=ID`'ye gider.
- Ders planı tek tablodur; "1.Yarıyıl Ders Planı" satırları yarıyılı, `collapse_<grup>` sınıfı havuz üyeliğini belirtir.
- Keşif sayfasında birim adı `<h5 class="panel-title">` içindedir, ID'si aynı başlıktaki `facAbout.aspx?curUnit=`
  bilgi ikonundadır. Ders planı başlığı ("MÜHENDİSLİK FAKÜLTESİ / BİLGİSAYAR MÜHENDİSLİĞİ - Dersler") ikinci kaynaktır.
- Sayfalarda `<meta charset>` yok; sunucu başlığı yoksa geçerli UTF-8 olduğu varsayılır.
- "Ortak seçmeli" havuzları (ör. OSD GÜZ2, ~100 ders) her programda aynı dersleri içerir ama her programa ayrı
  ID'lerle kopyalanmıştır (lisansta 29.616 havuz satırı, 4.574 farklı kod); bu yüzden varsayılan taramada detayları
  indirilmez, `--pool-details` ise her kodu bir kez indirir.
- Uzun taramalarda (~1 saat, binlerce istek) sunucu 503 dönmeye başlayabilir; istemci bu durumda tüm istekleri bekletir.

## Parser nasıl çalışır / siteye uyarlama

Sayfalar ASP.NET; ID/class yerine her tablo **önündeki başlık metniyle** etiketlenir
("Dersin Öğrenme Çıktıları", "Değerlendirme Sistemi"...) ve sütunlar başlık satırından bulunur.
Sayfa adları `internal/scraper/scraper.go` başındaki sabitlerde, seviye→`type` eşlemesi `LevelTypes`'ta.
Bir bölüm yanlış okunursa: `bologna inspect <url>` ile tablo etiketlerine bakın, `internal/parse/`'daki
ilgili `Norm` kalıbını düzeltin, gerçek HTML'i `--save` ile `testdata`'ya ekleyip test yazın.

## Testler

```bash
make test               # parser, HTTP istemcisi (httptest), scraper uçtan uca (fixture sunucusu), CLI
make test-integration   # + PostgreSQL (BOLOGNA_TEST_DB_URL; geçici şema açar, sonunda siler)
make lint
```

CI: `.github/workflows/bologna-scraper.yml` (PostgreSQL servisiyle).

`internal/parse/testdata/real/` gerçek OİBS sayfalarıdır (Bilgisayar Mühendisliği, 1371; ViewState
kırpıldı). Beş sayfa türünün (program bilgisi, program yeterlilikleri, ders planı, ders detayı
ve keşif (unitSelection) parser'ları bunlara karşı test edilir; kök dizindeki diğer fixture'lar
sentetik uç durumlardır.
Yeni gerçek sayfa eklemek için:
>
> ```bash
> bologna inspect "unitSelection.aspx?type=lis" --as units --save internal/parse/testdata/real_units.html
> bologna inspect "progCourses.aspx?curSunit=<id>" --save internal/parse/testdata/real_courses.html
> bologna inspect "progCourseDetails.aspx?curCourse=<id>" --as course
> ```
