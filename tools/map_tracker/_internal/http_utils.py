import json
import urllib.request
import urllib.error
import warnings

import cv2
import numpy as np

_HEADERS = {"User-Agent": "MaaEnd-tools/0.1"}


def download_image(
    url: str,
    *,
    timeout: float = 30.0,
    min_size: int = 0,
    max_size: int = 128 * 1024 * 1024,
) -> tuple[np.ndarray, int] | None:
    """Download an image from URL, returns (ndarray, byte_size) or None on failure."""
    try:
        req = urllib.request.Request(url, headers=_HEADERS)
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            if resp.status != 200:
                warnings.warn(
                    f"http_utils: Failed to download image from {url}: HTTP {resp.status}"
                )
                return None
            data = resp.read()
        if len(data) < min_size or len(data) > max_size:
            return None
        buf = np.frombuffer(data, dtype=np.uint8)
        img = cv2.imdecode(buf, cv2.IMREAD_UNCHANGED)
        if img is None:
            warnings.warn(f"http_utils: Failed to decode image from {url}")
            return None
        return img, len(data)
    except urllib.error.URLError as e:
        warnings.warn(
            f"http_utils: Failed to download image from {url}: "
            f"{type(e).__name__} - {e}"
        )
        return None


def download_json(
    url: str,
    *,
    timeout: float = 30.0,
) -> dict | None:
    """Download JSON from URL, returns parsed dict or None on failure."""
    try:
        req = urllib.request.Request(url, headers=_HEADERS)
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            if resp.status != 200:
                warnings.warn(
                    f"http_utils: Failed to download JSON from {url}: HTTP {resp.status}"
                )
                return None
            return json.loads(resp.read())
    except UnicodeDecodeError as e:
        warnings.warn(
            f"http_utils: Failed to decode JSON from {url}: {type(e).__name__} - {e}"
        )
        return None
    except json.JSONDecodeError as e:
        warnings.warn(
            f"http_utils: Failed to parse JSON from {url}: {type(e).__name__} - {e}"
        )
        return None
    except urllib.error.URLError as e:
        warnings.warn(
            f"http_utils: Failed to download JSON from {url}: "
            f"{type(e).__name__} - {e}"
        )
        return None
