-- Mersin Üniversitesi Bologna Bilgi Paketi şeması
-- Doğal anahtarlar OİBS'nin kendi ID'leridir: curUnit, curSunit, curCourse.
-- Tüm komutlar idempotent: `bologna migrate` tekrar tekrar çalıştırılabilir.

CREATE TABLE IF NOT EXISTS units (
    id          BIGINT PRIMARY KEY,              -- curUnit (fakülte / enstitü / MYO)
    name        TEXT,
    level       TEXT,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

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
    info                JSONB NOT NULL DEFAULT '{}'::jsonb,   -- ham anahtar/değer
    sections            JSONB NOT NULL DEFAULT '{}'::jsonb,   -- tüm bölümler, ham
    source_url          TEXT,
    scraped_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS programs_unit_idx ON programs(unit_id);

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
    credit             NUMERIC(6,2),
    ects               NUMERIC(6,2),
    language           TEXT,
    level              TEXT,
    department         TEXT,
    delivery_mode      TEXT,
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
CREATE INDEX IF NOT EXISTS courses_code_idx ON courses(code);

-- Ders planı: bir ders birden fazla programın planında yer alabilir (N:M).
CREATE TABLE IF NOT EXISTS program_courses (
    program_id   BIGINT NOT NULL REFERENCES programs(id) ON DELETE CASCADE,
    course_id    BIGINT NOT NULL REFERENCES courses(id) ON DELETE CASCADE,
    semester     INT,
    course_type  TEXT,
    ordinal      INT,
    PRIMARY KEY (program_id, course_id)
);
CREATE INDEX IF NOT EXISTS program_courses_course_idx ON program_courses(course_id);

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
    PRIMARY KEY (course_id, ordinal)
);

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
    contribution        SMALLINT,
    PRIMARY KEY (course_id, program_outcome_id)
);

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
       pc.course_type, c.theory_hours, c.practice_hours, c.credit, c.ects
FROM program_courses pc
JOIN programs p ON p.id = pc.program_id
JOIN courses c  ON c.id = pc.course_id
LEFT JOIN units u ON u.id = p.unit_id;
