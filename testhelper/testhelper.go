package testhelper

import (
	"database/sql"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	. "github.com/onsi/gomega"
)

var tables []string

func TruncateTables(db *sql.DB) {
	if len(tables) == 0 {
		tbls, err := getTables(db)
		Expect(err).To(BeNil())
		tables = tbls
	}

	tx, err := db.Begin()
	if err != nil {
		Expect(err).To(BeNil())
	}

	if _, err := tx.Exec(`SET local session_replication_role TO 'replica'`); err != nil {
		tx.Rollback()
		Expect(err).To(BeNil())
	}

	for _, tb := range tables {
		if _, err := tx.Exec(fmt.Sprintf(`DELETE FROM %s`, tb)); err != nil {
			tx.Rollback()
			Expect(err).To(BeNil())
		}
	}

	if err := tx.Commit(); err != nil {
		tx.Rollback()
		Expect(err).To(BeNil())
	}
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

// Do it all http request method
func MakeRequest(method, url string, data url.Values, headers http.Header, reqFn func(*http.Request)) (resp *http.Response, err error) {
	var (
		req    *http.Request
		isForm = method == "POST" || method == "PUT" || method == "PATCH"
	)

	if data == nil {
		req, err = http.NewRequest(method, url, nil)
	} else if isForm {
		r := strings.NewReader(data.Encode())
		req, err = http.NewRequest(method, url, r)
	} else {
		req, err = http.NewRequest(method, url+"?"+data.Encode(), nil)
	}

	if err != nil {
		return nil, err
	}

	if isForm {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}

	if headers != nil {
		for k, v := range headers {
			for _, h := range v {
				req.Header.Add(k, h)
			}
		}
	}

	if reqFn != nil {
		reqFn(req)
	}

	return http.DefaultClient.Do(req)
}
