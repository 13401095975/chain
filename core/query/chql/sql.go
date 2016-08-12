package chql

import (
	"bytes"
	"encoding/json"
	"strconv"
)

// AsSQL translates q to SQL.
func AsSQL(q Query, dataColumn string, values []interface{}) (sqlExpr SQLExpr, err error) {
	defer func() {
		r := recover()
		if e, ok := r.(error); ok {
			err = e
		} else if r != nil {
			panic(r)
		}
	}()

	return asSQL(q.expr, dataColumn, values)
}

type SQLExpr struct {
	SQL     string
	Values  []interface{}
	GroupBy [][]string
}

func asSQL(e expr, dataColumn string, values []interface{}) (exp SQLExpr, err error) {
	if e == nil {
		// An empty expression is a valid query without any filtering.
		return SQLExpr{}, nil
	}

	pvals := map[int]interface{}{}
	for i, v := range values {
		if v != nil {
			pvals[i+1] = v
		}
	}

	matches, bindings := matchingObjects(e, pvals)

	var buf bytes.Buffer
	var params []interface{}
	if len(matches) > 1 {
		buf.WriteString("(")
	}
	for i, condition := range matches {
		if i > 0 {
			buf.WriteString(" OR ")
		}

		b, err := json.Marshal(condition)
		if err != nil {
			return exp, err
		}

		params = append(params, string(b))
		buf.WriteString("(" + dataColumn + " @> $" + strconv.Itoa(len(params)) + "::jsonb)")
	}
	if len(matches) > 1 {
		buf.WriteString(")")
	}

	exp = SQLExpr{
		SQL:    buf.String(),
		Values: params,
	}
	for _, b := range bindings {
		revpath := make([]string, 0, len(b.path))
		for i := len(b.path) - 1; i >= 0; i-- {
			revpath = append(revpath, b.path[i])
		}
		exp.GroupBy = append(exp.GroupBy, revpath)
	}
	return exp, nil
}
