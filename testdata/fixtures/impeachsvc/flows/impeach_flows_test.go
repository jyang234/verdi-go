// Package flows_test drives the real impeachsvc object graph through flowmap's
// PUBLIC harness, capturing canonical traces for the behavioral-impeachment
// fixture. Two flows are gated:
//
//   - POST /loan exercises the DISCOVERED entrypoint and its DB INSERT — the
//     sound baseline (the static graph attributes this effect to the route).
//   - DELETE /admin/ledger exercises the MISSED admin route (mounted through the
//     custom, unhinted router) and its DB DELETE — the effect the static graph
//     reaches from no discovered entrypoint. Its captured trace is what impeaches
//     the analyzer's route→effect completeness.
//
// The DB is a hermetic fake driver with a few ms of latency, so the capture
// carries real span durations and goroutine scheduling that canon normalizes —
// the determinism self-test (3 re-drives, byte-identical) runs over that entropy.
// Run with -update to (re)write the goldens under testdata/flows.
package flows_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"net/http"
	"testing"
	"time"

	"github.com/jyang234/golang-code-graph/flow"
	"github.com/jyang234/golang-code-graph/harness"

	"example.com/impeachsvc/internal/admin"
	"example.com/impeachsvc/internal/handler"
	"example.com/impeachsvc/internal/router"
	"example.com/impeachsvc/internal/store"
)

func init() { sql.Register("fakepg-impeach", fakeDriver{}) }

// wire builds impeachsvc over a hermetic DB, mirroring main.run: the public route
// on the stdlib mux and the admin sub-app on the custom (unhinted) router.
func wire() http.Handler {
	db, _ := sql.Open("fakepg-impeach", "")
	loans := store.New(db)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /loan", handler.New(loans).Create)

	adminRouter := router.New()
	admin.New(loans).Mount(adminRouter)
	mux.Handle("/admin/", adminRouter)
	return mux
}

// TestLoanCreateFlow is the sound control: the discovered route's DB INSERT.
func TestLoanCreateFlow(t *testing.T) {
	app := harness.NewInProcess(t, wire(), harness.WithService("impeachsvc"))
	flow.New("POST /loan").
		TriggerBody("POST", "/loan", []byte(`{"id":"L1"}`)).
		ExpectExactlyOnce("DB postgres INSERT loans").
		Quiescence(15*time.Millisecond, 3*time.Second).
		Run(t, app)
}

// TestAdminPurgeFlow captures the impeachment witness: a DB DELETE reached on the
// admin route the static graph attributes to no entrypoint.
func TestAdminPurgeFlow(t *testing.T) {
	app := harness.NewInProcess(t, wire(), harness.WithService("impeachsvc"))
	flow.New("DELETE /admin/ledger").
		Trigger("DELETE", "/admin/ledger?loan=L1").
		ExpectExactlyOnce("DB postgres DELETE ledger").
		Quiescence(15*time.Millisecond, 3*time.Second).
		Run(t, app)
}

// --- hermetic double: a fake database/sql driver with a touch of latency -------

type fakeDriver struct{}

func (fakeDriver) Open(string) (driver.Conn, error) { return fakeConn{}, nil }

type fakeConn struct{}

func (fakeConn) Prepare(string) (driver.Stmt, error) { return nil, driver.ErrSkip }
func (fakeConn) Close() error                        { return nil }
func (fakeConn) Begin() (driver.Tx, error)           { return nil, driver.ErrSkip }

func (fakeConn) ExecContext(_ context.Context, _ string, _ []driver.NamedValue) (driver.Result, error) {
	time.Sleep(2 * time.Millisecond)
	return driver.RowsAffected(1), nil
}
