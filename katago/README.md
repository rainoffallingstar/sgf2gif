# KataGo Local Setup

This directory is configured to use the locally installed Homebrew KataGo.

Expected layout:

- `katago/bin/katago`
- `katago/models/default_model.bin.gz`
- `katago/configs/gtp_example.cfg`
- `katago/configs/analysis_example.cfg`

Quick checks:

```bash
./scripts/katago-selfcheck.sh
```

Analyze a specific move from an SGF:

```bash
./scripts/katago-evalsgf.sh ./85130272-301-yrc21-rainoffallingstar1234.sgf 120
```

Override visits/threads:

```bash
KATAGO_VISITS=400 KATAGO_THREADS=8 ./scripts/katago-evalsgf.sh ./85130272-301-yrc21-rainoffallingstar1234.sgf 120
```

Switch model temporarily:

```bash
KATAGO_MODEL=./katago/models/g170-b40c256x2-s5095420928-d1229425124.bin.gz \
./scripts/katago-evalsgf.sh ./85130272-301-yrc21-rainoffallingstar1234.sgf 120
```

Notes:

- The files in `katago/bin`, `katago/models`, and `katago/configs` are local symlinks to the Homebrew KataGo installation.
- If Homebrew upgrades KataGo and removes the old Cellar path, rerun the setup step or update the symlinks.
- `evalsgf` is a good starting point for per-move analysis before wiring KataGo into `sgf2gif` itself.
