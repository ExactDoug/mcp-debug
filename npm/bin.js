#!/usr/bin/env node

const { spawn } = require("child_process");
const os = require("os");
const path = require("path");

function getBinaryPath() {
  const suffix = os.platform() === "win32" ? ".exe" : "";
  return path.join(__dirname, `mcp-debug${suffix}`);
}

const binaryPath = getBinaryPath();
const args = process.argv.slice(2);

const child = spawn(binaryPath, args, {
  stdio: "inherit",
  windowsHide: true,
});

child.on("error", (err) => {
  if (err.code === "ENOENT") {
    console.error("mcp-debug binary not found. Try reinstalling the package:");
    console.error("  npm uninstall mcp-debug && npm install mcp-debug");
  } else {
    console.error(`Failed to run mcp-debug: ${err.message}`);
  }
  process.exit(1);
});

child.on("exit", (code, signal) => {
  if (signal) {
    process.exit(1);
  }
  process.exit(code ?? 0);
});
