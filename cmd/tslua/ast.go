package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/microsoft/typescript-go/shim/ast"
	"github.com/microsoft/typescript-go/shim/bundled"
	"github.com/microsoft/typescript-go/shim/compiler"
	"github.com/microsoft/typescript-go/shim/core"
	"github.com/microsoft/typescript-go/shim/tsoptions"
	"github.com/microsoft/typescript-go/shim/tspath"
	"github.com/microsoft/typescript-go/shim/vfs/cachedvfs"
	"github.com/microsoft/typescript-go/shim/vfs/osvfs"
)

func runAST(source string) error {
	if source == "" {
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("read stdin: %w", err)
		}
		source = string(b)
	}

	tmpDir, err := os.MkdirTemp("", "tslua-ast-")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	mainFile := "main.ts"
	if err := os.WriteFile(filepath.Join(tmpDir, mainFile), []byte(source), 0o644); err != nil {
		return fmt.Errorf("write source: %w", err)
	}

	files := []string{mainFile}
	filesJSON, _ := json.Marshal(files)
	tsconfig := fmt.Sprintf(`{"compilerOptions":{"target":"ESNext","lib":["ESNext"],"strict":true,"skipLibCheck":true,"moduleResolution":"node"},"files":%s}`, filesJSON)
	if err := os.WriteFile(filepath.Join(tmpDir, "tsconfig.json"), []byte(tsconfig), 0o644); err != nil {
		return fmt.Errorf("write tsconfig: %w", err)
	}

	fs := bundled.WrapFS(cachedvfs.From(osvfs.FS()))
	configPath := tspath.ResolvePath("", filepath.Join(tmpDir, "tsconfig.json"))
	configDir := tspath.GetDirectoryPath(configPath)
	host := compiler.NewCompilerHost(configDir, fs, bundled.LibPath(), nil, nil)

	configResult, diags := tsoptions.GetParsedCommandLineOfConfigFile(configPath, &core.CompilerOptions{}, nil, host, nil)
	if len(diags) > 0 {
		return fmt.Errorf("tsconfig parse error: %d diagnostic(s)", len(diags))
	}

	program := compiler.NewProgram(compiler.ProgramOptions{
		Config:         configResult,
		SingleThreaded: core.TSTrue,
		Host:           host,
	})
	program.BindSourceFiles()

	for _, sf := range program.SourceFiles() {
		rel, _ := filepath.Rel(tmpDir, sf.FileName())
		if strings.HasSuffix(rel, "main.ts") {
			printAST(sf.AsNode(), 0)
			return nil
		}
	}

	return fmt.Errorf("source file not found")
}

var showPos bool

func printAST(node *ast.Node, depth int) {
	indent := strings.Repeat("  ", depth)
	kind := node.Kind.String()

	// Strip "Kind" prefix for readability
	kind = strings.TrimPrefix(kind, "Kind")

	// Collect annotations for this node
	var annotations []string

	if showPos {
		annotations = append(annotations, fmt.Sprintf("[%d,%d)", node.Pos(), node.End()))
	}

	switch node.Kind {
	case ast.KindIdentifier:
		annotations = append(annotations, fmt.Sprintf("text=%q", node.AsIdentifier().Text))
	case ast.KindStringLiteral:
		annotations = append(annotations, fmt.Sprintf("text=%q", node.AsStringLiteral().Text))
	case ast.KindNumericLiteral:
		annotations = append(annotations, fmt.Sprintf("text=%q", node.AsNumericLiteral().Text))
	case ast.KindPropertyAccessExpression:
		pa := node.AsPropertyAccessExpression()
		if pa.Name() != nil && pa.Name().Kind == ast.KindIdentifier {
			annotations = append(annotations, fmt.Sprintf("name=%q", pa.Name().AsIdentifier().Text))
		}
	case ast.KindVariableDeclaration:
		vd := node.AsVariableDeclaration()
		if vd.Name().Kind == ast.KindIdentifier {
			annotations = append(annotations, fmt.Sprintf("name=%q", vd.Name().AsIdentifier().Text))
		}
	case ast.KindParameter:
		p := node.AsParameterDeclaration()
		if p.Name().Kind == ast.KindIdentifier {
			annotations = append(annotations, fmt.Sprintf("name=%q", p.Name().AsIdentifier().Text))
		}
		if p.DotDotDotToken != nil {
			annotations = append(annotations, "rest")
		}
	case ast.KindFunctionDeclaration:
		fd := node.AsFunctionDeclaration()
		if fd.Name() != nil {
			annotations = append(annotations, fmt.Sprintf("name=%q", fd.Name().AsIdentifier().Text))
		}
	case ast.KindVariableDeclarationList:
		vdl := node.AsVariableDeclarationList()
		flags := vdl.Flags
		if flags&ast.NodeFlagsLet != 0 {
			annotations = append(annotations, "let")
		} else if flags&ast.NodeFlagsConst != 0 {
			annotations = append(annotations, "const")
		} else {
			annotations = append(annotations, "var")
		}
	}

	line := indent + kind
	if len(annotations) > 0 {
		line += "  " + strings.Join(annotations, " ")
	}
	fmt.Println(line)

	node.ForEachChild(func(child *ast.Node) bool {
		printAST(child, depth+1)
		return false
	})
}
