# duel

ターミナルで動くサイドバイサイドのdiffビューア。

![demo](demo/demo.gif)

## インストール

```bash
go install github.com/yushisato825/duel@latest
```

またはソースからビルド：

```bash
go build -o duel .
```

## 使い方

```bash
# 2ファイルを比較
duel <file1> <file2>

# git diff をパイプで渡す
git diff | duel
```

## キー操作

| キー | 動作 |
|------|------|
| `j` / `↓` | 下にスクロール |
| `k` / `↑` | 上にスクロール |
| `h` / `←` | 左にスクロール |
| `l` / `→` | 右にスクロール |
| `0` | 横スクロールをリセット |
| `Ctrl+F` / `Ctrl+B` | ページダウン / ページアップ |
| `g` / `G` | 先頭 / 末尾へ移動 |
| `n` / `N` | 次 / 前の変更行へ移動 |
| `]c` / `[c` | 次 / 前のハンクへジャンプ |
| `]f` / `[f` | 次 / 前のファイルへジャンプ（複数ファイル差分時） |
| `/` | 検索 |
| `w` | 折り返しトグル |
| `I` | 空白無視モードトグル |
| `e` / `E` | カーソル位置の折りたたみ展開 / 全展開 |
| `C` | 全折りたたみ |
| `+` / `-` | コンテキスト行数を増減 |
| `>` | 現在のハンクを右ファイルに適用（2ファイルモードのみ） |
| `<` | 現在のハンクを左ファイルに適用（2ファイルモードのみ） |
| `u` | 直前の適用を元に戻す（2ファイルモードのみ） |
| `r` | 差分を再読み込み（2ファイルモードのみ） |
| `?` | ヘルプ表示トグル |
| `q` / `Ctrl+C` | 終了 |

## 依存

- [bubbletea](https://github.com/charmbracelet/bubbletea)
- [lipgloss](https://github.com/charmbracelet/lipgloss)
- [go-diff](https://github.com/sergi/go-diff)
