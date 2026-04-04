// Serialization must match Lua serialize() in testhelper_test.go

// Branded object for expect.stringContaining — survives through baking and
// gets serialized as "~contains:X" so the Go test helper can do substring matching.
const STRING_CONTAINING_BRAND = Symbol.for("tslua.stringContaining");

export function stringContaining(s: string): { [key: symbol]: true; value: string } {
  return { [STRING_CONTAINING_BRAND]: true, value: s } as any;
}

export function isStringContaining(v: unknown): v is { value: string } {
  return v != null && typeof v === "object" && (v as any)[STRING_CONTAINING_BRAND] === true;
}

// serializeString matches Lua's string.format("%q") escaping for control chars.
export function serializeString(s: string): string {
  return JSON.stringify(s).replace(/\\u([0-9a-fA-F]{4})/g, (_, hex) => {
    const code = parseInt(hex, 16);
    if (code < 256) return "\\" + code.toString().padStart(3, "0");
    return "\\u" + hex;
  });
}

export function serialize(v: unknown): string {
  if (v === undefined || v === null) return "nil";
  if (typeof v === "boolean") return String(v);
  if (typeof v === "number") {
    if (Number.isNaN(v)) return "NaN";
    if (v === Infinity) return "Infinity";
    if (v === -Infinity) return "-Infinity";
    if (Number.isInteger(v)) return String(v);
    return String(v);
  }
  if (typeof v === "string") return serializeString(v);
  if (isStringContaining(v)) return `~contains:${v.value}`;
  // ExecutionError: only serialize name + message (skip stack, which is platform-dependent)
  if (v instanceof Error && v.name === "ExecutionError") {
    return `{message = ${serializeString(v.message)}, name = "ExecutionError"}`;
  }
  if (Array.isArray(v)) {
    return "{" + v.map(serialize).join(", ") + "}";
  }
  if (typeof v === "object" && v !== null) {
    // Filter out undefined/null values — Lua tables can't represent nil-valued keys
    const keys = Object.keys(v)
      .filter(
        (k) =>
          (v as Record<string, unknown>)[k] !== undefined &&
          (v as Record<string, unknown>)[k] !== null,
      )
      .toSorted();
    return (
      "{" +
      keys.map((k) => `${k} = ${serialize((v as Record<string, unknown>)[k])}`).join(", ") +
      "}"
    );
  }
  return String(v);
}
