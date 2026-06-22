package core

import (
	"regexp"
	"strings"
)

// Minimal glob matcher replicating slopguard-swift's fnmatch semantics
// (FNM_PATHNAME off): `*` matches across path separators — which is the
// behaviour most people expect from `**`-style globs ("everything under
// vendor"). `?` matches one character; `[...]` character classes pass through
// (`[!...]` negates).

var globCache = map[string]*regexp.Regexp{}

// GlobToRegexp compiles an fnmatch-style glob into an anchored regular
// expression.
func GlobToRegexp(glob string) *regexp.Regexp {
	var b strings.Builder
	b.WriteString("^")
	i := 0
	for i < len(glob) {
		ch := glob[i]
		switch ch {
		case '*':
			b.WriteString(".*")
			i++
		case '?':
			b.WriteString(".")
			i++
		case '[':
			close := strings.IndexByte(glob[i+2:], ']')
			if close < 0 {
				b.WriteString("\\[")
				i++
				continue
			}
			close += i + 2
			body := glob[i+1 : close]
			if strings.HasPrefix(body, "!") {
				body = "^" + body[1:]
			}
			body = strings.ReplaceAll(body, "\\", "\\\\")
			b.WriteString("[")
			b.WriteString(body)
			b.WriteString("]")
			i = close + 1
		default:
			b.WriteString(regexp.QuoteMeta(string(ch)))
			i++
		}
	}
	b.WriteString("$")
	return regexp.MustCompile(b.String())
}

func compiledGlob(glob string) *regexp.Regexp {
	if re, ok := globCache[glob]; ok {
		return re
	}
	re := GlobToRegexp(glob)
	globCache[glob] = re
	return re
}

// MatchesAny matches the path against each glob, also trying a leading-slash
// variant so that `**/Foo/**` patterns match a top-level `Foo/bar.go`
// (mirroring gitignore semantics where a leading `**/` is effectively
// implicit). Paths must be forward-slash normalised.
func MatchesAny(globs []string, path string) bool {
	withSlash := "/" + path
	for _, g := range globs {
		re := compiledGlob(g)
		if re.MatchString(path) || re.MatchString(withSlash) {
			return true
		}
	}
	return false
}
