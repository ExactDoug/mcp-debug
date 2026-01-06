const os = require("os");
const path = require("path");

function getBinaryPath() {
  const suffix = os.platform() === "win32" ? ".exe" : "";
  return path.join(__dirname, `mcp-debug${suffix}`);
}

module.exports = {
  binaryPath: getBinaryPath(),
  getBinaryPath,
};
