#!/usr/bin/env python3
"""Normalize Cursor preToolUse JSON for shared Goat OS agent hooks."""
import json
import sys

raw = sys.stdin.read() or "{}"
try:
    data = json.loads(raw)
except Exception:
    data = {}
if not isinstance(data, dict):
    data = {}

tool = data.get("tool_name") or data.get("tool") or data.get("name") or ""
if tool == "Shell":
    data["tool_name"] = "Bash"

tool_input = data.get("tool_input") or data.get("input") or data.get("arguments") or {}
if not isinstance(tool_input, dict):
    tool_input = {}

if tool in ("Read", "read") and "path" in tool_input and "file_path" not in tool_input:
    tool_input["file_path"] = tool_input["path"]
if tool in ("Write", "StrReplace", "EditNotebook") and "path" in tool_input and "file_path" not in tool_input:
    tool_input["file_path"] = tool_input["path"]

if tool_input:
    data["tool_input"] = tool_input

json.dump(data, sys.stdout)
