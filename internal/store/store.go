// Package store, ayrıştırılan veriyi PostgreSQL'e idempotent olarak yazar.
// Aynı programı tekrar scrape etmek kopya üretmez; kayıtlar güncellenir.
package store

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mehmetfatihvar/staj/staj/bologna-scraper/internal/parse"
)

//go:embed schema.sql
var schemaSQL string

// Program çıktısı kaynak öncelikleri (yüksek olan düşüğün üzerine yazar).
const (
	SourceAboutText    = "about_text"
	SourceCourseMatrix = "course_matrix"
	SourceOutcomesPage = "outcomes_page"
)

func sourcePriority(s string) int {
	switch s {
	case SourceOutcomesPage:
		return 3
	case SourceCourseMatrix:
		return 2
	}
	return 1
}

type Store struct {
	pool *pgxpool.Pool
}

func Open(ctx context.Context, dsn string) (*Store, error) {
	if dsn == "" {
		return nil, errors.New("veritabanı adresi yok: --db veya BOLOGNA_DB_URL / DATABASE_URL verin")
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres bağlantısı: %w", err)
	}
	return &Store{pool: pool}, nil
}

func (s *Store) Close() { s.pool.Close() }

func (s *Store) Migrate(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, schemaSQL)
	return err
}

// nz, boş string'i NULL'a çevirir.
func nz(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}

func nzID(v int64) *int64 {
	if v == 0 {
		return nil
	}
	return &v
}

func (s *Store) Unit(ctx context.Context, id int64, name, level string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO units (id, name, levels) VALUES ($1, $2, CASE WHEN $3::text IS NULL THEN '{}' ELSE ARRAY[$3::text] END)
		ON CONFLICT (id) DO UPDATE SET
			name   = COALESCE(EXCLUDED.name, units.name),
			levels = CASE WHEN $3::text IS NULL OR $3::text = ANY(units.levels) THEN units.levels
			              ELSE array_append(units.levels, $3::text) END,
			updated_at = now()`, id, nz(name), nz(level))
	return err
}

// UnitAbout, birimin tanıtım sayfasını yazar. Boş ad mevcut adı silmez.
func (s *Store) UnitAbout(ctx context.Context, id int64, a *parse.UnitAbout) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO units (id, name, about, about_scraped_at) VALUES ($1, $2, $3, now())
		ON CONFLICT (id) DO UPDATE SET
			name = COALESCE(EXCLUDED.name, units.name), about = EXCLUDED.about,
			about_scraped_at = now(), updated_at = now()`, id, nz(a.Name), nz(a.About))
	return err
}

func (s *Store) Program(ctx context.Context, ref parse.ProgramRef, p *parse.Program, url string) error {
	name := p.Name
	if name == "" {
		name = ref.Name
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO programs (id, unit_id, name, level, language, duration_years, max_duration_years,
			quota, internship, head, ects_coordinator, content, history, degree, admission,
			progression, graduation, employment, assessment, info, sections, source_url, scraped_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22, now())
		ON CONFLICT (id) DO UPDATE SET
			unit_id            = COALESCE(EXCLUDED.unit_id, programs.unit_id),
			name               = COALESCE(EXCLUDED.name, programs.name),
			level              = COALESCE(EXCLUDED.level, programs.level),
			language           = EXCLUDED.language,
			duration_years     = EXCLUDED.duration_years,
			max_duration_years = EXCLUDED.max_duration_years,
			quota              = EXCLUDED.quota,
			internship         = EXCLUDED.internship,
			head               = EXCLUDED.head,
			ects_coordinator   = EXCLUDED.ects_coordinator,
			content            = EXCLUDED.content,
			history            = EXCLUDED.history,
			degree             = EXCLUDED.degree,
			admission          = EXCLUDED.admission,
			progression        = EXCLUDED.progression,
			graduation         = EXCLUDED.graduation,
			employment         = EXCLUDED.employment,
			assessment         = EXCLUDED.assessment,
			info               = EXCLUDED.info,
			sections           = EXCLUDED.sections,
			source_url         = EXCLUDED.source_url,
			scraped_at         = now()`,
		ref.ID, nzID(ref.UnitID), nz(name), nz(ref.Level), nz(p.Language), p.Duration, p.MaxDuration,
		nz(p.Quota), nz(p.Internship), nz(p.Head), nz(p.ECTSCoordinator), nz(p.Content), nz(p.History),
		nz(p.Degree), nz(p.Admission), nz(p.Progression), nz(p.Graduation), nz(p.Employment),
		nz(p.Assessment), p.Info, p.Sections, nz(url))
	return err
}

const upsertOutcomeSQL = `
	INSERT INTO program_outcomes (program_id, no, description, source) VALUES ($1, $2, $3, $4)
	ON CONFLICT (program_id, no) DO UPDATE SET
		description = COALESCE(EXCLUDED.description, program_outcomes.description),
		source      = CASE WHEN EXCLUDED.description IS NULL THEN program_outcomes.source ELSE EXCLUDED.source END
	WHERE program_outcomes.description IS NULL
	   OR (CASE program_outcomes.source WHEN 'outcomes_page' THEN 3 WHEN 'course_matrix' THEN 2 ELSE 1 END) <= $5`

func (s *Store) ProgramOutcomes(ctx context.Context, programID int64, outs []parse.Outcome, source string) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if source == SourceOutcomesPage {
			// Resmî liste geldi: metinden sezgisel çıkarılmış ve listede olmayan maddeler yanlış pozitiftir.
			nos := make([]int32, len(outs))
			for i, o := range outs {
				nos[i] = int32(o.No)
			}
			if _, err := tx.Exec(ctx, `DELETE FROM program_outcomes WHERE program_id = $1 AND source = $2 AND NOT (no = ANY($3))`,
				programID, SourceAboutText, nos); err != nil {
				return err
			}
		}
		b := &pgx.Batch{}
		for _, o := range outs {
			b.Queue(upsertOutcomeSQL, programID, o.No, nz(o.Description), source, sourcePriority(source))
		}
		return tx.SendBatch(ctx, b).Close()
	})
}

// CourseFresh, dersin detayı since'ten sonra yazılmışsa true döner (scraper.FreshChecker).
// Sahipsiz yazılmış dersler (katkıları bağlanamamış) taze sayılmaz, yeniden indirilir.
func (s *Store) CourseFresh(ctx context.Context, courseID int64, since time.Time) (bool, error) {
	var ok bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM courses WHERE id = $1 AND detail_scraped_at >= $2 AND program_id IS NOT NULL)`,
		courseID, since).Scan(&ok)
	return ok, err
}

func (s *Store) Curriculum(ctx context.Context, programID int64, cur *parse.Curriculum) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		// Yalnızca bu yılın planı değiştirilir; diğer yılların satırları korunur.
		if _, err := tx.Exec(ctx, `DELETE FROM program_courses WHERE program_id = $1 AND plan_year IS NOT DISTINCT FROM $2`,
			programID, cur.Year); err != nil {
			return err
		}
		if cur.Year != nil && !cur.Archived {
			if _, err := tx.Exec(ctx, `UPDATE programs SET curriculum_year = $2 WHERE id = $1`, programID, *cur.Year); err != nil {
				return err
			}
		}
		b := &pgx.Batch{}
		for i, e := range cur.Entries {
			sem := &e.Semester
			if e.Semester == 0 {
				sem = nil
			}
			// Ders plandaki güncel bilgilerle yazılır (plan ve detay sayfası aynı kaynaktan; en yeni değer kazanır,
			// boş gelen alan eskisini silmez). Sahip program ilk yazanda kalır. Detay sayfası sonra zenginleştirir.
			b.Queue(`
				INSERT INTO courses (id, program_id, code, name, semester, theory_hours, practice_hours, lab_hours, credit, ects)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
				ON CONFLICT (id) DO UPDATE SET
					program_id     = COALESCE(courses.program_id, EXCLUDED.program_id),
					code           = COALESCE(EXCLUDED.code, courses.code),
					name           = COALESCE(EXCLUDED.name, courses.name),
					semester       = COALESCE(EXCLUDED.semester, courses.semester),
					theory_hours   = COALESCE(EXCLUDED.theory_hours, courses.theory_hours),
					practice_hours = COALESCE(EXCLUDED.practice_hours, courses.practice_hours),
					lab_hours      = COALESCE(EXCLUDED.lab_hours, courses.lab_hours),
					credit         = COALESCE(EXCLUDED.credit, courses.credit),
					ects           = COALESCE(EXCLUDED.ects, courses.ects),
					updated_at     = now()`,
				e.CourseID, programID, nz(e.Code), nz(e.Name), sem, e.Theory, e.Practice, e.Lab, e.Credit, e.ECTS)
			b.Queue(`
				INSERT INTO program_courses (program_id, course_id, semester, course_type, delivery_mode,
					is_elective_group, group_course_count, elective_group_id, ordinal, plan_year)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
				ON CONFLICT (program_id, course_id) DO UPDATE SET
					semester = EXCLUDED.semester, course_type = EXCLUDED.course_type,
					delivery_mode = EXCLUDED.delivery_mode, is_elective_group = EXCLUDED.is_elective_group,
					group_course_count = EXCLUDED.group_course_count,
					elective_group_id = EXCLUDED.elective_group_id, ordinal = EXCLUDED.ordinal, plan_year = EXCLUDED.plan_year`,
				programID, e.CourseID, sem, nz(e.Type), nz(e.Delivery), e.IsGroup, e.GroupCount, nzID(e.GroupID), i, cur.Year)
		}
		return tx.SendBatch(ctx, b).Close()
	})
}

func (s *Store) Course(ctx context.Context, programID, courseID int64, d *parse.CourseDetail, url string) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var owner *int64
		err := tx.QueryRow(ctx, `
			INSERT INTO courses (id, program_id, code, name, semester, theory_hours, practice_hours, credit, ects,
				language, level, department, delivery_mode, course_type, objective, content, prerequisites,
				coordinator, instructors, assistants, details, source_url, teaching_mode, detail_source_id, detail_empty, detail_scraped_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25, now())
			ON CONFLICT (id) DO UPDATE SET
				program_id     = COALESCE(courses.program_id, EXCLUDED.program_id),
				code           = COALESCE(EXCLUDED.code, courses.code),
				name           = COALESCE(EXCLUDED.name, courses.name),
				semester       = COALESCE(EXCLUDED.semester, courses.semester),
				theory_hours   = COALESCE(EXCLUDED.theory_hours, courses.theory_hours),
				practice_hours = COALESCE(EXCLUDED.practice_hours, courses.practice_hours),
				credit         = COALESCE(EXCLUDED.credit, courses.credit),
				ects           = COALESCE(EXCLUDED.ects, courses.ects),
				language = EXCLUDED.language, level = EXCLUDED.level, department = EXCLUDED.department,
				delivery_mode = EXCLUDED.delivery_mode, course_type = EXCLUDED.course_type,
				objective = EXCLUDED.objective, content = EXCLUDED.content, prerequisites = EXCLUDED.prerequisites,
				coordinator = EXCLUDED.coordinator, instructors = EXCLUDED.instructors, assistants = EXCLUDED.assistants,
				details = EXCLUDED.details, source_url = EXCLUDED.source_url, teaching_mode = EXCLUDED.teaching_mode,
				detail_source_id = EXCLUDED.detail_source_id, detail_empty = EXCLUDED.detail_empty,
				detail_scraped_at = now(), updated_at = now()
			RETURNING program_id`,
			courseID, nzID(programID), nz(d.Code), nz(d.Name), d.Semester, d.Theory, d.Practice, d.Credit, d.ECTS,
			nz(d.Language), nz(d.Level), nz(d.Department), nz(d.DeliveryMode), nz(d.Type), nz(d.Objective),
			nz(d.Content), nz(d.Prerequisites), nz(d.Coordinator), nz(d.Instructors), nz(d.Assistants),
			d.Details, nz(url), nz(d.TeachingMode), nzID(d.SourceCourseID), d.Empty).Scan(&owner)
		if err != nil {
			return fmt.Errorf("course upsert: %w", err)
		}

		for _, t := range []string{"course_learning_outcomes", "course_weekly_topics", "course_resources",
			"course_assessments", "course_workloads", "course_program_outcomes", "course_outcome_matrix"} {
			if _, err := tx.Exec(ctx, "DELETE FROM "+t+" WHERE course_id = $1", courseID); err != nil {
				return err
			}
		}

		b := &pgx.Batch{}
		for i, lo := range d.LearningOutcomes {
			b.Queue(`INSERT INTO course_learning_outcomes (course_id, ordinal, no, description, extra) VALUES ($1,$2,$3,$4,$5)`,
				courseID, i, lo.No, nz(lo.Description), nz(lo.Extra))
		}
		for i, w := range d.WeeklyTopics {
			b.Queue(`INSERT INTO course_weekly_topics (course_id, ordinal, week, topic, preparation, documents) VALUES ($1,$2,$3,$4,$5,$6)`,
				courseID, i, w.Week, nz(w.Topic), nz(w.Preparation), nz(w.Documents))
		}
		for i, r := range d.Resources {
			b.Queue(`INSERT INTO course_resources (course_id, ordinal, kind, content) VALUES ($1,$2,$3,$4)`,
				courseID, i, nz(r.Kind), nz(r.Content))
		}
		for i, a := range d.Assessments {
			b.Queue(`INSERT INTO course_assessments (course_id, ordinal, activity, count, percentage) VALUES ($1,$2,$3,$4,$5)`,
				courseID, i, nz(a.Activity), a.Count, a.Percentage)
		}
		for i, w := range d.Workloads {
			b.Queue(`INSERT INTO course_workloads (course_id, ordinal, activity, count, hours, total) VALUES ($1,$2,$3,$4,$5,$6)`,
				courseID, i, nz(w.Activity), w.Count, w.Hours, w.Total)
		}
		// Katkı matrisi, dersin SAHİBİ olan programın çıktılarına bağlanır.
		if owner != nil {
			for _, oc := range d.ProgramOutcomes {
				b.Queue(upsertOutcomeSQL, *owner, oc.No, nz(oc.Description), SourceCourseMatrix, sourcePriority(SourceCourseMatrix))
				if oc.Level == nil { // sitede boş hücre = katkı yok; satır yazılmaz
					continue
				}
				b.Queue(`
					INSERT INTO course_program_outcomes (course_id, program_outcome_id, contribution)
					SELECT $1, id, $4 FROM program_outcomes WHERE program_id = $2 AND no = $3
					ON CONFLICT (course_id, program_outcome_id) DO UPDATE SET contribution = EXCLUDED.contribution`,
					courseID, *owner, oc.No, oc.Level)
			}
			for _, m := range d.OutcomeMatrix {
				// PÇ satırı yukarıda (ProgramOutcomes) oluşturulmadıysa da boş açıklamayla oluşsun.
				b.Queue(upsertOutcomeSQL, *owner, m.PO, nil, SourceCourseMatrix, sourcePriority(SourceCourseMatrix))
				b.Queue(`
					INSERT INTO course_outcome_matrix (course_id, lo_no, program_outcome_id, contribution)
					SELECT $1, $2, id, $5 FROM program_outcomes WHERE program_id = $3 AND no = $4
					ON CONFLICT (course_id, lo_no, program_outcome_id) DO UPDATE SET contribution = EXCLUDED.contribution`,
					courseID, m.LO, *owner, m.PO, m.Level)
			}
		}
		return tx.SendBatch(ctx, b).Close()
	})
}

// ---- çalışma kayıtları ----

func (s *Store) StartRun(ctx context.Context, args any) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `INSERT INTO scrape_runs (args) VALUES ($1) RETURNING id`, args).Scan(&id)
	return id, err
}

func (s *Store) FinishRun(ctx context.Context, id int64, status string, programs, courses, errs int64) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE scrape_runs SET finished_at = now(), status = $2, programs = $3, courses = $4, errors = $5
		WHERE id = $1`, id, status, programs, courses, errs)
	return err
}

func (s *Store) LogError(ctx context.Context, runID int64, url, msg string) {
	_, _ = s.pool.Exec(ctx, `INSERT INTO scrape_errors (run_id, url, message) VALUES ($1,$2,$3)`, nzID(runID), url, msg)
}

type TableCount struct {
	Table string
	Rows  int64
}

func (s *Store) Stats(ctx context.Context) ([]TableCount, error) {
	tables := []string{"units", "programs", "program_outcomes", "courses", "program_courses",
		"course_learning_outcomes", "course_weekly_topics", "course_resources", "course_assessments",
		"course_workloads", "course_program_outcomes", "course_outcome_matrix", "scrape_runs", "scrape_errors"}
	var out []TableCount
	for _, t := range tables {
		var n int64
		if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM "+t).Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, TableCount{t, n})
	}
	return out, nil
}
