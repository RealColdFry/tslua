package transpiler

import (
	"strings"

	"github.com/microsoft/typescript-go/shim/ast"
)

type AnnotationKind int

const (
	AnnotNoSelf AnnotationKind = iota
	AnnotNoSelfInFile
	AnnotCompileMembersOnly
)

var annotationValues = map[string]AnnotationKind{
	"noself":             AnnotNoSelf,
	"noselfinfile":       AnnotNoSelfInFile,
	"compilemembersonly": AnnotCompileMembersOnly,
}

func hasNodeAnnotation(node *ast.Node, sf *ast.SourceFile, kind AnnotationKind) bool {
	if node == nil || node.Flags&ast.NodeFlagsHasJSDoc == 0 {
		return false
	}
	for _, jsDoc := range node.JSDoc(sf) {
		if jsDoc.Kind != ast.KindJSDoc {
			continue
		}
		tags := jsDoc.AsJSDoc().Tags
		if tags == nil {
			continue
		}
		for _, tag := range tags.Nodes {
			if tagKind, ok := jsDocTagAnnotation(tag); ok && tagKind == kind {
				return true
			}
		}
	}
	return false
}

func hasFileAnnotation(sf *ast.SourceFile, kind AnnotationKind) bool {
	if sf.Statements == nil || len(sf.Statements.Nodes) == 0 {
		return false
	}
	return hasNodeAnnotation(sf.Statements.Nodes[0], sf, kind)
}

func jsDocTagAnnotation(tag *ast.Node) (AnnotationKind, bool) {
	if !ast.IsJSDocUnknownTag(tag) {
		return 0, false
	}
	tagName := strings.ToLower(tag.AsJSDocUnknownTag().TagName.AsIdentifier().Text)
	kind, ok := annotationValues[tagName]
	return kind, ok
}

// hasTypeAnnotation checks if the type at a node has a given annotation via its symbol's declaration.
func (t *Transpiler) hasTypeAnnotation(node *ast.Node, kind AnnotationKind) bool {
	if t.checker == nil {
		return false
	}
	typ := t.checker.GetTypeAtLocation(node)
	if typ == nil {
		return false
	}
	sym := typ.Symbol()
	if sym == nil {
		return false
	}
	annotName := ""
	for k, v := range annotationValues {
		if v == kind {
			annotName = k
			break
		}
	}
	for _, decl := range sym.Declarations {
		sf := ast.GetSourceFileOfNode(decl)
		if hasNodeAnnotation(decl, sf, kind) {
			return true
		}
		if annotName != "" && hasAnnotationInLeadingTrivia(decl, sf, annotName) {
			return true
		}
	}
	return false
}

// hasAnnotationInLeadingTrivia scans the leading trivia of a node for a JSDoc @annotation.
// In tsgo, Pos() includes leading trivia (comments, whitespace) before the actual token.
// This scans between Pos() and the node's first child (or End()) to find /** ... */ blocks.
func hasAnnotationInLeadingTrivia(node *ast.Node, sf *ast.SourceFile, annotName string) bool {
	sourceText := sf.Text()
	pos := node.Pos()
	if pos < 0 || pos >= len(sourceText) {
		return false
	}
	// The leading trivia region ends at the first child's Pos() or, if no children,
	// we approximate by finding the first non-trivia character after any comments.
	// This prevents scanning past the node into subsequent declarations.
	endPos := node.End()
	node.ForEachChild(func(child *ast.Node) bool {
		endPos = child.Pos()
		return true // stop at first child
	})
	if endPos <= pos || endPos > len(sourceText) {
		return false
	}
	region := sourceText[pos:endPos]
	idx := strings.Index(region, "/**")
	if idx < 0 {
		return false
	}
	endIdx := strings.Index(region[idx:], "*/")
	if endIdx < 0 {
		return false
	}
	comment := strings.ToLower(region[idx : idx+endIdx+2])
	target := "@" + annotName
	i := strings.Index(comment, target)
	if i < 0 {
		return false
	}
	// Ensure we matched the exact annotation, not a prefix (e.g. @noself vs @noselfinfile)
	end := i + len(target)
	if end < len(comment) && comment[end] >= 'a' && comment[end] <= 'z' {
		return false
	}
	return true
}

// getAnnotationArg extracts the first text argument from a JSDoc @annotation tag.
// Used for @customName, @customConstructor, etc.
func (t *Transpiler) getAnnotationArg(node *ast.Node, annotName string) string {
	sf := ast.GetSourceFileOfNode(node)
	if sf == nil {
		sf = t.sourceFile
	}
	if node.Flags&ast.NodeFlagsHasJSDoc == 0 {
		return ""
	}
	// Try the structured JSDoc API first
	docs := node.JSDoc(sf)
	for _, jsDoc := range docs {
		if jsDoc.Kind != ast.KindJSDoc {
			continue
		}
		tags := jsDoc.AsJSDoc().Tags
		if tags == nil {
			continue
		}
		for _, tag := range tags.Nodes {
			if !ast.IsJSDocUnknownTag(tag) {
				continue
			}
			tagName := strings.ToLower(tag.AsJSDocUnknownTag().TagName.AsIdentifier().Text)
			if tagName == annotName {
				comment := tag.AsJSDocUnknownTag().Comment
				if comment != nil {
					for _, c := range comment.Nodes {
						text := c.Text()
						text = strings.TrimSpace(text)
						text = strings.TrimRight(text, "*")
						text = strings.TrimSpace(text)
						// Take first word only (multi-line JSDoc may include continuation text)
						if idx := strings.IndexAny(text, " \t\n\r"); idx >= 0 {
							text = text[:idx]
						}
						if text != "" {
							return text
						}
					}
				}
			}
		}
	}
	// Fallback: tsgo's lazy JSDoc parsing may not resolve for nested nodes.
	// Extract from leading trivia directly.
	return getAnnotationArgFromTrivia(node, sf, annotName)
}

// getAnnotationArgFromTrivia extracts an annotation argument by scanning the leading trivia
// of a node. This handles cases where tsgo's JSDoc() doesn't return parsed results
// (e.g., function declarations inside namespace blocks).
func getAnnotationArgFromTrivia(node *ast.Node, sf *ast.SourceFile, annotName string) string {
	sourceText := sf.Text()
	pos := node.Pos()
	if pos < 0 || pos >= len(sourceText) {
		return ""
	}
	endPos := node.End()
	node.ForEachChild(func(child *ast.Node) bool {
		endPos = child.Pos()
		return true
	})
	if endPos <= pos || endPos > len(sourceText) {
		return ""
	}
	region := sourceText[pos:endPos]
	idx := strings.Index(region, "/**")
	if idx < 0 {
		return ""
	}
	closeIdx := strings.Index(region[idx:], "*/")
	if closeIdx < 0 {
		return ""
	}
	comment := region[idx : idx+closeIdx+2]
	target := "@" + annotName
	lower := strings.ToLower(comment)
	i := strings.Index(lower, target)
	if i < 0 {
		return ""
	}
	// Ensure exact match (not prefix like @noself vs @noselfinfile)
	end := i + len(target)
	if end < len(lower) && lower[end] >= 'a' && lower[end] <= 'z' {
		return ""
	}
	// Extract the first word after the annotation tag.
	// The region after the tag may contain trailing comment syntax (* and */)
	// and continuation lines — take only the first whitespace-delimited word.
	after := comment[end:]
	// Strip leading whitespace to get to the argument
	after = strings.TrimLeft(after, " \t")
	// Take first word (stop at whitespace, newline, or *)
	if endIdx := strings.IndexAny(after, " \t\n\r*"); endIdx >= 0 {
		after = after[:endIdx]
	}
	return strings.TrimSpace(after)
}

// getCustomName returns the @customName value for a node, or "" if not present.
func (t *Transpiler) getCustomName(node *ast.Node) string {
	return t.getAnnotationArg(node, "customname")
}

// getCustomNameFromSymbol returns the @customName annotation from a symbol's declaration.
// For import specifiers without a property name (i.e. not renamed), it follows through
// to the imported symbol's declaration.
func (t *Transpiler) getCustomNameFromSymbol(sym *ast.Symbol) string {
	if sym == nil || t.checker == nil {
		return ""
	}
	for _, decl := range sym.Declarations {
		if name := t.getAnnotationArg(decl, "customname"); name != "" {
			return name
		}
		// Follow through imports to the original declaration
		if decl.Kind == ast.KindImportSpecifier && decl.AsImportSpecifier().PropertyName == nil {
			importedType := t.checker.GetTypeAtLocation(decl)
			if importedType != nil && importedType.Symbol() != nil {
				if name := t.getCustomNameFromSymbol(importedType.Symbol()); name != "" {
					return name
				}
			}
		}
	}
	return ""
}

// hasTypeAnnotationTag reports whether the type's declaration has an @annotName JSDoc tag,
// regardless of whether it has arguments.
func (t *Transpiler) hasTypeAnnotationTag(node *ast.Node, annotName string) bool {
	if t.checker == nil {
		return false
	}
	typ := t.checker.GetTypeAtLocation(node)
	if typ == nil {
		return false
	}
	sym := typ.Symbol()
	if sym == nil {
		return false
	}
	for _, decl := range sym.Declarations {
		if t.hasAnnotationTag(decl, annotName) {
			return true
		}
	}
	return false
}

// hasAnnotationTag reports whether node has an @annotName JSDoc tag (regardless of arguments).
func (t *Transpiler) hasAnnotationTag(node *ast.Node, annotName string) bool {
	if node.Flags&ast.NodeFlagsHasJSDoc == 0 {
		return false
	}
	sf := ast.GetSourceFileOfNode(node)
	if sf == nil {
		sf = t.sourceFile
	}
	for _, jsDoc := range node.JSDoc(sf) {
		if jsDoc.Kind != ast.KindJSDoc {
			continue
		}
		tags := jsDoc.AsJSDoc().Tags
		if tags == nil {
			continue
		}
		for _, tag := range tags.Nodes {
			if !ast.IsJSDocUnknownTag(tag) {
				continue
			}
			tagName := strings.ToLower(tag.AsJSDocUnknownTag().TagName.AsIdentifier().Text)
			if tagName == annotName {
				return true
			}
		}
	}
	return false
}

// getTypeAnnotationArg extracts an annotation argument from the type's declaration.
// Used for type-level annotations like @customConstructor on a class.
func (t *Transpiler) getTypeAnnotationArg(node *ast.Node, annotName string) string {
	if t.checker == nil {
		return ""
	}
	typ := t.checker.GetTypeAtLocation(node)
	if typ == nil {
		return ""
	}
	sym := typ.Symbol()
	if sym == nil {
		return ""
	}
	for _, decl := range sym.Declarations {
		if result := t.getAnnotationArg(decl, annotName); result != "" {
			return result
		}
	}
	return ""
}
