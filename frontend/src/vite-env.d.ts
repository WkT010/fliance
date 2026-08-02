/// <reference types="vite/client" />

// Vite exposes build-time env vars prefixed with VITE_ via import.meta.env.
// This reference adds the types so TypeScript knows about import.meta.env.
interface ImportMetaEnv {
  readonly VITE_API_BASE?: string;
  readonly VITE_WS_URL?: string;
}
interface ImportMeta {
  readonly env: ImportMetaEnv;
}
