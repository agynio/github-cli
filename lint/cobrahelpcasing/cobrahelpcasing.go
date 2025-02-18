package cobrahelpcasing

import (
	"go/ast"
	"go/token"
	"strconv"
	"strings"

	"github.com/golangci/plugin-module-register/register"
	"golang.org/x/tools/go/analysis"
)

func init() {
	register.Plugin("cobrahelpcasing", New)
}

type Plugin struct{}

func New(settings any) (register.LinterPlugin, error) {
	return &Plugin{}, nil
}

func (p *Plugin) BuildAnalyzers() ([]*analysis.Analyzer, error) {
	return []*analysis.Analyzer{
		{
			Name: "cobrahelpcasing",
			Doc:  "Ensures that Cobra flag help texts start with a capital letter.",
			Run:  p.run,
		},
	}, nil
}

func (p *Plugin) GetLoadMode() string {
	return register.LoadModeSyntax
}

func (p *Plugin) run(pass *analysis.Pass) (interface{}, error) {
	for _, file := range pass.Files {
		ast.Inspect(file, func(n ast.Node) bool {
			callExpr, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}

			// Check if it's a Cobra flag definition
			if !isFlagVarFunction(callExpr) {
				return true
			}

			// Ensure there are at least 4 arguments (the 4th one is the help text)
			if len(callExpr.Args) < 4 {
				return true
			}

			// Extract the help text
			helpText, ok := extractStringLiteral(callExpr.Args[3])
			if !ok {
				return true
			}

			// Check if the help text starts with a lowercase letter
			if len(helpText) > 0 && strings.ToUpper(string(helpText[0])) != string(helpText[0]) {
				pass.Report(analysis.Diagnostic{
					Pos:      callExpr.Args[3].Pos(),
					Category: "cobrahelpcasing",
					Message:  "Cobra flag help text should start with a capital letter.",
				})
			}

			return true
		})
	}

	return nil, nil
}

func isFlagVarFunction(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}

	flagVarFuncs := map[string]bool{
		"StringVar":  true,
		"BoolVar":    true,
		"IntVar":     true,
		"Float64Var": true,
	}

	return flagVarFuncs[sel.Sel.Name]
}

func extractStringLiteral(expr ast.Expr) (string, bool) {
	if lit, ok := expr.(*ast.BasicLit); ok && lit.Kind == token.STRING {
		unquoted, err := strconv.Unquote(lit.Value)
		if err == nil {
			return unquoted, true
		}
	}
	return "", false
}
