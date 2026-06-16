package graphio

import (
	"golang.org/x/tools/go/ssa"

	"github.com/jyang234/golang-code-graph/internal/canon/sql"
	"github.com/jyang234/golang-code-graph/internal/static/features"
)

// dynamicLabel is the sentinel for a boundary argument the labeler could not read
// off a compile-time constant. It is the single source of truth shared by the
// label producers here and the consumers that must NOT treat an unreadable
// boundary as a concretely-named effect (see committedEffect).
const dynamicLabel = "<dynamic>"

// eventLabel is the published event name, or dynamicLabel if not constant.
func eventLabel(site ssa.CallInstruction) string {
	args := features.StringArgs(site)
	if len(args) >= 1 {
		if s, ok := features.ConstString(args[0]); ok {
			return s
		}
	}
	return dynamicLabel
}

// httpLabel is "peer method route" for a constant outbound call, else dynamicLabel.
func httpLabel(site ssa.CallInstruction) string {
	args := features.StringArgs(site)
	if len(args) >= 3 {
		p, ok1 := features.ConstString(args[0])
		m, ok2 := features.ConstString(args[1])
		r, ok3 := features.ConstString(args[2])
		if ok1 && ok2 && ok3 {
			return p + " " + m + " " + r
		}
	}
	return dynamicLabel
}

// dbLabel is the SQL operation and table ("SELECT applicants"), derived from the
// statement constant; it falls back to the DB method name. It shares the one
// canonical SQL normalizer (canon/sql) with the behavioral pipeline, so the
// static op/table cannot drift from the canonical op key.
func dbLabel(site ssa.CallInstruction) string {
	args := features.StringArgs(site)
	if len(args) >= 1 {
		if stmt, ok := features.ConstString(args[0]); ok {
			if op, table := sqlOpTable(stmt); op != "" {
				if table != "" {
					return op + " " + table
				}
				return op
			}
		}
	}
	if c := site.Common().StaticCallee(); c != nil {
		return c.Name()
	}
	return "call"
}

// sqlOpTable extracts the leading SQL operation and primary table from a
// statement. It delegates to the canonical normalizer (canon/sql) — the single
// source of truth shared with the behavioral op key — so a view label and a
// canonical op key never disagree on the verb or table for the same statement.
func sqlOpTable(stmt string) (op, table string) {
	n := sql.Normalize(stmt)
	return n.Operation, n.Table
}
