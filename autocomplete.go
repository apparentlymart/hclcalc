package main

import (
	"strings"

	prompt "github.com/c-bata/go-prompt"
	"github.com/hashicorp/hcl2/hcl"
	"github.com/hashicorp/hcl2/hcl/hclsyntax"
	"github.com/zclconf/go-cty/cty"
)

func (u ui) completer(d prompt.Document) []prompt.Suggest {
	t := d.TextBeforeCursor()

	// We're going to seek backwards through our string here through
	// characters that look like they could be part of a traversal. Only
	// if we find something promising will we try to actually parse it
	// as one.
	inBracket := false
	dot := false
	canIdent := true
	var result string
	var i int
Chars:
	for i = len(t) - 1; i >= 0; i-- {
		ch := t[i]

		if inBracket {
			if ch == '[' {
				inBracket = false
				canIdent = true
				dot = false
			}
			continue
		}

		switch {

		// This is a very lazy interpretation of identifier characters, since HCL
		// actually permits ID_Start+ID_Continue characters, which is a much bigger
		// set than this.
		case ch == '_' || (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9'):
			if !canIdent {
				result = t[i+1:]
				break Chars
			}
			dot = false
			canIdent = true

		case ch == '.':
			if dot {
				// A pair of sequential dots is not valid
				result = t[i+1:]
				break Chars
			}
			canIdent = true
			dot = true

		case ch == ' ' || ch == '\t' || ch == '\n':
			dot = false
			canIdent = false

		case ch == ']':
			dot = false
			inBracket = true
			canIdent = true

		default:
			// For anything else we'll stop
			result = t[i+1:]
			break Chars
		}
	}
	if i < 0 {
		// We ran out of characters
		result = t
	}

	result = strings.TrimSpace(result)
	if len(result) == 0 || result[0] == '.' || result[0] == '[' || result[len(result)-1] == ']' {
		return nil
	}

	prefix := result
	var completeNext bool
	if result[len(result)-1] == '.' {
		completeNext = true
		result = result[:len(result)-1]
	}

	resultBytes := []byte(result)

	// If we got this far then we have a promising-looking string which we
	// will now try to parse as a traversal.
	traversal, diags := hclsyntax.ParseTraversalAbs(resultBytes, "", hcl.Pos{})
	if diags.HasErrors() {
		return nil
	}

	var toComplete string
	if !completeNext {
		if len(traversal) == 1 {
			toComplete = traversal.RootName()
			traversal = traversal[:0]
		} else {
			var next hcl.Traverser
			traversal, next = traversal[:len(traversal)-1], traversal[len(traversal)-1]
			tNext, ok := next.(hcl.TraverseAttr)
			if !ok {
				return nil
			}
			toComplete = tNext.Name
		}
	}

	if len(traversal) == 0 {
		// We're completing the variable names themselves, then.
		names := u.table.NamesWithPrefix(toComplete)
		if len(names) == 0 {
			return nil
		}
		suggestions := make([]prompt.Suggest, len(names))
		for i, name := range names {
			suggestions[i] = prompt.Suggest{
				Text: name,
			}
		}
		return suggestions
	}

	// If we have a non-empty traversal at this point then we're completing
	// attributes for an object value.
	symName := traversal.RootName()
	val, _ := u.table.Value(symName)
	valTy := val.Type()
	if valTy == cty.DynamicPseudoType {
		return nil
	}

	ctx := &hcl.EvalContext{
		Variables: map[string]cty.Value{
			symName: cty.UnknownVal(valTy),
		},
	}

	val, _ = traversal.TraverseAbs(ctx)
	valTy = val.Type()
	if !valTy.IsObjectType() {
		return nil
	}

	atys := valTy.AttributeTypes()
	var suggestions []prompt.Suggest
	lastDot := strings.LastIndexByte(prefix, '.')
	prefix = prefix[:lastDot+1]
	for name, aty := range atys {
		if strings.HasPrefix(name, toComplete) && name != toComplete {
			suggestions = append(suggestions, prompt.Suggest{
				Text:        prefix + name,
				Description: aty.FriendlyName(),
			})
		}
	}
	return suggestions
}
