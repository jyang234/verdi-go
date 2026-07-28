package features

import (
	"fmt"
	"go/token"
	"go/types"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/tools/go/ssa"
)

// LocalTypeGraphMarker frames the InstanceDiscriminator suffix that carries the
// positional serialization of a function's reachable type graph. It is exported
// so the call-graph guard can recognize the one collision class the key is known
// NOT to decide (see FirstLocalDeclaration) without re-deriving the encoding's
// grammar; nothing else may parse the suffix.
//
// The name is deliberately not the retired "local-sites/v1": that suffix was a
// sorted, deduplicated SET of declaration sites, and this one is a positional
// graph serialization. A reader who guessed the old grammar would mis-parse
// these bytes, and a stale binary's diagnostic must be unmistakable.
const LocalTypeGraphMarker = "\x00local-type-graph/v1\x00"

// localTypeGraphBudget caps the serialization's definition bytes. The numbered
// DAG is linear in the number of DISTINCT reachable type nodes, so no analyzable
// program is expected to approach it; the budget is a fail-closed guard against a
// type graph this analysis did not anticipate. Exceeding it panics rather than
// truncating, because a truncated key would silently merge two distinct
// functions — the one outcome worse than a refused analysis.
//
// It is charged inside id(), i.e. DURING the traversal — before the encoder knows
// whether any function-local declaration was reachable. So a type graph over the
// budget is refused even when it carries no local declaration and would have
// emitted no suffix at all. That surface is wider than the suffix it guards, and
// it is disclosed in the design's "Byte budget" rule rather than left to be
// discovered: narrowing it would mean completing a >1 MiB traversal only to throw
// the result away. A loud abstain, never a merge.
const localTypeGraphBudget = 1 << 20

// HasLocalTypeGraph reports whether a discriminator carries the positional
// type-graph suffix, i.e. whether a function-local declaration was reachable from
// the function's type arguments or receiver.
func HasLocalTypeGraph(discriminator string) bool {
	return strings.Contains(discriminator, LocalTypeGraphMarker)
}

// LocalDeclaration identifies one function-local type declaration for a HUMAN
// reader. It is a struct rather than a joined string because the discriminator's
// site bytes and the diagnostic's prose are two different concerns with two
// different rules, and a single joined string forces one of them to re-parse the
// other's format.
//
// File and Offset are the PHYSICAL position — the same two values the key
// encodes. Line is DISPLAY ONLY; see the field comment.
type LocalDeclaration struct {
	// Name is the declared type's identifier, e.g. "L".
	Name string
	// File is the physical file's basename and Offset the physical byte offset
	// of the declaration within it. This pair, and nothing else, is what site()
	// hands the encoder.
	File   string
	Offset int
	// Line is the //line-ADJUSTED line number, resolved through FileSet.Position.
	// It is DISPLAY ONLY and never reaches a key: it is read from the FileSet
	// here, after the encoding is complete, and typeGraphEncoder never calls
	// FileSet.Position at all. Putting it in a key would let a //line directive
	// rewrite the discriminator, and a line alone cannot separate two
	// declarations written on one physical line — which is why site() is defined
	// on offsets. Neither reason governs what a human is shown.
	// TestLineDirectiveMovesDisplayLineNotTheKey pins the separation.
	Line int
}

// Location renders the declaration's position for a human: the reader of the
// residual diagnostic sees a byte offset, and "main.go:98" invites reading 98 as
// a line number in a file that may have twelve lines. The offset is kept because
// it is the value the key is actually built from, so a reader can correlate the
// message with the key; the line is added because it is what an editor jumps to.
func (d LocalDeclaration) Location() string {
	return fmt.Sprintf("%s, byte offset %d (line %d)", d.File, d.Offset, d.Line)
}

// FirstLocalDeclaration returns the FIRST function-local declaration in fn's
// type-graph encoding, in node-id order, and whether one exists. Ids are assigned
// in first-visit order of a traversal fixed by the input, so "first" is
// deterministic and does not depend on map order.
//
// It exists for the residual-collision diagnostic: when two distinct functions
// survive with one sort key, this names the single declaration both instances
// were produced from. It re-derives the encoding rather than parsing it, so the
// suffix grammar stays private to this file.
func FirstLocalDeclaration(fn *ssa.Function) (LocalDeclaration, bool) {
	roots := discriminatorRoots(fn)
	if len(roots) == 0 {
		return LocalDeclaration{}, false
	}
	e := newTypeGraphEncoder(fn)
	for _, root := range roots {
		e.id(root)
	}
	if !e.sawLocal {
		return LocalDeclaration{}, false
	}
	return LocalDeclaration{
		Name:   e.firstLocalName,
		File:   e.firstLocalSite.file,
		Offset: e.firstLocalSite.offset,
		// The ONLY FileSet.Position call in this file, reached only from here —
		// i.e. only after localTypeGraph has already produced the key bytes.
		Line: e.fset.Position(e.firstLocalPos).Line,
	}, true
}

// discriminatorRoots returns the roots of fn's type-graph encoding, in the order
// the encoding visits them: the type arguments in declaration order, then the
// receiver type when fn has one.
//
// The receiver is a root because a promoted-method wrapper over a function-local
// receiver carries NO type arguments — so InstanceDiscriminator used to return ""
// for it — while callgraph.mergeKey deliberately excludes receiver-carrying
// wrappers from its dedup subset. Two such wrappers over same-named local types
// fell between the two mechanisms and reached the sort with equal empty keys.
func discriminatorRoots(fn *ssa.Function) []types.Type {
	if fn == nil {
		return nil
	}
	targs := fn.TypeArgs()
	roots := make([]types.Type, 0, len(targs)+1)
	roots = append(roots, targs...)
	if fn.Signature != nil && fn.Signature.Recv() != nil {
		roots = append(roots, fn.Signature.Recv().Type())
	}
	return roots
}

// localTypeGraph returns the framed, positional serialization of the type graph
// reachable from roots, or "" when no function-local declaration is reachable —
// which is what keeps a package-level instance's discriminator bytes unchanged.
//
// The suffix is a SHARED NUMBERED DAG, not a set of sites:
//
//	\x00local-type-graph/v1\x00<len>:R<id>...\x00<len>:#<id>=<definition>...
//
// Every node gets an id on first visit, definitions are emitted in ascending id
// order, and each definition names its children BY ID IN POSITIONAL ORDER. That
// is the whole point: a sorted, deduplicated site set records which declarations
// exist and never which one sits in which role, so pair[outer, inner] and
// pair[inner, outer] over two same-rendering locals were indistinguishable, and
// two packages' offset-aligned locals pooled into one equal set. Sorting or
// deduplicating the sites here would reintroduce both.
//
// Sharing by id is what keeps the encoding linear in the number of DISTINCT
// reachable nodes. A fully-expanded encoding is exponential in OUTPUT SIZE, not
// merely in time: on a depth-20 shared DAG it is tens of megabytes, which is not
// a sort key. Memoizing the walk alone does not fix that.
//
// Unknown or invalid local identity inputs fail closed by panicking rather than
// emitting a plausible but untrustworthy key.
func localTypeGraph(fn *ssa.Function, roots []types.Type) string {
	e := newTypeGraphEncoder(fn)
	ids := make([]int, 0, len(roots))
	for _, root := range roots {
		ids = append(ids, e.id(root))
	}
	if !e.sawLocal {
		return ""
	}

	var b strings.Builder
	b.WriteString(LocalTypeGraphMarker)
	for i, id := range ids {
		if i > 0 {
			b.WriteByte('\x00')
		}
		writeFramed(&b, "R"+strconv.Itoa(id))
	}
	for id, def := range e.defs {
		b.WriteByte('\x00')
		writeFramed(&b, "#"+strconv.Itoa(id)+"="+def)
	}
	return b.String()
}

// typeGraphEncoder numbers the reachable type nodes and renders their
// definitions. ids is memo, cycle guard and sharing mechanism at once; it is
// QUERIED BY KEY ONLY and never ranged over, so map iteration order cannot reach
// the output.
//
// Node identity is types.Type pointer identity. That is a PRECISION dependency,
// never a soundness one: less sharing yields more ids and a finer key, more
// sharing yields a coarser key, and a key that is too coarse produces the
// duplicate-key panic in callgraph.finalize — a loud abstain — never a merge.
type typeGraphEncoder struct {
	fn   *ssa.Function
	fset *token.FileSet
	ids  map[types.Type]int
	defs []string
	// size is the running total of definition bytes, checked against
	// localTypeGraphBudget as each definition is completed.
	size int

	sawLocal       bool
	firstLocalName string
	firstLocalSite localSite
	// firstLocalPos is kept for the DISPLAY line only (see LocalDeclaration.Line).
	// The encoder itself never resolves it through the FileSet.
	firstLocalPos token.Pos
}

func newTypeGraphEncoder(fn *ssa.Function) *typeGraphEncoder {
	if fn == nil || fn.Prog == nil || fn.Prog.Fset == nil {
		panic("features: local type identity requires an SSA file set")
	}
	return &typeGraphEncoder{fn: fn, fset: fn.Prog.Fset, ids: make(map[types.Type]int)}
}

// id returns t's node id, assigning one on first visit. The id is RESERVED
// BEFORE recursing, so a recursive type's back edge resolves to an id that
// already exists: the encoding of a cyclic type is finite and independent of
// where the walk entered it, which a back-reference-by-depth scheme would not be.
func (e *typeGraphEncoder) id(t types.Type) int {
	if id, ok := e.ids[t]; ok {
		return id
	}
	id := len(e.defs)
	e.ids[t] = id
	e.defs = append(e.defs, "")
	def := e.define(t)
	e.defs[id] = def
	e.size += len(def)
	if e.size > localTypeGraphBudget {
		panic(fmt.Sprintf(
			"features: local type graph for %q exceeds the %d-byte budget",
			e.fn.RelString(nil), localTypeGraphBudget,
		))
	}
	return id
}

// define renders t's definition: its kind, EVERY intrinsic label of that kind,
// and its children by id in positional order. The intrinsic labels are not
// optional decoration — a definition of "kind plus children by id" does not
// separate basic:int from basic:string, so a local type whose structure varies
// with its enclosing type parameter would encode identically in both
// instantiations.
//
// Every variable-length lexeme is length-framed, so no legal filename, package
// path, field name, struct tag or method name byte can create an ambiguous
// concatenation.
func (e *typeGraphEncoder) define(t types.Type) string {
	var b strings.Builder
	switch x := t.(type) {
	case *types.Basic:
		b.WriteString("basic:")
		writeFramed(&b, x.Name())
	case *types.Alias:
		b.WriteString("alias:")
		e.writeObject(&b, x.Obj())
		b.WriteByte(':')
		e.writeTypeParams(&b, x.TypeParams())
		b.WriteByte(':')
		e.writeTypeArgs(&b, x.TypeArgs())
		b.WriteString(":>")
		b.WriteString(strconv.Itoa(e.id(x.Rhs())))
	case *types.Array:
		b.WriteString("array:")
		b.WriteString(strconv.FormatInt(x.Len(), 10))
		b.WriteString(":>")
		b.WriteString(strconv.Itoa(e.id(x.Elem())))
	case *types.Chan:
		b.WriteString("chan:")
		b.WriteString(strconv.Itoa(int(x.Dir())))
		b.WriteString(":>")
		b.WriteString(strconv.Itoa(e.id(x.Elem())))
	case *types.Interface:
		b.WriteString("iface:")
		for _, m := range sortedExplicitMethods(x) {
			writeFramed(&b, qualifiedFuncName(m))
			b.WriteByte('>')
			b.WriteString(strconv.Itoa(e.id(m.Type())))
		}
		for i := 0; i < x.NumEmbeddeds(); i++ {
			b.WriteString("e>")
			b.WriteString(strconv.Itoa(e.id(x.EmbeddedType(i))))
		}
	case *types.Map:
		b.WriteString("map:>")
		b.WriteString(strconv.Itoa(e.id(x.Key())))
		b.WriteString(":>")
		b.WriteString(strconv.Itoa(e.id(x.Elem())))
	case *types.Named:
		b.WriteString("named:")
		e.writeObject(&b, x.Obj())
		b.WriteByte(':')
		e.writeTypeParams(&b, x.TypeParams())
		b.WriteByte(':')
		e.writeTypeArgs(&b, x.TypeArgs())
		b.WriteString(":>")
		b.WriteString(strconv.Itoa(e.id(x.Underlying())))
	case *types.Pointer:
		b.WriteString("ptr:>")
		b.WriteString(strconv.Itoa(e.id(x.Elem())))
	case *types.Signature:
		b.WriteString("sig:")
		b.WriteString(boolLabel(x.Variadic()))
		b.WriteByte(':')
		if recv := x.Recv(); recv != nil {
			b.WriteString(strconv.Itoa(e.id(recv.Type())))
		} else {
			b.WriteByte('-')
		}
		b.WriteByte(':')
		e.writeTypeParams(&b, x.RecvTypeParams())
		b.WriteByte(':')
		e.writeTypeParams(&b, x.TypeParams())
		b.WriteString(":>")
		e.writeTupleID(&b, x.Params())
		b.WriteString(":>")
		e.writeTupleID(&b, x.Results())
	case *types.Slice:
		b.WriteString("slice:>")
		b.WriteString(strconv.Itoa(e.id(x.Elem())))
	case *types.Struct:
		b.WriteString("struct:")
		for i := 0; i < x.NumFields(); i++ {
			field := x.Field(i)
			writeFramed(&b, field.Name())
			b.WriteByte(':')
			b.WriteString(boolLabel(field.Embedded()))
			b.WriteByte(':')
			writeFramed(&b, x.Tag(i))
			b.WriteByte('>')
			b.WriteString(strconv.Itoa(e.id(field.Type())))
		}
	case *types.Tuple:
		b.WriteString("tuple:")
		for i := 0; i < x.Len(); i++ {
			b.WriteByte('>')
			b.WriteString(strconv.Itoa(e.id(x.At(i).Type())))
		}
	case *types.TypeParam:
		b.WriteString("typeparam:")
		b.WriteString(strconv.Itoa(x.Index()))
		b.WriteString(":>")
		b.WriteString(strconv.Itoa(e.id(x.Constraint())))
	case *types.Union:
		b.WriteString("union:")
		for i := 0; i < x.Len(); i++ {
			term := x.Term(i)
			b.WriteString(boolLabel(term.Tilde()))
			b.WriteByte('>')
			b.WriteString(strconv.Itoa(e.id(term.Type())))
		}
	default:
		panic(fmt.Sprintf(
			"features: unsupported go/types implementation %T in local type identity",
			t,
		))
	}
	return b.String()
}

// writeObject writes a Named or Alias node's declaring package path and name,
// plus its physical declaration site when — and only when — the object is
// function-local.
//
// The package path is carried on EVERY named node, local or not. Basename and
// offset alone are not a declaration identity across packages: two packages whose
// files share a basename and whose local declarations are offset-aligned produce
// equal sites, and a key that reads a site without its package would conflate
// them. Qualifying here also separates two package-scope named types with
// identical underlying structure (type A int, type B int) reached at aligned
// positions, which would otherwise encode identically.
//
// The site reaches the key through localSite.key() and through nothing else. The
// first local object's raw position is retained alongside it, unused by the
// encoding, so FirstLocalDeclaration can resolve a display line afterwards.
func (e *typeGraphEncoder) writeObject(b *strings.Builder, obj *types.TypeName) {
	if obj == nil {
		writeFramed(b, "")
		return
	}
	path := ""
	if obj.Pkg() != nil {
		path = obj.Pkg().Path()
	}
	writeFramed(b, path+"."+obj.Name())
	if !isFunctionLocal(obj) {
		return
	}
	site := e.site(obj)
	b.WriteByte('@')
	writeFramed(b, site.key())
	if !e.sawLocal {
		e.sawLocal = true
		e.firstLocalName = obj.Name()
		e.firstLocalSite = site
		e.firstLocalPos = obj.Pos()
	}
}

// isFunctionLocal reports whether a declared type object is function-local.
//
// A nil Parent() means "not package scope", NEVER "package scope". go/types
// re-creates a local TypeName during generic instantiation with a nil Parent(),
// so an `obj.Parent() != nil` conjunct here silently reclassifies every local
// declared inside a generic function as package-scope and drops its site. The
// predicate gates a DISCLOSURE, so its failure mode must be over-collection: an
// over-collected package-scope object costs a longer key, an under-collected
// local object costs a refused program.
func isFunctionLocal(obj *types.TypeName) bool {
	return obj != nil && obj.Pkg() != nil && obj.Parent() != obj.Pkg().Scope()
}

// localSite is a function-local declaration's PHYSICAL position. It carries the
// two values the key is allowed to see and no others — in particular no line
// number, so no code path can join one into the discriminator by accident.
type localSite struct {
	file   string
	offset int
}

// key renders the site as the discriminator encodes it, <basename>:<byte offset>.
// This is the ONLY rendering that reaches a key; LocalDeclaration.Location is the
// separate, display-only one.
func (s localSite) key() string { return s.file + ":" + strconv.Itoa(s.offset) }

// site returns obj's PHYSICAL declaration site.
//
// It reads the physical token.File, never FileSet.Position (which honours //line
// and would let a generated-file directive rewrite the key), never an absolute
// filename (which would vary with the checkout root), never a raw token.Pos
// (whose file-set base depends on load construction), and never a line number
// alone (valid Go may declare two types on one line). Incomplete position data
// is not safe to render as a canonical key, so it panics instead.
//
// All four reasons are about what may become a KEY. None of them governs what a
// human is shown, which is why LocalDeclaration carries a display-only line.
func (e *typeGraphEncoder) site(obj *types.TypeName) localSite {
	file := e.fset.File(obj.Pos())
	if file == nil {
		panic(fmt.Sprintf(
			"features: no physical token file for local type %q",
			obj.Name(),
		))
	}
	pos := int(obj.Pos())
	if pos < file.Base() || pos > file.Base()+file.Size() {
		panic(fmt.Sprintf(
			"features: invalid physical position for local type %q",
			obj.Name(),
		))
	}
	return localSite{file: filepath.Base(file.Name()), offset: file.Offset(obj.Pos())}
}

// writeTypeParams writes a comma-separated list of type-parameter node ids.
//
// Type parameters are NOT reachable through a node's underlying type or type
// arguments, and a local type can hide inside a constraint. Encoding them is a
// deliberate superset of the minimum needed to separate the known witnesses: it
// can only split an equivalence class, never merge one, and dropping it would
// under-collect — which costs a refused program.
func (e *typeGraphEncoder) writeTypeParams(b *strings.Builder, list *types.TypeParamList) {
	if list == nil {
		return
	}
	for i := 0; i < list.Len(); i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.Itoa(e.id(list.At(i))))
	}
}

// writeTypeArgs writes a comma-separated list of type-argument node ids, in
// declaration order. Order is load-bearing: it is what separates one
// instantiation of a two-parameter generic from the same two types swapped.
func (e *typeGraphEncoder) writeTypeArgs(b *strings.Builder, list *types.TypeList) {
	if list == nil {
		return
	}
	for i := 0; i < list.Len(); i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.Itoa(e.id(list.At(i))))
	}
}

// writeTupleID writes a signature tuple's node id, or "-" when go/types reports
// no tuple at all — which is distinct from an empty one and must not be conflated
// with it.
func (e *typeGraphEncoder) writeTupleID(b *strings.Builder, tuple *types.Tuple) {
	if tuple == nil {
		b.WriteByte('-')
		return
	}
	b.WriteString(strconv.Itoa(e.id(tuple)))
}

// sortedExplicitMethods returns an interface's explicit methods in ascending
// (package path, name) order. go/types already returns them in a canonical order
// for a completed interface; sorting makes the encoder independent of that
// guarantee. Method names are unique within an interface once qualified by
// package, so the order is total and no tie can arise. This is the ONLY sort in
// the encoder.
func sortedExplicitMethods(x *types.Interface) []*types.Func {
	methods := make([]*types.Func, 0, x.NumExplicitMethods())
	for i := 0; i < x.NumExplicitMethods(); i++ {
		methods = append(methods, x.ExplicitMethod(i))
	}
	sort.Slice(methods, func(i, j int) bool {
		return qualifiedFuncName(methods[i]) < qualifiedFuncName(methods[j])
	})
	return methods
}

func qualifiedFuncName(m *types.Func) string {
	if m == nil {
		return "."
	}
	path := ""
	if m.Pkg() != nil {
		path = m.Pkg().Path()
	}
	return path + "." + m.Name()
}

func boolLabel(v bool) string {
	if v {
		return "1"
	}
	return "0"
}

// writeFramed writes s prefixed by its decimal byte length. Length framing is
// what keeps a colon, comma, '>' or '@' inside a filename, package path, field
// name or struct tag from producing an ambiguous concatenation.
func writeFramed(b *strings.Builder, s string) {
	b.WriteString(strconv.Itoa(len(s)))
	b.WriteByte(':')
	b.WriteString(s)
}
