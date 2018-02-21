package main

import (
	"bytes"
	"fmt"

	"github.com/apparentlymart/hclcalc/calc"
	prompt "github.com/c-bata/go-prompt"
	"github.com/hashicorp/hcl2/hcl"
	"github.com/hashicorp/hcl2/hcl/hclsyntax"
	"github.com/zclconf/go-cty/cty/json"
)

func main() {
	table := calc.NewTable()
	u := ui{table: table}
	u.runREPL()
}

type ui struct {
	table *calc.Table
}

func (u ui) runREPL() {
	p := prompt.New(u.executor, u.completer)
	p.Run()
}

func (u ui) executor(inp string) {
	src := []byte(inp)
	toks, _ := hclsyntax.LexExpression(src, "", hcl.Pos{Line: 1, Column: 1})

	switch toks[0].Type {

	case hclsyntax.TokenDot:
		if len(toks) < 2 || toks[1].Type != hclsyntax.TokenIdent {
			var diags hcl.Diagnostics
			diags = append(diags, &hcl.Diagnostic{
				Severity: hcl.DiagError,
				Summary:  "Invalid directive",
				Detail:   "A period starting a directive line must be immediately followed by a directive name.",
			})
			u.showDiags(diags)
			break
		}
		u.directive(string(toks[1].Bytes), toks[2:len(toks)-1], src)

	default:
		u.exprOrAssign(toks[:len(toks)-1], src)
	}
}

func (u ui) completer(d prompt.Document) []prompt.Suggest {
	return nil
}

func (u ui) exprOrAssign(toks hclsyntax.Tokens, src []byte) {
	// First we'll see if this looks like an assignment. Any expression that
	// has an equals sign outside of brackets is potentially an assignment,
	// although we'll do some extra validation of the left hand side once
	// we've done this initial pass so we can give the user good feedback
	// if it's invalid.
	bracketCount := 0
	eqPos := -1
Tokens:
	for i, tok := range toks {
		switch tok.Type {
		case hclsyntax.TokenEqual:
			if bracketCount == 0 {
				eqPos = i
				break Tokens
			}
		case hclsyntax.TokenOParen, hclsyntax.TokenOBrace, hclsyntax.TokenOBrack, hclsyntax.TokenOQuote, hclsyntax.TokenOHeredoc, hclsyntax.TokenTemplateInterp, hclsyntax.TokenTemplateControl:
			bracketCount++
		case hclsyntax.TokenCParen, hclsyntax.TokenCBrace, hclsyntax.TokenCBrack, hclsyntax.TokenCQuote, hclsyntax.TokenCHeredoc, hclsyntax.TokenTemplateSeqEnd:
			bracketCount--
		}
	}
	if eqPos != -1 {
		lvalueRange := hcl.Range{
			Filename: toks[0].Range.Filename,
			Start:    toks[0].Range.Start,
			End:      toks[eqPos].Range.Start,
		}
		exprRange := hcl.Range{
			Filename: toks[0].Range.Filename,
			Start:    toks[eqPos].Range.End,
			End:      toks[len(toks)-1].Range.End,
		}
		lvalueSrc := lvalueRange.SliceBytes(src)
		exprSrc := exprRange.SliceBytes(src)
		u.assign(lvalueSrc, exprSrc)
		return
	}

	// If we fall out here then we'll try for a naked expression
	u.expr(src)
}

func (u ui) assign(lvalueSrc, exprSrc []byte) {
	lvalueTrav, diags := hclsyntax.ParseTraversalAbs(lvalueSrc, "", hcl.Pos{Line: 1, Column: 1})
	if len(lvalueTrav) != 1 {
		diags = append(diags, &hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  "Invalid assignment target",
			Detail:   fmt.Sprintf("Cannot assign to %s: a single identifier is required.", bytes.TrimSpace(lvalueSrc)),
		})
		u.showDiags(diags)
		return
	}

	sym := lvalueTrav.RootName()
	expr, exprDiags := calc.ParseExpression(exprSrc, sym)
	diags = append(diags, exprDiags...)
	u.showDiags(diags)
	if diags.HasErrors() {
		return
	}

	u.table.Define(sym, expr)
}

func (u ui) expr(src []byte) {
	expr, diags := calc.ParseExpression(src, "")
	if diags.HasErrors() {
		u.showDiags(diags)
		return
	}

	val, valDiags := u.table.Eval(expr)
	diags = append(diags, valDiags...)
	u.showDiags(diags)
	known := val.IsWhollyKnown()
	if diags.HasErrors() && !known {
		// If we have errors then the result is usually unknown, which is not
		// very interesting to return so we'll suppress it.
		return
	}

	if !known {
		fmt.Printf("(not yet known)\n")
		return
	}

	outBytes, _ := json.Marshal(val, val.Type())
	fmt.Printf("%s\n", outBytes)
}

func (u ui) directive(name string, toks hclsyntax.Tokens, src []byte) {
	switch name {

	case "defs":
		entries, _ := u.table.Values()

		nameLen := 0
		for _, entry := range entries {
			if len(entry.Symbol) > nameLen {
				nameLen = len(name)
			}
		}

		for _, entry := range entries {
			name := entry.Symbol
			src := bytes.TrimSpace(u.table.Source(name))
			if len(src) != 0 {
				fmt.Printf("%*s = %s\n", nameLen, name, src)
			} else {
				fmt.Printf("%*s = (not yet defined)\n", nameLen, name)
			}
		}

	case "vals":
		entries, diags := u.table.Values()
		u.showDiags(diags)

		nameLen := 0
		for _, entry := range entries {
			if len(entry.Symbol) > nameLen {
				nameLen = len(name)
			}
		}

		for _, entry := range entries {
			name := entry.Symbol
			val := entry.Value
			switch {
			case !val.IsWhollyKnown():
				src := bytes.TrimSpace(u.table.Source(name))
				if len(src) != 0 {
					fmt.Printf("%*s = %s\n", nameLen, name, src)
				} else {
					fmt.Printf("%*s = (not yet defined)\n", nameLen, name)
				}
			default:
				outBytes, _ := json.Marshal(val, val.Type())
				fmt.Printf("%*s = %s\n", nameLen, name, outBytes)
			}
		}

	default:
		var diags hcl.Diagnostics
		diags = append(diags, &hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  "Invalid Directive",
			Detail:   fmt.Sprintf("%q is not a valid directive", name),
		})
		u.showDiags(diags)

	}
}

func (u ui) showDiags(diags hcl.Diagnostics) {
	// TODO: Make this nicer
	for _, diag := range diags {
		fmt.Printf("- %s\n", diag)
	}
}
