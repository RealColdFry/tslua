// URL state: compressed JSON in the hash.
//
// Format: #0/<base64url-deflate-raw-json>
// "0" is a version prefix for future-proofing.

import { useState, useRef, useEffect, useCallback } from "react";

export interface PlaygroundState {
  code: string;
  target: string;
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

// --- Encode / Decode ---

async function encodeHash(state: PlaygroundState): Promise<string> {
  const compressed = await deflate(JSON.stringify(state));
  return "#0/" + toBase64Url(compressed);
}

async function decodeHash(): Promise<PlaygroundState | null> {
  const hash = window.location.hash;
  if (!hash.startsWith("#0/")) return null;
  try {
    const json = await inflate(fromBase64Url(hash.slice(3)));
    return JSON.parse(json) as PlaygroundState;
  } catch {
    return null;
  }
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

  // On mount: read hash → state
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
