package calc

import (
	"fmt"
	"sort"
	"strings"

	"github.com/hashicorp/hcl2/hcl"
	"github.com/hashicorp/hcl2/hcl/hclsyntax"
	"github.com/zclconf/go-cty/cty"
)

type Table struct {
	syms   map[string]Expression
	all    symbolSet
	reqs   edgeSet
	reqdBy edgeSet
}

func NewTable() *Table {
	return &Table{
		syms:   make(map[string]Expression),
		all:    make(symbolSet),
		reqs:   make(edgeSet),
		reqdBy: make(edgeSet),
	}
}

func (t *Table) Source(name string) []byte {
	return t.syms[name].Source
}

func (t *Table) Define(name string, expr Expression) {
	// Discard any existing symbol with the same name
	t.remove(name)

	t.syms[name] = expr
	for _, traversal := range expr.Variables() {
		reqdName := traversal.RootName()

		t.all.Add(reqdName)
		t.reqs.Add(name, reqdName)
		t.reqdBy.Add(reqdName, name)
	}
	t.all.Add(name)
}

func (t *Table) Remove(name string) {
	t.remove(name)
}

func (t *Table) remove(name string) {
	delete(t.syms, name)
	for reqdName := range t.reqs {
		t.reqdBy.Remove(reqdName, name)
	}
	t.reqs.RemoveFrom(name)
	if !t.reqs.FromHasAny(name) && !t.reqdBy.FromHasAny(name) {
		t.all.Remove(name)
	}
}

func (t *Table) visitSymbols(syms symbolSet, cb func(name string, expr Expression)) symbolSet {
	queue := make([]string, 0, len(t.all))
	inDeg := make(map[string]int, len(t.all))

	for name := range syms {
		inDeg[name] = len(t.reqs.AllFrom(name))
		if inDeg[name] == 0 {
			queue = append(queue, name)
		}
	}

	// Sort the initial items so that we'll visit them in lexicographical order.
	sort.Strings(queue)

	for len(queue) > 0 {
		// dequeue next item
		var name string
		name, queue = queue[0], queue[1:]

		expr, defined := t.syms[name]
		if !defined {
			expr = missingExpr
		}
		cb(name, expr)

		newQueueIdx := len(queue)
		for newName := range t.reqdBy[name] {
			inDeg[newName]--
			if inDeg[newName] == 0 && syms.Has(newName) {
				queue = append(queue, newName)
			}
		}
		// Sort the items we just queued so that we'll visit them in
		// lexicographical order.
		sort.Strings(queue[newQueueIdx:])
	}

	// If there's anything left in inDeg then we have a cycle. We'll return
	// these so the caller can decide what to do with them.
	cycled := newSymbolSet()
	for name, v := range inDeg {
		if v > 0 {
			cycled.Add(name)
		}
	}
	return cycled
}

func (t *Table) Value(name string) (cty.Value, hcl.Diagnostics) {
	expr, defined := t.syms[name]
	if !defined {
		var diags hcl.Diagnostics
		diags = append(diags, &hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  "Variable not defined",
			Detail:   fmt.Sprintf("The variable %q has not yet had an expression assigned.", name),
		})
		return cty.DynamicVal, diags
	}

	return t.Eval(expr)
}

func (t *Table) addRequiredSymbols(expr Expression, set symbolSet) {
	for _, traversal := range expr.Variables() {
		name := traversal.RootName()
		if set.Has(name) {
			continue
		}
		set.Add(traversal.RootName())
		if reqdExpr, defined := t.syms[name]; defined {
			t.addRequiredSymbols(reqdExpr, set)
		}
	}
}

func (t *Table) Values() ([]TableSymbolValue, hcl.Diagnostics) {
	if len(t.all) == 0 {
		return nil, nil
	}

	ret := make([]TableSymbolValue, 0, len(t.all))
	var diags hcl.Diagnostics

	ctx := globalCtx.NewChild()
	ctx.Variables = make(map[string]cty.Value, len(t.all))

	cycled := t.visitSymbols(t.all, func(name string, expr Expression) {
		val, valDiags := expr.Value(ctx)
		ret = append(ret, TableSymbolValue{
			Symbol: name,
			Value:  val,
		})
		ctx.Variables[name] = val
		diags = append(diags, valDiags...)
	})

	if len(cycled) > 0 {
		firstCycled := len(ret)
		for name := range cycled {
			ret = append(ret, TableSymbolValue{
				Symbol: name,
				Value:  cty.DynamicVal,
			})
		}
		sort.Slice(ret[firstCycled:], func(i, j int) bool {
			return ret[firstCycled+i].Symbol < ret[firstCycled+j].Symbol
		})

		diags = append(diags, &hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  "Dependency cycle",
			Detail:   fmt.Sprintf("There is a dependency cycle between the following variables: %s.", strings.Join(cycled.AppendNames(nil), ", ")),
		})
	}

	return ret, diags
}

func (t *Table) Eval(expr Expression) (cty.Value, hcl.Diagnostics) {
	var diags hcl.Diagnostics

	reqd := newSymbolSet()
	t.addRequiredSymbols(expr, reqd)

	var undef []string
	for name := range reqd {
		_, defined := t.syms[name]
		if !defined {
			undef = append(undef, name)
		}
	}
	sort.Strings(undef)
	for _, name := range undef {
		diags = append(diags, &hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  "Variable not defined",
			Detail:   fmt.Sprintf("The variable %q has not yet had an expression assigned.", name),
		})
	}

	ctx := globalCtx.NewChild()
	ctx.Variables = make(map[string]cty.Value, len(reqd))

	cycled := t.visitSymbols(reqd, func(name string, expr Expression) {
		val, valDiags := expr.Value(ctx)
		diags = append(diags, valDiags...)
		ctx.Variables[name] = val
	})

	if len(cycled) > 0 {
		for name := range cycled {
			ctx.Variables[name] = cty.DynamicVal
		}

		diags = append(diags, &hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  "Dependency cycle",
			Detail:   fmt.Sprintf("There is a dependency cycle between the following variables: %s.", strings.Join(cycled.AppendNames(nil), ", ")),
		})
	}

	ret, valDiags := expr.Value(ctx)
	diags = append(diags, valDiags...)
	return ret, diags
}

var globalCtx = &hcl.EvalContext{}

var missingExpr = Expression{
	Expression: &hclsyntax.LiteralValueExpr{
		Val: cty.DynamicVal,
	},
}

type TableSymbolValue struct {
	Symbol string
	Value  cty.Value
}
