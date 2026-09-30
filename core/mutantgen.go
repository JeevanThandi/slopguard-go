package core

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// MutantPlan is everything `mutate` knows before any test runs: the scanned
// files and the mutants generated from them.
type MutantPlan struct {
	// SourceRoot is the absolute path that was scanned (a directory or a
	// single file), reported the same way `analyze` reports it.
	SourceRoot string
	// Files lists every scanned file, sorted by path, including files that
	// yielded no mutants.
	Files []PlannedFile
	// Mutants is sorted by file, line, column and operator id. Each status is
	// StatusPending, or StatusIgnored when an ignore marker switches it off.
	Mutants []Mutant
}

// PlannedFile is one scanned source file.
type PlannedFile struct {
	// AbsPath is the absolute path the file was read from.
	AbsPath string
	// Path is the reported path, relative to the source root.
	Path string
	// Source is the content the mutants were generated from.
	Source []byte
}

// PlanMutants walks root exactly like AnalyzeTree (same default excludes,
// include/exclude globs and generated-file skip), generates the mutants of
// the given operators for every file, applies ignore markers, and sorts the
// result. operators holds valid ids (see ParseOperators); an empty list means
// every operator. Apart from reading the files it does no I/O.
func PlanMutants(root string, options AnalysisOptions, operators []string) (MutantPlan, error) {
	files, rootPrefix, err := sourceFiles(root, options)
	if err != nil {
		return MutantPlan{}, err
	}
	rootAbs, _ := filepath.Abs(root)
	enabled := map[string]bool{}
	for _, op := range operators {
		enabled[op] = true
	}
	plan := MutantPlan{SourceRoot: rootAbs, Files: make([]PlannedFile, 0, len(files))}
	for _, abs := range files {
		src, err := os.ReadFile(abs)
		if err != nil {
			return MutantPlan{}, UnreadableFile(abs, err)
		}
		rel := relativize(abs, rootPrefix)
		mutants, err := GenerateMutants(src, rel)
		if err != nil {
			return MutantPlan{}, err
		}
		for _, m := range mutants {
			if len(enabled) == 0 || enabled[m.Operator] {
				plan.Mutants = append(plan.Mutants, m)
			}
		}
		plan.Files = append(plan.Files, PlannedFile{AbsPath: abs, Path: rel, Source: src})
	}
	sort.Slice(plan.Files, func(i, j int) bool { return plan.Files[i].Path < plan.Files[j].Path })
	SortMutants(plan.Mutants)
	return plan, nil
}

// GenerateMutants lists the mutants of every operator for one Go source file
// held in memory. Each mutant names its enclosing function or method (via the
// complexity analyzer, so names match the CRAP report), ignore markers set
// StatusIgnored, and the result is sorted. reportedPath is recorded on every
// mutant and used in its id. A generated file (per the standard header)
// yields no mutants, just as AnalyzeSource skips it.
func GenerateMutants(src []byte, reportedPath string) ([]Mutant, error) {
	if generatedHeader.Match(headBytes(src)) {
		return nil, nil
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, reportedPath, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, ParseFailed(reportedPath, err)
	}
	methods, _ := analyze(fset, reportedPath, f)
	walker := &mutantWalker{src: src, file: fset.File(f.Pos())}
	walker.walk(f)

	lines := newLineIndex(src)
	ignores := parseIgnoreMarkers(src)
	mutants := make([]Mutant, 0, len(walker.sites))
	for _, site := range walker.sites {
		line, column := lines.position(site.start)
		status := StatusPending
		if ignores.ignores(line, site.operator) {
			status = StatusIgnored
		}
		mutants = append(mutants, Mutant{
			Column:      column,
			File:        reportedPath,
			ID:          MutantID(reportedPath, line, column, site.operator),
			Line:        line,
			Method:      enclosingMethod(methods, line),
			Operator:    site.operator,
			Original:    string(src[site.start:site.end]),
			Replacement: site.replacement,
			Status:      status,
			start:       site.start,
			end:         site.end,
		})
	}
	SortMutants(mutants)
	return mutants, nil
}

// Replacement tables per operator. A token can appear in more than one table:
// `<` yields a boundary mutant and a negate_conditional mutant.
var (
	arithmeticSwaps = map[token.Token]string{
		token.ADD: "-", token.SUB: "+", token.MUL: "/", token.QUO: "*", token.REM: "*",
	}
	compoundArithmeticSwaps = map[token.Token]string{
		token.ADD_ASSIGN: "-=", token.SUB_ASSIGN: "+=", token.MUL_ASSIGN: "/=",
		token.QUO_ASSIGN: "*=", token.REM_ASSIGN: "*=",
	}
	boundarySwaps = map[token.Token]string{
		token.LSS: "<=", token.LEQ: "<", token.GTR: ">=", token.GEQ: ">",
	}
	negatedConditions = map[token.Token]string{
		token.EQL: "!=", token.NEQ: "==",
		token.LSS: ">=", token.LEQ: ">", token.GTR: "<=", token.GEQ: "<",
	}
	logicalSwaps = map[token.Token]string{
		token.LAND: "||", token.LOR: "&&",
	}
)

// mutantSite is one generated change: replace src[start:end] with replacement.
type mutantSite struct {
	operator    string
	start, end  int
	replacement string
}

// mutantWalker visits every node of a parsed file and records a mutantSite
// for each change the operator set defines. It mutates only syntax nodes, so
// text inside comments and string literals is never touched. A stack of
// visited nodes gives each node its parent.
type mutantWalker struct {
	src   []byte
	file  *token.File
	stack []ast.Node
	sites []mutantSite
}

func (w *mutantWalker) walk(f *ast.File) {
	ast.Inspect(f, func(n ast.Node) bool {
		if n == nil {
			w.stack = w.stack[:len(w.stack)-1]
			return true
		}
		var parent ast.Node
		if len(w.stack) > 0 {
			parent = w.stack[len(w.stack)-1]
		}
		w.visit(n, parent)
		w.stack = append(w.stack, n)
		return true
	})
}

func (w *mutantWalker) visit(node, parent ast.Node) {
	switch n := node.(type) {
	case *ast.BinaryExpr:
		w.binary(n)
	case *ast.AssignStmt:
		if replacement, ok := compoundArithmeticSwaps[n.Tok]; ok && !isStringAppend(n) {
			w.emit(OpArithmetic, n.TokPos, len(n.Tok.String()), replacement)
		}
	case *ast.IncDecStmt:
		replacement := "--"
		if n.Tok == token.DEC {
			replacement = "++"
		}
		w.emit(OpIncrement, n.TokPos, 2, replacement)
	case *ast.UnaryExpr:
		switch n.Op {
		case token.SUB:
			w.emit(OpInvertNegative, n.OpPos, 1, w.removal(n.OpPos))
		case token.NOT:
			w.emit(OpRemoveNot, n.OpPos, 1, w.removal(n.OpPos))
		}
	case *ast.Ident:
		if isBooleanLiteral(n, parent) {
			replacement := "false"
			if n.Name == "false" {
				replacement = "true"
			}
			w.emit(OpBooleanLiteral, n.NamePos, len(n.Name), replacement)
		}
	case *ast.ExprStmt:
		if inStatementList(n, parent) && isRemovableCall(n.X) {
			w.sites = append(w.sites, mutantSite{
				operator: OpRemoveCall,
				start:    w.file.Offset(n.Pos()),
				end:      w.file.Offset(n.End()),
			})
		}
	}
}

func (w *mutantWalker) binary(n *ast.BinaryExpr) {
	width := len(n.Op.String())
	if replacement, ok := arithmeticSwaps[n.Op]; ok && !isStringConcatenation(n) {
		w.emit(OpArithmetic, n.OpPos, width, replacement)
	}
	if replacement, ok := boundarySwaps[n.Op]; ok {
		w.emit(OpBoundary, n.OpPos, width, replacement)
	}
	if replacement, ok := negatedConditions[n.Op]; ok {
		w.emit(OpNegateConditional, n.OpPos, width, replacement)
	}
	if replacement, ok := logicalSwaps[n.Op]; ok {
		w.emit(OpLogical, n.OpPos, width, replacement)
	}
}

// removal returns the text that replaces a removed one-byte token: empty
// text, or one space when the token sits between two identifier characters.
// Without the space, `return!x` would become the identifier `returnx`.
func (w *mutantWalker) removal(pos token.Pos) string {
	at := w.file.Offset(pos)
	if at > 0 && at+1 < len(w.src) && isIdentifierByte(w.src[at-1]) && isIdentifierByte(w.src[at+1]) {
		return " "
	}
	return ""
}

// isIdentifierByte reports whether b belongs to an identifier character: an
// ASCII letter, digit or underscore, or any byte of a non-ASCII rune.
func isIdentifierByte(b byte) bool {
	return b == '_' || b >= utf8.RuneSelf ||
		('0' <= b && b <= '9') || ('a' <= b && b <= 'z') || ('A' <= b && b <= 'Z')
}

func (w *mutantWalker) emit(operator string, pos token.Pos, width int, replacement string) {
	start := w.file.Offset(pos)
	w.sites = append(w.sites, mutantSite{operator: operator, start: start, end: start + width, replacement: replacement})
}

// isStringConcatenation reports whether a binary + joins strings rather than
// adding numbers: at least one operand is stringy.
func isStringConcatenation(n *ast.BinaryExpr) bool {
	return n.Op == token.ADD && (isStringy(n.X) || isStringy(n.Y))
}

// isStringAppend reports whether `x += y` appends a stringy y to a string.
func isStringAppend(n *ast.AssignStmt) bool {
	return n.Tok == token.ADD_ASSIGN && len(n.Rhs) == 1 && isStringy(n.Rhs[0])
}

// isStringy reports whether an operand is a string literal or, through
// parentheses, a + expression with a stringy operand — so every + in
// "a" + b + c counts as concatenation.
func isStringy(e ast.Expr) bool {
	switch e := e.(type) {
	case *ast.BasicLit:
		return e.Kind == token.STRING
	case *ast.ParenExpr:
		return isStringy(e.X)
	case *ast.BinaryExpr:
		return isStringConcatenation(e)
	}
	return false
}

// isBooleanLiteral reports whether id is the predeclared true or false used
// as a value. In Go these are identifiers, so declared names, labels, field
// names and selectors that happen to spell true/false are skipped.
func isBooleanLiteral(id *ast.Ident, parent ast.Node) bool {
	if id.Name != "true" && id.Name != "false" {
		return false
	}
	switch p := parent.(type) {
	case *ast.SelectorExpr:
		return p.X == ast.Expr(id)
	case *ast.Field, *ast.TypeSpec, *ast.FuncDecl, *ast.LabeledStmt, *ast.BranchStmt, *ast.ImportSpec:
		return false
	case *ast.ValueSpec:
		return !containsIdent(p.Names, id)
	case *ast.AssignStmt:
		return !containsExpr(p.Lhs, id)
	case *ast.RangeStmt:
		return p.Key != ast.Expr(id) && p.Value != ast.Expr(id)
	}
	return true
}

func containsIdent(list []*ast.Ident, id *ast.Ident) bool {
	for _, x := range list {
		if x == id {
			return true
		}
	}
	return false
}

func containsExpr(list []ast.Expr, id *ast.Ident) bool {
	for _, x := range list {
		if x == ast.Expr(id) {
			return true
		}
	}
	return false
}

// inStatementList reports whether stmt sits directly in a braced block or in
// the body of a switch/select clause. (Go has no unbraced if/for bodies, and
// a labelled statement's inner statement does not count.)
func inStatementList(stmt ast.Stmt, parent ast.Node) bool {
	switch p := parent.(type) {
	case *ast.BlockStmt, *ast.CaseClause:
		return true
	case *ast.CommClause:
		return p.Comm != stmt
	}
	return false
}

// isRemovableCall reports whether a statement's expression is a call that
// remove_call may delete: any call except logging and printing.
func isRemovableCall(x ast.Expr) bool {
	for {
		paren, ok := x.(*ast.ParenExpr)
		if !ok {
			break
		}
		x = paren.X
	}
	call, ok := x.(*ast.CallExpr)
	return ok && !isLoggingCall(call)
}

// isLoggingCall reports whether a call only logs or prints: fmt.Print*, any
// call rooted at log, and t.Log* / b.Log* in tests. Removing one rarely
// changes behaviour a test can observe.
func isLoggingCall(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	if selectorRoot(sel) == "log" {
		return true
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	switch pkg.Name {
	case "fmt":
		return strings.HasPrefix(sel.Sel.Name, "Print")
	case "t", "b":
		return strings.HasPrefix(sel.Sel.Name, "Log")
	}
	return false
}

// selectorRoot returns the identifier at the root of a selector chain
// (`log` for log.Default.Printf), or "" when the root is not an identifier.
func selectorRoot(sel *ast.SelectorExpr) string {
	var x ast.Expr = sel
	for {
		s, ok := x.(*ast.SelectorExpr)
		if !ok {
			break
		}
		x = s.X
	}
	if id, ok := x.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}

// lineIndex maps byte offsets to 1-based lines and 1-based columns counted in
// Unicode code points. It reads raw offsets, so //line directives do not
// shift the positions.
type lineIndex struct {
	src    []byte
	starts []int // byte offset where each line starts
}

func newLineIndex(src []byte) lineIndex {
	starts := []int{0}
	for i, b := range src {
		if b == '\n' {
			starts = append(starts, i+1)
		}
	}
	return lineIndex{src: src, starts: starts}
}

func (l lineIndex) position(offset int) (line, column int) {
	i := sort.Search(len(l.starts), func(i int) bool { return l.starts[i] > offset }) - 1
	return i + 1, utf8.RuneCount(l.src[l.starts[i]:offset]) + 1
}

// enclosingMethod returns the qualified name of the innermost analyzed method
// whose line range contains line: the smallest span wins, and on a tie the
// one that starts later. It returns nil for code outside every method.
func enclosingMethod(methods []MethodMetric, line int) *string {
	var best *MethodMetric
	for i := range methods {
		m := &methods[i]
		if line < m.StartLine || line > m.EndLine {
			continue
		}
		if best == nil || isInnermost(m, best) {
			best = m
		}
	}
	if best == nil {
		return nil
	}
	name := best.QualifiedName
	return &name
}

func isInnermost(candidate, best *MethodMetric) bool {
	candidateSpan := candidate.EndLine - candidate.StartLine
	bestSpan := best.EndLine - best.StartLine
	return candidateSpan < bestSpan || (candidateSpan == bestSpan && candidate.StartLine > best.StartLine)
}

// IgnoreMarker is the comment text that switches mutants off, for equivalent
// mutants (changes no test can observe):
//
//	if m.Complexity > max { // slopguard-ignore-mutant(boundary): equal values assign the same max
//
// The bare marker ignores every mutant on its line; a parenthesised,
// comma-separated list ignores only those operator ids (unknown ids are
// dropped). On a line that holds only a comment, the marker applies to the
// next line. Detection is a plain text search of the source lines.
const IgnoreMarker = "slopguard-ignore-mutant"

// ignoredOperators is the marker scope for one line.
type ignoredOperators struct {
	all bool
	ops map[string]bool
}

// ignoreMap holds the ignored operators per 1-based line.
type ignoreMap map[int]*ignoredOperators

func (m ignoreMap) ignores(line int, operator string) bool {
	scope := m[line]
	return scope != nil && (scope.all || scope.ops[operator])
}

// parseIgnoreMarkers finds every marker in src. Two markers that reach the
// same line combine.
func parseIgnoreMarkers(src []byte) ignoreMap {
	markers := ignoreMap{}
	for index, text := range strings.Split(string(src), "\n") {
		at := strings.Index(text, IgnoreMarker)
		if at < 0 {
			continue
		}
		line := index + 1
		if isCommentOnly(text) {
			line++
		}
		scope := markers[line]
		if scope == nil {
			scope = &ignoredOperators{ops: map[string]bool{}}
			markers[line] = scope
		}
		all, ops := markerScope(text[at+len(IgnoreMarker):])
		scope.all = scope.all || all
		for _, op := range ops {
			scope.ops[op] = true
		}
	}
	return markers
}

// isCommentOnly reports whether a line holds only a comment: it starts with
// `//` or `/*`, or it is a block-comment continuation line (` * text`,
// ` */`). A `*` directly followed by code (`*p = a + b`, a pointer
// dereference) starts a Go statement, not a comment.
func isCommentOnly(text string) bool {
	trimmed := strings.TrimLeftFunc(text, unicode.IsSpace)
	if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "/*") {
		return true
	}
	rest, ok := strings.CutPrefix(trimmed, "*")
	return ok && (rest == "" || rest[0] == '/' || unicode.IsSpace(rune(rest[0])))
}

// markerScope reads the text right after a marker: `(a,b)` narrows the marker
// to those operator ids (an unclosed list runs to the end of the line);
// anything else means every operator.
func markerScope(rest string) (all bool, ops []string) {
	if !strings.HasPrefix(rest, "(") {
		return true, nil
	}
	body := rest[1:]
	if closing := strings.IndexByte(body, ')'); closing >= 0 {
		body = body[:closing]
	}
	for _, id := range strings.Split(body, ",") {
		if id = strings.TrimSpace(id); IsMutationOperator(id) {
			ops = append(ops, id)
		}
	}
	return false, ops
}
