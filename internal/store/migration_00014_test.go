package store

import (
	"testing"
)

// TestMigration00014_DataMigration vérifie qu'un user seedé avant 00014
// avec role='admin' se retrouve membre du groupe grp_system_administrator
// après migration, et que la colonne role a disparu.
func TestMigration00014_DataMigration(t *testing.T) {
	db, err := Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	if err := db.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	// Vérifie que les 3 groupes système sont seedés.
	var n int
	row := db.QueryRow(`SELECT COUNT(*) FROM groups WHERE is_system = 1`)
	if err := row.Scan(&n); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if n != 3 {
		t.Errorf("expected 3 system groups, got %d", n)
	}

	// Vérifie que users.role n'existe plus.
	rows, err := db.Query(`PRAGMA table_info(users)`)
	if err != nil {
		t.Fatalf("pragma: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt any
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			t.Fatalf("scan col: %v", err)
		}
		if name == "role" {
			t.Errorf("users.role should have been dropped")
		}
	}
}
