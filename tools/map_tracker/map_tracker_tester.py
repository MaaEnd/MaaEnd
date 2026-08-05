# /// script
# requires-python = ">=3.12"
# dependencies = [
#     "maafw>=5",
#     "opencv-python>=4",
# ]
# ///

# MapTrackerTester - Test labelled MapTracker inference samples.
#
# Usage:
#   python map_tracker_tester.py batch_test -i/--input-dir <dir> [-p/--precision <0.0-1.0>]

import argparse
import math
import os
from dataclasses import dataclass

import numpy as np

from _internal.core_utils import _R, _G, _Y, _C, _A, _B, _0, cv2
from _internal.maa_interface import (
    MaaInitializationError,
    MaaInterface,
    MaaRuntimeError,
)
from _internal.sample_handler import SampleFilenameParser


_MAX_LOC_ERROR = 1.733
_MIN_LOC_PASSRATE = 0.95
_MAX_ROT_ERROR = 6.0
_MIN_ROT_PASSRATE = 0.95
_SUBPIXEL_ENTROPY_GEQ = 0.6
_BATCH_PRECISION = 0.7
_FULL_SEARCH_MODE = 0b01
_FAST_SEARCH_MODE = 0b11
_FAST_SEARCH_REPEATS = 4
_DISTRIBUTION_COLUMNS = (
    ("P1", 1),
    ("P25", 25),
    ("P50", 50),
    ("P75", 75),
    ("P99", 99),
)


@dataclass
class BatchCase:
    """A single batch_test case with the expected answer parsed from its filename."""

    filename: str
    map_name: str
    x: float
    y: float
    rot: int


def _rotation_error(expected: int, actual: int) -> float:
    """Returns the smallest absolute angular difference in degrees."""
    diff = abs(float(actual) - float(expected)) % 360.0
    return min(diff, 360.0 - diff)


def _signed_rotation_error(expected: int, actual: int) -> float:
    """Returns the signed shortest angular difference in degrees."""
    return (float(actual) - float(expected) + 180.0) % 360.0 - 180.0


def _batch_error_line(case: BatchCase) -> str:
    return f"  {_R}FAIL{_0} {case.filename} {_R}Internal error{_0}"


def _batch_result_line(
    case: BatchCase,
    result: dict,
    *,
    passed: bool,
    map_ok: bool,
    loc_passed: bool,
    rot_passed: bool,
    coord_err: float,
) -> str:
    tag = f"{_G}PASS{_0}" if passed else f"{_R}FAIL{_0}"
    map_color = _A if map_ok else _Y
    loc_color = _B if loc_passed else _Y
    rot_color = _B if rot_passed else _Y
    dx = result["x"] - case.x
    dy = result["y"] - case.y
    signed_rot_err = round(_signed_rotation_error(case.rot, result["rot"]))
    return (
        f"  {tag} {case.filename} "
        f"{map_color}{result['map_name']}{_0} "
        f"loc_conf={loc_color}{result['loc_conf']:.2f}{_0} "
        f"loc_err={loc_color}{coord_err:.2f}{_0} "
        f"{_A}({dx:+.1f}, {dy:+.1f}){_0} "
        f"rot_conf={rot_color}{result['rot_conf']:.2f}{_0} "
        f"rot_err={rot_color}{signed_rot_err:+d}{_0}"
    )


def _batch_fast_error_line(error: MaaRuntimeError | None) -> str:
    suffix = f" ({error})" if error is not None else ""
    return f"       {_Y}Fast search failed{suffix}{_0}"


def _evaluate_case(case: BatchCase, result: dict) -> tuple[bool, float, float, bool]:
    map_ok = result["map_name"] == case.map_name
    coord_err = math.hypot(result["x"] - case.x, result["y"] - case.y)
    rot_err = _rotation_error(case.rot, result["rot"])
    passed = map_ok and coord_err <= _MAX_LOC_ERROR and rot_err <= _MAX_ROT_ERROR
    return passed, coord_err, rot_err, map_ok


def _load_batch_cases(input_dir: str, parser: SampleFilenameParser) -> list[BatchCase]:
    cases: list[BatchCase] = []
    for filename in sorted(os.listdir(input_dir)):
        parsed = parser.parse_filename(filename)
        if parsed is None:
            continue
        map_name, x, y, rot = parsed
        cases.append(BatchCase(filename, map_name, x, y, rot))
    return cases


def _print_distribution(title: str, values: list[float], value_format: str) -> None:
    def format_header() -> str:
        cells = [f"{label:>6}" for label, _ in _DISTRIBUTION_COLUMNS]
        return "| " + " | ".join(cells) + " |"

    def format_row() -> str:
        if not values:
            cells = [f"{'N/A':>6}" for _ in _DISTRIBUTION_COLUMNS]
        else:
            percentiles = np.percentile(values, [p for _, p in _DISTRIBUTION_COLUMNS])
            cells = [f"{value:{value_format}}" for value in percentiles]
        return "| " + " | ".join(cells) + " |"

    print(f"{_Y}{title}{_0}")
    print(f"  {_A}{format_header()}{_0}")
    print(f"  {_C}{format_row()}{_0}")


def _normalized_entropy(values: list[float]) -> float:
    bins = [0] * 10
    for value in values:
        bins[min(9, max(0, int(value * 10)))] += 1
    total = sum(bins)
    if total == 0:
        return 0.0
    entropy = 0.0
    for count in bins:
        if count > 0:
            probability = count / total
            entropy -= probability * math.log(probability)
    return entropy / math.log(10.0)


def _print_inference_matrix(
    title: str, passed: int, failed: int, confidences: list[float]
) -> None:
    total = passed + failed
    print(f"\n{_C}[{title}]{_0}")
    print(f"Passed={_G}{passed}{_0}, Failed={_R}{failed}{_0}, Total={_C}{total}{_0}")
    _print_distribution("Confidence Distribution:", confidences, ">6.3f")


def _maybe_output_ci_error(message: str) -> None:
    if os.environ.get("GITHUB_ACTIONS") == "true":
        print(f"::error::{message}")


def _infer_on_image(
    maa_interface: MaaInterface,
    image: np.ndarray,
    *,
    precision: float,
    allowed_modes: int,
) -> tuple[dict | None, MaaRuntimeError | None]:
    try:
        result = maa_interface.do_infer_on_image(
            image,
            precision=precision,
            allowed_modes=allowed_modes,
        )
    except MaaRuntimeError as error:
        return None, error
    return result, None


def cmd_batch_test(input_dir: str, precision: float = _BATCH_PRECISION) -> None:
    """Loads labelled sample images and reports inference accuracy."""
    if not os.path.isdir(input_dir):
        print(f"  {_R}Input directory not found: {input_dir}{_0}")
        print("    Did you forget to setup test set repository via git submodule?")
        _maybe_output_ci_error(f"Input directory not found: {input_dir}")
        raise SystemExit(1)

    cases = _load_batch_cases(input_dir, SampleFilenameParser())
    if not cases:
        print(f"  {_R}No valid sample images found in {input_dir}{_0}")
        _maybe_output_ci_error(f"No valid sample images found in {input_dir}")
        raise SystemExit(1)

    maa_interface = MaaInterface()
    passed_count = 0
    error_count = 0
    loc_passed_count = 0
    loc_failed_count = 0
    rot_passed_count = 0
    rot_failed_count = 0
    loc_confidences: list[float] = []
    rot_confidences: list[float] = []
    full_search_times: list[float] = []
    fast_search_times: list[float] = []
    subpixel_frac_x: list[float] = []
    subpixel_frac_y: list[float] = []

    try:
        print("[Preparing]")
        print(f"  CPU core(s): {_C}{os.cpu_count() or 'unknown'}{_0}")
        print(f"  Loaded {_C}{len(cases)}{_0} test case(s).")
        print("  Initializing MapTracker agent...")
        maa_interface.init_agent()
        print(f"  {_G}Ready{_0}. Warming up...")
        warmup_image = cv2.imread(os.path.join(input_dir, cases[0].filename))
        if warmup_image is not None:
            try:
                maa_interface.do_infer_on_image(
                    warmup_image,
                    precision=precision,
                    allowed_modes=_FULL_SEARCH_MODE,
                )
            except MaaRuntimeError as error:
                print(f"  {_Y}Warmup failed: {error}{_0}")
        else:
            print(f"  {_Y}Warmup skipped: cannot read {cases[0].filename}{_0}")
        print(f"  {_G}Ready{_0}. Running batch test...\n")
        print("[Testing]")

        for case in cases:
            image = cv2.imread(os.path.join(input_dir, case.filename))
            if image is None:
                error_count += 1
                loc_failed_count += 1
                rot_failed_count += 1
                print(_batch_error_line(case))
                continue

            result, error = _infer_on_image(
                maa_interface,
                image,
                precision=precision,
                allowed_modes=_FULL_SEARCH_MODE,
            )
            if error is not None or result is None:
                error_count += 1
                loc_failed_count += 1
                rot_failed_count += 1
                print(_batch_error_line(case))
                continue

            full_search_times.append(float(result["infer_time_ms"]))
            subpixel_frac_x.append(result["x"] - math.floor(result["x"]))
            subpixel_frac_y.append(result["y"] - math.floor(result["y"]))
            fast_result = None
            fast_error = None
            for _ in range(_FAST_SEARCH_REPEATS):
                fast_result, fast_error = _infer_on_image(
                    maa_interface,
                    image,
                    precision=precision,
                    allowed_modes=_FAST_SEARCH_MODE,
                )
            if fast_error is not None or fast_result is None:
                print(_batch_fast_error_line(fast_error))
            else:
                fast_search_times.append(float(fast_result["infer_time_ms"]))

            passed, coord_err, rot_err, map_ok = _evaluate_case(case, result)
            loc_passed = map_ok and coord_err <= _MAX_LOC_ERROR
            rot_passed = rot_err <= _MAX_ROT_ERROR
            loc_confidences.append(result["loc_conf"])
            rot_confidences.append(result["rot_conf"])
            loc_passed_count += int(loc_passed)
            loc_failed_count += int(not loc_passed)
            rot_passed_count += int(rot_passed)
            rot_failed_count += int(not rot_passed)
            passed_count += int(passed)
            print(
                _batch_result_line(
                    case,
                    result,
                    passed=passed,
                    map_ok=map_ok,
                    loc_passed=loc_passed,
                    rot_passed=rot_passed,
                    coord_err=coord_err,
                )
            )
    except MaaInitializationError as error:
        print(f"  {_R}Initialization failed: {error}{_0}")
        _maybe_output_ci_error(f"Initialization failed: {error}")
        raise SystemExit(1) from error
    finally:
        maa_interface.dispose_agent()

    print("\n[Summary]")
    _print_inference_matrix(
        "Location Inference", loc_passed_count, loc_failed_count, loc_confidences
    )
    _print_distribution(
        "Full Search Time Distribution (ms):", full_search_times, ">6.1f"
    )
    _print_distribution(
        "Fast Search Time Distribution (ms):", fast_search_times, ">6.1f"
    )
    _print_inference_matrix(
        "Rotation Inference", rot_passed_count, rot_failed_count, rot_confidences
    )

    print(f"\n{_C}[Subpixel Analysis]{_0}")
    entropy_x = _normalized_entropy(subpixel_frac_x)
    entropy_y = _normalized_entropy(subpixel_frac_y)
    entropy_x_color = _G if entropy_x >= _SUBPIXEL_ENTROPY_GEQ else _R
    entropy_y_color = _G if entropy_y >= _SUBPIXEL_ENTROPY_GEQ else _R
    print(f"  X normalized entropy = {entropy_x_color}{entropy_x:.1%}{_A} / 100.0%{_0}")
    print(f"  Y normalized entropy = {entropy_y_color}{entropy_y:.1%}{_A} / 100.0%{_0}")

    print(f"\n{_C}[Standard Check]{_0}")
    should_fail = False
    matrix = [
        ("Location pass rate", loc_passed_count / len(cases), _MIN_LOC_PASSRATE),
        ("Rotation pass rate", rot_passed_count / len(cases), _MIN_ROT_PASSRATE),
        ("X subpixel entropy", entropy_x, _SUBPIXEL_ENTROPY_GEQ),
        ("Y subpixel entropy", entropy_y, _SUBPIXEL_ENTROPY_GEQ),
    ]
    for name, value, threshold in matrix:
        if value < threshold:
            should_fail = True
            print(f"  {_R}FAIL{_0} {name}: {value:>6.1%}{_A}  < {threshold:.1%}{_0}")
            _maybe_output_ci_error(
                f"Standard not satisfied: {name} is {value:.1%}, expected at least {threshold:.1%}"
            )
        else:
            print(f"  {_G}PASS{_0} {name}: {value:>6.1%}{_A} >= {threshold:.1%}{_0}")

    if should_fail:
        print(f"\n{_R}Batch test failed because some standards were not satisfied.{_0}")
        raise SystemExit(1)
    print(f"\n{_G}Batch test completed and passed all standards.{_0}")


def main() -> None:
    parser = argparse.ArgumentParser(description="Test MapTracker inference samples.")
    subparsers = parser.add_subparsers(dest="command", required=True)
    batch_parser = subparsers.add_parser(
        "batch_test", help="Infer labelled sample images and report accuracy."
    )
    batch_parser.add_argument(
        "-i", "--input-dir", required=True, help="Directory of labelled sample images."
    )
    batch_parser.add_argument(
        "-p",
        "--precision",
        type=float,
        default=_BATCH_PRECISION,
        help=f"Inference precision for batch testing. Defaults to {_BATCH_PRECISION}.",
    )
    batch_parser.set_defaults(
        func=lambda args: cmd_batch_test(args.input_dir, args.precision)
    )
    args = parser.parse_args()
    args.func(args)


if __name__ == "__main__":
    main()
