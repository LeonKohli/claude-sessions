# Tests

Follow Google's [Test Behavior, Not Implementation](https://testing.googleblog.com/2013/08/testing-on-toilet-test-behavior-not.html) when adding or reviewing tests.

- Name the user-visible scenario and assert its outcome through the smallest useful public boundary. For this repo, that means returned sessions, recovered bytes, CLI output and exit status, or the browser's response to events.
- Keep assertions valid when helpers, buffers, cache formats, or search generation counters change without changing behavior. Use implementation assertions only for an explicit requirement, and explain which failure they detect.
- Use temporary transcript stores and recorded-format fixtures. Exercise production code rather than recreating its algorithm in the test. Assert identities and content where counts alone could accept the wrong result.
- For a bug, reproduce the wrong observable result before fixing it. Preserve valid coverage when replacing a coupled test. Keep normal tests independent of personal session history and timing sleeps.

# File recovery

Treat logs as data. Recover recorded bytes with clear provenance; tool inputs alone do not prove a write succeeded. Never execute a recorded Python script or shell command to reconstruct a file. A checkpoint or successful write is a historical version, not proof of the session's final file state.
