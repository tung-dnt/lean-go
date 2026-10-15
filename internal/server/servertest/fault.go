package servertest

// Faults are how a test makes the database go wrong at one query, and only that one, to see what a handler does with
// it: every other query is the real one, on the real database, and the test has no other way to break the database
// than to break what the server really reads. A fault is made in the schema of the server and stays in it, so it is
// for a server of the test's own (Start), never the one that the tests of a package share.
//
// sqlc writes the name of a query above its text ("-- name: CountNotes :one"), and PostgreSQL's
// current_query() is the text of the statement that is running. A table is put behind a view that keeps the rows for
// which a function says yes, and the function looks at the statement: for the query that is named it says no, or
// fails, and for every other one it says yes. That covers the queries that read the table (a SELECT, and what reads
// in an UPDATE or a DELETE), and only the rows that the query would have read: a function that is not called for a
// row cannot fail for it, so the table has to hold a row that the query reads.
//
// A server has one fault at a time: making another replaces it, and Heal takes it away.
//
// A table behind a view is not the table. The view has no sequence (a query that asks for the next id of the table
// gets nothing: make the rows before the first fault, or start a server for the test that needs more) and no primary
// key (a query that groups by it and reads other columns fails for that, and not with the fault: fault the other table
// it reads), and Heal does not take the view away, only what it does.

import (
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

const (
	// faultMessage is what a query that fails fails with: the message of the error that the database answers it with.
	faultMessage = "boom"

	// faultTable is the table that holds the fault, the query that it is made for and what it does to it.
	faultTable = "servertest_fault"

	// faultGate is the function that the views of the tables are made of.
	faultGate = "servertest_gate"

	outcomeFail = "fail"
	outcomeHide = "hide"
)

// FailQuery makes the query that sqlc named query ("CountNotes") fail with faultMessage ("boom") when it
// reads table, and every other query go on as it did. The query fails for the rows of the table that it reads, so
// the table has to hold one. It replaces the fault that there was.
func (s *Server) FailQuery(t *testing.T, table, query string) {
	t.Helper()

	s.fault(t, table, query, outcomeFail)
}

// HideRows makes the query that sqlc named query find no row of table, and every other query read it as it did. It
// is the answer that the database gives for rows that were deleted, or rejected, or moved, since the query before
// it: a request is made of several queries, and no test can decide what happens between two of them. It replaces
// the fault that there was.
func (s *Server) HideRows(t *testing.T, table, query string) {
	t.Helper()

	s.fault(t, table, query, outcomeHide)
}

// Heal takes the fault away: every query reads and fails as it would have without it.
func (s *Server) Heal(t *testing.T) {
	t.Helper()

	faults := pgx.Identifier{faultTable}.Sanitize()
	s.exec(t, fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (query text NOT NULL, outcome text NOT NULL)`, faults))
	s.exec(t, fmt.Sprintf(`DELETE FROM %s`, faults))
}

// exec runs a statement that has no answer.
func (s *Server) exec(t *testing.T, sql string, args ...any) {
	t.Helper()

	_, err := s.DB.Exec(t.Context(), sql, args...)
	require.NoError(t, err, sql)
}

// fault makes outcome of what the query does to the rows of table, in place of the fault that there was.
func (s *Server) fault(t *testing.T, table, query, outcome string) {
	t.Helper()

	faults := pgx.Identifier{faultTable}.Sanitize()
	gate := pgx.Identifier{faultGate}.Sanitize()
	ident := pgx.Identifier{table}.Sanitize()
	ungated := pgx.Identifier{table + "_ungated"}.Sanitize()

	// What is faulty, and the function that looks it up for every row. The trailing space of the marker is what
	// keeps a query from being taken for the one whose name it begins.
	s.exec(t, fmt.Sprintf(`CREATE OR REPLACE FUNCTION %s() RETURNS boolean LANGUAGE plpgsql AS $gate$
DECLARE
	f record;
BEGIN
	FOR f IN SELECT outcome FROM %s WHERE position('-- name: ' || query || ' ' in current_query()) > 0 LOOP
		IF f.outcome = '%s' THEN
			RAISE EXCEPTION '%s';
		END IF;

		RETURN false;
	END LOOP;

	RETURN true;
END
$gate$`, gate, faults, outcomeFail, faultMessage))

	// The table behind a view of itself, once.
	var wrapped bool
	require.NoError(t, s.DB.QueryRow(t.Context(), `SELECT to_regclass($1) IS NOT NULL`, ungated).Scan(&wrapped))
	if !wrapped {
		s.exec(t, fmt.Sprintf(`ALTER TABLE %s RENAME TO %s`, ident, ungated))
		s.exec(t, fmt.Sprintf(`CREATE VIEW %s AS SELECT * FROM %s WHERE %s()`, ident, ungated, gate))
	}

	s.Heal(t)
	s.exec(t, fmt.Sprintf(`INSERT INTO %s (query, outcome) VALUES ($1, $2)`, faults), query, outcome)
}
