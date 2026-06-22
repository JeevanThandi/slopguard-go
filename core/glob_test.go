package core

import "testing"

func TestMatchesAny(t *testing.T) {
	cases := []struct {
		globs []string
		path  string
		want  bool
	}{
		{[]string{"**/vendor/**"}, "vendor/x/y.go", true},
		{[]string{"**/vendor/**"}, "a/b/vendor/c.go", true},
		{[]string{"**/*_test.go"}, "pkg/foo_test.go", true},
		{[]string{"**/*_test.go"}, "pkg/foo.go", false},
		{[]string{"**/*.pb.go"}, "api/service.pb.go", true},
		{[]string{"**/testdata/**"}, "x/testdata/file.json", true},
		{[]string{"**/sampleapps/**"}, "sampleapps/todolist/todo.go", true},
		{[]string{"**/foo.go"}, "foo.go", true}, // leading-slash variant
		{[]string{"src/*.go"}, "src/main.go", true},
		{[]string{"src/*.go"}, "src/sub/main.go", true}, // * crosses separators
	}
	for _, c := range cases {
		if got := MatchesAny(c.globs, c.path); got != c.want {
			t.Errorf("MatchesAny(%v, %q) = %v, want %v", c.globs, c.path, got, c.want)
		}
	}
}

func TestGlobCharClass(t *testing.T) {
	re := GlobToRegexp("file[0-9].go")
	if !re.MatchString("file3.go") {
		t.Error("expected file3.go to match char class")
	}
	if re.MatchString("filex.go") {
		t.Error("did not expect filex.go to match [0-9]")
	}
}
