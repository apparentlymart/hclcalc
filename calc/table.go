package calc

import (
	"sort"
	"sync"

	"github.com/hashicorp/hcl2/hcl"
	"github.com/hashicorp/hcl2/hcl/hclsyntax"
	"github.com/zclconf/go-cty/cty"
)

type Table struct {
	syms   map[string]Expression
	all    symbolSet
	reqs   edgeSet
	reqdBy edgeSet

	cacheMutex sync.Mutex
	valCache   map[string]cty.Value
	orderCache []string
	diagsCache hcl.Diagnostics
}

func NewTable() *Table {
	return &Table{
		syms:   make(map[string]Expression),
		all:    make(symbolSet),
		reqs:   make(edgeSet),
		reqdBy: make(edgeSet),
	}
}

func (t *Table) Define(name string, expr Expression) {
	t.cacheMutex.Lock()
	defer t.cacheMutex.Unlock()

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

	t.purgeCaches()
}

func (t *Table) Remove(name string) {
	t.cacheMutex.Lock()
	defer t.cacheMutex.Unlock()

	t.remove(name)
	t.purgeCaches()
}

func (t *Table) remove(name string) {
	// This internal form of remove must only be called by a function that
	// is already holding t.cacheMutex

	delete(t.syms, name)
	for reqdName := range t.reqs {
		t.reqdBy.Remove(reqdName, name)
	}
	t.reqs.RemoveFrom(name)
	if !t.reqs.FromHasAny(name) && !t.reqdBy.FromHasAny(name) {
		t.all.Remove(name)
	}
}

func (t *Table) purgeCaches() {
	// This must only be called by a function that is holding t.cacheMutex
	t.valCache = nil
	t.orderCache = nil
	t.diagsCache = nil
}

func (t *Table) visitSymbols(cb func(name string, expr Expression)) symbolSet {
	queue := make([]string, 0, len(t.all))
	inDeg := make(map[string]int, len(t.all))

	for name := range t.all {
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
			if inDeg[newName] == 0 {
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

func (t *Table) fillCaches() {
	// This must only be called by a function that is holding t.cacheMutex

	var diags hcl.Diagnostics
	ctx := globalCtx.NewChild()
	order := make([]string, 0, len(t.all))
	ctx.Variables = make(map[string]cty.Value, len(t.all))

	cycled := t.visitSymbols(func(name string, expr Expression) {
		var valDiags hcl.Diagnostics
		order = append(order, name)
		ctx.Variables[name], valDiags = expr.Value(ctx)
		diags = append(diags, valDiags...)
	})
	cycledStart := len(order)
	for name := range cycled {
		order = append(order, name)
		ctx.Variables[name] = cty.DynamicVal
	}
	sort.Strings(order[cycledStart:])

	t.valCache = ctx.Variables
	t.orderCache = order
	t.diagsCache = diags
}

func (t *Table) Values() map[string]cty.Value {
	t.cacheMutex.Lock()
	defer t.cacheMutex.Unlock()

	if t.valCache == nil {
		t.fillCaches()
	}

	return t.valCache
}

func (t *Table) Names() []string {
	t.cacheMutex.Lock()
	defer t.cacheMutex.Unlock()

	if t.orderCache == nil {
		t.fillCaches()
	}

	return t.orderCache
}

func (t *Table) Diagnostics() hcl.Diagnostics {
	t.cacheMutex.Lock()
	defer t.cacheMutex.Unlock()

	if t.diagsCache == nil {
		t.fillCaches()
	}

	return t.diagsCache
}

func (t *Table) Eval(expr Expression) (cty.Value, hcl.Diagnostics) {
	var diags hcl.Diagnostics
	ctx := globalCtx.NewChild()
	ctx.Variables = t.Values()
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
