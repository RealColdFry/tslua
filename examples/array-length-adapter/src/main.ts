// Exercise user-facing arr.length directly, and indirectly via methods
// that go through lualib (__TS__ArrayPush, __TS__ArrayFilter, etc.)
// which use arr.length internally. After step 4 (lualib rebuild), those
// internal reads will also emit Len(arr); today they still emit #arr
// because lualib comes from the embedded pre-built bundle.

declare function print(...args: unknown[]): void;

const items = [10, 20, 30];

// Direct length read: emits via adapter.
print("length via adapter:", items.length);

// Push uses __TS__ArrayPush internally; that function reads arr.length
// to find the next slot. Today the lualib bundle still emits #arr.
items.push(40, 50);
print("after push:", items.length);

// Filter via lualib: uses arr.length on both input and output arrays.
const evens = items.filter((x) => x % 2 === 0);
print("evens:", evens.length);

// Spread + ...
const copy = [...items];
print("copy:", copy.length);
