// URL state: compressed JSON in the hash.
//
// Format: #1/<base64url-deflate-raw-json>
// Version 0: { code, target }
// Version 1: { code, tsconfig?: { compilerOptions?, tstl? } }

import { useState, useRef, useEffect, useCallback } from "react";

export interface PlaygroundTsconfig {
  compilerOptions?: Record<string, unknown>;
  tstl?: {
    luaTarget?: string;
    emitMode?: string;
    classStyle?: string;
    noImplicitSelf?: boolean;
    noImplicitGlobalVariables?: boolean;
    trace?: boolean;
  };
}

export interface PlaygroundState {
  code: string;
  tsconfig: PlaygroundTsconfig;
}

const DEBOUNCE_MS = 500;

// --- Compression helpers ---

function toBase64Url(bytes: Uint8Array): string {
  let binary = "";
  for (const b of bytes) binary += String.fromCharCode(b);
  return btoa(binary).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

function fromBase64Url(s: string): Uint8Array {
  const padded = s.replace(/-/g, "+").replace(/_/g, "/") + "==".slice(0, (4 - (s.length % 4)) % 4);
  const binary = atob(padded);
  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i);
  return bytes;
}

async function collectStream(readable: ReadableStream<Uint8Array>): Promise<Uint8Array> {
  const chunks: Uint8Array[] = [];
  const reader = readable.getReader();
  for (;;) {
    const { done, value } = await reader.read();
    if (done) break;
    chunks.push(value);
  }
  const total = chunks.reduce((n, c) => n + c.length, 0);
  const result = new Uint8Array(total);
  let offset = 0;
  for (const c of chunks) {
    result.set(c, offset);
    offset += c.length;
  }
  return result;
}

async function deflate(data: string): Promise<Uint8Array> {
  const cs = new CompressionStream("deflate-raw");
  const writer = cs.writable.getWriter();
  void writer.write(new TextEncoder().encode(data) as unknown as BufferSource);
  void writer.close();
  return collectStream(cs.readable);
}

async function inflate(data: Uint8Array): Promise<string> {
  const ds = new DecompressionStream("deflate-raw");
  const writer = ds.writable.getWriter();
  void writer.write(data as unknown as BufferSource);
  void writer.close();
  return new TextDecoder().decode(await collectStream(ds.readable));
}

// --- Serialization (strip defaults to keep URLs short) ---

function serializeState(state: PlaygroundState): object {
  const out: Record<string, unknown> = { code: state.code };
  const ts = state.tsconfig;
  const tsOut: Record<string, unknown> = {};
  if (ts.compilerOptions && Object.keys(ts.compilerOptions).length > 0) {
    tsOut.compilerOptions = ts.compilerOptions;
  }
  if (ts.tstl) {
    const tstl: Record<string, unknown> = {};
    if (ts.tstl.luaTarget && ts.tstl.luaTarget !== "JIT") tstl.luaTarget = ts.tstl.luaTarget;
    if (ts.tstl.emitMode && ts.tstl.emitMode !== "tstl") tstl.emitMode = ts.tstl.emitMode;
    if (ts.tstl.classStyle) tstl.classStyle = ts.tstl.classStyle;
    if (ts.tstl.noImplicitSelf) tstl.noImplicitSelf = true;
    if (ts.tstl.noImplicitGlobalVariables) tstl.noImplicitGlobalVariables = true;
    if (ts.tstl.trace) tstl.trace = true;
    if (Object.keys(tstl).length > 0) tsOut.tstl = tstl;
  }
  if (Object.keys(tsOut).length > 0) out.tsconfig = tsOut;
  return out;
}

// --- Encode / Decode ---

async function encodeHash(state: PlaygroundState): Promise<string> {
  const compressed = await deflate(JSON.stringify(serializeState(state)));
  return "#1/" + toBase64Url(compressed);
}

async function decodeHash(): Promise<PlaygroundState | null> {
  const hash = window.location.hash;

  // Version 1: { code, tsconfig? }
  if (hash.startsWith("#1/")) {
    try {
      const json = await inflate(fromBase64Url(hash.slice(3)));
      const raw = JSON.parse(json);
      return {
        code: raw.code ?? "",
        tsconfig: raw.tsconfig ?? {},
      };
    } catch {
      return null;
    }
  }

  // Version 0 migration: { code, target } -> { code, tsconfig: { tstl: { luaTarget } } }
  if (hash.startsWith("#0/")) {
    try {
      const json = await inflate(fromBase64Url(hash.slice(3)));
      const raw = JSON.parse(json);
      const tsconfig: PlaygroundTsconfig = {};
      if (raw.target && raw.target !== "JIT") {
        tsconfig.tstl = { luaTarget: raw.target };
      }
      return { code: raw.code ?? "", tsconfig };
    } catch {
      return null;
    }
  }

  return null;
}

// --- Hook ---

export function useHashState(
  defaultState: PlaygroundState,
): [
  PlaygroundState,
  (next: PlaygroundState | ((prev: PlaygroundState) => PlaygroundState)) => void,
  boolean,
] {
  const [state, setState] = useState(defaultState);
  const [ready, setReady] = useState(false);
  const timerRef = useRef<ReturnType<typeof setTimeout>>(null);

  // On mount: read hash -> state
  useEffect(() => {
    decodeHash().then((restored) => {
      if (restored) setState(restored);
      setReady(true);
    });
  }, []);

  // Debounced hash write
  const setStateAndHash = useCallback(
    (next: PlaygroundState | ((prev: PlaygroundState) => PlaygroundState)) => {
      setState((prev) => {
        const val = typeof next === "function" ? next(prev) : next;
        if (timerRef.current) clearTimeout(timerRef.current);
        timerRef.current = setTimeout(() => {
          encodeHash(val).then((hash) => {
            window.history.replaceState(null, "", hash);
          });
        }, DEBOUNCE_MS);
        return val;
      });
    },
    [],
  );

  return [state, setStateAndHash, ready];
}
