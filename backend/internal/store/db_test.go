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
	tables := []string{"configs", "agents", "agent_configs", "alerts", "users"}
	for _, table := range tables {
		_, err := db.Exec("SELECT count(*) FROM " + table)
		if err != nil {
			t.Errorf("table %s not created: %v", table, err)
		}
	}
}

func TestMigrate_AgentConfigPushFields(t *testing.T) {
	db := newTestDB(t)
	rows, err := db.Query("SELECT error_message, pushed_by FROM agent_configs LIMIT 0")
	if err != nil {
		t.Fatalf("agent_configs missing new columns: %v", err)
	}
	rows.Close()

	rows, err = db.Query("SELECT remote_config_status FROM agents LIMIT 0")
	if err != nil {
		t.Fatalf("agents missing remote_config_status: %v", err)
	}
	rows.Close()
}
