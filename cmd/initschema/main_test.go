package main

import (
	"strings"
	"testing"
)

func TestNormalizeSQL(t *testing.T) {
	in := `SET NAMES utf8mb4;
USE ` + "`" + `aqicloud_link_0` + "`" + `;
SET FOREIGN_KEY_CHECKS = 0;
CREATE TABLE ` + "`" + `t` + "`" + ` (id INT);
SET FOREIGN_KEY_CHECKS = 1;`
	got := normalizeSQL(in)
	for _, banned := range []string{"SET NAMES", "SET FOREIGN_KEY_CHECKS", "USE `"} {
		if strings.Contains(got, banned) {
			t.Errorf("normalizeSQL left %q in:\n%s", banned, got)
		}
	}
	if !strings.Contains(got, "CREATE TABLE `t`") {
		t.Errorf("normalizeSQL dropped the DDL:\n%s", got)
	}
}

func TestComputePlan_ExistingDBNeverDropped(t *testing.T) {
	// Every base DB already has tables; no migration ledger. Default mode must
	// skip all base DDL (the old tool would have dropped every table).
	counts := map[string]int{}
	for _, b := range baseSchemas {
		counts[b.db] = 3
	}
	p := computePlan(counts, map[string]bool{}, options{})

	if len(p.base) != 0 {
		t.Errorf("base DDL scheduled on populated DBs: %+v", p.base)
	}
	if len(p.migrate) != len(migrations) {
		t.Errorf("migrations = %d, want %d", len(p.migrate), len(migrations))
	}
	if len(p.skip) < len(baseSchemas) {
		t.Errorf("expected %d base skips, got %+v", len(baseSchemas), p.skip)
	}
}

func TestComputePlan_FreshRebuildsBase(t *testing.T) {
	counts := map[string]int{}
	for _, b := range baseSchemas {
		counts[b.db] = 5
	}
	p := computePlan(counts, map[string]bool{}, options{fresh: true})
	if len(p.base) != len(baseSchemas) {
		t.Errorf("--fresh base = %d, want %d", len(p.base), len(baseSchemas))
	}
}

func TestComputePlan_AppliedMigrationsSkipped(t *testing.T) {
	counts := map[string]int{} // empty DBs
	applied := map[string]bool{"30_ab_test": true, "31_oidc_migration": true}
	p := computePlan(counts, applied, options{})

	if len(p.base) != len(baseSchemas) {
		t.Errorf("empty DBs must get base DDL, got %d", len(p.base))
	}
	if len(p.migrate) != len(migrations)-2 {
		t.Errorf("pending migrations = %d, want %d", len(p.migrate), len(migrations)-2)
	}
	if len(p.skip) != 2 {
		t.Errorf("skip = %d, want 2 (applied migrations)", len(p.skip))
	}
}

func TestComputePlan_BaselineSkipsAllMigrations(t *testing.T) {
	counts := map[string]int{}
	for _, b := range baseSchemas {
		counts[b.db] = 2
	}
	p := computePlan(counts, map[string]bool{}, options{baseline: true})
	if len(p.migrate) != 0 {
		t.Errorf("--baseline must not execute migrations, got %+v", p.migrate)
	}
	if len(p.skip) < len(migrations) {
		t.Errorf("--baseline should report every migration as skipped, got %+v", p.skip)
	}
}

func TestComputePlan_EmptyFreshDBFullRun(t *testing.T) {
	// Brand-new install: no tables, no migrations applied.
	p := computePlan(map[string]int{}, map[string]bool{}, options{})
	if len(p.base) != len(baseSchemas) || len(p.migrate) != len(migrations) {
		t.Errorf("fresh install plan: base=%d migrate=%d", len(p.base), len(p.migrate))
	}
	if len(p.skip) != 0 {
		t.Errorf("fresh install should skip nothing, got %+v", p.skip)
	}
}

func TestMigrationsOrdered(t *testing.T) {
	// Versions must be applied in ascending order; schema_migrations stores
	// the filename stem as PK.
	for i := 1; i < len(migrations); i++ {
		if migrations[i-1] >= migrations[i] {
			t.Errorf("migrations not sorted: %q >= %q", migrations[i-1], migrations[i])
		}
	}
}
