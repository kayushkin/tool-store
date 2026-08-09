#!/usr/bin/env python3
"""Sabotage cases for the rune-safe truncation work.

The defect this scores is one mechanism spread over six files: a shared pair of
helpers, and five tools that cut a string down to a byte budget. The engine in
sabotage.py scores one file at a time, so this drives it once per target and
adds the scores up.

Scoring the helper alone would not be enough. The helper's own tests could be
perfect while a call site still byte-cuts, and that is exactly the state this
repo was in before tonight — so every call site gets a case that puts the plain
byte cut back, and it has to go red.

Run with --diffs at least once and read each applied edit against its label. A
row prints the name it was given, not the edit it made.
"""

import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from sabotage import Case, REPO, counts_as_coverage, problems, score  # noqa: E402

PACKAGES = ["./schema/", "./tools/"]

# The fixture guards in the packages above, by message. A guard fires when a
# mutation stops the test input reaching the code under test; `go test` exits
# non-zero for that exactly as it does for a real assertion, so without these
# the engine counts the test falling over as coverage. See
# sabotage.classify_caught().
#
# Only tools/runecut_test.go:121 is in scope here. The repo's other guard,
# provision_test.go:193 ("instance opt-in did not reach the config"), sits in
# the root package, which PACKAGES does not run.
GUARD_MARKERS = (
    "input was not truncated at all, so the test proves nothing",
)

HELPER = REPO / "schema" / "runeboundary.go"

HELPER_CASES = [
    # The walks themselves. Written as drifted comparisons rather than
    # deletions so no identifier is orphaned and the case scores instead of
    # reporting a compile error.
    Case("prefix walk never runs (comparison drifted)",
         [("for cut > 0 && !utf8.RuneStart(s[cut])",
           "for cut > len(s) && !utf8.RuneStart(s[cut])")]),
    Case("prefix walk runs the wrong way",
         [("\t\tcut--", "\t\tcut++")]),
    Case("suffix walk never runs (comparison drifted)",
         [("for cut < len(s) && !utf8.RuneStart(s[cut])",
           "for cut < 0 && !utf8.RuneStart(s[cut])")]),
    Case("suffix walk runs the wrong way",
         [("\t\tcut++", "\t\tcut--")]),

    # "Valid UTF-8 within budget" is satisfied by giving up entirely. These are
    # the cases the length assertions exist for.
    Case("prefix trims to nothing", [("\treturn s[:cut]", "\treturn \"\"")]),
    Case("suffix trims to nothing", [("\treturn s[cut:]", "\treturn \"\"")]),

    # The budget itself.
    Case("prefix ignores the budget when the string is under twice it",
         [("\tif len(s) <= maxBytes {\n\t\treturn s\n\t}\n\t// s[cut] is the first byte past",
           "\tif len(s) <= maxBytes*2 {\n\t\treturn s\n\t}\n\t// s[cut] is the first byte past")]),
    Case("suffix ignores the budget when the string is under twice it",
         [("\tif len(s) <= maxBytes {\n\t\treturn s\n\t}\n\t// s[cut] is the first byte of",
           "\tif len(s) <= maxBytes*2 {\n\t\treturn s\n\t}\n\t// s[cut] is the first byte of")]),

    Case("CONTROL known-positive: the prefix helper returns a fixed string",
         [("\treturn s[:cut]", "\treturn \"SABOTAGE\"")]),
    Case("CONTROL known-negative: <= 0 rewritten as < 1, identical for ints",
         [("\tif maxBytes <= 0 {\n\t\treturn \"\"\n\t}\n\tif len(s) <= maxBytes {\n\t\treturn s\n\t}\n\t// s[cut] is the first byte past",
           "\tif maxBytes < 1 {\n\t\treturn \"\"\n\t}\n\tif len(s) <= maxBytes {\n\t\treturn s\n\t}\n\t// s[cut] is the first byte past")],
         expected_unnoticed="a behavioural no-op; it must NOT be caught"),
]

# Each call site gets the plain byte cut put back. If the suite stays green for
# any of these, that tool is unpinned no matter how well the helper scores.
CALL_SITES = {
    REPO / "tools" / "web_fetch.go": [
        Case("web_fetch byte-cuts fetched page text again",
             [("schema.TruncateAtRuneBoundary(content, maxChars)", "content[:maxChars]")]),
    ],
    REPO / "tools" / "fs.go": [
        Case("read_files byte-cuts file content again",
             [("schema.TruncateAtRuneBoundary(content, maxWholeFileReadBytes)",
               "content[:maxWholeFileReadBytes]")]),
        Case("the read banner reports the cap instead of what was kept",
             [("\t\t\tkeptBytes, len(data), totalLines)",
               "\t\t\tmaxWholeFileReadBytes, len(data), totalLines)")]),
    ],
    REPO / "tools" / "task_plan.go": [
        Case("the build output written to .task.md is byte-cut again",
             [("schema.TruncateAtRuneBoundary(buildOutput, 500)", "buildOutput[:500]")]),
        Case("the build result output is byte-cut again",
             [("schema.TruncateAtRuneBoundary(output, 2000)", "output[:2000]")]),
    ],
    REPO / "tools" / "scheduler.go": [
        Case("the scheduler tool byte-cuts run output again",
             [("schema.TruncateAtRuneBoundary(output, 500)", "output[:500]")]),
    ],
    REPO / "tools" / "shell.go": [
        Case("shell_commands byte-cuts the head again",
             [("schema.TruncateAtRuneBoundary(s, headChars)", "s[:headChars]")]),
        Case("shell_commands byte-cuts the tail again",
             [("schema.SuffixAtRuneBoundary(s, tailChars)", "s[len(s)-tailChars:]")]),
        Case("the omitted-bytes count is derived from the caps, not what was kept",
             [("omitted := len(s) - len(head) - len(tail)",
               "omitted := len(s) - headChars - tailChars")]),
    ],
}


def main():
    total_caught = 0
    total_real = 0
    found = []

    targets = [(HELPER, HELPER_CASES)] + list(CALL_SITES.items())
    for target, cases in targets:
        print("\n" + "=" * 62)
        print("target: %s" % target.relative_to(REPO))
        print("=" * 62)
        results = score(target, PACKAGES, cases, GUARD_MARKERS)
        found += problems(results)
        for case, verdict, _, _ in results:
            if case.name.startswith("CONTROL"):
                continue
            total_real += 1
            # counts_as_coverage, not `verdict == "CAUGHT"`: a row that went red
            # because a fixture guard fired is not a mechanism this suite pins,
            # and adding it in here is the inflation the split exists to stop.
            if counts_as_coverage(verdict):
                total_caught += 1

    print("\n" + "=" * 62)
    print("TOTAL: %d/%d real mechanisms caught across %d files"
          % (total_caught, total_real, len(targets)))
    if found:
        print("  ⚠️  %d problem(s) across the tables above" % len(found))

    # Ask the engine what went wrong rather than counting non-CAUGHT rows here.
    # The private counter this replaced was a second, weaker definition of the
    # same thing: it skipped every CONTROL, so a known-positive control that
    # went UNNOTICED — the suite not running at all — added nothing to it, and
    # it ignored expected_unnoticed, so the first KNOWN GAP case added here
    # would have counted as a failure. Two definitions of "wrong" drift, and
    # the copy is the half nobody re-reads.
    return 1 if found else 0


if __name__ == "__main__":
    sys.exit(main())
