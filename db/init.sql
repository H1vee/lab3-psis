-- Schema and seed data for the tea catalogue.
-- The file is executed by the postgres image on the first start of an empty
-- data volume (docker-entrypoint-initdb.d).

CREATE TABLE tea_kinds (
    id          SERIAL PRIMARY KEY,
    code        TEXT    NOT NULL UNIQUE,
    title       TEXT    NOT NULL,
    oxidation   TEXT    NOT NULL,
    sort_order  INTEGER NOT NULL
);

CREATE TABLE brewing_params (
    id           SERIAL PRIMARY KEY,
    tea_kind_id  INTEGER NOT NULL REFERENCES tea_kinds (id) ON DELETE CASCADE,
    water_min_c  INTEGER NOT NULL CHECK (water_min_c BETWEEN 40 AND 100),
    water_max_c  INTEGER NOT NULL CHECK (water_max_c BETWEEN 40 AND 100),
    seconds_min  INTEGER NOT NULL CHECK (seconds_min > 0),
    seconds_max  INTEGER NOT NULL CHECK (seconds_max > 0),
    needs_rinse  BOOLEAN NOT NULL DEFAULT FALSE,
    CONSTRAINT brewing_params_tea_kind_unique UNIQUE (tea_kind_id),
    CONSTRAINT brewing_params_water_range CHECK (water_max_c >= water_min_c),
    CONSTRAINT brewing_params_time_range CHECK (seconds_max >= seconds_min)
);

CREATE INDEX brewing_params_tea_kind_idx ON brewing_params (tea_kind_id);

INSERT INTO tea_kinds (code, title, oxidation, sort_order) VALUES
    ('white',  'Білий',   'мінімальне',  1),
    ('green',  'Зелений', 'зупинене',    2),
    ('oolong', 'Улун',    'часткове',    3),
    ('black',  'Чорний',  'повне',       4),
    ('puer',   'Пуер',    'постферментація', 5);

INSERT INTO brewing_params
    (tea_kind_id, water_min_c, water_max_c, seconds_min, seconds_max, needs_rinse)
SELECT k.id, v.water_min_c, v.water_max_c, v.seconds_min, v.seconds_max, v.needs_rinse
FROM tea_kinds k
JOIN (VALUES
    ('white',  80,  85, 120, 180, FALSE),
    ('green',  70,  80,  60, 120, FALSE),
    ('oolong', 90,  95,  60, 120, TRUE),
    ('black',  90,  95, 180, 240, FALSE),
    ('puer',   95, 100,  20,  40, TRUE)
) AS v (code, water_min_c, water_max_c, seconds_min, seconds_max, needs_rinse)
  ON v.code = k.code;
