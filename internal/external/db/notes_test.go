package db_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tung-dnt/lean-go/internal/external/db"
	"github.com/tung-dnt/lean-go/pkg/postgres"
	"github.com/tung-dnt/lean-go/pkg/testdb"
)

var (
	pool *pgxpool.Pool
	q    *db.Queries
)

// TestMain builds a throwaway schema from the embedded migrations, so these tests exercise the
// exact DDL and queries production runs, without pulling in the whole server.
func TestMain(m *testing.M) {
	os.Exit(run(m))
}

func run(m *testing.M) int {
	ctx := context.Background()

	stopDB, err := testdb.Setup(ctx)
	if err != nil {
		panic(err)
	}
	defer stopDB()

	conn := postgres.ReplaceRand(os.Getenv(testdb.EnvConnection))
	if err := postgres.CreateSchema(conn); err != nil {
		panic(err)
	}
	defer func() {
		if err := postgres.DropSchema(conn); err != nil {
			panic(err)
		}
	}()
	if err := postgres.Migrate(conn); err != nil {
		panic(err)
	}

	if pool, err = pgxpool.New(ctx, conn); err != nil {
		panic(err)
	}
	defer pool.Close()
	q = db.New(pool)

	return m.Run()
}

// Notes made in the same instant still come in one order, newest id first, so that a page boundary never repeats or
// skips one: the order of the list is total.
func TestListNotes__NewestFirstAndTotalWithinTheSameInstant(t *testing.T) {
	ctx := t.Context()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
	qtx := q.WithTx(tx)

	// One transaction is one NOW(): every note has the same created_at.
	_, err = tx.Exec(ctx, `DELETE FROM notes`)
	require.NoError(t, err)
	var ids []int64
	for _, title := range []string{"a", "b", "c"} {
		n, err := qtx.CreateNote(ctx, db.CreateNoteParams{Title: title, Body: ""})
		require.NoError(t, err)
		ids = append(ids, n.ID)
	}

	first, err := qtx.ListNotes(ctx, db.ListNotesParams{PageLimit: 2, PageOffset: 0})
	require.NoError(t, err)
	second, err := qtx.ListNotes(ctx, db.ListNotesParams{PageLimit: 2, PageOffset: 2})
	require.NoError(t, err)

	var got []int64
	for _, n := range append(first, second...) {
		got = append(got, n.ID)
	}
	assert.Equal(t, []int64{ids[2], ids[1], ids[0]}, got)
}

// The table holds a note to the limits the form checks, so a note that slips past the form is refused all the same.
func TestCreateNote__TheTableHoldsTheLimits(t *testing.T) {
	for name, p := range map[string]db.CreateNoteParams{
		"an empty title":   {Title: "", Body: "x"},
		"a title too long": {Title: strings.Repeat("é", 121), Body: "x"},
		"a body too long":  {Title: "t", Body: strings.Repeat("é", 5001)},
	} {
		_, err := q.CreateNote(t.Context(), p)
		assert.Error(t, err, name)
	}

	_, err := q.CreateNote(t.Context(), db.CreateNoteParams{Title: strings.Repeat("é", 120), Body: strings.Repeat("é", 5000)})
	assert.NoError(t, err, "the limits are characters, not bytes")
}

func TestDeleteNote__SaysWhetherThereWasOne(t *testing.T) {
	n, err := q.CreateNote(t.Context(), db.CreateNoteParams{Title: "to delete", Body: ""})
	require.NoError(t, err)

	rows, err := q.DeleteNote(t.Context(), n.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(1), rows)

	rows, err = q.DeleteNote(t.Context(), n.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(0), rows, "already gone")
}
