#!/usr/bin/env python3
"""Build and minify static assets with content-based hashing."""

import hashlib
import json
from pathlib import Path

import rcssmin
import rjsmin

# Directories
STATIC_DIR = Path("static")
DIST_DIR = STATIC_DIR / "dist"
MANIFEST_PATH = STATIC_DIR / "manifest.json"

# Files to process (source path relative to STATIC_DIR)
ASSETS = [
    "css/style.css",
    "js/htmx.min.js",
    "js/app.js",
]


def compute_hash(content: bytes, length: int = 8) -> str:
    """Compute short hash of content."""
    return hashlib.md5(content).hexdigest()[:length]


def minify_css(content: str) -> str:
    """Minify CSS content."""
    return rcssmin.cssmin(content)


def minify_js(content: str) -> str:
    """Minify JavaScript content."""
    return rjsmin.jsmin(content)


def process_file(source_path: Path) -> tuple[str, str]:
    """Process a single file: minify and compute hash.

    Args:
        source_path: Path to source file relative to STATIC_DIR

    Returns:
        Tuple of (original_url, versioned_url)
    """
    full_path = STATIC_DIR / source_path
    content = full_path.read_text(encoding="utf-8")

    # Minify based on file type
    suffix = source_path.suffix.lower()
    if suffix == ".css":
        minified = minify_css(content)
    elif suffix == ".js":
        minified = minify_js(content)
    else:
        minified = content

    # Compute hash from minified content
    content_hash = compute_hash(minified.encode("utf-8"))

    # Create output path: dist/css/style.css or dist/js/htmx.min.js
    output_path = DIST_DIR / source_path
    output_path.parent.mkdir(parents=True, exist_ok=True)
    output_path.write_text(minified, encoding="utf-8")

    # Return URL mapping
    original_url = f"/static/{source_path}"
    versioned_url = f"/static/dist/{source_path}?v={content_hash}"

    return original_url, versioned_url


def build_assets() -> dict[str, str]:
    """Build all assets and return manifest."""
    manifest = {}

    for asset in ASSETS:
        source_path = Path(asset)
        original_url, versioned_url = process_file(source_path)
        manifest[original_url] = versioned_url
        print(f"  {original_url} -> {versioned_url}")

    return manifest


def main():
    """Main entry point."""
    print("Building assets...")

    # Clean dist directory
    if DIST_DIR.exists():
        import shutil

        shutil.rmtree(DIST_DIR)

    # Build assets
    manifest = build_assets()

    # Write manifest
    MANIFEST_PATH.write_text(
        json.dumps(manifest, indent=2),
        encoding="utf-8",
    )
    print(f"\nManifest written to {MANIFEST_PATH}")
    print("Done!")


if __name__ == "__main__":
    main()
