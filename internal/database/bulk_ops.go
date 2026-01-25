package database

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// BulkInsert performs a bulk insert operation using COPY protocol
func BulkInsert(ctx context.Context, pool *pgxpool.Pool, tableName string, columns []string, rows [][]interface{}) error {
	if len(rows) == 0 {
		return nil
	}

	// Use COPY for efficient bulk insert
	copySource := pgx.CopyFromRows(rows)
	copyCount, err := pool.CopyFrom(
		ctx,
		pgx.Identifier{tableName},
		columns,
		copySource,
	)

	if err != nil {
		return fmt.Errorf("failed to bulk insert: %w", err)
	}

	if copyCount != int64(len(rows)) {
		return fmt.Errorf("expected to insert %d rows, but inserted %d", len(rows), copyCount)
	}

	return nil
}

// BulkUpdate performs a bulk update operation using a temporary table
func BulkUpdate(ctx context.Context, pool *pgxpool.Pool, tableName string, updates []BulkUpdateItem) error {
	if len(updates) == 0 {
		return nil
	}

	// Start transaction
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// Create temporary table
	tempTable := fmt.Sprintf("temp_%s_updates", tableName)
	createTempQuery := fmt.Sprintf(`
		CREATE TEMP TABLE %s (
			id TEXT PRIMARY KEY,
			data JSONB
		) ON COMMIT DROP
	`, tempTable)

	if _, err := tx.Exec(ctx, createTempQuery); err != nil {
		return fmt.Errorf("failed to create temp table: %w", err)
	}

	// Insert updates into temp table
	for _, update := range updates {
		insertQuery := fmt.Sprintf(`
			INSERT INTO %s (id, data) VALUES ($1, $2)
		`, tempTable)

		if _, err := tx.Exec(ctx, insertQuery, update.ID, update.Data); err != nil {
			return fmt.Errorf("failed to insert into temp table: %w", err)
		}
	}

	// Perform bulk update from temp table
	// This is a simplified example - actual implementation would depend on schema
	updateQuery := fmt.Sprintf(`
		UPDATE %s t
		SET updated_at = NOW()
		FROM %s tmp
		WHERE t.id = tmp.id
	`, tableName, tempTable)

	if _, err := tx.Exec(ctx, updateQuery); err != nil {
		return fmt.Errorf("failed to bulk update: %w", err)
	}

	// Commit transaction
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit bulk update: %w", err)
	}

	return nil
}

// BulkDelete performs a bulk delete operation
func BulkDelete(ctx context.Context, pool *pgxpool.Pool, tableName string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}

	// Build placeholders for IN clause
	placeholders := make([]string, len(ids))
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}

	query := fmt.Sprintf(`
		DELETE FROM %s WHERE id IN (%s)
	`, tableName, strings.Join(placeholders, ", "))

	result, err := pool.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("failed to bulk delete: %w", err)
	}

	if result.RowsAffected() != int64(len(ids)) {
		return fmt.Errorf("expected to delete %d rows, but deleted %d", len(ids), result.RowsAffected())
	}

	return nil
}

// BulkUpdateItem represents an item to be updated
type BulkUpdateItem struct {
	ID   string
	Data interface{}
}

// BatchExecute executes multiple queries in a single transaction
func BatchExecute(ctx context.Context, pool *pgxpool.Pool, queries []BatchQuery) error {
	if len(queries) == 0 {
		return nil
	}

	// Start transaction
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// Execute all queries
	for i, query := range queries {
		if _, err := tx.Exec(ctx, query.SQL, query.Args...); err != nil {
			return fmt.Errorf("failed to execute query %d: %w", i, err)
		}
	}

	// Commit transaction
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit batch: %w", err)
	}

	return nil
}

// BatchQuery represents a query to be executed in a batch
type BatchQuery struct {
	SQL  string
	Args []interface{}
}
