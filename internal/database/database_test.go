package database_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"queuectl/internal/database"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func TestDatabase_ConnectAndMigrate(t *testing.T) {
	db, err := database.Connect(":memory:")
	require.NoError(t, err)
	defer db.Close()

	ctx := context.Background()

	// Verify database connection is alive
	err = db.PingContext(ctx)
	require.NoError(t, err)

	// Run migrations
	err = database.RunMigrations(ctx, db)
	require.NoError(t, err)

	// Verify jobs table was created
	var count int
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM jobs").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count)

	// Verify workers table was created
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM workers").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count)

	// Verify execution_logs table was created
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM execution_logs").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestDatabase_RunMigrationsContextCancel(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	defer db.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Pre-cancel context

	err = database.RunMigrations(ctx, db)
	assert.Error(t, err)
}

func TestDatabase_ConnectNestedDir(t *testing.T) {
	tmpDir := t.TempDir()
	nestedPath := filepath.Join(tmpDir, "nested/subdir/queue.db")

	db, err := database.Connect(nestedPath)
	require.NoError(t, err)
	defer db.Close()

	err = db.Ping()
	assert.NoError(t, err)
}

func TestDatabase_ConnectDirectoryError(t *testing.T) {
	tmpDir := t.TempDir()

	// Connect with directory path instead of file path to trigger SQLite error
	_, err := database.Connect(tmpDir)
	assert.Error(t, err)
}

func TestDatabase_ConnectDirectoryCreateError(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "file.txt")

	err := os.WriteFile(filePath, []byte("data"), 0644)
	require.NoError(t, err)

	// Sub-directory of a file path cannot be created
	badDbPath := filepath.Join(filePath, "nested/db.sqlite")
	_, err = database.Connect(badDbPath)
	assert.Error(t, err)
}
