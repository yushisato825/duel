# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## ビルドと実行

```bash
go build -o duel .
./duel <file1> <file2>
git diff | ./duel
```

## アーキテクチャ

単一ファイル構成（`main.go`）。[bubbletea](https://github.com/charmbracelet/bubbletea) の Elm アーキテクチャに従い、`model` / `Init` / `Update` / `View` で構成される。

### データフロー

```
入力（2ファイル or stdin unified diff）
  → computeDiff()       差分計算（go-diff による行単位diff）
  → alignRaw()          ブロック行揃え（削除/追加ブロックをペアリング）
  → []diffLine          表示用データ構造
  → View()              lipgloss でレンダリング
```

### 主要な型

- `diffLine` — 1行分の表示データ。`left`/`right` にテキスト、`leftNum`/`rightNum` に行番号、`kind` に行の種別を持つ
- `lineKind` — `kindEqual` / `kindChanged` / `kindRemoved` / `kindAdded` / `kindPad`
- `model` — bubbletea モデル。`editable bool` でファイル編集可否を管理

### レイアウト計算

```
[leftContent(contentW)][leftGutter(1)][leftNum(4)]│[rightNum(4)][rightGutter(1)][rightContent(contentW)]
```

`panelW = (width - 1) / 2`、`contentW = panelW - 5`

### 色の意味

| 色 | 意味 |
|----|------|
| 赤（#52） | 削除行 (`kindRemoved`) |
| 緑（#22） | 追加行 (`kindAdded`) |
| 青（#18） | 変更行 (`kindChanged`)、インライン強調は明るい青（#27） |
| グレー（#236） | 行数を揃えるパディング行 |

### ファイル編集機能

`editable=true`（2ファイルモード）のとき `>` / `<` キーで現在のハンクをファイルに適用できる。`applyChange()` が `tea.Cmd` を返し、適用後に `computeDiff` を再実行して `diffUpdatedMsg` でモデルを更新する。
