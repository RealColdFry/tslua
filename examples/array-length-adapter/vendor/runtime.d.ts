// Runtime-adapter declaration for a hypothetical embedded host that
// exposes proxy arrays with host-tracked length via a free function `Len`.
// tslua reads the @luaArrayRuntime JSDoc tag and routes all `arr.length`
// emits through `Len(arr)` instead of the default `#arr`.

declare function Len(arr: readonly unknown[]): number;

/** @luaArrayRuntime */
declare const HostArrayRuntime: {
    length: typeof Len;
};
