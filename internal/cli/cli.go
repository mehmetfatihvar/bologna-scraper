// Package cli, `bologna` komut satırı arayüzü.
package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/mehmetfatihvar/staj/staj/bologna-scraper/internal/fetch"
	"github.com/mehmetfatihvar/staj/staj/bologna-scraper/internal/parse"
	"github.com/mehmetfatihvar/staj/staj/bologna-scraper/internal/scraper"
	"github.com/mehmetfatihvar/staj/staj/bologna-scraper/internal/store"
)

type globals struct {
	db        string
	baseURL   string
	lang      string
	rps       float64
	timeout   time.Duration
	retries   int
	cacheDir  string
	useCache  bool
	userAgent string
	verbose   bool
	logJSON   bool
}

var g globals

func Execute() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := newRoot().ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "hata:", err)
		return 1
	}
	return 0
}

func envOr(keys ...string) string {
	for _, k := range keys {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return ""
}

func newRoot() *cobra.Command {
	root := &cobra.Command{
		Use:           "bologna",
		Short:         "Mersin Üniversitesi Bologna Bilgi Paketi scraper'ı (OİBS -> PostgreSQL)",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	f := root.PersistentFlags()
	f.StringVar(&g.db, "db", envOr("BOLOGNA_DB_URL", "DATABASE_URL"), "PostgreSQL bağlantı adresi (env: BOLOGNA_DB_URL, DATABASE_URL)")
	f.StringVar(&g.baseURL, "base-url", "https://obs.mersin.edu.tr/oibs/bologna/", "OİBS Bologna kök adresi")
	f.StringVar(&g.lang, "lang", "tr", "içerik dili (tr|en) — parser Türkçe başlıklara göre ayarlıdır")
	f.Float64Var(&g.rps, "rps", 2, "saniyedeki azami istek (tüm worker'lar toplamı)")
	f.DurationVar(&g.timeout, "timeout", 30*time.Second, "istek zaman aşımı")
	f.IntVar(&g.retries, "retries", 3, "geçici hatalarda tekrar sayısı")
	f.StringVar(&g.cacheDir, "cache-dir", "", "ham HTML'i bu klasöre kaydet")
	f.BoolVar(&g.useCache, "use-cache", false, "önce --cache-dir'deki kayıtları kullan (sunucuya gitme)")
	f.StringVar(&g.userAgent, "user-agent", "", "HTTP User-Agent")
	f.BoolVarP(&g.verbose, "verbose", "v", false, "ayrıntılı log")
	f.BoolVar(&g.logJSON, "log-json", false, "logları JSON olarak yaz")

	root.AddCommand(migrateCmd(), discoverCmd(), scrapeCmd(), inspectCmd(), statsCmd())
	return root
}

func logger() *slog.Logger {
	lvl := slog.LevelInfo
	if g.verbose {
		lvl = slog.LevelDebug
	}
	opts := &slog.HandlerOptions{Level: lvl}
	if g.logJSON {
		return slog.New(slog.NewJSONHandler(os.Stderr, opts))
	}
	return slog.New(slog.NewTextHandler(os.Stderr, opts))
}

func client() (*fetch.Client, error) {
	return fetch.New(fetch.Options{
		BaseURL: g.baseURL, Lang: g.lang, RPS: g.rps, Timeout: g.timeout, Retries: g.retries,
		UserAgent: g.userAgent, CacheDir: g.cacheDir, UseCache: g.useCache,
	})
}

// ---------------- migrate ----------------

func migrateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "migrate",
		Short: "Veritabanı şemasını oluşturur/günceller (idempotent)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			st, err := store.Open(cmd.Context(), g.db)
			if err != nil {
				return err
			}
			defer st.Close()
			if err := st.Migrate(cmd.Context()); err != nil {
				return err
			}
			fmt.Println("✓ şema hazır")
			return nil
		},
	}
}

// ---------------- discover ----------------

func levelList(levels []string, all bool) ([]string, error) {
	if all {
		return []string{"onlisans", "lisans", "yukseklisans", "doktora"}, nil
	}
	for _, l := range levels {
		if _, ok := scraper.LevelTypes[l]; !ok {
			return nil, fmt.Errorf("bilinmeyen seviye %q (onlisans|lisans|yukseklisans|doktora)", l)
		}
	}
	return levels, nil
}

func discover(ctx context.Context, sc *scraper.Scraper, levels []string, typeOverride string) ([]parse.ProgramRef, error) {
	var all []parse.ProgramRef
	seen := map[int64]bool{}
	for _, lvl := range levels {
		typ := scraper.LevelTypes[lvl]
		if typeOverride != "" {
			typ = typeOverride
		}
		refs, err := sc.Discover(ctx, lvl, typ)
		if err != nil {
			return nil, fmt.Errorf("%s keşfi: %w", lvl, err)
		}
		sc.Log.Info("keşif tamam", "seviye", lvl, "program", len(refs))
		for _, r := range refs {
			if !seen[r.ID] {
				seen[r.ID] = true
				all = append(all, r)
			}
		}
	}
	return all, nil
}

func discoverCmd() *cobra.Command {
	var (
		levels []string
		all    bool
		typ    string
		asJSON bool
	)
	cmd := &cobra.Command{
		Use:   "discover",
		Short: "Birim ve programları listeler (veritabanına yazmaz)",
		Example: `  bologna discover --level lisans
  bologna discover --all --json > programlar.json`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			lv, err := levelList(levels, all)
			if err != nil {
				return err
			}
			c, err := client()
			if err != nil {
				return err
			}
			sc := &scraper.Scraper{Client: c, Log: logger()}
			refs, err := discover(cmd.Context(), sc, lv, typ)
			if err != nil {
				return err
			}
			if asJSON {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				enc.SetEscapeHTML(false)
				return enc.Encode(refs)
			}
			tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "SEVİYE\tBİRİM\tPROGRAM ID\tPROGRAM")
			for _, r := range refs {
				fmt.Fprintf(tw, "%s\t%s\t%d\t%s\n", r.Level, r.UnitName, r.ID, r.Name)
			}
			tw.Flush()
			fmt.Fprintf(os.Stderr, "toplam %d program\n", len(refs))
			return nil
		},
	}
	cmd.Flags().StringSliceVarP(&levels, "level", "l", []string{"lisans"}, "seviye(ler): onlisans,lisans,yukseklisans,doktora")
	cmd.Flags().BoolVar(&all, "all", false, "tüm seviyeler")
	cmd.Flags().StringVar(&typ, "type", "", "unitSelection.aspx 'type' parametresini elle ver (seviye eşlemesini ezer)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "JSON çıktı")
	return cmd
}

// ---------------- scrape ----------------

func scrapeCmd() *cobra.Command {
	var (
		levels      []string
		all         bool
		typ         string
		programs    []string
		fromFile    string
		workers     int
		limit       int
		skipDetails bool
		freshWithin time.Duration
		dryRun      bool
	)
	cmd := &cobra.Command{
		Use:   "scrape",
		Short: "Programları, ders planlarını ve ders detaylarını çekip veritabanına yazar",
		Example: `  bologna scrape --program 352                   # tek program (curSunit)
  bologna scrape -p "https://obs.mersin.edu.tr/oibs/bologna/index.aspx?lang=tr&curOp=showPac&curUnit=12&curSunit=352"
  bologna scrape --level lisans --workers 3
  bologna scrape --all --rps 1.5
  bologna scrape --from-file programlar.json      # discover --json çıktısı
  bologna scrape --all --fresh-within 24h        # kesilen taramayı sürdür
  bologna scrape --program 352 --dry-run | jq .   # DB'ye yazmadan JSON göster`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			log := logger()
			c, err := client()
			if err != nil {
				return err
			}
			sc := &scraper.Scraper{Client: c, Log: log, Workers: workers, SkipDetails: skipDetails, FreshWithin: freshWithin}

			// Hedef programlar
			var refs []parse.ProgramRef
			switch {
			case fromFile != "":
				b, err := os.ReadFile(fromFile)
				if err != nil {
					return err
				}
				if err := json.Unmarshal(b, &refs); err != nil {
					return fmt.Errorf("%s: %w", fromFile, err)
				}
			case len(programs) > 0:
				for _, p := range programs {
					ref, err := parseProgramArg(p)
					if err != nil {
						return err
					}
					refs = append(refs, ref)
				}
			default:
				lv, err := levelList(levels, all)
				if err != nil {
					return err
				}
				if len(lv) == 0 {
					return errors.New("--program, --level, --all veya --from-file verin")
				}
				if refs, err = discover(ctx, sc, lv, typ); err != nil {
					return err
				}
			}
			if limit > 0 && len(refs) > limit {
				refs = refs[:limit]
			}
			if len(refs) == 0 {
				return errors.New("işlenecek program bulunamadı (discover çıktısını kontrol edin: bologna inspect ...)")
			}

			// Hedef sink
			var runID int64
			var st *store.Store
			if dryRun {
				sc.Sink = scraper.NewJSONSink(os.Stdout)
			} else {
				if st, err = store.Open(ctx, g.db); err != nil {
					return err
				}
				defer st.Close()
				if err := st.Migrate(ctx); err != nil {
					return fmt.Errorf("migrate: %w", err)
				}
				sc.Sink = st
				runID, _ = st.StartRun(ctx, map[string]any{
					"programs": len(refs), "levels": levels, "all": all, "workers": workers, "rps": g.rps, "skip_details": skipDetails,
				})
				sc.OnError = func(u string, e error) { st.LogError(context.Background(), runID, u, e.Error()) }
			}

			start := time.Now()
			log.Info("başlıyor", "program", len(refs), "worker", workers, "rps", g.rps)
			stats := sc.Run(ctx, refs)

			status := "ok"
			if ctx.Err() != nil {
				status = "interrupted"
			} else if stats.Errors > 0 {
				status = "partial"
			}
			if st != nil {
				_ = st.FinishRun(context.Background(), runID, status, stats.Programs, stats.Courses, stats.Errors)
			}
			log.Info("bitti", "durum", status, "program", stats.Programs, "ders", stats.Courses,
				"atlanan_ders", stats.Skipped, "hata", stats.Errors, "süre", time.Since(start).Round(time.Second), "run_id", runID)
			if status == "interrupted" {
				return errors.New("kullanıcı tarafından durduruldu")
			}
			return nil
		},
	}
	fl := cmd.Flags()
	fl.StringSliceVarP(&levels, "level", "l", nil, "seviye(ler): onlisans,lisans,yukseklisans,doktora")
	fl.BoolVar(&all, "all", false, "tüm seviyeler")
	fl.StringVar(&typ, "type", "", "unitSelection.aspx 'type' parametresini elle ver")
	fl.StringSliceVarP(&programs, "program", "p", nil, "program ID'leri (curSunit) veya sitedeki program adresleri; virgülle veya tekrar ederek")
	fl.StringVar(&fromFile, "from-file", "", "discover --json çıktısından program listesi")
	fl.IntVarP(&workers, "workers", "w", 2, "paralel işlenecek program sayısı")
	fl.IntVar(&limit, "limit", 0, "en fazla N program işle (test için)")
	fl.BoolVar(&skipDetails, "skip-details", false, "ders detay sayfalarını atla (sadece program + ders planı)")
	fl.DurationVar(&freshWithin, "fresh-within", 0, "detayı bu süre içinde yazılmış dersleri atla (ör. 24h; kesilen taramayı sürdürmek için)")
	fl.BoolVar(&dryRun, "dry-run", false, "veritabanına yazma; sonuçları JSON satırları olarak stdout'a bas")
	return cmd
}

var (
	reArgSunit = regexp.MustCompile(`(?i)curSunit=(\d+)`)
	reArgUnit  = regexp.MustCompile(`(?i)curUnit=(\d+)`)
)

// parseProgramArg, "352" veya "...index.aspx?curUnit=12&curSunit=352" biçimini kabul eder.
func parseProgramArg(s string) (parse.ProgramRef, error) {
	s = strings.TrimSpace(s)
	if id, err := strconv.ParseInt(s, 10, 64); err == nil && id > 0 {
		return parse.ProgramRef{ID: id}, nil
	}
	m := reArgSunit.FindStringSubmatch(s)
	if m == nil {
		return parse.ProgramRef{}, fmt.Errorf("program %q: sayı ya da curSunit= içeren bir adres olmalı", s)
	}
	ref := parse.ProgramRef{}
	ref.ID, _ = strconv.ParseInt(m[1], 10, 64)
	if u := reArgUnit.FindStringSubmatch(s); u != nil {
		ref.UnitID, _ = strconv.ParseInt(u[1], 10, 64)
	}
	return ref, nil
}

// ---------------- inspect ----------------

func inspectCmd() *cobra.Command {
	var (
		as     string
		rows   int
		saveTo string
	)
	cmd := &cobra.Command{
		Use:   "inspect <url | dosya.html | sayfa.aspx?param=..>",
		Short: "Bir sayfayı parser'ın gözünden gösterir (seçici ayarlamak için)",
		Long: `Tek bir sayfayı indirir (veya yerel dosyayı okur) ve parser'ın gördüğünü basar.
--as tables: tüm tabloları etiketleriyle listeler (varsayılan)
--as about|outcomes|curriculum|course|units: ilgili parser'ın JSON çıktısı`,
		Example: `  bologna inspect "progCourseDetails.aspx?curCourse=12345" --as course
  bologna inspect "progCourses.aspx?curSunit=352"
  bologna inspect sayfa.html --as about`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			body, err := readSource(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if saveTo != "" {
				if err := os.WriteFile(saveTo, body, 0o644); err != nil {
					return err
				}
			}
			var out any
			switch as {
			case "tables":
				ts, _, err := parse.ParseTables(bytes.NewReader(body))
				if err != nil {
					return err
				}
				fmt.Print(parse.Debug(ts, rows))
				fmt.Fprintf(os.Stderr, "%d tablo\n", len(ts))
				return nil
			case "about":
				out, err = parse.ParseProgramAbout(bytes.NewReader(body))
			case "outcomes":
				out, err = parse.ParseOutcomesPage(bytes.NewReader(body))
			case "curriculum":
				out, err = parse.ParseCurriculum(body)
			case "course":
				out, err = parse.ParseCourseDetail(bytes.NewReader(body))
			case "units":
				p, u, e := parse.ParseUnitSelection(bytes.NewReader(body))
				out, err = map[string]any{"programs": p, "units": u}, e
			default:
				return fmt.Errorf("bilinmeyen --as %q", as)
			}
			if err != nil {
				return err
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			enc.SetEscapeHTML(false)
			return enc.Encode(out)
		},
	}
	cmd.Flags().StringVar(&as, "as", "tables", "tables|about|outcomes|curriculum|course|units")
	cmd.Flags().IntVar(&rows, "rows", 6, "tables modunda tablo başına gösterilecek satır")
	cmd.Flags().StringVar(&saveTo, "save", "", "ham HTML'i bu dosyaya da kaydet")
	return cmd
}

func readSource(ctx context.Context, src string) ([]byte, error) {
	if _, err := os.Stat(src); err == nil {
		return os.ReadFile(src)
	}
	c, err := client()
	if err != nil {
		return nil, err
	}
	u := src
	if !strings.HasPrefix(src, "http://") && !strings.HasPrefix(src, "https://") {
		u = c.Resolve(src)
	}
	fmt.Fprintln(os.Stderr, "GET", u)
	return c.Get(ctx, u)
}

// ---------------- stats ----------------

func statsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stats",
		Short: "Tablolardaki kayıt sayılarını gösterir",
		RunE: func(cmd *cobra.Command, _ []string) error {
			st, err := store.Open(cmd.Context(), g.db)
			if err != nil {
				return err
			}
			defer st.Close()
			counts, err := st.Stats(cmd.Context())
			if err != nil {
				return err
			}
			tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', tabwriter.AlignRight)
			for _, c := range counts {
				fmt.Fprintf(tw, "%s\t%s\t\n", c.Table, strconv.FormatInt(c.Rows, 10))
			}
			return tw.Flush()
		},
	}
}
