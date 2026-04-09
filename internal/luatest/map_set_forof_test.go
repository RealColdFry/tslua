package luatest

import (
	"testing"

	"github.com/realcoldfry/tslua/internal/transpiler"
)

func TestOptimizedMapForOf(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
		want string
	}{
		{
			"for-of map entries",
			`const m = new Map<string, number>();
			m.set("a", 1);
			m.set("b", 2);
			let result = "";
			for (const [k, v] of m) {
				result += k + v;
			}
			return result;`,
			`"a1b2"`,
		},
		{
			"for-of map.entries()",
			`const m = new Map<string, number>();
			m.set("x", 10);
			m.set("y", 20);
			let result = "";
			for (const [k, v] of m.entries()) {
				result += k + v;
			}
			return result;`,
			`"x10y20"`,
		},
		{
			"for-of map.keys()",
			`const m = new Map<string, number>();
			m.set("a", 1);
			m.set("b", 2);
			const keys: string[] = [];
			for (const k of m.keys()) {
				keys.push(k);
			}
			return keys.join(",");`,
			`"a,b"`,
		},
		{
			"for-of map.values()",
			`const m = new Map<string, number>();
			m.set("a", 1);
			m.set("b", 2);
			let sum = 0;
			for (const v of m.values()) {
				sum += v;
			}
			return sum;`,
			`3`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ExpectFunction(t, tc.body, tc.want, Opts{})
		})
	}
}

func TestOptimizedSetForOf(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
		want string
	}{
		{
			"for-of set",
			`const s = new Set<number>();
			s.add(1);
			s.add(2);
			s.add(3);
			let sum = 0;
			for (const v of s) {
				sum += v;
			}
			return sum;`,
			`6`,
		},
		{
			"for-of set.values()",
			`const s = new Set<string>();
			s.add("a");
			s.add("b");
			const vals: string[] = [];
			for (const v of s.values()) {
				vals.push(v);
			}
			return vals.join(",");`,
			`"a,b"`,
		},
		{
			"for-of set.keys()",
			`const s = new Set<number>();
			s.add(10);
			s.add(20);
			let sum = 0;
			for (const k of s.keys()) {
				sum += k;
			}
			return sum;`,
			`30`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ExpectFunction(t, tc.body, tc.want, Opts{})
		})
	}
}

func TestOptimizedArrayEntriesForOf(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
		want string
	}{
		{
			"for-of array.entries() with index and value",
			`const arr = ["a", "b", "c"];
			let result = "";
			for (const [i, v] of arr.entries()) {
				result += i + ":" + v + " ";
			}
			return result.trim();`,
			`"0:a 1:b 2:c"`,
		},
		{
			"for-of array.entries() index is 0-based",
			`const arr = [10, 20, 30];
			let sum = 0;
			for (const [i, v] of arr.entries()) {
				sum += i;
			}
			return sum;`,
			`3`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ExpectFunction(t, tc.body, tc.want, Opts{})
		})
	}
}

// Optimized emit mode tests - these hit the tryOptimizedMapSetForOf
// and tryOptimizedArrayEntriesForOf code paths.

var optimizedOpts = Opts{EmitMode: transpiler.EmitModeOptimized}

func TestOptimizedEmit_MapForOf(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
		want string
	}{
		{
			"for-of map entries",
			`const m = new Map<string, number>();
			m.set("a", 1);
			m.set("b", 2);
			let result = "";
			for (const [k, v] of m) {
				result += k + v;
			}
			return result;`,
			`"a1b2"`,
		},
		{
			"for-of map.entries()",
			`const m = new Map<string, number>();
			m.set("x", 10);
			m.set("y", 20);
			let result = "";
			for (const [k, v] of m.entries()) {
				result += k + v;
			}
			return result;`,
			`"x10y20"`,
		},
		{
			"for-of map.keys()",
			`const m = new Map<string, number>();
			m.set("a", 1);
			m.set("b", 2);
			const keys: string[] = [];
			for (const k of m.keys()) {
				keys.push(k);
			}
			return keys.join(",");`,
			`"a,b"`,
		},
		{
			"for-of map.values()",
			`const m = new Map<string, number>();
			m.set("a", 1);
			m.set("b", 2);
			let sum = 0;
			for (const v of m.values()) {
				sum += v;
			}
			return sum;`,
			`3`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ExpectFunction(t, tc.body, tc.want, optimizedOpts)
		})
	}
}

func TestOptimizedEmit_SetForOf(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
		want string
	}{
		{
			"for-of set",
			`const s = new Set<number>();
			s.add(1);
			s.add(2);
			s.add(3);
			let sum = 0;
			for (const v of s) {
				sum += v;
			}
			return sum;`,
			`6`,
		},
		{
			"for-of set.values()",
			`const s = new Set<string>();
			s.add("a");
			s.add("b");
			const vals: string[] = [];
			for (const v of s.values()) {
				vals.push(v);
			}
			return vals.join(",");`,
			`"a,b"`,
		},
		{
			"for-of set.keys()",
			`const s = new Set<number>();
			s.add(10);
			s.add(20);
			let sum = 0;
			for (const k of s.keys()) {
				sum += k;
			}
			return sum;`,
			`30`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ExpectFunction(t, tc.body, tc.want, optimizedOpts)
		})
	}
}

func TestOptimizedEmit_ArrayEntriesForOf(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
		want string
	}{
		{
			"for-of array.entries() with index and value",
			`const arr = ["a", "b", "c"];
			let result = "";
			for (const [i, v] of arr.entries()) {
				result += i + ":" + v + " ";
			}
			return result.trim();`,
			`"0:a 1:b 2:c"`,
		},
		{
			"for-of array.entries() index is 0-based",
			`const arr = [10, 20, 30];
			let sum = 0;
			for (const [i, v] of arr.entries()) {
				sum += i;
			}
			return sum;`,
			`3`,
		},
		{
			"for-of array.entries() value only (unused index)",
			`const arr = [10, 20, 30];
			let sum = 0;
			for (const [, v] of arr.entries()) {
				sum += v;
			}
			return sum;`,
			`60`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ExpectFunction(t, tc.body, tc.want, optimizedOpts)
		})
	}
}
