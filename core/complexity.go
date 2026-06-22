package core

import (
	"go/ast"
	"go/token"
)

// analyzer walks a parsed Go file and produces:
//   - one MethodMetric per function/method declaration (FuncDecl)
//   - one TypeDecl per struct / interface / named-type declaration (TypeSpec)
//
// It computes two complexity metrics on a single pass.
//
// Cyclomatic complexity (McCabe). Each function starts at 1. +1 for: `if`,
// `for`, `range`, each non-default `case` (in a switch, type switch, or
// select), and each `&&` / `||`. This mirrors the established `gocyclo`
// counting so the numbers are comparable to the wider Go tooling ecosystem,
// and matches the TypeScript/Swift ports (switch itself is 0; cases carry it).
//
// Cognitive complexity (SonarSource 2023 spec). Each function starts at 0.
// Three increment kinds:
//   - B. Structural (+1 + nesting, bumps nesting for inner code): `if` (head
//     of an if/else-if chain), `for`, `range`, `switch`, type switch, and
//     `select` — each whole switch/select is ONE increment regardless of case
//     count, which is the whole point of cognitive vs cyclomatic on flat
//     dispatch.
//   - D. Hybrid (+1 flat OR +0, bumps nesting): a chained `else if` and a
//     plain `else` each add +1 flat; function literals (closures) add +0 but
//     bump the nesting level for their body.
//   - C. Fundamental (+1 flat, no nesting interaction): each new run of like
//     boolean operators (`a && b && c` is one run; `a && b || c` is two), and
//     labelled jumps (`break label`, `continue label`, `goto label`).
//
// Ignored (cognitive +0): the function itself, `defer`/`go`, individual `case`
// labels, `fallthrough`, and plain `break`/`continue`/`return` (early exits
// per the spec's "no other jumps cause an increment" rule).
type analyzer struct {
	fset *token.FileSet
	file string

	stack       []*walkFrame
	methodStack []*methodFrame

	methods []MethodMetric
	types   []TypeDecl
}

type walkFrame struct {
	node       ast.Node
	bumpedNest bool         // did entering this node increment the current method's nesting?
	method     *methodFrame // non-nil if this node opened a method scope
}

type methodFrame struct {
	name          string
	qualifiedName string
	typeName      string
	kind          MethodKind
	startLine     int
	endLine       int
	complexity    int // cyclomatic, base 1
	cognitive     int // cognitive, base 0
	nesting       int
}

// analyzeFile parses nothing; it walks an already-parsed file.
func analyze(fset *token.FileSet, file string, f *ast.File) ([]MethodMetric, []TypeDecl) {
	a := &analyzer{fset: fset, file: file}
	ast.Walk(a, f)
	return a.methods, a.types
}

// Visit implements ast.Visitor. ast.Walk calls Visit(node) on the way down and
// Visit(nil) on the way back up, perfectly nested, so an explicit stack lets us
// recover each node's parent and unwind nesting/method scopes deterministically.
func (a *analyzer) Visit(node ast.Node) ast.Visitor {
	if node == nil {
		a.pop()
		return a
	}

	parent := a.parentNode()
	frame := &walkFrame{node: node}

	switch n := node.(type) {
	case *ast.FuncDecl:
		a.openMethod(n, frame)
	case *ast.FuncLit:
		// Anonymous closure — D-Hybrid: +0 score, nesting bump only.
		a.incNesting(frame)
	case *ast.TypeSpec:
		a.recordType(n)
	case *ast.IfStmt:
		a.handleIf(n, parent, frame)
	case *ast.ForStmt:
		a.bumpCyclomatic()
		a.bumpCognitive(1 + a.nesting())
		a.incNesting(frame)
	case *ast.RangeStmt:
		a.bumpCyclomatic()
		a.bumpCognitive(1 + a.nesting())
		a.incNesting(frame)
	case *ast.SwitchStmt:
		// Whole switch = one structural increment; cases drive cyclomatic only.
		a.bumpCognitive(1 + a.nesting())
		a.incNesting(frame)
	case *ast.TypeSwitchStmt:
		a.bumpCognitive(1 + a.nesting())
		a.incNesting(frame)
	case *ast.SelectStmt:
		a.bumpCognitive(1 + a.nesting())
		a.incNesting(frame)
	case *ast.CaseClause:
		if len(n.List) > 0 { // non-default case
			a.bumpCyclomatic()
		}
	case *ast.CommClause:
		if n.Comm != nil { // non-default select case
			a.bumpCyclomatic()
		}
	case *ast.BinaryExpr:
		a.handleBinary(n, parent)
	case *ast.BranchStmt:
		// Labelled jumps are C-Fundamental; goto always carries a label, and
		// plain break/continue/fallthrough stay free.
		if n.Label != nil {
			a.bumpCognitive(1)
		}
	}

	a.push(frame)
	return a
}

func (a *analyzer) push(f *walkFrame) { a.stack = append(a.stack, f) }

func (a *analyzer) pop() {
	if len(a.stack) == 0 {
		return
	}
	f := a.stack[len(a.stack)-1]
	a.stack = a.stack[:len(a.stack)-1]
	if f.bumpedNest {
		if m := a.curMethod(); m != nil {
			m.nesting--
		}
	}
	if f.method != nil {
		a.finishMethod(f.method)
	}
}

func (a *analyzer) parentNode() ast.Node {
	if len(a.stack) == 0 {
		return nil
	}
	return a.stack[len(a.stack)-1].node
}

func (a *analyzer) curMethod() *methodFrame {
	if len(a.methodStack) == 0 {
		return nil
	}
	return a.methodStack[len(a.methodStack)-1]
}

func (a *analyzer) nesting() int {
	if m := a.curMethod(); m != nil {
		return m.nesting
	}
	return 0
}

func (a *analyzer) incNesting(f *walkFrame) {
	if m := a.curMethod(); m != nil {
		m.nesting++
		f.bumpedNest = true
	}
}

func (a *analyzer) bumpCyclomatic() {
	if m := a.curMethod(); m != nil {
		m.complexity++
	}
}

func (a *analyzer) bumpCognitive(amount int) {
	if amount <= 0 {
		return
	}
	if m := a.curMethod(); m != nil {
		m.cognitive += amount
	}
}

func (a *analyzer) openMethod(n *ast.FuncDecl, f *walkFrame) {
	name := n.Name.Name
	kind := KindFunction
	typeName := ""
	if n.Recv != nil && len(n.Recv.List) > 0 {
		kind = KindMethod
		typeName = receiverTypeName(n.Recv.List[0].Type)
	}
	qualified := name
	if typeName != "" {
		qualified = typeName + "." + name
	}
	start, end := a.lineRange(n)
	mf := &methodFrame{
		name:          name,
		qualifiedName: qualified,
		typeName:      typeName,
		kind:          kind,
		startLine:     start,
		endLine:       end,
		complexity:    1,
		cognitive:     0,
	}
	a.methodStack = append(a.methodStack, mf)
	f.method = mf
}

func (a *analyzer) finishMethod(mf *methodFrame) {
	// Pop the frame off the method stack (it is always the top).
	if len(a.methodStack) > 0 {
		a.methodStack = a.methodStack[:len(a.methodStack)-1]
	}
	a.methods = append(a.methods, MakeMethodMetric(MethodMetric{
		Name:                mf.name,
		QualifiedName:       mf.qualifiedName,
		TypeName:            mf.typeName,
		Kind:                mf.kind,
		File:                a.file,
		StartLine:           mf.startLine,
		EndLine:             mf.endLine,
		Complexity:          mf.complexity,
		CognitiveComplexity: mf.cognitive,
	}))
}

func (a *analyzer) recordType(n *ast.TypeSpec) {
	kind := KindNamed
	switch n.Type.(type) {
	case *ast.StructType:
		kind = KindStruct
	case *ast.InterfaceType:
		kind = KindInterface
	}
	start, end := a.lineRange(n)
	a.types = append(a.types, TypeDecl{
		Kind:      kind,
		Name:      n.Name.Name,
		File:      a.file,
		StartLine: start,
		EndLine:   end,
	})
}

// handleIf mirrors the TypeScript/Swift if-chain handling: the head `if` is
// B-Structural (+1 + nesting); a chained `else if` is D-Hybrid (+1 flat); a
// trailing plain `else` is also D-Hybrid (+1 flat). Cyclomatic counts every
// `if` (head and chained) as +1. Every if frame bumps nesting for its subtree.
func (a *analyzer) handleIf(n *ast.IfStmt, parent ast.Node, f *walkFrame) {
	a.bumpCyclomatic()
	chainedElseIf := false
	if pIf, ok := parent.(*ast.IfStmt); ok && pIf.Else == ast.Stmt(n) {
		chainedElseIf = true
	}
	if chainedElseIf {
		a.bumpCognitive(1)
	} else {
		a.bumpCognitive(1 + a.nesting())
	}
	if n.Else != nil {
		if _, isIf := n.Else.(*ast.IfStmt); !isIf {
			a.bumpCognitive(1) // plain else
		}
	}
	a.incNesting(f)
}

// handleBinary counts boolean operators. Cyclomatic: every `&&` / `||` is a
// branch. Cognitive run-collapse: a sequence of like operators is one
// increment; with Go's left-associative parse that reduces to "bump unless the
// immediate parent is a binary expression with the same operator". Parentheses
// break a run (a ParenExpr parent is not a same-op binary).
func (a *analyzer) handleBinary(n *ast.BinaryExpr, parent ast.Node) {
	if n.Op != token.LAND && n.Op != token.LOR {
		return
	}
	a.bumpCyclomatic()
	continuesRun := false
	if pb, ok := parent.(*ast.BinaryExpr); ok && pb.Op == n.Op {
		continuesRun = true
	}
	if !continuesRun {
		a.bumpCognitive(1)
	}
}

func (a *analyzer) lineRange(n ast.Node) (int, int) {
	return a.fset.Position(n.Pos()).Line, a.fset.Position(n.End()).Line
}

// receiverTypeName extracts the base type name from a method receiver,
// unwrapping pointers (`*T`) and generic instantiations (`T[P]`, `T[P, Q]`).
func receiverTypeName(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.StarExpr:
		return receiverTypeName(e.X)
	case *ast.IndexExpr:
		return receiverTypeName(e.X)
	case *ast.IndexListExpr:
		return receiverTypeName(e.X)
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		return e.Sel.Name
	default:
		return ""
	}
}
