package store

import "testing"

func TestOpen_SQLite_InMemory(t *testing.T) {
	db, err := Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()
}

func TestMigrate(t *testing.T) {
	db, err := Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	if err := db.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	// Verify tables exist by querying them
	tables := []string{"configs", "workloads", "workload_configs", "workload_events", "alerts", "users"}
	for _, table := range tables {
		_, err := db.Exec("SELECT count(*) FROM " + table)
		if err != nil {
			t.Errorf("table %s not created: %v", table, err)
		}
	}
}

func TestMigrate_WorkloadConfigPushFields(t *testing.T) {
	db := newTestDB(t)
	assertColumns(t, db, "SELECT error_message, pushed_by FROM workload_configs LIMIT 0",
		"workload_configs missing push fields")
	assertColumns(t, db, "SELECT remote_config_status FROM workloads LIMIT 0",
		"workloads missing remote_config_status")
}

func assertColumns(t *testing.T, db *DB, query, msg string) {
	t.Helper()
	rows, err := db.Query(query)
	if err != nil {
		t.Fatalf("%s: %v", msg, err)
	}
	defer rows.Close()
}
