package main

import (
	"database/sql"
	"fmt"
	"strconv"
)

func (a *app) queryMaps(query string, args ...any) ([]map[string]any, error) {
	rows, err := a.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	out := []map[string]any{}
	for rows.Next() {
		raw := make([]sql.RawBytes, len(cols))
		dest := make([]any, len(cols))
		for i := range raw {
			dest[i] = &raw[i]
		}
		if err := rows.Scan(dest...); err != nil {
			return nil, err
		}
		m := map[string]any{}
		for i, c := range cols {
			if raw[i] == nil {
				m[c] = nil
				continue
			}
			m[c] = smartValue(string(raw[i]))
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func smartValue(s string) any {
	if s == "" {
		return ""
	}
	if isIntString(s) {
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			return n
		}
	}
	return s
}

func isIntString(s string) bool {
	for i, r := range s {
		if i == 0 && r == '-' {
			continue
		}
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func ptr(m *map[string]any, key string) any {
	var v sql.NullString
	return scannerFunc(func(src any) error {
		switch x := src.(type) {
		case nil:
			(*m)[key] = ""
		case []byte:
			v.String = string(x)
			(*m)[key] = smartValue(v.String)
		default:
			(*m)[key] = smartValue(fmt.Sprint(x))
		}
		return nil
	})
}

type scannerFunc func(any) error

func (f scannerFunc) Scan(src any) error { return f(src) }
