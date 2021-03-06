package structor_test

import (
	"bytes"
	"fmt"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nikolay-turpitko/structor"
	"github.com/nikolay-turpitko/structor/el"
	"github.com/nikolay-turpitko/structor/funcs/encoding"
	"github.com/nikolay-turpitko/structor/funcs/math"
	funcs_strings "github.com/nikolay-turpitko/structor/funcs/strings"
	"github.com/nikolay-turpitko/structor/funcs/use"
	"github.com/nikolay-turpitko/structor/scanner"
)

// TestSimple tests simple structor usage: string fields, data from context,
// simple custom functions.
func TestSimple(t *testing.T) {
	type simple struct {
		A string `eval:"Field '{{.Name}}' had value '{{.Val}}' and tags:[{{printMap .Tags}}]" 1:"first" TagA:"aaa" b:"bbb"`
		B string `eval:"Field '{{.Name}}' had value '{{.Val}}' and tags:[{{index .Tags \"1\"}}, {{.Tags.TagA}}, {{.Tags.b}}]" 1:"first" TagA:"aaa" b:"bbb"`
		C string `eval:"{{.Extra.X}}"`
		D string `eval:"{{.Struct.C}}"`
		E string `eval:"eee"`
		F string `eval:"{{.Struct.E}} + {{.Extra.X}}"`
		G string `eval:""`
	}
	v := &simple{
		A: "init A",
		B: "init B",
		C: "init C",
		D: "init D",
	}
	extra := struct{ X string }{"extra field X"}
	ev := structor.NewDefaultEvaluator(use.FuncMap{
		"printMap": func(m map[string]string) string {
			keys := []string{}
			for k := range m {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			var buf bytes.Buffer
			for _, k := range keys {
				fmt.Fprintf(&buf, "%s:%s, ", k, m[k])
			}
			return buf.String()
		},
	})
	err := ev.Eval(v, extra)
	assert.NoError(t, err)
	assert.Equal(t, "Field 'A' had value 'init A' and tags:[1:first, TagA:aaa, b:bbb, ]", v.A)
	assert.Equal(t, "Field 'B' had value 'init B' and tags:[first, aaa, bbb]", v.B)
	assert.Equal(t, "extra field X", v.C)
	assert.Equal(t, "extra field X", v.D)
	assert.Equal(t, "eee", v.E)
	assert.Equal(t, "eee + extra field X", v.F)
	assert.Equal(t, "", v.G)
}

// TestObj tests structor usage with non-string fields, type conversion,
// nested structures.
func TestObj(t *testing.T) {
	type innerSub struct {
		F1 string `eval:"{{index .Sub 0}}"`
		F2 string `eval:"{{index .Sub 1}}"`
		F3 string `eval:"{{index .Sub 2}}"`
	}
	type inner struct {
		L string `eval:"LLL"`
	}
	type obj struct {
		A string    `eval:"40"`
		B int       `eval:"{{set (add (atoi .Struct.A) (atoi .Tags.b))}}" b:"2"`
		C float64   `eval:"{{set .Struct.B}}"`
		D []byte    `eval:"{{set (unbase64 .Tags.d)}}" d:"dGVzdAo="`
		E []string  `eval:"{{set (split \" \" .Tags.e)}}" e:"first second third"`
		F *innerSub `eval:"{{set .Struct.E}}"`
		G int       `eval:"{{set 42}}"`
		H string    `eval:"{{set 0xa}}"` // conversion of int to string
		I struct {
			J string `eval:"jjj"`
		}
		K inner
		N interface{} `eval:"{{set nil}}"`
		P string      `eval:{{or .Val "second"}}`
	}
	v := &obj{F: &innerSub{}, N: "xxx", P: "first"}
	ev := structor.NewDefaultEvaluator(use.Packages(
		use.Pkg{Prefix: "", Funcs: math.Pkg},
		use.Pkg{Prefix: "", Funcs: encoding.Pkg},
		use.Pkg{Prefix: "", Funcs: funcs_strings.Pkg},
	))
	err := ev.Eval(v, nil)
	assert.NoError(t, err)
	assert.Equal(t, 42, v.B)
	assert.Equal(t, 42.0, v.C)
	assert.Equal(t, []byte("test\n"), v.D)
	assert.Equal(t, []string{"first", "second", "third"}, v.E)
	assert.Equal(t, "first", v.F.F1)
	assert.Equal(t, "second", v.F.F2)
	assert.Equal(t, "third", v.F.F3)
	assert.Equal(t, 42, v.G)
	assert.Equal(t, "\n", v.H)
	assert.Equal(t, "jjj", v.I.J)
	assert.Equal(t, "LLL", v.K.L)
	assert.Nil(t, v.N)
	assert.Equal(t, "first", v.P)
}

// TestError tests structor's error handling.
func TestError(t *testing.T) {
	ev := structor.NewDefaultEvaluator(nil)

	// Error in template contains template name, which consists of struct's
	// type and field name.
	type errStruct struct {
		A string `eval:"{{error}}"`
	}
	v := &errStruct{}
	err := ev.Eval(v, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "<<*structor_test.errStruct.A>>")

	// Error during type conversion contains template name.
	type errStruct2 struct {
		A int `eval:"42"`
	}
	v2 := &errStruct2{}
	err = ev.Eval(v2, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "<<*structor_test.errStruct2.A>>")
}

// cc - char counting "interpreter"
type cc struct{}

func (cc) Execute(expr string, _ *el.Context) (interface{}, error) {
	return len(expr), nil
}

// TestCustomInterpretor tests usage of custom interpreter and tag name.
func TestCustomInterpretor(t *testing.T) {
	ev := structor.NewEvaluator(
		scanner.Default,
		structor.Interpreters{
			"cc": &cc{},
		})
	type theStruct struct {
		A int `cc:"something"`
	}
	v := &theStruct{}
	err := ev.Eval(v, nil)
	assert.NoError(t, err)
	assert.Equal(t, 9, v.A)
}

// TestWholeTag tests usage of the whole tag value as an expression for custom
// interpreter.
func TestWholeTag(t *testing.T) {
	ev := structor.NewEvaluator(
		scanner.Default,
		structor.Interpreters{
			structor.WholeTag: &cc{},
		})
	type theStruct struct {
		A int `this whole string should be processed as an EL expression`
	}
	v := &theStruct{}
	err := ev.Eval(v, nil)
	assert.NoError(t, err)
	assert.Equal(t, 57, v.A)
}

// TestWholeTagAutoEnclose tests usage of the whole tag value as an
// text/template EL expression with automatic enclosing into delimiters.
func TestWholeTagAutoEnclose(t *testing.T) {
	ev := structor.NewEvaluator(
		scanner.Default,
		structor.Interpreters{
			structor.WholeTag: &el.DefaultInterpreter{
				AutoEnclose: true,
				Funcs:       math.Pkg,
			},
		})
	type theStruct struct {
		A int `set 40`
		B int `set 2`
		C int `set (add .Struct.A .Struct.B)`
	}
	v := &theStruct{}
	err := ev.Eval(v, nil)
	assert.NoError(t, err)
	assert.Equal(t, 42, v.C)
}

func TestNonMutatingEvaluator(t *testing.T) {
	ev := structor.NewNonmutatingEvaluator(
		scanner.Default,
		structor.Interpreters{
			structor.WholeTag: &el.DefaultInterpreter{
				AutoEnclose: true,
				Funcs:       math.Pkg,
			},
		})
	type theStruct struct {
		A int     `set 40`
		B int     `set 2`
		C int     `set (add .Struct.A .Struct.B)`
		D *int    `set 5`
		E *string `set "xxx"`
	}
	v := &theStruct{}
	err := ev.Eval(v, nil)
	assert.NoError(t, err)
	assert.Equal(t, 0, v.A)
	assert.Equal(t, 0, v.B)
	assert.Equal(t, 0, v.C)
	assert.Nil(t, v.D)
	assert.Nil(t, v.E)
}

// Example is an example of structor's usage.
//
// Whole struct tag string is used for EL expression.
// Interpreter based on "text/template" used to interpret it.
// Custom math and strings functions are used in expressions.
func Example() {
	ev := structor.NewEvaluator(
		scanner.Default,
		structor.Interpreters{
			structor.WholeTag: &el.DefaultInterpreter{
				AutoEnclose: true,
				Funcs: use.Packages(
					use.Pkg{Funcs: math.Pkg},
					use.Pkg{Funcs: funcs_strings.Pkg},
				),
			},
		})
	type theStruct struct {
		A int    `set 40`
		B int    `set 2`
		C int    `add .Struct.A .Struct.B | set`
		D string `"structor" | upper`
	}
	v := &theStruct{}
	err := ev.Eval(v, nil)
	if err != nil {
		panic(err)
	}
	fmt.Println(v.C)
	fmt.Println(v.D)

	// Output: 42
	// STRUCTOR
}
