import { defineConfig } from "vite";
import vue from "@vitejs/plugin-vue";

// 产物交给 Go 的 //go:embed 打包（cmd/pk-desktop 里 embed frontend/dist）
export default defineConfig({
  plugins: [vue()],
  build: { outDir: "dist", emptyOutDir: true, assetsInlineLimit: 0 },
});
