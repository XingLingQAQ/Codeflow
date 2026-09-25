//go:build !windows && !linux

package process

// platformSupported 在未实现进程身份原语的平台上返回 false。
//
// 能力必须如实上报（§28 T1.08.a）：宁可让上层把执行能力标成 capability=false，
// 也不能用“PID 相同就算同一个进程”来假装支持——那正是防误杀要避免的错误。
func platformSupported() bool { return false }

// platformCapture 在未支持的平台上不做任何事，只返回 ErrUnsupportedPlatform。
func platformCapture(pid int) (string, error) { return "", ErrUnsupportedPlatform }

// platformVerify 在未支持的平台上返回 unknown + ErrUnsupportedPlatform，
// 绝不能返回 same。
func platformVerify(id Identity) (VerifyResult, error) { return VerifyUnknown, ErrUnsupportedPlatform }
