# sgf2gif
Generate animated gifs from sgf files.

# Installation
```
$ go install github.com/rainoffallingstar/sgf2gif@latest
```

# Usage Example
Given the file `/tmp/foo.sgf`
(AlphaGo vs. Lee Sedol 2016-03-15 match)
you can generate the corresponding gif file as follows:

```
$ sgf2gif /tmp/foo.sgf /tmp/foo.gif
```

To show move numbers on stones instead of the default last-move highlight:

```
$ sgf2gif --move-numbers /tmp/foo.sgf /tmp/foo.gif
```

To show move numbers only for the most recent 20 moves:

```
$ sgf2gif --recent-move-numbers 20 /tmp/foo.sgf /tmp/foo.gif
```

To export a specific variation path instead of always taking the first branch:

```
$ sgf2gif --variation-path 2,1 /tmp/foo.sgf /tmp/foo.gif
```

To export every leaf variation to separate GIF files:

```
$ sgf2gif --all-variations /tmp/foo.sgf /tmp/foo.gif
```

This writes files like `/tmp/foo.var-main.gif` and `/tmp/foo.var-2-1.gif`.

You can also render directly from remote game sources without saving the SGF first:

```
$ sgf2gif https://online-go.com/game/85130272 /tmp/foo.gif
$ sgf2gif https://www.foxwq.com/qipu/newlist/id/2026031862241631.html /tmp/foo.gif
```

To download a single remote SGF instead of rendering a GIF:

```
$ sgf2gif --download-sgf ogs:85130272 /tmp/game.sgf
$ sgf2gif --download-sgf https://www.foxwq.com/qipu/newlist/id/2026031862241631.html /tmp/game.sgf
```

To batch-download recent OGS games for a user into a directory:

```
$ sgf2gif --download-sgf --download-limit 20 ogs-user:rainoffallingstar1234 /tmp/ogs-games
```

If a source requires login, you can put a raw `Cookie` header value into a file and pass it with:

```
$ sgf2gif --download-sgf --download-cookie-file /tmp/cookie.txt ogs-user:rainoffallingstar1234 /tmp/ogs-games
```

To analyze each rendered position with KataGo, add winrate and score-lead curves,
and show KataGo's recommended next moves on the current board:

```
$ sgf2gif --katago-analyze /tmp/foo.sgf /tmp/foo.gif
```

With KataGo enabled, the GIF now also includes:

- ghost-stone next-move recommendations on the board
- an arrow from the last move to KataGo's predicted next move
- a move-quality badge and gradient last-move highlight based on the winrate gap
- a final summary frame with phase hit rates, move-quality comparison, verdict lines, and top blunders
- a companion analyzed SGF saved as `*.katago.sgf` for cache-based rerenders

You can switch the analysis panel between black and white perspective:

```
$ sgf2gif --katago-analyze --katago-view white /tmp/foo.sgf /tmp/foo.gif
```

You can control how many candidate moves are drawn on the board:

```
$ sgf2gif --katago-analyze --katago-top-moves 5 /tmp/foo.sgf /tmp/foo.gif
```

You can combine move numbers with KataGo overlays. This is often the most useful review mode:

```
$ sgf2gif --katago-strength mild --recent-move-numbers 20 /tmp/foo.sgf /tmp/foo.gif
```

You can use built-in strength presets:

```
$ sgf2gif --katago-strength mild /tmp/foo.sgf /tmp/foo.gif
$ sgf2gif --katago-strength fast /tmp/foo.sgf /tmp/foo.gif
$ sgf2gif --katago-strength strong /tmp/foo.sgf /tmp/foo.gif
$ sgf2gif --katago-strength monster /tmp/foo.sgf /tmp/foo.gif
```

These presets map to:

- `mild` = `50` visits
- `fast` = `100` visits
- `strong` = `1000` visits
- `monster` = `10000` visits

You can also tune the analysis budget directly:

```
$ sgf2gif --katago-analyze --katago-visits 400 --katago-threads 4 /tmp/foo.sgf /tmp/foo.gif
```

When `--katago-analyze` is enabled:

- On Linux and Windows, if KataGo is missing, `sgf2gif` will download the latest official KataGo release automatically.
- On macOS, if KataGo is missing, `sgf2gif` will prompt you to install it with `brew install katago`.
- If the KataGo model or analysis config are missing, `sgf2gif` will download the latest official model and config files automatically into `./katago/`.
- KataGo startup/loading logs are suppressed during normal analysis; the terminal shows a live progress bar with elapsed time and ETA, then a final total duration line.
- The analysis panel can show the current move, KataGo's best move, and the estimated point loss for the played move.
- The analysis panel can also show the winrate gap and a quality label.
- The final frame summarizes phase-by-phase accuracy, average loss, largest swing, phase pressure, and top blunders.
- By default, an analyzed companion SGF is saved next to the GIF as `*.katago.sgf`.
- The companion SGF cache stores the rendered-position analysis, diagnostics summary, visit budget, top-move count, and the actual resolved backend when it can be determined.
- Cache reuse is compatibility-based: `sgf2gif` reuses a cache when its `maxVisits` and `topMoves` are at least as strong as the current request, and when an explicitly requested backend remains compatible.
- `--katago-refresh` forces a fresh KataGo run even if a compatible cache already exists.
- `--katago-cache-only` refuses to launch KataGo and fails unless a compatible cache is already present.
- `--katago-no-cache-write` still runs KataGo analysis, but skips writing the companion `*.katago.sgf` file.

For example, you can analyze once and rerender from cache later:

```
$ sgf2gif --katago-strength strong /tmp/foo.sgf /tmp/foo.gif
$ sgf2gif /tmp/foo.katago.sgf /tmp/foo-rerender.gif
```

If you want to force a rerun or require cache-only behavior:

```
$ sgf2gif --katago-refresh --katago-strength strong /tmp/foo.sgf /tmp/foo.gif
$ sgf2gif --katago-cache-only /tmp/foo.katago.sgf /tmp/foo-rerender.gif
```

If you want analysis overlays for this run but do not want to write a companion cache file:

```
$ sgf2gif --katago-analyze --katago-no-cache-write /tmp/foo.sgf /tmp/foo.gif
```

If you want a denser review pass with more candidate moves and more visits:

```
$ sgf2gif --katago-strength strong --katago-top-moves 5 --recent-move-numbers 30 /tmp/foo.sgf /tmp/foo.gif
```

Colab users can start from:

- [notebooks/sgf2gif_katago_colab.ipynb](/Volumes/DataCenter_01/GitHub/sgf2gif/notebooks/sgf2gif_katago_colab.ipynb)
- [Open In Colab](https://colab.research.google.com/github/rainoffallingstar/sgf2gif/blob/master/notebooks/sgf2gif_katago_colab.ipynb)

The Colab notebook now starts with remote download smoke tests for OGS and Fox, then runs a `mild` KataGo analysis check on [testdata/katago-e2e.sgf](/Volumes/DataCenter_01/GitHub/sgf2gif/testdata/katago-e2e.sgf), and finally lets you scale up to `fast`, `strong`, or `monster`.
It installs the official Go `1.26.1` toolchain directly, because Colab's default `golang-go` package is too old for this module.
It is safe to rerun from the top, because the notebook switches back to `/content` before removing and recloning the repo.
If your current Colab session is already stuck in a deleted repo directory, run `%cd /content` once before rerunning the first cell.

The resulting gif is shown below.

![AlpahGo vs. Lee Sedol 2016-03-15](https://user-images.githubusercontent.com/9169414/33006598-3c0b2106-cdcb-11e7-94d0-d6db14675d71.gif)

# Limitations
Rectangular boards are not supported.
Fox support currently targets public qipu pages or URLs that are accessible with the cookie you provide.
