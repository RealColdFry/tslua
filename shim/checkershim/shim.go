// Package checkershim provides tslua-specific shims for checker internals
// not exposed by tsgolint's generated checker shim.
package checkershim

import (
	"unsafe"

	"github.com/microsoft/typescript-go/internal/ast"
	"github.com/microsoft/typescript-go/internal/checker"
)

type extra_Signature struct {
	flags                    checker.SignatureFlags
	minArgumentCount         int32
	resolvedMinArgumentCount int32
	declaration              *ast.Node
	typeParameters           []*checker.Type
	parameters               []*ast.Symbol
	thisParameter            *ast.Symbol
	resolvedReturnType       *checker.Type
	resolvedTypePredicate    *checker.TypePredicate
	target                   *checker.Signature
	mapper                   *checker.TypeMapper
	isolatedSignatureType    *checker.Type
	composite                *checker.CompositeSignature
}

func Signature_composite(v *checker.Signature) *checker.CompositeSignature {
	return ((*extra_Signature)(unsafe.Pointer(v))).composite
}

type extra_CompositeSignature struct {
	isUnion    bool
	signatures []*checker.Signature
}

func CompositeSignature_signatures(v *checker.CompositeSignature) []*checker.Signature {
	return ((*extra_CompositeSignature)(unsafe.Pointer(v))).signatures
}
