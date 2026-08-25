package store

// schemaVersion is the single migration version. Bumping it with an appended
// migration block keeps the database evolution explicit and auditable.
const schemaVersion = 1

// migrations is an ordered list of idempotent migration statements. Each
// entry is applied inside a transaction guarded by a schema version table so
// that restarts never re-apply an already-installed migration.
var migrations = []string{
	// --- version 1: base schema ---
	`
	CREATE TABLE IF NOT EXISTS garden_plots (
		id   TEXT PRIMARY KEY,
		name TEXT NOT NULL
	);

	CREATE TABLE IF NOT EXISTS picking_rounds (
		id   TEXT PRIMARY KEY,
		name TEXT NOT NULL
	);

	CREATE TABLE IF NOT EXISTS plot_rounds (
		plot_id  TEXT NOT NULL REFERENCES garden_plots(id),
		round_id TEXT NOT NULL REFERENCES picking_rounds(id),
		PRIMARY KEY (plot_id, round_id)
	);

	CREATE TABLE IF NOT EXISTS processing_rule_versions (
		version    TEXT NOT NULL,
		digest     TEXT PRIMARY KEY,
		published  INTEGER NOT NULL DEFAULT 0,
		current    INTEGER NOT NULL DEFAULT 0,
		thresholds TEXT NOT NULL
	);

	CREATE TABLE IF NOT EXISTS rule_plot_rounds (
		digest   TEXT NOT NULL REFERENCES processing_rule_versions(digest),
		plot_id  TEXT NOT NULL,
		round_id TEXT NOT NULL,
		PRIMARY KEY (digest, plot_id, round_id)
	);

	CREATE TABLE IF NOT EXISTS withering_templates (
		digest     TEXT NOT NULL REFERENCES processing_rule_versions(digest),
		ordinal    INTEGER NOT NULL,
		time_point INTEGER NOT NULL,
		PRIMARY KEY (digest, ordinal)
	);

	CREATE TABLE IF NOT EXISTS resource_capabilities (
		resource_type TEXT NOT NULL,
		resource_id   TEXT NOT NULL,
		PRIMARY KEY (resource_type, resource_id)
	);

	CREATE TABLE IF NOT EXISTS personnel (
		id        TEXT PRIMARY KEY,
		name      TEXT NOT NULL,
		qualified INTEGER NOT NULL DEFAULT 0
	);

	CREATE TABLE IF NOT EXISTS personnel_rounds (
		personnel_id TEXT NOT NULL REFERENCES personnel(id),
		plot_id      TEXT NOT NULL,
		round_id     TEXT NOT NULL,
		PRIMARY KEY (personnel_id, plot_id, round_id)
	);

	CREATE TABLE IF NOT EXISTS tasks (
		id                     TEXT PRIMARY KEY,
		leaf_batch             TEXT NOT NULL,
		garden_plot            TEXT NOT NULL,
		picking_round          TEXT NOT NULL,
		rule_digest            TEXT NOT NULL DEFAULT '',
		generation             INTEGER NOT NULL DEFAULT 1,
		state                  TEXT NOT NULL,
		snapshot_json          TEXT NOT NULL DEFAULT '{}',
		version                INTEGER NOT NULL DEFAULT 0,
		logical_time           INTEGER NOT NULL DEFAULT 0,
		terminal_cmd           TEXT NOT NULL DEFAULT '',
		terminal_op            TEXT NOT NULL DEFAULT '',
		terminal_at            INTEGER NOT NULL DEFAULT 0,
		fixation_credential_id TEXT NOT NULL DEFAULT '',
		fixation_confirmed     INTEGER NOT NULL DEFAULT 0
	);

	CREATE UNIQUE INDEX IF NOT EXISTS idx_tasks_leaf_batch
		ON tasks(leaf_batch);

	CREATE TABLE IF NOT EXISTS basket_samples (
		seal    TEXT PRIMARY KEY,
		task_id TEXT NOT NULL REFERENCES tasks(id)
	);

	CREATE TABLE IF NOT EXISTS blind_samples (
		code       TEXT PRIMARY KEY,
		task_id    TEXT NOT NULL REFERENCES tasks(id),
		commitment TEXT NOT NULL,
		sealed     INTEGER NOT NULL DEFAULT 0,
		revealed   INTEGER NOT NULL DEFAULT 0
	);

	CREATE TABLE IF NOT EXISTS tenderness_points (
		id      TEXT NOT NULL,
		task_id TEXT NOT NULL REFERENCES tasks(id),
		PRIMARY KEY (id)
	);

	CREATE TABLE IF NOT EXISTS resource_leases (
		resource_type  TEXT NOT NULL,
		resource_id    TEXT NOT NULL,
		task_id        TEXT NOT NULL REFERENCES tasks(id),
		generation     INTEGER NOT NULL,
		version        INTEGER NOT NULL DEFAULT 1,
		released       INTEGER NOT NULL DEFAULT 0,
		release_reason TEXT NOT NULL DEFAULT '',
		PRIMARY KEY (resource_type, resource_id)
	);

	CREATE INDEX IF NOT EXISTS idx_leases_task ON resource_leases(task_id);

	CREATE TABLE IF NOT EXISTS withering_cells (
		task_id          TEXT NOT NULL REFERENCES tasks(id),
		time_point       INTEGER NOT NULL,
		basket           TEXT NOT NULL,
		env_humidity     TEXT NOT NULL,
		moisture_content TEXT NOT NULL,
		leaf_temperature TEXT NOT NULL,
		water_loss       TEXT NOT NULL,
		red_leaf_ratio   TEXT NOT NULL,
		supplemental     INTEGER NOT NULL DEFAULT 0,
		digest           TEXT NOT NULL,
		PRIMARY KEY (task_id, time_point, basket)
	);

	CREATE TABLE IF NOT EXISTS tenderness_evidence (
		task_id          TEXT NOT NULL REFERENCES tasks(id),
		version          INTEGER NOT NULL,
		operation        TEXT NOT NULL,
		generation       INTEGER NOT NULL,
		single_bud       INTEGER NOT NULL,
		one_bud_one_leaf INTEGER NOT NULL,
		old_leaf         INTEGER NOT NULL,
		red_leaf         INTEGER NOT NULL,
		total            INTEGER NOT NULL,
		digest           TEXT NOT NULL,
		PRIMARY KEY (task_id, version)
	);

	CREATE TABLE IF NOT EXISTS assay_evidence (
		task_id    TEXT NOT NULL REFERENCES tasks(id),
		version    INTEGER NOT NULL,
		well       TEXT NOT NULL,
		blind_code TEXT NOT NULL,
		generation INTEGER NOT NULL,
		inhibition TEXT NOT NULL,
		digest     TEXT NOT NULL,
		PRIMARY KEY (task_id, version)
	);

	CREATE TABLE IF NOT EXISTS retest_evidence (
		task_id          TEXT NOT NULL REFERENCES tasks(id),
		version          INTEGER NOT NULL,
		generation       INTEGER NOT NULL,
		leaf_temperature TEXT NOT NULL,
		moisture_content TEXT NOT NULL,
		digest           TEXT NOT NULL,
		PRIMARY KEY (task_id, version)
	);

	CREATE TABLE IF NOT EXISTS device_attempts (
		task_id      TEXT NOT NULL REFERENCES tasks(id),
		call_key     TEXT NOT NULL,
		attempt_seq  INTEGER NOT NULL,
		logical_time INTEGER NOT NULL,
		device_kind  TEXT NOT NULL,
		retryable    INTEGER NOT NULL DEFAULT 0,
		succeeded    INTEGER NOT NULL DEFAULT 0,
		result_json  TEXT NOT NULL DEFAULT '',
		PRIMARY KEY (task_id, call_key, attempt_seq)
	);

	CREATE TABLE IF NOT EXISTS rejudgment_cases (
		task_id         TEXT NOT NULL REFERENCES tasks(id),
		generation      INTEGER NOT NULL,
		prev_generation INTEGER,
		affected_json   TEXT NOT NULL,
		PRIMARY KEY (task_id, generation)
	);

	CREATE TABLE IF NOT EXISTS receipts (
		task_id      TEXT NOT NULL REFERENCES tasks(id),
		personnel_id TEXT NOT NULL,
		PRIMARY KEY (task_id, personnel_id)
	);

	CREATE TABLE IF NOT EXISTS review_decisions (
		task_id      TEXT NOT NULL REFERENCES tasks(id),
		personnel_id TEXT NOT NULL,
		approved     INTEGER NOT NULL,
		generation   INTEGER NOT NULL,
		PRIMARY KEY (task_id, personnel_id)
	);

	CREATE TABLE IF NOT EXISTS fixation_credentials (
		id        TEXT PRIMARY KEY,
		task_id   TEXT NOT NULL REFERENCES tasks(id),
		issued_at INTEGER NOT NULL,
		confirmed INTEGER NOT NULL DEFAULT 0
	);

	CREATE TABLE IF NOT EXISTS terminal_decisions (
		task_id                TEXT PRIMARY KEY REFERENCES tasks(id),
		command                TEXT NOT NULL,
		operation              TEXT NOT NULL,
		fixation_credential_id TEXT NOT NULL DEFAULT ''
	);

	-- The idempotency key is (operation_id, task_id): an operation id scopes
	-- to a single client request against a single task. The same op id replayed
	-- against a different task is a distinct operation and must not replay the
	-- first task's result. Using a composite primary key instead of operation_id
	-- alone prevents a shared operation id from replaying one task's cached
	-- response for a different task (cross-task aliasing).
	CREATE TABLE IF NOT EXISTS idempotency_records (
		operation_id   TEXT NOT NULL,
		task_id        TEXT NOT NULL,
		generation     INTEGER NOT NULL,
		request_digest TEXT NOT NULL,
		response_json  TEXT NOT NULL,
		PRIMARY KEY (operation_id, task_id)
	);
	`,
}
