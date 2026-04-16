package transpiler

import (
	"fmt"
	"strconv"

	"github.com/microsoft/typescript-go/shim/ast"
	"github.com/realcoldfry/tslua/internal/lua"
)

// transformEnumDeclaration emits a Lua table for a TS enum.
func (t *Transpiler) transformEnumDeclaration(node *ast.Node) []lua.Statement {
	ed := node.AsEnumDeclaration()

	// Const enums are erased
	flags := ast.GetCombinedModifierFlags(node)
	if flags&ast.ModifierFlagsConst != 0 {
		return nil
	}

	comments := t.getLeadingComments(node)

	isExported := hasExportModifier(node)
	origName := ed.Name().AsIdentifier().Text
	name := origName
	if t.hasUnsafeIdentifierName(ed.Name()) {
		name = luaSafeName(origName)
	}

	membersOnly := hasNodeAnnotation(node, t.sourceFile, AnnotCompileMembersOnly)
	if !membersOnly {
		membersOnly = t.hasTypeAnnotation(ed.Name(), AnnotCompileMembersOnly)
	}
	if !membersOnly {
		membersOnly = hasAnnotationInLeadingTrivia(node, t.sourceFile, "compilemembersonly")
	}

	var result []lua.Statement

	// Only emit initialization for the first declaration of this enum (supports enum merging)
	isFirst := t.isFirstDeclaration(node, ed.Name())

	enumExportTarget := "____exports"
	if t.currentNamespace != "" {
		enumExportTarget = t.currentNamespace
	}

	if !membersOnly && isFirst {
		// When the enum is only declared in this file, we use `local Foo = {}`
		// to avoid leaking into global scope. Cross-file enums (same name in 2+
		// files) need the global `Foo = Foo or ({})` pattern for runtime merging.
		crossFile := t.crossFileEnums[origName]
		sameFile := !crossFile && t.allDeclarationsInCurrentFile(ed.Name())

		initExpr := lua.Expression(lua.Table())
		if !sameFile || (isExported && t.exportAsGlobal) {
			var selfRef lua.Expression = lua.Ident(name)
			if isExported && t.currentNamespace != "" {
				selfRef = lua.Index(lua.Ident(enumExportTarget), lua.Str(origName))
			}
			initExpr = lua.Binary(selfRef, lua.OpOr, lua.Table())
		}

		if isExported && t.isExportAsGlobalTopLevel() {
			result = append(result, lua.Assign(
				[]lua.Expression{lua.Ident(name)},
				[]lua.Expression{initExpr},
			))
		} else if isExported {
			result = append(result, lua.Assign(
				[]lua.Expression{lua.Index(lua.Ident(enumExportTarget), lua.Str(origName))},
				[]lua.Expression{initExpr},
			))
		} else if t.isModule || sameFile {
			result = append(result, lua.LocalDecl(
				[]*lua.Identifier{lua.Ident(name)},
				[]lua.Expression{initExpr},
			))
		} else {
			result = append(result, lua.Assign(
				[]lua.Expression{lua.Ident(name)},
				[]lua.Expression{initExpr},
			))
		}
	}

	var enumRef lua.Expression = lua.Ident(name)
	if isExported && !t.isExportAsGlobalTopLevel() {
		enumRef = lua.Index(lua.Ident(enumExportTarget), lua.Str(origName))
	}

	// Emit each member
	if ed.Members != nil {
		for _, member := range ed.Members.Nodes {
			em := member.AsEnumMember()
			memberNameExpr, namePrec := t.enumMemberName(em)
			valueExpr, isString, valuePrec := t.enumMemberValue(node, member)

			result = append(result, namePrec...)
			result = append(result, valuePrec...)

			if membersOnly {
				// @compileMembersOnly: each member becomes its own declaration,
				// so replicate the enum's leading JSDoc onto every member.
				memberStart := len(result)
				// Check if the enum symbol is exported in this scope
				enumExported := isExported
				if !enumExported {
					sym := t.checker.GetSymbolAtLocation(ed.Name())
					if sym != nil {
						enumExported = sym.ExportSymbol != nil
					}
				}
				if enumExported {
					var exportTarget lua.Expression = lua.Ident("____exports")
					if t.currentNamespace != "" {
						exportTarget = lua.Ident(t.currentNamespace)
					}
					result = append(result, lua.Assign(
						[]lua.Expression{lua.Index(exportTarget, memberNameExpr)},
						[]lua.Expression{valueExpr},
					))
				} else if t.isModule {
					// For non-computed names, declare as local; computed names use assignment
					if id, ok := memberNameExpr.(*lua.StringLiteral); ok {
						result = append(result, lua.LocalDecl(
							[]*lua.Identifier{lua.Ident(id.Value)},
							[]lua.Expression{valueExpr},
						))
					} else {
						result = append(result, lua.Assign(
							[]lua.Expression{memberNameExpr},
							[]lua.Expression{valueExpr},
						))
					}
				} else {
					if id, ok := memberNameExpr.(*lua.StringLiteral); ok {
						result = append(result, lua.Assign(
							[]lua.Expression{lua.Ident(id.Value)},
							[]lua.Expression{valueExpr},
						))
					} else {
						result = append(result, lua.Assign(
							[]lua.Expression{memberNameExpr},
							[]lua.Expression{valueExpr},
						))
					}
				}
				if len(comments) > 0 && len(result) > memberStart {
					setLeadingComments(result[memberStart], comments)
				}
			} else {
				result = append(result, lua.Assign(
					[]lua.Expression{lua.Index(enumRef, memberNameExpr)},
					[]lua.Expression{valueExpr},
				))

				if !isString {
					result = append(result, lua.Assign(
						[]lua.Expression{lua.Index(enumRef, lua.Index(enumRef, memberNameExpr))},
						[]lua.Expression{memberNameExpr},
					))
				}
			}
		}
	}

	return result
}

func (t *Transpiler) enumMemberName(em *ast.EnumMember) (lua.Expression, []lua.Statement) {
	nameNode := em.Name()
	switch nameNode.Kind {
	case ast.KindIdentifier:
		return lua.Str(nameNode.AsIdentifier().Text), nil
	case ast.KindStringLiteral:
		return lua.Str(nameNode.AsStringLiteral().Text), nil
	case ast.KindComputedPropertyName:
		expr, stmts := t.transformExprInScope(nameNode.AsComputedPropertyName().Expression)
		return expr, stmts
	default:
		return lua.Str(nameNode.AsIdentifier().Text), nil
	}
}

func (t *Transpiler) enumMemberValue(enumNode *ast.Node, memberNode *ast.Node) (lua.Expression, bool, []lua.Statement) {
	em := memberNode.AsEnumMember()

	constVal := t.checker.GetConstantValue(memberNode)
	if constVal != nil {
		if expr, ok := formatConstantValue(constVal); ok {
			return expr, isStringConstant(constVal), nil
		}
	}

	if em.Initializer != nil {
		// Check if initializer references another member of the same enum
		if em.Initializer.Kind == ast.KindIdentifier {
			sym := t.checker.GetSymbolAtLocation(em.Initializer)
			if sym != nil && sym.ValueDeclaration != nil && sym.ValueDeclaration.Kind == ast.KindEnumMember {
				if sym.ValueDeclaration.Parent == enumNode {
					enumName := enumNode.AsEnumDeclaration().Name().AsIdentifier().Text
					memberName := em.Initializer.AsIdentifier().Text
					return lua.Index(lua.Ident(enumName), lua.Str(memberName)), false, nil
				}
			}
		}
		initExpr, stmts := t.transformExprInScope(em.Initializer)
		isStr := em.Initializer.Kind == ast.KindStringLiteral || em.Initializer.Kind == ast.KindNoSubstitutionTemplateLiteral
		return initExpr, isStr, stmts
	}

	return lua.Nil(), false, nil
}

func (t *Transpiler) tryGetConstEnumValue(node *ast.Node) (lua.Expression, bool) {

	constVal := t.checker.GetConstantValue(node)
	if constVal == nil {
		return nil, false
	}
	return formatConstantValue(constVal)
}

func formatConstantValue(val any) (lua.Expression, bool) {
	switch v := val.(type) {
	case string:
		return lua.Str(v), true
	default:
		f, err := strconv.ParseFloat(fmt.Sprint(v), 64)
		if err != nil {
			return nil, false
		}
		if f == float64(int64(f)) {
			return lua.Num(strconv.FormatInt(int64(f), 10)), true
		}
		return lua.Num(strconv.FormatFloat(f, 'f', -1, 64)), true
	}
}

func isStringConstant(val any) bool {
	_, ok := val.(string)
	return ok
}
