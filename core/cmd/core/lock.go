package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ErrAlreadyRunning 是单实例锁被活着的 Core 持有。
var ErrAlreadyRunning = errors.New("cmd/core: 已有 Core 在跑同一个数据目录")

// lock 是数据目录下的单实例锁。
//
// 实现方式有意选"O_EXCL + pid + 死进程接管"而不是文件建议锁：这台机器没有 C 编译器，
// Windows 的 LockFileEx 要 import golang.org/x/sys/windows，那是新的直接依赖（归主控裁定）。
// ponytail: 已知上限是 pid 复用会误判成"还活着"；真撞上再换 LockFileEx 或让端口当锁。
type lock struct {
	path string
	f    *os.File
}

// acquireLock 拿不到锁就返回 ErrAlreadyRunning。takeOver=true 时允许显式接管死锁。
func acquireLock(dir string, takeOver bool) (*lock, error) {
	path := filepath.Join(dir, "core.lock")
	for attempt := 0; attempt < 2; attempt++ {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			if _, werr := fmt.Fprintf(f, "%d\n", os.Getpid()); werr != nil {
				f.Close()
				os.Remove(path)
				return nil, werr
			}
			return &lock{path: path, f: f}, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		stale, rerr := holderIsDead(path)
		if rerr != nil || (!stale && !takeOver) {
			return nil, ErrAlreadyRunning
		}
		// 上一个进程死在路上留了个死锁：清掉再抢一次。
		if err := os.Remove(path); err != nil {
			return nil, fmt.Errorf("接管死锁文件: %w", err)
		}
	}
	return nil, ErrAlreadyRunning
}

func holderIsDead(path string) (bool, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || pid <= 0 {
		return true, nil // 读不出 pid 的文件没资格占着锁
	}
	// Windows：FindProcess 走 OpenProcess，进程已退就报错，这一条足够判定。
	// Unix：FindProcess 几乎总成功（发不出信号也不报错），所以这里判"还活着"，
	// 由 operator 用 -take-over-lock 显式接管 —— 不用 Signal 探活是因为 Go 在 Windows 上
	// 对非 Kill 的 Signal 直接返回 not-supported，判不出真相。
	if _, err := os.FindProcess(pid); err != nil {
		return true, nil
	}
	return false, nil
}

func (l *lock) release() {
	l.f.Close()
	os.Remove(l.path) // 正常退出清干净；被 kill 留下的由接管逻辑或 -take-over-lock 处理
}
