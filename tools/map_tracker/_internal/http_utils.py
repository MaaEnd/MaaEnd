import json
import os
import time
import urllib.request
import urllib.error
import warnings

import cv2
import numpy as np

_HEADERS = {"User-Agent": "MaaEnd-tools/0.1"}


def _http_utils_notice(msg: str):
    warnings.warn(f"http_utils: {msg}", stacklevel=3)
    if os.environ.get("GITHUB_ACTIONS") == "true":
        print(f"::notice::{msg}")


def _http_utils_warn(msg: str):
    warnings.warn(f"http_utils: {msg}", stacklevel=3)
    if os.environ.get("GITHUB_ACTIONS") == "true":
        print(f"::warning::{msg}")


def download(
    url: str,
    *,
    max_retries: int = 5,
    base_delay: float = 0.5,
    timeout: float = 60.0,
) -> bytes | None:
    """Download raw bytes from URL (with retries), returns bytes or None on failure."""
    retries = 0
    max_retries = max(0, max_retries)
    warning_msg = None
    while retries <= max_retries:
        if retries > 0:
            this_delay = base_delay * (2 ** (retries - 1))
            _http_utils_notice(
                f"Retrying download from {url} (attempt {retries}/{max_retries}, delay {this_delay:.1f}s)"
            )
            time.sleep(this_delay)
        try:
            req = urllib.request.Request(url, headers=_HEADERS)
            with urllib.request.urlopen(req, timeout=timeout) as resp:
                if resp.status != 200:
                    raise urllib.error.HTTPError(
                        url, resp.status, resp.reason, resp.headers, None
                    )
                return resp.read()
        except urllib.error.HTTPError as e:
            warning_msg = f"Failed to download from {url}: HTTP {e.code} - {e.reason}"
            break  # HTTP errors are unlikely to be resolved by retries
        except Exception as e:
            warning_msg = f"Failed to download from {url}: {type(e).__name__} - {e}"
        retries += 1

    if warning_msg:
        _http_utils_warn(warning_msg)
    return None


def download_image(
    url: str,
    *,
    min_size: int = 0,
    max_size: int = 128 * 1024 * 1024,
) -> tuple[np.ndarray, int] | None:
    """Download an image from URL, returns (ndarray, byte_size) or None on failure."""
    data = download(url)
    if data is None:
        return None
    if len(data) < min_size or len(data) > max_size:
        _http_utils_warn(f"Unexpected content size from {url}")
        return None
    buf = np.frombuffer(data, dtype=np.uint8)
    img = cv2.imdecode(buf, cv2.IMREAD_UNCHANGED)
    if img is None:
        _http_utils_warn(f"Failed to decode image from {url}")
        return None
    return img, len(data)


def download_json(url: str) -> dict | None:
    """Download JSON from URL, returns parsed dict or None on failure."""
    try:
        data = download(url)
        if data is None:
            return None
        return json.loads(data)
    except UnicodeDecodeError as e:
        _http_utils_warn(f"Failed to decode JSON from {url}: {type(e).__name__} - {e}")
        return None
    except json.JSONDecodeError as e:
        _http_utils_warn(f"Failed to parse JSON from {url}: {type(e).__name__} - {e}")
        return None
