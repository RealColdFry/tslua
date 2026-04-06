package lua

import (
	"testing"
)

func TestPrintLiterals(t *testing.T) {
	tests := []struct {
		name string
		expr Expression
		want string
	}{
		{"nil", Nil(), "nil"},
		{"true", Bool(true), "true"},
		{"false", Bool(false), "false"},
		{"number", Num("42"), "42"},
		{"string", Str("hello"), `"hello"`},
		{"identifier", Ident("foo"), "foo"},
		{"dots", Dots(), "..."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := PrintExpression(tt.expr)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPrintBinaryPrecedence(t *testing.T) {
	tests := []struct {
		name string
		expr Expression
		want string
	}{
		{
			"add no parens",
			Binary(Ident("a"), OpAdd, Ident("b")),
			"a + b",
		},
		{
			"mul higher than add",
			Binary(Ident("a"), OpAdd, Binary(Ident("b"), OpMul, Ident("c"))),
			"a + b * c",
		},
		{
			"add needs parens in mul",
			Binary(Binary(Ident("a"), OpAdd, Ident("b")), OpMul, Ident("c")),
			"(a + b) * c",
		},
		{
			"concat right-assoc no parens on right",
			Binary(Ident("a"), OpConcat, Binary(Ident("b"), OpConcat, Ident("c"))),
			"a .. b .. c",
		},
		{
			"concat needs parens on left grouping",
			Binary(Binary(Ident("a"), OpConcat, Ident("b")), OpConcat, Ident("c")),
			"(a .. b) .. c",
		},
		{
			"and/or precedence",
			Binary(Binary(Ident("a"), OpAnd, Ident("b")), OpOr, Ident("c")),
			"a and b or c",
		},
		{
			"or needs parens in and",
			Binary(Binary(Ident("a"), OpOr, Ident("b")), OpAnd, Ident("c")),
			"(a or b) and c",
		},
		{
			"not operator",
			Unary(OpNot, Ident("x")),
			"not x",
		},
		{
			"length operator",
			Unary(OpLen, Ident("arr")),
			"#arr",
		},
		{
			"negation",
			Unary(OpNeg, Ident("x")),
			"-x",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := PrintExpression(tt.expr)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPrintCalls(t *testing.T) {
	tests := []struct {
		name string
		expr Expression
		want string
	}{
		{
			"simple call",
			Call(Ident("print"), Str("hello")),
			`print("hello")`,
		},
		{
			"method call",
			MethodCall(Ident("obj"), "method", Ident("a"), Ident("b")),
			"obj:method(a, b)",
		},
		{
			"no args",
			Call(Ident("foo")),
			"foo()",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := PrintExpression(tt.expr)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPrintTableSimple(t *testing.T) {
	// {1, 2, 3} — simple, ≤4 elements
	tbl := Table(Field(Num("1")), Field(Num("2")), Field(Num("3")))
	got := PrintExpression(tbl)
	want := "{1, 2, 3}"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestPrintTableKeyed(t *testing.T) {
	// {x = 1, y = 2} — simple, ≤4 elements
	tbl := Table(
		KeyField(Ident("x"), Num("1")),
		KeyField(Ident("y"), Num("2")),
	)
	got := PrintExpression(tbl)
	want := "{x = 1, y = 2}"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestPrintTableMultiline(t *testing.T) {
	// 5+ fields → multiline
	tbl := Table(
		KeyField(Ident("a"), Num("1")),
		KeyField(Ident("b"), Num("2")),
		KeyField(Ident("c"), Num("3")),
		KeyField(Ident("d"), Num("4")),
		KeyField(Ident("e"), Num("5")),
	)
	got := PrintExpression(tbl)
	want := "{\n    a = 1,\n    b = 2,\n    c = 3,\n    d = 4,\n    e = 5\n}"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestPrintTableWithCalls(t *testing.T) {
	// Table with call → multiline even with ≤4 elements
	tbl := Table(
		KeyField(Ident("x"), Call(Ident("foo"))),
		KeyField(Ident("y"), Num("2")),
	)
	got := PrintExpression(tbl)
	want := "{\n    x = foo(),\n    y = 2\n}"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestPrintStatements(t *testing.T) {
	stmts := []Statement{
		LocalDecl([]*Identifier{Ident("x")}, []Expression{Num("1")}),
		ExprStmt(Call(Ident("print"), Ident("x"))),
		Return(Ident("x")),
	}
	got := PrintStatements(stmts, false)
	want := "local x = 1\nprint(x)\nreturn x\n"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestPrintIfElseIf(t *testing.T) {
	stmt := &IfStatement{
		Condition: Binary(Ident("x"), OpGt, Num("0")),
		IfBlock:   &Block{Statements: []Statement{ExprStmt(Call(Ident("print"), Str("positive")))}},
		ElseBlock: &IfStatement{
			Condition: Binary(Ident("x"), OpLt, Num("0")),
			IfBlock:   &Block{Statements: []Statement{ExprStmt(Call(Ident("print"), Str("negative")))}},
			ElseBlock: &Block{Statements: []Statement{ExprStmt(Call(Ident("print"), Str("zero")))}},
		},
	}
	got := PrintStatements([]Statement{stmt}, false)
	want := `if x > 0 then
    print("positive")
elseif x < 0 then
    print("negative")
else
    print("zero")
end
`
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestPrintTableIndex(t *testing.T) {
	tests := []struct {
		name string
		expr Expression
		want string
	}{
		{
			"dot access",
			Index(Ident("obj"), Str("name")),
			"obj.name",
		},
		{
			"bracket access",
			Index(Ident("arr"), Num("1")),
			"arr[1]",
		},
		{
			"bracket string non-ident",
			Index(Ident("obj"), Str("foo-bar")),
			`obj["foo-bar"]`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := PrintExpression(tt.expr)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPrintFunctionExpression(t *testing.T) {
	fe := &FunctionExpression{
		Params: []*Identifier{Ident("self"), Ident("x")},
		Body: &Block{Statements: []Statement{
			Return(Binary(Ident("x"), OpAdd, Num("1"))),
		}},
	}
	got := PrintExpression(fe)
	want := "function(self, x)\n    return x + 1\nend"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestPrintFunctionDefinition(t *testing.T) {
	fe := &FunctionExpression{
		Params: []*Identifier{Ident("self"), Ident("x")},
		Body: &Block{Statements: []Statement{
			Return(Binary(Ident("x"), OpMul, Num("2"))),
		}},
		Flags: FlagDeclaration,
	}
	stmt := LocalDecl([]*Identifier{Ident("double")}, []Expression{fe})
	got := PrintStatements([]Statement{stmt}, false)
	want := "local function double(self, x)\n    return x * 2\nend\n"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}
