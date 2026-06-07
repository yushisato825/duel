package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	runewidth "github.com/mattn/go-runewidth"
	"github.com/sergi/go-diff/diffmatchpatch"
)

// --- スタイル定義 ---
var (
	addedStyle          = lipgloss.NewStyle().Background(lipgloss.Color("22")).Foreground(lipgloss.Color("255"))             // 追加: 濃い緑
	removedStyle        = lipgloss.NewStyle().Background(lipgloss.Color("52")).Foreground(lipgloss.Color("255"))             // 削除: 濃い赤
	changedStyle        = lipgloss.NewStyle().Background(lipgloss.Color("18")).Foreground(lipgloss.Color("255"))             // 変更: 濃い青
	changedInlineStyle  = lipgloss.NewStyle().Background(lipgloss.Color("27")).Foreground(lipgloss.Color("255")).Bold(true)  // 変更内強調: 明るい青
	addedInlineStyle    = lipgloss.NewStyle().Background(lipgloss.Color("34")).Foreground(lipgloss.Color("255")).Bold(true)  // 追加箇所強調: 明るい緑
	removedInlineStyle  = lipgloss.NewStyle().Background(lipgloss.Color("160")).Foreground(lipgloss.Color("255")).Bold(true) // 削除箇所強調: 明るい赤
	addedPadStyle       = lipgloss.NewStyle().Background(lipgloss.Color("236"))                                              // 追加パディング: グレー
	removedPadStyle     = lipgloss.NewStyle().Background(lipgloss.Color("236"))                                              // 削除パディング: グレー
	changedPadStyle     = lipgloss.NewStyle().Background(lipgloss.Color("18")).Foreground(lipgloss.Color("18"))              // 変更パディング（未使用予定）
	equalStyle          = lipgloss.NewStyle()
	headerStyle         = lipgloss.NewStyle().Bold(true).Background(lipgloss.Color("237")).Foreground(lipgloss.Color("250"))
	lineNumStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Width(4).Align(lipgloss.Right)
	scrollStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	helpStyle           = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	dividerStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("237"))
	fileHeaderStyle     = lipgloss.NewStyle().Background(lipgloss.Color("239")).Foreground(lipgloss.Color("255")).Bold(true)
	fileHeaderRuleStyle = lipgloss.NewStyle().Background(lipgloss.Color("239")).Foreground(lipgloss.Color("241"))
	collapsedStyle      = lipgloss.NewStyle().Background(lipgloss.Color("235")).Foreground(lipgloss.Color("244"))
	statusOkStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("46")).Bold(true)
	statusErrStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)
	gutterEq            = lipgloss.NewStyle().Foreground(lipgloss.Color("237")).Render("│")
	gutterChanged       = lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Bold(true).Render("⟷")
	gutterRemoved       = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true).Render("›")
	gutterAdded         = lipgloss.NewStyle().Foreground(lipgloss.Color("46")).Bold(true).Render("‹")
	searchBarStyle      = lipgloss.NewStyle().Background(lipgloss.Color("237")).Foreground(lipgloss.Color("255"))
	searchPromptStyle   = lipgloss.NewStyle().Background(lipgloss.Color("237")).Foreground(lipgloss.Color("220")).Bold(true)
	searchMatchStyle    = lipgloss.NewStyle().Background(lipgloss.Color("220")).Foreground(lipgloss.Color("0")).Bold(true)
)

// --- データ型 ---
type lineKind int

const (
	kindEqual      lineKind = iota
	kindChanged             // 削除行(左) + 追加行(右) のペア
	kindRemoved             // 削除のみ（追加より多い分）
	kindAdded               // 追加のみ（削除より多い分）
	kindPad                 // 行数を揃えるためのパディング行
	kindFileHeader          // ファイル区切り行
	kindCollapsed           // 折りたたまれた等号行群
)

const defaultContext = 3 // 差分前後に表示するコンテキスト行数のデフォルト値

type diffLine struct {
	left           string
	right          string
	leftNum        int
	rightNum       int
	kind           lineKind
	padSide        int        // kindPad のとき: -1=左がパディング, 1=右がパディング
	collapsedLines []diffLine // kindCollapsed のとき: 折りたたまれた行
}

// --- Teaメッセージ ---
type diffUpdatedMsg struct {
	lines       []diffLine
	diffBlocks  int
	hasBackup   bool
	leftBackup  []string
	rightBackup []string
	isUndo      bool
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
	editable    bool   // ファイルモード時のみtrue（パイプ入力では編集不可）
	status      string // ステータスメッセージ
	statusErr   bool
	canUndo     bool
	leftBackup  []string
	rightBackup []string
	cursor        int
	context       int
	showHelp      bool
	pendingKey    string
	searching     bool
	searchQuery   string
	searchMatches []int
	searchIdx     int
}

func (m model) footerHeight() int {
	if m.searching || !m.showHelp || m.width == 0 {
		return 1
	}
	return len(wrapHelpItems(allHelpItems(m.editable), m.width))
}

func (m model) visibleLines() int {
	return m.height - 2 - m.footerHeight()
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
		m.lines = foldLines(msg.lines, m.context)
		m.diffBlocks = msg.diffBlocks
		m.cursor = clamp(m.cursor, 0, max(0, len(m.lines)-1))
		m.offset = clamp(m.offset, 0, max(0, len(m.lines)-m.visibleLines()))
		m.statusErr = false
		m.searchMatches = nil
		m.searchIdx = 0
		if msg.isUndo {
			m.status = "元に戻しました"
			m.canUndo = false
			m.leftBackup = nil
			m.rightBackup = nil
		} else {
			m.status = "適用しました"
			if msg.hasBackup {
				m.leftBackup = msg.leftBackup
				m.rightBackup = msg.rightBackup
				m.canUndo = true
			}
		}

	case statusMsg:
		m.status = msg.text
		m.statusErr = msg.isErr

	case tea.KeyMsg:
		if m.searching {
			switch msg.String() {
			case "enter":
				m.searching = false
				if len(m.searchMatches) > 0 {
					m = setCursor(m, m.searchMatches[m.searchIdx])
				}
			case "esc", "ctrl+c":
				m.searching = false
				m.searchQuery = ""
				m.searchMatches = nil
				m.searchIdx = 0
				m = setCursor(m, m.cursor)
			case "backspace", "ctrl+h":
				if len(m.searchQuery) > 0 {
					runes := []rune(m.searchQuery)
					m.searchQuery = string(runes[:len(runes)-1])
					m.searchMatches = findSearchMatches(m.lines, m.searchQuery)
					m.searchIdx = 0
					if len(m.searchMatches) > 0 {
						m = setCursor(m, m.searchMatches[0])
					}
				}
			default:
				if len(msg.Runes) > 0 {
					m.searchQuery += string(msg.Runes)
					m.searchMatches = findSearchMatches(m.lines, m.searchQuery)
					m.searchIdx = 0
					if len(m.searchMatches) > 0 {
						m = setCursor(m, m.searchMatches[0])
					}
				}
			}
			return m, nil
		}
		if m.pendingKey != "" {
			prev := m.pendingKey
			m.pendingKey = ""
			switch prev + msg.String() {
			case "]f":
				m = setCursor(m, nextFileHeader(m.lines, m.cursor))
			case "[f":
				m = setCursor(m, prevFileHeader(m.lines, m.cursor))
			}
			return m, nil
		}
		m.status = "" // キー入力でステータスをクリア
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "down", "j":
			m = setCursor(m, m.cursor+1)
		case "up", "k":
			m = setCursor(m, m.cursor-1)
		case "pgdown", "ctrl+f":
			m = setCursor(m, m.cursor+m.visibleLines())
		case "pgup", "ctrl+b":
			m = setCursor(m, m.cursor-m.visibleLines())
		case "g":
			m = setCursor(m, 0)
		case "G":
			m = setCursor(m, len(m.lines)-1)
		case "n":
			if len(m.searchMatches) > 0 {
				m.searchIdx = (m.searchIdx + 1) % len(m.searchMatches)
				m = setCursor(m, m.searchMatches[m.searchIdx])
			} else {
				m = setCursor(m, nextChange(m.lines, m.cursor))
			}
		case "N":
			if len(m.searchMatches) > 0 {
				m.searchIdx = (m.searchIdx - 1 + len(m.searchMatches)) % len(m.searchMatches)
				m = setCursor(m, m.searchMatches[m.searchIdx])
			} else {
				m = setCursor(m, prevChange(m.lines, m.cursor))
			}
		case "right", "l":
			m.hOffset += 4
		case "left", "h":
			m.hOffset = max(0, m.hOffset-4)
		case "0":
			m.hOffset = 0
		case "/":
			m.searching = true
			m.searchQuery = ""
			m.searchMatches = nil
			m.searchIdx = 0
		case "]", "[":
			m.pendingKey = msg.String()
			return m, nil
		case "?":
			m.showHelp = !m.showHelp
			m = setCursor(m, m.cursor)
		case "e":
			m.lines = expandCollapsed(m.lines, m.cursor)
			m = setCursor(m, m.cursor)
		case "E":
			m.lines = expandAll(m.lines)
			m = setCursor(m, m.cursor)
		case "C":
			m.lines = foldLines(expandAll(m.lines), m.context)
			m = setCursor(m, m.cursor)
		case "+":
			m.context++
			m.lines = foldLines(expandAll(m.lines), m.context)
			m = setCursor(m, m.cursor)
		case "-":
			if m.context > 0 {
				m.context--
			}
			m.lines = foldLines(expandAll(m.lines), m.context)
			m = setCursor(m, m.cursor)
		case "u":
			if m.editable && m.canUndo {
				return m, undoChange(m)
			} else if m.editable {
				m.status = "元に戻す操作はありません"
				m.statusErr = true
			}
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
		isCursor := absIdx == m.cursor
		if dl.kind == kindFileHeader {
			label := dl.left
			if dl.left == "/dev/null" {
				label = dl.right
			} else if dl.right != "" && dl.right != dl.left {
				label = dl.left + " → " + dl.right
			}
			prefix := "  ▸ "
			hStyle := fileHeaderStyle
			if isCursor {
				prefix = "  ▶ "
				hStyle = fileHeaderStyle.Copy().Background(lipgloss.Color("238"))
			}
			rulePad := max(0, m.width-lipgloss.Width(prefix)-lipgloss.Width(label)-1)
			rule := " " + strings.Repeat("─", rulePad)
			rows = append(rows, hStyle.Render(prefix+label)+fileHeaderRuleStyle.Render(rule))
			continue
		}
		if dl.kind == kindCollapsed {
			count := len(dl.collapsedLines)
			text := fmt.Sprintf("  ⋯  %d 行  (e で展開)", count)
			cStyle := collapsedStyle
			if isCursor {
				cStyle = collapsedStyle.Copy().Background(lipgloss.Color("238"))
			}
			rows = append(rows, cStyle.Width(m.width).Render(text))
			continue
		}
		isBlockStart := absIdx == 0 || m.lines[absIdx-1].kind == kindEqual || m.lines[absIdx-1].kind == kindFileHeader || m.lines[absIdx-1].kind == kindCollapsed
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
			leftCell, rightCell = renderInlineDiff(leftSlice, rightSlice, leftBg, rightBg, changedInlineStyle, changedInlineStyle, contentW, m.searchQuery)
			leftGutter = leftBg.Copy().Foreground(lipgloss.Color("214")).Bold(true).Render("›")
			rightGutter = rightBg.Copy().Foreground(lipgloss.Color("214")).Bold(true).Render("‹")
		case kindRemoved:
			leftBg, rightBg = removedStyle, removedPadStyle
			leftCell = renderWithSearch(leftText, m.searchQuery, leftBg, searchMatchStyle, contentW)
			rightCell = rightBg.Width(contentW).Render("")
			rightNumStr = "    "
			leftGutter = leftBg.Copy().Foreground(lipgloss.Color("196")).Bold(true).Render("›")
			rightGutter = rightBg.Render(" ")
		case kindAdded:
			leftBg, rightBg = addedPadStyle, addedStyle
			leftCell = leftBg.Width(contentW).Render("")
			rightCell = renderWithSearch(rightText, m.searchQuery, rightBg, searchMatchStyle, contentW)
			leftNumStr = "    "
			leftGutter = leftBg.Render(" ")
			rightGutter = rightBg.Copy().Foreground(lipgloss.Color("46")).Bold(true).Render("‹")
		case kindPad:
			if dl.padSide == -1 {
				leftBg, rightBg = addedPadStyle, addedStyle
				leftCell = leftBg.Width(contentW).Render("")
				rightCell = renderWithSearch(rightText, m.searchQuery, rightBg, searchMatchStyle, contentW)
				leftNumStr = "    "
				leftGutter = leftBg.Render(" ")
				rightGutter = rightBg.Copy().Foreground(lipgloss.Color("46")).Bold(true).Render("‹")
			} else {
				leftBg, rightBg = removedStyle, removedPadStyle
				leftCell = renderWithSearch(leftText, m.searchQuery, leftBg, searchMatchStyle, contentW)
				rightCell = rightBg.Width(contentW).Render("")
				rightNumStr = "    "
				leftGutter = leftBg.Copy().Foreground(lipgloss.Color("196")).Bold(true).Render("›")
				rightGutter = rightBg.Render(" ")
			}
		default:
			leftBg, rightBg = equalStyle, equalStyle
			leftCell = renderWithSearch(leftText, m.searchQuery, leftBg, searchMatchStyle, contentW)
			rightCell = renderWithSearch(rightText, m.searchQuery, rightBg, searchMatchStyle, contentW)
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
		if isCursor {
			dimLeft = leftBg.Copy().Foreground(lipgloss.Color("255")).Width(4).Align(lipgloss.Right)
			dimRight = rightBg.Copy().Foreground(lipgloss.Color("255")).Width(4).Align(lipgloss.Right)
			if dl.kind == kindEqual {
				leftGutter = lipgloss.NewStyle().Foreground(lipgloss.Color("51")).Bold(true).Render("▶")
			}
		}
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
	if m.diffBlocks > 0 {
		hunkNum := currentHunkNum(m.lines, m.cursor)
		scrollInfo = fmt.Sprintf(" hunk %d/%d  ctx:%d", hunkNum, m.diffBlocks, m.context)
	}
	if total > 0 {
		pct := min((m.offset+visible)*100/total, 100)
		scrollInfo += fmt.Sprintf("  %d/%d (%d%%)", m.offset+1, total, pct)
	}
	if m.hOffset > 0 {
		scrollInfo += fmt.Sprintf("  ←→:%d", m.hOffset)
	}

	var helpText string
	if m.searching {
		prompt := searchPromptStyle.Render("/")
		query := searchBarStyle.Render(m.searchQuery + "█")
		var matchInfo string
		if m.searchQuery != "" {
			if len(m.searchMatches) == 0 {
				matchInfo = searchBarStyle.Render("  [一致なし]")
			} else {
				matchInfo = searchBarStyle.Render(fmt.Sprintf("  [%d/%d]", m.searchIdx+1, len(m.searchMatches)))
			}
		}
		content := " " + prompt + " " + query + matchInfo
		if pad := m.width - lipgloss.Width(content); pad > 0 {
			content += searchBarStyle.Render(strings.Repeat(" ", pad))
		}
		helpText = content
	} else if m.showHelp {
		items := wrapHelpItems(allHelpItems(m.editable), m.width)
		var rendered []string
		for i, line := range items {
			s := helpStyle.Render(line)
			if i == len(items)-1 {
				s += scrollStyle.Render(scrollInfo)
			}
			rendered = append(rendered, s)
		}
		helpText = strings.Join(rendered, "\n")
	} else {
		nHelp := "n/N: 変更箇所"
		if len(m.searchMatches) > 0 {
			nHelp = "n/N: マッチ"
		}
		helpText = helpStyle.Render(nHelp+"  /: 検索  ?: ヘルプ  q: 終了") +
			scrollStyle.Render(scrollInfo)
		if m.pendingKey != "" {
			helpText += "  " + statusOkStyle.Render(m.pendingKey+"…")
		} else if m.status != "" {
			rendered := statusOkStyle.Render(m.status)
			if m.statusErr {
				rendered = statusErrStyle.Render(m.status)
			}
			helpText += "  " + rendered
		}
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
	isChange := func(k lineKind) bool { return !isContextKind(k) }

	pivot := offset
	if offset < len(lines) && !isChange(lines[offset].kind) {
		pivot = -1
		for i := offset; i < len(lines); i++ {
			if isChange(lines[i].kind) {
				pivot = i
				break
			}
		}
		if pivot == -1 {
			return 0, 0, false
		}
	}

	start = pivot
	for start > 0 && isChange(lines[start-1].kind) {
		start--
	}
	end = pivot + 1
	for end < len(lines) && isChange(lines[end].kind) {
		end++
	}
	return start, end, true
}

// applyChange はハンクをファイルに適用するコマンドを返す。
// dir=1: 左→右（右ファイルを書き換え）, dir=-1: 右→左（左ファイルを書き換え）
func applyChange(m model, dir int) tea.Cmd {
	return func() tea.Msg {
		start, end, ok := findCurrentHunk(m.lines, m.cursor)
		if !ok {
			return statusMsg{"変更ブロックが見つかりません", true}
		}

		leftBefore, err := readLines(m.leftFile)
		if err != nil {
			return statusMsg{err.Error(), true}
		}
		rightBefore, err := readLines(m.rightFile)
		if err != nil {
			return statusMsg{err.Error(), true}
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

		var applyErr error
		if dir == 1 {
			// 左→右: 右ファイルを書き換え
			if len(rightNums) > 0 {
				applyErr = replaceFileLines(m.rightFile, rightNums[0], rightNums[len(rightNums)-1], leftContent)
			} else {
				applyErr = insertFileLines(m.rightFile, rightAnchor, leftContent)
			}
		} else {
			// 右→左: 左ファイルを書き換え
			if len(leftNums) > 0 {
				applyErr = replaceFileLines(m.leftFile, leftNums[0], leftNums[len(leftNums)-1], rightContent)
			} else {
				applyErr = insertFileLines(m.leftFile, leftAnchor, rightContent)
			}
		}
		if applyErr != nil {
			return statusMsg{applyErr.Error(), true}
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
		return diffUpdatedMsg{
			lines:       newLines,
			diffBlocks:  countDiffBlocks(newLines),
			hasBackup:   true,
			leftBackup:  leftBefore,
			rightBackup: rightBefore,
		}
	}
}

func undoChange(m model) tea.Cmd {
	return func() tea.Msg {
		if err := writeLines(m.leftFile, m.leftBackup); err != nil {
			return statusMsg{err.Error(), true}
		}
		if err := writeLines(m.rightFile, m.rightBackup); err != nil {
			return statusMsg{err.Error(), true}
		}
		newLines := computeDiff(m.leftBackup, m.rightBackup)
		return diffUpdatedMsg{
			lines:      newLines,
			diffBlocks: countDiffBlocks(newLines),
			isUndo:     true,
		}
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

// foldLines は連続する kindEqual 行を差分前後 ctx 行だけ残して折りたたむ。
func foldLines(lines []diffLine, ctx int) []diffLine {
	n := len(lines)
	var result []diffLine
	seenDiff := false

	isDiff := func(k lineKind) bool {
		return k != kindEqual && k != kindFileHeader && k != kindCollapsed
	}

	for i := 0; i < n; {
		if lines[i].kind == kindFileHeader {
			seenDiff = false
			result = append(result, lines[i])
			i++
			continue
		}
		if isDiff(lines[i].kind) {
			seenDiff = true
			result = append(result, lines[i])
			i++
			continue
		}
		// kindEqual の連続範囲を特定
		j := i
		for j < n && lines[j].kind == kindEqual {
			j++
		}
		runLen := j - i

		// 後続に差分があるか確認
		hasDiffAfter := false
		for k := j; k < n; k++ {
			if isDiff(lines[k].kind) {
				hasDiffAfter = true
				break
			}
		}

		showBefore := 0
		if seenDiff {
			showBefore = min(ctx, runLen)
		}
		showAfter := 0
		if hasDiffAfter {
			showAfter = min(ctx, runLen-showBefore)
		}

		hiddenStart := i + showBefore
		hiddenEnd := j - showAfter

		result = append(result, lines[i:hiddenStart]...)
		if hiddenEnd > hiddenStart {
			result = append(result, diffLine{
				kind:           kindCollapsed,
				collapsedLines: append([]diffLine{}, lines[hiddenStart:hiddenEnd]...),
			})
		}
		result = append(result, lines[hiddenEnd:j]...)
		i = j
	}
	return result
}

func expandAll(lines []diffLine) []diffLine {
	var result []diffLine
	for _, dl := range lines {
		if dl.kind == kindCollapsed {
			result = append(result, dl.collapsedLines...)
		} else {
			result = append(result, dl)
		}
	}
	return result
}

// expandCollapsed はカーソル位置に最も近い kindCollapsed 行を展開する。
func expandCollapsed(lines []diffLine, offset int) []diffLine {
	idx := -1
	for i := offset; i < len(lines); i++ {
		if lines[i].kind == kindCollapsed {
			idx = i
			break
		}
	}
	if idx == -1 {
		for i := offset - 1; i >= 0; i-- {
			if lines[i].kind == kindCollapsed {
				idx = i
				break
			}
		}
	}
	if idx == -1 {
		return lines
	}
	var result []diffLine
	result = append(result, lines[:idx]...)
	result = append(result, lines[idx].collapsedLines...)
	result = append(result, lines[idx+1:]...)
	return result
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
	type fileSection struct {
		leftFile   string
		rightFile  string
		leftLines  []string
		rightLines []string
	}

	scanner := bufio.NewScanner(r)
	var sections []fileSection
	var cur fileSection
	inFile := false

	flush := func() {
		if inFile {
			sections = append(sections, cur)
			cur = fileSection{}
			inFile = false
		}
	}

	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "--- "):
			flush()
			name := strings.TrimPrefix(strings.TrimPrefix(line, "--- "), "a/")
			name = strings.SplitN(name, "\t", 2)[0]
			cur.leftFile = name
		case strings.HasPrefix(line, "+++ "):
			name := strings.TrimPrefix(strings.TrimPrefix(line, "+++ "), "b/")
			name = strings.SplitN(name, "\t", 2)[0]
			cur.rightFile = name
			inFile = true
		case strings.HasPrefix(line, "@@"):
			// skip
		case strings.HasPrefix(line, "-"):
			if inFile {
				cur.leftLines = append(cur.leftLines, line[1:])
			}
		case strings.HasPrefix(line, "+"):
			if inFile {
				cur.rightLines = append(cur.rightLines, line[1:])
			}
		case strings.HasPrefix(line, " "):
			if inFile {
				cur.leftLines = append(cur.leftLines, line[1:])
				cur.rightLines = append(cur.rightLines, line[1:])
			}
		}
	}
	flush()

	var allLines []diffLine
	var leftFileNames, rightFileNames []string

	for _, section := range sections {
		allLines = append(allLines, diffLine{kind: kindFileHeader, left: section.leftFile, right: section.rightFile})
		sectionLines := computeDiff(section.leftLines, section.rightLines)
		allLines = append(allLines, sectionLines...)
		leftFileNames = append(leftFileNames, section.leftFile)
		rightFileNames = append(rightFileNames, section.rightFile)
	}

	return allLines, diffFileLabel(leftFileNames, "before"), diffFileLabel(rightFileNames, "after")
}

func diffFileLabel(files []string, fallback string) string {
	seen := map[string]struct{}{}
	for _, f := range files {
		if f != "/dev/null" {
			seen[f] = struct{}{}
		}
	}
	switch len(seen) {
	case 0:
		return fallback
	case 1:
		for f := range seen {
			return f
		}
	}
	return fmt.Sprintf("%d files", len(seen))
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
func matchRanges(text, query string) [][2]int {
	if query == "" {
		return nil
	}
	lowerText := strings.ToLower(text)
	lowerQ := strings.ToLower(query)
	var ranges [][2]int
	i := 0
	for {
		j := strings.Index(lowerText[i:], lowerQ)
		if j == -1 {
			break
		}
		start := i + j
		end := start + len(lowerQ)
		ranges = append(ranges, [2]int{start, end})
		i = end
	}
	return ranges
}

func renderSegment(text string, pos int, ranges [][2]int, baseStyle, hlStyle lipgloss.Style) string {
	if len(ranges) == 0 {
		return baseStyle.Render(text)
	}
	var b strings.Builder
	cur := 0
	for _, r := range ranges {
		start := r[0] - pos
		end := r[1] - pos
		if end <= 0 || start >= len(text) {
			continue
		}
		if start < 0 {
			start = 0
		}
		if end > len(text) {
			end = len(text)
		}
		if start > cur {
			b.WriteString(baseStyle.Render(text[cur:start]))
		}
		b.WriteString(hlStyle.Render(text[start:end]))
		cur = end
	}
	if cur < len(text) {
		b.WriteString(baseStyle.Render(text[cur:]))
	}
	return b.String()
}

func renderInlineDiff(leftText, rightText string, baseLeft, baseRight, hlDel, hlIns lipgloss.Style, contentW int, searchQuery string) (string, string) {
	dmp := diffmatchpatch.New()
	diffs := dmp.DiffMain(leftText, rightText, false)
	dmp.DiffCleanupSemantic(diffs)

	leftRanges := matchRanges(leftText, searchQuery)
	rightRanges := matchRanges(rightText, searchQuery)

	var leftBuf, rightBuf strings.Builder
	leftPos, rightPos := 0, 0

	for _, d := range diffs {
		switch d.Type {
		case diffmatchpatch.DiffEqual:
			leftBuf.WriteString(renderSegment(d.Text, leftPos, leftRanges, baseLeft, searchMatchStyle))
			rightBuf.WriteString(renderSegment(d.Text, rightPos, rightRanges, baseRight, searchMatchStyle))
			leftPos += len(d.Text)
			rightPos += len(d.Text)
		case diffmatchpatch.DiffDelete:
			leftBuf.WriteString(renderSegment(d.Text, leftPos, leftRanges, hlDel, searchMatchStyle))
			leftPos += len(d.Text)
		case diffmatchpatch.DiffInsert:
			rightBuf.WriteString(renderSegment(d.Text, rightPos, rightRanges, hlIns, searchMatchStyle))
			rightPos += len(d.Text)
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

func setCursor(m model, pos int) model {
	m.cursor = clamp(pos, 0, max(0, len(m.lines)-1))
	vis := m.visibleLines()
	if m.cursor < m.offset {
		m.offset = m.cursor
	} else if m.cursor >= m.offset+vis {
		m.offset = m.cursor - vis + 1
	}
	return m
}

func hscroll(s string, offset, w int) string {
	if w <= 0 {
		return ""
	}
	runes := []rune(s)

	// 表示列数ベースで offset 列分スキップ
	col := 0
	start := 0
	for start < len(runes) {
		cw := runewidth.RuneWidth(runes[start])
		if col+cw > offset {
			break
		}
		col += cw
		start++
	}
	runes = runes[start:]

	// 表示列数ベースで w 列に収まるよう切り詰め
	col = 0
	for i, r := range runes {
		cw := runewidth.RuneWidth(r)
		if col+cw > w {
			if col < w {
				return string(runes[:i]) + "…"
			}
			return string(runes[:i])
		}
		col += cw
	}
	return string(runes)
}

func isContextKind(k lineKind) bool {
	return k == kindEqual || k == kindFileHeader || k == kindCollapsed
}

func allHelpItems(editable bool) []string {
	items := []string{
		"↑↓/j/k: 縦スクロール",
		"←→/h/l: 横スクロール",
		"0: 横リセット",
		"n/N: 次/前の変更",
		"]f/[f: 次/前のファイル",
		"g/G: 先頭/末尾",
		"/: 検索",
		"e: 折りたたみ展開",
		"E: 全展開",
		"C: 全折りたたみ",
		"+/-: コンテキスト行数",
		"?: ヘルプを閉じる",
		"q: 終了",
	}
	if editable {
		items = append(items, ">: 左→右に取り込み", "<: 右→左に取り込み", "u: 元に戻す")
	}
	return items
}

func wrapHelpItems(items []string, width int) []string {
	const sep = "  "
	sepW := runewidth.StringWidth(sep)
	var lines []string
	cur := ""
	curW := 0
	for _, item := range items {
		itemW := runewidth.StringWidth(item)
		if cur == "" {
			cur = item
			curW = itemW
		} else if curW+sepW+itemW <= width {
			cur += sep + item
			curW += sepW + itemW
		} else {
			lines = append(lines, cur)
			cur = item
			curW = itemW
		}
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return lines
}

func findSearchMatches(lines []diffLine, query string) []int {
	if query == "" {
		return nil
	}
	lowerQ := strings.ToLower(query)
	var matches []int
	for i, dl := range lines {
		if strings.Contains(strings.ToLower(dl.left), lowerQ) ||
			strings.Contains(strings.ToLower(dl.right), lowerQ) {
			matches = append(matches, i)
		}
	}
	return matches
}

func highlightQuery(text, query string, baseStyle, hlStyle lipgloss.Style) string {
	if query == "" || !strings.Contains(strings.ToLower(text), strings.ToLower(query)) {
		return baseStyle.Render(text)
	}
	lowerText := strings.ToLower(text)
	lowerQ := strings.ToLower(query)
	var b strings.Builder
	i := 0
	for i < len(text) {
		j := strings.Index(lowerText[i:], lowerQ)
		if j == -1 {
			b.WriteString(baseStyle.Render(text[i:]))
			break
		}
		if j > 0 {
			b.WriteString(baseStyle.Render(text[i : i+j]))
		}
		b.WriteString(hlStyle.Render(text[i+j : i+j+len(lowerQ)]))
		i += j + len(lowerQ)
	}
	return b.String()
}

func renderWithSearch(text, query string, baseStyle, hlStyle lipgloss.Style, contentW int) string {
	result := highlightQuery(text, query, baseStyle, hlStyle)
	if pad := contentW - lipgloss.Width(result); pad > 0 {
		result += baseStyle.Render(strings.Repeat(" ", pad))
	}
	return result
}

func currentHunkNum(lines []diffLine, cursor int) int {
	count := 0
	inBlock := false
	for i := 0; i <= cursor && i < len(lines); i++ {
		if !isContextKind(lines[i].kind) {
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

func countDiffBlocks(lines []diffLine) int {
	count := 0
	inBlock := false
	for _, l := range lines {
		if !isContextKind(l.kind) {
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

func nextFileHeader(lines []diffLine, from int) int {
	for i := from + 1; i < len(lines); i++ {
		if lines[i].kind == kindFileHeader {
			return i
		}
	}
	return from
}

func prevFileHeader(lines []diffLine, from int) int {
	for i := from - 1; i >= 0; i-- {
		if lines[i].kind == kindFileHeader {
			return i
		}
	}
	return from
}

func nextChange(lines []diffLine, from int) int {
	for i := from + 1; i < len(lines); i++ {
		if !isContextKind(lines[i].kind) {
			return i
		}
	}
	return from
}

func prevChange(lines []diffLine, from int) int {
	for i := from - 1; i >= 0; i-- {
		if !isContextKind(lines[i].kind) {
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

	folded := foldLines(diffLines, defaultContext)
	m := model{
		lines:      folded,
		leftFile:   leftFile,
		rightFile:  rightFile,
		diffBlocks: countDiffBlocks(diffLines),
		editable:   editable,
		context:    defaultContext,
	}

	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "エラー: %v\n", err)
		os.Exit(1)
	}
}
