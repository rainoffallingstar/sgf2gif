# sgf2gif
Generate animated gifs from sgf files.

# Installation
```
$ go install github.com/alcortesm/sgf2gif@latest
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

To analyze each rendered position with KataGo, add winrate and score-lead curves,
and show KataGo's recommended next moves on the current board:

```
$ sgf2gif --katago-analyze /tmp/foo.sgf /tmp/foo.gif
```

You can use built-in strength presets:

```
$ sgf2gif --katago-strength fast /tmp/foo.sgf /tmp/foo.gif
$ sgf2gif --katago-strength strong /tmp/foo.sgf /tmp/foo.gif
$ sgf2gif --katago-strength monster /tmp/foo.sgf /tmp/foo.gif
```

These presets map to:

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

Colab users can start from:

- [notebooks/sgf2gif_katago_colab.ipynb](/Volumes/DataCenter_01/GitHub/sgf2gif/notebooks/sgf2gif_katago_colab.ipynb)

The resulting gif is shown below.

![AlpahGo vs. Lee Sedol 2016-03-15](https://user-images.githubusercontent.com/9169414/33006598-3c0b2106-cdcb-11e7-94d0-d6db14675d71.gif)

# Limitations
Only the main line is rendered when the SGF contains variations.
Dead stones are not removed from the board.
Rectangular boards are not supported.
