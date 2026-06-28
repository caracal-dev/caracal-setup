const fs = require("node:fs");

if (!fs.existsSync("dist")) {
  console.error("Missing frontend dist directory: dist");
  process.exit(1);
}

if (!fs.existsSync("dist/index.html")) {
  console.error("Missing required frontend asset: dist/index.html");
  process.exit(1);
}

if (!fs.existsSync("dist/main.css")) {
  console.error("Missing required frontend asset: dist/main.css");
  process.exit(1);
}

if (!fs.existsSync("dist/main.js")) {
  console.error("Missing required frontend asset: dist/main.js");
  process.exit(1);
}

console.log("Frontend dist is ready.");
