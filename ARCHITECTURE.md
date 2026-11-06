# 架构

这份文档讲的是"为什么这么切",不是"有哪些文件"。文件清单在 README 末尾。

## 1. 约束推导出的形状

三条硬约束决定了整个架构:

1. **webframe 的 `Frame` 必须建在主 goroutine,`Run()` 会阻塞它。** 一个页面 = 一个进程。
2. **MCP 服务器必须长期存活**,并且要同时服务多个会话(一个模型可能同时开两个页面、一个 `.so`)。
3. **golem 的 emulator 不是并发安全的**(它持有 CPU 与内存状态),但一个会话内的调用必须串行。

于是:

- H5 只能出进程:每个页面一个 `web-host` 子进程,父进程用 JSONL 管道驱动。第一、二条同时满足。
- Android 留在进程内,但用 golem 的 `emulator.Pool` 而不是一把大锁:引擎借给一个调用者,同一时刻只服务一次调用,并发靠引擎数(`"pool": N`,默认 1)。第三条满足,且 `pool:1` 与"一把锁"语义等价。

## 2. 进程与线程模型

```
runvm (MCP server, 主 goroutine = MCP 读循环)
├── goroutine: Session.readLoop        ← 读 web-host 的 stdout
└── child: runvm web-host <target>     ← 主 goroutine = WebKit 事件循环
    └── Xvfb (Linux, 无 DISPLAY 时)     ← Pdeathsig 绑在宿主主线程上
```

`web-host` 的 `main` 第一件事就是 `runtime.LockOSThread()`:WebKit 的事件循环必须待在这一根 OS 线程上,任何被调度走的步骤都会让 WebView 在错误的线程上被访问。Xvfb 的 `Pdeathsig` 因此也绑在主线程——宿主被 `SIGKILL` 时 X server 不会变成孤儿。

父进程侧没有任何线程亲和性要求:MCP 读循环把每个请求丢进 goroutine(最多 32 个在飞),`h5_eval` 的等待、不同 Android 会话的调用都可以真正并行。同一个页面的 `h5_eval` 用一把锁串行——页面是单线程的,交错执行会让副作用顺序不可预测。

## 3. 数据流

### H5 上行(页面 → 模型)

```
页面 console.log / 异常 / webframe.exit
  → webframe 的 hooks(注入的 hooks.js)
  → Go 回调(Console/Exception/Exit)
  → emit({event,level,text})  ← JSON 一行
  → 父进程 readLoop 分类:console/exception 进环形缓冲,ready/done 控制状态
  → h5_console 取走
```

环形缓冲上限 500 行:页面在循环里打日志不能把 MCP 服务器内存吃光。

### H5 下行(模型 → 页面)

```
h5_eval {js}
  → 分配 id,pending[id] = chan
  → 写一行 {"id":N,"op":"eval","js":...}
  → 子进程:注册回调 → Frame.Eval(js, cb)
  → 回调 emit({id:N,ok,result|error})
  → 父进程按 id 唤醒等待者
```

三条退出路径都覆盖了:结果返回、页面在结果到达前关掉(`done`)、超时。超时会删掉 pending 项,不会泄漏。

`Eval` 在 `ready` 之前被调用会阻塞,因为 webframe 要求 hooks 装好之后才能求值;`ready` 事件就是父进程 `h5_open` 等待的那个信号。

### Android

```
android_open {so}
  → 读 ELF(debug/elf)拿 arch/导出表 —— 这一步不启动 emulator
  → emulator.New(Config{SOPath, AssetRoot, Engine})  ← 跑 DT_INIT/init_array/JNI_OnLoad
  → 会话登记
android_call {symbol, args}
  → args 归一化:word / cstring(拷进 guest 内存) / buffer(分配 n 字节,记下地址)
  → emulator.CallSymbol(symbol, values...)
  → 对每个 buffer 读回字节,转 hex + 可打印 ascii
```

buffer 参数是刻意设计:很多 native 接口不返回值,而是往出参里写。没有它,`android_call` 只能覆盖"纯函数"这一小类;有了它,一次调用就能完成"分配 → 调 → 读回"。上限 1 MiB,防止一条调用把地址空间打爆。

## 4. 失败语义

不做"假装健康":

请求并发、响应乱序是 MCP 允许的,顺序只在真正需要的地方由后端保证(一页一个 eval、一引擎一次调用)。

| 场景 | 行为 |
|---|---|
| 工具执行失败 | `isError: true` + 人话错误(模型能读、能自己修),而不是 JSON-RPC error |
| 协议错(未知方法/未知工具) | JSON-RPC error(`-32601` / `-32602`) |
| 页面加载失败 | `h5_open` 立刻失败,并带上页面自己的异常文本 |
| 页面从未 ready | 30 秒超时后杀掉宿主进程再报错,不留孤儿 |
| 会话 ID 不存在 | 明确报错并提示用哪个工具开一个 |
| emulator 启动失败 | 错误里指向 `runvm doctor` |

模型看到的是"下一步该做什么",不是堆栈。

## 5. 生命周期与清理

- 会话是显式资源:`*_close` 释放;`CloseAll()` 在 MCP 进程退出时兜底,杀掉所有子进程、关掉所有 emulator。
- `h5_close` 先礼后兵:发 `{"op":"close"}` → 等 3 秒 → `SIGKILL` → `Wait()` 回收,避免僵尸进程。
- 子进程的 stderr 直接继承父进程,不参与协议:WebKit/GTK 的噪声不能污染 stdout 上的 JSON-RPC。

## 6. 不变式

改代码时该守住的东西:

1. **stdout 只走协议。** 任何日志进 stderr。子进程的 stdout 只能是被解析的 JSON。
2. **一个会话一把锁。** golem 的 emulator 不允许并发调用;H5 的 stdin 写入按行串行。
3. **不重复关闭。** `ready`/`done` 用 `sync.Once`;`Close()` 幂等。
4. **超时必须能收敛。** 每个等待都有上限(ready 30s、eval 30s、close 3s、工具 120s)。
5. **不猜。** 拿不到的值返回错误,不返回零值假装成功。

## 7. 刻意不做的事

- **不做通用 RPC**:MCP 是唯一入口,避免第二套语义漂移。
- **不做截图/PDF**:webframe 没暴露,假实现会误导模型;要做就在 `web-host` 里正经注入 CDP。
- **不做容器编排**:一个 runvm 进程就是一批评测的全部,再往上应该由 CI 负责。
- **不缓存 emulator**:golem 有 engine pool,但那是"多引擎共享只读页"的优化,和 MCP 的会话语义重叠;等有压测数据再说。
