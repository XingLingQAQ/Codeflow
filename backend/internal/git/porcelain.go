package git

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
)

// ErrMalformedPorcelain 表示 NUL 分隔（-z）机器输出无法按格式解析。
// 空路径、半截记录、状态头损坏、R/C 分数缺失或非法都会返回包装该哨兵的错误。
var ErrMalformedPorcelain = errors.New("malformed git -z output")

// PorcelainStatusEntry 是 `git status --porcelain=v1 -z` 的一条记录。
//
// v1 -z 格式（git 2.55 实测）：`XY<SP><path>\0`，rename/copy 为 `XY<SP><new>\0<old>\0`。
// 注意与文本格式 `old -> new` 相反：-z 下新路径在前、源路径在后（git 文档称 field order reversed）。
type PorcelainStatusEntry struct {
	IndexStatus    byte   // X：暂存区状态（' ' 表示无变更）
	WorktreeStatus byte   // Y：工作区状态（' ' 表示无变更）
	Path           string // 目标路径；rename/copy 时为新路径
	OldPath        string // rename/copy 的源路径；非 rename/copy 为空
}

// NameStatusEntry 是 `git diff --name-status -z` 的一条记录。
//
// 格式（git 2.55 实测）：普通状态 `<letter>\0<path>\0`；
// rename/copy 为 `<letter><score>\0<old>\0<new>\0`，score 为三位零填充相似度（如 R079、C100）。
// 注意 R/C 的路径顺序与 status -z 相反：这里源路径在前、新路径在后。
type NameStatusEntry struct {
	Status  byte   // A/C/D/M/R/T/U/X/B 等单字母状态
	Score   int    // R/C 的相似度分数（0-100）；非 R/C 为 -1
	Path    string // 目标路径；R/C 时为新路径
	OldPath string // R/C 的源路径；非 R/C 为空
}

// splitNulFields 按 NUL 切分原始输出。输入为空返回 nil；
// 数据必须以 NUL 结尾，否则视为半截记录并报错。
func splitNulFields(data []byte) ([][]byte, error) {
	if len(data) == 0 {
		return nil, nil
	}
	if data[len(data)-1] != 0 {
		return nil, fmt.Errorf("%w: truncated record: output does not end with NUL", ErrMalformedPorcelain)
	}
	fields := bytes.Split(data, []byte{0})
	// 末尾 NUL 之后必为空片段，丢弃
	return fields[:len(fields)-1], nil
}

func isRenameOrCopy(s byte) bool {
	return s == 'R' || s == 'C'
}

// ParseStatusPorcelainZ 解析 `git status --porcelain=v1 -z` 的原始字节。
// 空输入返回空切片。任何半截记录、空路径或损坏的状态头都返回包装
// ErrMalformedPorcelain 的错误，调用方不得按部分结果继续。
func ParseStatusPorcelainZ(data []byte) ([]PorcelainStatusEntry, error) {
	fields, err := splitNulFields(data)
	if err != nil {
		return nil, err
	}

	entries := make([]PorcelainStatusEntry, 0, len(fields))
	for i := 0; i < len(fields); i++ {
		field := fields[i]
		if len(field) < 3 {
			return nil, fmt.Errorf("%w: record %d: status header too short (%d bytes)", ErrMalformedPorcelain, i, len(field))
		}
		x, y := field[0], field[1]
		if field[2] != ' ' {
			return nil, fmt.Errorf("%w: record %d: expected space after XY status, got %q", ErrMalformedPorcelain, i, field[2])
		}
		path := string(field[3:])
		if path == "" {
			return nil, fmt.Errorf("%w: record %d: empty path", ErrMalformedPorcelain, i)
		}

		entry := PorcelainStatusEntry{
			IndexStatus:    x,
			WorktreeStatus: y,
			Path:           path,
		}
		// v1 中 rename/copy 只出现在 X 列；解析器对两列都接受 R/C 并消费紧随的源路径字段。
		if isRenameOrCopy(x) || isRenameOrCopy(y) {
			i++
			if i >= len(fields) {
				return nil, fmt.Errorf("%w: record %d: rename/copy entry missing source path", ErrMalformedPorcelain, i-1)
			}
			oldPath := string(fields[i])
			if oldPath == "" {
				return nil, fmt.Errorf("%w: record %d: empty rename/copy source path", ErrMalformedPorcelain, i-1)
			}
			entry.OldPath = oldPath
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// ParseNameStatusZ 解析 `git diff --name-status -z` 的原始字节。
// 空输入返回空切片。R/C 记录必须带相似度分数和源/目标两个路径；
// 非 R/C 记录带分数、R/C 缺分数、空路径或半截记录都返回包装
// ErrMalformedPorcelain 的错误。
func ParseNameStatusZ(data []byte) ([]NameStatusEntry, error) {
	fields, err := splitNulFields(data)
	if err != nil {
		return nil, err
	}

	entries := make([]NameStatusEntry, 0, len(fields))
	for i := 0; i < len(fields); i++ {
		token := fields[i]
		if len(token) == 0 {
			return nil, fmt.Errorf("%w: record %d: empty status token", ErrMalformedPorcelain, i)
		}
		letter := token[0]
		rest := token[1:]

		entry := NameStatusEntry{Status: letter, Score: -1}
		if len(rest) > 0 {
			if !isRenameOrCopy(letter) {
				return nil, fmt.Errorf("%w: record %d: unexpected score %q on status %q", ErrMalformedPorcelain, i, rest, letter)
			}
			for _, d := range rest {
				if d < '0' || d > '9' {
					return nil, fmt.Errorf("%w: record %d: non-numeric similarity score %q", ErrMalformedPorcelain, i, rest)
				}
			}
			score, err := strconv.Atoi(string(rest))
			if err != nil || score > 100 {
				return nil, fmt.Errorf("%w: record %d: invalid similarity score %q", ErrMalformedPorcelain, i, rest)
			}
			entry.Score = score
		}

		if isRenameOrCopy(letter) {
			if len(rest) == 0 {
				return nil, fmt.Errorf("%w: record %d: rename/copy status %q missing similarity score", ErrMalformedPorcelain, i, letter)
			}
			// diff --name-status -z：源路径在前、目标路径在后（与 status -z 相反）。
			if i+2 >= len(fields) {
				return nil, fmt.Errorf("%w: record %d: rename/copy entry truncated, need source and target paths", ErrMalformedPorcelain, i)
			}
			oldPath := string(fields[i+1])
			newPath := string(fields[i+2])
			if oldPath == "" || newPath == "" {
				return nil, fmt.Errorf("%w: record %d: empty rename/copy path", ErrMalformedPorcelain, i)
			}
			entry.OldPath = oldPath
			entry.Path = newPath
			i += 2
		} else {
			if i+1 >= len(fields) {
				return nil, fmt.Errorf("%w: record %d: truncated record, missing path", ErrMalformedPorcelain, i)
			}
			path := string(fields[i+1])
			if path == "" {
				return nil, fmt.Errorf("%w: record %d: empty path", ErrMalformedPorcelain, i)
			}
			entry.Path = path
			i++
		}
		entries = append(entries, entry)
	}
	return entries, nil
}
