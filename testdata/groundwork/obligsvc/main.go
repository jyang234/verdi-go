// obligsvc is groundwork's path-obligations fixture: every app function is
// reachable from main so the call graph (and therefore the obligations check)
// covers each verdict shape.
package main

import (
	"example.com/obligsvc/internal/app"
	"example.com/obligsvc/internal/store"
)

func main() {
	s := &store.Store{}
	_ = app.Transfer(s)
	_ = app.TransferDefer(s)
	tx, _ := app.TransferOwn(s)
	if tx != nil {
		_ = tx.Commit()
	}
	app.Disburse(true)
	app.DisburseRacy(true)
}
