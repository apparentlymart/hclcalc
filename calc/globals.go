package calc

import (
	"github.com/hashicorp/hcl2/hcl"
	"github.com/zclconf/go-cty/cty/function"
	"github.com/zclconf/go-cty/cty/function/stdlib"
)

var globalCtx = &hcl.EvalContext{
	Functions: map[string]function.Function{
		"coalesce":   stdlib.CoalesceFunc,
		"concat":     stdlib.ConcatFunc,
		"format":     stdlib.FormatFunc,
		"formatlist": stdlib.FormatListFunc,
		"hasindex":   stdlib.HasIndexFunc,
		"int":        stdlib.IntFunc,
		"jsondecode": stdlib.JSONDecodeFunc,
		"jsonencode": stdlib.JSONEncodeFunc,
		"length":     stdlib.LengthFunc,
		"lower":      stdlib.LowerFunc,
		"max":        stdlib.MaxFunc,
		"min":        stdlib.MinFunc,
		"reverse":    stdlib.ReverseFunc,
		"strlen":     stdlib.StrlenFunc,
		"substr":     stdlib.SubstrFunc,
		"upper":      stdlib.UpperFunc,
	},
}
