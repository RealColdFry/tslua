package luatest

import "testing"

func TestClassExpression_Named(t *testing.T) {
	t.Parallel()
	ExpectFunction(t, `
		const Foo = class MyClass {
			x: number;
			constructor(x: number) { this.x = x; }
			get() { return this.x; }
		};
		const f = new Foo(42);
		return f.get();
	`, `42`, Opts{})
}

func TestClassExpression_Anonymous(t *testing.T) {
	t.Parallel()
	ExpectFunction(t, `
		const Foo = class {
			value: number;
			constructor(v: number) { this.value = v; }
		};
		return new Foo(10).value;
	`, `10`, Opts{})
}

func TestClassExpression_WithExtends(t *testing.T) {
	t.Parallel()
	ExpectFunction(t, `
		class Base {
			x: number;
			constructor(x: number) { this.x = x; }
		}
		const Derived = class extends Base {
			y: number;
			constructor(x: number, y: number) {
				super(x);
				this.y = y;
			}
		};
		const d = new Derived(1, 2);
		return d.x + d.y;
	`, `3`, Opts{})
}

func TestClassExpression_InlineInCall(t *testing.T) {
	t.Parallel()
	// Class expression used directly in a function argument
	ExpectFunction(t, `
		function create(cls: new (x: number) => { val: number }) {
			return new cls(99);
		}
		const obj = create(class {
			val: number;
			constructor(x: number) { this.val = x; }
		});
		return obj.val;
	`, `99`, Opts{})
}
