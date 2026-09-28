# bologna-scraper

Mersin Üniversitesi **OİBS Bologna Bilgi Paketi**'ni
([obs.mersin.edu.tr/oibs/bologna](https://obs.mersin.edu.tr/oibs/bologna/index.aspx)) tarayan ve
tüm veriyi **ilişkileri koruyarak PostgreSQL'e** yazan bir Go komut satırı aracı (CLI).

Taranan veriler:

- **Birimler:** fakülte, yüksekokul, MYO ve enstitüler, tanıtım metinleriyle birlikte.
- **Programlar:** önlisans, lisans, yüksek lisans ve doktora. Program bilgileri, kabul ve mezuniyet koşulları, istihdam vb.
- **Program çıktıları** (program yeterlilikleri).
- **Ders planları:** yarıyıl, zorunlu/seçmeli, AKTS, T+U+L ve öğretim şekli. Seçmeli grup → havuz dersi ilişkisi dahil. Sitedeki **eski yılların planları** da istenirse alınır.
- **Ders detayları:**
  - amaç, içerik, dil, düzey, koordinatör, öğretim elemanları
  - öğrenme çıktıları, haftalık konular, kaynaklar
  - değerlendirme ve AKTS iş yükü
  - **öğrenme çıktısı × program çıktısı katkı matrisi**

Sitenin tamamı 2026-09'da taranarak doğrulandı: 4 seviye, 442 program, tüm plan yıllarıyla 95.432 ders ve 0 hata.
Ayrıntılar aşağıdaki [Tam tarama](#6-tam-tarama-süre-ve-sonuçlar) bölümünde; projenin süreci ve kararları [RAPOR.md](RAPOR.md) dosyasında.

---

## İçindekiler

1. [Hızlı başlangıç: Docker](#1-hızlı-başlangıç-docker)
2. [Kurulum: Windows](#2-kurulum-windows)
3. [Kurulum: Linux / macOS](#3-kurulum-linux--macos)
4. [Kullanım](#4-kullanım)
5. [Veri modeli ve örnek sorgular](#5-veri-modeli-ve-örnek-sorgular)
6. [Tam tarama: süre ve sonuçlar](#6-tam-tarama-süre-ve-sonuçlar)
7. [Sitenin özellikleri ve tasarım kararları](#7-sitenin-özellikleri-ve-tasarım-kararları)
8. [Geliştirme ve testler](#8-geliştirme-ve-testler)
9. [Bilinen sınırlar](#9-bilinen-sınırlar)

---

## 1. Hızlı başlangıç: Docker

En kolay yol budur. Yalnızca [Docker Desktop](https://www.docker.com/products/docker-desktop/) gerekir;
Go veya PostgreSQL kurmanıza gerek yoktur.

```bash
git clone https://github.com/mehmetfatihvar/staj.git
cd staj/staj/bologna-scraper

# 1) PostgreSQL'i başlat (arka planda çalışır, veriler "pgdata" volume'unda kalır)
docker compose up -d db

# 2) Tek bir programla dene (~1 dk): Bilgisayar Mühendisliği
docker compose --profile scrape run --rm scraper scrape -p 1371

# 3) Tüm siteyi tara (~3,5 saat, bkz. "Tam tarama")
docker compose --profile scrape run --rm scraper scrape --all --pool-details

# 4) Sonuçlar
docker compose --profile scrape run --rm scraper stats
docker compose exec db psql -U bologna -d bologna      # SQL ile sorgula (\q ile çık)
```

Veritabanı bağlantısı: `postgres://bologna:bologna@localhost:5432/bologna`. DBeaver veya pgAdmin gibi bir araçla da
bağlanabilirsiniz. Bilgisayarınızda 5432 portunu kullanan başka bir PostgreSQL varsa `docker-compose.yml`
içindeki `"5432:5432"` satırını `"5433:5432"` yapın.

Kapatmak için: `docker compose down`. Verileri de silmek için: `docker compose down -v`.

---

## 2. Kurulum: Windows

### 2.1 Gerekenler

| Yazılım | İndirme | Not |
|---|---|---|
| Git | https://git-scm.com/download/win | |
| Go 1.22+ | https://go.dev/dl/ | Kurulumdan sonra **yeni** bir cmd açın, `go version` çalışmalı |
| PostgreSQL 16+ | https://www.postgresql.org/download/windows/ | Kurulumda `postgres` kullanıcısına bir şifre verin |

### 2.2 Derleme

```bat
git clone https://github.com/mehmetfatihvar/staj.git
cd staj\staj\bologna-scraper
go build -o bologna.exe ./cmd/bologna
bologna.exe --help
```

### 2.3 Veritabanı

PostgreSQL kurulum programı bir Windows servisi kurar ve servis otomatik başlar. Önce boş bir veritabanı oluşturun
(şifre sorar):

```bat
"C:\Program Files\PostgreSQL\18\bin\createdb" -U postgres bologna
```

> Sürüm numarası farklıysa (16, 17…) yoldaki `18`'i değiştirin.

Bağlantı adresini **kalıcı olarak** kaydedin. `ŞİFRE` yerine kurulumda verdiğiniz şifreyi yazın, sonra **yeni bir cmd
açın**:

```bat
setx BOLOGNA_DB_URL "postgres://postgres:ŞİFRE@localhost:5432/bologna?sslmode=disable"
```

### 2.4 Çalıştırma

```bat
bologna.exe migrate
bologna.exe scrape -p 1371
bologna.exe stats
```

`migrate` tabloları oluşturur; `scrape` da bunu otomatik yapar. `scrape -p 1371` tek bir programı (~1 dk) tarar.
Tüm site için `bologna.exe scrape --all --pool-details` çalıştırın; bkz. [Kullanım](#4-kullanım).

SQL ile bakmak için:

```bat
chcp 1254
"C:\Program Files\PostgreSQL\18\bin\psql" -U postgres -d bologna
```

`chcp 1254`, Türkçe karakterlerin cmd'de doğru görünmesi içindir.

<details>
<summary>Servis olmadan (kurulum programı kullanmadan, zip ile) PostgreSQL</summary>

```bat
"C:\Program Files\PostgreSQL\18\bin\initdb" -D %USERPROFILE%\pgdata -U postgres -A trust -E UTF8 --locale=C --locale-provider=icu --icu-locale=tr-TR
"C:\Program Files\PostgreSQL\18\bin\pg_ctl" -D %USERPROFILE%\pgdata -l %USERPROFILE%\pgdata\log.txt start
"C:\Program Files\PostgreSQL\18\bin\createdb" -U postgres bologna
setx BOLOGNA_DB_URL "postgres://postgres@localhost:5432/bologna?sslmode=disable"
```

- `--locale=C --locale-provider=icu`: Türkçe Windows'ta, yerel ayar adındaki Türkçe karakter yüzünden `initdb`'nin hata vermesini önler.
- Bu şekilde başlatılan sunucu, bilgisayar yeniden başlayınca durur. Tekrar başlatmak için `pg_ctl ... start` satırını yeniden çalıştırın.
</details>

---

## 3. Kurulum: Linux / macOS

```bash
git clone https://github.com/mehmetfatihvar/staj.git
cd staj/staj/bologna-scraper
make build                 # bin/bologna
make db                    # docker ile PostgreSQL (veya kendi PostgreSQL'iniz)
export BOLOGNA_DB_URL="postgres://bologna:bologna@localhost:5432/bologna?sslmode=disable"
bin/bologna scrape -p 1371
```

---

## 4. Kullanım

### 4.1 Komutlar

| Komut | Ne yapar |
|---|---|
| `bologna discover -l lisans` | Seviyedeki birim ve programları listeler (DB'ye yazmaz, birkaç saniye) |
| `bologna scrape -p 1371` | Tek program (`curSunit`). Tarayıcıdaki adres de verilebilir: `-p "https://obs.mersin.edu.tr/oibs/bologna/index.aspx?...&curSunit=1371"` |
| `bologna scrape -l lisans` | Bir seviye (`onlisans`, `lisans`, `yukseklisans`, `doktora`; virgülle birden çoğu) |
| `bologna scrape --all --pool-details` | **Tüm site**. Önerilen tam tarama |
| `bologna scrape --all --pool-details --years all` | Ayrıca sitedeki **eski yılların ders planları** |
| `bologna scrape --all --pool-details --fresh-within 24h` | Kesilen taramayı sürdürür: son 24 saatte yazılmış dersleri atlar |
| `bologna scrape -p 1371 --dry-run` | DB'ye yazmadan JSON olarak ekrana basar |
| `bologna stats` | Tablo kayıt sayıları |
| `bologna migrate` | Şemayı oluşturur/günceller (`scrape` da otomatik yapar; tekrar çalıştırmak güvenlidir) |
| `bologna inspect "progCourses.aspx?curSunit=1371"` | Bir sayfayı parser'ın gözünden gösterir (`--as course\|about\|curriculum\|units`, `--save x.html`, `--year 2025`) |

### 4.2 `scrape` seçenekleri

| Seçenek | Varsayılan | Açıklama |
|---|---|---|
| `--pool-details` | kapalı | Seçmeli havuz derslerinin detay sayfaları. Her ders kodu **bir kez** indirilir (bkz. §7). `--pool-details=all` her kopyayı ayrı indirir (~30 bin sayfa daha) |
| `--years N` / `--years all` | `1` | Ders planı yılı sayısı. `1` = yalnızca güncel plan, `3` = güncel + 2 eski, `all` = sitedeki tüm yıllar. Eski planların dersleri de ayrı detaylarıyla alınır, **süre yıl sayısıyla katlanır** |
| `--fresh-within 24h` | – | Detayı bu süre içinde yazılmış dersleri atla (kesilen taramayı sürdürmek için) |
| `--skip-details` | kapalı | Yalnızca program ve ders planı (ders detay sayfaları yok; çok hızlı) |
| `-w, --workers` | `2` | Paralel işlenen program sayısı |
| `--rps` | `2` | Saniyedeki azami istek (**tüm worker'ların toplamı**). Siteye yük bindirmemek için 2'nin üstü önerilmez |
| `--dry-run` | kapalı | Veritabanına yazma, JSON satırları bas |
| `--from-file x.json` | – | `discover --json` çıktısındaki programları tara |

Ortak seçenekler: `--db` (veya `BOLOGNA_DB_URL`), `--retries`, `--timeout`, `-v` (ayrıntılı log), `--log-json`,
`--cache-dir ./cache --use-cache` (parser geliştirirken sayfaları diskten okumak için).

### 4.3 Çalışırken

- Her program bitince `ilerleme biten=… toplam=… kalan_süre=…` satırı basılır. Havuz aşamasında da `havuz ilerleme` basılır.
- **Ctrl+C** taramayı güvenle durdurur (`scrape_runs.status = interrupted`). Aynı komutu `--fresh-within 24h` ile
  tekrar çalıştırırsanız kaldığı yerden devam eder.
- Tekrar çalıştırmak kopya üretmez: kayıtlar sitenin kendi ID'leriyle güncellenir.
- Sunucu uzun taramada 503 dönebilir. Bu durumda istemci **tüm istekleri** 30 sn bekletir; hata sürerse bekleme 5 dk'ya
  kadar katlanır. Hatalar `scrape_errors` tablosuna yazılır.

---

## 5. Veri modeli ve örnek sorgular

```
units (curUnit) ──1:N── programs (curSunit) ──1:N── program_outcomes (PÇ1, PÇ2…)
  │ about                     │ curriculum_year                  │
  │                           │                                  │
  │                   program_courses (plan satırı)              │
  │                   yarıyıl, Z/S, plan_year,                   │
  │                   seçmeli grup ─► havuz dersi                │
  │                           │                                  │
  │                        courses (curCourse) ──────────────────┤
  │                           ├─ course_learning_outcomes (ÖÇ1, ÖÇ2…)
  │                           ├─ course_weekly_topics
  │                           ├─ course_resources
  │                           ├─ course_assessments
  │                           ├─ course_workloads
  │                           ├─ course_outcome_matrix   (ÖÇ × PÇ → katkı 1-5)
  │                           └─ course_program_outcomes (PÇ başına en yüksek katkı)
scrape_runs ──1:N── scrape_errors
```

Tüm ilişkiler foreign key ile tanımlı. Hazır görünümler (view):

| View | İçerik |
|---|---|
| `v_curriculum` | Ders planı: birim, program, yarıyıl, ders, AKTS, seçmeli grup kodu/adı, `plan_year`, `is_current` |
| `v_course_lo_po_matrix` | Ders → öğrenme çıktısı → program çıktısı → katkı düzeyi (açıklamalarıyla) |
| `v_course_outcome_matrix` | Ders → program çıktısı → en yüksek katkı |

Önemli sütunlar:

| Sütun | Anlamı |
|---|---|
| `programs.curriculum_year` | Sitede seçili (güncel) ders planı yılı |
| `program_courses.plan_year`, `plan_id` | Satırın ait olduğu plan yılı ve sitedeki plan kimliği (eski planlar `--years` ile; bazı programlarda aynı yıla ait birden çok plan vardır, bunlar `plan_id` ile ayrılır) |
| `program_courses.is_elective_group`, `group_course_count` | Seçmeli grup satırı ve gruptan seçilecek ders sayısı |
| `program_courses.elective_group_id` | Havuz dersinin bağlı olduğu grup |
| `courses.program_id` | Dersin sahibi olan program (katkı matrisi bu programın çıktılarına bağlanır) |
| `courses.detail_source_id` | Havuz kopyasının içeriğinin alındığı ders (bkz. §7) |
| `courses.detail_empty` | Sitede ders sayfası boş ("Kayıt Yok...") |
| `courses.details`, `programs.sections` | Sayfadaki tüm alanlar ham olarak (JSONB). Tanınmayan bölümler de kaybolmaz |

Örnek sorgular:

```sql
-- Bir programın güncel ders planı
SELECT semester, code, course_name, course_type, ects, elective_group_name
FROM v_curriculum WHERE program_id = 1371 AND is_current ORDER BY semester, code;

-- Bir dersin öğrenme çıktısı × program çıktısı katkı matrisi
SELECT lo_no, learning_outcome, outcome_no, contribution
FROM v_course_lo_po_matrix WHERE course_id = 1098805 ORDER BY lo_no, outcome_no;

-- Programların toplam AKTS'si (güncel plan, grup satırları hariç zorunlu dersler)
SELECT program_name, sum(ects) FROM v_curriculum
WHERE is_current AND course_type ILIKE 'zorunlu%' GROUP BY program_name ORDER BY 2 DESC;

-- Bir dersin haftalık konuları
SELECT week, topic FROM course_weekly_topics WHERE course_id = 1098805 ORDER BY week;

-- Birim tanıtımları
SELECT name, left(about, 120) FROM units ORDER BY name;
```

---

## 6. Tam tarama: süre ve sonuçlar

2026-09-26'da `--pool-details` ile, 2 istek/sn hızında:

| Seviye | Program | Süre | Hata |
|---|---|---|---|
| Lisans | 153 | ~1 sa 40 dk (havuz dersleri dahil) | 0 |
| Önlisans | 88 | 35 dk | 0 |
| Yüksek lisans + Doktora | 201 | 63 dk | 0 |
| **Toplam** | **442** | **~3,5 saat** | **0** |

Sonuç (yalnızca güncel planlar): 45 birim, 442 program, 4.598 program çıktısı, 50.576 ders, 233 bin öğrenme
çıktısı, 595 bin haftalık konu ve 556 bin katkı matrisi hücresi.

### Eski plan yılları dahil (`--years all`)

2026-09-27'de `scrape --all --pool-details --years all` ile her programın sitedeki tüm plan yılları alındı:

| Tablo | Satır |
|---|---|
| units | 45 (tanıtım metinleriyle) |
| programs | 442 |
| program_outcomes | 4.598 |
| courses | 95.432 |
| program_courses | 95.432 |
| course_learning_outcomes | 436.894 |
| course_weekly_topics | 1.115.829 |
| course_resources | 160.291 |
| course_assessments | 160.682 |
| course_workloads | 361.997 |
| course_program_outcomes | 240.462 |
| course_outcome_matrix | 930.383 |
| scrape_errors | 0 |

`courses` ile `program_courses` sayılarının eşit olması, her dersin tam olarak bir plana bağlandığını gösterir.

**Boş veritabanıyla beklenen süreler** (2 istek/sn):

| Komut | Süre |
|---|---|
| `scrape --all --pool-details` (güncel planlar) | ~3,5 saat |
| `scrape --all --pool-details --years all` (tüm yıllar) | ~7 saat |
| Aynı komutu `--fresh-within 168h` ile tekrar (yalnızca eksikler) | ~20-30 dk |

Uzun taramayı gece çalıştırmanız önerilir. Kesilirse aynı komutu `--fresh-within 24h` ekleyerek
yeniden başlatın; daha önce yazılmış dersler atlanır.

---

## 7. Sitenin özellikleri ve tasarım kararları

- **ASP.NET sayfaları:** tablolar ID/class ile değil, **önlerindeki başlık metniyle** ("Dersin Öğrenme Çıktıları",
  "AKTS Hesaplama İçeriği"…) tanınır. Sütunlar başlık satırından bulunur.
- **Ders linkleri:** `href` değil, `onclick="prolizOpenCourseDetails(ID)"`.
- **Ders planı:** tek bir tablodur. "1.Yarıyıl Ders Planı" satırları yarıyılı belirtir; `collapse_<grup>` sınıfı
  havuz üyeliğini gösterir.
- **Seçmeli havuzlar:** OİBS ortak seçmeli dersleri **her programa ayrı ID ile kopyalar** (lisansta 29.616 kopya,
  4.574 farklı ders). `--pool-details` her ders kodunu **bir kez** indirir, en küçük ID'li kopyayı tam yazar,
  diğer kopyalara aynı içeriği yazar (`detail_source_id` kaynağı gösterir). Katkı matrisi kopyalara yazılmaz, çünkü
  sayfadaki P1…Pn kaynak programın çıktılarıdır. Bu yöntemle istek sayısı ~30 binden ~4.600'e iner.
- **Eski yıllar:** ders planı sayfasındaki yıl seçici bir ASP.NET form gönderimidir (postback). Scraper formu
  (ViewState dahil) tarayıcı gibi geri gönderir. Her planın **ayrı ders ID'leri** vardır, bu yüzden eski planlar
  aynı tablolarda karışmadan durur. Bazı programlarda aynı yıla ait birden çok plan bulunur (ör. iki ayrı "2026");
  planlar bu yüzden yılla değil sitedeki plan kimliğiyle (`plan_id`) ayırt edilir. Site seçilen planı ASP.NET
  oturumunda tuttuğundan her programın ders planı kendi oturumunda istenir (paylaşılan oturumda başka
  programın seçimi sayfayı boş döndürür).
- **Boş sayfalar:** içeriği girilmemiş derslerde site "Kayıt Yok..." gösterir; bunlar `detail_empty = true` ile
  işaretlenir (lisansta ~3.300 ders).
- **Kodlama:** sayfalarda `<meta charset>` yoktur; geçerli UTF-8 değilse `windows-1254` kabul edilir.
- **Nezaket:** varsayılan 2 istek/sn, 503/429'da geri çekilme ve bekleme, tekrar denemeler.

---

## 8. Geliştirme ve testler

```
cmd/bologna/        main
internal/cli/       komutlar (cobra)
internal/fetch/     HTTP istemcisi: hız sınırı, tekrar deneme, bekleme, çerez, POST (postback), kodlama
internal/parse/     HTML → Go yapıları (sayfa türü başına bir parser)
internal/scraper/   tarama akışı: keşif, program, yıllar, ders detayları, havuzlar
internal/store/     PostgreSQL (schema.sql + upsert'ler)
internal/parse/testdata/real/   sitenin gerçek sayfaları (testlerde kullanılır)
```

```bash
make test               # parser, HTTP istemcisi, uçtan uca scraper (gerçek sayfaları sunan test sunucusu)
make test-integration   # + PostgreSQL (BOLOGNA_TEST_DB_URL; geçici şemada çalışır, sonunda siler)
make lint               # gofmt + go vet
```

GitHub Actions (`.github/workflows/bologna-scraper.yml`) her push'ta lint, PostgreSQL'li testler ve derlemeyi çalıştırır.

**Site değişirse:** `bologna inspect <sayfa> --save x.html` ile sayfayı indirin ve tablo etiketlerine bakın.
`internal/parse/` içindeki ilgili kalıbı düzeltin, sayfayı `testdata/real/` altına ekleyip test yazın.
Sayfa adları `internal/scraper/scraper.go` başındaki sabitlerdedir.

---

## 9. Bilinen sınırlar

- **İngilizce sürüm** (`lang=en`) taranmaz. Parser Türkçe başlıklara göre ayarlıdır.
- Bologna ana menüsündeki **üniversite geneli bilgi sayfaları** (şehir, barınma vb.) taranmaz. Yalnızca birim,
  program ve ders sayfaları taranır.
- **"Ders Yapısı"** yüzdeleri (ör. "Sosyal Bilimler %50") ayrı tabloya ayrılmaz, `courses.details` içinde metin olarak durur.
- Siteden kaldırılan program veya dersler veritabanından otomatik silinmez. Bir programın planı ise her taramada
  yeniden yazılır; plandan çıkan ders ilişkisi silinir.
