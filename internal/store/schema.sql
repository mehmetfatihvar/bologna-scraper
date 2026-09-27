-- Mersin Üniversitesi Bologna Bilgi Paketi şeması
-- Doğal anahtarlar OİBS'nin kendi ID'leridir: curUnit, curSunit, curCourse.
-- Tüm komutlar idempotent: `bologna migrate` tekrar tekrar çalıştırılabilir.

CREATE TABLE IF NOT EXISTS units (
    id          BIGINT PRIMARY KEY,              -- curUnit (fakülte / enstitü / MYO)
    name        TEXT,
    levels      TEXT[] NOT NULL DEFAULT '{}',    -- enstitüler birden çok seviyede olabilir
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
ALTER TABLE units ADD COLUMN IF NOT EXISTS levels TEXT[] NOT NULL DEFAULT '{}';  -- eski şemadan geçiş
-- facAbout.aspx tanıtım metni (paragraflar satır sonuyla ayrılır)
ALTER TABLE units ADD COLUMN IF NOT EXISTS about TEXT;
ALTER TABLE units ADD COLUMN IF NOT EXISTS about_scraped_at TIMESTAMPTZ;

CREATE TABLE IF NOT EXISTS programs (
    id                  BIGINT PRIMARY KEY,      -- curSunit
    unit_id             BIGINT REFERENCES units(id) ON DELETE SET NULL,
    name                TEXT,
    level               TEXT,                    -- onlisans | lisans | yukseklisans | doktora
    language            TEXT,
    duration_years      INT,
    max_duration_years  INT,
    quota               TEXT,
    internship          TEXT,
    head                TEXT,
    ects_coordinator    TEXT,
    content             TEXT,
    history             TEXT,
    degree              TEXT,
    admission           TEXT,
    progression         TEXT,
    graduation          TEXT,
    employment          TEXT,
    assessment          TEXT,
    curriculum_year     INT,                     -- scrape edilen ders planının yılı (sitede seçili olan)
    info                JSONB NOT NULL DEFAULT '{}'::jsonb,   -- ham anahtar/değer
    sections            JSONB NOT NULL DEFAULT '{}'::jsonb,   -- tüm bölümler, ham
    source_url          TEXT,
    scraped_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS programs_unit_idx ON programs(unit_id);
ALTER TABLE programs ADD COLUMN IF NOT EXISTS curriculum_year INT;

CREATE TABLE IF NOT EXISTS program_outcomes (
    id          BIGSERIAL PRIMARY KEY,
    program_id  BIGINT NOT NULL REFERENCES programs(id) ON DELETE CASCADE,
    no          INT NOT NULL,
    description TEXT,
    source      TEXT NOT NULL,                   -- outcomes_page > course_matrix > about_text
    UNIQUE (program_id, no)
);

CREATE TABLE IF NOT EXISTS courses (
    id                 BIGINT PRIMARY KEY,       -- curCourse
    program_id         BIGINT REFERENCES programs(id) ON DELETE SET NULL, -- sahibi olan program
    code               TEXT,
    name               TEXT,
    semester           INT,
    theory_hours       INT,
    practice_hours     INT,
    lab_hours          INT,
    credit             NUMERIC(6,2),
    ects               NUMERIC(6,2),
    language           TEXT,
    level              TEXT,
    department         TEXT,
    delivery_mode      TEXT,                     -- Öğrenim Türü: Örgün Öğretim …
    teaching_mode      TEXT,                     -- Dersin Öğretim Şekli: Yüz Yüze | Uzaktan …
    course_type        TEXT,
    objective          TEXT,
    content            TEXT,
    prerequisites      TEXT,
    coordinator        TEXT,
    instructors        TEXT,
    assistants         TEXT,
    details            JSONB NOT NULL DEFAULT '{}'::jsonb,
    source_url         TEXT,
    detail_scraped_at  TIMESTAMPTZ,
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS courses_program_idx ON courses(program_id);
ALTER TABLE courses ADD COLUMN IF NOT EXISTS lab_hours INT;
ALTER TABLE courses ADD COLUMN IF NOT EXISTS teaching_mode TEXT;
-- İçerik aynı kodlu başka bir kopyanın sayfasından alındıysa o ders (bkz. --pool-details=shared).
ALTER TABLE courses ADD COLUMN IF NOT EXISTS detail_source_id BIGINT REFERENCES courses(id) ON DELETE SET NULL;
-- sitede ders sayfası boş ("Dersin Ayrıntıları: Kayıt Yok...")
ALTER TABLE courses ADD COLUMN IF NOT EXISTS detail_empty BOOLEAN NOT NULL DEFAULT false;
CREATE INDEX IF NOT EXISTS courses_code_idx ON courses(code);
CREATE INDEX IF NOT EXISTS courses_detail_scraped_idx ON courses(detail_scraped_at);

-- Ders planı: bir ders birden fazla programın planında yer alabilir (N:M).
-- Seçmeli dersler iki katmanlıdır: grup satırı (is_elective_group, kaç ders seçileceği
-- group_course_count) ve ona elective_group_id ile bağlı havuz dersleri.
CREATE TABLE IF NOT EXISTS program_courses (
    program_id          BIGINT NOT NULL REFERENCES programs(id) ON DELETE CASCADE,
    course_id           BIGINT NOT NULL REFERENCES courses(id) ON DELETE CASCADE,
    semester            INT,
    course_type         TEXT,
    delivery_mode       TEXT,                    -- Öğretim Şekli: Yüz Yüze | Uzaktan …
    is_elective_group   BOOLEAN NOT NULL DEFAULT false,
    group_course_count  INT,                     -- grup satırında: seçilecek ders adedi
    elective_group_id   BIGINT,                  -- havuz dersinde: bağlı olduğu grup (course_id)
    ordinal             INT,
    PRIMARY KEY (program_id, course_id)
);
ALTER TABLE program_courses ADD COLUMN IF NOT EXISTS delivery_mode TEXT;
ALTER TABLE program_courses ADD COLUMN IF NOT EXISTS is_elective_group BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE program_courses ADD COLUMN IF NOT EXISTS group_course_count INT;
ALTER TABLE program_courses ADD COLUMN IF NOT EXISTS elective_group_id BIGINT;
CREATE INDEX IF NOT EXISTS program_courses_course_idx ON program_courses(course_id);
-- Ders planı yılı. OİBS her yılın planına ayrı ders ID'leri verir; programs.curriculum_year güncel yıldır,
-- daha eski yıllar (--years) aynı tabloda kendi plan_year değeriyle durur.
ALTER TABLE program_courses ADD COLUMN IF NOT EXISTS plan_year INT;
UPDATE program_courses pc SET plan_year = p.curriculum_year
FROM programs p WHERE pc.program_id = p.id AND pc.plan_year IS NULL AND p.curriculum_year IS NOT NULL;
CREATE INDEX IF NOT EXISTS program_courses_year_idx ON program_courses(program_id, plan_year);
-- Sitedeki plan kimliği: bir yılda birden çok plan olabilir (ör. aynı programda iki ayrı "2026" planı).
ALTER TABLE program_courses ADD COLUMN IF NOT EXISTS plan_id TEXT;
CREATE INDEX IF NOT EXISTS program_courses_group_idx ON program_courses(program_id, elective_group_id);

CREATE TABLE IF NOT EXISTS course_learning_outcomes (
    course_id    BIGINT NOT NULL REFERENCES courses(id) ON DELETE CASCADE,
    ordinal      INT NOT NULL,
    no           INT,
    description  TEXT,
    extra        TEXT,
    PRIMARY KEY (course_id, ordinal)
);

CREATE TABLE IF NOT EXISTS course_weekly_topics (
    course_id    BIGINT NOT NULL REFERENCES courses(id) ON DELETE CASCADE,
    ordinal      INT NOT NULL,
    week         INT,
    topic        TEXT,
    preparation  TEXT,
    documents    TEXT,
    PRIMARY KEY (course_id, ordinal)
);
ALTER TABLE course_weekly_topics ADD COLUMN IF NOT EXISTS documents TEXT;

CREATE TABLE IF NOT EXISTS course_resources (
    course_id  BIGINT NOT NULL REFERENCES courses(id) ON DELETE CASCADE,
    ordinal    INT NOT NULL,
    kind       TEXT,
    content    TEXT,
    PRIMARY KEY (course_id, ordinal)
);

CREATE TABLE IF NOT EXISTS course_assessments (
    course_id   BIGINT NOT NULL REFERENCES courses(id) ON DELETE CASCADE,
    ordinal     INT NOT NULL,
    activity    TEXT,
    count       INT,
    percentage  NUMERIC(6,2),
    PRIMARY KEY (course_id, ordinal)
);

CREATE TABLE IF NOT EXISTS course_workloads (
    course_id  BIGINT NOT NULL REFERENCES courses(id) ON DELETE CASCADE,
    ordinal    INT NOT NULL,
    activity   TEXT,
    count      NUMERIC(8,2),
    hours      NUMERIC(8,2),
    total      NUMERIC(8,2),
    PRIMARY KEY (course_id, ordinal)
);

-- Ders ↔ program çıktısı katkı matrisi (1-5)
CREATE TABLE IF NOT EXISTS course_program_outcomes (
    course_id           BIGINT NOT NULL REFERENCES courses(id) ON DELETE CASCADE,
    program_outcome_id  BIGINT NOT NULL REFERENCES program_outcomes(id) ON DELETE CASCADE,
    contribution        SMALLINT CHECK (contribution BETWEEN 0 AND 5),
    PRIMARY KEY (course_id, program_outcome_id)
);
CREATE INDEX IF NOT EXISTS course_program_outcomes_po_idx ON course_program_outcomes(program_outcome_id);

-- Ayrıntılı matris: dersin hangi öğrenme çıktısı (lo_no → course_learning_outcomes.no) hangi
-- program çıktısına ne düzeyde katkı veriyor. course_program_outcomes bunun PÇ başına en yükseğidir.
CREATE TABLE IF NOT EXISTS course_outcome_matrix (
    course_id           BIGINT NOT NULL REFERENCES courses(id) ON DELETE CASCADE,
    lo_no               INT NOT NULL,
    program_outcome_id  BIGINT NOT NULL REFERENCES program_outcomes(id) ON DELETE CASCADE,
    contribution        SMALLINT CHECK (contribution BETWEEN 0 AND 5),
    PRIMARY KEY (course_id, lo_no, program_outcome_id)
);
CREATE INDEX IF NOT EXISTS course_outcome_matrix_po_idx ON course_outcome_matrix(program_outcome_id);

CREATE TABLE IF NOT EXISTS scrape_runs (
    id           BIGSERIAL PRIMARY KEY,
    started_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at  TIMESTAMPTZ,
    status       TEXT NOT NULL DEFAULT 'running',
    args         JSONB,
    programs     INT,
    courses      INT,
    errors       INT
);

CREATE TABLE IF NOT EXISTS scrape_errors (
    id       BIGSERIAL PRIMARY KEY,
    run_id   BIGINT REFERENCES scrape_runs(id) ON DELETE CASCADE,
    url      TEXT,
    message  TEXT,
    at       TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS scrape_errors_run_idx ON scrape_errors(run_id);

CREATE OR REPLACE VIEW v_course_outcome_matrix AS
SELECT p.id  AS program_id,
       p.name AS program_name,
       c.id  AS course_id,
       c.code,
       c.name AS course_name,
       po.no AS outcome_no,
       po.description AS outcome,
       cpo.contribution
FROM course_program_outcomes cpo
JOIN courses c           ON c.id = cpo.course_id
JOIN program_outcomes po ON po.id = cpo.program_outcome_id
JOIN programs p          ON p.id = po.program_id;

CREATE OR REPLACE VIEW v_curriculum AS
SELECT u.name AS unit_name, p.id AS program_id, p.name AS program_name, p.level,
       pc.semester, c.id AS course_id, c.code, c.name AS course_name,
       pc.course_type, c.theory_hours, c.practice_hours, c.credit, c.ects,
       -- yeni sütunlar sona eklenir (CREATE OR REPLACE VIEW sütun sırasını değiştiremez)
       c.lab_hours, pc.delivery_mode, pc.is_elective_group, pc.group_course_count,
       pc.elective_group_id, g.code AS elective_group_code, g.name AS elective_group_name,
       pc.plan_year, (pc.plan_year IS NOT DISTINCT FROM p.curriculum_year) AS is_current, pc.plan_id
FROM program_courses pc
JOIN programs p ON p.id = pc.program_id
JOIN courses c  ON c.id = pc.course_id
LEFT JOIN courses g ON g.id = pc.elective_group_id
LEFT JOIN units u ON u.id = p.unit_id;

CREATE OR REPLACE VIEW v_course_lo_po_matrix AS
SELECT c.id AS course_id, c.code, c.name AS course_name,
       m.lo_no, lo.description AS learning_outcome,
       po.program_id, po.no AS outcome_no, po.description AS program_outcome,
       m.contribution
FROM course_outcome_matrix m
JOIN courses c           ON c.id = m.course_id
JOIN program_outcomes po ON po.id = m.program_outcome_id
LEFT JOIN course_learning_outcomes lo ON lo.course_id = m.course_id AND lo.no = m.lo_no;

-- Bu sütundan önce yazılmış boş sayfaları işaretle (yer tutucu metin details'ten silinir; tekrar çalışınca eşleşmez).
UPDATE courses c SET detail_empty = true, details = '{}'::jsonb
WHERE NOT c.detail_empty AND c.detail_scraped_at IS NOT NULL AND c.details <> '{}'::jsonb
  AND NOT EXISTS (SELECT 1 FROM jsonb_each_text(c.details) kv WHERE kv.value NOT LIKE 'Kayıt Yok%')
  AND NOT EXISTS (SELECT 1 FROM course_learning_outcomes x WHERE x.course_id = c.id)
  AND NOT EXISTS (SELECT 1 FROM course_weekly_topics x WHERE x.course_id = c.id)
  AND NOT EXISTS (SELECT 1 FROM course_assessments x WHERE x.course_id = c.id)
  AND NOT EXISTS (SELECT 1 FROM course_workloads x WHERE x.course_id = c.id);
