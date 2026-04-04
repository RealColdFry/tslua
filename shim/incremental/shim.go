// Package incremental provides a shim for typescript-go's internal execute/incremental package.
package incremental

import (
	"context"
	_ "unsafe"

	"github.com/microsoft/typescript-go/internal/ast"
	"github.com/microsoft/typescript-go/internal/compiler"
	"github.com/microsoft/typescript-go/internal/execute/incremental"
	"github.com/microsoft/typescript-go/internal/tsoptions"
)

// Program wraps a compiler.Program with incremental state for watch mode.
type Program = incremental.Program

// Host provides mtime tracking for incremental builds.
type Host = incremental.Host

// BuildInfoReader reads .tsbuildinfo files.
type BuildInfoReader = incremental.BuildInfoReader

//go:linkname NewProgram github.com/microsoft/typescript-go/internal/execute/incremental.NewProgram
func NewProgram(program *compiler.Program, oldProgram *incremental.Program, host incremental.Host, testing bool) *incremental.Program

//go:linkname CreateHost github.com/microsoft/typescript-go/internal/execute/incremental.CreateHost
func CreateHost(compilerHost compiler.CompilerHost) incremental.Host

//go:linkname NewBuildInfoReader github.com/microsoft/typescript-go/internal/execute/incremental.NewBuildInfoReader
func NewBuildInfoReader(host compiler.CompilerHost) incremental.BuildInfoReader

//go:linkname ReadBuildInfoProgram github.com/microsoft/typescript-go/internal/execute/incremental.ReadBuildInfoProgram
func ReadBuildInfoProgram(config *tsoptions.ParsedCommandLine, reader incremental.BuildInfoReader, host compiler.CompilerHost) *incremental.Program

//go:linkname Program_GetProgram github.com/microsoft/typescript-go/internal/execute/incremental.(*Program).GetProgram
func Program_GetProgram(recv *incremental.Program) *compiler.Program

//go:linkname Program_GetSemanticDiagnostics github.com/microsoft/typescript-go/internal/execute/incremental.(*Program).GetSemanticDiagnostics
func Program_GetSemanticDiagnostics(recv *incremental.Program, ctx context.Context, file *ast.SourceFile) []*ast.Diagnostic

//go:linkname Program_GetSourceFiles github.com/microsoft/typescript-go/internal/execute/incremental.(*Program).GetSourceFiles
func Program_GetSourceFiles(recv *incremental.Program) []*ast.SourceFile
