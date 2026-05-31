# duel

ターミナルで動くサイドバイサイドのdiffビューア。

## インストール

```bash
go install github.com/yourusername/duel@latest
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
| `>` | 現在のハンクを右ファイルに適用 |
| `<` | 現在のハンクを左ファイルに適用 |
| `q` / `Ctrl+C` | 終了 |

## 依存

- [bubbletea](https://github.com/charmbracelet/bubbletea)
- [lipgloss](https://github.com/charmbracelet/lipgloss)
- [go-diff](https://github.com/sergi/go-diff)
