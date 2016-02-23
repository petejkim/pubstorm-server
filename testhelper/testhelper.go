package testhelper

import (
	"database/sql"
	"fmt"
)

var tables []string

func TruncateTables(db *sql.DB) error {
	if len(tables) == 0 {
		tbls, err := getTables(db)
		if err != nil {
			return err
		}
		tables = tbls
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}

	if _, err := tx.Exec(`SET local session_replication_role TO 'replica'`); err != nil {
		tx.Rollback()
		return err
	}

	for _, tb := range tables {
		if _, err := tx.Exec(fmt.Sprintf(`DELETE FROM %s`, tb)); err != nil {
			tx.Rollback()
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		tx.Rollback()
		return err
	}

	return nil
}

func getTables(db *sql.DB) ([]string, error) {
	rows, err := db.Query(`SELECT tablename FROM pg_catalog.pg_tables WHERE schemaname = 'public' AND tablename <> 'schema_migrations';`)
	if err != nil {
		return []string{}, err
	}
	defer rows.Close()
	tbls := []string{}
	for rows.Next() {
		var tablename string
		err = rows.Scan(&tablename)
		if err != nil {
			return []string{}, err
		}
		tbls = append(tbls, tablename)
	}
	return tbls, nil
}
