"""MCP Debug - A debugging and development tool for MCP servers."""

import os
import platform
import stat
import subprocess
import sys
import urllib.request
from pathlib import Path

__version__ = "1.0.0"

GITHUB_REPO = "standardbeagle/mcp-debug"


def get_binary_name() -> str:
    """Get the appropriate binary name for the current platform."""
    system = platform.system().lower()
    machine = platform.machine().lower()

    # Normalize architecture names
    if machine in ("x86_64", "amd64"):
        arch = "amd64"
    elif machine in ("arm64", "aarch64"):
        arch = "arm64"
    else:
        raise RuntimeError(f"Unsupported architecture: {machine}")

    # Normalize OS names
    if system == "darwin":
        os_name = "darwin"
    elif system == "linux":
        os_name = "linux"
    elif system == "windows":
        os_name = "windows"
    else:
        raise RuntimeError(f"Unsupported operating system: {system}")

    suffix = ".exe" if os_name == "windows" else ""
    return f"mcp-debug-{os_name}-{arch}{suffix}"


def get_binary_path() -> Path:
    """Get the path where the binary should be stored."""
    cache_dir = Path.home() / ".cache" / "mcp-debug"
    cache_dir.mkdir(parents=True, exist_ok=True)
    binary_name = get_binary_name()
    return cache_dir / binary_name


def get_download_url(version: str) -> str:
    """Get the download URL for the binary."""
    binary_name = get_binary_name()
    return f"https://github.com/{GITHUB_REPO}/releases/download/v{version}/{binary_name}"


def download_binary(url: str, dest: Path) -> None:
    """Download the binary from the given URL."""
    print(f"Downloading mcp-debug from {url}...", file=sys.stderr)
    try:
        urllib.request.urlretrieve(url, dest)
        # Make executable on Unix
        if platform.system() != "Windows":
            dest.chmod(dest.stat().st_mode | stat.S_IXUSR | stat.S_IXGRP | stat.S_IXOTH)
        print("Download complete.", file=sys.stderr)
    except Exception as e:
        if dest.exists():
            dest.unlink()
        raise RuntimeError(f"Failed to download binary: {e}") from e


def ensure_binary() -> Path:
    """Ensure the binary exists, downloading if necessary."""
    binary_path = get_binary_path()

    # Check if binary exists and is the right version
    version_file = binary_path.parent / ".version"
    current_version = None
    if version_file.exists():
        current_version = version_file.read_text().strip()

    if not binary_path.exists() or current_version != __version__:
        url = get_download_url(__version__)
        download_binary(url, binary_path)
        version_file.write_text(__version__)

    return binary_path


def main() -> int:
    """Main entry point - download and run the binary."""
    try:
        binary_path = ensure_binary()
        result = subprocess.run([str(binary_path)] + sys.argv[1:])
        return result.returncode
    except KeyboardInterrupt:
        return 130
    except Exception as e:
        print(f"Error: {e}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
