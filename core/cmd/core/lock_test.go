package main

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// freePort 绑一个 :0 再立刻放开：拿到的就是"现在没人听、但下一位可能撞上"的那个端口。
// 判活靠"能不能绑上锁里记的端口"，所以测试要的"死锁"就长这样：端口空着、pid 却一定 OpenProcess 得动。
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return port
}

func writeLockFile(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, lockFileName), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestLockJudgesDeadByPortNotPid 就是 REQ-L-1 验收判据①（确定性那一条）的机械版：
// 造一个"pid 一定判得出活着、但那其实不是活 Core"的锁文件 —— 用**本测试进程自己的 pid**，
// `os.FindProcess` 对它永远成功 ⇒ 旧的 pid 启发式必然判"还活着"、拒绝接管；
// 新的端口判据必须判**死**并完成接管。只有真数据能红的不算闭上这一格（§1.1 第 16 条同族）。
func TestLockJudgesDeadByPortNotPid(t *testing.T) {
	dir := t.TempDir()
	writeLockFile(t, dir, fmt.Sprintf("%d\n%d\n", os.Getpid(), freePort(t)))

	l, err := acquireLock(dir, freePort(t))
	if err != nil {
		t.Fatalf("pid 是我自己（OpenProcess 必成功）但端口空着 ⇒ 必须判死并接管，实际 %v", err)
	}
	pid, port := parseLockRecord(readLockFile(filepath.Join(dir, lockFileName)))
	if pid != os.Getpid() || port <= 0 {
		t.Errorf("接管之后锁里该记我的 pid 与我的端口，实际 %d / %d", pid, port)
	}
	l.release()
	if _, err := os.Stat(filepath.Join(dir, lockFileName)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("release 该把锁文件清掉，实际 %v", err)
	}
}

// TestLockPortHeldByNonCore 钉裁定里那句"不许复用那句假原因"：
// 端口被占但占着它的不是 Core ⇒ 必须是 ErrPortTakenByOther，**不能**是 ErrAlreadyRunning
// （报成"已有 Core 在跑"会把 operator 支去杀一个无辜进程）。两种"不像 Core"都要试：
// 答 404 的、答 200 但体不是那份 health 形状的。
func TestLockPortHeldByNonCore(t *testing.T) {
	for _, body := range []string{"not json at all", `{"state":"BOOTING"}`} {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		port := ln.Addr().(*net.TCPAddr).Port
		srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if body == "not json at all" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, body)
		})}
		go func() { _ = srv.Serve(ln) }()
		t.Cleanup(func() { _ = srv.Close() })

		dir := t.TempDir()
		writeLockFile(t, dir, fmt.Sprintf("%d\n%d\n", os.Getpid(), port))
		// 这里三只旋钮都用短值（间隔/期限/单次探针），为的是别在套件里陪生产等满实测推出来的那 1364ms；
		// **窗口本身有没有在跑**由 TestThirdStateBootingCoreIsNotJudgedTakenByOther 钉（它同时钉两种结局）。
		_, err = acquireLockWithGrace(dir, freePort(t), grace{20 * time.Millisecond, 120 * time.Millisecond, 20 * time.Millisecond})
		if !errors.Is(err, ErrPortTakenByOther) {
			t.Errorf("端口被非 Core 占着该报 ErrPortTakenByOther，实际 %v", err)
		}
		if errors.Is(err, ErrAlreadyRunning) {
			t.Errorf("不许把「端口被别人占了」写成「已有 Core 在跑」，实际 %v", err)
		}
		if _, statErr := os.Stat(filepath.Join(dir, lockFileName)); statErr != nil {
			t.Errorf("判成「别人占着」时不该顺手把那份锁清掉（它不是死锁），实际 %v", statErr)
		}
	}
}

// TestLockPortHeldByLiveCore 是同一格的对立面：端口上还答着真 Core 的 /health ⇒ 必须是 ErrAlreadyRunning。
// 用真 boot+serve 出来的 Core，不拿假 body 冒充（假 body 只能证"我认那个形状"，证不了"真 Core 会被认出来"）。
func TestLockPortHeldByLiveCore(t *testing.T) {
	core := bootAndServe(t)
	port := core.ln.Addr().(*net.TCPAddr).Port
	for attempt := 0; attempt < 100 && !healthAnswersAt(port); attempt++ {
		time.Sleep(20 * time.Millisecond)
	}
	if !healthAnswersAt(port) {
		t.Fatal("serve 起来的 Core 没答 /health，这条测的就不是「已有活 Core」")
	}
	dir := t.TempDir()
	writeLockFile(t, dir, fmt.Sprintf("%d\n%d\n", 999999, port))
	if _, err := acquireLock(dir, freePort(t)); !errors.Is(err, ErrAlreadyRunning) {
		t.Errorf("端口上还答着 Core ⇒ 该 ErrAlreadyRunning，实际 %v", err)
	}
}

// TestThirdStateBootingCoreIsNotJudgedTakenByOther 钉 REQ-T21 那第三态（契约① §1.1 第 19 条修法 (a) 的补裁）：
// `boot()` 是"绑端口 → 拿锁 → 开库 → 迁移 → 才 Serve"，所以"端口已绑、HTTP 还不应答"是一段合法窗口。
// 原来两分法在**第一次探针失败**后就判 `ErrPortTakenByOther`，等于把"正在启动"伪装成"别人占了"。
// 现在必须有界重试：等到它开始应答 ⇒ 老实报 `ErrAlreadyRunning`；整段期限都不答 ⇒ 才许报 `ErrPortTakenByOther`。
func TestThirdStateBootingCoreIsNotJudgedTakenByOther(t *testing.T) {
	t.Run("启动中的 Core 会被等到", func(t *testing.T) {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		port := ln.Addr().(*net.TCPAddr).Port
		srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"state":"BOOTING","ready":false,"version":"v","migration_version":1}`)
		})}
		// 150ms 之内只有 listener、没有 Serve：探针会连上、把请求压在 backlog 里等不到响应 —— 就是那段窗口。
		go func() {
			time.Sleep(150 * time.Millisecond)
			_ = srv.Serve(ln)
		}()
		t.Cleanup(func() { _ = srv.Close() })

		dir := t.TempDir()
		writeLockFile(t, dir, fmt.Sprintf("%d\n%d\n", 999999, port))
		_, err = acquireLockWithGrace(dir, freePort(t), grace{20 * time.Millisecond, 2 * time.Second, 20 * time.Millisecond})
		// 旧的"一探定论"在这里必然返回 ErrPortTakenByOther（第一次探针正好落在窗口里）⇒ 这条断言就是那格牙齿。
		if !errors.Is(err, ErrAlreadyRunning) {
			t.Errorf("端口已绑、只是还没 Serve ⇒ 等到它应答之后该报 ErrAlreadyRunning，实际 %v", err)
		}
		if errors.Is(err, ErrPortTakenByOther) {
			t.Errorf("不许把「还在启动的 Core」判成「别的进程占了端口」（REQ-T21 补的就是这一格）：%v", err)
		}
	})

	t.Run("整段期限都不答才判别人占", func(t *testing.T) {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		port := ln.Addr().(*net.TCPAddr).Port
		defer ln.Close() // 全程只有 listener，永远不 Serve：这就是"占着端口又不答话"那一类。

		dir := t.TempDir()
		writeLockFile(t, dir, fmt.Sprintf("%d\n%d\n", 999999, port))
		const deadline = 400 * time.Millisecond
		start := time.Now()
		_, err = acquireLockWithGrace(dir, freePort(t), grace{20 * time.Millisecond, deadline, 20 * time.Millisecond})
		elapsed := time.Since(start)
		if !errors.Is(err, ErrPortTakenByOther) {
			t.Errorf("整段期限不答才该判 ErrPortTakenByOther，实际 %v", err)
		}
		if elapsed < deadline {
			t.Errorf("重试没跑满期限就下结论（%.2fms < %.2fms）⇒ 那是探针一次定论", elapsed.Seconds()*1000, deadline.Seconds()*1000)
		}
		if elapsed > 5*time.Second {
			t.Errorf("有界重试没界住：等了 %.2fs，operator 启动会被拖死", elapsed.Seconds())
		}
	})
}

// TestParseLockRecordAcceptsOldFormat 钉"读不出端口就没资格占锁"这一格的边界：
// 旧格式（只有 pid）、空文件、非数字都要落到 port=0，让 acquireLock 走接管那一支而不是卡死。
func TestParseLockRecordAcceptsOldFormat(t *testing.T) {
	for _, tc := range []struct {
		in       string
		wantPid  int
		wantPort int
	}{
		{"4242\n6100\n", 4242, 6100},
		{"4242\n", 4242, 0},  // 第四十九批之前的旧格式
		{"", 0, 0},           // 半截文件
		{"abc\nxyz\n", 0, 0}, // 读不出数字
	} {
		pid, port := parseLockRecord([]byte(tc.in))
		if pid != tc.wantPid || port != tc.wantPort {
			t.Errorf("parseLockRecord(%q) = %d/%d，期望 %d/%d", tc.in, pid, port, tc.wantPid, tc.wantPort)
		}
	}
}

// TestAcquireLockPublishesPortAtomically 钉发布那一半：新锁必须两行都在，
// 且同一端口不能有两个持有者（第二个必然撞 holderIsCore / holderIsOther，而不是各自成功）。
func TestAcquireLockPublishesPortAtomically(t *testing.T) {
	dir := t.TempDir()
	port := freePort(t)
	l, err := acquireLock(dir, port)
	if err != nil {
		t.Fatal(err)
	}
	defer l.release()
	if pid, got := parseLockRecord(readLockFile(filepath.Join(dir, lockFileName))); got != port || pid != os.Getpid() {
		t.Fatalf("锁里该记 %d/%d，实际 %d/%d", os.Getpid(), port, pid, got)
	}
	// 第二个 Core 拿自己的新端口去发布，撞上的必须是"这份锁活着"那一类判决。
	if _, err := acquireLock(dir, freePort(t)); err == nil {
		t.Fatal("同一目录第二次 acquireLock 不该成功")
	}
	// 顺手把这一批的临时文件形态钉住：tmp 名字不许留在盘上（Link 之后必删）。
	fs, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range fs {
		if filepath.Ext(f.Name()) != "" && f.Name() != lockFileName {
			t.Errorf("发布之后目录里留了个多余文件 %s", f.Name())
		}
	}
}
