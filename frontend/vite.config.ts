import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// In dev, proxy the API routes to the Go backend (default :8080) so the
// frontend can call same-origin paths and SSE streams through untouched.
const API = process.env.VITE_API_TARGET || "http://localhost:8080";

export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      "/optimize": { target: API, changeOrigin: true },
      "/recheck": { target: API, changeOrigin: true },
      "/explain": { target: API, changeOrigin: true },
      "/health": { target: API, changeOrigin: true },
    },
  },
});
