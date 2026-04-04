// Package diagnosticwriter provides a shim for typescript-go's internal diagnosticwriter package.
package diagnosticwriter

import (
	"fmt"
	"io"
	"strings"
	"unsafe"

	"github.com/microsoft/typescript-go/internal/ast"
	"github.com/microsoft/typescript-go/internal/core"
	"github.com/microsoft/typescript-go/internal/diagnostics"
	"github.com/microsoft/typescript-go/internal/diagnosticwriter"
	"github.com/microsoft/typescript-go/internal/locale"
	"github.com/microsoft/typescript-go/internal/scanner"
	"github.com/microsoft/typescript-go/internal/tspath"
)

const (
	colorReset  = "\x1b[0m"
	colorRed    = "\x1b[91m"
	colorYellow = "\x1b[93m"
	colorCyan   = "\x1b[96m"
	colorGrey   = "\x1b[90m"
)

// FormatTSTLDiagnostic writes a tslua diagnostic. When colorize is true, includes
// colors, source context, and squiggly underlines. When false, single-line plain text.
func FormatTSTLDiagnostic(w io.Writer, d *ast.Diagnostic, cwd string, format DiagnosticFormat, colorize bool) {
	opts := &diagnosticwriter.FormattingOptions{
		NewLine: "\n",
		ComparePathsOptions: tspath.ComparePathsOptions{
			CurrentDirectory: cwd,
		},
	}
	wrapped := diagnosticwriter.WrapASTDiagnostic(d)

	code := DiagCode(wrapped.Code())

	if !colorize {
		// Plain single-line: file(line,col): error TSTL: message
		if wrapped.File() != nil {
			line, character := scanner.GetECMALineAndUTF16CharacterOfPosition(wrapped.File(), wrapped.Pos())
			relPath := tspath.ConvertToRelativePath(wrapped.File().FileName(), opts.ComparePathsOptions)
			fmt.Fprintf(w, "%s(%d,%d): ", relPath, line+1, int(character)+1)
		}
		switch format {
		case DiagFormatNative:
			fmt.Fprintf(w, "%s TL%d: ", wrapped.Category().Name(), wrapped.Code())
		default:
			fmt.Fprintf(w, "%s TSTL: ", wrapped.Category().Name())
		}
		diagnosticwriter.WriteFlattenedDiagnosticMessage(w, wrapped, opts.NewLine, opts.Locale)
		fmt.Fprint(w, opts.NewLine)
		if help := diagHelp[code]; help != "" {
			writeHelp(w, help, false)
		}
		return
	}

	// Location: file:line:col
	if wrapped.File() != nil {
		diagnosticwriter.WriteLocation(w, wrapped.File(), wrapped.Pos(), opts, writeWithStyleAndReset)
		fmt.Fprint(w, " - ")
	}

	// Severity + code prefix
	cat := wrapped.Category()
	color := colorRed
	if cat == diagnostics.CategoryWarning {
		color = colorYellow
	}
	fmt.Fprintf(w, "%s%s%s", color, cat.Name(), colorReset)
	switch format {
	case DiagFormatNative:
		fmt.Fprintf(w, "%s TL%d: %s", colorGrey, wrapped.Code(), colorReset)
	default:
		fmt.Fprintf(w, "%s TSTL: %s", colorGrey, colorReset)
	}

	// Message text
	diagnosticwriter.WriteFlattenedDiagnosticMessage(w, wrapped, opts.NewLine, opts.Locale)

	// Source context + squiggly underline
	if wrapped.File() != nil {
		fmt.Fprint(w, opts.NewLine)
		writeCodeSnippet(w, wrapped.File(), wrapped.Pos(), wrapped.Len(), color, "", opts)
		fmt.Fprint(w, opts.NewLine)
	}

	// Help text
	if help := diagHelp[code]; help != "" {
		writeHelp(w, help, true)
	}
}

// FormatTSDiagnostics writes TypeScript diagnostics. When colorize is true, uses
// tsgo's native colored formatter with source context. When false, plain single-line.
func FormatTSDiagnostics(w io.Writer, diags []*ast.Diagnostic, cwd string, colorize bool) {
	opts := &diagnosticwriter.FormattingOptions{
		NewLine: "\n",
		ComparePathsOptions: tspath.ComparePathsOptions{
			CurrentDirectory: cwd,
		},
	}
	wrapped := diagnosticwriter.FromASTDiagnostics(diags)
	if colorize {
		diagnosticwriter.FormatDiagnosticsWithColorAndContext(w, wrapped, opts)
	} else {
		diagnosticwriter.WriteFormatDiagnostics(w, wrapped, opts)
	}
}

// FormatTSTLDiagnostics writes multiple tslua diagnostics.
func FormatTSTLDiagnostics(w io.Writer, diags []*ast.Diagnostic, cwd string, format DiagnosticFormat, colorize bool) {
	for i, d := range diags {
		if i > 0 && colorize {
			fmt.Fprintln(w)
		}
		FormatTSTLDiagnostic(w, d, cwd, format, colorize)
	}
}

// writeHelp renders help text with "  help: " prefix, indenting continuation lines.
func writeHelp(w io.Writer, help string, colorize bool) {
	lines := strings.Split(help, "\n")
	for i, line := range lines {
		if i == 0 {
			if colorize {
				fmt.Fprintf(w, "%s  help: %s%s\n", colorCyan, colorReset, line)
			} else {
				fmt.Fprintf(w, "  help: %s\n", line)
			}
		} else {
			fmt.Fprintf(w, "        %s\n", line)
		}
	}
}

// writeWithStyleAndReset matches diagnosticwriter's FormattedWriter signature.
func writeWithStyleAndReset(w io.Writer, s string, style string) {
	fmt.Fprintf(w, "%s%s%s", style, s, colorReset)
}

//go:linkname writeCodeSnippet github.com/microsoft/typescript-go/internal/diagnosticwriter.writeCodeSnippet
func writeCodeSnippet(writer io.Writer, sourceFile diagnosticwriter.FileLike, start int, length int, squiggleColor string, indent string, formatOpts *diagnosticwriter.FormattingOptions)

// DiagCode is a diagnostic code matching TSTL's numbering (100xxx series).
// Codes match the TSTL test environment (where src entry point is imported first).
type DiagCode int32

const (
	UnsupportedNodeKind                      DiagCode = 100013
	ForbiddenForIn                           DiagCode = 100014
	UnsupportedNoSelfFunctionConversion      DiagCode = 100015
	UnsupportedSelfFunctionConversion        DiagCode = 100016
	UnsupportedOverloadAssignment            DiagCode = 100017
	DecoratorInvalidContext                  DiagCode = 100018
	AnnotationInvalidArgumentCount           DiagCode = 100019
	InvalidRangeUse                          DiagCode = 100020
	InvalidVarargUse                         DiagCode = 100021
	InvalidRangeControlVariable              DiagCode = 100022
	InvalidMultiIterableWithoutDestructuring DiagCode = 100023
	InvalidPairsIterableWithoutDestructuring DiagCode = 100024
	UnsupportedAccessorInObjectLiteral      DiagCode = 100025
	UnsupportedRightShiftOperator            DiagCode = 100026
	UnsupportedForTarget                     DiagCode = 100027
	UnsupportedForTargetButOverrideAvailable DiagCode = 100028
	UnsupportedProperty                      DiagCode = 100029
	InvalidAmbientIdentifierName             DiagCode = 100030
	UnsupportedVarDeclaration                DiagCode = 100031
	InvalidMultiFunctionUse                  DiagCode = 100032
	InvalidMultiFunctionReturnType           DiagCode = 100033
	InvalidMultiReturnAccess                 DiagCode = 100034
	InvalidCallExtensionUse                  DiagCode = 100035
	AnnotationDeprecated                     DiagCode = 100036
	TruthyOnlyConditionalValue               DiagCode = 100037
	NotAllowedOptionalAssignment             DiagCode = 100038
	AwaitMustBeInAsyncFunction               DiagCode = 100039
	UnsupportedBuiltinOptionalCall           DiagCode = 100040
	UnsupportedOptionalCompileMembersOnly    DiagCode = 100041
	UndefinedInArrayLiteral                  DiagCode = 100042
	InvalidMethodCallExtensionUse            DiagCode = 100043
	InvalidSpreadInCallExtension             DiagCode = 100044
	CannotAssignToNodeOfKind                 DiagCode = 100045
	IncompleteFieldDecoratorWarning          DiagCode = 100046
	UnsupportedArrayWithLengthConstructor    DiagCode = 100047
	CouldNotResolveRequire                   DiagCode = 100048
	UnsupportedJsxEmit                       DiagCode = 100010
)

// diagHelp maps error codes to help text shown below the diagnostic.
var diagHelp = map[DiagCode]string{
	InvalidAmbientIdentifierName: "Ambient declarations (declare const/function/etc.) emit the identifier as-is into Lua.\n" +
		"Rename to avoid conflicting with Lua reserved words.",
	UndefinedInArrayLiteral: "Lua tables have no concept of 'undefined' slots. A nil in the middle of an array\n" +
		"breaks the length operator (#) and ipairs iteration.",
	ForbiddenForIn: "In Lua, for...in iterates over table keys (like Object.keys in JS).\n" +
		"For arrays, use for...of instead.",
	UnsupportedRightShiftOperator: "Lua 5.3+ only has unsigned right shift (>>). The signed right shift\n" +
		"operator (>>) from TypeScript has no native equivalent. Use `>>>` instead.",
}

// DiagnosticInfo extracts the code and message text from a diagnostic.
func DiagnosticInfo(d *ast.Diagnostic) (code int32, message string) {
	wrapped := diagnosticwriter.WrapASTDiagnostic(d)
	var zeroLocale locale.Locale
	return wrapped.Code(), wrapped.Localize(zeroLocale)
}

// DiagnosticLocation extracts the file name, line, and character from a diagnostic.
// Returns ok=false if the diagnostic has no file location.
func DiagnosticLocation(d *ast.Diagnostic) (file string, line int, character int, ok bool) {
	wrapped := diagnosticwriter.WrapASTDiagnostic(d)
	if wrapped.File() == nil {
		return "", 0, 0, false
	}
	l, c := scanner.GetECMALineAndUTF16CharacterOfPosition(wrapped.File(), wrapped.Pos())
	return string(wrapped.File().FileName()), l, int(c), true
}

// DiagnosticFormat controls how tslua diagnostics are displayed.
type DiagnosticFormat int

const (
	DiagFormatTSTL   DiagnosticFormat = iota // "error TSTL:" (TSTL-compatible, default)
	DiagFormatNative                         // "error TL1001:" (tslua-native codes)
)

// NewErrorForNode creates an error-level diagnostic at the given node's error range.
func NewErrorForNode(sourceFile *ast.SourceFile, node *ast.Node, code DiagCode, message string) *ast.Diagnostic {
	return newForNode(sourceFile, node, diagnostics.CategoryError, code, message)
}

// NewWarningForNode creates a warning-level diagnostic at the given node's error range.
func NewWarningForNode(sourceFile *ast.SourceFile, node *ast.Node, code DiagCode, message string) *ast.Diagnostic {
	return newForNode(sourceFile, node, diagnostics.CategoryWarning, code, message)
}

func newForNode(sourceFile *ast.SourceFile, node *ast.Node, category diagnostics.Category, code DiagCode, message string) *ast.Diagnostic {
	// Use the node's direct span (matching TSTL's node.getStart() + node.getWidth())
	// rather than GetErrorRangeForNode, which remaps to "error ranges" like declaration
	// names — not what we want for custom TSTL diagnostics.
	start := scanner.SkipTrivia(sourceFile.Text(), node.Pos())
	loc := core.NewTextRange(start, node.End())
	msg := newTSTLMessage(category, code, message)
	return ast.NewDiagnostic(sourceFile, loc, msg)
}

// newTSTLMessage creates a diagnostics.Message for tslua diagnostics.
func newTSTLMessage(category diagnostics.Category, code DiagCode, text string) *diagnostics.Message {
	// Mirror diagnostics.Message struct layout to set unexported fields.
	type message struct {
		code                         int32
		category                     diagnostics.Category
		key                          diagnostics.Key
		text                         string
		reportsUnnecessary           bool
		elidedInCompatibilityPyramid bool
		reportsDeprecated            bool
	}
	m := &message{
		code:     int32(code),
		category: category,
		key:      diagnostics.Key("TSTL"),
		text:     text,
	}
	return (*diagnostics.Message)(unsafe.Pointer(m))
}
