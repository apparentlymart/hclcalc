package calc

import (
	"github.com/hashicorp/hcl2/hcl"
	"github.com/hashicorp/hcl2/hcl/hclsyntax"
)

type Expression struct {
	hcl.Expression
	Source []byte
}

func ParseExpression(src []byte, name string) (Expression, hcl.Diagnostics) {
	expr, diags := hclsyntax.ParseExpression(src, name, hcl.Pos{Line: 1, Column: 1})
	return Expression{
		Expression: expr,
		Source:     src,
	}, diags
}

func ParseExpressionString(src string, name string) (Expression, hcl.Diagnostics) {
	srcBytes := []byte(src)
	return ParseExpression(srcBytes, name)
}
