const fs = require("node:fs");
const path = require("node:path");

const distDir = path.resolve(__dirname, "..", "dist");
const required = ["index.html", "main.css", "main.js"];

if (!fs.existsSync(distDir)) {
  console.error(`Missing frontend dist directory: ${distDir}`);
  process.exit(1);
}

for (const file of required) {
  const fullPath = path.join(distDir, file);
  if (!fs.existsSync(fullPath)) {
    console.error(`Missing required frontend asset: ${fullPath}`);
    process.exit(1);
  }
}

console.log("Frontend dist is ready.");
