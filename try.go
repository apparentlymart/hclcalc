package main

import (
	"bufio"
	"bytes"
	"fmt"

	"github.com/apparentlymart/hclcalc/calc"
	prompt "github.com/c-bata/go-prompt"
	"github.com/hashicorp/hcl2/hcl"
	"github.com/hashicorp/hcl2/hcl/hclsyntax"
	wordwrap "github.com/mitchellh/go-wordwrap"
	"github.com/zclconf/go-cty/cty/json"
)

func main() {
	pp := prompt.NewStandardInputParser()
	size := pp.GetWinSize()

	table := calc.NewTable()
	u := ui{
		table: table,
		size:  size,
	}
	u.runREPL()
}

type ui struct {
	table *calc.Table
	size  *prompt.WinSize
}

func (u ui) runREPL() {
	p := prompt.New(u.executor, u.completer)
	p.Run()
}

func (u ui) executor(inp string) {
	src := []byte(inp)
	toks, _ := hclsyntax.LexExpression(src, "", hcl.Pos{Line: 1, Column: 1})
	if len(toks) == 1 {
		return
	}

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
		u.showDiagsSrc(diags, src)
		return
	}

	val, valDiags := u.table.Eval(expr)
	diags = append(diags, valDiags...)
	u.showDiagsSrc(diags, src)
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
	fmt.Printf("%s\n\n", outBytes)
}

func (u ui) directive(name string, toks hclsyntax.Tokens, src []byte) {
	switch name {

	case "clear":
		fmt.Print("\x1b[2J\x1b[0;0H")

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
	u.showDiagsSrc(diags, nil)
}

func (u ui) showDiagsSrc(diags hcl.Diagnostics, defSrc []byte) {
	if len(diags) == 0 {
		return
	}

	fmt.Print("\n")

	for _, diag := range diags {
		switch diag.Severity {
		case hcl.DiagError:
			fmt.Print("\x1b[1;31mError: \x1b[0m")
		case hcl.DiagWarning:
			fmt.Print("\x1b[1;33mWarning: \x1b[0m")
		}
		fmt.Printf("\x1b[1m%s\x1b[0m\n", diag.Summary)

		var src []byte
		var srcName string
		if diag.Subject != nil {
			if diag.Subject.Filename != "" {
				srcName = diag.Subject.Filename
				src = u.table.Source(srcName)
			} else {
				src = defSrc
			}
		}

		if src != nil {
			// We'll attempt to render a source code snippet as context
			name := srcName
			highlightRange := *diag.Subject
			if highlightRange.Empty() {
				// We can't illustrate an empty range, so we'll turn such ranges into
				// single-character ranges, which might not be totally valid (may point
				// off the end of a line, or off the end of the file) but are good
				// enough for the bounds checks we do below.
				highlightRange.End.Byte++
				highlightRange.End.Column++
			}
			sc := hcl.NewRangeScanner(src, name, bufio.ScanLines)
			var prefix string
			if name != "" {
				prefix = name + " = "
			} else {
				prefix = "> "
			}
			prefixLen := len(prefix)
			for sc.Scan() {
				lineRange := sc.Range()
				beforeRange, highlightedRange, afterRange := lineRange.PartitionAround(highlightRange)
				if highlightedRange.Empty() {
					fmt.Printf("    %*s%s\n", prefixLen, prefix, bytes.TrimSpace(sc.Bytes()))
				} else {
					before := beforeRange.SliceBytes(src)
					highlighted := highlightedRange.SliceBytes(src)
					after := afterRange.SliceBytes(src)
					fmt.Printf("    %*s%s\x1b[1;4m%s\x1b[m%s\n", prefixLen, prefix, bytes.TrimLeft(before, " "), highlighted, after)
				}
				prefix = "" // don't repeat the "name =" prefix on subsequent lines
			}
		}

		fmt.Printf("%s\n\n", wordwrap.WrapString(diag.Detail, uint(u.size.Col)))
	}
}
