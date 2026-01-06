#!/usr/bin/env node

const { execSync } = require("child_process");
const fs = require("fs");
const https = require("https");
const http = require("http");
const os = require("os");
const path = require("path");
const { URL } = require("url");

const VERSION = require("./package.json").version;
const GITHUB_REPO = "standardbeagle/mcp-debug";

function getPlatformInfo() {
  const platform = os.platform();
  const arch = os.arch();

  let osName;
  switch (platform) {
    case "darwin":
      osName = "darwin";
      break;
    case "linux":
      osName = "linux";
      break;
    case "win32":
      osName = "windows";
      break;
    default:
      throw new Error(`Unsupported platform: ${platform}`);
  }

  let archName;
  switch (arch) {
    case "x64":
      archName = "amd64";
      break;
    case "arm64":
      archName = "arm64";
      break;
    default:
      throw new Error(`Unsupported architecture: ${arch}`);
  }

  const suffix = osName === "windows" ? ".exe" : "";
  const binaryName = `mcp-debug-${osName}-${archName}${suffix}`;

  return { osName, archName, binaryName, suffix };
}

function getDownloadUrl() {
  const { binaryName } = getPlatformInfo();
  return `https://github.com/${GITHUB_REPO}/releases/download/v${VERSION}/${binaryName}`;
}

function getBinaryPath() {
  const { suffix } = getPlatformInfo();
  return path.join(__dirname, `mcp-debug${suffix}`);
}

function download(url, dest) {
  return new Promise((resolve, reject) => {
    const file = fs.createWriteStream(dest);
    const urlObj = new URL(url);
    const protocol = urlObj.protocol === "https:" ? https : http;

    const request = protocol.get(url, (response) => {
      // Handle redirects
      if (response.statusCode >= 300 && response.statusCode < 400 && response.headers.location) {
        file.close();
        fs.unlinkSync(dest);
        return download(response.headers.location, dest).then(resolve).catch(reject);
      }

      if (response.statusCode !== 200) {
        file.close();
        fs.unlinkSync(dest);
        reject(new Error(`Failed to download: HTTP ${response.statusCode}`));
        return;
      }

      response.pipe(file);

      file.on("finish", () => {
        file.close();
        resolve();
      });
    });

    request.on("error", (err) => {
      file.close();
      if (fs.existsSync(dest)) {
        fs.unlinkSync(dest);
      }
      reject(err);
    });

    file.on("error", (err) => {
      file.close();
      if (fs.existsSync(dest)) {
        fs.unlinkSync(dest);
      }
      reject(err);
    });
  });
}

async function main() {
  const binaryPath = getBinaryPath();
  const url = getDownloadUrl();

  console.log(`Downloading mcp-debug from ${url}...`);

  try {
    await download(url, binaryPath);

    // Make executable on Unix
    if (os.platform() !== "win32") {
      fs.chmodSync(binaryPath, 0o755);
    }

    console.log("Download complete.");
  } catch (error) {
    console.error(`Failed to download mcp-debug: ${error.message}`);
    process.exit(1);
  }
}

main();
