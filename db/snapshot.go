package db

import (
	"context"
	"fmt"

	"github.com/hasura/pgdeltastream/types"
	"github.com/jackc/pgx"
	log "github.com/sirupsen/logrus"
)

func SnapshotData(session *types.Session, tableName string, offset, limit int) ([]map[string]interface{}, error) {
	log.Info("Begin transaction")
	tx, err := session.Conn.BeginEx(context.TODO(), &pgx.TxOptions{
		IsoLevel: pgx.RepeatableRead,
	})

	if err != nil {
		return nil, err
	}

	log.Info("Setting transaction snapshot ", session.SnapshotName)
	_, err = tx.Exec(fmt.Sprintf("SET TRANSACTION SNAPSHOT '%s'", session.SnapshotName))
	if err != nil {
		return nil, err
	}

	query := fmt.Sprintf("SELECT * FROM %s OFFSET %d LIMIT %d", tableName, offset, limit)
	log.Info("Executing query: ", query)
	rows, err := tx.Query(query)
	if err != nil {
		return nil, err
	}
	data := processRows(rows)
	log.Info("Number of results: ", len(data))

	err = tx.Commit()
	if err != nil {
		return nil, err
	}

	return data, nil
}

func processRows(rows *pgx.Rows) []map[string]interface{} {
	// put each row in a struct containing type, table and values
	fields := rows.FieldDescriptions()
	log.Info(fields)
	resultsMessage := make([]map[string]interface{}, 0)
	for rows.Next() {
		values, _ := rows.Values()
		rowJSON := make(map[string]interface{})
		for i := 0; i < len(fields); i++ {
			name := fields[i].Name
			value := values[i]
			rowJSON[name] = value
		}
		resultsMessage = append(resultsMessage, rowJSON)
	}

	return resultsMessage
}
