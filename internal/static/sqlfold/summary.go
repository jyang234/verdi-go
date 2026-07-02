package sqlfold

import (
	"go/constant"
	"go/types"

	"golang.org/x/tools/go/ssa"

	"github.com/jyang234/golang-code-graph/internal/static/features"
)

// summaryKind classifies a builder method by its effect on the accumulator.
type summaryKind int

const (
	smUnknown  summaryKind = iota // not a recognizable accumulator method → abstain
	smWriter                      // appends fragments and returns the receiver
	smTerminal                    // returns recv.<builder>.String() (Build/String)
)

// tfragKind is a template fragment: a builder method's append, before the call
// site supplies its parameter values.
type tfragKind int

const (
	tfConst       tfragKind = iota // a constant the method appends itself
	tfPlaceholder                  // a separator-free placeholder ($N / ?) the method emits
	tfParam                        // the method appends its parameter #param verbatim
)

type tfrag struct {
	kind  tfragKind
	text  string
	param int
}

type summary struct {
	kind          summaryKind
	template      []tfrag
	terminalIndex int // for smTerminal: which result is recv.<builder>.String()
}

// summarize classifies a builder method. It is a pure function of the method's
// SSA body (no shared cache — determinism over micro-optimization). Terminal is
// checked first: Build both writes nothing and returns String(), so a method that
// returns the accumulated string is the terminator, not an appender.
func summarize(fn *ssa.Function) summary {
	if fn == nil {
		return summary{kind: smUnknown}
	}
	if idx, ok := returnsBuilderString(fn); ok {
		return summary{kind: smTerminal, terminalIndex: idx}
	}
	if tmpl, ok := writerTemplate(fn); ok {
		return summary{kind: smWriter, template: tmpl}
	}
	return summary{kind: smUnknown}
}

// writerTemplate returns the ordered fragments a method appends to its receiver's
// strings.Builder, and whether the method is a clean appender. It abstains (ok=
// false) unless EVERY builder write is in the entry block (no conditional or
// looped append inside the method) and EVERY appended value is classifiable as a
// parameter, a constant, or a separator-free integer placeholder — anything else
// is content the fold cannot see, so the method is not foldable.
func writerTemplate(fn *ssa.Function) ([]tfrag, bool) {
	if len(fn.Params) == 0 || len(fn.Blocks) == 0 {
		return nil, false
	}
	recv := fn.Params[0]
	entry := fn.Blocks[0]
	var tmpl []tfrag
	wrote := false
	for _, b := range fn.Blocks {
		for _, instr := range b.Instrs {
			call, ok := instr.(*ssa.Call)
			if !ok {
				continue
			}
			callee := call.Common().StaticCallee()
			if callee == nil || !isStringsBuilderMethod(callee) {
				// Not a modeled strings.Builder write. If the receiver — or a pointer
				// into it, e.g. &w.sb — flows into this call, the callee may append to
				// the buffer where the template cannot see it: fmt.Fprintf(&w.sb, "%s",
				// userInput), a helper w.writeCol(...), or a dynamic dispatch on the
				// receiver. Such a hole under a "complete constant SELECT" claim is a
				// false READ that smuggles a runtime-spliced statement past read-
				// classification — the exact case the package doc says must be
				// prevented — so fail closed (H-11). A value load of a receiver field
				// (strconv.Itoa(len(w.args))) cannot mutate the builder and is fine.
				if callMayMutateReceiver(call, ssa.Value(recv)) {
					return nil, false
				}
				continue
			}
			name := callee.Name()
			if name == "String" || name == "Len" || name == "Cap" || name == "Grow" {
				continue // reads/capacity, no text appended
			}
			args := call.Common().Args
			if len(args) < 2 || rootOf(args[0]) != ssa.Value(recv) {
				return nil, false // a builder write we cannot attribute to the receiver
			}
			if b != entry {
				return nil, false // a conditional/looped append: not a clean appender
			}
			wrote = true
			switch name {
			case "WriteString", "Write":
				tf, ok := classifyAppend(fn, args[1])
				if !ok {
					return nil, false
				}
				tmpl = append(tmpl, tf)
			case "WriteByte", "WriteRune":
				c, ok := constByteOrRune(args[1])
				if !ok {
					return nil, false
				}
				tmpl = append(tmpl, tfrag{kind: tfConst, text: c})
			default:
				return nil, false // Reset and friends could rewrite the buffer
			}
		}
	}
	if !wrote {
		return nil, false
	}
	return tmpl, true
}

// callMayMutateReceiver reports whether the receiver — or a pointer derived from
// it — is handed to call, so the (non-modeled) callee could append to the
// builder invisibly. An interface-dispatched method ON the receiver counts (its
// dynamic body is unseen). Among positional args only a POINTER rooting back to
// the receiver counts: a callee can write through a pointer (&w.sb, or w itself)
// but not through a loaded field value (len(w.args), w.n). This is the H-11
// fail-closed guard: any un-modeled path to the buffer is a template hole, and a
// hole under a completeness claim is a false read.
func callMayMutateReceiver(call *ssa.Call, recv ssa.Value) bool {
	cc := call.Common()
	// touches reports whether v is a pointer rooting back to the receiver. It
	// strips interface-boxing and conversions first: fmt.Fprintf(&w.sb, …) boxes
	// &w.sb into an io.Writer (a MakeInterface), so the raw arg is an interface
	// value whose root is not the receiver — the address of the buffer is one
	// unwrap in. Only a pointer counts: a callee mutates the builder through a
	// pointer (&w.sb, or w itself), never through a loaded field value.
	touches := func(v ssa.Value) bool {
		v = stripConversions(v)
		if rootOf(v) != recv {
			return false
		}
		_, isPtr := v.Type().Underlying().(*types.Pointer)
		return isPtr
	}
	if cc.IsInvoke() && touches(cc.Value) {
		return true
	}
	for _, a := range cc.Args {
		if touches(a) {
			return true
		}
	}
	return false
}

// stripConversions unwraps the identity-value wrappers SSA inserts around an
// argument — interface boxing and type/interface conversions — back to the
// underlying value, so a receiver pointer handed to a call is recognized through
// the io.Writer box (MakeInterface) that fmt.Fprintf and friends require.
func stripConversions(v ssa.Value) ssa.Value {
	for {
		switch x := v.(type) {
		case *ssa.MakeInterface:
			v = x.X
		case *ssa.ChangeType:
			v = x.X
		case *ssa.ChangeInterface:
			v = x.X
		case *ssa.Convert:
			v = x.X
		default:
			return v
		}
	}
}

// classifyAppend maps a value appended via WriteString/Write to a template
// fragment: the method's own parameter (instantiated at the call site), a
// compile-time constant, or a separator-free integer rendering (a placeholder).
func classifyAppend(fn *ssa.Function, v ssa.Value) (tfrag, bool) {
	for i, p := range fn.Params {
		if v == ssa.Value(p) {
			return tfrag{kind: tfParam, param: i}, true
		}
	}
	if s, ok := features.ConstString(v); ok {
		return tfrag{kind: tfConst, text: s}, true
	}
	if call, ok := v.(*ssa.Call); ok && isStrconvIntegral(call.Common().StaticCallee()) {
		return tfrag{kind: tfPlaceholder}, true
	}
	return tfrag{}, false
}

// returnsBuilderString reports whether fn returns recv.<builder>.String() and at
// which result index — the terminal signature (Build returns (string, []any);
// strings.Builder.String returns the string directly).
func returnsBuilderString(fn *ssa.Function) (int, bool) {
	if len(fn.Params) == 0 {
		return 0, false
	}
	recv := fn.Params[0]
	for _, b := range fn.Blocks {
		for _, instr := range b.Instrs {
			ret, ok := instr.(*ssa.Return)
			if !ok {
				continue
			}
			for i, res := range ret.Results {
				call, ok := res.(*ssa.Call)
				if !ok {
					continue
				}
				callee := call.Common().StaticCallee()
				if callee == nil || !isStringsBuilderMethod(callee) || callee.Name() != "String" {
					continue
				}
				a := call.Common().Args
				if len(a) >= 1 && rootOf(a[0]) == ssa.Value(recv) {
					return i, true
				}
			}
		}
	}
	return 0, false
}

// rootOf unwraps the field/load chain from a strings.Builder receiver back to the
// value it lives on, so `w.sb` (FieldAddr of the receiver) resolves to `w`.
func rootOf(v ssa.Value) ssa.Value {
	for {
		switch x := v.(type) {
		case *ssa.FieldAddr:
			v = x.X
		case *ssa.Field:
			v = x.X
		case *ssa.UnOp:
			v = x.X
		default:
			return v
		}
	}
}

// isStringsBuilderMethod reports whether fn is a method on strings.Builder.
func isStringsBuilderMethod(fn *ssa.Function) bool {
	if fn.Signature == nil || fn.Signature.Recv() == nil {
		return false
	}
	return namedIs(fn.Signature.Recv().Type(), "strings", "Builder")
}

// isStrconvIntegral reports whether fn renders an integer to its decimal string —
// a provably separator- and keyword-free alphabet ([-+]?[0-9]+), so its output is
// a safe placeholder, never a statement separator.
func isStrconvIntegral(fn *ssa.Function) bool {
	if fn == nil || fn.Pkg == nil || fn.Pkg.Pkg == nil || fn.Pkg.Pkg.Path() != "strconv" {
		return false
	}
	switch fn.Name() {
	case "Itoa", "FormatInt", "FormatUint":
		return true
	}
	return false
}

// namedIs reports whether t (after stripping a pointer) is the named type
// pkgPath.name. The nil-safe identity compare is the shared features.NamedTypeIs (one
// source of truth with the blind-spot benign-func tier); the pointer strip is this
// site's own resolution (a method receiver is a *T).
func namedIs(t types.Type, pkgPath, name string) bool {
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	n, _ := t.(*types.Named)
	return features.NamedTypeIs(n, pkgPath, name)
}

func isStringType(t types.Type) bool {
	b, ok := t.Underlying().(*types.Basic)
	return ok && b.Kind() == types.String
}

// constByteOrRune returns the one-character string of a constant byte/rune value.
func constByteOrRune(v ssa.Value) (string, bool) {
	c, ok := v.(*ssa.Const)
	if !ok || c.Value == nil || c.Value.Kind() != constant.Int {
		return "", false
	}
	i, ok := constant.Int64Val(c.Value)
	if !ok {
		return "", false
	}
	return string(rune(i)), true
}

// position is a call's deterministic program order: its block index, then its
// position within the block.
func position(c *ssa.Call) (int, int) {
	b := c.Block()
	if b == nil {
		return 1 << 30, 0
	}
	for i, instr := range b.Instrs {
		if instr == ssa.Instruction(c) {
			return b.Index, i
		}
	}
	return b.Index, 1 << 30
}
