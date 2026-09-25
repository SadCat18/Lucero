package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ErrAlreadyRunning 是单实例锁被一个**真的还在答 /health 的 Core** 持有。
var ErrAlreadyRunning = errors.New("cmd/core: 已有 Core 在跑同一个数据目录")

// ErrPortTakenByOther 是"锁文件记的那个端口绑不上，但占着它的不是 Core"。
// 这两句话必须分开报：REQ-L-1 要的诚实处置就是"不许复用那句假原因"——
// 端口被一个非 Core 占着时，说"已有 Core 在跑"会把 operator 支去杀一个无辜的进程。
var ErrPortTakenByOther = errors.New("cmd/core: 数据目录里记录的端口被别的进程占着，不是 Core")

// 单实例锁（REQ-L-1 裁定：契约① §1.1 第 19 条，修法选 (a) 端口当锁）。
//
// 为什么不再是"pid 判活"：Windows 上 `os.FindProcess` 就是 `OpenProcess`，而进程对象在任一句柄关闭前一直存在
// ⇒ "已退"与"`OpenProcess` 失败"不是同一件事，父方还攒着子进程句柄（崩溃测试就是父方）或 pid 被复用都会判成"还活着"
// ⇒ 接管不触发 ⇒ 第二代 Core 直接 ErrAlreadyRunning、`core.endpoint` 不换（test 定档 4/65 ≈ 6%，Wave-2 门每 ≈15 次合验红一次）。
//
// 现在的判活交给 OS：**能不能绑上锁文件里记的那个端口**。绑得上就证明前一个必已死（它的 socket 随进程一起没了），
// 绑不上就对这个端口打 `/api/v1/health`：答得上来（200 + 那四字段之一在）才是"已有 Core 在跑"。
//
// REQ-T21 补的是**第三个态**（契约① §1.1 第 19 条修法 (a) 里那段，09-24 10:2x）：`boot()` 的顺序是
// 绑端口 → 拿锁 → 开库 → 迁移 → 才 `Serve`，所以"**端口已绑、HTTP 还不应答**"是一段合法且必然存在的窗口。
// 一次探针落在这窗口里会答不上来，而两分法会把它判成"端口被非 Core 占了" —— 那是把"正在启动"伪装成"别人占了"，
// 与当初删 pid 启发式同一个毛病。处置改成**有界重试**：固定间隔轮询，开始应答 ⇒ 老实报 `ErrAlreadyRunning`；
// 超过期限仍不应答 ⇒ 才允许报 `ErrPortTakenByOther`。
//
// `-take-over-lock` 那格随之变成多余（裁定原话）：现在 flag 仍在，但只喊一句"已忽略"，
// 因为没有任何一种判决需要人工强制 —— 判得清的就判，判不出端口的文件没资格占着锁。
const (
	lockFileName = "core.lock"
	// 健康探针要有期限：占着这个端口的可能是个不答话的服务，没期限就是启动挂死。
	healthProbeTimeout = 2 * time.Second

	// 下面三个常量不是抄来的整数，是一条实测推出来的：`bootMeasuredColdMax` 是地基读数，
	// 10:35:34→10:35:37 那遍 n=10 **冷数据目录**（口径 = CreateProcess 之前 → 首次 `/api/v1/health` 真答上来）：
	// min 68.24ms / 中位 89.29ms / **max 340.84ms**；同批热目录（迁移空跑）max 105.67ms。原文与命令在第 261 条。
	// 期限取 **4× 冷启最大读数**（向上进到整 ms ⇒ 341ms × 4 = 1364ms）；间隔取那个读数的 1/3，
	// 于是窗口里至少探 4 次，而最坏也只多等一个间隔。**换机器/换磁盘要重量再改，别把这个数当常量搬。**
	bootMeasuredColdMax  = 341 * time.Millisecond
	restartGraceDeadline = 4 * bootMeasuredColdMax
	restartGraceInterval = bootMeasuredColdMax / 3
	restartProbeTimeout  = restartGraceInterval // 单次探针不许超过间隔：否则一次挂住的探针就吃光整个窗口
)

type lock struct {
	path string
	f    *os.File
}

// acquireLock 把"我这个 Core 正在用这个端口"发布成锁文件；拿不到就返回 ErrAlreadyRunning / ErrPortTakenByOther。
//
// 发布是**先写全内容再 Link 进正式名**：`O_CREATE|O_EXCL` 之后才 write 会留一个"文件已存在但内容还没有"的窗口，
// 那个窗口里进程死掉，后来者就读不到端口、也就永远证明不了前一个已死。硬链接在同一目录内是原子的，
// 目标已存在时 `os.Link` 报 EEXIST ⇒ 正好就是"锁已被占"的判据。
func acquireLock(dir string, port int) (*lock, error) {
	return acquireLockWithGrace(dir, port, productionGrace)
}

// grace 是"端口绑不上之后怎么等"的三只旋钮：轮询间隔、总期限、**单次探针的超时**。
// 第三只必须能调，否则这条判据没有牙：生产探针给到 2s 的话，一个还在启动的 Core 会在探针在途时被 Serve 接住，
// "有界重试"与"一次探针"看起来一样 —— 只有把探针预算压到间隔量级，两种写法才会分出两种结局。
type grace struct {
	interval, deadline, probe time.Duration
}

var productionGrace = grace{
	interval: restartGraceInterval,
	deadline: restartGraceDeadline,
	probe:    restartProbeTimeout,
}

// acquireLockWithGrace 把第三态那三个旋钮做成参数：生产走上面那组实测推出来的常量，
// 测试要的是"能在几十毫秒里把两种结局各看一遍"，而不是陪生产等满 1364ms。
func acquireLockWithGrace(dir string, port int, g grace) (*lock, error) {
	path := filepath.Join(dir, lockFileName)
	for attempt := 0; attempt < 2; attempt++ {
		err := publishLock(path, port)
		if err == nil {
			f, oerr := os.Open(path) // 持一个打开的句柄到 release：别人删不掉我这一份名字
			if oerr != nil {
				os.Remove(path)
				return nil, oerr
			}
			return &lock{path: path, f: f}, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		verdict, recorded := judgeHolder(path, g)
		if verdict == holderIsCore {
			return nil, ErrAlreadyRunning
		}
		if verdict == holderIsOther {
			return nil, fmt.Errorf("%w（端口 %d，%s 内未答 /api/v1/health）", ErrPortTakenByOther, recorded, g.deadline)
		}
		// holderFree（端口绑得上 ⇒ 前一个必已死）与 holderNoPort（读不出端口 ⇒ 当前二进制不可能在活着时发布过它，
		// 只能是旧格式或半截文件）都清掉再抢一次。后者就是崩溃测试与升级后第一次启动要的那条路。
		if err := os.Remove(path); err != nil {
			return nil, fmt.Errorf("接管死锁文件: %w", err)
		}
	}
	return nil, ErrAlreadyRunning
}

type holderVerdict int

const (
	holderFree holderVerdict = iota
	holderIsCore
	holderIsOther
	holderNoPort
)

// judgeHolder 读锁文件、拿里面记的端口做网络判活。返回 (判决, 记着的端口)。
// 绑不上之后**不是**一次定论：先按固定间隔重试到期限（REQ-T21 第三态），期限内答上来才算"已有 Core"。
func judgeHolder(path string, g grace) (holderVerdict, int) {
	pid, port := parseLockRecord(readLockFile(path))
	if port <= 0 || port > 65535 {
		// 端口缺失/畸形 ⇒ 这份记录不可能是当前二进制在活着时发布出来的（发布路径要求先有端口）。
		return holderNoPort, 0
	}
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err == nil {
		ln.Close()
		// 喊一声是谁的锁被接管了：崩溃重启是常规路径，operator 需要看得见"我动的是哪一份"。
		log.Printf("core: 接管死锁 %s（记的 pid=%d、端口=%d 已绑得上 ⇒ 那个进程必已退）", path, pid, port)
		return holderFree, port
	}
	if waitForHealth(port, g) {
		return holderIsCore, port
	}
	log.Printf("core: 锁 %s 记的端口 %d 绑不上，且 %s 内没答 /api/v1/health ⇒ 判成「别的进程占着端口」（不是「已有 Core 在跑」）",
		path, port, g.deadline)
	return holderIsOther, port
}

// waitForHealth 是有界重试那一腿：固定间隔轮询，直到对方开始应答（true）或期限到（false）。
// 单次探针用 g.probe（与间隔同量级），不是 `healthProbeTimeout` 那 2 秒 —— 正在启动的 Core 会把 SYN 收进
// backlog 却不答，拿 2s 去探的话一个窗口里只探得了两次，"重试"就成了装饰，而且和"一次探针"分不出结局。
func waitForHealth(port int, g grace) bool {
	until := time.Now().Add(g.deadline)
	for {
		if healthAnswerWithin(port, g.probe) {
			return true
		}
		remaining := time.Until(until)
		if remaining <= 0 {
			return false
		}
		if remaining < g.interval {
			time.Sleep(remaining)
			continue
		}
		time.Sleep(g.interval)
	}
}

func readLockFile(path string) []byte {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return b
}

// publishLock 先写 `<文件>.tmp-<pid>`（内容已经齐），再 Link 成正式名；目标存在时 Link 报 EEXIST。
func publishLock(path string, port int) error {
	tmp := path + fmt.Sprintf(".tmp-%d", os.Getpid())
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	// pid 留在第一格只为人读现场服务；判活不再看它（REQ-L-1）。
	_, werr := fmt.Fprintf(f, "%d\n%d\n", os.Getpid(), port)
	cerr := f.Close()
	if werr != nil || cerr != nil {
		os.Remove(tmp)
		return errors.Join(werr, cerr)
	}
	err = os.Link(tmp, path)
	os.Remove(tmp)
	return err
}

// parseLockRecord 认两行（pid / 端口）；只有一行（第四十九批之前的旧格式）也认，端口按 0 处理。
func parseLockRecord(b []byte) (int, int) {
	lines := strings.Fields(strings.TrimSpace(string(b)))
	if len(lines) == 0 {
		return 0, 0
	}
	pid, _ := strconv.Atoi(lines[0])
	if len(lines) < 2 {
		return pid, 0
	}
	port, _ := strconv.Atoi(lines[1])
	return pid, port
}

// healthAnswersAt 用默认那份 2s 期限探一次（单次判活，例如测试里等一个真 Core 起来）。
func healthAnswersAt(port int) bool { return healthAnswerWithin(port, healthProbeTimeout) }

// healthAnswerWithin 就是"答得上来"的落地：200 + 体里有 Core 那份 health 的字段。
// 超时/连不上/不是这个形状都算答不上来 —— 那正是"占着端口的不是 Core"（或还没 Serve 的 Core，见 waitForHealth）。
func healthAnswerWithin(port int, timeout time.Duration) bool {
	cl := &http.Client{Timeout: timeout}
	resp, err := cl.Get(fmt.Sprintf("http://127.0.0.1:%d/api/v1/health", port))
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	var h struct {
		State string `json:"state"`
		Ready *bool  `json:"ready"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&h); err != nil {
		return false
	}
	return h.State != "" && h.Ready != nil
}

func (l *lock) release() {
	if l.f != nil {
		l.f.Close()
	}
	os.Remove(l.path) // 正常退出清干净；被 kill 留下的由端口判活自动接管
}
