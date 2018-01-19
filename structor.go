/*
Package structor contains interface and default implementation of EL
(expression language) evaluator which evaluates EL expressions within tags of
the struct, using optional additional struct as an extra context.

Basic idea is to use simple expression language within Go struct tags to
compute struct fields based on other fields or provided additional context.

Due usage of reflection and EL interpretation, this package is hardly suitable
for tasks, requiring high performance, but rather intended to be used during
application setup or in cases where high performance is not an ultimate goal.

See tests for examples of usage with xpath, regexp, goquery, encryption,
reading from files, shell invocation, etc.
*/
package structor

import (
	"fmt"
	"reflect"

	multierror "github.com/hashicorp/go-multierror"

	"github.com/nikolay-turpitko/structor/el"
	"github.com/nikolay-turpitko/structor/funcs/use"
	"github.com/nikolay-turpitko/structor/scanner"
)

// Evaluator is an interface of evaluator, which gets structure and extra
// context as input, iterates over `s`'s fields and evaluate expression tag on
// every field.
type Evaluator interface {
	Eval(s, extra interface{}) error
}

// Interpreters is a map of tag names to el.Interpreters.  Used to register
// different interpreters for different tag names.
//
// Only first tag name on the struct field is currently recognized and
// processed. So, only one EL expression per structure field, but different
// fields of the same structure can be processed by different interpreters.
type Interpreters map[string]el.Interpreter

// WholeTag constant can be used as tag name in the Interpreters to indicate
// that whole tag value should be passed to the interpreter.
//
// Registering interpreter to the whole tag value conflicts with any other
// usage of the struct's tag, but can be convenient to simplify complex EL
// expressions with quotes (regexp, for example).
//
// WholeTag interpreter is probed after all other registered interpreters.
const WholeTag = ""

// NewEvaluator returns Evaluator with desired settings.
//
// Only first tag with EL will be recognized and used (only one
// expression per struct field). Different fields of the same struct can be
// processed using different EL interpreters.
//
//  scanner - is a scanner implementation to be used to scan tags.
//  interpreters - is a map of registered tag names to EL interpreters.
func NewEvaluator(
	scanner scanner.Scanner,
	interpreters Interpreters) Evaluator {
	return NewEvaluatorWithOptions(scanner, interpreters, Options{})
}

func NewEvaluatorWithOptions(
	scanner scanner.Scanner,
	interpreters Interpreters,
	options Options) Evaluator {
	if len(interpreters) == 0 {
		panic("no interpreters registered")
	}
	return &evaluator{scanner, interpreters, options}
}

// NewDefaultEvaluator returns default Evaluator implementation. Default
// implementation uses tag "eval" for expressions and EL interpreter, based on
// `"text/template"`.
//
//  funcs - custom functions, available for interpreter;
func NewDefaultEvaluator(funcs use.FuncMap) Evaluator {
	return NewEvaluator(
		scanner.Default,
		Interpreters{
			"eval": &el.DefaultInterpreter{Funcs: funcs},
		})
}

// NewNonmutatingEvaluator creates Evaluator implementation which does not
// change original structure (does not save evaluated results) itself.
// Though, interpreters can change structures' fields as a side effect.
//
// See NewEvaluator() for additional information.
//
// BUG: does not visit embedded structs.
func NewNonmutatingEvaluator(
	scanner scanner.Scanner,
	interpreters Interpreters) Evaluator {
	return NewEvaluatorWithOptions(scanner, interpreters, Options{NonMutating: true})
}

type evaluator struct {
	scanner      scanner.Scanner
	interpreters Interpreters
	options      Options
}

type Options struct {
	NonMutating   bool
	EvalEmptyTags bool
}

func (ev evaluator) Eval(s, extra interface{}) error {
	return ev.eval(s, extra, nil, nil)
}

func (ev evaluator) evalExpr(
	intrprName, expr string,
	ctx *el.Context) (interface{}, error) {
	intrpr, ok := ev.interpreters[intrprName]
	if !ok {
		return nil, fmt.Errorf("unknown interpreter: %s", intrprName)
	}
	return intrpr.Execute(expr, ctx)
}

func (ev evaluator) eval(s, extra, substruct, subctx interface{}) error {
	curr := s
	if substruct != nil {
		curr = substruct
	}
	val, typ, err := ev.structIntrospect(curr)
	if err != nil {
		return err
	}
	var merr error
	for i, l := 0, typ.NumField(); i < l; i++ {
		err := func() error {
			f, err := ev.fieldIntrospect(val, typ, i)
			longName := fmt.Sprintf("%T.%s", curr, f.name)
			if err != nil {
				return fmt.Errorf("structor: <<%s>>: %v", longName, err)
			}
			var result interface{}
			if f.expr != "" || ev.options.EvalEmptyTags {
				ctx := &el.Context{
					Name:     f.name,
					LongName: longName,
					Tags:     f.tags,
					Struct:   s,
					Extra:    extra,
					Sub:      subctx,
					EvalExpr: ev.evalExpr,
				}
				if f.value.IsValid() {
					ctx.Val = f.value.Interface()
				}
				result, err = f.interpreter.Execute(f.expr, ctx)
				if err != nil {
					return err
				}
				if !ev.options.NonMutating && f.settable {
					err := reflectSet(f.value, f.typ, result)
					if err != nil {
						return fmt.Errorf("structor: <<%s>>: %v", longName, err)
					}
				}
			}
			v := f.value
			k := v.Kind()
			if k == reflect.Interface {
				v = v.Elem()
				k = reflect.Indirect(v).Kind()
			}
			if k == reflect.Struct {
				// process embedded struct with tag
				return ev.eval(s, extra, byRef(v), result)
			}
			return nil
		}()
		if err != nil {
			merr = multierror.Append(merr, err)
		}
	}
	return merr
}

func (ev evaluator) structIntrospect(
	s interface{}) (reflect.Value, reflect.Type, error) {
	v := reflect.Indirect(reflect.ValueOf(s))
	t := v.Type()
	if t.Kind() != reflect.Struct {
		err := fmt.Errorf(
			"structor: %v must be a struct or a pointer to struct, actually: %v",
			s,
			t.Kind())
		return v, t, err
	}
	return v, t, nil
}

type fieldDescr struct {
	name        string
	expr        string
	interpreter el.Interpreter
	value       reflect.Value
	typ         reflect.Type
	tags        map[string]string
	settable    bool
}

func (ev evaluator) fieldIntrospect(
	val reflect.Value,
	typ reflect.Type,
	i int) (fieldDescr, error) {
	f := typ.Field(i)
	v := reflect.Indirect(val.Field(i))
	tags, err := ev.scanner.Tags(f.Tag)
	res := fieldDescr{
		name:  f.Name,
		value: v,
		typ:   f.Type,
		tags:  tags,
	}
	if err != nil {
		return res, err
	}
	res.settable = v.CanSet() || (v.CanAddr() && v.Addr().CanSet())
	for k, t := range tags {
		if intr, ok := ev.interpreters[k]; ok {
			delete(tags, k)
			res.expr = t
			res.interpreter = intr
			return res, nil
		}
	}
	if intr, ok := ev.interpreters[WholeTag]; ok {
		delete(tags, WholeTag)
		res.expr = string(f.Tag)
		res.interpreter = intr
		return res, nil
	}
	return res, nil
}

func reflectSet(v reflect.Value, vt reflect.Type, nv interface{}) (err error) {
	defer func() {
		if r := recover(); r != nil {
			var ok bool
			if err, ok = r.(error); !ok {
				err = fmt.Errorf("structor: %v", r)
			}
		}
	}()
	if nv == nil {
		if v.IsValid() {
			v.Set(reflect.Zero(vt))
		}
		return nil
	}
	vnv := reflect.ValueOf(nv)
	if !vnv.Type().ConvertibleTo(vt) &&
		v.Kind() == reflect.Struct {
		return nil
	}
	// Try to convert, it may give a panic with suitable message.
	v.Set(vnv.Convert(vt))
	return nil
}

func byRef(v reflect.Value) interface{} {
	if v.CanAddr() {
		v = v.Addr()
	}
	return v.Interface()
}
