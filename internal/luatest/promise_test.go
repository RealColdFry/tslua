package luatest

import "testing"

func TestPromise_FinallyReturnsNewPromise(t *testing.T) {
	t.Parallel()
	ExpectFunction(t, `
		const p1 = new Promise(() => {});
		const p2 = p1.finally();
		return p1 === p2;`,
		"false", Opts{})
}
