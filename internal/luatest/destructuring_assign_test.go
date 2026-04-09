package luatest

import "testing"

func TestDestructuringAssignment_Array(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
		want string
	}{
		{
			"simple array destructuring assignment",
			`let a: number, b: number;
			[a, b] = [10, 20];
			return a + b;`,
			`30`,
		},
		{
			"swap via destructuring",
			`let a = 1, b = 2;
			[a, b] = [b, a];
			return a * 10 + b;`,
			`21`,
		},
		{
			"destructuring with skip",
			`let a: number, c: number;
			[a, , c] = [1, 2, 3];
			return a + c;`,
			`4`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ExpectFunction(t, tc.body, tc.want, Opts{})
		})
	}
}

func TestDestructuringAssignment_Object(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
		want string
	}{
		{
			"simple object destructuring assignment",
			`let a: number, b: number;
			({ a, b } = { a: 5, b: 10 });
			return a + b;`,
			`15`,
		},
		{
			"object destructuring with rename",
			`let x: number;
			({ a: x } = { a: 42 });
			return x;`,
			`42`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ExpectFunction(t, tc.body, tc.want, Opts{})
		})
	}
}

func TestDestructuringAssignment_PropertyAccess(t *testing.T) {
	t.Parallel()
	// Tests transformAssignmentLHSExpression with property access targets
	ExpectFunction(t, `
		const obj: { x: number; y: number } = { x: 0, y: 0 };
		[obj.x, obj.y] = [100, 200];
		return obj.x + obj.y;
	`, `300`, Opts{})
}

func TestDestructuringAssignment_ElementAccess(t *testing.T) {
	t.Parallel()
	// Tests transformAssignmentLHSExpression with element access targets
	ExpectFunction(t, `
		const arr = [0, 0, 0];
		[arr[0], arr[2]] = [10, 30];
		return arr[0] + arr[2];
	`, `40`, Opts{})
}

func TestDestructuringAssignment_ObjectToPropertyAccess(t *testing.T) {
	t.Parallel()
	// This hits emitAssignToTarget via object destructuring with non-simple LHS
	ExpectFunction(t, `
		const target = { x: 0, y: 0 };
		({ x: target.x, y: target.y } = { x: 5, y: 10 });
		return target.x + target.y;
	`, `15`, Opts{})
}

func TestDestructuringAssignment_ObjectToElementAccess(t *testing.T) {
	t.Parallel()
	ExpectFunction(t, `
		const arr = [0, 0];
		({ a: arr[0], b: arr[1] } = { a: 7, b: 8 });
		return arr[0] + arr[1];
	`, `15`, Opts{})
}

func TestDestructuringAssignment_ArrayLength(t *testing.T) {
	t.Parallel()
	// Tests emitAssignToTarget with array.length
	ExpectFunction(t, `
		const arr = [1, 2, 3, 4, 5];
		arr.length = 3;
		return arr.length;
	`, `3`, Opts{})
}
