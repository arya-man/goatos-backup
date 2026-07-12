#!/usr/bin/env python3
"""Unit tests for telemetry_guard.py. Stdlib-only (unittest + tempfile + subprocess).

Run with:
    python3 -m unittest tools/telemetry-guard/test_telemetry_guard.py -v
"""

from __future__ import annotations

import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

import telemetry_guard as tg  # noqa: E402


def _init_git_repo(root: Path) -> None:
    subprocess.run(["git", "init", "-q"], cwd=root, check=True)
    subprocess.run(["git", "config", "user.email", "test@example.com"], cwd=root, check=True)
    subprocess.run(["git", "config", "user.name", "Test"], cwd=root, check=True)


def _commit_all(root: Path, message: str) -> None:
    subprocess.run(["git", "add", "-A"], cwd=root, check=True)
    subprocess.run(["git", "commit", "-q", "-m", message], cwd=root, check=True)


class TelemetryGuardConfigTests(unittest.TestCase):
    def test_default_config_loads_and_has_expected_surfaces(self):
        config = tg.load_config()
        self.assertIn("android", config["surfaces"])
        self.assertIn("admin_web", config["surfaces"])
        self.assertEqual(config["exempt_marker"], "telemetry:exempt")


class TelemetryGuardMatchingTests(unittest.TestCase):
    def test_glob_matches_nested_screen_file(self):
        self.assertTrue(
            tg.matches_any_glob(
                "apps/goatos-android/feature-drives/src/main/kotlin/DriveScreen.kt",
                ["apps/goatos-android/**/*Screen.kt"],
            )
        )

    def test_glob_does_not_match_unrelated_file(self):
        self.assertFalse(
            tg.matches_any_glob(
                "backend/internal/protocol/service.go",
                ["apps/goatos-android/**/*Screen.kt"],
            )
        )

    def test_has_marker_finds_analytics_port(self):
        self.assertEqual(tg.has_marker("class Foo { val analytics: AnalyticsPort }", ["AnalyticsPort"]), "AnalyticsPort")

    def test_has_marker_returns_none_when_absent(self):
        self.assertIsNone(tg.has_marker("class Foo {}", ["AnalyticsPort", "Crashlytics"]))

    def test_has_exempt_detects_marker(self):
        self.assertTrue(tg.has_exempt("// telemetry:exempt internal debug-only screen", "telemetry:exempt"))
        self.assertFalse(tg.has_exempt("// nothing here", "telemetry:exempt"))


class TelemetryGuardEndToEndTests(unittest.TestCase):
    """Builds a throwaway git repo mirroring apps/goatos-android's layout and
    runs the real `run()` function against it via --base diffs."""

    def setUp(self):
        self._tmp = tempfile.TemporaryDirectory()
        self.root = Path(self._tmp.name)
        _init_git_repo(self.root)
        self.screen_dir = self.root / "apps/goatos-android/feature-drives/src/main/kotlin/sg/mesha/goatos/drives"
        self.screen_dir.mkdir(parents=True)
        (self.root / "README.md").write_text("seed\n")
        _commit_all(self.root, "seed")

        self.config = tg.load_config()

    def _diff_scoped_report(self):
        return tg.run(
            repo=self.root,
            config=self.config,
            base=None,
            staged=False,
            scan_all=False,
            warn=lambda _msg: None,
        )

    def test_compliant_screen_passes(self):
        screen = self.screen_dir / "DriveScreen.kt"
        screen.write_text(
            "package sg.mesha.goatos.drives\n\n"
            "class DriveScreen(private val analytics: AnalyticsPort) {\n"
            "    fun onOpen() { analytics.track(AnalyticsEvents.APP_OPEN) }\n"
            "}\n"
        )
        _commit_all(self.root, "add compliant screen")

        report = self._diff_scoped_report()
        blocking = [f for f in report.findings if f.file.endswith("DriveScreen.kt")]
        self.assertEqual(blocking, [])
        self.assertFalse(report.has_blocking)

    def test_noncompliant_screen_fails(self):
        screen = self.screen_dir / "DriveScreen.kt"
        screen.write_text(
            "package sg.mesha.goatos.drives\n\n"
            "class DriveScreen {\n"
            "    fun onOpen() { /* no telemetry wired */ }\n"
            "}\n"
        )
        _commit_all(self.root, "add non-compliant screen")

        report = self._diff_scoped_report()
        matches = [f for f in report.findings if f.file.endswith("DriveScreen.kt")]
        self.assertEqual(len(matches), 1)
        self.assertEqual(matches[0].severity, "FAIL")
        self.assertTrue(report.has_blocking)

    def test_exempt_comment_passes(self):
        screen = self.screen_dir / "DriveScreen.kt"
        screen.write_text(
            "package sg.mesha.goatos.drives\n\n"
            "// telemetry:exempt internal debug-only screen, never shipped to users\n"
            "class DriveScreen {\n"
            "    fun onOpen() { }\n"
            "}\n"
        )
        _commit_all(self.root, "add exempt screen")

        report = self._diff_scoped_report()
        matches = [f for f in report.findings if f.file.endswith("DriveScreen.kt")]
        self.assertEqual(matches, [])
        self.assertFalse(report.has_blocking)

    def test_sibling_marker_satisfies_requirement(self):
        screen = self.screen_dir / "DriveScreen.kt"
        screen.write_text(
            "package sg.mesha.goatos.drives\n\n"
            "class DriveScreen {\n"
            "    fun onOpen() { }\n"
            "}\n"
        )
        viewmodel = self.screen_dir / "DriveViewModel.kt"
        viewmodel.write_text(
            "package sg.mesha.goatos.drives\n\n"
            "class DriveViewModel(private val analytics: AnalyticsPort) {\n"
            "    fun onLoad() { analytics.track(\"drive_opened\") }\n"
            "}\n"
        )
        _commit_all(self.root, "add screen with sibling viewmodel wiring analytics")

        report = self._diff_scoped_report()
        matches = [f for f in report.findings if f.file.endswith("DriveScreen.kt")]
        self.assertEqual(matches, [])

    def test_unrelated_file_change_is_ignored(self):
        (self.root / "README.md").write_text("seed\nmore docs\n")
        _commit_all(self.root, "docs only change")

        report = self._diff_scoped_report()
        self.assertEqual(report.findings, [])
        self.assertFalse(report.has_blocking)

    def test_admin_web_missing_marker_warns_not_blocks(self):
        page_dir = self.root / "apps/admin-web/app/vaccination"
        page_dir.mkdir(parents=True)
        (page_dir / "page.tsx").write_text("export default function Page() { return <div /> }\n")
        _commit_all(self.root, "add admin-web page without telemetry")

        report = self._diff_scoped_report()
        matches = [f for f in report.findings if f.file.endswith("app/vaccination/page.tsx")]
        self.assertEqual(len(matches), 1)
        self.assertEqual(matches[0].severity, "WARN")
        self.assertFalse(report.has_blocking)


if __name__ == "__main__":
    unittest.main()
