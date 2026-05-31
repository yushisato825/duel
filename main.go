package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sergi/go-diff/diffmatchpatch"
)

// --- スタイル定義 ---
var (
	addedStyle         = lipgloss.NewStyle().Background(lipgloss.Color("22")).Foreground(lipgloss.Color("255"))   // 追加: 濃い緑
	removedStyle       = lipgloss.NewStyle().Background(lipgloss.Color("52")).Foreground(lipgloss.Color("255"))   // 削除: 濃い赤
	changedStyle       = lipgloss.NewStyle().Background(lipgloss.Color("18")).Foreground(lipgloss.Color("255"))   // 変更: 濃い青
	changedInlineStyle = lipgloss.NewStyle().Background(lipgloss.Color("27")).Foreground(lipgloss.Color("255")).Bold(true) // 変更内強調: 明るい青
	addedInlineStyle   = lipgloss.NewStyle().Background(lipgloss.Color("34")).Foreground(lipgloss.Color("255")).Bold(true)  // 追加箇所強調: 明るい緑
	removedInlineStyle = lipgloss.NewStyle().Background(lipgloss.Color("160")).Foreground(lipgloss.Color("255")).Bold(true) // 削除箇所強調: 明るい赤
	addedPadStyle      = lipgloss.NewStyle().Background(lipgloss.Color("236"))                                    // 追加パディング: グレー
	removedPadStyle    = lipgloss.NewStyle().Background(lipgloss.Color("236"))                                    // 削除パディング: グレー
	changedPadStyle    = lipgloss.NewStyle().Background(lipgloss.Color("18")).Foreground(lipgloss.Color("18"))   // 変更パディング（未使用予定）
	equalStyle         = lipgloss.NewStyle()
	headerStyle        = lipgloss.NewStyle().Bold(true).Background(lipgloss.Color("237")).Foreground(lipgloss.Color("250"))
	lineNumStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Width(4).Align(lipgloss.Right)
	scrollStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	helpStyle          = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	dividerStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("237"))
	statusOkStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("46")).Bold(true)
	statusErrStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)
	gutterEq           = lipgloss.NewStyle().Foreground(lipgloss.Color("237")).Render("│")
	gutterChanged      = lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Bold(true).Render("⟷")
	gutterRemoved      = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true).Render("›")
	gutterAdded        = lipgloss.NewStyle().Foreground(lipgloss.Color("46")).Bold(true).Render("‹")
)

// --- データ型 ---
type lineKind int

const (
	kindEqual   lineKind = iota
	kindChanged          // 削除行(左) + 追加行(右) のペア
	kindRemoved          // 削除のみ（追加より多い分）
	kindAdded            // 追加のみ（削除より多い分）
	kindPad              // 行数を揃えるためのパディング行
)

type diffLine struct {
	left     string
	right    string
	leftNum  int
	rightNum int
	kind     lineKind
	padSide  int // kindPad のとき: -1=左がパディング, 1=右がパディング
}

// --- Teaメッセージ ---
type diffUpdatedMsg struct {
	lines      []diffLine
	diffBlocks int
}
type statusMsg struct {
	text  string
	isErr bool
}

// --- Model ---
type model struct {
	lines      []diffLine
	offset     int
	hOffset    int
	width      int
	height     int
	leftFile   string
	rightFile  string
	diffBlocks int
	editable   bool   // ファイルモード時のみtrue（パイプ入力では編集不可）
	status     string // ステータスメッセージ
	statusErr  bool
}

func (m model) visibleLines() int {
	return m.height - 3
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case diffUpdatedMsg:
		m.lines = msg.lines
		m.diffBlocks = msg.diffBlocks
		m.offset = clamp(m.offset, 0, max(0, len(m.lines)-m.visibleLines()))
		m.status = "適用しました"
		m.statusErr = false

	case statusMsg:
		m.status = msg.text
		m.statusErr = msg.isErr

	case tea.KeyMsg:
		m.status = "" // キー入力でステータスをクリア
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "down", "j":
			m.offset = clamp(m.offset+1, 0, max(0, len(m.lines)-m.visibleLines()))
		case "up", "k":
			m.offset = clamp(m.offset-1, 0, max(0, len(m.lines)-m.visibleLines()))
		case "pgdown", "ctrl+f":
			m.offset = clamp(m.offset+m.visibleLines(), 0, max(0, len(m.lines)-m.visibleLines()))
		case "pgup", "ctrl+b":
			m.offset = clamp(m.offset-m.visibleLines(), 0, max(0, len(m.lines)-m.visibleLines()))
		case "g":
			m.offset = 0
		case "G":
			m.offset = max(0, len(m.lines)-m.visibleLines())
		case "n":
			m.offset = nextChange(m.lines, m.offset)
		case "N":
			m.offset = prevChange(m.lines, m.offset)
		case "right", "l":
			m.hOffset += 4
		case "left", "h":
			m.hOffset = max(0, m.hOffset-4)
		case "0":
			m.hOffset = 0
		case ">":
			if m.editable {
				return m, applyChange(m, 1) // 左→右
			}
		case "<":
			if m.editable {
				return m, applyChange(m, -1) // 右→左
			}
		}
	}
	return m, nil
}

func (m model) View() string {
	if m.width == 0 {
		return ""
	}

	lineNumW := 4
	panelW := (m.width - 1) / 2
	contentW := panelW - lineNumW - 1

	// ヘッダー
	diffInfo := fmt.Sprintf(" %d differences ", m.diffBlocks)
	leftHeader := headerStyle.Width(panelW).Render(" " + m.leftFile)
	rightHeaderText := m.rightFile + strings.Repeat(" ", max(0, panelW-len(m.rightFile)-len(diffInfo)-1)) + diffInfo
	rightHeader := headerStyle.Width(panelW).Render(" " + rightHeaderText)
	header := lipgloss.JoinHorizontal(lipgloss.Top, leftHeader, dividerStyle.Render("┃"), rightHeader)

	// 差分行のレンダリング
	visible := m.visibleLines()
	end := min(m.offset+visible, len(m.lines))
	var rows []string
	for i, dl := range m.lines[m.offset:end] {
		absIdx := m.offset + i
		isBlockStart := absIdx == 0 || m.lines[absIdx-1].kind == kindEqual
		leftNumStr := "    "
		rightNumStr := "    "
		if dl.leftNum > 0 {
			leftNumStr = fmt.Sprintf("%4d", dl.leftNum)
		}
		if dl.rightNum > 0 {
			rightNumStr = fmt.Sprintf("%4d", dl.rightNum)
		}

		leftText := hscroll(dl.left, m.hOffset, contentW)
		rightText := hscroll(dl.right, m.hOffset, contentW)

		// 行の種類ごとに背景・内容・ガターを決定
		// レイアウト: [content][leftGutter][linenum]│[linenum][rightGutter][content]
		var leftCell, rightCell string
		var leftBg, rightBg lipgloss.Style
		var leftGutter, rightGutter string

		switch dl.kind {
		case kindChanged:
			leftBg, rightBg = changedStyle, changedStyle
			leftSlice := hscroll(dl.left, m.hOffset, contentW)
			rightSlice := hscroll(dl.right, m.hOffset, contentW)
			leftCell, rightCell = renderInlineDiff(leftSlice, rightSlice, leftBg, rightBg, changedInlineStyle, changedInlineStyle, contentW)
			leftGutter = leftBg.Copy().Foreground(lipgloss.Color("214")).Bold(true).Render("›")
			rightGutter = rightBg.Copy().Foreground(lipgloss.Color("214")).Bold(true).Render("‹")
		case kindRemoved:
			leftBg, rightBg = removedStyle, removedPadStyle
			leftCell = leftBg.Width(contentW).Render(leftText)
			rightCell = rightBg.Width(contentW).Render("")
			rightNumStr = "    "
			leftGutter = leftBg.Copy().Foreground(lipgloss.Color("196")).Bold(true).Render("›")
			rightGutter = rightBg.Render(" ")
		case kindAdded:
			leftBg, rightBg = addedPadStyle, addedStyle
			leftCell = leftBg.Width(contentW).Render("")
			rightCell = rightBg.Width(contentW).Render(rightText)
			leftNumStr = "    "
			leftGutter = leftBg.Render(" ")
			rightGutter = rightBg.Copy().Foreground(lipgloss.Color("46")).Bold(true).Render("‹")
		case kindPad:
			if dl.padSide == -1 {
				leftBg, rightBg = addedPadStyle, addedStyle
				leftCell = leftBg.Width(contentW).Render("")
				rightCell = rightBg.Width(contentW).Render(rightText)
				leftNumStr = "    "
				leftGutter = leftBg.Render(" ")
				rightGutter = rightBg.Copy().Foreground(lipgloss.Color("46")).Bold(true).Render("‹")
			} else {
				leftBg, rightBg = removedStyle, removedPadStyle
				leftCell = leftBg.Width(contentW).Render(leftText)
				rightCell = rightBg.Width(contentW).Render("")
				rightNumStr = "    "
				leftGutter = leftBg.Copy().Foreground(lipgloss.Color("196")).Bold(true).Render("›")
				rightGutter = rightBg.Render(" ")
			}
		default:
			leftBg, rightBg = equalStyle, equalStyle
			leftCell = leftBg.Width(contentW).Render(leftText)
			rightCell = rightBg.Width(contentW).Render(rightText)
			leftGutter = " "
			rightGutter = " "
		}

		// ブロックの先頭行以外はガターをスペースにする
		if !isBlockStart {
			leftGutter = leftBg.Render(" ")
			rightGutter = rightBg.Render(" ")
		}

		dimLeft := leftBg.Copy().Foreground(lipgloss.Color("241")).Width(4).Align(lipgloss.Right)
		dimRight := rightBg.Copy().Foreground(lipgloss.Color("241")).Width(4).Align(lipgloss.Right)
		leftSide := leftCell + leftGutter + dimLeft.Render(leftNumStr)
		rightSide := dimRight.Render(rightNumStr) + rightGutter + rightCell
		rows = append(rows, leftSide+dividerStyle.Render("│")+rightSide)
	}

	for len(rows) < visible {
		rows = append(rows, strings.Repeat(" ", m.width))
	}

	// ヘルプバー
	total := len(m.lines)
	scrollInfo := ""
	if total > 0 {
		pct := min((m.offset+visible)*100/total, 100)
		scrollInfo = fmt.Sprintf(" %d/%d (%d%%)", m.offset+1, total, pct)
	}
	if m.hOffset > 0 {
		scrollInfo += fmt.Sprintf("  ←→:%d", m.hOffset)
	}

	editHelp := ""
	if m.editable {
		editHelp = "  >/<: 取り込み"
	}
	helpText := helpStyle.Render("↑↓/k/j: 縦  ←→/h/l: 横  n/N: 変更箇所  g/G: 先頭/末尾"+editHelp+"  q: 終了") +
		scrollStyle.Render(scrollInfo)

	// ステータスメッセージ（あれば末尾に追記）
	if m.status != "" {
		s := m.status
		rendered := statusOkStyle.Render(s)
		if m.statusErr {
			rendered = statusErrStyle.Render(s)
		}
		helpText += "  " + rendered
	}

	return lipgloss.JoinVertical(lipgloss.Left,
		header,
		strings.Join(rows, "\n"),
		helpText,
	)
}

// --- ハンク操作 ---

// findCurrentHunk はカーソル行が属するハンクの範囲を返す。
// カーソルが equal 行にある場合は直後の変更ブロックを探す。
func findCurrentHunk(lines []diffLine, offset int) (start, end int, ok bool) {
	pivot := offset
	if offset < len(lines) && lines[offset].kind == kindEqual {
		pivot = -1
		for i := offset; i < len(lines); i++ {
			if lines[i].kind != kindEqual {
				pivot = i
				break
			}
		}
		if pivot == -1 {
			return 0, 0, false
		}
	}

	start = pivot
	for start > 0 && lines[start-1].kind != kindEqual {
		start--
	}
	end = pivot + 1
	for end < len(lines) && lines[end].kind != kindEqual {
		end++
	}
	return start, end, true
}

// applyChange はハンクをファイルに適用するコマンドを返す。
// dir=1: 左→右（右ファイルを書き換え）, dir=-1: 右→左（左ファイルを書き換え）
func applyChange(m model, dir int) tea.Cmd {
	return func() tea.Msg {
		start, end, ok := findCurrentHunk(m.lines, m.offset)
		if !ok {
			return statusMsg{"変更ブロックが見つかりません", true}
		}

		hunk := m.lines[start:end]

		var leftContent, rightContent []string
		var leftNums, rightNums []int
		for _, dl := range hunk {
			if dl.leftNum > 0 {
				leftContent = append(leftContent, dl.left)
				leftNums = append(leftNums, dl.leftNum)
			}
			if dl.rightNum > 0 {
				rightContent = append(rightContent, dl.right)
				rightNums = append(rightNums, dl.rightNum)
			}
		}

		// equal 行の直前のアンカー（挿入位置用）
		var leftAnchor, rightAnchor int
		if start > 0 && m.lines[start-1].kind == kindEqual {
			leftAnchor = m.lines[start-1].leftNum
			rightAnchor = m.lines[start-1].rightNum
		}

		var err error
		if dir == 1 {
			// 左→右: 右ファイルを書き換え
			if len(rightNums) > 0 {
				err = replaceFileLines(m.rightFile, rightNums[0], rightNums[len(rightNums)-1], leftContent)
			} else {
				err = insertFileLines(m.rightFile, rightAnchor, leftContent)
			}
		} else {
			// 右→左: 左ファイルを書き換え
			if len(leftNums) > 0 {
				err = replaceFileLines(m.leftFile, leftNums[0], leftNums[len(leftNums)-1], rightContent)
			} else {
				err = insertFileLines(m.leftFile, leftAnchor, rightContent)
			}
		}
		if err != nil {
			return statusMsg{err.Error(), true}
		}

		// diff を再計算
		leftLines, err := readLines(m.leftFile)
		if err != nil {
			return statusMsg{err.Error(), true}
		}
		rightLines, err := readLines(m.rightFile)
		if err != nil {
			return statusMsg{err.Error(), true}
		}
		newLines := computeDiff(leftLines, rightLines)
		return diffUpdatedMsg{lines: newLines, diffBlocks: countDiffBlocks(newLines)}
	}
}

// replaceFileLines はファイルの startLine〜endLine（1始まり、両端含む）を newContent で置き換える
func replaceFileLines(path string, startLine, endLine int, newContent []string) error {
	lines, err := readLines(path)
	if err != nil {
		return err
	}
	var result []string
	result = append(result, lines[:startLine-1]...)
	result = append(result, newContent...)
	if endLine < len(lines) {
		result = append(result, lines[endLine:]...)
	}
	return writeLines(path, result)
}

// insertFileLines はファイルの afterLine 行目の直後に newContent を挿入する（afterLine=0 は先頭）
func insertFileLines(path string, afterLine int, newContent []string) error {
	lines, err := readLines(path)
	if err != nil {
		return err
	}
	var result []string
	result = append(result, lines[:afterLine]...)
	result = append(result, newContent...)
	result = append(result, lines[afterLine:]...)
	return writeLines(path, result)
}

func writeLines(path string, lines []string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	for i, line := range lines {
		if i > 0 {
			w.WriteString("\n")
		}
		w.WriteString(line)
	}
	return w.Flush()
}

// --- 差分計算 ---
func computeDiff(leftLines, rightLines []string) []diffLine {
	dmp := diffmatchpatch.New()
	leftText := strings.Join(leftLines, "\n")
	rightText := strings.Join(rightLines, "\n")

	lineText1, lineText2, lineArray := dmp.DiffLinesToChars(leftText, rightText)
	diffs := dmp.DiffMain(lineText1, lineText2, false)
	diffs = dmp.DiffCharsToLines(diffs, lineArray)

	return alignRaw(diffs)
}

func alignRaw(diffs []diffmatchpatch.Diff) []diffLine {
	var result []diffLine
	leftNum := 0
	rightNum := 0

	i := 0
	for i < len(diffs) {
		d := diffs[i]
		lines := splitLines(d.Text)

		if d.Type == diffmatchpatch.DiffEqual {
			for _, l := range lines {
				leftNum++
				rightNum++
				result = append(result, diffLine{
					left: l, right: l,
					leftNum: leftNum, rightNum: rightNum,
					kind: kindEqual,
				})
			}
			i++
			continue
		}

		var removed, added []string
		var removedNums, addedNums []int

		for i < len(diffs) && diffs[i].Type == diffmatchpatch.DiffDelete {
			for _, l := range splitLines(diffs[i].Text) {
				leftNum++
				removed = append(removed, l)
				removedNums = append(removedNums, leftNum)
			}
			i++
		}
		for i < len(diffs) && diffs[i].Type == diffmatchpatch.DiffInsert {
			for _, l := range splitLines(diffs[i].Text) {
				rightNum++
				added = append(added, l)
				addedNums = append(addedNums, rightNum)
			}
			i++
		}

		maxLen := max(len(removed), len(added))
		for j := 0; j < maxLen; j++ {
			dl := diffLine{}
			hasLeft := j < len(removed)
			hasRight := j < len(added)
			switch {
			case hasLeft && hasRight:
				dl = diffLine{kind: kindChanged,
					left: removed[j], right: added[j],
					leftNum: removedNums[j], rightNum: addedNums[j]}
			case hasLeft:
				dl = diffLine{kind: kindRemoved,
					left: removed[j], leftNum: removedNums[j]}
			case hasRight:
				dl = diffLine{kind: kindAdded,
					right: added[j], rightNum: addedNums[j]}
			}
			result = append(result, dl)
		}
	}
	return result
}

func splitLines(s string) []string {
	lines := strings.Split(s, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// --- パイプ入力パース ---
func parseUnifiedDiff(r io.Reader) ([]diffLine, string, string) {
	scanner := bufio.NewScanner(r)
	var leftLines, rightLines []string
	leftFile, rightFile := "before", "after"

	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "--- "):
			leftFile = strings.TrimPrefix(strings.TrimPrefix(line, "--- "), "a/")
		case strings.HasPrefix(line, "+++ "):
			rightFile = strings.TrimPrefix(strings.TrimPrefix(line, "+++ "), "b/")
		case strings.HasPrefix(line, "@@"):
			// skip
		case strings.HasPrefix(line, "-"):
			leftLines = append(leftLines, line[1:])
		case strings.HasPrefix(line, "+"):
			rightLines = append(rightLines, line[1:])
		case strings.HasPrefix(line, " "):
			leftLines = append(leftLines, line[1:])
			rightLines = append(rightLines, line[1:])
		}
	}
	return computeDiff(leftLines, rightLines), leftFile, rightFile
}

func readLines(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return lines, scanner.Err()
}

// --- インライン差分レンダリング ---
func renderInlineDiff(leftText, rightText string, baseLeft, baseRight, hlDel, hlIns lipgloss.Style, contentW int) (string, string) {
	dmp := diffmatchpatch.New()
	diffs := dmp.DiffMain(leftText, rightText, false)
	dmp.DiffCleanupSemantic(diffs)

	var leftBuf, rightBuf strings.Builder

	for _, d := range diffs {
		switch d.Type {
		case diffmatchpatch.DiffEqual:
			leftBuf.WriteString(baseLeft.Render(d.Text))
			rightBuf.WriteString(baseRight.Render(d.Text))
		case diffmatchpatch.DiffDelete:
			leftBuf.WriteString(hlDel.Render(d.Text))
		case diffmatchpatch.DiffInsert:
			rightBuf.WriteString(hlIns.Render(d.Text))
		}
	}

	// ANSI コードを含む実際の表示幅を測ってパディング
	left := leftBuf.String()
	right := rightBuf.String()
	if pad := contentW - lipgloss.Width(left); pad > 0 {
		left += baseLeft.Render(strings.Repeat(" ", pad))
	}
	if pad := contentW - lipgloss.Width(right); pad > 0 {
		right += baseRight.Render(strings.Repeat(" ", pad))
	}

	return left, right
}

// --- ユーティリティ ---
func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func hscroll(s string, offset, w int) string {
	if w <= 0 {
		return ""
	}
	runes := []rune(s)
	if offset >= len(runes) {
		return ""
	}
	runes = runes[offset:]
	if len(runes) <= w {
		return string(runes)
	}
	return string(runes[:w-1]) + "…"
}

func countDiffBlocks(lines []diffLine) int {
	count := 0
	inBlock := false
	for _, l := range lines {
		if l.kind != kindEqual {
			if !inBlock {
				count++
				inBlock = true
			}
		} else {
			inBlock = false
		}
	}
	return count
}

func nextChange(lines []diffLine, from int) int {
	for i := from + 1; i < len(lines); i++ {
		if lines[i].kind != kindEqual {
			return i
		}
	}
	return from
}

func prevChange(lines []diffLine, from int) int {
	for i := from - 1; i >= 0; i-- {
		if lines[i].kind != kindEqual {
			return i
		}
	}
	return from
}

// --- エントリポイント ---
func main() {
	var diffLines []diffLine
	var leftFile, rightFile string
	editable := false

	stat, _ := os.Stdin.Stat()
	isPipe := (stat.Mode() & os.ModeCharDevice) == 0

	if isPipe {
		diffLines, leftFile, rightFile = parseUnifiedDiff(os.Stdin)
	} else if len(os.Args) == 3 {
		leftFile = os.Args[1]
		rightFile = os.Args[2]
		leftLines, err := readLines(leftFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "エラー: %v\n", err)
			os.Exit(1)
		}
		rightLines, err := readLines(rightFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "エラー: %v\n", err)
			os.Exit(1)
		}
		diffLines = computeDiff(leftLines, rightLines)
		editable = true
	} else {
		fmt.Fprintln(os.Stderr, "使い方:")
		fmt.Fprintln(os.Stderr, "  duel <file1> <file2>")
		fmt.Fprintln(os.Stderr, "  git diff | duel")
		os.Exit(1)
	}

	m := model{
		lines:      diffLines,
		leftFile:   leftFile,
		rightFile:  rightFile,
		diffBlocks: countDiffBlocks(diffLines),
		editable:   editable,
	}

	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "エラー: %v\n", err)
		os.Exit(1)
	}
}
