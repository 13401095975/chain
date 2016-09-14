package query

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/lib/pq"

	"chain/core/query/filter"
)

type OutputsAfter struct {
	lastBlockHeight uint64
	lastTxPos       uint32
	lastIndex       uint32
}

func (cur OutputsAfter) String() string {
	return fmt.Sprintf("%x-%x-%x", cur.lastBlockHeight, cur.lastTxPos, cur.lastIndex)
}

func DecodeOutputsAfter(str string) (c *OutputsAfter, err error) {
	s := strings.Split(str, "-")
	if len(s) != 3 {
		return nil, ErrBadAfter
	}
	lastBlockHeight, err := strconv.ParseUint(s[0], 16, 64)
	if err != nil {
		return nil, ErrBadAfter
	}
	lastTxPos, err := strconv.ParseUint(s[1], 16, 32)
	if err != nil {
		return nil, ErrBadAfter
	}
	lastIndex, err := strconv.ParseUint(s[2], 16, 32)
	if err != nil {
		return nil, ErrBadAfter
	}
	return &OutputsAfter{
		lastBlockHeight: lastBlockHeight,
		lastTxPos:       uint32(lastTxPos),
		lastIndex:       uint32(lastIndex),
	}, nil
}

func (ind *Indexer) Outputs(ctx context.Context, p filter.Predicate, vals []interface{}, timestampMS uint64, after *OutputsAfter, limit int) ([]interface{}, *OutputsAfter, error) {
	if len(vals) != p.Parameters {
		return nil, nil, ErrParameterCountMismatch
	}
	expr, err := filter.AsSQL(p, "data", vals)
	if err != nil {
		return nil, nil, err
	}
	queryStr, queryArgs := constructOutputsQuery(expr, timestampMS, after, limit)
	rows, err := ind.db.Query(ctx, queryStr, queryArgs...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	var newAfter OutputsAfter
	if after != nil {
		newAfter = *after
	}

	outputs := make([]interface{}, 0, limit)
	for rows.Next() {
		var (
			blockHeight uint64
			txPos       uint32
			index       uint32
			data        []byte
		)
		err = rows.Scan(&blockHeight, &txPos, &index, &data)
		if err != nil {
			return nil, nil, err
		}
		outputs = append(outputs, (*json.RawMessage)(&data))

		newAfter.lastBlockHeight = blockHeight
		newAfter.lastTxPos = txPos
		newAfter.lastIndex = index
	}
	err = rows.Err()
	if err != nil {
		return nil, nil, err
	}

	return outputs, &newAfter, nil
}

func constructOutputsQuery(expr filter.SQLExpr, timestampMS uint64, after *OutputsAfter, limit int) (string, []interface{}) {
	sql := fmt.Sprintf("SELECT block_height, tx_pos, output_index, data FROM %s", pq.QuoteIdentifier("annotated_outputs"))

	vals := make([]interface{}, 0, 4+len(expr.Values))
	vals = append(vals, expr.Values...)

	vals = append(vals, timestampMS)
	timestampValIndex := len(vals)

	where := strings.TrimSpace(expr.SQL)
	timespanExpr := fmt.Sprintf("timespan @> $%d::int8", timestampValIndex)
	if where == "" {
		where = timespanExpr
	} else {
		where = fmt.Sprintf("(%s) AND %s", where, timespanExpr)
	}

	if after != nil {
		vals = append(vals, after.lastBlockHeight)
		lastBlockHeightValIndex := len(vals)

		vals = append(vals, after.lastTxPos)
		lastTxPosValIndex := len(vals)

		vals = append(vals, after.lastIndex)
		lastIndexValIndex := len(vals)

		where = fmt.Sprintf("%s AND (block_height, tx_pos, output_index) > ($%d, $%d, $%d)", where, lastBlockHeightValIndex, lastTxPosValIndex, lastIndexValIndex)
	}

	sql += fmt.Sprintf(" WHERE %s ORDER BY block_height ASC, tx_pos ASC, output_index ASC LIMIT %d", where, limit)

	return sql, vals
}
