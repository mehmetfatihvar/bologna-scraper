# Bologna Bilgi Paketi Tarayıcısı — Proje Raporu

**Proje:** Mersin Üniversitesi OİBS Bologna Bilgi Paketi → PostgreSQL aktarımı
**Depo:** https://github.com/mehmetfatihvar/staj (klasör: `staj/bologna-scraper`)
**Dil / araçlar:** Go, PostgreSQL, Docker
**Tarih:** Eylül 2026

---

## 1. Özet

Bu projede, Mersin Üniversitesi'nin Bologna Bilgi Paketi sitesindeki
(`https://obs.mersin.edu.tr/oibs/bologna/index.aspx`) tüm akademik verinin, aralarındaki ilişkiler
korunarak PostgreSQL veritabanına aktarılması için Go ile yazılmış bir komut satırı aracı
geliştirildi. Araç; birimleri, programları, program çıktılarını, ders planlarını (eski yıllar dahil) ve
her dersin öğrenme çıktılarından AKTS iş yüküne, program çıktılarına katkı matrisine kadar tüm
detaylarını tarar ve ilişkisel bir şemaya yazar.

Sitenin tamamı dört seviyede (önlisans, lisans, yüksek lisans, doktora) taranarak doğrulandı. Son tam
taramada 45 birim, 442 program, tüm plan yıllarıyla 95.432 ders ve 930 bini aşkın katkı matrisi hücresi
veritabanına yazıldı; tarama sırasında hiç hata kaydedilmedi. Proje, Docker ile tek komutla ya da
Windows/Linux üzerinde doğrudan kurulup çalıştırılabilecek biçimde, Türkçe kullanım kılavuzu ve
otomatik testlerle birlikte teslim edilmektedir.

## 2. Görevin tanımı

İstenen, sitedeki verinin eksiksiz biçimde ve ilişkileri bozulmadan bir veritabanına aktarılmasıydı.
Yani yalnızca sayfalardaki metinleri toplamak değil, "hangi ders hangi programın hangi yılki planında,
hangi yarıyılda yer alıyor", "bu dersin şu öğrenme çıktısı şu program çıktısına ne kadar katkı
veriyor" gibi bağlantıları da korumak gerekiyordu. Dil olarak Go, biçim olarak komut satırı aracı
tercih edildi.

## 3. Teknoloji seçimleri ve gerekçeleri

Go, derlendiğinde tek bir çalıştırılabilir dosya ürettiği için seçildi; aracı kullanacak kişinin
bilgisayarına ayrıca bir çalışma ortamı kurması gerekmiyor. Komut yapısı için cobra kütüphanesi
kullanıldı; bu sayede `migrate`, `discover`, `scrape`, `inspect` ve `stats` gibi alt komutlar ve
yardım metinleri düzenli bir biçimde sunulabildi. Veritabanı bağlantısı için Go'nun en yaygın
PostgreSQL sürücüsü olan pgx, HTML ayrıştırma için ise Go ekibinin `golang.org/x/net/html` paketi
kullanıldı. Dış bağımlılık sayısı bilinçli olarak düşük tutuldu.

Veritabanı olarak PostgreSQL, görevde istendiği için ve ilişkisel bütünlüğü (yabancı anahtarlar,
benzersizlik kısıtları) güçlü biçimde sağladığı için kullanıldı. Kurulumu kolaylaştırmak amacıyla
veritabanı ve aracın birlikte ayağa kalktığı bir Docker Compose yapılandırması hazırlandı. Her
değişiklikte testlerin otomatik çalışması için GitHub Actions ile sürekli entegrasyon kuruldu.

## 4. Sitenin yapısı ve tarama yöntemi

Site ASP.NET WebForms ile yazılmış ve tarama açısından birkaç özelliği var. Sayfalardaki tabloların
sabit bir kimliği ya da sınıfı bulunmuyor; buna karşılık tabloların önündeki başlıklar ("Dersin
Öğrenme Çıktıları", "AKTS Hesaplama İçeriği" vb.) sabit. Bu nedenle ayrıştırıcı tabloları başlık
metinlerinden, sütunları da tablonun başlık satırından tanıyor. Böylece sitede sütun sırası değişse
bile ayrıştırma bozulmuyor. Ders bağlantıları normal bir adres yerine
`prolizOpenCourseDetails(ID)` biçiminde bir JavaScript çağrısı içerdiğinden ders numaraları bu
çağrıdan okunuyor.

Tarama sırası sitenin kendi hiyerarşisini izliyor: önce seviyeye göre birimler, sonra her birimin
programları, ardından her programın bilgi sayfaları, program çıktıları ve ders planı, en son da ders
planındaki her dersin detay sayfası alınıyor. Programlar belirli sayıda işçi tarafından paralel
işleniyor.

Üniversite sunucusunu yormamak için tüm işçiler için ortak bir hız sınırı (varsayılan olarak saniyede
iki istek) uygulanıyor. Geçici hatalarda istek birkaç kez tekrarlanıyor; sunucu üst üste hata
verdiğinde ise tüm istekler birlikte bir süre bekletiliyor. Veritabanına yazma işlemleri "varsa
güncelle, yoksa ekle" mantığıyla yapıldığından tarama tekrar tekrar çalıştırılabiliyor ve veri
çoğalmıyor. Uzun bir tarama kesilirse `--fresh-within` seçeneğiyle yakın zamanda yazılmış dersler
atlanarak kalınan yerden devam edilebiliyor.

## 5. Veri modeli

Şema, sitedeki hiyerarşiyi doğrudan yansıtıyor. `units` tablosu fakülte, yüksekokul ve enstitüleri
tanıtım metinleriyle birlikte; `programs` tablosu programları; `program_outcomes` program çıktılarını
tutuyor. `courses` tablosu her dersin genel bilgilerini (kod, ad, AKTS, dil, düzey, koordinatör vb.)
içeriyor. `program_courses` tablosu dersleri programlara, plan yıllarına ve yarıyıllara bağlıyor; bir
programın aynı yılda birden fazla planı olabildiği için planlar sitedeki plan numarasıyla (`plan_id`)
ayırt ediliyor.

Ders detayları ayrı tablolara bölündü: `course_learning_outcomes` (öğrenme çıktıları),
`course_weekly_topics` (haftalık konular), `course_resources` (kaynaklar), `course_assessments`
(değerlendirme), `course_workloads` (AKTS iş yükü), `course_program_outcomes` (dersin program
çıktılarına katkısı) ve `course_outcome_matrix` (öğrenme çıktısı × program çıktısı matrisi). Her
taramanın özeti `scrape_runs`, alınamayan sayfalar `scrape_errors` tablosunda tutuluyor.

Sorgulamayı kolaylaştırmak için üç görünüm (view) tanımlandı: `v_curriculum` ders planını programı ve
ders bilgileriyle birlikte, güncel planı işaretleyerek gösteriyor; `v_course_outcome_matrix` ve
`v_course_lo_po_matrix` katkı matrisini okunur biçimde sunuyor.

Seçmeli havuz dersleri için ayrıca bir düzenleme yapıldı. Aynı seçmeli ders çok sayıda programın
havuzunda tekrar ettiğinden, her ders kodu yılda yalnızca bir kez indirilip içeriği diğer kopyalara da
yazılıyor; içeriğin hangi dersten alındığı `detail_source_id` sütununda saklanıyor. Bu yöntem havuz
için gereken istek sayısını yaklaşık otuz binden dört bin altı yüze indirdi. Sitede içeriği "Kayıt Yok"
olan dersler ise `detail_empty` ile işaretleniyor; böylece "bu dersin detayı sitede yok" durumu
"henüz indirilmedi" durumundan ayırt edilebiliyor.

## 6. Geliştirme süreci

Çalışma aşamalı olarak ilerledi. İlk aşamada birimleri, programları, ders planlarını ve ders
detaylarını tarayan temel araç ve veritabanı şeması oluşturuldu. İkinci aşamada seçmeli havuz
derslerinin detayları eklendi ve boş ders sayfaları işaretlenmeye başlandı. Üçüncü aşamada tarama
lisanstan önlisans, yüksek lisans ve doktora seviyelerine genişletildi. Dördüncü aşamada sitede
geriye dönük olarak bulunan eski ders planı yılları ve birimlerin tanıtım sayfaları eklendi. Son
aşamada tam taramalarla ortaya çıkan hatalar giderildi; kurulum belgeleri, Docker yapılandırması ve
bu rapor hazırlandı. Her aşama ayrı bir değişiklik isteği (pull request) olarak, testler geçtikten
sonra ana dala birleştirildi.

## 7. Karşılaşılan sorunlar ve çözümler

**Eski plan yıllarına erişim.** Sitede eski yılların ayrı bir adresi bulunmuyor; yıl seçimi ASP.NET'in
"postback" yöntemiyle, sayfanın gizli form alanlarıyla birlikte sunucuya yeniden gönderilmesiyle
yapılıyor. Bunun için sayfadaki formu okuyan, yıl seçim kutusunun değerini değiştiren ve bir
tarayıcının yapacağı gibi POST isteği gönderen bir mekanizma yazıldı. Dönen sayfanın gerçekten istenen
plan olup olmadığı da ayrıca doğrulanıyor.

**Oturumun paylaşılması.** Sunucu seçilen yılı kullanıcının oturumunda saklıyor. Programlar paralel
işlenirken hepsi aynı oturumu kullandığında bir programın yıl seçimi diğerini etkiliyor ve bazı
planlar boş geliyordu. Sorun, her programa kendi çerezleriyle ayrı bir oturum verilerek çözüldü; hız
sınırı ise tüm oturumlar arasında ortak kaldı. Hatanın tekrar etmemesi için, düzeltme olmadan
başarısız olan bir test eklendi.

**Aynı yılda birden fazla plan.** Bazı programların aynı yıl için birden fazla ders planı var (örneğin
bir programda üç ayrı "2026" planı). İlk sürümde planlar yıla göre ayrıldığından bu planlar birbirinin
kayıtlarını siliyor ve bazı ders–plan bağlantıları kayboluyordu. Planlar sitedeki plan numarasıyla
ayırt edilmeye başlanınca sorun giderildi. Son taramada ders sayısının ders–plan bağlantısı sayısına
birebir eşit çıkması bu iki düzeltmenin doğru çalıştığını gösteriyor.

**Tekrarlanan havuz dersleri ve boş sayfalar.** Yukarıda anlatılan paylaşımlı indirme ve `detail_empty`
işaretiyle çözüldü. Boş sayfalardaki yer tutucu metinlerin gerçek veri gibi yazılması da engellendi.

**Kesilen taramalar.** Kullanıcı taramayı durdurduğunda yarıda kalan istekler hata olarak
kaydediliyordu; bunlar artık hata sayılmıyor ve tarama "durduruldu" olarak işaretleniyor.

**Kurulum ortamı.** Windows üzerinde PostgreSQL komutlarının tam yolla çağrılması, veritabanı
adresinin kalıcı ortam değişkeni olarak tanımlanması ve komut satırında Türkçe karakterlerin düzgün
görünmesi gibi konular kurulum belgesine eklendi. Docker ile kurulumda bu adımların hiçbirine gerek
kalmıyor.

## 8. Sonuçlar

Son tam tarama `scrape --all --pool-details --years all` komutuyla yapıldı. Veritabanındaki satır
sayıları şöyle:

| Tablo | Satır |
|---|---|
| Birimler (`units`) | 45 |
| Programlar (`programs`) | 442 |
| Program çıktıları (`program_outcomes`) | 4.598 |
| Dersler (`courses`, tüm plan yılları) | 95.432 |
| Ders–plan bağlantıları (`program_courses`) | 95.432 |
| Öğrenme çıktıları | 436.894 |
| Haftalık konular | 1.115.829 |
| Kaynaklar | 160.291 |
| Değerlendirme satırları | 160.682 |
| AKTS iş yükü satırları | 361.997 |
| Ders–program çıktısı katkıları | 240.462 |
| Katkı matrisi hücreleri | 930.383 |
| Tarama hataları | 0 |

Yalnızca güncel planların taranması boş bir veritabanıyla yaklaşık üç buçuk saat, eski yıllar dahil
tam tarama tahminen yedi saat sürüyor. Var olan bir veritabanını güncellemek (`--fresh-within` ile)
yirmi-otuz dakika alıyor. Süreler büyük ölçüde sunucuya saygı amacıyla konulan hız sınırından
kaynaklanıyor.

## 9. Test ve doğrulama

Ayrıştırıcılar, gerçek siteden kaydedilmiş HTML sayfalarıyla test ediliyor; böylece testler
internete bağlı olmadan ve sitenin gerçek yapısına karşı çalışıyor. Tarama mantığı, gerçek sayfaları
sunan sahte bir web sitesine karşı; veritabanı katmanı ise gerçek bir PostgreSQL'e karşı test
ediliyor. Toplam 45 test bulunuyor ve her değişiklikte GitHub Actions üzerinde otomatik çalışıyor.
Bunlara ek olarak sitenin tamamı birden çok kez taranarak tablolar arası tutarlılık (her dersin bir
plana bağlı olması, hata kaydı bulunmaması) kontrol edildi.

## 10. Kullanım

Ayrıntılı kurulum ve kullanım adımları `README.md` dosyasında yer alıyor. En kısa yol Docker ile:

```bash
git clone https://github.com/mehmetfatihvar/staj.git
cd staj/staj/bologna-scraper
docker compose up -d db
docker compose --profile scrape run --rm scraper scrape -p 1371                    # tek program, ~1 dk
docker compose --profile scrape run --rm scraper scrape --all --pool-details       # tüm site
docker compose --profile scrape run --rm scraper stats
```

Taramanın kapsamı seçeneklerle ayarlanabiliyor: `--all` ya da `-l` ile seviye, `-p` ile tek tek
programlar, `--years` ile kaç plan yılının alınacağı, `--pool-details` ile havuz derslerinin
detayları, `--skip-details` ile yalnızca planlar seçilebiliyor. `--dry-run` veritabanına yazmadan
sonuçları JSON olarak veriyor, `inspect` komutu ise tek bir sayfanın nasıl ayrıştırıldığını
gösteriyor.

## 11. Bilinen sınırlar ve olası geliştirmeler

Sitenin İngilizce sürümü taranmıyor; aynı verinin başka dildeki hali olduğu ve ayrıştırıcı Türkçe
başlıklara göre çalıştığı için kapsam dışında bırakıldı. Üniversitenin programlara bağlı olmayan genel
bilgi sayfaları ve "Ders Yapısı" özet grafikleri (veritabanındaki verilerden zaten hesaplanabilen
dağılımlar) alınmıyor. Siteden kaldırılmış programlara da erişilemiyor.

İleride İngilizce içeriğin ayrı sütunlara eklenmesi, taramanın zamanlanmış görev olarak düzenli
çalıştırılıp değişikliklerin raporlanması ve veritabanı üzerine basit bir sorgulama arayüzü
yapılması düşünülebilir.
