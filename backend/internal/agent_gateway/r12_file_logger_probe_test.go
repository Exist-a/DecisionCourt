package agent_gateway

// R12: FileLogger 目录可写性的启动期探测。
//
// 背景（docs/todo/deferred-items-2026-10-07.md R12 / docs/OBSERVABILITY.md §8.3）：
// 线上 `AGENT_GATEWAY_FILE_LOGGER=true` 但容器里 `/app/logs` 不可写 —— 容器跑在
// uid 1001（compose `user`），镜像里只建了 uid 10001，宿主机 bind mount 目录归
// root。FileLogger.Write 每次都失败，但那条 WARN 只在真发生 LLM 调用时才出现：
// 新部署、还没人用的机器上一个字节都不会写，于是"详细日志 + trace 端点将来会是
// 空的"这件事完全不可见。
//
// 这里钉住探测函数本身：可写目录 → nil，不可写路径 → 明确的 error。

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestProbeLogDir_WritableDir 正常场景：目录不存在时会创建，探测通过。
func TestProbeLogDir_WritableDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "logs")
	require.NoError(t, ProbeLogDir(dir))
	assert.DirExists(t, dir)

	// 探测文件必须被清理掉，不留垃圾
	_, err := os.Stat(filepath.Join(dir, WritableProbeName))
	assert.True(t, os.IsNotExist(err), "探测完成后不应残留 %s", WritableProbeName)
}

// TestProbeLogDir_EmptyUsesDefault 空字符串探测默认目录（与 NewFileLogger 同规则）。
// 测试里显式给出目录，避免在仓库根目录留下 logs/。
func TestProbeLogDir_EmptyPathFallsBackToDefault(t *testing.T) {
	// 只验证"空串不会 panic 且会走默认目录规则"：临时把 cwd 切到 TempDir，
	// 于是默认目录 "logs" 落在 TempDir 下而不是仓库里。
	cwd, err := os.Getwd()
	require.NoError(t, err)
	tmp := t.TempDir()
	require.NoError(t, os.Chdir(tmp))
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	require.NoError(t, ProbeLogDir(""))
	assert.DirExists(t, filepath.Join(tmp, defaultLogDir))
}

// TestProbeLogDir_NotADirectory 路径被一个同名文件占住 → MkdirAll 失败。
//
// 用"同名文件"而不是"只读目录"来构造失败：chmod 在 Windows 上不生效，
// 而本项目的单测要能在 Windows 上跑（见 deferred-items 里 `-race` 的记录）。
func TestProbeLogDir_PathBlockedByFile(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "logs")
	require.NoError(t, os.WriteFile(filePath, []byte("not a dir"), 0o644))

	err := ProbeLogDir(filePath)
	require.Error(t, err, "同名文件占位必须报错，不能静默通过")
	assert.Contains(t, err.Error(), "create log dir")
}

// TestProbeLogDir_UnwritableDirIsReported 目录存在但不可写 → 报错并可定位。
//
// 跳过 Windows：POSIX 权限位在 Windows 上不生效（以管理员跑时更是完全无效）。
// CI 的 backend-test 跑在 ubuntu runner 上，这条在 CI 侧有效。
func TestProbeLogDir_UnwritableDirIsReported(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root 无视权限位，跳过")
	}
	dir := filepath.Join(t.TempDir(), "ro")
	require.NoError(t, os.Mkdir(dir, 0o555))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	err := ProbeLogDir(dir)
	if err == nil {
		t.Skip("当前文件系统忽略权限位（例如挂载为无权限检查），跳过")
	}
	assert.Contains(t, err.Error(), "not writable")
}
