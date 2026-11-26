// Command initschema initializes and migrates the short-link MySQL schemas.
//
// Safety model (replaces the old unconditional DROP TABLE behavior):
//   - Base DDL (sql/01..05) contains DROP TABLE IF EXISTS. It is executed only
//     when the target database has NO tables, so a rerun never wipes a populated
//     database. Use --fresh to explicitly force a rebuild.
//   - Incremental migrations (sql/30..) contain ALTER TABLE ADD COLUMN, which is
//     NOT idempotent. Applied versions are recorded in schema_migrations and
//     skipped on subsequent runs. On an existing (already migrated) database,
//     --baseline marks current migrations as applied without re-running them.
//
// Modes:
//
//	(default)   idempotent init + migrate (safe to rerun)
//	--baseline  mark all current migrations as applied, then run pending base DDL only
//	--fresh     DROP and rebuild base DDL, then run all migrations (DESTRUCTIVE)
//	--check     print what would be done, execute nothing
package main

import (
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	_ "github.com/go-sql-driver/mysql"
)

// baseSchema maps a DDL file to the database it owns. If that database already
// has any table, base DDL is skipped (it would otherwise drop them).
type baseSchema struct {
	file string
	db   string
}

var baseSchemas = []baseSchema{
	{"01_aqicloud_account.sql", "aqicloud_account"},
	{"02_aqicloud_link_0.sql", "aqicloud_link_0"},
	{"03_aqicloud_link_1.sql", "aqicloud_link_1"},
	{"04_aqicloud_link_a.sql", "aqicloud_link_a"},
	{"05_aqicloud_shop.sql", "aqicloud_shop"},
}

// migrations are applied in this order; the version is the filename stem.
var migrations = []string{
	"30_ab_test.sql",
	"31_oidc_migration.sql",
	"32_p0_password.sql",
	"33_p2_migration.sql",
	"34_p3_migration.sql",
}

// migrationLedgerDB holds schema_migrations. All migrations share one connection,
// so a single ledger for the whole set is sufficient.
const migrationLedgerDB = "aqicloud_link_0"

const createLedgerSQL = `CREATE TABLE IF NOT EXISTS ` + migrationLedgerDB + `.schema_migrations (
	version VARCHAR(128) NOT NULL PRIMARY KEY,
	applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`

var (
	setNamesRe = regexp.MustCompile(`(?im)^\s*SET\s+NAMES\s+utf8mb4\s*;\s*$`)
	setFKRe    = regexp.MustCompile(`(?im)^\s*SET\s+FOREIGN_KEY_CHECKS\s*=\s*[01]\s*;\s*$`)
	useStmtRe  = regexp.MustCompile("(?im)^\\s*USE\\s+`[^`]+`\\s*;\\s*$")
)

// normalizeSQL strips statements the Go driver cannot run in a multi-statement
// batch. USE is removed because base DDL declares CREATE DATABASE/USE itself
// and migrations fully qualify objects as db.table; the DSN has no default db.
func normalizeSQL(stmt string) string {
	stmt = setNamesRe.ReplaceAllString(stmt, "")
	stmt = setFKRe.ReplaceAllString(stmt, "")
	stmt = useStmtRe.ReplaceAllString(stmt, "")
	return stmt
}

type options struct {
	sqlDir   string
	fresh    bool
	baseline bool
	check    bool
}

type dbConfig struct {
	host, port, user, pwd string
}

func (c dbConfig) dsn() string {
	return fmt.Sprintf("%s:%s@tcp(%s:%s)/?charset=utf8mb4&parseTime=true&multiStatements=true",
		c.user, c.pwd, c.host, c.port)
}

func dbConfigFromEnv() dbConfig {
	return dbConfig{
		host: getEnv("MYSQL_HOST", "localhost"),
		port: getEnv("MYSQL_PORT", "3306"),
		user: getEnv("MYSQL_USER", "root"),
		pwd:  getEnv("MYSQL_PWD", ""),
	}
}

func getEnv(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

// plan is the work computed before any mutation.
type plan struct {
	base    []baseSchema
	migrate []string
	skip    []string
}

// tableCount returns the number of tables in a schema.
func tableCount(db *sql.DB, schema string) (int, error) {
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = ?`, schema).Scan(&n)
	return n, err
}

// ledgerExists checks for the version table without creating it (--check must
// stay read-only).
func ledgerExists(db *sql.DB) (bool, error) {
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = ? AND table_name = 'schema_migrations'`, migrationLedgerDB).Scan(&n)
	return n > 0, err
}

// appliedVersions reads the ledger. An empty ledger is valid for a fresh DB.
func appliedVersions(db *sql.DB) (map[string]bool, error) {
	rows, err := db.Query(`SELECT version FROM ` + migrationLedgerDB + `.schema_migrations`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	applied := map[string]bool{}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		applied[v] = true
	}
	return applied, rows.Err()
}

// computePlan is a pure decision function: given DB observations, decide what
// to execute. Kept separate from buildPlan so the safety rules are testable
// without a live MySQL.
func computePlan(tableCounts map[string]int, applied map[string]bool, opt options) plan {
	var p plan

	for _, b := range baseSchemas {
		n := tableCounts[b.db]
		if n > 0 && !opt.fresh {
			p.skip = append(p.skip, fmt.Sprintf("base %s (库 %s 已有 %d 张表)", b.file, b.db, n))
			continue
		}
		p.base = append(p.base, b)
	}

	for _, m := range migrations {
		version := strings.TrimSuffix(m, ".sql")
		if applied[version] {
			p.skip = append(p.skip, fmt.Sprintf("migration %s (已记录)", version))
			continue
		}
		if opt.baseline {
			p.skip = append(p.skip, fmt.Sprintf("migration %s (baseline 标记为已执行)", version))
			continue
		}
		// A missing ledger means nothing was applied yet; buildPlan creates it
		// before execution (except in --check, which never writes).
		p.migrate = append(p.migrate, m)
	}
	return p
}

// buildPlan computes idempotent work using DB reads only.
func buildPlan(db *sql.DB, opt options) (plan, error) {
	counts := map[string]int{}
	for _, b := range baseSchemas {
		n, err := tableCount(db, b.db)
		if err != nil {
			return plan{}, fmt.Errorf("count tables in %s: %w", b.db, err)
		}
		counts[b.db] = n
	}

	exists, err := ledgerExists(db)
	if err != nil {
		return plan{}, fmt.Errorf("check migration ledger: %w", err)
	}

	applied := map[string]bool{}
	if exists {
		applied, err = appliedVersions(db)
		if err != nil {
			return plan{}, fmt.Errorf("read migration ledger: %w", err)
		}
	} else if !opt.check {
		if _, err := db.Exec(createLedgerSQL); err != nil {
			return plan{}, fmt.Errorf("create migration ledger: %w", err)
		}
	}

	return computePlan(counts, applied, opt), nil
}

func run(db *sql.DB, opt options) error {
	p, err := buildPlan(db, opt)
	if err != nil {
		return err
	}

	fmt.Println("--- Plan ---")
	for _, b := range p.base {
		verb := "EXEC base"
		if opt.fresh {
			verb = "REBUILD base"
		}
		fmt.Printf("  %s %s\n", verb, b.file)
	}
	for _, m := range p.migrate {
		fmt.Printf("  APPLY migration %s\n", m)
	}
	for _, s := range p.skip {
		fmt.Printf("  SKIP %s\n", s)
	}

	if opt.check {
		fmt.Println("--- check 模式：未执行任何变更 ---")
		return nil
	}

	for _, b := range p.base {
		content, err := os.ReadFile(filepath.Join(opt.sqlDir, b.file))
		if err != nil {
			return fmt.Errorf("read %s: %w", b.file, err)
		}
		if _, err := db.Exec(normalizeSQL(string(content))); err != nil {
			return fmt.Errorf("execute base %s: %w", b.file, err)
		}
		fmt.Printf("OK: %s\n", b.file)
	}

	for _, m := range p.migrate {
		content, err := os.ReadFile(filepath.Join(opt.sqlDir, m))
		if err != nil {
			return fmt.Errorf("read %s: %w", m, err)
		}
		if _, err := db.Exec(normalizeSQL(string(content))); err != nil {
			return fmt.Errorf("execute migration %s: %w", m, err)
		}
		version := strings.TrimSuffix(m, ".sql")
		if _, err := db.Exec(`INSERT INTO `+migrationLedgerDB+`.schema_migrations (version) VALUES (?)`, version); err != nil {
			return fmt.Errorf("record migration %s: %w", version, err)
		}
		fmt.Printf("OK: %s (recorded)\n", m)
	}

	if opt.baseline {
		for _, m := range migrations {
			version := strings.TrimSuffix(m, ".sql")
			if _, err := db.Exec(`INSERT IGNORE INTO `+migrationLedgerDB+`.schema_migrations (version) VALUES (?)`, version); err != nil {
				return fmt.Errorf("baseline record %s: %w", version, err)
			}
		}
		fmt.Println("Baseline recorded for all current migrations")
	}
	return nil
}

func main() {
	log.SetFlags(0)
	var opt options
	flag.StringVar(&opt.sqlDir, "sql-dir", "sql", "directory containing SQL files")
	flag.BoolVar(&opt.fresh, "fresh", false, "DESTRUCTIVE: drop and rebuild base DDL")
	flag.BoolVar(&opt.baseline, "baseline", false, "mark current migrations as applied without running them")
	flag.BoolVar(&opt.check, "check", false, "print pending work only, execute nothing")
	flag.Parse()

	cfg := dbConfigFromEnv()
	db, err := sql.Open("mysql", cfg.dsn())
	if err != nil {
		log.Fatalf("connect failed: %v", err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		log.Fatalf("ping failed: %v", err)
	}
	fmt.Println("MySQL connected OK")

	if err := run(db, opt); err != nil {
		log.Fatalf("initschema failed: %v", err)
	}
	fmt.Println("Done")
}
