package unexport

import (
	"fmt"
	"go/build"
	"golang.org/x/tools/go/buildutil"
	"golang.org/x/tools/go/loader"
	"golang.org/x/tools/go/types"
	"strings"
	"testing"
)

func TestUsedIdentifiers(t *testing.T) {
	for _, test := range []struct {
		ctx *build.Context
		pkg string
	}{
		{ctx: fakeContext(
			map[string][]string{
				"foo": {`
package foo
type I interface{
F()
}
`},
				"bar": {`
package bar
import "foo"
type s int
func (s) F() {}
var _ foo.I = s(0)
`},
			},
		),
			pkg: "bar",
		},
	} {
		prog, err := loadProgram(test.ctx, []string{test.pkg})
		if err != nil {
			t.Fatal(err)
		}
		u := &unexporter{
			iprog:        prog,
			packages:     make(map[*types.Package]*loader.PackageInfo),
			objsToUpdate: make(map[types.Object]bool),
		}
		for _, info := range prog.Imported {
			u.packages[info.Pkg] = info
		}
		used := u.usedObjects()
		if len(used) != 3 {
			t.Errorf("expected 3 used objects, got %v", used)
		}
	}
}

func TestUnusedIdentifiers(t *testing.T) {
	for _, test := range []struct {
		ctx  *build.Context
		pkg  string
		want map[string]string
	}{
		// init data
		// unused var
		{ctx: main(`package main; var Unused int = 1`),
			pkg:  "main",
			want: map[string]string{"\"main\".Unused": "unused"},
		},
		// unused const
		{ctx: main(`package main; const Unused int = 1`),
			pkg:  "main",
			want: map[string]string{"\"main\".Unused": "unused"},
		},
		// unused type
		{ctx: main(`package main; type S int`),
			pkg:  "main",
			want: map[string]string{"\"main\".S": "s"},
		},
		// unused type field
		{ctx: main(`package main; type s struct { T int }`),
			pkg:  "main",
			want: map[string]string{"(\"main\".s).T": "t"},
		},
		// unused type method
		{ctx: main(`package main; type s int; func (s) F(){}`),
			pkg:  "main",
			want: map[string]string{"(\"main\".s).F": "f"},
		},
		// unused interface method
		{ctx: main(`package main; type s interface { F() }`),
			pkg:  "main",
			want: map[string]string{"(\"main\".s).F": "f"},
		},
		// type used by function
		{ctx: fakeContext(map[string][]string{
			"foo": {`
package foo
type S int
type T int
`},
			"bar": {`
package bar
import "foo"

func f(t *foo.T) {}
`},
		}),
			pkg:  "foo",
			want: map[string]string{"\"foo\".S": "s"},
		},
		// type used, but field not used
		{ctx: fakeContext(map[string][]string{
			"foo": {`
package foo
type S struct {
F int
}
`},
			"bar": {`
package bar
import "foo"

var _ foo.S = foo.S{}
`},
		}),
			pkg:  "foo",
			want: map[string]string{"(\"foo\".S).F": "f"},
		},
		// type used, but field not used
		{ctx: fakeContext(map[string][]string{
			"foo": {`
package foo
type S struct {
F int
}
`},
			"bar": {`
package bar
import "foo"

var _ foo.S = foo.S{}
`},
		}),
			pkg:  "foo",
			want: map[string]string{"(\"foo\".S).F": "f"},
		},
		// type embedded, #4
		{ctx: fakeContext(map[string][]string{
			"foo": {`
package foo
type S struct {
f int
}
`},
			"bar": {`
package bar
import "foo"

type x struct {
*foo.S
}
`},
		}),
			pkg: "foo",
		},
		// unused interface type
		{ctx: fakeContext(map[string][]string{
			"foo": {`
package foo
type I interface {
}
`},
		}),
			pkg:  "foo",
			want: map[string]string{"\"foo\".I": "i"},
		},
		// interface satisfied only within package
		{ctx: fakeContext(map[string][]string{
			"foo": {`
package foo
type i interface{
F()
}
type t int
func (t) F() {}
var _ i = t(0)
`},
		}),
			pkg:  "foo",
			want: map[string]string{"(\"foo\".t).F": "f", "(\"foo\".i).F": "f"},
		},
		// interface satisfied by struct type
		{ctx: fakeContext(map[string][]string{
			"foo": {`
package foo
type I interface {
F()
}
`},
			"bar": {`
package bar
import "foo"
type t int
func (t) F() {}
var _ foo.I = t(0)
`},
		}),
			pkg: "foo",
		},
		// interface satisfied by interface
		{ctx: fakeContext(map[string][]string{
			"foo": {`
package foo
type I interface {
F()
}
`},
			"bar": {`
package bar
import "foo"
type j interface {
foo.I
G()
}
type t int
func (t) F() {}
var _ foo.I = t(0)
`},
		}),
			pkg:  "bar",
			want: map[string]string{"(\"bar\".j).G": "g"},
		},
		// interface used in typeswitch
		{ctx: fakeContext(map[string][]string{
			"foo": {`
package foo
type I interface {
F() int
}
`},
			"bar": {`
package bar
import "foo"
func f(z interface{}) {
		switch y := z.(type) {
				case foo.I:
						print(y.F())
				default:
						print(y)
		}
}
`},
		}),
			pkg: "foo",
		},
		// interface used by function
		{ctx: fakeContext(map[string][]string{
			"foo": {`
package foo
type I interface {
F() int
}
`},
			"bar": {`
package bar
import "foo"
func f(y foo.I) int {
return y.F()
}
`},
		}),
			pkg: "foo",
		},
	} {
		// test body
		cmds, err := Main(test.ctx, test.pkg)
		if err != nil {
			t.Fatal(err)
		}
		if len(cmds) > 1 {
			var concated string
			for k, v := range cmds {
				concated += formatCmd(map[string]string{k: v})
			}
			for k, v := range test.want {
				want := map[string]string{k: v}
				if !strings.Contains(concated, formatCmd(want)) {
					t.Errorf("command %s is not returned", formatCmd(want))
				}
			}
		} else {
			if len(test.want) > 0 {
				if len(cmds) == 0 {
					t.Errorf("expected %s, got none", formatCmd(test.want))
				} else if formatCmd(cmds) != formatCmd(test.want) {
					t.Errorf("expected %s, got %s", formatCmd(test.want), formatCmd(cmds))
				}
			} else {
				if len(cmds) > 0 {
					t.Errorf("expected no renaming, got\n %v", cmds)
				}
			}
		}
	}
}

// ---------------------------------------------------------------------

// Simplifying wrapper around buildutil.FakeContext for packages whose
// filenames are sequentially numbered (%d.go).  pkgs maps a package
// import path to its list of file contents.
func fakeContext(pkgs map[string][]string) *build.Context {
	pkgs2 := make(map[string]map[string]string)
	for path, files := range pkgs {
		filemap := make(map[string]string)
		for i, contents := range files {
			filemap[fmt.Sprintf("%d.go", i)] = contents
		}
		pkgs2[path] = filemap
	}
	return buildutil.FakeContext(pkgs2)
}

// helper for single-file main packages with no imports.
func main(content string) *build.Context {
	return fakeContext(map[string][]string{"main": {content}})
}

func formatCmd(pair map[string]string) string {
	for k, v := range pair {
		return fmt.Sprintf("gorename -from %s -to %s\n", k, v)
	}
	return ""
}
