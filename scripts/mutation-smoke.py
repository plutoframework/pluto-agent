#!/usr/bin/env python3
"""Seeded, bounded smoke checks; build errors/timeouts never count as kills."""

import os
from pathlib import Path
import random
import shutil
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parent.parent
SEED = 20261028
MUTANTS = [
    ("run restriction", "dispatch.go", "!s.allowed[call.Name]", "false"),
    ("permission", "dispatch.go", "if !access.Public {", "if !access.Public && false {"),
    ("resource policy", "dispatch.go", "prepared.policy(ctx); err != nil {", "prepared.policy(ctx); err != nil && false {"),
    ("approval", "dispatch.go", "if prepared.approval {", "if prepared.approval && false {"),
    ("undeclared access", "tool.go", '(c.Access.Public == (c.Access.Permission != ""))', "false"),
    ("write replay", "dispatch.go", "if s.writes[key] {", "if s.writes[key] && false {"),
    ("tool limit", "dispatch.go", "len(calls) > s.agent.config.Limits.MaxToolCalls-s.result.ToolCalls {", "len(calls) > s.agent.config.Limits.MaxToolCalls-s.result.ToolCalls && false {"),
    ("model limit", "run.go", "s.result.ModelCalls >= limits.MaxModelCalls {", "s.result.ModelCalls >= limits.MaxModelCalls && false {"),
    ("token limit", "run.go", "used+u.InputTokens+u.OutputTokens > s.agent.config.Limits.MaxTokens {", "used+u.InputTokens+u.OutputTokens > s.agent.config.Limits.MaxTokens && false {"),
    ("repair limit", "run.go", "s.result.Repairs >= s.agent.config.Limits.MaxRepairs {", "s.result.Repairs >= s.agent.config.Limits.MaxRepairs && false {"),
    ("input limit", "run.go", "len(run.Input) > a.config.Limits.MaxInputBytes ||", "false ||"),
    ("object validation", "schema.go", "if !exists || child.validate(v) != nil {", "if !exists || (child.validate(v) != nil && false) {"),
]


def run(command, cwd, env):
    return subprocess.run(
        command, cwd=cwd, env=env, text=True, capture_output=True,
        timeout=45, check=False,
    )


def main():
    env = dict(os.environ, GOWORK="off")
    tests = ["go", "test", "-count=1", "-timeout=20s", "."]
    baseline = run(tests, ROOT, env)
    if baseline.returncode:
        raise SystemExit("Baseline failed:\n" + baseline.stdout + baseline.stderr)
    mutants = list(MUTANTS)
    random.Random(SEED).shuffle(mutants)
    killed = 0
    for name, filename, before, after in mutants:
        with tempfile.TemporaryDirectory(prefix="pluto-agent-mutant-") as tmp:
            checkout = Path(tmp)
            for source in ROOT.glob("*.go"):
                shutil.copy2(source, checkout / source.name)
            shutil.copy2(ROOT / "go.mod", checkout / "go.mod")
            shutil.copytree(ROOT / "providers", checkout / "providers")
            target = checkout / filename
            original = target.read_text()
            if original.count(before) != 1:
                raise SystemExit(f"Invalid mutation anchor: {name}")
            target.write_text(original.replace(before, after, 1))
            try:
                build = run(["go", "test", "-run=^$", "."], checkout, env)
                if build.returncode:
                    raise SystemExit(f"INVALID (build failure): {name}\n{build.stdout}{build.stderr}")
                outcome = run(tests, checkout, env)
            except subprocess.TimeoutExpired:
                raise SystemExit(f"INVALID (timeout): {name}")
            if "test timed out" in outcome.stdout + outcome.stderr:
                raise SystemExit(f"INVALID (test timeout): {name}")
            if not outcome.returncode:
                raise SystemExit(f"SURVIVED: {name}")
            killed += 1
            print(f"KILLED: {name}", flush=True)
    print(f"seed={SEED}; killed={killed}/{len(mutants)}; invalid=0")


if __name__ == "__main__":
    main()
